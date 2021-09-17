// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"reflect"
	"testing"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

// newHashFromStr converts the passed big-endian hex string into a
// chainhash.Hash.  It only differs from the one available in chainhash in that
// it ignores the error since it will only (and must only) be called with
// hard-coded, and therefore known good, hashes.
func newHashFromStr(hexStr string) *chainhash.Hash {
	hash, _ := chainhash.NewHashFromStr(hexStr)
	return hash
}

// hexToBytes converts the passed hex string into bytes and will panic if there
// is an error.  This is only provided for the hard-coded constants so errors in
// the source code can be detected. It will only (and must only) be called with
// hard-coded values.
func hexToBytes(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("invalid hex in source file: " + s)
	}
	return b
}

func TestSerializeSize(t *testing.T) {
	tests := []struct {
		ld   LeafData
		size int
	}{
		{
			ld: LeafData{
				BlockHash: *newHashFromStr("00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d"),
				OutPoint: OutPoint{
					Hash:  *newHashFromStr("061bb0bf3a1b9df13773da06bf92920394887a9c2b8b8772ac06be4e077df5eb"),
					Index: 10,
				},
				Amount:     200000,
				PkScript:   hexToBytes("a914e8d74935cfa223f9750a32b18d609cba17a5c3fe87"),
				Height:     1599255,
				IsCoinBase: false,
			},
			size: 104, //32 + 32 + 4 + 8 + 1 + 23 + 4
		},
	}

	t.Logf("Running %d tests", len(tests))
	for i, test := range tests {
		serializedSize := test.ld.SerializeSize()
		if serializedSize != test.size {
			t.Errorf("MsgTx.SerializeSize: #%d got: %d, want: %d", i,
				serializedSize, test.size)
			continue
		}
	}
}

// Just a function that checks for LeafData equality that doesn't use reflect.
func checkLeafEqual(ld, checkLeaf LeafData) error {
	if !bytes.Equal(ld.BlockHash[:], checkLeaf.BlockHash[:]) {
		return fmt.Errorf("LeafData BlockHash mismatch. expect %s, got %s",
			ld.BlockHash.String(), checkLeaf.BlockHash.String())
	}

	if !bytes.Equal(ld.OutPoint.Hash[:], checkLeaf.OutPoint.Hash[:]) {
		return fmt.Errorf("LeafData outpoint hash mismatch. expect %s, got %s",
			ld.OutPoint.Hash.String(), checkLeaf.OutPoint.Hash.String())
	}

	if ld.OutPoint.Index != checkLeaf.OutPoint.Index {
		return fmt.Errorf("LeafData outpoint index mismatch. expect %v, got %v",
			ld.OutPoint.Index, checkLeaf.OutPoint.Index)
	}

	if ld.Amount != checkLeaf.Amount {
		return fmt.Errorf("LeafData amount mismatch. expect %v, got %v",
			ld.Amount, checkLeaf.Amount)
	}

	if ld.IsCoinBase != checkLeaf.IsCoinBase {
		return fmt.Errorf("LeafData IsCoinBase mismatch. expect %v, got %v",
			ld.IsCoinBase, checkLeaf.IsCoinBase)
	}

	if ld.Height != checkLeaf.Height {
		return fmt.Errorf("LeafData height mismatch. expect %v, got %v",
			ld.Height, checkLeaf.Height)
	}

	if !bytes.Equal(ld.PkScript[:], checkLeaf.PkScript[:]) {
		return fmt.Errorf("LeafData pkscript mismatch. expect %x, got %x",
			ld.PkScript, checkLeaf.PkScript)
	}

	return nil
}

func TestLeafDataSerialize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ld     LeafData
		before []byte
		after  []byte
	}{
		{
			name: "Testnet3 tx 061bb0bf... from block 1600000",
			ld: LeafData{
				BlockHash: *newHashFromStr("00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d"),
				OutPoint: OutPoint{
					Hash:  *newHashFromStr("061bb0bf3a1b9df13773da06bf92920394887a9c2b8b8772ac06be4e077df5eb"),
					Index: 10,
				},
				Amount:     200000,
				PkScript:   hexToBytes("a914e8d74935cfa223f9750a32b18d609cba17a5c3fe87"),
				Height:     1599255,
				IsCoinBase: false,
			},
		},
		{
			name: "Mainnet coinbase tx fa201b65... from block 573123",
			ld: LeafData{
				BlockHash: *newHashFromStr("000000000000000000278eb9386b4e70b850a4ec21907af3a27f50330b7325aa"),
				OutPoint: OutPoint{
					Hash:  *newHashFromStr("fa201b650eef761f5701afbb610e4a211b86985da4745aec3ac0f4b7a8e2c8d2"),
					Index: 0,
				},
				Amount:     1315080370,
				PkScript:   hexToBytes("76a9142cc2b87a28c8a097f48fcc1d468ced6e7d39958d88ac"),
				Height:     573123,
				IsCoinBase: true,
			},
		},
	}

	for _, test := range tests {
		// Serialize
		writer := &bytes.Buffer{}
		test.ld.Serialize(writer)
		test.before = writer.Bytes()

		// Deserialize
		checkLeaf := NewLeafData()
		checkLeaf.Deserialize(writer)

		err := checkLeafEqual(test.ld, checkLeaf)
		if err != nil {
			t.Errorf("%s: LeafData mismatch. err: %s", test.name, err.Error())
		}

		if !reflect.DeepEqual(test.ld, checkLeaf) {
			t.Errorf("%s: LeafData mismatch.", test.name)
		}

		// Re-serialize
		afterWriter := &bytes.Buffer{}
		checkLeaf.Serialize(afterWriter)
		test.after = afterWriter.Bytes()

		// Check if before and after match.
		if !bytes.Equal(test.before, test.after) {
			t.Errorf("%s: LeafData serialize/deserialize fail. "+
				"Before len %d, after len %d", test.name,
				len(test.before), len(test.after))
		}
	}
}

