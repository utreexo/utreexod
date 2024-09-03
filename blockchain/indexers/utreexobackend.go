// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/wire"
)

const (
	// utreexoDirName is the name of the directory in which the utreexo state
	// is stored.
	utreexoDirName = "utreexostate"

	// oldDefaultUtreexoFileName is the file name of the utreexo state that the num leaves
	// used to be stored in.
	oldDefaultUtreexoFileName = "forest.dat"
)

var (
	// utreexoStateConsistencyKeyName is name of the db key used to store the consistency
	// state for the utreexo accumulator state.
	utreexoStateConsistencyKeyName = []byte("utreexostateconsistency")
)

// UtreexoConfig is a descriptor which specifies the Utreexo state instance configuration.
type UtreexoConfig struct {
	// MaxMemoryUsage is the desired memory usage for the utreexo state cache.
	MaxMemoryUsage int64

	// Params are the Bitcoin network parameters. This is used to separately store
	// different accumulators.
	Params *chaincfg.Params

	// If the node is a pruned node or not.
	Pruned bool

	// DataDir is the base path of where all the data for this node will be stored.
	// Utreexo has custom storage method and that data will be stored under this
	// directory.
	DataDir string

	// Name is what the type of utreexo proof indexer this utreexo state is related
	// to.
	Name string

	// FlushMainDB flushes the main database where all the data is stored.
	FlushMainDB func() error
}

// UtreexoState is a wrapper around the raw accumulator with configuration
// information.  It contains the entire, non-pruned accumulator.
type UtreexoState struct {
	config         *UtreexoConfig
	state          utreexo.Utreexo
	utreexoStateDB *leveldb.DB

	isFlushNeeded       func() bool
	flushLeavesAndNodes func(ldbTx *leveldb.Transaction) error
}

// flush flushes the utreexo state and all the data necessary for the utreexo state to be recoverable
// on sudden crashes.
func (us *UtreexoState) flush(bestHash *chainhash.Hash) error {
	ldbTx, err := us.utreexoStateDB.OpenTransaction()
	if err != nil {
		return err
	}

	// If we're keeping everything in memory, make sure to remove everything from the
	// current database before flushing as the keys deleted in memory may still exist
	// on disk.
	if us.config.MaxMemoryUsage < 0 {
		iter := us.utreexoStateDB.NewIterator(nil, nil)
		for iter.Next() {
			err := ldbTx.Delete(iter.Key(), nil)
			if err != nil {
				ldbTx.Discard()
				return err
			}
		}
	}

	// Write the best block hash and the numleaves for the utreexo state.
	err = dbWriteUtreexoStateConsistency(ldbTx, bestHash, us.state.GetNumLeaves())
	if err != nil {
		ldbTx.Discard()
		return err
	}

	err = us.flushLeavesAndNodes(ldbTx)
	if err != nil {
		ldbTx.Discard()
		return err
	}

	err = ldbTx.Commit()
	if err != nil {
		ldbTx.Discard()
		return err
	}

	return nil
}

// utreexoBasePath returns the base path of where the utreexo state should be
// saved to with the with UtreexoConfig information.
func utreexoBasePath(cfg *UtreexoConfig) string {
	return filepath.Join(cfg.DataDir, utreexoDirName+"_"+cfg.Name)
}

// deleteUtreexoState removes the utreexo state directory and all the contents
// in it.
func deleteUtreexoState(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		log.Infof("Deleting the utreexo state at directory %s", path)
	} else {
		log.Infof("No utreexo state to delete")
	}
	return os.RemoveAll(path)
}

// checkUtreexoExists checks that the data for this utreexo state type specified
// in the config is present and should be resumed off of.
func checkUtreexoExists(cfg *UtreexoConfig, basePath string) bool {
	path := filepath.Join(basePath, oldDefaultUtreexoFileName)
	_, err := os.Stat(path)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}

// dbWriteUtreexoStateConsistency writes the consistency state to the database using the given transaction.
func dbWriteUtreexoStateConsistency(ldbTx *leveldb.Transaction, bestHash *chainhash.Hash, numLeaves uint64) error {
	// Create the byte slice to be written.
	var buf [8 + chainhash.HashSize]byte
	binary.LittleEndian.PutUint64(buf[:8], numLeaves)
	copy(buf[8:], bestHash[:])

	return ldbTx.Put(utreexoStateConsistencyKeyName, buf[:], nil)
}

