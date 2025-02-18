// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"math/rand"
	"os"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/utreexo/utreexod/chaincfg"
)

func TestUtreexoStateConsistencyWrite(t *testing.T) {
	dbPath := t.TempDir()
	db, err := pebble.Open(dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { os.RemoveAll(dbPath) }()

	// Values to write.
	numLeaves := rand.Uint64()
	hash := chaincfg.MainNetParams.GenesisHash

	batch := db.NewBatch()
	err = dbWriteUtreexoStateConsistency(batch, hash, numLeaves)
	if err != nil {
		t.Fatal(err)
	}
	err = batch.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Fetch the consistency state.
	gotHash, gotNumLeaves, err := dbFetchUtreexoStateConsistency(db)
	if err != nil {
		t.Fatal(err)
	}

	// Compare.
	if *hash != *gotHash {
		t.Fatalf("expected %v, got %v", hash.String(), gotHash.String())
	}
	if numLeaves != gotNumLeaves {
		t.Fatalf("expected %v, got %v", numLeaves, gotNumLeaves)
	}
}
