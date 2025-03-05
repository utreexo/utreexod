// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2018-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"fmt"

	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/wire"
)

// maybeAcceptBlock potentially accepts a block into the block chain and, if
// accepted, returns whether or not it is on the main chain.  It performs
// several validation checks which depend on its position within the block chain
// before adding it.  The block is expected to have already gone through
// ProcessBlock before calling this function with it.
//
// The flags are also passed to checkBlockContext and connectBestChain.  See
// their documentation for how the flags modify their behavior.
//
// This function MUST be called with the chain state lock held (for writes).
func (b *BlockChain) maybeAcceptBlock(block *btcutil.Block, flags BehaviorFlags) (bool, error) {
	// The height of this block is one more than the referenced previous
	// block.
	prevHash := &block.MsgBlock().Header.PrevBlock
	prevNode := b.index.LookupNode(prevHash)
	if prevNode == nil {
		str := fmt.Sprintf("previous block %s is unknown", prevHash)
		return false, ruleError(ErrPreviousBlockUnknown, str)
	} else if b.index.NodeStatus(prevNode).KnownInvalid() {
		str := fmt.Sprintf("previous block %s is known to be invalid", prevHash)
		return false, ruleError(ErrInvalidAncestorBlock, str)
	}

	blockHeight := prevNode.height + 1
	block.SetHeight(blockHeight)

	// The block must pass all of the validation rules which depend on the
	// position of the block within the block chain.
	err := b.checkBlockContext(block, prevNode, flags)
	if err != nil {
		return false, err
	}

	// Insert the block into the database if it's not already there.  Even
	// though it is possible the block will ultimately fail to connect, it
	// has already passed all proof-of-work and validity tests which means
	// it would be prohibitively expensive for an attacker to fill up the
	// disk with a bunch of blocks that fail to connect.  This is necessary
	// since it allows block download to be decoupled from the much more
	// expensive connection logic.  It also has some other nice properties
	// such as making blocks that never become part of the main chain or
	// blocks that fail to connect available for further analysis.
	err = b.db.Update(func(dbTx database.Tx) error {
		return dbStoreBlock(dbTx, block)
	})
	if err != nil {
		return false, err
	}

	// Check to see if we already have the blocknode in our index.
	node := b.index.LookupNode(block.Hash())
	if node == nil {
		// Create a new block node for the block and add it to the node index. Even
		// if the block ultimately gets connected to the main chain, it starts out
		// on a side chain.
		blockHeader := &block.MsgBlock().Header
		newNode := newBlockNode(blockHeader, prevNode)
		newNode.status = statusDataStored

		b.index.AddNode(newNode)
		node = newNode
	} else {
		// If we already have it, then just set the status as data stored.
		b.index.SetStatusFlags(node, statusDataStored)
	}
	err = b.index.flushToDB()
	if err != nil {
		return false, err
	}

	// Connect the passed block to the chain while respecting proper chain
	// selection according to the chain with the most proof of work.  This
	// also handles validation of the transaction scripts.
	isMainChain, err := b.connectBestChain(node, block, flags)
	if err != nil {
		return false, err
	}

	// Notify the caller that the new block was accepted into the block
	// chain.  The caller would typically want to react by relaying the
	// inventory to other peers.
	b.chainLock.Unlock()
	b.sendNotification(NTBlockAccepted, block)
	b.chainLock.Lock()

	return isMainChain, nil
}

