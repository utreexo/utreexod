package indexers

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	_ "github.com/utreexo/utreexod/database/ffldb"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
	"golang.org/x/exp/slices"
)

const (
	// testDbType is the database backend type to use for the tests.
	testDbType = "ffldb"

	// testDbRoot is the root directory used to create all test databases.
	testDbRoot = "testdbs"

	// blockDataNet is the expected network in the test block data.
	blockDataNet = wire.MainNet
)

func createDB(dbName string) (database.DB, string, error) {
	if !blockchain.IsSupportedDbType(testDbType) {
		return nil, "", fmt.Errorf("unsupported db type %v", testDbType)
	}

	// Handle memory database specially since it doesn't need the disk
	// specific handling.
	var db database.DB
	var dbPath string
	if testDbType == "memdb" {
		ndb, err := database.Create(testDbType)
		if err != nil {
			return nil, "", fmt.Errorf("error creating db: %v", err)
		}
		db = ndb
	} else {
		// Create the root directory for test databases.
		if !blockchain.FileExists(testDbRoot) {
			if err := os.MkdirAll(testDbRoot, 0700); err != nil {
				err := fmt.Errorf("unable to create test db "+
					"root: %v", err)
				return nil, "", err
			}
		}

		// Create a new database to store the accepted blocks into.
		dbPath = filepath.Join(testDbRoot, dbName)
		_ = os.RemoveAll(dbPath)
		ndb, err := database.Create(testDbType, dbPath, blockDataNet)
		if err != nil {
			return nil, "", fmt.Errorf("error creating db: %v", err)
		}
		db = ndb
	}

	return db, dbPath, nil
}

func initIndexes(dbPath string, db database.DB, params *chaincfg.Params) (
	*Manager, []Indexer, error) {

	flatUtreexoProofIndex, err := NewFlatUtreexoProofIndex(false, params, 50*1024*1024, dbPath, db.Flush)
	if err != nil {
		return nil, nil, err
	}

	utreexoProofIndex, err := NewUtreexoProofIndex(db, false, 50*1024*1024, params, dbPath, db.Flush)
	if err != nil {
		return nil, nil, err
	}

	indexes := []Indexer{
		utreexoProofIndex,
		flatUtreexoProofIndex,
	}
	indexManager := NewManager(db, indexes)
	return indexManager, indexes, nil
}

func indexersTestChain(testName string) (*blockchain.BlockChain, []Indexer, *chaincfg.Params, *Manager, func()) {
	params := chaincfg.RegressionNetParams
	params.CoinbaseMaturity = 1

	db, dbPath, err := createDB(testName)
	tearDown := func() {
		db.Close()
		os.RemoveAll(dbPath)
	}
	if err != nil {
		tearDown()
		os.RemoveAll(testDbRoot)
		panic(fmt.Errorf("error creating database: %v", err))
	}

	// Create the indexes to be used in the chain.
	indexManager, indexes, err := initIndexes(dbPath, db, &params)
	if err != nil {
		tearDown()
		os.RemoveAll(testDbRoot)
		panic(fmt.Errorf("error creating indexes: %v", err))
	}

	// Create the main chain instance.
	chain, err := blockchain.New(&blockchain.Config{
		DB:               db,
		ChainParams:      &params,
		Checkpoints:      nil,
		TimeSource:       blockchain.NewMedianTime(),
		SigCache:         txscript.NewSigCache(1000),
		UtxoCacheMaxSize: 10 * 1024 * 1024,
		IndexManager:     indexManager,
	})
	if err != nil {
		tearDown()
		os.RemoveAll(testDbRoot)
		panic(fmt.Errorf("failed to create chain instance: %v", err))
	}

	return chain, indexes, &params, indexManager, tearDown
}

// csnTestChain creates a chain using the compact utreexo state.
func csnTestChain(testName string) (*blockchain.BlockChain, *chaincfg.Params, func(), error) {
	params := chaincfg.RegressionNetParams
	params.CoinbaseMaturity = 1

	db, dbPath, err := createDB(testName)
	tearDown := func() {
		db.Close()
		os.RemoveAll(dbPath)
	}
	if err != nil {
		return nil, nil, tearDown, err
	}

	// Create the main csn chain instance.
	chain, err := blockchain.New(&blockchain.Config{
		DB:          db,
		ChainParams: &params,
		Checkpoints: nil,
		TimeSource:  blockchain.NewMedianTime(),
		SigCache:    txscript.NewSigCache(1000),
		UtreexoView: blockchain.NewUtreexoViewpoint(),
	})
	if err != nil {
		err := fmt.Errorf("failed to create csn chain instance: %v", err)
		return nil, nil, tearDown, err
	}

	return chain, &params, tearDown, nil
}

