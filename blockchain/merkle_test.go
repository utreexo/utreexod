// Copyright (c) 2013-2017 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"fmt"
	"testing"

	"github.com/utreexo/utreexod/btcutil"
)

// TestMerkle tests the BuildMerkleTreeStore API.
func TestMerkle(t *testing.T) {
	block := btcutil.NewBlock(&Block100000)
	merkles := BuildMerkleTreeStore(block.Transactions(), false)
	calculatedMerkleRoot := merkles[len(merkles)-1]
	wantMerkle := &Block100000.Header.MerkleRoot
	if !wantMerkle.IsEqual(calculatedMerkleRoot) {
		t.Errorf("BuildMerkleTreeStore: merkle root mismatch - "+
			"got %v, want %v", calculatedMerkleRoot, wantMerkle)
	}
}

func TestExtractMerkleBranch(t *testing.T) {
	tests := []struct {
		getBlock func() *btcutil.Block
	}{
		{
			getBlock: func() *btcutil.Block { return btcutil.NewBlock(&Block100000) },
		},
		{
			getBlock: func() *btcutil.Block {
				testBlockNum := 277647
				blockDataFile := fmt.Sprintf("%d.dat.bz2", testBlockNum)
				blocks, err := loadBlocks(blockDataFile)
				if err != nil {
					t.Fatalf("Error loading file: %v\n", err)
				}
				return blocks[0]
			},
		},
	}

	for i, test := range tests {
		txs := test.getBlock().Transactions()
		merkles := BuildMerkleTreeStore(txs, false)
		expectedMerkleRoot := merkles[len(merkles)-1]

		for idx := 0; idx < len(txs); idx++ {
			// Calculate the root from the extracted relevant merkle branches.
			proveHash := merkles[idx]
			relevantMerkles := ExtractMerkleBranch(merkles, *proveHash)
			gotMerkleRoot := HashMerkleRoot(relevantMerkles, *proveHash, idx)
			if gotMerkleRoot != *expectedMerkleRoot {
				t.Errorf("TestExtractMerkleBranch fail at %d. got %s, expected %s\n",
					i, gotMerkleRoot.String(), expectedMerkleRoot.String())
			}
		}
	}
}
