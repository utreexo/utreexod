package blockchain

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/utreexo/utreexo"
)

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
	tests := []struct {
		maxSize    int64
		bucketSize uintptr
	}{
		{100 * 1024 * 1024, nodesMapBucketSize},
		{150 * 1024 * 1024, nodesMapBucketSize},
		{250 * 1024 * 1024, nodesMapBucketSize},
		{1000 * 1024 * 1024, nodesMapBucketSize},
		{10000 * 1024 * 1024, nodesMapBucketSize},

		{100 * 1024 * 1024, cachedLeavesMapBucketSize},
		{150 * 1024 * 1024, cachedLeavesMapBucketSize},
		{250 * 1024 * 1024, cachedLeavesMapBucketSize},
		{1000 * 1024 * 1024, cachedLeavesMapBucketSize},
		{10000 * 1024 * 1024, cachedLeavesMapBucketSize},
	}

	for _, test := range tests {
		entries, _ := calcNumEntries(test.bucketSize, test.maxSize)

		roughSize := 0
		for _, entry := range entries {
			roughSize += calculateRoughMapSize(entry, nodesMapBucketSize)
		}

		// Check if the roughSize is within 1% of test.maxSize.
		if !isApproximate(float64(test.maxSize), float64(roughSize), 0.01) {
			t.Fatalf("Expected value to be approximately %v but got %v",
				test.maxSize, roughSize)
		}
	}
}

func TestNodesMapSliceMaxCacheElems(t *testing.T) {
	_, maxCacheElems := newNodesMapSlice(0)
	if maxCacheElems != 0 {
		t.Fatalf("expected %v got %v", 0, maxCacheElems)
	}

	_, maxCacheElems = newNodesMapSlice(-1)
	if maxCacheElems != 0 {
		t.Fatalf("expected %v got %v", 0, maxCacheElems)
	}

	_, maxCacheElems = newNodesMapSlice(8000)
	if maxCacheElems <= 0 {
		t.Fatalf("expected something bigger than 0 but got %v", maxCacheElems)
	}

	_, maxCacheElems = newCachedLeavesMapSlice(0)
	if maxCacheElems != 0 {
		t.Fatalf("expected %v got %v", 0, maxCacheElems)
	}

	_, maxCacheElems = newCachedLeavesMapSlice(-1)
	if maxCacheElems != 0 {
		t.Fatalf("expected %v got %v", 0, maxCacheElems)
	}

	_, maxCacheElems = newCachedLeavesMapSlice(8000)
	if maxCacheElems <= 0 {
		t.Fatalf("expected something bigger than 0 but got %v", maxCacheElems)
	}
}

func TestNodesMapSliceDuplicates(t *testing.T) {
	m, maxElems := newNodesMapSlice(8000)
	for i := 0; i < 10; i++ {
		for j := int64(0); j < maxElems; j++ {
			if !m.put(uint64(j), cachedLeaf{}) {
				t.Fatalf("unexpected error on m.put")
			}
		}
	}

	if m.length() != int(maxElems) {
		t.Fatalf("expected length of %v but got %v",
			maxElems, m.length())
	}

	// Try inserting x which should be unique. Should fail as the map is full.
	x := uint64(0)
	x -= 1
	if m.put(x, cachedLeaf{}) {
		t.Fatalf("expected error but successfully called put")
	}

	// Remove the first element in the first map and then try inserting
	// a duplicate element.
	m.delete(0)
	x = uint64(maxElems) - 1
	if !m.put(x, cachedLeaf{}) {
		t.Fatalf("unexpected failure on put")
	}

	// Make sure the length of the map is 1 less than the max elems.
	if m.length() != int(maxElems)-1 {
		t.Fatalf("expected length of %v but got %v",
			maxElems-1, m.length())
	}

	// Put 0 back in and then compare the map.
	if !m.put(0, cachedLeaf{}) {
		t.Fatalf("didn't expect error but unsuccessfully called put")
	}
	if m.length() != int(maxElems) {
		t.Fatalf("expected length of %v but got %v",
			maxElems, m.length())
	}
}

