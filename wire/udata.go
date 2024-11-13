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
}

// Copy creates a deep copy of the utreexo data so the original does not get modified
// when the copy is manipulated.
func (ud *UData) Copy() *UData {
	proofCopy := utreexo.Proof{
		Targets: make([]uint64, len(ud.AccProof.Targets)),
		Proof:   make([]utreexo.Hash, len(ud.AccProof.Proof)),
	}
	copy(proofCopy.Targets, ud.AccProof.Targets)
	copy(proofCopy.Proof, ud.AccProof.Proof)

	newUD := UData{
		AccProof:  proofCopy,
		LeafDatas: make([]LeafData, len(ud.LeafDatas)),
	}

	for i := range newUD.LeafDatas {
		newUD.LeafDatas[i] = *ud.LeafDatas[i].Copy()
	}

	return &newUD
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

// SerializeSize returns the number of bytes it would take to serialize the
// UData.
func (ud *UData) SerializeSize() int {
	// Leaf data size.
	return BatchProofSerializeSize(&ud.AccProof) + ud.SerializeUtxoDataSize()
}

// -----------------------------------------------------------------------------
// UData serialization includes all the data that is needed for a utreexo node to
// verify a block or a tx with only the utreexo roots.
//
// The serialized format is:
// [<accumulator proof><leaf datas>]
//
// Accumulator proof serialization follows the batchproof serialization found
// in wire/batchproof.go.
//
// LeafData serialization can be found in wire/leaf.go.
//
// All together, the serialization looks like so:
//
// Field                    Type       Size
// accumulator proof        []byte     variable
// leaf datas               []byte     variable
//
// -----------------------------------------------------------------------------

// Serialize encodes the UData to w using the UData serialization format.
func (ud *UData) Serialize(w io.Writer) error {
	// Write batch proof.
	err := BatchProofSerialize(w, &ud.AccProof)
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
	if udCount == 0 {
		return nil
	}

	ud.LeafDatas = make([]LeafData, 0, udCount)
	for i := 0; i < int(udCount); i++ {
		ld := LeafData{}
		err = ld.Deserialize(r)
		if err != nil {
			str := fmt.Sprintf("targetCount:%d, Stxos[%d], err:%s\n",
				len(ud.AccProof.Targets), i, err.Error())
			returnErr := messageError("Deserialize stxos", str)
			return returnErr
		}
		ud.LeafDatas = append(ud.LeafDatas, ld)
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
// [<accumulator proof><leaf datas>]
//
// Accumulator proof serialization follows the batchproof serialization found
// in wire/batchproof.go.
//
// Compact LeafData serialization can be found in wire/leaf.go.
//
// All together, the serialization looks like so:
//
// Field                    Type       Size
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
	// Accumulator proof size + leaf data size
	return BatchProofSerializeSize(&ud.AccProof) + ud.SerializeUxtoDataSizeCompact()
}

// SerializeCompact encodes the UData to w using the compact UData
// serialization format.  It follows the normal UData serialization format with
// the exception that compact leaf data serialization is used.  Everything else
// remains the same.
func (ud *UData) SerializeCompact(w io.Writer) error {
	err := BatchProofSerialize(w, &ud.AccProof)
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
	proof, err := BatchProofDeserialize(r)
	if err != nil {
		return err
	}
	ud.AccProof = *proof

	// Grab the count for the udatas
	udCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}
	if udCount == 0 {
		return nil
	}
	ud.LeafDatas = make([]LeafData, udCount)

	for i := range ud.LeafDatas {
		err = ud.LeafDatas[i].DeserializeCompact(r)
		if err != nil {
			str := fmt.Sprintf("targetCount:%d, LeafDatas[%d], err:%s\n",
				len(ud.AccProof.Targets), i, err.Error())
			returnErr := messageError("Deserialize leaf datas", str)
			return returnErr
		}
	}

	return nil
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
