// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sort"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
)

// UtreexoViewpoint is the compact state of the chainstate using the utreexo accumulator
type UtreexoViewpoint struct {
	// accumulator is the bare-minimum accumulator for the utxo set.
	// It only holds the root hashes and the number of elements in the
	// accumulator.
	accumulator utreexo.MapPollard
}

// CopyWithRoots returns a new utreexo viewpoint with just the roots copied.
func (uview *UtreexoViewpoint) CopyWithRoots() *UtreexoViewpoint {
	newUview := NewUtreexoViewpoint()

	roots := uview.accumulator.GetRoots()
	rootPositions := utreexo.RootPositions(uview.accumulator.NumLeaves, uview.accumulator.TotalRows)
	for i := range roots {
		newUview.accumulator.Nodes.Put(rootPositions[i], utreexo.Leaf{Hash: roots[i]})
	}
	newUview.accumulator.NumLeaves = uview.accumulator.NumLeaves

	return newUview
}

// ProcessUData updates the underlying accumulator. It does NOT check if the verification passes.
func (uview *UtreexoViewpoint) ProcessUData(block *btcutil.Block,
	bestChain *chainView, ud *wire.UData) error {

	// Extracts the block into additions and deletions that will be processed.
	// Adds correspond to newly created UTXOs and dels correspond to STXOs.
	adds, err := ExtractAccumulatorAdds(block, []uint32{})
	if err != nil {
		return err
	}

	dels, err := ExtractAccumulatorDels(block, bestChain, []uint32{})
	if err != nil {
		return err
	}

	// Update the underlying accumulator.
	updateData, err := uview.Modify(ud, adds, dels)
	if err != nil {
		return fmt.Errorf("ProcessUData fail. Error: %v", err)
	}

	// Add the utreexo data to the block.
	block.SetUtreexoUpdateData(updateData)
	block.SetUtreexoAdds(adds)

	return nil
}

// VerifyUData checks the accumulator proof to ensure that the leaf preimages exist in the
// accumulator.
func (uview *UtreexoViewpoint) VerifyUData(block *btcutil.Block,
	bestChain *chainView, ud *wire.UData) error {

	// Extracts the block into additions and deletions that will be processed.
	// Adds correspond to newly created UTXOs and dels correspond to STXOs.
	dels, err := ExtractAccumulatorDels(block, bestChain, []uint32{})
	if err != nil {
		return err
	}

	// Return error if the length of the dels we've extracted do not match the targets.
	if len(dels) != len(ud.AccProof.Targets) {
		return fmt.Errorf("have %d dels but proof proves %d dels",
			len(dels), len(ud.AccProof.Targets))
	}

	// For checking if the block is spending the unspendable utxos that were written
	// over with the historical BIP0030 violations.
	for _, del := range dels {
		if del == block91722UnspendableUtreexoLeafHash {
			return fmt.Errorf("ProcessUData fail. Block %s(%d) attempts "+
				"to spend unspendable leaf %s", block.Hash().String(),
				block.Height(), block91722UnspendableUtreexoLeafHash.String())
		}

		if del == block91812UnspendableUtreexoLeafHash {
			return fmt.Errorf("ProcessUData fail. Block %s(%d) attempts "+
				"to spend unspendable leaf %s", block.Hash().String(),
				block.Height(), block91812UnspendableUtreexoLeafHash.String())
		}
	}

	return uview.accumulator.Verify(dels, ud.AccProof, false)
}

// AddProof first checks that the utreexo proofs are valid. If it is valid,
// it readys the utreexo accumulator for additions/deletions by ingesting the proof.
func (uview *UtreexoViewpoint) AddProof(delHashes []utreexo.Hash, accProof *utreexo.Proof) error {
	err := uview.accumulator.Verify(delHashes, *accProof, true)
	if err != nil {
		return err
	}

	return nil
}

