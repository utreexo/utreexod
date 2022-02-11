// Copyright (c) 2021-2022 The utreexo developer
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/wire"
)

// proofStatsSize has 19 elements that are each 8 bytes big.
const proofStatsSize int = 8 * 19

// proofStats are the relevant proof statistics to check how big each proofs are.
type proofStats struct {
	// The height of the chain for the below stats.
	BlockHeight uint64

	// The overhead of the multi interval proof.
	MultiBlockProofOverheadSum float64
	MultiBlockProofCount       uint64

	// The overhead of the single interval proof.
	BlockProofOverheadSum float64
	BlockProofCount       uint64

	// Total deletions vs the proven deletions by the multi-block proof.
	TotalDels       uint64
	TotalProvenDels uint64

	// Size of all the leaf datas.
	LdSize  uint64
	LdCount uint64

	// Size of all the targets in the batchproofs.
	TgSize  uint64
	TgCount uint64

	// Size of all the proofs in the batchproofs.
	ProofSize  uint64
	ProofCount uint64

	// Size of the multi-block targets.
	MbTgSize  uint64
	MbTgCount uint64

	// Size of the multi-block proofs.
	MbProofSize  uint64
	MbProofCount uint64

	// Size of the leafhashes for the multi-block proofs.
	MbHashSize  uint64
	MbHashCount uint64
}

// UpdateTotalDelCount updates the deletion count in the proof stats.
func (ps *proofStats) UpdateTotalDelCount(delCount uint64) {
	ps.TotalDels += delCount
}

// UpdateUDStats updates the all the udata statistics.
func (ps *proofStats) UpdateUDStats(excludeAccProof bool, ud *wire.UData) {
	// Update target size.
	ps.TgSize += uint64(wire.BatchProofSerializeTargetSize(&ud.AccProof))
	ps.TgCount += uint64(len(ud.AccProof.Targets))

	// Update leaf data size.
	ps.LdSize += uint64(ud.SerializeUxtoDataSizeCompact(false))
	ps.LdCount += uint64(len(ud.LeafDatas))

	// Update proof size if the proof is to be included.
	if !excludeAccProof {
		ps.ProofSize += uint64(wire.BatchProofSerializeAccProofSize(&ud.AccProof))
		ps.ProofCount += uint64(len(ud.AccProof.Proof))
	}

	// Calculate the proof overhead.
	overhead := calcProofOverhead(ud)
	ps.BlockProofCount++
	ps.BlockProofOverheadSum += overhead
}

// UpdateMultiUDStats updates the multi-block utreexo data statistics.
func (ps *proofStats) UpdateMultiUDStats(delCount int, multiUd *wire.UData) {
	// Update target size.
	ps.MbTgSize += uint64(wire.BatchProofSerializeTargetSize(&multiUd.AccProof))
	ps.MbTgCount += uint64(len(multiUd.AccProof.Targets))

	// Update proof size.
	ps.MbProofSize += uint64(wire.BatchProofSerializeAccProofSize(&multiUd.AccProof))
	ps.MbProofCount += uint64(len(multiUd.AccProof.Proof))

	// Update multi-block proof overhead.
	overhead := calcProofOverhead(multiUd)
	ps.MultiBlockProofCount++
	ps.MultiBlockProofOverheadSum += overhead

	// Update the multi-block proof hash size.
	ps.MbHashSize += uint64(delCount * chainhash.HashSize)
	ps.MbHashCount += uint64(delCount)

	// Update proven dels by the multi-block proofs.
	ps.TotalProvenDels += uint64(delCount)
}

