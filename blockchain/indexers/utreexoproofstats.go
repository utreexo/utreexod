// Copyright (c) 2021-2022 The utreexo developer
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"

	"github.com/utreexo/utreexod/wire"
)

// ProofStatsSize has 10 elements that are each 8 bytes big.
const ProofStatsSize int = 8 * 10

// ProofStats are the relevant proof statistics to check how big each proofs are.
type ProofStats struct {
	// The height of the chain for the below stats.
	BlockHeight uint64

	// The overhead of the single interval proof.
	BlockProofOverheadSum float64
	BlockProofCount       uint64

	// Total deletions.
	TotalDels uint64

	// Size of all the leaf datas.
	LdSize  uint64
	LdCount uint64

	// Size of all the targets in the batchproofs.
	TgSize  uint64
	TgCount uint64

	// Size of all the proofs in the batchproofs.
	ProofSize  uint64
	ProofCount uint64
}

// UpdateTotalDelCount updates the deletion count in the proof stats.
func (ps *ProofStats) UpdateTotalDelCount(delCount uint64) {
	ps.TotalDels += delCount
}

// UpdateUDStats updates the all the udata statistics.
func (ps *ProofStats) UpdateUDStats(excludeAccProof bool, ud *wire.UData) {
	// Update target size.
	ps.TgSize += uint64(wire.BatchProofSerializeTargetSize(&ud.AccProof))
	ps.TgCount += uint64(len(ud.AccProof.Targets))

	// Update leaf data size.
	ps.LdSize += uint64(ud.SerializeCompactUtxoDataSize())
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

// LogProofStats outputs a log of the proof statistics.
func (ps *ProofStats) LogProofStats() {
	log.Infof("height %d: totalDels %d, ldSize %d, ldCount %d, tgSize %d, tgCount %d, proofSize %d, proofCount %d",
		ps.BlockHeight, ps.TotalDels, ps.LdSize, ps.LdCount, ps.TgSize, ps.TgCount,
		ps.ProofSize, ps.ProofCount)

	log.Infof("height %d, average-blockoverhead %f, blockoverhead-sum %f, blockcount %d",
		ps.BlockHeight, ps.BlockProofOverheadSum/float64(ps.BlockProofCount),
		ps.BlockProofOverheadSum, ps.BlockProofCount)
}

// Serialize serializes the proof statistics into the writer.
func (ps *ProofStats) Serialize(w io.Writer) error {
	// 19 * 8
	var buf [8]byte

	binary.BigEndian.PutUint64(buf[:], ps.BlockHeight)
	_, err := w.Write(buf[:])
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

	return nil
}

// Deserialize deserializes the proof statistics from the reader.
func (ps *ProofStats) Deserialize(r io.Reader) error {
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

	return nil
}

// WritePStats writes the proof statistics into the passed in flatfile.  It always
// writes the stats in the beginning of the file.
func (ps *ProofStats) WritePStats(pStatFF *FlatFileState) error {
	w := bytes.NewBuffer(make([]byte, 0, ProofStatsSize))

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
func (ps *ProofStats) InitPStats(pStatFF *FlatFileState) error {
	buf := make([]byte, ProofStatsSize)
	n, err := pStatFF.dataFile.ReadAt(buf, 0)
	if n < ProofStatsSize {
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