// maybeAcceptBlockHeader potentially accepts the header to the block index and,
// if accepted, returns the block node associated with the header.  It performs
// several context independent checks as well as those which depend on its
// position within the chain.
//
// The flag for check header sanity allows the additional header sanity checks
// to be skipped which is useful for the full block processing path which checks
// the sanity of the entire block, including the header, before attempting to
// accept its header in order to quickly eliminate blocks that are obviously
// incorrect.
//
// In the case the block header is already known, the associated block node is
// examined to determine if the block is already known to be invalid, in which
// case an appropriate error will be returned.
//
// The returned bool indicates if the header extended the best chain of headers.
//
// This function MUST be called with the chain lock held (for writes).
func (b *BlockChain) maybeAcceptBlockHeader(header *wire.BlockHeader, flags BehaviorFlags) (bool, error) {
	// Avoid validating the header again if its validation status is already
	// known.  Invalid headers are never added to the block index, so if there
	// is an entry for the block hash, the header itself is known to be valid.
	// However, it might have since been marked invalid either due to the
	// associated block, or an ancestor, later failing validation.
	hash := header.BlockHash()
	node := b.index.LookupNode(&hash)
	if node != nil {
		if err := b.checkKnownInvalidBlock(node); err != nil {
			return false, err
		}

		// If the node is in the bestHeaders chainview, it's in the main chain.
		// If it isn't, then we'll go through the verification process below.
		if b.bestHeader.Contains(node) {
			return true, nil
		}
	}

	// Perform context-free sanity checks on the block header.
	err := checkBlockHeaderSanity(header, b.chainParams.PowLimit, b.timeSource, flags)
	if err != nil {
		return false, err
	}

	// Orphan headers are not allowed and this function should never be called
	// with the genesis block.
	prevHash := &header.PrevBlock
	prevNode := b.index.LookupNode(prevHash)
	if prevNode == nil {
		str := fmt.Sprintf("previous block %s is not known", prevHash)
		return false, ruleError(ErrMissingParent, str)
	}

	// There is no need to validate the header if an ancestor is already known
	// to be invalid.
	prevNodeStatus := b.index.NodeStatus(prevNode)
	if prevNodeStatus.KnownInvalid() {
		str := fmt.Sprintf("previous block %s is known to be invalid", prevHash)
		return false, ruleError(ErrInvalidAncestorBlock, str)
	}

	// The block must pass all of the validation rules which depend on the
	// position of the block within the block chain.
	err = b.checkBlockHeaderContext(header, prevNode, flags)
	if err != nil {
		return false, err
	}

	// Create a new block node for the block and add it to the block index.
	//
	// Note that the additional information for the actual transactions and
	// witnesses in the block can't be populated until the full block data is
	// known since that information is not available in the header.
	if node == nil {
		node = newBlockNode(header, prevNode)
		b.index.AddNode(node)
	}

	// Check if the header extends the best header tip.
	isMainChain := false
	parentHash := &header.PrevBlock
	if parentHash.IsEqual(&b.bestHeader.Tip().hash) {
		log.Debugf("accepted header %v as the new header tip", node.hash)

		// This header is now the end of the best headers.
		b.bestHeader.SetTip(node)
		isMainChain = true
		return isMainChain, nil
	}

	// We're extending (or creating) a side chain, but the cumulative
	// work for this new side chain is not enough to make it the new chain.
	if node.workSum.Cmp(b.bestHeader.Tip().workSum) <= 0 {
		// Log information about how the header is forking the chain.
		fork := b.bestHeader.FindFork(node)
		if fork.hash.IsEqual(parentHash) {
			log.Infof("FORK: BlockHeader %v(%v) forks the chain at block %v(%v) "+
				"but did not have enough work to be the "+
				"main chain", node.hash, node.height, fork.hash, fork.height)
		} else {
			log.Infof("EXTEND FORK: BlockHeader %v(%v) extends a side chain "+
				"which forks the chain at block %v(%v)",
				node.hash, node.height, fork.hash, fork.height)
		}

		return false, nil
	}

	prevTip := b.bestHeader.Tip()
	log.Infof("NEW BEST HEADER CHAIN: BlockHeader %v(%v) is now a longer "+
		"PoW chain than the previous header tip of %v(%v).",
		node.hash, node.height,
		prevTip.hash, prevTip.height)

	b.bestHeader.SetTip(node)
	isMainChain = true

	return isMainChain, nil
}
