// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"encoding/binary"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/cockroachdb/pebble/sstable"
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
	db           *pebble.DB
	maxCacheElem int64
	cache        utreexobackends.NodesMapSlice
}

// InitNodesBackEnd returns a newly initialized NodesBackEnd which implements
// utreexo.NodesInterface.
func InitNodesBackEnd(db *pebble.DB, maxTotalMemoryUsage int64) (*NodesBackEnd, error) {
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
	buf := [8]byte{}
	binary.BigEndian.PutUint64(buf[:], k)

	val, closer, err := m.db.Get(buf[:])
	if err != nil {
		return utreexo.Leaf{}, false
	}
	defer closer.Close()

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

// NodesBatchPut puts a key-value pair in the given pebbledb batch.
func NodesBatchPut(batch *pebble.Batch, k uint64, v utreexo.Leaf) error {
	buf := [8]byte{}
	binary.BigEndian.PutUint64(buf[:], k)

	serialized := serializeLeaf(v)
	return batch.Set(buf[:], serialized[:], nil)
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

// NodesBatchDelete deletes the corresponding key-value pair from the given pebble batch.
func NodesBatchDelete(batch *pebble.Batch, k uint64) error {
	buf := [8]byte{}
	binary.BigEndian.PutUint64(buf[:], k)
	return batch.Delete(buf[:], nil)
}

// Delete removes the given key from the underlying map. No-op if the key
// doesn't exist.
func (m *NodesBackEnd) Delete(k uint64) {
	// Don't delete as the same key may get called to be removed multiple times.
	// Cache it as removed so that we don't call expensive flushes on keys that
	// are not in the database.
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
	m.cache.ForEach(func(u uint64, cl utreexobackends.CachedLeaf) error {
		// Only count the entry if it's not removed and it's not already
		// in the database.
		if !cl.IsRemoved() && cl.IsFresh() {
			length++
		}

		return nil
	})

	// Doesn't look like NewIter can actually return an error based on the
	// implimentation.
	iter, _ := m.db.NewIter(nil)
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		// The relevant key-value pairs for nodesbackend are leafLength.
		// Skip it since it's not relevant here.
		value := iter.Value()
		if len(value) != leafLength {
			continue
		}

		k := binary.BigEndian.Uint64(iter.Key())
		val, found := m.cache.Get(k)
		if found && val.IsRemoved() {
			// Skip if the key-value pair has already been removed in the cache.
			continue
		}
		length++
	}

	return length
}

// ForEach calls the given function for each of the elements in the underlying map.
func (m *NodesBackEnd) ForEach(fn func(uint64, utreexo.Leaf) error) error {
	m.cache.ForEach(func(u uint64, cl utreexobackends.CachedLeaf) error {
		// Only operate on the entry if it's not removed and it's not already
		// in the database.
		if !cl.IsRemoved() && cl.IsFresh() {
			fn(u, cl.Leaf)
		}

		return nil
	})

	iter, _ := m.db.NewIter(nil)
	defer iter.Close()
	for iter.Next() {
		// The relevant key-value pairs for nodesbackend are leafLength.
		// Skip it since it's not relevant here.
		value := iter.Value()
		if len(value) != leafLength {
			continue
		}

		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		k := binary.BigEndian.Uint64(iter.Key())
		val, found := m.cache.Get(k)
		if found && val.IsRemoved() {
			// Skip if the key-value pair has already been removed in the cache.
			continue
		}

		v := deserializeLeaf(*(*[leafLength]byte)(value))
		err := fn(k, v)
		if err != nil {
			return err
		}
	}

	return iter.Error()
}

// IsFlushNeeded returns true if the backend needs to be flushed.
func (m *NodesBackEnd) IsFlushNeeded() bool {
	return m.cache.Overflowed()
}

// UsageStats returns the currently cached elements and the total amount the cache can hold.
func (m *NodesBackEnd) UsageStats() (int64, int64) {
	return int64(m.cache.Length()), m.maxCacheElem
}

// FlushBatch saves all the cached entries to disk and resets the cache map using the Batch.
func (m *NodesBackEnd) FlushBatch(batch *pebble.Batch) error {
	err := m.cache.ForEach(func(k uint64, v utreexobackends.CachedLeaf) error {
		if v.IsFresh() {
			if !v.IsRemoved() {
				err := NodesBatchPut(batch, k, v.Leaf)
				if err != nil {
					return err
				}
			}
		} else {
			if v.IsRemoved() {
				err := NodesBatchDelete(batch, k)
				if err != nil {
					return err
				}
			} else {
				err := NodesBatchPut(batch, k, v.Leaf)
				if err != nil {
					return err
				}
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("NodesBackEnd flush error. %v", err)
	}

	m.cache.ClearMaps()
	return nil
}

// serializeLeafInfo serializes the given LeafInfo into a [12]byte array.
func serializeLeafInfo(leafInfo utreexo.LeafInfo) [12]byte {
	var buf [12]byte
	byteOrder.PutUint64(buf[:8], leafInfo.Position)
	byteOrder.PutUint32(buf[8:12], leafInfo.AddIndex)

	return buf
}

// deserializeLeafInfo returns a LeafInfo from a [12]byte array.
func deserializeLeafInfo(buf [12]byte) utreexo.LeafInfo {
	return utreexo.LeafInfo{
		Position: byteOrder.Uint64(buf[:8]),
		AddIndex: byteOrder.Uint32(buf[8:12]),
	}
}

var _ utreexo.CachedLeavesInterface = (*CachedLeavesBackEnd)(nil)

// CachedLeavesBackEnd implements the CachedLeavesInterface interface. The cache assumes
// that anything in the cache doesn't exist in the db and vise-versa.
type CachedLeavesBackEnd struct {
	db           *pebble.DB
	maxCacheElem int64
	cache        utreexobackends.CachedLeavesMapSlice
}

// dbGet fetches and deserializes the value from the database.
func (m *CachedLeavesBackEnd) dbGet(k utreexo.Hash) (utreexo.LeafInfo, bool) {
	val, closer, err := m.db.Get(k[:])
	if err != nil || len(val) != 12 {
		return utreexo.LeafInfo{}, false
	}
	defer closer.Close()

	return deserializeLeafInfo(([12]byte)(val)), true
}

// InitCachedLeavesBackEnd returns a newly initialized CachedLeavesBackEnd which implements
// utreexo.CachedLeavesInterface.
func InitCachedLeavesBackEnd(db *pebble.DB, maxMemoryUsage int64) (*CachedLeavesBackEnd, error) {
	cache, maxCacheElem := utreexobackends.NewCachedLeavesMapSlice(maxMemoryUsage)
	return &CachedLeavesBackEnd{maxCacheElem: maxCacheElem, db: db, cache: cache}, nil
}

// Get returns the data from the underlying cache or the database.
func (m *CachedLeavesBackEnd) Get(k utreexo.Hash) (utreexo.LeafInfo, bool) {
	leafInfo, found := m.cache.Get(k)
	if !found {
		return m.dbGet(k)
	}
	// Even if the entry was found, if the position value is math.MaxUint64,
	// then it was already deleted.
	if leafInfo.IsRemoved() {
		return utreexo.LeafInfo{}, false
	}

	return leafInfo.LeafInfo, found
}

// CachedLeavesBatchPut puts a key-value pair in the given pebbledb batch.
func CachedLeavesBatchPut(tx *pebble.Batch, k utreexo.Hash, v utreexo.LeafInfo) error {
	buf := serializeLeafInfo(v)
	return tx.Set(k[:], buf[:], nil)
}

// Add adds the given hash and the data that makes up the LeafInfo into the backend.
func (m *CachedLeavesBackEnd) Add(k utreexo.Hash, v uint64, index uint32) {
	m.cache.Put(k, utreexobackends.CachedPosition{
		LeafInfo: utreexo.LeafInfo{
			Position: v,
			AddIndex: index,
		},
		Flags: utreexobackends.Fresh,
	})
}

// Update changes the given leafhash's position with the one that's passed in.
// It doesn't add the leaf if it doesn't already exist in the backend.
func (m *CachedLeavesBackEnd) Update(k utreexo.Hash, v uint64) {
	cachedPos, found := m.cache.Get(k)
	if found {
		cachedPos.LeafInfo.Position = v
		m.cache.Put(k, cachedPos)
		return
	}

	leafInfo, found := m.dbGet(k)
	if !found {
		return
	}
	leafInfo.Position = v

	p := utreexobackends.CachedPosition{
		LeafInfo: leafInfo,
		Flags:    utreexobackends.Modified,
	}

	m.cache.Put(k, p)
}

// Delete removes the given key from the underlying map. No-op if the key
// doesn't exist.
func (m *CachedLeavesBackEnd) Delete(k utreexo.Hash) {
	leafInfo, found := m.cache.Get(k)
	if found && leafInfo.IsFresh() {
		m.cache.Delete(k)
		return
	}
	p := utreexobackends.CachedPosition{
		LeafInfo: leafInfo.LeafInfo,
		Flags:    leafInfo.Flags | utreexobackends.Removed,
	}

	m.cache.Put(k, p)
}

// Length returns the amount of items in the underlying db and the cache.
func (m *CachedLeavesBackEnd) Length() int {
	length := 0
	m.cache.ForEach(func(k utreexo.Hash, v utreexobackends.CachedPosition) error {
		// Only operate on the entry if it's not removed and it's not already
		// in the database.
		if !v.IsRemoved() && v.IsFresh() {
			length++
		}
		return nil
	})
	iter, _ := m.db.NewIter(nil)
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		// If the itered key is not chainhash.HashSize, it's not for cachedLeavesBackend.
		// Skip it since it's not relevant here.
		if len(iter.Key()) != chainhash.HashSize {
			continue
		}
		k := iter.Key()
		val, found := m.cache.Get(*(*[chainhash.HashSize]byte)(k))
		if found && val.IsRemoved() {
			// Skip if the key-value pair has already been removed in the cache.
			continue
		}

		length++
	}

	return length
}

// ForEach calls the given function for each of the elements in the underlying map.
func (m *CachedLeavesBackEnd) ForEach(fn func(utreexo.Hash, utreexo.LeafInfo) error) error {
	m.cache.ForEach(func(k utreexo.Hash, v utreexobackends.CachedPosition) error {
		// Only operate on the entry if it's not removed and it's not already
		// in the database.
		if !v.IsRemoved() && v.IsFresh() {
			fn(k, v.LeafInfo)
		}
		return nil
	})
	iter, _ := m.db.NewIter(nil)
	defer iter.Close()
	for iter.Next() {
		// If the itered key is not chainhash.HashSize, it's not for cachedLeavesBackend.
		// Skip it since it's not relevant here.
		if len(iter.Key()) != chainhash.HashSize {
			continue
		}
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		k := iter.Key()
		val, found := m.cache.Get(*(*[chainhash.HashSize]byte)(k))
		if found && val.IsRemoved() {
			// Skip if the key-value pair has already been removed in the cache.
			continue
		}
		serialized := iter.Value()
		if len(serialized) != 12 {
			continue
		}
		leafInfo := deserializeLeafInfo(([12]byte)(serialized))

		err := fn(*(*[chainhash.HashSize]byte)(k), leafInfo)
		if err != nil {
			return err
		}
	}

	return iter.Error()
}

// IsFlushNeeded returns true if the backend needs to be flushed.
func (m *CachedLeavesBackEnd) IsFlushNeeded() bool {
	return m.cache.Overflowed()
}

// UsageStats returns the currently cached elements and the total amount the cache can hold.
func (m *CachedLeavesBackEnd) UsageStats() (int64, int64) {
	return int64(m.cache.Length()), m.maxCacheElem
}

// FlushBatch resets the cache and saves all the key values onto the given Batch.
func (m *CachedLeavesBackEnd) FlushBatch(batch *pebble.Batch) error {
	err := m.cache.ForEach(func(k utreexo.Hash, v utreexobackends.CachedPosition) error {
		if v.IsRemoved() {
			err := batch.Delete(k[:], nil)
			if err != nil {
				return err
			}
		} else {
			err := CachedLeavesBatchPut(batch, k, v.LeafInfo)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("CachedLeavesBackEnd flush error. %v", err)
	}

	m.cache.ClearMaps()
	return nil
}

// FlushToSstable writes all the cached items in the maps to the given writer.
func FlushToSstable(writer *sstable.Writer, nDB *NodesBackEnd, cDB *CachedLeavesBackEnd) {
	utreexobackends.FlushToSstable(writer, &nDB.cache, &cDB.cache, serializeLeaf, serializeLeafInfo)
}
