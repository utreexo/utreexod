package integration

import (
	"bufio"
	"encoding/hex"
	"os"
	"reflect"
	"testing"

	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/integration/rpctest"
)

func loadTestBlocks(filename string) ([]*btcutil.Block, []*chainhash.Hash, error) {
	// Open the file
	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	// Close the file when the program finishes
	defer file.Close()

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(file)

	blocks := make([]*btcutil.Block, 0, 151)
	blockHashes := make([]*chainhash.Hash, 0, 151)

	// Loop through each line in the file
	for scanner.Scan() {
		line := scanner.Text()
		serializedBlock, err := hex.DecodeString(line)
		if err != nil {
			return nil, nil, err
		}
		block, err := btcutil.NewBlockFromBytes(serializedBlock)
		if err != nil {
			return nil, nil, err
		}
		blocks = append(blocks, block)
		blockHashes = append(blockHashes, block.Hash())
	}

	return blocks, blockHashes, nil
}

func TestGetUtreexoRoots(t *testing.T) {
	fileName := "blocks.txt"
	blocks, blockHashes, err := loadTestBlocks(fileName)
	if err != nil {
		t.Fatalf("failed to load blocks from file \"%s\". %v", fileName, err)
	}

	/*
	 *
	 * Below is just boilerplate for setting up the test harnesses.
	 *
	 */

	// Set up the utreexoproofindex.
	utreexoProofIdxArgs := []string{
		"--utreexoproofindex",
		"--noutreexo",
		"--nobdkwallet",
		"--prune=0",
	}
	utreexoProofNode, err := rpctest.New(&chaincfg.RegressionNetParams, nil, utreexoProofIdxArgs, "")
	if err != nil {
		t.Fatal("TestGetUtreexoRoots fail. Unable to create primary harness: ", err)
	}
	if err := utreexoProofNode.SetUp(true, 0); err != nil {
		t.Fatalf("TestGetUtreexoRoots fail. Unable to setup test chain: %v", err)
	}
	defer utreexoProofNode.TearDown()

	// Set up the flatutreexoproofindex.
	flatUtreexoProofIdxArgs := []string{
		"--flatutreexoproofindex",
		"--noutreexo",
		"--nobdkwallet",
		"--prune=0",
	}
	flatUtreexoProofNode, err := rpctest.New(&chaincfg.RegressionNetParams, nil, flatUtreexoProofIdxArgs, "")
	if err != nil {
		t.Fatal("TestGetUtreexoRoots fail. Unable to create primary harness: ", err)
	}
	if err := flatUtreexoProofNode.SetUp(true, 0); err != nil {
		t.Fatalf("TestGetUtreexoRoots fail. Unable to setup test chain: %v", err)
	}
	defer flatUtreexoProofNode.TearDown()

	// Set up the CSN.
	csn, err := rpctest.New(&chaincfg.RegressionNetParams, nil, []string{"--nobdkwallet"}, "")
	if err != nil {
		t.Fatal("TestGetUtreexoRoots fail. Unable to create primary harness: ", err)
	}
	if err := csn.SetUp(true, 0); err != nil {
		t.Fatalf("TestGetUtreexoRoots fail. Unable to setup test chain: %v", err)
	}
	defer csn.TearDown()

	harnesses := []*rpctest.Harness{utreexoProofNode, flatUtreexoProofNode, csn}

	/*
	 *
	 * Above is just boilerplate for setting up the test harnesses.
	 *
	 */

	for i, block := range blocks {
		// Skip 0 because we already have it.
		if i == 0 {
			continue
		}
		err = utreexoProofNode.Client.SubmitBlock(block, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = flatUtreexoProofNode.Client.SubmitBlock(block, nil)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Sync the CSN. Omit genesis.
	utreexoBlocks, err := fetchBlocks(blockHashes[1:], flatUtreexoProofNode)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range utreexoBlocks {
		err = csn.Client.SubmitBlock(block, nil)
		if err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		blockHeight     int64
		expectNumLeaves uint64
		expectRoots     []string
	}{
		{
			blockHeight:     100,
			expectNumLeaves: 100,
			expectRoots: []string{
				"a2f1e6db842e13c7480c8d80f29ca2db5f9b96e1b428ebfdbd389676d7619081",
				"b21aae30bc74e9aef600a5d507ef27d799b9b6ba08e514656d34d717bdb569d2",
				"bedb648c9a3c5741660f926c1552d83ebb4cb1842cca6855b6d1089bb4951ce1",
			},
		},
		{
			blockHeight:     150,
			expectNumLeaves: 150,
			expectRoots: []string{
				"e00b4ecc7c30865af0ac3b0c7c1b996015f51d6a6577ee6f52cc04b55933eb91",
				"9bf9659f93e246e0431e228032cd4b3a4d8a13e57f3e08a221e61f3e0bae657f",
				"e329a7ddcc888130bb6e4f82ce9f5cf5a712a7b0ae05a1aaf21b363866a9b05e",
				"1864a4982532447dcb3d9a5d2fea9f8ed4e3b1e759d55b8a427fb599fed0c302",
			},
		},
	}

	for _, test := range tests {
		for _, harness := range harnesses {
			hash, err := harness.Client.GetBlockHash(test.blockHeight)
			if err != nil {
				t.Fatal(err)
			}
			state, err := harness.Client.GetUtreexoRoots(hash)
			if err != nil {
				t.Fatal(err)
			}
			if state.NumLeaves != test.expectNumLeaves {
				t.Fatalf("expected numleaves of %d but got %d", test.expectNumLeaves, state.NumLeaves)
			}
			if !reflect.DeepEqual(test.expectRoots, state.Roots) {
				t.Fatalf("expected roots of %s\nbut got %s\n", test.expectRoots, state.Roots)
			}
		}
	}
}
