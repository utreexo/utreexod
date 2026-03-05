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

	// Flush with a known hash.
	hash := chaincfg.MainNetParams.GenesisHash
	err = forest.Flush([32]byte(*hash))
	require.NoError(t, err)

	// Read it back and cast to chainhash.Hash.
	gotBytes, err := forest.ReadConsistencyHash()
	require.NoError(t, err)
	gotHash := chainhash.Hash(gotBytes)

	require.Equal(t, *hash, gotHash)
}
