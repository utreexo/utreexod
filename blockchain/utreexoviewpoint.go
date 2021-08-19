// Copyright (c) 2015-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"bytes"
	"fmt"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/mit-dci/utreexo/accumulator"
	"github.com/mit-dci/utreexo/util"
)

// UtreexoViewpoint is the compact state of the chainstate using the utreexo accumulator
type UtreexoViewpoint struct {
	accumulator *accumulator.Pollard
}

// Modify takes an ublock and adds the utxos and deletes the stxos from the utreexo state
func (uview *UtreexoViewpoint) Modify(block *btcutil.Block) error {
	// Check that UData field isn't nil before doing anything else.
	if block.MsgBlock().UData == nil {
		return fmt.Errorf("UtreexoViewpoint.Modify(): block.MsgBlock().UData is nil. " +
			"Cannot validate utreexo accumulator proof")
	}
	ud := block.MsgBlock().UData

	// outskip is all the txOuts that are referenced by a txIn in the same block
	// outCount is the count of all outskips.
	_, outCount, _, outskip := util.DedupeBlock(block)

	// grab the "nl" (numLeaves) which is number of all the utxos currently in the
	// utreexo accumulator. h is the height of the utreexo accumulator
	nl, h := uview.accumulator.ReconstructStats()
	err := ProofSanity(block, nl, h)
	if err != nil {
		return err
	}

	// make slice of hashes from leafdata. These are the hash commitments
	// to be proven.
	delHashes := make([]accumulator.Hash, len(ud.LeafDatas))
	for i := range ud.LeafDatas {
		delHashes[i] = accumulator.Hash(*ud.LeafDatas[i].LeafHash())
	}

	// IngestBatchProof first checks that the utreexo proofs are valid. If it is valid,
	// it readys the utreexo accumulator for additions/deletions.
	err = uview.accumulator.IngestBatchProof(delHashes, ud.AccProof)
	if err != nil {
		return err
	}

	// Remember is used to keep some utxos that will be spent in the near future
	// so that the node won't have to re-download those UTXOs over the wire.
	remember := make([]bool, len(block.MsgBlock().UData.TxoTTLs))
	for i, ttl := range block.MsgBlock().UData.TxoTTLs {
		// If the time-to-live value is less than the chosen amount of blocks
		// then remember it.
		if ttl == 0 {
			remember[i] = false
		} else {
			remember[i] = ttl < uview.accumulator.Lookahead
		}
	}

	// Make the now verified utxos into 32 byte leaves ready to be added into the
	// utreexo accumulator.
	leaves := BlockToAddLeaves(block, remember, outskip, outCount)

	// Add the utxos into the accumulator
	err = uview.accumulator.Modify(leaves, ud.AccProof.Targets)
	if err != nil {
		return err
	}

	return nil
}

// ProofSanity checks the consistency of a UBlock.  Does the proof prove
// all the inputs in the block?
func ProofSanity(block *btcutil.Block, nl uint64, h uint8) error {
	// get the outpoints that need proof
	proveOPs := util.BlockToDelOPs(block)

	ud := block.MsgBlock().UData
	// ensure that all outpoints are provided in the extradata
	if len(proveOPs) != len(ud.LeafDatas) {
		err := fmt.Errorf("height %d %d outpoints need proofs but only %d proven\n",
			ud.Height, len(proveOPs), len(ud.LeafDatas))
		return err
	}

	for i := range ud.LeafDatas {
		if proveOPs[i].Hash != ud.LeafDatas[i].OutPoint.Hash ||
			proveOPs[i].Index != ud.LeafDatas[i].OutPoint.Index {
			err := fmt.Errorf("block/utxoData mismatch %s v %s\n",
				proveOPs[i].String(), ud.LeafDatas[i].OutPoint.String())
			return err
		}
	}

	// make sure the udata is consistent, with the same number of leafDatas
	// as targets in the accumulator batch proof
	if len(ud.AccProof.Targets) != len(ud.LeafDatas) {
		fmt.Printf("Verify failed: %d targets but %d leafdatas\n",
			len(ud.AccProof.Targets), len(ud.LeafDatas))
	}

	return nil
}

// BlockToAdds turns all the new utxos in a msgblock into leafTxos
// uses remember slice up to number of txos, but doesn't check that it's the
// right length.  Similar with skiplist, doesn't check it.
func BlockToAddLeaves(block *btcutil.Block,
	remember []bool, skiplist []uint32,
	outCount int) (leaves []accumulator.Leaf) {

	// We're overallocating a little bit since all the unspendables
	// won't be appended. It's ok though for the pre-allocation savings.
	leaves = make([]accumulator.Leaf, 0, outCount-len(skiplist))

	var txonum uint32
	for coinbase, tx := range block.Transactions() {
		for outIdx, txOut := range tx.MsgTx().TxOut {
			// Skip all the OP_RETURNs
			if util.IsUnspendable(txOut) {
				txonum++
				continue
			}
			// Skip txos on the skip list
			if len(skiplist) > 0 && skiplist[0] == txonum {
				skiplist = skiplist[1:]
				txonum++
				continue
			}

			op := wire.OutPoint{
				Hash:  *tx.Hash(),
				Index: uint32(outIdx),
			}

			stxo := wire.SpentTxOut{
				Amount:     txOut.Value,
				PkScript:   txOut.PkScript,
				Height:     block.Height(),
				IsCoinBase: coinbase == 0,
			}

			var leaf = wire.LeafData{
				BlockHash: block.Hash(),
				OutPoint:  &op,
				Stxo:      &stxo,
			}

			uleaf := accumulator.Leaf{
				Hash: accumulator.Hash(*leaf.LeafHash()),
			}

			if len(remember) > int(txonum) {
				uleaf.Remember = remember[txonum]
			}

			leaves = append(leaves, uleaf)
			txonum++
		}
	}
	return
}

