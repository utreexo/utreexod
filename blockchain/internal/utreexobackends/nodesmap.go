package utreexobackends

import (
	"container/heap"
	"sync"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain/internal/sizehelper"
)

const (
	// Calculated with unsafe.Sizeof(cachedLeaf{}).
	cachedLeafSize = 34

	// Bucket size for the node map.
	nodesMapBucketSize = 16 + sizehelper.Uint64Size*sizehelper.Uint64Size + sizehelper.Uint64Size*cachedLeafSize
)

// CachedFlag is the status of each of the cached elements in the NodesBackEnd.
type CachedFlag uint8

const (
	// Fresh means it's never been in the database
	Fresh CachedFlag = 1 << iota

	// Modified means it's been in the database and has been modified in the cache.
	Modified

	// Removed means that the key it belongs to has been removed but it's still
	// in the cache.
	Removed
)

// CachedLeaf has the leaf and a flag for the status in the cache.
type CachedLeaf struct {
	Leaf  utreexo.Leaf
	Flags CachedFlag
}

// IsFresh returns if the cached leaf has never been in the database.
func (c *CachedLeaf) IsFresh() bool {
	return c.Flags&Fresh == Fresh
}

// IsModified returns if the cached leaf has been in the database and was modified in the cache.
func (c *CachedLeaf) IsModified() bool {
	return c.Flags&Modified == Modified
}

// IsRemoved returns if the key for this cached leaf has been removed.
func (c *CachedLeaf) IsRemoved() bool {
	return c.Flags&Removed == Removed
}

// intHeap is just the slice of the keys in the nodes map.
type intHeap []uint64

func (h intHeap) Len() int           { return len(h) }
func (h intHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h intHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h intHeap) View() any {
	if len(h) == 0 {
		return nil
	}
	return h[0]
}
func (h *intHeap) Push(x any) { *h = append(*h, x.(uint64)) }
func (h *intHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// NodesMapSlice is a slice of maps for utxo entries.  The slice of maps are needed to
// guarantee that the map will only take up N amount of bytes.  As of v1.20, the
// go runtime will allocate 2^N + few extra buckets, meaning that for large N, we'll
// allocate a lot of extra memory if the amount of entries goes over the previously
// allocated buckets.  A slice of maps allows us to have a better control of how much
// total memory gets allocated by all the maps.
type NodesMapSlice struct {
	// mtx protects against concurrent access for the map slice.
	mtx *sync.Mutex

	// keyPriorityQueue keeps all the keys in a priority queue so that we're able
	// to support the Pop() functionality.
	keyPriorityQueue intHeap

	// maps are the underlying maps in the slice of maps.
	maps []map[uint64]CachedLeaf

	// overflow puts the overflowed entries.
	overflow map[uint64]CachedLeaf

	// maxEntries is the maximum amount of elements that the map is allocated for.
	maxEntries []int

	// maxTotalMemoryUsage is the maximum memory usage in bytes that the state
	// should contain in normal circumstances.
	maxTotalMemoryUsage uint64
}

// Length returns the length of all the maps in the map slice added together.
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) Length() int {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	return ms.length()
}

// length returns the length of all the maps in the map slice added together.
//
// This function is NOT safe for concurrent access.
func (ms *NodesMapSlice) length() int {
	var l int
	for _, m := range ms.maps {
		l += len(m)
	}

	l += len(ms.overflow)

	return l
}

// get looks for the outpoint in all the maps in the map slice and returns
// the entry. nil and false is returned if the outpoint is not found.
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) Get(k uint64) (CachedLeaf, bool) {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	var v CachedLeaf
	var found bool

	for _, m := range ms.maps {
		v, found = m[k]
		if found {
			return v, found
		}
	}

	if len(ms.overflow) > 0 {
		v, found = ms.overflow[k]
		if found {
			return v, found
		}
	}

	return v, false
}

