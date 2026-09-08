package blockchain

import (
	"bytes"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
)

// TestReorg exercises connecting a main chain of blocks followed by a
// longer alternate chain, which forces the chain to reorganize.
func TestReorg(t *testing.T) {
	source := rand.NewSource(time.Now().UnixNano())
	rand := rand.New(source)

	chain, params, tearDown := utxoCacheTestChain("TestReorg")
	defer tearDown()
	tip := btcutil.NewBlock(params.GenesisBlock)

	// Create block at height 1.
	var emptySpendableOuts []*SpendableOut
	b1, spendableOuts1, err := AddBlock(chain, tip, emptySpendableOuts)
	require.NoError(t, err)

	var allSpends []*SpendableOut
	nextBlock := b1
	nextSpends := spendableOuts1

	// Create a chain with 101 blocks.
	for b := 0; b < 100; b++ {
		newBlock, newSpendableOuts, err := AddBlock(chain, nextBlock, nextSpends)
		require.NoError(t, err)
		nextBlock = newBlock

		allSpends = append(allSpends, newSpendableOuts...)

		var nextSpendsTmp []*SpendableOut
		for i := 0; i < len(allSpends); i++ {
			randIdx := rand.Intn(len(allSpends))

			spend := allSpends[randIdx]                                       // get
			allSpends = append(allSpends[:randIdx], allSpends[randIdx+1:]...) // delete
			nextSpendsTmp = append(nextSpendsTmp, spend)
		}
		nextSpends = nextSpendsTmp

		if b%10 == 0 {
			// Commit the two base blocks to DB
			err := chain.FlushUtxoCache(FlushRequired)
			require.NoError(t, err, "flush utxo cache")
		}
	}

	// We'll start adding a different chain starting from block 1. Once we reach block 102,
	// we'll switch over to this chain.
	altBlocks := make([]*btcutil.Block, 110)
	var altSpends []*SpendableOut
	altNextSpends := spendableOuts1
	altNextBlock := b1
	for i := range altBlocks {
		var newSpends []*SpendableOut
		altBlocks[i], newSpends, err = AddBlock(chain, altNextBlock, altNextSpends)
		require.NoError(t, err)
		altNextBlock = altBlocks[i]

		altSpends = append(altSpends, newSpends...)

		var nextSpendsTmp []*SpendableOut
		for i := 0; i < len(altSpends); i++ {
			randIdx := rand.Intn(len(altSpends))

			spend := altSpends[randIdx]                                       // get
			altSpends = append(altSpends[:randIdx], altSpends[randIdx+1:]...) // delete
			nextSpendsTmp = append(nextSpendsTmp, spend)
		}
		altNextSpends = nextSpendsTmp
	}
}

// proofStoreCountingDB counts proof writes made by the chain.
type proofStoreCountingDB struct {
	database.DB
	stores int
}

// Update runs fn with a transaction that counts proof store writes.
func (db *proofStoreCountingDB) Update(fn func(database.Tx) error) error {
	return db.DB.Update(func(dbTx database.Tx) error {
		return fn(&proofStoreCountingTx{Tx: dbTx, db: db})
	})
}

// proofStoreCountingTx counts proof store writes made through the
// transaction.
type proofStoreCountingTx struct {
	database.Tx
	db *proofStoreCountingDB
}

// StoreUtreexoProof counts the write and forwards it to the wrapped
// transaction.
func (tx *proofStoreCountingTx) StoreUtreexoProof(hash *chainhash.Hash,
	proof []byte) error {

	tx.db.stores++
	return tx.Tx.StoreUtreexoProof(hash, proof)
}

