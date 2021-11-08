package blockchain

import (
	"math/rand"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcutil"
)

func TestReorg(t *testing.T) {
	source := rand.NewSource(time.Now().UnixNano())
	rand := rand.New(source)

	chain, params, tearDown := utxoCacheTestChain("TestReorg")
	defer tearDown()
	tip := btcutil.NewBlock(params.GenesisBlock)

	// Create block at height 1.
	var emptySpendableOuts []*spendableOut
	b1, spendableOuts1 := addBlock(chain, tip, emptySpendableOuts)

	var allSpends []*spendableOut
	nextBlock := b1
	nextSpends := spendableOuts1

	// Create a chain with 101 blocks.
	for b := 0; b < 100; b++ {
		newBlock, newSpendableOuts := addBlock(chain, nextBlock, nextSpends)
		nextBlock = newBlock

		allSpends = append(allSpends, newSpendableOuts...)

		var nextSpendsTmp []*spendableOut
		for i := 0; i < len(allSpends); i++ {
			randIdx := rand.Intn(len(allSpends))

			spend := allSpends[randIdx]                                       // get
			allSpends = append(allSpends[:randIdx], allSpends[randIdx+1:]...) // delete
			nextSpendsTmp = append(nextSpendsTmp, spend)
		}
		nextSpends = nextSpendsTmp

		if b%10 == 0 {
			// Commit the two base blocks to DB
			if err := chain.FlushCachedState(FlushRequired); err != nil {
				t.Fatalf("unexpected error while flushing cache: %v", err)
			}
		}
	}

	// We'll start adding a different chain starting from block 1. Once we reach block 102,
	// we'll switch over to this chain.
	altBlocks := make([]*btcutil.Block, 110)
	var altSpends []*spendableOut
	altNextSpends := spendableOuts1
	altNextBlock := b1
	for i := range altBlocks {
		var newSpends []*spendableOut
		altBlocks[i], newSpends = addBlock(chain, altNextBlock, altNextSpends)
		altNextBlock = altBlocks[i]

		altSpends = append(altSpends, newSpends...)

		var nextSpendsTmp []*spendableOut
		for i := 0; i < len(altSpends); i++ {
			randIdx := rand.Intn(len(altSpends))

			spend := altSpends[randIdx]                                       // get
			altSpends = append(altSpends[:randIdx], altSpends[randIdx+1:]...) // delete
			nextSpendsTmp = append(nextSpendsTmp, spend)
		}
		altNextSpends = nextSpendsTmp
	}
}
