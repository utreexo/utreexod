// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/wire"
)

const (
	// flatUtreexoProofIndexName is the human-readable name for the index.
	flatUtreexoProofIndexName = "flat utreexo proof index"

	// flatUtreexoProofIndexType is the type of backend storage the flat utreexo
	// proof index has.  This name is used as a suffix for the flat file storage
	// directory.
	flatUtreexoProofIndexType = "flat"

	// flatUtreexoProofName is the name given to the proof data of the flat utreexo
	// proof index.  This name is used as the dataFile name in the flat files.
	flatUtreexoProofName = "proof"

	// flatUtreexoUndoName is the name given to the undo data of the flat utreexo
	// proof index.  This name is used as the dataFile name in the flat files.
	flatUtreexoUndoName = "undo"

	// flatRememberIdxName is the name given to the remember idx data of the flat
	// utreexo proof index.  This name is used as the dataFile name in the flat
	// files.
	flatRememberIdxName = "remember"

	// flatUtreexoProofStatsName is the name given to the proof stats data of the flat
	// utreexo proof index.  This name is used as the dataFile name in the flat
	// files.
	flatUtreexoProofStatsName = "utreexoproofstats"

	// flatUtreexoRootsName is the name given to the roots data of the flat
	// utreexo proof index.  This name is used as the dataFile name in the flat
	// files.
	flatUtreexoRootsName = "roots"

	// defaultProofGenInterval is the default value used to determine how often
	// a utreexo accumulator proof should be generated.  An interval of 10 will
	// make the proof be generated on blocks 10, 20, 30 and so on.
	defaultProofGenInterval = 10
)

var (
	// flatUtreexoBucketKey is the name of the flat utreexo proof
	// index.
	//
	// NOTE This key is not used to store any data as we store everything in
	// flat files for flat utreexo proof index.  It is just here to abide by the
	// indexer interface which uses this key to determine if the indexer
	// is resuming or not.
	flatUtreexoBucketKey = []byte("flatutreexoparentindexkey")
)

// Ensure the UtreexoProofIndex type implements the Indexer interface.
var _ Indexer = (*FlatUtreexoProofIndex)(nil)

// Ensure the UtreexoProofIndex type implements the NeedsInputser interface.
var _ NeedsInputser = (*FlatUtreexoProofIndex)(nil)

// FlatUtreexoProofIndex implements a utreexo accumulator proof index for all the blocks.
// In a flat file.
type FlatUtreexoProofIndex struct {
	proofGenInterVal int32
	proofState       FlatFileState
	undoState        FlatFileState
	rememberIdxState FlatFileState
	proofStatsState  FlatFileState
	rootsState       FlatFileState
	chainParams      *chaincfg.Params

	// The blockchain instance the index corresponds to.
	chain *blockchain.BlockChain

	// mtx protects concurrent access to the utreexoView .
	mtx *sync.RWMutex

	// utreexoState represents the Bitcoin UTXO set as a utreexo accumulator.
	// It keeps all the elements of the forest in order to generate proofs.
	utreexoState *UtreexoState

	// pStats are the proof size statistics that are kept for research purposes.
	pStats proofStats
}

// NeedsInputs signals that the index requires the referenced inputs in order
// to properly create the index.
//
// This implements the NeedsInputser interface.
func (idx *FlatUtreexoProofIndex) NeedsInputs() bool {
	return true
}

// Init initializes the flat utreexo proof index. This is part of the Indexer
// interface.
func (idx *FlatUtreexoProofIndex) Init() error {
	return nil // nothing to do
}

// Name returns the human-readable name of the index.
//
// This is part of the Indexer interface.
func (idx *FlatUtreexoProofIndex) Name() string {
	return flatUtreexoProofIndexName
}

// Key returns the database key to use for the index as a byte slice. This is
// part of the Indexer interface.
//
// NOTE This key is NEVER used as we store everything in flat files
// for flat utreexo proof index.  It is just here to abide by the
// indexer interface.
func (idx *FlatUtreexoProofIndex) Key() []byte {
	return flatUtreexoBucketKey
}

// Create is invoked when the indexer manager determines the index needs
// to be created for the first time.
//
// This is part of the Indexer interface.
func (idx *FlatUtreexoProofIndex) Create(dbTx database.Tx) error {
	_, err := dbTx.Metadata().CreateBucket(flatUtreexoBucketKey)
	if err != nil {
		return err
	}
	return nil // nothing to do
}

