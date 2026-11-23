// Copyright (c) 2021-2022 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/wire/v2"
	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/bip182"
	"github.com/utreexo/utreexod/bip183"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/utreexoblock"
)

func TestProcessUDataTTLCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ttls    []bip183.TTLInfo
		wantErr bool
	}{
		{name: "empty", wantErr: true},
		{name: "too few", ttls: []bip183.TTLInfo{{DeathHeight: 2}}, wantErr: true},
		{name: "too many", ttls: []bip183.TTLInfo{{DeathHeight: 2}, {}, {}}, wantErr: true},
		{name: "matching count", ttls: []bip183.TTLInfo{{DeathHeight: 2}, {}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Setup: a coinbase with two accumulator additions.
			block := utreexoblock.New(btcutil.NewBlock(&wire.MsgBlock{
				Transactions: []*wire.MsgTx{{
					TxIn: []*wire.TxIn{{PreviousOutPoint: wire.OutPoint{Index: wire.MaxPrevOutIndex}}},
					TxOut: []*wire.TxOut{
						{Value: 1, PkScript: []byte{txscript.OP_TRUE}},
						{Value: 2, PkScript: []byte{txscript.OP_TRUE}},
					},
				}},
			}))
			block.SetHeight(1)
			block.SetUtreexoTTLs(&bip183.UtreexoTTL{BlockHeight: 1, TTLs: test.ttls})
			view := NewUtreexoViewpoint()
			view.accumulator = *utreexo.InitWithStump(utreexo.Stump{
				Roots: []utreexo.Hash{{1}}, NumLeaves: 1,
			})
			view.agg.InitFromBytes([64]byte{1})
			before := view.CopyWithRoots()

			// A mismatched count must be rejected before changing either
			// the accumulator or the aggregator for the spent addition.
			err := view.ProcessUData(block, nil, &bip183.UData{})
			if test.wantErr {
				require.Error(t, err)
				require.Equal(t, before.accumulator.GetStump(), view.accumulator.GetStump())
				require.Equal(t, before.agg, view.agg)
				return
			}
			require.NoError(t, err)
			require.Equal(t, uint64(3), view.NumLeaves())
		})
	}
}

func TestChainTipProofSerialize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ctp     ChainTipProof
		encoded string
	}{
		{
			name: "Signet tx 5939fa8344355201cf0e2c3a6327a3fa530860a50ded2b0edc4a409589e0e986 vout 0",
			ctp: ChainTipProof{
				ProvedAtHash: newHashFromStr("000000eee99f783553176a208f16460901d9f12fe9163bc1421737c290ca2dc4"),
				AccProof: &utreexo.Proof{
					Targets: []uint64{3378993},
					Proof: []utreexo.Hash{
						utreexo.Hash(*newHashFromStr("145c67c2461101ce37b8c86516f17fb71ad0f42a7a60b7b5164605537faa86c9")),
						utreexo.Hash(*newHashFromStr("849539e61119384f64d9994320cdc697ceb78ef2685a0075a513100b4006b7ab")),
						utreexo.Hash(*newHashFromStr("9284130744b0f52b5b4ba092f64a6241ab4c8e949542ef206c8f85f02368aeb7")),
					},
				},
				HashesProven: []utreexo.Hash{utreexo.Hash(*newHashFromStr("b1544ba433103c2bca6a489d9d878b398d4447dfad3c40c6ac085105bd9eddef"))},
			},
			encoded: "c42dca90c2371742c13b16e92ff1d9010946168f206a1" +
				"75335789fe9ee00000001fe318f330003c986aa7f53054" +
				"616b5b7607a2af4d01ab77ff11665c8b837ce011146c26" +
				"75c14abb706400b1013a575005a68f28eb7ce97c6cd204" +
				"399d9644f381911e6399584b7ae6823f0858f6c20ef429" +
				"5948e4cab41624af692a04b5b2bf5b0440713849201000" +
				"000efdd9ebd055108acc6403caddf47448d398b879d9d4" +
				"86aca2b3c1033a44b54b1",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			gotString := test.ctp.String()
			require.Equalf(t, test.encoded, gotString, "encoded string mismatch")

			var gotCtp ChainTipProof
			require.NoErrorf(t, gotCtp.DecodeString(test.encoded), "DecodeString failed")
			require.Equalf(t, test.ctp, gotCtp, "decoded chain tip proof mismatch")
		})
	}
}

