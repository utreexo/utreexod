// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
)

const (
	// utreexoDirName is the name of the directory in which the utreexo state
	// is stored.
	utreexoDirName         = "utreexostate"
	nodesDBDirName         = "nodes"
	cachedLeavesDBDirName  = "cachedleaves"
	defaultUtreexoFileName = "forest.dat"

	// udataSerializeBool defines the argument that should be passed to the
	// serialize and deserialize functions for udata.
	udataSerializeBool = false
)

// UtreexoConfig is a descriptor which specifies the Utreexo state instance configuration.
type UtreexoConfig struct {
	// DataDir is the base path of where all the data for this node will be stored.
	// Utreexo has custom storage method and that data will be stored under this
	// directory.
	DataDir string

	// Name is what the type of utreexo proof indexer this utreexo state is related
	// to.
	Name string

	// Params are the Bitcoin network parameters. This is used to separately store
	// different accumulators.
	Params *chaincfg.Params
}

// UtreexoState is a wrapper around the raw accumulator with configuration
// information.  It contains the entire, non-pruned accumulator.
type UtreexoState struct {
	config *UtreexoConfig
	state  utreexo.Utreexo

	closeDB func() error
}

// utreexoBasePath returns the base path of where the utreexo state should be
// saved to with the with UtreexoConfig information.
func utreexoBasePath(cfg *UtreexoConfig) string {
	return filepath.Join(cfg.DataDir, utreexoDirName+"_"+cfg.Name)
}

// InitUtreexoState returns an initialized utreexo state. If there isn't an
// existing state on disk, it creates one and returns it.
func InitUtreexoState(cfg *UtreexoConfig) (*UtreexoState, error) {
	basePath := utreexoBasePath(cfg)
	log.Infof("Initializing Utreexo state from '%s'", basePath)
	defer log.Info("Utreexo state loaded")
	return initUtreexoState(cfg, basePath)
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
	path := filepath.Join(basePath, defaultUtreexoFileName)
	_, err := os.Stat(path)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
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

// FlushUtreexoState saves the utreexo state to disk.
func (idx *UtreexoProofIndex) FlushUtreexoState() error {
	basePath := utreexoBasePath(idx.utreexoState.config)
	if _, err := os.Stat(basePath); err != nil {
		os.MkdirAll(basePath, os.ModePerm)
	}
	forestFilePath := filepath.Join(basePath, defaultUtreexoFileName)
	forestFile, err := os.OpenFile(forestFilePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], idx.utreexoState.state.GetNumLeaves())
	_, err = forestFile.Write(buf[:])
	if err != nil {
		return err
	}

	return idx.utreexoState.closeDB()
}

// FlushUtreexoState saves the utreexo state to disk.
func (idx *FlatUtreexoProofIndex) FlushUtreexoState() error {
	basePath := utreexoBasePath(idx.utreexoState.config)
	if _, err := os.Stat(basePath); err != nil {
		os.MkdirAll(basePath, os.ModePerm)
	}
	forestFilePath := filepath.Join(basePath, defaultUtreexoFileName)
	forestFile, err := os.OpenFile(forestFilePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], idx.utreexoState.state.GetNumLeaves())
	_, err = forestFile.Write(buf[:])
	if err != nil {
		return err
	}

	return idx.utreexoState.closeDB()
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

// initUtreexoState creates a new utreexo state and returns it.
func initUtreexoState(cfg *UtreexoConfig, basePath string) (*UtreexoState, error) {
	p := utreexo.NewMapPollard(true)

	nodesPath := filepath.Join(basePath, nodesDBDirName)
	nodesDB, err := blockchain.InitNodesBackEnd(nodesPath)
	if err != nil {
		return nil, err
	}

	cachedLeavesPath := filepath.Join(basePath, cachedLeavesDBDirName)
	cachedLeavesDB, err := blockchain.InitCachedLeavesBackEnd(cachedLeavesPath)
	if err != nil {
		return nil, err
	}

	if checkUtreexoExists(cfg, basePath) {
		forestFilePath := filepath.Join(basePath, defaultUtreexoFileName)
		file, err := os.OpenFile(forestFilePath, os.O_RDWR, 0400)
		if err != nil {
			return nil, err
		}
		var buf [8]byte
		_, err = file.Read(buf[:])
		if err != nil {
			return nil, err
		}
		p.NumLeaves = binary.LittleEndian.Uint64(buf[:])
	}

	p.Nodes = nodesDB
	p.CachedLeaves = cachedLeavesDB
	closeDB := func() error {
		err := nodesDB.Close()
		if err != nil {
			return err
		}

		err = cachedLeavesDB.Close()
		if err != nil {
			return err
		}

		return nil
	}

	uState := &UtreexoState{
		config:  cfg,
		state:   &p,
		closeDB: closeDB,
	}

	return uState, err
}
