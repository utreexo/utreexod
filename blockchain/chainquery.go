// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2018-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import "github.com/utreexo/utreexod/chaincfg/chainhash"

// PutNextNeededBlocks populates the provided slice with hashes for the next
// blocks after the current best chain tip that are needed to make progress
// towards the current best known header skipping any blocks that already have
// their data available.
//
// The provided slice will be populated with either as many hashes as it will
// fit per its length or as many hashes it takes to reach best header, whichever
// is smaller.
//
// It returns a sub slice of the provided one with its bounds adjusted to the
// number of entries populated.
//
// This function is safe for concurrent access.
func (b *BlockChain) PutNextNeededBlocks(out []chainhash.Hash) []chainhash.Hash {
	// Nothing to do when no results are requested.
	maxResults := len(out)
	if maxResults == 0 {
		return out[:0]
	}

	b.index.RLock()
	defer b.index.RUnlock()

	// Populate the provided slice by making use of a sliding window.  Note that
	// the needed block hashes are populated in forwards order while it is
	// necessary to walk the block index backwards to determine them.  Further,
	// an unknown number of blocks may already have their data and need to be
	// skipped, so it's not possible to determine the precise height after the
	// fork point to start iterating from.  Using a sliding window efficiently
	// handles these conditions without needing additional allocations.
	//
	// The strategy is to initially determine the common ancestor between the
	// current best chain tip and the current best known header as the starting
	// fork point and move the fork point forward by the window size after
	// populating the output slice with all relevant nodes in the window until
	// either there are no more results or the desired number of results have
	// been populated.
	const windowSize = 32
	var outputIdx int
	var window [windowSize]chainhash.Hash
	bestHeader := b.index.bestHeader
	fork := b.bestChain.FindFork(bestHeader)
	for outputIdx < maxResults && fork != nil && fork != bestHeader {
		// Determine the final descendant block on the branch that leads to the
		// best known header in this window by clamping the number of
		// descendants to consider to the window size.
		endNode := bestHeader
		numBlocksToConsider := endNode.height - fork.height
		if numBlocksToConsider > windowSize {
			endNode = endNode.Ancestor(fork.height + windowSize)
		}

		// Populate the blocks in this window from back to front by walking
		// backwards from the final block to consider in the window to the first
		// one excluding any blocks that already have their data available.
		windowIdx := windowSize
		for node := endNode; node != nil && node != fork; node = node.parent {
			if node.status.HaveData() {
				continue
			}

			windowIdx--
			window[windowIdx] = node.hash
		}

		// Populate the outputs with as many from the back of the window as
		// possible (since the window might not have been fully populated due to
		// skipped blocks) and move the output index forward to match.
		outputIdx += copy(out[outputIdx:], window[windowIdx:])

		// Move the fork point forward to the final block of the window.
		fork = endNode
	}

	return out[:outputIdx]
}

// BestHeader returns the header with the most cumulative work that is NOT
// known to be invalid.
func (b *BlockChain) BestHeader() (chainhash.Hash, int32) {
	b.index.RLock()
	header := b.index.bestHeader
	blockHash := b.index.bestHeader.hash
	height, _ := b.NodeHeightByHash(&blockHash)
	b.index.RUnlock()
	return header.hash, height
}
