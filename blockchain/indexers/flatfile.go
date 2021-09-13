// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	// flatFileNameSuffix is the suffix for any flat files.  The data
	// that is stored here is prepended to this value to form the
	// full flatfile name
	flatFileNameSuffix = "flat"

	// offsetFileName is the name given to the offsetFile in any given
	// FlatFileState.
	offsetFileName = "offset.dat"

	// dataFileSuffix is the suffix given to the dataFile name.
	dataFileSuffix = ".dat"
)

var (
	// magicBytes are the bytes prepended to any entry in the dataFiles.
	magicBytes = []byte{0xaa, 0xff, 0xaa, 0xff}
)

// FlatFileState is the shared state for storing flatfiles.  It is specifically designed
// for the utreexo proofs and stores data as a [key-value] of [height-data].
type FlatFileState struct {
	// The latest height.
	currentHeight int32

	// The latest offset.
	currentOffset int64

	// mtx controls concurrent access to the dataFile and offsetFile.
	mtx *sync.RWMutex

	// dataFile is where the actual data is kept.
	dataFile *os.File

	// offsetFile is where all the offset are kept for the dataFile.
	offsetFile *os.File

	// offsets contain all the byte offset information for the where each of the
	// blocks can be found in the dataFile.  On exit, all the offsets are flushed
	// to the offsetFile.
	offsets []int64
}

// Init initializes the FlatFileState.  If resuming, it loads the offsets onto memory.
// If starting new, it creates an offsetFile and a dataFile along with the directories
// those belong in.
func (ff *FlatFileState) Init(path, dataName string) error {
	// Call MkdirAll before doing anything.  This will just do nothin if
	// the directories are already there.
	err := os.MkdirAll(path, 0700)
	if err != nil {
		return err
	}

	offsetPath := filepath.Join(path, offsetFileName)
	ff.offsetFile, err = os.OpenFile(offsetPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}

	dataPath := filepath.Join(path, dataName+dataFileSuffix)
	ff.dataFile, err = os.OpenFile(dataPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}

	// seek to end to get the number of offsets in the file (# of blocks)
	offsetFileSize, err := ff.offsetFile.Seek(0, 2)
	if err != nil {
		return err
	}

	// Offsets are always 8 bytes each.
	if offsetFileSize%8 != 0 {
		return fmt.Errorf("FlatFileState.Init(): Corrupt FlatFileState. " +
			"offsetFile not mulitple of 8 bytes")
	}

	// If the file size is bigger than 0, we're resuming and will read all
	// existing offsets to ff.offsets.
	if offsetFileSize > 0 {
		maxHeight := int32(offsetFileSize / 8)

		// seek back to the file start / block "0"
		_, err := ff.offsetFile.Seek(0, 0)
		if err != nil {
			return err
		}
		ff.offsets = make([]int64, maxHeight)

		// run through the file, read everything and push into the channel
		for ff.currentHeight < maxHeight {
			err = binary.Read(ff.offsetFile, binary.BigEndian, &ff.currentOffset)
			if err != nil {
				return err
			}

			ff.offsets[ff.currentHeight] = ff.currentOffset
			ff.currentHeight++
		}

		// set currentOffset to the end of the data file
		ff.currentOffset, err = ff.dataFile.Seek(0, 2)
		if err != nil {
			return err
		}

	} else {
		// We don't save block 0 with utreexo proof index.  Just append
		// 0s since we don't keep it.
		_, err = ff.offsetFile.Write(make([]byte, 8))
		if err != nil {
			return err
		}

		// do the same with the in-ram slice
		ff.offsets = make([]int64, 1)
	}

	return nil
}