// countingUtreexoTestChain creates a compact-state chain whose proof store
// writes are counted.
func countingUtreexoTestChain(t *testing.T, name string) (*BlockChain,
	*chaincfg.Params, *proofStoreCountingDB, func()) {

	t.Helper()

	params := chaincfg.RegressionNetParams
	dbPath := filepath.Join(t.TempDir(), name)
	db, err := database.Create(testDbType, dbPath, blockDataNet)
	require.NoError(t, err, "Create")

	countingDB := &proofStoreCountingDB{DB: db}
	paramsCopy := params
	chain, err := New(&Config{
		DB:          countingDB,
		ChainParams: &paramsCopy,
		TimeSource:  NewMedianTime(),
		SigCache:    txscript.NewSigCache(1000),
		UtreexoView: NewUtreexoViewpoint(),
	})
	if err != nil {
		db.Close()
		require.NoError(t, err, "New")
	}
	chain.TstSetCoinbaseMaturity(1)

	return chain, &paramsCopy, countingDB, func() {
		db.Close()
	}
}

// spendableOutsFromLeaves converts the accumulator leaves into the
// spendable outs consumed by NewBlock.
func spendableOutsFromLeaves(leaves []wire.LeafData) []*SpendableOut {
	spends := make([]*SpendableOut, len(leaves))
	for i := range leaves {
		leaf := leaves[i]
		spends[i] = &SpendableOut{
			PrevOut: leaf.OutPoint,
			Amount:  btcutil.Amount(leaf.Amount),
		}
	}
	return spends
}

// testUtreexoProofState tracks the accumulator state for one branch.
type testUtreexoProofState struct {
	uView *UtreexoViewpoint
}

// newTestUtreexoProofState returns a test state with a fresh accumulator.
func newTestUtreexoProofState() *testUtreexoProofState {
	return &testUtreexoProofState{
		uView: NewUtreexoViewpoint(),
	}
}

// newBlock builds a block on top of prev that spends the provided leaves
// and attaches a valid proof for the tracked state.  It returns the block
// along with the accumulator leaves it creates.
func (s *testUtreexoProofState) newBlock(t *testing.T, chain *BlockChain,
	prev *btcutil.Block, spends []wire.LeafData) (*btcutil.Block,
	[]wire.LeafData) {

	t.Helper()

	block, _ := NewBlock(chain, prev, spendableOutsFromLeaves(spends))
	adds := s.attachUData(t, block, spends)

	return block, adds
}

// attachUData proves the block and advances the tracked branch state.
func (s *testUtreexoProofState) attachUData(t *testing.T, block *btcutil.Block,
	spends []wire.LeafData) []wire.LeafData {

	t.Helper()

	udata, err := wire.GenerateUData(spends, &s.uView.accumulator)
	require.NoError(t, err, "GenerateUData")
	block.SetUtreexoData(udata)

	adds := ExtractAccumulatorAdds(block)
	addLeaves := make([]utreexo.Leaf, len(adds))
	for i := range adds {
		addLeaves[i] = utreexo.Leaf{
			Hash:     adds[i].LeafHash(),
			Remember: true,
		}
	}

	delHashes := make([]utreexo.Hash, len(spends))
	for i := range spends {
		delHashes[i] = spends[i].LeafHash()
	}

	_, err = s.uView.Modify(udata, addLeaves, delHashes)
	require.NoError(t, err, "Modify proof state for %s", block.Hash())

	return adds
}

// processBlock runs the block through ProcessBlock and asserts it is
// accepted without being orphaned, on the main chain when wantMainChain is
// true.
func processBlock(t *testing.T, chain *BlockChain, block *btcutil.Block,
	wantMainChain bool) {

	t.Helper()

	gotMainChain, gotOrphan, err := chain.ProcessBlock(block, BFNone)
	require.NoError(t, err, "ProcessBlock %s", block.Hash())
	require.False(t, gotOrphan, "ProcessBlock %s returned orphan",
		block.Hash())
	require.Equal(t, wantMainChain, gotMainChain, "ProcessBlock %s main "+
		"chain", block.Hash())
}

