// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"encoding/hex"
	"fmt"
	"io"

	"github.com/mit-dci/utreexo/accumulator"
)

// UData contains data needed to prove the existence and validity of all inputs
// for a Bitcoin block.  With this data, a full node may only keep the utreexo
// roots and still be able to fully validate a block.
type UData struct {
	// Height is the height of the block this UData corresponds to.
	Height int32

	// AccProof is the utreexo accumulator proof for all the inputs.
	AccProof accumulator.BatchProof

	// LeafDatas are the tx validation data for every input.
	LeafDatas []LeafData

	// TxoTTLs are the time to live values for all the stxos.
	TxoTTLs []int32
}

// StxosHashes returns the hash of all stxos in this UData.  The hashes returned
// here represent the hash commitments of the stxos.
func (ud *UData) StxoHashes() []accumulator.Hash {
	leafHashes := make([]accumulator.Hash, len(ud.LeafDatas))
	for i, stxo := range ud.LeafDatas {
		leafHashes[i] = stxo.LeafHash()
	}

	return leafHashes
}

// SerializeSize returns the number of bytes it would take to serialize the
// UData.
func (ud *UData) SerializeSize() int {
	// Size of all the leafData.
	var ldSize int
	for _, l := range ud.LeafDatas {
		ldSize += l.SerializeSize()
	}

	// Size of all the time to live values.
	var txoTTLSize int
	for _, ttl := range ud.TxoTTLs {
		txoTTLSize += VarIntSerializeSize(uint64(ttl))
	}

	// Add on accumulator proof size and the varint serialized height size.
	return txoTTLSize + ldSize + ud.AccProof.SerializeSize() +
		VarIntSerializeSize(uint64(ud.Height))
}

// -----------------------------------------------------------------------------
// UData serialization includes all the data that is needed for a utreexo node to
// verify a block or a tx with only the utreexo roots.
//
// The serialized format is:
// [<block height><accumulator proof><leaf datas><txo time-to-live values>]
//
// Accumulator proof serialization can be found in github.com/mit-dci/utreexo.
//
// LeafData serialization can be found in wire/leaf.go.
//
// All together, the serialization looks like so:
//
// Field                    Type       Size
// block height             int32      4
// accumulator proof        []byte     variable
// leaf datas               []byte     variable
// txo time-to-live values  []int32    variable
//
// -----------------------------------------------------------------------------

// Serialize encodes the UData to w using the UData serialization format.
func (ud *UData) Serialize(w io.Writer) error {
	bs := newSerializer()
	defer bs.free()

	err := bs.PutUint32(w, littleEndian, uint32(ud.Height))
	if err != nil {
		return err
	}

	err = WriteVarInt(w, 0, uint64(len(ud.TxoTTLs)))
	if err != nil {
		return err
	}

	for _, ttlval := range ud.TxoTTLs {
		err = bs.PutUint32(w, littleEndian, uint32(ttlval))
		if err != nil {
			return err
		}
	}

	err = ud.AccProof.Serialize(w)
	if err != nil {
		returnErr := messageError("Serialize", err.Error())
		return returnErr
	}

	// Write all the leafDatas.
	for _, ld := range ud.LeafDatas {
		err = ld.Serialize(w)
		if err != nil {
			return err
		}
	}

	return nil
}

// Deserialize encodes the UData to w using the UData serialization format.
func (ud *UData) Deserialize(r io.Reader) error {
	bs := newSerializer()
	defer bs.free()

	height, err := bs.Uint32(r, littleEndian)
	if err != nil {
		returnErr := messageError("Deserialize height", err.Error())
		return returnErr
	}
	ud.Height = int32(height)

	ttlCount, err := ReadVarInt(r, 0)
	if err != nil {
		returnErr := messageError("Deserialize ttlCount", err.Error())
		return returnErr
	}

	ud.TxoTTLs = make([]int32, ttlCount)
	for i := range ud.TxoTTLs {
		ttl, err := bs.Uint32(r, littleEndian)
		if err != nil {
			returnErr := messageError("Deserialize ttl", err.Error())
			return returnErr
		}

		ud.TxoTTLs[i] = int32(ttl)
	}

	err = ud.AccProof.Deserialize(r)
	if err != nil {
		returnErr := messageError("Deserialize AccProof", err.Error())
		return returnErr
	}

	// we've already gotten targets. 1 leafdata per target
	ud.LeafDatas = make([]LeafData, len(ud.AccProof.Targets))
	for i := range ud.LeafDatas {
		err = ud.LeafDatas[i].Deserialize(r)
		if err != nil {
			str := fmt.Sprintf("Height:%d, ttlCount:%d, targetCount:%d, Stxos[%d], err:%s\n",
				ud.Height, ttlCount, len(ud.AccProof.Targets), i, err.Error())
			returnErr := messageError("Deserialize stxos", str)
			return returnErr
		}
	}

	return nil
}