// compareUtreexoIdx compares the indexed proof and the undo blocks from start
// to end.
func compareUtreexoIdx(start, end int32, pruned bool, chain *blockchain.BlockChain, indexes []Indexer) error {
	// Check that the newly added data to both of the indexes are equal.
	for b := start; b <= end; b++ {
		// Declare the utreexo data and the undo blocks that we'll be
		// comparing.
		var utreexoUD, flatUD *wire.UData
		var stump, flatStump utreexo.Stump
		var numAdds, flatNumAdds uint64
		var targets, flatTargets []uint64
		var delHashes, flatDelHashes []utreexo.Hash

		for _, indexer := range indexes {
			switch idxType := indexer.(type) {
			case *UtreexoProofIndex:
				block, err := chain.BlockByHeight(b)

				err = idxType.db.View(func(dbTx database.Tx) error {
					stump, err = dbFetchUtreexoState(dbTx, block.Hash())
					if err != nil {
						return err
					}
					return nil
				})
				if err != nil {
					return err
				}

				if !idxType.config.Pruned {
					utreexoUD, err = idxType.FetchUtreexoProof(block.Hash())
					if err != nil {
						return err
					}

				} else {
					err = idxType.db.View(func(dbTx database.Tx) error {
						numAdds, targets, delHashes, err = dbFetchUndoData(dbTx, block.Hash())
						if err != nil {
							return err
						}

						return nil
					})
				}

			case *FlatUtreexoProofIndex:
				var err error
				if !idxType.config.Pruned {
					flatUD, err = idxType.FetchUtreexoProof(b)
					if err != nil {
						return err
					}
				} else {
					var err error
					flatNumAdds, flatTargets, flatDelHashes, err = idxType.fetchUndoBlock(b)
					if err != nil {
						return err
					}
				}

				flatStump, err = idxType.fetchRoots(b)
				if err != nil {
					return err
				}
			}
		}

		if pruned {
			if numAdds != flatNumAdds ||
				!slices.Equal(targets, flatTargets) ||
				!slices.Equal(delHashes, flatDelHashes) {

				err := fmt.Errorf("Fetched undo data differ for "+
					"utreexo proof index and flat utreexo proof index at height %d", b)
				return err
			}
		} else {
			if !reflect.DeepEqual(utreexoUD, flatUD) {
				err := fmt.Errorf("Fetched utreexo data differ for "+
					"utreexo proof index and flat utreexo proof index at height %d", b)
				return err
			}
		}

		if !reflect.DeepEqual(stump, flatStump) {
			err := fmt.Errorf("Fetched root data differ for "+
				"utreexo proof index and flat utreexo proof index at height %d", b)
			return err
		}
	}

	return nil
}

// syncCsnChain will take in two chains: one to sync from, one to sync.  Sync will
// be done from start to end.
func syncCsnChain(start, end int32, chainToSyncFrom, csnChain *blockchain.BlockChain,
	indexes []Indexer) error {

	for b := start; b <= end; b++ {
		// Fetch the raw block bytes from the database.
		block, err := chainToSyncFrom.BlockByHeight(b)
		if err != nil {
			str := fmt.Errorf("Fail at block height %d err:%s\n", b, err)
			return str
		}

		var flatUD, ud *wire.UData
		for _, indexer := range indexes {
			switch idxType := indexer.(type) {
			case *FlatUtreexoProofIndex:
				flatUD, err = idxType.FetchUtreexoProof(b)
				if err != nil {
					return err
				}
			case *UtreexoProofIndex:
				ud, err = idxType.FetchUtreexoProof(block.Hash())
				if err != nil {
					return err
				}
			}
		}

		if !reflect.DeepEqual(ud, flatUD) {
			err := fmt.Errorf("Fetched utreexo data differ for "+
				"utreexo proof index and flat utreexo proof index at height %d", b)
			return err
		}

		block.MsgBlock().UData = ud

		// This will reset the serializedBlock inside the btcutil.Block.  It's needed because the
		// serializedBlock is already cached without the udata which is needed for the csn.
		newBlock := btcutil.NewBlock(block.MsgBlock())
		_, _, err = csnChain.ProcessBlock(newBlock, blockchain.BFNone)
		if err != nil {
			str := fmt.Errorf("ProcessBlock fail at block height %d err: %s\n", b, err)
			return str
		}
	}

	return nil
}

