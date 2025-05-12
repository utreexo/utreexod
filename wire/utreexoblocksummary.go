// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// MaxPossibleInputsPerBlock is the maximum possible inputs you can have per block.
// The smallest block you can have is 145 instead of 146 as stated in the stackexchange answer
// but that doesn't change the max possible input value.
//
// https://bitcoin.stackexchange.com/questions/85752/maximum-number-of-inputs-per-transaction
const MaxPossibleInputsPerBlock = 24_386

// MaxUtreexoBlockSummaryPayload the amount of inputs a block can possibly have multiplied by the size
// of a max varint.
const MaxUtreexoBlockSummaryPayload = (MaxPossibleInputsPerBlock * MaxVarIntPayload)

// BlockHash + numadds + length of block targets + MaxUtreexoBlockSummarySize.
const MaxUtreexoBlockSummarySize = chainhash.HashSize + MaxVarIntPayload + MaxVarIntPayload + MaxUtreexoBlockSummaryPayload

// UtreexoBlockSummary implements the Message interface and represents a bitcoin
// utreexo block header message. It's used to provide the positions of the inputs
// that are being spent in the given block.
type UtreexoBlockSummary struct {
	BlockHash    chainhash.Hash
	NumAdds      uint64
	BlockTargets []uint64
}

// SerializeSize returns the number of bytes it would take to serialize the
// utreexo block summary.
func (h *UtreexoBlockSummary) SerializeSize() int {
	n := chainhash.HashSize + VarIntSerializeSize(h.NumAdds) + VarIntSerializeSize(uint64(len(h.BlockTargets)))
	for _, target := range h.BlockTargets {
		n += VarIntSerializeSize(target)
	}

	return n
}

// Deserialize reads a block summary from the reader.
func (h *UtreexoBlockSummary) Deserialize(r io.Reader) error {
	return readUtreexoBlockSummary(r, 0, h)
}

// Serialize writes the block summary to the writer.
func (h *UtreexoBlockSummary) Serialize(w io.Writer) error {
	return writeUtreexoBlockSummary(w, 0, h)
}

// NewUtreexoBlockSummary returns a new UtreexoBlockSummary using the provided arguments.
func NewUtreexoBlockSummary(blockHash chainhash.Hash,
	numAdds uint64, targets []uint64) *UtreexoBlockSummary {

	return &UtreexoBlockSummary{
		BlockHash:    blockHash,
		NumAdds:      numAdds,
		BlockTargets: targets,
	}
}

// readUtreexoBlockSummary reads a bitcoin block header from r.
func readUtreexoBlockSummary(r io.Reader, _ uint32, bh *UtreexoBlockSummary) error {
	_, err := io.ReadFull(r, bh.BlockHash[:])
	if err != nil {
		return err
	}

	bh.NumAdds, err = ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	count, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	bh.BlockTargets = make([]uint64, count)
	for i := range bh.BlockTargets {
		bh.BlockTargets[i], err = ReadVarInt(r, 0)
		if err != nil {
			return err
		}
	}

	return nil
}

// writeUtreexoBlockSummary writes a utreexo block summary to w.
func writeUtreexoBlockSummary(w io.Writer, _ uint32, bh *UtreexoBlockSummary) error {
	_, err := w.Write(bh.BlockHash[:])
	if err != nil {
		return err
	}

	err = WriteVarInt(w, 0, bh.NumAdds)
	if err != nil {
		return err
	}

	err = WriteVarInt(w, 0, uint64(len(bh.BlockTargets)))
	if err != nil {
		return err
	}

	for _, t := range bh.BlockTargets {
		err = WriteVarInt(w, 0, t)
		if err != nil {
			return err
		}
	}

	return nil
}