// ConnectBlock is invoked by the index manager when a new block has been
// connected to the main chain.
//
// This is part of the Indexer interface.
func (idx *FlatUtreexoProofIndex) ConnectBlock(dbTx database.Tx, block *btcutil.Block,
	stxos []blockchain.SpentTxOut) error {

	// Don't include genesis blocks.
	if block.Height() == 0 {
		log.Tracef("UtreexoProofIndex.ConnectBlock: Asked to connect genesis"+
			" block (height %d) Ignoring request and skipping block",
			block.Height())
		return nil
	}

	_, outCount, inskip, outskip := blockchain.DedupeBlock(block)
	dels, _, err := blockchain.BlockToDelLeaves(stxos, idx.chain, block, inskip, -1)
	if err != nil {
		return err
	}
	adds := blockchain.BlockToAddLeaves(block, outskip, nil, outCount)

	idx.mtx.RLock()
	ud, err := wire.GenerateUData(dels, idx.utreexoState.state)
	idx.mtx.RUnlock()
	if err != nil {
		return err
	}

	delHashes := make([]utreexo.Hash, 0, len(ud.LeafDatas))
	for _, ld := range ud.LeafDatas {
		delHashes = append(delHashes, ld.LeafHash())
	}

	err = idx.storeUndoBlock(block.Height(),
		uint64(len(adds)), ud.AccProof.Targets, delHashes)
	if err != nil {
		return err
	}

	err = idx.storeRoots(block.Height(), idx.utreexoState.state)
	if err != nil {
		return err
	}

	addHashes := make([]utreexo.Hash, 0, len(adds))
	for _, add := range adds {
		addHashes = append(addHashes, add.Hash)
	}

	idx.mtx.Lock()
	err = idx.utreexoState.state.Modify(adds, delHashes, ud.AccProof)
	idx.mtx.Unlock()
	if err != nil {
		return err
	}

	idx.pStats.UpdateTotalDelCount(uint64(len(dels)))
	idx.pStats.UpdateUDStats(false, ud)

	// If the interval is 1, then just save the utreexo proof and we're done.
	if idx.proofGenInterVal == 1 {
		err = idx.storeProof(block.Height(), false, ud)
		if err != nil {
			return err
		}
	} else {
		// Every proof generation interval, we'll make a multi-block proof.
		if (block.Height() % idx.proofGenInterVal) == 0 {
			err = idx.MakeMultiBlockProof(block.Height(), block.Height()-idx.proofGenInterVal,
				block, ud, stxos)
			if err != nil {
				return err
			}
		} else {
			err = idx.storeProof(block.Height(), true, ud)
			if err != nil {
				return err
			}
		}
	}

	idx.pStats.BlockHeight = uint64(block.Height())
	err = idx.pStats.WritePStats(&idx.proofStatsState)
	if err != nil {
		return err
	}

	if block.Height()%1000 == 0 {
		idx.pStats.LogProofStats()
	}

	return nil
}

// calcProofOverhead calculates the overhead of the current utreexo accumulator proof
// has.
func calcProofOverhead(ud *wire.UData) float64 {
	if ud == nil || len(ud.AccProof.Targets) == 0 {
		return 0
	}

	return float64(len(ud.AccProof.Proof)) / float64(len(ud.AccProof.Targets))
}

// fetchBlocks fetches the blocks and stxos from the given range of blocks.
func (idx *FlatUtreexoProofIndex) fetchBlocks(start, end int32) (
	[]*btcutil.Block, [][]blockchain.SpentTxOut, error) {

	blocks := make([]*btcutil.Block, 0, end-start)
	allStxos := make([][]blockchain.SpentTxOut, 0, end-start)
	for i := start; i < end; i++ {
		block, err := idx.chain.BlockByHeight(i)
		if err != nil {
			return nil, nil, err
		}
		blocks = append(blocks, block)

		stxos, err := idx.chain.FetchSpendJournalUnsafe(block)
		if err != nil {
			return nil, nil, err
		}

		allStxos = append(allStxos, stxos)
	}

	return blocks, allStxos, nil
}

// deletionsToProve returns all the deletions that need to be proven from the given
// blocks and stxos.
func (idx *FlatUtreexoProofIndex) deletionsToProve(blocks []*btcutil.Block,
	stxos [][]blockchain.SpentTxOut) ([]wire.LeafData, [][]uint32, error) {

	// Check that the length is equal to prevent index errors in the below loop.
	if len(blocks) != len(stxos) {
		err := fmt.Errorf("Got %d blocks but %d stxos", len(blocks), len(stxos))
		return nil, nil, err
	}

	// createdMap will keep track of all the utxos created in the blocks that were
	// passed in.
	createdMap := make(map[wire.OutPoint]uint32)

	remembers := make([][]uint32, len(blocks))

	var delsToProve []wire.LeafData
	for i, block := range blocks {
		_, _, inskip, outskip := blockchain.DedupeBlock(block)

		var txOutBlockIdx uint32
		for _, tx := range block.Transactions() {
			for outIdx := range tx.MsgTx().TxOut {
				// Skip txos on the skip list
				if len(outskip) > 0 && outskip[0] == txOutBlockIdx {
					outskip = outskip[1:]
					txOutBlockIdx++
					continue
				}

				op := wire.OutPoint{Hash: *tx.Hash(), Index: uint32(outIdx)}
				createdMap[op] = txOutBlockIdx
				txOutBlockIdx++
			}
		}

		excludeAfter := block.Height() - (block.Height() % idx.proofGenInterVal)

		dels, excludes, err := blockchain.BlockToDelLeaves(stxos[i], idx.chain,
			block, inskip, excludeAfter)
		if err != nil {
			return nil, nil, err
		}

		for _, excluded := range excludes {
			val, ok := createdMap[excluded.Outpoint]
			if ok {
				idx := excluded.Height - excludeAfter
				remembers[idx] = append(remembers[idx], val)
			}
		}

		delsToProve = append(delsToProve, dels...)
	}

	return delsToProve, remembers, nil
}

