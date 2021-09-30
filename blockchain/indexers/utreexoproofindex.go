// Copyright (c) 2021 The utreexo developer
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"sync"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/database"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/mit-dci/utreexo/accumulator"
	"github.com/mit-dci/utreexo/util"
)

const (
	// utreexoProofIndexName is the human-readable name for the index.
	utreexoProofIndexName = "utreexo proof index"
)

var (
	// utreexoParentBucketKey is the name of the parent bucket of the
	// utreexo proof index.  It contains the buckets for the proof and
	// the undo data.
	utreexoParentBucketKey = []byte("utreexoparentindexkey")

	// utreexoProofIndexKey is the name of the utreexo proof data.  It
	// is included in the utreexoParentBucketKey and contains utreexo proof
	// data.
	utreexoProofIndexKey = []byte("utreexoproofindexkey")

	// utreexoUndoKey is the name of the utreexo undo data.  It is included
	// in the utreexoParentBucketKey and contains the utreexo undo data.
	utreexoUndoKey = []byte("utreexoundokey")
)

// Ensure the UtreexoProofIndex type implements the Indexer interface.
var _ Indexer = (*UtreexoProofIndex)(nil)

// Ensure the UtreexoProofIndex type implements the NeedsInputser interface.
var _ NeedsInputser = (*UtreexoProofIndex)(nil)

// UtreexoProofIndex implements a utreexo accumulator proof index for all the blocks.
type UtreexoProofIndex struct {
	db          database.DB
	chainParams *chaincfg.Params

	// chain is solely used to fetch the blockindex data.
	chain *blockchain.BlockChain

	// mtx protects concurrent access to utreexoView.
	mtx *sync.RWMutex

	// utreexoState represents the Bitcoin UTXO set as a utreexo accumulator.
	// It keeps all the elements of the forest in order to generate proofs.
	utreexoState *UtreexoState
}

// NeedsInputs signals that the index requires the referenced inputs in order
// to properly create the index.
//
// This implements the NeedsInputser interface.
func (idx *UtreexoProofIndex) NeedsInputs() bool {
	return true
}

// Init initializes the utreexo proof index. This is part of the Indexer
// interface.
func (idx *UtreexoProofIndex) Init() error {
	return nil // nothing to do
}

// Name returns the human-readable name of the index.
//
// This is part of the Indexer interface.
func (idx *UtreexoProofIndex) Name() string {
	return utreexoProofIndexName
}

// Key returns the database key to use for the index as a byte slice. This is
// part of the Indexer interface.
func (idx *UtreexoProofIndex) Key() []byte {
	return utreexoParentBucketKey
}

// Create is invoked when the indexer manager determines the index needs
// to be created for the first time.  It creates the bucket for the utreexo proof
// index.
//
// This is part of the Indexer interface.
func (idx *UtreexoProofIndex) Create(dbTx database.Tx) error {
	utreexoParentBucket, err := dbTx.Metadata().CreateBucket(utreexoParentBucketKey)
	if err != nil {
		return err
	}

	_, err = utreexoParentBucket.CreateBucket(utreexoProofIndexKey)
	if err != nil {
		return err
	}

	_, err = utreexoParentBucket.CreateBucket(utreexoUndoKey)
	if err != nil {
		return err
	}

	return nil
}

// ConnectBlock is invoked by the index manager when a new block has been
// connected to the main chain.
//
// This is part of the Indexer interface.
func (idx *UtreexoProofIndex) ConnectBlock(dbTx database.Tx, block *btcutil.Block,
	stxos []blockchain.SpentTxOut) error {

	// Don't include genesis blocks.
	if block.Height() == 0 {
		log.Tracef("UtreexoProofIndex.ConnectBlock: Asked to connect genesis"+
			" block (height %d) Ignoring request and skipping block",
			block.Height())
		return nil
	}

	_, outCount, inskip, outskip := util.DedupeBlock(block)
	dels, err := blockchain.BlockToDelLeaves(stxos, idx.chain, block, inskip)
	if err != nil {
		return err
	}

	adds := blockchain.BlockToAddLeaves(block, nil, outskip, outCount)

	idx.mtx.RLock()
	ud, err := wire.GenerateUData(dels, idx.utreexoState.state)
	idx.mtx.RUnlock()
	if err != nil {
		return err
	}

	err = dbStoreUtreexoProof(dbTx, block.Hash(), ud)
	if err != nil {
		return err
	}

	idx.mtx.Lock()
	undoBlock, err := idx.utreexoState.state.Modify(adds, ud.AccProof.Targets)
	idx.mtx.Unlock()
	if err != nil {
		return err
	}

	// UndoBlocks needed during reorgs.
	err = dbStoreUndoBlock(dbTx, block.Hash(), undoBlock)
	if err != nil {
		return err
	}

	return nil
}

// DisconnectBlock is invoked by the index manager when a new block has been
// disconnected to the main chain.
//
// This is part of the Indexer interface.
func (idx *UtreexoProofIndex) DisconnectBlock(dbTx database.Tx, block *btcutil.Block,
	stxos []blockchain.SpentTxOut) error {

	undoBlockBytes, err := dbFetchUndoBlockEntry(dbTx, block.Hash())
	if err != nil {
		return err
	}

	r := bytes.NewReader(undoBlockBytes)
	undoBlock := new(accumulator.UndoBlock)
	err = undoBlock.Deserialize(r)
	if err != nil {
		return err
	}

	idx.mtx.Lock()
	err = idx.utreexoState.state.Undo(*undoBlock)
	idx.mtx.Unlock()
	if err != nil {
		return err
	}

	err = dbDeleteUndoBlockEntry(dbTx, block.Hash())
	if err != nil {
		return err
	}

	err = dbDeleteUtreexoProofEntry(dbTx, block.Hash())
	if err != nil {
		return err
	}

	return nil
}

