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
	magicBytes = [4]byte{0xaa, 0xff, 0xaa, 0xff}
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
	//
	// NOTE Since we account for the genesis block in the offsets, to fetch data for
	// height x, you'd do 'offsets[x]' and not 'offsets[x-1]'.
	offsets []int64
}

// Init initializes the FlatFileState.  If resuming, it loads the offsets onto memory.
// If starting new, it creates an offsetFile and a dataFile along with the directories
// those belong in.
func (ff *FlatFileState) Init(path, dataName string) error {
	// Call MkdirAll before doing anything.  This will just do nothing if
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

	// Seek to end to get the number of offsets in the file (# of blocks).
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
		// -1 since we have to account for the genesis block of height 0.
		ff.currentHeight = int32(offsetFileSize/8) - 1

		// Seek back to the file start / block "0".
		_, err := ff.offsetFile.Seek(0, 0)
		if err != nil {
			return err
		}
		ff.offsets = make([]int64, offsetFileSize/8)

		// Go through the entire offset file and read&store every offset in memory.
		for i := int32(0); i <= ff.currentHeight; i++ {
			err = binary.Read(ff.offsetFile, binary.BigEndian, &ff.currentOffset)
			if err != nil {
				return err
			}

			ff.offsets[i] = ff.currentOffset
		}

		// Set the currentOffset to the end of the data file.
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

		// Oo the same with the in-ram slice.
		ff.offsets = make([]int64, 1)
	}

	return nil
}

// StoreData stores the given byte slice as a new entry in the dataFile.
// Two important things to note:
//
// 1: The height passed in should be of the corresponding Bitcoin block
//
//	height and it is used as a key to later fetch the stored data.
//
// 2: The height and data passed in should be for the next Bitcoin block in
//
//	sequence.  If data for block 50 was stored last, then data for block
//	51 should be passed in.
//
// This function is safe for concurrent access.
func (ff *FlatFileState) StoreData(height int32, data []byte) error {
	ff.mtx.Lock()
	defer ff.mtx.Unlock()

	// We only accept the next block in seqence.
	if height != ff.currentHeight+1 || height <= 0 {
		return fmt.Errorf("Passed in height not the next block in sequence. "+
			"Expected height of %d but got %d", ff.currentHeight+1, height)
	}

	// Pre-allocate the needed buffer.
	buf := make([]byte, len(data)+8)

	// Slice the buffer to 8 bytes and encode the offset to it.
	buf = buf[:8]
	ff.offsets = append(ff.offsets, ff.currentOffset)
	binary.BigEndian.PutUint64(buf, uint64(ff.currentOffset))

	// Do the actual currentOffset write to the offset file.
	_, err := ff.offsetFile.WriteAt(buf, int64(height)*8)
	if err != nil {
		return err
	}

	// Re-slice the buffer to the total length.
	buf = buf[:len(data)+8]

	// Add the magic bytes, size, and the data to the buffer to be written.
	copy(buf[:4], magicBytes[:])
	binary.BigEndian.PutUint32(buf[4:8], uint32(len(data)))
	copy(buf[8:], data)

	// Write the magic+size+data to the dataFile.
	_, err = ff.dataFile.WriteAt(buf, ff.currentOffset)
	if err != nil {
		return err
	}

	// Increment the current offset.  +8 to account for the magic bytes and size.
	ff.currentOffset += int64(len(data)) + 8

	// Finally, increment the currentHeight.
	ff.currentHeight++

	return nil
}

// FetchData fetches the data stored for the given block height.  Returns
// nil if the requested height is greater than the one it stored.  Also
// returns nil if asked to fetch height 0.
//
// This function is safe for concurrent access.
func (ff *FlatFileState) FetchData(height int32) ([]byte, error) {
	ff.mtx.RLock()
	defer ff.mtx.RUnlock()

	// If the height requsted is greater than the one we have saved,
	// just return nil.
	if height > ff.currentHeight || height <= 0 {
		return nil, nil
	}

	// Grab the offset for where the data is in the dataFile.
	offset := ff.offsets[height]

	// Read from the dataFile.  This read will grab the magic bytes and the
	// size bytes.
	buf := make([]byte, 8)
	_, err := ff.dataFile.ReadAt(buf, offset)
	if err != nil {
		return nil, err
	}

	// Sanity check.  If wrong magic was read, then error out.
	if !bytes.Equal(buf[:4], magicBytes[:]) {
		return nil, fmt.Errorf("Read wrong magic bytes. Expect %x but got %x",
			magicBytes, buf[:4])
	}

	// Size of the actual data we want to fetch.
	size := binary.BigEndian.Uint32(buf[4:])

	// Now do the actual read of the data from the dataFile.
	dataBuf := make([]byte, size)
	_, err = ff.dataFile.ReadAt(dataBuf, offset+8)
	if err != nil {
		return nil, err
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

	if !bytes.Equal(buf[:4], magicBytes[:]) {
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

	// Set the currentOffset as the last offset.
	ff.currentOffset = ff.offsets[len(ff.offsets)-1]

	// Pop the offset in memory.
	ff.offsets = ff.offsets[:len(ff.offsets)-1]

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
