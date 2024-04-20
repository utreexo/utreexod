// Copyright (c) 2023 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package sizehelper

import (
	"math"
	"testing"
)

const (
	// outpointSize is the size of an outpoint.
	//
	// This value is calculated by running the following:
	//	unsafe.Sizeof(wire.OutPoint{})
	outpointSize = 36

	// uint64Size is the size of an uint64 allocated in memory.
	uint64Size = 8

	// bucketSize is the size of the bucket in the cache map.  Exact
	// calculation is (16 + keysize*8 + valuesize*8) where for the map of:
	// map[wire.OutPoint]*UtxoEntry would have a keysize=36 and valuesize=8.
	//
	// https://github.com/golang/go/issues/34561#issuecomment-536115805
	bucketSize = 16 + uint64Size*outpointSize + uint64Size*uint64Size
)

// calculateEntries returns a number of entries that will make the map allocate
// the given total bytes.  The returned number is always the maximum number of
// entries that will allocate inside the given parameters.
func calculateEntries(totalBytes int, bucketSize int) int {
	// 48 is the number of bytes needed for the map header in a
	// 64 bit system. Refer to hmap in runtime/map.go in the go
	// standard library.
	totalBytes -= 48

	numBuckets := totalBytes / bucketSize
	B := uint8(math.Log2(float64(numBuckets)))
	if B == 0 {
		// For 0 buckets, the max is the bucket count.
		return bucketCnt
	}

	return int(loadFactorNum * (bucketShift(B) / loadFactorDen))
}

func TestCalculateEntries(t *testing.T) {
	for i := 0; i < 10_000_000; i++ {
		// It's not possible to calculate the exact amount of entries since
		// the map will only allocate for 2^N where N is the amount of buckets.
		//
		// So to see if the calculate entries function is working correctly,
		// we get the rough map size for i entries, then calculate the entries
		// for that map size.  If the size is the same, the function is correct.
		roughMapSize := CalculateRoughMapSize(i, bucketSize)
		entries := calculateEntries(roughMapSize, bucketSize)
		gotRoughMapSize := CalculateRoughMapSize(entries, bucketSize)

		if roughMapSize != gotRoughMapSize {
			t.Errorf("For hint of %d, expected %v, got %v\n",
				i, roughMapSize, gotRoughMapSize)
		}

		// Test that the entries returned are the maximum for the given map size.
		// If we increment the entries by one, we should get a bigger map.
		gotRoughMapSizeWrong := CalculateRoughMapSize(entries+1, bucketSize)
		if roughMapSize == gotRoughMapSizeWrong {
			t.Errorf("For hint %d incremented by 1, expected %v, got %v\n",
				i, gotRoughMapSizeWrong*2, gotRoughMapSizeWrong)
		}

		minEntries := CalculateMinEntries(roughMapSize, bucketSize)
		gotMinRoughMapSize := CalculateRoughMapSize(minEntries, bucketSize)
		if roughMapSize != gotMinRoughMapSize {
			t.Errorf("For hint of %d, expected %v, got %v\n",
				i, roughMapSize, gotMinRoughMapSize)
		}

		// Can only test if they'll be half the size if the entries aren't 0.
		if minEntries > 0 {
			gotMinRoughMapSizeWrong := CalculateRoughMapSize(minEntries-1, bucketSize)
			if gotMinRoughMapSize == gotMinRoughMapSizeWrong {
				t.Errorf("For hint %d decremented by 1, expected %v, got %v\n",
					i, gotRoughMapSizeWrong/2, gotRoughMapSizeWrong)
			}
		}
	}
}
