// Copyright (c) 2013-2016 The btcsuite developers
// Copyright (c) 2015 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chainhash

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
)

// HashSize of array used to store hashes.  See Hash.
const HashSize = 32

// MaxHashStringSize is the maximum length of a Hash hash string.
const MaxHashStringSize = HashSize * 2

// ErrHashStrSize describes an error that indicates the caller specified a hash
// string that has too many characters.
var ErrHashStrSize = fmt.Errorf("max hash string length is %v bytes", MaxHashStringSize)

// Hash is used in several of the bitcoin messages and common structures.  It
// typically represents the double sha256 of data.
type Hash [HashSize]byte

// String returns the Hash as the hexadecimal string of the byte-reversed
// hash.
func (hash Hash) String() string {
	for i := 0; i < HashSize/2; i++ {
		hash[i], hash[HashSize-1-i] = hash[HashSize-1-i], hash[i]
	}
	return hex.EncodeToString(hash[:])
}

// CloneBytes returns a copy of the bytes which represent the hash as a byte
// slice.
//
// NOTE: It is generally cheaper to just slice the hash directly thereby reusing
// the same bytes rather than calling this method.
func (hash *Hash) CloneBytes() []byte {
	newHash := make([]byte, HashSize)
	copy(newHash, hash[:])

	return newHash
}

// SetBytes sets the bytes which represent the hash.  An error is returned if
// the number of bytes passed in is not HashSize.
func (hash *Hash) SetBytes(newHash []byte) error {
	nhlen := len(newHash)
	if nhlen != HashSize {
		return fmt.Errorf("invalid hash length of %v, want %v", nhlen,
			HashSize)
	}
	copy(hash[:], newHash)

	return nil
}

// IsEqual returns true if target is the same as hash.
func (hash *Hash) IsEqual(target *Hash) bool {
	if hash == nil && target == nil {
		return true
	}
	if hash == nil || target == nil {
		return false
	}
	return *hash == *target
}

// MarshalJSON serialises the hash as a JSON appropriate string value.
func (hash Hash) MarshalJSON() ([]byte, error) {
	return json.Marshal(hash.String())
}

// UnmarshalJSON parses the hash with JSON appropriate string value.
func (hash *Hash) UnmarshalJSON(input []byte) error {
	var sh string
	err := json.Unmarshal(input, &sh)
	if err != nil {
		return err
	}
	newHash, err := NewHashFromStr(sh)
	if err != nil {
		return err
	}

	return hash.SetBytes(newHash[:])
}

// NewHash returns a new Hash from a byte slice.  An error is returned if
// the number of bytes passed in is not HashSize.
func NewHash(newHash []byte) (*Hash, error) {
	var sh Hash
	err := sh.SetBytes(newHash)
	if err != nil {
		return nil, err
	}
	return &sh, err
}

// NewHashFromStr creates a Hash from a hash string.  The string should be
// the hexadecimal string of a byte-reversed hash, but any missing characters
// result in zero padding at the end of the Hash.
func NewHashFromStr(hash string) (*Hash, error) {
	ret := new(Hash)
	err := Decode(ret, hash)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// Decode decodes the byte-reversed hexadecimal string encoding of a Hash to a
// destination.
func Decode(dst *Hash, src string) error {
	// Return error if hash string is too long.
	if len(src) > MaxHashStringSize {
		return ErrHashStrSize
	}

	// Hex decoder expects the hash to be a multiple of two.  When not, pad
	// with a leading zero.
	var srcBytes []byte
	if len(src)%2 == 0 {
		srcBytes = []byte(src)
	} else {
		srcBytes = make([]byte, 1+len(src))
		srcBytes[0] = '0'
		copy(srcBytes[1:], src)
	}

	// Hex decode the source bytes to a temporary destination.
	var reversedHash Hash
	_, err := hex.Decode(reversedHash[HashSize-hex.DecodedLen(len(srcBytes)):], srcBytes)
	if err != nil {
		return err
	}

	// Reverse copy from the temporary hash to destination.  Because the
	// temporary was zeroed, the written result will be correctly padded.
	for i, b := range reversedHash[:HashSize/2] {
		dst[i], dst[HashSize-1-i] = reversedHash[HashSize-1-i], b
	}

	return nil
}

// Uint64sToPackedHashes packs the passed in uint64s into the 32 byte hashes. 4 uint64s are packed into
// each 32 byte hash and if there's leftovers, it's filled with maxuint64.
func Uint64sToPackedHashes(ints []uint64) []Hash {
	// 4 uint64s fit into a 32 byte slice. For len(ints) < 4, count is 0.
	count := len(ints) / 4

	// If there's leftovers, we need to allocate 1 more.
	if len(ints)%4 != 0 {
		count++
	}

	hashes := make([]Hash, count)
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
		end := HashSize

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
func PackedHashesToUint64(hashes []Hash) []uint64 {
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
