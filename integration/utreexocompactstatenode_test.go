package integration

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/btcjson"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/integration/rpctest"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
)

// fetchBlocks fetches the blocks for the given block hashes and attaches the
// udata to each of the blocks.
func fetchBlocks(blockhashes []*chainhash.Hash, harness *rpctest.Harness) (
	[]*btcutil.Block, error) {

	blocks := make([]*btcutil.Block, 0, len(blockhashes))
	for _, blockhash := range blockhashes {
		msgBlock, err := harness.Client.GetBlock(blockhash)
		if err != nil {
			return nil, err
		}

		utreexoProof, err := harness.Client.GetUtreexoProof(blockhash)
		if err != nil {
			return nil, err
		}

		jsonToUdata := func(*btcjson.GetUtreexoProofVerboseResult) (*wire.UData, error) {
			lds := []wire.LeafData{}
			for _, ldString := range utreexoProof.TargetPreimages {
				raw, err := hex.DecodeString(ldString)
				if err != nil {
					return nil, err
				}

				ld := new(wire.LeafData)
				err = ld.Deserialize(bytes.NewReader(raw))
				if err != nil {
					return nil, err
				}

				lds = append(lds, *ld)
			}
			proofHashes := []utreexo.Hash{}
			for _, proofString := range utreexoProof.ProofHashes {
				raw, err := hex.DecodeString(proofString)
				if err != nil {
					return nil, err
				}
				proofHashes = append(proofHashes, *((*utreexo.Hash)(raw)))
			}
			accProof := utreexo.Proof{Targets: utreexoProof.ProofTargets, Proof: proofHashes}

			udata := wire.UData{
				AccProof:    accProof,
				LeafDatas:   lds,
				RememberIdx: utreexoProof.RememberIndexes,
			}

			return &udata, nil
		}

		msgBlock.UData, err = jsonToUdata(utreexoProof)
		if err != nil {
			return nil, err
		}

		block := btcutil.NewBlock(msgBlock)
		blocks = append(blocks, block)
	}

	return blocks, nil
}