// Modify modifies the utreexo state by adding and deleting elements from the utreexo state.
// The deletions are marked by the accumulator proof in the utreexo data and the additions
// are the leaves passed in.
//
// This function is NOT safe for concurrent access.
func (uview *UtreexoViewpoint) Modify(ud *wire.UData,
	adds []utreexo.Leaf, dels []utreexo.Hash) (*utreexo.UpdateData, error) {

	addHashes := make([]utreexo.Hash, len(adds))
	for i, add := range adds {
		addHashes[i] = add.Hash
	}

	// We have to do this in order to generate the update data for the wallet.
	// TODO: get rid of this once the pollard can generate the update data.
	s := uview.accumulator.GetStump()
	var updateData utreexo.UpdateData
	updateData, err := s.Update(dels, addHashes, ud.AccProof)
	if err != nil {
		return nil, err
	}

	// Ingest and modify the accumulator.
	err = uview.accumulator.Ingest(dels, ud.AccProof)
	if err != nil {
		return nil, err
	}
	err = uview.accumulator.Modify(adds, dels, ud.AccProof)
	if err != nil {
		return nil, err
	}

	return &updateData, nil
}

// blockToDelOPs gives all the UTXOs in a block that need proofs in order to be
// deleted.  All txinputs except for the coinbase input and utxos created
// within the same block (on the skiplist)
func BlockToDelOPs(
	blk *btcutil.Block) []wire.OutPoint {

	transactions := blk.Transactions()
	inCount, _, inskip, _ := DedupeBlock(blk)

	delOPs := make([]wire.OutPoint, 0, inCount-len(inskip))

	var blockInIdx uint32
	for txinblock, tx := range transactions {
		if txinblock == 0 {
			blockInIdx += uint32(len(tx.MsgTx().TxIn)) // coinbase can have many inputs
			continue
		}

		// loop through inputs
		for _, txin := range tx.MsgTx().TxIn {
			// check if on skiplist.  If so, don't make leaf
			if len(inskip) > 0 && inskip[0] == blockInIdx {
				inskip = inskip[1:]
				blockInIdx++
				continue
			}

			delOPs = append(delOPs, txin.PreviousOutPoint)
			blockInIdx++
		}
	}
	return delOPs
}

// DedupeBlock takes a bitcoin block, and returns two int slices: the indexes of
// inputs, and indexes of outputs which can be removed.  These are indexes
// within the block as a whole, even the coinbase tx.
// So the coinbase tx in & output numbers affect the skip lists even though
// the coinbase ins/outs can never be deduped.  it's simpler that way.
func DedupeBlock(blk *btcutil.Block) (inCount, outCount int, inskip []uint32, outskip []uint32) {
	var i uint32
	// wire.Outpoints are comparable with == which is nice.
	inmap := make(map[wire.OutPoint]uint32)

	// go through txs then inputs building map
	for coinbase, tx := range blk.Transactions() {
		if coinbase == 0 { // coinbase tx can't be deduped
			i += uint32(len(tx.MsgTx().TxIn)) // coinbase can have many inputs
			continue
		}
		for _, in := range tx.MsgTx().TxIn {
			inmap[in.PreviousOutPoint] = i
			i++
		}
	}
	inCount = int(i)

	i = 0
	// start over, go through outputs finding skips
	for coinbase, tx := range blk.Transactions() {
		txOut := tx.MsgTx().TxOut
		if coinbase == 0 { // coinbase tx can't be deduped
			i += uint32(len(txOut)) // coinbase can have multiple outputs
			continue
		}

		for outidx := range txOut {
			op := wire.OutPoint{Hash: *tx.Hash(), Index: uint32(outidx)}
			inpos, exists := inmap[op]
			if exists {
				inskip = append(inskip, inpos)
				outskip = append(outskip, i)
			}
			i++
		}
	}
	outCount = int(i)
	// sort inskip list, as it's built in order consumed not created
	sortUint32s(inskip)
	return
}

// ExtractAccumulatorDels extracts the deletions that will be used to modify the utreexo accumulator.
func ExtractAccumulatorDels(block *btcutil.Block, bestChain *chainView, remembers []uint32) (
	[]utreexo.Hash, error) {

	// Check that UData field isn't nil before doing anything else.
	if block.MsgBlock().UData == nil {
		return nil, fmt.Errorf("ExtractAccumulatorDels(): block.MsgBlock().UData is nil. " +
			"Cannot extract utreexo accumulator deletions")
	}

	ud := block.MsgBlock().UData

	_, _, inskip, _ := DedupeBlock(block)

	// Make slice of hashes from the LeafDatas. These are the hash commitments
	// to be proven.
	//
	// NOTE Trying to call ProofSanity() before reconstructUData() will result
	// in an error.
	var delHashes []utreexo.Hash
	if len(ud.LeafDatas) > 0 {
		var err error
		delHashes, err = reconstructUData(ud, block, bestChain, inskip)
		if err != nil {
			return nil, err
		}
	}

	// Grab the outpoints that need their existence proven and check that
	// the udata matches up.
	OPsToProve := BlockToDelOPs(block)
	err := ProofSanity(ud, OPsToProve)
	if err != nil {
		return nil, err
	}

	return delHashes, nil
}

