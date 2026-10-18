// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcdwire

// COPIED FROM BTCD'S WIRE PACKAGE. These are wire's validateElementCount and
// the helpers it calls, from btcd's master branch since wire/v2 v2.0.1 doesn't
// have them, with validateElementCount exported for the BIP-183 decoders.

import (
	"bytes"
	"io"
)

// readerRemaining returns the number of buffered bytes exposed by the concrete
// readers used by wire decoders.
func readerRemaining(r io.Reader) (uint64, bool) {
	switch r := r.(type) {
	case *bytes.Buffer:
		return uint64(r.Len()), true

	case *bytes.Reader:
		return uint64(r.Len()), true

	default:
		return 0, false
	}
}

// ValidateElementCount ensures a buffered payload has enough bytes remaining
// to encode the claimed number of elements before a decoder allocates for the
// count. The framed wire path passes a bytes.Buffer to each message decoder,
// while callers without a measurable remainder retain streaming semantics.
func ValidateElementCount(r io.Reader, count,
	minElementSize uint64) error {

	_, err := canPreallocateElements(r, count, minElementSize)
	return err
}

// canPreallocateElements reports whether a reader proves that the minimum
// encoding for count elements is already buffered. Readers without a
// measurable remainder must grow element storage as decoding makes progress.
func canPreallocateElements(r io.Reader, count,
	minElementSize uint64) (bool, error) {

	remaining, ok := readerRemaining(r)
	if !ok || minElementSize == 0 {
		return false, nil
	}

	if count <= remaining/minElementSize {
		return true, nil
	}

	return false, elementCountError(remaining)
}

// elementCountError preserves the short-read error returned by the element
// decoder when the claimed vector cannot fit in the remaining payload.
func elementCountError(remaining uint64) error {
	if remaining == 0 {
		return io.EOF
	}

	return io.ErrUnexpectedEOF
}
