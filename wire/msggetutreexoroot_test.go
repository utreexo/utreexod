// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"testing"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

var genesisHash = chainhash.Hash([chainhash.HashSize]byte{ // Make go vet happy.
	0x6f, 0xe2, 0x8c, 0x0a, 0xb6, 0xf1, 0xb3, 0x72,
	0xc1, 0xa6, 0xa2, 0x46, 0xae, 0x63, 0xf7, 0x4f,
	0x93, 0x1e, 0x83, 0x65, 0xe1, 0x5a, 0x08, 0x9c,
	0x68, 0xd6, 0x19, 0x00, 0x00, 0x00, 0x00, 0x00,
})

func TestMsgGetUtreexoRootEncode(t *testing.T) {
	testCases := []struct {
		hash chainhash.Hash
	}{
		{
			hash: genesisHash,
		},
	}

	for _, testCase := range testCases {
		beforeMsg := NewMsgGetUtreexoRoot(testCase.hash)

		// Encode.
		var buf bytes.Buffer
		err := beforeMsg.BtcEncode(&buf, 0, LatestEncoding)
		if err != nil {
			t.Fatal(err)
		}

		serialized := buf.Bytes()

		afterMsg := MsgGetUtreexoRoot{}
		r := bytes.NewReader(serialized)
		err = afterMsg.BtcDecode(r, 0, LatestEncoding)
		if err != nil {
			t.Fatal(err)
		}

		if !afterMsg.BlockHash.IsEqual(&testCase.hash) {
			t.Fatalf("expected %v but got %v",
				testCase.hash, afterMsg.BlockHash)
		}
	}
}