func TestUtreexoViewpointCopyWithRoots(t *testing.T) {
	t.Parallel()

	hash := func(label string) utreexo.Hash {
		return utreexo.Hash(sha256.Sum256([]byte(label)))
	}

	setAgg := func(u *UtreexoViewpoint, seed byte) {
		var aggBytes [64]byte
		for i := range aggBytes {
			aggBytes[i] = seed + byte(i)
		}
		u.agg.InitFromBytes(aggBytes)
	}

	tests := []struct {
		name   string
		setup  func(orig *UtreexoViewpoint)
		mutate func(orig *UtreexoViewpoint)
	}{
		{
			name: "single-root",
			setup: func(orig *UtreexoViewpoint) {
				h := hash("leaf-1")
				left := hash("left")
				right := hash("right")
				orig.accumulator.NumLeaves = 1
				orig.accumulator.TotalRows = 1
				orig.accumulator.Roots = []utreexo.Hash{h}
				orig.accumulator.Nodes.Put(h, utreexo.Node{
					LBelow:   left,
					RBelow:   right,
					AddIndex: 5,
				})
				setAgg(orig, 1)
			},
		},
		{
			name: "multi-root-mutation-resistant",
			setup: func(orig *UtreexoViewpoint) {
				h1 := hash("leaf-1")
				h2 := hash("leaf-2")
				orig.accumulator.NumLeaves = 2
				orig.accumulator.TotalRows = 2
				orig.accumulator.Roots = []utreexo.Hash{h1, h2}
				orig.accumulator.Nodes.Put(h1, utreexo.Node{AddIndex: 1})
				orig.accumulator.Nodes.Put(h2, utreexo.Node{AddIndex: 2})
				setAgg(orig, 42)
			},
			mutate: func(orig *UtreexoViewpoint) {
				if len(orig.accumulator.Roots) > 0 {
					orig.accumulator.Roots[0] = utreexo.Hash{}
				}
				delta := hash("delta")
				orig.agg.Add256((*[32]byte)(&delta))
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			orig := NewUtreexoViewpoint()
			test.setup(orig)

			copyView := orig.CopyWithRoots()

			require.Equal(t, orig.accumulator.NumLeaves, copyView.accumulator.NumLeaves, "num leaves mismatch")
			require.Equal(t, orig.accumulator.TotalRows, copyView.accumulator.TotalRows, "total rows mismatch")
			require.Equal(t, orig.accumulator.Roots, copyView.accumulator.Roots, "roots mismatch")
			require.Equal(t, orig.agg, copyView.agg, "aggregator mismatch")

			for _, root := range copyView.accumulator.Roots {
				node, ok := copyView.accumulator.Nodes.Get(root)
				require.Truef(t, ok, "missing node for root %x", root[:])
				require.Equal(t, utreexo.Node{AddIndex: -1}, node,
					"root node is not a parent of any cached children")
			}

			expectedRoots := append([]utreexo.Hash(nil), copyView.accumulator.Roots...)
			expectedAgg := copyView.agg

			if test.mutate != nil {
				test.mutate(orig)

				require.Equal(t, expectedRoots, copyView.accumulator.Roots, "copy roots mutated")
				require.Equal(t, expectedAgg, copyView.agg, "copy aggregator mutated")
			}
		})
	}
}

// TestCopyWithRootsAppliesProof ensures a copy built by CopyWithRoots can
// still ingest and apply a proof. The copy holds only the roots, so the
// proof must carry every sibling hash needed to rebuild the deletion path.
func TestCopyWithRootsAppliesProof(t *testing.T) {
	t.Parallel()

	// Build an accumulator with enough leaves that the roots have cached
	// children below them.
	orig := NewUtreexoViewpoint()
	leaves := make([]utreexo.Leaf, 4)
	for i := range leaves {
		h := sha256.Sum256([]byte{byte(i)})
		leaves[i] = utreexo.Leaf{Hash: utreexo.Hash(h), Remember: true}
	}
	_, err := orig.Modify(&bip183.UData{}, leaves, nil)
	require.NoError(t, err, "Modify to seed accumulator")

	// Prove a deletion against the original, which still holds the full
	// cached tree.
	delHashes := []utreexo.Hash{leaves[0].Hash}
	proof, err := orig.accumulator.Prove(delHashes)
	require.NoError(t, err, "Prove deletion against original")

	// Apply the proof to a roots-only copy. The proof carries every sibling
	// hash needed to rebuild the deletion path.
	copyView := orig.CopyWithRoots()
	err = copyView.accumulator.Verify(delHashes, proof, false)
	require.NoError(t, err, "Verify proof on copy")
	err = copyView.accumulator.Ingest(delHashes, proof)
	require.NoError(t, err, "Ingest proof on copy")
	err = copyView.accumulator.Modify(nil, delHashes, proof)
	require.NoError(t, err, "Modify copy with proof")
}

