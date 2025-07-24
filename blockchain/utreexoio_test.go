package blockchain

import (
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain/internal/utreexobackends"
)

func TestNodesBackEnd(t *testing.T) {
	tests := []struct {
		tmpDir      string
		maxMemUsage int64
	}{
		{
			tmpDir: func() string {
				return filepath.Join(os.TempDir(), "TestNodesBackEnd")
			}(),
			maxMemUsage: 1 * 1024 * 1024,
		},
	}

	for _, test := range tests {
		db, err := pebble.Open(test.tmpDir, nil)
		if err != nil {
			t.Fatal(err)
		}
		nodesBackEnd, err := InitNodesBackEnd(db, test.maxMemUsage)
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(test.tmpDir)

		count := uint64(1000)
		compareMap := make(map[utreexo.Hash]utreexobackends.CachedNode)
		for i := uint64(0); i < count/2; i++ {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])

			if i%5 == 0 {
				compareMap[hash] = utreexobackends.CachedNode{Node: utreexo.Node{AddIndex: int32(-1)}}
				nodesBackEnd.Put(hash, utreexo.Node{AddIndex: int32(-1)})
			} else {
				compareMap[hash] = utreexobackends.CachedNode{Node: utreexo.Node{AddIndex: int32(i)}}
				nodesBackEnd.Put(hash, utreexo.Node{AddIndex: int32(i)})
			}
		}

		batch := db.NewBatch()
		err = batch.Set([]byte("utreexostateconsistency"), make([]byte, 40), nil)
		if err != nil {
			t.Fatal(err)
		}
		// Close and reopen the backend.
		nodesBackEnd.FlushBatch(batch)
		err = batch.Commit(nil)
		if err != nil {
			t.Fatal(err)
		}
		err = db.Close()
		if err != nil {
			t.Fatal(err)
		}

		db, err = pebble.Open(test.tmpDir, nil)
		if err != nil {
			t.Fatal(err)
		}
		nodesBackEnd, err = InitNodesBackEnd(db, test.maxMemUsage)
		if err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(1)
		// Delete every other element from the backend that we currently have.
		go func() {
			for i := uint64(0); i < count/2; i++ {
				if i%2 != 0 {
					continue
				}

				var buf [8]byte
				binary.LittleEndian.PutUint64(buf[:], i)
				hash := sha256.Sum256(buf[:])
				nodesBackEnd.Delete(hash)
			}
			wg.Done()
		}()

		wg.Add(1)
		// Put the rest of the elements into the backend.
		go func() {
			for i := count / 2; i < count; i++ {
				var buf [8]byte
				binary.LittleEndian.PutUint64(buf[:], i)
				hash := sha256.Sum256(buf[:])

				nodesBackEnd.Put(hash, utreexo.Node{AddIndex: int32(i)})
			}
			wg.Done()
		}()

		wg.Wait()

		// Do the same for the compare map. We do it here because a hashmap
		// isn't concurrency safe.
		for i := uint64(0); i < count/2; i++ {
			if i%2 != 0 {
				continue
			}

			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])
			delete(compareMap, hash)
		}
		for i := count / 2; i < count; i++ {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])

			compareMap[hash] = utreexobackends.CachedNode{Node: utreexo.Node{AddIndex: int32(i)}}
		}

		if nodesBackEnd.Length() != len(compareMap) {
			t.Fatalf("compareMap has %d elements but the backend has %d elements",
				len(compareMap), nodesBackEnd.Length())
		}

		// Compare the map and the backend.
		for k, v := range compareMap {
			got, found := nodesBackEnd.Get(k)
			if !found {
				t.Fatalf("expected %v but it wasn't found", v)
			}

			if got.AddIndex != v.Node.AddIndex {
				t.Fatalf("for key %v, expected %v but got %v", k, v.Node.AddIndex, got.AddIndex)
			}
		}
	}
}
