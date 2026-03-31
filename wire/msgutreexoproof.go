// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// maxSupportedRows is the maximum number of rows in the utreexo tree that
// can be supported while keeping MaxUtreexoProofSize under MaxMessagePayload.
// The theoretical max is 63 rows since leaf positions are uint64, but 51 is the highest that fits
// within the 32 MiB message payload limit.
const maxSupportedRows = 51

// MaxProofHashes is the maximum number of proof hashes needed to prove
// MaxPossibleInputsPerBlock leaves in a tree of maxSupportedRows rows.
// For rows 0-35, each leaf needs one proof hash per row (no shared siblings).
// At row 36 (2^14 = 16,384 pairs < 24,386 leaves), forced sibling pairing
// yields 2^15 - 24,386 = 8,382 proof hashes. All rows above contribute 0.
const MaxProofHashes = (maxSupportedRows-15)*MaxPossibleInputsPerBlock + (1<<15 - MaxPossibleInputsPerBlock)

// MaxUtreexoProofSize is blockhash + len proofhashes + proofhashes + len targets +
// targets + len leafdatas + pkscript size + leafdata overhead of 12 bytes.
const MaxUtreexoProofSize = chainhash.HashSize +
	MaxVarIntPayload +
	MaxProofHashes*chainhash.HashSize +
	MaxVarIntPayload +
	MaxVarIntPayload*MaxPossibleInputsPerBlock +
	MaxVarIntPayload +
	MaxBlockPayload +
	MaxPossibleInputsPerBlock*12

// Enforce that the MaxUtreexoProofSize is smaller than the max message payload.
var _ [MaxMessagePayload - MaxUtreexoProofSize]struct{}

// MsgUtreexoProof is a utreexo proof for a given block that includes the rest of the data not
// communicated by the utreexo header. It may or may not include all the data needed to prove
// the given block as a peer is able to ask for only the proof data it needs.
type MsgUtreexoProof struct {
	// BlockHash is the hash of the block this utreexo proof proves.
	BlockHash chainhash.Hash

	// ProofHashes is the hashes needed to hash up to the utreexo roots.
	ProofHashes []utreexo.Hash

	// Targets are the list of leaf locations to delete.
	Targets []uint64

	// LeafDatas are the tx validation data for every input.
	LeafDatas []LeafData
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
// See Deserialize for decoding transactions stored to disk, such as in a
// database, as opposed to decoding transactions from the wire.
func (msg *MsgUtreexoProof) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	_, err := r.Read(msg.BlockHash[:])
	if err != nil {
		return err
	}

	proofCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	msg.ProofHashes = make([]utreexo.Hash, proofCount)
	for i := range msg.ProofHashes {
		_, err = io.ReadFull(r, msg.ProofHashes[i][:])
		if err != nil {
			return err
		}
	}

	targetCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	msg.Targets = make([]uint64, targetCount)
	for i := range msg.Targets {
		msg.Targets[i], err = ReadVarInt(r, 0)
		if err != nil {
			return err
		}
	}

	leafCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	msg.LeafDatas = make([]LeafData, leafCount)
	for i := range msg.LeafDatas {
		err = msg.LeafDatas[i].DeserializeCompact(r)
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
func (msg *MsgUtreexoProof) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	_, err := w.Write(msg.BlockHash[:])
	if err != nil {
		return err
	}

	err = WriteVarInt(w, 0, uint64(len(msg.ProofHashes)))
	if err != nil {
		return err
	}

	for _, h := range msg.ProofHashes {
		_, err = w.Write(h[:])
		if err != nil {
			return err
		}
	}

	err = WriteVarInt(w, 0, uint64(len(msg.Targets)))
	if err != nil {
		return err
	}

	for _, target := range msg.Targets {
		err = WriteVarInt(w, 0, target)
		if err != nil {
			return err
		}
	}

	// Write the size of the leaf datas.
	err = WriteVarInt(w, 0, uint64(len(msg.LeafDatas)))
	if err != nil {
		return err
	}

	// Write the actual leaf datas.
	for _, ld := range msg.LeafDatas {
		err = ld.SerializeCompact(w)
		if err != nil {
			return err
		}
	}

	return nil
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgUtreexoProof) Command() string {
	return CmdUtreexoProof
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (msg *MsgUtreexoProof) MaxPayloadLength(pver uint32) uint32 {
	return MaxUtreexoProofSize
}
