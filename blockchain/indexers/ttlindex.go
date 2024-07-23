package indexers

import (
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/wire"
)

const (
	// ttlIndexName is the human-readable name for the index.
	ttlIndexName = "time to live index"
)

var (
	// ttlIndexKey is the name of the
	ttlIndexKey = []byte("ttlindexkey")
)

// TTLIndex implements a time to live index for all the STXOs.
type TTLIndex struct {
	db          database.DB
	chainParams *chaincfg.Params
}

// Ensure the TTLIndex type implements the Indexer interface.
var _ Indexer = (*TTLIndex)(nil)

// Ensure the TTLIndex type implements the NeedsInputser interface.
var _ NeedsInputser = (*TTLIndex)(nil)

// NeedsInputs signals that the index requires the referenced inputs in order
// to properly create the index.
//
// This implements the NeedsInputser interface.
func (idx *TTLIndex) NeedsInputs() bool {
	return true
}

// Init initializes the time to live index. This is part of the Indexer
// interface.
func (idx *TTLIndex) Init(_ *blockchain.BlockChain, _ *chainhash.Hash, _ int32) error {
	return nil // Nothing to do.
}

// Name returns the human-readable name of the index.
//
// This is part of the Indexer interface.
func (idx *TTLIndex) Name() string {
	return ttlIndexName
}

// Key returns the database key to use for the index as a byte slice. This is
// part of the Indexer interface.
func (idx *TTLIndex) Key() []byte {
	return ttlIndexKey
}

// Create is invoked when the indexer manager determines the index needs
// to be created for the first time.  It creates the bucket for the ttl
// index.
//
// This is part of the Indexer interface.
func (idx *TTLIndex) Create(dbTx database.Tx) error {
	_, err := dbTx.Metadata().CreateBucket(ttlIndexKey)
	return err
}

// ConnectBlock is invoked by the index manager when a new block has been
// connected to the main chain.  This indexer stores ttl value for every
// stxo in the block.
//
// This is part of the Indexer interface.
func (idx *TTLIndex) ConnectBlock(dbTx database.Tx, block *btcutil.Block,
	stxos []blockchain.SpentTxOut) error {
	ttlIdxBucket := dbTx.Metadata().Bucket(ttlIndexKey)
	return storeTTLEntries(ttlIdxBucket, block, stxos)
}

// DisconnectBlock is invoked by the index manager when a new block has been
// disconnected to the main chain.  This indexer removes the ttl value for
// every stxo in the block.
//
// This is part of the Indexer interface.
func (idx *TTLIndex) DisconnectBlock(dbTx database.Tx, block *btcutil.Block,
	stxos []blockchain.SpentTxOut) error {
	ttlIdxBucket := dbTx.Metadata().Bucket(ttlIndexKey)
	return removeTTLEntries(ttlIdxBucket, block)
}

// PruneBlock is invoked when an older block is deleted after it's been
// processed.
// NOTE: For TTLIndex, it's a no-op as TTLIndex isn't allowed to be enabled
// with pruning. This may be visited at a later date so that TTLIndex is
// supported with pruning.
//
// This is part of the Indexer interface.
func (idx *TTLIndex) PruneBlock(_ database.Tx, _ *chainhash.Hash, _ int32) error {
	return nil
}

// For TTLIndex, flush is a no-op.
//
// This is part of the Indexer interface.
func (idx *TTLIndex) Flush(_ *chainhash.Hash, _ blockchain.FlushMode, _ bool) error {
	return nil
}

// GetTTL returns a pointer to the ttl value of a transaction outpout.
// Returns nil for a UTXO.
//
// This function is safe for concurrent access.
func (idx *TTLIndex) GetTTL(op *wire.OutPoint) *int32 {
	var ttl *int32
	idx.db.View(func(dbTx database.Tx) error {
		ttl = dbFetchTTLEntry(dbTx, op)
		return nil
	})

	return ttl
}

