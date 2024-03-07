// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"fmt"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// leafLength is the length of a seriailzed leaf.
const leafLength = chainhash.HashSize + 1

// serializeLeaf serializes the leaf to [leafLength]byte.
func serializeLeaf(leaf utreexo.Leaf) [leafLength]byte {
	var buf [leafLength]byte
	copy(buf[:chainhash.HashSize], leaf.Hash[:])
	if leaf.Remember {
		buf[32] = 1
	}

	return buf
}

// deserializeLeaf serializes the leaf to [leafLength]byte.
func deserializeLeaf(serialized [leafLength]byte) utreexo.Leaf {
	leaf := utreexo.Leaf{
		Hash: *(*[chainhash.HashSize]byte)(serialized[:32]),
	}
	if serialized[32] == 1 {
		leaf.Remember = true
	}

	return leaf
}

// cachedFlag is the status of each of the cached elements in the NodesBackEnd.
type cachedFlag uint8

const (
	// fresh means it's never been in the database
	fresh cachedFlag = 1 << iota

	// modified means it's been in the database and has been modified in the cache.
	modified

	// removed means that the key it belongs to has been removed but it's still
	// in the cache.
	removed
)

// cachedLeaf has the leaf and a flag for the status in the cache.
type cachedLeaf struct {
	leaf  utreexo.Leaf
	flags cachedFlag
}

// isFresh returns if the cached leaf has never been in the database.
func (c *cachedLeaf) isFresh() bool {
	return c.flags&fresh == fresh
}

// isModified returns if the cached leaf has been in the database and was modified in the cache.
func (c *cachedLeaf) isModified() bool {
	return c.flags&modified == modified
}

// isRemoved returns if the key for this cached leaf has been removed.
func (c *cachedLeaf) isRemoved() bool {
	return c.flags&removed == removed
}

const (
	// Calculated with unsafe.Sizeof(cachedLeaf{}).
	cachedLeafSize = 34

	// Bucket size for the node map.
	nodesMapBucketSize = 16 + uint64Size*uint64Size + uint64Size*cachedLeafSize

	// Bucket size for the cached leaves map.
	cachedLeavesMapBucketSize = 16 + uint64Size*chainhash.HashSize + uint64Size*uint64Size
)

// nodesMapSlice is a slice of maps for utxo entries.  The slice of maps are needed to
// guarantee that the map will only take up N amount of bytes.  As of v1.20, the
// go runtime will allocate 2^N + few extra buckets, meaning that for large N, we'll
// allocate a lot of extra memory if the amount of entries goes over the previously
// allocated buckets.  A slice of maps allows us to have a better control of how much
// total memory gets allocated by all the maps.
type nodesMapSlice struct {
	// mtx protects against concurrent access for the map slice.
	mtx *sync.Mutex

	// maps are the underlying maps in the slice of maps.
	maps []map[uint64]cachedLeaf

	// maxEntries is the maximum amount of elemnts that the map is allocated for.
	maxEntries []int

	// maxTotalMemoryUsage is the maximum memory usage in bytes that the state
	// should contain in normal circumstances.
	maxTotalMemoryUsage uint64
}

// length returns the length of all the maps in the map slice added together.
//
// This function is safe for concurrent access.
func (ms *nodesMapSlice) length() int {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	var l int
	for _, m := range ms.maps {
		l += len(m)
	}

	return l
}

// get looks for the outpoint in all the maps in the map slice and returns
// the entry. nil and false is returned if the outpoint is not found.
//
// This function is safe for concurrent access.
func (ms *nodesMapSlice) get(k uint64) (cachedLeaf, bool) {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	var v cachedLeaf
	var found bool

	for _, m := range ms.maps {
		v, found = m[k]
		if found {
			return v, found
		}
	}

	return v, found
}

