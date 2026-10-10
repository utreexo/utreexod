// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip182

import (
	"bytes"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"sync"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/utreexo/utreexod/internal/btcdwire"
	"github.com/utreexo/utreexod/wire"
)

const (
	// MaxScriptSize is the maximum allowed length of a raw script.
	//
	// TODO: This is a duplicate of MaxScriptSize in package txscript.  However,
	// importing package txscript to bip182 will cause a import cycle so this is a
	// stopgap solution.
	MaxScriptSize = 10000
)

var (
	// empty is useful when comparing against BlockHash to see if it hasn't been
	// initialized.
	empty chainhash.Hash

	// emptyLd is useful when comparing against LeafData to see if it hasn't been
	// initialized.
	emptyLd LeafData
)

// LeafData is all the data that goes into a leaf in the utreexo accumulator.
// The data here serve two roles: commitments and data needed for verification.
//
// Commitment:
//   - BlockHash is included in the LeafData to commit to a block.
//
// Verification:
//   - OutPoint is the OutPoint for the utxo being referenced.
//   - Height, IsCoinbase, Amount, and PkScript is the data needed for
//     tx verification (script, signatures, etc).
type LeafData struct {
	BlockHash             chainhash.Hash
	OutPoint              wire.OutPoint
	Height                int32
	IsCoinBase            bool
	Amount                int64
	ReconstructablePkType PkType
	PkScript              []byte
}

// Equal returns if the passed in LeafData is equal to this one.
func (l *LeafData) Equal(other LeafData) bool {
	return l.BlockHash == other.BlockHash &&
		l.OutPoint == other.OutPoint &&
		l.Height == other.Height &&
		l.IsCoinBase == other.IsCoinBase &&
		l.Amount == other.Amount &&
		l.ReconstructablePkType == other.ReconstructablePkType &&
		bytes.Equal(l.PkScript, other.PkScript)
}

// Copy creates a deep copy of the leafdata so the original does not get modified
// when the copy is manipulated.
func (l *LeafData) Copy() *LeafData {
	newL := LeafData{
		BlockHash:             l.BlockHash,
		OutPoint:              l.OutPoint,
		Height:                l.Height,
		IsCoinBase:            l.IsCoinBase,
		Amount:                l.Amount,
		ReconstructablePkType: l.ReconstructablePkType,
		PkScript:              make([]byte, len(l.PkScript)),
	}

	copy(newL.PkScript, l.PkScript)
	return &newL
}

func (l LeafData) MarshalJSON() ([]byte, error) {
	s := struct {
		BlockHash             string `json:"blockhash"`
		TxHash                string `json:"txhash"`
		Index                 uint32 `json:"index"`
		Height                int32  `json:"height"`
		IsCoinbase            bool   `json:"iscoinbase"`
		Amount                int64  `json:"amount"`
		ReconstructablePkType int    `json:"reconstructtype"`
		PkScript              string `json:"pkscript"`
	}{
		BlockHash:             l.BlockHash.String(),
		TxHash:                l.OutPoint.Hash.String(),
		Index:                 l.OutPoint.Index,
		Height:                l.Height,
		IsCoinbase:            l.IsCoinBase,
		Amount:                l.Amount,
		ReconstructablePkType: int(l.ReconstructablePkType),
		PkScript:              hex.EncodeToString(l.PkScript),
	}

	return json.Marshal(s)
}

func (l *LeafData) UnmarshalJSON(data []byte) error {
	s := struct {
		BlockHash             string `json:"blockhash"`
		TxHash                string `json:"txhash"`
		Index                 uint32 `json:"index"`
		Height                int32  `json:"height"`
		IsCoinbase            bool   `json:"iscoinbase"`
		Amount                int64  `json:"amount"`
		ReconstructablePkType int    `json:"reconstructtype"`
		PkScript              string `json:"pkscript"`
	}{}

	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	blockhash, err := chainhash.NewHashFromStr(s.BlockHash)
	if err != nil {
		return err
	}
	l.BlockHash = *blockhash

	txHash, err := chainhash.NewHashFromStr(s.TxHash)
	if err != nil {
		return err
	}
	l.OutPoint = wire.OutPoint{Hash: *txHash, Index: s.Index}

	l.Height = s.Height
	l.IsCoinBase = s.IsCoinbase
	l.Amount = s.Amount
	l.ReconstructablePkType = PkType(s.ReconstructablePkType)
	l.PkScript, err = hex.DecodeString(s.PkScript)
	if err != nil {
		return err
	}

	return nil
}

// leafHasherPool holds reusable LeafHashers so LeafHash can hash without
// allocating a digest per call. Callers that hash many leaves on a single
// goroutine should use NewLeafHasher directly to avoid the pool entirely.
var leafHasherPool = sync.Pool{
	New: func() interface{} {
		return NewLeafHasher()
	},
}

// LeafHash concats and hashes all the data in LeafData.
func (l *LeafData) LeafHash() [32]byte {
	lh := leafHasherPool.Get().(*LeafHasher)
	defer leafHasherPool.Put(lh)
	return lh.HashLeaf(l)
}

// LeafHasher is a pre-allocated hasher for computing LeafHash without
// sync.Pool contention. Create one per goroutine via NewLeafHasher.
type LeafHasher struct {
	digest hash.Hash
	buf    [8]byte // scratch for uint32/uint64 encoding
}

// NewLeafHasher creates a LeafHasher with its own SHA-512/256 state.
func NewLeafHasher() *LeafHasher {
	return &LeafHasher{digest: sha512.New512_256()}
}

