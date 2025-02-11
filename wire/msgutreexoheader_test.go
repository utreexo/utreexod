// Copyright (c) 2024-2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"testing"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

func TestUtreexoBlockHeaderSerializeSize(t *testing.T) {
	tests := []struct {
		data MsgUtreexoHeader
	}{
		{
			data: MsgUtreexoHeader{
				BlockHash: chainhash.HashH([]byte{1}),
				NumAdds:   8,
				Targets:   []uint64{14181, 484518},
			},
		},
		{
			data: MsgUtreexoHeader{
				BlockHash: chainhash.HashH([]byte{5}),
				NumAdds:   1544,
				Targets:   []uint64{14181, 484518, 11774, 148185, 189416854, 15481, 18518, 2},
			},
		},
	}

	for _, test := range tests {
		var buf bytes.Buffer
		err := test.data.Serialize(&buf)
		if err != nil {
			t.Fatal(err)
		}

		expected := len(buf.Bytes())
		got := test.data.SerializeSize()
		if got != expected {
			t.Fatalf("expected %v, got %v", expected, got)
		}
	}
}
