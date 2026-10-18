// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcdwire

import (
	"bytes"
	"io"
	"testing"
)

// TestValidateElementCount ensures element counts are checked against the
// bytes left in buffered readers and not checked for other readers.
func TestValidateElementCount(t *testing.T) {
	tests := []struct {
		r       io.Reader
		count   uint64
		minSize uint64
		err     error
	}{
		{bytes.NewBuffer(make([]byte, 64)), 2, 32, nil},
		{bytes.NewBuffer(make([]byte, 64)), 3, 32, io.ErrUnexpectedEOF},
		{bytes.NewReader(make([]byte, 64)), 2, 32, nil},
		{bytes.NewReader(make([]byte, 64)), 3, 32, io.ErrUnexpectedEOF},
		{bytes.NewBuffer(nil), 0, 32, nil},
		{bytes.NewBuffer(nil), 1, 32, io.EOF},
		{bytes.NewBuffer(make([]byte, 7)), 7, 1, nil},
		{bytes.NewBuffer(make([]byte, 7)), 1<<64 - 1, 1, io.ErrUnexpectedEOF},
		{bytes.NewBuffer(make([]byte, 7)), 1<<64 - 1, 0, nil},
		{io.LimitReader(bytes.NewReader(nil), 0), 1<<64 - 1, 32, nil},
	}

	for i, test := range tests {
		err := ValidateElementCount(test.r, test.count, test.minSize)
		if err != test.err {
			t.Errorf("ValidateElementCount #%d wrong error got: %v, "+
				"want: %v", i, err, test.err)
		}
	}
}
