// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"fmt"

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

var _ utreexo.NodesInterface = (*NodesBackEnd)(nil)

// NodesBackEnd implements the NodesInterface interface. It's really just the database.
type NodesBackEnd struct {
	db *leveldb.DB
}

// NewNodesBackEnd returns a newly initialized NodesBackEnd which implements
// utreexo.NodesInterface.
func NewNodesBackEnd(datadir string) (*NodesBackEnd, error) {
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

var _ utreexo.CachedLeavesInterface = (*CachedLeavesBackEnd)(nil)

// CachedLeavesBackEnd implements the CachedLeavesInterface interface. It's really just a map.
type CachedLeavesBackEnd struct {
	db *leveldb.DB
}

// NewCachedLeavesBackEnd returns a newly initialized CachedLeavesBackEnd which implements
// utreexo.CachedLeavesInterface.
func NewCachedLeavesBackEnd(datadir string) (*CachedLeavesBackEnd, error) {
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
