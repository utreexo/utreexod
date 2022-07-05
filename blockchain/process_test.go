package blockchain_test

import (
	"testing"

	"github.com/utreexo/utreexod/blockchain/fullblocktests"
	"github.com/utreexo/utreexod/chaincfg"
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
	for testNum, test := range tests {
		for itemNum, item := range test {
			switch item := item.(type) {
			case fullblocktests.AcceptedHeader:
				harness.AcceptHeader(item.Name)
			case fullblocktests.RejectedHeader:
				harness.RejectHeader(item.Name, item.RejectCode)
			default:
				t.Fatalf("test #%d, item #%d is not one of "+
					"the supported test instance types -- "+
					"got type: %T", testNum, itemNum, item)
			}
		}
	}
}
