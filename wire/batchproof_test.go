// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	"github.com/utreexo/utreexo"
)

func leavesToHashes(leaves []utreexo.Leaf) []utreexo.Hash {
	hashes := make([]utreexo.Hash, 0, len(leaves))
	for _, leaf := range leaves {
		hashes = append(hashes, leaf.Hash)
	}

	return hashes
}

func makeBatchProofs() ([]*utreexo.Proof, error) {
	// Create forest and add testLeaves.
	p := utreexo.NewAccumulator(true)
	err := p.Modify(testLeaves, nil, utreexo.Proof{})
	if err != nil {
		return nil, err
	}

	leaves := leavesToHashes(testLeaves)

	bps := []*utreexo.Proof{}

	for i := 1; i < len(leaves); i++ {
		batchProof, err := p.Prove(leaves[:i])
		if err != nil {
			return nil, err
		}
		bps = append(bps, &batchProof)
	}

	return bps, nil
}

func makeRandBatchProofs(leafCount, bpCount int) (int64, []utreexo.Proof, error) {
	// Get random seed.
	time := time.Now().UnixNano()
	seed := rand.NewSource(time)
	rand := rand.New(seed)

	// Create forest.
	p := utreexo.NewAccumulator(true)
	leaves := make([]utreexo.Leaf, 0, leafCount)
	for i := 0; i < leafCount; i++ {
		leafVal, ok := quick.Value(reflect.TypeOf(utreexo.Leaf{}), rand)
		if !ok {
			return time, nil, fmt.Errorf("Couldn't create a random leaf")
		}
		leaf := leafVal.Interface().(utreexo.Leaf)
		leaf.Remember = true

		leaves = append(leaves, leaf)
	}

	// Add all the leaves.
	err := p.Modify(leaves, nil, utreexo.Proof{})
	if err != nil {
		return time, nil, err
	}

	// Create all the requsted batch proofs.
	bps := make([]utreexo.Proof, 0, bpCount)
	for i := 0; i < bpCount; i++ {
		// Get a random number of leaves to prove.
		proveCount := rand.Intn(leafCount)

		// Grab random leaves and add to the leaves to be proven.
		leavesToProve := make([]utreexo.Leaf, 0, proveCount)
		for j := 0; j < proveCount; j++ {
			idx := rand.Intn(leafCount)
			leavesToProve = append(leavesToProve, leaves[idx])
		}

		hashes := leavesToHashes(leavesToProve)

		bp, err := p.Prove(hashes)
		if err != nil {
			return time, nil, err
		}

		bps = append(bps, bp)
	}

	return time, bps, nil
}

func compareBatchProof(bp, newBP *utreexo.Proof) error {
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

	for _, test := range testBatchProofs {
		if BatchProofSerializeSize(&test.bp) != test.size {
			err := fmt.Errorf("TestBatchProofSerializeSize \"%s\": expected size of %d but got %d",
				test.name, test.size, BatchProofSerializeSize(&test.bp))
			t.Errorf(err.Error())
		}

		var buf bytes.Buffer
		err := BatchProofSerialize(&buf, &test.bp)
		if err != nil {
			t.Fatal(err)
		}

		if test.size != len(buf.Bytes()) {
			err := fmt.Errorf("TestBatchProofSerializeSize \"%s\": serialized bytes len of %d but serialize size of %d",
				test.name, len(buf.Bytes()), BatchProofSerializeSize(&test.bp))
			t.Errorf(err.Error())
		}
	}
}

func TestSerializeBatchProof(t *testing.T) {
	t.Parallel()

	bps, err := makeBatchProofs()
	if err != nil {
		t.Fatal(err)
	}

	for _, bp := range bps {
		// Serialize the batchproof.
		var w bytes.Buffer
		err = BatchProofSerialize(&w, bp)
		if err != nil {
			t.Fatal(err)
		}
		serializedBytes := w.Bytes()

		// Deserialize the batchproof.
		r := bytes.NewBuffer(serializedBytes)
		newBP, err := BatchProofDeserialize(r)
		if err != nil {
			t.Fatal(err)
		}

		err = compareBatchProof(bp, newBP)
		if err != nil {
			t.Fatal(err)
		}

		var newW bytes.Buffer
		err = BatchProofSerialize(&newW, newBP)
		if err != nil {
			t.Fatal(err)
		}
		newSerializedBytes := newW.Bytes()

		if !bytes.Equal(newSerializedBytes, serializedBytes) {
			err := fmt.Errorf("Serialized BatchProof not equal. Expect %s, got %s",
				hex.EncodeToString(serializedBytes),
				hex.EncodeToString(newSerializedBytes))
			t.Fatal(err)
		}
	}
}

