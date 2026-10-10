// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcdwire

// COPIED FROM BTCD'S WIRE PACKAGE. These are wire's ReadMessageWithEncodingN,
// WriteMessageWithEncodingN, ReadV2MessageN, and WriteV2MessageN and the
// helpers they call, changed to take the messages wire doesn't define as
// parameters, to hand every other message to wire, and to use encoding/binary
// where wire uses unexported helpers.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/utreexo/utreexod/wire"
)

// messageHeader defines the header structure for all bitcoin protocol messages.
type messageHeader struct {
	magic    wire.BitcoinNet // 4 bytes
	command  string          // 12 bytes
	length   uint32          // 4 bytes
	checksum [4]byte         // 4 bytes

	// raw is the header as read, for handing a message that isn't parsed
	// here back to wire.
	raw [wire.MessageHeaderSize]byte
}

// readMessageHeader reads a bitcoin message header from r.
func readMessageHeader(r io.Reader) (int, *messageHeader, error) {
	// Read the entire header into a buffer first in case there is a short
	// read so the proper amount of read bytes are known.  This works since
	// the header is a fixed size.
	var headerBytes [wire.MessageHeaderSize]byte
	n, err := io.ReadFull(r, headerBytes[:])
	if err != nil {
		return n, nil, err
	}

	// Create and populate a messageHeader struct from the raw header bytes.
	hdr := messageHeader{raw: headerBytes}
	var command [wire.CommandSize]byte
	magic := binary.LittleEndian.Uint32(headerBytes[0:4])
	hdr.magic = wire.BitcoinNet(magic)
	copy(command[:], headerBytes[4:4+wire.CommandSize])
	hdr.length = binary.LittleEndian.Uint32(headerBytes[16:20])
	copy(hdr.checksum[:], headerBytes[20:24])

	// Strip trailing zeros from command string.
	hdr.command = string(bytes.TrimRight(command[:], "\x00"))

	return n, &hdr, nil
}

// discardInput reads n bytes from reader r in chunks and discards the read
// bytes.  This is used to skip payloads when various errors occur and helps
// prevent rogue nodes from causing massive memory allocation through forging
// header length.
func discardInput(r io.Reader, n uint32) {
	maxSize := uint32(10 * 1024) // 10k at a time
	numReads := n / maxSize
	bytesRemaining := n % maxSize
	if n > 0 {
		buf := make([]byte, maxSize)
		for i := uint32(0); i < numReads; i++ {
			io.ReadFull(r, buf)
		}
	}
	if bytesRemaining > 0 {
		buf := make([]byte, bytesRemaining)
		io.ReadFull(r, buf)
	}
}

// WriteV2MessageN writes a Message to the passed Writer using the bip324 v2
// encoding.  shortIDs maps the commands of the messages written here to their
// short IDs, and every other message is written with wire.WriteV2MessageN.
func WriteV2MessageN(w io.Writer, msg wire.Message, pver uint32,
	encoding wire.MessageEncoding, shortIDs map[string]uint8) (int, error) {

	var totalBytes int

	cmd := msg.Command()
	if len(cmd) > wire.CommandSize {
		str := fmt.Sprintf("command [%s] is too long [max %v]",
			cmd, wire.CommandSize)
		return totalBytes, NewMessageError("WriteMessage", str)
	}

	index, exists := shortIDs[cmd]
	if !exists {
		return wire.WriteV2MessageN(w, msg, pver, encoding)
	}

	n, err := w.Write([]byte{index})
	if err != nil {
		return 0, err
	}

	totalBytes += n

	var bw bytes.Buffer
	err = msg.BtcEncode(&bw, pver, encoding)
	if err != nil {
		return totalBytes, err
	}

	payload := bw.Bytes()
	lenp := len(payload)

	// Enforce maximum overall message payload.
	if lenp > wire.MaxMessagePayload {
		str := fmt.Sprintf("message payload is too large - encoded "+
			"%d bytes, but maximum message payload is %d bytes",
			lenp, wire.MaxMessagePayload)
		return totalBytes, NewMessageError("WriteMessage", str)
	}

	mpl := msg.MaxPayloadLength(pver)
	if uint32(lenp) > mpl {
		str := fmt.Sprintf("message payload is too large - encoded "+
			"%d bytes, but maximum message payload size for "+
			"messages of type [%s] is %d.", lenp, cmd, mpl)
		return totalBytes, NewMessageError("WriteMessage", str)
	}

	if len(payload) > 0 {
		n, err := w.Write(payload)
		totalBytes += n

		return totalBytes, err
	}

	return totalBytes, nil
}

