package indexers

import (
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
)

// blockPlan describes the spends of a single branch block.
type blockPlan struct {
	// shared is how many outputs created on the shared history (before
	// the fork) the block spends.
	shared int

	// own is how many outputs created earlier on the same branch the
	// block spends.
	own int
}

// csnReattachScenario describes one fork/reorg shape for
// TestCsnReorgReattachKnownValidWithSpends.
type csnReattachScenario struct {
	name string

	// sharedBlocks is the number of blocks mined on top of the genesis
	// block before the chain forks.  Their coinbase outputs are the
	// shared spendable outputs available to both branches.
	sharedBlocks int

	// sharedSpends makes every shared-history block from the third one
	// on spend one output created two blocks earlier, so the accumulator
	// at the fork point contains deletions and a bushier tree shape.
	sharedSpends bool

	// planA and planB describe the spends of the initial branch-A and
	// branch-B blocks.  planA[0].own and planB[0].own must be 0 since a
	// branch has no own outputs before its first block.
	planA []blockPlan
	planB []blockPlan

	// extOwnSpends is how many branch-created outputs every branch block
	// past the initial plans (the blocks added to win each reorg round)
	// spends.
	extOwnSpends int

	// cycles is how many full B->A reorg round trips the compact state
	// node is driven through.  Every round trip re-attaches the
	// KnownValid spending blocks of branch A once, and from the second
	// cycle on the KnownValid spending blocks of branch B as well.
	cycles int
}

// TestCsnReorgReattachKnownValidWithSpends is a regression test for a mainnet
// compact state node failure: after a chain fork, re-attaching a block that
// the node had previously connected and then detached (block index status
// KnownValid, block data with utreexo proof stored in the db) fails inside
// verifyReorganizationValidity when the block contains deletions (spends).
// On mainnet, utreexoView.VerifyUData passes for the re-attached block but
// utreexoView.ProcessUData fails inside uview.Modify with "node hash <hash>
// not found", leaving the node stuck retrying the same reorg forever.
//
// The test drives a csn chain through:
//  1. shared history -> branch A (with blocks spending shared-history and
//     branch-created outputs) connected,
//  2. reorg to a heavier branch B (a-blocks detached, KnownValid),
//  3. reorg back to an extended branch A, which must re-attach the
//     KnownValid spending blocks through verifyReorganizationValidity's
//     KnownValid attach path, running VerifyUData and ProcessUData for each
//     of them sequentially on the scratch utreexoView rebuilt from the db
//     roots at the fork point.
//
// The test asserts that every reorg succeeds.  A failure with
// "verifyReorganizationValidity fail while attaching block ... node hash ...
// not found" reproduces the mainnet bug.
func TestCsnReorgReattachKnownValidWithSpends(t *testing.T) {
	// Always remove the root on return.
	defer os.RemoveAll(testDbRoot)

	scenarios := []csnReattachScenario{
		{
			// The block right after the fork spends, branch B is
			// coinbase-only past its first block, one reorg away
			// and one reorg back.
			name:         "spend-at-fork-plus-1",
			sharedBlocks: 2,
			planA:        []blockPlan{{shared: 1}, {}},
			planB:        []blockPlan{{shared: 1}},
			cycles:       1,
		},
		{
			// More leaves in the accumulator at the fork point and
			// multiple deletions in the re-attached block.
			name:         "bushy-accumulator-multi-spend",
			sharedBlocks: 8,
			sharedSpends: true,
			planA:        []blockPlan{{shared: 3}, {}},
			planB:        []blockPlan{{shared: 1}},
			cycles:       1,
		},
		{
			// The spending block sits deeper than fork+1, so the
			// scratch utreexoView has already been modified by an
			// earlier KnownValid attach when it is processed.
			name:         "spend-at-fork-plus-2",
			sharedBlocks: 3,
			planA:        []blockPlan{{}, {shared: 1}, {}},
			planB:        []blockPlan{{shared: 1}},
			cycles:       1,
		},
		{
			// A branch-A block spends an output created on branch A
			// itself, so its leaf data commits to a branch-A
			// blockhash and proves a leaf that was added to the
			// scratch utreexoView by the previous KnownValid
			// attach, not one present in the fork-point roots.
			name:         "spend-branch-created-output",
			sharedBlocks: 2,
			planA:        []blockPlan{{shared: 1}, {own: 1}, {}},
			planB:        []blockPlan{{shared: 1}},
			cycles:       1,
		},
		{
			// Every block on both branches spends, so each reorg
			// re-attaches several KnownValid blocks with deletions
			// back to back on the same scratch utreexoView.
			name:         "every-block-spends",
			sharedBlocks: 10,
			sharedSpends: true,
			planA: []blockPlan{
				{shared: 2}, {shared: 1, own: 1}, {shared: 1, own: 1},
			},
			planB:        []blockPlan{{shared: 2}, {shared: 1, own: 1}},
			extOwnSpends: 1,
			cycles:       3,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			if testing.Short() && scenario.sharedBlocks > 3 {
				t.Skip("heavy reorg scenario, skipped under -short")
			}
			runCsnReattachScenario(t, scenario)
		})
	}

	// Seeded random search over fork/reorg shapes.  Everything is
	// deterministic for a given seed; if one of these fails, the seed in
	// the subtest name pins down the failing shape.  The search mines
	// many blocks per seed and is the heavy part of this test, so -short
	// skips it and the named scenarios above stand in for it.
	maxSeed := int64(8)
	if testing.Short() {
		maxSeed = 0
	}
	for seed := int64(1); seed <= maxSeed; seed++ {
		s := randomCsnReattachScenario(seed)
		t.Run(s.name, func(t *testing.T) {
			runCsnReattachScenario(t, s)
		})
	}
}