// put puts the keys and the values into one of the maps in the map slice.  If the
// existing maps are all full and it fails to put the entry in the cache, it will
// return false.
//
// This function is safe for concurrent access.
func (ms *nodesMapSlice) put(k uint64, v cachedLeaf) bool {
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

	// We only reach this code if we've failed to insert into the map above as
	// all the current maps were full.
	return false
}

// delete attempts to delete the given outpoint in all of the maps. No-op if the
// key doesn't exist.
//
// This function is safe for concurrent access.
func (ms *nodesMapSlice) delete(k uint64) {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	for i := 0; i < len(ms.maps); i++ {
		delete(ms.maps[i], k)
	}
}

// deleteMaps deletes all maps and allocate new ones with the maxEntries defined in
// ms.maxEntries.
//
// This function is safe for concurrent access.
func (ms *nodesMapSlice) deleteMaps() {
	for i := range ms.maxEntries {
		ms.maps[i] = make(map[uint64]cachedLeaf, ms.maxEntries[i])
	}
}

// calcNumEntries returns a list of ints that represent how much entries a map
// should allocate for to stay under the maxMemoryUsage and an int that's a sum
// of the returned list of ints.
func calcNumEntries(bucketSize uintptr, maxMemoryUsage int64) ([]int, int) {
	entries := []int{}

	totalElemCount := 0
	totalMapSize := int64(0)
	for maxMemoryUsage > totalMapSize {
		numMaxElements := calculateMinEntries(int(maxMemoryUsage-totalMapSize), nodesMapBucketSize)
		if numMaxElements == 0 {
			break
		}

		mapSize := int64(calculateRoughMapSize(numMaxElements, nodesMapBucketSize))
		if maxMemoryUsage <= totalMapSize+mapSize {
			break
		}
		totalMapSize += mapSize

		entries = append(entries, numMaxElements)
		totalElemCount += numMaxElements
	}

	return entries, totalElemCount
}

// createMaps creates a slice of maps and returns the total count that the maps
// can handle. maxEntries are also set along with the newly created maps.
func (ms *nodesMapSlice) createMaps(maxMemoryUsage int64) int64 {
	if maxMemoryUsage <= 0 {
		return 0
	}

	// Get the entry count for the maps we'll allocate.
	var totalElemCount int
	ms.maxEntries, totalElemCount = calcNumEntries(nodesMapBucketSize, maxMemoryUsage)

	// maxMemoryUsage that's smaller than the minimum map size will return a totalElemCount
	// that's equal to 0.
	if totalElemCount <= 0 {
		return 0
	}

	// Create the maps.
	ms.maps = make([]map[uint64]cachedLeaf, len(ms.maxEntries))
	for i := range ms.maxEntries {
		ms.maps[i] = make(map[uint64]cachedLeaf, ms.maxEntries[i])
	}

	return int64(totalElemCount)
}

// newNodesMapSlice returns a newNodesMapSlice and the total amount of elements
// that the map slice can accomodate.
func newNodesMapSlice(maxTotalMemoryUsage int64) (nodesMapSlice, int64) {
	ms := nodesMapSlice{
		mtx:                 new(sync.Mutex),
		maxTotalMemoryUsage: uint64(maxTotalMemoryUsage),
	}

	totalCacheElem := ms.createMaps(maxTotalMemoryUsage)
	return ms, totalCacheElem
}

var _ utreexo.NodesInterface = (*NodesBackEnd)(nil)

// NodesBackEnd implements the NodesInterface interface. It's really just the database.
type NodesBackEnd struct {
	db *leveldb.DB
}

// InitNodesBackEnd returns a newly initialized NodesBackEnd which implements
// utreexo.NodesInterface.
func InitNodesBackEnd(datadir string) (*NodesBackEnd, error) {
	db, err := leveldb.OpenFile(datadir, nil)
	if err != nil {
		return nil, err
	}

	return &NodesBackEnd{db: db}, nil
}

