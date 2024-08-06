// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"math/rand"
	"os"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/utreexo/utreexod/chaincfg"
)

func TestUtreexoStateConsistencyWrite(t *testing.T) {
	dbPath := t.TempDir()
	db, err := leveldb.OpenFile(dbPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { os.RemoveAll(dbPath) }()

	// Values to write.
	numLeaves := rand.Uint64()
	hash := chaincfg.MainNetParams.GenesisHash

	// Write the consistency state.
	ldbTx, err := db.OpenTransaction()
	if err != nil {
		t.Fatal(err)
	}
	err = dbWriteUtreexoStateConsistency(ldbTx, hash, numLeaves)
	if err != nil {
		t.Fatal(err)
	}
	err = ldbTx.Commit()
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