// dbFetchUtreexoStateConsistency returns the stored besthash and the numleaves in the database.
func dbFetchUtreexoStateConsistency(db *leveldb.DB) (*chainhash.Hash, uint64, error) {
	buf, err := db.Get(utreexoStateConsistencyKeyName, nil)
	if err != nil && err != leveldb.ErrNotFound {
		return nil, 0, err
	}
	// Set error to nil as the error may have been ErrNotFound.
	err = nil
	if buf == nil {
		return nil, 0, nil
	}

	bestHash, err := chainhash.NewHash(buf[8:])
	if err != nil {
		return nil, 0, err
	}

	return bestHash, binary.LittleEndian.Uint64(buf[:8]), nil
}

// FetchCurrentUtreexoState returns the current utreexo state.
func (idx *UtreexoProofIndex) FetchCurrentUtreexoState() ([]*chainhash.Hash, uint64) {
	idx.mtx.RLock()
	defer idx.mtx.RUnlock()

	roots := idx.utreexoState.state.GetRoots()
	chainhashRoots := make([]*chainhash.Hash, len(roots))

	for i, root := range roots {
		newRoot := chainhash.Hash(root)
		chainhashRoots[i] = &newRoot
	}

	return chainhashRoots, idx.utreexoState.state.GetNumLeaves()
}

// FetchCurrentUtreexoState returns the current utreexo state.
func (idx *FlatUtreexoProofIndex) FetchCurrentUtreexoState() ([]*chainhash.Hash, uint64) {
	idx.mtx.RLock()
	defer idx.mtx.RUnlock()

	roots := idx.utreexoState.state.GetRoots()

	chainhashRoots := make([]*chainhash.Hash, len(roots))
	for i, root := range roots {
		newRoot := chainhash.Hash(root)
		chainhashRoots[i] = &newRoot
	}

	return chainhashRoots, idx.utreexoState.state.GetNumLeaves()
}

// FetchUtreexoState returns the utreexo state at the desired block.
func (idx *UtreexoProofIndex) FetchUtreexoState(dbTx database.Tx, blockHash *chainhash.Hash) ([]*chainhash.Hash, uint64, error) {
	stump, err := dbFetchUtreexoState(dbTx, blockHash)
	if err != nil {
		return nil, 0, err
	}

	chainhashRoots := make([]*chainhash.Hash, len(stump.Roots))
	for i, root := range stump.Roots {
		newRoot := chainhash.Hash(root)
		chainhashRoots[i] = &newRoot
	}
	return chainhashRoots, stump.NumLeaves, nil
}

// FetchUtreexoState returns the utreexo state at the desired block.
func (idx *FlatUtreexoProofIndex) FetchUtreexoState(blockHeight int32) ([]*chainhash.Hash, uint64, error) {
	stump, err := idx.fetchRoots(blockHeight)
	if err != nil {
		return nil, 0, err
	}

	chainhashRoots := make([]*chainhash.Hash, len(stump.Roots))
	for i, root := range stump.Roots {
		newRoot := chainhash.Hash(root)
		chainhashRoots[i] = &newRoot
	}
	return chainhashRoots, stump.NumLeaves, nil
}

// Flush flushes the utreexo state. The different modes pass in as an argument determine if the utreexo state
// will be flushed or not.
//
// The onConnect bool is if the Flush is called on a block connect or a disconnect.
// It's important as it determines if we flush the main node db before attempting to flush the utreexo state.
// For the utreexo state to be recoverable, it has to be behind whatever tip the main database is at.
// On block connects, we always want to flush first but on disconnects, we want to flush first before the
// data necessary undo data is removed.
func (idx *UtreexoProofIndex) Flush(bestHash *chainhash.Hash, mode blockchain.FlushMode, onConnect bool) error {
	switch mode {
	case blockchain.FlushPeriodic:
		// If the time since the last flush less then the interval, just return.
		if time.Since(idx.lastFlushTime) < blockchain.UtxoFlushPeriodicInterval {
			return nil
		}
	case blockchain.FlushIfNeeded:
		if !idx.utreexoState.isFlushNeeded() {
			return nil
		}
	case blockchain.FlushRequired:
		// Purposely left empty.
	}

	if onConnect {
		// Flush the main database first. This is because the block and other data may still
		// be in the database cache. If we flush the utreexo state before, there's no way to
		// undo the utreexo state to the last block where the main database flushed. Flushing
		// this before we flush the utreexo state ensures that we leave the database state at
		// a recoverable state.
		//
		// This is different from on disconnect as you want the utreexo state to be flushed
		// first as the utreexo state can always catch up to the main db tip but can't undo
		// without the main database data.
		err := idx.config.FlushMainDB()
		if err != nil {
			return err
		}
	}
	err := idx.flushUtreexoState(bestHash)
	if err != nil {
		return err
	}

	// Set the last flush time as now as the flush was successful.
	idx.lastFlushTime = time.Now()
	return nil
}