// ExtractAccumulatorAdds extracts the additions that will beused to modify the utreexo accumulator.
func ExtractAccumulatorAdds(block *btcutil.Block, remembers []uint32) (
	[]utreexo.Leaf, error) {

	// outskip is all the txOuts that are referenced by a txIn in the same block
	// outCount is the count of all outskips.
	_, outCount, _, outskip := DedupeBlock(block)

	// Make the now verified utxos into 32 byte leaves ready to be added into the
	// utreexo accumulator.
	leaves := BlockToAddLeaves(block, outskip, remembers, outCount)

	return leaves, nil
}

// it'd be cool if you just had .sort() methods on slices of builtin types...
func sortUint32s(s []uint32) {
	sort.Slice(s, func(a, b int) bool { return s[a] < s[b] })
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

	return nil
}

// ReconstructUData is a wrapper around reconstructUData and provides a convenient
// function for udata reconstruction.
// NOTE: ReconstructUData will only work on udata for blocks that are part of the
// best chain. It will return an error when trying to reconstruct a udata for a
// block in a side chain.
//
// This function is safe for concurrent access.
func (b *BlockChain) ReconstructUData(ud *wire.UData, blockHash chainhash.Hash) ([]utreexo.Hash, error) {
	block, err := b.BlockByHash(&blockHash)
	if err != nil {
		return nil, err
	}

	_, _, inskip, _ := DedupeBlock(block)
	delHashes, err := reconstructUData(ud, block, b.bestChain, inskip)
	if err != nil {
		return nil, err
	}

	return delHashes, nil
}

// reconstructUData adds in missing information to the passed in compact UData and
// makes it full. The hashes returned are the hashes of the individual leaf data
// that were commited into the accumulator.
//
// This function is safe for concurrent access.
func reconstructUData(ud *wire.UData, block *btcutil.Block, chainView *chainView,
	inskip []uint32) ([]utreexo.Hash, error) {
	if chainView == nil {
		return nil, fmt.Errorf("Passed in chainView is nil. Cannot make compact udata to full")
	}

	// blockInIdx is used to get the indexes of the skips.  ldIdx is used
	// as a separate idx for the LeafDatas.  We need both of them because
	// LeafDatas have already been deduped while the transactions are not.
	var blockInIdx, ldIdx uint32
	delHashes := make([]utreexo.Hash, 0, len(ud.LeafDatas))
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
			var err error
			ld, err = reconstructLeafData(ld, txIn, chainView)
			if err != nil {
				return nil, err
			}
			delHashes = append(delHashes, ld.LeafHash())

			blockInIdx++
			ldIdx++
		}
	}

	return delHashes, nil
}

// ReconstructLeafDatas reconstruct the passed in leaf datas with the given txIns.
//
// NOTE: the length of the leafdatas MUST match the TxIns. Otherwise it'll return an error.
func (b *BlockChain) ReconstructLeafDatas(lds []wire.LeafData, txIns []*wire.TxIn) ([]wire.LeafData, error) {
	if len(lds) == 0 {
		return lds, nil
	}

	if len(lds) != len(txIns) {
		err := fmt.Errorf("Can't reconstruct leaf datas.  Have %d txins but %d leaf datas",
			len(txIns), len(lds))
		return nil, err
	}

	for i, txIn := range txIns {
		if lds[i].IsUnconfirmed() {
			continue
		}

		ld, err := reconstructLeafData(&lds[i], txIn, b.bestChain)
		if err != nil {
			return nil, err
		}
		lds[i] = *ld
	}

	return lds, nil
}