// BlockToDelLeaves takes a non-utreexo block and stxos and turns the block into
// leaves that are to be deleted.
func BlockToDelLeaves(stxos []SpentTxOut, block *btcutil.Block, inskip []uint32) (
	delLeaves []wire.LeafData, err error) {
	var blockInputs int
	var blockInIdx uint32
	for idx, tx := range block.Transactions() {
		if idx == 0 {
			// coinbase can have many inputs
			blockInIdx += uint32(len(tx.MsgTx().TxIn))
			continue
		}

		for _, txIn := range tx.MsgTx().TxIn {
			blockInputs++
			// Skip txos on the skip list
			if len(inskip) > 0 && inskip[0] == blockInIdx {
				inskip = inskip[1:]
				blockInIdx++
				continue
			}

			op := wire.OutPoint{
				Hash:  txIn.PreviousOutPoint.Hash,
				Index: txIn.PreviousOutPoint.Index,
			}

			stxo := wire.SpentTxOut{
				Amount:     stxos[blockInIdx-1].Amount,
				PkScript:   stxos[blockInIdx-1].PkScript,
				Height:     stxos[blockInIdx-1].Height,
				IsCoinBase: stxos[blockInIdx-1].IsCoinBase,
			}

			var leaf = wire.LeafData{
				// TODO fetch block hash and add it to the data
				// to be commited to.
				//BlockHash: hash,
				OutPoint: &op,
				Stxo:     &stxo,
			}

			delLeaves = append(delLeaves, leaf)
			blockInIdx++
		}
	}

	// just an assertion to check the code is correct. Should never happen
	if blockInputs != len(stxos) {
		return nil, fmt.Errorf(
			"block height: %v, hash:%x, has %v txs but %v stxos",
			block.Height(), block.Hash(), len(block.Transactions()), len(stxos))
	}

	return
}

// GetRoots returns the utreexo roots of the current UtreexoViewpoint.
//
// This function is NOT safe for concurrent access. GetRoots should not
// be called when the UtreexoViewpoint is being modified.
func (uview *UtreexoViewpoint) GetRoots() []*chainhash.Hash {
	roots := uview.accumulator.GetRoots()

	chainhashRoots := make([]*chainhash.Hash, len(roots))

	for i, root := range roots {
		newRoot := chainhash.Hash(root)
		chainhashRoots[i] = &newRoot
	}

	return chainhashRoots
}

// Equal compares the UtreexoViewpoint with the roots that were passed in.
// returns true if they are equal.
//
// This function is NOT safe for concurrent access. Equal should not be called
// when the UtreexoViewpoint is being modified.
func (uview *UtreexoViewpoint) Equal(compRoots []*chainhash.Hash) bool {
	uViewRoots := uview.accumulator.GetRoots()
	if len(uViewRoots) != len(compRoots) {
		log.Criticalf("Length of the given roots differs from the one" +
			"fetched from the utreexoViewpoint.")
		return false
	}

	passedInRoots := make([]accumulator.Hash, len(compRoots))

	for i, compRoot := range compRoots {
		passedInRoots[i] = accumulator.Hash(*compRoot)
	}

	for i, root := range passedInRoots {
		if !bytes.Equal(root[:], uViewRoots[i][:]) {
			log.Criticalf("The compared Utreexo roots differ."+
				"Passed in root:%x\nRoot from utreexoViewpoint:%x\n", uViewRoots[i], root)
			return false
		}
	}

	return true
}

// compareRoots is the underlying method that calls the utreexo accumulator code
func (uview *UtreexoViewpoint) compareRoots(compRoot []accumulator.Hash) bool {
	uviewRoots := uview.accumulator.GetRoots()

	if len(uviewRoots) != len(compRoot) {
		log.Criticalf("Length of the given roots differs from the one" +
			"fetched from the utreexoViewpoint.")
		return false
	}

	for i, root := range compRoot {
		if !bytes.Equal(root[:], uviewRoots[i][:]) {
			log.Debugf("The compared Utreexo roots differ."+
				"Passed in root:%x\nRoot from utreexoViewpoint:%x\n", root, uviewRoots[i])
			return false
		}
	}

	return true
}

// NewUtreexoViewpoint returns an empty UtreexoViewpoint
func NewUtreexoViewpoint() *UtreexoViewpoint {
	return &UtreexoViewpoint{
		accumulator: new(accumulator.Pollard),
	}
}

// IsUtreexoViewActive returns true if the node depends on the utreexoView
// instead of a full UTXO set.  Returns false if it's not.
func (b *BlockChain) IsUtreexoViewActive() bool {
	var utreexoActive bool
	b.chainLock.Lock()
	if b.utreexoView != nil {
		utreexoActive = true
	}
	b.chainLock.Unlock()

	return utreexoActive
}
