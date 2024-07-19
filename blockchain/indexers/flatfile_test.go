// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sync"
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

func closeFF(ff *FlatFileState) (int32, int64, []int64, error) {
	ff.mtx.Lock()
	defer ff.mtx.Unlock()

	err := ff.offsetFile.Close()
	if err != nil {
		return 0, 0, nil, err
	}

	err = ff.dataFile.Close()
	if err != nil {
		return 0, 0, nil, err
	}

	return ff.currentHeight, ff.currentOffset, ff.offsets, nil
}

func restartFF(tmpDir, testName string) (*FlatFileState, error) {
	dir := testName
	name := "data"
	ffPath := filepath.Join(tmpDir, dir)

	ff := NewFlatFileState()
	err := ff.Init(ffPath, name)
	if err != nil {
		return nil, err
	}

	return ff, nil
}

func TestRestart(t *testing.T) {
	t.Parallel()

	ff, tmpDir, err := initFF("TestRestart")
	if err != nil {
		t.Fatal(err)
	}

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

	expectHeight, expectCurrentOffset, offsets, err := closeFF(ff)
	if err != nil {
		t.Fatal(err)
	}
	expectOffsets := make([]int64, len(offsets))
	copy(expectOffsets, offsets)

	ff = nil

	newff, err := restartFF(tmpDir, "TestRestart")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	if newff.currentHeight != expectHeight {
		err := fmt.Errorf("TestRestart Err. Expect currentHeight of "+
			"%d, got %d", expectHeight, newff.currentHeight)
		t.Fatal(err)
	}

	if newff.currentOffset != expectCurrentOffset {
		err := fmt.Errorf("TestRestart Err. Expect currentOffset of "+
			"%d, got %d", expectCurrentOffset, newff.currentOffset)
		t.Fatal(err)
	}

	if len(newff.offsets) != len(expectOffsets) {
		err := fmt.Errorf("TestRestart Err. Expect offset length of "+
			"%d, got %d", len(expectOffsets), len(newff.offsets))
		t.Fatal(err)
	}

	for i, expectOffset := range expectOffsets {
		if expectOffset != newff.offsets[i] {
			err := fmt.Errorf("TestRestart Err. Expect offset at i of %d "+
				"to be %d, got %d", i, expectOffset, newff.offsets[i])
			t.Fatal(err)
		}
	}
}

