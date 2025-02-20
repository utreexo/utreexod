// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	proofState      FlatFileState
	undoState       FlatFileState
	proofStatsState FlatFileState
	rootsState      FlatFileState

	// All the configurable metadata.
	config *UtreexoConfig

	// The blockchain instance the index corresponds to.
	chain *blockchain.BlockChain

	// mtx protects concurrent access to the utreexoView .
	mtx *sync.RWMutex

	// utreexoState represents the Bitcoin UTXO set as a utreexo accumulator.
	// It keeps all the elements of the forest in order to generate proofs.
	utreexoState *UtreexoState

	// utreexoRootsState is the accumulator for all the roots at each of the
	// blocks. This is so that we can serve to peers the proof that a set of
	// roots at a block is correct.
	utreexoRootsState utreexo.Pollard

	// blockSummaryState is the accumulator for all the block summaries at each
	// of the blocks. This is so that we can serve to peers the proof that the given
	// block summaries of a block is correct.
	blockSummaryState utreexo.Pollard

	// pStats are the proof size statistics that are kept for research purposes.
	pStats proofStats

	// The time of when the utreexo state was last flushed.
	lastFlushTime time.Time
}

// NeedsInputs signals that the index requires the referenced inputs in order
// to properly create the index.
//
// This implements the NeedsInputser interface.
func (idx *FlatUtreexoProofIndex) NeedsInputs() bool {
	return true
}

// consistentFlatFileState rolls back all the flat file states to the tip height.
// The data is written to the flat files directly but the index tips are cached and
// then written to disk. This may lead to states where the index tip is lower than the
// data stored in the flat files. Rolling back the flat file state to the index tip
// keep ths entire indexer consistent.
func (idx *FlatUtreexoProofIndex) consistentFlatFileState(tipHeight int32) error {
	if !idx.config.Pruned {
		if idx.proofState.BestHeight() != 0 &&
			tipHeight < idx.proofState.BestHeight() {
			bestHeight := idx.proofState.BestHeight()
			for tipHeight != bestHeight && bestHeight > 0 {
				err := idx.proofState.DisconnectBlock(bestHeight)
				if err != nil {
					return err
				}
				bestHeight--
			}
		}
	}

	if idx.undoState.BestHeight() != 0 &&
		tipHeight < idx.undoState.BestHeight() {
		bestHeight := idx.undoState.BestHeight()
		for tipHeight != bestHeight && bestHeight > 0 {
			err := idx.undoState.DisconnectBlock(bestHeight)
			if err != nil {
				return err
			}
			bestHeight--
		}
	}

	if idx.proofStatsState.BestHeight() != 0 &&
		tipHeight < idx.proofStatsState.BestHeight() {
		bestHeight := idx.proofStatsState.BestHeight()
		for tipHeight != bestHeight && bestHeight > 0 {
			err := idx.proofStatsState.DisconnectBlock(bestHeight)
			if err != nil {
				return err
			}
			bestHeight--
		}
	}
	if idx.rootsState.BestHeight() != 0 &&
		tipHeight < idx.rootsState.BestHeight() {
		bestHeight := idx.rootsState.BestHeight()
		for tipHeight != bestHeight && bestHeight > 0 {
			err := idx.rootsState.DisconnectBlock(bestHeight)
			if err != nil {
				return err
			}
			bestHeight--
		}
	}

	return nil
}

// initUtreexoRootsState creates an accumulator from all the existing roots and
// holds it in memory so that the proofs for them can be generated.
func (idx *FlatUtreexoProofIndex) initUtreexoRootsState() error {
	idx.utreexoRootsState = utreexo.NewAccumulator()

	bestHeight := idx.rootsState.BestHeight()
	for h := int32(0); h <= bestHeight; h++ {
		stump, err := idx.fetchRoots(h)
		if err != nil {
			return err
		}
		bytes, err := blockchain.SerializeUtreexoRoots(stump.NumLeaves, stump.Roots)
		if err != nil {
			return err
		}
		rootHash := sha256.Sum256(bytes)

		err = idx.utreexoRootsState.Modify(
			[]utreexo.Leaf{{Hash: rootHash}}, nil, utreexo.Proof{})
		if err != nil {
			return err
		}
	}

	return nil
}

