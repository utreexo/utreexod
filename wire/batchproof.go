// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"fmt"
	"io"
	"strings"

	"github.com/mit-dci/utreexo/accumulator"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// BatchProofSerializeTargetSize returns how many bytes it would take to serialize all
// the targets in the batch proof.
func BatchProofSerializeTargetSize(bp *accumulator.BatchProof) int {
	size := VarIntSerializeSize(uint64(len(bp.Targets)))
	for _, target := range bp.Targets {
		size += VarIntSerializeSize(target)
	}

	return size
}

// BatchProofAccProofSize returns how many bytes it would take to serialize the
// accumulator proof in the batch proof.
func BatchProofSerializeAccProofSize(bp *accumulator.BatchProof) int {
	size := VarIntSerializeSize(uint64(len(bp.Proof)))
	size += chainhash.HashSize * len(bp.Proof)
	return size
}

// BatchProofSerializeSize returns the number of bytes it would tkae to serialize
// a BatchProof.
func BatchProofSerializeSize(bp *accumulator.BatchProof) int {
	// First the targets.
	size := BatchProofSerializeTargetSize(bp)

	// Then the proofs.
	return size + BatchProofSerializeAccProofSize(bp)
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
	err := WriteVarInt(w, 0, uint64(len(bp.Targets)))
	if err != nil {
		return err
	}

	for _, t := range bp.Targets {
		err = WriteVarInt(w, 0, t)
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

	targets := make([]uint64, targetCount)
	for i := range targets {
		target, err := ReadVarInt(r, 0)
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

// BatchProofToString converts a batchproof into a human-readable string.  Note
// that the hashes are in little endian order.
func BatchProofToString(bp *accumulator.BatchProof) string {
	// First the targets.
	str := "targets:" + strings.Join(strings.Fields(fmt.Sprint(bp.Targets)), ",") + " "

	// Then the proofs.
	str += "proofs: ["
	for i, hash := range bp.Proof {
		str += chainhash.Hash(hash).String()

		if i != len(bp.Proof)-1 {
			str += ","
		}
	}
	str += "]"

	return str
}