// newSpendTx returns a transaction that spends the provided output to
// opTrueScript.
func newSpendTx(spend SpendableOut) *wire.MsgTx {
	spendTx := wire.NewMsgTx(1)
	spendTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: spend.PrevOut,
		Sequence:         wire.MaxTxInSequenceNum,
	})
	spendTx.AddTxOut(wire.NewTxOut(int64(spend.Amount-lowFee),
		opTrueScript))
	// Add a random (prunable) OP_RETURN output to make the txid unique.
	spendTx.AddTxOut(wire.NewTxOut(0, uniqueOpReturnScript()))

	return spendTx
}

// newSameBlockSpendBlock builds a block on top of prev.  The transactions
// after the coinbase are the ones that txs returns, given the coinbase and
// originTx, a transaction that spends the provided leaf.
func newSameBlockSpendBlock(t *testing.T, chain *BlockChain,
	prev *utreexoblock.Block, spend bip182.LeafData,
	txs func(cb, originTx *wire.MsgTx) []*wire.MsgTx) *utreexoblock.Block {

	t.Helper()

	// 1. Build on prev with one spend so the base block supplies both
	// the coinbase and originTx.
	base, _ := NewBlock(chain, prev.Block,
		spendableOutsFromLeaves([]bip182.LeafData{spend}))
	msgBlock := base.MsgBlock()
	cb, originTx := msgBlock.Transactions[0], msgBlock.Transactions[1]

	// 2. Keep the coinbase first and let the test case choose the
	// remaining transactions and their order.
	msgBlock.Transactions = append([]*wire.MsgTx{cb}, txs(cb, originTx)...)

	// 3. Recalculate the merkle root for the final transaction list
	// and solve the updated header.
	block := btcutil.NewBlock(msgBlock)
	block.SetHeight(base.Height())
	msgBlock.Header.MerkleRoot = CalcMerkleRoot(block.Transactions(), false)
	require.True(t, SolveBlock(&msgBlock.Header), "unable to solve block")

	return utreexoblock.New(block)
}

