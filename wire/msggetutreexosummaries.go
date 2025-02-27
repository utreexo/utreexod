// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package wire

import (
	"fmt"
	"io"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// AccumulatorRows is the pre-allocated rows that the accumulator has.
const AccumulatorRows = 63

// MsgGetUtreexoSummaries implements the Message interface and represents a bitcoin
// getutreexosummaries message. It's used to request the utreexo summaries from the given
// start hash.
type MsgGetUtreexoSummaries struct {
	StartHash          chainhash.Hash
	MaxReceiveExponent uint8
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	_, err := io.ReadFull(r, msg.StartHash[:])
	if err != nil {
		return err
	}

	msg.MaxReceiveExponent = msg.StartHash[31]
	msg.StartHash[31] = 0

	if msg.MaxReceiveExponent > MaxUtreexoExponent {
		str := fmt.Sprintf("exponent too high in message [max %v, got %v]",
			MaxUtreexoExponent, msg.MaxReceiveExponent)
		return messageError("MsgGetUtreexoSummaries.BtcDecode", str)
	}

	return nil
}

// BtcEncode encodes the receiver to w using the bitcoin protocol encoding.
// This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	if msg.MaxReceiveExponent > MaxUtreexoExponent {
		str := fmt.Sprintf("exponent too high in message [max %v, got %v]",
			MaxUtreexoExponent, msg.MaxReceiveExponent)
		return messageError("MsgGetUtreexoSummaries.BtcEncode", str)
	}

	msg.StartHash[31] = msg.MaxReceiveExponent

	_, err := w.Write(msg.StartHash[:])
	if err != nil {
		return err
	}

	return nil
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) Command() string {
	return CmdGetUtreexoSummaries
}

// MaxPayloadLength returns the maximum length the payload can be for the
// receiver.  This is part of the Message interface implementation.
func (msg *MsgGetUtreexoSummaries) MaxPayloadLength(pver uint32) uint32 {
	return chainhash.HashSize
}

// NewMsgGetUtreexoSummaries returns a new bitcoin getutreexosummaries message that conforms to
// the Message interface.  See MsgGetUtreexoSummaries for details.
func NewMsgGetUtreexoSummaries(blockHash chainhash.Hash, maxReceiveExponent uint8) *MsgGetUtreexoSummaries {
	return &MsgGetUtreexoSummaries{StartHash: blockHash, MaxReceiveExponent: maxReceiveExponent}
}

// GetUtreexoSummaryHeights returns the heights of the blocks that we can serve based on the startBlock
// and the exponent. The returned heights are such that they always minimize the proof size.
func GetUtreexoSummaryHeights(startBlock int32, exponent uint8) ([]int32, error) {
	parentPos, err := utreexo.ParentMany(uint64(startBlock), exponent, AccumulatorRows)
	if err != nil {
		return nil, err
	}
	startPos, err := utreexo.ChildMany(parentPos, exponent, AccumulatorRows)
	if err != nil {
		return nil, err
	}
	count := int32(1 << exponent)

	heights := make([]int32, 0, count)
	for i := int32(0); i < count; i++ {
		height := i + int32(startPos)
		if height < startBlock {
			continue
		}
		heights = append(heights, i+int32(startPos))
	}

	return heights, nil
}
