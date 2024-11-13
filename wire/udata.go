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

// SerializeAccSize returns the number of bytes it would take to serialize
// the accumulator with the compact accumulator serialization format.
func (ud *UData) SerializeAccSize() int {
	return BatchProofSerializeSize(&ud.AccProof)
}

// SerializeUtxoDataSize returns the number of bytes it would take to serialize the
// utxo data size.
func (ud *UData) SerializeUtxoDataSize() int {
	size := VarIntSerializeSize(uint64(len(ud.LeafDatas)))
	for _, l := range ud.LeafDatas {
		size += l.SerializeSizeCompact()
	}

	return size
}

// SerializeSize returns the number of bytes it would take to serialize the
// UData.
func (ud *UData) SerializeSize() int {
	// Accumulator proof size + leaf data size
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
// LeafData compact serialization can be found in wire/leaf.go.
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
		return err
	}

	// Write the size of the leaf datas.
	err = WriteVarInt(w, 0, uint64(len(ud.LeafDatas)))
	if err != nil {
		return err
	}

	// Write the actual leaf datas.
	for _, ld := range ud.LeafDatas {
		err = ld.SerializeCompact(w)
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
		return err
	}
	ud.AccProof = *proof

	// Read the size of the leaf datas.
	txInCount, err := ReadVarInt(r, 0)
	if err != nil {
		return err
	}
	if txInCount == 0 {
		ud.LeafDatas = nil
		return nil
	}

	ud.LeafDatas = make([]LeafData, 0, txInCount)
	for i := 0; i < int(txInCount); i++ {
		ld := LeafData{}
		err = ld.DeserializeCompact(r)
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
