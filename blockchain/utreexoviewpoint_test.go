// Copyright (c) 2021-2022 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"crypto/sha256"
	"testing"

	"github.com/btcsuite/btcd/wire/v2"
	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/bip182"
	"github.com/utreexo/utreexod/bip183"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/txscript"
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
			block := btcutil.NewBlock(&wire.MsgBlock{
				Transactions: []*wire.MsgTx{{
					TxIn: []*wire.TxIn{{PreviousOutPoint: wire.OutPoint{Index: wire.MaxPrevOutIndex}}},
					TxOut: []*wire.TxOut{
						{Value: 1, PkScript: []byte{txscript.OP_TRUE}},
						{Value: 2, PkScript: []byte{txscript.OP_TRUE}},
					},
				}},
			})
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
	prev *btcutil.Block, spend bip182.LeafData,
	txs func(cb, originTx *wire.MsgTx) []*wire.MsgTx) *btcutil.Block {

	t.Helper()

	base, _ := NewBlock(chain, prev,
		spendableOutsFromLeaves([]bip182.LeafData{spend}))
	msgBlock := base.MsgBlock()
	cb, originTx := msgBlock.Transactions[0], msgBlock.Transactions[1]
	msgBlock.Transactions = append([]*wire.MsgTx{cb}, txs(cb, originTx)...)

	block := btcutil.NewBlock(msgBlock)
	block.SetHeight(base.Height())
	msgBlock.Header.MerkleRoot = CalcMerkleRoot(block.Transactions(), false)
	require.True(t, SolveBlock(&msgBlock.Header), "unable to solve block")

	return block
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
			chain, params, _, tearDown := countingUtreexoTestChain(t,
				"same-block-spend")
			defer tearDown()

			genesis := btcutil.NewBlock(params.GenesisBlock)
			genesis.SetHeight(0)
			proofState := newTestUtreexoProofState()
			firstBlock, firstAdds := proofState.newBlock(t, chain, genesis, nil)
			processBlock(t, chain, firstBlock, true)

			spends := firstAdds[:1]
			block := newSameBlockSpendBlock(t, chain, firstBlock,
				spends[0], test.txs)
			proofState.attachUData(t, block, spends)
			if test.valid {
				processBlock(t, chain, block, true)
				return
			}

			_, _, err := chain.ProcessBlock(block, BFNone)
			require.Error(t, err, "ProcessBlock accepted an invalid block")
			var rerr RuleError
			require.ErrorAs(t, err, &rerr)
			require.Equal(t, test.code, rerr.ErrorCode)

			node := chain.index.LookupNode(block.Hash())
			require.NotNil(t, node, "block is not in the block index")
			require.True(t, chain.index.NodeStatus(node).KnownValidateFailed(),
				"block is not marked invalid")
		})
	}
}
