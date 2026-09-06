// Copyright (c) 2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This file is ignored during the regular tests due to the following build tag.
//go:build rpctest
// +build rpctest

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/integration/rpctest"
	"github.com/utreexo/utreexod/rpcclient"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
)

func testGetBestBlock(r *rpctest.Harness, t *testing.T) {
	_, prevbestHeight, err := r.Client.GetBestBlock()
	if err != nil {
		t.Fatalf("Call to `getbestblock` failed: %v", err)
	}

	// Create a new block connecting to the current tip.
	generatedBlockHashes, err := r.Client.Generate(1)
	if err != nil {
		t.Fatalf("Unable to generate block: %v", err)
	}

	bestHash, bestHeight, err := r.Client.GetBestBlock()
	if err != nil {
		t.Fatalf("Call to `getbestblock` failed: %v", err)
	}

	// Hash should be the same as the newly submitted block.
	if !bytes.Equal(bestHash[:], generatedBlockHashes[0][:]) {
		t.Fatalf("Block hashes do not match. Returned hash %v, wanted "+
			"hash %v", bestHash, generatedBlockHashes[0][:])
	}

	// Block height should now reflect newest height.
	if bestHeight != prevbestHeight+1 {
		t.Fatalf("Block heights do not match. Got %v, wanted %v",
			bestHeight, prevbestHeight+1)
	}
}

func testGetBlockCount(r *rpctest.Harness, t *testing.T) {
	// Save the current count.
	currentCount, err := r.Client.GetBlockCount()
	if err != nil {
		t.Fatalf("Unable to get block count: %v", err)
	}

	if _, err := r.Client.Generate(1); err != nil {
		t.Fatalf("Unable to generate block: %v", err)
	}

	// Count should have increased by one.
	newCount, err := r.Client.GetBlockCount()
	if err != nil {
		t.Fatalf("Unable to get block count: %v", err)
	}
	if newCount != currentCount+1 {
		t.Fatalf("Block count incorrect. Got %v should be %v",
			newCount, currentCount+1)
	}
}

func testGetBlockHash(r *rpctest.Harness, t *testing.T) {
	// Create a new block connecting to the current tip.
	generatedBlockHashes, err := r.Client.Generate(1)
	if err != nil {
		t.Fatalf("Unable to generate block: %v", err)
	}

	info, err := r.Client.GetInfo()
	if err != nil {
		t.Fatalf("call to getinfo cailed: %v", err)
	}

	blockHash, err := r.Client.GetBlockHash(int64(info.Blocks))
	if err != nil {
		t.Fatalf("Call to `getblockhash` failed: %v", err)
	}

	// Block hashes should match newly created block.
	if !bytes.Equal(generatedBlockHashes[0][:], blockHash[:]) {
		t.Fatalf("Block hashes do not match. Returned hash %v, wanted "+
			"hash %v", blockHash, generatedBlockHashes[0][:])
	}
}

func testBulkClient(r *rpctest.Harness, t *testing.T) {
	// Create a new block connecting to the current tip.
	generatedBlockHashes, err := r.Client.Generate(20)
	if err != nil {
		t.Fatalf("Unable to generate block: %v", err)
	}

	var futureBlockResults []rpcclient.FutureGetBlockResult
	for _, hash := range generatedBlockHashes {
		futureBlockResults = append(futureBlockResults, r.BatchClient.GetBlockAsync(hash))
	}

	err = r.BatchClient.Send()
	if err != nil {
		t.Fatal(err)
	}

	isKnownBlockHash := func(blockHash chainhash.Hash) bool {
		for _, hash := range generatedBlockHashes {
			if blockHash.IsEqual(hash) {
				return true
			}
		}
		return false
	}

	for _, block := range futureBlockResults {
		msgBlock, err := block.Receive()
		if err != nil {
			t.Fatal(err)
		}
		blockHash := msgBlock.Header.BlockHash()
		if !isKnownBlockHash(blockHash) {
			t.Fatalf("expected hash %s  to be in generated hash list", blockHash)
		}
	}

}

