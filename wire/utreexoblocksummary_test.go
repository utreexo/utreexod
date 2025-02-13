// Copyright (c) 2024-2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

func TestUtreexoBlockHeaderSerialize(t *testing.T) {
	tests := []struct {
		data UtreexoBlockSummary
	}{
		{
			data: UtreexoBlockSummary{
				BlockHash:    chainhash.HashH([]byte{1}),
				NumAdds:      8,
				BlockTargets: []uint64{14181, 484518},
			},
		},
		{
			data: UtreexoBlockSummary{
				BlockHash:    chainhash.HashH([]byte{1}),
				NumAdds:      8,
				BlockTargets: []uint64{14181, 484518},
			},
		},
		{
			data: UtreexoBlockSummary{
				BlockHash:    chainhash.HashH([]byte{5}),
				NumAdds:      1544,
				BlockTargets: []uint64{14181, 484518, 11774, 148185, 189416854, 15481, 18518, 2},
			},
		},
		{
			data: UtreexoBlockSummary{
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
		got := UtreexoBlockSummary{}
		err = got.Deserialize(r)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.data) {
			t.Fatalf("expected %v, got %v", test.data, got)
		}
	}
}