// attachBlock attaches the passed in block to the utreexo accumulator state.
func (idx *FlatUtreexoProofIndex) attachBlock(blk *btcutil.Block, stxos []blockchain.SpentTxOut) error {
	_, outCount, inskip, outskip := blockchain.DedupeBlock(blk)
	dels, _, err := blockchain.BlockToDelLeaves(stxos, idx.chain, blk, inskip, -1)
	if err != nil {
		return err
	}

	adds := blockchain.BlockToAddLeaves(blk, outskip, nil, outCount)
	ud, err := wire.GenerateUData(dels, idx.utreexoState.state)
	if err != nil {
		return err
	}

	delHashes := make([]utreexo.Hash, len(dels))
	for i, del := range dels {
		delHashes[i] = del.LeafHash()
	}

	err = idx.utreexoState.state.Modify(adds, delHashes, ud.AccProof)
	if err != nil {
		return err
	}

	return nil
}

// resyncUtreexoState fetches blocks from start to finish-1 and attaches all the fetched
// blocks to the utreexo accumulator state.
func (idx *FlatUtreexoProofIndex) resyncUtreexoState(start, finish int32,
	blocks []*btcutil.Block, allStxos [][]blockchain.SpentTxOut) error {
	for h := start; h < finish; h++ {
		if h == 0 {
			// nothing to do for genesis blocks.
			continue
		}

		blk, err := idx.chain.BlockByHeight(h)
		if err != nil {
			return err
		}

		stxos, err := idx.chain.FetchSpendJournalUnsafe(blk)
		if err != nil {
			return err
		}

		err = idx.attachBlock(blk, stxos)
		if err != nil {
			return err
		}
	}

	return nil
}

// reattachToUtreexoState reattaches the passed in blocks and slice of stxos slices back
// to the utreexo accumulator state.
func (idx *FlatUtreexoProofIndex) reattachToUtreexoState(blocks []*btcutil.Block,
	allStxos [][]blockchain.SpentTxOut) error {

	if len(blocks) != len(allStxos) {
		return fmt.Errorf("Got %d blocks but got %d []stxos",
			len(blocks), len(allStxos))
	}

	for i, block := range blocks {
		if block.Height() == 0 {
			continue
		}

		err := idx.attachBlock(block, allStxos[i])
		if err != nil {
			return err
		}
	}

	return nil
}

// printHashes returns the hashes encoded to string.
func printHashes(hashes []utreexo.Hash) string {
	str := ""
	for i, hash := range hashes {
		str += " " + hex.EncodeToString(hash[:])

		if i != len(hashes)-1 {
			str += "\n"
		}
	}

	return str
}

