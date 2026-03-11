// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package integration

// This file is ignored during the regular tests due to the following build tag.

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/integration/rpctest"
	"github.com/utreexo/utreexod/wire"
)

// peerServices connects nodeA to nodeB and returns the service flags that
// nodeA sees advertised by nodeB via getpeerinfo.
func peerServices(t *testing.T, nodeA, nodeB *rpctest.Harness) wire.ServiceFlag {
	t.Helper()

	require.NoError(t, rpctest.ConnectNode(nodeA, nodeB))

	var services wire.ServiceFlag
	require.Eventually(t, func() bool {
		peers, err := nodeA.Client.GetPeerInfo()
		if err != nil || len(peers) == 0 {
			return false
		}
		nodeAddr := nodeB.P2PAddress()
		for _, p := range peers {
			if p.Addr == nodeAddr {
				raw, err := strconv.ParseUint(p.Services, 10, 64)
				require.NoError(t, err)
				services = wire.ServiceFlag(raw)
				return true
			}
		}
		return false
	}, 30*time.Second, 100*time.Millisecond)

	fmt.Println(services)
	return services
}

// TestUtreexoServiceFlags verifies that nodes advertise the correct
// NODE_UTREEXO (bit 12) and NODE_UTREEXO_ARCHIVE (bit 13) service flags
// as defined in BIP-0183.
//
// The test spins up four node configurations:
//  1. CSN (default, pruned): signals NODE_UTREEXO but NOT NODE_UTREEXO_ARCHIVE
//  2. CSN (archival): signals NODE_UTREEXO + NODE_UTREEXO_ARCHIVE
//  3. Bridge node (archival): signals NODE_UTREEXO + NODE_UTREEXO_ARCHIVE
//  4. Bridge node (pruned): signals NODE_UTREEXO but NOT NODE_UTREEXO_ARCHIVE
func TestUtreexoServiceFlags(t *testing.T) {
	t.Parallel()

	// observer is a plain non-utreexo node used only to connect and read peer services.
	observer, err := rpctest.New(&chaincfg.SimNetParams, nil, []string{"--noutreexo"}, "")
	require.NoError(t, err)
	require.NoError(t, observer.SetUp(false, 0))
	t.Cleanup(func() { require.NoError(t, observer.TearDown()) })

	// prunedCSN is a default utreexo compact state node (pruned by default).
	prunedCSN, err := rpctest.New(&chaincfg.SimNetParams, nil, nil, "")
	require.NoError(t, err)
	require.NoError(t, prunedCSN.SetUp(false, 0))
	t.Cleanup(func() { require.NoError(t, prunedCSN.TearDown()) })

	// archivalCSN is a utreexo compact state node with --prune=0 (archival).
	archivalCSN, err := rpctest.New(&chaincfg.SimNetParams, nil, []string{"--prune=0"}, "")
	require.NoError(t, err)
	require.NoError(t, archivalCSN.SetUp(false, 0))
	t.Cleanup(func() { require.NoError(t, archivalCSN.TearDown()) })

	// archivalBridge is a bridge node with the flat utreexo proof index (archival, not pruned).
	archivalBridge, err := rpctest.New(&chaincfg.SimNetParams, nil, []string{
		"--flatutreexoproofindex",
		"--prune=0",
	}, "")
	require.NoError(t, err)
	require.NoError(t, archivalBridge.SetUp(false, 0))
	t.Cleanup(func() { require.NoError(t, archivalBridge.TearDown()) })

	// prunedBridge is a bridge node with the flat utreexo proof index (pruned).
	prunedBridge, err := rpctest.New(&chaincfg.SimNetParams, nil, []string{
		"--flatutreexoproofindex",
		"--prune=550",
	}, "")
	require.NoError(t, err)
	require.NoError(t, prunedBridge.SetUp(false, 0))
	t.Cleanup(func() { require.NoError(t, prunedBridge.TearDown()) })

	tests := []struct {
		name        string
		target      *rpctest.Harness
		wantUtreexo bool
		wantArchive bool
	}{
		{
			name:        "pruned CSN signals NODE_UTREEXO but not NODE_UTREEXO_ARCHIVE",
			target:      prunedCSN,
			wantUtreexo: true,
			wantArchive: false,
		},
		{
			name:        "archival CSN signals NODE_UTREEXO and NODE_UTREEXO_ARCHIVE",
			target:      archivalCSN,
			wantUtreexo: true,
			wantArchive: true,
		},
		{
			name:        "archival bridge signals NODE_UTREEXO and NODE_UTREEXO_ARCHIVE",
			target:      archivalBridge,
			wantUtreexo: true,
			wantArchive: true,
		},
		{
			name:        "pruned bridge signals NODE_UTREEXO but not NODE_UTREEXO_ARCHIVE",
			target:      prunedBridge,
			wantUtreexo: true,
			wantArchive: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			services := peerServices(t, observer, tc.target)

			hasUtreexo := services.HasFlag(wire.SFNodeUtreexo)
			hasArchive := services.HasFlag(wire.SFNodeUtreexoArchive)

			require.Equal(t, tc.wantUtreexo, hasUtreexo,
				"NODE_UTREEXO (bit 12): got %v, want %v (services: %s)",
				hasUtreexo, tc.wantUtreexo, services)

			require.Equal(t, tc.wantArchive, hasArchive,
				"NODE_UTREEXO_ARCHIVE (bit 13): got %v, want %v (services: %s)",
				hasArchive, tc.wantArchive, services)
		})
	}
}
