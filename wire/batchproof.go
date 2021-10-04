// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/mit-dci/utreexo/accumulator"
)

// BatchProofSerializeSize returns the number of bytes it would tkae to serialize
// a BatchProof.
func BatchProofSerializeSize(bp *accumulator.BatchProof) int {
	size := VarIntSerializeSize(uint64(len(bp.Targets)))
	size += VarIntSerializeSize(uint64(len(bp.Proof)))
	size += chainhash.HashSize * len(bp.Proof)
	return size + (8 * len(bp.Targets))
}

// -----------------------------------------------------------------------------
// BatchProof serialization defines how the utreexo accumulator proof will be
// serialized both for i/o.
//
// Note that this serialization format differs from the one from
// github.com/mit-dci/utreexo/accumulator as this serialization method uses
// varints and the one in that package does not.  They are not compatible and
// should not be used together.  The serialization method here is more compact
// and thus is better for wire and disk storage.
//
// The serialized format is:
// [<target count><targets><proof count><proofs>]
//
// All together, the serialization looks like so:
// Field          Type       Size
// target count   varint     1-8 bytes
// targets        []uint64   variable
// hash count     varint     1-8 bytes
// hashes         []32 byte  variable
//
// -----------------------------------------------------------------------------

// BatchProofSerialize encodes the BatchProof to w using the BatchProof
// serialization format.
func BatchProofSerialize(w io.Writer, bp *accumulator.BatchProof) error {
	bs := newSerializer()
	defer bs.free()

	err := WriteVarInt(w, 0, uint64(len(bp.Targets)))
	if err != nil {
		return err
	}

	for _, t := range bp.Targets {
		err = bs.PutUint64(w, littleEndian, t)
		if err != nil {
			return err
		}
	}

	err = WriteVarInt(w, 0, uint64(len(bp.Proof)))
	if err != nil {
		return err
	}

	// then the rest is just hashes
	for _, h := range bp.Proof {
		_, err = w.Write(h[:])
		if err != nil {
			return err
		}
	}

	return nil
}

// BatchProofSerialize decodes the BatchProof to r using the BatchProof
// serialization format.
func BatchProofDeserialize(r io.Reader) (*accumulator.BatchProof, error) {
	targetCount, err := ReadVarInt(r, 0)
	if err != nil {
		return nil, err
	}

	bs := newSerializer()
	defer bs.free()

	targets := make([]uint64, targetCount)
	for i := range targets {
		target, err := bs.Uint64(r, littleEndian)
		if err != nil {
			return nil, err
		}

		targets[i] = target
	}

	proofCount, err := ReadVarInt(r, 0)
	if err != nil {
		return nil, err
	}

	proofs := make([]accumulator.Hash, proofCount)
	for i := range proofs {
		_, err = io.ReadFull(r, proofs[i][:])
		if err != nil {
			return nil, err
		}
	}

	return &accumulator.BatchProof{Targets: targets, Proof: proofs}, nil
}