// SerializeSizeCompact returns the number of bytes it would take to serialize the
// UData using the compact UData serialization format.
func (ud *UData) SerializeSizeCompact() int {
	// Size of all the leafData.
	var ldSize int
	for _, l := range ud.LeafDatas {
		ldSize += l.SerializeSizeCompact()
	}

	// Size of all the time to live values.
	txoTTLSize := len(ud.TxoTTLs) * 4

	// Add on accumulator proof size and the height size.
	return txoTTLSize + ldSize + ud.AccProof.SerializeSize() + 4
}

// SerializeCompact encodes the UData to w using the compact UData
// serialization format.  It follows the normal UData serialization format with
// the exception that compact leaf data serialization is used.  Everything else
// remains the same.
func (ud *UData) SerializeCompact(w io.Writer) error {
	bs := newSerializer()
	defer bs.free()

	err := bs.PutUint32(w, littleEndian, uint32(ud.Height))
	if err != nil {
		return err
	}

	err = WriteVarInt(w, 0, uint64(len(ud.TxoTTLs)))
	if err != nil {
		return err
	}
	for _, ttlval := range ud.TxoTTLs {
		err = bs.PutUint32(w, littleEndian, uint32(ttlval))
		if err != nil {
			return err
		}
	}

	err = ud.AccProof.Serialize(w)
	if err != nil {
		returnErr := messageError("SerializeCompact", err.Error())
		return returnErr
	}

	// Write all the leafDatas.
	for _, ld := range ud.LeafDatas {
		err = ld.SerializeCompact(w)
		if err != nil {
			return err
		}
	}

	return nil
}

// DeserializeCompact decodes the UData from r using the compact UData
// serialization format.
func (ud *UData) DeserializeCompact(r io.Reader) error {
	bs := newSerializer()
	defer bs.free()

	height, err := bs.Uint32(r, littleEndian)
	if err != nil {
		returnErr := messageError("DeserializeCompact height", err.Error())
		return returnErr
	}
	ud.Height = int32(height)

	ttlCount, err := ReadVarInt(r, 0)
	if err != nil {
		returnErr := messageError("DeserializeCompact ttlCount", err.Error())
		return returnErr
	}

	ud.TxoTTLs = make([]int32, ttlCount)
	for i := range ud.TxoTTLs {
		ttl, err := bs.Uint32(r, littleEndian)
		if err != nil {
			returnErr := messageError("DeserializeCompact ttl", err.Error())
			return returnErr
		}

		ud.TxoTTLs[i] = int32(ttl)
	}

	err = ud.AccProof.Deserialize(r)
	if err != nil {
		returnErr := messageError("DeserializeCompact", err.Error())
		return returnErr
	}

	// we've already gotten targets. 1 leafdata per target
	ud.LeafDatas = make([]LeafData, len(ud.AccProof.Targets))
	for i := range ud.LeafDatas {
		err = ud.LeafDatas[i].DeserializeCompact(r)
		if err != nil {
			str := fmt.Sprintf("Height:%d, ttlCount:%d, targetCount:%d, Stxos[%d], err:%s\n",
				ud.Height, ttlCount, len(ud.AccProof.Targets), i, err.Error())
			returnErr := messageError("Deserialize stxos", str)
			return returnErr
		}
	}

	return nil
}

// GenerateUData creates a block proof, calling forest.ProveBatch with the leaf indexes
// to get a batched inclusion proof from the accumulator. It then adds on the leaf data,
// to create a block proof which both proves inclusion and gives all utxo data
// needed for transaction verification.
func GenerateUData(txIns []LeafData, forest *accumulator.Forest, blockHeight int32) (
	*UData, error) {

	ud := new(UData)
	ud.Height = blockHeight
	ud.LeafDatas = txIns

	// make slice of hashes from leafdata
	delHashes := make([]accumulator.Hash, len(ud.LeafDatas))
	for i := range ud.LeafDatas {
		delHashes[i] = ud.LeafDatas[i].LeafHash()
	}

	// Generate the utreexo accumulator proof for all the inputs.
	var err error
	ud.AccProof, err = forest.ProveBatch(delHashes)
	if err != nil {
		// Find out which exact one is causing the error.
		for i, delHash := range delHashes {
			_, err = forest.ProveBatch([]accumulator.Hash{delHash})
			if err != nil {
				ld := ud.LeafDatas[i]
				return nil,
					fmt.Errorf("LeafData couldn't be proven. "+
						"BlockHash %s, Outpoint %s, height %v, "+
						"IsCoinbase %v, Amount %v, PkScript %s",
						ld.BlockHash.String(), ld.OutPoint.String(),
						ld.Height, ld.IsCoinBase, ld.Amount,
						hex.EncodeToString(ld.PkScript))
			}
		}
		return nil, err
	}

	if len(ud.AccProof.Targets) != len(txIns) {
		str := fmt.Sprintf("GenerateUData has %d txIns but has proofs for %d txIns",
			len(txIns), len(ud.AccProof.Targets))
		return nil, messageError("GenerateUData", str)
	}

	return ud, nil
}
