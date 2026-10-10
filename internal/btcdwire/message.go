// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcdwire

// COPIED FROM BTCD'S WIRE PACKAGE. These are wire's ReadMessageWithEncodingN,
// WriteMessageWithEncodingN, ReadV2MessageN, and WriteV2MessageN, changed to
// take the messages wire doesn't define as parameters, to bound BIP-183
// messages by wire.MaxMessagePayload since they can exceed
// wire.MaxProtocolMessageLength, and to use encoding/binary where wire uses
// unexported helpers.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
)

// ReadMessageWithEncodingN reads, validates, and parses the next bitcoin
// Message from r for the provided protocol version and bitcoin network.  It
// returns the number of bytes read in addition to the parsed Message and raw
// bytes which comprise the message.  newMsg returns an empty message for the
// commands parsed here and nil for every other command, which is read with
// wire.ReadMessageWithEncodingN.
func ReadMessageWithEncodingN(r io.Reader, pver uint32, btcnet wire.BitcoinNet,
	enc wire.MessageEncoding, newMsg func(command string) wire.Message) (
	int, wire.Message, []byte, error) {

	// Read the header to find out which message follows.
	var hdr [wire.MessageHeaderSize]byte
	totalBytes, err := io.ReadFull(r, hdr[:])
	if err != nil {
		return totalBytes, nil, nil, err
	}

	command := string(bytes.TrimRight(hdr[4:4+wire.CommandSize], "\x00"))
	msg := newMsg(command)
	if msg == nil {
		// Hand wire the header that was already read followed by the
		// rest of the message.
		mr := io.MultiReader(bytes.NewReader(hdr[:]), r)
		return wire.ReadMessageWithEncodingN(mr, pver, btcnet, enc)
	}

	magic := wire.BitcoinNet(binary.LittleEndian.Uint32(hdr[0:4]))
	length := binary.LittleEndian.Uint32(hdr[16:20])
	checksum := hdr[20:24]

	// Enforce maximum message payload.
	if length > wire.MaxMessagePayload {
		str := fmt.Sprintf("message payload is too large - header "+
			"indicates %d bytes, but max message payload is %d "+
			"bytes.", length, wire.MaxMessagePayload)
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)
	}

	// Check for messages from the wrong bitcoin network.
	if magic != btcnet {
		discardInput(r, length)
		str := fmt.Sprintf("message from other network [%v]", magic)
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)
	}

	// Check for maximum length based on the message type as a malicious
	// client could otherwise create a well-formed header and set the length
	// to max numbers in order to exhaust the machine's memory.
	mpl := msg.MaxPayloadLength(pver)
	if length > mpl {
		discardInput(r, length)
		str := fmt.Sprintf("payload exceeds max length - header "+
			"indicates %v bytes, but max payload size for "+
			"messages of type [%v] is %v.", length, command, mpl)
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)
	}

	// Read payload.
	payload := make([]byte, length)
	n, err := io.ReadFull(r, payload)
	totalBytes += n
	if err != nil {
		return totalBytes, nil, nil, err
	}

	// Test checksum.
	sum := chainhash.DoubleHashB(payload)[0:4]
	if !bytes.Equal(sum, checksum) {
		str := fmt.Sprintf("payload checksum failed - header "+
			"indicates %v, but actual checksum is %v.",
			checksum, sum)
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)
	}

	pr := bytes.NewBuffer(payload)
	err = msg.BtcDecode(pr, pver, enc)
	if err != nil {
		return totalBytes, nil, nil, err
	}

	// Ensure the entire payload was consumed by BtcDecode. Trailing bytes
	// would pass checksum validation but are not part of the decoded
	// message.
	if pr.Len() > 0 {
		str := fmt.Sprintf("message payload has %d extra bytes "+
			"after decode", pr.Len())
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)
	}

	return totalBytes, msg, payload, nil
}

// WriteMessageWithEncodingN writes a bitcoin Message to w including the
// necessary header information and returns the number of bytes written.
// newMsg returns an empty message for the commands written here and nil for
// every other command, which is written with wire.WriteMessageWithEncodingN.
func WriteMessageWithEncodingN(w io.Writer, msg wire.Message, pver uint32,
	btcnet wire.BitcoinNet, encoding wire.MessageEncoding,
	newMsg func(command string) wire.Message) (int, error) {

	cmd := msg.Command()
	if newMsg(cmd) == nil {
		return wire.WriteMessageWithEncodingN(w, msg, pver, btcnet,
			encoding)
	}

	// Encode the message payload.
	var bw bytes.Buffer
	err := msg.BtcEncode(&bw, pver, encoding)
	if err != nil {
		return 0, err
	}
	payload := bw.Bytes()
	lenp := len(payload)

	// Enforce maximum overall message payload.
	if lenp > wire.MaxMessagePayload {
		str := fmt.Sprintf("message payload is too large - encoded "+
			"%d bytes, but maximum message payload is %d bytes",
			lenp, wire.MaxMessagePayload)
		return 0, NewMessageError("WriteMessage", str)
	}

	// Enforce maximum message payload based on the message type.
	mpl := msg.MaxPayloadLength(pver)
	if uint32(lenp) > mpl {
		str := fmt.Sprintf("message payload is too large - encoded "+
			"%d bytes, but maximum message payload size for "+
			"messages of type [%s] is %d.", lenp, cmd, mpl)
		return 0, NewMessageError("WriteMessage", str)
	}

	// Create the header for the message.
	var hdr [wire.MessageHeaderSize]byte
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(btcnet))
	copy(hdr[4:4+wire.CommandSize], cmd)
	binary.LittleEndian.PutUint32(hdr[16:20], uint32(lenp))
	copy(hdr[20:24], chainhash.DoubleHashB(payload)[0:4])

	// Write header.
	totalBytes, err := w.Write(hdr[:])
	if err != nil {
		return totalBytes, err
	}

	// Only write the payload if there is one.
	if len(payload) > 0 {
		n, err := w.Write(payload)
		totalBytes += n
		return totalBytes, err
	}

	return totalBytes, nil
}

