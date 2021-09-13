// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"
	"time"
)

func TestInit(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir) // clean up. Always runs

	dir := "TestInit"
	name := "data"
	ffPath := filepath.Join(tmpDir, dir)

	ff := NewFlatFileState()
	err = ff.Init(ffPath, name)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the directory is there.
	_, err = os.Stat(ffPath)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the file is under the directory.
	_, err = os.Stat(filepath.Join(ffPath, name+dataFileSuffix))
	if err != nil {
		t.Fatal(err)
	}

	// Check that the offsetfile is under the directory.
	_, err = os.Stat(filepath.Join(ffPath, offsetFileName))
	if err != nil {
		t.Fatal(err)
	}
}

// A function with boilerplate for the FlatFileState init.
func initFF(testName string) (*FlatFileState, string, error) {
	tmpDir, err := os.MkdirTemp("", "test")
	if err != nil {
		return nil, "", err
	}

	dir := testName
	name := "data"
	ffPath := filepath.Join(tmpDir, dir)

	ff := NewFlatFileState()
	err = ff.Init(ffPath, name)
	if err != nil {
		return nil, "", err
	}

	return ff, tmpDir, nil
}

func createRandByteSlice(rnd *rand.Rand) ([]byte, error) {
	arrayVal, ok := quick.Value(reflect.TypeOf([10]byte{}), rnd)
	if !ok {
		err := fmt.Errorf("Failed to create slice")
		return nil, err
	}
	array := arrayVal.Interface().([10]byte)
	return array[:], nil
}

// tryToStoreUnallowed tries to store unallowed height to the flatFileState.
func tryToStoreUnallowed(ff *FlatFileState, height int32, data []byte) error {
	// This should error out.
	err := ff.StoreData(height-1, data)
	if err == nil {
		return fmt.Errorf("Should not be able to store data for height %d "+
			"when ff.currentHeight is %d but successfully did so",
			height-1, ff.currentHeight)
	}

	// This should error out.
	err = ff.StoreData(height+1, data)
	if err == nil {
		return fmt.Errorf("Should not be able to store data for height %d "+
			"when ff.currentHeight is %d but successfully did so",
			height+1, ff.currentHeight)
	}

	return nil
}

