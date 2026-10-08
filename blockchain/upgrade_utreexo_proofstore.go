// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"bytes"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/database"
)

// blockFileRewriter rewrites the stored block files by transforming each
// block's raw bytes.  It is implemented by databases that can strip the
// legacy inline proof data from block records for the proof store upgrade.
type blockFileRewriter interface {
	RewriteBlockFiles(transform func(hash *chainhash.Hash,
		blockBytes []byte) ([]byte, []byte, error)) error
}

// maybeUpgradeUtreexoProofStore backfills the utreexo proof store from
// the proofs serialized inline with the block bytes by older versions.
// Only utreexo nodes have proofs, so non-utreexo nodes are left alone.
// A database without a recorded version predates the proof store and
// still serializes proofs inline with the block bytes, so the backfill
// runs.  Fresh databases have the current version written when the chain
// state is created and skip the backfill.
func (b *BlockChain) maybeUpgradeUtreexoProofStore(interrupt <-chan struct{}) error {
	if b.utreexoView == nil {
		return nil
	}

	var proofStoreVersion uint32
	err := b.db.View(func(dbTx database.Tx) error {
		proofStoreVersion = dbFetchVersion(dbTx,
			utreexoProofStoreVersionKeyName)
		return nil
	})
	if err != nil {
		return err
	}

	if proofStoreVersion >= utreexoProofStoreVersion {
		return nil
	}

	return b.upgradeUtreexoProofStoreToV1(interrupt)
}

// upgradeUtreexoProofStoreToV1 backfills the utreexo proof store by
// moving each stored block's proof, serialized inline with the block bytes by
// older versions, into the proof store and rewriting the block files without
// it.  It is idempotent and resumable: blocks whose proofs are already in the
// store are not written again and files whose records are all unchanged are
// left alone.  The version is written only after the whole chain has been
// backfilled and stripped.
func (b *BlockChain) upgradeUtreexoProofStoreToV1(interrupt <-chan struct{}) error {
	rewriter, ok := b.db.(blockFileRewriter)
	if !ok {
		return AssertError("database does not support rewriting block " +
			"files")
	}

	bestHeight := b.bestChain.Tip().height
	if bestHeight < 1 {
		// Nothing to backfill (only the genesis block, which has no proof).
		return b.db.Update(func(dbTx database.Tx) error {
			return dbPutVersion(dbTx, utreexoProofStoreVersionKeyName,
				utreexoProofStoreVersion)
		})
	}

	log.Infof("Backfilling the utreexo proof store from inline block proofs "+
		"up to height %d. This is a one-time database migration and may take "+
		"a while...", bestHeight)
	log.Warnf("A hard shutdown, such as kill -9 or a power loss, while a " +
		"block file is being rewritten can corrupt the database and " +
		"require a resync.")

	// Parse the proof serialized inline with each block and hand the
	// stripped block bytes and the proof to the database, which rewrites
	// the block files and stores the proofs.
	transform := func(hash *chainhash.Hash, blockBytes []byte) ([]byte,
		[]byte, error) {

		if interruptRequested(interrupt) {
			return nil, nil, errInterruptRequested
		}

		blk, err := btcutil.NewBlockFromBytes(blockBytes)
		if err != nil {
			return nil, nil, err
		}
		blk.ParseUtreexoData()
		ud := blk.UtreexoData()
		if ud == nil {
			return blockBytes, nil, nil
		}

		var stripped bytes.Buffer
		if err := blk.MsgBlock().Serialize(&stripped); err != nil {
			return nil, nil, err
		}
		var proof bytes.Buffer
		if err := ud.Serialize(&proof); err != nil {
			return nil, nil, err
		}
		return stripped.Bytes(), proof.Bytes(), nil
	}

	if err := rewriter.RewriteBlockFiles(transform); err != nil {
		return err
	}

	// The whole chain is backfilled; record the new version.
	return b.db.Update(func(dbTx database.Tx) error {
		return dbPutVersion(dbTx, utreexoProofStoreVersionKeyName,
			utreexoProofStoreVersion)
	})
}
