// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package wire

import (
	"fmt"
	"io"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// MsgGetUtreexoSummaries implements the Message interface and represents a bitcoin
// getutreexosummaries message. It's used to request the utreexo summaries from the given
// start hash.
type MsgGetUtreexoSummaries struct {
	StartHash        chainhash.Hash
	MaxReceiveBlocks uint8
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	_, err := io.ReadFull(r, msg.StartHash[:])
	if err != nil {
		return err
	}

	msg.MaxReceiveBlocks = msg.StartHash[31]
	msg.StartHash[31] = 0

	if msg.MaxReceiveBlocks > MaxUtreexoBlockSummaryPerMsg {
		str := fmt.Sprintf("too many summaries in message [max %v]",
			MaxUtreexoBlockSummaryPerMsg)
		return messageError("MsgGetUtreexoSummaries.BtcDecode", str)
	}

	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	if msg.MaxReceiveBlocks > MaxUtreexoBlockSummaryPerMsg {
		str := fmt.Sprintf("too many summaries in message [max %v]",
			MaxUtreexoBlockSummaryPerMsg)
		return messageError("MsgGetUtreexoSummaries.BtcEncode", str)
	}

	msg.StartHash[31] = msg.MaxReceiveBlocks

	_, err := w.Write(msg.StartHash[:])
	if err != nil {
		return err
	}

	return nil
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) Command() string {
	return CmdGetUtreexoSummaries
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) MaxPayloadLength(pver uint32) uint32 {
	return chainhash.HashSize
}

// NewMsgGetUtreexoSummaries returns a new bitcoin getutreexosummaries message that conforms to
// the Message interface.  See MsgGetUtreexoSummaries for details.
func NewMsgGetUtreexoSummaries(blockHash chainhash.Hash, maxReceiveBlocks uint8) *MsgGetUtreexoSummaries {
	return &MsgGetUtreexoSummaries{StartHash: blockHash, MaxReceiveBlocks: maxReceiveBlocks}
}
