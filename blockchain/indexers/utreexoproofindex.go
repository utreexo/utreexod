// Copyright (c) 2021 The utreexo developer
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"crypto/sha256"
	"fmt"
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

	// utreexoStateKey is the name of the utreexo state data.  It is included
	// in the utreexoParentBucketKey and contains the utreexo state data.
	utreexoStateKey = []byte("utreexostatekey")

	// utreexoUndoKey is the name of the utreexo undo data bucket. It is included
	// in the utreexoParentBucketKey and contains the data necessary for disconnecting
	// blocks.
	utreexoUndoKey = []byte("utreexoundokey")
)

// Ensure the UtreexoProofIndex type implements the Indexer interface.
var _ Indexer = (*UtreexoProofIndex)(nil)

// Ensure the UtreexoProofIndex type implements the NeedsInputser interface.
var _ NeedsInputser = (*UtreexoProofIndex)(nil)

// UtreexoProofIndex implements a utreexo accumulator proof index for all the blocks.
type UtreexoProofIndex struct {
	db     database.DB
	config *UtreexoConfig

	// The blockchain instance the index corresponds to.
	chain *blockchain.BlockChain

	// mtx protects concurrent access to utreexoView.
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

	// The time of when the utreexo state was last flushed.
	lastFlushTime time.Time
}

// NeedsInputs signals that the index requires the referenced inputs in order
// to properly create the index.
//
// This implements the NeedsInputser interface.
func (idx *UtreexoProofIndex) NeedsInputs() bool {
	return true
}

