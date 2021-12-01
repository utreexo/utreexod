package blockchain

import (
	"fmt"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/wire"
)

// assertConsistencyState asserts the utxo consistency states of the blockchain.
func assertConsistencyState(t *testing.T, chain *BlockChain, code byte, hash *chainhash.Hash) {
	var actualCode byte
	var actualHash *chainhash.Hash
	err := chain.db.View(func(dbTx database.Tx) (err error) {
		actualCode, actualHash, err = dbFetchUtxoStateConsistency(dbTx)
		return
	})
	if err != nil {
		t.Fatalf("Error fetching utxo state consistency: %v", err)
	}
	if actualCode != code {
		t.Fatalf("Unexpected consistency code: %d instead of %d",
			actualCode, code)
	}
	if !actualHash.IsEqual(hash) {
		t.Fatalf("Unexpected consistency hash: %v instead of %v",
			actualHash, hash)
	}
}

func parseOutpointKey(b []byte) wire.OutPoint {
	op := wire.OutPoint{}
	copy(op.Hash[:], b[:chainhash.HashSize])
	idx, _ := deserializeVLQ(b[chainhash.HashSize:])
	op.Index = uint32(idx)
	return op
}

// assertNbEntriesOnDisk asserts that the total number of utxo entries on the
// disk is equal to the given expected number.
func assertNbEntriesOnDisk(t *testing.T, chain *BlockChain, expectedNumber int) {
	t.Log("Asserting nb entries on disk...")
	var nb int
	err := chain.db.View(func(dbTx database.Tx) error {
		cursor := dbTx.Metadata().Bucket(utxoSetBucketName).Cursor()
		nb = 0
		for b := cursor.First(); b; b = cursor.Next() {
			nb++
			entry, err := deserializeUtxoEntry(cursor.Value())
			if err != nil {
				t.Fatalf("Failed to deserialize entry: %v", err)
			}
			outpoint := parseOutpointKey(cursor.Key())
			t.Log(outpoint.String())
			t.Log(spew.Sdump(entry))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Error fetching utxo entries: %v", err)
	}
	if nb != expectedNumber {
		t.Fatalf("Expected %d elements in the UTXO set, but found %d",
			expectedNumber, nb)
	}
	t.Log("Assertion done")
}

// utxoCacheTestChain creates a test BlockChain to be used for utxo cache tests.
// It uses the regression test parameters, a coin matutiry of 1 block and sets
// the cache size limit to 10 MiB.
func utxoCacheTestChain(testName string) (*BlockChain, *chaincfg.Params, func()) {
	params := chaincfg.RegressionNetParams
	chain, tearDown, err := chainSetup(testName, &params)
	if err != nil {
		panic(fmt.Sprintf("error loading blockchain with database: %v", err))
	}

	chain.TstSetCoinbaseMaturity(1)
	chain.utxoCache.maxTotalMemoryUsage = 10 * 1024 * 1024

	return chain, &params, tearDown
}

// TODO:kcalvinalvin These tests can't be parallel since there's a problem with closing
// the database.  Get rid of ffldb.

func TestUtxoCache_SimpleFlush(t *testing.T) {
	chain, params, tearDown := utxoCacheTestChain("TestUtxoCache_SimpleFlush")
	defer tearDown()
	cache := chain.utxoCache
	tip := btcutil.NewBlock(params.GenesisBlock)

	// The chainSetup init triggered write of consistency status of genesis.
	assertConsistencyState(t, chain, ucsConsistent, params.GenesisHash)
	assertNbEntriesOnDisk(t, chain, 0)

	// LastFlushHash starts with genesis.
	if cache.lastFlushHash != *params.GenesisHash {
		t.Fatalf("lastFlushHash before first flush expected to be "+
			"genesis block hash, instead was %v", cache.lastFlushHash)
	}

	// First, add 10 utxos without flushing.
	for i := 0; i < 10; i++ {
		tip, _ = AddBlock(chain, tip, nil)
	}
	if len(cache.cachedEntries) != 10 {
		t.Fatalf("Expected 10 entries, has %d instead", len(cache.cachedEntries))
	}

	// All elements should be fresh and modified.
	for outpoint, elem := range cache.cachedEntries {
		if elem == nil {
			t.Fatalf("Unexpected nil entry found for %v", outpoint)
		}
		if elem.packedFlags&tfModified == 0 {
			t.Fatal("Entry should be marked mofified")
		}
		if elem.packedFlags&tfFresh == 0 {
			t.Fatal("Entry should be marked fresh")
		}
	}

	// Not flushed yet.
	assertConsistencyState(t, chain, ucsConsistent, params.GenesisHash)
	assertNbEntriesOnDisk(t, chain, 0)

	// Flush.
	if err := chain.FlushCachedState(FlushRequired); err != nil {
		t.Fatalf("unexpected error while flushing cache: %v", err)
	}
	if len(cache.cachedEntries) != 0 {
		t.Fatalf("Expected 0 entries, has %d instead", len(cache.cachedEntries))
	}
	assertConsistencyState(t, chain, ucsConsistent, tip.Hash())
	assertNbEntriesOnDisk(t, chain, 10)
}

func TestUtxoCache_ThresholdPeriodicFlush(t *testing.T) {
	chain, params, tearDown := utxoCacheTestChain("TestUtxoCache_ThresholdPeriodicFlush")
	defer tearDown()
	cache := chain.utxoCache
	tip := btcutil.NewBlock(params.GenesisBlock)

	// Set the limit to the size of 10 empty elements.  This will trigger
	// flushing when adding 10 non-empty elements.
	cache.maxTotalMemoryUsage = (*UtxoEntry)(nil).memoryUsage() * 10

	// Add 10 elems and let it exceed the threshold.
	var flushedAt *chainhash.Hash
	for i := 0; i < 10; i++ {
		tip, _ = AddBlock(chain, tip, nil)
		if len(cache.cachedEntries) == 0 {
			flushedAt = tip.Hash()
		}
	}

	// Should have flushed in the meantime.
	if flushedAt == nil {
		t.Fatal("should have flushed")
	}
	assertConsistencyState(t, chain, ucsConsistent, flushedAt)
	if len(cache.cachedEntries) >= 10 {
		t.Fatalf("Expected less than 10 entries, has %d instead",
			len(cache.cachedEntries))
	}

	// Make sure flushes on periodic.
	cache.maxTotalMemoryUsage = (utxoFlushPeriodicThreshold*cache.totalMemoryUsage())/100 - 1
	if err := chain.FlushCachedState(FlushPeriodic); err != nil {
		t.Fatalf("unexpected error while flushing cache: %v", err)
	}
	if len(cache.cachedEntries) != 0 {
		t.Fatalf("Expected 0 entries, has %d instead", len(cache.cachedEntries))
	}
	assertConsistencyState(t, chain, ucsConsistent, tip.Hash())
	assertNbEntriesOnDisk(t, chain, 10)
}

func TestUtxoCache_Reorg(t *testing.T) {
	chain, params, tearDown := utxoCacheTestChain("TestUtxoCache_Reorg")
	defer tearDown()
	tip := btcutil.NewBlock(params.GenesisBlock)

	// Create base blocks 1 and 2 that will not be reorged.
	// Spend the outputs of block 1.
	var emptySpendableOuts []*SpendableOut
	b1, spendableOuts1 := AddBlock(chain, tip, emptySpendableOuts)
	b2, spendableOuts2 := AddBlock(chain, b1, spendableOuts1)
	t.Log(spew.Sdump(spendableOuts2))
	//                 db       cache
	// block 1:                  stxo
	// block 2:                  utxo

	// Commit the two base blocks to DB
	if err := chain.FlushCachedState(FlushRequired); err != nil {
		t.Fatalf("unexpected error while flushing cache: %v", err)
	}
	//                 db       cache
	// block 1:       stxo
	// block 2:       utxo
	assertConsistencyState(t, chain, ucsConsistent, b2.Hash())
	assertNbEntriesOnDisk(t, chain, len(spendableOuts2))

	// Add blocks 3 and 4 that will be orphaned.
	// Spend the outputs of block 2 and 3a.
	b3a, spendableOuts3 := AddBlock(chain, b2, spendableOuts2)
	AddBlock(chain, b3a, spendableOuts3)
	//                 db       cache
	// block 1:       stxo
	// block 2:       utxo       stxo      << these are left spent without flush
	// ---
	// block 3a:                 stxo
	// block 4a:                 utxo

	// Build an alternative chain of blocks 3 and 4 + new 5th, spending none of the outputs
	b3b, altSpendableOuts3 := AddBlock(chain, b2, nil)
	b4b, altSpendableOuts4 := AddBlock(chain, b3b, nil)
	b5b, altSpendableOuts5 := AddBlock(chain, b4b, nil)
	totalSpendableOuts := spendableOuts2[:]
	totalSpendableOuts = append(totalSpendableOuts, altSpendableOuts3...)
	totalSpendableOuts = append(totalSpendableOuts, altSpendableOuts4...)
	totalSpendableOuts = append(totalSpendableOuts, altSpendableOuts5...)
	t.Log(spew.Sdump(totalSpendableOuts))
	//                 db       cache
	// block 1:       stxo
	// block 2:       utxo       utxo     << now they should become utxo
	// ---
	// block 3b:                 utxo
	// block 4b:                 utxo
	// block 5b:                 utxo

	if err := chain.FlushCachedState(FlushRequired); err != nil {
		t.Fatalf("unexpected error while flushing cache: %v", err)
	}
	//                 db       cache
	// block 1:       stxo
	// block 2:       utxo
	// ---
	// block 3b:      utxo
	// block 4b:      utxo
	// block 5b:      utxo
	assertConsistencyState(t, chain, ucsConsistent, b5b.Hash())
	assertNbEntriesOnDisk(t, chain, len(totalSpendableOuts))
}
