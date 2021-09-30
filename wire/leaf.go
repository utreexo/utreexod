// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

const (
	// MaxScriptSize is the maximum allowed length of a raw script.
	//
	// TODO: This is a duplicate of MaxScriptSize in package txscript.  However,
	// importing package txscript to wire will cause a import cycle so this is a
	// stopgap solution.
	MaxScriptSize = 10000
)

var (
	// empty is useful when comparing against BlockHash to see if it hasn't been
	// initialized.
	empty chainhash.Hash
)

// LeafData is all the data that goes into a leaf in the utreexo accumulator.
// The data here serve two roles: commitments and data needed for verification.
//
// Commitment:   BlockHash is included in the LeafData to commit to a block.
// Verification: OutPoint is the OutPoint for the utxo being referenced.
//               Height, IsCoinbase, Amount, and PkScript is the data needed for
//               tx verification (script, signatures, etc).
type LeafData struct {
	BlockHash  chainhash.Hash
	OutPoint   OutPoint
	Height     int32
	IsCoinBase bool
	Amount     int64
	PkScript   []byte
}

// LeafHash concats and hashes all the data in LeafData.
func (l *LeafData) LeafHash() [32]byte {
	digest := sha512.New512_256()
	l.Serialize(digest)

	// TODO go 1.17 support slice to array conversion so we
	// can avoid this extra copy.
	hash := [32]byte{}
	copy(hash[:], digest.Sum(nil))
	return hash
}

// ToString turns a LeafData into a string for logging.
func (l *LeafData) ToString() (s string) {
	s += fmt.Sprintf("BlockHash:%s,", hex.EncodeToString(l.BlockHash[:]))
	s += fmt.Sprintf("OutPoint:%s,", l.OutPoint.String())
	s += fmt.Sprintf("Amount:%d,", l.Amount)
	s += fmt.Sprintf("PkScript:%s,", hex.EncodeToString(l.PkScript))
	s += fmt.Sprintf("BlockHeight:%d,", l.Height)
	s += fmt.Sprintf("IsCoinBase:%v,", l.IsCoinBase)
	s += fmt.Sprintf("LeafHash:%x,", l.LeafHash())
	s += fmt.Sprintf("Size:%d", l.SerializeSize())
	return
}

// IsUnconfirmed returns whether the leaf data in question corresponds to an
// unconfirmed transaction.
func (l *LeafData) IsUnconfirmed() bool {
	return l.Height == -1
}

// SetUnconfirmed sets the leaf data as unconfirmed.
func (l *LeafData) SetUnconfirmed() {
	l.Height = -1
}

// -----------------------------------------------------------------------------
// LeafData serialization includes all the data needed for generating the hash
// commitment of the LeafData.
//
// The serialized format is:
// [<block hash><outpoint><header code><amount><pkscript len><pkscript>]
//
// The outpoint serialized format is:
// [<tx hash><index>]
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
// All together, the serialization looks like so:
//
// Field              Type       Size
// block hash         [32]byte   32
// outpoint           -          36
//   tx hash          [32]byte   32
//   vout             [4]byte    4
// header code        int32      4
// amount             int64      8
// pkscript length    VLQ        variable
// pkscript           []byte     variable
//
// -----------------------------------------------------------------------------

// SerializeSize returns the number of bytes it would take to serialize the
// LeafData.
func (l *LeafData) SerializeSize() int {
	// Block Hash 32 + OutPoint Hash 32 bytes + Outpoint index 4 bytes +
	// header code 4 bytes + amount 8 bytes.
	size := 80

	// Add pkscript size.
	return size + VarIntSerializeSize(uint64(len(l.PkScript))) + len(l.PkScript)
}

// Serialize encodes the LeafData to w using the LeafData serialization format.
func (l *LeafData) Serialize(w io.Writer) error {
	if l.BlockHash == empty {
		return fmt.Errorf("LeafData Serialize Err: BlockHash is empty %s.",
			l.BlockHash)
	}
	_, err := w.Write(l.BlockHash[:])
	if err != nil {
		return err
	}
	err = WriteOutPoint(w, 0, 0, &l.OutPoint)
	if err != nil {
		return err
	}

	bs := newSerializer()
	defer bs.free()

	hcb := l.Height << 1
	if l.IsCoinBase {
		hcb |= 1
	}
	err = bs.PutUint32(w, littleEndian, uint32(hcb))
	if err != nil {
		return err
	}

	err = bs.PutUint64(w, littleEndian, uint64(l.Amount))
	if err != nil {
		return err
	}
	if uint32(len(l.PkScript)) > MaxScriptSize {
		return messageError("LeafData.Serialize", "pkScript too long")
	}

	return WriteVarBytes(w, 0, l.PkScript)
}

