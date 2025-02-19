// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package wire

import (
	"encoding/binary"
	"io"
)

// MsgGetUtreexoSummaries implements the Message interface and represents a bitcoin
// getutreexosummaries message. It's used to request the utreexo summaries at the given
// blocks.
type MsgGetUtreexoSummaries struct {
	BlockPosition uint64
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	bs := newSerializer()
	defer bs.free()

	var err error
	msg.BlockPosition, err = bs.Uint64(r, binary.LittleEndian)
	return err
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	bs := newSerializer()
	defer bs.free()

	return bs.PutUint64(w, binary.LittleEndian, msg.BlockPosition)
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) Command() string {
	return CmdGetUtreexoSummaries
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) MaxPayloadLength(pver uint32) uint32 {
	return 8
}

// NewMsgGetUtreexoSummaries returns a new bitcoin getutreexosummaries message that conforms to
// the Message interface.  See MsgGetUtreexoSummaries for details.
func NewMsgGetUtreexoSummaries(blockPosition uint64) *MsgGetUtreexoSummaries {
	return &MsgGetUtreexoSummaries{BlockPosition: blockPosition}
}