// undoUtreexoState reverses the utreexo accumulator state to the desired height.
func (idx *FlatUtreexoProofIndex) undoUtreexoState(currentHeight, desiredHeight int32) error {
	restoreState := func(start, finish int32, prevErr error) {
		err := idx.resyncUtreexoState(start, finish, nil, nil)
		if err != nil {
			str := fmt.Errorf("undoUtreexoState: cannot restore state at %d. This likely "+
				"is happening because of a disk correuption. The user should re-download the blocks "+
				"undoUtreexoState err: %v, resyncUtreexoState err: %v", finish, prevErr, err)
			panic(str)
		}
	}

	var desiredRoots []utreexo.Hash
	if desiredHeight != 0 {
		stump, err := idx.fetchRoots(desiredHeight)
		if err != nil {
			return fmt.Errorf("undoUtreexoState: cannot find roots at %d, err %v.", desiredHeight, err)
		}

		desiredRoots = stump.Roots
	}

	// Go back to the desired block to generate the multi-block proof.
	for h := currentHeight; h >= desiredHeight; h-- {
		if h == 0 {
			// nothing to do for genesis blocks.
			continue
		}

		var stump utreexo.Stump
		var err error
		stump, err = idx.fetchRoots(h)
		if err != nil {
			restoreState(h, currentHeight, err)
			return fmt.Errorf("undoUtreexoState: cannot find roots at %d, err %v.",
				h, err)
		}
		numAdds, targets, delHashes, err := idx.fetchUndoBlock(h)
		if err != nil {
			restoreState(h, currentHeight, err)
			return fmt.Errorf("undoUtreexoState: cannot find undoblock at %d, err %v.",
				h, err)
		}

		err = idx.utreexoState.state.Undo(numAdds, utreexo.Proof{Targets: targets}, delHashes, stump.Roots)
		if err != nil {
			restoreState(h, currentHeight, err)
			return err
		}

		roots := idx.utreexoState.state.GetRoots()

		if len(stump.Roots) != len(roots) {
			err := fmt.Errorf("Error undoing height %d. Expected root length of %d but got %d\nExpected:\n%s\nGot:\n%s\n",
				h, len(stump.Roots), len(roots), printHashes(stump.Roots), printHashes(roots))
			return err
		}
		for i, desiredRoot := range stump.Roots {
			if roots[i] != desiredRoot {
				return fmt.Errorf("Error undoing height %d. Expected root of %s at index %d but got %s",
					h, hex.EncodeToString(desiredRoot[:]), i, hex.EncodeToString(roots[i][:]))
			}
		}
	}

	gotRoots := idx.utreexoState.state.GetRoots()
	if len(desiredRoots) != len(gotRoots) {
		err := fmt.Errorf("Expected root length of %d but got %d\nExpected:\n%s\nGot:\n%s\n",
			len(desiredRoots), len(gotRoots), printHashes(desiredRoots), printHashes(gotRoots))
		return err
	}
	for i, desiredRoot := range desiredRoots {
		if gotRoots[i] != desiredRoot {
			return fmt.Errorf("Expected root of %s at index %d but got %s",
				hex.EncodeToString(desiredRoot[:]), i, hex.EncodeToString(gotRoots[i][:]))
		}
	}

	return nil
}

// MakeMultiBlockProof reverses the utreexo accumulator state to the multi-block proof
// generation height and makes a proof of all the stxos in the upcoming interval.  The
// utreexo state is caught back up to the current height after the mulit-block proof is
// generated.
func (idx *FlatUtreexoProofIndex) MakeMultiBlockProof(currentHeight, proveHeight int32,
	block *btcutil.Block, currentUD *wire.UData, stxos []blockchain.SpentTxOut) error {

	idx.mtx.Lock()
	defer idx.mtx.Unlock()

	startRoots := idx.utreexoState.state.GetRoots()

	// Go back to the desired block to generate the multi-block proof.
	err := idx.undoUtreexoState(currentHeight, proveHeight)
	if err != nil {
		return err
	}

	blocks, allStxos, err := idx.fetchBlocks(proveHeight, currentHeight)
	if err != nil {
		return err
	}

	if int32(len(blocks)) != idx.proofGenInterVal {
		err := fmt.Errorf("Only fetched %d blocks but the proofGenInterVal is %d",
			len(blocks), idx.proofGenInterVal)
		panic(err)
	}

	delsToProve, remembers, err := idx.deletionsToProve(blocks, allStxos)
	if err != nil {
		return err
	}

	ud, err := wire.GenerateUData(delsToProve, idx.utreexoState.state)
	if err != nil {
		panic(err)
	}

	delHashes := make([]utreexo.Hash, 0, len(delsToProve))
	for _, del := range delsToProve {
		delHashes = append(delHashes, del.LeafHash())
	}

	// Store the proof that we have created.
	err = idx.storeMultiBlockProof(currentHeight, currentUD, ud, delHashes)
	if err != nil {
		return err
	}

	// Store the cache that we have created.
	err = idx.storeRemembers(remembers, proveHeight)
	if err != nil {
		return err
	}

	// Re-sync all the reorged blocks.
	err = idx.reattachToUtreexoState(blocks, allStxos)
	if err != nil {
		return err
	}

	// Attach the current block.
	err = idx.attachBlock(block, stxos)
	if err != nil {
		return err
	}

	// Sanity check.
	endRoots := idx.utreexoState.state.GetRoots()
	if !reflect.DeepEqual(endRoots, startRoots) {
		err := fmt.Errorf("MakeMultiBlockProof: start roots and endroots differ. " +
			"Likely that the database is corrupted.")
		panic(err)
	}

	idx.pStats.UpdateMultiUDStats(len(delsToProve), ud)

	return nil
}

