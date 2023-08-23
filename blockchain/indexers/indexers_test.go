package indexers

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/database"
	_ "github.com/utreexo/utreexod/database/ffldb"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
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

func initIndexes(interval int32, dbPath string, db *database.DB, params *chaincfg.Params) (
	*Manager, []Indexer, error) {

	proofGenInterval := new(int32)
	*proofGenInterval = interval
	flatUtreexoProofIndex, err := NewFlatUtreexoProofIndex(dbPath, params, proofGenInterval)
	if err != nil {
		return nil, nil, err
	}

	utreexoProofIndex, err := NewUtreexoProofIndex(*db, dbPath, params)
	if err != nil {
		return nil, nil, err
	}

	indexes := []Indexer{
		utreexoProofIndex,
		flatUtreexoProofIndex,
	}
	indexManager := NewManager(*db, indexes)
	return indexManager, indexes, nil
}

func indexersTestChain(testName string, proofGenInterval int32) (*blockchain.BlockChain, []Indexer, *chaincfg.Params, func()) {
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
	indexManager, indexes, err := initIndexes(proofGenInterval, dbPath, &db, &params)
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

	// Init the indexes.
	err = indexManager.Init(chain, nil)
	if err != nil {
		tearDown()
		os.RemoveAll(testDbRoot)
		panic(fmt.Errorf("failed to init indexs: %v", err))
	}

	return chain, indexes, &params, tearDown
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
func compareUtreexoIdx(start, end int32, chain *blockchain.BlockChain, indexes []Indexer) error {
	// Check that the newly added data to both of the indexes are equal.
	for b := start; b < end; b++ {
		// Declare the utreexo data and the undo blocks that we'll be
		// comparing.
		var utreexoUD, flatUD *wire.UData
		var stump, flatStump utreexo.Stump

		for _, indexer := range indexes {
			switch idxType := indexer.(type) {
			case *UtreexoProofIndex:
				block, err := chain.BlockByHeight(b)
				utreexoUD, err = idxType.FetchUtreexoProof(block.Hash())
				if err != nil {
					return err
				}

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

			case *FlatUtreexoProofIndex:
				var err error
				flatUD, err = idxType.FetchUtreexoProof(b, false)
				if err != nil {
					return err
				}

				flatStump, err = idxType.fetchRoots(b)
				if err != nil {
					return err
				}
			}
		}

		if !reflect.DeepEqual(utreexoUD, flatUD) {
			err := fmt.Errorf("Fetched utreexo data differ for "+
				"utreexo proof index and flat utreexo proof index at height %d", b)
			return err
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

	for b := start; b < end; b++ {
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
				flatUD, err = idxType.FetchUtreexoProof(b, false)
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

		_, _, err = csnChain.ProcessBlock(block, blockchain.BFNone)
		if err != nil {
			str := fmt.Errorf("ProcessBlock fail at block height %d err: %s\n", b, err)
			return str
		}
	}

	return nil
}

// syncCsnChainMultiBlockProof will take in two chains: one to sync from, one to sync.
// Sync will be done from start to end using multi-block proofs.
func syncCsnChainMultiBlockProof(start, end, interval int32, chainToSyncFrom, csnChain *blockchain.BlockChain,
	indexes []Indexer) error {

	for b := start; b < end; b++ {
		var err error
		var ud, multiUd *wire.UData
		var dels []utreexo.Hash
		var remembers []uint32
		if (b % interval) == 0 {
			for _, indexer := range indexes {
				switch idxType := indexer.(type) {
				case *FlatUtreexoProofIndex:
					var roots utreexo.Stump
					if b != 0 {
						roots, err = idxType.fetchRoots(b)
						if err != nil {
							return err
						}
					}

					_, multiUd, dels, err = idxType.FetchMultiUtreexoProof(b + csnChain.GetUtreexoView().GetProofInterval())
					if err != nil {
						return err
					}

					ud, _, _, err = idxType.FetchMultiUtreexoProof(b)
					if err != nil {
						return err
					}

					remembers, err = idxType.FetchRemembers(b)
					if err != nil {
						return err
					}

					_, err = utreexo.Verify(roots, dels, multiUd.AccProof)
					if err != nil {
						panic(err)
					}
				}
			}
			err = csnChain.GetUtreexoView().AddProof(dels, &multiUd.AccProof)
			if err != nil {
				return fmt.Errorf("syncCsnChainMultiBlockProof err at height %d. err: %v:", b, err)
			}
		} else {
			for _, indexer := range indexes {
				switch idxType := indexer.(type) {
				case *FlatUtreexoProofIndex:
					ud, err = idxType.FetchUtreexoProof(b, true)
					if err != nil {
						return err
					}

					remembers, err = idxType.FetchRemembers(b)
					if err != nil {
						return err
					}
				}
			}
		}

		// Fetch the raw block bytes from the database.
		block, err := chainToSyncFrom.BlockByHeight(b)
		if err != nil {
			str := fmt.Errorf("Fail at block height %d err:%s\n", b, err)
			return str
		}

		ud.RememberIdx = remembers
		block.MsgBlock().UData = ud

		_, _, err = csnChain.ProcessBlock(block, blockchain.BFNone)
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
			flatUD, err = idxType.FetchUtreexoProof(block.Height(), false)
			if err != nil {
				return err
			}

			flatStump, err = idxType.fetchRoots(block.Height())
			if err != nil {
				return err
			}

		case *UtreexoProofIndex:
			var err error
			ud, err = idxType.FetchUtreexoProof(block.Hash())
			if err != nil {
				return err
			}

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

	dels, _, err := blockchain.BlockToDelLeaves(stxos, chain, block, inskip, -1)
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
			err := idxType.utreexoState.state.Undo(uint64(len(adds)), flatUD.AccProof.Targets, delHashes, flatStump.Roots)
			if err != nil {
				return err
			}
			// Verify the proof.
			err = idxType.utreexoState.state.Verify(delHashes, flatUD.AccProof)
			if err != nil {
				return err
			}
			// Go back to the original state.
			err = idxType.utreexoState.state.Modify(adds, delHashes, flatUD.AccProof.Targets)
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
			err = idxType.utreexoState.state.Undo(uint64(len(adds)), ud.AccProof.Targets, delHashes, stump.Roots)
			if err != nil {
				return err
			}
			// Verify the proof.
			err = idxType.utreexoState.state.Verify(delHashes, ud.AccProof)
			if err != nil {
				return err
			}
			// Go back to the original state.
			err = idxType.utreexoState.state.Modify(adds, delHashes, ud.AccProof.Targets)
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

	source := rand.NewSource(time.Now().UnixNano())
	rand := rand.New(source)

	chain, indexes, params, tearDown := indexersTestChain("TestProveUtxos", 1)
	defer tearDown()

	var allSpends []*blockchain.SpendableOut
	var nextSpends []*blockchain.SpendableOut

	// Create a chain with 101 blocks.
	nextBlock := btcutil.NewBlock(params.GenesisBlock)
	for i := 0; i < 100; i++ {
		newBlock, newSpendableOuts := blockchain.AddBlock(chain, nextBlock, nextSpends)
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
				t.Fatalf("TestProveUtxos fail. Unexpected error while flushing cache: %v", err)
			}
		}
	}

	// Check that the newly added data to both of the indexes are equal.
	err := compareUtreexoIdx(1, 100, chain, indexes)
	if err != nil {
		t.Fatal(err)
	}

	// Create a chain that consumes the data from the indexes and test that this
	// chain is able to consume the data properly.
	csnChain, _, csnTearDown, err := csnTestChain("TestProveUtxos-CsnChain")
	defer csnTearDown()
	if err != nil {
		t.Fatal(err)
	}

	// Sync the csn chain to the tip from block 1.
	err = syncCsnChain(1, 101, chain, csnChain, indexes)
	if err != nil {
		t.Fatal(err)
	}

	// Sanity checking.  The chains need to be at the same height for the proofs
	// to verify.
	csnHeight := csnChain.BestSnapshot().Height
	bridgeHeight := chain.BestSnapshot().Height
	if csnHeight != bridgeHeight {
		err := fmt.Errorf("TestProveUtxos fail. Height mismatch. csn chain is at %d "+
			"while bridge chain is at %d", csnHeight, bridgeHeight)
		t.Fatal(err)
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
				t.Fatalf("TestProveUtxos fail. err: outpoint %s not found.",
					spendable.PrevOut.String())
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
					t.Fatalf("TestProveUtxos fail."+
						"Failed to create proof. err: %v", err)
				}
			case *UtreexoProofIndex:
				var err error
				proof, err = idxType.ProveUtxos(utxos, &outpoints)
				if err != nil {
					t.Fatalf("TestProveUtxos fail."+
						"Failed to create proof. err: %v", err)
				}
			}
		}

		// Sanity check.
		if !reflect.DeepEqual(proof, flatProof) {
			err := fmt.Errorf("Generated utreexo proof differ for " +
				"utreexo proof index and flat utreexo proof index")
			t.Fatal(err)
		}
		if !reflect.DeepEqual(proof.HashesProven, flatProof.HashesProven) {
			err := fmt.Errorf("Hashes proven for differ for " +
				"utreexo proof index and flat utreexo proof index")
			t.Fatal(err)
		}

		// Verify the proof with the compact state node.
		uView := csnChain.GetUtreexoView()
		err = uView.VerifyAccProof(proof.HashesProven, proof.AccProof)
		if err != nil {
			t.Fatalf("TestProveUtxos fail. Failed to verify proof err: %v", err)
		}
	}
}

