// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"errors"
	"testing"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/chaincfg"
)

// TestFlatFlushBeforeMainDB checks that Flush reaches the main database when
// flat-file syncing succeeds, but stops before it when a flat-file sync fails.
// Both archive and pruned indexes must work; pruned indexes have fewer files.
func TestFlatFlushBeforeMainDB(t *testing.T) {
	modes := []struct {
		name   string
		pruned bool
	}{
		{name: "archive", pruned: false},
		{name: "pruned", pruned: true},
	}
	cases := []struct {
		name          string
		closeRootFile bool
	}{
		{name: "flat_files_sync_successfully", closeRootFile: false},
		{name: "flat_file_sync_fails", closeRootFile: true},
	}

	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					// Record whether Flush reaches the main database. Return a
					// recognizable error to stop there: this test does not set up
					// the forest that Flush would otherwise persist next.
					stopAtMainDB := errors.New("stop after reaching the main database")
					mainDBFlushCalled := false
					flushMainDB := func() error {
						mainDBFlushCalled = true
						return stopAtMainDB
					}

					// Each case gets its own index and files.
					idx, err := NewFlatUtreexoProofIndex(mode.pruned,
						&chaincfg.MainNetParams, 0, t.TempDir(), flushMainDB)
					require.NoError(t, err)
					files := []*FlatFileState{
						&idx.undoState,
						&idx.rootsState,
						&idx.proofStatsState,
					}
					if !mode.pruned {
						files = append(files, &idx.targetState, &idx.proofState,
							&idx.leafDataState, &idx.ttlState)
					}
					t.Cleanup(func() {
						for _, file := range files {
							file.dataFile.Close()
							file.offsetFile.Close()
						}
					})

					// Syncing a closed file must fail. This simulates a storage
					// error before the main database can be flushed.
					if test.closeRootFile {
						require.NoError(t, idx.rootsState.dataFile.Close())
					}

					// Force a flush as if we had just connected a block.
					err = idx.Flush(chaincfg.MainNetParams.GenesisHash,
						blockchain.FlushRequired, true)

					if test.closeRootFile {
						require.Error(t, err)
						require.NotErrorIs(t, err, stopAtMainDB)
						require.False(t, mainDBFlushCalled,
							"a flat-file sync failure must stop the main database flush")
					} else {
						require.True(t, mainDBFlushCalled,
							"successful flat-file syncing must allow the main database flush")
						require.ErrorIs(t, err, stopAtMainDB)
					}
				})
			}
		})
	}
}

func TestReadConsistencyHash(t *testing.T) {
	dbPath := t.TempDir()
	forest, err := utreexo.OpenForest(dbPath)
	require.NoError(t, err)

	// Close with a known hash so it is persisted via the WAL.
	hash := chaincfg.MainNetParams.GenesisHash
	require.NoError(t, forest.Close([32]byte(*hash)))

	// Reopen and read the hash back, mirroring how production callers
	// recover the consistency hash after a restart.
	forest, err = utreexo.OpenForest(dbPath)
	require.NoError(t, err)
	defer forest.Close([32]byte(*hash))

	gotBytes, err := forest.ReadConsistencyHash()
	require.NoError(t, err)
	gotHash := chainhash.Hash(gotBytes)

	require.Equal(t, *hash, gotHash)
}