// assertStoredUtreexoProof ensures the proof attached to block is present
// in the proof store and matches it.
func assertStoredUtreexoProof(t *testing.T, chain *BlockChain,
	block *btcutil.Block) {

	t.Helper()

	var want bytes.Buffer
	err := block.UtreexoData().Serialize(&want)
	require.NoError(t, err, "Serialize udata for %s", block.Hash())

	err = chain.db.View(func(dbTx database.Tx) error {
		got, err := dbTx.FetchUtreexoProof(block.Hash())
		if err != nil {
			return err
		}
		require.NotNil(t, got, "no stored utreexo proof for %s",
			block.Hash())
		require.Equal(t, want.Bytes(), got, "stored utreexo proof "+
			"mismatch for %s", block.Hash())
		return nil
	})
	require.NoError(t, err, "FetchUtreexoProof")
}

// TestInvalidUtreexoProofIsNotStored ensures rejected proofs are not persisted.
func TestInvalidUtreexoProofIsNotStored(t *testing.T) {
	tests := []struct {
		name      string
		flags     BehaviorFlags
		sideChain bool
	}{
		{name: "main chain"},
		{name: "main chain fast add", flags: BFFastAdd},
		{name: "side chain", sideChain: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			chain, params, _, tearDown := countingUtreexoTestChain(t,
				"invalid-proof")
			defer tearDown()

			genesis := btcutil.NewBlock(params.GenesisBlock)
			genesis.SetHeight(0)
			mainProofState := newTestUtreexoProofState()
			branchProofState := newTestUtreexoProofState()

			forkBlock, forkAdds := mainProofState.newBlock(t, chain, genesis, nil)
			branchProofState.attachUData(t, forkBlock, nil)
			processBlock(t, chain, forkBlock, true)

			if test.sideChain {
				mainBlock, _ := mainProofState.newBlock(t, chain, forkBlock, nil)
				processBlock(t, chain, mainBlock, true)
			}

			// Corrupt the proof by changing a committed leaf amount.
			block, _ := branchProofState.newBlock(t, chain, forkBlock, forkAdds[:1])
			ud := *block.UtreexoData()
			ud.LeafDatas = append([]wire.LeafData(nil), ud.LeafDatas...)
			ud.LeafDatas[0].Amount++
			block.SetUtreexoData(&ud)

			_, _, err := chain.ProcessBlock(block, test.flags)
			require.Error(t, err, "ProcessBlock accepted an invalid utreexo proof")

			var hasBlock bool
			var proof []byte
			err = chain.db.View(func(dbTx database.Tx) error {
				var err error
				hasBlock, err = dbTx.HasBlock(block.Hash())
				if err != nil {
					return err
				}
				proof, err = dbTx.FetchUtreexoProof(block.Hash())
				proof = bytes.Clone(proof)
				return err
			})
			require.NoError(t, err)
			require.False(t, hasBlock, "block stored with an invalid utreexo proof")
			require.Nil(t, proof)
		})
	}
}

// TestStoreMainChainUtreexoProof ensures a main-chain proof is stored once.
func TestStoreMainChainUtreexoProof(t *testing.T) {
	chain, params, countingDB, tearDown := countingUtreexoTestChain(t,
		"main-proof")
	defer tearDown()

	genesis := btcutil.NewBlock(params.GenesisBlock)
	genesis.SetHeight(0)
	proofState := newTestUtreexoProofState()
	block, _ := proofState.newBlock(t, chain, genesis, nil)

	processBlock(t, chain, block, true)
	require.Equal(t, 1, countingDB.stores)
	assertStoredUtreexoProof(t, chain, block)
}