// Get returns the leaf from the underlying map.
func (m *NodesBackEnd) Get(k uint64) (utreexo.Leaf, bool) {
	size := serializeSizeVLQ(k)
	buf := make([]byte, size)
	putVLQ(buf, k)

	val, err := m.db.Get(buf[:], nil)
	if err != nil {
		return utreexo.Leaf{}, false
	}

	// Must be leafLength bytes long.
	if len(val) != leafLength {
		return utreexo.Leaf{}, false
	}

	return deserializeLeaf(*(*[leafLength]byte)(val)), true
}

// Put puts the given position and the leaf to the underlying map.
func (m *NodesBackEnd) Put(k uint64, v utreexo.Leaf) {
	size := serializeSizeVLQ(k)
	buf := make([]byte, size)
	putVLQ(buf, k)

	serialized := serializeLeaf(v)
	m.db.Put(buf, serialized[:], nil)
}

// Delete removes the given key from the underlying map. No-op if the key
// doesn't exist.
func (m *NodesBackEnd) Delete(k uint64) {
	size := serializeSizeVLQ(k)
	buf := make([]byte, size)
	putVLQ(buf, k)

	m.db.Delete(buf, nil)
}

// Length returns the amount of items in the underlying database.
func (m *NodesBackEnd) Length() int {
	length := 0
	iter := m.db.NewIterator(nil, nil)
	for iter.Next() {
		length++
	}
	iter.Release()

	return length
}

// ForEach calls the given function for each of the elements in the underlying map.
func (m *NodesBackEnd) ForEach(fn func(uint64, utreexo.Leaf) error) error {
	iter := m.db.NewIterator(nil, nil)
	for iter.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		k, _ := deserializeVLQ(iter.Key())

		value := iter.Value()
		if len(value) != leafLength {
			return fmt.Errorf("expected value of length %v but got %v",
				leafLength, len(value))
		}
		v := deserializeLeaf(*(*[leafLength]byte)(value))

		err := fn(k, v)
		if err != nil {
			return err
		}
	}
	iter.Release()
	return iter.Error()
}

// Close closes the underlying database.
func (m *NodesBackEnd) Close() error {
	return m.db.Close()
}

// cachedLeavesMapSlice is a slice of maps for utxo entries.  The slice of maps are needed to
// guarantee that the map will only take up N amount of bytes.  As of v1.20, the
// go runtime will allocate 2^N + few extra buckets, meaning that for large N, we'll
// allocate a lot of extra memory if the amount of entries goes over the previously
// allocated buckets.  A slice of maps allows us to have a better control of how much
// total memory gets allocated by all the maps.
type cachedLeavesMapSlice struct {
	// mtx protects against concurrent access for the map slice.
	mtx *sync.Mutex

	// maps are the underlying maps in the slice of maps.
	maps []map[utreexo.Hash]uint64

	// maxEntries is the maximum amount of elemnts that the map is allocated for.
	maxEntries []int

	// maxTotalMemoryUsage is the maximum memory usage in bytes that the state
	// should contain in normal circumstances.
	maxTotalMemoryUsage uint64
}

// length returns the length of all the maps in the map slice added together.
//
// This function is safe for concurrent access.
func (ms *cachedLeavesMapSlice) length() int {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	var l int
	for _, m := range ms.maps {
		l += len(m)
	}

	return l
}

// get looks for the outpoint in all the maps in the map slice and returns
// the entry.  nil and false is returned if the outpoint is not found.
//
// This function is safe for concurrent access.
func (ms *cachedLeavesMapSlice) get(k utreexo.Hash) (uint64, bool) {
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

	return 0, false
}

// put puts the keys and the values into one of the maps in the map slice.  If the
// existing maps are all full and it fails to put the entry in the cache, it will
// return false.
//
// This function is safe for concurrent access.
func (ms *cachedLeavesMapSlice) put(k utreexo.Hash, v uint64) bool {
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

	// We only reach this code if we've failed to insert into the map above as
	// all the current maps were full.
	return false
}