// testUtreexoProof tests the generated proof on the exact same accumulator,
// making sure that the verification code pass.
func testUtreexoProof(block *btcutil.Block, chain *blockchain.BlockChain, indexes []Indexer) error {
	if len(indexes) != 2 {
		err := fmt.Errorf("Expected 2 indexs but got %d", len(indexes))
		return err
	}

	// Fetch the proofs from each of the indexes.
	var flatUD, ud *wire.UData
	var stump, flatStump utreexo.Stump
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			var err error
			flatUD, err = idxType.FetchUtreexoProof(block.Height())
			if err != nil {
				return err
			}

			flatStump, err = idxType.fetchRoots(block.Height() - 1)
			if err != nil {
				return err
			}

		case *UtreexoProofIndex:
			var err error
			ud, err = idxType.FetchUtreexoProof(block.Hash())
			if err != nil {
				return err
			}

			prevHash, err := chain.BlockHashByHeight(block.Height() - 1)
			if err != nil {
				return err
			}
			err = idxType.db.View(func(dbTx database.Tx) error {
				stump, err = dbFetchUtreexoState(dbTx, prevHash)
				if err != nil {
					return err
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}

	// Sanity check.
	if !reflect.DeepEqual(ud, flatUD) {
		err := fmt.Errorf("Fetched utreexo data differ for "+
			"utreexo proof index and flat utreexo proof "+
			"index at height %d", block.Height())
		return err
	}
	if !reflect.DeepEqual(stump, flatStump) {
		err := fmt.Errorf("Fetched root data differ for "+
			"utreexo proof index and flat utreexo proof "+
			"index at height %d", block.Height())
		return err
	}

	// Generate the additions and deletions to be made to the accumulator.
	stxos, err := chain.FetchSpendJournal(block)
	if err != nil {
		return err
	}

	_, outCount, inskip, outskip := blockchain.DedupeBlock(block)
	adds := blockchain.BlockToAddLeaves(block, outskip, nil, outCount)

	dels, err := blockchain.BlockToDelLeaves(stxos, chain, block, inskip)
	if err != nil {
		return err
	}
	delHashes := make([]utreexo.Hash, 0, len(ud.LeafDatas))
	for _, del := range dels {
		if del.IsUnconfirmed() {
			continue
		}
		delHashes = append(delHashes, del.LeafHash())
	}

	// Verify the proof on the accumulator.
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			origRoots := idxType.utreexoState.state.GetRoots()

			// Undo back to the state where the proof was generated.
			err := idxType.utreexoState.state.Undo(uint64(len(adds)), utreexo.Proof{Targets: flatUD.AccProof.Targets}, delHashes, flatStump.Roots)
			if err != nil {
				return err
			}
			// Verify the proof.
			err = idxType.utreexoState.state.Verify(delHashes, flatUD.AccProof, false)
			if err != nil {
				return err
			}
			// Go back to the original state.
			err = idxType.utreexoState.state.Modify(adds, delHashes, utreexo.Proof{Targets: flatUD.AccProof.Targets})
			if err != nil {
				return err
			}

			roots := idxType.utreexoState.state.GetRoots()
			if !reflect.DeepEqual(origRoots, roots) {
				return fmt.Errorf("Roots don't equal after undo")
			}

		case *UtreexoProofIndex:
			origRoots := idxType.utreexoState.state.GetRoots()

			// Undo back to the state where the proof was generated.
			err = idxType.utreexoState.state.Undo(uint64(len(adds)), utreexo.Proof{Targets: ud.AccProof.Targets}, delHashes, stump.Roots)
			if err != nil {
				return err
			}
			// Verify the proof.
			err = idxType.utreexoState.state.Verify(delHashes, ud.AccProof, false)
			if err != nil {
				return err
			}
			// Go back to the original state.
			err = idxType.utreexoState.state.Modify(adds, delHashes, utreexo.Proof{Targets: ud.AccProof.Targets})
			if err != nil {
				return err
			}

			roots := idxType.utreexoState.state.GetRoots()
			if !reflect.DeepEqual(origRoots, roots) {
				return fmt.Errorf("Roots don't equal after undo")
			}
		}
	}

	return nil
}

// TestProveUtxos tests that the utxos that are proven by the utreexo proof index are verifiable
// by the compact state nodes.
func TestProveUtxos(t *testing.T) {
	// Always remove the root on return.
	defer os.RemoveAll(testDbRoot)

	timenow := time.Now().UnixNano()
	source := rand.NewSource(timenow)
	rand := rand.New(source)

	chain, indexes, params, _, tearDown := indexersTestChain("TestProveUtxos")
	defer tearDown()

	var allSpends []*blockchain.SpendableOut
	var nextSpends []*blockchain.SpendableOut

	// Create a chain with 101 blocks.
	nextBlock := btcutil.NewBlock(params.GenesisBlock)
	for i := 0; i < 100; i++ {
		newBlock, newSpendableOuts, err := blockchain.AddBlock(chain, nextBlock, nextSpends)
		if err != nil {
			t.Fatalf("timenow:%v. %v", timenow, err)
		}
		nextBlock = newBlock

		allSpends = append(allSpends, newSpendableOuts...)

		var nextSpendsTmp []*blockchain.SpendableOut
		for j := 0; j < len(allSpends); j++ {
			randIdx := rand.Intn(len(allSpends))

			spend := allSpends[randIdx]                                       // get
			allSpends = append(allSpends[:randIdx], allSpends[randIdx+1:]...) // delete
			nextSpendsTmp = append(nextSpendsTmp, spend)
		}
		nextSpends = nextSpendsTmp

		if i%10 == 0 {
			// Commit the two base blocks to DB
			if err := chain.FlushUtxoCache(blockchain.FlushRequired); err != nil {
				t.Fatalf("timenow %v. TestProveUtxos fail. Unexpected error while flushing cache: %v", timenow, err)
			}
		}
	}

	// Check that the newly added data to both of the indexes are equal.
	err := compareUtreexoIdx(1, 100, false, chain, indexes)
	if err != nil {
		t.Fatalf("timenow:%v. %v", timenow, err)
	}

	// Create a chain that consumes the data from the indexes and test that this
	// chain is able to consume the data properly.
	csnChain, _, csnTearDown, err := csnTestChain("TestProveUtxos-CsnChain")
	defer csnTearDown()
	if err != nil {
		t.Fatalf("timenow:%v. %v", timenow, err)
	}

	// Sync the csn chain to the tip from block 1.
	err = syncCsnChain(1, chain.BestSnapshot().Height, chain, csnChain, indexes)
	if err != nil {
		t.Fatalf("timenow:%v. %v", timenow, err)
	}

	// Sanity checking.  The chains need to be at the same height for the proofs
	// to verify.
	csnHeight := csnChain.BestSnapshot().Height
	bridgeHeight := chain.BestSnapshot().Height
	if csnHeight != bridgeHeight {
		err := fmt.Errorf("TestProveUtxos fail. Height mismatch. csn chain is at %d "+
			"while bridge chain is at %d", csnHeight, bridgeHeight)
		t.Fatalf("timenow:%v. %v", timenow, err)
	}

	// Repeat verify and prove from the chain 20 times.
	for i := 0; i < 20; i++ {
		// Grab a range of spendables.
		min := 1
		max := len(allSpends)
		randIdxEnd := rand.Intn(max-min+1) + min
		randIdxStart := rand.Intn(randIdxEnd)
		spendables := allSpends[randIdxStart:randIdxEnd]

		// Prep the spendables into utxos and outpoints.
		var utxos []*blockchain.UtxoEntry
		var outpoints []wire.OutPoint
		for _, spendable := range spendables {
			utxo, err := chain.FetchUtxoEntry(spendable.PrevOut)
			if err != nil {
				t.Fatalf("timenow %v. TestProveUtxos fail. err: outpoint %s not found.",
					timenow, spendable.PrevOut.String())
			}

			utxos = append(utxos, utxo)
			outpoints = append(outpoints, spendable.PrevOut)
		}

		// Generate the proof and the hashes of the utxos that are in the accumulator.
		var proof, flatProof *blockchain.ChainTipProof
		for _, indexer := range indexes {
			switch idxType := indexer.(type) {
			case *FlatUtreexoProofIndex:
				var err error
				flatProof, err = idxType.ProveUtxos(utxos, &outpoints)
				if err != nil {
					t.Fatalf("timenow %v. TestProveUtxos fail."+
						"Failed to create proof. err: %v", timenow, err)
				}
			case *UtreexoProofIndex:
				var err error
				proof, err = idxType.ProveUtxos(utxos, &outpoints)
				if err != nil {
					t.Fatalf("timenow %v. TestProveUtxos fail."+
						"Failed to create proof. err: %v", timenow, err)
				}
			}
		}

		// Sanity check.
		if !reflect.DeepEqual(proof, flatProof) {
			err := fmt.Errorf("Generated utreexo proof differ for " +
				"utreexo proof index and flat utreexo proof index")
			t.Fatalf("timenow:%v. %v", timenow, err)
		}
		if !reflect.DeepEqual(proof.HashesProven, flatProof.HashesProven) {
			err := fmt.Errorf("Hashes proven for differ for " +
				"utreexo proof index and flat utreexo proof index")
			t.Fatalf("timenow:%v. %v", timenow, err)
		}

		// Verify the proof with the compact state node.
		uView := csnChain.GetUtreexoView()
		err = uView.VerifyAccProof(proof.HashesProven, proof.AccProof)
		if err != nil {
			t.Fatalf("timenow %v. TestProveUtxos fail. Failed to verify proof err: %v", timenow, err)
		}
	}
}

