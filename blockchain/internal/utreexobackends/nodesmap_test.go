package utreexobackends

import (
	"encoding/binary"
	"testing"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

func TestNodesMapSliceMaxCacheElems(t *testing.T) {
	_, maxCacheElems := NewNodesMapSlice(0)
	if maxCacheElems != 0 {
		t.Fatalf("expected %v got %v", 0, maxCacheElems)
	}

	_, maxCacheElems = NewNodesMapSlice(-1)
	if maxCacheElems != 0 {
		t.Fatalf("expected %v got %v", 0, maxCacheElems)
	}

	_, maxCacheElems = NewNodesMapSlice(8000)
	if maxCacheElems <= 0 {
		t.Fatalf("expected something bigger than 0 but got %v", maxCacheElems)
	}
}

func TestNodesMapSliceDuplicates(t *testing.T) {
	m, maxElems := NewNodesMapSlice(8000)
	for i := 0; i < 10; i++ {
		for j := int64(0); j < maxElems; j++ {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], uint64(j))
			if !m.Put(utreexo.Hash(chainhash.HashH(buf[:])), CachedNode{}) {
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
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], x)
	hash := utreexo.Hash(chainhash.HashH(buf[:]))
	if m.Put(hash, CachedNode{}) {
		t.Fatalf("expected error but successfully called put")
	}

	// Remove the first element in the first map and then try inserting
	// a duplicate element.
	binary.LittleEndian.PutUint64(buf[:], uint64(0))
	firstHash := utreexo.Hash(chainhash.HashH(buf[:]))
	m.Delete(firstHash)

	x = uint64(maxElems) - 1
	binary.LittleEndian.PutUint64(buf[:], x)
	dupHash := utreexo.Hash(chainhash.HashH(buf[:]))
	if !m.Put(dupHash, CachedNode{}) {
		t.Fatalf("unexpected failure on put")
	}

	// Make sure the length of the map is 1 less than the max elems.
	if m.Length()-len(m.overflow) != int(maxElems)-1 {
		t.Fatalf("expected length of %v but got %v",
			maxElems-1, m.Length())
	}

	// Put 0 back in and then compare the map.
	binary.LittleEndian.PutUint64(buf[:], uint64(0))
	zeroHash := utreexo.Hash(chainhash.HashH(buf[:]))
	if !m.Put(zeroHash, CachedNode{}) {
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
