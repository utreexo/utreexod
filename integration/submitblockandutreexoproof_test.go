//go:build rpctest
// +build rpctest

package integration

import (
	"testing"

	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/integration/rpctest"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
)

// TestSubmitBlockAndUtreexoProof tests the submitblockandutreexoproof RPC command.
// This RPC allows submitting a block along with its utreexo proof and leaf data
// as separate parameters, which is useful for CSN (Compact State Node) operation.
func TestSubmitBlockAndUtreexoProof(t *testing.T) {
	// Set up a bridge node that can provide utreexo proofs.
	bridgeNodeArgs := []string{
		"--flatutreexoproofindex",
		"--noutreexo",
		"--nobdkwallet",
		"--prune=0",
	}
	bridgeNode, err := rpctest.New(&chaincfg.RegressionNetParams, nil, bridgeNodeArgs, "")
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Unable to create bridge node harness: %v", err)
	}
	if err := bridgeNode.SetUp(true, 0); err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Unable to setup bridge node: %v", err)
	}
	defer bridgeNode.TearDown()

	// Generate some blocks on the bridge node.
	numBlocks := 25
	blockHashes, err := bridgeNode.Client.Generate(uint32(numBlocks))
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Unable to generate blocks: %v", err)
	}

	// Set up a CSN (Compact State Node) that will receive blocks via submitblockandutreexoproof.
	csnArgs := []string{
		"--nobdkwallet",
	}
	csn, err := rpctest.New(&chaincfg.RegressionNetParams, nil, csnArgs, "")
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Unable to create CSN harness: %v", err)
	}
	if err := csn.SetUp(true, 0); err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Unable to setup CSN: %v", err)
	}
	defer csn.TearDown()

	// Fetch blocks with their utreexo proofs from the bridge node.
	blocks, udatas, err := fetchBlocks(blockHashes, bridgeNode)
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Unable to fetch blocks with udata: %v", err)
	}

	// Submit each block to the CSN using submitblockandutreexoproof.
	for i, block := range blocks {
		err = csn.Client.SubmitBlockAndUtreexoProof(block, udatas[i])
		if err != nil {
			t.Fatalf("TestSubmitBlockAndUtreexoProof fail. "+
				"SubmitBlockAndUtreexoProof failed for block %s at height %d: %v",
				block.Hash().String(), i+1, err)
		}
	}

	// Verify that the CSN has the same chain tip as the bridge node.
	bridgeBestHash, bridgeBestHeight, err := bridgeNode.Client.GetBestBlock()
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Unable to get bridge node best block: %v", err)
	}

	csnBestHash, csnBestHeight, err := csn.Client.GetBestBlock()
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Unable to get CSN best block: %v", err)
	}

	if !bridgeBestHash.IsEqual(csnBestHash) {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Best block hash mismatch. "+
			"Bridge node: %s, CSN: %s", bridgeBestHash.String(), csnBestHash.String())
	}

	if bridgeBestHeight != csnBestHeight {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Best block height mismatch. "+
			"Bridge node: %d, CSN: %d", bridgeBestHeight, csnBestHeight)
	}

	// Verify the utreexo roots match between the bridge node and the CSN.
	bridgeRoots, err := bridgeNode.Client.GetUtreexoRoots(bridgeBestHash)
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Unable to get bridge node utreexo roots: %v", err)
	}

	csnRoots, err := csn.Client.GetUtreexoRoots(csnBestHash)
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Unable to get CSN utreexo roots: %v", err)
	}

	if bridgeRoots.NumLeaves != csnRoots.NumLeaves {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. NumLeaves mismatch. "+
			"Bridge node: %d, CSN: %d", bridgeRoots.NumLeaves, csnRoots.NumLeaves)
	}

	if len(bridgeRoots.Roots) != len(csnRoots.Roots) {
		t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Roots length mismatch. "+
			"Bridge node: %d, CSN: %d", len(bridgeRoots.Roots), len(csnRoots.Roots))
	}

	for i, bridgeRoot := range bridgeRoots.Roots {
		if bridgeRoot != csnRoots.Roots[i] {
			t.Fatalf("TestSubmitBlockAndUtreexoProof fail. Root mismatch at index %d. "+
				"Bridge node: %s, CSN: %s", i, bridgeRoot, csnRoots.Roots[i])
		}
	}

	t.Logf("TestSubmitBlockAndUtreexoProof: Successfully submitted %d blocks to CSN via submitblockandutreexoproof", numBlocks)
}

