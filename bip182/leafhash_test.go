// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip182

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"testing"
)

func TestTaggedHash512_256(t *testing.T) {
	tests := []struct {
		tagStr string
		msg    string
	}{
		{tagStr: "UtreexoV1", msg: "hi"},
		{tagStr: "UtreexoV1", msg: "12354561654"},
		{tagStr: "hi", msg: "12354561654"},
	}

	for _, test := range tests {
		var serialized bytes.Buffer
		tag, found := precomputedUtreexoTags[test.tagStr]
		if !found {
			tag = sha512.Sum512([]byte(test.tagStr))
		}
		serialized.Write(tag[:])
		serialized.Write(tag[:])
		serialized.Write([]byte(test.msg))

		expect := sha512.Sum512_256(serialized.Bytes())

		got := TaggedHash512_256([]byte(test.tagStr), func(w io.Writer) { w.Write([]byte(test.msg)) })
		if !bytes.Equal(got[:], expect[:]) {
			t.Fatalf("expected %s, got %s", hex.EncodeToString(expect[:]), hex.EncodeToString(got[:]))
		}
	}
}
