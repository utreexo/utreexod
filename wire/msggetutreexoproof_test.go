package wire

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

func randomBytes(size int) []byte {
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		panic("failed to generate random bytes")
	}
	return b
}

func TestMsgGetUtreexoProofEncodeDecode(t *testing.T) {
	testCases := []struct {
		name string
		msg  MsgGetUtreexoProof
	}{
		{
			name: "Basic case",
			msg: MsgGetUtreexoProof{
				BlockHash:        chainhash.HashH([]byte("basic test hash")),
				ProofIndexBitMap: []byte{1, 2, 3, 4},
				LeafIndexBitMap:  []byte{5, 6, 7, 8},
			},
		},
		{
			name: "Empty indexes",
			msg: MsgGetUtreexoProof{
				BlockHash:        chainhash.HashH([]byte("empty test hash")),
				ProofIndexBitMap: []byte{},
				LeafIndexBitMap:  []byte{},
			},
		},
		{
			name: "Large indexes",
			msg: MsgGetUtreexoProof{
				BlockHash:        chainhash.HashH([]byte("large test hash")),
				ProofIndexBitMap: randomBytes(100),
				LeafIndexBitMap:  randomBytes(100),
			},
		},
	}

	for _, tc := range testCases {
		var buf bytes.Buffer
		pver := uint32(70015) // Example protocol version

		// Encode the message
		err := tc.msg.BtcEncode(&buf, pver, LatestEncoding)
		assert.NoError(t, err, "BtcEncode should not return an error")

		// Decode into a new message
		var decodedMsg MsgGetUtreexoProof
		err = decodedMsg.BtcDecode(&buf, pver, LatestEncoding)
		assert.NoError(t, err, "BtcDecode should not return an error")

		// Verify the decoded message matches the original
		assert.Equal(t, tc.msg.BlockHash, decodedMsg.BlockHash, "BlockHash should match")
		assert.Equal(t, tc.msg.ProofIndexBitMap, decodedMsg.ProofIndexBitMap, "ProofIndexBitMap should match")
		assert.Equal(t, tc.msg.LeafIndexBitMap, decodedMsg.LeafIndexBitMap, "LeafIndexBitMap should match")
	}
}

func setBitSlice(size int, bitIndexes []int) []byte {
	b := make([]byte, size)
	for _, bitIndex := range bitIndexes {
		byteIndex := bitIndex / 8
		bitOffset := bitIndex % 8
		if byteIndex < size {
			b[byteIndex] |= (1 << bitOffset)
		}
	}
	return b
}

func TestIsBitSet(t *testing.T) {
	testCases := []struct {
		slice    []byte
		indexes  []int
		expected []bool
	}{
		{
			slice:    setBitSlice(32, []int{1, 4}),
			indexes:  []int{1, 2, 4},
			expected: []bool{true, false, true},
		},

		{
			slice:    setBitSlice(100, []int{1, 4, 98}),
			indexes:  []int{1, 2, 3, 55, 98, 100},
			expected: []bool{true, false, false, false, true, false},
		},
	}

	for _, tc := range testCases {
		for i, index := range tc.indexes {
			assert.Equal(t, tc.expected[i], isBitSet(tc.slice, index))
		}
	}
}

func TestCreateBitmap(t *testing.T) {
	testCases := []struct {
		name     string
		includes []bool
		expected []byte
	}{
		{
			name:     "All false",
			includes: []bool{false, false, false, false, false, false, false, false},
			expected: []byte{0x00},
		},
		{
			name:     "All true",
			includes: []bool{true, true, true, true, true, true, true, true},
			expected: []byte{0xFF},
		},
		{
			name:     "Alternating true/false",
			includes: []bool{true, false, true, false, true, false, true, false},
			expected: []byte{0x55}, // 0b01010101
		},
		{
			name:     "Partial byte (less than 8 bits)",
			includes: []bool{true, false, true, false, true},
			expected: []byte{0x15}, // 0b00010101
		},
		{
			name: "Multi-byte input",
			includes: []bool{
				true, false, true, false, true, false, true, false, // 0b01010101
				false, true, false, true, false, true, false, true, // 0b10101010
			},
			expected: []byte{0x55, 0xAA},
		},
		{
			name:     "Single bit set at end",
			includes: []bool{false, false, false, false, false, false, false, true},
			expected: []byte{0x80}, // 0b10000000
		},
	}

	for _, tc := range testCases {
		bitmap := createBitmap(tc.includes)
		assert.Equal(t, tc.expected, bitmap)
	}
}