// ReadV2MessageN takes the passed plaintext and attempts to construct a
// Message from the bytes using the bip324 v2 encoding.  shortIDs maps the
// short IDs of the messages parsed here to their commands, and newMsg returns
// an empty message for those commands and nil for every other command.  Every
// other message is parsed with wire.ReadV2MessageN.
func ReadV2MessageN(plaintext []byte, pver uint32, enc wire.MessageEncoding,
	newMsg func(command string) wire.Message, shortIDs map[uint8]string) (
	wire.Message, []byte, error) {

	// Find the command from either the long-form encoding or the short ID.
	var command string
	var payload []byte
	switch {
	case len(plaintext) == 0:

	case plaintext[0] == 0x00:
		if len(plaintext) >= wire.CommandSize+1 {
			command = string(bytes.TrimRight(
				plaintext[1:wire.CommandSize+1], "\x00"))
			payload = plaintext[wire.CommandSize+1:]
		}

	default:
		command = shortIDs[plaintext[0]]
		payload = plaintext[1:]
	}

	msg := newMsg(command)
	if msg == nil {
		return wire.ReadV2MessageN(plaintext, pver, enc)
	}

	// Enforce maximum message payload.
	if len(payload) > wire.MaxMessagePayload {
		str := fmt.Sprintf("message payload is too large - "+
			"%d bytes, but max message payload is %d bytes",
			len(payload), wire.MaxMessagePayload)
		return nil, nil, NewMessageError("ReadV2MessageN", str)
	}

	// Check for maximum length based on the message type.
	mpl := msg.MaxPayloadLength(pver)
	if len(payload) > int(mpl) {
		str := fmt.Sprintf("payload exceeds max length - "+
			"%d bytes, but max payload size for messages of "+
			"type [%v] is %v.", len(payload), command, mpl)
		return nil, nil, NewMessageError("ReadV2MessageN", str)
	}

	buf := bytes.NewBuffer(payload)
	err := msg.BtcDecode(buf, pver, enc)
	if err != nil {
		return nil, nil, err
	}

	if buf.Len() > 0 {
		str := fmt.Sprintf("message payload has %d extra bytes "+
			"after decode", buf.Len())
		return nil, nil, NewMessageError("ReadV2MessageN", str)
	}

	return msg, payload, nil
}

// WriteV2MessageN writes a Message to the passed Writer using the bip324 v2
// encoding.  shortIDs maps the commands of the messages written here to their
// short IDs, and every other message is written with wire.WriteV2MessageN.
func WriteV2MessageN(w io.Writer, msg wire.Message, pver uint32,
	encoding wire.MessageEncoding, shortIDs map[string]uint8) (int, error) {

	cmd := msg.Command()
	shortID, ok := shortIDs[cmd]
	if !ok {
		return wire.WriteV2MessageN(w, msg, pver, encoding)
	}

	var bw bytes.Buffer
	err := msg.BtcEncode(&bw, pver, encoding)
	if err != nil {
		return 0, err
	}

	payload := bw.Bytes()
	lenp := len(payload)

	// Enforce maximum overall message payload.
	if lenp > wire.MaxMessagePayload {
		str := fmt.Sprintf("message payload is too large - encoded "+
			"%d bytes, but maximum message payload is %d bytes",
			lenp, wire.MaxMessagePayload)
		return 0, NewMessageError("WriteMessage", str)
	}

	mpl := msg.MaxPayloadLength(pver)
	if uint32(lenp) > mpl {
		str := fmt.Sprintf("message payload is too large - encoded "+
			"%d bytes, but maximum message payload size for "+
			"messages of type [%s] is %d.", lenp, cmd, mpl)
		return 0, NewMessageError("WriteMessage", str)
	}

	n, err := w.Write([]byte{shortID})
	if err != nil {
		return n, err
	}
	totalBytes := n

	n, err = w.Write(payload)
	totalBytes += n

	return totalBytes, err
}

// discardInput reads n bytes from reader r and discards them.  It is used to
// skip the payload of a message that is rejected after its header is read.
func discardInput(r io.Reader, n uint32) {
	_, _ = io.CopyN(io.Discard, r, int64(n))
}