// TestUtreexoSameBlockSpend ensures a compact-state chain accepts a block
// that spends an output created earlier in the same block, and rejects and
// marks invalid, with the error a full node returns, a block whose
// transactions spend outputs in ways the block alone rules out.
func TestUtreexoSameBlockSpend(t *testing.T) {
	tests := []struct {
		name  string
		txs   func(cb, originTx *wire.MsgTx) []*wire.MsgTx
		valid bool
		code  ErrorCode
	}{
		{
			name: "output created earlier",
			txs: func(_, originTx *wire.MsgTx) []*wire.MsgTx {
				spendTx := newSpendTx(MakeSpendableOutForTx(originTx, 0))
				return []*wire.MsgTx{originTx, spendTx}
			},
			valid: true,
		},
		{
			name: "output created later",
			txs: func(_, originTx *wire.MsgTx) []*wire.MsgTx {
				spendTx := newSpendTx(MakeSpendableOutForTx(originTx, 0))
				return []*wire.MsgTx{spendTx, originTx}
			},
			code: ErrMissingTxOut,
		},
		{
			name: "output spent twice",
			txs: func(_, originTx *wire.MsgTx) []*wire.MsgTx {
				spend := MakeSpendableOutForTx(originTx, 0)
				return []*wire.MsgTx{originTx, newSpendTx(spend),
					newSpendTx(spend)}
			},
			code: ErrMissingTxOut,
		},
		{
			name: "output spent before and after it is created",
			txs: func(_, originTx *wire.MsgTx) []*wire.MsgTx {
				spend := MakeSpendableOutForTx(originTx, 0)
				return []*wire.MsgTx{newSpendTx(spend), originTx,
					newSpendTx(spend)}
			},
			code: ErrMissingTxOut,
		},
		{
			name: "output index out of range",
			txs: func(_, originTx *wire.MsgTx) []*wire.MsgTx {
				spend := MakeSpendableOutForTx(originTx, 0)
				spend.PrevOut.Index = uint32(len(originTx.TxOut))
				return []*wire.MsgTx{originTx, newSpendTx(spend)}
			},
			code: ErrMissingTxOut,
		},
		{
			name: "coinbase output",
			txs: func(cb, originTx *wire.MsgTx) []*wire.MsgTx {
				spendTx := newSpendTx(MakeSpendableOutForTx(cb, 0))
				return []*wire.MsgTx{originTx, spendTx}
			},
			code: ErrImmatureSpend,
		},
		{
			name: "output from an earlier block spent twice",
			txs: func(_, originTx *wire.MsgTx) []*wire.MsgTx {
				// Spend the outpoint that originTx already spends.
				spendTx := newSpendTx(SpendableOut{
					PrevOut: originTx.TxIn[0].PreviousOutPoint,
					Amount: btcutil.Amount(
						originTx.TxOut[0].Value),
				})
				return []*wire.MsgTx{originTx, spendTx}
			},
			code: ErrMissingTxOut,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// 1. Give each case its own chain and proof state. Coinbase
			// maturity is one block on this test chain, so the next
			// block can spend the first block's coinbase output.
			chain, params, _, tearDown := countingUtreexoTestChain(t,
				"same-block-spend")
			defer tearDown()

			genesis := utreexoblock.New(btcutil.NewBlock(params.GenesisBlock))
			genesis.SetHeight(0)
			proofState := newTestUtreexoProofState()
			firstBlock, firstAdds := proofState.newBlock(t, chain, genesis, nil)
			processBlock(t, chain, firstBlock, true)

			// 2. Build the block for this case and attach a proof for
			// the output it spends from the first block.
			spends := firstAdds[:1]
			block := newSameBlockSpendBlock(t, chain, firstBlock,
				spends[0], test.txs)
			proofState.attachUData(t, block, spends)

			// 3. Check that a valid block joins the main chain and an
			// invalid block returns the expected RuleError.
			if test.valid {
				processBlock(t, chain, block, true)
				return
			}

			_, _, err := chain.ProcessBlock(block, BFNone)
			require.Error(t, err, "ProcessBlock accepted an invalid block")
			var rerr RuleError
			require.ErrorAs(t, err, &rerr)
			require.Equal(t, test.code, rerr.ErrorCode)

			// 4. Check that the block index also marks the rejected
			// block invalid.
			node := chain.index.LookupNode(block.Hash())
			require.NotNil(t, node, "block is not in the block index")
			require.True(t, chain.index.NodeStatus(node).KnownValidateFailed(),
				"block is not marked invalid")
		})
	}
}

