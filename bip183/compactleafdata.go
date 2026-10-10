// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip183

import (
	"fmt"
	"io"

	"github.com/btcsuite/btcd/wire/v2"
	"github.com/utreexo/utreexod/bip182"
	"github.com/utreexo/utreexod/internal/btcdwire"
)

// -----------------------------------------------------------------------------
// Compact LeafData serialization leaves out duplicate data that is also present
// in the Bitcoin block.  It's important to note that to genereate the hash
// commitment for the LeafData, there data left out from the compact serialization
// is still needed and must be fetched from the Bitcoin block.
//
// Also note that the serialization differs for whether this leaf data is for a
// block or for a transaction.
//
// The serialized format for a block is:
// [<header code><amount><pkscript len><pkscript>]
//
// The serialized header code format is:
//   bit 0 - containing transaction is a coinbase
//   bits 1-x - height of the block that contains the spent txout
//
// It's calculated with:
//   header_code = <<= 1
//   if IsCoinBase {
//       header_code |= 1 // only set the bit 0 if it's a coinbase.
//   }
//
// Field              Type       Size
// header code        int32      4
// amount             int64      8
// pkType             byte       1
// pkscript length    VLQ        variable
// pkscript           []byte     variable
//
// -----------------------------------------------------------------------------

// PkScriptSerializeSizeCompact returns the number of bytes it would take to
// serialize the pkScript with the reconstructable method.
func PkScriptSerializeSizeCompact(ty bip182.PkType, pkScript []byte) int {
	if ty == bip182.OtherTy {
		// pkType 1 byte + varint pkscript len + pkscript
		return 1 + wire.VarIntSerializeSize(uint64(len(pkScript))) + len(pkScript)
	}
	return 1
}

// PkScriptSerializeCompact encodes the pkScript to w using the pkScript with the
// reconstructable serialization format.
func PkScriptSerializeCompact(w io.Writer, ty bip182.PkType, pkscript []byte) error {
	var err error
	switch ty {
	case bip182.OtherTy:
		_, err = w.Write([]byte{0x0})
		if err != nil {
			return err
		}
		buf := btcdwire.ScratchPool.Get().(*[8]byte)
		err = wire.WriteVarBytesBuf(w, 0, pkscript, buf[:])
		btcdwire.ScratchPool.Put(buf)
	case bip182.PubKeyHashTy:
		_, err = w.Write([]byte{0x1})
	case bip182.WitnessV0PubKeyHashTy:
		_, err = w.Write([]byte{0x2})
	case bip182.ScriptHashTy:
		_, err = w.Write([]byte{0x3})
	case bip182.WitnessV0ScriptHashTy:
		_, err = w.Write([]byte{0x4})
	}

	return err
}

// PkScriptSerializeCompact encodes the pkScript to w using the pkScript with the
// reconstructable serialization format.
func PkScriptDeserializeCompact(r io.Reader) (bip182.PkType, []byte, error) {
	buf := make([]byte, 1)
	_, err := r.Read(buf)
	if err != nil {
		return 0, nil, err
	}

	var ty bip182.PkType
	var pkScript []byte

	switch buf[0] {
	case 0:
		ty = bip182.OtherTy
		scratch := btcdwire.ScratchPool.Get().(*[8]byte)
		pkScript, err = wire.ReadVarBytesBuf(r, 0, scratch[:],
			bip182.MaxScriptSize, "pkScript size")
		btcdwire.ScratchPool.Put(scratch)
		if err != nil {
			return 0, nil, err
		}
	case 1:
		ty = bip182.PubKeyHashTy
	case 2:
		ty = bip182.WitnessV0PubKeyHashTy
	case 3:
		ty = bip182.ScriptHashTy
	case 4:
		ty = bip182.WitnessV0ScriptHashTy
	default:
		return 0, nil, fmt.Errorf("%v is not a valid type", buf[0])
	}

	return ty, pkScript, err
}

// LeafDataSerializeSizeCompact returns the number of bytes it would take to
// serialize the LeafData in the compact serialization format.
func LeafDataSerializeSizeCompact(l *bip182.LeafData) int {
	// If the leaf data corresponds to an unconfirmed tx, we don't
	// serialize it.
	if l.IsUnconfirmed() {
		return 0
	}

	// header code 4 bytes + amount 8 bytes + pkscript.
	return 12 + PkScriptSerializeSizeCompact(
		l.ReconstructablePkType, l.PkScript)
}

// LeafDataSerializeCompact encodes the LeafData to w using the compact leaf
// data serialization format.
func LeafDataSerializeCompact(w io.Writer, l *bip182.LeafData) error {
	if l.IsUnconfirmed() {
		return nil
	}

	buf := btcdwire.ScratchPool.Get().(*[8]byte)
	defer btcdwire.ScratchPool.Put(buf)

	// Height & IsCoinBase.
	hcb := l.Height << 1
	if l.IsCoinBase {
		hcb |= 1
	}
	err := btcdwire.WriteUint32(w, buf[:], uint32(hcb))
	if err != nil {
		return err
	}

	err = btcdwire.WriteUint64(w, buf[:], uint64(l.Amount))
	if err != nil {
		return err
	}

	if uint32(len(l.PkScript)) > bip182.MaxScriptSize {
		return btcdwire.NewMessageError("LeafData SerializeCompact", "pkScript too long")
	}

	return PkScriptSerializeCompact(w, l.ReconstructablePkType, l.PkScript)
}

// LeafDataDeserializeCompact decodes r into the LeafData using the compact
// leaf serialization format.
func LeafDataDeserializeCompact(r io.Reader, l *bip182.LeafData) error {
	buf := btcdwire.ScratchPool.Get().(*[8]byte)
	defer btcdwire.ScratchPool.Put(buf)

	height, err := btcdwire.ReadUint32(r, buf[:])
	if err != nil {
		return err
	}
	l.Height = int32(height)

	if l.Height&1 == 1 {
		l.IsCoinBase = true
	}
	l.Height >>= 1

	amt, err := btcdwire.ReadUint64(r, buf[:])
	if err != nil {
		return err
	}
	l.Amount = int64(amt)

	ty, pkScript, err := PkScriptDeserializeCompact(r)
	if err != nil {
		return err
	}
	l.ReconstructablePkType = ty

	// NOTE pkScript might be nil depending on if the type of
	// the script.
	l.PkScript = pkScript

	return nil
}