// Deserialize encodes the LeafData from r using the LeafData serialization format.
func (l *LeafData) Deserialize(r io.Reader) error {
	_, err := io.ReadFull(r, l.BlockHash[:])
	if err != nil {
		return err
	}

	// Deserialize the outpoint.
	l.OutPoint = OutPoint{Hash: *(new(chainhash.Hash)), Index: 0}
	err = readOutPoint(r, 0, 0, &l.OutPoint)
	if err != nil {
		return err
	}

	bs := newSerializer()
	defer bs.free()

	// Deserialize the stxo.
	height, err := bs.Uint32(r, littleEndian)
	if err != nil {
		return err
	}
	l.Height = int32(height)

	if l.Height&1 == 1 {
		l.IsCoinBase = true
	}
	l.Height >>= 1

	amt, err := bs.Uint64(r, littleEndian)
	if err != nil {
		return err
	}
	l.Amount = int64(amt)

	l.PkScript, err = ReadVarBytes(r, 0, MaxScriptSize, "pkscript size")
	if err != nil {
		return err
	}

	return nil
}

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
// pkscript length    VLQ        variable
// pkscript           []byte     variable
//
// The serialized format for a transaction is:
// [<unconfirmed marker><header code><amount><pkscript len><pkscript>]
//
// All other fields with the exception of 'unconfirmed marker' is the same as
// the serialization for a block.  The unconfirmed marker is represented in
// the struct as height = -1.
//
// Field               Type       Size
// unconfirmed marker  byte       1
// header code         int32      4
// amount              int64      8
// pkscript length     VLQ        variable
// pkscript            []byte     variable
//
// -----------------------------------------------------------------------------

// SerializeSizeCompact returns the number of bytes it would take to serialize the
// LeafData in the compact serialization format.
func (l *LeafData) SerializeSizeCompact(isForTx bool) int {
	if isForTx {
		if l.IsUnconfirmed() {
			// If the leaf data corresponds to an unconfirmed tx, we only
			// send a byte.
			return 1
		} else {
			// header code 4 bytes + amount 8 bytes + unconfirmed marker + pkscript.
			return 13 + VarIntSerializeSize(uint64(len(l.PkScript))) + len(l.PkScript)
		}
	}
	// header code 4 bytes + amount 8 bytes + pkscript.
	return 12 + VarIntSerializeSize(uint64(len(l.PkScript))) + len(l.PkScript)
}

// SerializeCompact encodes the LeafData to w using the compact leaf data serialization format.
func (l *LeafData) SerializeCompact(w io.Writer, isForTx bool) error {
	if isForTx {
		// If the tx is unconfirmed, write the unconfirmed marker and
		// return immediately.
		if l.IsUnconfirmed() {
			_, err := w.Write([]byte{0x1})
			if err != nil {
				return err
			}

			// Return if unconfirmed.
			return nil
		}

		// Write 0 to mark that this transaction is confirmed.
		_, err := w.Write([]byte{0x0})
		if err != nil {
			return err
		}
	}

	bs := newSerializer()
	defer bs.free()

	// Height & IsCoinBase.
	hcb := l.Height << 1
	if l.IsCoinBase {
		hcb |= 1
	}
	err := bs.PutUint32(w, littleEndian, uint32(hcb))
	if err != nil {
		return err
	}

	err = bs.PutUint64(w, littleEndian, uint64(l.Amount))
	if err != nil {
		return err
	}

	if uint32(len(l.PkScript)) > MaxScriptSize {
		return messageError("LeafData.SerializeCompact", "pkScript too long")
	}
	return WriteVarBytes(w, 0, l.PkScript)
}

// DeserializeCompact encodes the LeafData to w using the compact leaf serialization format.
func (l *LeafData) DeserializeCompact(r io.Reader, isForTx bool) error {
	if isForTx {
		// Read unconfirmed marker.
		unconfirmed := make([]byte, 1)
		_, err := io.ReadFull(r, unconfirmed)
		if err != nil {
			return err
		}

		// 1 means that the LeafData corresponds to an uncomfirmed tx.
		// Set IsUnconfirmed as true and return.
		if bytes.Equal(unconfirmed, []byte{0x1}) {
			l.SetUnconfirmed()

			// Return immediately here if the tx is unconfirmed.
			return nil
		}
	}

	bs := newSerializer()
	defer bs.free()

	height, err := bs.Uint32(r, littleEndian)
	if err != nil {
		return err
	}
	l.Height = int32(height)

	if l.Height&1 == 1 {
		l.IsCoinBase = true
	}
	l.Height >>= 1

	amt, err := bs.Uint64(r, littleEndian)
	if err != nil {
		return err
	}
	l.Amount = int64(amt)

	l.PkScript, err = ReadVarBytes(r, 0, MaxScriptSize, "pkScript size")
	if err != nil {
		return err
	}

	return nil
}

// NewLeafData initializes and returns a zeroed out LeafData.
func NewLeafData() LeafData {
	return LeafData{
		OutPoint: *NewOutPoint(new(chainhash.Hash), 0),
	}
}