// reconstructLeafData reconstructs a single leafdata given the associated txIn and the chainview.
func reconstructLeafData(ld *wire.LeafData, txIn *wire.TxIn, chainView *chainView) (*wire.LeafData, error) {
	// Get BlockHash.
	blockNode := chainView.NodeByHeight(ld.Height)
	if blockNode == nil {
		return nil, fmt.Errorf("Couldn't find blockNode for height %d",
			ld.Height)
	}
	ld.BlockHash = blockNode.hash

	// Get OutPoint.
	ld.OutPoint = txIn.PreviousOutPoint

	if ld.ReconstructablePkType != wire.OtherTy &&
		ld.PkScript == nil {

		var class txscript.ScriptClass

		switch ld.ReconstructablePkType {
		case wire.PubKeyHashTy:
			class = txscript.PubKeyHashTy
		case wire.ScriptHashTy:
			class = txscript.ScriptHashTy
		case wire.WitnessV0PubKeyHashTy:
			class = txscript.WitnessV0PubKeyHashTy
		case wire.WitnessV0ScriptHashTy:
			class = txscript.WitnessV0ScriptHashTy
		}

		scriptToUse, err := txscript.ReconstructScript(
			txIn.SignatureScript, txIn.Witness, class)
		if err != nil {
			return nil, err
		}

		ld.PkScript = scriptToUse
	}

	return ld, nil
}

// IsUnspendable determines whether a tx is spendable or not.
// returns true if spendable, false if unspendable.
func IsUnspendable(o *wire.TxOut) bool {
	switch {
	case len(o.PkScript) > 10000: //len 0 is OK, spendable
		return true
	case len(o.PkScript) > 0 && o.PkScript[0] == 0x6a: // OP_RETURN is 0x6a
		return true
	default:
		return false
	}
}

// BlockToAdds turns the newly created utxos in a block into leaves that will
// be commited to the utreexo accumulator.
//
// skiplist will cause skip indexes of the utxos that match with the ones
// included in the slice. For example, if [0, 3, 11] is given as the skiplist,
// then utxos that appear in the 0th, 3rd, and 11th in the block will
// be skipped over.
func BlockToAddLeaves(block *btcutil.Block, skiplist []uint32, remembers []uint32,
	outCount int) []utreexo.Leaf {

	// Sort first as the below loop expects the remembers to be in order.
	sortUint32s(remembers)

	// We're overallocating a little bit since all the unspendables
	// won't be appended. It's ok though for the pre-allocation savings.
	leaves := make([]utreexo.Leaf, 0, outCount-len(skiplist))

	var txonum uint32
	for coinbase, tx := range block.Transactions() {
		for outIdx, txOut := range tx.MsgTx().TxOut {
			// Skip all the OP_RETURNs
			if IsUnspendable(txOut) {
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

			// Set remember to be true if the current UTXO corresponds
			// to an index that should be remembered.
			remember := false
			if len(remembers) > 0 && remembers[0] == txonum {
				remembers = remembers[1:]
				remember = true
			}

			uleaf := utreexo.Leaf{
				Hash:     leaf.LeafHash(),
				Remember: remember,
			}

			leaves = append(leaves, uleaf)
			txonum++
		}
	}

	return leaves
}

// ExcludedUtxo is the utxo that was excluded because it was spent and created
// within a given block interval.  It includes the creation height and the outpoint
// of the utxo.
type ExcludedUtxo struct {
	// Height is where this utxo was created.
	Height int32

	// Outpoint represents the utxo txid:vout.
	Outpoint wire.OutPoint
}

// BlockToDelLeaves takes a non-utreexo block and stxos and turns the block into
// leaves that are to be deleted.
//
// inskip is an optional argument. inskip will skip indexes of the txIns that match
// with the ones included in the slice. For example, if [0, 3, 11] is given as the
// inskip list, then txIns that appear in the 0th, 3rd, and 11th in the block will
// be skipped over.
//
// NOTE To opt out of the optional arguments inskip, just pass nil.
func BlockToDelLeaves(stxos []SpentTxOut, chain *BlockChain, block *btcutil.Block,
	inskip []uint32) (delLeaves []wire.LeafData, err error) {

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

			var pkType wire.PkType

			scriptType, err := txscript.GetReconstructScriptType(
				txIn.SignatureScript, stxo.PkScript, txIn.Witness)
			if err != nil {
				log.Debugf("GetReconstructScriptType error. %v. "+
					"Defaulting to wire.OtherTy", err)
				scriptType = txscript.NonStandardTy
			}
			switch scriptType {
			case txscript.PubKeyHashTy:
				pkType = wire.PubKeyHashTy
			case txscript.WitnessV0PubKeyHashTy:
				pkType = wire.WitnessV0PubKeyHashTy
			case txscript.ScriptHashTy:
				pkType = wire.ScriptHashTy
			case txscript.WitnessV0ScriptHashTy:
				pkType = wire.WitnessV0ScriptHashTy
			default:
				pkType = wire.OtherTy
			}

			var leaf = wire.LeafData{
				BlockHash:             *blockHash,
				OutPoint:              op,
				Amount:                stxo.Amount,
				ReconstructablePkType: pkType,
				PkScript:              stxo.PkScript,
				Height:                stxo.Height,
				IsCoinBase:            stxo.IsCoinBase,
			}

			delLeaves = append(delLeaves, leaf)
			blockInIdx++
		}
	}

	return
}

