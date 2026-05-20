// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

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