// randomCsnReattachScenario derives a scenario from the given seed.
func randomCsnReattachScenario(seed int64) csnReattachScenario {
	rng := rand.New(rand.NewSource(seed))

	planLen := func() int { return 2 + rng.Intn(3) }

	makePlan := func(n int) []blockPlan {
		plan := make([]blockPlan, n)
		for i := range plan {
			plan[i].shared = rng.Intn(3)
			if i > 0 {
				plan[i].own = rng.Intn(2)
			}
		}
		return plan
	}

	s := csnReattachScenario{
		name:         fmt.Sprintf("random-seed-%d", seed),
		sharedBlocks: 8 + rng.Intn(12),
		sharedSpends: rng.Intn(2) == 0,
		planA:        makePlan(planLen()),
		planB:        makePlan(planLen()),
		extOwnSpends: rng.Intn(2),
		cycles:       1 + rng.Intn(3),
	}

	// The first branch-A block must differ from the first branch-B block
	// (same parent and height) and branch A needs deletions to
	// re-attach, so force spends into the first block of each branch.
	if s.planA[0].shared == 0 {
		s.planA[0].shared = 1
	}
	if s.planB[0].shared == 0 {
		s.planB[0].shared = 1
	}

	return s
}

// branchState tracks one of the two competing branches while the test
// builds them on the bridge and feeds them to the csn.
type branchState struct {
	blocks []*btcutil.Block

	// sharedPool is this branch's disjoint allocation of spendable
	// outputs created on the shared history.
	sharedPool []*blockchain.SpendableOut

	// ownPool is the spendable outputs created by this branch's own
	// blocks.
	ownPool []*blockchain.SpendableOut

	// fed is how many of blocks have been handed to the csn already.
	fed int
}

// takeSpends pops the spends for one block out of the branch pools.
func (bs *branchState) takeSpends(t *testing.T, p blockPlan) []*blockchain.SpendableOut {
	require.LessOrEqual(t, p.shared, len(bs.sharedPool),
		"scenario spends more shared outputs than allocated to the branch")
	require.LessOrEqual(t, p.own, len(bs.ownPool),
		"scenario spends more branch outputs than the branch created")

	spends := make([]*blockchain.SpendableOut, 0, p.shared+p.own)
	spends = append(spends, bs.sharedPool[:p.shared]...)
	bs.sharedPool = bs.sharedPool[p.shared:]
	spends = append(spends, bs.ownPool[:p.own]...)
	bs.ownPool = bs.ownPool[p.own:]
	return spends
}