func TestUtreexoCSN(t *testing.T) {
	bridgeNodeArgs := []string{
		"--flatutreexoproofindex",
	}
	// Set up regtest chain for the bridge node.
	bridgeNode, err := rpctest.New(&chaincfg.RegressionNetParams, nil, bridgeNodeArgs, "")
	if err != nil {
		t.Fatal("TestUtreexoCSN fail. Unable to create primary harness: ", err)
	}
	if err := bridgeNode.SetUp(true, 0); err != nil {
		t.Fatalf("TestUtreexoCSN fail. Unable to setup test chain: %v", err)
	}
	defer bridgeNode.TearDown()

	// Helper function for generating spends.
	genSpend := func(amt btcutil.Amount) (btcutil.Address, *chainhash.Hash) {
		// Grab a fresh address from the wallet.
		addr, err := bridgeNode.NewAddress()
		if err != nil {
			t.Fatalf("unable to get new address: %v", err)
		}

		// Next, send amt BTC to this address, spending from one of our mature
		// coinbase outputs.
		addrScript, err := txscript.PayToAddrScript(addr)
		if err != nil {
			t.Fatalf("unable to generate pkscript to addr: %v", err)
		}
		output := wire.NewTxOut(int64(amt), addrScript)
		txid, err := bridgeNode.SendOutputs([]*wire.TxOut{output}, 10)
		if err != nil {
			t.Fatalf("coinbase spend failed: %v", err)
		}
		return addr, txid
	}

	// Generate 10 mature coinbases.
	mainBranchBlockHashes, err := bridgeNode.Client.Generate(110)
	if err != nil {
		t.Fatal(err)
	}

	// Generate blocks and spends. Generates up to block 170.
	watchAddrs := []btcutil.Address{}
	for i := 0; i < 12; i++ {
		blockhashes, err := bridgeNode.Client.Generate(5)
		if err != nil {
			t.Fatal(err)
		}

		mainBranchBlockHashes = append(mainBranchBlockHashes, blockhashes...)

		addr, _ := genSpend(btcutil.Amount(25 * btcutil.SatoshiPerBitcoin))
		addr2, _ := genSpend(btcutil.Amount(25 * btcutil.SatoshiPerBitcoin))

		watchAddrs = append(watchAddrs, addr)
		watchAddrs = append(watchAddrs, addr2)
	}

	// Invalidate block 151 and create a side branch that will reorg out the
	// main branch for the CSN.
	invalidateBlockHash := mainBranchBlockHashes[150]
	err = bridgeNode.Client.InvalidateBlock(invalidateBlockHash)
	if err != nil {
		t.Fatal(err)
	}

	// Side branch now up to block 160.
	var sideBranchBlockHashes []*chainhash.Hash
	for i := 0; i < 2; i++ {
		blockhashes, err := bridgeNode.Client.Generate(5)
		if err != nil {
			t.Fatal(err)
		}

		sideBranchBlockHashes = append(sideBranchBlockHashes, blockhashes...)

		addr, _ := genSpend(btcutil.Amount(25 * btcutil.SatoshiPerBitcoin))
		addr2, _ := genSpend(btcutil.Amount(25 * btcutil.SatoshiPerBitcoin))

		watchAddrs = append(watchAddrs, addr)
		watchAddrs = append(watchAddrs, addr2)
	}

	// Set up the CSN.
	csnArgs := []string{
		"--utreexo",
		"--watchonlywallet",
	}
	for _, addr := range watchAddrs {
		csnArgs = append(csnArgs, fmt.Sprintf("--registeraddresstowatchonlywallet=%s", addr.String()))
	}
	csn, err := rpctest.New(&chaincfg.RegressionNetParams, nil, csnArgs, "")
	if err != nil {
		t.Fatal("TestUtreexoCSN fail. Unable to create primary harness: ", err)
	}
	if err := csn.SetUp(true, 0); err != nil {
		t.Fatalf("TestUtreexoCSN fail. Unable to setup test chain: %v", err)
	}
	defer csn.TearDown()

	// Sync the CSN up to block 150.
	blocks, err := fetchBlocks(mainBranchBlockHashes[:150], bridgeNode)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range blocks {
		err = csn.Client.SubmitBlock(block, nil)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Submit side chain to the CSN. This will become the main chain.
	sideBranchBlocks, err := fetchBlocks(sideBranchBlockHashes, bridgeNode)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range sideBranchBlocks {
		err = csn.Client.SubmitBlock(block, nil)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Reconsider block 151 on the bridge node.
	err = bridgeNode.Client.ReconsiderBlock(invalidateBlockHash)
	if err != nil {
		t.Fatal(err)
	}

	// Sync the CSN up to block 170 on the now active chain.
	mainBranchBlocks, err := fetchBlocks(mainBranchBlockHashes[150:], bridgeNode)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range mainBranchBlocks {
		submitErr := csn.Client.SubmitBlock(block, nil)

		// If we errored out on a submitblock, write all the blocks that we used in this test to a file.
		if submitErr != nil {
			t.Errorf("TestUtreexoCSN fail on block %s. Error %v",
				block.Hash().String(), submitErr)

			str := ""
			for _, block := range blocks {
				bBytes, err := block.Bytes()
				if err != nil {
					t.Fatalf("Error while writing the blocks that caused the submitblock error. Error: %v", err)
				}
				str += fmt.Sprintf("shared block %s\n", hex.EncodeToString(bBytes))
			}
			for _, block := range sideBranchBlocks {
				bBytes, err := block.Bytes()
				if err != nil {
					t.Fatalf("Error while writing the blocks that caused the submitblock error. Error: %v", err)
				}
				str += fmt.Sprintf("side branch block %s\n", hex.EncodeToString(bBytes))
			}

			for _, block := range mainBranchBlocks {
				bBytes, err := block.Bytes()
				if err != nil {
					t.Fatalf("Error while writing the blocks that caused the submitblock error. Error: %v", err)
				}
				str += fmt.Sprintf("main branch block %s\n", hex.EncodeToString(bBytes))
			}
			currentDir, err := os.Getwd()
			if err != nil {
				t.Fatalf("Error while writing the blocks that caused the submitblock error. Error: %v", err)
			}

			fileName := "TestUtreexoCSNBlocks"
			filePath := filepath.Join(currentDir, fileName)

			f, err := os.Create(filePath)
			if err != nil {
				t.Fatalf("Error while writing the blocks that caused the submitblock error. Error: %v", err)
			}
			_, err = f.WriteString(str)
			if err != nil {
				t.Fatalf("Error while writing the blocks that caused the submitblock error. Error: %v", err)
			}

			t.Fatalf("TestUtreexoCSN fail on block %s. Error %v "+
				"Wrote the blocks that caused the error to %s",
				block.Hash().String(), submitErr, filePath)
		}
	}

	// Get bridgenode and csn chain tips and assert they are the same.
	bridgeNodeChainTips, err := bridgeNode.Client.GetChainTips()
	if err != nil {
		t.Fatal(err)
	}
	bridgeNodeChainTipsMap := make(map[string]*btcjson.GetChainTipsResult, len(bridgeNodeChainTips))
	for _, tip := range bridgeNodeChainTips {
		bridgeNodeChainTipsMap[tip.Hash] = tip
	}

	csnChainTips, err := csn.Client.GetChainTips()
	if err != nil {
		t.Fatal(err)
	}
	for _, csnChainTip := range csnChainTips {
		bridgeNodeTip, found := bridgeNodeChainTipsMap[csnChainTip.Hash]
		if !found {
			t.Fatalf("TestUtreexoCSN fail. Have chain tip with hash %s "+
				"in csn but not in the bridge node", csnChainTip.Hash)
		}

		if bridgeNodeTip.BranchLen != csnChainTip.BranchLen ||
			bridgeNodeTip.Status != csnChainTip.Status ||
			bridgeNodeTip.Height != csnChainTip.Height {

			t.Fatalf("TestUtreexoCSN fail. Chain tip %s mismatch. "+
				"BridgeNode: branchlen %d, status %s, height %d "+
				"CSN: branchlen %d, status %s, height %d", bridgeNodeTip.Hash,
				bridgeNodeTip.BranchLen, bridgeNodeTip.Status, bridgeNodeTip.Height,
				csnChainTip.BranchLen, csnChainTip.Status, csnChainTip.Height,
			)
		}
	}

	// Verify that the proof in the wallet is correct.
	chainTipProof, err := csn.Client.ProveWatchOnlyChainTipInclusion()
	if err != nil {
		t.Fatal(err)
	}
	err = bridgeNode.Client.VerifyUtxoChainTipInclusionProof(chainTipProof.Hex)
	if err != nil {
		t.Fatalf("TestUtreexoCSN fail while verifying chain tip proof for the CSN. Error: %v", err)
	}
}