// DisconnectBlock is invoked by the index manager when a new block has been
// disconnected to the main chain.
//
// This is part of the Indexer interface.
func (idx *FlatUtreexoProofIndex) DisconnectBlock(dbTx database.Tx, block *btcutil.Block,
	stxos []blockchain.SpentTxOut) error {

	ud, err := idx.FetchUtreexoProof(block.Height(), false)
	if err != nil {
		return err
	}

	// Need to call reconstruct since the saved utreexo data is in the compact form.
	delHashes, err := idx.chain.ReconstructUData(ud, *block.Hash())
	if err != nil {
		return err
	}

	_, outCount, _, outskip := blockchain.DedupeBlock(block)
	adds := blockchain.BlockToAddLeaves(block, outskip, nil, outCount)

	state, err := idx.fetchRoots(block.Height())
	if err != nil {
		return err
	}

	idx.mtx.Lock()
	err = idx.utreexoState.state.Undo(uint64(len(adds)), utreexo.Proof{Targets: ud.AccProof.Targets}, delHashes, state.Roots)
	idx.mtx.Unlock()
	if err != nil {
		return err
	}

	// Check if we're at a height where proof was generated.
	if (block.Height() % idx.proofGenInterVal) == 0 {
		height := block.Height() / idx.proofGenInterVal
		err = idx.proofState.DisconnectBlock(height)
		if err != nil {
			return err
		}
	}

	err = idx.undoState.DisconnectBlock(block.Height())
	if err != nil {
		return err
	}

	err = idx.rootsState.DisconnectBlock(block.Height())
	if err != nil {
		return err
	}

	return nil
}

// PruneBlock is invoked when an older block is deleted after it's been
// processed.
//
// This is part of the Indexer interface.
func (idx *FlatUtreexoProofIndex) PruneBlock(dbTx database.Tx, blockHash *chainhash.Hash) error {
	return nil
}

// FetchUtreexoProof returns the Utreexo proof data for the given block height.
func (idx *FlatUtreexoProofIndex) FetchUtreexoProof(height int32, excludeAccProof bool) (
	*wire.UData, error) {

	if height == 0 {
		return nil, fmt.Errorf("No Utreexo Proof for height %d", height)
	}

	proofBytes, err := idx.proofState.FetchData(height)
	if err != nil {
		return nil, err
	}
	if proofBytes == nil {
		return nil, fmt.Errorf("Couldn't fetch Utreexo proof for height %d", height)
	}
	r := bytes.NewReader(proofBytes)

	ud := new(wire.UData)
	if excludeAccProof {
		err = ud.DeserializeCompactNoAccProof(r)
		if err != nil {
			return nil, err
		}
	} else {
		err = ud.DeserializeCompact(r, udataSerializeBool, 0)
		if err != nil {
			return nil, err
		}
	}

	return ud, nil
}

// GetLeafHashPositions returns the positions of the passed in hashes.
func (idx *FlatUtreexoProofIndex) GetLeafHashPositions(delHashes []utreexo.Hash) []uint64 {
	idx.mtx.RLock()
	defer idx.mtx.RUnlock()

	return idx.utreexoState.state.GetLeafPositions(delHashes)
}

// GenerateUDataPartial generates a utreexo data based on the current state of the accumulator.
// It leaves out the full proof hashes and only fetches the requested positions.
func (idx *FlatUtreexoProofIndex) GenerateUDataPartial(dels []wire.LeafData, positions []uint64) (*wire.UData, error) {
	idx.mtx.RLock()
	defer idx.mtx.RUnlock()

	ud := new(wire.UData)
	ud.LeafDatas = dels

	// Get the positions of the targets of delHashes.
	delHashes, err := wire.HashesFromLeafDatas(ud.LeafDatas)
	if err != nil {
		return nil, err
	}

	hashes := make([]utreexo.Hash, len(positions))
	for i, pos := range positions {
		hashes[i] = idx.utreexoState.state.GetHash(pos)
	}

	targets := idx.utreexoState.state.GetLeafPositions(delHashes)
	ud.AccProof = utreexo.Proof{
		Targets: targets,
		Proof:   hashes,
	}

	return ud, nil
}

