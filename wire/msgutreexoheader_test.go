// Copyright (c) 2024-2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

func TestUtreexoBlockHeaderSerialize(t *testing.T) {
	tests := []struct {
		data MsgUtreexoHeader
	}{
		{
			data: MsgUtreexoHeader{
				BlockHash:    chainhash.HashH([]byte{1}),
				NumAdds:      8,
				BlockTargets: []uint64{14181, 484518},
				ProofTargets: []uint64{33, 34, 35, 36, 37, 38},
				ProofHashes: []utreexo.Hash{
					utreexo.Hash(chainhash.HashH([]byte{1})),
					utreexo.Hash(chainhash.HashH([]byte{2})),
					utreexo.Hash(chainhash.HashH([]byte{3})),
					utreexo.Hash(chainhash.HashH([]byte{4})),
				},
			},
		},
		{
			data: MsgUtreexoHeader{
				BlockHash:    chainhash.HashH([]byte{1}),
				NumAdds:      8,
				BlockTargets: []uint64{14181, 484518},
			},
		},
		{
			data: MsgUtreexoHeader{
				BlockHash:    chainhash.HashH([]byte{5}),
				NumAdds:      1544,
				BlockTargets: []uint64{14181, 484518, 11774, 148185, 189416854, 15481, 18518, 2},
				ProofTargets: []uint64{84154, 84155, 84156, 84157, 84158, 84159, 84160, 84161, 84162, 84163, 84164, 84165},
				ProofHashes: []utreexo.Hash{
					utreexo.Hash(chainhash.HashH([]byte{6})),
					utreexo.Hash(chainhash.HashH([]byte{7})),
					utreexo.Hash(chainhash.HashH([]byte{8})),
					utreexo.Hash(chainhash.HashH([]byte{9})),
				},
			},
		},
		{
			data: MsgUtreexoHeader{
				BlockHash:    chainhash.HashH([]byte{5}),
				NumAdds:      1544,
				BlockTargets: []uint64{14181, 484518, 11774, 148185, 189416854, 15481, 18518, 2},
			},
		},
	}

	for _, test := range tests {
		var buf bytes.Buffer
		err := test.data.Serialize(&buf)
		if err != nil {
			t.Fatal(err)
		}

		b := buf.Bytes()

		// Check size.
		expectedSize := len(b)
		gotSize := test.data.SerializeSize()
		if gotSize != expectedSize {
			t.Fatalf("expected %v, got %v", expectedSize, gotSize)
		}

		// Check data.
		r := bytes.NewBuffer(b)
		got := MsgUtreexoHeader{}
		err = got.Deserialize(r)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.data) {
			t.Fatalf("expected %v, got %v", test.data, got)
		}
	}
}
