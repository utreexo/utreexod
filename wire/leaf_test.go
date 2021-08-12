// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

//func createRandHash(rnd *rand.Rand) (*chainhash.Hash, error) {
//	hashVal, ok := quick.Value(reflect.TypeOf(chainhash.Hash{}), rnd)
//	if !ok {
//		err := fmt.Errorf("Failed to create stxo")
//		return nil, err
//	}
//	h := chainhash.Hash(hashVal.Interface().(chainhash.Hash))
//	return &h, nil
//}
//
//func createRandOP(rnd *rand.Rand) (*wire.OutPoint, error) {
//	//index := rand.Uint32()
//	//txHash, err := createRandHash(rnd)
//	//if err != nil {
//	//	return nil, err
//	//}
//
//	//op := wire.OutPoint{
//	//	Hash:  *txHash,
//	//	Index: index,
//	//}
//
//	opVal, ok := quick.Value(reflect.TypeOf(wire.OutPoint{}), rnd)
//	if !ok {
//		err := fmt.Errorf("Failed to create stxo")
//		return nil, err
//	}
//	op := wire.OutPoint(opVal.Interface().(wire.OutPoint))
//
//	fmt.Println(op.String())
//
//	return &op, nil
//}
//
//func createRandLeafData() (*LeafData, error) {
//	rnd := rand.New(rand.NewSource(time.Now().Unix()))
//
//	op, err := createRandOP(rnd)
//	if err != nil {
//		return nil, err
//	}
//
//	blockHash, err := createRandHash(rnd)
//	if err != nil {
//		return nil, err
//	}
//
//	ld := LeafData{
//		OutPoint:  op,
//		BlockHash: blockHash,
//	}
//
//	fmt.Println(ld)
//
//	return &ld, nil
//}
//
//func createRandStxo() (*blockchain.SpentTxOut, error) {
//	stxoVal, ok := quick.Value(reflect.TypeOf(blockchain.SpentTxOut{}), rand)
//	if !ok {
//		err := fmt.Errorf("Failed to create stxo")
//		t.Fatal(err)
//	}
//	stxo := blockchain.SpentTxOut(stxoVal.Interface().(blockchain.SpentTxOut))
//	fmt.Println(stxo)
//
//	stxo := blockchain.SpentTxOut{}
//}

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
				BlockHash: newHashFromStr("00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d"),
				OutPoint: &OutPoint{
					Hash:  *newHashFromStr("061bb0bf3a1b9df13773da06bf92920394887a9c2b8b8772ac06be4e077df5eb"),
					Index: 10,
				},
				Stxo: &SpentTxOut{
					Amount:     200000,
					PkScript:   hexToBytes("a914e8d74935cfa223f9750a32b18d609cba17a5c3fe87"),
					Height:     1599255,
					IsCoinBase: false,
				},
			},
		},
		{
			name: "Mainnet coinbase tx fa201b65... from block 573123",
			ld: LeafData{
				BlockHash: newHashFromStr("000000000000000000278eb9386b4e70b850a4ec21907af3a27f50330b7325aa"),
				OutPoint: &OutPoint{
					Hash:  *newHashFromStr("fa201b650eef761f5701afbb610e4a211b86985da4745aec3ac0f4b7a8e2c8d2"),
					Index: 0,
				},
				Stxo: &SpentTxOut{
					Amount:     1315080370,
					PkScript:   hexToBytes("76a9142cc2b87a28c8a097f48fcc1d468ced6e7d39958d88ac"),
					Height:     573123,
					IsCoinBase: true,
				},
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
				BlockHash: newHashFromStr("00000000000172ff8a4e14441512072bacaf8d38b995a3fcd2f8435efc61717d"),
				OutPoint: &OutPoint{
					Hash:  *newHashFromStr("061bb0bf3a1b9df13773da06bf92920394887a9c2b8b8772ac06be4e077df5eb"),
					Index: 10,
				},
				Stxo: &SpentTxOut{
					Amount:     200000,
					PkScript:   hexToBytes("a914e8d74935cfa223f9750a32b18d609cba17a5c3fe87"),
					Height:     1599255,
					IsCoinBase: false,
				},
			},
		},
		{
			name: "Mainnet coinbase tx fa201b65... from block 573123",
			ld: LeafData{
				BlockHash: newHashFromStr("000000000000000000278eb9386b4e70b850a4ec21907af3a27f50330b7325aa"),
				OutPoint: &OutPoint{
					Hash:  *newHashFromStr("fa201b650eef761f5701afbb610e4a211b86985da4745aec3ac0f4b7a8e2c8d2"),
					Index: 0,
				},
				Stxo: &SpentTxOut{
					Amount:     1315080370,
					PkScript:   hexToBytes("76a9142cc2b87a28c8a097f48fcc1d468ced6e7d39958d88ac"),
					Height:     573123,
					IsCoinBase: true,
				},
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
