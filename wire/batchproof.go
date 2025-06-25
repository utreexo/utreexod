// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"fmt"
	"io"
	"strings"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// BatchProofSerializeTargetSize returns how many bytes it would take to serialize all
// the targets in the batch proof.
func BatchProofSerializeTargetSize(bp *utreexo.Proof) int {
	size := VarIntSerializeSize(uint64(len(bp.Targets)))
	for _, target := range bp.Targets {
		size += VarIntSerializeSize(target)
	}

	return size
}

// BatchProofAccProofSize returns how many bytes it would take to serialize the
// accumulator proof in the batch proof.
func BatchProofSerializeAccProofSize(bp *utreexo.Proof) int {
	size := VarIntSerializeSize(uint64(len(bp.Proof)))
	size += chainhash.HashSize * len(bp.Proof)
	return size
}

// BatchProofSerializeSize returns the number of bytes it would tkae to serialize
// a BatchProof.
func BatchProofSerializeSize(bp *utreexo.Proof) int {
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

// ProofTargetsSerialize serializes the targets to w.
func ProofTargetsSerialize(w io.Writer, targets []uint64) error {
	err := WriteVarInt(w, 0, uint64(len(targets)))
	if err != nil {
		return err
	}

	for _, t := range targets {
		err = WriteVarInt(w, 0, t)
		if err != nil {
			return err
		}
	}

	return nil
}

// ProofHashesSerialize serializes the hashes to w.
func ProofHashesSerialize(w io.Writer, hashes []utreexo.Hash) error {
	err := WriteVarInt(w, 0, uint64(len(hashes)))
	if err != nil {
		return err
	}

	// then the rest is just hashes
	for _, h := range hashes {
		_, err = w.Write(h[:])
		if err != nil {
			return err
		}
	}

	return nil
}

// BatchProofSerialize encodes the BatchProof to w using the BatchProof
// serialization format.
func BatchProofSerialize(w io.Writer, bp *utreexo.Proof) error {
	err := ProofTargetsSerialize(w, bp.Targets)
	if err != nil {
		return err
	}

	return ProofHashesSerialize(w, bp.Proof)
}

// ProofTargetsDeserialize deserializes r to targets using the BatchProof
// serialization format.
func ProofTargetsDeserialize(r io.Reader) ([]uint64, error) {
	targetCount, err := ReadVarInt(r, 0)
	if err != nil {
		return nil, err
	}
	if targetCount == 0 {
		return nil, nil
	}

	targets := make([]uint64, 0, targetCount)
	for i := 0; i < int(targetCount); i++ {
		target, err := ReadVarInt(r, 0)
		if err != nil {
			return nil, err
		}

		targets = append(targets, target)
	}

	return targets, nil
}

// ProofHashesDeserialize deserializes r to proof hashes using the BatchProof
// serialization format.
func ProofHashesDeserialize(r io.Reader) ([]utreexo.Hash, error) {
	proofCount, err := ReadVarInt(r, 0)
	if err != nil {
		return nil, err
	}
	if proofCount == 0 {
		return nil, nil
	}

	proofs := make([]utreexo.Hash, 0, proofCount)
	for i := 0; i < int(proofCount); i++ {
		var hash utreexo.Hash
		_, err = io.ReadFull(r, hash[:])
		if err != nil {
			return nil, err
		}
		proofs = append(proofs, hash)
	}

	return proofs, nil
}

// BatchProofDeserialize decodes the BatchProof to r using the BatchProof
// serialization format.
func BatchProofDeserialize(r io.Reader) (*utreexo.Proof, error) {
	proof := new(utreexo.Proof)

	var err error
	proof.Targets, err = ProofTargetsDeserialize(r)
	if err != nil {
		return nil, err
	}

	proof.Proof, err = ProofHashesDeserialize(r)
	if err != nil {
		return nil, err
	}

	return proof, nil
}

// BatchProofToString converts a batchproof into a human-readable string.  Note
// that the hashes are in little endian order.
func BatchProofToString(bp *utreexo.Proof) string {
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