// -----------------------------------------------------------------------------
// Each TTL entry is stored on disk with a <key><value> of:
//
// <Outpoint><TTL>
//
// Serialized key format is:
//
// Field  Type     Size
// TXID   [32]byte 32
// index  VARINT   variable
// -----
// Total: 33-36 Bytes per value
//
//
// Serialized value format is:
//
// Field  Type     Size
// TTL    int32    4
// -----
// Total: 4 Bytes per value
//
// TXIDs are stored in little-endian format.  The index and TTL is serialized with
// the variable length integer format.
// -----------------------------------------------------------------------------

// storeTTLEntry stores a single ttl entry into the database. Refer to the above comment
// for the serialization used for the ttl entry.
func storeTTLEntry(bucket internalBucket, height int32, txIn *wire.TxIn,
	stxo *blockchain.SpentTxOut, bytes [4]byte) error {
	key := blockchain.OutpointKey(txIn.PreviousOutPoint)

	byteOrder.PutUint32(bytes[:], uint32(height-stxo.Height))
	return bucket.Put(*key, bytes[:])

	// NOTE: The key is intentionally not recycled here since the
	// database interface contract prohibits modifications.  It will
	// be garbage collected normally when the database is done with
	// it.
}

// removeTTLEntry removes a single ttl entry into the database.
func removeTTLEntry(bucket internalBucket, txIn *wire.TxIn) error {
	key := blockchain.OutpointKey(txIn.PreviousOutPoint)
	err := bucket.Delete(*key)
	blockchain.RecycleOutpointKey(key)
	if err != nil {
		return err
	}

	return nil
}

// storeTTLEntries stores all the ttls for all the STXOs for a block into the database.
// Refer to the above comment for the serialization used for the ttl entry.
func storeTTLEntries(bucket internalBucket, blk *btcutil.Block, stxos []blockchain.SpentTxOut) error {
	height := blk.Height()
	var value [4]byte

	// A separate idx for stxos.
	stxoIdx := 0
	for txIdx, tx := range blk.Transactions() {
		// Skip coinbase tx as they don't reference an input.
		if txIdx != 0 {
			for _, txIn := range tx.MsgTx().TxIn {
				err := storeTTLEntry(bucket, height, txIn,
					&(stxos[stxoIdx]), value)
				if err != nil {
					return err
				}
				stxoIdx++
			}
		}
	}

	return nil
}

// removeTTLEntries removes all the ttls for all the STXOs for a block into the database.
func removeTTLEntries(bucket internalBucket, blk *btcutil.Block) error {
	// A separate idx for stxos.
	for txIdx, tx := range blk.Transactions() {
		// Skip coinbase tx as they don't have inputs and thus are not stored.
		if txIdx != 0 {
			for _, txIn := range tx.MsgTx().TxIn {
				err := removeTTLEntry(bucket, txIn)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// dbFetchTTLEntry returns a pointer to the ttl value of a transaction output.
// Returns nil for UTXOs.
func dbFetchTTLEntry(dbTx database.Tx, op *wire.OutPoint) *int32 {
	ttlIdxBucket := dbTx.Metadata().Bucket(ttlIndexKey)

	key := blockchain.OutpointKey(*op)
	ttlBytes := ttlIdxBucket.Get(*key)
	blockchain.RecycleOutpointKey(key)
	if ttlBytes == nil {
		return nil
	}

	ttl := int32(byteOrder.Uint32(ttlBytes[:]))
	return &ttl
}

// NewTTLIndex returns a new instance of an indexer that is used to create a
// mapping of all STXOs to their time to live value.
//
// It implements the Indexer interface which plugs into the IndexManager that in
// turn is used by the blockchain package.  This allows the index to be
// seamlessly maintained along with the chain.
func NewTTLIndex(db database.DB, chainParams *chaincfg.Params) *TTLIndex {
	return &TTLIndex{
		db:          db,
		chainParams: chainParams,
	}
}

// DropTTLIndex drops the address index from the provided database if it
// exists.
func DropTTLIndex(db database.DB, interrupt <-chan struct{}) error {
	return dropIndex(db, ttlIndexKey, ttlIndexName, interrupt)
}
