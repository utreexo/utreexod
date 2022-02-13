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

	"github.com/mit-dci/utreexo/accumulator"
)

func leavesToHashes(leaves []accumulator.Leaf) []accumulator.Hash {
	hashes := make([]accumulator.Hash, 0, len(leaves))
	for _, leaf := range leaves {
		hashes = append(hashes, leaf.Hash)
	}

	return hashes
}

func makeBatchProofs() ([]*accumulator.BatchProof, error) {
	// Create forest and add testLeaves.
	f := accumulator.NewForest(accumulator.RamForest, nil, "", 0)
	_, err := f.Modify(testLeaves, nil)
	if err != nil {
		return nil, err
	}

	leaves := leavesToHashes(testLeaves)

	bps := []*accumulator.BatchProof{}

	for i := 1; i < len(leaves); i++ {
		batchProof, err := f.ProveBatch(leaves[:i])
		if err != nil {
			return nil, err
		}
		bps = append(bps, &batchProof)
	}

	return bps, nil
}

func makeRandBatchProofs(leafCount, bpCount int) (int64, []accumulator.BatchProof, error) {
	// Get random seed.
	time := time.Now().UnixNano()
	seed := rand.NewSource(time)
	rand := rand.New(seed)

	// Create forest.
	f := accumulator.NewForest(accumulator.RamForest, nil, "", 0)
	leaves := make([]accumulator.Leaf, 0, leafCount)
	for i := 0; i < leafCount; i++ {
		leafVal, ok := quick.Value(reflect.TypeOf(accumulator.Leaf{}), rand)
		if !ok {
			return time, nil, fmt.Errorf("Couldn't create a random leaf")
		}
		leaf := leafVal.Interface().(accumulator.Leaf)
		leaf.Remember = true

		leaves = append(leaves, leaf)
	}

	// Add all the leaves.
	_, err := f.Modify(leaves, nil)
	if err != nil {
		return time, nil, err
	}

	// Create all the requested batch proofs.
	bps := make([]accumulator.BatchProof, 0, bpCount)
	for i := 0; i < bpCount; i++ {
		// Get a random number of leaves to prove.
		proveCount := rand.Intn(leafCount)

		// Grab random leaves and add to the leaves to be proven.
		leavesToProve := make([]accumulator.Leaf, 0, proveCount)
		for j := 0; j < proveCount; j++ {
			idx := rand.Intn(leafCount)
			leavesToProve = append(leavesToProve, leaves[idx])
		}

		hashes := leavesToHashes(leavesToProve)

		bp, err := f.ProveBatch(hashes)
		if err != nil {
			return time, nil, err
		}

		bps = append(bps, bp)
	}

	return time, bps, nil
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
	bp   accumulator.BatchProof
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
		bp: accumulator.BatchProof{Targets: []uint64{0},
			Proof: []accumulator.Hash{
				accumulator.Hash(*newHashFromStr("e76ed48f7d6a947c50841e00a37fdd3b0c562a767a4fca4de4c6fa45c3718b2a")),
				accumulator.Hash(*newHashFromStr("1b719855236212adb6442f9b55909bfec484edd3236aa6021425cf8c4ae2e90f")),
				accumulator.Hash(*newHashFromStr("846fa80f86d68d0143f6deba79fc5e4cc539b799a63ab6469845d47a38d98884")),
				accumulator.Hash(*newHashFromStr("09859744ff2fc432c6d0348dab74c79f32f1afc569cc7e844396e05d65e88dec")),
			},
		},
		size: 131, // 1 + 1 + 1 + 128
	},
	{
		name: "15 leaves, prove 1-8",
		bp: accumulator.BatchProof{
			Targets: []uint64{1, 2, 3, 4, 5, 6, 7, 8},
			Proof: []accumulator.Hash{
				accumulator.Hash(*newHashFromStr("a0f628257e495d20a8604f731ef1212393d422ecc93314950adec6feee3dfa72")),
				accumulator.Hash(*newHashFromStr("4e7ab445b1165870d243a0f51ed54f74de2f11c625aac1eb7408218491248c40")),
				accumulator.Hash(*newHashFromStr("f497452128c3af13f940a8b02aa5bf0868336316f6b38b15fa4e94047f62857c")),
			},
		},
		size: 106, // 1 + 8 + 1 + 96
	},
	{
		name: "15 leaves, prove 0-14",
		bp: accumulator.BatchProof{
			Targets: []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14},
			Proof: []accumulator.Hash{
				accumulator.Hash(*newHashFromStr("36b8f508a26a86902dcc5a325c0033e4ac56d641a628c835a28c930aa8efea14")),
			},
		},
		size: 49, // 1 + 15 + 1 + 32
	},
	// The below was fetched from signet.
	{
		name: "signet batchproof 14872",
		bp: accumulator.BatchProof{
			Targets: []uint64{12042, 125690},
			Proof: []accumulator.Hash{
				accumulator.Hash(*newHashFromStr("bb1ef355ab967f8879183454c0dfefdb52d31eabfed2a0531974e76df72d4f4f")),
				accumulator.Hash(*newHashFromStr("096130bd97984834f53a3691fd60c1f2a1ef682fda5bca5100c7e20ca7530fa9")),
				accumulator.Hash(*newHashFromStr("d1f826d1c4e7285f8c0c5d38a90d76c6d0222dd7ab3919edecac5ce7161a66f7")),
				accumulator.Hash(*newHashFromStr("e309134fc656838dff46266ae0a251e4c16d56253441e2c8b675cca1c007fc33")),
				accumulator.Hash(*newHashFromStr("65ea664f530e4d59feea2b7ceaa1ff1d9f373a39e5c428b75d367bd50e05d6a8")),
				accumulator.Hash(*newHashFromStr("92e0277312b1ff84d7a3d700fad30906586f313a9df85cc5d2fe3dcfc9ce9f57")),
				accumulator.Hash(*newHashFromStr("51fefe9badf11426d6868d08f65c231d228caec82827d9126186fbd8fbef8f3a")),
				accumulator.Hash(*newHashFromStr("d439c846256b79a81e7a78cedac687edc61c7df5ce2a7144f68297e4a7633be8")),
				accumulator.Hash(*newHashFromStr("c823770de618684d16796e873fb0a1752fc515294b54788c7ee79a01a4827990")),
				accumulator.Hash(*newHashFromStr("bcbb54d6dc471449e815077bc85ef3038af66e1daa7fdc0b3ac4d9f2301df379")),
				accumulator.Hash(*newHashFromStr("05717dc54d7b04b18b124b76a76a4c9b8e25f6f56442600955eb591df03ad8fc")),
				accumulator.Hash(*newHashFromStr("93d666021eb954e45d243a02eba6b45a5c5769928a3aeadd4daafbb30d96dfe4")),
				accumulator.Hash(*newHashFromStr("bf9964abb64f4924c06d395c37996dda9a426ef9cd55559ec7518e49bfeba43f")),
				accumulator.Hash(*newHashFromStr("26f41da695b37d472b147c66a62338e171a0d48ae63425d57d1cab004386cf3f")),
				accumulator.Hash(*newHashFromStr("404267aa20916355e1a6f05cdfc41173c7d1231ea0542986be32723e605dfe53")),
				accumulator.Hash(*newHashFromStr("bb5e9a70add55d582605b1d3747e8863ede1e93ba0f3581eb90d1771e061c386")),
				accumulator.Hash(*newHashFromStr("64cd4acc1920f11eab803bcde2c7cdcee6182c07716e712fc725da079f3a0fd4")),
			},
		},
		size: 554, // 1 + 8 + 1 + 544
	},
}

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
