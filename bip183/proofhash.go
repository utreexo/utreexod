// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package bip183 implements the utreexo peer services encodings defined in
// BIP-183.
package bip183

import (
	"encoding/binary"
	"math"

	"github.com/btcsuite/btcd/chainhash/v2"
)

// Uint64sToPackedHashes packs the passed in uint64s into the 32 byte hashes. 4 uint64s are packed into
// each 32 byte hash and if there's leftovers, it's filled with maxuint64.
func Uint64sToPackedHashes(ints []uint64) []chainhash.Hash {
	// 4 uint64s fit into a 32 byte slice. For len(ints) < 4, count is 0.
	count := len(ints) / 4

	// If there's leftovers, we need to allocate 1 more.
	if len(ints)%4 != 0 {
		count++
	}

	hashes := make([]chainhash.Hash, count)
	hashIdx := 0
	for i := range ints {
		// Move on to the next hash after putting in 4 uint64s into a hash.
		if i != 0 && i%4 == 0 {
			hashIdx++
		}

		// 8 is the size of a uint64.
		start := (i % 4) * 8
		binary.LittleEndian.PutUint64(hashes[hashIdx][start:start+8], ints[i])
	}

	// Pad the last hash with math.MaxUint64 if needed. We check this by seeing
	// if modulo 4 doesn't equate 0.
	if len(ints)%4 != 0 {
		// Start at the end.
		end := chainhash.HashSize

		// Get the count of how many empty uint64 places we should pad.
		padAmount := 4 - len(ints)%4
		for i := 0; i < padAmount; i++ {
			// 8 is the size of a uint64.
			binary.LittleEndian.PutUint64(hashes[len(hashes)-1][end-8:end], math.MaxUint64)
			end -= 8
		}
	}

	return hashes
}

// PackedHashesToUint64 returns the uint64s in the packed hashes as a slice of uint64s.
func PackedHashesToUint64(hashes []chainhash.Hash) []uint64 {
	ints := make([]uint64, 0, len(hashes)*4)
	for i := range hashes {
		// We pack 4 ints per hash.
		for j := 0; j < 4; j++ {
			// Offset for each int should be calculated by multiplying by
			// the size of a uint64.
			start := j * 8
			read := binary.LittleEndian.Uint64(hashes[i][start : start+8])

			// If we reach padded values, break.
			if read == math.MaxUint64 {
				break
			}

			// Otherwise we append the read uint64 to the slice.
			ints = append(ints, read)
		}
	}

	return ints
}
