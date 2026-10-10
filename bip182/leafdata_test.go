// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip182

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/utreexo/utreexod/wire"
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
				OutPoint: wire.OutPoint{
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

		// Sanity check.  Actually serialize the data and compare against our hardcoded number.
		var buf bytes.Buffer
		err := test.ld.Serialize(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if len(buf.Bytes()) != test.size {
			t.Errorf("MsgTx.SerializeSize: #%d serialized: %d, want: %d", i,
				len(buf.Bytes()), test.size)
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
				OutPoint: wire.OutPoint{
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
				OutPoint: wire.OutPoint{
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

func TestLeafDataJsonMarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ld   LeafData
		want string
	}{
		{
			name: "Testnet3 tx 061bb0bf... from block 1600000",
			ld: LeafData{
				BlockHash: *newHashFromStr("00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d"),
				OutPoint: wire.OutPoint{
					Hash:  *newHashFromStr("061bb0bf3a1b9df13773da06bf92920394887a9c2b8b8772ac06be4e077df5eb"),
					Index: 10,
				},
				Amount:                200000,
				ReconstructablePkType: ScriptHashTy,
				PkScript:              hexToBytes("a914e8d74935cfa223f9750a32b18d609cba17a5c3fe87"),
				Height:                1599255,
				IsCoinBase:            false,
			},
			want: `{"blockhash":"00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d","txhash":"061bb0bf3a1b9df13773da06bf92920394887a9c2b8b8772ac06be4e077df5eb","index":10,"height":1599255,"iscoinbase":false,"amount":200000,"reconstructtype":3,"pkscript":"a914e8d74935cfa223f9750a32b18d609cba17a5c3fe87"}`,
		},
		{
			name: "Mainnet coinbase tx fa201b65... from block 573123",
			ld: LeafData{
				BlockHash: *newHashFromStr("000000000000000000278eb9386b4e70b850a4ec21907af3a27f50330b7325aa"),
				OutPoint: wire.OutPoint{
					Hash:  *newHashFromStr("fa201b650eef761f5701afbb610e4a211b86985da4745aec3ac0f4b7a8e2c8d2"),
					Index: 0,
				},
				Amount:                1315080370,
				ReconstructablePkType: PubKeyHashTy,
				PkScript:              hexToBytes("76a9142cc2b87a28c8a097f48fcc1d468ced6e7d39958d88ac"),
				Height:                573123,
				IsCoinBase:            true,
			},
			want: `{"blockhash":"000000000000000000278eb9386b4e70b850a4ec21907af3a27f50330b7325aa","txhash":"fa201b650eef761f5701afbb610e4a211b86985da4745aec3ac0f4b7a8e2c8d2","index":0,"height":573123,"iscoinbase":true,"amount":1315080370,"reconstructtype":1,"pkscript":"76a9142cc2b87a28c8a097f48fcc1d468ced6e7d39958d88ac"}`,
		},
		{
			name: "unconfirmed",
			ld: LeafData{
				BlockHash: *newHashFromStr("000000000000000000278eb9386b4e70b850a4ec21907af3a27f50330b7325aa"),
				OutPoint: wire.OutPoint{
					Hash:  *newHashFromStr("fa201b650eef761f5701afbb610e4a211b86985da4745aec3ac0f4b7a8e2c8d2"),
					Index: 0,
				},
				Amount:                1315080370,
				ReconstructablePkType: PubKeyHashTy,
				PkScript:              hexToBytes("76a9142cc2b87a28c8a097f48fcc1d468ced6e7d39958d88ac"),
				Height:                -1,
				IsCoinBase:            true,
			},
			want: `{"blockhash":"000000000000000000278eb9386b4e70b850a4ec21907af3a27f50330b7325aa","txhash":"fa201b650eef761f5701afbb610e4a211b86985da4745aec3ac0f4b7a8e2c8d2","index":0,"height":-1,"iscoinbase":true,"amount":1315080370,"reconstructtype":1,"pkscript":"76a9142cc2b87a28c8a097f48fcc1d468ced6e7d39958d88ac"}`,
		},
		{
			name: "confirmed",
			ld: LeafData{
				BlockHash: *newHashFromStr("000000000000000000278eb9386b4e70b850a4ec21907af3a27f50330b7325aa"),
				OutPoint: wire.OutPoint{
					Hash:  *newHashFromStr("fa201b650eef761f5701afbb610e4a211b86985da4745aec3ac0f4b7a8e2c8d2"),
					Index: 0,
				},
				Amount:                1315080370,
				ReconstructablePkType: PubKeyHashTy,
				PkScript:              hexToBytes("76a9142cc2b87a28c8a097f48fcc1d468ced6e7d39958d88ac"),
				Height:                573123,
				IsCoinBase:            true,
			},
			want: `{"blockhash":"000000000000000000278eb9386b4e70b850a4ec21907af3a27f50330b7325aa","txhash":"fa201b650eef761f5701afbb610e4a211b86985da4745aec3ac0f4b7a8e2c8d2","index":0,"height":573123,"iscoinbase":true,"amount":1315080370,"reconstructtype":1,"pkscript":"76a9142cc2b87a28c8a097f48fcc1d468ced6e7d39958d88ac"}`,
		},
	}

	for _, test := range tests {
		got, err := json.Marshal(&test.ld)
		if err != nil {
			t.Error(err)
		}

		if got := string(got); got != test.want {
			t.Errorf("%s: LeafData json marshal/unmarshal fail. "+
				"Expected %s, got %s", test.name, test.want, got)
		}

		var testData LeafData
		err = json.Unmarshal(got, &testData)
		if err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(test.ld, testData) {
			t.Errorf("%s: LeafData json marshal/unmarshal fail.", test.name)
		}
	}
}

func TestLeafHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ld   LeafData
	}{
		{
			name: "Testnet3 tx 061bb0bf... from block 1600000",
			ld: LeafData{
				BlockHash: *newHashFromStr("00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d"),
				OutPoint: wire.OutPoint{
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
				OutPoint: wire.OutPoint{
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

	// Just a hash func for sanity checking here.
	hashFunc := func(ld LeafData) [32]byte {
		shaTag := sha512.Sum512(TagUtreexoV1)
		preimage := make([]byte, 0, (64*2)+ld.SerializeSize())
		preimage = append(preimage, shaTag[:]...)
		preimage = append(preimage, shaTag[:]...)
		var buf bytes.Buffer
		err := ld.Serialize(&buf)
		if err != nil {
			t.Fatal(err)
		}
		preimage = append(preimage, buf.Bytes()...)

		return sha512.Sum512_256(preimage)
	}

	for _, test := range tests {
		got := test.ld.LeafHash()
		expect := hashFunc(test.ld)
		if got != expect {
			t.Fatalf("expect %s but got %s",
				hex.EncodeToString(expect[:]),
				hex.EncodeToString(got[:]))
		}

		// Verify LeafHasher produces the same result.
		lh := NewLeafHasher()
		gotHasher := lh.HashLeaf(&test.ld)
		if gotHasher != expect {
			t.Fatalf("LeafHasher: expect %s but got %s",
				hex.EncodeToString(expect[:]),
				hex.EncodeToString(gotHasher[:]))
		}
	}
}

// TestLeafDataCopy tests that modifying the leafdata copy does not modify the original.
func TestLeafDataCopy(t *testing.T) {
	ld := LeafData{
		BlockHash: *newHashFromStr("00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d"),
		OutPoint: wire.OutPoint{
			Hash:  *newHashFromStr("061bb0bf3a1b9df13773da06bf92920394887a9c2b8b8772ac06be4e077df5eb"),
			Index: 10,
		},
		Amount:     200000,
		PkScript:   hexToBytes("a914e8d74935cfa223f9750a32b18d609cba17a5c3fe87"),
		Height:     1599255,
		IsCoinBase: false,
	}

	ldOrig := LeafData{
		BlockHash: *newHashFromStr("00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d"),
		OutPoint: wire.OutPoint{
			Hash:  *newHashFromStr("061bb0bf3a1b9df13773da06bf92920394887a9c2b8b8772ac06be4e077df5eb"),
			Index: 10,
		},
		Amount:     200000,
		PkScript:   hexToBytes("a914e8d74935cfa223f9750a32b18d609cba17a5c3fe87"),
		Height:     1599255,
		IsCoinBase: false,
	}

	ldCopy := ld.Copy()
	ldCopy.OutPoint.Index = 7777
	ldCopy.OutPoint.Hash[31] = 0x17
	ldCopy.PkScript[0] = 0x77
	if reflect.DeepEqual(ldCopy, ld) {
		t.Fatalf("ldCopy and ld are same")
	}

	if !reflect.DeepEqual(ld, ldOrig) {
		t.Fatalf("ld and ldOrig are different")
	}
}