func TestUtreexoProofIndex(t *testing.T) {
	// Always remove the root on return.
	defer os.RemoveAll(testDbRoot)

	timenow := time.Now().UnixNano()
	source := rand.NewSource(timenow)
	rand := rand.New(source)

	chain, indexes, params, _, tearDown := indexersTestChain("TestUtreexoProofIndex")
	defer tearDown()

	tip := btcutil.NewBlock(params.GenesisBlock)

	// Create block at height 1.
	var emptySpendableOuts []*blockchain.SpendableOut
	b1, spendableOuts1, err := blockchain.AddBlock(chain, tip, emptySpendableOuts)
	if err != nil {
		t.Fatalf("timenow:%v. %v", timenow, err)
	}

	var allSpends []*blockchain.SpendableOut
	nextBlock := b1
	nextSpends := spendableOuts1

	// Create a chain with 101 blocks.
	for b := 0; b < 100; b++ {
		newBlock, newSpendableOuts, err := blockchain.AddBlock(chain, nextBlock, nextSpends)
		if err != nil {
			t.Fatalf("timenow:%v. %v", timenow, err)
		}
		nextBlock = newBlock

		allSpends = append(allSpends, newSpendableOuts...)

		var nextSpendsTmp []*blockchain.SpendableOut
		for i := 0; i < len(allSpends); i++ {
			randIdx := rand.Intn(len(allSpends))

			spend := allSpends[randIdx]                                       // get
			allSpends = append(allSpends[:randIdx], allSpends[randIdx+1:]...) // delete
			nextSpendsTmp = append(nextSpendsTmp, spend)
		}
		nextSpends = nextSpendsTmp

		// Test that the proof that the indexes generated verify on those
		// same indexes.
		err = testUtreexoProof(newBlock, chain, indexes)
		if err != nil {
			t.Fatalf("timenow %v. TestUtreexoProofIndex failed testUtreexoProof at height %d. err: %v", timenow, b, err)
		}

		if b%10 == 0 {
			// Commit the two base blocks to DB
			if err := chain.FlushUtxoCache(blockchain.FlushRequired); err != nil {
				t.Fatalf("timenow %v. unexpected error while flushing cache: %v", timenow, err)
			}
		}
	}

	// Check that the added 100 blocks are equal for both indexes.
	err = compareUtreexoIdx(1, 100, false, chain, indexes)
	if err != nil {
		t.Fatalf("timenow:%v. %v", timenow, err)
	}

	// Create a chain that consumes the data from the indexes and test that this
	// chain is able to consume the data properly.
	csnChain, _, csnTearDown, err := csnTestChain("TestUtreexoProofIndex-CsnChain")
	defer csnTearDown()
	if err != nil {
		t.Fatalf("timenow:%v. %v", timenow, err)
	}

	// Sync the csn chain to the tip from block 1.
	err = syncCsnChain(1, 100, chain, csnChain, indexes)
	if err != nil {
		t.Fatalf("timenow:%v. %v", timenow, err)
	}

	// We'll start adding a different chain starting from block 1. Once we reach block 102,
	// we'll switch over to this chain.
	altBlocks := make([]*btcutil.Block, 110)
	var altSpends []*blockchain.SpendableOut
	altNextSpends := spendableOuts1
	altNextBlock := b1
	for i := range altBlocks {
		var newSpends []*blockchain.SpendableOut
		altBlocks[i], newSpends, err = blockchain.AddBlock(chain, altNextBlock, altNextSpends)
		if err != nil {
			t.Fatalf("timenow:%v. %v", timenow, err)
		}
		altNextBlock = altBlocks[i]

		altSpends = append(altSpends, newSpends...)

		var nextSpendsTmp []*blockchain.SpendableOut
		for i := 0; i < len(altSpends); i++ {
			randIdx := rand.Intn(len(altSpends))

			spend := altSpends[randIdx]                                       // get
			altSpends = append(altSpends[:randIdx], altSpends[randIdx+1:]...) // delete
			nextSpendsTmp = append(nextSpendsTmp, spend)
		}
		altNextSpends = nextSpendsTmp
	}

	// Check that the newly added data to both of the indexes are equal.
	err = compareUtreexoIdx(1, 100, false, chain, indexes)
	if err != nil {
		t.Fatalf("timenow:%v. %v", timenow, err)
	}

	// Reorg the csn chain as well.
	err = syncCsnChain(2, chain.BestSnapshot().Height, chain, csnChain, indexes)
	if err != nil {
		t.Fatalf("timenow:%v. %v", timenow, err)
	}

	// Sanity check that the csn chain did reorg.
	if chain.BestSnapshot().Hash != csnChain.BestSnapshot().Hash {
		t.Fatalf("timenow %v. expected tip to be %s but got %s for the csn chain", timenow,
			chain.BestSnapshot().Hash.String(), csnChain.BestSnapshot().Hash.String())
	}
}

