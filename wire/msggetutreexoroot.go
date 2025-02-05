// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// MsgGetUtreexoRoot implements the Message interface and represents a bitcoin
// getutreexoroot message. It's used to request the utreexo roots at the given
// block.
type MsgGetUtreexoRoot struct {
	BlockHash chainhash.Hash
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoRoot) BtcDecode(r io.Reader, _ uint32, _ MessageEncoding) error {
	_, err := io.ReadFull(r, msg.BlockHash[:])
	if err != nil {
		return err
	}
	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoRoot) BtcEncode(w io.Writer, _ uint32, _ MessageEncoding) error {
	_, err := w.Write(msg.BlockHash[:])
	return err
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgGetUtreexoRoot) Command() string {
	return CmdGetUtreexoRoot
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (msg *MsgGetUtreexoRoot) MaxPayloadLength(_ uint32) uint32 {
	return chainhash.HashSize
}

// NewMsgGetUtreexoRoot returns a new bitcoin getheaders message that conforms to
// the Message interface.  See MsgGetUtreexoRoot for details.
func NewMsgGetUtreexoRoot(blockHash chainhash.Hash) *MsgGetUtreexoRoot {
	return &MsgGetUtreexoRoot{BlockHash: blockHash}
}
