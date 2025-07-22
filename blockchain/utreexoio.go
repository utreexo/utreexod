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

// nodeLength is the length of a seriailzed node. 4 because the remember bit is
// stored with the addindex.
const nodeLength = (chainhash.HashSize * 3) + 4

// serializeNode serializes the node to [nodeLength]byte.
func serializeNode(node utreexo.Node) [nodeLength]byte {
	var buf [nodeLength]byte

	idx := 0

	copy(buf[idx:idx+chainhash.HashSize], node.Above[:])
	idx += chainhash.HashSize

	copy(buf[idx:idx+chainhash.HashSize], node.LBelow[:])
	idx += chainhash.HashSize

	copy(buf[idx:idx+chainhash.HashSize], node.RBelow[:])
	idx += chainhash.HashSize

	addIndex := node.AddIndex << 1
	if node.Remember {
		addIndex |= 1
	}
	var indexBuf [4]byte
	binary.LittleEndian.PutUint32(indexBuf[:], uint32(addIndex))
	copy(buf[idx:idx+4], indexBuf[:])

	return buf
}

// deserializeNode serializes the node to [nodeLength]byte.
func deserializeNode(serialized [nodeLength]byte) utreexo.Node {
	node := utreexo.Node{}

	idx := 0
	hash := serialized[idx : idx+chainhash.HashSize]
	node.Above = ([32]byte)(hash)
	idx += chainhash.HashSize

	hash = serialized[idx : idx+chainhash.HashSize]
	node.LBelow = ([32]byte)(hash)
	idx += chainhash.HashSize

	hash = serialized[idx : idx+chainhash.HashSize]
	node.RBelow = ([32]byte)(hash)
	idx += chainhash.HashSize

	addIndex := binary.LittleEndian.Uint32(serialized[idx : idx+4])
	node.Remember = addIndex&1 == 1
	node.AddIndex = int32(addIndex) >> 1

	return node
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
func (m *NodesBackEnd) dbGet(k utreexo.Hash) (utreexo.Node, bool) {
	val, closer, err := m.db.Get(k[:])
	if err != nil {
		return utreexo.Node{}, false
	}
	defer closer.Close()

	// Must be leafLength bytes long.
	if len(val) != nodeLength {
		return utreexo.Node{}, false
	}

	node := deserializeNode(*(*[nodeLength]byte)(val))
	return node, true
}

// Get returns the node from the underlying map.
func (m *NodesBackEnd) Get(k utreexo.Hash) (utreexo.Node, bool) {
	cachedNode, found := m.cache.Get(k)
	if !found {
		node, found := m.dbGet(k)
		if !found {
			return utreexo.Node{}, false
		}
		// Cache the leaf before returning it.
		m.cache.Put(k, utreexobackends.CachedNode{Node: node})

		return node, found
	}
	// Even if the entry was found, don't return it if it's marked as removed.
	if cachedNode.IsRemoved() {
		return utreexo.Node{}, false
	}

	return cachedNode.Node, found
}

// NodesBatchPut puts a key-value pair in the given pebbledb batch.
func NodesBatchPut(batch *pebble.Batch, k utreexo.Hash, v utreexo.Node) error {
	serialized := serializeNode(v)
	return batch.Set(k[:], serialized[:], nil)
}

// Put puts the given position and the leaf to the underlying map.
func (m *NodesBackEnd) Put(k utreexo.Hash, v utreexo.Node) {
	leaf, found := m.cache.Get(k)
	if found {
		leaf.Flags &^= utreexobackends.Removed
		l := utreexobackends.CachedNode{
			Node:  v,
			Flags: leaf.Flags | utreexobackends.Modified,
		}

		m.cache.Put(k, l)
	} else {
		// If the key isn't found, mark it as fresh.
		l := utreexobackends.CachedNode{
			Node:  v,
			Flags: utreexobackends.Fresh,
		}

		m.cache.Put(k, l)
	}
}

// NodesBatchDelete deletes the corresponding key-value pair from the given pebble batch.
func NodesBatchDelete(batch *pebble.Batch, k utreexo.Hash) error {
	return batch.Delete(k[:], nil)
}

// Delete removes the given key from the underlying map. No-op if the key
// doesn't exist.
func (m *NodesBackEnd) Delete(k utreexo.Hash) {
	cachedNode, found := m.cache.Get(k)
	if found && cachedNode.IsFresh() {
		m.cache.Delete(k)
		return
	}
	p := utreexobackends.CachedNode{
		Node:  cachedNode.Node,
		Flags: cachedNode.Flags | utreexobackends.Removed,
	}

	m.cache.Put(k, p)
}

// Length returns the amount of items in the underlying database.
func (m *NodesBackEnd) Length() int {
	length := 0
	m.cache.ForEach(func(_ utreexo.Hash, n utreexobackends.CachedNode) error {
		// Only count the entry if it's not removed and it's not already
		// in the database.
		if !n.IsRemoved() && n.IsFresh() {
			length++
		}

		return nil
	})

	// Doesn't look like NewIter can actually return an error based on the
	// implimentation.
	iter, _ := m.db.NewIter(nil)
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		// The relevant key-value pairs for nodesbackend are nodeLength.
		// Skip it since it's not relevant here.
		value := iter.Value()
		if len(value) != nodeLength {
			continue
		}

		k := iter.Key()
		if len(k) != chainhash.HashSize {
			continue
		}
		val, found := m.cache.Get(([chainhash.HashSize]byte)(k))
		if found && val.IsRemoved() {
			// Skip if the key-value pair has already been removed in the cache.
			continue
		}
		length++
	}

	return length
}

// ForEach calls the given function for each of the elements in the underlying map.
func (m *NodesBackEnd) ForEach(fn func(utreexo.Hash, utreexo.Node) error) error {
	m.cache.ForEach(func(k utreexo.Hash, n utreexobackends.CachedNode) error {
		// Only operate on the entry if it's not removed and it's not already
		// in the database.
		if !n.IsRemoved() && n.IsFresh() {
			fn(k, n.Node)
		}

		return nil
	})

	iter, _ := m.db.NewIter(nil)
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		// The relevant key-value pairs for nodesbackend are leafLength.
		// Skip it since it's not relevant here.
		value := iter.Value()
		if len(value) != nodeLength {
			continue
		}

		k := iter.Key()
		if len(k) != chainhash.HashSize {
			continue
		}

		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		key := ([chainhash.HashSize]byte)(k)
		val, found := m.cache.Get(key)
		if found && val.IsRemoved() {
			// Skip if the key-value pair has already been removed in the cache.
			continue
		}

		v := deserializeNode(*(*[nodeLength]byte)(value))
		err := fn(key, v)
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

// RoughSize is a quick calculation of the cached items. The returned value is simply the
// length multiplied by the cache length.
func (m *NodesBackEnd) RoughSize() uint64 {
	return uint64(m.cache.Length()) * (chainhash.HashSize + nodeLength)
}

// FlushBatch saves all the cached entries to disk and resets the cache map using the Batch.
func (m *NodesBackEnd) FlushBatch(batch *pebble.Batch) error {
	err := m.cache.ForEachAndDelete(func(k utreexo.Hash, v utreexobackends.CachedNode) error {
		if v.IsFresh() {
			if !v.IsRemoved() {
				err := NodesBatchPut(batch, k, v.Node)
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
				err := NodesBatchPut(batch, k, v.Node)
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

	return nil
}

// FlushToSstable writes all the cached items in the maps to the given writer.
func FlushToSstable(writer *sstable.Writer, nDB *NodesBackEnd) {
	utreexobackends.FlushToSstable(writer, &nDB.cache, serializeNode)
}
