package blockchain_test

import (
	"sync"
	"testing"

	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/blockchain/fullblocktests"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
)

var (
	processTestGeneratorLock sync.Mutex
	processTestGenerator     *fullblocktests.TestGenerator
)

// TestProcessLogic ensures processing a mix of headers and blocks under a wide
// variety of fairly complex scenarios selects the expected best chain and
// properly tracks the header with the most cumulative work that is not known to
// be invalid as well as the one that is known to be invalid (when it exists).
func TestProcessLogic(t *testing.T) {
	// Generate or reuse a shared chain generator with a set of blocks that form
	// a fairly complex overall block tree including multiple forks such that
	// some branches are valid and others contain invalid headers and/or blocks
	// with multiple valid descendants as well as further forks at various
	// heights from those invalid branches.
	g, tests := fullblocktests.GenerateHeaders()
	// Create a new database and chain instance to run tests against.
	chain, teardownFunc, err := chainSetup("fullblocktest",
		&chaincfg.RegressionNetParams)
	if err != nil {
		t.Errorf("Failed to setup chain instance: %v", err)
		return
	}
	defer teardownFunc()
	harness := chaingenHarness{
		g, t, chain,
	}
	// testRejectedBlock attempts to process the block in the provided test
	// instance and ensures that it was rejected with the reject code
	// specified in the test.
	testRejectedBlock := func(item fullblocktests.RejectedBlock) {
		blockHeight := item.Height
		block := btcutil.NewBlock(item.Block)
		block.SetHeight(blockHeight)
		t.Logf("Testing block %s (hash %s, height %d)",
			item.Name, block.Hash(), blockHeight)

		_, _, err := chain.ProcessBlock(block, blockchain.BFNone)
		if err == nil {
			t.Fatalf("block %q (hash %s, height %d) should not "+
				"have been accepted", item.Name, block.Hash(),
				blockHeight)
		}

		// Ensure the error code is of the expected type and the reject
		// code matches the value specified in the test instance.
		rerr, ok := err.(blockchain.RuleError)
		if !ok {
			t.Fatalf("block %q (hash %s, height %d) returned "+
				"unexpected error type -- got %T, want "+
				"blockchain.RuleError", item.Name, block.Hash(),
				blockHeight, err)
		}
		if rerr.ErrorCode != item.RejectCode {
			t.Fatalf("block %q (hash %s, height %d) does not have "+
				"expected reject code -- got %v, want %v",
				item.Name, block.Hash(), blockHeight,
				rerr.ErrorCode, item.RejectCode)
		}
	}
	for testNum, test := range tests {
		for itemNum, item := range test {
			switch item := item.(type) {
			case fullblocktests.AcceptedHeader:
				harness.AcceptHeader(item.Name)
			case fullblocktests.RejectedHeader:
				harness.RejectHeader(item.Name, item.RejectCode)
			case fullblocktests.RejectedBlock:
				testRejectedBlock(item)
			default:
				t.Fatalf("test #%d, item #%d is not one of "+
					"the supported test instance types -- "+
					"got type: %T", testNum, itemNum, item)
			}
		}
	}
}