// TestUtreexoProofStoreReplaysForkPointBranch ensures validation replays stored
// branch proofs from the fork point.
func TestUtreexoProofStoreReplaysForkPointBranch(t *testing.T) {
	target, params, _, tearDownTarget := countingUtreexoTestChain(t,
		"target")
	defer tearDownTarget()

	genesis := btcutil.NewBlock(params.GenesisBlock)
	genesis.SetHeight(0)
	mainProofState := newTestUtreexoProofState()
	sideProofState := newTestUtreexoProofState()

	// Build the shared fork point and apply it to both proof states.
	forkBlock, forkAdds := mainProofState.newBlock(t, target, genesis, nil)
	sideProofState.attachUData(t, forkBlock, nil)
	processBlock(t, target, forkBlock, true)

	// Keep extending only the target chain so later blocks from the fork
	// remain detached side-chain blocks there.
	mainTip := forkBlock
	for i := 0; i < 3; i++ {
		block, _ := mainProofState.newBlock(t, target, mainTip, nil)
		processBlock(t, target, block, true)
		mainTip = block
	}

	// Build the side branch against its own accumulator state.
	side1, side1Adds := sideProofState.newBlock(t, target, forkBlock,
		forkAdds[:1])
	processBlock(t, target, side1, false)
	assertStoredUtreexoProof(t, target, side1)

	// Spend a side1 output so side2 validation must replay side1.
	side2Spends := side1Adds[:1]
	side2, _ := sideProofState.newBlock(t, target, side1, side2Spends)
	require.Equal(t, side1Adds[0].OutPoint, side2Spends[0].OutPoint,
		"side2 must spend side1's coinbase")
	processBlock(t, target, side2, false)
	assertStoredUtreexoProof(t, target, side2)

	side2Node := target.index.LookupNode(side2.Hash())
	require.NotNil(t, side2Node, "side chain block %s not indexed",
		side2.Hash())
	require.False(t, target.bestChain.Contains(side2Node),
		"side chain block %s unexpectedly became best chain",
		side2.Hash())
}

// TestUtreexoProofStoreReorgWritesOnce ensures reorgs do not rewrite stored
// proofs.
func TestUtreexoProofStoreReorgWritesOnce(t *testing.T) {
	chain, params, countingDB, tearDown := countingUtreexoTestChain(t,
		"reorg-proof")
	defer tearDown()

	genesis := btcutil.NewBlock(params.GenesisBlock)
	genesis.SetHeight(0)
	mainProofState := newTestUtreexoProofState()
	sideProofState := newTestUtreexoProofState()

	forkBlock, forkAdds := mainProofState.newBlock(t, chain, genesis, nil)
	sideProofState.attachUData(t, forkBlock, nil)
	processBlock(t, chain, forkBlock, true)

	mainTip := forkBlock
	for i := 0; i < 2; i++ {
		block, _ := mainProofState.newBlock(t, chain, mainTip, nil)
		processBlock(t, chain, block, true)
		mainTip = block
	}

	// Build a two-block side chain below the main chain's work.
	side1, side1Adds := sideProofState.newBlock(t, chain, forkBlock,
		forkAdds[:1])
	processBlock(t, chain, side1, false)
	side2, side2Adds := sideProofState.newBlock(t, chain, side1,
		side1Adds[:1])
	processBlock(t, chain, side2, false)

	// The third side-chain block triggers a reorg and one new proof write.
	storesBeforeReorg := countingDB.stores
	side3, _ := sideProofState.newBlock(t, chain, side2, side2Adds[:1])
	processBlock(t, chain, side3, true)
	require.Equal(t, 1, countingDB.stores-storesBeforeReorg,
		"proof store calls during reorg")
	for _, block := range []*btcutil.Block{side1, side2, side3} {
		assertStoredUtreexoProof(t, chain, block)
	}

	// Detaching and reattaching the branch should reuse its stored proofs.
	storesAfterReorg := countingDB.stores
	err := chain.InvalidateBlock(side1.Hash())
	require.NoError(t, err, "InvalidateBlock")
	require.Equal(t, *mainTip.Hash(), chain.bestChain.Tip().hash,
		"tip after invalidation")
	require.Equal(t, storesAfterReorg, countingDB.stores,
		"invalidation stored proofs")

	err = chain.ReconsiderBlock(side1.Hash())
	require.NoError(t, err, "ReconsiderBlock")
	require.Equal(t, *side3.Hash(), chain.bestChain.Tip().hash,
		"tip after reconsideration")
	require.Equal(t, storesAfterReorg, countingDB.stores,
		"reconsideration stored proofs")
}
