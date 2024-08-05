package indexers

import (
	"errors"

	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/btcutil/gcs/builder"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/wire"
)

// utreexoCFIndexName is the human-readable name for the index.
const (
	utreexoCFIndexName = "utreexo custom commited filter index"
)

// utreexocfilter is a custom commited filter which serves utreexo roots
// these roots are already present, so they need not be created/stored, their
// headers could be stored though
var (
	// utreexoCFIndexParentBucketKey is the name of the parent bucket used to
	// house the index. The rest of the buckets live below this bucket.
	utreexoCFIndexParentBucketKey = []byte("utreexocfindexparentbucket")

	// utreexoCfHeaderKeys is an array of db bucket names used to house indexes of
	// block hashes to cf headers.
	utreexoCfHeaderKeys = [][]byte{
		[]byte("utreexocfheaderbyhashidx"),
	}
)

// dbFetchFilterIdxEntry retrieves a data blob from the filter index database.
// An entry's absence is not considered an error.
// Right now, the value of 'key' will always be the key to the utreexocfheader key
// as we don't need to fetch filters or filter hashes from the main bucket, as those
// are not stored
func dbFetchUtreexoCFilterIdxEntry(dbTx database.Tx, key []byte, h *chainhash.Hash) ([]byte, error) {
	idx := dbTx.Metadata().Bucket(utreexoCFIndexParentBucketKey).Bucket(key)
	return idx.Get(h[:]), nil
}

// dbStoreFilterIdxEntry stores a data blob in the filter index database.
func dbStoreUtreexoCFilterIdxEntry(dbTx database.Tx, key []byte, h *chainhash.Hash, f []byte) error {
	idx := dbTx.Metadata().Bucket(utreexoCFIndexParentBucketKey).Bucket(key)
	return idx.Put(h[:], f)
}

// dbDeleteFilterIdxEntry deletes a data blob from the filter index database.
func dbDeleteUtreexoCFilterIdxEntry(dbTx database.Tx, key []byte, h *chainhash.Hash) error {
	idx := dbTx.Metadata().Bucket(utreexoCFIndexParentBucketKey).Bucket(key)
	return idx.Delete(h[:])
}

var _ Indexer = (*UtreexoCFIndex)(nil)

var _ NeedsInputser = (*UtreexoCFIndex)(nil)

type UtreexoCFIndex struct {
	db          database.DB
	chainParams *chaincfg.Params

	chain *blockchain.BlockChain

	utreexoProofIndex *UtreexoProofIndex

	flatUtreexoProofIndex *FlatUtreexoProofIndex
}

func (idx *UtreexoCFIndex) NeedsInputs() bool {
	return true
}

// Init initializes the utreexo cf index. This is part of the Indexer
// interface.
func (idx *UtreexoCFIndex) Init(_ *blockchain.BlockChain) error {
	return nil // Nothing to do.
}

// Key returns the database key to use for the index as a byte slice. This is
// part of the Indexer interface.
func (idx *UtreexoCFIndex) Key() []byte {
	return utreexoCFIndexParentBucketKey
}

// Name returns the human-readable name of the index. This is part of the
// Indexer interface.
func (idx *UtreexoCFIndex) Name() string {
	return utreexoCFIndexName
}

// Create is invoked when the index manager determines the index needs to
// be created for the first time. It creates buckets for the custom utreexo
// filter index.
func (idx *UtreexoCFIndex) Create(dbTx database.Tx) error {
	meta := dbTx.Metadata()

	utreexoCfIndexParentBucket, err := meta.CreateBucket(utreexoCFIndexParentBucketKey)
	if err != nil {
		return err
	}

	for _, bucketName := range utreexoCfHeaderKeys {
		_, err = utreexoCfIndexParentBucket.CreateBucket(bucketName)
		if err != nil {
			return err
		}
	}

	return nil
}

// storeUtreexoCFilter stores a given utreexocfilter header
func storeUtreexoCFHeader(dbTx database.Tx, block *btcutil.Block, filterData []byte,
	filterType wire.FilterType) error {
	if filterType != wire.UtreexoCFilter {
		return errors.New("invalid filter type")
	}

	// Figure out which header bucket to use.
	hkey := utreexoCfHeaderKeys[0]
	h := block.Hash()

	// fetch the previous block's filter header.
	var prevHeader *chainhash.Hash
	ph := &block.MsgBlock().Header.PrevBlock
	if ph.IsEqual(&zeroHash) {
		prevHeader = &zeroHash
	} else {
		pfh, err := dbFetchUtreexoCFilterIdxEntry(dbTx, hkey, ph)
		if err != nil {
			return err
		}

		// Construct the new block's filter header, and store it.
		prevHeader, err = chainhash.NewHash(pfh)
		if err != nil {
			return err
		}
	}

	fh, err := builder.MakeHeaderForUtreexoCFilter(filterData, *prevHeader)
	if err != nil {
		return err
	}
	return dbStoreUtreexoCFilterIdxEntry(dbTx, hkey, h, fh[:])
}

