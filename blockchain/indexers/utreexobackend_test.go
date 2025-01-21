// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/syndtr/goleveldb/leveldb"

	//"github.com/utreexo/utreexod/blockchain/indexers"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

func TestUtreexoStateConsistencyWrite(t *testing.T) {
	dbPath := t.TempDir()
	db, err := leveldb.OpenFile(dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { os.RemoveAll(dbPath) }()

	// Values to write.
	numLeaves := rand.Uint64()
	hash := chaincfg.MainNetParams.GenesisHash

	// Write the consistency state.
	ldbTx, err := db.OpenTransaction()
	if err != nil {
		t.Fatal(err)
	}
	err = dbWriteUtreexoStateConsistency(ldbTx, hash, numLeaves)
	if err != nil {
		t.Fatal(err)
	}
	err = ldbTx.Commit()
	if err != nil {
		t.Fatal(err)
	}

	// Fetch the consistency state.
	gotHash, gotNumLeaves, err := dbFetchUtreexoStateConsistency(db)
	if err != nil {
		t.Fatal(err)
	}

	// Compare.
	if *hash != *gotHash {
		t.Fatalf("expected %v, got %v", hash.String(), gotHash.String())
	}
	if numLeaves != gotNumLeaves {
		t.Fatalf("expected %v, got %v", numLeaves, gotNumLeaves)
	}
}

func TestInitConsistentUtreexoState(t *testing.T) {
	// Always remove the root on return.
	defer os.RemoveAll(testDbRoot)

	// Initialize a random number generator with the current time as the seed for unique randomness.
	timenow := time.Now().UnixNano()
	source := rand.NewSource(timenow)
	rand := rand.New(source)

	// Call IndexersTestChainWrapper to initialize the test environment
	chain, indexes, params, manager, tearDown := IndexersTestChainWrapper("TestInitConsistentUtreexoState")
	defer tearDown()

	// Verify that the test environment has been initialized as expected
	if chain == nil {
		t.Fatalf("Failed to initialize blockchain")
	}
	if len(indexes) == 0 {
		t.Fatalf("Failed to initialize indexes")
	}
	if params == nil {
		t.Fatalf("Failed to initialize chain parameters")
	}
	if manager == nil {
		t.Fatalf("Failed to initialize index manager")
	}

	var allSpends []*blockchain.SpendableOut
	var nextSpends []*blockchain.SpendableOut
	var blocks []*btcutil.Block

	// Create a chain with 101 blocks.
	nextBlock := btcutil.NewBlock(params.GenesisBlock)
	// Create a slice with 101 Blocks.
	blocks = append(blocks, nextBlock)

	for i := 0; i < 100; i++ {
		// Add a new block to the chain using the previous block and available UTXOs
		newBlock, newSpendableOuts, err := blockchain.AddBlock(chain, nextBlock, nextSpends)
		if err != nil {
			t.Fatalf("timenow:%v. %v", timenow, err)
		}
		// Update the current block reference to the newly created block
		nextBlock = newBlock
		// Append the newly created block to the list of blocks
		blocks = append(blocks, newBlock)
		// Add the new UTXOs from the block to the global spendable outputs list
		allSpends = append(allSpends, newSpendableOuts...)

		// Shuffle and select UTXOs to be spent in the next block
		var nextSpendsTmp []*blockchain.SpendableOut
		for j := 0; j < len(allSpends); j++ {
			// Randomly pick an index from the spendable outputs
			randIdx := rand.Intn(len(allSpends))
			// Select the spendable output and remove it from the global list
			spend := allSpends[randIdx]                                       // Get the UTXO
			allSpends = append(allSpends[:randIdx], allSpends[randIdx+1:]...) // Remove the UTXO
			nextSpendsTmp = append(nextSpendsTmp, spend)                      // Add to the temporary list
		}
		// Update the spendable outputs for the next block
		nextSpends = nextSpendsTmp

		// Every 10 blocks, flush the UTXO cache to the database
		if i%10 == 0 {
			// Commit the two base blocks to DB
			if err := chain.FlushUtxoCache(blockchain.FlushRequired); err != nil {
				t.Fatalf("timenow %v. TestInitConsistentUtreexoState fail. Unexpected error while flushing cache: %v", timenow, err)
			}
		}
	}

	// Create a temporary directory for LevelDB storage
	dbPath := t.TempDir()
	db, err := leveldb.OpenFile(dbPath, nil)
	if err != nil {
		t.Fatalf("Failed to initialize LevelDB: %v", err)
	}
	// Ensure the database is closed and the directory is removed after the test
	defer func() {
		db.Close()
		os.RemoveAll(dbPath)
	}()

	// Initialize UtreexoState
	p := utreexo.NewMapPollard(true)
	cfg := &UtreexoConfig{MaxMemoryUsage: 1024 * 1024}
	utreexoState := &UtreexoState{
		config:         cfg,
		state:          &p,
		utreexoStateDB: db,
		isFlushNeeded: func() bool {
			return true
		},
		flushLeavesAndNodes: func(tx *leveldb.Transaction) error {
			return manager.Flush(&chain.BestSnapshot().Hash, blockchain.FlushRequired, true)
		},
	}

	// Assign managed blocks to variables as appropriate indexes
	tipHash := blocks[100].Hash()
	tipHeight := int32(100)
	savedHash := blocks[99].Hash()
	invalidHash := chainhash.Hash{0xaa, 0xbb, 0xcc} // Arbitrary invalid hash

	// Define a set of test cases for initConsistentUtreexoState.
	// Each test case specifies a name, savedHash, tipHash, tipHeight, and expected error outcome.
	testCases := []struct {
		name        string
		savedHash   *chainhash.Hash
		tipHash     *chainhash.Hash
		tipHeight   int32
		expectError bool
		description string // Detailed description of the test case
	}{
		{
			name:        "Saved hash equals tip hash",
			savedHash:   tipHash,
			tipHash:     tipHash,
			tipHeight:   tipHeight,
			expectError: false,
			description: "The saved hash is identical to the tip hash; no error should occur.",
		},
		{
			name:        "Saved hash is not equal to tip hash",
			savedHash:   savedHash,
			tipHash:     tipHash,
			tipHeight:   tipHeight,
			expectError: true,
			description: "The saved hash differs from the tip hash; an error is expected.",
		},
		{
			name:        "Saved hash is nil",
			savedHash:   nil,
			tipHash:     tipHash,
			tipHeight:   tipHeight,
			expectError: false,
			description: "The saved hash is nil; this should initialize the state without errors.",
		},
		{
			name:        "Saved hash is not in chain",
			savedHash:   &invalidHash,
			tipHash:     tipHash,
			tipHeight:   tipHeight,
			expectError: true,
			description: "The saved hash does not exist in the chain; an error is expected.",
		},
		{
			name:        "Tip height is negative",
			savedHash:   savedHash,
			tipHash:     tipHash,
			tipHeight:   -1,
			expectError: false,
			description: "A negative tip height implies no processing is needed; no error should occur.",
		},
		{
			name:        "Tip hash is nil",
			savedHash:   savedHash,
			tipHash:     nil,
			tipHeight:   tipHeight,
			expectError: true,
			description: "A nil tip hash is invalid; an error is expected.",
		},
		{
			name:        "Current height less than tip height",
			savedHash:   blocks[50].Hash(),
			tipHash:     blocks[100].Hash(),
			tipHeight:   tipHeight,
			expectError: true,
			description: "The state should recover from block 51 to 100 without errors.",
		},
	}
	// Run a subtest for each case using the test case name.
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := utreexoState.initConsistentUtreexoState(chain, tc.savedHash, tc.tipHash, tc.tipHeight)
			if tc.expectError && err == nil {
				t.Fatalf("Expected error but got none")
			}
			if !tc.expectError && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}