// TestReconstructScript checks that reconstructScript reconstructs the pkScript
// of a spent output from the sigScript or witness of the input spending it.
//
// Each case names the output whose pkScript it uses by the outpoint in the
// comment at the top of the case. reconstructScript hashes the data of the
// last opcode in the sigScript (p2pkh, p2sh) or the last witness item
// (p2wpkh, p2wsh) and puts the hash in the script template of the given
// class. A sigScript that does not end with a push of the pubkey or redeem
// script reconstructs to a different script, and getReconstructScriptType
// uses that mismatch to mark the output as not reconstructable.
//
// sigScript and pkScript are hex with a space between each opcode or push:
//
//	00  OP_0
//	75  OP_DROP
//	76  OP_DUP
//	87  OP_EQUAL
//	88  OP_EQUALVERIFY
//	a9  OP_HASH160
//	ac  OP_CHECKSIG
//
// A byte from 01 to 4b pushes that many following bytes, so 14 pushes 20
// bytes and 20 pushes 32 bytes.
func TestReconstructScript(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sigScript   string               // sigScript of the spending input
		pkScript    string               // pkScript of the spent output
		witness     []string             // witness items of the spending input, plain hex
		class       txscript.ScriptClass // class passed to reconstructScript
		expectErr   bool                 // reconstructScript returns an error
		expectEqual bool                 // the reconstructed script equals pkScript
	}{
		{
			// a44d40b3a14aca3f19ccf47244ef4a70ed02d00f5d840c38756368e9a7cc24b1:4 on testnet3
			name: "uncompressed-p2pkh",
			// OP_DUP OP_HASH160 <20-byte pubkey hash> OP_EQUALVERIFY OP_CHECKSIG
			pkScript: "76 a9 14 " +
				"6b2044146a4438e6e5bfbc65f147afeb64d14fbb " +
				"88 ac",
			// <71-byte signature> <65-byte uncompressed pubkey>
			sigScript: "47 3044022075c0666d413fc85cca94ea2f24adc0fedb61a3ba0fcfb240" +
				"c1a4fd2587b03bf90220525ad4d92c6bf635f8b97c188ebf491c6e342b767a5432f" +
				"318cbb0245a7f64be01 41 04c4bee5e6dbb5c1651437cb4386c1515c7776" +
				"c64535077204c6f24f05a37d04a32bc78beb2193b53b104c9954c44b0ce168bc78efd5f1e1c7db9d6c21b3016599",
			class:       txscript.PubKeyHashTy,
			expectErr:   false,
			expectEqual: true,
		},
		{
			// 32350969d79813e8c6ec33d0bc2cd1e78536391fe795f4369aa38f3a72220c2b:1 on testnet3
			name: "compressed-p2pkh",
			// OP_DUP OP_HASH160 <20-byte pubkey hash> OP_EQUALVERIFY OP_CHECKSIG
			pkScript: "76 a9 14 9e4ebbeb42dfdf60420b45e2c3a65f90e8df7d63 " +
				"88 ac",
			// <72-byte signature> <33-byte compressed pubkey>
			sigScript: "48 3045022100f47da779a8ce9da3a051a1e8f51043e035e02878e74a7b7ba" +
				"96846a460fe5a43022064e04f9e0385aee9fe011b9d51f1c0068244e2fa43e737a990" +
				"261e89b4d1797f01 21 0232dff012d75cb8eb49e760db17f5e2fc5436f738bd" +
				"47922c36d699a7fcd4e403",
			class:       txscript.PubKeyHashTy,
			expectErr:   false,
			expectEqual: true,
		},
		{
			// a44d40b3a14aca3f19ccf47244ef4a70ed02d00f5d840c38756368e9a7cc24b1:4 on testnet3
			name: "uncompressed-p2pkh-with-op-dup-and-op-drop-at-the-end",
			// OP_DUP OP_HASH160 <20-byte pubkey hash> OP_EQUALVERIFY OP_CHECKSIG
			pkScript: "76 a9 14 " +
				"6b2044146a4438e6e5bfbc65f147afeb64d14fbb " +
				"88 ac",
			// <71-byte signature> <65-byte uncompressed pubkey> OP_DUP OP_DROP
			// This is the sigScript of uncompressed-p2pkh with OP_DUP OP_DROP
			// appended.
			// The last opcode, OP_DROP, carries no data, so the reconstructed
			// script hashes empty data and does not match pkScript.
			sigScript: "47 3044022075c0666d413fc85cca94ea2f24adc0fedb61a3ba0fcfb240" +
				"c1a4fd2587b03bf90220525ad4d92c6bf635f8b97c188ebf491c6e342b767a5432f" +
				"318cbb0245a7f64be01 41 04c4bee5e6dbb5c1651437cb4386c1515c7776" +
				"c64535077204c6f24f05a37d04a32bc78beb2193b53b104c9954c44b0ce168bc78efd5f1e1c7db9d6c21b3016599 " +
				"76 75",
			class:       txscript.PubKeyHashTy,
			expectErr:   false,
			expectEqual: false,
		},
		{
			// e244bacd69d8f5ae77c349f0e7e4f977f21f8e219ac8e0a35afd43414efdee90:1 on testnet3
			name: "p2pkh-that-has-a-op-drop-in-the-middle",
			// OP_DUP OP_HASH160 <20-byte pubkey hash> OP_EQUALVERIFY OP_CHECKSIG
			pkScript: "76 a9 14 " +
				"e1c5ba4d1fef0a3c7806603de565929684f9c2b1 " +
				"88 ac",
			// <33-byte pubkey> OP_DROP <72-byte signature> <33-byte pubkey>
			// The last push is still the pubkey, so the reconstructed script
			// matches pkScript.
			sigScript: "21 035922db6794aac9676322fb72d0cf4fb3148770d5a1931de2ab3263b7ec9e8ec9 " +
				"75 48 " +
				"304502210086c4ff9fb0fccd6a626c4e902d4c3c05b32f6abc1cdafc6c4b08ce88ae4bb9e0022" +
				"02367a6cfd15d5aa98903c83f60773616da2b6e01b20c9c76fc8cfbfb5ccbefee01 " +
				"21 0270e4215fbe540cab09ac91c9586eba4fc797537859489f4a23d3e22356f1732f",
			class:       txscript.PubKeyHashTy,
			expectErr:   false,
			expectEqual: true,
		},
		{
			// 8ea059a12212dd6d2ca3a87b9550bbaa290211679a6be990e9ed8152094c987d:0 on testnet3
			name: "p2pkh-with-op-drop-at-the-end",
			// OP_DUP OP_HASH160 <20-byte pubkey hash> OP_EQUALVERIFY OP_CHECKSIG
			pkScript: "76 a9 14 " +
				"00da9fa0c8d40f1344f6488eef501096f05fa80e " +
				"88 ac",
			// <71-byte signature> <33-byte compressed pubkey> OP_DUP OP_DROP
			// The last opcode, OP_DROP, carries no data, so the reconstructed
			// script hashes empty data and does not match pkScript.
			sigScript: "47 3044022100f2325e245f5b69723f7" +
				"429bb280f0f43885b2a395992d82af6441e3c4cf5e" +
				"916021f60e36487e2b7e86569a3d95896c54187d6b" +
				"4a3b61f294d1cb681f182dd238101 " +
				"21 037b80492279732766f5960bd3" +
				"8b18a2ae7089a1efdaf188d2a536bffd3430c574 " +
				"76 75",
			class:       txscript.PubKeyHashTy,
			expectErr:   false,
			expectEqual: false,
		},
		{
			// 37096812a90af1a8a3267d3f9d75111a25e2a8c5411770998e0ee5f75599c9cc:1 on mainnet
			name: "p2sh",
			// OP_HASH160 <20-byte script hash> OP_EQUAL
			pkScript: "a9 14 31aeeadd4e385a424ee51e35e8297c38008f29ff " +
				"87",
			// <22-byte redeem script>. The redeem script is the p2wpkh program
			// OP_0 <20-byte pubkey hash>.
			sigScript: "16 0014844565543a8e39f50801c31659647c9256bb1ac5",
			// The witness of the nested p2wpkh. reconstructScript only reads
			// the sigScript for p2sh.
			witness: []string{
				"30440220388aa0e47897fcd8e57fc64fc678e8bb2e" +
					"23e728e96a3143d2cec851757653d10220" +
					"585031572ab678b453b85e844abd8d9d9a" +
					"87c0e56dddf7eba00248601423c94701",
				"03f7fda7194f762c315362487d2f65f3807d0cd34e" +
					"285bab7c2a5db2155911ece3",
			},
			class:       txscript.ScriptHashTy,
			expectErr:   false,
			expectEqual: true,
		},
		{
			// 8127acdb257b70c74857be882e869e80f081f460f70dc8b6490cd32ecc14c55f:1 on mainnet
			name: "p2wpkh",
			// <signature> <33-byte compressed pubkey>
			witness: []string{
				"304402201c9f9a478cb65b967336c1429bf3e18bbf" +
					"ee3e24de7322a700d4a66f508ca04e022006367533" +
					"2f989a732a8e2225a4327b71774c73d72fcb7558a4" +
					"a3fa5317c569c701",
				"0336bb8a29a7b167a865bc7537b9a192b45f29f772" +
					"c1aed639fdfd818ac7028259"},
			// OP_0 <20-byte pubkey hash>
			pkScript: "00 14 " +
				"8d6046312c5c9d4bf42dc53e1b5a668a23d573eb",
			class:       txscript.WitnessV0PubKeyHashTy,
			expectErr:   false,
			expectEqual: true,
		},
		{
			// b714a52258e3034e532a188433a4506ca806cf1885d21034fb26906cc4a1c3c3:1 on mainnet
			name: "p2wsh",
			// OP_0 <32-byte script hash>
			pkScript: "00 20 701a8d401c84fb13e6baf169d5968" +
				"4e17abd9fa216c8cc5b9fc63d622ff8c58d",
			// <signature> <signature> <2-of-3 multisig witness script>
			witness: []string{
				"30440220770eaadb662c0c899c42ebad0af51870f1" +
					"fef86619ded3a1d58ab749a35d24ac0220" +
					"57af983c31a9ebf5ef512d65116d4b054c" +
					"2dc8d201c390fffaac48ce5d8563ea01",
				"30440220172cf970ebda9bfa1402e0afc075d6fba8" +
					"39ed1195078a7143572e68c087ddd90220" +
					"4eb655648a10229cb158adba184360ee3e" +
					"d13ab8c405c2058f498cc7bac1325701",
				"52210375e00eb72e29da82b89367947f29ef34afb7" +
					"5e8654f6ea368e0acdfd92976b7c2103a1" +
					"b26313f430c4b15bb1fdce663207659d8c" +
					"ac749a0e53d70eff01874496feff2103c9" +
					"6d495bfdd5ba4145e3e046fee45e84a8a4" +
					"8ad05bd8dbb395c011a32cf9f88053ae",
			},
			class:       txscript.WitnessV0ScriptHashTy,
			expectErr:   false,
			expectEqual: true,
		},
		{
			// b714a52258e3034e532a188433a4506ca806cf1885d21034fb26906cc4a1c3c3:1 on mainnet
			// but missing witness. reconstructScript needs the last witness
			// item for p2wsh, so it returns an error.
			name: "p2wsh",
			// OP_0 <32-byte script hash>
			pkScript: "00 20 701a8d401c84fb13e6baf169d5968" +
				"4e17abd9fa216c8cc5b9fc63d622ff8c58d",
			witness:     []string{},
			class:       txscript.WitnessV0ScriptHashTy,
			expectErr:   true,
			expectEqual: false,
		},
		{
			// 4cd658ba8b5f10f2544becf4f1756d5e929d72f0a7372a120cc2affc16679e8d:0 on mainnet
			name: "other",
			// <65-byte uncompressed pubkey> OP_CHECKSIG, a pay to pubkey
			// output, which is not a class reconstructScript supports.
			pkScript: "41 04e76ce66264c3624c94e1a641b8f5" +
				"d953c46866c2c2336506f4135758ae8b386a62767b" +
				"902f98bce197a05a3471de35fd39cb11376482fd41" +
				"ec8f14decced65d3 ac",
			// <73-byte signature>
			sigScript: "49 3046022100ea4af80b1926fe0b555" +
				"f28ed45df7fb4d9f71764c9aaad3e46f4c36921934" +
				"cc6022100ef76bcd57a871129836d7fe3b898e97bf" +
				"6a241e7d81c2ae3b026567b9246c4dc01",
			class:       txscript.PubKeyTy,
			expectErr:   true,
			expectEqual: false,
		},
	}

	for _, test := range tests {
		// Drop the spaces between opcodes and pushes, then decode the hex.
		sigScript := hexToBytes(strings.ReplaceAll(test.sigScript, " ", ""))
		pkScript := hexToBytes(strings.ReplaceAll(test.pkScript, " ", ""))

		// Decode the witness items. Cases that expect an error keep an
		// empty witness.
		var witness wire.TxWitness

		if !test.expectErr {
			for _, witElement := range test.witness {
				wit, err := hex.DecodeString(witElement)
				if err != nil {
					t.Fatalf("%s: unable to decode witness "+
						"element: %v", test.name, err)
				}

				witness = append(witness, wit)
			}
		}

		// Reconstruct the pkScript from the sigScript or witness.
		reconScript, err := reconstructScript(sigScript, witness, test.class)
		if err != nil {
			if !test.expectErr {
				t.Errorf("%s: got unexpected error - %v",
					test.name, err)
			}
			continue
		}

		if test.expectErr {
			t.Errorf("%s: expected an error", test.name)
		}

		// For the four supported classes, the reconstructed script
		// must equal pkScript if expectEqual is set and differ from it
		// if not.
		if (test.class == txscript.PubKeyHashTy) ||
			(test.class == txscript.ScriptHashTy) ||
			(test.class == txscript.WitnessV0PubKeyHashTy) ||
			(test.class == txscript.WitnessV0ScriptHashTy) {

			if bytes.Equal(reconScript, pkScript) != test.expectEqual {
				t.Errorf("%s: expect %s, got %s",
					test.name,
					hex.EncodeToString(pkScript),
					hex.EncodeToString(reconScript))
			}
		} else {
			// Other classes must not reconstruct a script.
			if reconScript != nil {
				t.Errorf("%s: expected nil reconstructed script"+
					"for script class %v", test.name, test.class)
			}
		}
	}
}