// StoreData stores the given byte slice as a new entry in the dataFile.
// Two important things to note:
//
// 1: The height passed in should be of the corresponding Bitcoin block
//    height and it is used as a key to later fetch the stored data.
//
// 2: The height and data passed in should be for the next Bitcoin block in
//    sequence.  If data for block 50 was stored last, then data for block
//    51 should be passed in.
//
// This function is safe for concurrent access.
func (ff *FlatFileState) StoreData(height int32, data []byte) error {
	ff.mtx.Lock()
	defer ff.mtx.Unlock()

	// We only accept the next block in seqence.
	if height != ff.currentHeight+1 {
		return fmt.Errorf("Passed in height not the next block in sequence. "+
			"Expected height of %d but got %d", ff.currentHeight+1, height)
	}

	// Pre-allocated the needed buffer.
	buf := make([]byte, 8)

	// Write the offset to buffer.
	buf = buf[:8]
	ff.offsets = append(ff.offsets, ff.currentOffset)
	binary.BigEndian.PutUint64(buf, uint64(ff.currentOffset))

	// Do the actual write.
	_, err := ff.offsetFile.WriteAt(buf, int64(height)*8)
	if err != nil {
		return err
	}

	_, err = ff.dataFile.WriteAt(magicBytes, ff.currentOffset)
	if err != nil {
		return err
	}

	// prefix with size
	buf = buf[:4]
	binary.BigEndian.PutUint32(buf, uint32(len(data)))

	// +4 to account for the 4 magic bytes
	_, err = ff.dataFile.WriteAt(buf, ff.currentOffset+4)
	if err != nil {
		return err
	}

	// Write to the file
	// +4 +4 to account for the 4 magic bytes and the 4 size bytes
	_, err = ff.dataFile.WriteAt(data, ff.currentOffset+8)
	if err != nil {
		return err
	}

	// 4B magic & 4B size comes first
	ff.currentOffset += int64(len(data)) + 8
	ff.currentHeight++

	return nil
}

// FetchData fetches the data stored for the given block height.  Returns
// nil if the requested height is greater than the one it stored.
//
// This function is safe for concurrent access.
func (ff *FlatFileState) FetchData(height int32) ([]byte, error) {
	ff.mtx.RLock()
	defer ff.mtx.RUnlock()

	// If the height requsted is greater than the one we have saved,
	// just return nil.
	if height > ff.currentHeight {
		return nil, nil
	}

	offset := ff.offsets[height]
	buf := make([]byte, 8)

	// Then read the actual proof from the prooffile and deserialize
	_, err := ff.dataFile.ReadAt(buf, offset)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal(buf[:4], magicBytes) {
		return nil, fmt.Errorf("read wrong magic of %x", buf[:4])
	}

	size := binary.BigEndian.Uint32(buf[4:])

	dataBuf := make([]byte, size)

	read, err := ff.dataFile.ReadAt(dataBuf, offset+8)
	if err != nil {
		return nil, err
	}

	if read != int(size) {
		return nil, fmt.Errorf("Expected to read %d bytes but read % bytes",
			size, read)
	}

	return dataBuf, nil
}

// DisconnectBlock is used during reorganizations and it deletes the last data
// stored to the FlatFileState.  The height given is only used to check that
// the height that is requested to be deleted matches the last data stored.
//
// This function is safe for concurrent access.
func (ff *FlatFileState) DisconnectBlock(height int32) error {
	ff.mtx.Lock()
	defer ff.mtx.Unlock()

	if height != ff.currentHeight {
		return fmt.Errorf("FlatFileState: Lastest block saved is %d but was asked to disconnect height %d",
			ff.currentHeight, height)
	}

	offset := ff.offsets[height]
	buf := make([]byte, 8)

	// Read from the dataFile to get the size of the data.
	_, err := ff.dataFile.ReadAt(buf, offset)
	if err != nil {
		return err
	}

	if !bytes.Equal(buf[:4], magicBytes) {
		return fmt.Errorf("read wrong magic of %x", buf[:4])
	}

	size := int64(binary.BigEndian.Uint32(buf[4:]))

	dataFileSize, err := ff.dataFile.Seek(0, 2)
	if err != nil {
		return err
	}

	err = ff.dataFile.Truncate(dataFileSize - (size + 8))
	if err != nil {
		return err
	}

	offsetFileSize, err := ff.offsetFile.Seek(0, 2)
	if err != nil {
		return err
	}

	// Each offset is 8 bytes.
	err = ff.offsetFile.Truncate(offsetFileSize - 8)
	if err != nil {
		return err
	}

	// Pop the offset in memory.
	ff.offsets = ff.offsets[:len(ff.offsets)-1]

	// Set the currentOffset as the last offset after the pop.
	ff.currentOffset = ff.offsets[len(ff.offsets)-1]

	// Go back one height.
	ff.currentHeight--

	return nil
}

// deleteFileFile removes the flat file state directory and all the contents
// in it.
func deleteFlatFile(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		log.Infof("Deleting the flatfiles at directory %s", path)
	} else {
		log.Infof("No flatfiles to delete")
	}
	return os.RemoveAll(path)
}

// NewFlatFileState returns a new but uninitialized FlatFileState.
func NewFlatFileState() *FlatFileState {
	return &FlatFileState{
		mtx: new(sync.RWMutex),
	}
}
