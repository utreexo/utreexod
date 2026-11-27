package ffldb

import (
	"errors"
	"reflect"
	"testing"

	"github.com/utreexo/utreexod/database"
)

func TestCalcBlockFilesSizeAbove4GiB(t *testing.T) {
	store := &blockStore{
		fileStartEndNumFunc: func() (uint32, uint32, error) { return 0, 96, nil },
		fileSizeFunc:        func(uint32) (uint32, error) { return 128 * 1024 * 1024, nil },
	}
	_, _, got, err := store.calcBlockFilesSize()
	if err != nil {
		t.Fatal(err)
	}
	want := uint64(12) * 1024 * 1024 * 1024
	if got != want {
		t.Fatalf("total size = %d, want %d", got, want)
	}
}

// TestPruneBlocksLargeTarget uses fake file sizes with a real transaction to
// cover both overflowing an individual store and combining two smaller stores.
// No large files or disk allocations are needed.
func TestPruneBlocksLargeTarget(t *testing.T) {
	tests := []struct {
		name        string
		files       uint32
		blockSize   uint32
		undoSize    uint32
		target      uint64
		wantDeleted uint32
	}{
		{name: "12_GiB_blocks_10_GiB_target", files: 96,
			blockSize: 128 * 1024 * 1024, target: 10 * 1024 * 1024 * 1024,
			wantDeleted: 16},
		{name: "combined_stores_above_4_GiB", files: 24,
			blockSize: 128 * 1024 * 1024, undoSize: 128 * 1024 * 1024,
			target: 5 * 1024 * 1024 * 1024, wantDeleted: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			idb, err := database.Create(dbType, t.TempDir(), blockDataNet)
			if err != nil {
				t.Fatal(err)
			}
			defer idb.Close()
			db := idb.(*db)
			db.blkStore.fileStartEndNumFunc = func() (uint32, uint32, error) {
				return 0, test.files, nil
			}
			db.blkStore.fileSizeFunc = func(uint32) (uint32, error) {
				return test.blockSize, nil
			}
			db.sjStore.fileStartEndNumFunc = db.blkStore.fileStartEndNumFunc
			db.sjStore.fileSizeFunc = func(uint32) (uint32, error) {
				return test.undoSize, nil
			}
			itx, err := idb.Begin(true)
			if err != nil {
				t.Fatal(err)
			}
			defer itx.Rollback()
			tx := itx.(*transaction)
			for i := uint32(0); i <= test.files; i++ {
				if err := tx.putHeightInfo(i, int32(i*100), int32(i*100+99)); err != nil {
					t.Fatal(err)
				}
			}
			earliest, err := tx.PruneBlocks(test.target, int32(test.files*100))
			if err != nil {
				t.Fatal(err)
			}
			wantEarliest := int32(test.wantDeleted * 100)
			if earliest != wantEarliest {
				t.Fatalf("earliest retained height = %d, want %d", earliest, wantEarliest)
			}
			wantFiles := make([]uint32, test.wantDeleted)
			for i := range wantFiles {
				wantFiles[i] = uint32(i)
			}
			if !reflect.DeepEqual(tx.pendingDelFileNums, wantFiles) {
				t.Fatalf("pending deletions = %v, want %v", tx.pendingDelFileNums, wantFiles)
			}
			// Rollback must leave this simulated pruning uncommitted.
		})
	}
}

func TestPruneBlocksSizeError(t *testing.T) {
	for _, undo := range []bool{false, true} {
		name := "blocks"
		if undo {
			name = "undo"
		}
		t.Run(name, func(t *testing.T) {
			idb, err := database.Create(dbType, t.TempDir(), blockDataNet)
			if err != nil {
				t.Fatal(err)
			}
			defer idb.Close()
			db := idb.(*db)
			store := db.blkStore
			if undo {
				store = db.sjStore
			}
			failure := errors.New("cannot determine file size")
			store.fileStartEndNumFunc = func() (uint32, uint32, error) {
				return 0, 1, nil
			}
			store.fileSizeFunc = func(uint32) (uint32, error) { return 0, failure }
			err = idb.Update(func(tx database.Tx) error {
				_, err := tx.PruneBlocks(10*1024*1024*1024, 288)
				return err
			})
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v, want %v", err, failure)
			}
		})
	}
}
