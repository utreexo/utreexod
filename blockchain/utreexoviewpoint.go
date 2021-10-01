// Copyright (c) 2021 The utreexo developers
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

// Modify takes an ublock and adds the utxos and deletes the stxos from the utreexo state.
//
// This function is NOT safe for concurrent access.
func (uview *UtreexoViewpoint) Modify(block *btcutil.Block, bestChain *chainView) error {
	// Check that UData field isn't nil before doing anything else.
	if block.MsgBlock().UData == nil {
		return fmt.Errorf("UtreexoViewpoint.Modify(): block.MsgBlock().UData is nil. " +
			"Cannot validate utreexo accumulator proof")
	}
	ud := block.MsgBlock().UData

	// outskip is all the txOuts that are referenced by a txIn in the same block
	// outCount is the count of all outskips.
	_, outCount, inskip, outskip := util.DedupeBlock(block)

	// Make slice of hashes from the LeafDatas. These are the hash commitments
	// to be proven.
	var delHashes []accumulator.Hash
	if len(ud.LeafDatas) > 0 {
		var err error
		delHashes, err = generateCommitments(ud, block, bestChain, inskip)
		if err != nil {
			return err
		}
	}

	// Grab the outpoints that need their existence proven and check that
	// the udata matches up.
	OPsToProve := util.BlockToDelOPs(block)
	err := ProofSanity(ud, OPsToProve)
	if err != nil {
		return err
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

// ProofSanity checks that the UData that was given proves the same outPoints that
// is included in the corresponding block.
func ProofSanity(ud *wire.UData, outPoints []wire.OutPoint) error {
	// Check that the length is the same.
	if len(outPoints) != len(ud.LeafDatas) {
		err := fmt.Errorf("ProofSanity error. %d outpoints need proofs but %d proven\n",
			len(outPoints), len(ud.LeafDatas))
		return err
	}

	// Check that all the outpoints match up.
	for i := range ud.LeafDatas {
		if outPoints[i].Hash != ud.LeafDatas[i].OutPoint.Hash ||
			outPoints[i].Index != ud.LeafDatas[i].OutPoint.Index {
			err := fmt.Errorf("ProofSanity err: OutPoint mismatch. Expect %s, got %s\n",
				outPoints[i].String(), ud.LeafDatas[i].OutPoint.String())
			return err
		}
	}

	// Final check.  The length of the targets should be the same as the leafdata
	// as targets represent the positions of each of the leafdata hash in the
	// accumulator.
	if len(ud.AccProof.Targets) != len(ud.LeafDatas) {
		return fmt.Errorf("ProofSanity err: %d targets but %d leafdatas\n",
			len(ud.AccProof.Targets), len(ud.LeafDatas))
	}

	return nil
}

// generateCommitments adds in missing information to the passed in compact UData and
// hashes it to recreate the hashes that were commited into the accumulator.  This
// function also fills in the missing outpoint and blockhash information to the compact
// UData, making it full.
//
// This function is safe for concurrent access.
func generateCommitments(ud *wire.UData, block *btcutil.Block, chainView *chainView,
	inskip []uint32) ([]accumulator.Hash, error) {
	if chainView == nil {
		return nil, fmt.Errorf("Passed in chainView is nil. Cannot make compact udata to full")
	}

	// blockInIdx is used to get the indexes of the skips.  ldIdx is used
	// as a separate idx for the LeafDatas.  We need both of them because
	// LeafDatas have already been deduped while the transactions are not.
	var blockInIdx, ldIdx uint32
	delHashes := make([]accumulator.Hash, 0, len(ud.LeafDatas))
	for idx, tx := range block.Transactions() {
		if idx == 0 {
			// coinbase can have many inputs
			blockInIdx += uint32(len(tx.MsgTx().TxIn))
			continue
		}
		for _, txIn := range tx.MsgTx().TxIn {
			// Skip txos on the skip list
			if len(inskip) > 0 && inskip[0] == blockInIdx {
				inskip = inskip[1:]
				blockInIdx++
				continue
			}

			ld := &ud.LeafDatas[ldIdx]

			// Get BlockHash.
			blockNode := chainView.NodeByHeight(ld.Height)
			if blockNode == nil {
				return nil, fmt.Errorf("Couldn't find blockNode for height %d",
					ld.Height)
			}
			ld.BlockHash = blockNode.hash

			// Get OutPoint.
			op := wire.OutPoint{
				Hash:  txIn.PreviousOutPoint.Hash,
				Index: txIn.PreviousOutPoint.Index,
			}
			ld.OutPoint = op

			delHashes = append(delHashes, ld.LeafHash())

			blockInIdx++
			ldIdx++
		}
	}

	return delHashes, nil
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

			var leaf = wire.LeafData{
				BlockHash:  *block.Hash(),
				OutPoint:   op,
				Amount:     txOut.Value,
				PkScript:   txOut.PkScript,
				Height:     block.Height(),
				IsCoinBase: coinbase == 0,
			}

			uleaf := accumulator.Leaf{
				Hash: leaf.LeafHash(),
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
func BlockToDelLeaves(stxos []SpentTxOut, chain *BlockChain, block *btcutil.Block, inskip []uint32) (
	delLeaves []wire.LeafData, err error) {

	if chain == nil {
		return nil, fmt.Errorf("Passed in chain is nil. Cannot make delLeaves")
	}

	var blockInIdx uint32
	for idx, tx := range block.Transactions() {
		if idx == 0 {
			// coinbase can have many inputs
			blockInIdx += uint32(len(tx.MsgTx().TxIn))
			continue
		}

		for _, txIn := range tx.MsgTx().TxIn {
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

			stxo := stxos[blockInIdx-1]
			blockHash, err := chain.BlockHashByHeight(stxo.Height)
			if err != nil {
				return nil, err
			}
			if blockHash == nil {
				return nil, fmt.Errorf("Couldn't find blockhash for height %d",
					stxo.Height)
			}

			var leaf = wire.LeafData{
				BlockHash:  *blockHash,
				OutPoint:   op,
				Amount:     stxo.Amount,
				PkScript:   stxo.PkScript,
				Height:     stxo.Height,
				IsCoinBase: stxo.IsCoinBase,
			}

			delLeaves = append(delLeaves, leaf)
			blockInIdx++
		}
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

// VerifyUData processes the given UData and then verifies that the proof validates
// with the underlying UtreexoViewpoint for the txIns that are given.
//
// NOTE: the caller must not include any txIns for tx that isn't already included
// a block (ex: CPFP txs) as this will make the accumulator verification fail.
//
// The passed in txIns must be in the same order they appear in the transaction.
// A mixed up ordering will make the verification fail.
//
// This function does not modify the underlying UtreexoViewpoint.
// This function is safe for concurrent access.
func (b *BlockChain) VerifyUData(ud *wire.UData, txIns []*wire.TxIn) error {
	// Nothing to prove.
	if len(txIns) == 0 {
		return nil
	}

	// If there is something to prove but ud is nil, return an error.
	if ud == nil {
		return fmt.Errorf("VerifyUData(): passed in UData is nil. " +
			"Cannot validate utreexo accumulator proof")
	}

	// Check that there are equal amount of LeafDatas for txIns.
	if len(txIns) != len(ud.LeafDatas) {
		str := fmt.Sprintf("VerifyUData(): length of txIns and LeafDatas differ. "+
			"%d txIns, but %d LeafDatas. TxIns PreviousOutPoints are:\n",
			len(txIns), len(ud.LeafDatas))
		for _, txIn := range txIns {
			str += fmt.Sprintf("%s\n", txIn.PreviousOutPoint.String())
		}

		return fmt.Errorf(str)
	}

	// Make a slice of hashes from LeafDatas. These are the hash commitments
	// to be proven.
	delHashes := make([]accumulator.Hash, 0, len(ud.LeafDatas))
	for i, txIn := range txIns {
		ld := &ud.LeafDatas[i]

		// Get OutPoint.
		op := wire.OutPoint{
			Hash:  txIn.PreviousOutPoint.Hash,
			Index: txIn.PreviousOutPoint.Index,
		}
		ld.OutPoint = op

		// Only append and try to fetch blockHash for confirmed txs.  Skip
		// all unconfirmed txs.
		if !ld.IsUnconfirmed() {
			// Get BlockHash.
			blockNode := b.bestChain.NodeByHeight(ld.Height)
			if blockNode == nil {
				return fmt.Errorf("Couldn't find blockNode for height %d for outpoint %s",
					ld.Height, txIn.PreviousOutPoint.String())
			}
			ld.BlockHash = blockNode.hash

			delHashes = append(delHashes, ld.LeafHash())
		}
	}

	// Acquire read lock before accessing the accumulator state.
	b.chainLock.RLock()
	defer b.chainLock.RUnlock()

	// VerifyBatchProof checks that the utreexo proofs are valid without
	// mutating the accumulator.
	err := b.utreexoView.accumulator.VerifyBatchProof(delHashes, ud.AccProof)
	if err != nil {
		str := "VerifyBatchProof fail. All txIns-leaf datas:\n"
		for i, txIn := range txIns {
			str += fmt.Sprintf("txIn: %s, leafdata: %s\n", txIn.PreviousOutPoint.String(),
				ud.LeafDatas[i].ToString())
		}
		str += fmt.Sprintf("err: %s", err.Error())
		return fmt.Errorf(str)
	}

	return nil
}
