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

func TestCachedLeavesBackEnd(t *testing.T) {
	tests := []struct {
		tmpDir      string
		maxMemUsage int64
	}{
		{
			tmpDir: func() string {
				return filepath.Join(os.TempDir(), "TestCachedLeavesBackEnd")
			}(),
			maxMemUsage: 1 * 1024 * 1024,
		},
	}

	for _, test := range tests {
		db, err := pebble.Open(test.tmpDir, nil)
		if err != nil {
			t.Fatal(err)
		}
		cachedLeavesBackEnd, err := InitCachedLeavesBackEnd(db, test.maxMemUsage)
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(test.tmpDir)

		count := uint64(1000)
		compareMap := make(map[utreexo.Hash]uint64)
		for i := uint64(0); i < count/2; i++ {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])

			compareMap[hash] = i
			cachedLeavesBackEnd.Put(hash, i)
		}

		batch := db.NewBatch()
		err = batch.Set([]byte("utreexostateconsistency"), make([]byte, 40), nil)
		if err != nil {
			t.Fatal(err)
		}
		// Close and reopen the backend.
		cachedLeavesBackEnd.Flush(batch)
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
		cachedLeavesBackEnd, err = InitCachedLeavesBackEnd(db, test.maxMemUsage)
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

				cachedLeavesBackEnd.Delete(hash)
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

				cachedLeavesBackEnd.Put(hash, i)
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

			compareMap[hash] = i
		}

		if cachedLeavesBackEnd.Length() != len(compareMap) {
			t.Fatalf("compareMap has %d elements but the backend has %d elements",
				len(compareMap), cachedLeavesBackEnd.Length())
		}

		// Compare the map and the backend.
		for k, v := range compareMap {
			got, found := cachedLeavesBackEnd.Get(k)
			if !found {
				t.Fatalf("expected %v but it wasn't found", v)
			}

			if got != v {
				var buf [8]byte
				binary.LittleEndian.PutUint64(buf[:], got)
				gotHash := sha256.Sum256(buf[:])

				binary.LittleEndian.PutUint64(buf[:], v)
				expectHash := sha256.Sum256(buf[:])

				if gotHash != expectHash {
					t.Fatalf("for key %v, expected %v but got %v", k.String(), v, got)
				}
			}
		}
	}
}

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
		compareMap := make(map[uint64]utreexobackends.CachedLeaf)
		for i := uint64(0); i < count/2; i++ {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])

			compareMap[i] = utreexobackends.CachedLeaf{Leaf: utreexo.Leaf{Hash: hash}}
			nodesBackEnd.Put(i, utreexo.Leaf{Hash: hash})
		}

		batch := db.NewBatch()
		err = batch.Set([]byte("utreexostateconsistency"), make([]byte, 40), nil)
		if err != nil {
			t.Fatal(err)
		}
		// Close and reopen the backend.
		nodesBackEnd.Flush(batch)
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

				nodesBackEnd.Delete(i)
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

				nodesBackEnd.Put(i, utreexo.Leaf{Hash: hash})
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

			delete(compareMap, i)
		}
		for i := count / 2; i < count; i++ {
			var buf [8]byte
			binary.LittleEndian.PutUint64(buf[:], i)
			hash := sha256.Sum256(buf[:])

			compareMap[i] = utreexobackends.CachedLeaf{Leaf: utreexo.Leaf{Hash: hash}}
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

			if got.Hash != v.Leaf.Hash {
				if got.Hash != v.Leaf.Hash {
					t.Fatalf("for key %v, expected %v but got %v", k, v.Leaf.Hash, got.Hash)
				}
			}
		}
	}
}