// put puts the keys and the values into one of the maps in the map slice.  If the
// existing maps are all full and it fails to put the entry in the cache, it will
// return false.
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) Put(k uint64, v CachedLeaf) bool {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	for i := range ms.maxEntries {
		m := ms.maps[i]
		_, found := m[k]
		if found {
			m[k] = v
			return true
		}
	}

	if len(ms.overflow) > 0 {
		_, found := ms.overflow[k]
		if found {
			ms.overflow[k] = v
			return true
		}
	}

	for i, maxNum := range ms.maxEntries {
		m := ms.maps[i]
		if len(m) >= maxNum {
			// Don't try to insert if the map already at max since
			// that'll force the map to allocate double the memory it's
			// currently taking up.
			continue
		}

		m[k] = v
		return true // Return as we were successful in adding the entry.
	}

	ms.overflow[k] = v

	// We only reach this code if we've failed to insert into the map above as
	// all the current maps were full.
	return false
}

// delete attempts to delete the given outpoint in all of the maps. No-op if the
// key doesn't exist.
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) delete(k uint64) {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	for i := 0; i < len(ms.maps); i++ {
		delete(ms.maps[i], k)
	}

	delete(ms.overflow, k)
}

// DeleteMaps deletes all maps and allocate new ones with the maxEntries defined in
// ms.maxEntries.
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) DeleteMaps() {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	for i := range ms.maxEntries {
		ms.maps[i] = make(map[uint64]CachedLeaf, ms.maxEntries[i])
	}

	ms.overflow = make(map[uint64]CachedLeaf)
}

// ClearMaps clears all maps
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) ClearMaps() {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	for i := range ms.maps {
		for key := range ms.maps[i] {
			delete(ms.maps[i], key)
		}
	}

	for key := range ms.overflow {
		delete(ms.overflow, key)
	}
}

// ForEach loops through all the elements in the nodes map slice and calls fn with the key-value pairs.
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) ForEach(fn func(uint64, CachedLeaf) error) error {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	for _, m := range ms.maps {
		for k, v := range m {
			err := fn(k, v)
			if err != nil {
				return err
			}
		}
	}

	if len(ms.overflow) > 0 {
		for k, v := range ms.overflow {
			err := fn(k, v)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// ForEachAndDelete loops through all the elements in the nodes map slice and calls fn with the key-value pairs and deletes that key.
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) ForEachAndDelete(fn func(uint64, CachedLeaf) error) error {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	for _, m := range ms.maps {
		for k, v := range m {
			err := fn(k, v)
			if err != nil {
				return err
			}

			delete(m, k)
		}
	}

	if len(ms.overflow) > 0 {
		for k, v := range ms.overflow {
			err := fn(k, v)
			if err != nil {
				return err
			}

			delete(ms.overflow, k)
		}
	}

	return nil
}

// createMaps creates a slice of maps and returns the total count that the maps
// can handle. maxEntries are also set along with the newly created maps.
func (ms *NodesMapSlice) createMaps(maxMemoryUsage int64) int64 {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	if maxMemoryUsage <= 0 {
		return 0
	}

	// Get the entry count for the maps we'll allocate.
	var totalElemCount int
	ms.maxEntries, totalElemCount = sizehelper.CalcNumEntries(nodesMapBucketSize, maxMemoryUsage)

	// maxMemoryUsage that's smaller than the minimum map size will return a totalElemCount
	// that's equal to 0.
	if totalElemCount <= 0 {
		return 0
	}

	// Create the maps.
	ms.maps = make([]map[uint64]CachedLeaf, len(ms.maxEntries))
	for i := range ms.maxEntries {
		ms.maps[i] = make(map[uint64]CachedLeaf, ms.maxEntries[i])
	}

	ms.overflow = make(map[uint64]CachedLeaf)

	return int64(totalElemCount)
}

// Overflowed returns true if the map slice overflowed.
func (ms *NodesMapSlice) Overflowed() bool {
	return len(ms.overflow) > 0
}

// NewNodesMapSlice returns a new NodesMapSlice and the total amount of elements
// that the map slice can accomodate.
func NewNodesMapSlice(maxTotalMemoryUsage int64) (NodesMapSlice, int64) {
	ms := NodesMapSlice{
		mtx:                 new(sync.Mutex),
		maxTotalMemoryUsage: uint64(maxTotalMemoryUsage),
	}

	totalCacheElem := ms.createMaps(maxTotalMemoryUsage)
	return ms, totalCacheElem
}
