// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/mit-dci/utreexo/accumulator"
)

func testLeavesToHashes() []accumulator.Hash {
	hashes := make([]accumulator.Hash, 0, len(testLeaves))
	for _, leaf := range testLeaves {
		hashes = append(hashes, leaf.Hash)
	}

	return hashes
}

func makeBatchProof() (*accumulator.BatchProof, error) {
	// Create forest and add testLeaves.
	f := accumulator.NewForest(accumulator.RamForest, nil, "", 0)
	_, err := f.Modify(testLeaves, nil)
	if err != nil {
		return nil, err
	}

	leaves := testLeavesToHashes()

	// Generate BatchProof, only prove one.
	batchProof, err := f.ProveBatch(leaves[:1])
	if err != nil {
		return nil, err
	}

	return &batchProof, nil
}

func compareBatchProof(bp, newBP *accumulator.BatchProof) error {
	if len(bp.Targets) != len(newBP.Targets) {
		return fmt.Errorf("BatchProofs differ. Expect target length of %d, got %d",
			len(bp.Targets), len(newBP.Targets))
	}

	if len(bp.Proof) != len(newBP.Proof) {
		return fmt.Errorf("BatchProofs differ. Expect proof length of %d, got %d",
			len(bp.Proof), len(newBP.Proof))
	}

	for i := range bp.Targets {
		if bp.Targets[i] != newBP.Targets[i] {
			return fmt.Errorf("Expected target of %d, got %d",
				bp.Targets[i], newBP.Targets[i])
		}
	}

	for i := range bp.Proof {
		if bp.Proof[i] != newBP.Proof[i] {
			return fmt.Errorf("Expected proof of %d, got %d",
				bp.Proof[i], newBP.Proof[i])
		}
	}

	return nil
}

func TestBatchProofSerializeSize(t *testing.T) {
	t.Parallel()

	bp, err := makeBatchProof()
	if err != nil {
		t.Fatal(err)
	}

	// 1 byte target len + one 8 byte target +
	// 1 byte proof len + three proofs that are each 32 bytes (total 96 bytes)
	if BatchProofSerializeSize(bp) != 106 {
		err := fmt.Errorf("expect size of %d but got %d",
			106, BatchProofSerializeSize(bp))
		t.Errorf(err.Error())
	}
}

func TestSerializeBatchProof(t *testing.T) {
	t.Parallel()

	bp, err := makeBatchProof()
	if err != nil {
		t.Fatal(err)
	}

	// Serialize the batchproof.
	var w bytes.Buffer
	err = bp.Serialize(&w)
	if err != nil {
		t.Fatal(err)
	}
	serializedBytes := w.Bytes()

	var newBP accumulator.BatchProof

	// Deserialize the batchproof.
	r := bytes.NewBuffer(serializedBytes)
	err = newBP.Deserialize(r)
	if err != nil {
		t.Fatal(err)
	}

	err = compareBatchProof(bp, &newBP)
	if err != nil {
		t.Fatal(err)
	}

	var newW bytes.Buffer
	newBP.Serialize(&newW)
	newSerializedBytes := newW.Bytes()

	if !bytes.Equal(newSerializedBytes, serializedBytes) {
		err := fmt.Errorf("Serialized BatchProof not equal. Expect %s, got %s",
			hex.EncodeToString(serializedBytes),
			hex.EncodeToString(newSerializedBytes))
		t.Fatal(err)
	}
}

// Leaves made for testing purposes.
var testLeaves = []accumulator.Leaf{
	{
		Hash:     sha512.Sum512_256([]byte{0}),
		Remember: true,
	},
	{
		Hash:     sha512.Sum512_256([]byte{1}),
		Remember: true,
	},
	{
		Hash:     sha512.Sum512_256([]byte{2}),
		Remember: true,
	},
	{
		Hash:     sha512.Sum512_256([]byte{3}),
		Remember: true,
	},
	{
		Hash:     sha512.Sum512_256([]byte{4}),
		Remember: false,
	},
	{
		Hash:     sha512.Sum512_256([]byte{5}),
		Remember: false,
	},
	{
		Hash:     sha512.Sum512_256([]byte{6}),
		Remember: false,
	},
	{
		Hash:     sha512.Sum512_256([]byte{7}),
		Remember: true,
	},
}
