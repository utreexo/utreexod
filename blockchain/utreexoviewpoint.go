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
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
)

// UtreexoViewpoint is the compact state of the chainstate using the utreexo accumulator
type UtreexoViewpoint struct {
	// proofInterval is the interval of block in which to receive the
	// accumulator proofs. Only relevant when in multiblock proof mode.
	proofInterval int32

	// accumulator is the bare-minimum accumulator for the utxo set.
	// It only holds the root hashes and the number of elements in the
	// accumulator.
	accumulator utreexo.Stump

	// cached is the cached proof that keeps track proofs for recently created
	// utxos that'll be spent quickly.
	//
	// NOTE during a reorg, the cached proof and the leaf hashes will be cleared.
	cached utreexo.Proof

	// cachedLeafHashes are the hashes for the elements being cached in the
	// cached proof.
	//
	// NOTE during a reorg, the cached proof and the leaf hashes will be cleared.
	cachedLeafHashes []utreexo.Hash
}

// Copy returns a copy of the utreexo viewpoint.
func (uview *UtreexoViewpoint) Copy() UtreexoViewpoint {
	copyAccumulator := utreexo.Stump{
		Roots:     make([]utreexo.Hash, len(uview.accumulator.Roots)),
		NumLeaves: uview.accumulator.NumLeaves,
	}
	copy(copyAccumulator.Roots, uview.accumulator.Roots)

	copyCached := utreexo.Proof{
		Targets: make([]uint64, len(uview.cached.Targets)),
		Proof:   make([]utreexo.Hash, len(uview.cached.Proof)),
	}
	copy(copyCached.Proof, uview.cached.Proof)
	copy(copyCached.Targets, uview.cached.Targets)

	copyCachedLeafHashes := make([]utreexo.Hash, len(uview.cachedLeafHashes))
	copy(copyCachedLeafHashes, uview.cachedLeafHashes)

	copyuView := UtreexoViewpoint{
		proofInterval:    uview.proofInterval,
		accumulator:      copyAccumulator,
		cached:           copyCached,
		cachedLeafHashes: copyCachedLeafHashes,
	}

	return copyuView
}

// ProcessUData checks that the accumulator proof and the utxo data included in the UData
// passes consensus and then it updates the underlying accumulator.
func (uview *UtreexoViewpoint) ProcessUData(block *btcutil.Block,
	bestChain *chainView, ud *wire.UData) error {

	// Extracts the block into additions and deletions that will be processed.
	// Adds correspond to newly created UTXOs and dels correspond to STXOs.
	adds, dels, err := ExtractAccumulatorAddDels(block, bestChain, ud.RememberIdx)
	if err != nil {
		return err
	}

	// Update the underlying accumulator.
	updateData, err := uview.Modify(ud, adds, dels, ud.RememberIdx)
	if err != nil {
		return fmt.Errorf("ProcessUData fail. Error: %v", err)
	}

	// Add the utreexo data to the block.
	block.SetUtreexoUpdateData(updateData)
	block.SetUtreexoAdds(adds)

	return nil
}

// AddProof first checks that the utreexo proofs are valid. If it is valid,
// it readys the utreexo accumulator for additions/deletions by ingesting the proof.
func (uview *UtreexoViewpoint) AddProof(delHashes []utreexo.Hash, accProof *utreexo.Proof) error {
	_, err := utreexo.Verify(uview.accumulator, delHashes, *accProof)
	if err != nil {
		return err
	}
	uview.cachedLeafHashes, uview.cached = utreexo.AddProof(
		uview.cached, *accProof, uview.cachedLeafHashes, delHashes, uview.accumulator.NumLeaves)

	return nil
}

