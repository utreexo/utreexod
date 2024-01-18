// Copyright (c) 2023 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This file is ignored during the regular tests due to the following build tag.
//go:build rpctest
// +build rpctest

package integration

import (
	"testing"

	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/integration/rpctest"
)

func TestPrune(t *testing.T) {
	t.Parallel()

	// Boilerplate code to make a pruned node.
	btcdCfg := []string{"--prune=550"}
	r, err := rpctest.New(&chaincfg.SimNetParams, nil, btcdCfg, "")
	if err != nil {
		t.Fatal("TestPrune fail. Unable to create primary harness: ", err)
	}
	if err := r.SetUp(false, 0); err != nil {
		t.Fatalf("TestPrune fail. Unable to setup test chain: %v", err)
	}
	t.Cleanup(func() { r.TearDown() })

	// Check that the rpc call for block chain info comes back correctly.
	chainInfo, err := r.Client.GetBlockChainInfo()
	if err != nil {
		t.Fatal(err)
	}

	if !chainInfo.Pruned {
		t.Fatalf("expected the node to be pruned but the pruned "+
			"boolean was %v", chainInfo.Pruned)
	}
}