func TestBridgeNodePruneUndoDataGen(t *testing.T) {
	// Always remove the root on return.
	defer os.RemoveAll(testDbRoot)

	chain, indexes, params, indexManager, tearDown := indexersTestChain("TestBridgeNodePruneUndoDataGen")
	defer tearDown()

	var allSpends []*blockchain.SpendableOut
	var nextSpends []*blockchain.SpendableOut

	// Number of blocks we'll generate for the test.
	maxHeight := int32(300)

	// We'll use these to compare against the utreexo state after undo.
	utreexoStates := make([][]*chainhash.Hash, 0, maxHeight)
	utreexoNumLeaves := make([]uint64, 0, maxHeight)

	// Grab the utreexo state and numLeaves.
	for _, indexer := range indexes {
		state := chain.BestSnapshot()
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			utreexoState, numLeaves, err := idxType.FetchUtreexoState(state.Height)
			if err != nil {
				t.Fatal(err)
			}
			utreexoStates = append(utreexoStates, utreexoState)
			utreexoNumLeaves = append(utreexoNumLeaves, numLeaves)
		}
	}

	nextBlock := btcutil.NewBlock(params.GenesisBlock)
	for i := int32(1); i <= maxHeight; i++ {
		newBlock, newSpendableOuts, err := blockchain.AddBlock(chain, nextBlock, nextSpends)
		if err != nil {
			t.Fatal(err)
		}
		nextBlock = newBlock

		allSpends = append(allSpends, newSpendableOuts...)

		var nextSpendsTmp []*blockchain.SpendableOut
		for j := 0; j < len(allSpends); j++ {
			randIdx := rand.Intn(len(allSpends))

			spend := allSpends[randIdx]                                       // get
			allSpends = append(allSpends[:randIdx], allSpends[randIdx+1:]...) // delete
			nextSpendsTmp = append(nextSpendsTmp, spend)
		}
		nextSpends = nextSpendsTmp

		// Grab the utreexo state and numLeaves.
		for _, indexer := range indexes {
			switch idxType := indexer.(type) {
			case *FlatUtreexoProofIndex:
				utreexoState, numLeaves, err := idxType.FetchUtreexoState(i)
				if err != nil {
					t.Fatal(err)
				}
				utreexoStates = append(utreexoStates, utreexoState)
				utreexoNumLeaves = append(utreexoNumLeaves, numLeaves)
			}
		}

		// Compare against the flatutreexo proof index.
		for _, indexer := range indexes {
			switch idxType := indexer.(type) {
			case *UtreexoProofIndex:
				expectRoots := utreexoStates[len(utreexoStates)-1]
				expectNumLeaves := utreexoNumLeaves[len(utreexoNumLeaves)-1]

				var utreexoState []*chainhash.Hash
				var numLeaves uint64
				err = idxType.db.View(func(dbTx database.Tx) error {
					hash, err := idxType.chain.BlockHashByHeight(i)
					if err != nil {
						t.Fatal(err)
					}
					utreexoState, numLeaves, err = idxType.FetchUtreexoState(dbTx, hash)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				if expectNumLeaves != numLeaves {
					t.Fatalf("expected numLeaves of %v but got %v",
						expectNumLeaves, numLeaves)
				}

				if !reflect.DeepEqual(expectRoots, utreexoState) {
					t.Fatalf("block %v, expected roots of %v but got %v",
						i, expectRoots, utreexoState)
				}
			}
		}

		if i%10 == 0 {
			// Commit the two base blocks to DB
			if err := chain.FlushUtxoCache(blockchain.FlushRequired); err != nil {
				t.Fatalf("TestProveUtxos fail. Unexpected error while flushing cache: %v", err)
			}
		}
	}

	// Sanity checking that the proofs are equal.
	err := compareUtreexoIdx(1, maxHeight, false, chain, indexes)
	if err != nil {
		t.Fatal(err)
	}

	// Mark the indexes as pruned. We try fetching proofs just to make sure
	// it's possible to fetch them before marking them as pruned.
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			for height := int32(1); height <= maxHeight; height++ {
				_, err := idxType.FetchUtreexoProof(height)
				if err != nil {
					t.Fatal(err)
				}
			}
			idxType.config.Pruned = true

		case *UtreexoProofIndex:
			for height := int32(1); height <= maxHeight; height++ {
				hash, err := chain.BlockHashByHeight(height)
				if err != nil {
					t.Fatal(err)
				}

				_, err = idxType.FetchUtreexoProof(hash)
				if err != nil {
					t.Fatal(err)
				}
			}
			idxType.config.Pruned = true
		}
	}

	// Close the databases so that they can be initialized again
	// to generate the undo data.
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			err := idxType.CloseUtreexoState()
			if err != nil {
				t.Fatal(err)
			}
		case *UtreexoProofIndex:
			err := idxType.CloseUtreexoState()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	// Here we generate the undo data and delete the proof files.
	err = indexManager.Init(chain, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Make sure that the undo data is the same.
	err = compareUtreexoIdx(1, maxHeight, true, chain, indexes)
	if err != nil {
		t.Fatal(err)
	}

	// Sanity check that the proofs are not fetchable now.
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			_, err := idxType.FetchUtreexoProof(maxHeight)
			if err == nil {
				t.Fatalf("expected an error when trying to" +
					"fetch proofs from pruned bridges")
			}
		case *UtreexoProofIndex:
			hash, err := chain.BlockHashByHeight(maxHeight)
			if err != nil {
				t.Fatal(err)
			}

			_, err = idxType.FetchUtreexoProof(hash)
			if err == nil {
				t.Fatalf("expected an error when trying to" +
					"fetch proofs from pruned bridges")
			}
		}
	}

	// Disconnect utreexostate to genesis. Since we only generate undo blocks
	// til maxHeight-288, don't go all the way to the genesis.
	for i := maxHeight; i > maxHeight-288; i-- {
		block, err := chain.BlockByHeight(i)
		if err != nil {
			t.Fatal(err)
		}

		stxos, err := chain.FetchSpendJournal(block)
		if err != nil {
			t.Fatal(err)
		}

		// Pop off oen because we're gonna check the previous utreexo state.
		utreexoStates = utreexoStates[:len(utreexoStates)-1]
		utreexoNumLeaves = utreexoNumLeaves[:len(utreexoNumLeaves)-1]

		// Grab the last one.
		expectState := utreexoStates[len(utreexoStates)-1]
		expectNumLeaves := utreexoNumLeaves[len(utreexoNumLeaves)-1]

		for _, indexer := range indexes {
			switch idxType := indexer.(type) {
			case *FlatUtreexoProofIndex:
				err = idxType.DisconnectBlock(nil, block, stxos)
				if err != nil {
					t.Fatal(err)
				}

				utreexoState, numLeaves, err := idxType.FetchUtreexoState(block.Height() - 1)
				if err != nil {
					t.Fatal(err)
				}
				if numLeaves != expectNumLeaves {
					t.Fatalf("expected numLeaves of %v but got %v",
						expectNumLeaves, numLeaves)
				}

				if !reflect.DeepEqual(expectState, utreexoState) {
					t.Fatalf("block %v, expected roots of %v but got %v",
						i, expectState, utreexoState)
				}

			case *UtreexoProofIndex:
				err = idxType.db.Update(func(dbTx database.Tx) error {
					return idxType.DisconnectBlock(dbTx, block, stxos)
				})
				if err != nil {
					t.Fatal(err)
				}

				var utreexoState []*chainhash.Hash
				var numLeaves uint64
				err = idxType.db.Update(func(dbTx database.Tx) error {
					hash, err := idxType.chain.BlockHashByHeight(block.Height() - 1)
					if err != nil {
						t.Fatal(err)
					}
					utreexoState, numLeaves, err = idxType.FetchUtreexoState(dbTx, hash)
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(expectState, utreexoState) {
					t.Fatalf("height %v, expected roots of:\n%v\nbut got:\n%v",
						i, expectState, utreexoState)
				}

				if numLeaves != expectNumLeaves {
					t.Fatalf("at height %d, expected numLeaves of %v but got %v",
						i, expectNumLeaves, numLeaves)
				}
			}
		}
	}
}

func compareBlockSummaries(indexes []Indexer, blockHashes []*chainhash.Hash) error {
	var err error
	var flatMsg *wire.MsgUtreexoSummaries
	var msg *wire.MsgUtreexoSummaries
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			flatMsg, err = idxType.FetchUtreexoSummaries(blockHashes)
			if err != nil {
				return err
			}

		case *UtreexoProofIndex:
			msg, err = idxType.FetchUtreexoSummaries(blockHashes)
			if err != nil {
				return err
			}
		}
	}

	for i, flatSummary := range flatMsg.Summaries {
		err = compareSummary(flatSummary, msg.Summaries[i])
		if err != nil {
			return err
		}
	}

	return nil
}

