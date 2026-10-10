// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/bip183"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
)

// proofStoreUpgradeFixture holds a database laid out in the legacy format,
// with a block whose proof is serialized inline, along with the expected
// post-upgrade state used to assert the migration.
type proofStoreUpgradeFixture struct {
	db                  database.DB
	chain               *BlockChain
	node                *blockNode
	blockFile           string
	beforeBlockFileSize int64
	proofBytes          []byte
	strippedBlockBytes  []byte
}

// newProofStoreUpgradeFixture creates a regression database with a single
// block stored in the legacy format, its proof serialized inline with the
// block bytes, and the proof store version marker removed so the upgrade
// backfills it.  It returns the fixture with the sizes and bytes the
// assertions need.
func newProofStoreUpgradeFixture(t *testing.T) *proofStoreUpgradeFixture {

	t.Helper()

	// Create a utreexo database and chain for the regression network.
	params := chaincfg.RegressionNetParams
	dbPath := filepath.Join(t.TempDir(), "db")
	db, err := database.Create(testDbType, dbPath, blockDataNet)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

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

	// Build a minimal height 1 block along with the utreexo proof that
	// older versions serialized inline with it.
	want := &bip183.UData{
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

	// Keep the base block serialization, which is what the migration must
	// leave on disk after stripping the inline proof.
	var strippedBlockBytes bytes.Buffer
	if err := msgBlock.Serialize(&strippedBlockBytes); err != nil {
		t.Fatalf("Serialize stripped block: %v", err)
	}

	// Serialize the proof the block will carry inline.
	var proofBytes bytes.Buffer
	if err := want.Serialize(&proofBytes); err != nil {
		t.Fatalf("Serialize proof: %v", err)
	}

	// Serialize the block in the OLD on-disk format: base block bytes
	// followed by the proof serialized inline.
	var oldBytes bytes.Buffer
	if err := msgBlock.Serialize(&oldBytes); err != nil {
		t.Fatalf("Serialize block: %v", err)
	}
	if _, err := oldBytes.Write(proofBytes.Bytes()); err != nil {
		t.Fatalf("append proof: %v", err)
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

	// Register the block with the index and best chain so the chain
	// recognizes it when the upgrade runs.
	node1 := newBlockNode(&hdr, genesisNode)
	node1.status = statusDataStored | statusValid
	chain.index.AddNode(node1)
	chain.bestChain.SetTip(node1)

	// Store the block in the legacy format.
	err = db.Update(func(dbTx database.Tx) error {
		if err := dbStoreBlock(dbTx, block); err != nil {
			return err
		}
		// New databases are initialized at the latest version.  Delete the
		// marker to model a database created before the proof store existed.
		return dbTx.Metadata().Delete(utreexoProofStoreVersionKeyName)
	})
	if err != nil {
		t.Fatalf("store old-format block: %v", err)
	}

	// Record the block file size before the upgrade so the assertions can
	// check the file shrank by the proof size.
	blockFile := filepath.Join(dbPath, "000000000.fdb")
	beforeStat, err := os.Stat(blockFile)
	if err != nil {
		t.Fatalf("stat block file before upgrade: %v", err)
	}

	return &proofStoreUpgradeFixture{
		db:                  db,
		chain:               chain,
		node:                node1,
		blockFile:           blockFile,
		beforeBlockFileSize: beforeStat.Size(),
		proofBytes:          append([]byte(nil), proofBytes.Bytes()...),
		strippedBlockBytes:  append([]byte(nil), strippedBlockBytes.Bytes()...),
	}
}

// assertUpgraded asserts the upgrade moved the inline proof into the proof
// store, stripped it from the block bytes, rewrote the block file to the
// shrunken size, and recorded the proof store version.
func (f *proofStoreUpgradeFixture) assertUpgraded(t *testing.T) {
	t.Helper()

	afterStat, err := os.Stat(f.blockFile)
	if err != nil {
		t.Fatalf("stat block file after upgrade: %v", err)
	}
	wantBlockFileSize := f.beforeBlockFileSize - int64(len(f.proofBytes))
	if afterStat.Size() != wantBlockFileSize {
		t.Fatalf("block file size after upgrade = %d, want %d",
			afterStat.Size(), wantBlockFileSize)
	}

	err = f.db.View(func(dbTx database.Tx) error {
		gotProof, err := dbTx.FetchUtreexoProof(&f.node.hash)
		if err != nil {
			return err
		}
		if !bytes.Equal(gotProof, f.proofBytes) {
			t.Fatalf("stored proof mismatch: got %d bytes, want %d",
				len(gotProof), len(f.proofBytes))
		}

		ver := dbFetchVersion(dbTx, utreexoProofStoreVersionKeyName)
		if ver != 1 {
			t.Fatalf("proof store version = %d, want 1", ver)
		}

		gotBlockBytes, err := dbTx.FetchBlock(&f.node.hash)
		if err != nil {
			return err
		}
		if !bytes.Equal(gotBlockBytes, f.strippedBlockBytes) {
			t.Fatalf("block bytes mismatch: got %d bytes, want %d",
				len(gotBlockBytes), len(f.strippedBlockBytes))
		}
		if bytes.Contains(gotBlockBytes, f.proofBytes) {
			t.Fatal("block bytes still contain the old inline proof")
		}
		roundTrip, err := btcutil.NewBlockFromBytes(gotBlockBytes)
		if err != nil {
			return err
		}
		roundTrip.ParseUtreexoData()
		if roundTrip.UtreexoData() != nil {
			t.Fatal("inline proof should have been stripped from block bytes")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("post-upgrade view: %v", err)
	}
}

// TestUpgradeUtreexoProofStore verifies that opening a utreexo database whose
// proofs are still serialized inline with the block bytes moves those proofs
// into the proof store and strips them from the block store.
func TestUpgradeUtreexoProofStore(t *testing.T) {
	fixture := newProofStoreUpgradeFixture(t)

	err := fixture.db.View(func(dbTx database.Tx) error {
		if serialized := dbTx.Metadata().Get(
			utreexoProofStoreVersionKeyName); serialized != nil {

			t.Fatal("proof store version exists, want no version")
		}
		got, err := dbTx.FetchUtreexoProof(&fixture.node.hash)
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

	if err := fixture.chain.maybeUpgradeUtreexoProofStore(nil); err != nil {
		t.Fatalf("maybeUpgradeUtreexoProofStore: %v", err)
	}
	fixture.assertUpgraded(t)
}