func uint64ToHash(v uint64) utreexo.Hash {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	return sha256.Sum256(buf[:])
}

func TestCachedLeaveMapSliceDuplicates(t *testing.T) {
	m, maxElems := newCachedLeavesMapSlice(8000)
	for i := 0; i < 10; i++ {
		for j := int64(0); j < maxElems; j++ {
			if !m.put(uint64ToHash(uint64(j)), 0) {
				t.Fatalf("unexpected error on m.put")
			}
		}
	}

	if m.length() != int(maxElems) {
		t.Fatalf("expected length of %v but got %v",
			maxElems, m.length())
	}

	// Try inserting x which should be unique. Should fail as the map is full.
	x := uint64(0)
	x -= 1
	if m.put(uint64ToHash(x), 0) {
		t.Fatalf("expected error but successfully called put")
	}

	// Remove the first element in the first map and then try inserting
	// a duplicate element.
	m.delete(uint64ToHash(0))
	x = uint64(maxElems) - 1
	if !m.put(uint64ToHash(x), 0) {
		t.Fatalf("unexpected failure on put")
	}

	// Make sure the length of the map is 1 less than the max elems.
	if m.length() != int(maxElems)-1 {
		t.Fatalf("expected length of %v but got %v",
			maxElems-1, m.length())
	}

	// Put 0 back in and then compare the map.
	if !m.put(uint64ToHash(0), 0) {
		t.Fatalf("didn't expect error but unsuccessfully called put")
	}
	if m.length() != int(maxElems) {
		t.Fatalf("expected length of %v but got %v",
			maxElems, m.length())
	}
}

func TestCachedLeavesBackEnd(t *testing.T) {
	tests := []struct {
		tmpDir      string
		maxMemUsage int64
	}{
		{
			tmpDir: func() string {
				return filepath.Join(os.TempDir(), "TestCachedLeavesBackEnd0")
			}(),
			maxMemUsage: -1,
		},
		{
			tmpDir: func() string {
				return filepath.Join(os.TempDir(), "TestCachedLeavesBackEnd1")
			}(),
			maxMemUsage: 0,
		},
		{
			tmpDir: func() string {
				return filepath.Join(os.TempDir(), "TestCachedLeavesBackEnd2")
			}(),
			maxMemUsage: 1 * 1024 * 1024,
		},
	}

	for _, test := range tests {
		cachedLeavesBackEnd, err := InitCachedLeavesBackEnd(test.tmpDir, test.maxMemUsage)
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(test.tmpDir)

		count := uint64(1000)
		compareMap := make(map[utreexo.Hash]uint64)
		for i := uint64(0); i < count/2; i++ {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])

			compareMap[hash] = i
			cachedLeavesBackEnd.Put(hash, i)
		}

		// Close and reopen the backend.
		err = cachedLeavesBackEnd.Close()
		if err != nil {
			t.Fatal(err)
		}
		cachedLeavesBackEnd, err = InitCachedLeavesBackEnd(test.tmpDir, test.maxMemUsage)
		if err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(1)
		// Delete every other element from the backend that we currently have.
		go func() {
			for i := uint64(0); i < count/2; i++ {
				if i%2 != 0 {
					continue
				}

				var buf [8]byte
				binary.LittleEndian.PutUint64(buf[:], i)
				hash := sha256.Sum256(buf[:])

				cachedLeavesBackEnd.Delete(hash)
			}
			wg.Done()
		}()

		wg.Add(1)
		// Put the rest of the elements into the backend.
		go func() {
			for i := count / 2; i < count; i++ {
				var buf [8]byte
				binary.LittleEndian.PutUint64(buf[:], i)
				hash := sha256.Sum256(buf[:])

				cachedLeavesBackEnd.Put(hash, i)
			}
			wg.Done()
		}()

		wg.Wait()

		// Do the same for the compare map. We do it here because a hashmap
		// isn't concurrency safe.
		for i := uint64(0); i < count/2; i++ {
			if i%2 != 0 {
				continue
			}

			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])
			delete(compareMap, hash)
		}
		for i := count / 2; i < count; i++ {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])

			compareMap[hash] = i
		}

		if cachedLeavesBackEnd.Length() != len(compareMap) {
			t.Fatalf("compareMap has %d elements but the backend has %d elements",
				len(compareMap), cachedLeavesBackEnd.Length())
		}

		// Compare the map and the backend.
		for k, v := range compareMap {
			got, found := cachedLeavesBackEnd.Get(k)
			if !found {
				t.Fatalf("expected %v but it wasn't found", v)
			}

			if got != v {
				var buf [8]byte
				binary.LittleEndian.PutUint64(buf[:], got)
				gotHash := sha256.Sum256(buf[:])

				binary.LittleEndian.PutUint64(buf[:], v)
				expectHash := sha256.Sum256(buf[:])

				if gotHash != expectHash {
					t.Fatalf("for key %v, expected %v but got %v", k.String(), v, got)
				}
			}
		}
	}
}

