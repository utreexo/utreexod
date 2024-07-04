// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"fmt"
	"math"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain/internal/utreexobackends"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

// leafLength is the length of a seriailzed leaf.
const leafLength = chainhash.HashSize + 1

// buffer size for VLQ serialization.  Double the size needed to serialize 2^64
const vlqBufSize = 22

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

var _ utreexo.NodesInterface = (*NodesBackEnd)(nil)

// NodesBackEnd implements the NodesInterface interface.
type NodesBackEnd struct {
	db           *leveldb.DB
	maxCacheElem int64
	cache        utreexobackends.NodesMapSlice
}

// InitNodesBackEnd returns a newly initialized NodesBackEnd which implements
// utreexo.NodesInterface.
func InitNodesBackEnd(db *leveldb.DB, maxTotalMemoryUsage int64) (*NodesBackEnd, error) {
	cache, maxCacheElems := utreexobackends.NewNodesMapSlice(maxTotalMemoryUsage)
	nb := NodesBackEnd{
		db:           db,
		maxCacheElem: maxCacheElems,
		cache:        cache,
	}

	return &nb, nil
}

// dbPut serializes and puts the key value pair into the database.
func (m *NodesBackEnd) dbPut(k uint64, v utreexo.Leaf) error {
	var buf [vlqBufSize]byte
	size := putVLQ(buf[:], k)

	serialized := serializeLeaf(v)
	return m.db.Put(buf[:size], serialized[:], nil)
}

// dbGet fetches the value from the database and deserializes it and returns
// the leaf value and a boolean for whether or not it was successful.
func (m *NodesBackEnd) dbGet(k uint64) (utreexo.Leaf, bool) {
	size := serializeSizeVLQ(k)
	buf := make([]byte, size)
	putVLQ(buf, k)

	val, err := m.db.Get(buf, nil)
	if err != nil {
		return utreexo.Leaf{}, false
	}
	// Must be leafLength bytes long.
	if len(val) != leafLength {
		return utreexo.Leaf{}, false
	}

	leaf := deserializeLeaf(*(*[leafLength]byte)(val))
	return leaf, true
}

// dbDel removes the key from the database.
func (m *NodesBackEnd) dbDel(k uint64) error {
	var buf [vlqBufSize]byte
	size := putVLQ(buf[:], k)
	return m.db.Delete(buf[:size], nil)
}

// Get returns the leaf from the underlying map.
func (m *NodesBackEnd) Get(k uint64) (utreexo.Leaf, bool) {
	if m.maxCacheElem == 0 {
		return m.dbGet(k)
	}

	// Look it up on the cache first.
	cLeaf, found := m.cache.Get(k)
	if found {
		// The leaf might not have been cleaned up yet.
		if cLeaf.IsRemoved() {
			return utreexo.Leaf{}, false
		}

		// If the cache is full, flush the cache then Put
		// the leaf in.
		if !m.cache.Put(k, cLeaf) {
			m.Flush()
			m.cache.Put(k, cLeaf)
		}

		// If we found it, return here.
		return cLeaf.Leaf, true
	}

	// Since it's not in the cache, look it up in the database.
	leaf, found := m.dbGet(k)
	if !found {
		// If it's not in the database and the cache, it
		// doesn't exist.
		return utreexo.Leaf{}, false
	}

	// Cache the leaf before returning it.
	if !m.cache.Put(k, utreexobackends.CachedLeaf{Leaf: leaf}) {
		m.Flush()
		m.cache.Put(k, utreexobackends.CachedLeaf{Leaf: leaf})
	}
	return leaf, true
}

// Put puts the given position and the leaf to the underlying map.
func (m *NodesBackEnd) Put(k uint64, v utreexo.Leaf) {
	if m.maxCacheElem == 0 {
		err := m.dbPut(k, v)
		if err != nil {
			log.Warnf("NodesBackEnd dbPut fail. %v", err)
		}

		return
	}

	if int64(m.cache.Length()) > m.maxCacheElem {
		m.Flush()
	}

	leaf, found := m.cache.Get(k)
	if found {
		leaf.Flags &^= utreexobackends.Removed
		l := utreexobackends.CachedLeaf{
			Leaf:  v,
			Flags: leaf.Flags | utreexobackends.Modified,
		}

		// It shouldn't fail here but handle it anyways.
		if !m.cache.Put(k, l) {
			m.Flush()
			m.cache.Put(k, l)
		}
	} else {
		// If the key isn't found, mark it as fresh.
		l := utreexobackends.CachedLeaf{
			Leaf:  v,
			Flags: utreexobackends.Fresh,
		}

		// It shouldn't fail here but handle it anyways.
		if !m.cache.Put(k, l) {
			m.Flush()
			m.cache.Put(k, l)
		}
	}
}

// Delete removes the given key from the underlying map. No-op if the key
// doesn't exist.
func (m *NodesBackEnd) Delete(k uint64) {
	if m.maxCacheElem == 0 {
		err := m.dbDel(k)
		if err != nil {
			log.Warnf("NodesBackEnd dbDel fail. %v", err)
		}

		return
	}

	leaf, found := m.cache.Get(k)
	if !found {
		if int64(m.cache.Length()) >= m.maxCacheElem {
			m.Flush()
		}
	}
	l := utreexobackends.CachedLeaf{
		Leaf:  leaf.Leaf,
		Flags: leaf.Flags | utreexobackends.Removed,
	}
	if !m.cache.Put(k, l) {
		m.Flush()
		m.cache.Put(k, l)
	}
}