// initBlockSummaryState creates and accumulator from the block summaries of each
// block and holds it in memory so that the proofs for them can be generated.
func (idx *FlatUtreexoProofIndex) initBlockSummaryState() error {
	idx.blockSummaryState = utreexo.NewAccumulator()

	var prevNumLeaves uint64
	bestHeight := idx.proofState.BestHeight()
	for h := int32(0); h <= bestHeight; h++ {
		blockHash, err := idx.chain.BlockHashByHeight(h)
		if err != nil {
			return err
		}

		stump, err := idx.fetchRoots(h)
		if err != nil {
			return err
		}
		numAdds := uint16(stump.NumLeaves - prevNumLeaves)
		prevNumLeaves = stump.NumLeaves

		proof, err := idx.FetchUtreexoProof(h)
		if err != nil {
			return err
		}

		blockHeader := wire.UtreexoBlockSummary{
			BlockHash:    *blockHash,
			NumAdds:      numAdds,
			BlockTargets: make([]uint64, len(proof.AccProof.Targets)),
		}
		copy(blockHeader.BlockTargets, proof.AccProof.Targets)

		buf := bytes.NewBuffer(make([]byte, 0, blockHeader.SerializeSize()))
		err = blockHeader.Serialize(buf)
		if err != nil {
			return err
		}
		rootHash := sha256.Sum256(buf.Bytes())

		err = idx.blockSummaryState.Modify(
			[]utreexo.Leaf{{Hash: rootHash}}, nil, utreexo.Proof{})
		if err != nil {
			return err
		}
	}

	return nil
}

