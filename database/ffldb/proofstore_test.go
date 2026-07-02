// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ffldb_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	_ "github.com/utreexo/utreexod/database/ffldb"
	"github.com/utreexo/utreexod/wire"
)

// makeProof returns a deterministic proof-shaped payload of the given size for
// the given seed.
func makeProof(seed byte, size int) []byte {
	p := make([]byte, size)
	for i := range p {
		p[i] = seed + byte(i)
	}
	return p
}

// hashFromByte returns a deterministic distinct hash for the given seed.
func hashFromByte(seed byte) chainhash.Hash {
	var h chainhash.Hash
	h[0] = seed
	h[1] = 0xab
	return h
}

// TestUtreexoProofStore exercises the out-of-band utreexo proof store through
// the public database interface: storing proofs of varying sizes, fetching
// them back, a miss returning nil, persistence across a close/reopen (which
// recovers the proof write cursor from the proof files on disk), and appending
// more proofs after a reopen.
func TestUtreexoProofStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "db")
	db, err := database.Create("ffldb", dbPath, wire.MainNet)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer db.Close()

	// Store a batch of proofs of varying sizes.
	proofs := make(map[chainhash.Hash][]byte)
	for i := 0; i < 40; i++ {
		h := hashFromByte(byte(i))
		proofs[h] = makeProof(byte(i), 200+i*64)
	}
	store := func(d database.DB, set map[chainhash.Hash][]byte) {
		err := d.Update(func(tx database.Tx) error {
			for h := range set {
				h := h
				if err := tx.StoreUtreexoProof(&h, set[h]); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("StoreUtreexoProof: %v", err)
		}
	}
	verify := func(d database.DB, set map[chainhash.Hash][]byte) {
		err := d.View(func(tx database.Tx) error {
			for h, want := range set {
				h := h
				got, err := tx.FetchUtreexoProof(&h)
				if err != nil {
					return err
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("proof mismatch for %v: got %d bytes, want %d",
						h, len(got), len(want))
				}
			}
			// A hash with no stored proof must return (nil, nil).
			missing := hashFromByte(0xff)
			got, err := tx.FetchUtreexoProof(&missing)
			if err != nil {
				return err
			}
			if got != nil {
				t.Fatalf("expected nil for missing proof, got %d bytes", len(got))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("FetchUtreexoProof: %v", err)
		}
	}

	store(db, proofs)
	verify(db, proofs)

	// Close and reopen: the proof write cursor must be recovered from the
	// proof files on disk, and all proofs must still be readable.
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	db, err = database.Open("ffldb", dbPath, wire.MainNet)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	verify(db, proofs)

	// Appending more proofs after the reopen must work and not disturb the
	// existing ones (verifies the recovered cursor appends, not overwrites).
	more := make(map[chainhash.Hash][]byte)
	for i := 100; i < 120; i++ {
		h := hashFromByte(byte(i))
		more[h] = makeProof(byte(i), 500+i)
	}
	store(db, more)
	verify(db, proofs)
	verify(db, more)
}
