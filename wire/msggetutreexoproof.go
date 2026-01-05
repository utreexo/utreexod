// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"
	"math"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// math.MaxUint8 in bitmaps. Ceiling divide.
const maxProofSizePerInput = (math.MaxUint8 + 8 - 1) / 8

// MaxPossibleInputsPerBlock in bitmaps. Ceiling divide.
const maxLeafIndexBitMapSize = (MaxPossibleInputsPerBlock + 8 - 1) / 8

// hashsize + target bool + proofBitMap len + maxProofSizePerInput*MaxPossibleInputsPerBlock +
// leafindex bitmap len + maxLeafIndexBitMapSize.
const MaxGetUtreexoProofSize = chainhash.HashSize +
	1 +
	MaxVarIntPayload +
	(maxProofSizePerInput * MaxPossibleInputsPerBlock) +
	MaxVarIntPayload +
	maxLeafIndexBitMapSize

// Enforce that the MaxGetUtreexoProofSize is smaller than the max message payload.
var _ [MaxMessagePayload - MaxGetUtreexoProofSize]struct{}

// MsgGetUtreexoProof encodes uint64s in varints to request specifics indexes of a
// utreexoproof from a peer.
type MsgGetUtreexoProof struct {
	// BlockHash is the hash of the block we want the utreexo proof for.
	BlockHash chainhash.Hash

	// RequestBitMap indicates if the targets, proofs, or leaf indexes should be
	// included in the proof message.
	RequestBitMap uint8

	// ProofIndexBitMap is a bitmap of the proof indexes. The bits that are
	// turned on indicate the proofs that the requester wants.
	ProofIndexBitMap []byte

	// LeafIndexBitMap is a bitmap of the leafdata indexes. The bits that are
	// turned on indicate the leafdata that the requester wants.
	LeafIndexBitMap []byte
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
// See Deserialize for decoding transactions stored to disk, such as in a
// database, as opposed to decoding transactions from the wire.
func (msg *MsgGetUtreexoProof) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	_, err := r.Read(msg.BlockHash[:])
	if err != nil {
		return err
	}

	var b [1]byte
	_, err = r.Read(b[:])
	if err != nil {
		return err
	}
	msg.RequestBitMap = b[0]

	proofCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	msg.ProofIndexBitMap = make([]byte, proofCount)
	_, err = r.Read(msg.ProofIndexBitMap[:])
	if err != nil {
		return err
	}

	leafCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	msg.LeafIndexBitMap = make([]byte, leafCount)
	_, err = r.Read(msg.LeafIndexBitMap[:])
	if err != nil {
		return err
	}

	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
// See Serialize for encoding transactions to be stored to disk, such as in a
// database, as opposed to encoding transactions for the wire.
func (msg *MsgGetUtreexoProof) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	_, err := w.Write(msg.BlockHash[:])
	if err != nil {
		return err
	}

	_, err = w.Write([]byte{msg.RequestBitMap})
	if err != nil {
		return err
	}

	err = WriteVarInt(w, pver, uint64(len(msg.ProofIndexBitMap)))
	if err != nil {
		return err
	}

	_, err = w.Write(msg.ProofIndexBitMap[:])
	if err != nil {
		return err
	}

	err = WriteVarInt(w, pver, uint64(len(msg.LeafIndexBitMap)))
	if err != nil {
		return err
	}

	_, err = w.Write(msg.LeafIndexBitMap[:])
	if err != nil {
		return err
	}

	return nil
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgGetUtreexoProof) Command() string {
	return CmdGetUtreexoProof
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (msg *MsgGetUtreexoProof) MaxPayloadLength(pver uint32) uint32 {
	return MaxGetUtreexoProofSize
}

// SetTargetRequestBit sets the bit for requesting all the targets.
func (msg *MsgGetUtreexoProof) SetTargetRequestBit() {
	msg.RequestBitMap |= 1
}

// SetProofHashRequestBit sets the bit for requesting all the proof hashes.
func (msg *MsgGetUtreexoProof) SetProofHashRequestBit() {
	msg.RequestBitMap |= (1 << 1)
}

// SetLeafDataRequestBit sets the bit for requesting all the leaf datas.
func (msg *MsgGetUtreexoProof) SetLeafDataRequestBit() {
	msg.RequestBitMap |= (1 << 2)
}

// IsLeafDataRequested returns if the leafdata at the given index is requested or not.
func (msg *MsgGetUtreexoProof) IsLeafDataRequestedAtIdx(idx int) bool {
	return isBitSet(msg.LeafIndexBitMap, idx)
}

// IsProofRequested returns if the proof hash at the given index is requested or not.
func (msg *MsgGetUtreexoProof) IsProofRequestedAtIdx(idx int) bool {
	return isBitSet(msg.ProofIndexBitMap, idx)
}

// AreTargetsRequested returns if all the targets are requested or not.
func (msg *MsgGetUtreexoProof) AreTargetsRequested() bool {
	return isBitSet([]byte{msg.RequestBitMap}, 0)
}

// IsEntireProofRequested returns if the entire proof hashes are requested or not.
func (msg *MsgGetUtreexoProof) IsEntireProofRequested() bool {
	return isBitSet([]byte{msg.RequestBitMap}, 1)
}

// IsEntireLeafDataRequested returns if the entire leaf datas are requested or not.
func (msg *MsgGetUtreexoProof) IsEntireLeafDataRequested() bool {
	return isBitSet([]byte{msg.RequestBitMap}, 2)
}

// Returns true if the bit at the given index is set.
func isBitSet(slice []byte, idx int) bool {
	bytesIdx := idx / 8
	if len(slice) <= bytesIdx {
		return false
	}

	bit := idx % 8
	b := slice[bytesIdx]
	return b&(1<<bit) != 0
}

// createBitmap returns a bitmap from the given slice of bools.
func createBitmap(includes []bool) []byte {
	count := len(includes) / 8
	if len(includes)%8 != 0 {
		count++
	}

	bitMap := make([]byte, count)

	bitMapIdx := 0
	for idx, include := range includes {
		bitPlace := idx % 8
		if idx != 0 && bitPlace == 0 {
			bitMapIdx++
		}

		if include {
			bitMap[bitMapIdx] |= (1 << bitPlace)
		}
	}

	return bitMap

}