func TestNodesBackEnd(t *testing.T) {
	tests := []struct {
		tmpDir      string
		maxMemUsage int64
	}{
		{
			tmpDir: func() string {
				return filepath.Join(os.TempDir(), "TestNodesBackEnd0")
			}(),
			maxMemUsage: -1,
		},
		{
			tmpDir: func() string {
				return filepath.Join(os.TempDir(), "TestNodesBackEnd1")
			}(),
			maxMemUsage: 0,
		},
		{
			tmpDir: func() string {
				return filepath.Join(os.TempDir(), "TestNodesBackEnd2")
			}(),
			maxMemUsage: 1 * 1024 * 1024,
		},
	}

	for _, test := range tests {
		nodesBackEnd, err := InitNodesBackEnd(test.tmpDir, test.maxMemUsage)
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(test.tmpDir)

		count := uint64(1000)
		compareMap := make(map[uint64]cachedLeaf)
		for i := uint64(0); i < count/2; i++ {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])

			compareMap[i] = cachedLeaf{leaf: utreexo.Leaf{Hash: hash}}
			nodesBackEnd.Put(i, utreexo.Leaf{Hash: hash})
		}

		// Close and reopen the backend.
		err = nodesBackEnd.Close()
		if err != nil {
			t.Fatal(err)
		}
		nodesBackEnd, err = InitNodesBackEnd(test.tmpDir, test.maxMemUsage)
		if err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(1)
		// Delete every other element from the backend that we currently have.
		go func() {
			for i := uint64(0); i < count/2; i++ {
				if i%2 != 0 {
					continue
				}

				nodesBackEnd.Delete(i)
			}
			wg.Done()
		}()

		wg.Add(1)
		// Put the rest of the elements into the backend.
		go func() {
			for i := count / 2; i < count; i++ {
				var buf [8]byte
				binary.LittleEndian.PutUint64(buf[:], i)
				hash := sha256.Sum256(buf[:])

				nodesBackEnd.Put(i, utreexo.Leaf{Hash: hash})
			}
			wg.Done()
		}()

		wg.Wait()

		// Do the same for the compare map. We do it here because a hashmap
		// isn't concurrency safe.
		for i := uint64(0); i < count/2; i++ {
			if i%2 != 0 {
				continue
			}

			delete(compareMap, i)
		}
		for i := count / 2; i < count; i++ {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])

			compareMap[i] = cachedLeaf{leaf: utreexo.Leaf{Hash: hash}}
		}

		if nodesBackEnd.Length() != len(compareMap) {
			t.Fatalf("compareMap has %d elements but the backend has %d elements",
				len(compareMap), nodesBackEnd.Length())
		}

		// Compare the map and the backend.
		for k, v := range compareMap {
			got, found := nodesBackEnd.Get(k)
			if !found {
				t.Fatalf("expected %v but it wasn't found", v)
			}

			if got.Hash != v.leaf.Hash {
				if got.Hash != v.leaf.Hash {
					t.Fatalf("for key %v, expected %v but got %v", k, v.leaf.Hash, got.Hash)
				}
			}
		}
	}
}
