// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcdwire

import (
	"encoding/binary"
	"io"
	"sync"

	"github.com/utreexo/utreexod/wire"
)

// ScratchPool holds buffers that the read and write helpers use as scratch
// space so serializing integers doesn't allocate.
var ScratchPool = sync.Pool{
	New: func() interface{} { return new([8]byte) },
}

// ReadUint8 reads a single byte from r using buf as scratch space.
func ReadUint8(r io.Reader, buf []byte) (uint8, error) {
	if _, err := io.ReadFull(r, buf[:1]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

// ReadUint32 reads a little endian uint32 from r using buf as scratch space.
func ReadUint32(r io.Reader, buf []byte) (uint32, error) {
	if _, err := io.ReadFull(r, buf[:4]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:4]), nil
}

// ReadUint64 reads a little endian uint64 from r using buf as scratch space.
func ReadUint64(r io.Reader, buf []byte) (uint64, error) {
	if _, err := io.ReadFull(r, buf[:8]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(buf[:8]), nil
}

// WriteUint8 writes val to w using buf as scratch space.
func WriteUint8(w io.Writer, buf []byte, val uint8) error {
	buf[0] = val
	_, err := w.Write(buf[:1])
	return err
}

// WriteUint32 writes val to w as a little endian uint32 using buf as scratch
// space.
func WriteUint32(w io.Writer, buf []byte, val uint32) error {
	binary.LittleEndian.PutUint32(buf[:4], val)
	_, err := w.Write(buf[:4])
	return err
}

// WriteUint64 writes val to w as a little endian uint64 using buf as scratch
// space.
func WriteUint64(w io.Writer, buf []byte, val uint64) error {
	binary.LittleEndian.PutUint64(buf[:8], val)
	_, err := w.Write(buf[:8])
	return err
}

// ReadOutPoint reads the hash and index of an outpoint from r.
func ReadOutPoint(r io.Reader, op *wire.OutPoint) error {
	_, err := io.ReadFull(r, op.Hash[:])
	if err != nil {
		return err
	}

	buf := ScratchPool.Get().(*[8]byte)
	op.Index, err = ReadUint32(r, buf[:])
	ScratchPool.Put(buf)
	return err
}

// NewMessageError creates an error for the given function and description.
func NewMessageError(f string, desc string) *wire.MessageError {
	return &wire.MessageError{Func: f, Description: desc}
}