// HashLeaf computes the leaf hash without touching any sync.Pool.
func (lh *LeafHasher) HashLeaf(l *LeafData) [32]byte {
	d := lh.digest
	d.Reset()

	d.Write(UTREEXO_TAG_V1_APPEND[:])

	// Inline serialization to avoid sync.Pool in Serialize/WriteOutPoint/WriteVarInt.
	// BlockHash (32 bytes)
	d.Write(l.BlockHash[:])
	// OutPoint: Hash (32 bytes) + Index (4 bytes LE)
	d.Write(l.OutPoint.Hash[:])
	binary.LittleEndian.PutUint32(lh.buf[:4], l.OutPoint.Index)
	d.Write(lh.buf[:4])
	// Header code: Height<<1 | IsCoinBase (4 bytes LE)
	hcb := l.Height << 1
	if l.IsCoinBase {
		hcb |= 1
	}
	binary.LittleEndian.PutUint32(lh.buf[:4], uint32(hcb))
	d.Write(lh.buf[:4])
	// Amount (8 bytes LE)
	binary.LittleEndian.PutUint64(lh.buf[:8], uint64(l.Amount))
	d.Write(lh.buf[:8])
	// PkScript: varint length + bytes
	lh.writeVarInt(d, uint64(len(l.PkScript)))
	d.Write(l.PkScript)

	return *(*[32]byte)(d.Sum(nil))
}

// writeVarInt writes a Bitcoin-style variable-length integer directly to w.
func (lh *LeafHasher) writeVarInt(w io.Writer, val uint64) {
	if val < 0xfd {
		lh.buf[0] = uint8(val)
		w.Write(lh.buf[:1])
	} else if val <= 0xffff {
		lh.buf[0] = 0xfd
		binary.LittleEndian.PutUint16(lh.buf[1:3], uint16(val))
		w.Write(lh.buf[:3])
	} else if val <= 0xffffffff {
		lh.buf[0] = 0xfe
		binary.LittleEndian.PutUint32(lh.buf[1:5], uint32(val))
		w.Write(lh.buf[:5])
	} else {
		lh.buf[0] = 0xff
		w.Write(lh.buf[:1])
		binary.LittleEndian.PutUint64(lh.buf[:8], val)
		w.Write(lh.buf[:8])
	}
}

// String turns a LeafData into a string for logging.
func (l *LeafData) String() (s string) {
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

// IsCompact returns if the leaf data is in the compact state.
func (l *LeafData) IsCompact() bool {
	return l.BlockHash == empty &&
		l.OutPoint.Hash == empty &&
		l.OutPoint.Index == 0
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
	return wire.VarIntSerializeSize(uint64(len(l.PkScript))) +
		len(l.PkScript) + size
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
	err = wire.WriteOutPoint(w, 0, 0, &l.OutPoint)
	if err != nil {
		return err
	}

	buf := btcdwire.ScratchPool.Get().(*[8]byte)
	defer btcdwire.ScratchPool.Put(buf)

	hcb := l.Height << 1
	if l.IsCoinBase {
		hcb |= 1
	}
	err = btcdwire.WriteUint32(w, buf[:], uint32(hcb))
	if err != nil {
		return err
	}

	err = btcdwire.WriteUint64(w, buf[:], uint64(l.Amount))
	if err != nil {
		return err
	}
	if uint32(len(l.PkScript)) > MaxScriptSize {
		return btcdwire.NewMessageError("LeafData Serialize", "pkScript too long")
	}
	if l.ReconstructablePkType != OtherTy && l.PkScript == nil {
		desc := fmt.Sprintf("pkscript of type %s, has not been reconstructed",
			l.ReconstructablePkType.String())
		return btcdwire.NewMessageError("LeafData Serialize", desc)
	}

	return wire.WriteVarBytes(w, 0, l.PkScript)
}

// Deserialize encodes the LeafData from r using the LeafData serialization format.
func (l *LeafData) Deserialize(r io.Reader) error {
	_, err := io.ReadFull(r, l.BlockHash[:])
	if err != nil {
		return err
	}

	// Deserialize the outpoint.
	l.OutPoint = wire.OutPoint{Hash: *(new(chainhash.Hash)), Index: 0}
	err = btcdwire.ReadOutPoint(r, &l.OutPoint)
	if err != nil {
		return err
	}

	buf := btcdwire.ScratchPool.Get().(*[8]byte)
	defer btcdwire.ScratchPool.Put(buf)

	// Deserialize the stxo.
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

	l.PkScript, err = wire.ReadVarBytes(r, 0, MaxScriptSize, "pkscript size")
	if err != nil {
		return err
	}

	return nil
}

// PkType is a list of different pkScript types that can be reconstructed.
// The pkScripts that can not be reconstructed are specified as other. All
// other types can be reconstructed.
type PkType byte

const (
	OtherTy               PkType = iota
	PubKeyHashTy                 // Pay to pubkey hash.
	WitnessV0PubKeyHashTy        // Pay to witness pubkey hash.
	ScriptHashTy                 // Pay to script hash.
	WitnessV0ScriptHashTy        // Pay to witness script hash.
)

// pkTypeToName maps PkType to strings.
var pkTypeToName = []string{
	OtherTy:               "other",
	PubKeyHashTy:          "pubkeyhash",
	WitnessV0PubKeyHashTy: "witness_v0_keyhash",
	ScriptHashTy:          "scripthash",
	WitnessV0ScriptHashTy: "witness_v0_scripthash",
}

// String returns a string for the type of PkType.
func (ty PkType) String() string {
	if int(ty) > len(pkTypeToName) || int(ty) < 0 {
		return "Invalid"
	}

	return pkTypeToName[ty]
}

// NewLeafData initializes and returns a zeroed out LeafData.
func NewLeafData() LeafData {
	return LeafData{
		OutPoint: *wire.NewOutPoint(new(chainhash.Hash), 0),
	}
}
