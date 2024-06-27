// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"encoding/hex"
	"fmt"
	"io"

	"github.com/utreexo/utreexo"
)

// UData contains data needed to prove the existence and validity of all inputs
// for a Bitcoin block.  With this data, a full node may only keep the utreexo
// roots and still be able to fully validate a block.
type UData struct {
	// AccProof is the utreexo accumulator proof for all the inputs.
	AccProof utreexo.Proof

	// LeafDatas are the tx validation data for every input.
	LeafDatas []LeafData

	// All the indexes of new utxos to remember.
	RememberIdx []uint32
}

// StxosHashes returns the hash of all stxos in this UData.  The hashes returned
// here represent the hash commitments of the stxos.
func (ud *UData) StxoHashes() []utreexo.Hash {
	leafHashes := make([]utreexo.Hash, len(ud.LeafDatas))
	for i, stxo := range ud.LeafDatas {
		leafHashes[i] = stxo.LeafHash()
	}

	return leafHashes
}

// SerializeUtxoDataSize returns the number of bytes it would take to serialize the
// utxo data size.
func (ud *UData) SerializeUtxoDataSize() int {
	size := VarIntSerializeSize(uint64(len(ud.LeafDatas)))
	for _, l := range ud.LeafDatas {
		size += l.SerializeSize()
	}

	return size
}

// SerializeRememberIdxSize returns the number of bytes it would take to serialize the
// remember indexes.
func (ud *UData) SerializeRememberIdxSize() int {
	return SerializeRemembersSize(ud.RememberIdx)
}

// SerializeSize returns the number of bytes it would take to serialize the
// UData.
func (ud *UData) SerializeSize() int {
	// Accumulator proof size.
	size := BatchProofSerializeSize(&ud.AccProof)

	// Leaf data size.
	size += ud.SerializeUtxoDataSize()

	// Remember indexes size.
	return size + ud.SerializeRememberIdxSize()
}

// -----------------------------------------------------------------------------
// UData serialization includes all the data that is needed for a utreexo node to
// verify a block or a tx with only the utreexo roots.  Remember indexes aren't
// necessary but are there for caching purposes.
//
// The serialized format is:
// [<remember indexes><accumulator proof><leaf datas>]
//
// Accumulator proof serialization follows the batchproof serialization found
// in wire/batchproof.go.
//
// LeafData serialization can be found in wire/leaf.go.
//
// All together, the serialization looks like so:
//
// Field                    Type       Size
// remember indexes         []varint   variable
// accumulator proof        []byte     variable
// leaf datas               []byte     variable
//
// -----------------------------------------------------------------------------