// WriteMessageWithEncodingN writes a bitcoin Message to w including the
// necessary header information and returns the number of bytes written.
// newMsg returns an empty message for the commands written here and nil for
// every other command, which is written with wire.WriteMessageWithEncodingN.
func WriteMessageWithEncodingN(w io.Writer, msg wire.Message, pver uint32,
	btcnet wire.BitcoinNet, encoding wire.MessageEncoding,
	newMsg func(command string) wire.Message) (int, error) {

	totalBytes := 0

	// Enforce max command size.
	var command [wire.CommandSize]byte
	cmd := msg.Command()
	if len(cmd) > wire.CommandSize {
		str := fmt.Sprintf("command [%s] is too long [max %v]",
			cmd, wire.CommandSize)
		return totalBytes, NewMessageError("WriteMessage", str)
	}
	copy(command[:], []byte(cmd))

	if newMsg(cmd) == nil {
		return wire.WriteMessageWithEncodingN(w, msg, pver, btcnet,
			encoding)
	}

	// Encode the message payload.
	var bw bytes.Buffer
	err := msg.BtcEncode(&bw, pver, encoding)
	if err != nil {
		return totalBytes, err
	}
	payload := bw.Bytes()
	lenp := len(payload)

	// Enforce maximum overall message payload.
	if lenp > wire.MaxMessagePayload {
		str := fmt.Sprintf("message payload is too large - encoded "+
			"%d bytes, but maximum message payload is %d bytes",
			lenp, wire.MaxMessagePayload)
		return totalBytes, NewMessageError("WriteMessage", str)
	}

	// Enforce maximum message payload based on the message type.
	mpl := msg.MaxPayloadLength(pver)
	if uint32(lenp) > mpl {
		str := fmt.Sprintf("message payload is too large - encoded "+
			"%d bytes, but maximum message payload size for "+
			"messages of type [%s] is %d.", lenp, cmd, mpl)
		return totalBytes, NewMessageError("WriteMessage", str)
	}

	// Create header for the message.
	hdr := messageHeader{}
	hdr.magic = btcnet
	hdr.command = cmd
	hdr.length = uint32(lenp)
	copy(hdr.checksum[:], chainhash.DoubleHashB(payload)[0:4])

	// Encode the header for the message.
	var hw [wire.MessageHeaderSize]byte
	binary.LittleEndian.PutUint32(hw[0:4], uint32(hdr.magic))
	copy(hw[4:4+wire.CommandSize], command[:])
	binary.LittleEndian.PutUint32(hw[16:20], hdr.length)
	copy(hw[20:24], hdr.checksum[:])

	// Write header.
	n, err := w.Write(hw[:])
	totalBytes += n
	if err != nil {
		return totalBytes, err
	}

	// Only write the payload if there is one, e.g., verack messages don't
	// have one.
	if len(payload) > 0 {
		n, err = w.Write(payload)
		totalBytes += n
	}

	return totalBytes, err
}

// ReadV2MessageN takes the passed plaintext and attempts to construct a
// Message from the bytes using the bip324 v2 encoding.  shortIDs maps the
// short IDs of the messages parsed here to their commands, and newMsg returns
// an empty message for those commands and nil for every other command.  Every
// other message is parsed with wire.ReadV2MessageN.
func ReadV2MessageN(plaintext []byte, pver uint32, enc wire.MessageEncoding,
	newMsg func(command string) wire.Message, shortIDs map[uint8]string) (
	wire.Message, []byte, error) {

	if len(plaintext) == 0 {
		return nil, nil, fmt.Errorf("invalid plaintext length")
	}

	// Keep the whole plaintext to hand to wire if newMsg returns nil for
	// its command.
	full := plaintext

	var msgCmd string

	// If the first byte is 0x00, read the next 12 bytes to determine what
	// message this is.
	if plaintext[0] == 0x00 {
		if len(plaintext) < wire.CommandSize+1 {
			return nil, nil, fmt.Errorf("invalid plaintext length")
		}

		// Slice off the first 0x00 and the trailing 0x00 bytes.
		var command [wire.CommandSize]byte
		copy(command[:], plaintext[1:wire.CommandSize+1])

		msgCmd = string(bytes.TrimRight(command[:], "\x00"))

		plaintext = plaintext[wire.CommandSize+1:]
	} else {
		// The first byte denotes what message this is.
		msgCmd = shortIDs[plaintext[0]]

		plaintext = plaintext[1:]
	}

	msg := newMsg(msgCmd)
	if msg == nil {
		return wire.ReadV2MessageN(full, pver, enc)
	}

	mpl := msg.MaxPayloadLength(pver)
	if len(plaintext) > int(mpl) {
		return nil, nil, fmt.Errorf("payload exceeds max length")
	}

	buf := bytes.NewBuffer(plaintext)
	err := msg.BtcDecode(buf, pver, enc)
	if err != nil {
		return nil, nil, err
	}

	return msg, plaintext, nil
}

