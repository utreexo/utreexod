// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"fmt"
	"io"
)

// MsgGetUtreexoTTLs implements the Message interface and represents a bitcoin
// getutreexottls message. It's used to request the utreexo ttls from the given
// start height.
type MsgGetUtreexoTTLs struct {
	// Version is the height of the committed ttl accumulator. It's used to
	// specify which accumulator the ttl should be proved against.
	Version uint32

	// StartHeight is the first block which the ttl message will be provided for.
	StartHeight uint32

	// MaxReceiveExponent denotes how many ttls should be provided. The provided ttl
	// count will be 2**MaxReceiveExponent.
	MaxReceiveExponent uint8
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoTTLs) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	bs := newSerializer()
	defer bs.free()

	var err error
	msg.Version, err = bs.Uint32(r, littleEndian)
	if err != nil {
		return err
	}

	msg.StartHeight, err = bs.Uint32(r, littleEndian)
	if err != nil {
		return err
	}

	msg.MaxReceiveExponent, err = bs.Uint8(r)
	if err != nil {
		return err
	}

	if msg.Version < msg.StartHeight {
		str := fmt.Sprintf("version cannot be lower than startheight "+
			"[version %v, startheight %v]",
			msg.Version, msg.StartHeight)
		return messageError("MsgGetUtreexoTTLs.BtcDecode", str)
	}

	if msg.MaxReceiveExponent > MaxUtreexoTTLExponent {
		str := fmt.Sprintf("exponent too high in message [max %v, got %v]",
			MaxUtreexoTTLExponent, msg.MaxReceiveExponent)
		return messageError("MsgGetUtreexoTTLs.BtcDecode", str)
	}

	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoTTLs) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	if msg.MaxReceiveExponent > MaxUtreexoTTLExponent {
		str := fmt.Sprintf("exponent too high in message [max %v, got %v]",
			MaxUtreexoTTLExponent, msg.MaxReceiveExponent)
		return messageError("MsgGetUtreexoTTLs.BtcEncode", str)
	}

	if msg.Version < msg.StartHeight {
		str := fmt.Sprintf("version cannot be lower than startheight "+
			"[version %v, startheight %v]",
			msg.Version, msg.StartHeight)
		return messageError("MsgGetUtreexoTTLs.BtcEncode", str)
	}

	bs := newSerializer()
	defer bs.free()

	err := bs.PutUint32(w, littleEndian, msg.Version)
	if err != nil {
		return err
	}

	err = bs.PutUint32(w, littleEndian, msg.StartHeight)
	if err != nil {
		return err
	}

	return bs.PutUint8(w, msg.MaxReceiveExponent)
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgGetUtreexoTTLs) Command() string {
	return CmdGetUtreexoTTLs
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (msg *MsgGetUtreexoTTLs) MaxPayloadLength(pver uint32) uint32 {
	return 9
}

// NewMsgGetUtreexoTTLs returns a new bitcoin getutreexottls message that conforms to
// the Message interface.  See MsgGetUtreexoTTLs for details.
func NewMsgGetUtreexoTTLs(version, startHeight uint32, maxReceiveExponent uint8) *MsgGetUtreexoTTLs {
	return &MsgGetUtreexoTTLs{
		Version:            version,
		StartHeight:        startHeight,
		MaxReceiveExponent: maxReceiveExponent,
	}
}

// GetUtreexoTTLsExponent returns the ideal exponent to minimize the proof size for the given arguments
// while also not going over the endHeight for a get utreexo ttls message.
func GetUtreexoTTLsExponent(startBlock, endHeight, bestHeight int32) uint8 {
	return getUtreexoExponent(startBlock, endHeight, bestHeight, MaxUtreexoTTLExponent)
}

// CalculateGetUtreexoTTLMsgs returns the required get ttl messages needed to fetch the given
// heights.
//
// NOTE: endHeight cannot be larger than the version. If it is, it'll just use the version as the
// endHeight.
func CalculateGetUtreexoTTLMsgs(version uint32, startHeight, endHeight int32) MsgGetUtreexoTTLs {
	exp := GetUtreexoTTLsExponent(startHeight, endHeight, int32(version)-1)
	msg := MsgGetUtreexoTTLs{
		Version:            version,
		StartHeight:        uint32(startHeight),
		MaxReceiveExponent: exp,
	}

	return msg
}
