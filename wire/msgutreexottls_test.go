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
				TTLs:        []uint64{0},
			},
		},
		{
			data: UtreexoTTL{
				BlockHeight: 4785,
				TTLs:        []uint64{0, 1, 4, 5, 0, 0, 1, 522, 1},
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
