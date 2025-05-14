// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

func TestUtreexoProofSerialize(t *testing.T) {
	tests := []struct {
		data MsgUtreexoProof
	}{
		{
			data: MsgUtreexoProof{
				BlockHash: chainhash.HashH([]byte{1}),
				ProofHashes: []utreexo.Hash{
					{0xff, 0x0, 0x4},
					{0xff, 0x1, 0xd, 0xdf},
				},
				Targets: []uint64{1254548, 481754},
				LeafDatas: []LeafData{
					{
						Height:     784611,
						IsCoinBase: true,
						Amount:     631465945,
						PkScript:   []byte{},
					},
					{
						Height:     784631,
						IsCoinBase: false,
						Amount:     637465960,
						PkScript:   []byte{},
					},
				},
			},
		},
	}

	for _, test := range tests {
		var buf bytes.Buffer
		err := test.data.BtcEncode(&buf, 0, LatestEncoding)
		if err != nil {
			t.Fatal(err)
		}

		b := buf.Bytes()

		// Check data.
		r := bytes.NewBuffer(b)
		got := MsgUtreexoProof{}
		err = got.BtcDecode(r, 0, LatestEncoding)
		if err != nil {
			t.Fatal(err)
		}

		require.Equal(t, test.data, got)
	}
}
