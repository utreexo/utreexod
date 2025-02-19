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

// MaxUtreexoBlockSummaryPerMsg is the maximum number of utreexo headers that can be in a single
// bitcoin headers message.
const MaxUtreexoBlockSummaryPerMsg = 128

// MsgUtreexoSummaries implements the Message interface and represents a bitcoin
// utreexoblocksummaries message. It's has the block summaries which is used as a
// response to a getutreexoblocksummaries message. There can only be a maximum of a
// 100 block summaries in one message.
type MsgUtreexoSummaries struct {
	Summaries   []*UtreexoBlockSummary
	ProofHashes []utreexo.Hash
}

// AddSummary adds a new utreexo block summary to the message. It checks that the
// maximum limit is not exceeded.
func (msg *MsgUtreexoSummaries) AddSummary(s *UtreexoBlockSummary) error {
	if len(msg.Summaries)+1 > MaxUtreexoBlockSummaryPerMsg {
		str := fmt.Sprintf("too many summaries in message [max %v]",
			MaxUtreexoBlockSummaryPerMsg)
		return messageError("MsgUtreexoSummaries.AddSummary", str)
	}

	msg.Summaries = append(msg.Summaries, s)
	return nil
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgUtreexoSummaries) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	count, err := ReadVarInt(r, pver)
	if err != nil {
		return err
	}

	// Limit to max utreexo summaries per message.
	if count > MaxUtreexoBlockSummaryPerMsg {
		str := fmt.Sprintf("too many utreexo summaries for message "+
			"[count %v, max %v]", count, MaxUtreexoBlockSummaryPerMsg)
		return messageError("MsgUtreexoSummaries.BtcDecode", str)
	}

	summaries := make([]UtreexoBlockSummary, count)
	for i := range summaries {
		err = summaries[i].Deserialize(r)
		if err != nil {
			return err
		}
		msg.AddSummary(&summaries[i])
	}

	proofHashCount, err := ReadVarInt(r, pver)
	if err != nil {
		return err
	}

	msg.ProofHashes = make([]utreexo.Hash, proofHashCount)
	for i := range msg.ProofHashes {
		_, err = io.ReadFull(r, msg.ProofHashes[i][:])
		if err != nil {
			return err
		}
	}

	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgUtreexoSummaries) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	// Limit to max utreexo summaries per message.
	count := len(msg.Summaries)
	if count > MaxUtreexoBlockSummaryPerMsg {
		str := fmt.Sprintf("too many utreexo summaries for message "+
			"[count %v, max %v]", count, MaxUtreexoBlockSummaryPerMsg)
		return messageError("MsgUtreexoSummaries.BtcEncode", str)
	}

	err := WriteVarInt(w, pver, uint64(count))
	if err != nil {
		return err
	}

	for _, s := range msg.Summaries {
		err := writeUtreexoBlockSummary(w, pver, s)
		if err != nil {
			return err
		}
	}

	err = WriteVarInt(w, pver, uint64(len(msg.ProofHashes)))
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
func (msg *MsgUtreexoSummaries) Command() string {
	return CmdUtreexoSummaries
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver. This is part of the Message interface implementation.
func (msg *MsgUtreexoSummaries) MaxPayloadLength(pver uint32) uint32 {
	return (MaxVarIntPayload * 2) +
		(MaxUtreexoBlockSummaryPerMsg * MaxUtreexoBlockSummarySize) +
		(math.MaxUint8 * chainhash.HashSize)
}

// NewMsgUtreexoSummaries returns a new bitcoin utreexo summaries message that conforms to the
// Message interface.  See MsgUtreexoSummaries for details.
func NewMsgUtreexoSummaries() *MsgUtreexoSummaries {
	return &MsgUtreexoSummaries{
		Summaries:   make([]*UtreexoBlockSummary, 0, MaxUtreexoBlockSummaryPerMsg),
		ProofHashes: make([]utreexo.Hash, 0, math.MaxUint8),
	}
}
