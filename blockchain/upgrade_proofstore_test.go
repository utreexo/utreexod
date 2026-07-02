// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
)

// TestUpgradeUtreexoProofStore verifies that opening a utreexo database whose
// proofs are still serialized inline with the block bytes (the format used
// before the out-of-band proof store) backfills those proofs into the proof
// store and bumps the proof store version.
func TestUpgradeUtreexoProofStore(t *testing.T) {
	params := chaincfg.RegressionNetParams
	dbPath := filepath.Join(t.TempDir(), "db")
	db, err := database.Create(testDbType, dbPath, blockDataNet)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer db.Close()

	paramsCopy := params
	chain, err := New(&Config{
		DB:          db,
		ChainParams: &paramsCopy,
		TimeSource:  NewMedianTime(),
		SigCache:    txscript.NewSigCache(1000),
		UtreexoView: NewUtreexoViewpoint(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Build a block extending genesis that carries a utreexo proof.
	want := &wire.UData{
		AccProof: utreexo.Proof{
			Targets: []uint64{1, 2, 3},
			Proof:   []utreexo.Hash{{0x01}, {0x02}, {0x03}},
		},
	}
	genesisNode := chain.bestChain.Tip()
	hdr := wire.BlockHeader{
		Version:   1,
		PrevBlock: genesisNode.hash,
		Bits:      params.PowLimitBits,
		Timestamp: time.Unix(genesisNode.timestamp+1, 0),
	}
	msgBlock := wire.NewMsgBlock(&hdr)
	coinbase := wire.NewMsgTx(1)
	coinbase.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Index: 0xffffffff},
		SignatureScript:  []byte{0x00, 0x00},
	})
	coinbase.AddTxOut(&wire.TxOut{Value: 0, PkScript: []byte{txscript.OP_RETURN}})
	msgBlock.AddTransaction(coinbase)

	// Serialize the block in the OLD on-disk format: base block bytes followed
	// by the proof serialized inline.
	var oldBytes bytes.Buffer
	if err := msgBlock.Serialize(&oldBytes); err != nil {
		t.Fatalf("Serialize block: %v", err)
	}
	if err := want.Serialize(&oldBytes); err != nil {
		t.Fatalf("Serialize proof: %v", err)
	}
	block, err := btcutil.NewBlockFromBytes(oldBytes.Bytes())
	if err != nil {
		t.Fatalf("NewBlockFromBytes: %v", err)
	}
	block.SetHeight(1)

	// Sanity check that the inline proof parses back.
	block.ParseUtreexoData()
	if block.UtreexoData() == nil {
		t.Fatal("inline proof should parse from the old-format block bytes")
	}

	// Splice the block into the best chain in memory and store its old-format
	// bytes on disk, then mark the proof store as not yet migrated.  The proof
	// is intentionally not written to the proof store.
	node1 := newBlockNode(&hdr, genesisNode)
	node1.status = statusDataStored | statusValid
	chain.index.AddNode(node1)
	chain.bestChain.SetTip(node1)

	err = db.Update(func(dbTx database.Tx) error {
		if err := dbStoreBlock(dbTx, block); err != nil {
			return err
		}
		return dbPutVersion(dbTx, utreexoProofStoreVersionKeyName, 0)
	})
	if err != nil {
		t.Fatalf("store old-format block: %v", err)
	}

	// Precondition: the proof is not yet in the proof store.
	err = db.View(func(dbTx database.Tx) error {
		got, err := dbTx.FetchUtreexoProof(&node1.hash)
		if err != nil {
			return err
		}
		if got != nil {
			t.Fatal("proof must not be in the store before the upgrade")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("pre-upgrade view: %v", err)
	}

	// Run the upgrade.
	if err := chain.maybeUpgradeDbBuckets(nil); err != nil {
		t.Fatalf("maybeUpgradeDbBuckets: %v", err)
	}

	// The proof must now be in the proof store and the version bumped.
	var wantBytes bytes.Buffer
	if err := want.Serialize(&wantBytes); err != nil {
		t.Fatalf("Serialize want: %v", err)
	}
	err = db.View(func(dbTx database.Tx) error {
		got, err := dbTx.FetchUtreexoProof(&node1.hash)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, wantBytes.Bytes()) {
			t.Fatalf("backfilled proof mismatch: got %d bytes, want %d",
				len(got), wantBytes.Len())
		}
		ver, err := dbFetchOrCreateVersion(dbTx, utreexoProofStoreVersionKeyName, 0)
		if err != nil {
			return err
		}
		if ver != utreexoProofStoreVersion {
			t.Fatalf("proof store version = %d, want %d", ver, utreexoProofStoreVersion)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("post-upgrade view: %v", err)
	}
}
