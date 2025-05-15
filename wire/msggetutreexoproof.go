// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// MsgGetUtreexoProof encodes uint64s in varints to request specifics indexes of a
// utreexoproof from a peer.
type MsgGetUtreexoProof struct {
	// BlockHash is the hash of the block we want the utreexo proof for.
	BlockHash chainhash.Hash

	// TargetBool indicates if the targets should be included in the proof message.
	TargetBool bool

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
	msg.TargetBool = b[0] == 1

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

	var b [1]byte
	if msg.TargetBool {
		b[0] = 1
	}
	_, err = w.Write(b[:])
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
	// hashsize + target bool + proofBitMap len + math.MaxUint8 in bitmaps +
	// leafindex bitmap len + MaxPossibleInputsPerBlock in bitmaps.
	return chainhash.HashSize + 1 + MaxVarIntPayload + 32 + MaxVarIntPayload + 3049
}

// IsLeafDataRequested returns if the leafdata at the given index is requested or not.
func (msg *MsgGetUtreexoProof) IsLeafDataRequested(idx int) bool {
	return isBitSet(msg.LeafIndexBitMap, idx)
}

// IsProofRequested returns if the proof hash at the given index is requested or not.
func (msg *MsgGetUtreexoProof) IsProofRequested(idx int) bool {
	return isBitSet(msg.ProofIndexBitMap, idx)
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