// Init initializes the flat utreexo proof index. This is part of the Indexer
// interface.
func (idx *FlatUtreexoProofIndex) Init(chain *blockchain.BlockChain,
	tipHash *chainhash.Hash, tipHeight int32) error {

	idx.chain = chain

	// Init Utreexo State.
	uState, err := InitUtreexoState(idx.config, chain, tipHash, tipHeight)
	if err != nil {
		return err
	}
	idx.utreexoState = uState
	idx.lastFlushTime = time.Now()

	err = idx.consistentFlatFileState(tipHeight)
	if err != nil {
		return err
	}

	err = idx.initUtreexoRootsState()
	if err != nil {
		return err
	}

	if !idx.config.Pruned {
		// Only build the block summary state if we're not pruned.
		err = idx.initBlockSummaryState()
		if err != nil {
			return err
		}

		return nil
	}

	// We're here because the node is pruned.
	//
	// If the node is pruned, then we need to check if it started off as
	// a pruned node or if the user switch to being a pruned node.
	proofPath := flatFilePath(idx.config.DataDir, flatUtreexoProofName)
	_, err = os.Stat(proofPath)
	if err != nil {
		// If the error isn't nil, that means the proofpath
		// doesn't exist.
		return nil
	}
	proofState, err := loadFlatFileState(idx.config.DataDir, flatUtreexoProofName)
	if err != nil {
		return err
	}
	idx.proofState = *proofState

	// Nothing to do if the best height is 0.
	bestHeight := chain.BestSnapshot().Height
	if bestHeight <= 0 {
		return nil
	}

	// Make undo blocks for blocks up to 288 block from the tip. 288 since
	// that's the basis used for NODE_NETWORK_LIMITED. Reorgs that go past
	// that are gonna be problematic anyways.
	undoCount := int32(288)

	// The bestHeight is less than 288, then just undo all the blocks we have.
	if undoCount > bestHeight {
		undoCount = bestHeight
	}

	// Disconnect blocks.
	for i := int32(0); i < undoCount; i++ {
		height := bestHeight - i
		err = idx.undoState.DisconnectBlock(height)
		if err != nil {
			return err
		}
	}

	for height := bestHeight - (undoCount - 1); height <= bestHeight; height++ {
		// Fetch and deserialize proof.
		proofBytes, err := idx.proofState.FetchData(height)
		if err != nil {
			return err
		}
		if proofBytes == nil {
			return fmt.Errorf("Couldn't fetch Utreexo proof for height %d", height)
		}
		r := bytes.NewReader(proofBytes)
		ud := new(wire.UData)
		err = ud.Deserialize(r)
		if err != nil {
			return err
		}

		// Fetch block.
		block, err := idx.chain.BlockByHeight(height)
		if err != nil {
			return err
		}

		// Generate the data for the undo block.
		_, outCount, _, outskip := blockchain.DedupeBlock(block)
		adds := blockchain.BlockToAddLeaves(block, outskip, nil, outCount)
		delHashes, err := idx.chain.ReconstructUData(ud, *block.Hash())
		if err != nil {
			return err
		}

		// Store undo block.
		err = idx.storeUndoBlock(height,
			uint64(len(adds)), ud.AccProof.Targets, delHashes)
		if err != nil {
			return err
		}
	}

	// Remove all the proofs as we don't serve proofs as a
	// pruned brigde node.
	err = deleteFlatFile(proofPath)
	if err != nil {
		return err
	}

	// Delete proof stat file since it's not relevant to a pruned bridge node.
	proofStatPath := flatFilePath(idx.config.DataDir, flatUtreexoProofStatsName)
	err = deleteFlatFile(proofStatPath)
	if err != nil {
		return err
	}

	return nil
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
	dels, err := blockchain.BlockToDelLeaves(stxos, idx.chain, block, inskip)
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
	// For pruned nodes, we need to save the undo block in order to undo
	// a block on reorgs. If we have all the proofs block by block, that
	// data can be used for reorgs but a pruned node will not have the
	// proofs available.
	if idx.config.Pruned {
		err = idx.storeUndoBlock(block.Height(),
			uint64(len(adds)), ud.AccProof.Targets, delHashes)
		if err != nil {
			return err
		}
	} else {
		err = idx.storeUndoBlock(block.Height(), 0, nil, nil)
		if err != nil {
			return err
		}
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

	err = idx.storeRoots(block.Height(), idx.utreexoState.state)
	if err != nil {
		return err
	}

	err = idx.updateRootsState()
	if err != nil {
		return err
	}

	// Don't store proofs if the node is pruned.
	if idx.config.Pruned {
		return nil
	}

	idx.pStats.UpdateTotalDelCount(uint64(len(dels)))
	idx.pStats.UpdateUDStats(false, ud)

	idx.pStats.BlockHeight = uint64(block.Height())
	err = idx.pStats.WritePStats(&idx.proofStatsState)
	if err != nil {
		return err
	}

	err = idx.storeProof(block.Height(), ud)
	if err != nil {
		return err
	}

	return idx.updateBlockSummaryState(uint16(len(adds)), block.Hash(), ud.AccProof)
}

// calcProofOverhead calculates the overhead of the current utreexo accumulator proof
// has.
func calcProofOverhead(ud *wire.UData) float64 {
	if ud == nil || len(ud.AccProof.Targets) == 0 {
		return 0
	}

	return float64(len(ud.AccProof.Proof)) / float64(len(ud.AccProof.Targets))
}

// attachBlock attaches the passed in block to the utreexo accumulator state.
func (idx *FlatUtreexoProofIndex) attachBlock(blk *btcutil.Block, stxos []blockchain.SpentTxOut) error {
	_, outCount, inskip, outskip := blockchain.DedupeBlock(blk)
	dels, err := blockchain.BlockToDelLeaves(stxos, idx.chain, blk, inskip)
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
func (idx *FlatUtreexoProofIndex) resyncUtreexoState(start, finish int32) error {
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

// getUndoData returns the data needed for undo. For pruned nodes, we fetch the data from the undo block.
// For archive nodes, we generate the data from the proof.
func (idx *FlatUtreexoProofIndex) getUndoData(block *btcutil.Block) (uint64, []uint64, []utreexo.Hash, error) {
	var (
		numAdds   uint64
		targets   []uint64
		delHashes []utreexo.Hash
	)

	if !idx.config.Pruned {
		ud, err := idx.FetchUtreexoProof(block.Height())
		if err != nil {
			return 0, nil, nil, err
		}

		targets = ud.AccProof.Targets

		// Need to call reconstruct since the saved utreexo data is in the compact form.
		delHashes, err = idx.chain.ReconstructUData(ud, *block.Hash())
		if err != nil {
			return 0, nil, nil, err
		}

		_, outCount, _, outskip := blockchain.DedupeBlock(block)
		adds := blockchain.BlockToAddLeaves(block, outskip, nil, outCount)

		numAdds = uint64(len(adds))
	} else {
		var err error
		numAdds, targets, delHashes, err = idx.fetchUndoBlock(block.Height())
		if err != nil {
			return 0, nil, nil, err
		}
	}

	return numAdds, targets, delHashes, nil
}

// DisconnectBlock is invoked by the index manager when a new block has been
// disconnected to the main chain.
//
// This is part of the Indexer interface.
func (idx *FlatUtreexoProofIndex) DisconnectBlock(dbTx database.Tx, block *btcutil.Block,
	stxos []blockchain.SpentTxOut) error {

	state, err := idx.fetchRoots(block.Height() - 1)
	if err != nil {
		return err
	}

	numAdds, targets, delHashes, err := idx.getUndoData(block)
	if err != nil {
		return err
	}

	idx.mtx.Lock()
	err = idx.utreexoState.state.Undo(numAdds, utreexo.Proof{Targets: targets}, delHashes, state.Roots)
	idx.mtx.Unlock()
	if err != nil {
		return err
	}

	// Always flush the utreexo state on flushes to never leave the utreexoState
	// at an unrecoverable state.
	err = idx.flushUtreexoState(&block.MsgBlock().Header.PrevBlock)
	if err != nil {
		return err
	}

	// Check if we're at a height where proof was generated. Only check if we're not
	// pruned as we don't keep the historical proofs as a pruned node.
	if !idx.config.Pruned {
		err = idx.proofState.DisconnectBlock(block.Height())
		if err != nil {
			return err
		}

		// Re-initializes to the current accumulator roots, effectively
		// disconnecting a block.
		err = idx.initBlockSummaryState()
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

	// Re-initializes to the current accumulator roots, effectively disconnecting
	// a block.
	return idx.initUtreexoRootsState()
}

// PruneBlock is invoked when an older block is deleted after it's been
// processed.
//
// This is part of the Indexer interface.
func (idx *FlatUtreexoProofIndex) PruneBlock(_ database.Tx, _ *chainhash.Hash, lastKeptHeight int32) error {
	hash, _, err := dbFetchUtreexoStateConsistency(idx.utreexoState.utreexoStateDB)
	if err != nil {
		return err
	}

	// It's ok to call block by hash here as the utreexo state consistency hash is always
	// included in the best chain.
	lastFlushHeight, err := idx.chain.BlockHeightByHash(hash)
	if err != nil {
		return err
	}

	// If the last flushed utreexo state is the last or greater than the kept block,
	// we can sync up to the tip so a flush is not required.
	if lastKeptHeight <= lastFlushHeight {
		return nil
	}

	// It's ok to fetch the best snapshot here as the block called on pruneblock has not
	// been yet connected yet on the utreexo state. So this is indeed the correct hash.
	bestHash := idx.chain.BestSnapshot().Hash
	return idx.Flush(&bestHash, blockchain.FlushRequired, true)
}

// FetchUtreexoProof returns the Utreexo proof data for the given block height.
func (idx *FlatUtreexoProofIndex) FetchUtreexoProof(height int32) (
	*wire.UData, error) {

	if height == 0 {
		return &wire.UData{}, nil
	}

	if idx.config.Pruned {
		return nil, fmt.Errorf("Cannot fetch historical proof as the node is pruned")
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
	err = ud.Deserialize(r)
	if err != nil {
		return nil, err
	}

	return ud, nil
}

// GetLeafHashPositions returns the positions of the passed in hashes.
func (idx *FlatUtreexoProofIndex) GetLeafHashPositions(delHashes []utreexo.Hash) []uint64 {
	idx.mtx.RLock()
	defer idx.mtx.RUnlock()

	positions := make([]uint64, len(delHashes))
	for i, delHash := range delHashes {
		pos, _ := idx.utreexoState.state.GetLeafPosition(delHash)
		positions[i] = pos
	}

	return positions
}

// GenerateUDataPartial generates a utreexo data based on the current state of the accumulator.
// It leaves out the full proof hashes and only fetches the requested positions.
func (idx *FlatUtreexoProofIndex) GenerateUDataPartial(dels []wire.LeafData, positions []uint64) (*wire.UData, error) {
	idx.mtx.RLock()
	defer idx.mtx.RUnlock()

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

	hashes := make([]utreexo.Hash, len(positions))
	for i, pos := range positions {
		hashes[i] = idx.utreexoState.state.GetHash(pos)
	}

	targets := make([]uint64, len(delHashes))
	for i, delHash := range delHashes {
		pos, found := idx.utreexoState.state.GetLeafPosition(delHash)
		if found {
			targets[i] = pos
		}
	}

	ud.AccProof = utreexo.Proof{
		Targets: targets,
		Proof:   hashes,
	}

	return ud, nil
}

// storeProof serializes and stores the utreexo data in the proof state.
func (idx *FlatUtreexoProofIndex) storeProof(height int32, ud *wire.UData) error {
	bytesBuf := bytes.NewBuffer(make([]byte, 0, ud.SerializeSize()))
	err := ud.Serialize(bytesBuf)
	if err != nil {
		return err
	}

	err = idx.proofState.StoreData(height, bytesBuf.Bytes())
	if err != nil {
		return fmt.Errorf("store proof err. %v", err)
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
		return fmt.Errorf("store undoblock err. %v", err)
	}

	return nil
}

// storeRoots serializes and stores roots to the roots state.
func (idx *FlatUtreexoProofIndex) storeRoots(height int32, p utreexo.Utreexo) error {
	serialized, err := blockchain.SerializeUtreexoRoots(p.GetNumLeaves(), p.GetRoots())
	if err != nil {
		return err
	}

	err = idx.rootsState.StoreData(height, serialized)
	if err != nil {
		return fmt.Errorf("store roots err. %v", err)
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

// fetchRoots returns the roots at the given height. It doesn't work for the current hegiht
// and will return an error.
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

// updateRootsState updates the roots accumulator state from the roots of the current accumulator.
func (idx *FlatUtreexoProofIndex) updateRootsState() error {
	idx.mtx.Lock()
	numLeaves := idx.utreexoState.state.GetNumLeaves()
	roots := idx.utreexoState.state.GetRoots()
	idx.mtx.Unlock()

	bytes, err := blockchain.SerializeUtreexoRoots(numLeaves, roots)
	if err != nil {
		return err
	}

	rootHash := sha256.Sum256(bytes)
	return idx.utreexoRootsState.Modify([]utreexo.Leaf{{Hash: rootHash}}, nil, utreexo.Proof{})
}

// updateBlockSummaryState updates the block summary accumulator state with the given inputs.
func (idx *FlatUtreexoProofIndex) updateBlockSummaryState(numAdds uint16, blockHash *chainhash.Hash, proof utreexo.Proof) error {
	summary := wire.UtreexoBlockSummary{
		BlockHash:    *blockHash,
		NumAdds:      numAdds,
		BlockTargets: make([]uint64, len(proof.Targets)),
	}
	copy(summary.BlockTargets, proof.Targets)

	buf := bytes.NewBuffer(make([]byte, 0, summary.SerializeSize()))
	err := summary.Serialize(buf)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(buf.Bytes())

	return idx.blockSummaryState.Modify([]utreexo.Leaf{{Hash: hash}}, nil, utreexo.Proof{})
}

// FetchUtreexoSummaries fetches all the summaries and attaches a proof for those summaries.
func (idx *FlatUtreexoProofIndex) FetchUtreexoSummaries(blockHashes []*chainhash.Hash) (*wire.MsgUtreexoSummaries, error) {
	msg := wire.MsgUtreexoSummaries{
		Summaries: make([]*wire.UtreexoBlockSummary, 0, len(blockHashes)),
	}

	leafHashes := make([]utreexo.Hash, 0, len(blockHashes))
	for _, blockHash := range blockHashes {
		summary, err := idx.fetchBlockSummary(blockHash)
		if err != nil {
			return nil, err
		}
		msg.Summaries = append(msg.Summaries, summary)

		buf := bytes.NewBuffer(make([]byte, 0, summary.SerializeSize()))
		err = summary.Serialize(buf)
		if err != nil {
			return nil, err
		}
		leafHashes = append(leafHashes, sha256.Sum256(buf.Bytes()))
	}

	proof, err := idx.blockSummaryState.Prove(leafHashes)
	if err != nil {
		return nil, err
	}

	msg.ProofHashes = proof.Proof

	return &msg, nil
}

func (idx *FlatUtreexoProofIndex) fetchBlockSummary(blockHash *chainhash.Hash) (*wire.UtreexoBlockSummary, error) {
	height, err := idx.chain.BlockHeightByHash(blockHash)
	if err != nil {
		return nil, err
	}

	ud, err := idx.FetchUtreexoProof(height)
	if err != nil {
		return nil, err
	}

	stump, err := idx.fetchRoots(height)
	if err != nil {
		return nil, err
	}
	var prevStump utreexo.Stump
	if height-1 != 0 {
		prevStump, err = idx.fetchRoots(height - 1)
		if err != nil {
			return nil, err
		}
	}
	numAdds := uint16(stump.NumLeaves - prevStump.NumLeaves)

	return &wire.UtreexoBlockSummary{
		BlockHash:    *blockHash,
		NumAdds:      numAdds,
		BlockTargets: ud.AccProof.Targets,
	}, nil
}

// FetchMsgUtreexoRoot returns a complete utreexoroot bitcoin message on the requested block.
func (idx *FlatUtreexoProofIndex) FetchMsgUtreexoRoot(blockHash *chainhash.Hash) (*wire.MsgUtreexoRoot, error) {
	height, err := idx.chain.BlockHeightByHash(blockHash)
	if err != nil {
		return nil, err
	}

	stump, err := idx.fetchRoots(height)
	if err != nil {
		return nil, err
	}

	bytes, err := blockchain.SerializeUtreexoRoots(stump.NumLeaves, stump.Roots)
	if err != nil {
		return nil, err
	}
	rootHash := sha256.Sum256(bytes)

	proof, err := idx.utreexoRootsState.Prove([]utreexo.Hash{rootHash})
	if err != nil {
		return nil, err
	}

	msg := &wire.MsgUtreexoRoot{
		NumLeaves: stump.NumLeaves,
		Target:    proof.Targets[0],
		BlockHash: *blockHash,
		Roots:     stump.Roots,
		Proof:     proof.Proof,
	}

	return msg, nil
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
// The passed in maxMemoryUsage should be in bytes and it determines how much memory the proof index will use up.
//
// It implements the Indexer interface which plugs into the IndexManager that in
// turn is used by the blockchain package.  This allows the index to be
// seamlessly maintained along with the chain.
func NewFlatUtreexoProofIndex(pruned bool, chainParams *chaincfg.Params,
	maxMemoryUsage int64, dataDir string, flush func() error) (*FlatUtreexoProofIndex, error) {

	idx := &FlatUtreexoProofIndex{
		mtx: new(sync.RWMutex),
		config: &UtreexoConfig{
			MaxMemoryUsage: maxMemoryUsage,
			Params:         chainParams,
			Pruned:         pruned,
			DataDir:        dataDir,
			Name:           flatUtreexoProofIndexType,
			FlushMainDB:    flush,
		},
	}

	// Init the utreexo proof state if the node isn't pruned.
	if !idx.config.Pruned {
		proofState, err := loadFlatFileState(dataDir, flatUtreexoProofName)
		if err != nil {
			return nil, err
		}
		idx.proofState = *proofState
	}

	// Init the undo block state.
	undoState, err := loadFlatFileState(dataDir, flatUtreexoUndoName)
	if err != nil {
		return nil, err
	}
	idx.undoState = *undoState

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

// FlatUtreexoProofIndexInitialized returns true if the cfindex has been created previously.
func FlatUtreexoProofIndexInitialized(db database.DB) bool {
	var exists bool
	db.View(func(dbTx database.Tx) error {
		bucket := dbTx.Metadata().Bucket(flatUtreexoBucketKey)
		exists = bucket != nil
		return nil
	})

	return exists
}