// ReadMessageWithEncodingN reads, validates, and parses the next bitcoin
// Message from r for the provided protocol version and bitcoin network.  It
// returns the number of bytes read in addition to the parsed Message and raw
// bytes which comprise the message.  newMsg returns an empty message for the
// commands parsed here and nil for every other command, which is read with
// wire.ReadMessageWithEncodingN.
func ReadMessageWithEncodingN(r io.Reader, pver uint32, btcnet wire.BitcoinNet,
	enc wire.MessageEncoding, newMsg func(command string) wire.Message) (
	int, wire.Message, []byte, error) {

	totalBytes := 0
	n, hdr, err := readMessageHeader(r)
	totalBytes += n
	if err != nil {
		return totalBytes, nil, nil, err
	}

	// Hand every message newMsg returns nil for to wire before checking
	// anything, with the header put back in front of the rest of the
	// message, so wire reads it exactly as it would on its own.
	msg := newMsg(hdr.command)
	if msg == nil {
		mr := io.MultiReader(bytes.NewReader(hdr.raw[:]), r)
		return wire.ReadMessageWithEncodingN(mr, pver, btcnet, enc)
	}

	return readMessageWithEncodingNInternal(
		r, pver, hdr, msg, btcnet, enc, totalBytes,
	)
}

// readMessageWithEncodingNInternal checks hdr, then reads the payload that
// follows it and decodes the payload into msg, the empty message newMsg
// returned for hdr's command.
func readMessageWithEncodingNInternal(r io.Reader, pver uint32,
	hdr *messageHeader, msg wire.Message, btcnet wire.BitcoinNet,
	enc wire.MessageEncoding, totalBytes int) (int, wire.Message, []byte,
	error) {

	// Enforce maximum message payload.
	if hdr.length > wire.MaxMessagePayload {
		str := fmt.Sprintf("message payload is too large - header "+
			"indicates %d bytes, but max message payload is %d "+
			"bytes.", hdr.length, wire.MaxMessagePayload)
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)

	}

	// Check for messages from the wrong bitcoin network.
	if hdr.magic != btcnet {
		discardInput(r, hdr.length)
		str := fmt.Sprintf("message from other network [%v]", hdr.magic)
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)
	}

	// Check for malformed commands.
	command := hdr.command
	if !utf8.ValidString(command) {
		discardInput(r, hdr.length)
		str := fmt.Sprintf("invalid command %v", []byte(command))
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)
	}

	// Check for maximum length based on the message type as a malicious client
	// could otherwise create a well-formed header and set the length to max
	// numbers in order to exhaust the machine's memory.
	mpl := msg.MaxPayloadLength(pver)
	if hdr.length > mpl {
		discardInput(r, hdr.length)
		str := fmt.Sprintf("payload exceeds max length - header "+
			"indicates %v bytes, but max payload size for "+
			"messages of type [%v] is %v.", hdr.length, command, mpl)
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)
	}

	// Read payload.
	payload := make([]byte, hdr.length)
	n, err := io.ReadFull(r, payload)
	totalBytes += n
	if err != nil {
		return totalBytes, nil, nil, err
	}

	// Test checksum.
	checksum := chainhash.DoubleHashB(payload)[0:4]
	if !bytes.Equal(checksum, hdr.checksum[:]) {
		str := fmt.Sprintf("payload checksum failed - header "+
			"indicates %v, but actual checksum is %v.",
			hdr.checksum, checksum)
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)
	}

	// Unmarshal message.  NOTE: This must be a *bytes.Buffer since the
	// MsgVersion BtcDecode function requires it.
	pr := bytes.NewBuffer(payload)
	err = msg.BtcDecode(pr, pver, enc)
	if err != nil {
		return totalBytes, nil, nil, err
	}

	// Ensure the entire payload was consumed by BtcDecode. Trailing
	// bytes would pass checksum validation but are not part of the
	// decoded message.
	if pr.Len() > 0 {
		str := fmt.Sprintf("message payload has %d extra bytes "+
			"after decode", pr.Len())
		return totalBytes, nil, nil, NewMessageError("ReadMessage", str)
	}

	return totalBytes, msg, payload, nil
}
