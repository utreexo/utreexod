package utreexobackends

import "testing"

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
			if !m.Put(uint64(j), CachedLeaf{}) {
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
	if m.Put(x, CachedLeaf{}) {
		t.Fatalf("expected error but successfully called put")
	}

	// Remove the first element in the first map and then try inserting
	// a duplicate element.
	m.delete(0)
	x = uint64(maxElems) - 1
	if !m.Put(x, CachedLeaf{}) {
		t.Fatalf("unexpected failure on put")
	}

	// Make sure the length of the map is 1 less than the max elems.
	if m.Length() != int(maxElems)-1 {
		t.Fatalf("expected length of %v but got %v",
			maxElems-1, m.Length())
	}

	// Put 0 back in and then compare the map.
	if !m.Put(0, CachedLeaf{}) {
		t.Fatalf("didn't expect error but unsuccessfully called put")
	}
	if m.Length() != int(maxElems) {
		t.Fatalf("expected length of %v but got %v",
			maxElems, m.Length())
	}
}
