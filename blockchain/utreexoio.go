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

// Get returns the leaf from the underlying map.
func (m *NodesBackEnd) Get(k uint64) (utreexo.Leaf, bool) {
	// Look it up on the cache first.
	cLeaf, found := m.cache.Get(k)
	if found {
		// The leaf might not have been cleaned up yet.
		if cLeaf.IsRemoved() {
			return utreexo.Leaf{}, false
		}

		m.cache.Put(k, cLeaf)

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
	m.cache.Put(k, utreexobackends.CachedLeaf{Leaf: leaf})

	return leaf, true
}

// NodesBackendPut puts a key-value pair in the given leveldb tx.
func NodesBackendPut(tx *leveldb.Transaction, k uint64, v utreexo.Leaf) error {
	size := serializeSizeVLQ(k)
	buf := make([]byte, size)
	putVLQ(buf, k)

	serialized := serializeLeaf(v)
	return tx.Put(buf[:], serialized[:], nil)
}

// Put puts the given position and the leaf to the underlying map.
func (m *NodesBackEnd) Put(k uint64, v utreexo.Leaf) {
	leaf, found := m.cache.Get(k)
	if found {
		leaf.Flags &^= utreexobackends.Removed
		l := utreexobackends.CachedLeaf{
			Leaf:  v,
			Flags: leaf.Flags | utreexobackends.Modified,
		}

		m.cache.Put(k, l)
	} else {
		// If the key isn't found, mark it as fresh.
		l := utreexobackends.CachedLeaf{
			Leaf:  v,
			Flags: utreexobackends.Fresh,
		}

		m.cache.Put(k, l)
	}
}

// NodesBackendDelete deletes the corresponding key-value pair from the given leveldb tx.
func NodesBackendDelete(tx *leveldb.Transaction, k uint64) error {
	size := serializeSizeVLQ(k)
	buf := make([]byte, size)
	putVLQ(buf, k)
	return tx.Delete(buf, nil)
}

// Delete removes the given key from the underlying map. No-op if the key
// doesn't exist.
func (m *NodesBackEnd) Delete(k uint64) {
	leaf, _ := m.cache.Get(k)
	l := utreexobackends.CachedLeaf{
		Leaf:  leaf.Leaf,
		Flags: leaf.Flags | utreexobackends.Removed,
	}

	m.cache.Put(k, l)
}

// Length returns the amount of items in the underlying database.
func (m *NodesBackEnd) Length() int {
	length := 0
	m.cache.ForEach(func(u uint64, cl utreexobackends.CachedLeaf) {
		// Only count the entry if it's not removed and it's not already
		// in the database.
		if !cl.IsRemoved() && cl.IsFresh() {
			length++
		}
	})

	iter := m.db.NewIterator(nil, nil)
	for iter.Next() {
		// If the itered key is chainhash.HashSize, it means that the entry is for nodesbackend.
		// Skip it since it's not relevant here.
		if len(iter.Key()) == 32 {
			continue
		}
		k, _ := deserializeVLQ(iter.Key())
		val, found := m.cache.Get(k)
		if found && val.IsRemoved() {
			// Skip if the key-value pair has already been removed in the cache.
			continue
		}
		length++
	}
	iter.Release()

	return length
}

// ForEach calls the given function for each of the elements in the underlying map.
func (m *NodesBackEnd) ForEach(fn func(uint64, utreexo.Leaf) error) error {
	m.cache.ForEach(func(u uint64, cl utreexobackends.CachedLeaf) {
		// Only operate on the entry if it's not removed and it's not already
		// in the database.
		if !cl.IsRemoved() && cl.IsFresh() {
			fn(u, cl.Leaf)
		}
	})

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
		val, found := m.cache.Get(k)
		if found && val.IsRemoved() {
			// Skip if the key-value pair has already been removed in the cache.
			continue
		}

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
func (m *NodesBackEnd) Flush(ldbTx *leveldb.Transaction) {
	m.cache.ForEach(func(k uint64, v utreexobackends.CachedLeaf) {
		if v.IsRemoved() {
			err := NodesBackendDelete(ldbTx, k)
			if err != nil {
				ldbTx.Discard()
				log.Warnf("NodesBackEnd flush error. %v", err)
				return
			}
		} else if v.IsFresh() || v.IsModified() {
			err := NodesBackendPut(ldbTx, k, v.Leaf)
			if err != nil {
				ldbTx.Discard()
				log.Warnf("NodesBackEnd flush error. %v", err)
				return
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

// CachedLeavesBackendPut puts a key-value pair in the given leveldb tx.
func CachedLeavesBackendPut(tx *leveldb.Transaction, k utreexo.Hash, v uint64) error {
	size := serializeSizeVLQ(v)
	buf := make([]byte, size)
	putVLQ(buf, v)
	return tx.Put(k[:], buf, nil)
}

// Put puts the given data to the underlying cache. If the cache is full, it evicts
// the earliest entries to make room.
func (m *CachedLeavesBackEnd) Put(k utreexo.Hash, v uint64) {
	m.cache.Put(k, v)
}

// Delete removes the given key from the underlying map. No-op if the key
// doesn't exist.
func (m *CachedLeavesBackEnd) Delete(k utreexo.Hash) {
	m.cache.Put(k, math.MaxUint64)
}

// Length returns the amount of items in the underlying db and the cache.
func (m *CachedLeavesBackEnd) Length() int {
	length := 0
	m.cache.ForEach(func(k utreexo.Hash, v uint64) {
		// Only operate on the entry if it's not removed and it's not already
		// in the database.
		if v != math.MaxUint64 {
			_, found := m.dbGet(k)
			if !found {
				length++
			}
		}
	})
	iter := m.db.NewIterator(nil, nil)
	for iter.Next() {
		// If the itered key is chainhash.HashSize, it means that the entry is for nodesbackend.
		// Skip it since it's not relevant here.
		if len(iter.Key()) != chainhash.HashSize {
			continue
		}
		k := iter.Key()
		val, found := m.cache.Get(*(*[chainhash.HashSize]byte)(k))
		if found && val == math.MaxUint64 {
			// Skip if the key-value pair has already been removed in the cache.
			continue
		}

		length++
	}
	iter.Release()

	return length
}

// ForEach calls the given function for each of the elements in the underlying map.
func (m *CachedLeavesBackEnd) ForEach(fn func(utreexo.Hash, uint64) error) error {
	m.cache.ForEach(func(k utreexo.Hash, v uint64) {
		// Only operate on the entry if it's not removed and it's not already
		// in the database.
		if v != math.MaxUint64 {
			_, found := m.dbGet(k)
			if !found {
				fn(k, v)
			}
		}
	})
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
		val, found := m.cache.Get(*(*[chainhash.HashSize]byte)(k))
		if found && val == math.MaxUint64 {
			// Skip if the key-value pair has already been removed in the cache.
			continue
		}
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
func (m *CachedLeavesBackEnd) Flush(ldbTx *leveldb.Transaction) {
	m.cache.ForEach(func(k utreexo.Hash, v uint64) {
		if v == math.MaxUint64 {
			err := ldbTx.Delete(k[:], nil)
			if err != nil {
				ldbTx.Discard()
				log.Warnf("CachedLeavesBackEnd delete fail. %v", err)
				return
			}
		} else {
			err := CachedLeavesBackendPut(ldbTx, k, v)
			if err != nil {
				ldbTx.Discard()
				log.Warnf("CachedLeavesBackEnd put fail. %v", err)
				return
			}
		}
	})

	m.cache.ClearMaps()
}
