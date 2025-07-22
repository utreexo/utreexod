package utreexobackends

import (
	"container/heap"
	"sync"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain/internal/sizehelper"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

const (
	// Calculated with unsafe.Sizeof(CachedNode{}).
	cachedNodeSize = 108

	// Bucket size for the node map.
	nodesMapBucketSize = 16 + sizehelper.Uint64Size*chainhash.HashSize + sizehelper.Uint64Size*cachedNodeSize
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

// CachedNode has the leaf and a flag for the status in the cache.
type CachedNode struct {
	Node  utreexo.Node
	Flags CachedFlag
}

// IsFresh returns if the node has never been in the database.
func (c *CachedNode) IsFresh() bool {
	return c.Flags&Fresh == Fresh
}

// IsModified returns if the node has been in the database and was modified in the cache.
func (c *CachedNode) IsModified() bool {
	return c.Flags&Modified == Modified
}

// IsRemoved returns if true the key for this cached node has been removed.
func (c *CachedNode) IsRemoved() bool {
	return c.Flags&Removed == Removed
}

// hashHeap is just the slice of the keys in the nodes map.
type hashHeap []utreexo.Hash

func (h hashHeap) Len() int           { return len(h) }
func (h hashHeap) Less(i, j int) bool { return string(h[i][:]) < string(h[j][:]) }
func (h hashHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h hashHeap) View() any {
	if len(h) == 0 {
		return nil
	}
	return h[0]
}
func (h *hashHeap) Push(x any) { *h = append(*h, x.(utreexo.Hash)) }
func (h *hashHeap) Pop() any {
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
	keyPriorityQueue hashHeap

	// maps are the underlying maps in the slice of maps.
	maps []map[utreexo.Hash]CachedNode

	// overflow puts the overflowed entries.
	overflow map[utreexo.Hash]CachedNode

	// maxEntries is the maximum amount of elements that the map is allocated for.
	maxEntries []int

	// maxTotalMemoryUsage is the maximum memory usage in bytes that the state
	// should contain in normal circumstances.
	maxTotalMemoryUsage uint64
}

// popForFlushing returns the key-value pairs in the cache starting from the
// smallest key when serialized.
//
// NOTE: this function should not be used as a general pop() function as the
// priority queue is only built again once all the key-value pairs have been popped.
func (ms *NodesMapSlice) popForFlushing() (utreexo.Hash, *CachedNode) {
	length := ms.length()
	if length == 0 {
		ms.keyPriorityQueue = nil
		return utreexo.Hash{}, nil
	}

	// If the length is 0, it means this is the first time we've called this
	// function and we need to build the priority queue.
	if len(ms.keyPriorityQueue) == 0 {
		ms.keyPriorityQueue = make([]utreexo.Hash, 0, length)
		for _, m := range ms.maps {
			for k := range m {
				ms.keyPriorityQueue.Push(k)
			}
		}

		if len(ms.overflow) > 0 {
			for k := range ms.overflow {
				ms.keyPriorityQueue.Push(k)
			}
		}

		heap.Init(&ms.keyPriorityQueue)
	}

	key := heap.Pop(&ms.keyPriorityQueue).(utreexo.Hash)

	for _, m := range ms.maps {
		v, found := m[key]
		if found {
			delete(m, key)

			return key, &v
		}
	}

	if len(ms.overflow) > 0 {
		v, found := ms.overflow[key]
		if found {
			delete(ms.overflow, key)

			return key, &v
		}
	}

	return utreexo.Hash{}, nil
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
// the entry. nil and false is returned if the node is not found.
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) Get(k utreexo.Hash) (CachedNode, bool) {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	var v CachedNode
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
func (ms *NodesMapSlice) Put(k utreexo.Hash, v CachedNode) bool {
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

// Delete attempts to delete the given hash in all of the maps. No-op if the
// key doesn't exist.
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) Delete(k utreexo.Hash) {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	for i := 0; i < len(ms.maps); i++ {
		delete(ms.maps[i], k)
	}

	delete(ms.overflow, k)
}

// ForEach loops through all the elements in the nodes map slice and calls fn with the key-value pairs.
//
// This function is safe for concurrent access.
func (ms *NodesMapSlice) ForEach(fn func(utreexo.Hash, CachedNode) error) error {
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
func (ms *NodesMapSlice) ForEachAndDelete(fn func(utreexo.Hash, CachedNode) error) error {
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
	ms.maps = make([]map[utreexo.Hash]CachedNode, len(ms.maxEntries))
	for i := range ms.maxEntries {
		ms.maps[i] = make(map[utreexo.Hash]CachedNode, ms.maxEntries[i])
	}

	ms.overflow = make(map[utreexo.Hash]CachedNode)

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
