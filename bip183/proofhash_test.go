// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip183

import (
	"reflect"
	"testing"
)

func TestPackedHashes(t *testing.T) {
	tests := []struct {
		uints []uint64
	}{
		{uints: []uint64{0, 1, 2, 3}},
		{uints: []uint64{0, 1, 2, 3, 4}},
		{uints: []uint64{0, 1, 2, 3, 4, 5}},
		{uints: []uint64{0, 1, 2, 3, 4, 5, 6}},
		{uints: []uint64{0, 1, 2, 3, 4, 5, 6, 7}},
		{uints: []uint64{11, 22, 33, 44, 55}},
	}

	for _, test := range tests {
		hashes := Uint64sToPackedHashes(test.uints)
		got := PackedHashesToUint64(hashes)

		if !reflect.DeepEqual(test.uints, got) {
			t.Fatalf("expected %v but got %v", test.uints, got)
		}
	}
}