func TestSerializeSizeCompact(t *testing.T) {
	tests := []struct {
		ld   LeafData
		size int
	}{
		{
			ld: LeafData{
				BlockHash: *newHashFromStr("00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d"),
				OutPoint: OutPoint{
					Hash:  *newHashFromStr("061bb0bf3a1b9df13773da06bf92920394887a9c2b8b8772ac06be4e077df5eb"),
					Index: 10,
				},
				Amount:     200000,
				PkScript:   hexToBytes("a914e8d74935cfa223f9750a32b18d609cba17a5c3fe87"),
				Height:     1599255,
				IsCoinBase: false,
			},
			size: 36, // 8 + 1 + 23 + 4
		},
	}

	t.Logf("Running %d tests", len(tests))
	for i, test := range tests {
		serializedSize := test.ld.SerializeSizeCompact()
		if serializedSize != test.size {
			t.Errorf("MsgTx.SerializeSizeCompact: #%d got: %d, want: %d", i,
				serializedSize, test.size)
			continue
		}
	}
}

// Just a function that checks for Compact LeafData equality that doesn't use reflect.
func checkCompactLeafEqual(ld, checkLeaf LeafData) error {
	// Only amount, hcb, and pkscript is serialized with the compact serialization.
	if ld.Amount != checkLeaf.Amount {
		return fmt.Errorf("LeafData amount mismatch. expect %v, got %v",
			ld.Amount, checkLeaf.Amount)
	}

	if ld.IsCoinBase != checkLeaf.IsCoinBase {
		return fmt.Errorf("LeafData IsCoinBase mismatch. expect %v, got %v",
			ld.IsCoinBase, checkLeaf.IsCoinBase)
	}

	if ld.Height != checkLeaf.Height {
		return fmt.Errorf("LeafData height mismatch. expect %v, got %v",
			ld.Height, checkLeaf.Height)
	}

	if !bytes.Equal(ld.PkScript[:], checkLeaf.PkScript[:]) {
		return fmt.Errorf("LeafData pkscript mismatch. expect %x, got %x",
			ld.PkScript, checkLeaf.PkScript)
	}

	return nil
}

func TestLeafDataSerializeCompact(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ld     LeafData
		before []byte
		after  []byte
	}{
		{
			name: "Testnet3 tx 061bb0bf... from block 1600000",
			ld: LeafData{
				BlockHash: *newHashFromStr("00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d"),
				OutPoint: OutPoint{
					Hash:  *newHashFromStr("061bb0bf3a1b9df13773da06bf92920394887a9c2b8b8772ac06be4e077df5eb"),
					Index: 10,
				},
				Amount:     200000,
				PkScript:   hexToBytes("a914e8d74935cfa223f9750a32b18d609cba17a5c3fe87"),
				Height:     1599255,
				IsCoinBase: false,
			},
		},
		{
			name: "Mainnet coinbase tx fa201b65... from block 573123",
			ld: LeafData{
				BlockHash: *newHashFromStr("000000000000000000278eb9386b4e70b850a4ec21907af3a27f50330b7325aa"),
				OutPoint: OutPoint{
					Hash:  *newHashFromStr("fa201b650eef761f5701afbb610e4a211b86985da4745aec3ac0f4b7a8e2c8d2"),
					Index: 0,
				},
				Amount:     1315080370,
				PkScript:   hexToBytes("76a9142cc2b87a28c8a097f48fcc1d468ced6e7d39958d88ac"),
				Height:     573123,
				IsCoinBase: true,
			},
		},
	}

	for _, test := range tests {
		// Serialize
		writer := &bytes.Buffer{}
		test.ld.SerializeCompact(writer)
		test.before = writer.Bytes()

		// Deserialize
		checkLeaf := NewLeafData()
		checkLeaf.DeserializeCompact(writer)

		err := checkCompactLeafEqual(test.ld, checkLeaf)
		if err != nil {
			t.Errorf("%s: LeafData mismatch. err: %s", test.name, err.Error())
		}

		// Re-serialize
		afterWriter := &bytes.Buffer{}
		checkLeaf.SerializeCompact(afterWriter)
		test.after = afterWriter.Bytes()

		// Check if before and after match.
		if !bytes.Equal(test.before, test.after) {
			t.Errorf("%s: LeafData compact serialize/deserialize fail. "+
				"Before len %d, after len %d", test.name,
				len(test.before), len(test.after))
		}
	}
}