// ConnectBlock is invoked by the index manager when a new block has been
// connected to the main chain.
// This is part of the Indexer interface.
func (idx *UtreexoCFIndex) ConnectBlock(dbTx database.Tx, block *btcutil.Block,
	stxos []blockchain.SpentTxOut) error {

	blockHash := block.Hash()
	roots, leaves, err := idx.fetchUtreexoRoots(dbTx, blockHash)

	if err != nil {
		return err
	}

	// serialize the hashes of the utreexo roots hash
	serializedUtreexo, err := blockchain.SerializeUtreexoRootsHash(leaves, roots)
	if err != nil {
		return err
	}

	return storeUtreexoCFHeader(dbTx, block, serializedUtreexo, wire.UtreexoCFilter)
}

// fetches the utreexo roots for a given block hash
func (idx *UtreexoCFIndex) fetchUtreexoRoots(dbTx database.Tx,
	blockHash *chainhash.Hash) ([]*chainhash.Hash, uint64, error) {

	var leaves uint64
	var roots []*chainhash.Hash

	// For compact state nodes
	if idx.chain.IsUtreexoViewActive() {
		viewPoint, err := idx.chain.FetchUtreexoViewpoint(blockHash)
		if err != nil {
			return nil, 0, err
		}
		roots = viewPoint.GetRoots()
		leaves = viewPoint.NumLeaves()
	}
	// for bridge nodes
	if idx.utreexoProofIndex != nil {
		roots, leaves, err := idx.utreexoProofIndex.FetchUtreexoState(dbTx, blockHash)
		if err != nil {
			return nil, 0, err
		}
		return roots, leaves, nil
	} else if idx.flatUtreexoProofIndex != nil {
		height, err := idx.chain.BlockHeightByHash(blockHash)
		if err != nil {
			return nil, 0, err
		}
		roots, leaves, err := idx.flatUtreexoProofIndex.FetchUtreexoState(height)
		if err != nil {
			return nil, 0, err
		}
		return roots, leaves, nil
	}

	return roots, leaves, nil
}

// DisconnectBlock is invoked by the index manager when a block has been
// disconnected from the main chain.  This indexer removes the hash-to-cf
// mapping for every passed block. This is part of the Indexer interface.
func (idx *UtreexoCFIndex) DisconnectBlock(dbTx database.Tx, block *btcutil.Block,
	_ []blockchain.SpentTxOut) error {

	for _, key := range utreexoCfHeaderKeys {
		err := dbDeleteUtreexoCFilterIdxEntry(dbTx, key, block.Hash())
		if err != nil {
			return err
		}
	}

	return nil
}

// PruneBlock is invoked when an older block is deleted after it's been
// processed.
// TODO (kcalvinalvin): Consider keeping the filters at a later date to help with
// reindexing as a pruned node.
//
// This is part of the Indexer interface.
func (idx *UtreexoCFIndex) PruneBlock(dbTx database.Tx, blockHash *chainhash.Hash) error {

	for _, key := range utreexoCfHeaderKeys {
		err := dbDeleteUtreexoCFilterIdxEntry(dbTx, key, blockHash)
		if err != nil {
			return err
		}
	}

	return nil
}

// entryByBlockHash fetches a filter index entry of a particular type
// (eg. filter, filter header, etc) for a filter type and block hash.
func (idx *UtreexoCFIndex) entryByBlockHash(filterTypeKeys [][]byte,
	filterType wire.FilterType, h *chainhash.Hash) ([]byte, error) {

	if uint8(filterType) != uint8(wire.UtreexoCFilter) {
		return nil, errors.New("unsupported filter type")
	}

	// if the filtertype keys is empty, then we are fetching the filter data itself
	// if not, we are fetching the filter header
	if len(filterTypeKeys) == 0 {
		var serializedUtreexo []byte
		err := idx.db.View(func(dbTx database.Tx) error {
			var err error
			roots, leaves, err := idx.fetchUtreexoRoots(dbTx, h)

			if err != nil {
				return err
			}
			// serialize the hashes of the utreexo roots hash
			serializedUtreexo, err = blockchain.SerializeUtreexoRootsHash(leaves, roots)
			return err
		})

		// serialize the hashes of the utreexo roots hash
		// serializedUtreexo, err := blockchain.SerializeUtreexoRootsHash(leaves, roots)
		if err != nil {
			return nil, err
		}

		return serializedUtreexo, err
	} else {
		// using filtertypekeys[0] as there is only one entry in the filterTypeKeys (utreexoCfHeaderKeys)
		key := filterTypeKeys[0]

		var entry []byte
		err := idx.db.View(func(dbTx database.Tx) error {
			var err error
			entry, err = dbFetchUtreexoCFilterIdxEntry(dbTx, key, h)
			return err
		})
		return entry, err
	}
}

