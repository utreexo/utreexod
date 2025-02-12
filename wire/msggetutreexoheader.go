// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package wire

import (
	"io"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// MsgGetUtreexoHeader implements the Message interface and represents a bitcoin
// getutreexoheader message. It's used to request the utreexo header at the given
// block.
type MsgGetUtreexoHeader struct {
	BlockHash    chainhash.Hash
	IncludeProof bool
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoHeader) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
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
func (msg *MsgGetUtreexoHeader) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
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
func (msg *MsgGetUtreexoHeader) Command() string {
	return CmdGetUtreexoHeader
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (msg *MsgGetUtreexoHeader) MaxPayloadLength(pver uint32) uint32 {
	return chainhash.HashSize
}

// NewMsgGetUtreexoHeader returns a new bitcoin getheaders message that conforms to
// the Message interface.  See MsgGetUtreexoHeader for details.
func NewMsgGetUtreexoHeader(blockHash chainhash.Hash, includeProof bool) *MsgGetUtreexoHeader {
	return &MsgGetUtreexoHeader{BlockHash: blockHash, IncludeProof: includeProof}
}
