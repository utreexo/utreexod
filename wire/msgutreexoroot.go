// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"
	"math"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// MaxUtreexoRootMsgSize is the very maximum size of a utreexo root message.
const MaxUtreexoRootMsgSize = (MaxVarIntPayload * 2) + chainhash.HashSize + maxRootSize + maxProofSize

// maxRoots is 63 because that's the maximum roots there can be with numleaves as uint64.
const maxRootSize = (63 * chainhash.HashSize) + MaxVarIntPayload

// maxProofSize is just the proof count * the hashsize. Proof count is calculated from
// the max accumulator height which is represented as an uint8.
const maxProofSize = (math.MaxUint8 * chainhash.HashSize) + MaxVarIntPayload

// MsgUtreexoRoots implements the Message interface and represents a bitcoin
// utreexo root message. It's used to deliver the roots and the optional proof
// in response to a getutreexoroot message.
type MsgUtreexoRoot struct {
	NumLeaves uint64
	Target    uint64
	BlockHash chainhash.Hash
	Roots     []utreexo.Hash
	Proof     []utreexo.Hash
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
// See Deserialize for decoding transactions stored to disk, such as in a
// database, as opposed to decoding transactions from the wire.
func (msg *MsgUtreexoRoot) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	var err error
	msg.NumLeaves, err = ReadVarInt(r, pver)
	if err != nil {
		return err
	}

	msg.Target, err = ReadVarInt(r, pver)
	if err != nil {
		return err
	}

	_, err = r.Read(msg.BlockHash[:])
	if err != nil {
		return err
	}

	rootCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}
	msg.Roots = make([]utreexo.Hash, rootCount)
	for i := range msg.Roots {
		_, err = io.ReadFull(r, msg.Roots[i][:])
		if err != nil {
			return err
		}
	}

	proofCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}
	msg.Proof = make([]utreexo.Hash, proofCount)
	for i := range msg.Proof {
		_, err = io.ReadFull(r, msg.Proof[i][:])
		if err != nil {
			return err
		}
	}

	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
// See Serialize for encoding blocks to be stored to disk, such as in a
// database, as opposed to encoding blocks for the wire.
func (msg *MsgUtreexoRoot) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	err := WriteVarInt(w, 0, (msg.NumLeaves))
	if err != nil {
		return err
	}
	err = WriteVarInt(w, 0, msg.Target)
	if err != nil {
		return err
	}
	_, err = w.Write(msg.BlockHash[:])
	if err != nil {
		return err
	}

	err = WriteVarInt(w, 0, uint64(len(msg.Roots)))
	if err != nil {
		return err
	}
	for _, h := range msg.Roots {
		_, err = w.Write(h[:])
		if err != nil {
			return err
		}
	}

	err = WriteVarInt(w, 0, uint64(len(msg.Proof)))
	if err != nil {
		return err
	}
	for _, h := range msg.Proof {
		_, err = w.Write(h[:])
		if err != nil {
			return err
		}
	}

	return nil
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgUtreexoRoot) Command() string {
	return CmdUtreexoRoot
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (msg *MsgUtreexoRoot) MaxPayloadLength(pver uint32) uint32 {
	return MaxUtreexoRootMsgSize
}
