// Copyright (c) 2026 The utreexod developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/wire"
)

var errRegressionProofStore = errors.New("regression proof store failure")

// regressionProofStoreFailDB wraps write transactions so proof persistence can
// be failed without affecting any of the other database writes ProcessBlock
// performs before connecting a block.
type regressionProofStoreFailDB struct {
	database.DB
}

// Update wraps the transaction passed to the update callback.
func (db *regressionProofStoreFailDB) Update(fn func(database.Tx) error) error {
	return db.DB.Update(func(dbTx database.Tx) error {
		return fn(&regressionProofStoreFailTx{Tx: dbTx})
	})
}

// regressionProofStoreFailTx fails proof writes and forwards all other
// transaction operations to the underlying database transaction.
type regressionProofStoreFailTx struct {
	database.Tx
}

// StoreUtreexoProof simulates a proof persistence failure.
func (tx *regressionProofStoreFailTx) StoreUtreexoProof(*chainhash.Hash,
	[]byte) error {

	return errRegressionProofStore
}

// TestUtreexoViewRollbackOnProofStoreFailure ensures proof persistence failure
// leaves the block and accumulator state unchanged so the block can be retried.
func TestUtreexoViewRollbackOnProofStoreFailure(t *testing.T) {
	chain, params, countingDB, tearDown := countingUtreexoTestChain(t,
		"proof-store-rollback")
	defer tearDown()

	genesis := btcutil.NewBlock(params.GenesisBlock)
	genesis.SetHeight(0)
	proofState := newTestUtreexoProofState()
	block, _ := proofState.newBlock(t, chain, genesis, nil)

	rootsBefore := append([]utreexo.Hash(nil),
		chain.utreexoView.accumulator.GetRoots()...)
	leavesBefore := chain.utreexoView.NumLeaves()
	tipBefore := chain.bestChain.Tip().hash

	chain.db = &regressionProofStoreFailDB{DB: countingDB}
	_, _, err := chain.ProcessBlock(block, BFNone)
	require.ErrorIs(t, err, errRegressionProofStore)
	require.Equal(t, tipBefore, chain.bestChain.Tip().hash,
		"best-chain tip changed after proof store failure")
	require.True(t, chain.utreexoView.compareRoots(rootsBefore),
		"proof store failure left the block applied to the accumulator")
	require.Equal(t, leavesBefore, chain.utreexoView.NumLeaves(),
		"proof store failure changed the accumulator leaf count")

	var hasBlock bool
	var proof []byte
	err = chain.db.View(func(dbTx database.Tx) error {
		var err error
		hasBlock, err = dbTx.HasBlock(block.Hash())
		if err != nil {
			return err
		}
		proof, err = dbTx.FetchUtreexoProof(block.Hash())
		proof = bytes.Clone(proof)
		return err
	})
	require.NoError(t, err)
	require.False(t, hasBlock, "block stored without its utreexo proof")
	require.Nil(t, proof)

	// Restore proof writes to verify the same block can be submitted again.
	chain.db = countingDB
	processBlock(t, chain, block, true)
	var want bytes.Buffer
	require.NoError(t, block.UtreexoData().Serialize(&want))
	err = chain.db.View(func(dbTx database.Tx) error {
		var err error
		proof, err = dbTx.FetchUtreexoProof(block.Hash())
		proof = bytes.Clone(proof)
		return err
	})
	require.NoError(t, err)
	require.Equal(t, want.Bytes(), proof)
}

// TestInvalidTTLUtreexoDataIsNotStored ensures malformed Utreexo data is
// rejected before the block and proof are persisted or the accumulator changes.
func TestInvalidTTLUtreexoDataIsNotStored(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*btcutil.Block)
	}{
		{
			name: "empty leaf data",
			mutate: func(block *btcutil.Block) {
				udata := block.UtreexoData()
				udata.LeafDatas = nil
				block.SetUtreexoData(udata)
			},
		},
		{
			name: "short leaf data",
			mutate: func(block *btcutil.Block) {
				udata := block.UtreexoData()
				udata.LeafDatas = udata.LeafDatas[:1]
				block.SetUtreexoData(udata)
			},
		},
		{
			name: "short ttls",
			mutate: func(block *btcutil.Block) {
				ttls := block.UtreexoTTLs()
				ttls.TTLs = ttls.TTLs[:len(ttls.TTLs)-1]
			},
		},
	}
	for _, mode := range []struct {
		name  string
		flags BehaviorFlags
	}{
		{name: "normal", flags: BFNone},
		{name: "fast add", flags: BFFastAdd},
	} {
		for _, test := range tests {
			t.Run(mode.name+"/"+test.name, func(t *testing.T) {
				chain, params, _, tearDown := countingUtreexoTestChain(t,
					"invalid-ttl-udata")
				defer tearDown()

				// Build a block that needs two leaf datas so a partial slice is invalid.
				genesis := btcutil.NewBlock(params.GenesisBlock)
				genesis.SetHeight(0)
				proofState := newTestUtreexoProofState()
				firstBlock, firstAdds := proofState.newBlock(t, chain, genesis, nil)
				processBlock(t, chain, firstBlock, true)
				secondBlock, secondAdds := proofState.newBlock(t, chain, firstBlock, nil)
				processBlock(t, chain, secondBlock, true)

				spends := []wire.LeafData{firstAdds[0], secondAdds[0]}
				block, adds := proofState.newBlock(t, chain, secondBlock, spends)
				block.SetUtreexoTTLs(&wire.UtreexoTTL{
					BlockHeight: uint32(block.Height()),
					TTLs:        make([]wire.TTLInfo, len(adds)),
				})
				test.mutate(block)

				// Rejection must leave the chain and both stores unchanged.
				tipBefore := chain.BestSnapshot()
				rootsBefore := append([]utreexo.Hash(nil),
					chain.utreexoView.accumulator.GetRoots()...)
				leavesBefore := chain.utreexoView.NumLeaves()
				aggBefore := chain.utreexoView.agg
				_, _, err := chain.ProcessBlock(block, mode.flags)
				require.Error(t, err)
				require.Equal(t, tipBefore, chain.BestSnapshot())
				require.Equal(t, tipBefore.Hash, chain.bestChain.Tip().hash)
				require.True(t, chain.utreexoView.compareRoots(rootsBefore))
				require.Equal(t, leavesBefore, chain.utreexoView.NumLeaves())
				require.Equal(t, aggBefore, chain.utreexoView.agg)

				var hasBlock bool
				var proof []byte
				err = chain.db.View(func(dbTx database.Tx) error {
					var err error
					hasBlock, err = dbTx.HasBlock(block.Hash())
					if err != nil {
						return err
					}
					proof, err = dbTx.FetchUtreexoProof(block.Hash())
					proof = bytes.Clone(proof)
					return err
				})
				require.NoError(t, err)
				require.False(t, hasBlock)
				require.Nil(t, proof)
			})
		}
	}
}
