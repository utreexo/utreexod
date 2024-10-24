// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"sync"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
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
//
//	Height, IsCoinbase, Amount, and PkScript is the data needed for
//	tx verification (script, signatures, etc).
type LeafData struct {
	BlockHash             chainhash.Hash
	OutPoint              OutPoint
	Height                int32
	IsCoinBase            bool
	Amount                int64
	ReconstructablePkType PkType
	PkScript              []byte
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
	l.OutPoint = OutPoint{Hash: *txHash, Index: s.Index}

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

var sha512DigestPool = sync.Pool{
	New: func() interface{} {
		return sha512.New512_256()
	},
}

// LeafHash concats and hashes all the data in LeafData.
func (l *LeafData) LeafHash() [32]byte {
	digest := sha512DigestPool.Get().(hash.Hash)
	digest.Reset()
	defer sha512DigestPool.Put(digest)

	digest.Write(chainhash.UTREEXO_TAG_V1_APPEND[:])
	l.Serialize(digest)

	return *(*[32]byte)(digest.Sum(nil))
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
	return VarIntSerializeSize(uint64(len(l.PkScript))) +
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
		return messageError("LeafData Serialize", "pkScript too long")
	}
	if l.ReconstructablePkType != OtherTy && l.PkScript == nil {
		desc := fmt.Sprintf("pkscript of type %s, has not been reconstructed",
			l.ReconstructablePkType.String())
		return messageError("LeafData Serialize", desc)
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
// pkType             byte       1
// pkscript length    VLQ        variable
// pkscript           []byte     variable
//
// -----------------------------------------------------------------------------

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

// PkScriptSerializeSizeCompact returns the number of bytes it would take to
// serialize the pkScript with the reconstructable method.
func PkScriptSerializeSizeCompact(ty PkType, pkScript []byte) int {
	if ty == OtherTy {
		// pkType 1 byte + varint pkscript len + pkscript
		return 1 + VarIntSerializeSize(uint64(len(pkScript))) + len(pkScript)
	}
	return 1
}

// PkScriptSerializeCompact encodes the pkScript to w using the pkScript with the
// reconstructable serialization format.
func PkScriptSerializeCompact(w io.Writer, ty PkType, pkscript []byte) error {
	var err error
	switch ty {
	case OtherTy:
		_, err = w.Write([]byte{0x0})
		if err != nil {
			return err
		}
		err = WriteVarBytes(w, 0, pkscript)
	case PubKeyHashTy:
		_, err = w.Write([]byte{0x1})
	case WitnessV0PubKeyHashTy:
		_, err = w.Write([]byte{0x2})
	case ScriptHashTy:
		_, err = w.Write([]byte{0x3})
	case WitnessV0ScriptHashTy:
		_, err = w.Write([]byte{0x4})
	}

	return err
}

// PkScriptSerializeCompact encodes the pkScript to w using the pkScript with the
// reconstructable serialization format.
func PkScriptDeserializeCompact(r io.Reader) (PkType, []byte, error) {
	buf := make([]byte, 1)
	_, err := r.Read(buf)
	if err != nil {
		return 0, nil, err
	}

	var ty PkType
	var pkScript []byte

	switch buf[0] {
	case 0:
		ty = OtherTy
		pkScript, err = ReadVarBytes(r, 0, MaxScriptSize, "pkScript size")
		if err != nil {
			return 0, nil, err
		}
	case 1:
		ty = PubKeyHashTy
	case 2:
		ty = WitnessV0PubKeyHashTy
	case 3:
		ty = ScriptHashTy
	case 4:
		ty = WitnessV0ScriptHashTy
	default:
		return 0, nil, fmt.Errorf("%v is not a valid type", buf[0])
	}

	return ty, pkScript, err
}

// SerializeSizeCompact returns the number of bytes it would take to serialize the
// LeafData in the compact serialization format.
func (l *LeafData) SerializeSizeCompact() int {
	// If the leaf data corresponds to an unconfirmed tx, we don't
	// serialize it.
	if l.IsUnconfirmed() {
		return 0
	}

	// header code 4 bytes + amount 8 bytes + pkscript.
	return 12 + PkScriptSerializeSizeCompact(
		l.ReconstructablePkType, l.PkScript)
}

// SerializeCompact encodes the LeafData to w using the compact leaf data serialization format.
func (l *LeafData) SerializeCompact(w io.Writer) error {
	// If the tx is unconfirmed, write the unconfirmed marker and
	// return immediately.
	if l.IsUnconfirmed() {
		return nil
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
		return messageError("LeafData SerializeCompact", "pkScript too long")
	}

	return PkScriptSerializeCompact(w, l.ReconstructablePkType, l.PkScript)
}

// DeserializeCompact encodes the LeafData to w using the compact leaf serialization format.
func (l *LeafData) DeserializeCompact(r io.Reader) error {
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

// NewLeafData initializes and returns a zeroed out LeafData.
func NewLeafData() LeafData {
	return LeafData{
		OutPoint: *NewOutPoint(new(chainhash.Hash), 0),
	}
}