// Serialize encodes the UData to w using the UData serialization format.
func (ud *UData) Serialize(w io.Writer) error {
	err := SerializeRemembers(w, ud.RememberIdx)
	if err != nil {
		return err
	}

	// Write batch proof.
	err = BatchProofSerialize(w, &ud.AccProof)
	if err != nil {
		returnErr := messageError("Serialize", err.Error())
		return returnErr
	}

	// Write the size of the leaf datas.
	err = WriteVarInt(w, 0, uint64(len(ud.LeafDatas)))
	if err != nil {
		return err
	}

	// Write the actual leaf datas.
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
	remembers, err := DeserializeRemembers(r)
	if err != nil {
		return err
	}
	ud.RememberIdx = remembers

	proof, err := BatchProofDeserialize(r)
	if err != nil {
		returnErr := messageError("Deserialize AccProof", err.Error())
		return returnErr
	}
	ud.AccProof = *proof

	udCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}

	ud.LeafDatas = make([]LeafData, udCount)
	for i := range ud.LeafDatas {
		err = ud.LeafDatas[i].Deserialize(r)
		if err != nil {
			str := fmt.Sprintf("rememberCount %d, targetCount:%d, Stxos[%d], err:%s\n",
				len(remembers), len(ud.AccProof.Targets), i, err.Error())
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
// by a node.  Remember indexes aren't necessary but are there for caching
// purposes.
//
// The serialized format is:
// [<remember indexes><accumulator proof><leaf datas>]
//
// Accumulator proof serialization follows the batchproof serialization found
// in wire/batchproof.go.
//
// Compact LeafData serialization can be found in wire/leaf.go.
//
// All together, the serialization looks like so:
//
// Field                    Type       Size
// remember indexes         []varint   variable
// accumulator proof        []byte     variable
// leaf datas               []byte     variable
//
// -----------------------------------------------------------------------------

// SerializeAccSizeCompact returns the number of bytes it would take to serialize
// the accumulator with the compact accumulator serialization format.
func (ud *UData) SerializeAccSizeCompact() int {
	return BatchProofSerializeSize(&ud.AccProof)
}

// SerializeUxtoDataSizeCompact returns the number of bytes it would take to serialize
// the utxo data and the remember idx data with the compact serialization format.
func (ud *UData) SerializeUxtoDataSizeCompact() int {
	var size int

	// Explicitly serialize the count for the leaf datas.
	size += VarIntSerializeSize(uint64(len(ud.LeafDatas)))
	for _, l := range ud.LeafDatas {
		size += l.SerializeSizeCompact()
	}

	return size
}

// SerializeSizeCompact returns the number of bytes it would take to serialize the
// UData using the compact UData serialization format.
func (ud *UData) SerializeSizeCompact() int {
	// Accumulator proof size.
	size := BatchProofSerializeSize(&ud.AccProof)

	// Leaf data size
	size += ud.SerializeUxtoDataSizeCompact()

	// Remember indexes size.
	return size + ud.SerializeRememberIdxSize()
}

// SerializeCompact encodes the UData to w using the compact UData
// serialization format.  It follows the normal UData serialization format with
// the exception that compact leaf data serialization is used.  Everything else
// remains the same.
func (ud *UData) SerializeCompact(w io.Writer) error {
	err := SerializeRemembers(w, ud.RememberIdx)
	if err != nil {
		return err
	}

	err = BatchProofSerialize(w, &ud.AccProof)
	if err != nil {
		returnErr := messageError("SerializeCompact", err.Error())
		return returnErr
	}

	err = WriteVarInt(w, 0, uint64(len(ud.LeafDatas)))
	if err != nil {
		return err
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
//
// NOTE if deserializing for a transaction, a non zero txInCount MUST be passed
// in as a correct txCount is critical for deserializing correctly.  When
// deserializing a block, txInCount does not matter.
func (ud *UData) DeserializeCompact(r io.Reader) error {
	remembers, err := DeserializeRemembers(r)
	if err != nil {
		return err
	}
	ud.RememberIdx = remembers

	proof, err := BatchProofDeserialize(r)
	if err != nil {
		returnErr := messageError("DeserializeCompact", err.Error())
		return returnErr
	}
	ud.AccProof = *proof

	// Grab the count for the udatas
	udCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}
	ud.LeafDatas = make([]LeafData, udCount)

	for i := range ud.LeafDatas {
		err = ud.LeafDatas[i].DeserializeCompact(r)
		if err != nil {
			str := fmt.Sprintf("rememberCount %d, targetCount:%d, LeafDatas[%d], err:%s\n",
				len(remembers), len(ud.AccProof.Targets), i, err.Error())
			returnErr := messageError("Deserialize leaf datas", str)
			return returnErr
		}
	}

	return nil
}

// SerializeSizeCompactNoAccProof returns the number of bytes it would take to
// serialize the utreexo data without the accumulator proof.
func (ud *UData) SerializeSizeCompactNoAccProof() int {
	size := VarIntSerializeSize(uint64(len(ud.AccProof.Targets)))
	for _, target := range ud.AccProof.Targets {
		size += VarIntSerializeSize(target)
	}

	return size + ud.SerializeUxtoDataSizeCompact()
}

// SerializeCompactNoAccProof will serialize the utreexo data with the compact
// utreexo data format but will leave out the accumulator proof.
func (ud *UData) SerializeCompactNoAccProof(w io.Writer) error {
	// Serialize the targets.
	bp := ud.AccProof
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

	// Write all the leafDatas.
	err = WriteVarInt(w, 0, uint64(len(ud.LeafDatas)))
	if err != nil {
		return err
	}
	for _, ld := range ud.LeafDatas {
		err := ld.SerializeCompact(w)
		if err != nil {
			return err
		}
	}

	return nil
}

// DeserializeCompactNoAccProof will deserialize the utreexo data that has been
// serialized without the accumulator proof.
//
// NOTE: this function should only be called on udata that has been compactly serialized
// without the accumulator proof.
func (ud *UData) DeserializeCompactNoAccProof(r io.Reader) error {
	// Deserialize the targets.
	targetCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}
	targets := make([]uint64, targetCount)
	for i := range targets {
		target, err := ReadVarInt(r, 0)
		if err != nil {
			return err
		}

		targets[i] = target
	}
	ud.AccProof = utreexo.Proof{Targets: targets}

	// Grab the count for the udatas
	udCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}
	ud.LeafDatas = make([]LeafData, udCount)
	for i := range ud.LeafDatas {
		err := ud.LeafDatas[i].DeserializeCompact(r)
		if err != nil {
			str := fmt.Sprintf("targetCount:%d, LeafDatas[%d], err:%s\n",
				len(ud.AccProof.Targets), i, err.Error())
			returnErr := messageError("Deserialize leaf datas", str)
			return returnErr
		}
	}

	return nil
}

// SerializeRemembersSize returns how many bytes it would take to serialize
// all the remember indexes.
func SerializeRemembersSize(remembers []uint32) int {
	size := VarIntSerializeSize(uint64(len(remembers)))
	for _, remember := range remembers {
		size += VarIntSerializeSize(uint64(remember))
	}

	return size
}

// SerializeRemembers serializes the passed in remembers to the writer.
func SerializeRemembers(w io.Writer, remembers []uint32) error {
	err := WriteVarInt(w, 0, uint64(len(remembers)))
	if err != nil {
		return err
	}

	for _, remember := range remembers {
		err = WriteVarInt(w, 0, uint64(remember))
		if err != nil {
			return err
		}
	}

	return nil
}

// DeserializeRemembers deserializes the remember indexes from the reader and
// returns the deserialized remembers.
func DeserializeRemembers(r io.Reader) ([]uint32, error) {
	count, err := ReadVarInt(r, 0)
	if err != nil {
		return nil, err
	}

	remembers := make([]uint32, count)
	for i := range remembers {
		remember, err := ReadVarInt(r, 0)
		if err != nil {
			return nil, err
		}
		remembers[i] = uint32(remember)
	}

	return remembers, nil
}

// HashesFromLeafDatas hashes the passed in leaf datas. Returns an error if a
// leaf data is compact as you can't generate the correct hash.
func HashesFromLeafDatas(leafDatas []LeafData) ([]utreexo.Hash, error) {
	// make slice of hashes from leafdata
	delHashes := make([]utreexo.Hash, 0, len(leafDatas))
	for _, ld := range leafDatas {
		// We can't calculate the correct hash if the leaf data is in
		// the compact state.
		if ld.IsCompact() {
			return nil, fmt.Errorf("leafdata is compact. Unable " +
				"to generate a leafhash")
		}

		delHashes = append(delHashes, ld.LeafHash())
	}

	return delHashes, nil
}

// GenerateUData creates a block proof, calling forest.ProveBatch with the leaf indexes
// to get a batched inclusion proof from the accumulator. It then adds on the leaf data,
// to create a block proof which both proves inclusion and gives all utxo data
// needed for transaction verification.
func GenerateUData(txIns []LeafData, pollard utreexo.Utreexo) (
	*UData, error) {

	ud := new(UData)
	ud.LeafDatas = txIns

	// Make a slice of hashes from the leafdatas.
	delHashes, err := HashesFromLeafDatas(ud.LeafDatas)
	if err != nil {
		return nil, err
	}

	// Generate the utreexo accumulator proof for all the inputs.
	ud.AccProof, err = pollard.Prove(delHashes)
	if err != nil {
		// Find out which exact one is causing the error.
		for i, delHash := range delHashes {
			_, err = pollard.Prove([]utreexo.Hash{delHash})
			if err != nil {
				ld := ud.LeafDatas[i]
				return nil,
					fmt.Errorf("LeafData hash %s couldn't be proven. "+
						"BlockHash %s, Outpoint %s, height %v, "+
						"IsCoinbase %v, Amount %v, PkScript %s. "+
						"err: %s",
						hex.EncodeToString(delHash[:]),
						ld.BlockHash.String(), ld.OutPoint.String(),
						ld.Height, ld.IsCoinBase, ld.Amount,
						hex.EncodeToString(ld.PkScript), err.Error())
			}
		}
		return nil, err
	}

	return ud, nil
}