func compareSummary(this, other *wire.UtreexoBlockSummary) error {
	if !this.BlockHash.IsEqual(&other.BlockHash) {
		return fmt.Errorf("expected %v, got %v", this.BlockHash, other.BlockHash)
	}

	if this.NumAdds != other.NumAdds {
		return fmt.Errorf("expected numadds of %v, got %v", this.NumAdds, other.NumAdds)
	}

	if len(this.BlockTargets) != len(other.BlockTargets) {
		return fmt.Errorf("expected %v, got %v", len(this.BlockTargets), len(other.BlockTargets))
	}
	for i := range this.BlockTargets {
		if this.BlockTargets[i] != other.BlockTargets[i] {
			return fmt.Errorf("expected %v, got %v", this.BlockTargets, other.BlockTargets)
		}
	}

	return nil
}

func compareBlockSummaryState(indexes []Indexer, blockHash *chainhash.Hash) error {
	var err error
	var flatMsg *wire.UtreexoBlockSummary
	var msg *wire.UtreexoBlockSummary

	var flatSummaries *wire.MsgUtreexoSummaries
	var summaries *wire.MsgUtreexoSummaries
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			flatMsg, err = idxType.fetchBlockSummary(blockHash)
			if err != nil {
				return err
			}

			flatSummaries, err = idxType.FetchUtreexoSummaries([]*chainhash.Hash{blockHash})
			if err != nil {
				return err
			}

		case *UtreexoProofIndex:
			height, err := idxType.chain.BlockHeightByHash(blockHash)
			if err != nil {
				return err
			}
			prevHash, err := idxType.chain.BlockHashByHeight(height - 1)
			if err != nil {
				return err
			}

			msg, err = idxType.fetchBlockSummary(blockHash, prevHash)
			if err != nil {
				return err
			}

			summaries, err = idxType.FetchUtreexoSummaries([]*chainhash.Hash{blockHash})
			if err != nil {
				return err
			}
		}
	}

	for i, summary := range summaries.Summaries {
		err = compareSummary(summary, flatSummaries.Summaries[i])
		if err != nil {
			return err
		}
	}

	return compareSummary(flatMsg, msg)
}