func TestSerializeRandBatchProof(t *testing.T) {
	t.Parallel()

	seedTime, bps, err := makeRandBatchProofs(100000, 20)
	if err != nil {
		t.Fatal(err)
	}

	for _, bp := range bps {
		// Serialize the batchproof.
		var w bytes.Buffer
		err = BatchProofSerialize(&w, &bp)
		if err != nil {
			t.Fatal(err)
		}
		serializedBytes := w.Bytes()

		// Deserialize the batchproof.
		r := bytes.NewBuffer(serializedBytes)
		newBP, err := BatchProofDeserialize(r)
		if err != nil {
			t.Fatal(err)
		}

		err = compareBatchProof(&bp, newBP)
		if err != nil {
			t.Fatal(err)
		}

		var newW bytes.Buffer
		err = BatchProofSerialize(&newW, newBP)
		if err != nil {
			t.Fatal(err)
		}
		newSerializedBytes := newW.Bytes()

		if !bytes.Equal(newSerializedBytes, serializedBytes) {
			err := fmt.Errorf("SeedTime %v, Serialized BatchProof not equal. Expect len %d, got len %d",
				seedTime,
				len(serializedBytes),
				len(newSerializedBytes))
			t.Fatal(err)
		}
	}
}

type batchProofTest struct {
	name string
	bp   utreexo.Proof
	size int
}

var testBatchProofs = []batchProofTest{
	// The below two batch proofs are made with testLeaves and the tree looks like
	// the one below.  The leaves values are hashed with sha512_256.
	//
	// 30
	// |-------------------------------\
	// 28                              29
	// |---------------\               |---------------\
	// 24              25              26              27
	// |-------\       |-------\       |-------\       |-------\
	// 16      17      18      19      20      21      22      23
	// |---\   |---\   |---\   |---\   |---\   |---\   |---\   |---\
	// 00  01  02  03  04  05  06  07  08  09  10  11  12  13  14  15
	{
		name: "15 leaves, prove 0",
		bp: utreexo.Proof{Targets: []uint64{0},
			Proof: []utreexo.Hash{
				utreexo.Hash(*newHashFromStr("e76ed48f7d6a947c50841e00a37fdd3b0c562a767a4fca4de4c6fa45c3718b2a")),
				utreexo.Hash(*newHashFromStr("1b719855236212adb6442f9b55909bfec484edd3236aa6021425cf8c4ae2e90f")),
				utreexo.Hash(*newHashFromStr("846fa80f86d68d0143f6deba79fc5e4cc539b799a63ab6469845d47a38d98884")),
				utreexo.Hash(*newHashFromStr("09859744ff2fc432c6d0348dab74c79f32f1afc569cc7e844396e05d65e88dec")),
			},
		},
		size: 131, // 1 + 1 + 1 + 128
	},
	{
		name: "15 leaves, prove 1-8",
		bp: utreexo.Proof{
			Targets: []uint64{1, 2, 3, 4, 5, 6, 7, 8},
			Proof: []utreexo.Hash{
				utreexo.Hash(*newHashFromStr("a0f628257e495d20a8604f731ef1212393d422ecc93314950adec6feee3dfa72")),
				utreexo.Hash(*newHashFromStr("4e7ab445b1165870d243a0f51ed54f74de2f11c625aac1eb7408218491248c40")),
				utreexo.Hash(*newHashFromStr("f497452128c3af13f940a8b02aa5bf0868336316f6b38b15fa4e94047f62857c")),
			},
		},
		size: 106, // 1 + 8 + 1 + 96
	},
	{
		name: "15 leaves, prove 0-14",
		bp: utreexo.Proof{
			Targets: []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14},
			Proof: []utreexo.Hash{
				utreexo.Hash(*newHashFromStr("36b8f508a26a86902dcc5a325c0033e4ac56d641a628c835a28c930aa8efea14")),
			},
		},
		size: 49, // 1 + 15 + 1 + 32
	},
	// The below was fetched from signet.
	{
		name: "signet batchproof 14872",
		bp: utreexo.Proof{
			Targets: []uint64{15474, 15478},
			Proof: []utreexo.Hash{
				utreexo.Hash(*newHashFromStr("7039d1916a1da960bcbd3762274fbde7231c020f6dc2cffd0693ce2915816e14")),
				utreexo.Hash(*newHashFromStr("f5f358fedb0e26c40a8daa476a57b75c128513f968816de2e362a652ed2bcf9c")),
				utreexo.Hash(*newHashFromStr("c9978759587c0beb3c543c80ac26fcbab2ff60a56725c412a1ad381eb88d3923")),
				utreexo.Hash(*newHashFromStr("ae46f987bcb72a34ff83382341c4f00647940bb75a643ffa2287fe9bac1fe027")),
			},
		},
		size: 136, // 1 + 6 + 1 + 128
	},
}

var testLeaves = []utreexo.Leaf{
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
	{
		Hash:     sha512.Sum512_256([]byte{8}),
		Remember: true,
	},
	{
		Hash:     sha512.Sum512_256([]byte{9}),
		Remember: true,
	},
	{
		Hash:     sha512.Sum512_256([]byte{10}),
		Remember: true,
	},
	{
		Hash:     sha512.Sum512_256([]byte{11}),
		Remember: true,
	},
	{
		Hash:     sha512.Sum512_256([]byte{12}),
		Remember: true,
	},
	{
		Hash:     sha512.Sum512_256([]byte{13}),
		Remember: false,
	},
	{
		Hash:     sha512.Sum512_256([]byte{14}),
		Remember: false,
	},
	{
		Hash:     sha512.Sum512_256([]byte{15}),
		Remember: false,
	},
}