// LogProofStats outputs a log of the proof statistics.
func (ps *proofStats) LogProofStats() {
	log.Infof("height %d: totalProvenPercentage %f, totalDels %d, totalProvenDels %d, ldSize %d, ldCount %d, tgSize %d, tgCount %d, proofSize %d, proofCount %d "+
		"mbTgSize %d, mbTgCount %d, mbProofSize %d, mbProofCount %d, mbHashSize %d, mbHashCount %d",
		ps.BlockHeight, float64(ps.TotalProvenDels)/float64(ps.TotalDels), ps.TotalDels, ps.TotalProvenDels, ps.LdSize, ps.LdCount, ps.TgSize, ps.TgCount,
		ps.ProofSize, ps.ProofCount, ps.MbTgSize, ps.MbTgCount,
		ps.MbProofSize, ps.MbProofCount, ps.MbHashSize, ps.MbHashCount)

	log.Infof("height %d, average-blockoverhead %f, average-multiblockoverhead %f,  blockoverhead-sum %f, blockcount %d, mboverhead-sum %f, mbCount %d",
		ps.BlockHeight, ps.BlockProofOverheadSum/float64(ps.BlockProofCount), ps.MultiBlockProofOverheadSum/float64(ps.MultiBlockProofCount),
		ps.BlockProofOverheadSum, ps.BlockProofCount, ps.MultiBlockProofOverheadSum, ps.MultiBlockProofCount)
}

// Serialize serializes the proof statistics into the writer.
func (ps *proofStats) Serialize(w io.Writer) error {
	// 19 * 8
	var buf [8]byte

	binary.BigEndian.PutUint64(buf[:], ps.BlockHeight)
	_, err := w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], math.Float64bits(ps.MultiBlockProofOverheadSum))
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.MultiBlockProofCount)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], math.Float64bits(ps.BlockProofOverheadSum))
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.BlockProofCount)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.TotalDels)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.TotalProvenDels)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.LdSize)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.LdCount)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.TgSize)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.TgCount)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.ProofSize)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.ProofCount)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.MbTgSize)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.MbTgCount)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.MbProofSize)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.MbProofCount)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.MbHashSize)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	binary.BigEndian.PutUint64(buf[:], ps.MbHashCount)
	_, err = w.Write(buf[:])
	if err != nil {
		return err
	}

	return nil
}

// Deserialize deserializes the proof statistics from the reader.
func (ps *proofStats) Deserialize(r io.Reader) error {
	var buf [8]byte
	var res uint64

	_, err := r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.BlockHeight = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	res = binary.BigEndian.Uint64(buf[:])
	ps.MultiBlockProofOverheadSum = math.Float64frombits(res)

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.MultiBlockProofCount = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	res = binary.BigEndian.Uint64(buf[:])
	ps.BlockProofOverheadSum = math.Float64frombits(res)

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.BlockProofCount = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.TotalDels = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.TotalProvenDels = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.LdSize = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.LdCount = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.TgSize = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.TgCount = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.ProofSize = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.ProofCount = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.MbTgSize = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.MbTgCount = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.MbProofSize = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.MbProofCount = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.MbHashSize = binary.BigEndian.Uint64(buf[:])

	_, err = r.Read(buf[:])
	if err != nil {
		return err
	}
	ps.MbHashCount = binary.BigEndian.Uint64(buf[:])

	return nil
}

// WritePStats writes the proof statistics into the passed in flatfile.  It always
// writes the stats in the beginning of the file.
func (ps *proofStats) WritePStats(pStatFF *FlatFileState) error {
	w := bytes.NewBuffer(make([]byte, 0, proofStatsSize))

	err := ps.Serialize(w)
	if err != nil {
		return err
	}

	_, err = pStatFF.dataFile.WriteAt(w.Bytes(), 0)
	if err != nil {
		return err
	}

	return nil
}

// InitPStats reads the proof statistics from the passed in flatfile and attempts to
// initializes the proof statistics.  If the size read is smaller than the proofStatsSize,
// then nothing is initialized and the function returns.
func (ps *proofStats) InitPStats(pStatFF *FlatFileState) error {
	buf := make([]byte, proofStatsSize)
	n, err := pStatFF.dataFile.ReadAt(buf, 0)
	if n < proofStatsSize {
		return nil
	}
	if err != nil {
		return err
	}

	reader := bytes.NewBuffer(buf)
	err = ps.Deserialize(reader)
	if err != nil {
		return err
	}

	return nil
}
