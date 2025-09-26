// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"reflect"
	"testing"
)

func TestUtreexoTTLsSerialize(t *testing.T) {
	tests := []struct {
		data UtreexoTTL
	}{
		{
			data: UtreexoTTL{
				BlockHeight: 1,
				TTLs: []TTLInfo{
					{0, 1, 8},
				},
			},
		},
		{
			data: UtreexoTTL{
				BlockHeight: 4785,
				TTLs: []TTLInfo{
					{0, 4, 8},
					{1, 2, 56},
					{4, 0, 141},
					{5, 12, 0},
					{0, 14, 0},
					{1, 50, 1841},
					{522, 1354, 878418},
					{1, 0, 876},
				},
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
		got := UtreexoTTL{}
		err = got.Deserialize(r)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, test.data) {
			t.Fatalf("expected %v, got %v", test.data, got)
		}
	}
}