// TxToDelLeaves takes a tx and generates the leaf datas for all the inputs.  The
// leaf datas represent a utxoviewpoinnt just for the tx, along with the accumulator
// proof that proves all the txIns' inclusion.
func TxToDelLeaves(tx *btcutil.Tx, chain *BlockChain) ([]wire.LeafData, error) {
	confirmedUtxoView, err := chain.FetchUtxoView(tx)
	if err != nil {
		return nil, err
	}

	// TODO this is dumb since FetchUtxoView never needs to
	// add the outputs.
	prevOut := wire.OutPoint{Hash: *tx.Hash()}
	for txOutIdx := range tx.MsgTx().TxOut {
		prevOut.Index = uint32(txOutIdx)
		confirmedUtxoView.RemoveEntry(prevOut)
	}

	viewLen := len(confirmedUtxoView.Entries())

	// We should have fetched the exact same count as there are txins
	// since unconfimred txs will be in the map as well.
	if viewLen != len(tx.MsgTx().TxIn) {
		err = fmt.Errorf("utxoview fetch fail.  Have %d txIns but %d view entries",
			len(tx.MsgTx().TxIn), viewLen)
		return nil, err
	}

	// Prep the UDatas to be sent over.  These will also be
	// used to generate the accumulator proofs.
	leafDatas := make([]wire.LeafData, 0, viewLen)
	for _, txIn := range tx.MsgTx().TxIn {
		entry := confirmedUtxoView.LookupEntry(txIn.PreviousOutPoint)
		// Only initialize with height of -1 to mark that this
		// tx has not yet been included in a block.
		if entry == nil {
			log.Debugf("Marking %s as uncomfirmed for tx %s",
				txIn.PreviousOutPoint.String(), tx.Hash().String())
			ld := wire.LeafData{}
			ld.SetUnconfirmed()
			leafDatas = append(leafDatas, ld)
			continue
		}
		// Error out if the input being reference is already spent.
		if entry.IsSpent() {
			err := fmt.Errorf("Couldn't generate UData for tx %s "+
				"as input %s being referenced is marked as spent",
				tx.Hash().String(), txIn.PreviousOutPoint.String())
			return nil, err
		}

		blockHash, err := chain.BlockHashByHeight(entry.BlockHeight())
		if err != nil {
			return nil, err
		}
		if blockHash == nil {
			err := fmt.Errorf("Couldn't find blockhash for height %d",
				entry.BlockHeight())
			return nil, err
		}

		var pkType wire.PkType

		scriptType, err := txscript.GetReconstructScriptType(
			txIn.SignatureScript, entry.PkScript(), txIn.Witness)
		if err != nil {
			log.Debugf("GetReconstructScriptType error. %v. "+
				"Defaulting to wire.OtherTy", err)
			scriptType = txscript.NonStandardTy
		}
		switch scriptType {
		case txscript.PubKeyHashTy:
			pkType = wire.PubKeyHashTy
		case txscript.WitnessV0PubKeyHashTy:
			pkType = wire.WitnessV0PubKeyHashTy
		case txscript.ScriptHashTy:
			pkType = wire.ScriptHashTy
		case txscript.WitnessV0ScriptHashTy:
			pkType = wire.WitnessV0ScriptHashTy
		default:
			pkType = wire.OtherTy
		}

		leaf := wire.LeafData{
			BlockHash:             *blockHash,
			OutPoint:              txIn.PreviousOutPoint,
			Amount:                entry.Amount(),
			Height:                entry.BlockHeight(),
			IsCoinBase:            entry.IsCoinBase(),
			ReconstructablePkType: pkType,
		}
		// Copy the key over so it doesn't get dropped while
		// we're still using it.
		//
		// TODO check if this copy actually needs to happen.
		// Might not need it and could save a bit of memory.
		leaf.PkScript = make([]byte, len(entry.PkScript()))
		copy(leaf.PkScript, entry.PkScript())

		leafDatas = append(leafDatas, leaf)
	}

	// The count of leaf datas and txins should always be the same as
	// we account for unconfirmed referenced outputs for tx udata
	// serialization.
	if len(leafDatas) != len(tx.MsgTx().TxIn) {
		err = fmt.Errorf("Failed to generate UData.  Have %d txins but %d leaf datas",
			len(tx.MsgTx().TxIn), len(leafDatas))
		return nil, err
	}

	return leafDatas, nil
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

	passedInRoots := make([]utreexo.Hash, len(compRoots))

	for i, compRoot := range compRoots {
		passedInRoots[i] = utreexo.Hash(*compRoot)
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

// VerifyAccProof verifies the given accumulator proof.  Returns an error if the
// verification failed.
func (uview *UtreexoViewpoint) VerifyAccProof(toProve []utreexo.Hash,
	proof *utreexo.Proof) error {
	return uview.accumulator.Verify(toProve, *proof, false)
}

// ToString outputs a string of the underlying accumulator.
func (uview *UtreexoViewpoint) ToString() string {
	return uview.accumulator.String()
}

// compareRoots is the underlying method that calls the utreexo accumulator code
func (uview *UtreexoViewpoint) compareRoots(compRoot []utreexo.Hash) bool {
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

// PruneAll deletes all the cached leaves in the utreexo viewpoint, leaving only the
// roots of the accumulator.
func (uview *UtreexoViewpoint) PruneAll() {
	newUView := uview.CopyWithRoots()
	uview.accumulator = newUView.accumulator
}

// NewUtreexoViewpoint returns an empty UtreexoViewpoint
func NewUtreexoViewpoint() *UtreexoViewpoint {
	return &UtreexoViewpoint{
		accumulator: utreexo.NewMapPollard(false),
	}
}

// SetUtreexoStateFromAssumePoint sets an initialized utreexoviewpoint from the
// assumedUtreexoPoint.
func (b *BlockChain) SetUtreexoStateFromAssumePoint() {
	b.utreexoView = &UtreexoViewpoint{
		accumulator: utreexo.NewMapPollardFromRoots(
			b.assumeUtreexoPoint.Roots, b.assumeUtreexoPoint.NumLeaves, false),
	}
}

// GetUtreexoView returns the underlying utreexo viewpoint.
func (b *BlockChain) GetUtreexoView() *UtreexoViewpoint {
	return b.utreexoView
}

// NumLeaves returns the total number of elements (aka leaves) in the accumulator.
func (uview *UtreexoViewpoint) NumLeaves() uint64 {
	return uview.accumulator.NumLeaves
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

// IsAssumeUtreexo returns true if the assume utreexo points are set.
func (b *BlockChain) IsAssumeUtreexo() bool {
	return len(b.assumeUtreexoPoint.Roots) > 0 && len(b.utreexoView.GetRoots()) == 0
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
func (b *BlockChain) VerifyUData(ud *wire.UData, txIns []*wire.TxIn, remember bool) error {
	// Nothing to prove.
	if len(txIns) == 0 {
		return nil
	}

	// If there is something to prove but ud is nil, return an error.
	if ud == nil {
		return fmt.Errorf("VerifyUData(): passed in UData is nil. " +
			"Cannot validate utreexo accumulator proof")
	}

	var err error
	ud.LeafDatas, err = b.ReconstructLeafDatas(ud.LeafDatas, txIns)
	if err != nil {
		return err
	}

	delHashes := make([]utreexo.Hash, 0, len(ud.LeafDatas))
	for _, ld := range ud.LeafDatas {
		if ld.IsCompact() || ld.IsUnconfirmed() {
			continue
		}

		delHashes = append(delHashes, ld.LeafHash())
	}

	// Acquire read lock before accessing the accumulator state.
	b.chainLock.RLock()
	defer b.chainLock.RUnlock()

	// VerifyBatchProof checks that the utreexo proofs are valid without
	// mutating the accumulator.
	err = b.utreexoView.accumulator.VerifyPartialProof(ud.AccProof.Targets, delHashes, ud.AccProof.Proof, remember)
	if err != nil {
		str := "Verify fail. All txIns-leaf datas:\n"
		for i, txIn := range txIns {
			leafHash := ud.LeafDatas[i].LeafHash()
			str += fmt.Sprintf("txIn: %s, leafdata: %s, hash %s\n", txIn.PreviousOutPoint.String(),
				ud.LeafDatas[i].String(), hex.EncodeToString(leafHash[:]))
		}
		str += fmt.Sprintf("err: %s", err.Error())
		return fmt.Errorf("%v", str)
	}

	if remember {
		log.Debugf("cached hashes: %v", delHashes)
	}

	return nil
}

// GenerateUDataPartial generates a utreexo data based on the current state of the utreexo viewpoint.
// It leaves out the full proof hashes and only fetches the requested positions.
//
// This function is safe for concurrent access.
func (b *BlockChain) GenerateUDataPartial(dels []wire.LeafData, positions []uint64) (*wire.UData, error) {
	b.chainLock.RLock()
	defer b.chainLock.RUnlock()

	ud := new(wire.UData)
	ud.LeafDatas = dels

	delHashes := make([]utreexo.Hash, 0, len(dels))
	for _, del := range dels {
		// We can't calculate the correct hash if the leaf data is in
		// the compact state.
		if !del.IsUnconfirmed() {
			delHashes = append(delHashes, del.LeafHash())
		}
	}
	targets := b.getLeafHashPositions(delHashes)

	// Fetch the requested hashes.
	hashes := make([]utreexo.Hash, len(positions))
	for i, pos := range positions {
		hashes[i] = b.utreexoView.accumulator.GetHash(pos)
	}

	// Put the proof together and return.
	ud.AccProof = utreexo.Proof{
		Targets: targets,
		Proof:   hashes,
	}

	return ud, nil
}

// GenerateUData generates a utreexo data based on the current state of the utreexo viewpoint.
//
// This function is safe for concurrent access.
func (b *BlockChain) GenerateUData(dels []wire.LeafData) (*wire.UData, error) {
	b.chainLock.RLock()
	defer b.chainLock.RUnlock()

	ud, err := wire.GenerateUData(dels, &b.utreexoView.accumulator)
	if err != nil {
		return nil, err
	}

	return ud, nil
}

// PruneFromAccumulator uncaches the given hashes from the accumulator.  No action is taken
// if the hashes are not already cached.
func (b *BlockChain) PruneFromAccumulator(leaves []wire.LeafData) error {
	if b.utreexoView == nil {
		return fmt.Errorf("This blockchain instance doesn't have an " +
			"accumulator. Cannot prune leaves")
	}

	b.chainLock.Lock()
	defer b.chainLock.Unlock()

	hashes := make([]utreexo.Hash, 0, len(leaves))
	for i := range leaves {
		// Unconfirmed leaves aren't present in the accumulator.
		if leaves[i].IsUnconfirmed() {
			continue
		}

		if leaves[i].IsCompact() {
			return fmt.Errorf("Cannot generate hash as " +
				"the leafdata is compact")
		}

		hashes = append(hashes, leaves[i].LeafHash())
	}

	log.Debugf("uncaching hashes: %v", hashes)

	err := b.utreexoView.accumulator.Prune(hashes)
	if err != nil {
		return err
	}

	return nil
}

// packedPositions fetches and returns the positions of the leafHashes as chainhash.Hash.
//
// This function is NOT safe for concurrent access.
func (b *BlockChain) packedPositions(leafHashes []utreexo.Hash) []chainhash.Hash {
	positions := b.utreexoView.accumulator.GetLeafHashPositions(leafHashes)
	return chainhash.Uint64sToPackedHashes(positions)
}

// PackedPositions fetches and returns the positions of the leafHashes as chainhash.Hash.
//
// This function is safe for concurrent access.
func (b *BlockChain) PackedPositions(leafHashes []utreexo.Hash) []chainhash.Hash {
	b.chainLock.RLock()
	defer b.chainLock.RUnlock()

	return b.packedPositions(leafHashes)
}

// getLeafHashPositions returns the positions of the passed in leaf hashes.
//
// This function is NOT safe for concurrent access.
func (b *BlockChain) getLeafHashPositions(leafHashes []utreexo.Hash) []uint64 {
	return b.utreexoView.accumulator.GetLeafHashPositions(leafHashes)
}

// GetLeafHashPositions returns the positions of the passed in leaf hashes.
//
// This function is safe for concurrent access.
func (b *BlockChain) GetLeafHashPositions(leafHashes []utreexo.Hash) []uint64 {
	b.chainLock.RLock()
	defer b.chainLock.RUnlock()

	return b.getLeafHashPositions(leafHashes)
}

// GetNeededPositions returns the positions of the needed hashes in order to verify the
// positions passed in.
//
// This function is safe for concurrent access.
func (b *BlockChain) GetNeededPositions(packedPositions []chainhash.Hash) []chainhash.Hash {
	b.chainLock.RLock()
	defer b.chainLock.RUnlock()

	positions := chainhash.PackedHashesToUint64(packedPositions)
	missing := b.utreexoView.accumulator.GetMissingPositions(positions)
	return chainhash.Uint64sToPackedHashes(missing)
}

// FetchUtreexoViewpoint returns the utreexo viewpoint at the given block hash.
// returns nil if it wasn't found.
//
// This function is safe for concurrent access.
func (b *BlockChain) FetchUtreexoViewpoint(blockHash *chainhash.Hash) (*UtreexoViewpoint, error) {
	b.chainLock.RLock()
	defer b.chainLock.RUnlock()

	// Quick check to not panic.
	if b.utreexoView == nil {
		return nil, nil
	}

	var utreexoView *UtreexoViewpoint
	err := b.db.View(func(dbTx database.Tx) error {
		var err error
		utreexoView, err = dbFetchUtreexoView(dbTx, blockHash)
		return err
	})
	if err != nil {
		return nil, err
	}

	return utreexoView, nil
}

// ChainTipProof represents all the information that is needed to prove that a
// utxo exists in the chain tip with utreexo accumulator proof.
type ChainTipProof struct {
	ProvedAtHash *chainhash.Hash
	AccProof     *utreexo.Proof
	HashesProven []utreexo.Hash
}

// Serialize encodes the chain-tip proof into the passed in writer.
func (ctp *ChainTipProof) Serialize(w io.Writer) error {
	_, err := w.Write(ctp.ProvedAtHash[:])
	if err != nil {
		return err
	}

	err = wire.BatchProofSerialize(w, ctp.AccProof)
	if err != nil {
		return err
	}

	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(len(ctp.HashesProven)))

	_, err = w.Write(buf)
	if err != nil {
		return err
	}

	for _, hash := range ctp.HashesProven {
		_, err = w.Write(hash[:])
		if err != nil {
			return err
		}
	}

	return nil
}

// Deserialize decodes a chain-tip proof from the given reader.
func (ctp *ChainTipProof) Deserialize(r io.Reader) error {
	var provedAtHash chainhash.Hash
	_, err := r.Read(provedAtHash[:])
	if err != nil {
		return err
	}

	ctp.ProvedAtHash = &provedAtHash

	bp, err := wire.BatchProofDeserialize(r)
	if err != nil {
		return err
	}
	ctp.AccProof = bp

	countBuf := make([]byte, 4)
	_, err = r.Read(countBuf)
	if err != nil {
		return err
	}

	hashesProven := make([]utreexo.Hash, binary.LittleEndian.Uint32(countBuf))
	for i := range hashesProven {
		_, err = r.Read(hashesProven[i][:])
		if err != nil {
			return err
		}
	}
	ctp.HashesProven = hashesProven

	return nil
}

// String returns a hex encoded string of the chain-tip proof.
func (ctp *ChainTipProof) String() string {
	var buf bytes.Buffer
	err := ctp.Serialize(&buf)
	if err != nil {
		return fmt.Sprintf("err: %v", err)
	}

	return hex.EncodeToString(buf.Bytes())
}

// DecodeString takes a string and decodes and deserializes the chain-tip proof
// from the string.
func (ctp *ChainTipProof) DecodeString(proof string) error {
	proofBytes, err := hex.DecodeString(proof)
	if err != nil {
		return err
	}

	r := bytes.NewReader(proofBytes)
	err = ctp.Deserialize(r)
	if err != nil {
		return err
	}

	return nil
}
