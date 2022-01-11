// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/mit-dci/utreexo/accumulator"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
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
	chainParams      *chaincfg.Params

	// The blockchain instance the index corresponds to.
	chain *blockchain.BlockChain

	// mtx protects concurrent access to the utreexoView .
	mtx *sync.RWMutex

	// utreexoState represents the Bitcoin UTXO set as a utreexo accumulator.
	// It keeps all the elements of the forest in order to generate proofs.
	utreexoState *UtreexoState
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
	dels, err := blockchain.BlockToDelLeaves(stxos, idx.chain, block, inskip, -1)
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

	idx.mtx.Lock()
	undoBlock, err := idx.utreexoState.state.Modify(adds, ud.AccProof.Targets)
	idx.mtx.Unlock()
	if err != nil {
		return err
	}

	err = idx.storeUndoBlock(block.Height(), *undoBlock)
	if err != nil {
		return err
	}

	// If the interval is 1, then just save the utreexo proof and we're done.
	if idx.proofGenInterVal == 1 {
		err = idx.storeProof(block.Height(), ud)
		if err != nil {
			return err
		}
	} else {
		// Every proof generation interval, we'll make a multi-block proof.
		if (block.Height() % idx.proofGenInterVal) == 0 {
			err = idx.MakeMultiBlockProof(block.Height(), block.Height()-idx.proofGenInterVal,
				block, stxos)
			if err != nil {
				return err
			}
		}
	}

	return nil
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
	stxos [][]blockchain.SpentTxOut) ([]wire.LeafData, error) {

	// Check that the length is equal to prevent index errors in the below loop.
	if len(blocks) != len(stxos) {
		err := fmt.Errorf("Got %d blocks but %d stxos", len(blocks), len(stxos))
		return nil, err
	}

	var delsToProve []wire.LeafData
	for i, block := range blocks {
		excludeAfter := block.Height() - (block.Height() % idx.proofGenInterVal)

		_, _, inskip, _ := blockchain.DedupeBlock(block)
		dels, err := blockchain.BlockToDelLeaves(stxos[i], idx.chain,
			block, inskip, excludeAfter)
		if err != nil {
			return nil, err
		}
		delsToProve = append(delsToProve, dels...)
	}

	return delsToProve, nil
}

func (idx *FlatUtreexoProofIndex) MakeMultiBlockProof(currentHeight, proveHeight int32,
	block *btcutil.Block, stxos []blockchain.SpentTxOut) error {

	idx.mtx.Lock()
	defer idx.mtx.Unlock()

	// Go back to the desired block to generate the multi-block proof.
	for h := currentHeight; h > proveHeight; h-- {
		undoBlock, err := idx.fetchUndoBlock(h)
		if err != nil {
			return err
		}

		err = idx.utreexoState.state.Undo(*undoBlock)
		if err != nil {
			return err
		}
	}

	// TODO not sure if it should actually be proveHeight+1.  But GenerateUData
	// fails if it doesn't.
	blocks, allStxos, err := idx.fetchBlocks(proveHeight+1, currentHeight)
	if err != nil {
		return err
	}

	delsToProve, err := idx.deletionsToProve(blocks, allStxos)
	if err != nil {
		return err
	}

	ud, err := wire.GenerateUData(delsToProve, idx.utreexoState.state)
	if err != nil {
		return err
	}

	// The height used to store the multi-block proof is the actual block height
	// divided by the proof generation interval.
	//
	// This is because the flatfile only takes in proofs in numerical order
	// (1,2,3...).  By dividing by the interval, we get a height that
	// increments up by one.  If the interval is every 10 blocks, then the
	// resulting values will be 10/10, 20/10, 30/10, 40/10...
	err = idx.storeProof(currentHeight/idx.proofGenInterVal, ud)
	if err != nil {
		return err
	}

	// Re-sync all the reorged blocks.
	for h := proveHeight + 1; h < currentHeight; h++ {
		blk, err := idx.chain.BlockByHeight(h)
		if err != nil {
			return err
		}
		_, outCount, inskip, outskip := blockchain.DedupeBlock(blk)

		stxos, err := idx.chain.FetchSpendJournalUnsafe(blk)
		if err != nil {
			return err
		}

		dels, err := blockchain.BlockToDelLeaves(stxos, idx.chain, blk, inskip, -1)
		if err != nil {
			return err
		}
		adds := blockchain.BlockToAddLeaves(blk, outskip, outCount)

		ud, err := wire.GenerateUData(dels, idx.utreexoState.state)
		if err != nil {
			return err
		}

		_, err = idx.utreexoState.state.Modify(adds, ud.AccProof.Targets)
		if err != nil {
			return err
		}
	}

	// Attach the current block.
	_, outCount, inskip, outskip := blockchain.DedupeBlock(block)
	dels, err := blockchain.BlockToDelLeaves(stxos, idx.chain, block, inskip, -1)
	if err != nil {
		return err
	}
	adds := blockchain.BlockToAddLeaves(block, outskip, outCount)

	ud, err = wire.GenerateUData(dels, idx.utreexoState.state)
	if err != nil {
		return err
	}

	_, err = idx.utreexoState.state.Modify(adds, ud.AccProof.Targets)
	if err != nil {
		return err
	}

	return nil
}