// delete attempts to delete the given outpoint in all of the maps. No-op if the
// outpoint doesn't exist.
//
// This function is safe for concurrent access.
func (ms *cachedLeavesMapSlice) delete(k utreexo.Hash) {
	ms.mtx.Lock()
	defer ms.mtx.Unlock()

	for i := 0; i < len(ms.maps); i++ {
		delete(ms.maps[i], k)
	}
}

// createMaps creates a slice of maps and returns the total count that the maps
// can handle. maxEntries are also set along with the newly created maps.
func (ms *cachedLeavesMapSlice) createMaps(maxMemoryUsage int64) int64 {
	if maxMemoryUsage <= 0 {
		return 0
	}

	// Get the entry count for the maps we'll allocate.
	var totalElemCount int
	ms.maxEntries, totalElemCount = calcNumEntries(nodesMapBucketSize, maxMemoryUsage)

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

	return int64(totalElemCount)
}

// newCachedLeavesMapSlice returns a newCachedLeavesMapSlice and the total amount of elements
// that the map slice can accomodate.
func newCachedLeavesMapSlice(maxTotalMemoryUsage int64) (cachedLeavesMapSlice, int64) {
	ms := cachedLeavesMapSlice{
		mtx:                 new(sync.Mutex),
		maxTotalMemoryUsage: uint64(maxTotalMemoryUsage),
	}

	totalCacheElem := ms.createMaps(maxTotalMemoryUsage)
	return ms, totalCacheElem
}

var _ utreexo.CachedLeavesInterface = (*CachedLeavesBackEnd)(nil)

// CachedLeavesBackEnd implements the CachedLeavesInterface interface. It's really just a map.
type CachedLeavesBackEnd struct {
	db *leveldb.DB
}

// InitCachedLeavesBackEnd returns a newly initialized CachedLeavesBackEnd which implements
// utreexo.CachedLeavesInterface.
func InitCachedLeavesBackEnd(datadir string) (*CachedLeavesBackEnd, error) {
	db, err := leveldb.OpenFile(datadir, nil)
	if err != nil {
		return nil, err
	}

	return &CachedLeavesBackEnd{db: db}, nil
}

// Get returns the data from the underlying map.
func (m *CachedLeavesBackEnd) Get(k utreexo.Hash) (uint64, bool) {
	val, err := m.db.Get(k[:], nil)
	if err != nil {
		return 0, false
	}

	pos, _ := deserializeVLQ(val)
	return pos, true
}

// Put puts the given data to the underlying map.
func (m *CachedLeavesBackEnd) Put(k utreexo.Hash, v uint64) {
	size := serializeSizeVLQ(v)
	buf := make([]byte, size)
	putVLQ(buf, v)

	m.db.Put(k[:], buf, nil)
}

// Delete removes the given key from the underlying map. No-op if the key
// doesn't exist.
func (m *CachedLeavesBackEnd) Delete(k utreexo.Hash) {
	m.db.Delete(k[:], nil)
}

// Length returns the amount of items in the underlying db.
func (m *CachedLeavesBackEnd) Length() int {
	length := 0
	iter := m.db.NewIterator(nil, nil)
	for iter.Next() {
		length++
	}
	iter.Release()

	return length
}

// ForEach calls the given function for each of the elements in the underlying map.
func (m *CachedLeavesBackEnd) ForEach(fn func(utreexo.Hash, uint64) error) error {
	iter := m.db.NewIterator(nil, nil)
	for iter.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		k := iter.Key()
		v, _ := deserializeVLQ(iter.Value())

		err := fn(*(*[chainhash.HashSize]byte)(k), v)
		if err != nil {
			return err
		}
	}
	iter.Release()
	return iter.Error()
}

// Close closes the underlying database.
func (m *CachedLeavesBackEnd) Close() error {
	return m.db.Close()
}
