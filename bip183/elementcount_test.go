// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip183

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/btcsuite/btcd/wire/v2"
	"github.com/stretchr/testify/require"
)

// TestElementCounts ensures the BIP-183 decoders reject element counts
// that are over the message's limit or that the rest of the payload can't
// hold.
func TestElementCounts(t *testing.T) {
	// varint returns the varint encoding of v.
	varint := func(v uint64) []byte {
		var buf bytes.Buffer
		require.NoError(t, wire.WriteVarInt(&buf, 0, v))
		return buf.Bytes()
	}
	cat := func(bs ...[]byte) []byte { return bytes.Join(bs, nil) }
	hash := make([]byte, 32)

	// decoders reads b with each decoder that takes a count.
	decoders := map[string]func(b []byte) error{
		"MsgUtreexoProof": func(b []byte) error {
			return new(MsgUtreexoProof).BtcDecode(bytes.NewBuffer(b), 0, wire.BaseEncoding)
		},
		"MsgGetUtreexoProof": func(b []byte) error {
			return new(MsgGetUtreexoProof).BtcDecode(bytes.NewBuffer(b), 0, wire.BaseEncoding)
		},
		"MsgUtreexoRoot": func(b []byte) error {
			return new(MsgUtreexoRoot).BtcDecode(bytes.NewBuffer(b), 0, wire.BaseEncoding)
		},
		"MsgUtreexoTTLs": func(b []byte) error {
			return new(MsgUtreexoTTLs).BtcDecode(bytes.NewBuffer(b), 0, wire.BaseEncoding)
		},
		"MsgUtreexoTx": func(b []byte) error {
			return new(MsgUtreexoTx).BtcDecode(bytes.NewBuffer(b), 0, wire.BaseEncoding)
		},
		"BatchProofDeserialize": func(b []byte) error {
			_, err := BatchProofDeserialize(bytes.NewReader(b))
			return err
		},
		"UtreexoBlockSummary": func(b []byte) error {
			return new(UtreexoBlockSummary).Deserialize(bytes.NewBuffer(b))
		},
	}

	tests := []struct {
		name    string
		decoder string
		// prefix is everything before the count under test.
		prefix []byte
		// counts are the counts tried. Empty means 1<<32 and
		// math.MaxUint64.
		counts []uint64
		// msgErr is whether the count is over a limit the message
		// defines, which returns a MessageError.
		msgErr bool
	}{
		{
			name:    "proof hashes over the max",
			decoder: "MsgUtreexoProof",
			prefix:  hash,
			msgErr:  true,
		},
		{
			name:    "proof hashes",
			decoder: "MsgUtreexoProof",
			prefix:  hash,
			counts:  []uint64{MaxProofHashes},
		},
		{
			name:    "targets",
			decoder: "MsgUtreexoProof",
			prefix:  cat(hash, varint(0)),
			msgErr:  true,
		},
		{
			name:    "leaf datas",
			decoder: "MsgUtreexoProof",
			prefix:  cat(hash, varint(0), varint(0)),
			msgErr:  true,
		},
		{
			name:    "proof index bitmap",
			decoder: "MsgGetUtreexoProof",
			prefix:  cat(hash, []byte{0}),
			msgErr:  true,
		},
		{
			name:    "leaf index bitmap",
			decoder: "MsgGetUtreexoProof",
			prefix:  cat(hash, []byte{0}, varint(0)),
			msgErr:  true,
		},
		{
			name:    "roots",
			decoder: "MsgUtreexoRoot",
			prefix:  cat(varint(1), varint(0), hash),
		},
		{
			name:    "root proof hashes",
			decoder: "MsgUtreexoRoot",
			prefix:  cat(varint(1), varint(0), hash, varint(0)),
		},
		{
			name:    "ttls",
			decoder: "MsgUtreexoTTLs",
			prefix:  cat(varint(1), []byte{1, 0, 0, 0}),
		},
		{
			name:    "ttl proof hashes",
			decoder: "MsgUtreexoTTLs",
			prefix:  varint(0),
		},
		{
			name:    "tx proof targets",
			decoder: "MsgUtreexoTx",
		},
		{
			name:    "tx proof hashes",
			decoder: "MsgUtreexoTx",
			prefix:  varint(0),
		},
		{
			name:    "batch proof targets",
			decoder: "BatchProofDeserialize",
		},
		{
			name:    "batch proof hashes",
			decoder: "BatchProofDeserialize",
			prefix:  varint(0),
		},
		{
			name:    "block summary targets",
			decoder: "UtreexoBlockSummary",
			prefix:  cat(hash, varint(0)),
			msgErr:  true,
		},
		{
			name:    "block summary targets at the max",
			decoder: "UtreexoBlockSummary",
			prefix:  cat(hash, varint(0)),
			counts:  []uint64{MaxPossibleInputsPerBlock},
		},
	}

	for _, test := range tests {
		counts := test.counts
		if len(counts) == 0 {
			counts = []uint64{1 << 32, math.MaxUint64}
		}
		for _, count := range counts {
			// A few bytes follow the count so the payload isn't
			// empty.
			b := cat(test.prefix, varint(count), make([]byte, 7))
			err := decoders[test.decoder](b)
			require.Error(t, err, test.name)

			var msgErr *wire.MessageError
			if test.msgErr {
				require.ErrorAs(t, err, &msgErr, test.name)
			} else {
				require.True(t, errors.Is(err, io.ErrUnexpectedEOF), "%s: %v",
					test.name, err)
			}
		}
	}
}