// TestSubmitBlockAndUtreexoProofInvalidProof tests that submitblockandutreexoproof
// correctly rejects blocks with invalid utreexo proofs.
func TestSubmitBlockAndUtreexoProofInvalidProof(t *testing.T) {
	// Set up a bridge node that can provide utreexo proofs.
	bridgeNodeArgs := []string{
		"--flatutreexoproofindex",
		"--noutreexo",
		"--nobdkwallet",
		"--prune=0",
	}
	bridgeNode, err := rpctest.New(&chaincfg.RegressionNetParams, nil, bridgeNodeArgs, "")
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. Unable to create bridge node harness: %v", err)
	}
	if err := bridgeNode.SetUp(true, 0); err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. Unable to setup bridge node: %v", err)
	}
	defer bridgeNode.TearDown()

	// Generate 110 blocks to get mature coinbase outputs (100 for maturity + 10 mature outputs).
	blockHashes, err := bridgeNode.Client.Generate(110)
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. Unable to generate blocks: %v", err)
	}

	// Create a spend to ensure the next block has a non-empty utreexo proof.
	addr, err := bridgeNode.NewAddress()
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. Unable to get new address: %v", err)
	}
	addrScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. Unable to generate pkscript: %v", err)
	}
	output := wire.NewTxOut(int64(10*btcutil.SatoshiPerBitcoin), addrScript)
	_, err = bridgeNode.SendOutputs([]*wire.TxOut{output}, 10)
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. Unable to send outputs: %v", err)
	}

	// Generate a block that includes the spend.
	spendBlockHashes, err := bridgeNode.Client.Generate(1)
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. Unable to generate block with spend: %v", err)
	}
	blockHashes = append(blockHashes, spendBlockHashes...)

	// Set up a CSN.
	csnArgs := []string{
		"--nobdkwallet",
	}
	csn, err := rpctest.New(&chaincfg.RegressionNetParams, nil, csnArgs, "")
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. Unable to create CSN harness: %v", err)
	}
	if err := csn.SetUp(true, 0); err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. Unable to setup CSN: %v", err)
	}
	defer csn.TearDown()

	// Fetch blocks with their utreexo proofs from the bridge node.
	blocks, udatas, err := fetchBlocks(blockHashes, bridgeNode)
	if err != nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. Unable to fetch blocks with udata: %v", err)
	}

	// Submit all blocks except the last one (which has the spend) to sync the CSN.
	for i := 0; i < len(blocks)-1; i++ {
		err = csn.Client.SubmitBlockAndUtreexoProof(blocks[i], udatas[i])
		if err != nil {
			t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. "+
				"SubmitBlockAndUtreexoProof failed for block %d: %v", i+1, err)
		}
	}

	// Now try to submit the last block (with the spend) with a corrupted proof.
	lastBlock := blocks[len(blocks)-1]
	lastUdata := udatas[len(udatas)-1]

	// Verify that this block actually has a non-empty proof (has spends).
	if len(lastUdata.AccProof.Proof) == 0 {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. " +
			"Expected block with spend to have non-empty proof")
	}

	// Corrupt the proof by modifying the first proof hash.
	lastUdata.AccProof.Proof[0][0] ^= 0xFF

	// This should fail because the proof is invalid.
	err = csn.Client.SubmitBlockAndUtreexoProof(lastBlock, lastUdata)
	if err == nil {
		t.Fatalf("TestSubmitBlockAndUtreexoProofInvalidProof fail. " +
			"Expected error when submitting block with corrupted proof, but got none")
	}

	t.Logf("TestSubmitBlockAndUtreexoProofInvalidProof: Correctly rejected block with invalid proof: %v", err)
}
