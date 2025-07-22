// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/objstorage/objstorageprovider"
	"github.com/cockroachdb/pebble/sstable"
	"github.com/cockroachdb/pebble/vfs"
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

	// flushSSTableName is the file name of the sstable that we'll use to flush the utreexo state
	// into.
	flushSSTableName = "forestFlush.dat"
)

var (
	// utreexoStateConsistencyKeyName is name of the db key used to store the consistency
	// state for the utreexo accumulator state.
	//
	// We pad with 32 bytes of 0xff so that it's always the first key in the database. This
	// is because the largest key will be the hash -> position mapping.
	utreexoStateConsistencyKeyName = []byte(
		"" +
			"\x00\x00\x00\x00\x00\x00\x00\x00" +
			"\x00\x00\x00\x00\x00\x00\x00\x00" +
			"\x00\x00\x00\x00\x00\x00\x00\x00" +
			"\x00\x00\x00\x00\x00\x00\x00\x00" +
			"utreexostateconsistency")
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
	state          *utreexo.MapPollard
	utreexoStateDB *pebble.DB

	isFlushNeeded func() bool
	flush         func(uint64, *chainhash.Hash) error
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
func dbWriteUtreexoStateConsistency(batch *pebble.Batch,
	bestHash *chainhash.Hash, roots []utreexo.Hash, numLeaves uint64) error {

	bytes := utreexoStateConsistencyToKeyValue(bestHash, roots, numLeaves)
	return batch.Set(utreexoStateConsistencyKeyName, bytes, nil)
}

// utreexoStateConsistencyToKeyValue returns the given info as a serialized value.
func utreexoStateConsistencyToKeyValue(bestHash *chainhash.Hash, roots []utreexo.Hash, numLeaves uint64) []byte {
	size := 8 + chainhash.HashSize + (chainhash.HashSize * len(roots))
	buf := make([]byte, size)

	start := 0
	binary.LittleEndian.PutUint64(buf[start:8], numLeaves)
	start += 8

	copy(buf[start:start+chainhash.HashSize], bestHash[:])
	start += chainhash.HashSize

	for _, root := range roots {
		copy(buf[start:start+chainhash.HashSize], root[:])
		start += chainhash.HashSize
	}

	return buf[:]
}

// dbFetchUtreexoStateConsistency returns the stored besthash, numleaves and roots in the database.
func dbFetchUtreexoStateConsistency(db *pebble.DB) (*chainhash.Hash, []utreexo.Hash, uint64, error) {
	buf, closer, err := db.Get(utreexoStateConsistencyKeyName)
	if err != nil && err != pebble.ErrNotFound {
		return nil, nil, 0, err
	}
	// Set error to nil as the error may have been ErrNotFound.
	err = nil
	if buf == nil {
		return nil, nil, 0, err
	}
	defer closer.Close()

	start := 0

	// Read numLeaves.
	numLeaves := binary.LittleEndian.Uint64(buf[start:8])
	start += 8

	// Read besthash.
	bestHash, err := chainhash.NewHash(buf[start : start+chainhash.HashSize])
	start += chainhash.HashSize
	if err != nil {
		return nil, nil, 0, err
	}

	// Read roots. -40 since numLeaves are 8 and besthash is 32 bytes long.
	rootCount := (len(buf) - 40) / chainhash.HashSize
	roots := make([]utreexo.Hash, rootCount)
	for i := range roots {
		roots[i] = ([32]byte)(buf[start : start+chainhash.HashSize])
		start += chainhash.HashSize
	}

	return bestHash, roots, numLeaves, nil
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
	return idx.utreexoState.flush(idx.utreexoState.state.NumLeaves, bestHash)
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
	return idx.utreexoState.flush(idx.utreexoState.state.NumLeaves, bestHash)
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
func serializeUndoBlock(proof *utreexo.Proof, delHashes []utreexo.Hash) ([]byte, error) {
	proofSize := wire.BatchProofSerializeSize(proof)
	delHashesCountSize := 4
	delHashesSize := len(delHashes) * chainhash.HashSize

	w := bytes.NewBuffer(make([]byte, 0, proofSize+delHashesCountSize+delHashesSize))

	// Write the proof.
	//
	// Proofs are prefixed with the count in uint32.
	err := wire.BatchProofSerialize(w, proof)
	if err != nil {
		return nil, err
	}

	// Write the delHashes.
	//
	// DelHashes are prefixed with the count in uint32.
	var buf [4]byte
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
func deserializeUndoBlock(serialized []byte) (*utreexo.Proof, []utreexo.Hash, error) {
	r := bytes.NewReader(serialized)

	proof, err := wire.BatchProofDeserialize(r)
	if err != nil {
		return nil, nil, err
	}

	// Read the delHashes.
	var buf [4]byte
	_, err = r.Read(buf[:])
	if err != nil {
		return nil, nil, err
	}
	hashLen := byteOrder.Uint32(buf[:])
	delHashes := make([]utreexo.Hash, hashLen)

	var hashBuf utreexo.Hash
	for i := range delHashes {
		_, err = r.Read(hashBuf[:])
		if err != nil {
			return nil, nil, err
		}

		delHashes[i] = *(*utreexo.Hash)(hashBuf[:])
	}

	return proof, delHashes, nil
}

// initConsistentUtreexoState makes the utreexo state consistent with the given tipHash.
func (us *UtreexoState) initConsistentUtreexoState(chain *blockchain.BlockChain, ttlIdx *FlatFileState,
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

	// Remove the unnecessary tmp sstable. It's ok if we error out here since getSStableWriter
	// will always remove it before creating a new one.
	path := filepath.Join(utreexoBasePath(us.config), flushSSTableName)
	os.Remove(path)

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
		dels, err := blockchain.BlockToDelLeaves(stxos, chain, block, inskip)
		if err != nil {
			return err
		}
		adds := blockchain.BlockToAddLeaves(block, outskip, outCount)

		ud, err := wire.GenerateUData(dels, us.state)
		if err != nil {
			return err
		}
		delHashes := make([]utreexo.Hash, len(ud.LeafDatas))
		for i := range delHashes {
			delHashes[i] = ud.LeafDatas[i].LeafHash()
		}

		addHashes := make([]utreexo.Leaf, 0, len(adds))
		for _, add := range adds {
			addHashes = append(addHashes, utreexo.Leaf{Hash: add.LeafHash(), Remember: false})
		}

		if us.config.Pruned {
			err := us.state.Modify(addHashes, delHashes, ud.AccProof)
			if err != nil {
				return err
			}
		} else {
			createIndexes, err := us.state.ModifyAndReturnTTLs(addHashes, delHashes, ud.AccProof)
			if err != nil {
				return err
			}

			err = writeTTLs(block.Height(), createIndexes, ud.AccProof.Targets, ud.LeafDatas, ttlIdx)
			if err != nil {
				return err
			}
		}

		if us.isFlushNeeded() {
			log.Infof("Flushing the utreexo state to disk...")
			err = us.flush(us.state.NumLeaves, block.Hash())
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// batchFlush is a helper function for flushing the given nodes backend to the batch.
func batchFlush(batch *pebble.Batch, nDB *blockchain.NodesBackEnd) error {
	return nDB.FlushBatch(batch)
}

// getSStableWriter returns a new sstable.Writer. The returned path is always the same.
func (us *UtreexoState) getSStableWriter() (*sstable.Writer, string, error) {
	path := filepath.Join(utreexoBasePath(us.config), flushSSTableName)
	flushFile, err := vfs.Default.Create(path)
	if err != nil {
		return nil, "", err
	}

	return sstable.NewWriter(objstorageprovider.NewFileWritable(flushFile), sstable.WriterOptions{
		TableFormat: us.utreexoStateDB.FormatMajorVersion().MaxTableFormat(),
	}), path, nil
}

// sstableFlush first writes a sst to a file on disk and calls db.Ingest to ingest that sst.
func (us *UtreexoState) sstableFlush(consistencyFlushValue []byte,
	nDB *blockchain.NodesBackEnd) error {

	writer, path, err := us.getSStableWriter()
	if err != nil {
		return err
	}
	writer.Set(utreexoStateConsistencyKeyName, consistencyFlushValue)

	blockchain.FlushToSstable(writer, nDB)

	err = writer.Close()
	if err != nil {
		return err
	}

	return us.utreexoStateDB.Ingest([]string{path})
}

// formatBytesToGB formats the bytes into a human readable GB format.
func formatBytesToGB(bytes uint64) string {
	const gb = 1024 * 1024 * 1024 // 1 GB in bytes
	return fmt.Sprintf("%.2f GB", float64(bytes)/float64(gb))
}

// InitUtreexoState returns an initialized utreexo state. If there isn't an
// existing state on disk, it creates one and returns it.
// maxMemoryUsage of 0 will keep every element on disk. A negaive maxMemoryUsage will
// load every element to the memory.
func InitUtreexoState(cfg *UtreexoConfig, chain *blockchain.BlockChain, ttlIdx *FlatFileState,
	tipHash *chainhash.Hash, tipHeight int32) (*UtreexoState, error) {

	log.Infof("Initializing Utreexo state from '%s'", utreexoBasePath(cfg))
	defer log.Info("Utreexo state loaded")

	p := utreexo.NewMapPollard(true)

	cache := pebble.NewCache(128 << 20) // 128MB cache
	db, err := pebble.Open(utreexoBasePath(cfg), &pebble.Options{
		Cache: cache,
	})
	cache.Unref()
	if err != nil {
		return nil, err
	}

	nodesDB, err := blockchain.InitNodesBackEnd(db, cfg.MaxMemoryUsage)
	if err != nil {
		return nil, err
	}

	savedHash, numLeaves, err := dbFetchUtreexoStateConsistency(db)
	if err != nil {
		return nil, err
	}
	p.NumLeaves = numLeaves

	p.Nodes = nodesDB

	uState := &UtreexoState{
		config:         cfg,
		state:          &p,
		utreexoStateDB: db,
	}

	flush := func(numLeaves uint64, bestHash *chainhash.Hash) error {
		nodesUsed, nodesCapacity := nodesDB.UsageStats()
		log.Debugf("Utreexo index nodesDB cache usage: %d/%d (%v%%)\n",
			nodesUsed, nodesCapacity,
			float64(nodesUsed)/float64(nodesCapacity))

		size := nodesDB.RoughSize()
		if size >= math.MaxUint32 {
			log.Debugf("flushing with sstable. size ~%v", formatBytesToGB(size))
			value := utreexoStateConsistencyToKeyValue(bestHash, numLeaves)
			return uState.sstableFlush(value, nodesDB)
		}

		log.Debugf("flushing with batch. size ~%v", formatBytesToGB(size))
		batch := uState.utreexoStateDB.NewBatch()
		err = dbWriteUtreexoStateConsistency(batch, bestHash, numLeaves)
		if err != nil {
			return err
		}

		err = batchFlush(batch, nodesDB)
		if err != nil {
			return err
		}

		return batch.Commit(nil)
	}
	isFlushNeeded := func() bool {
		return nodesDB.IsFlushNeeded()
	}

	uState.flush = flush
	uState.isFlushNeeded = isFlushNeeded

	// Make sure that the utreexo state is consistent before returning it.
	err = uState.initConsistentUtreexoState(chain, ttlIdx, savedHash, tipHash, tipHeight)
	if err != nil {
		return nil, err
	}

	return uState, err
}
