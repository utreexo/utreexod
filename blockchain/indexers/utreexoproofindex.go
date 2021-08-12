// Copyright (c) 2021 The utreexo developer
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"fmt"

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
	// utreexoParentBucketKey is the name of the
	utreexoParentBucketKey = []byte("utreexoparentindexkey")

	// utreexoProofIndexKey is the name of the
	utreexoProofIndexKey = []byte("utreexoproofindexkey")

	// utreexoUndoKey is the name of the
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

	// utreexoView represents the Bitcoin UTXO set as a utreexo accumulator.
	// It keeps all the elements of the forest in order to generate proofs.
	utreexoView *accumulator.Forest
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
	_, _, inskip, outskip := util.DedupeBlock(block)
	dels, err := blockToDelLeaves(stxos, block, inskip)
	if err != nil {
		return err
	}

	adds := blockToAddLeaves(block, nil, outskip)

	ud, err := wire.GenerateUData(dels, idx.utreexoView, block.Height())
	if err != nil {
		return err
	}

	err = dbStoreUtreexoProof(dbTx, block.Hash(), ud)
	if err != nil {
		return err
	}

	undoBlock, err := idx.utreexoView.Modify(adds, ud.AccProof.Targets)
	if err != nil {
		return err
	}

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

	err = idx.utreexoView.Undo(*undoBlock)
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

// NewUtreexoProofIndex returns a new instance of an indexer that is used to create a
//
// It implements the Indexer interface which plugs into the IndexManager that in
// turn is used by the blockchain package.  This allows the index to be
// seamlessly maintained along with the chain.
func NewUtreexoProofIndex(db database.DB, chainParams *chaincfg.Params, uView *accumulator.Forest) *UtreexoProofIndex {
	return &UtreexoProofIndex{
		db:          db,
		chainParams: chainParams,
		utreexoView: uView,
	}
}

// DropUtreexoProofIndex drops the address index from the provided database if it
// exists.
func DropUtreexoProofIndex(db database.DB, dataDir string, interrupt <-chan struct{}) error {
	err := dropIndex(db, utreexoParentBucketKey, utreexoProofIndexName, interrupt)
	if err != nil {
		return err
	}

	return deleteUtreexoState(dataDir)
}

// blockToDelLeaves takes a non-utreexo block and stxos and turns the block into
// leaves that are to be deleted from the UtreexoBridgeState.
func blockToDelLeaves(stxos []blockchain.SpentTxOut, block *btcutil.Block, inskip []uint32) (
	delLeaves []wire.LeafData, err error) {
	var blockInputs int
	var blockInIdx uint32
	for idx, tx := range block.Transactions() {
		if idx == 0 {
			// coinbase can have many inputs
			blockInIdx += uint32(len(tx.MsgTx().TxIn))
			continue
		}

		for _, txIn := range tx.MsgTx().TxIn {
			blockInputs++
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

			stxo := wire.SpentTxOut(stxos[blockInIdx-1])

			var leaf = wire.LeafData{
				// TODO fetch block hash and add it to the data
				// to be commited to.
				//BlockHash: hash,
				OutPoint: &op,
				Stxo:     &stxo,
			}

			delLeaves = append(delLeaves, leaf)
			blockInIdx++
		}
	}

	// just an assertion to check the code is correct. Should never happen
	if blockInputs != len(stxos) {
		return nil, fmt.Errorf(
			"block height: %v, hash:%x, has %v txs but %v stxos",
			block.Height(), block.Hash(), len(block.Transactions()), len(stxos))
	}

	return
}

// blockToAddLeaves takes a non-utreexo block and stxos and turns the block into
// leaves that are to be added to the UtreexoBridgeState.
func blockToAddLeaves(block *btcutil.Block, remember []bool, outskip []uint32) []accumulator.Leaf {
	var leaves []accumulator.Leaf

	var txonum uint32
	for coinbase, tx := range block.Transactions() {
		for outIdx, txOut := range tx.MsgTx().TxOut {
			// Skip all the OP_RETURNs
			if isUnspendable(txOut) {
				txonum++
				continue
			}

			// Skip txos on the skip list
			if len(outskip) > 0 && outskip[0] == txonum {
				outskip = outskip[1:]
				txonum++
				continue
			}

			op := wire.OutPoint{
				Hash:  *tx.Hash(),
				Index: uint32(outIdx),
			}

			stxo := wire.SpentTxOut{
				Amount:     txOut.Value,
				PkScript:   txOut.PkScript,
				Height:     block.Height(),
				IsCoinBase: coinbase == 0,
			}

			var leaf = wire.LeafData{
				BlockHash: block.Hash(),
				OutPoint:  &op,
				Stxo:      &stxo,
			}

			uleaf := accumulator.Leaf{
				Hash: accumulator.Hash(*leaf.LeafHash()),
			}

			if len(remember) > int(txonum) {
				uleaf.Remember = remember[txonum]
			}

			leaves = append(leaves, uleaf)
			txonum++
		}
	}

	return leaves
}

// isUnspendable determines whether a tx is spendable or not.
// returns true if spendable, false if unspendable.
//
// NOTE: for utreexo, we're keeping our own isUnspendable function that has the
// same behavior as the bitcoind code. There are some utxos that btcd will mark
// unspendable that bitcoind will not and vise versa.
func isUnspendable(o *wire.TxOut) bool {
	switch {
	case len(o.PkScript) > 10000: //len 0 is OK, spendable
		return true
	case len(o.PkScript) > 0 && o.PkScript[0] == 0x6a: // OP_RETURN is 0x6a
		return true
	default:
		return false
	}
}

// Stores the utreexo proof in the database. It uses compact serialization.
func dbStoreUtreexoProof(dbTx database.Tx, hash *chainhash.Hash, ud *wire.UData) error {
	var buf bytes.Buffer
	err := ud.SerializeCompact(&buf)
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