var rpcTestCases = []rpctest.HarnessTestCase{
	testGetBestBlock,
	testGetBlockCount,
	testGetBlockHash,
	testBulkClient,
}

var primaryHarness *rpctest.Harness

func TestMain(m *testing.M) {
	var err error

	// In order to properly test scenarios on as if we were on mainnet,
	// ensure that non-standard transactions aren't accepted into the
	// mempool or relayed.
	btcdCfg := []string{"--rejectnonstd", "--noutreexo"}
	primaryHarness, err = rpctest.New(
		&chaincfg.SimNetParams, nil, btcdCfg, "",
	)
	if err != nil {
		fmt.Println("unable to create primary harness: ", err)
		os.Exit(1)
	}

	// Initialize the primary mining node with a chain of length 125,
	// providing 25 mature coinbases to allow spending from for testing
	// purposes.
	if err := primaryHarness.SetUp(true, 25); err != nil {
		fmt.Println("unable to setup test chain: ", err)

		// Even though the harness was not fully setup, it still needs
		// to be torn down to ensure all resources such as temp
		// directories are cleaned up.  The error is intentionally
		// ignored since this is already an error path and nothing else
		// could be done about it anyways.
		_ = primaryHarness.TearDown()
		os.Exit(1)
	}

	exitCode := m.Run()

	// Clean up any active harnesses that are still currently running.This
	// includes removing all temporary directories, and shutting down any
	// created processes.
	if err := rpctest.TearDownAll(); err != nil {
		fmt.Println("unable to tear down all harnesses: ", err)
		os.Exit(1)
	}

	os.Exit(exitCode)
}

func TestRpcServer(t *testing.T) {
	var currentTestNum int
	defer func() {
		// If one of the integration tests caused a panic within the main
		// goroutine, then tear down all the harnesses in order to avoid
		// any leaked btcd processes.
		if r := recover(); r != nil {
			fmt.Println("recovering from test panic: ", r)
			if err := rpctest.TearDownAll(); err != nil {
				fmt.Println("unable to tear down all harnesses: ", err)
			}
			t.Fatalf("test #%v panicked: %s", currentTestNum, debug.Stack())
		}
	}()

	for _, testCase := range rpcTestCases {
		testCase(primaryHarness, t)

		currentTestNum++
	}
}

// newHarness creates a node with the given args, sets up its test chain, and
// registers teardown.
func newHarness(t *testing.T, args ...string) *rpctest.Harness {
	t.Helper()

	h, err := rpctest.New(&chaincfg.RegressionNetParams, nil, args, "")
	if err != nil {
		t.Fatalf("unable to create harness: %v", err)
	}
	if err := h.SetUp(true, 0); err != nil {
		t.Fatalf("unable to set up harness: %v", err)
	}
	t.Cleanup(func() { h.TearDown() })
	return h
}

// getUtreexoProofErr issues getutreexoproof through the low-level RawRequest so
// the actual RPC error is returned.  The typed GetUtreexoProof client wrapper
// masks server errors behind a generic JSON decode error.
func getUtreexoProofErr(t *testing.T, harness *rpctest.Harness, hash *chainhash.Hash) error {
	t.Helper()

	hashParam, err := json.Marshal(hash.String())
	if err != nil {
		t.Fatalf("unable to marshal hash param: %v", err)
	}
	verbosityParam, err := json.Marshal(1)
	if err != nil {
		t.Fatalf("unable to marshal verbosity param: %v", err)
	}

	_, err = harness.Client.RawRequest("getutreexoproof",
		[]json.RawMessage{hashParam, verbosityParam})
	return err
}

