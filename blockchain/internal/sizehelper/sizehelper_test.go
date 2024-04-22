// Copyright (c) 2023 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package sizehelper

import (
	"math"
	"testing"
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
		roughMapSize := CalculateRoughMapSize(i, UtxoCacheBucketSize)
		entries := calculateEntries(roughMapSize, UtxoCacheBucketSize)
		gotRoughMapSize := CalculateRoughMapSize(entries, UtxoCacheBucketSize)

		if roughMapSize != gotRoughMapSize {
			t.Errorf("For hint of %d, expected %v, got %v\n",
				i, roughMapSize, gotRoughMapSize)
		}

		// Test that the entries returned are the maximum for the given map size.
		// If we increment the entries by one, we should get a bigger map.
		gotRoughMapSizeWrong := CalculateRoughMapSize(entries+1, UtxoCacheBucketSize)
		if roughMapSize == gotRoughMapSizeWrong {
			t.Errorf("For hint %d incremented by 1, expected %v, got %v\n",
				i, gotRoughMapSizeWrong*2, gotRoughMapSizeWrong)
		}

		minEntries := CalculateMinEntries(roughMapSize, UtxoCacheBucketSize)
		gotMinRoughMapSize := CalculateRoughMapSize(minEntries, UtxoCacheBucketSize)
		if roughMapSize != gotMinRoughMapSize {
			t.Errorf("For hint of %d, expected %v, got %v\n",
				i, roughMapSize, gotMinRoughMapSize)
		}

		// Can only test if they'll be half the size if the entries aren't 0.
		if minEntries > 0 {
			gotMinRoughMapSizeWrong := CalculateRoughMapSize(minEntries-1, UtxoCacheBucketSize)
			if gotMinRoughMapSize == gotMinRoughMapSizeWrong {
				t.Errorf("For hint %d decremented by 1, expected %v, got %v\n",
					i, gotRoughMapSizeWrong/2, gotRoughMapSizeWrong)
			}
		}
	}
}

// isApproximate returns if a and b are within the given percentage of each other.
func isApproximate(a, b, percentage float64) bool {
	// Calculate % of 'a'
	percentageOfA := math.Abs(a) * percentage

	// Calculate the absolute difference between 'a' and 'b'
	difference := math.Abs(a - b)

	// Check if the absolute difference is less than or equal to 1% of 'a'
	return difference <= percentageOfA
}

func TestCalcNumEntries(t *testing.T) {
	const (
		nodesMapBucketSize        = 352
		cachedLeavesMapBucketSize = 336
	)
	type teststruct struct {
		maxSize    int64
		bucketSize uintptr
	}

	testGen := func(bSize uintptr) []teststruct {
		ts := make([]teststruct, 0, 100_000)
		for i := int64(0); i < 100_000; i++ {
			ms := i * 1024 * 1024
			ts = append(ts, teststruct{ms, bSize})
		}
		return ts
	}

	tests := make([]teststruct, 0, 300_000)
	tests = append(tests, testGen(nodesMapBucketSize)...)
	tests = append(tests, testGen(cachedLeavesMapBucketSize)...)
	tests = append(tests, testGen(UtxoCacheBucketSize)...)

	for _, test := range tests {
		entries, _ := CalcNumEntries(test.bucketSize, test.maxSize)

		roughSize := 0
		for _, entry := range entries {
			roughSize += CalculateRoughMapSize(entry, test.bucketSize)
		}

		// Check if the roughSize is within 0.1% of test.maxSize.
		if !isApproximate(float64(test.maxSize), float64(roughSize), 0.001) {
			t.Fatalf("Expected value to be approximately %v but got %v",
				test.maxSize, roughSize)
		}
	}
}