// planFor returns the spends of the branch block at index i (0-based).
func planFor(s *csnReattachScenario, plan []blockPlan, i int) blockPlan {
	if i < len(plan) {
		return plan[i]
	}
	return blockPlan{own: s.extOwnSpends}
}

// closeUtreexoStates releases the utreexo states of the proof indexes so
// their memory mappings don't accumulate across scenarios.
func closeUtreexoStates(t *testing.T, indexes []Indexer) {
	for _, indexer := range indexes {
		switch idxType := indexer.(type) {
		case *FlatUtreexoProofIndex:
			require.NoError(t, idxType.CloseUtreexoState())
		case *UtreexoProofIndex:
			require.NoError(t, idxType.CloseUtreexoState())
		}
	}
}

func runCsnReattachScenario(t *testing.T, s csnReattachScenario) {
	require.NotEmpty(t, s.planA)
	require.NotEmpty(t, s.planB)
	require.Zero(t, s.planA[0].own, "first branch block cannot spend own outputs")
	require.Zero(t, s.planB[0].own, "first branch block cannot spend own outputs")

	// The bridge node generates and indexes the utreexo proofs that the
	// compact state node consumes.
	bridge, indexes, params, _, tearDown := indexersTestChain(s.name)
	defer tearDown()
	defer closeUtreexoStates(t, indexes)

	csn, _, csnTearDown, err := csnTestChain(s.name + "-csn")
	defer csnTearDown()
	require.NoError(t, err)

	// feedCsn hands the bridge's main chain blocks in [start, end] to the
	// csn chain together with their proofs.
	feedCsn := func(start, end int32) error {
		return syncCsnChain(start, end, bridge, csn, indexes)
	}

	// Build the shared history.  Coinbase maturity is 1, so every shared
	// output is spendable on either branch past the fork.
	var sharedOuts []*blockchain.SpendableOut
	tip := btcutil.NewBlock(params.GenesisBlock)
	for i := 0; i < s.sharedBlocks; i++ {
		var spends []*blockchain.SpendableOut
		if s.sharedSpends && i >= 2 {
			spends = sharedOuts[:1]
			sharedOuts = sharedOuts[1:]
		}

		var outs []*blockchain.SpendableOut
		tip, outs, err = blockchain.AddBlock(bridge, tip, spends)
		require.NoError(t, err)
		sharedOuts = append(sharedOuts, outs...)
	}
	forkBlock := tip
	forkHeight := forkBlock.Height()

	require.NoError(t, feedCsn(1, forkHeight))

	// Split the shared outputs between the branches so that their
	// shared-history spends never overlap.
	sharedA, sharedB := 0, 0
	for i := range s.planA {
		sharedA += s.planA[i].shared
	}
	for i := range s.planB {
		sharedB += s.planB[i].shared
	}
	require.LessOrEqual(t, sharedA+sharedB, len(sharedOuts),
		"scenario spends more shared outputs than the shared history creates")

	branchA := &branchState{sharedPool: sharedOuts[:sharedA]}
	branchB := &branchState{sharedPool: sharedOuts[sharedA : sharedA+sharedB]}

	// addBranchBlock mines the next block of the branch on the bridge.
	addBranchBlock := func(bs *branchState, plan []blockPlan) {
		prev := forkBlock
		if len(bs.blocks) > 0 {
			prev = bs.blocks[len(bs.blocks)-1]
		}
		spends := bs.takeSpends(t, planFor(&s, plan, len(bs.blocks)))

		block, outs, err := blockchain.AddBlock(bridge, prev, spends)
		require.NoError(t, err)
		bs.blocks = append(bs.blocks, block)
		bs.ownPool = append(bs.ownPool, outs...)
	}

	// feedBranch hands the csn the branch blocks it has not seen yet.
	// The bridge must be on the branch's tip so that the heights map to
	// the branch's blocks.
	feedBranch := func(bs *branchState) error {
		err := feedCsn(forkHeight+int32(bs.fed)+1, forkHeight+int32(len(bs.blocks)))
		if err != nil {
			return err
		}
		bs.fed = len(bs.blocks)
		return nil
	}

	// Build the initial branch A on the bridge and connect it on the
	// csn.  The spending blocks are now fully validated by the csn:
	// their block nodes are KnownValid and their block data, including
	// the utreexo proofs, is stored in the csn's db.
	for i := 0; i < len(s.planA); i++ {
		addBranchBlock(branchA, s.planA)
	}
	require.NoError(t, feedBranch(branchA))
	require.Equal(t, *branchA.blocks[len(branchA.blocks)-1].Hash(), csn.BestSnapshot().Hash)

	for cycle := 1; cycle <= s.cycles; cycle++ {
		// Extend branch B on the bridge until it is heavier than
		// branch A.
		for len(branchB.blocks) <= len(branchA.blocks) {
			addBranchBlock(branchB, s.planB)
		}
		require.Equal(t, *branchB.blocks[len(branchB.blocks)-1].Hash(),
			bridge.BestSnapshot().Hash, "bridge did not reorg to branch B")

		// Feed the new branch-B blocks to the csn.  The last one makes
		// branch B heavier, so the csn reorganizes: the branch-A
		// blocks are detached and become KnownValid side chain blocks.
		// From the second cycle on this also re-attaches the
		// KnownValid branch-B blocks, including the spending ones.
		err = feedBranch(branchB)
		require.NoError(t, err, "cycle %d: csn failed to reorg to branch B", cycle)
		require.Equal(t, *branchB.blocks[len(branchB.blocks)-1].Hash(),
			csn.BestSnapshot().Hash, "cycle %d: csn did not reorg to branch B", cycle)

		// Extend branch A on the bridge until it is heavier than
		// branch B.
		for len(branchA.blocks) <= len(branchB.blocks) {
			addBranchBlock(branchA, s.planA)
		}
		require.Equal(t, *branchA.blocks[len(branchA.blocks)-1].Hash(),
			bridge.BestSnapshot().Hash, "bridge did not reorg back to branch A")

		// Feed the new branch-A blocks to the csn.  The last one makes
		// branch A heavier again, so the csn must reorganize back:
		// verifyReorganizationValidity detaches the branch-B blocks
		// and re-attaches the KnownValid branch-A blocks, running
		// VerifyUData and ProcessUData for each spending block on the
		// scratch utreexoView rebuilt from the db roots.  This is the
		// step that reproduces the mainnet "node hash not found"
		// failure.
		err = feedBranch(branchA)
		require.NoError(t, err,
			"cycle %d: csn failed to reorg back to branch A, "+
				"re-attaching the KnownValid spending blocks", cycle)
		require.Equal(t, *branchA.blocks[len(branchA.blocks)-1].Hash(),
			csn.BestSnapshot().Hash, "cycle %d: csn did not reorg back to branch A", cycle)
	}

	// Sanity check: the csn and the bridge agree on the final tip.
	require.Equal(t, bridge.BestSnapshot().Hash, csn.BestSnapshot().Hash)
}

