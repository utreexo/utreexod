package utreexobackends

import (
	"sync"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain/internal/sizehelper"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

const (
	// Bucket size for the cached leaves map.
	cachedLeavesMapBucketSize = 16 + sizehelper.Uint64Size*chainhash.HashSize + sizehelper.Uint64Size*sizehelper.Uint64Size
)

// CachedLeavesMapSlice is a slice of maps for utxo entries.  The slice of maps are needed to
// guarantee that the map will only take up N amount of bytes.  As of v1.20, the
// go runtime will allocate 2^N + few extra buckets, meaning that for large N, we'll
// allocate a lot of extra memory if the amount of entries goes over the previously
// allocated buckets.  A slice of maps allows us to have a better control of how much
// total memory gets allocated by all the maps.
type CachedLeavesMapSlice struct {
	// mtx protects against concurrent access for the map slice.
	mtx *sync.Mutex

	// maps are the underlying maps in the slice of maps.
	maps []map[utreexo.Hash]uint64

	// overflow puts the overflowed entries.
	overflow map[utreexo.Hash]uint64

	// maxEntries is the maximum amount of elemnts that the map is allocated for.
	maxEntries []int

	// maxTotalMemoryUsage is the maximum memory usage in bytes that the state
	// should contain in normal circumstances.
	maxTotalMemoryUsage uint64
}

// Length returns the length of all the maps in the map slice added together.
//
// This function is safe for concurrent access.
func (ms *CachedLeavesMapSlice) Length() int {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	var l int
	for _, m := range ms.maps {
		l += len(m)
	}

	l += len(ms.overflow)

	return l
}

// Get looks for the outpoint in all the maps in the map slice and returns
// the entry.  nil and false is returned if the outpoint is not found.
//
// This function is safe for concurrent access.
func (ms *CachedLeavesMapSlice) Get(k utreexo.Hash) (uint64, bool) {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	var v uint64
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

	return 0, false
}

// Put puts the keys and the values into one of the maps in the map slice.  If the
// existing maps are all full and it fails to put the entry in the cache, it will
// return false.
//
// This function is safe for concurrent access.
func (ms *CachedLeavesMapSlice) Put(k utreexo.Hash, v uint64) bool {
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

// Delete attempts to delete the given outpoint in all of the maps. No-op if the
// outpoint doesn't exist.
//
// This function is safe for concurrent access.
func (ms *CachedLeavesMapSlice) Delete(k utreexo.Hash) {
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
func (ms *CachedLeavesMapSlice) DeleteMaps() {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	ms.maps = make([]map[utreexo.Hash]uint64, len(ms.maxEntries))
	for i := range ms.maxEntries {
		ms.maps[i] = make(map[utreexo.Hash]uint64, ms.maxEntries[i])
	}

	ms.overflow = make(map[utreexo.Hash]uint64)
}

// ClearMaps clears all maps
//
// This function is safe for concurrent access.
func (ms *CachedLeavesMapSlice) ClearMaps() {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	for i := range ms.maps {
		for key := range ms.maps[i] {
			delete(ms.maps[i], key)
		}
	}
}

// ForEach loops through all the elements in the cachedleaves map slice and calls fn with the key-value pairs.
//
// This function is safe for concurrent access.
func (ms *CachedLeavesMapSlice) ForEach(fn func(utreexo.Hash, uint64) error) error {
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

// createMaps creates a slice of maps and returns the total count that the maps
// can handle. maxEntries are also set along with the newly created maps.
func (ms *CachedLeavesMapSlice) createMaps(maxMemoryUsage int64) int64 {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	if maxMemoryUsage <= 0 {
		return 0
	}

	// Get the entry count for the maps we'll allocate.
	var totalElemCount int
	ms.maxEntries, totalElemCount = sizehelper.CalcNumEntries(cachedLeavesMapBucketSize, maxMemoryUsage)

	// maxMemoryUsage that's smaller than the minimum map size will return a totalElemCount
	// that's equal to 0.
	if totalElemCount <= 0 {
		return 0
	}

	// Create the maps.
	ms.maps = make([]map[utreexo.Hash]uint64, len(ms.maxEntries))
	for i := range ms.maxEntries {
		ms.maps[i] = make(map[utreexo.Hash]uint64, ms.maxEntries[i])
	}

	ms.overflow = make(map[utreexo.Hash]uint64)

	return int64(totalElemCount)
}

// Overflowed returns true if the map slice overflowed.
func (ms *CachedLeavesMapSlice) Overflowed() bool {
	return len(ms.overflow) > 0
}

// NewCachedLeavesMapSlice returns a new CachedLeavesMapSlice and the total amount of elements
// that the map slice can accomodate.
func NewCachedLeavesMapSlice(maxTotalMemoryUsage int64) (CachedLeavesMapSlice, int64) {
	ms := CachedLeavesMapSlice{
		mtx:                 new(sync.Mutex),
		maxTotalMemoryUsage: uint64(maxTotalMemoryUsage),
	}

	totalCacheElem := ms.createMaps(maxTotalMemoryUsage)
	return ms, totalCacheElem
}
