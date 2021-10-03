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
	return txoTTLSize + ldSize + BatchProofSerializeSize(&ud.AccProof)
}

// -----------------------------------------------------------------------------
// UData serialization includes all the data that is needed for a utreexo node to
// verify a block or a tx with only the utreexo roots.
//
// The serialized format is:
// [<accumulator proof><leaf datas><txo time-to-live values>]
//
// Accumulator proof serialization follows the batchproof serialization found
// in wire/batchproof.go.
//
// LeafData serialization can be found in wire/leaf.go.
//
// All together, the serialization looks like so:
//
// Field                    Type       Size
// txo time-to-live values  []int32    variable
// accumulator proof        []byte     variable
// leaf datas               []byte     variable
//
// -----------------------------------------------------------------------------

// Serialize encodes the UData to w using the UData serialization format.
func (ud *UData) Serialize(w io.Writer) error {
	err := WriteVarInt(w, 0, uint64(len(ud.TxoTTLs)))
	if err != nil {
		return err
	}

	bs := newSerializer()
	defer bs.free()

	for _, ttlval := range ud.TxoTTLs {
		err = bs.PutUint32(w, littleEndian, uint32(ttlval))
		if err != nil {
			return err
		}
	}

	err = BatchProofSerialize(w, &ud.AccProof)
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
	ttlCount, err := ReadVarInt(r, 0)
	if err != nil {
		returnErr := messageError("Deserialize ttlCount", err.Error())
		return returnErr
	}

	bs := newSerializer()
	defer bs.free()

	ud.TxoTTLs = make([]int32, ttlCount)
	for i := range ud.TxoTTLs {
		ttl, err := bs.Uint32(r, littleEndian)
		if err != nil {
			returnErr := messageError("Deserialize ttl", err.Error())
			return returnErr
		}

		ud.TxoTTLs[i] = int32(ttl)
	}

	proof, err := BatchProofDeserialize(r)
	if err != nil {
		returnErr := messageError("Deserialize AccProof", err.Error())
		return returnErr
	}
	ud.AccProof = *proof

	// we've already gotten targets. 1 leafdata per target
	ud.LeafDatas = make([]LeafData, len(ud.AccProof.Targets))
	for i := range ud.LeafDatas {
		err = ud.LeafDatas[i].Deserialize(r)
		if err != nil {
			str := fmt.Sprintf("ttlCount:%d, targetCount:%d, Stxos[%d], err:%s\n",
				ttlCount, len(ud.AccProof.Targets), i, err.Error())
			returnErr := messageError("Deserialize stxos", str)
			return returnErr
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// UData compact serialization includes only the data that is missing for a
// utreexo node to verify a block or a tx with only the utreexo roots.  The
// compact serialization leaves out data that is able to be fetched locally
// by a node.
//
// The serialized format is:
// [<accumulator proof><leaf datas><txo time-to-live values>]
//
// Accumulator proof serialization follows the batchproof serialization found
// in wire/batchproof.go.
//
// Compact LeafData serialization can be found in wire/leaf.go.
//
// All together, the serialization looks like so:
//
// Field                    Type       Size
// txo time-to-live values  []int32    variable
// accumulator proof        []byte     variable
// leaf datas               []byte     variable
//
// -----------------------------------------------------------------------------

// SerializeSizeCompact returns the number of bytes it would take to serialize the
// UData using the compact UData serialization format.
func (ud *UData) SerializeSizeCompact(isForTx bool) int {
	// Size of all the leafData.
	var ldSize int
	for _, l := range ud.LeafDatas {
		ldSize += l.SerializeSizeCompact(isForTx)
	}

	// Size of all the time to live values.
	txoTTLSize := len(ud.TxoTTLs) * 4

	// Add on accumulator proof size and the height size.
	return txoTTLSize + ldSize + BatchProofSerializeSize(&ud.AccProof)
}

// SerializeCompact encodes the UData to w using the compact UData
// serialization format.  It follows the normal UData serialization format with
// the exception that compact leaf data serialization is used.  Everything else
// remains the same.
func (ud *UData) SerializeCompact(w io.Writer, isForTx bool) error {
	err := WriteVarInt(w, 0, uint64(len(ud.TxoTTLs)))
	if err != nil {
		return err
	}

	bs := newSerializer()
	defer bs.free()

	for _, ttlval := range ud.TxoTTLs {
		err = bs.PutUint32(w, littleEndian, uint32(ttlval))
		if err != nil {
			return err
		}
	}

	err = BatchProofSerialize(w, &ud.AccProof)
	if err != nil {
		returnErr := messageError("SerializeCompact", err.Error())
		return returnErr
	}

	// Write all the leafDatas.
	for _, ld := range ud.LeafDatas {
		err = ld.SerializeCompact(w, isForTx)
		if err != nil {
			return err
		}
	}

	return nil
}

// DeserializeCompact decodes the UData from r using the compact UData
// serialization format.
//
// NOTE if deserializing for a transaction, a non zero txInCount MUST be passed
// in as a correct txCount is critical for deserializing correctly.  When
// deserializing a block, txInCount does not matter.
func (ud *UData) DeserializeCompact(r io.Reader, isForTx bool, txInCount int) error {
	ttlCount, err := ReadVarInt(r, 0)
	if err != nil {
		returnErr := messageError("DeserializeCompact ttlCount", err.Error())
		return returnErr
	}

	bs := newSerializer()
	defer bs.free()

	ud.TxoTTLs = make([]int32, ttlCount)
	for i := range ud.TxoTTLs {
		ttl, err := bs.Uint32(r, littleEndian)
		if err != nil {
			returnErr := messageError("DeserializeCompact ttl", err.Error())
			return returnErr
		}

		ud.TxoTTLs[i] = int32(ttl)
	}

	proof, err := BatchProofDeserialize(r)
	if err != nil {
		returnErr := messageError("DeserializeCompact", err.Error())
		return returnErr
	}
	ud.AccProof = *proof

	// NOTE there may be more leafDatas vs targets for txs as unconfirmed
	// txs will be included as leaf datas but not as targets.  For blocks
	// the leaf data count will match the target count.
	if isForTx {
		ud.LeafDatas = make([]LeafData, txInCount)
	} else {
		ud.LeafDatas = make([]LeafData, len(ud.AccProof.Targets))
	}
	for i := range ud.LeafDatas {
		err = ud.LeafDatas[i].DeserializeCompact(r, isForTx)
		if err != nil {
			str := fmt.Sprintf("ttlCount:%d, targetCount:%d, LeafDatas[%d], err:%s\n",
				ttlCount, len(ud.AccProof.Targets), i, err.Error())
			returnErr := messageError("Deserialize leaf datas", str)
			return returnErr
		}
	}

	return nil
}

// GenerateUData creates a block proof, calling forest.ProveBatch with the leaf indexes
// to get a batched inclusion proof from the accumulator. It then adds on the leaf data,
// to create a block proof which both proves inclusion and gives all utxo data
// needed for transaction verification.
func GenerateUData(txIns []LeafData, forest *accumulator.Forest) (
	*UData, error) {

	ud := new(UData)
	ud.LeafDatas = txIns

	// make slice of hashes from leafdata
	var unconfirmedCount int
	delHashes := make([]accumulator.Hash, 0, len(ud.LeafDatas))
	for _, ld := range ud.LeafDatas {
		if ld.IsUnconfirmed() {
			unconfirmedCount++
			continue
		}
		delHashes = append(delHashes, ld.LeafHash())
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

	if len(ud.AccProof.Targets) != len(txIns)-unconfirmedCount {
		str := fmt.Sprintf("GenerateUData has %d txIns that need to be proven but has proofs for %d txIns",
			len(txIns)-unconfirmedCount, len(ud.AccProof.Targets))
		return nil, messageError("GenerateUData", str)
	}

	return ud, nil
}