// Modify modifies the utreexo state by adding and deleting elements from the utreexo state.
// The deletions are marked by the accumulator proof in the utreexo data and the additions
// are the leaves passed in.
//
// This function is NOT safe for concurrent access.
func (uview *UtreexoViewpoint) Modify(ud *wire.UData, adds []utreexo.Leaf, dels []utreexo.Hash, remembers []uint32) (*utreexo.UpdateData, error) {
	addHashes := make([]utreexo.Hash, len(adds))
	remembers = make([]uint32, len(adds))
	for i, add := range adds {
		addHashes[i] = add.Hash
		remembers[i] = uint32(i)
	}

	var err error
	var updateData utreexo.UpdateData
	if uview.proofInterval == 1 {
		updateData, err = uview.accumulator.Update(dels, addHashes, ud.AccProof)
		if err != nil {
			return nil, err
		}
	} else {
		// Extract only the necessary targets and proof hashes for this block.
		blockHashes, blockProof, err := utreexo.GetProofSubset(uview.cached, uview.cachedLeafHashes, ud.AccProof.Targets, uview.accumulator.NumLeaves)
		if err != nil {
			return nil, err
		}

		// Update the accumulator.
		updateData, err := uview.accumulator.Update(blockHashes, addHashes, blockProof)
		if err != nil {
			return nil, err
		}

		// Update the cached proof.
		uview.cachedLeafHashes, err = uview.cached.Update(uview.cachedLeafHashes, addHashes, blockProof.Targets, remembers, updateData)
		if err != nil {
			return nil, err
		}
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
				// fmt.Printf("skip %s\n", txin.PreviousOutPoint.String())
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
// inputs, and idexes of outputs which can be removed.  These are indexes
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

// ExtractAccumulatorAddDels extracts the additions and the deletions that will be
// used to modify the utreexo accumulator.
func ExtractAccumulatorAddDels(block *btcutil.Block, bestChain *chainView, remembers []uint32) (
	[]utreexo.Leaf, []utreexo.Hash, error) {

	// Check that UData field isn't nil before doing anything else.
	if block.MsgBlock().UData == nil {
		return nil, nil, fmt.Errorf("ExtractAccumulatorAddDels(): block.MsgBlock().UData is nil. " +
			"Cannot extract utreexo accumulator additions and deletions")
	}

	ud := block.MsgBlock().UData

	// outskip is all the txOuts that are referenced by a txIn in the same block
	// outCount is the count of all outskips.
	_, outCount, inskip, outskip := DedupeBlock(block)

	// Make the now verified utxos into 32 byte leaves ready to be added into the
	// utreexo accumulator.
	leaves := BlockToAddLeaves(block, outskip, remembers, outCount)

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
			return nil, nil, err
		}
	}

	// Grab the outpoints that need their existence proven and check that
	// the udata matches up.
	OPsToProve := BlockToDelOPs(block)
	err := ProofSanity(ud, OPsToProve)
	if err != nil {
		return nil, nil, err
	}

	return leaves, delHashes, nil
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

			delHashes = append(delHashes, ld.LeafHash())

			blockInIdx++
			ldIdx++
		}
	}

	return delHashes, nil
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
// inskip and excludeAfter are optional arguments. inskip will skip indexes of the
// txIns that match with the ones included in the slice. For example, if [0, 3, 11]
// is given as the inskip list, then txIns that appear in the 0th, 3rd, and 11th in
// the block will be skipped over.
//
// excludeAfter will exclude all txIns that were created after a certain block height.
// For example, 1000 as the excludeAfter value will skip the generation of leaf datas
// for txIns that reference utxos created on and after the height 1000.
//
// NOTE To opt out of the optional arguments inskip and excludeAfter, just pass nil
// for inskip and -1 for excludeAfter.
func BlockToDelLeaves(stxos []SpentTxOut, chain *BlockChain, block *btcutil.Block,
	inskip []uint32, excludeAfter int32) (delLeaves []wire.LeafData, excluded []ExcludedUtxo, err error) {

	if chain == nil {
		return nil, nil, fmt.Errorf("Passed in chain is nil. Cannot make delLeaves")
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

			// Skip all those that have heights greater and equal to
			// the excludeAfter height.
			if excludeAfter >= 0 && stxo.Height >= excludeAfter {
				blockInIdx++
				excluded = append(excluded, ExcludedUtxo{stxo.Height, op})
				continue
			}

			blockHash, err := chain.BlockHashByHeight(stxo.Height)
			if err != nil {
				return nil, nil, err
			}
			if blockHash == nil {
				return nil, nil, fmt.Errorf("Couldn't find blockhash for height %d",
					stxo.Height)
			}

			var pkType wire.PkType

			scriptType := txscript.GetScriptClass(stxo.PkScript)
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

		scriptType := txscript.GetScriptClass(entry.PkScript())
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
	roots := uview.accumulator.Roots

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
	uViewRoots := uview.accumulator.Roots
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
	_, err := utreexo.Verify(uview.accumulator, toProve, *proof)
	return err
}

// ToString outputs a string of the underlying accumulator.
func (uview *UtreexoViewpoint) ToString() string {
	return uview.accumulator.String()
}

// compareRoots is the underlying method that calls the utreexo accumulator code
func (uview *UtreexoViewpoint) compareRoots(compRoot []utreexo.Hash) bool {
	uviewRoots := uview.accumulator.Roots

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

// SetProofInterval sets the interval of the utreexo proofs to be received by the node.
// Ex: interval of 10 means that you receive a utreexo proof every 10 blocks.
func (uview *UtreexoViewpoint) SetProofInterval(proofInterval int32) {
	uview.proofInterval = proofInterval
}

// GetProofInterval returns the proof interval of the current utreexo viewpoint.
func (uview *UtreexoViewpoint) GetProofInterval() int32 {
	return uview.proofInterval
}

// PruneAll deletes all the cached leaves in the utreexo viewpoint, leaving only the
// roots of the accumulator.
func (uview *UtreexoViewpoint) PruneAll() {
	uview.cached.Targets = uview.cached.Targets[:0]
	uview.cached.Proof = uview.cached.Proof[:0]
	uview.cachedLeafHashes = uview.cachedLeafHashes[:0]
}

// NewUtreexoViewpoint returns an empty UtreexoViewpoint
func NewUtreexoViewpoint() *UtreexoViewpoint {
	return &UtreexoViewpoint{
		// Use 1 as a default value.
		proofInterval: 1,
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
	delHashes := make([]utreexo.Hash, 0, len(ud.LeafDatas))
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
					return err
				}

				ld.PkScript = scriptToUse
			}

			delHashes = append(delHashes, ld.LeafHash())
		}
	}

	// Acquire read lock before accessing the accumulator state.
	b.chainLock.RLock()
	defer b.chainLock.RUnlock()

	// VerifyBatchProof checks that the utreexo proofs are valid without
	// mutating the accumulator.
	_, err := utreexo.Verify(b.utreexoView.accumulator, delHashes, ud.AccProof)
	if err != nil {
		str := "Verify fail. All txIns-leaf datas:\n"
		for i, txIn := range txIns {
			str += fmt.Sprintf("txIn: %s, leafdata: %s\n", txIn.PreviousOutPoint.String(),
				ud.LeafDatas[i].ToString())
		}
		str += fmt.Sprintf("err: %s", err.Error())
		return fmt.Errorf(str)
	}

	return nil
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