func TestStoreAndFetchData(t *testing.T) {
	t.Parallel()

	ff, tmpDir, err := initFF("TestStoreAndFetchData")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir) // clean up. Always runs

	// Store random data to the flatfile.  Keep a copy of the stored
	// data in a map.
	storedData := make(map[int32][]byte)
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	blockCount := int32(1000)
	for i := int32(1); i <= blockCount; i++ {
		data, err := createRandByteSlice(rnd)
		if err != nil {
			t.Fatal(err)
		}
		storedData[i] = data

		// Try to store unallowed.
		err = tryToStoreUnallowed(ff, i, data)
		if err != nil {
			t.Fatal(err)
		}

		// Actually do the store.
		err = ff.StoreData(i, data)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Check that the height matches.
	if ff.currentHeight != blockCount {
		err := fmt.Errorf("Expected currentHeight of %d but got %d",
			blockCount, ff.currentHeight)
		t.Fatal(err)
	}

	// Check that the height matches.
	dataOffset, err := ff.dataFile.Seek(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if ff.currentOffset != dataOffset {
		err := fmt.Errorf("Expected currentOffset of %d but got %d",
			dataOffset, ff.currentOffset)
		t.Fatal(err)
	}

	// Check that the offset count matches.
	if len(ff.offsets) != int(blockCount)+1 {
		err := fmt.Errorf("Expected offsets length of %d but got %d",
			blockCount+1, len(ff.offsets))
		t.Fatal(err)
	}

	// Check that the offset size matches.
	offsetOffset, err := ff.offsetFile.Seek(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if (int64(blockCount)*8)+8 != offsetOffset {
		err := fmt.Errorf("Expected currentOffset of %d but got %d",
			offsetOffset, (blockCount*8)+8)
		t.Fatal(err)
	}

	// Fetch and compare to the data stored in the map.
	for key, value := range storedData {
		fetchedBytes, err := ff.FetchData(key)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(fetchedBytes, value) {
			err := fmt.Errorf("Expected %x but got %x", value, fetchedBytes)
			t.Fatal(err)
		}
	}
}

// tryToDisconnectUnallowed tries to disconnect unallowed height from the flatFileState.
func tryToDisconnectUnallowed(ff *FlatFileState, height int32) error {
	// This should error out.
	err := ff.DisconnectBlock(height - 1)
	if err == nil {
		return fmt.Errorf("Should not be able to disconnect block for height %d "+
			"when ff.currentHeight is %d but successfully did so",
			height-1, ff.currentHeight)
	}

	// This should error out.
	err = ff.DisconnectBlock(height + 1)
	if err == nil {
		return fmt.Errorf("Should not be able to disconnect block for height %d "+
			"when ff.currentHeight is %d but successfully did so",
			height+1, ff.currentHeight)
	}

	return nil
}

// getAfterSizes returns the correct file sizes after a disconnect.
func getAfterSizes(ff *FlatFileState, height int32) (int64, int64, error) {
	// Get the size of the data to be disconnected.
	offset := ff.offsets[height]
	buf := make([]byte, 8)

	_, err := ff.dataFile.ReadAt(buf, offset)
	if err != nil {
		return 0, 0, err
	}
	if !bytes.Equal(buf[:4], magicBytes) {
		return 0, 0, fmt.Errorf("read wrong magic of %x", buf[:4])
	}
	dataSize := binary.BigEndian.Uint32(buf[4:])

	// Get data file size.
	dataFileSize, err := ff.dataFile.Seek(0, 2)
	if err != nil {
		return 0, 0, err
	}

	// Get offset file size.
	offsetSize, err := ff.offsetFile.Seek(0, 2)
	if err != nil {
		return 0, 0, err
	}

	return dataFileSize - int64(dataSize+8), offsetSize - 8, nil
}

func getSizes(ff *FlatFileState) (int64, int64, error) {
	dataSize, err := ff.dataFile.Seek(0, 2)
	if err != nil {
		return 0, 0, err
	}

	offsetSize, err := ff.offsetFile.Seek(0, 2)
	if err != nil {
		return 0, 0, err
	}

	return dataSize, offsetSize, nil
}

func TestDisconnectBlock(t *testing.T) {
	t.Parallel()

	ff, tmpDir, err := initFF("TestDisconnectBlock")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir) // clean up. Always runs

	// Store random data to the flatfile.  Keep a copy of the stored
	// data in a map.
	storedData := make(map[int32][]byte)
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	blockCount := int32(1000)
	for i := int32(1); i <= blockCount; i++ {
		data, err := createRandByteSlice(rnd)
		if err != nil {
			t.Fatal(err)
		}
		storedData[i] = data

		err = ff.StoreData(i, data)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Disconnect all blocks til height 1.
	for ; blockCount >= 1; blockCount-- {
		// Check that we can get the latest block.
		gotData, err := ff.FetchData(blockCount)
		if err != nil {
			t.Fatal(err)
		}

		expectData := storedData[blockCount]
		if !bytes.Equal(expectData, gotData) {
			err := fmt.Errorf("Expected %x but got %x for height %d",
				expectData, gotData, blockCount)
			t.Fatal(err)
		}

		err = tryToDisconnectUnallowed(ff, blockCount)
		if err != nil {
			t.Fatal(err)
		}

		dataSizeExpect, offsetSizeExpect, err := getAfterSizes(ff, blockCount)
		if err != nil {
			t.Fatal(err)
		}

		// Disconnect the last block.
		err = ff.DisconnectBlock(blockCount)
		if err != nil {
			t.Fatal(err)
		}

		dataSize, offsetSize, err := getSizes(ff)
		if err != nil {
			t.Fatal(err)
		}

		// Check the sizes after the disconnect.
		if dataSizeExpect != dataSize {
			err := fmt.Errorf("Expected dataFile size of %d but got %d",
				dataSizeExpect, dataSize)
			t.Fatal(err)
		}
		if offsetSizeExpect != offsetSize {
			err := fmt.Errorf("Expected offsetFile size of %d but got %d",
				offsetSizeExpect, offsetSize)
			t.Fatal(err)
		}

		// Try to fetch that data.  Error if we get anything else other than
		// nil.
		shouldBeNil, err := ff.FetchData(blockCount)
		if err != nil {
			t.Fatal(err)
		}

		if shouldBeNil != nil {
			err := fmt.Errorf("Expected nil but fetched %v", shouldBeNil)
			t.Fatal(err)
		}
	}
}
