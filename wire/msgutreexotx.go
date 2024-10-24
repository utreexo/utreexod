// Copyright (c) 2024 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"
)

// MsgUtreexoTx implements the Message interface and represents a bitcoin utreexo
// tx message. It is used to deliver transaction information in response to a getdata
// message (MsgGetData) for a given transaction with the utreexo proof to verify the
// transaction.
//
// Use the AddTxIn and AddTxOut functions to build up the list of transaction
// inputs and outputs.
type MsgUtreexoTx struct {
	// MsgTx is the underlying Bitcoin transaction message.
	*MsgTx

	// UData is the underlying utreexo data.
	*UData
}

// Copy creates a deep copy of a transaction so that the original does not get
// modified when the copy is manipulated.
func (msg *MsgUtreexoTx) Copy() *MsgUtreexoTx {
	msgTx := msg.MsgTx.Copy()
	newTx := MsgUtreexoTx{
		MsgTx: msgTx,
		UData: msg.UData.Copy(),
	}

	return &newTx
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
// See Deserialize for decoding transactions stored to disk, such as in a
// database, as opposed to decoding transactions from the wire.
func (msg *MsgUtreexoTx) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	// Decode the MsgTx.
	msgTx := new(MsgTx)
	err := msgTx.BtcDecode(r, pver, enc)
	if err != nil {
		return err
	}
	msg.MsgTx = msgTx

	// Decode the utreexo data.
	ud := new(UData)
	err = ud.Deserialize(r)
	if err != nil {
		return err
	}

	return nil
}

// Deserialize decodes a transaction from r into the receiver using a format
// that is suitable for long-term storage such as a database while respecting
// the Version field in the transaction.  This function differs from BtcDecode
// in that BtcDecode decodes from the bitcoin wire protocol as it was sent
// across the network.  The wire encoding can technically differ depending on
// the protocol version and doesn't even really need to match the format of a
// stored transaction at all.  As of the time this comment was written, the
// encoded transaction is the same in both instances, but there is a distinct
// difference and separating the two allows the API to be flexible enough to
// deal with changes.
func (msg *MsgUtreexoTx) Deserialize(r io.Reader) error {
	// At the current time, there is no difference between the wire encoding
	// at protocol version 0 and the stable long-term storage format.  As
	// a result, make use of BtcDecode.
	return msg.BtcDecode(r, 0, WitnessEncoding)
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
// See Serialize for encoding transactions to be stored to disk, such as in a
// database, as opposed to encoding transactions for the wire.
func (msg *MsgUtreexoTx) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	// Encode the msgTx.
	err := msg.MsgTx.BtcEncode(w, pver, enc)
	if err != nil {
		return err
	}

	// Encode the utreexo data.
	return msg.UData.Serialize(w)
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgUtreexoTx) Command() string {
	return CmdUtreexoTx
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (msg *MsgUtreexoTx) MaxPayloadLength(pver uint32) uint32 {
	return MaxBlockPayload
}
