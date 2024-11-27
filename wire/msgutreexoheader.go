// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package wire

import (
	"io"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// MaxUtreexoHeaderPayload the amount of inputs a block can possibly have multiplied by the size
// of uint64.
const MaxUtreexoHeaderPayload = (28_000 * 8)

// MsgUtreexoHeader implements the Message interface and represents a bitcoin
// utreexo block header message. It's used to provide the positions of the inputs
// that are being spent in the given block.
type MsgUtreexoHeader struct {
	BlockHash chainhash.Hash
	NumAdds   uint16
	Targets   []uint64
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
// See Deserialize for decoding block headers stored to disk, such as in a
// database, as opposed to decoding block headers from the wire.
func (h *MsgUtreexoHeader) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	return readUtreexoHeader(r, pver, h)
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
// See Serialize for encoding block headers to be stored to disk, such as in a
// database, as opposed to encoding block headers for the wire.
func (h *MsgUtreexoHeader) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	return writeUtreexoHeader(w, pver, h)
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (h *MsgUtreexoHeader) Command() string {
	return CmdUtreexoHeader
}

// Deserialize decodes a block header from r into the receiver using a format
// that is suitable for long-term storage such as a database while respecting
// the Version field.
func (h *MsgUtreexoHeader) Deserialize(r io.Reader) error {
	// At the current time, there is no difference between the wire encoding
	// at protocol version 0 and the stable long-term storage format.  As
	// a result, make use of readUtreexoHeader.
	return readUtreexoHeader(r, 0, h)
}

// Serialize encodes a block header from r into the receiver using a format
// that is suitable for long-term storage such as a database while respecting
// the Version field.
func (h *MsgUtreexoHeader) Serialize(w io.Writer) error {
	// At the current time, there is no difference between the wire encoding
	// at protocol version 0 and the stable long-term storage format.  As
	// a result, make use of writeUtreexoHeader.
	return writeUtreexoHeader(w, 0, h)
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (h *MsgUtreexoHeader) MaxPayloadLength(pver uint32) uint32 {
	// BlockHash size + uint16 size + Num headers (varInt) + the max payload of the targets.
	return chainhash.HashSize + 2 + MaxVarIntPayload + MaxUtreexoHeaderPayload
}

// NewMsgUtreexoHeader returns a new MsgUtreexoHeader using the provided version, previous
// block hash, merkle root hash, difficulty bits, and nonce used to generate the
// block with defaults for the remaining fields.
func NewMsgUtreexoHeader(blockHash chainhash.Hash,
	numAdds uint16, targets []uint64) *MsgUtreexoHeader {

	return &MsgUtreexoHeader{
		BlockHash: blockHash,
		NumAdds:   numAdds,
		Targets:   targets,
	}
}

// readBlockHeader reads a bitcoin block header from r.  See Deserialize for
// decoding block headers stored to disk, such as in a database, as opposed to
// decoding from the wire.
func readUtreexoHeader(r io.Reader, _ uint32, bh *MsgUtreexoHeader) error {
	_, err := io.ReadFull(r, bh.BlockHash[:])
	if err != nil {
		return err
	}

	bs := newSerializer()
	defer bs.free()

	bh.NumAdds, err = bs.Uint16(r, littleEndian)
	if err != nil {
		return err
	}

	count, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	bh.Targets = make([]uint64, count)
	for i := range bh.Targets {
		bh.Targets[i], err = ReadVarInt(r, 0)
		if err != nil {
			return err
		}
	}

	return nil
}

// writeBlockHeader writes a bitcoin block header to w.  See Serialize for
// encoding block headers to be stored to disk, such as in a database, as
// opposed to encoding for the wire.
func writeUtreexoHeader(w io.Writer, _ uint32, bh *MsgUtreexoHeader) error {
	_, err := w.Write(bh.BlockHash[:])
	if err != nil {
		return err
	}

	bs := newSerializer()
	defer bs.free()

	err = bs.PutUint16(w, littleEndian, bh.NumAdds)
	if err != nil {
		return err
	}

	err = WriteVarInt(w, 0, uint64(len(bh.Targets)))
	if err != nil {
		return err
	}

	for _, t := range bh.Targets {
		err = WriteVarInt(w, 0, t)
		if err != nil {
			return err
		}
	}

	return nil
}
