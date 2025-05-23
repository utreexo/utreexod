// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import "io"

// MaxUtreexoTTLSize is height 4 bytes, + varint len of ttls + varint size * max outputs per block.
const MaxUtreexoTTLSize = 4 + MaxVarIntPayload + (99_984 * MaxVarIntPayload)

// UtreexoTTL provides information about the time-to-live values of each added leaf to the
// accumulator on a given block height. It's used for ibd optimization for utreexo nodes.
type UtreexoTTL struct {
	BlockHeight uint32
	TTLs        []uint64
}

// SerializeSize returns how many bytes would be required to serialize the utreexo ttl.
func (ut *UtreexoTTL) SerializeSize() int {
	size := 4 + VarIntSerializeSize(uint64(len(ut.TTLs)))
	for _, ttl := range ut.TTLs {
		size += VarIntSerializeSize(ttl)
	}

	return size
}

// Deserialize constructs a utreexo ttl from the given reader.
func (ut *UtreexoTTL) Deserialize(r io.Reader) error {
	bs := newSerializer()
	defer bs.free()

	var err error
	ut.BlockHeight, err = bs.Uint32(r, littleEndian)
	if err != nil {
		return err
	}

	count, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	ut.TTLs = make([]uint64, count)
	for i := range ut.TTLs {
		ut.TTLs[i], err = ReadVarInt(r, 0)
		if err != nil {
			return err
		}
	}

	return nil
}

// Serialize serializes the utreexo ttl to the writer.
func (ut *UtreexoTTL) Serialize(w io.Writer) error {
	bs := newSerializer()
	defer bs.free()

	err := bs.PutUint32(w, littleEndian, ut.BlockHeight)
	if err != nil {
		return err
	}

	err = WriteVarInt(w, 0, uint64(len(ut.TTLs)))
	if err != nil {
		return err
	}

	for _, ttl := range ut.TTLs {
		err = WriteVarInt(w, 0, ttl)
		if err != nil {
			return err
		}
	}

	return nil
}
