package utreexobackends

import (
	"encoding/binary"

	"github.com/cockroachdb/pebble/sstable"
	"github.com/utreexo/utreexo"
)

// FlushToSstable flushes the cache of both the nodes map and the cached leaves map without
// allocating extra memory for the values in both of the maps.
func FlushToSstable(writer *sstable.Writer, nMap *NodesMapSlice, cMap *CachedLeavesMapSlice,
	nValueSerFunc func(utreexo.Leaf) [33]byte, cValueSerFunc func(utreexo.LeafInfo) [12]byte) {

	nMap.mtx.Lock()
	cMap.mtx.Lock()
	defer nMap.mtx.Unlock()
	defer cMap.mtx.Unlock()

	// Keep popping until we don't have any more left.
	nodeKey, nodeValue := nMap.popForFlushing()
	leafKey, leafValue := cMap.popForFlushing()

	nodeKeyBytes := [8]byte{}
	binary.BigEndian.PutUint64(nodeKeyBytes[:], nodeKey)

	for nodeValue != nil && leafValue != nil {
		if string(nodeKeyBytes[:]) < string(leafKey[:]) {
			if nodeValue.IsRemoved() {
				writer.Delete(nodeKeyBytes[:])
			} else {
				valueBytes := nValueSerFunc(nodeValue.Leaf)
				writer.Set(nodeKeyBytes[:], valueBytes[:])
			}

			nodeKey, nodeValue = nMap.popForFlushing()
			binary.BigEndian.PutUint64(nodeKeyBytes[:], nodeKey)
		} else {
			if leafValue.IsRemoved() {
				writer.Delete(leafKey[:])
			} else {
				valueBytes := cValueSerFunc(leafValue.LeafInfo)
				writer.Set(leafKey[:], valueBytes[:])
			}

			leafKey, leafValue = cMap.popForFlushing()
		}
	}

	// Add the rest.
	if nodeValue != nil {
		for nodeValue != nil {
			if nodeValue.IsRemoved() {
				writer.Delete(nodeKeyBytes[:])
			} else {
				valueBytes := nValueSerFunc(nodeValue.Leaf)
				writer.Set(nodeKeyBytes[:], valueBytes[:])
			}

			nodeKey, nodeValue = nMap.popForFlushing()
			binary.BigEndian.PutUint64(nodeKeyBytes[:], nodeKey)
		}
	} else {
		for leafKey != nil {
			if leafValue.IsRemoved() {
				writer.Delete(leafKey[:])
			} else {
				valueBytes := cValueSerFunc(leafValue.LeafInfo)
				writer.Set(leafKey[:], valueBytes[:])
			}

			leafKey, leafValue = cMap.popForFlushing()
		}
	}
}