// FlushUtreexoState saves the utreexo state to disk.
func (idx *UtreexoProofIndex) flushUtreexoState(bestHash *chainhash.Hash) error {
	idx.mtx.Lock()
	defer idx.mtx.Unlock()

	log.Infof("Flushing the utreexo state to disk...")
	return idx.utreexoState.flush(bestHash)
}

// CloseUtreexoState flushes and closes the utreexo database state.
func (idx *UtreexoProofIndex) CloseUtreexoState() error {
	bestHash := idx.chain.BestSnapshot().Hash
	err := idx.flushUtreexoState(&bestHash)
	if err != nil {
		log.Warnf("error whiling flushing the utreexo state. %v", err)
	}
	return idx.utreexoState.utreexoStateDB.Close()
}

// Flush flushes the utreexo state. The different modes pass in as an argument determine if the utreexo state
// will be flushed or not.
//
// The onConnect bool is if the Flush is called on a block connect or a disconnect.
// It's important as it determines if we flush the main node db before attempting to flush the utreexo state.
// For the utreexo state to be recoverable, it has to be behind whatever tip the main database is at.
// On block connects, we always want to flush first but on disconnects, we want to flush first before the
// data necessary undo data is removed.
func (idx *FlatUtreexoProofIndex) Flush(bestHash *chainhash.Hash, mode blockchain.FlushMode, onConnect bool) error {
	switch mode {
	case blockchain.FlushPeriodic:
		// If the time since the last flush less then the interval, just return.
		if time.Since(idx.lastFlushTime) < blockchain.UtxoFlushPeriodicInterval {
			return nil
		}
	case blockchain.FlushIfNeeded:
		if !idx.utreexoState.isFlushNeeded() {
			return nil
		}
	case blockchain.FlushRequired:
		// Purposely left empty.
	}

	if onConnect {
		// Flush the main database first. This is because the block and other data may still
		// be in the database cache. If we flush the utreexo state before, there's no way to
		// undo the utreexo state to the last block where the main database flushed. Flushing
		// this before we flush the utreexo state ensures that we leave the database state at
		// a recoverable state.
		//
		// This is different from on disconnect as you want the utreexo state to be flushed
		// first as the utreexo state can always catch up to the main db tip but can't undo
		// without the main database data.
		err := idx.config.FlushMainDB()
		if err != nil {
			return err
		}
	}
	err := idx.flushUtreexoState(bestHash)
	if err != nil {
		return err
	}

	// Set the last flush time as now as the flush was successful.
	idx.lastFlushTime = time.Now()
	return nil
}

// FlushUtreexoState saves the utreexo state to disk.
func (idx *FlatUtreexoProofIndex) flushUtreexoState(bestHash *chainhash.Hash) error {
	idx.mtx.Lock()
	defer idx.mtx.Unlock()

	log.Infof("Flushing the utreexo state to disk...")
	return idx.utreexoState.flush(bestHash)
}

// CloseUtreexoState flushes and closes the utreexo database state.
func (idx *FlatUtreexoProofIndex) CloseUtreexoState() error {
	bestHash := idx.chain.BestSnapshot().Hash
	err := idx.flushUtreexoState(&bestHash)
	if err != nil {
		log.Warnf("error whiling flushing the utreexo state. %v", err)
	}
	return idx.utreexoState.utreexoStateDB.Close()
}