// buildChainWithSpends mines enough blocks for the coinbase outputs to mature
// and then mines numSpendBlocks blocks that each include a spend, so the
// resulting blocks carry non-empty utreexo proofs.  It returns the ordered
// hashes of every block above genesis.
func buildChainWithSpends(t *testing.T, bridge *rpctest.Harness, numSpendBlocks int) []*chainhash.Hash {
	t.Helper()

	const numMaturityBlocks = 110
	allHashes, err := bridge.Client.Generate(numMaturityBlocks)
	if err != nil {
		t.Fatalf("unable to generate maturity blocks: %v", err)
	}

	for i := 0; i < numSpendBlocks; i++ {
		addr, err := bridge.NewAddress()
		if err != nil {
			t.Fatalf("unable to get new address: %v", err)
		}
		addrScript, err := txscript.PayToAddrScript(addr)
		if err != nil {
			t.Fatalf("unable to generate pkscript: %v", err)
		}
		output := wire.NewTxOut(int64(btcutil.SatoshiPerBitcoin), addrScript)
		if _, err := bridge.SendOutputs([]*wire.TxOut{output}, 10); err != nil {
			t.Fatalf("unable to send outputs: %v", err)
		}

		spendHashes, err := bridge.Client.Generate(1)
		if err != nil {
			t.Fatalf("unable to generate block with spend: %v", err)
		}
		allHashes = append(allHashes, spendHashes...)
	}

	return allHashes
}

// TestBuildChainWithSpends checks that buildChainWithSpends actually mines a
// spend.  The proof comparisons rely on it to produce non-empty proofs, which
// would otherwise silently pass comparing only empty (coinbase only) proofs.
func TestBuildChainWithSpends(t *testing.T) {
	node := newHarness(t, "--noutreexo", "--nobdkwallet")

	sawSpend := false
	for _, blockHash := range buildChainWithSpends(t, node, 1) {
		block, err := node.Client.GetBlock(blockHash)
		if err != nil {
			t.Fatalf("unable to get block %s: %v", blockHash, err)
		}
		// A block with no spends holds only the coinbase, so any additional
		// transaction is a spend.
		if len(block.Transactions) > 1 {
			sawSpend = true
			break
		}
	}

	if !sawSpend {
		t.Fatal("buildChainWithSpends produced no block with a spend")
	}
}