func compareUtreexoRootsState(indexes []Indexer, blockHash *chainhash.Hash) error {
	var err error
	var flatMsg *wire.MsgUtreexoRoot
	var msg *wire.MsgUtreexoRoot
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			flatMsg, err = idxType.FetchMsgUtreexoRoot(blockHash)
			if err != nil {
				return err
			}

		case *UtreexoProofIndex:
			msg, err = idxType.FetchMsgUtreexoRoot(blockHash)
			if err != nil {
				return err
			}
		}
	}

	if flatMsg.NumLeaves != msg.NumLeaves {
		return fmt.Errorf("expected %v, got %v", flatMsg.NumLeaves, msg.NumLeaves)
	}

	if flatMsg.Target != msg.Target {
		return fmt.Errorf("expected %v, got %v", flatMsg.Target, msg.Target)
	}

	if !flatMsg.BlockHash.IsEqual(&msg.BlockHash) {
		return fmt.Errorf("expected %v, got %v", flatMsg.BlockHash, msg.BlockHash)
	}

	if len(flatMsg.Roots) != len(msg.Roots) {
		return fmt.Errorf("expected %v, got %v", len(flatMsg.Roots), len(msg.Roots))
	}
	for i := range flatMsg.Roots {
		if flatMsg.Roots[i] != msg.Roots[i] {
			return fmt.Errorf("expected %v, got %v", flatMsg.Roots[i], msg.Roots[i])
		}
	}

	if len(flatMsg.Proof) != len(msg.Proof) {
		return fmt.Errorf("expected %v, got %v", len(flatMsg.Proof), len(msg.Proof))
	}
	for i := range flatMsg.Proof {
		if flatMsg.Proof[i] != msg.Proof[i] {
			return fmt.Errorf("expected %v, got %v", flatMsg.Proof[i], msg.Proof[i])
		}
	}

	return nil
}

func TestUtreexoRootsAndSummaryState(t *testing.T) {
	// Always remove the root on return.
	defer os.RemoveAll(testDbRoot)

	chain, indexes, params, _, tearDown := indexersTestChain("TestUtreexoRootsState")
	defer tearDown()

	var allSpends []*blockchain.SpendableOut
	var nextSpends []*blockchain.SpendableOut

	// Number of blocks we'll generate for the test.
	maxHeight := int32(300)

	nextBlock := btcutil.NewBlock(params.GenesisBlock)
	for i := int32(1); i <= maxHeight; i++ {
		newBlock, newSpendableOuts, err := blockchain.AddBlock(chain, nextBlock, nextSpends)
		if err != nil {
			t.Fatal(err)
		}
		nextBlock = newBlock

		allSpends = append(allSpends, newSpendableOuts...)

		var nextSpendsTmp []*blockchain.SpendableOut
		for j := 0; j < len(allSpends); j++ {
			randIdx := rand.Intn(len(allSpends))

			spend := allSpends[randIdx]                                       // get
			allSpends = append(allSpends[:randIdx], allSpends[randIdx+1:]...) // delete
			nextSpendsTmp = append(nextSpendsTmp, spend)
		}
		nextSpends = nextSpendsTmp

		err = compareUtreexoRootsState(indexes, newBlock.Hash())
		if err != nil {
			t.Fatal(err)
		}

		err = compareBlockSummaryState(indexes, newBlock.Hash())
		if err != nil {
			t.Fatal(err)
		}
	}

	bestHash := chain.BestSnapshot().Hash
	err := chain.InvalidateBlock(&bestHash)
	if err != nil {
		t.Fatal(err)
	}

	bestHash = chain.BestSnapshot().Hash
	err = compareUtreexoRootsState(indexes, &bestHash)
	if err != nil {
		t.Fatal(err)
	}

	err = compareBlockSummaryState(indexes, &bestHash)
	if err != nil {
		t.Fatal(err)
	}

	blockHeights := []int32{1, 2, 3, 4}
	blockHashes := make([]*chainhash.Hash, 0, len(blockHeights))
	for _, height := range blockHeights {
		hash, err := chain.BlockHashByHeight(height)
		if err != nil {
			t.Fatal(err)
		}

		blockHashes = append(blockHashes, hash)
	}

	err = compareBlockSummaries(indexes, blockHashes)
	if err != nil {
		t.Fatal(err)
	}
}

