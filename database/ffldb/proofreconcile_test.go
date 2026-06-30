// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ffldb

import (
	"path/filepath"
	"testing"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
)

// TestUtreexoProofReconcileRollback simulates an unclean shutdown that occurs
// after a proof's bytes are written and synced to the proof flat file but
// before the metadata batch (the proof index entry and the proof write cursor)
// is committed.  Reopening the database must detect that the proof files on
// disk are ahead of the metadata and roll the proof store back to the
// committed position instead of returning a corruption error.
func TestUtreexoProofReconcileRollback(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ffldb-proofreconcile")
	idb, err := database.Create(dbType, dbPath, blockDataNet)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Write orphan proof bytes straight to the proof store, advancing and
	// syncing its write cursor without committing any metadata.  This is the
	// on-disk state left by a crash mid-commit.
	pdb := idb.(*db)
	if _, err := pdb.proofStore.writeBlock(make([]byte, 4096), nil); err != nil {
		t.Fatalf("writeBlock: %v", err)
	}
	if err := pdb.proofStore.syncBlocks(); err != nil {
		t.Fatalf("syncBlocks: %v", err)
	}
	if pdb.proofStore.writeCursor.curOffset == 0 {
		t.Fatal("expected the proof write cursor to have advanced")
	}
	if err := idb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen.  reconcileDB must roll the proof store back to (0, 0) rather
	// than erroring, because the metadata proof write cursor was never moved.
	idb, err = database.Open(dbType, dbPath, blockDataNet)
	if err != nil {
		t.Fatalf("Open should reconcile the orphan proof bytes, got: %v", err)
	}
	defer idb.Close()

	pdb = idb.(*db)
	wc := pdb.proofStore.writeCursor
	if wc.curFileNum != 0 || wc.curOffset != 0 {
		t.Fatalf("proof write cursor not rolled back: file %d, offset %d",
			wc.curFileNum, wc.curOffset)
	}

	// The database must remain usable: a proof stored after recovery round
	// trips correctly.
	var hash chainhash.Hash
	hash[0] = 0x7
	want := []byte("recovered-proof-payload")
	err = idb.Update(func(tx database.Tx) error {
		return tx.StoreUtreexoProof(&hash, want)
	})
	if err != nil {
		t.Fatalf("StoreUtreexoProof after recovery: %v", err)
	}
	err = idb.View(func(tx database.Tx) error {
		got, err := tx.FetchUtreexoProof(&hash)
		if err != nil {
			return err
		}
		if string(got) != string(want) {
			t.Fatalf("proof mismatch after recovery: got %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("FetchUtreexoProof after recovery: %v", err)
	}
}
