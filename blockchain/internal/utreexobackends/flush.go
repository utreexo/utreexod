package utreexobackends

import (
	"github.com/cockroachdb/pebble/sstable"
	"github.com/utreexo/utreexo"
)

// FlushToSstable flushes the cache of the nodes map without allocating extra memory for the values.
func FlushToSstable(writer *sstable.Writer, nMap *NodesMapSlice,
	nValueSerFunc func(utreexo.Node) [100]byte) {

	nMap.mtx.Lock()
	defer nMap.mtx.Unlock()

	// Keep popping until we don't have any more left.
	nodeKey, nodeValue := nMap.popForFlushing()
	for nodeValue != nil {
		if nodeValue.IsRemoved() {
			writer.Delete(nodeKey[:])
		} else {
			valueBytes := nValueSerFunc(nodeValue.Node)
			writer.Set(nodeKey[:], valueBytes[:])
		}

		nodeKey, nodeValue = nMap.popForFlushing()
	}
}