// FetchMultiUtreexoProof fetches the utreexo data, multi-block proof, and the hashes for
// the given height.  Attempting to fetch multi-block proof at a height where there weren't
// any mulit-block proof generated will result in an error.
func (idx *FlatUtreexoProofIndex) FetchMultiUtreexoProof(height int32) (
	*wire.UData, *wire.UData, []utreexo.Hash, error) {

	if height == 0 {
		return nil, nil, nil, fmt.Errorf("No Utreexo Proof for height %d", height)
	}

	if height%idx.proofGenInterVal != 0 {
		return nil, nil, nil, fmt.Errorf("Attempting to fetch multi-block proof at the wrong height "+
			"height:%d, proofGenInterVal:%d", height, idx.proofGenInterVal)
	}

	// Fetch the serialized data.
	proofBytes, err := idx.proofState.FetchData(height)
	if err != nil {
		return nil, nil, nil, err
	}
	if proofBytes == nil {
		return nil, nil, nil, fmt.Errorf("Couldn't fetch Utreexo proof for height %d", height)
	}
	r := bytes.NewReader(proofBytes)

	// Deserialize the utreexo data for the block at the interval.
	ud := new(wire.UData)
	err = ud.DeserializeCompactNoAccProof(r)
	if err != nil {
		return nil, nil, nil, err
	}

	// Deserialize the utreexo data that will provide the proof for the upcoming
	// blocks in the interval.
	multiUd := new(wire.UData)
	err = multiUd.DeserializeCompact(r, udataSerializeBool, 0)
	if err != nil {
		return nil, nil, nil, err
	}

	// Deserialize the hashes of the leaf datas.
	buf := make([]byte, 4)
	_, err = r.Read(buf)
	if err != nil {
		return nil, nil, nil, err
	}
	count := binary.LittleEndian.Uint32(buf)
	dels := make([]utreexo.Hash, count)
	for i := range dels {
		_, err = r.Read(dels[i][:])
		if err != nil {
			return nil, nil, nil, err
		}
	}

	return ud, multiUd, dels, nil
}

// FetchRemembers fetches the remember indexes of the desired block height.
func (idx *FlatUtreexoProofIndex) FetchRemembers(height int32) ([]uint32, error) {
	// Fetch the raw bytes.
	rememberBytes, err := idx.rememberIdxState.FetchData(height)
	if err != nil {
		return nil, err
	}

	// Deserialize the raw bytes into a uint32 slice.
	r := bytes.NewReader(rememberBytes)
	remembers, err := wire.DeserializeRemembers(r)
	if err != nil {
		return nil, err
	}

	return remembers, nil
}

// storeProof serializes and stores the utreexo data in the proof state.
func (idx *FlatUtreexoProofIndex) storeProof(height int32, excludeAccProof bool, ud *wire.UData) error {
	if excludeAccProof {
		bytesBuf := bytes.NewBuffer(make([]byte, 0, ud.SerializeUxtoDataSizeCompact(udataSerializeBool)))
		err := ud.SerializeCompactNoAccProof(bytesBuf)
		if err != nil {
			return err
		}

		err = idx.proofState.StoreData(height, bytesBuf.Bytes())
		if err != nil {
			return err
		}
	} else {
		bytesBuf := bytes.NewBuffer(make([]byte, 0, ud.SerializeSizeCompact(udataSerializeBool)))
		err := ud.SerializeCompact(bytesBuf, udataSerializeBool)
		if err != nil {
			return err
		}

		err = idx.proofState.StoreData(height, bytesBuf.Bytes())
		if err != nil {
			return err
		}
	}

	return nil
}

// storeMultiBlockProof stores the utreexo proof at the block height where the proof should
// be generated.  The targets for the block, utreexo data for the block, and the mulit-block
// accumulator proof for the upcoming block interval is stored.
func (idx *FlatUtreexoProofIndex) storeMultiBlockProof(height int32, ud, multiUd *wire.UData,
	dels []utreexo.Hash) error {

	size := ud.SerializeSizeCompactNoAccProof()
	size += multiUd.SerializeSizeCompact(udataSerializeBool)
	size += (len(dels) * chainhash.HashSize) + 4 // 4 for uint32 size

	bytesBuf := bytes.NewBuffer(make([]byte, 0, size))
	err := ud.SerializeCompactNoAccProof(bytesBuf)
	if err != nil {
		return err
	}

	multiUd.LeafDatas = []wire.LeafData{}
	err = multiUd.SerializeCompact(bytesBuf, udataSerializeBool)
	if err != nil {
		return err
	}

	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(len(dels)))
	_, err = bytesBuf.Write(buf)
	if err != nil {
		return err
	}

	for _, del := range dels {
		_, err = bytesBuf.Write(del[:])
		if err != nil {
			return err
		}
	}

	err = idx.proofState.StoreData(height, bytesBuf.Bytes())
	if err != nil {
		return err
	}

	return nil
}

// storeUndoBlock serializes and stores undo blocks in the undo state.
func (idx *FlatUtreexoProofIndex) storeUndoBlock(height int32,
	numAdds uint64, targets []uint64, delHashes []utreexo.Hash) error {

	bytes, err := serializeUndoBlock(numAdds, targets, delHashes)
	if err != nil {
		return err
	}

	err = idx.undoState.StoreData(height, bytes)
	if err != nil {
		return err
	}

	return nil
}

// storeRoots serializes and stores roots to the roots state.
func (idx *FlatUtreexoProofIndex) storeRoots(height int32, p *utreexo.Pollard) error {
	serialized, err := blockchain.SerializeUtreexoRoots(p.NumLeaves, p.GetRoots())
	if err != nil {
		return err
	}

	err = idx.rootsState.StoreData(height, serialized)
	if err != nil {
		return err
	}

	return nil
}