// entriesByBlockHashes batch fetches a filter index entry of a particular type
// (eg. filter, filter header, etc) for a filter type and slice of block hashes.
func (idx *UtreexoCFIndex) entriesByBlockHashes(filterTypeKeys [][]byte,
	filterType wire.FilterType, blockHashes []*chainhash.Hash) ([][]byte, error) {

	if uint8(filterType) != uint8(wire.UtreexoCFilter) {
		return nil, errors.New("unsupported filter type")
	}

	// we use filterTypeKeys[0] as the key for the utreexo cfilter since for now,
	// there is only one type of utreexo cfilter and the filterTypeKeys always
	// returns the filterheaderkeys, which has just the one value
	key := filterTypeKeys[0]

	entries := make([][]byte, 0, len(blockHashes))
	err := idx.db.View(func(dbTx database.Tx) error {
		for _, blockHash := range blockHashes {
			entry, err := dbFetchUtreexoCFilterIdxEntry(dbTx, key, blockHash)
			if err != nil {
				return err
			}
			entries = append(entries, entry)
		}
		return nil
	})
	return entries, err
}

// FilterByBlockHash returns the serialized contents of a block's utreexo
// cfilter.
func (idx *UtreexoCFIndex) FilterByBlockHash(h *chainhash.Hash,
	filterType wire.FilterType) ([]byte, error) {
	// we create an ampty variable of type [][]byte to pass to the entryByBlockHash
	// in order to fetch the filter data itself
	utreexoCfIndexKeys := [][]byte{}
	return idx.entryByBlockHash(utreexoCfIndexKeys, filterType, h)
}

// FilterHeaderByBlockHash returns the serialized contents of a block's utreexo
// committed filter header.
func (idx *UtreexoCFIndex) FilterHeaderByBlockHash(h *chainhash.Hash,
	filterType wire.FilterType) ([]byte, error) {
	return idx.entryByBlockHash(utreexoCfHeaderKeys, filterType, h)
}

// FilterHeadersByBlockHashes returns the serialized contents of a block's
// utreexo commited filter header for a set of blocks by hash.
func (idx *UtreexoCFIndex) FilterHeadersByBlockHashes(blockHashes []*chainhash.Hash,
	filterType wire.FilterType) ([][]byte, error) {
	return idx.entriesByBlockHashes(utreexoCfHeaderKeys, filterType, blockHashes)
}

// NewCfIndex returns a new instance of an indexer that is used to create a
// mapping of the hashes of all blocks in the blockchain to their respective
// committed filters.
//
// It implements the Indexer interface which plugs into the IndexManager that
// in turn is used by the blockchain package. This allows the index to be
// seamlessly maintained along with the chain.
func NewUtreexoCfIndex(db database.DB, chainParams *chaincfg.Params, utreexoProofIndex *UtreexoProofIndex,
	flatUtreexoProofIndex *FlatUtreexoProofIndex) *UtreexoCFIndex {
	return &UtreexoCFIndex{db: db, chainParams: chainParams, utreexoProofIndex: utreexoProofIndex,
		flatUtreexoProofIndex: flatUtreexoProofIndex}
}

// DropCfIndex drops the CF index from the provided database if exists.
func DropUtreexoCfIndex(db database.DB, interrupt <-chan struct{}) error {
	return dropIndex(db, utreexoCFIndexParentBucketKey, utreexoCFIndexName, interrupt)
}

// CfIndexInitialized returns true if the cfindex has been created previously.
func UtreexoCfIndexInitialized(db database.DB) bool {
	var exists bool
	db.View(func(dbTx database.Tx) error {
		bucket := dbTx.Metadata().Bucket(utreexoCFIndexParentBucketKey)
		exists = bucket != nil
		return nil
	})

	return exists
}