// TestCsnAttachOnlyReorgReattachKnownValidWithSpends reproduces the mainnet
// reorg-loop failure through an attach-only reorganization.
//
// When the reorganization has blocks to detach, verifyReorganizationValidity
// rebuilds its scratch utreexoView from the per-block roots stored in the db
// (dbFetchUtreexoView), which yields a self-consistent roots-only
// accumulator.  When there is nothing to detach, the scratch view is
// b.utreexoView.CopyWithRoots() instead: a copy of the live accumulator.
// CopyWithRoots copies the live root Node structs, whose LBelow/RBelow
// fields still point at child nodes that are not copied into the new node
// map.  VerifyUData only matches the computed roots and passes, but
// ProcessUData -> Modify -> Ingest walks those dangling pointers: it skips
// re-inserting the children of a node that already claims to have them and
// then fails to find the children, returning "node hash <hash> not found".
// The reorganization fails the same way on every retry, which is the mainnet
// reorg loop.
//
// The csn ends up in an attach-only reorganization with KnownValid attach
// blocks here the same way a stuck mainnet node does, with its tip on the
// fork point and the heavier branch fully validated but detached:
//
//  1. branch A (a1 spends shared outputs, a2) is connected and fully
//     validated, then detached by a reorg to the heavier branch B, leaving
//     a1 and a2 KnownValid side chain blocks with their proof data stored,
//  2. branch B's first block is invalidated, which detaches branch B (tip
//     is now the fork point) and then reorganizes to the now-best branch A:
//     detachNodes is empty and attachNodes re-attaches the KnownValid a1,
//     with deletions, through verifyReorganizationValidity's KnownValid
//     path.
func TestCsnAttachOnlyReorgReattachKnownValidWithSpends(t *testing.T) {
	// Always remove the root on return.
	defer os.RemoveAll(testDbRoot)

	name := "csn-attach-only-reattach"

	// The bridge node generates and indexes the utreexo proofs that the
	// compact state node consumes.
	bridge, indexes, params, _, tearDown := indexersTestChain(name)
	defer tearDown()
	defer closeUtreexoStates(t, indexes)

	csn, _, csnTearDown, err := csnTestChain(name + "-csn")
	defer csnTearDown()
	require.NoError(t, err)

	// Build the shared history.  Coinbase maturity is 1, so every shared
	// output is spendable on either branch past the fork.
	var sharedOuts []*blockchain.SpendableOut
	tip := btcutil.NewBlock(params.GenesisBlock)
	for i := 0; i < 4; i++ {
		var outs []*blockchain.SpendableOut
		tip, outs, err = blockchain.AddBlock(bridge, tip, nil)
		require.NoError(t, err)
		sharedOuts = append(sharedOuts, outs...)
	}
	forkBlock := tip
	forkHeight := forkBlock.Height()

	require.NoError(t, syncCsnChain(1, forkHeight, bridge, csn, indexes))

	// Branch A: a1 spends a shared output, a2 is coinbase-only.
	a1, _, err := blockchain.AddBlock(bridge, forkBlock, sharedOuts[:1])
	require.NoError(t, err)
	a2, _, err := blockchain.AddBlock(bridge, a1, nil)
	require.NoError(t, err)

	// Connect branch A on the csn.  a1 and a2 are now fully validated:
	// KnownValid with their proof data stored in the csn's db.
	require.NoError(t, syncCsnChain(forkHeight+1, forkHeight+2, bridge, csn, indexes))
	require.Equal(t, *a2.Hash(), csn.BestSnapshot().Hash)

	// Branch B: heavier than branch A.  b1 spends a different shared
	// output so that it differs from a1.
	b1, _, err := blockchain.AddBlock(bridge, forkBlock, sharedOuts[1:2])
	require.NoError(t, err)
	b2, _, err := blockchain.AddBlock(bridge, b1, nil)
	require.NoError(t, err)
	b3, _, err := blockchain.AddBlock(bridge, b2, nil)
	require.NoError(t, err)
	require.Equal(t, *b3.Hash(), bridge.BestSnapshot().Hash)

	// Feed branch B to the csn: it reorgs to branch B, detaching a1 and
	// a2, which stay KnownValid.
	require.NoError(t, syncCsnChain(forkHeight+1, forkHeight+3, bridge, csn, indexes))
	require.Equal(t, *b3.Hash(), csn.BestSnapshot().Hash)

	// Invalidating b1 first detaches all of branch B, leaving the csn's
	// tip on the fork point, and then reorganizes to the now-best branch
	// A.  That reorganization has nothing to detach and re-attaches the
	// KnownValid a1 (with deletions) and a2.  This is the step that
	// reproduces the mainnet "node hash not found" failure.
	err = csn.InvalidateBlock(b1.Hash())
	require.NoError(t, err,
		"attach-only reorg failed to re-attach the KnownValid spending block %v",
		a1.Hash())
	require.Equal(t, *a2.Hash(), csn.BestSnapshot().Hash,
		"csn did not reorg back to branch A after invalidating branch B")
}