// assertProofsMatch checks that each harness in others serves the same proof as
// ref for every block.
func assertProofsMatch(t *testing.T, blockHashes []*chainhash.Hash,
	ref *rpctest.Harness, others map[string]*rpctest.Harness) {

	t.Helper()

	for i, blockHash := range blockHashes {
		want, err := ref.Client.GetUtreexoProof(blockHash)
		if err != nil {
			t.Fatalf("reference GetUtreexoProof failed at height %d: %v", i+1, err)
		}

		for name, h := range others {
			got, err := h.Client.GetUtreexoProof(blockHash)
			if err != nil {
				t.Fatalf("%s GetUtreexoProof failed at height %d: %v", name, i+1, err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("%s proof mismatch at height %d.\nwant: %+v\ngot:  %+v",
					name, i+1, want, got)
			}
		}
	}
}

// TestGetUtreexoProof checks that a utreexoproofindex node, a
// flatutreexoproofindex node, and a compact state node all serve the same
// proof.  The index paths guard against regressions and the CSN path fetches
// straight from the utreexo view.
func TestGetUtreexoProof(t *testing.T) {
	utreexoProofNode := newHarness(t, "--utreexoproofindex", "--noutreexo", "--nobdkwallet", "--prune=0")
	flatUtreexoProofNode := newHarness(t, "--flatutreexoproofindex", "--noutreexo", "--nobdkwallet", "--prune=0")
	csn := newHarness(t, "--nobdkwallet")

	// The flatutreexoproofindex node generates the chain (with spends so the
	// proofs aren't all empty) and the other two nodes are synced from it.
	blockHashes := buildChainWithSpends(t, flatUtreexoProofNode, 10)
	blocks, udatas, err := fetchBlocks(blockHashes, flatUtreexoProofNode)
	if err != nil {
		t.Fatalf("unable to fetch blocks with udata: %v", err)
	}
	for i, block := range blocks {
		if err := utreexoProofNode.Client.SubmitBlock(block, nil); err != nil {
			t.Fatalf("unable to submit block %d to the utreexo proof node: %v", i+1, err)
		}
		if err := csn.Client.SubmitBlockAndUtreexoProof(block, udatas[i]); err != nil {
			t.Fatalf("unable to submit block %d to the csn: %v", i+1, err)
		}
	}

	assertProofsMatch(t, blockHashes, flatUtreexoProofNode, map[string]*rpctest.Harness{
		"utreexoproofindex": utreexoProofNode,
		"csn":               csn,
	})

	// Genesis has no proof and an unknown block isn't in the chain.  Both must
	// return a clean error rather than panic.
	genesisHash, err := flatUtreexoProofNode.Client.GetBlockHash(0)
	if err != nil {
		t.Fatalf("unable to get genesis hash: %v", err)
	}
	var unknownHash chainhash.Hash
	for _, h := range []*rpctest.Harness{utreexoProofNode, flatUtreexoProofNode, csn} {
		if err := getUtreexoProofErr(t, h, genesisHash); err == nil ||
			!strings.Contains(err.Error(), "genesis") {
			t.Fatalf("expected a genesis error but got: %v", err)
		}
		if err := getUtreexoProofErr(t, h, &unknownHash); err == nil ||
			!strings.Contains(err.Error(), "couldn't be fetched") {
			t.Fatalf("expected a height lookup error but got: %v", err)
		}
	}
}

// TestGetUtreexoProofDisabled checks that getutreexoproof is rejected on a node
// that has neither a utreexo proof index nor an active utreexo view.
func TestGetUtreexoProofDisabled(t *testing.T) {
	fullNode := newHarness(t, "--noutreexo", "--nobdkwallet")

	// The guard runs before the block lookup, so any hash triggers it.
	genesisHash, err := fullNode.Client.GetBlockHash(0)
	if err != nil {
		t.Fatalf("unable to get genesis hash: %v", err)
	}
	if err := getUtreexoProofErr(t, fullNode, genesisHash); err == nil ||
		!strings.Contains(err.Error(), "must be enabled") {
		t.Fatalf("expected the disabled error but got: %v", err)
	}
}

// TestGetUtreexoProofPrunedCSN checks that a compact state node running with
// pruning enabled still serves the same proofs as an archival bridge for the
// blocks it retains.  It does not exercise the deleted block path: no block is
// actually pruned at this block count, since ffldb only deletes whole 128 MiB
// block files and always keeps the last 288 blocks.  The deleted block behavior
// that getutreexoproof relies on is covered by blockchain.TestBlockByHashPruned.
func TestGetUtreexoProofPrunedCSN(t *testing.T) {
	bridge := newHarness(t, "--flatutreexoproofindex", "--noutreexo", "--nobdkwallet", "--prune=0")
	prunedCSN := newHarness(t, "--nobdkwallet", "--prune=550")

	// This only confirms the node came up with pruning enabled.  Nothing is
	// deleted at this scale.
	info, err := prunedCSN.Client.GetBlockChainInfo()
	if err != nil {
		t.Fatalf("unable to get chain info: %v", err)
	}
	if !info.Pruned {
		t.Fatal("expected the csn to have pruning enabled")
	}

	blockHashes := buildChainWithSpends(t, bridge, 10)
	blocks, udatas, err := fetchBlocks(blockHashes, bridge)
	if err != nil {
		t.Fatalf("unable to fetch blocks with udata: %v", err)
	}
	for i, block := range blocks {
		if err := prunedCSN.Client.SubmitBlockAndUtreexoProof(block, udatas[i]); err != nil {
			t.Fatalf("unable to submit block %d to the pruned csn: %v", i+1, err)
		}
	}

	assertProofsMatch(t, blockHashes, bridge, map[string]*rpctest.Harness{
		"pruned csn": prunedCSN,
	})
}
