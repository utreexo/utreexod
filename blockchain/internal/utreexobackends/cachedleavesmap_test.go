package utreexobackends

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/utreexo/utreexo"
)

func TestCachedLeavesMapSliceMaxCacheElems(t *testing.T) {
	_, maxCacheElems := NewCachedLeavesMapSlice(0)
	if maxCacheElems != 0 {
		t.Fatalf("expected %v got %v", 0, maxCacheElems)
	}

	_, maxCacheElems = NewCachedLeavesMapSlice(-1)
	if maxCacheElems != 0 {
		t.Fatalf("expected %v got %v", 0, maxCacheElems)
	}

	_, maxCacheElems = NewCachedLeavesMapSlice(8000)
	if maxCacheElems <= 0 {
		t.Fatalf("expected something bigger than 0 but got %v", maxCacheElems)
	}
}

func uint64ToHash(v uint64) utreexo.Hash {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	return sha256.Sum256(buf[:])
}

func TestCachedLeaveMapSliceDuplicates(t *testing.T) {
	m, maxElems := NewCachedLeavesMapSlice(8000)
	for i := 0; i < 10; i++ {
		for j := int64(0); j < maxElems; j++ {
			if !m.Put(uint64ToHash(uint64(j)), CachedPosition{}) {
				t.Fatalf("unexpected error on m.put")
			}
		}
	}

	if m.Length() != int(maxElems) {
		t.Fatalf("expected length of %v but got %v",
			maxElems, m.Length())
	}

	// Try inserting x which should be unique. Should fail as the map is full.
	x := uint64(0)
	x -= 1
	if m.Put(uint64ToHash(x), CachedPosition{}) {
		t.Fatalf("expected error but successfully called put")
	}

	// Remove the first element in the first map and then try inserting
	// a duplicate element.
	m.Delete(uint64ToHash(0))
	x = uint64(maxElems) - 1
	if !m.Put(uint64ToHash(x), CachedPosition{}) {
		t.Fatalf("unexpected failure on put")
	}

	// Make sure the length of the map is 1 less than the max elems.
	if m.Length()-len(m.overflow) != int(maxElems)-1 {
		t.Fatalf("expected length of %v but got %v",
			maxElems-1, m.Length())
	}

	// Put 0 back in and then compare the map.
	if !m.Put(uint64ToHash(0), CachedPosition{}) {
		t.Fatalf("didn't expect error but unsuccessfully called put")
	}
	if m.Length()-len(m.overflow) != int(maxElems) {
		t.Fatalf("expected length of %v but got %v",
			maxElems, m.Length())
	}

	if len(m.overflow) != 1 {
		t.Fatalf("expected length of %v but got %v",
			1, len(m.overflow))
	}
}