// serializeUndoBlock serializes all the data that's needed for undoing a full utreexo state
// into a slice of bytes.
func serializeUndoBlock(numAdds uint64, targets []uint64, delHashes []utreexo.Hash) ([]byte, error) {
	numAddsSize := 8
	targetCountSize := 4
	targetsSize := len(targets) * 8
	delHashesCountSize := 4
	delHashesSize := len(delHashes) * chainhash.HashSize

	w := bytes.NewBuffer(make([]byte, 0, numAddsSize+targetCountSize+targetsSize+delHashesCountSize+delHashesSize))

	// Write numAdds.
	buf := make([]byte, numAddsSize)
	byteOrder.PutUint64(buf[:], numAdds)
	_, err := w.Write(buf[:])
	if err != nil {
		return nil, err
	}

	// Write the targets.
	//
	// Targets are prefixed with the count in uint32.
	buf = buf[:targetCountSize]
	byteOrder.PutUint32(buf[:], uint32(len(targets)))
	_, err = w.Write(buf[:])
	if err != nil {
		return nil, err
	}
	buf = buf[:8]
	for _, targ := range targets {
		byteOrder.PutUint64(buf[:], targ)

		_, err = w.Write(buf[:])
		if err != nil {
			return nil, err
		}
	}

	// Write the delHashes.
	//
	// DelHashes are prefixed with the count in uint32.
	buf = buf[:delHashesCountSize]
	byteOrder.PutUint32(buf[:], uint32(len(delHashes)))
	_, err = w.Write(buf[:])
	if err != nil {
		return nil, err
	}
	for _, hash := range delHashes {
		_, err = w.Write(hash[:])
		if err != nil {
			return nil, err
		}
	}

	return w.Bytes(), nil
}

// deserializeUndoBlock deserializes all the data that's needed to undo a full utreexo
// state from a slice of serialized bytes.
func deserializeUndoBlock(serialized []byte) (uint64, []uint64, []utreexo.Hash, error) {
	r := bytes.NewReader(serialized)

	// Read the numAdds.
	buf := make([]byte, chainhash.HashSize)
	buf = buf[:8]
	_, err := r.Read(buf)
	if err != nil {
		return 0, nil, nil, err
	}

	numAdds := byteOrder.Uint64(buf)

	// Read the targets.
	buf = buf[:4]
	_, err = r.Read(buf)
	if err != nil {
		return 0, nil, nil, err
	}

	targLen := byteOrder.Uint32(buf)
	targets := make([]uint64, targLen)

	buf = buf[:8]
	for i := range targets {
		_, err = r.Read(buf)
		if err != nil {
			return 0, nil, nil, err
		}

		targets[i] = byteOrder.Uint64(buf)
	}

	// Read the delHashes.
	buf = buf[:4]
	_, err = r.Read(buf)
	if err != nil {
		return 0, nil, nil, err
	}
	hashLen := byteOrder.Uint32(buf)
	delHashes := make([]utreexo.Hash, hashLen)

	buf = buf[:chainhash.HashSize]
	for i := range delHashes {
		_, err = r.Read(buf)
		if err != nil {
			return 0, nil, nil, err
		}

		delHashes[i] = *(*utreexo.Hash)(buf)
	}

	return numAdds, targets, delHashes, nil
}

// upgradeUtreexoState upgrades the utreexo state to be atomic.
func upgradeUtreexoState(cfg *UtreexoConfig, p *utreexo.MapPollard,
	db *leveldb.DB, bestHash *chainhash.Hash) error {

	// Check if the current database is an older database that needs to be upgraded.
	if !checkUtreexoExists(cfg, utreexoBasePath(cfg)) {
		return nil
	}

	log.Infof("Upgrading the utreexo state database. Do NOT shut down this process. " +
		"This may take a while...")

	// Write the nodes to the new database.
	nodesPath := filepath.Join(utreexoBasePath(cfg), "nodes")
	nodesDB, err := leveldb.OpenFile(nodesPath, nil)
	if err != nil {
		return err
	}

	ldbTx, err := db.OpenTransaction()
	if err != nil {
		return err
	}

	iter := nodesDB.NewIterator(nil, nil)
	for iter.Next() {
		err = ldbTx.Put(iter.Key(), iter.Value(), nil)
		if err != nil {
			ldbTx.Discard()
			return err
		}
	}
	nodesDB.Close()

	// Write the cached leaves to the new database.
	cachedLeavesPath := filepath.Join(utreexoBasePath(cfg), "cachedleaves")
	cachedLeavesDB, err := leveldb.OpenFile(cachedLeavesPath, nil)
	if err != nil {
		return err
	}

	iter = cachedLeavesDB.NewIterator(nil, nil)
	for iter.Next() {
		err = ldbTx.Put(iter.Key(), iter.Value(), nil)
		if err != nil {
			ldbTx.Discard()
			return err
		}
	}
	cachedLeavesDB.Close()

	// Open the file and read the numLeaves.
	forestFilePath := filepath.Join(utreexoBasePath(cfg), oldDefaultUtreexoFileName)
	file, err := os.OpenFile(forestFilePath, os.O_RDWR, 0400)
	if err != nil {
		return err
	}
	var buf [8]byte
	_, err = file.Read(buf[:])
	if err != nil {
		return err
	}

	// Save the consistency state
	p.NumLeaves = binary.LittleEndian.Uint64(buf[:8])
	err = dbWriteUtreexoStateConsistency(ldbTx, bestHash, p.NumLeaves)
	if err != nil {
		ldbTx.Discard()
		return err
	}

	// Commit all the writes to the database.
	err = ldbTx.Commit()
	if err != nil {
		ldbTx.Discard()
		return err
	}

	// Remove the unnecessary file after the upgrade.
	err = os.Remove(forestFilePath)
	if err != nil {
		return err
	}
	err = os.RemoveAll(cachedLeavesPath)
	if err != nil {
		return err
	}
	err = os.RemoveAll(nodesPath)
	if err != nil {
		return err
	}

	log.Infof("Finished upgrading the utreexo state database.")
	return nil
}

