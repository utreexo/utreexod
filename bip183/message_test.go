// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip183

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/bip182"
	"github.com/utreexo/utreexod/wire"
)

// TestMessage ensures the BIP-183 messages are read and written on v1 and v2
// connections and that other messages are passed through to wire.
func TestMessage(t *testing.T) {
	hash := chainhash.Hash{0x01}
	tests := []struct {
		msg     wire.Message
		bytes   int   // Expected num bytes read/written on v1
		shortID uint8 // Expected BIP 324 short ID on v2, 0 for long-form
	}{
		{
			msg: &MsgUtreexoProof{
				BlockHash:   hash,
				ProofHashes: []utreexo.Hash{{0x02}},
				Targets:     []uint64{3},
				LeafDatas:   []bip182.LeafData{},
			},
			bytes:   92,
			shortID: 29,
		},
		{
			msg: &MsgGetUtreexoProof{
				BlockHash:        hash,
				RequestBitMap:    1,
				ProofIndexBitMap: []byte{0x01},
				LeafIndexBitMap:  []byte{0x01},
			},
			bytes:   61,
			shortID: 30,
		},
		{
			msg: &MsgUtreexoTTLs{
				TTLs: []UtreexoTTL{{
					BlockHeight: 1,
					TTLs:        []TTLInfo{{DeathHeight: 2, DeathBlkIndex: 3}},
				}},
				ProofHashes: []utreexo.Hash{{0x02}},
			},
			bytes:   71,
			shortID: 31,
		},
		{
			msg:     NewMsgGetUtreexoTTLs(2, 1, 0),
			bytes:   33,
			shortID: 32,
		},
		{
			msg:     NewMsgUtreexoTx(1),
			bytes:   36,
			shortID: 34,
		},
		{
			msg: &MsgUtreexoRoot{
				NumLeaves: 1,
				Target:    0,
				BlockHash: hash,
				Roots:     []utreexo.Hash{{0x02}},
				Proof:     []utreexo.Hash{},
			},
			bytes:   92,
			shortID: 35,
		},
		{
			msg:     NewMsgGetUtreexoRoot(hash),
			bytes:   56,
			shortID: 36,
		},
		{
			msg:     wire.NewMsgPing(7),
			bytes:   32,
			shortID: 18,
		},
		{
			msg:     wire.NewMsgSendAddrV2(),
			bytes:   24,
			shortID: 0,
		},
	}

	for _, test := range tests {
		cmd := test.msg.Command()

		var buf bytes.Buffer
		nw, err := WriteMessageWithEncodingN(&buf, test.msg,
			wire.ProtocolVersion, wire.MainNet, wire.BaseEncoding)
		require.NoError(t, err, cmd)
		require.Equal(t, test.bytes, nw, cmd)

		nr, msg, _, err := ReadMessageWithEncodingN(&buf,
			wire.ProtocolVersion, wire.MainNet, wire.BaseEncoding)
		require.NoError(t, err, cmd)
		require.Equal(t, test.bytes, nr, cmd)
		require.Equal(t, test.msg, msg, cmd)

		buf.Reset()
		_, err = WriteV2MessageN(&buf, test.msg, wire.ProtocolVersion,
			wire.BaseEncoding)
		require.NoError(t, err, cmd)
		require.Equal(t, test.shortID, buf.Bytes()[0], cmd)

		msg, _, err = ReadV2MessageN(buf.Bytes(), wire.ProtocolVersion,
			wire.BaseEncoding)
		require.NoError(t, err, cmd)
		require.Equal(t, test.msg, msg, cmd)
	}
}

// TestMessageLongForm ensures BIP-183 messages sent with the long-form v2
// encoding are read.
func TestMessageLongForm(t *testing.T) {
	msg := NewMsgGetUtreexoRoot(chainhash.Hash{0x01})

	var payload bytes.Buffer
	require.NoError(t, msg.BtcEncode(&payload, wire.ProtocolVersion,
		wire.BaseEncoding))

	var command [wire.CommandSize]byte
	copy(command[:], msg.Command())
	plaintext := append([]byte{0x00}, command[:]...)
	plaintext = append(plaintext, payload.Bytes()...)

	got, _, err := ReadV2MessageN(plaintext, wire.ProtocolVersion,
		wire.BaseEncoding)
	require.NoError(t, err)
	require.Equal(t, msg, got)
}