// storeRemembers serializes and stores the remember indexes in the remember index state.
func (idx *FlatUtreexoProofIndex) storeRemembers(remembers [][]uint32, startHeight int32) error {
	for i, remember := range remembers {
		if startHeight == 0 && i == 0 {
			continue
		}

		// Remember indexes size.
		size := wire.SerializeRemembersSize(remember)
		bytesBuf := bytes.NewBuffer(make([]byte, 0, size))

		err := wire.SerializeRemembers(bytesBuf, remember)
		if err != nil {
			return err
		}

		err = idx.rememberIdxState.StoreData(startHeight+int32(i), bytesBuf.Bytes())
		if err != nil {
			return err
		}
	}

	return nil
}

// fetchUndoBlock returns the undoblock for the given block height.
func (idx *FlatUtreexoProofIndex) fetchUndoBlock(height int32) (uint64, []uint64, []utreexo.Hash, error) {
	if height == 0 {
		return 0, nil, nil, fmt.Errorf("No Undo Block for height %d", height)
	}

	undoBytes, err := idx.undoState.FetchData(height)
	if err != nil {
		return 0, nil, nil, err
	}

	return deserializeUndoBlock(undoBytes)
}

func (idx *FlatUtreexoProofIndex) fetchRoots(height int32) (utreexo.Stump, error) {
	if height == 0 {
		return utreexo.Stump{}, nil
	}

	undoBytes, err := idx.rootsState.FetchData(height)
	if err != nil {
		return utreexo.Stump{}, err
	}

	numLeaves, roots, err := blockchain.DeserializeUtreexoRoots(undoBytes)
	if err != nil {
		return utreexo.Stump{}, err
	}

	return utreexo.Stump{Roots: roots, NumLeaves: numLeaves}, nil
}

// GenerateUData generates utreexo data for the dels passed in.  Height passed in
// should either be of block height of where the deletions are happening or just
// the lastest block height for mempool tx proof generation.
func (idx *FlatUtreexoProofIndex) GenerateUData(dels []wire.LeafData) (*wire.UData, error) {
	idx.mtx.RLock()
	ud, err := wire.GenerateUData(dels, idx.utreexoState.state)
	idx.mtx.RUnlock()
	if err != nil {
		return nil, err
	}

	return ud, nil
}

// ProveUtxos returns an accumulator proof of the outpoints passed in with
// respect to the UTXO state at chaintip.
//
// NOTE The accumulator state differs at every block height.  The caller must
// take into consideration that an accumulator proof at block X will not be valid
// at block height X+1.
//
// This function is safe for concurrent access.
func (idx *FlatUtreexoProofIndex) ProveUtxos(utxos []*blockchain.UtxoEntry,
	outpoints *[]wire.OutPoint) (*blockchain.ChainTipProof, error) {

	// We'll turn the entries and outpoints into leaves that go in
	// the accumulator.
	leaves := make([]wire.LeafData, 0, len(utxos))
	for i, utxo := range utxos {
		if utxo == nil || utxo.IsSpent() {
			err := fmt.Errorf("Passed in utxo at index %d "+
				"is nil or is already spent", i)
			return nil, err
		}

		blockHash, err := idx.chain.BlockHashByHeight(utxo.BlockHeight())
		if err != nil {
			return nil, err
		}
		if blockHash == nil {
			err := fmt.Errorf("Couldn't find blockhash for height %d",
				utxo.BlockHeight())
			return nil, err
		}
		leaf := wire.LeafData{
			BlockHash:  *blockHash,
			OutPoint:   (*outpoints)[i],
			Amount:     utxo.Amount(),
			PkScript:   utxo.PkScript(),
			Height:     utxo.BlockHeight(),
			IsCoinBase: utxo.IsCoinBase(),
		}

		leaves = append(leaves, leaf)
	}

	// Now we'll turn those leaves into hashes.  These are the hashes that are
	// commited in the accumulator.
	hashes := make([]utreexo.Hash, 0, len(leaves))
	for _, leaf := range leaves {
		hashes = append(hashes, leaf.LeafHash())
	}

	// Get a read lock for the index.  This will prevent connectBlock from updating
	// the beststate snapshot and the utreexo state.
	idx.mtx.RLock()
	defer idx.mtx.RUnlock()

	accProof, err := idx.utreexoState.state.Prove(hashes)
	if err != nil {
		return nil, err
	}

	// Grab the height and the blockhash the proof was generated at.
	snapshot := idx.chain.BestSnapshot()
	provedAtHash := snapshot.Hash

	proof := &blockchain.ChainTipProof{
		ProvedAtHash: &provedAtHash,
		AccProof:     &accProof,
		HashesProven: hashes,
	}

	return proof, nil
}