// initConsistentUtreexoState makes the utreexo state consistent with the given tipHash.
func (us *UtreexoState) initConsistentUtreexoState(chain *blockchain.BlockChain,
	savedHash, tipHash *chainhash.Hash, tipHeight int32) error {

	// This is a new accumulator state that we're working with.
	var empty chainhash.Hash
	if tipHeight == -1 && tipHash.IsEqual(&empty) {
		return nil
	}

	// We're all caught up if both of the hashes are equal.
	if savedHash != nil && savedHash.IsEqual(tipHash) {
		return nil
	}

	currentHeight := int32(-1)
	if savedHash != nil {
		// Even though this should always be true, make sure the fetched hash is in
		// the best chain.
		if !chain.MainChainHasBlock(savedHash) {
			return fmt.Errorf("last utreexo consistency status contains "+
				"hash that is not in best chain: %v", savedHash)
		}

		var err error
		currentHeight, err = chain.BlockHeightByHash(savedHash)
		if err != nil {
			return err
		}

		if currentHeight > tipHeight {
			return fmt.Errorf("Saved besthash has a heigher height "+
				"of %v than tip height of %v. The utreexo state is NOT "+
				"recoverable and should be dropped and reindexed",
				currentHeight, tipHeight)
		}
	} else {
		// Mark it as an empty hash for logging below.
		savedHash = new(chainhash.Hash)
	}

	log.Infof("Reconstructing the Utreexo state after an unclean shutdown. The Utreexo state is "+
		"consistent at block %s (%d) but the index tip is at block %s (%d),  This may "+
		"take a long time...", savedHash.String(), currentHeight, tipHash.String(), tipHeight)

	for h := currentHeight + 1; h <= tipHeight; h++ {
		block, err := chain.BlockByHeight(h)
		if err != nil {
			return err
		}

		stxos, err := chain.FetchSpendJournal(block)
		if err != nil {
			return err
		}

		_, outCount, inskip, outskip := blockchain.DedupeBlock(block)
		dels, _, err := blockchain.BlockToDelLeaves(stxos, chain, block, inskip, -1)
		if err != nil {
			return err
		}
		adds := blockchain.BlockToAddLeaves(block, outskip, nil, outCount)

		ud, err := wire.GenerateUData(dels, us.state)
		if err != nil {
			return err
		}
		delHashes := make([]utreexo.Hash, len(ud.LeafDatas))
		for i := range delHashes {
			delHashes[i] = ud.LeafDatas[i].LeafHash()
		}

		err = us.state.Modify(adds, delHashes, ud.AccProof)
		if err != nil {
			return err
		}

		if us.isFlushNeeded() {
			log.Infof("Flushing the utreexo state to disk...")
			err = us.flush(block.Hash())
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// InitUtreexoState returns an initialized utreexo state. If there isn't an
// existing state on disk, it creates one and returns it.
// maxMemoryUsage of 0 will keep every element on disk. A negaive maxMemoryUsage will
// load every element to the memory.
func InitUtreexoState(cfg *UtreexoConfig, chain *blockchain.BlockChain,
	tipHash *chainhash.Hash, tipHeight int32) (*UtreexoState, error) {

	log.Infof("Initializing Utreexo state from '%s'", utreexoBasePath(cfg))
	defer log.Info("Utreexo state loaded")

	p := utreexo.NewMapPollard(true)

	maxNodesMem := cfg.MaxMemoryUsage * 7 / 10
	maxCachedLeavesMem := cfg.MaxMemoryUsage - maxNodesMem

	db, err := leveldb.OpenFile(utreexoBasePath(cfg), nil)
	if err != nil {
		return nil, err
	}

	nodesDB, err := blockchain.InitNodesBackEnd(db, maxNodesMem)
	if err != nil {
		return nil, err
	}

	cachedLeavesDB, err := blockchain.InitCachedLeavesBackEnd(db, maxCachedLeavesMem)
	if err != nil {
		return nil, err
	}

	// The utreexo state may be an older version where the numLeaves were stored in a flat
	// file. Upgrade the utreexo state if it needs to be.
	err = upgradeUtreexoState(cfg, &p, db, tipHash)
	if err != nil {
		return nil, err
	}

	savedHash, numLeaves, err := dbFetchUtreexoStateConsistency(db)
	if err != nil {
		return nil, err
	}
	p.NumLeaves = numLeaves

	var flush func(ldbTx *leveldb.Transaction) error
	var isFlushNeeded func() bool
	if cfg.MaxMemoryUsage >= 0 {
		p.Nodes = nodesDB
		p.CachedLeaves = cachedLeavesDB
		flush = func(ldbTx *leveldb.Transaction) error {
			nodesUsed, nodesCapacity := nodesDB.UsageStats()
			log.Debugf("Utreexo index nodesDB cache usage: %d/%d (%v%%)\n",
				nodesUsed, nodesCapacity,
				float64(nodesUsed)/float64(nodesCapacity))

			cachedLeavesUsed, cachedLeavesCapacity := cachedLeavesDB.UsageStats()
			log.Debugf("Utreexo index cachedLeavesDB cache usage: %d/%d (%v%%)\n",
				cachedLeavesUsed, cachedLeavesCapacity,
				float64(cachedLeavesUsed)/float64(cachedLeavesCapacity))

			err = nodesDB.Flush(ldbTx)
			if err != nil {
				return err
			}
			err = cachedLeavesDB.Flush(ldbTx)
			if err != nil {
				return err
			}

			return nil
		}
		isFlushNeeded = func() bool {
			nodesNeedsFlush := nodesDB.IsFlushNeeded()
			leavesNeedsFlush := cachedLeavesDB.IsFlushNeeded()
			return nodesNeedsFlush || leavesNeedsFlush
		}
	} else {
		log.Infof("loading the utreexo state from disk...")
		err = nodesDB.ForEach(func(k uint64, v utreexo.Leaf) error {
			p.Nodes.Put(k, v)
			return nil
		})
		if err != nil {
			return nil, err
		}

		err = cachedLeavesDB.ForEach(func(k utreexo.Hash, v uint64) error {
			p.CachedLeaves.Put(k, v)
			return nil
		})
		if err != nil {
			return nil, err
		}

		log.Infof("Finished loading the utreexo state from disk.")

		flush = func(ldbTx *leveldb.Transaction) error {
			err = p.Nodes.ForEach(func(k uint64, v utreexo.Leaf) error {
				return blockchain.NodesBackendPut(ldbTx, k, v)
			})
			if err != nil {
				return err
			}

			err = p.CachedLeaves.ForEach(func(k utreexo.Hash, v uint64) error {
				return blockchain.CachedLeavesBackendPut(ldbTx, k, v)
			})
			if err != nil {
				return err
			}

			return nil
		}

		// Flush is never needed since we're keeping everything in memory.
		isFlushNeeded = func() bool {
			return false
		}
	}

	uState := &UtreexoState{
		config:              cfg,
		state:               &p,
		utreexoStateDB:      db,
		isFlushNeeded:       isFlushNeeded,
		flushLeavesAndNodes: flush,
	}

	// Make sure that the utreexo state is consistent before returning it.
	err = uState.initConsistentUtreexoState(chain, savedHash, tipHash, tipHeight)
	if err != nil {
		return nil, err
	}

	return uState, err
}
