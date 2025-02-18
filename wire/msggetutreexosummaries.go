// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package wire

import (
	"io"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// MsgGetUtreexoSummaries implements the Message interface and represents a bitcoin
// getutreexoheader message. It's used to request the utreexo header at the given
// block.
type MsgGetUtreexoSummaries struct {
	BlockHash    chainhash.Hash
	IncludeProof bool
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	_, err := io.ReadFull(r, msg.BlockHash[:])
	if err != nil {
		return err
	}

	msg.IncludeProof = msg.BlockHash[31] == 2
	msg.BlockHash[31] = 0

	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	if !msg.IncludeProof {
		msg.BlockHash[31] = 1
	} else {
		msg.BlockHash[31] = 2
	}

	_, err := w.Write(msg.BlockHash[:])
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

// NewMsgGetUtreexoSummaries returns a new bitcoin getheaders message that conforms to
// the Message interface.  See MsgGetUtreexoSummaries for details.
func NewMsgGetUtreexoSummaries(blockHash chainhash.Hash, includeProof bool) *MsgGetUtreexoSummaries {
	return &MsgGetUtreexoSummaries{BlockHash: blockHash, IncludeProof: includeProof}
}