// VerifyAccProof verifies the given accumulator proof.  Returns an error if the
// verification failed.
func (idx *FlatUtreexoProofIndex) VerifyAccProof(toProve []utreexo.Hash,
	proof *utreexo.Proof) error {
	return idx.utreexoState.state.Verify(toProve, *proof, false)
}

// SetChain sets the given chain as the chain to be used for blockhash fetching.
func (idx *FlatUtreexoProofIndex) SetChain(chain *blockchain.BlockChain) {
	idx.chain = chain
}

// flatFilePath returns the path to the flatfile.
func flatFilePath(dataDir, dataName string) string {
	flatFileName := dataName + "_" + flatFileNameSuffix
	flatFilePath := filepath.Join(dataDir, flatFileName)
	return flatFilePath
}

// loadFlatFileState initializes the FlatFileState in the dataDir with
// name used to name the directory and the dataFile that the data will be
// stored to.
func loadFlatFileState(dataDir, name string) (*FlatFileState, error) {
	path := flatFilePath(dataDir, name)
	ff := NewFlatFileState()

	err := ff.Init(path, name)
	if err != nil {
		return nil, err
	}

	return ff, nil
}

// NewFlatUtreexoProofIndex returns a new instance of an indexer that is used to create a flat utreexo proof index.
//
// It implements the Indexer interface which plugs into the IndexManager that in
// turn is used by the blockchain package.  This allows the index to be
// seamlessly maintained along with the chain.
func NewFlatUtreexoProofIndex(dataDir string, chainParams *chaincfg.Params,
	proofGenInterVal *int32) (*FlatUtreexoProofIndex, error) {

	// If the proofGenInterVal argument is nil, use the default value.
	var intervalToUse int32
	if proofGenInterVal != nil {
		intervalToUse = *proofGenInterVal
	} else {
		intervalToUse = defaultProofGenInterval
	}

	idx := &FlatUtreexoProofIndex{
		proofGenInterVal: intervalToUse,
		chainParams:      chainParams,
		mtx:              new(sync.RWMutex),
	}

	// Init Utreexo State.
	uState, err := InitUtreexoState(&UtreexoConfig{
		DataDir: dataDir,
		Name:    flatUtreexoProofIndexType,
		// Default to ram for now.
		Params: chainParams,
	})
	if err != nil {
		return nil, err
	}
	idx.utreexoState = uState

	// Init the utreexo proof state.
	proofState, err := loadFlatFileState(dataDir, flatUtreexoProofName)
	if err != nil {
		return nil, err
	}
	idx.proofState = *proofState

	// Init the undo block state.
	undoState, err := loadFlatFileState(dataDir, flatUtreexoUndoName)
	if err != nil {
		return nil, err
	}
	idx.undoState = *undoState

	// Init the remember idx state.
	rememberIdxState, err := loadFlatFileState(dataDir, flatRememberIdxName)
	if err != nil {
		return nil, err
	}
	idx.rememberIdxState = *rememberIdxState

	proofStatsState, err := loadFlatFileState(dataDir, flatUtreexoProofStatsName)
	if err != nil {
		return nil, err
	}
	idx.proofStatsState = *proofStatsState

	rootsState, err := loadFlatFileState(dataDir, flatUtreexoRootsName)
	if err != nil {
		return nil, err
	}
	idx.rootsState = *rootsState

	err = idx.pStats.InitPStats(proofStatsState)
	if err != nil {
		return nil, err
	}

	return idx, nil
}

// DropFlatUtreexoProofIndex drops the address index from the provided database if it
// exists.
func DropFlatUtreexoProofIndex(db database.DB, dataDir string, interrupt <-chan struct{}) error {
	err := dropIndex(db, flatUtreexoBucketKey, flatUtreexoProofIndexName, interrupt)
	if err != nil {
		return err
	}

	proofPath := flatFilePath(dataDir, flatUtreexoProofName)
	err = deleteFlatFile(proofPath)
	if err != nil {
		return err
	}

	undoPath := flatFilePath(dataDir, flatUtreexoUndoName)
	err = deleteFlatFile(undoPath)
	if err != nil {
		return err
	}

	rememberIdxPath := flatFilePath(dataDir, flatRememberIdxName)
	err = deleteFlatFile(rememberIdxPath)
	if err != nil {
		return err
	}

	proofStatsPath := flatFilePath(dataDir, flatUtreexoProofStatsName)
	err = deleteFlatFile(proofStatsPath)
	if err != nil {
		return err
	}

	rootsPath := flatFilePath(dataDir, flatUtreexoRootsName)
	err = deleteFlatFile(rootsPath)
	if err != nil {
		return err
	}

	path := utreexoBasePath(&UtreexoConfig{DataDir: dataDir, Name: flatUtreexoProofIndexType})
	return deleteUtreexoState(path)
}
