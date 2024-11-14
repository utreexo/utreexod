// Copyright (c) 2024 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"

	"github.com/utreexo/utreexo"
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
	MsgTx

	// AccProof is the utreexo accumulator proof for all the inputs.
	AccProof utreexo.Proof

	// LeafDatas are the tx validation data for every input.
	LeafDatas []LeafData
}

// Copy creates a deep copy of a transaction so that the original does not get
// modified when the copy is manipulated.
func (msg *MsgUtreexoTx) Copy() *MsgUtreexoTx {
	msgTx := msg.MsgTx.Copy()

	// Copy proof
	proofCopy := utreexo.Proof{
		Targets: make([]uint64, len(msg.AccProof.Targets)),
		Proof:   make([]utreexo.Hash, len(msg.AccProof.Proof)),
	}
	copy(proofCopy.Targets, msg.AccProof.Targets)
	copy(proofCopy.Proof, msg.AccProof.Proof)

	// Copy leaf datas.
	LeafDatas := make([]LeafData, len(msg.LeafDatas))
	for i := range LeafDatas {
		LeafDatas[i] = *msg.LeafDatas[i].Copy()
	}

	newTx := MsgUtreexoTx{
		MsgTx:     *msgTx,
		AccProof:  proofCopy,
		LeafDatas: LeafDatas,
	}

	return &newTx
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
// See Deserialize for decoding transactions stored to disk, such as in a
// database, as opposed to decoding transactions from the wire.
func (msg *MsgUtreexoTx) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	// Decode the batchproof.
	proof, err := BatchProofDeserialize(r)
	if err != nil {
		return err
	}
	msg.AccProof = *proof

	// Decode the MsgTx.
	var msgTx MsgTx
	err = msgTx.BtcDecode(r, pver, enc)
	if err != nil {
		return err
	}
	msg.MsgTx = msgTx

	// Return early if it's 0. Makes sure msg.LeafDatas is nil which is important for
	// reflect.DeepEqual in the tests.
	if len(msg.MsgTx.TxIn) == 0 {
		return nil
	}

	msg.LeafDatas = make([]LeafData, len(msg.MsgTx.TxIn))
	for i, txIn := range msgTx.TxIn {
		isUnconfirmed := txIn.PreviousOutPoint.Index&1 == 1
		txIn.PreviousOutPoint.Index >>= 1

		if isUnconfirmed {
			continue
		}

		err = msg.LeafDatas[i].DeserializeCompact(r)
		if err != nil {
			return err
		}
	}

	return nil
}

// Deserialize decodes a transaction from r into the receiver using a format
// that is suitable for long-term storage such as a database while respecting
// the Version field in the transaction.  This function differs from BtcDecode
// in that BtcDecode decodes from the bitcoin wire protocol as it was sent across the network.  The wire encoding can technically differ depending on
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
	// Write batch proof.
	err := BatchProofSerialize(w, &msg.AccProof)
	if err != nil {
		return err
	}

	// Go through the TxIns and mark the ones that are not confirmed with a 1 in the LSB.
	for i := range msg.TxIn {
		msg.TxIn[i].PreviousOutPoint.Index <<= 1
		if msg.LeafDatas[i].Equal(emptyLd) {
			msg.TxIn[i].PreviousOutPoint.Index |= 1
		}
	}

	// Encode the msgTx.
	err = msg.MsgTx.BtcEncode(w, pver, enc)
	if err != nil {
		return err
	}

	// Write the actual leaf datas.
	for _, ld := range msg.LeafDatas {
		err = ld.SerializeCompact(w)
		if err != nil {
			return err
		}
	}

	return nil
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

// NewMsgUtreexoTx returns a new bitcoin utreexotx message that conforms to the
// Message interface. The return instance has a default tx message and the udata
// is initialized to the default values.
func NewMsgUtreexoTx(version int32) *MsgUtreexoTx {
	return &MsgUtreexoTx{
		MsgTx: *NewMsgTx(1),
	}
}