func checkTTLRoots(t *testing.T, maxHeight int32, expected []utreexo.Stump, indexes []Indexer) {
	for i := int32(1); i <= maxHeight; i++ {
		for _, indexer := range indexes {
			switch idxType := indexer.(type) {
			case *FlatUtreexoProofIndex:
				gotPollard, err := idxType.initTTLState(i)
				if err != nil {
					t.Fatal(err)
				}
				gotStump := utreexo.Stump{
					Roots:     gotPollard.GetRoots(),
					NumLeaves: gotPollard.GetNumLeaves(),
				}

				require.Equal(t, expected[i-1], gotStump)
			}
		}
	}
}

func TestTTLs(t *testing.T) {
	// Always remove the root on return.
	defer os.RemoveAll(testDbRoot)

	chain, indexes, params, _, tearDown := indexersTestChain("TestTTLs")
	defer tearDown()

	var allSpends []*blockchain.SpendableOut
	var nextSpends []*blockchain.SpendableOut

	// Number of blocks we'll generate for the test.
	maxHeight := int32(500)

	expectedStumps := make([]utreexo.Stump, 0, maxHeight)

	expectAfterUndoTTLs := make([][]uint64, 0, maxHeight)
	nextBlock := btcutil.NewBlock(params.GenesisBlock)
	for i := int32(1); i <= maxHeight; i++ {
		newBlock, newSpendableOuts, err := blockchain.AddBlock(chain, nextBlock, nextSpends)
		if err != nil {
			t.Fatal(err)
		}
		nextBlock = newBlock

		allSpends = append(allSpends, newSpendableOuts...)

		var nextSpendsTmp []*blockchain.SpendableOut
		for j := 0; j < len(allSpends); j++ {
			randIdx := rand.Intn(len(allSpends))

			spend := allSpends[randIdx]                                       // get
			allSpends = append(allSpends[:randIdx], allSpends[randIdx+1:]...) // delete
			nextSpendsTmp = append(nextSpendsTmp, spend)
		}
		nextSpends = nextSpendsTmp

		if i == maxHeight-1 {
			for _, indexer := range indexes {
				switch idxType := indexer.(type) {
				case *FlatUtreexoProofIndex:
					for i := int32(1); i <= maxHeight-1; i++ {
						ttl, err := idxType.fetchTTLs(i)
						if err != nil {
							t.Fatal(err)
						}
						expectAfterUndoTTLs = append(expectAfterUndoTTLs, ttl)
					}
				}
			}
		}

		for _, indexer := range indexes {
			switch idxType := indexer.(type) {
			case *FlatUtreexoProofIndex:
				stump := utreexo.Stump{}
				for h := int32(1); h <= i; h++ {
					ttls, err := idxType.fetchTTLs(h)
					if err != nil {
						t.Fatal(err)
					}

					ttl := wire.UtreexoTTL{
						BlockHeight: uint32(h),
						TTLs:        ttls,
					}

					buf := bytes.NewBuffer(make([]byte, 0, ttl.SerializeSize()))
					err = ttl.Serialize(buf)
					if err != nil {
						t.Fatal(err)
					}
					rootHash := sha256.Sum256(buf.Bytes())

					_, err = stump.Update(nil, []utreexo.Hash{rootHash}, utreexo.Proof{})
					if err != nil {
						t.Fatal(err)
					}
				}

				expectedStumps = append(expectedStumps, stump)
			}
		}
	}

	checkTTLRoots(t, maxHeight, expectedStumps, indexes)

	ttls := make([][]uint64, 0, maxHeight)
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			for i := int32(1); i <= maxHeight; i++ {
				ttl, err := idxType.fetchTTLs(i)
				if err != nil {
					t.Fatal(err)
				}
				ttls = append(ttls, ttl)
			}
		}
	}

	// Undo a block.
	bestHash := chain.BestSnapshot().Hash
	err := chain.InvalidateBlock(&bestHash)
	if err != nil {
		t.Fatal(err)
	}

	// Check if the ttls match up to the previous block.
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			for i := int32(1); i <= maxHeight-1; i++ {
				ttl, err := idxType.fetchTTLs(i)
				if err != nil {
					t.Fatal(err)
				}
				require.Equal(t, expectAfterUndoTTLs[i-1], ttl)
			}
		}
	}

	// Re-do that block.
	err = chain.ReconsiderBlock(&bestHash)
	if err != nil {
		t.Fatal(err)
	}

	// Check if the ttls still match up.
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			for i := int32(1); i <= maxHeight; i++ {
				ttl, err := idxType.fetchTTLs(i)
				if err != nil {
					t.Fatal(err)
				}
				require.Equal(t, ttls[i-1], ttl)
			}
		}
	}
}
