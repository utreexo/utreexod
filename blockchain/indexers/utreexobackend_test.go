// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"math/rand"
	"os"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
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
	roots := make([]utreexo.Hash, 4)
	for i := range roots {
		roots[i][0] = uint8(i)
	}

	batch := db.NewBatch()
	err = dbWriteUtreexoStateConsistency(batch, hash, roots, numLeaves)
	if err != nil {
		t.Fatal(err)
	}
	err = batch.Commit(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Fetch the consistency state.
	gotHash, gotRoots, gotNumLeaves, err := dbFetchUtreexoStateConsistency(db)
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

	require.Equal(t, roots, gotRoots)
}