func TestUtreexoProofIndex(t *testing.T) {
	// Always remove the root on return.
	defer os.RemoveAll(testDbRoot)

	source := rand.NewSource(time.Now().UnixNano())
	rand := rand.New(source)

	chain, indexes, params, tearDown := indexersTestChain("TestUtreexoProofIndex", 1)
	defer tearDown()

	tip := btcutil.NewBlock(params.GenesisBlock)

	// Create block at height 1.
	var emptySpendableOuts []*blockchain.SpendableOut
	b1, spendableOuts1 := blockchain.AddBlock(chain, tip, emptySpendableOuts)

	var allSpends []*blockchain.SpendableOut
	nextBlock := b1
	nextSpends := spendableOuts1

	// Create a chain with 101 blocks.
	for b := 0; b < 100; b++ {
		newBlock, newSpendableOuts := blockchain.AddBlock(chain, nextBlock, nextSpends)
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
		err := testUtreexoProof(newBlock, chain, indexes)
		if err != nil {
			t.Fatalf("TestUtreexoProofIndex failed testUtreexoProof. err: %v", err)
		}

		if b%10 == 0 {
			// Commit the two base blocks to DB
			if err := chain.FlushUtxoCache(blockchain.FlushRequired); err != nil {
				t.Fatalf("unexpected error while flushing cache: %v", err)
			}
		}
	}

	// Check that the added 100 blocks are equal for both indexes.
	err := compareUtreexoIdx(1, 100, chain, indexes)
	if err != nil {
		t.Fatal(err)
	}

	// Create a chain that consumes the data from the indexes and test that this
	// chain is able to consume the data properly.
	csnChain, _, csnTearDown, err := csnTestChain("TestUtreexoProofIndex-CsnChain")
	defer csnTearDown()
	if err != nil {
		t.Fatal(err)
	}

	// Sync the csn chain to the tip from block 1.
	err = syncCsnChain(1, 100, chain, csnChain, indexes)
	if err != nil {
		t.Fatal(err)
	}

	// We'll start adding a different chain starting from block 1. Once we reach block 102,
	// we'll switch over to this chain.
	altBlocks := make([]*btcutil.Block, 110)
	var altSpends []*blockchain.SpendableOut
	altNextSpends := spendableOuts1
	altNextBlock := b1
	for i := range altBlocks {
		var newSpends []*blockchain.SpendableOut
		altBlocks[i], newSpends = blockchain.AddBlock(chain, altNextBlock, altNextSpends)
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
	err = compareUtreexoIdx(1, 100, chain, indexes)
	if err != nil {
		t.Fatal(err)
	}

	// Reorg the csn chain as well.
	err = syncCsnChain(2, 100, chain, csnChain, indexes)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMultiBlockProof(t *testing.T) {
	// Always remove the root on return.
	defer os.RemoveAll(testDbRoot)

	// NOTE Use a fixed source to generate the same data.
	source := rand.NewSource(time.Now().UnixNano())
	rand := rand.New(source)

	chain, indexes, params, tearDown := indexersTestChain("TestMultiBlockProof", defaultProofGenInterval)
	defer tearDown()

	tip := btcutil.NewBlock(params.GenesisBlock)

	// Create block at height 1.
	var emptySpendableOuts []*blockchain.SpendableOut
	b1, spendableOuts1 := blockchain.AddBlock(chain, tip, emptySpendableOuts)

	var allSpends []*blockchain.SpendableOut
	nextBlock := b1
	nextSpends := spendableOuts1

	// Create a chain with 101 blocks.
	for b := 0; b < defaultProofGenInterval*10; b++ {
		newBlock, newSpendableOuts := blockchain.AddBlock(chain, nextBlock, nextSpends)
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

		if b%10 == 0 {
			// Commit the two base blocks to DB
			if err := chain.FlushUtxoCache(blockchain.FlushRequired); err != nil {
				t.Fatalf("unexpected error while flushing cache: %v. Rand source %v",
					err, source)
			}
		}
	}

	// Create a chain that consumes the data from the indexes and test that this
	// chain is able to consume the data properly.
	csnChain, _, csnTearDown, err := csnTestChain("TestMultiBlockProof-CsnChain")
	defer csnTearDown()
	if err != nil {
		str := fmt.Errorf("TestMultiBlockProof: csnTestChain err: %v. Rand source: %v", err, source)
		t.Fatal(str)
	}

	csnChain.GetUtreexoView().SetProofInterval(defaultProofGenInterval)

	// Sync the csn chain to the tip from block 1.
	err = syncCsnChainMultiBlockProof(1, defaultProofGenInterval*10, defaultProofGenInterval, chain, csnChain, indexes)
	if err != nil {
		str := fmt.Errorf("TestMultiBlockProof: syncCsnChainMultiBlockProof err: %v. "+
			"Rand source: %v", err, source)
		t.Fatal(str)
	}
}
