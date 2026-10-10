// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip183

import (
	"io"

	"github.com/btcsuite/btcd/wire/v2"
	"github.com/utreexo/utreexod/internal/btcdwire"
)

// MaxUtreexoTTLSize is:
// height 4 bytes +
// varint len of ttls +
// (death height&death block index * max outputs per block)
const MaxUtreexoTTLSize = 4 + wire.MaxVarIntPayload + (99_984 * (4 + 4))

// TTLInfo is the ttl of the leaf this represents.
type TTLInfo struct {
	DeathHeight   uint32
	DeathBlkIndex uint32
}

// UtreexoTTL provides information about the time-to-live values of each added leaf to the
// accumulator on a given block height. It's used for ibd optimization for utreexo nodes.
type UtreexoTTL struct {
	BlockHeight uint32
	TTLs        []TTLInfo
}

// SerializeSize returns how many bytes would be required to serialize the utreexo ttl.
func (ut *UtreexoTTL) SerializeSize() int {
	// Size of the BlockHeight and length of TTLs.
	size := 4 + wire.VarIntSerializeSize(uint64(len(ut.TTLs)))

	// Size of DeathHeight & DeathBlkIndex for all the TTLs.
	size += len(ut.TTLs) * (4 + 4)

	return size
}

// Deserialize constructs a utreexo ttl from the given reader.
func (ut *UtreexoTTL) Deserialize(r io.Reader) error {
	buf := btcdwire.ScratchPool.Get().(*[8]byte)
	defer btcdwire.ScratchPool.Put(buf)

	var err error
	ut.BlockHeight, err = btcdwire.ReadUint32(r, buf[:])
	if err != nil {
		return err
	}

	count, err := wire.ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	ut.TTLs = make([]TTLInfo, count)
	for i := range ut.TTLs {
		ut.TTLs[i].DeathHeight, err = btcdwire.ReadUint32(r, buf[:])
		if err != nil {
			return err
		}

		ut.TTLs[i].DeathBlkIndex, err = btcdwire.ReadUint32(r, buf[:])
		if err != nil {
			return err
		}
	}

	return nil
}

// Serialize serializes the utreexo ttl to the writer.
func (ut *UtreexoTTL) Serialize(w io.Writer) error {
	buf := btcdwire.ScratchPool.Get().(*[8]byte)
	defer btcdwire.ScratchPool.Put(buf)

	err := btcdwire.WriteUint32(w, buf[:], ut.BlockHeight)
	if err != nil {
		return err
	}

	err = wire.WriteVarInt(w, 0, uint64(len(ut.TTLs)))
	if err != nil {
		return err
	}

	for _, ttl := range ut.TTLs {
		err = btcdwire.WriteUint32(w, buf[:], ttl.DeathHeight)
		if err != nil {
			return err
		}

		err = btcdwire.WriteUint32(w, buf[:], ttl.DeathBlkIndex)
		if err != nil {
			return err
		}
	}

	return nil
}
