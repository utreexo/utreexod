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

	// ProofIndexes are the indexes from the utreexo proof hashes that we want.
	ProofIndexes []uint64

	// LeafIndexes are the indexes from the leaf datas we want.
	LeafIndexes []uint64
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

	proofCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	msg.ProofIndexes = make([]uint64, proofCount)
	for i := range msg.ProofIndexes {
		msg.ProofIndexes[i], err = ReadVarInt(r, pver)
		if err != nil {
			return err
		}
	}

	leafCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	msg.LeafIndexes = make([]uint64, leafCount)
	for i := range msg.LeafIndexes {
		msg.LeafIndexes[i], err = ReadVarInt(r, pver)
		if err != nil {
			return err
		}
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

	err = WriteVarInt(w, pver, uint64(len(msg.ProofIndexes)))
	if err != nil {
		return err
	}

	for i := range msg.ProofIndexes {
		err = WriteVarInt(w, pver, msg.ProofIndexes[i])
		if err != nil {
			return err
		}
	}

	err = WriteVarInt(w, pver, uint64(len(msg.LeafIndexes)))
	if err != nil {
		return err
	}

	for i := range msg.LeafIndexes {
		err = WriteVarInt(w, pver, msg.LeafIndexes[i])
		if err != nil {
			return err
		}
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
	return MaxBlockPayload
}