func createRandByteSlice(rnd *rand.Rand) ([]byte, error) {
	const length = 20
	// Random value to differ up the array lengths.
	arrayVal, ok := quick.Value(reflect.TypeOf([length]byte{}), rnd)
	if !ok {
		err := fmt.Errorf("Failed to create slice")
		return nil, err
	}
	array := arrayVal.Interface().([length]byte)
	sliceLen := rand.Intn(length)
	return array[:sliceLen], nil
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
	if !bytes.Equal(buf[:4], magicBytes[:]) {
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

// ffStoreRandData generates and stores the data onto the map and the FlatFileState.
// Returns the map with the newly generated data.
func ffStoreRandData(blockCount int32, rnd *rand.Rand, ff *FlatFileState) (
	map[int32][]byte, error) {

	storedData := make(map[int32][]byte)

	for i := int32(1); i <= blockCount; i++ {
		data, err := createRandByteSlice(rnd)
		if err != nil {
			return nil, err
		}
		storedData[i] = data

		err = ff.StoreData(i, data)
		if err != nil {
			return nil, err
		}
	}

	return storedData, nil
}

// Given the lastHeight, check that all data from block 0 to block lastHeight
// equals the data in the map.
func checkDataStillFetches(lastHeight int32, ff *FlatFileState, storedData map[int32][]byte) error {
	for i := int32(0); i < lastHeight; i++ {
		gotBytes, err := ff.FetchData(i)
		if err != nil {
			return err
		}

		expectBytes := storedData[i]

		if !bytes.Equal(expectBytes, gotBytes) {
			err := fmt.Errorf("Expected to fetch %s but got %s",
				hex.EncodeToString(expectBytes), hex.EncodeToString(gotBytes))
			return err
		}
	}

	return nil
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
	blockCount := int32(1000)
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	storedData, err := ffStoreRandData(blockCount, rnd, ff)
	if err != nil {
		t.Fatal(err)
	}

	// Disconnect blocks til height 100.
	for ; blockCount > 100; blockCount-- {
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

		offsetLen := len(ff.offsets)

		// Disconnect the last block.
		err = ff.DisconnectBlock(blockCount)
		if err != nil {
			t.Fatal(err)
		}

		err = checkDataStillFetches(blockCount, ff, storedData)
		if err != nil {
			t.Fatal(err)
		}

		// Sanity check to make sure only one offset is added.
		offsetLenAfter := len(ff.offsets)
		if offsetLen-1 != offsetLenAfter {
			err := fmt.Errorf("expected offset len of %d but got %d",
				offsetLen-1, offsetLenAfter)
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

	// Store 1000 blocks of new data from the current height after the disconnects.
	newStoredData := make(map[int32][]byte)
	for i := ff.currentHeight + 1; i <= 1000; i++ {
		data, err := createRandByteSlice(rnd)
		if err != nil {
			t.Fatal(err)
		}
		newStoredData[i] = data

		err = ff.StoreData(i, data)
		if err != nil {
			t.Fatal(err)
		}

		for j := int32(0); j <= 100; j++ {
			gotBytes, err := ff.FetchData(j)
			if err != nil {
				t.Fatal(err)
			}

			expectBytes := storedData[j]

			if !bytes.Equal(expectBytes, gotBytes) {
				err := fmt.Errorf("Expected to fetch %s but got %s",
					hex.EncodeToString(expectBytes), hex.EncodeToString(gotBytes))
				t.Fatal(err)
			}
		}
	}

	// Check that all the data in the flatfile fetches and equals the data stored
	// in the map.
	for i := int32(0); i <= ff.currentHeight; i++ {
		gotBytes, err := ff.FetchData(i)
		if err != nil {
			t.Fatal(err)
		}

		// The data from blocks 0-100 are in the storedData map.  All else
		// are in the newStoredData map.
		if i <= 100 {
			expectBytes := storedData[i]

			if !bytes.Equal(expectBytes, gotBytes) {
				err := fmt.Errorf("Expected to fetch %s but got %s",
					hex.EncodeToString(expectBytes), hex.EncodeToString(gotBytes))
				t.Fatal(err)
			}
		} else {
			expectBytes := newStoredData[i]

			if !bytes.Equal(expectBytes, gotBytes) {
				err := fmt.Errorf("Expected to fetch %s but got %s",
					hex.EncodeToString(expectBytes), hex.EncodeToString(gotBytes))
				t.Fatal(err)
			}
		}
	}
}

// ffReader reads from the FlatFileState and compares it to the data stored
// on the map.  Only fetches random values that are specified between minBlock
// and maxBlock.
func ffReader(readPerWorker, minBlock, maxBlock int, ff *FlatFileState,
	wg *sync.WaitGroup, storedData map[int32][]byte) {

	defer wg.Done()

	randSource := rand.NewSource(time.Now().UnixNano())
	rand := rand.New(randSource)
	for i := 0; i < readPerWorker; i++ {
		getHeight := int32(rand.Intn(maxBlock-minBlock) + minBlock)
		gotBytes, err := ff.FetchData(getHeight)
		if err != nil {
			// TODO Would be better if we don't panic and do t.Error()
			panic(err)
		}

		expectBytes := storedData[getHeight]

		if !bytes.Equal(gotBytes, expectBytes) {
			err := fmt.Errorf("TestMultipleFetchData: Expected %s but got %s for height %v",
				hex.EncodeToString(expectBytes), hex.EncodeToString(gotBytes), getHeight)
			// TODO Would be better if we don't panic and do t.Error()
			panic(err)
		}
	}
}

func TestMultipleFetchData(t *testing.T) {
	t.Parallel()

	ff, tmpDir, err := initFF("TestMultipleFetchData")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir) // clean up. Always runs

	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	blockCount := int32(1000)

	// Store random data to the flatfile.  Keep a copy of the stored
	// data in a map.
	storedData, err := ffStoreRandData(blockCount, rnd, ff)
	if err != nil {
		t.Fatal(err)
	}

	readWorkerCount := 100
	readPerWorker := 1000
	readBlockCount := blockCount
	wg := new(sync.WaitGroup)

	for i := 0; i < readWorkerCount; i++ {
		wg.Add(1)
		// Generating block 0 as min block read tests if FetchData
		// correctly rejects block 0 reads.
		go ffReader(readPerWorker, 0, int(readBlockCount), ff, wg, storedData)
	}

	// Generate new blocks and store it in a new map.  Tests concurrent
	// writes while reads are still going.
	addCount := int32(50)
	newStoredData := make(map[int32][]byte)
	newCount := blockCount + addCount
	for i := blockCount + 1; i <= newCount; i++ {
		data, err := createRandByteSlice(rnd)
		if err != nil {
			t.Fatal(err)
		}
		newStoredData[i] = data

		err = ff.StoreData(i, data)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Fetch only the newly generated blocks and see if they match up to
	// the ones we have in the map.
	maxBlk := int(newCount)
	minBlk := int(blockCount) + 1
	for i := 0; i < readWorkerCount; i++ {
		wg.Add(1)
		go ffReader(readPerWorker, minBlk, maxBlk, ff, wg, newStoredData)
	}

	wg.Wait()
}
