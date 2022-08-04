// Copyright (c) 2022 The utreexod developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fullblocktests

import (
	"testing"

	"github.com/utreexo/utreexod/blockchain"
)

type chaingenHarness struct {
	*testGenerator

	t     *testing.T
	chain *blockchain.BlockChain
}

// AcceptHeader processes the block header associated with the given name in the
// harness generator and expects it to be accepted, but not necessarily to the
// main chain.  It also ensures the underlying block index is consistent with
// the result.
func (g *chaingenHarness) AcceptHeader(blockName string) {
	g.t.Helper()

	header := &g.blockByName(blockName).Header
	blockHash := header.BlockHash()
	g.t.Logf("Testing accept block header %q (hash %s)", blockName,
		blockHash)

	// Determine if the header is already known before attempting to process it.
	alreadyHaveHeader := g.chain.IndexLookupNode(&blockHash) != nil
	err := g.chain.ProcessBlockHeader(header)
	if err != nil {
		g.t.Fatalf("block header %q (hash %s) should have been "+
			"accepted: %v", blockName, blockHash, err)
	}

	// Ensure the accepted header now exists in the block index.
	node := g.chain.IndexLookupNode(&blockHash)
	if node == nil {
		g.t.Fatalf("accepted block header %q (hash %s) should have "+
			"been added to the block index", blockName, blockHash)
	}

	// Ensure the accepted header is not marked as known valid when it was not
	// previously known since that implies the block data is not yet available
	// and therefore it can't possibly be known to be valid.
	//
	// Also, ensure the accepted header is not marked as known invalid, as
	// having known invalid ancestors, or as known to have failed validation.
	status := g.chain.IndexNodeStatus(node)
	if !alreadyHaveHeader && status.KnownValid() {
		g.t.Fatalf("accepted block header %q (hash %s) was not "+
			"already known, but is marked as known valid", blockName, blockHash,
		)
	}
	if status.KnownInvalid() {
		g.t.Fatalf("accepted block header %q (hash %s) is marked "+
			"as known invalid", blockName, blockHash)
	}
	if status.KnownInvalidAncestor() {
		g.t.Fatalf("accepted block header %q (hash %s) is marked "+
			"as having a known invalid ancestor", blockName, blockHash,
		)
	}
	if status.KnownValidateFailed() {
		g.t.Fatalf("accepted block header %q (hash %s) is marked "+
			"as having known to fail validation", blockName, blockHash,
		)
	}
}

// RejectHeader expects the block header associated with the given name in the
// harness generator to be rejected with the provided error code and also
// ensures the underlying block index is consistent with the result.
func (g *chaingenHarness) RejectHeader(blockName string, code blockchain.ErrorCode) {
	g.t.Helper()

	header := &g.blockByName(blockName).Header
	blockHash := header.BlockHash()
	g.t.Logf("Testing reject block header %q (hash %s, reason %v)",
		blockName, blockHash, code)

	// Determine if the header is already known before attempting to process it.
	alreadyHaveHeader := g.chain.IndexLookupNode(&blockHash) != nil

	err := g.chain.ProcessBlockHeader(header)
	if err == nil {
		g.t.Fatalf("block header %q (hash %s) should not have been "+
			"accepted", blockName, blockHash)
	}

	// Ensure the error matches the value specified in the test instance.
	rerr, ok := err.(blockchain.RuleError)
	if (!ok) || rerr.ErrorCode != code {
		g.t.Fatalf("block header %q (hash %s) does not have "+
			"expected reject code -- got %v, want %v", blockName, blockHash,
			err, code)
	}

	// Ensure the rejected header was not added to the block index when it was
	// not already previously successfully added and that it was not removed if
	// it was already previously added.
	node := g.chain.IndexLookupNode(&blockHash)
	switch {
	case !alreadyHaveHeader && node == nil:
		// Header was not added as expected.
		return

	case !alreadyHaveHeader && node != nil:
		g.t.Fatalf("rejected block header %q (hash %s) was added "+
			"to the block index", blockName, blockHash)

	case alreadyHaveHeader && node == nil:
		g.t.Fatalf("rejected block header %q (hash %s) was removed "+
			"from the block index", blockName, blockHash)
	}

	// The header was previously added, so ensure it is not reported as having
	// been validated and that it is now known invalid.
	status := g.chain.IndexNodeStatus(node)
	if status.KnownValid() {
		g.t.Fatalf("rejected block header %q (hash %s) is marked "+
			"as known valid", blockName, blockHash)
	}
	if !status.KnownInvalid() {
		g.t.Fatalf("rejected block header %q (hash %s) is NOT "+
			"marked as known invalid", blockName, blockHash)
	}
}
