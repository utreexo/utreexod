// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"fmt"
	"io"
	"math"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// MaxUtreexoTTLExponent is the maximum exponent you can ask for in a bitcoin getutreexosummaries
// message.
const MaxUtreexoTTLExponent = 4

// MaxUtreexoTTLsPerMsg is the maximum amount of utreexo ttls there can be in a given MsgUtreexoTTLs.
const MaxUtreexoTTLsPerMsg = 1 << MaxUtreexoTTLExponent

// MaxUtreexoTTLsSize is the maximum size that the MsgUtreexoTTLs can be.
const MaxUtreexoTTLsSize = (MaxUtreexoTTLsPerMsg * MaxUtreexoTTLSize) + (2 * MaxVarIntPayload) +
	(chainhash.HashSize * math.MaxUint8)

// Enforce that the MaxUtreexoTTLsSize is smaller than the max message payload.
var _ [MaxMessagePayload - MaxUtreexoTTLsSize]struct{}

// MsgUtreexoTTLs implements the Message interface and represents a bitcoin
// utreexottls message. It has the utreexo ttls which is used as a
// response to a getutreexottls message. There can only be a maximum of a
// 1024 utreexo ttls in one message.
type MsgUtreexoTTLs struct {
	// TTLs are the ttls per block.
	TTLs []UtreexoTTL

	// ProofHashes prove that the ttls in this message are committed in the binary.
	ProofHashes []utreexo.Hash
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgUtreexoTTLs) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	count, err := ReadVarInt(r, pver)
	if err != nil {
		return err
	}

	if count > MaxUtreexoTTLsPerMsg {
		str := fmt.Sprintf("too many utreexo ttls for message "+
			"[count %v, max %v]", count, MaxUtreexoTTLsPerMsg)
		return messageError("MsgUtreexoTTLs.BtcDecode", str)
	}

	msg.TTLs = make([]UtreexoTTL, count)
	for i := range msg.TTLs {
		err = msg.TTLs[i].Deserialize(r)
		if err != nil {
			return err
		}
	}

	count, err = ReadVarInt(r, pver)
	if err != nil {
		return err
	}

	msg.ProofHashes = make([]utreexo.Hash, count)
	for i := range msg.ProofHashes {
		_, err := io.ReadFull(r, msg.ProofHashes[i][:])
		if err != nil {
			return err
		}
	}

	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgUtreexoTTLs) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	count := len(msg.TTLs)

	if count > MaxUtreexoTTLsPerMsg {
		str := fmt.Sprintf("too many utreexo ttls for message "+
			"[count %v, max %v]", count, MaxUtreexoTTLsPerMsg)
		return messageError("MsgUtreexoTTLs.BtcEncode", str)
	}

	err := WriteVarInt(w, 0, uint64(len(msg.TTLs)))
	if err != nil {
		return err
	}

	for _, ttl := range msg.TTLs {
		err := ttl.Serialize(w)
		if err != nil {
			return err
		}
	}

	err = WriteVarInt(w, 0, uint64(len(msg.ProofHashes)))
	if err != nil {
		return err
	}

	for _, proofHash := range msg.ProofHashes {
		_, err := w.Write(proofHash[:])
		if err != nil {
			return err
		}
	}

	return nil
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgUtreexoTTLs) Command() string {
	return CmdUtreexoTTLs
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver. This is part of the Message interface implementation.
func (msg *MsgUtreexoTTLs) MaxPayloadLength(_ uint32) uint32 {
	return MaxUtreexoTTLsSize
}