// rawMessage returns a v1 message with the given network, command, and
// payload, and a checksum computed over the payload.
func rawMessage(btcnet wire.BitcoinNet, command string, payload []byte) []byte {
	hdr := make([]byte, wire.MessageHeaderSize)
	binary.LittleEndian.PutUint32(hdr[0:4], uint32(btcnet))
	copy(hdr[4:4+wire.CommandSize], command)
	binary.LittleEndian.PutUint32(hdr[16:20], uint32(len(payload)))
	copy(hdr[20:24], chainhash.DoubleHashB(payload)[0:4])
	return append(hdr, payload...)
}

// TestReadMessageErrors ensures BIP-183 messages are rejected on v1
// connections when their header or payload is invalid.
func TestReadMessageErrors(t *testing.T) {
	msg := &MsgGetUtreexoProof{
		BlockHash:        chainhash.Hash{0x01},
		RequestBitMap:    1,
		ProofIndexBitMap: []byte{0x01},
		LeafIndexBitMap:  []byte{0x01},
	}
	var payload bytes.Buffer
	require.NoError(t, msg.BtcEncode(&payload, wire.ProtocolVersion,
		wire.BaseEncoding))
	valid := rawMessage(wire.MainNet, msg.Command(), payload.Bytes())

	// withLength returns the valid message with the payload length in the
	// header set to length.
	withLength := func(length uint32) []byte {
		b := bytes.Clone(valid)
		binary.LittleEndian.PutUint32(b[16:20], length)
		return b
	}

	badChecksum := bytes.Clone(valid)
	badChecksum[20] ^= 0xff

	extraBytes := append(bytes.Clone(payload.Bytes()), 0x00)

	tests := []struct {
		name  string
		bytes []byte
	}{
		{
			name:  "other network",
			bytes: rawMessage(wire.TestNet3, msg.Command(), payload.Bytes()),
		},
		{
			name:  "payload over the max message payload",
			bytes: withLength(wire.MaxMessagePayload + 1),
		},
		{
			name:  "payload over the max length for the message",
			bytes: withLength(msg.MaxPayloadLength(wire.ProtocolVersion) + 1),
		},
		{
			name:  "bad checksum",
			bytes: badChecksum,
		},
		{
			name:  "extra bytes after decode",
			bytes: rawMessage(wire.MainNet, msg.Command(), extraBytes),
		},
	}

	// The valid message is read.
	_, got, _, err := ReadMessageWithEncodingN(bytes.NewReader(valid),
		wire.ProtocolVersion, wire.MainNet, wire.BaseEncoding)
	require.NoError(t, err)
	require.Equal(t, msg, got)

	for _, test := range tests {
		_, _, _, err := ReadMessageWithEncodingN(bytes.NewReader(test.bytes),
			wire.ProtocolVersion, wire.MainNet, wire.BaseEncoding)
		var msgErr *wire.MessageError
		require.ErrorAs(t, err, &msgErr, test.name)
	}
}

// TestReadV2MessageErrors ensures BIP-183 messages over their max payload
// length are rejected on v2 connections.
func TestReadV2MessageErrors(t *testing.T) {
	msg := NewMsgGetUtreexoRoot(chainhash.Hash{0x01})
	mpl := msg.MaxPayloadLength(wire.ProtocolVersion)

	plaintext := append([]byte{v2Messages[msg.Command()]},
		make([]byte, mpl+1)...)
	_, _, err := ReadV2MessageN(plaintext, wire.ProtocolVersion,
		wire.BaseEncoding)
	require.ErrorContains(t, err, "payload exceeds max length")
}