// DisconnectBlock is invoked by the index manager when a new block has been
// disconnected to the main chain.
//
// This is part of the Indexer interface.
func (idx *FlatUtreexoProofIndex) DisconnectBlock(dbTx database.Tx, block *btcutil.Block,
	stxos []blockchain.SpentTxOut) error {

	undoBlock, err := idx.fetchUndoBlock(block.Height())
	if err != nil {
		return err
	}

	idx.mtx.Lock()
	err = idx.utreexoState.state.Undo(*undoBlock)
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

	return nil
}

// FetchUtreexoProof returns the Utreexo proof data for the given block height.
func (idx *FlatUtreexoProofIndex) FetchUtreexoProof(height int32) (*wire.UData, error) {
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
	err = ud.DeserializeCompact(r, udataSerializeBool, 0)
	if err != nil {
		return nil, err
	}

	return ud, nil
}

// storeProof serializes and stores the utreexo data in the proof state.
func (idx *FlatUtreexoProofIndex) storeProof(height int32, ud *wire.UData) error {
	bytesBuf := bytes.NewBuffer(make([]byte, 0, ud.SerializeSizeCompact(udataSerializeBool)))
	err := ud.SerializeCompact(bytesBuf, udataSerializeBool)
	if err != nil {
		return err
	}

	err = idx.proofState.StoreData(height, bytesBuf.Bytes())
	if err != nil {
		return err
	}

	return nil
}

// storeUndoBlock serializes and stores undo blocks in the undo state.
func (idx *FlatUtreexoProofIndex) storeUndoBlock(height int32, undoBlock accumulator.UndoBlock) error {
	undoBuf := bytes.NewBuffer(make([]byte, 0, undoBlock.SerializeSize()))
	err := undoBlock.Serialize(undoBuf)
	if err != nil {
		return err
	}

	err = idx.undoState.StoreData(height, undoBuf.Bytes())
	if err != nil {
		return err
	}

	return nil
}

// fetchUndoBlock returns the undoblock for the given block height.
func (idx *FlatUtreexoProofIndex) fetchUndoBlock(height int32) (*accumulator.UndoBlock, error) {
	if height == 0 {
		return nil, fmt.Errorf("No Undo Block for height %d", height)
	}

	undoBytes, err := idx.undoState.FetchData(height)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(undoBytes)

	undoBlock := new(accumulator.UndoBlock)
	err = undoBlock.Deserialize(r)
	if err != nil {
		return nil, err
	}

	return undoBlock, nil
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
	hashes := make([]accumulator.Hash, 0, len(leaves))
	for _, leaf := range leaves {
		hashes = append(hashes, leaf.LeafHash())
	}

	// Get a read lock for the index.  This will prevent connectBlock from updating
	// the beststate snapshot and the utreexo state.
	idx.mtx.RLock()
	defer idx.mtx.RUnlock()

	accProof, err := idx.utreexoState.state.ProveBatch(hashes)
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
func (idx *FlatUtreexoProofIndex) VerifyAccProof(toProve []accumulator.Hash,
	proof *accumulator.BatchProof) error {
	return idx.utreexoState.state.VerifyBatchProof(toProve, *proof)
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
		Type:   accumulator.RamForest,
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

	path := utreexoBasePath(&UtreexoConfig{DataDir: dataDir, Name: flatUtreexoProofIndexType})
	return deleteUtreexoState(path)
}
