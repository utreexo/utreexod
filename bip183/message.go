// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip183

import (
	"io"

	"github.com/utreexo/utreexod/internal/btcdwire"
	"github.com/utreexo/utreexod/wire"
)

var (
	// v2MessageIDs maps the BIP 324 short IDs utreexo nodes use for the
	// BIP-183 messages to their commands.
	v2MessageIDs = map[uint8]string{
		29: CmdUtreexoProof,
		30: CmdGetUtreexoProof,
		31: CmdUtreexoTTLs,
		32: CmdGetUtreexoTTLs,
		34: CmdUtreexoTx,
		35: CmdUtreexoRoot,
		36: CmdGetUtreexoRoot,
	}

	// v2Messages maps the commands of the BIP-183 messages to their BIP 324
	// short IDs.
	v2Messages = map[string]uint8{
		CmdUtreexoProof:    29,
		CmdGetUtreexoProof: 30,
		CmdUtreexoTTLs:     31,
		CmdGetUtreexoTTLs:  32,
		CmdUtreexoTx:       34,
		CmdUtreexoRoot:     35,
		CmdGetUtreexoRoot:  36,
	}
)

// makeEmptyMessage creates a message of the appropriate concrete type based
// on the command.  It returns nil if the command isn't a BIP-183 message.
func makeEmptyMessage(command string) wire.Message {
	switch command {
	case CmdUtreexoTx:
		return &MsgUtreexoTx{}

	case CmdGetUtreexoTTLs:
		return &MsgGetUtreexoTTLs{}

	case CmdUtreexoTTLs:
		return &MsgUtreexoTTLs{}

	case CmdUtreexoProof:
		return &MsgUtreexoProof{}

	case CmdGetUtreexoProof:
		return &MsgGetUtreexoProof{}

	case CmdUtreexoRoot:
		return &MsgUtreexoRoot{}

	case CmdGetUtreexoRoot:
		return &MsgGetUtreexoRoot{}
	}

	return nil
}

// ReadMessageWithEncodingN reads, validates, and parses the next bitcoin
// Message from r for the provided protocol version and bitcoin network.  It
// returns the number of bytes read in addition to the parsed Message and raw
// bytes which comprise the message.  Every message other than the BIP-183
// messages is read with wire.ReadMessageWithEncodingN.
func ReadMessageWithEncodingN(r io.Reader, pver uint32, btcnet wire.BitcoinNet,
	enc wire.MessageEncoding) (int, wire.Message, []byte, error) {

	return btcdwire.ReadMessageWithEncodingN(r, pver, btcnet, enc,
		makeEmptyMessage)
}

// ReadV2MessageN takes the passed plaintext and attempts to construct a
// Message from the bytes using the bip324 v2 encoding.  Every message other
// than the BIP-183 messages is parsed with wire.ReadV2MessageN.
func ReadV2MessageN(plaintext []byte, pver uint32, enc wire.MessageEncoding) (
	wire.Message, []byte, error) {

	return btcdwire.ReadV2MessageN(plaintext, pver, enc, makeEmptyMessage,
		v2MessageIDs)
}

// WriteV2MessageN writes a Message to the passed Writer using the bip324 v2
// encoding.  BIP-183 messages are written with their short IDs and every
// other message is written with wire.WriteV2MessageN.
func WriteV2MessageN(w io.Writer, msg wire.Message, pver uint32,
	encoding wire.MessageEncoding) (int, error) {

	return btcdwire.WriteV2MessageN(w, msg, pver, encoding, v2Messages)
}