// initUtreexoRootsState creates an accumulator from all the existing roots and
// holds it in memory so that the proofs for them can be generated.
func (idx *UtreexoProofIndex) initUtreexoRootsState(bestHeight int32) error {
	idx.utreexoRootsState = utreexo.NewAccumulator()

	// The bestHeight is always -1 during initialization. We change this
	// to 0 so that the genesis block gets connected.
	if bestHeight == -1 {
		bestHeight = 0
	}
	for h := int32(0); h <= bestHeight; h++ {
		hash, err := idx.chain.BlockHashByHeight(h)
		if err != nil {
			return err
		}
		var stump utreexo.Stump
		err = idx.db.View(func(dbTx database.Tx) error {
			stump, err = dbFetchUtreexoState(dbTx, hash)
			return err
		})
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

// initBlockSummaryState creates an accumulator from all the existing roots and
// holds it in memory so that the proofs for them can be generated.
func (idx *UtreexoProofIndex) initBlockSummaryState(bestHeight int32) error {
	idx.blockSummaryState = utreexo.NewAccumulator()

	var prevNumLeaves uint64
	for h := int32(0); h <= bestHeight; h++ {
		blockHash, err := idx.chain.BlockHashByHeight(h)
		if err != nil {
			return err
		}

		var numLeaves uint64
		err = idx.db.View(func(dbTx database.Tx) error {
			stump, err := dbFetchUtreexoState(dbTx, blockHash)
			numLeaves = stump.NumLeaves
			return err
		})

		numAdds := uint16(numLeaves - prevNumLeaves)
		prevNumLeaves = numLeaves

		proof, err := idx.FetchUtreexoProof(blockHash)
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

// Init initializes the utreexo proof index. This is part of the Indexer
// interface.
func (idx *UtreexoProofIndex) Init(chain *blockchain.BlockChain,
	tipHash *chainhash.Hash, tipHeight int32) error {

	idx.chain = chain

	// Init Utreexo State.
	uState, err := InitUtreexoState(idx.config, chain, nil, tipHash, tipHeight)
	if err != nil {
		return err
	}
	idx.utreexoState = uState
	idx.lastFlushTime = time.Now()

	err = idx.initUtreexoRootsState(tipHeight)
	if err != nil {
		return err
	}

	if !idx.config.Pruned {
		// Only build the block summary state as it's useless anyways
		// if we can't serve the proofs.
		return idx.initBlockSummaryState(tipHeight)
	}

	// Check if the utreexo undo bucket exists.
	var exists bool
	err = idx.db.View(func(dbTx database.Tx) error {
		parentBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey)
		bucket := parentBucket.Bucket(utreexoUndoKey)
		exists = bucket != nil
		return nil
	})
	if err != nil {
		return err
	}

	// If the undo bucket exists, we can return now. If the undo bucket
	// doesn't exist, the node is just now being pruned after being an
	// archive node.
	if exists {
		return nil
	}

	// Create the undo bucket as we're a pruned node now.
	err = idx.db.Update(func(dbTx database.Tx) error {
		parentBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey)
		_, err = parentBucket.CreateBucket(utreexoUndoKey)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	// The bestHeight is less than 288, then just undo all the blocks we have.
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

	for i := int32(0); i < undoCount; i++ {
		height := bestHeight - i

		block, err := idx.chain.BlockByHeight(height)
		if err != nil {
			return err
		}

		ud := new(wire.UData)
		err = idx.db.View(func(dbTx database.Tx) error {
			proofBytes, err := dbFetchUtreexoProofEntry(dbTx, block.Hash())
			if err != nil {
				return err
			}
			r := bytes.NewReader(proofBytes)

			err = ud.Deserialize(r)
			if err != nil {
				return err
			}

			return nil
		})

		// Generate the data for the undo block.
		_, outCount, _, outskip := blockchain.DedupeBlock(block)
		adds := blockchain.BlockToAddLeaves(block, outskip, nil, outCount)
		delHashes, err := idx.chain.ReconstructUData(ud, *block.Hash())
		if err != nil {
			return err
		}

		// Store undo block.
		err = idx.db.Update(func(dbTx database.Tx) error {
			err = dbStoreUndoData(dbTx, uint64(len(adds)),
				ud.AccProof.Targets, block.Hash(), delHashes)
			if err != nil {
				return err
			}

			return nil
		})
	}

	// Remove all proofs.
	err = idx.db.Update(func(tx database.Tx) error {
		parentBucket := tx.Metadata().Bucket(utreexoParentBucketKey)
		return parentBucket.DeleteBucket(utreexoProofIndexKey)
	})
	if err != nil {
		return err
	}

	return nil
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

	_, err = utreexoParentBucket.CreateBucket(utreexoStateKey)
	if err != nil {
		return err
	}

	// Only create the undo bucket if the node is pruned.
	if idx.config.Pruned {
		_, err = utreexoParentBucket.CreateBucket(utreexoUndoKey)
		if err != nil {
			return err
		}
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

	delHashes := make([]utreexo.Hash, len(ud.LeafDatas))
	for i := range delHashes {
		delHashes[i] = ud.LeafDatas[i].LeafHash()
	}

	// For pruned nodes, the undo data is necessary for reorgs.
	if idx.config.Pruned {
		err = dbStoreUndoData(dbTx,
			uint64(len(adds)), ud.AccProof.Targets, block.Hash(), delHashes)
		if err != nil {
			return err
		}
	}

	idx.mtx.Lock()
	err = idx.utreexoState.state.Modify(adds, delHashes, ud.AccProof)
	idx.mtx.Unlock()
	if err != nil {
		return err
	}

	// Don't store proofs if the node is pruned.
	if idx.config.Pruned {
		return nil
	}

	err = dbStoreUtreexoProof(dbTx, block.Hash(), ud)
	if err != nil {
		return err
	}

	err = dbStoreUtreexoState(dbTx, block.Hash(), idx.utreexoState.state)
	if err != nil {
		return err
	}

	err = idx.updateBlockSummaryState(uint16(len(adds)), block.Hash(), ud.AccProof)
	if err != nil {
		return err
	}

	return idx.updateRootsState()
}

// getUndoData returns the data needed for undo. For pruned nodes, we fetch the data from
// the undo block. For archive nodes, we generate the data from the proof.
func (idx *UtreexoProofIndex) getUndoData(dbTx database.Tx, block *btcutil.Block) (uint64, []uint64, []utreexo.Hash, error) {
	var (
		numAdds   uint64
		targets   []uint64
		delHashes []utreexo.Hash
	)

	if !idx.config.Pruned {
		ud, err := idx.FetchUtreexoProof(block.Hash())
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
		numAdds, targets, delHashes, err = dbFetchUndoData(dbTx, block.Hash())
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
func (idx *UtreexoProofIndex) DisconnectBlock(dbTx database.Tx, block *btcutil.Block,
	stxos []blockchain.SpentTxOut) error {

	prevHash, err := idx.chain.BlockHashByHeight(block.Height() - 1)
	if err != nil {
		return err
	}

	state, err := dbFetchUtreexoState(dbTx, prevHash)
	if err != nil {
		return err
	}

	numAdds, targets, delHashes, err := idx.getUndoData(dbTx, block)
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

	err = dbDeleteUtreexoState(dbTx, block.Hash())
	if err != nil {
		return err
	}

	if idx.config.Pruned {
		err = dbDeleteUndoData(dbTx, block.Hash())
		if err != nil {
			return err
		}
	} else {
		err = dbDeleteUtreexoProofEntry(dbTx, block.Hash())
		if err != nil {
			return err
		}

		err = idx.initBlockSummaryState(block.Height() - 1)
		if err != nil {
			return err
		}
	}

	// Re-initializes to the current accumulator roots, effectively disconnecting
	// a block.
	return idx.initUtreexoRootsState(block.Height() - 1)
}

// FetchUtreexoProof returns the Utreexo proof data for the given block hash.
func (idx *UtreexoProofIndex) FetchUtreexoProof(hash *chainhash.Hash) (*wire.UData, error) {
	if idx.config.Pruned {
		return nil, fmt.Errorf("Cannot fetch historical proof as the node is pruned")
	}

	ud := new(wire.UData)
	err := idx.db.View(func(dbTx database.Tx) error {
		proofBytes, err := dbFetchUtreexoProofEntry(dbTx, hash)
		if err != nil {
			return err
		}
		if proofBytes == nil {
			return nil
		}
		r := bytes.NewReader(proofBytes)

		err = ud.Deserialize(r)
		if err != nil {
			return err
		}

		return nil
	})

	return ud, err
}

// GetLeafHashPositions returns the positions of the passed in hashes.
func (idx *UtreexoProofIndex) GetLeafHashPositions(delHashes []utreexo.Hash) []uint64 {
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
func (idx *UtreexoProofIndex) GenerateUDataPartial(dels []wire.LeafData, positions []uint64) (*wire.UData, error) {
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

// ProveUtxos returns an accumulator proof of the outpoints passed in with
// respect to the UTXO state at chaintip.
//
// NOTE The accumulator state differs at every block height.  The caller must
// take into consideration that an accumulator proof at block X will not be valid
// at block height X+1.
//
// This function is safe for concurrent access.
func (idx *UtreexoProofIndex) ProveUtxos(utxos []*blockchain.UtxoEntry,
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
	// the height and the utreexo state.
	idx.mtx.RLock()
	defer idx.mtx.RUnlock()

	// Prove the commited hashes.
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
func (idx *UtreexoProofIndex) VerifyAccProof(toProve []utreexo.Hash,
	proof *utreexo.Proof) error {
	return idx.utreexoState.state.Verify(toProve, *proof, false)
}

// updateRootsState updates the roots accumulator state from the roots of the current accumulator.
func (idx *UtreexoProofIndex) updateRootsState() error {
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
func (idx *UtreexoProofIndex) updateBlockSummaryState(numAdds uint16, blockHash *chainhash.Hash, proof utreexo.Proof) error {
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

// PruneBlock is invoked when an older block is deleted after it's been
// processed.
//
// This is part of the Indexer interface.
func (idx *UtreexoProofIndex) PruneBlock(_ database.Tx, _ *chainhash.Hash, lastKeptHeight int32) error {
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

// FetchUtreexoSummaries fetches all the summaries and attaches a proof for those summaries if requsted with the includeProof boolean.
func (idx *UtreexoProofIndex) FetchUtreexoSummaries(blockHashes []*chainhash.Hash) (*wire.MsgUtreexoSummaries, error) {
	msg := wire.MsgUtreexoSummaries{
		Summaries: make([]*wire.UtreexoBlockSummary, 0, len(blockHashes)),
	}

	for i := range blockHashes {
		var prevHash *chainhash.Hash
		if i == 0 {
			height, err := idx.chain.BlockHeightByHash(blockHashes[i])
			if err != nil {
				return nil, err
			}

			prevHash, err = idx.chain.BlockHashByHeight(height - 1)
			if err != nil {
				return nil, err
			}
		} else {
			prevHash = blockHashes[i-1]
		}

		summary, err := idx.fetchBlockSummary(blockHashes[i], prevHash)
		if err != nil {
			return nil, err
		}
		msg.Summaries = append(msg.Summaries, summary)
	}

	return &msg, nil
}

func (idx *UtreexoProofIndex) fetchBlockSummary(blockHash, prevHash *chainhash.Hash) (*wire.UtreexoBlockSummary, error) {
	ud, err := idx.FetchUtreexoProof(blockHash)
	if err != nil {
		return nil, err
	}

	var stump utreexo.Stump
	err = idx.db.View(func(dbTx database.Tx) error {
		var err error
		stump, err = dbFetchUtreexoState(dbTx, blockHash)
		return err
	})
	if err != nil {
		return nil, err
	}

	var prevStump utreexo.Stump
	err = idx.db.View(func(dbTx database.Tx) error {
		var err error
		prevStump, err = dbFetchUtreexoState(dbTx, prevHash)
		return err
	})
	if err != nil {
		height, heighterr := idx.chain.BlockHeightByHash(prevHash)
		if heighterr != nil {
			return nil, err
		}
		if height != 0 {
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

// FetchSummariesRoots returns the roots of the block summary state and the blockhash they were
// at when the roots were fetched.
func (idx *UtreexoProofIndex) FetchSummariesRoots() (utreexo.Stump, chainhash.Hash) {
	stump := utreexo.Stump{
		Roots:     idx.blockSummaryState.GetRoots(),
		NumLeaves: idx.blockSummaryState.GetNumLeaves(),
	}
	besthash := idx.chain.BestSnapshot().Hash

	return stump, besthash
}

// FetchMsgUtreexoRoot returns a complete utreexoroot bitcoin message on the requested block.
func (idx *UtreexoProofIndex) FetchMsgUtreexoRoot(blockHash *chainhash.Hash) (*wire.MsgUtreexoRoot, error) {
	var stump utreexo.Stump
	err := idx.db.View(func(dbTx database.Tx) error {
		var err error
		stump, err = dbFetchUtreexoState(dbTx, blockHash)
		return err
	})
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

// NewUtreexoProofIndex returns a new instance of an indexer that is used to create a utreexo
// proof index using the database passed in. The passed in maxMemoryUsage should be in bytes and
// it determines how much memory the proof index will use up.
//
// It implements the Indexer interface which plugs into the IndexManager that in
// turn is used by the blockchain package.  This allows the index to be
// seamlessly maintained along with the chain.
func NewUtreexoProofIndex(db database.DB, pruned bool, maxMemoryUsage int64,
	chainParams *chaincfg.Params, dataDir string, flush func() error) (*UtreexoProofIndex, error) {

	idx := &UtreexoProofIndex{
		db:  db,
		mtx: new(sync.RWMutex),
		config: &UtreexoConfig{
			MaxMemoryUsage: maxMemoryUsage,
			Params:         chainParams,
			Pruned:         pruned,
			DataDir:        dataDir,
			Name:           db.Type(),
			FlushMainDB:    flush,
		},
	}

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
	udSize := ud.SerializeSize()
	buf := bytes.NewBuffer(make([]byte, 0, udSize))

	err := ud.Serialize(buf)
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
	idx := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoProofIndexKey)
	return idx.Delete(hash[:])
}

// Stores the utreexo state in the database.
func dbStoreUtreexoState(dbTx database.Tx, hash *chainhash.Hash, p utreexo.Utreexo) error {
	bytes, err := blockchain.SerializeUtreexoRoots(p.GetNumLeaves(), p.GetRoots())
	if err != nil {
		return err
	}

	stateBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoStateKey)
	return stateBucket.Put(hash[:], bytes)
}

// Fetches the utreexo state from the database.
func dbFetchUtreexoState(dbTx database.Tx, hash *chainhash.Hash) (utreexo.Stump, error) {
	stateBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoStateKey)
	serialized := stateBucket.Get(hash[:])
	if serialized == nil {
		return utreexo.Stump{}, nil
	}

	numLeaves, roots, err := blockchain.DeserializeUtreexoRoots(serialized)
	if err != nil {
		return utreexo.Stump{}, err
	}

	return utreexo.Stump{Roots: roots, NumLeaves: numLeaves}, nil
}

// Stores the data necessary for undoing blocks.
func dbStoreUndoData(dbTx database.Tx, numAdds uint64,
	targets []uint64, blockHash *chainhash.Hash, delHashes []utreexo.Hash) error {

	bytes, err := serializeUndoBlock(numAdds, targets, delHashes)
	if err != nil {
		return err
	}

	undoBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoUndoKey)
	return undoBucket.Put(blockHash[:], bytes)
}

// Fetches the data necessary for undoing blocks.
func dbFetchUndoData(dbTx database.Tx, blockHash *chainhash.Hash) (uint64, []uint64, []utreexo.Hash, error) {
	undoBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoUndoKey)
	bytes := undoBucket.Get(blockHash[:])

	return deserializeUndoBlock(bytes)
}

// Deletes the data for undoing blocks.
func dbDeleteUndoData(dbTx database.Tx, blockHash *chainhash.Hash) error {
	undoBucket := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoUndoKey)
	return undoBucket.Delete(blockHash[:])
}

// Deletes the utreexo state in the database.
func dbDeleteUtreexoState(dbTx database.Tx, hash *chainhash.Hash) error {
	idx := dbTx.Metadata().Bucket(utreexoParentBucketKey).Bucket(utreexoStateKey)
	return idx.Delete(hash[:])
}

// UtreexoProofIndexInitialized returns true if the cfindex has been created previously.
func UtreexoProofIndexInitialized(db database.DB) bool {
	var exists bool
	db.View(func(dbTx database.Tx) error {
		bucket := dbTx.Metadata().Bucket(utreexoParentBucketKey)
		exists = bucket != nil
		return nil
	})

	return exists
}