// Length returns the amount of items in the underlying database.
func (m *NodesBackEnd) Length() int {
	m.Flush()

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
	m.Flush()

	iter := m.db.NewIterator(nil, nil)
	for iter.Next() {
		// If the itered key is chainhash.HashSize, it means that the entry is for nodesbackend.
		// Skip it since it's not relevant here.
		if len(iter.Key()) == 32 {
			continue
		}
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

// flush saves all the cached entries to disk and resets the cache map.
func (m *NodesBackEnd) Flush() {
	if m.maxCacheElem == 0 {
		return
	}

	m.cache.ForEach(func(k uint64, v utreexobackends.CachedLeaf) {
		if v.IsRemoved() {
			err := m.dbDel(k)
			if err != nil {
				log.Warnf("NodesBackEnd flush error. %v", err)
			}
		} else if v.IsFresh() || v.IsModified() {
			err := m.dbPut(k, v.Leaf)
			if err != nil {
				log.Warnf("NodesBackEnd flush error. %v", err)
			}
		}
	})

	m.cache.ClearMaps()
}

var _ utreexo.CachedLeavesInterface = (*CachedLeavesBackEnd)(nil)

// CachedLeavesBackEnd implements the CachedLeavesInterface interface. The cache assumes
// that anything in the cache doesn't exist in the db and vise-versa.
type CachedLeavesBackEnd struct {
	db           *leveldb.DB
	maxCacheElem int64
	cache        utreexobackends.CachedLeavesMapSlice
}

// dbPut serializes and puts the key and the value into the database.
func (m *CachedLeavesBackEnd) dbPut(k utreexo.Hash, v uint64) error {
	var buf [vlqBufSize]byte
	size := putVLQ(buf[:], v)
	return m.db.Put(k[:], buf[:size], nil)
}

// dbGet fetches and deserializes the value from the database.
func (m *CachedLeavesBackEnd) dbGet(k utreexo.Hash) (uint64, bool) {
	val, err := m.db.Get(k[:], nil)
	if err != nil {
		return 0, false
	}
	pos, _ := deserializeVLQ(val)

	return pos, true
}

// InitCachedLeavesBackEnd returns a newly initialized CachedLeavesBackEnd which implements
// utreexo.CachedLeavesInterface.
func InitCachedLeavesBackEnd(db *leveldb.DB, maxMemoryUsage int64) (*CachedLeavesBackEnd, error) {
	cache, maxCacheElem := utreexobackends.NewCachedLeavesMapSlice(maxMemoryUsage)
	return &CachedLeavesBackEnd{maxCacheElem: maxCacheElem, db: db, cache: cache}, nil
}

// Get returns the data from the underlying cache or the database.
func (m *CachedLeavesBackEnd) Get(k utreexo.Hash) (uint64, bool) {
	if m.maxCacheElem == 0 {
		return m.dbGet(k)
	}

	pos, found := m.cache.Get(k)
	if !found {
		return m.dbGet(k)
	}
	// Even if the entry was found, if the position value is math.MaxUint64,
	// then it was already deleted.
	if pos == math.MaxUint64 {
		return 0, false
	}

	return pos, found
}

// Put puts the given data to the underlying cache. If the cache is full, it evicts
// the earliest entries to make room.
func (m *CachedLeavesBackEnd) Put(k utreexo.Hash, v uint64) {
	if m.maxCacheElem == 0 {
		err := m.dbPut(k, v)
		if err != nil {
			log.Warnf("CachedLeavesBackEnd dbPut fail. %v", err)
		}

		return
	}

	length := m.cache.Length()
	if int64(length) >= m.maxCacheElem {
		m.Flush()
	}

	m.cache.Put(k, v)
}

// Delete removes the given key from the underlying map. No-op if the key
// doesn't exist.
func (m *CachedLeavesBackEnd) Delete(k utreexo.Hash) {
	// Delete directly from the database if we don't cache anything.
	if m.maxCacheElem == 0 {
		err := m.db.Delete(k[:], nil)
		if err != nil {
			log.Warnf("CachedLeavesBackEnd delete fail. %v", err)
		}

		return
	}

	_, found := m.cache.Get(k)
	if !found {
		// Check if we need to flush as we'll be adding an entry to
		// the cache.
		if int64(m.cache.Length()) >= m.maxCacheElem {
			m.Flush()
		}
	}

	// Try inserting, if it fails, it means we need to flush.
	if !m.cache.Put(k, math.MaxUint64) {
		m.Flush()
		m.cache.Put(k, math.MaxUint64)
	}
}

// Length returns the amount of items in the underlying db and the cache.
func (m *CachedLeavesBackEnd) Length() int {
	m.Flush()

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
	m.Flush()

	iter := m.db.NewIterator(nil, nil)
	for iter.Next() {
		// If the itered key isn't chainhash.HashSize, it means that the entry is for nodesbackend.
		// Skip it since it's not relevant here.
		if len(iter.Key()) != chainhash.HashSize {
			continue
		}
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

// Flush resets the cache and saves all the key values onto the database.
func (m *CachedLeavesBackEnd) Flush() {
	if m.maxCacheElem == 0 {
		return
	}

	m.cache.ForEach(func(k utreexo.Hash, v uint64) {
		if v == math.MaxUint64 {
			err := m.db.Delete(k[:], nil)
			if err != nil {
				log.Warnf("CachedLeavesBackEnd delete fail. %v", err)
			}
		} else {
			err := m.dbPut(k, v)
			if err != nil {
				log.Warnf("CachedLeavesBackEnd dbPut fail. %v", err)
			}
		}
	})

	m.cache.ClearMaps()
}
