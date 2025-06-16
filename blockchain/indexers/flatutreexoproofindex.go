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

	// flatUtreexoTargetName is the name given to the target data of the flat utreexo
	// target index.  This name is used as the dataFile name in the flat files.
	flatUtreexoTargetName = "target"

	// flatUtreexoProofName is the name given to the proof data of the flat utreexo
	// proof index.  This name is used as the dataFile name in the flat files.
	flatUtreexoProofName = "proof"

	// flatUtreexoLeafDataName is the name given to the leafdata of the flat utreexo
	// proof index.  This name is used as the dataFile name in the flat files.
	flatUtreexoLeafDataName = "leafdata"

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

	// flatTTLsName is the name given to the ttl data of the flat utreexo proof index.
	// This name is used as the dataFile in the flat files.
	flatTTLsName = "ttls"

	// defaultProofGenInterval is the default value used to determine how often
	// a utreexo accumulator proof should be generated.  An interval of 10 will
	// make the proof be generated on blocks 10, 20, 30 and so on.
	defaultProofGenInterval = 10

	// ttlSize denotes how big each ttl is. It's 16 because each ttl is 2 uint64s.
	ttlSize = 16

	// size of a uint64. Shame go std lib doesn't provide it.
	uint64Size = 8
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
	targetState     FlatFileState
	proofState      FlatFileState
	leafDataState   FlatFileState
	undoState       FlatFileState
	proofStatsState FlatFileState
	rootsState      FlatFileState
	ttlState        FlatFileState

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

	// blockTTLState is the accumulator for all the ttls at each of the blocks.
	// Since their job is to provide proofs for the committed ttls, there's one
	// for each block that there's a commitment for.
	blockTTLState []utreexo.Pollard

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
		if idx.targetState.BestHeight() != 0 &&
			tipHeight < idx.targetState.BestHeight() {
			bestHeight := idx.targetState.BestHeight()
			for tipHeight != bestHeight && bestHeight > 0 {
				err := idx.targetState.DisconnectBlock(bestHeight)
				if err != nil {
					return err
				}
				bestHeight--
			}
		}

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

		if idx.leafDataState.BestHeight() != 0 &&
			tipHeight < idx.leafDataState.BestHeight() {
			bestHeight := idx.leafDataState.BestHeight()
			for tipHeight != bestHeight && bestHeight > 0 {
				err := idx.leafDataState.DisconnectBlock(bestHeight)
				if err != nil {
					return err
				}
				bestHeight--
			}
		}

		if idx.ttlState.BestHeight() != 0 &&
			tipHeight < idx.ttlState.BestHeight() {
			bestHeight := idx.ttlState.BestHeight()
			for tipHeight != bestHeight && bestHeight > 0 {
				err := idx.ttlState.DisconnectBlock(bestHeight)
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

// initTTLState initializes an accumulator to the passed in height and returns it.
// For each of the committed ttl, it checks that the ttl value is able to be calculated
// at that height. If it's not, then the ttl is set to 0.
func (idx *FlatUtreexoProofIndex) initTTLState(height int32) (*utreexo.Pollard, error) {
	p := utreexo.NewAccumulator()

	// Add ttl for block 0. This is so that the ttl on height 1 also has a target
	// of 1.
	emptyTTL := wire.UtreexoTTL{}
	buf := bytes.NewBuffer(make([]byte, 0, emptyTTL.SerializeSize()))
	err := emptyTTL.Serialize(buf)
	if err != nil {
		return nil, err
	}
	rootHash := sha256.Sum256(buf.Bytes())
	err = p.Modify([]utreexo.Leaf{{Hash: rootHash}}, nil, utreexo.Proof{})
	if err != nil {
		return nil, err
	}

	for h := int32(1); h <= height; h++ {
		ttls, err := idx.fetchTTLs(h)
		if err != nil {
			return nil, err
		}

		// If the leaf was spent after the given height, we reset it to 0
		// as it's not spent at the height and thus doesn't have a ttl value
		// we can use.
		for i, ttl := range ttls {
			if ttl.TTL+uint64(h) > uint64(height) {
				ttls[i].TTL = 0
				ttls[i].DeathPos = 0
			}
		}

		ttl := wire.UtreexoTTL{
			BlockHeight: uint32(h),
			TTLs:        ttls,
		}

		buf := bytes.NewBuffer(make([]byte, 0, ttl.SerializeSize()))
		err = ttl.Serialize(buf)
		if err != nil {
			return nil, err
		}
		rootHash := sha256.Sum256(buf.Bytes())

		err = p.Modify([]utreexo.Leaf{{Hash: rootHash}}, nil, utreexo.Proof{})
		if err != nil {
			return nil, err
		}
	}

	return &p, nil
}

// Init initializes the flat utreexo proof index. This is part of the Indexer
// interface.
func (idx *FlatUtreexoProofIndex) Init(chain *blockchain.BlockChain,
	tipHash *chainhash.Hash, tipHeight int32) error {

	idx.chain = chain

	// Init Utreexo State.
	uState, err := InitUtreexoState(idx.config, chain, &idx.ttlState, tipHash, tipHeight)
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

	err = idx.updateBlockTTLState(tipHeight)
	if err != nil {
		return err
	}

	// Nothing to do if we're not pruned.
	if !idx.config.Pruned {
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
		ud, err := idx.fetchUtreexoProof(height)
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
		adds := blockchain.BlockToAddLeaves(block, outskip, outCount)
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
	// pruned bridge node.
	err = deleteFlatFile(proofPath)
	if err != nil {
		return err
	}
	targetPath := flatFilePath(idx.config.DataDir, flatUtreexoTargetName)
	err = deleteFlatFile(targetPath)
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
	adds := blockchain.BlockToAddLeaves(block, outskip, outCount)

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

	addHashes := make([]utreexo.Leaf, 0, len(adds))
	for _, add := range adds {
		addHashes = append(addHashes, utreexo.Leaf{Hash: add.LeafHash(), Remember: false})
	}

	idx.mtx.Lock()
	createdIndexes, err := idx.utreexoState.state.ModifyAndReturnTTLs(addHashes, delHashes, ud.AccProof)
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

	err = idx.addEmptyTTLs(block.Height(), int32(len(adds)))
	if err != nil {
		return err
	}

	err = idx.writeTTLs(block.Height(), createdIndexes, ud.AccProof.Targets, ud.LeafDatas)
	if err != nil {
		return err
	}

	return idx.updateBlockTTLState(block.Height())
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

	adds := blockchain.BlockToAddLeaves(blk, outskip, outCount)
	ud, err := wire.GenerateUData(dels, idx.utreexoState.state)
	if err != nil {
		return err
	}

	delHashes := make([]utreexo.Hash, len(dels))
	for i, del := range dels {
		delHashes[i] = del.LeafHash()
	}

	addHashes := make([]utreexo.Leaf, 0, len(adds))
	for _, add := range adds {
		addHashes = append(addHashes, utreexo.Leaf{Hash: add.LeafHash(), Remember: false})
	}
	err = idx.utreexoState.state.Modify(addHashes, delHashes, ud.AccProof)
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
func (idx *FlatUtreexoProofIndex) getUndoData(block *btcutil.Block) (
	uint64, []uint64, []utreexo.Hash, *wire.UData, error) {

	var (
		numAdds   uint64
		targets   []uint64
		delHashes []utreexo.Hash
		ud        *wire.UData
	)

	if !idx.config.Pruned {
		var err error
		ud, err = idx.FetchUtreexoProof(block.Height())
		if err != nil {
			return 0, nil, nil, nil, err
		}

		targets = ud.AccProof.Targets

		// Need to call reconstruct since the saved utreexo data is in the compact form.
		delHashes, err = idx.chain.ReconstructUData(ud, *block.Hash())
		if err != nil {
			return 0, nil, nil, nil, err
		}

		_, outCount, _, outskip := blockchain.DedupeBlock(block)
		adds := blockchain.BlockToAddLeaves(block, outskip, outCount)

		numAdds = uint64(len(adds))
	} else {
		var err error
		numAdds, targets, delHashes, err = idx.fetchUndoBlock(block.Height())
		if err != nil {
			return 0, nil, nil, nil, err
		}
	}

	return numAdds, targets, delHashes, ud, nil
}

// getCreateIndexes returns the indexes within the newly created leaves that the delhashes were at.
func (idx *FlatUtreexoProofIndex) getCreateIndexes(lds []wire.LeafData, delHashes []utreexo.Hash) ([]uint32, error) {
	createIndexes := make([]uint32, 0, len(lds))
	for i, ld := range lds {
		stxoBlock, err := idx.chain.BlockByHeight(ld.Height)
		if err != nil {
			return nil, err
		}

		_, outCount, _, outskip := blockchain.DedupeBlock(stxoBlock)
		adds := blockchain.BlockToAddLeaves(stxoBlock, outskip, outCount)
		for j, add := range adds {
			if add.LeafHash() == delHashes[i] {
				createIndexes = append(createIndexes, uint32(j))
				break
			}
		}
	}

	return createIndexes, nil
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

	numAdds, targets, delHashes, ud, err := idx.getUndoData(block)
	if err != nil {
		return err
	}

	if !idx.config.Pruned {
		createIndexes, err := idx.getCreateIndexes(ud.LeafDatas, delHashes)
		if err != nil {
			return err
		}

		err = idx.resetTTLs(ud, createIndexes)
		if err != nil {
			return err
		}

		idx.mtx.Lock()
		err = idx.utreexoState.state.UndoWithTTLs(numAdds, createIndexes, utreexo.Proof{Targets: targets}, delHashes, state.Roots)
		idx.mtx.Unlock()
		if err != nil {
			return err
		}
	} else {
		idx.mtx.Lock()
		err = idx.utreexoState.state.Undo(numAdds, utreexo.Proof{Targets: targets}, delHashes, state.Roots)
		idx.mtx.Unlock()
		if err != nil {
			return err
		}
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
		err = idx.targetState.DisconnectBlock(block.Height())
		if err != nil {
			return err
		}

		err = idx.proofState.DisconnectBlock(block.Height())
		if err != nil {
			return err
		}

		err = idx.ttlState.DisconnectBlock(block.Height())
		if err != nil {
			return err
		}

		err = idx.leafDataState.DisconnectBlock(block.Height())
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

	return idx.fetchUtreexoProof(height)
}

// fetchUtreexoProof returns the Utreexo proof data for the given block height.
// Returns an error if it couldn't fetch the proof.
func (idx *FlatUtreexoProofIndex) fetchUtreexoProof(height int32) (
	*wire.UData, error) {

	targets, err := idx.fetchTargets(height)
	if err != nil {
		return nil, err
	}

	leafDatas, err := idx.fetchLeafDatas(height)
	if err != nil {
		return nil, err
	}

	proofHashes, err := idx.fetchProofHashes(height)
	if err != nil {
		return nil, err
	}

	ud := wire.UData{
		AccProof: utreexo.Proof{
			Targets: targets,
			Proof:   proofHashes,
		},
		LeafDatas: leafDatas,
	}

	return &ud, nil
}

// fetchTargets fetches the targets at the given height. Returns an error if it couldn't
// fetch it.
func (idx *FlatUtreexoProofIndex) fetchTargets(height int32) ([]uint64, error) {
	targetBytes, err := idx.targetState.FetchData(height)
	if err != nil {
		return nil, err
	}
	if targetBytes == nil {
		return nil, fmt.Errorf("Couldn't fetch targets for height %d", height)
	}
	r := bytes.NewReader(targetBytes)

	return wire.ProofTargetsDeserialize(r)
}

// fetchLeafDatas fetches the leafdatas at the given height. Returns an error if it couldn't
// fetch it.
func (idx *FlatUtreexoProofIndex) fetchLeafDatas(height int32) ([]wire.LeafData, error) {
	leafDataBytes, err := idx.leafDataState.FetchData(height)
	if err != nil {
		return nil, err
	}
	if leafDataBytes == nil {
		return nil, fmt.Errorf("Couldn't fetch leafDatas for height %d", height)
	}
	r := bytes.NewReader(leafDataBytes)

	return wire.DeserializeUtxoData(r)
}

// fetchProofHashes fetches the proof hashes at the given height. Returns an error if it
// couldn't fetch it.
func (idx *FlatUtreexoProofIndex) fetchProofHashes(height int32) ([]utreexo.Hash, error) {
	proofHashesBytes, err := idx.proofState.FetchData(height)
	if err != nil {
		return nil, err
	}
	if proofHashesBytes == nil {
		return nil, fmt.Errorf("Couldn't fetch proofHashess for height %d", height)
	}
	r := bytes.NewReader(proofHashesBytes)

	return wire.ProofHashesDeserialize(r)
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

// getPollard returns the pollard in the flatutreexoproofindex with the matching
// version. Returns nil if the version is not there.
func (idx *FlatUtreexoProofIndex) getPollard(version uint32) *utreexo.Pollard {
	var p *utreexo.Pollard
	for _, ttlState := range idx.blockTTLState {
		if ttlState.NumLeaves == uint64(version) {
			p = &ttlState
		}
	}

	return p
}

// FetchTTLs fetches the ttls as a MsgUtreexoTTLs from the given data.
func (idx *FlatUtreexoProofIndex) FetchTTLs(version, startHeight uint32, exponent uint8) (
	wire.MsgUtreexoTTLs, error) {

	p := idx.getPollard(version)
	if p == nil {
		return wire.MsgUtreexoTTLs{}, fmt.Errorf("version %v doesn't exist", version)
	}

	heights, err := wire.GetUtreexoSummaryHeights(
		int32(startHeight), int32(version), exponent)
	if err != nil {
		return wire.MsgUtreexoTTLs{}, err
	}

	msg := wire.MsgUtreexoTTLs{
		TTLs: make([]wire.UtreexoTTL, 0, len(heights)),
	}

	leafHashes := make([]utreexo.Hash, 0, len(heights))
	for _, height := range heights {
		ttls, err := idx.fetchTTLs(height)
		if err != nil {
			return wire.MsgUtreexoTTLs{}, err
		}

		// Remove the ttls that were added after the commitment.
		for i, ttl := range ttls {
			if ttl.TTL+uint64(height) > uint64(version) {
				ttls[i].TTL = 0
				ttls[i].DeathPos = 0
			}
		}

		ttl := wire.UtreexoTTL{
			BlockHeight: uint32(height),
			TTLs:        ttls,
		}
		msg.TTLs = append(msg.TTLs, ttl)

		buf := bytes.NewBuffer(make([]byte, 0, ttl.SerializeSize()))
		err = ttl.Serialize(buf)
		if err != nil {
			return wire.MsgUtreexoTTLs{}, err
		}
		leafHashes = append(leafHashes, sha256.Sum256(buf.Bytes()))
	}

	proof, err := p.Prove(leafHashes)
	if err != nil {
		return wire.MsgUtreexoTTLs{}, err
	}
	msg.ProofHashes = proof.Proof

	return msg, nil
}

// fetchTTLs fetches the ttls at the given height.
func (idx *FlatUtreexoProofIndex) fetchTTLs(height int32) (
	[]wire.TTLInfo, error) {

	if height == 0 {
		return nil, nil
	}

	if idx.config.Pruned {
		return nil, fmt.Errorf("Cannot fetch ttl data as the node is pruned")
	}

	ttlBytes, err := idx.ttlState.FetchData(height)
	if err != nil {
		return nil, err
	}
	if ttlBytes == nil {
		return nil, fmt.Errorf("Couldn't fetch ttl data for height %d", height)
	}

	ttls := make([]wire.TTLInfo, len(ttlBytes)/ttlSize)
	for i := range ttls {
		start := i * ttlSize
		ttls[i].TTL = byteOrder.Uint64(ttlBytes[start : start+uint64Size])
		ttls[i].DeathPos = byteOrder.Uint64(ttlBytes[start+uint64Size : start+ttlSize])
	}

	return ttls, nil
}

// resetTTLs is used during reorganizations when stxos are turned about into utxos. The ttls
// at the given heights and indexes are marked as unspent.
func (idx *FlatUtreexoProofIndex) resetTTLs(ud *wire.UData, createdIndexes []uint32) error {
	if len(ud.LeafDatas) == 0 {
		return nil
	}

	buf := [ttlSize]byte{}
	for i, ld := range ud.LeafDatas {
		cIndex := createdIndexes[i]
		offset := int32(cIndex) * ttlSize
		err := idx.ttlState.OverWrite(ld.Height, offset, buf[:])
		if err != nil {
			return err
		}
	}

	return nil
}

// writeTTLs writes the ttls at the given heights and indexes.
func writeTTLs(curHeight int32, createdIndexes []uint32, targets []uint64, lds []wire.LeafData,
	ttlIdx *FlatFileState) error {

	// Nothing to do.
	if ttlIdx == nil || len(lds) == 0 {
		return nil
	}
	buf := [ttlSize]byte{}

	for i, ld := range lds {
		ttl := uint64(curHeight - ld.Height)
		byteOrder.PutUint64(buf[:uint64Size], ttl)
		byteOrder.PutUint64(buf[uint64Size:], targets[i])

		cIndex := createdIndexes[i]
		offset := int32(cIndex) * ttlSize
		err := ttlIdx.OverWrite(ld.Height, offset, buf[:])
		if err != nil {
			return err
		}
	}

	return nil
}

// writeTTLs is a wrapper on raw writeTTLs.
func (idx *FlatUtreexoProofIndex) writeTTLs(
	curHeight int32, createdIndexes []uint32, targets []uint64, lds []wire.LeafData) error {

	return writeTTLs(curHeight, createdIndexes, targets, lds, &idx.ttlState)
}

// addEmptyTTLs adds slots for every newly created leaf so that they may be marked in the future when they're spent.
func (idx *FlatUtreexoProofIndex) addEmptyTTLs(height, numAdds int32) error {
	buf := make([]byte, numAdds*ttlSize)
	err := idx.ttlState.StoreData(height, buf)
	if err != nil {
		return fmt.Errorf("store ttl err. %v", err)
	}

	return nil
}

// storeProof serializes and stores the utreexo data in the proof state.
func (idx *FlatUtreexoProofIndex) storeProof(height int32, ud *wire.UData) error {
	proofBuf := bytes.NewBuffer(
		make([]byte, 0, wire.BatchProofSerializeAccProofSize(&ud.AccProof)))
	err := wire.ProofHashesSerialize(proofBuf, ud.AccProof.Proof)
	if err != nil {
		return err
	}

	err = idx.proofState.StoreData(height, proofBuf.Bytes())
	if err != nil {
		return fmt.Errorf("store proof err. %v", err)
	}

	targetsBuf := bytes.NewBuffer(
		make([]byte, 0, wire.BatchProofSerializeTargetSize(&ud.AccProof)))
	err = wire.ProofTargetsSerialize(targetsBuf, ud.AccProof.Targets)
	if err != nil {
		return err
	}

	err = idx.targetState.StoreData(height, targetsBuf.Bytes())
	if err != nil {
		return fmt.Errorf("store targets err. %v", err)
	}

	leafBuf := bytes.NewBuffer(make([]byte, 0, ud.SerializeUtxoDataSize()))
	err = wire.SerializeUtxoData(leafBuf, ud.LeafDatas)
	if err != nil {
		return err
	}

	err = idx.leafDataState.StoreData(height, leafBuf.Bytes())
	if err != nil {
		return fmt.Errorf("store targets err. %v", err)
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

// updateBlockTTLState updates the latest ttls that have have became final.
func (idx *FlatUtreexoProofIndex) updateBlockTTLState(height int32) error {
	// This means we've already initialized everything that we can.
	if len(idx.blockTTLState) == len(idx.config.Params.TTL.Stump) {
		return nil
	}

	for i, stump := range idx.config.Params.TTL.Stump {
		// NumLeaves -1 as if we have 2 numleaves, it means the stump
		// has heights 0 and 1.
		if height < int32(stump.NumLeaves)-1 {
			continue
		}

		// Skip if we've already initialized this ttl state.
		if i < len(idx.blockTTLState) {
			continue
		}

		// NumLeaves -1 as if we have 2 numleaves, it means the stump
		// has heights 0 and 1.
		p, err := idx.initTTLState(int32(stump.NumLeaves) - 1)
		if err != nil {
			return err
		}

		idx.blockTTLState = append(idx.blockTTLState, *p)
	}

	return nil
}

// FetchUtreexoSummaries fetches all the summaries.
func (idx *FlatUtreexoProofIndex) FetchUtreexoSummaries(blockHashes []*chainhash.Hash) (*wire.MsgUtreexoSummaries, error) {
	msg := wire.MsgUtreexoSummaries{
		Summaries: make([]*wire.UtreexoBlockSummary, 0, len(blockHashes)),
	}

	for _, blockHash := range blockHashes {
		summary, err := idx.fetchBlockSummary(blockHash)
		if err != nil {
			return nil, err
		}
		msg.Summaries = append(msg.Summaries, summary)
	}

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
	numAdds := stump.NumLeaves - prevStump.NumLeaves

	return &wire.UtreexoBlockSummary{
		BlockHash:    *blockHash,
		NumAdds:      numAdds,
		BlockTargets: ud.AccProof.Targets,
	}, nil
}

// FetchTTLRoots returns the roots of the ttl summary state.
func (idx *FlatUtreexoProofIndex) FetchTTLRoots() (utreexo.Stump, error) {
	p, err := idx.initTTLState(idx.ttlState.BestHeight())
	if err != nil {
		return utreexo.Stump{}, err
	}

	return utreexo.Stump{
		Roots:     p.GetRoots(),
		NumLeaves: p.GetNumLeaves(),
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
		targetState, err := loadFlatFileState(dataDir, flatUtreexoTargetName)
		if err != nil {
			return nil, err
		}
		idx.targetState = *targetState

		proofState, err := loadFlatFileState(dataDir, flatUtreexoProofName)
		if err != nil {
			return nil, err
		}
		idx.proofState = *proofState

		leafDataState, err := loadFlatFileState(dataDir, flatUtreexoLeafDataName)
		if err != nil {
			return nil, err
		}
		idx.leafDataState = *leafDataState

		ttlsState, err := loadFlatFileState(dataDir, flatTTLsName)
		if err != nil {
			return nil, err
		}
		idx.ttlState = *ttlsState
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

	targetPath := flatFilePath(dataDir, flatUtreexoTargetName)
	err = deleteFlatFile(targetPath)
	if err != nil {
		return err
	}

	proofPath := flatFilePath(dataDir, flatUtreexoProofName)
	err = deleteFlatFile(proofPath)
	if err != nil {
		return err
	}

	leafDataPath := flatFilePath(dataDir, flatUtreexoLeafDataName)
	err = deleteFlatFile(leafDataPath)
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

	ttlsPath := flatFilePath(dataDir, flatTTLsName)
	err = deleteFlatFile(ttlsPath)
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