// FetchUtreexoProof returns the Utreexo proof data for the given block hash.
func (idx *UtreexoProofIndex) FetchUtreexoProof(hash *chainhash.Hash) (*wire.UData, error) {
	ud := new(wire.UData)
	err := idx.db.View(func(dbTx database.Tx) error {
		proofBytes, err := dbFetchUtreexoProofEntry(dbTx, hash)
		if err != nil {
			return err
		}
		r := bytes.NewReader(proofBytes)

		err = ud.DeserializeCompact(r, udataSerializeBool, 0)
		if err != nil {
			return err
		}

		return nil
	})

	return ud, err
}

// GenerateUData generates utreexo data for the dels passed in.  Height passed in
// should either be of block height of where the deletions are happening or just
// the lastest block height for mempool tx proof generation.
func (idx *UtreexoProofIndex) GenerateUData(dels []wire.LeafData) (*wire.UData, error) {
	idx.mtx.RLock()
	ud, err := wire.GenerateUData(dels, idx.utreexoState.state)
	idx.mtx.RUnlock()
	if err != nil {
		return nil, err
	}

	return ud, nil
}

// SetChain sets the given chain as the chain to be used for blockhash fetching.
func (idx *UtreexoProofIndex) SetChain(chain *blockchain.BlockChain) {
	idx.chain = chain
}

// NewUtreexoProofIndex returns a new instance of an indexer that is used to create a
//
// It implements the Indexer interface which plugs into the IndexManager that in
// turn is used by the blockchain package.  This allows the index to be
// seamlessly maintained along with the chain.
func NewUtreexoProofIndex(db database.DB, dataDir string, chainParams *chaincfg.Params) (*UtreexoProofIndex, error) {
	idx := &UtreexoProofIndex{
		db:          db,
		chainParams: chainParams,
		mtx:         new(sync.RWMutex),
	}

	uState, err := InitUtreexoState(&UtreexoConfig{
		DataDir: dataDir,
		Name:    db.Type(),
		// Default to ram for now.
		Type:   accumulator.RamForest,
		Params: chainParams,
	})
	if err != nil {
		return nil, err
	}
	idx.utreexoState = uState

	return idx, nil
}

// DropUtreexoProofIndex drops the address index from the provided database if it
// exists.
func DropUtreexoProofIndex(db database.DB, dataDir string, interrupt <-chan struct{}) error {
	err := dropIndex(db, utreexoParentBucketKey, utreexoProofIndexName, interrupt)
	if err != nil {
		return err
	}

	path := utreexoBasePath(&UtreexoConfig{DataDir: dataDir, Name: db.Type()})
	return deleteUtreexoState(path)
}

// Stores the utreexo proof in the database.
// TODO Use the compact serialization.
func dbStoreUtreexoProof(dbTx database.Tx, hash *chainhash.Hash, ud *wire.UData) error {
	// Pre-allocated the needed buffer.
	udSize := ud.SerializeSizeCompact(udataSerializeBool)
	buf := bytes.NewBuffer(make([]byte, 0, udSize))

	err := ud.SerializeCompact(buf, udataSerializeBool)
	if err != nil {
		return err
	}

	proofBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoProofIndexKey)
	return proofBucket.Put(hash[:], buf.Bytes())
}

// Fetches the utreexo proof in the database as a byte slice. The returned byte slice
// is serialized using the compact serialization format.
func dbFetchUtreexoProofEntry(dbTx database.Tx, hash *chainhash.Hash) ([]byte, error) {
	proofBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoProofIndexKey)
	return proofBucket.Get(hash[:]), nil
}

// Deletes the utreexo proof in the database.
func dbDeleteUtreexoProofEntry(dbTx database.Tx, hash *chainhash.Hash) error {
	idx := dbTx.Metadata().Bucket(cfIndexParentBucketKey).Bucket(utreexoProofIndexKey)
	return idx.Delete(hash[:])
}

// Stores the undo block for forest in the database.
func dbStoreUndoBlock(dbTx database.Tx, hash *chainhash.Hash, undoBlock *accumulator.UndoBlock) error {
	var buf bytes.Buffer
	err := undoBlock.Serialize(&buf)
	if err != nil {
		return err
	}

	undoBlockBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoUndoKey)
	return undoBlockBucket.Put(hash[:], buf.Bytes())
}

// Fetches the undo block for forest in the database.
func dbFetchUndoBlockEntry(dbTx database.Tx, hash *chainhash.Hash) ([]byte, error) {
	undoBlockBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoUndoKey)
	return undoBlockBucket.Get(hash[:]), nil
}

// Deletes the undo block in the database.
func dbDeleteUndoBlockEntry(dbTx database.Tx, hash *chainhash.Hash) error {
	undoBlockBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoUndoKey)
	return undoBlockBucket.Delete(hash[:])
}
