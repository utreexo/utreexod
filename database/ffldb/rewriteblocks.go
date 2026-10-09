// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ffldb

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"sort"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/utreexo/utreexod/database"
)

// blockFileRewriteEntry associates a block hash with its location in a block
// file.
type blockFileRewriteEntry struct {
	hash chainhash.Hash
	loc  blockLocation
}

// blockTransform rewrites the raw bytes of a stored block, returning the
// replacement bytes and, when the block carries a proof to move into the
// proof store, the serialized proof.  Returning the original bytes with a
// nil proof leaves the block alone.
type blockTransform func(hash *chainhash.Hash, blockBytes []byte) ([]byte,
	[]byte, error)

// RewriteBlockFiles rewrites the stored block files, transforming each
// block's raw bytes through the provided function.
//
// Files are processed oldest to newest.  Each file is rewritten in place with
// its records packed back to back only when at least one of its records
// changed, so a run interrupted between files can be resumed safely: files
// whose records are all unchanged are left alone.  Proofs are written
// atomically with the file rewrite and hashes that already have a stored
// proof are skipped.
//
// A crash between a file's rewrite and the commit of its transaction leaves
// the file packed while the block index still records the old offsets.
// Reads then detect the mismatch as corruption and the database must be
// resynced.  Only a metadata journal would close that window.
//
// The rewrite is not safe to run concurrently with other database writers.
// The only caller is the open-time proof store upgrade, which runs before
// the node starts serving.  Should another writer interleave anyway, rows
// pruned mid run are skipped and an append to a file being rewritten is
// detected and reported so the caller can simply rerun the migration.
//
// This is intentionally not part of the public database interface: it is a
// migration hook reached through an interface assertion by the blockchain
// package, which uses it to strip legacy inline proof data from block
// records.
func (db *db) RewriteBlockFiles(transform func(hash *chainhash.Hash,
	blockBytes []byte) ([]byte, []byte, error)) error {

	// Gather the records of every block file.  The gathered locations are
	// only a starting point: each entry is re-checked against the index
	// inside its file's write transaction, so a stale snapshot entry can
	// never rewrite a block at an outdated location.
	entriesByFile, err := db.gatherBlockFileEntries()
	if err != nil {
		return err
	}

	fileNums := make([]uint32, 0, len(entriesByFile))
	for fileNum := range entriesByFile {
		fileNums = append(fileNums, fileNum)
	}
	sort.Slice(fileNums, func(i, j int) bool {
		return fileNums[i] < fileNums[j]
	})

	for _, fileNum := range fileNums {
		entries := entriesByFile[fileNum]
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].loc.fileOffset < entries[j].loc.fileOffset
		})

		err := db.Update(func(dbTx database.Tx) error {
			tx := dbTx.(*transaction)
			return db.rewriteBlockFileEntries(tx, fileNum, entries,
				transform)
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// gatherBlockFileEntries walks the block index and returns the location of
// every stored block grouped by block file.
func (db *db) gatherBlockFileEntries() (map[uint32][]blockFileRewriteEntry,
	error) {

	entriesByFile := make(map[uint32][]blockFileRewriteEntry)
	err := db.View(func(dbTx database.Tx) error {
		tx := dbTx.(*transaction)
		cursor := tx.blockIdxBucket.Cursor()
		for ok := cursor.First(); ok; ok = cursor.Next() {
			loc := deserializeBlockLoc(cursor.Value())

			var hash chainhash.Hash
			copy(hash[:], cursor.Key())
			entriesByFile[loc.blockFileNum] = append(
				entriesByFile[loc.blockFileNum],
				blockFileRewriteEntry{hash: hash, loc: loc})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return entriesByFile, nil
}

// rewriteBlockFileEntries transforms the records of the provided block
// file, given in offset order, and rewrites the file in place with the
// records packed back to back.  Proofs returned by the transform are
// stored and the block index rows are updated with the new locations.
// A file whose records are all unchanged is left alone.
func (db *db) rewriteBlockFileEntries(tx *transaction, fileNum uint32,
	entries []blockFileRewriteEntry, transform blockTransform) error {

	var rewritten bytes.Buffer
	newLocs := make([]blockFileRewriteEntry, 0, len(entries))
	changed := false
	var lastCurrentEnd uint32
	for _, entry := range entries {
		// Use the row's current location inside this write transaction.
		// The gathered snapshot could be stale if blocks were pruned
		// while the migration was running.  Pruning deletes whole files
		// along with all of their rows, so a block whose row is gone is
		// in a file that is being deleted anyway and is skipped.
		row := tx.blockIdxBucket.Get(entry.hash[:])
		if row == nil {
			continue
		}
		loc := deserializeBlockLoc(row)
		if end := loc.fileOffset + loc.blockLen; end > lastCurrentEnd {
			lastCurrentEnd = end
		}

		rawBlock, err := db.blkStore.readBlock(&entry.hash, loc)
		if err != nil {
			return err
		}

		newBytes, proof, err := transform(&entry.hash, rawBlock)
		if err != nil {
			return err
		}
		if proof != nil {
			existing, err := tx.FetchUtreexoProof(&entry.hash)
			if err != nil {
				return err
			}
			if existing == nil {
				if err := tx.StoreUtreexoProof(&entry.hash,
					proof); err != nil {

					return err
				}
			}
		}

		if !bytes.Equal(newBytes, rawBlock) {
			changed = true
		}

		record := db.blkStore.serializeBlockRecord(newBytes)
		if rewritten.Len()+len(record) >
			int(db.blkStore.maxBlockFileSize) {

			str := fmt.Sprintf("rewritten block file %d is too "+
				"large: got %d bytes, want at most %d", fileNum,
				rewritten.Len()+len(record),
				db.blkStore.maxBlockFileSize)
			return makeDbErr(database.ErrDriverSpecific, str, nil)
		}

		newLocs = append(newLocs, blockFileRewriteEntry{
			hash: entry.hash,
			loc: blockLocation{
				blockFileNum: fileNum,
				fileOffset:   uint32(rewritten.Len()),
				blockLen:     uint32(len(record)),
			},
		})
		_, _ = rewritten.Write(record)
	}

	if !changed {
		return nil
	}

	// Refuse to rewrite a file that no longer ends where its records do.
	// The migration runs while nothing else writes, so a larger file
	// means a block was appended concurrently and the rewrite would drop
	// it.  Rerunning the migration after the writer is done succeeds.
	size, err := db.blkStore.fileSize(fileNum)
	if err != nil {
		return err
	}
	if lastCurrentEnd != size {
		str := fmt.Sprintf("block file %d was modified while it was "+
			"being rewritten: file size %d, want %d.  Rerun the "+
			"migration", fileNum, size, lastCurrentEnd)
		return makeDbErr(database.ErrDriverSpecific, str, nil)
	}

	restore, err := db.blkStore.rewriteBlockFile(fileNum,
		rewritten.Bytes())
	if err != nil {
		return err
	}

	for _, entry := range newLocs {
		if err := tx.blockIdxBucket.Put(entry.hash[:],
			serializeBlockLoc(entry.loc)); err != nil {

			restore()
			return err
		}
	}
	return nil
}

// serializeBlockRecord returns the complete flat-file record for raw block
// bytes, including the network, block length, and checksum.
func (s *blockStore) serializeBlockRecord(rawBlock []byte) []byte {
	blockLen := uint32(len(rawBlock))
	fullLen := blockLen + 12
	record := make([]byte, fullLen)

	byteOrder.PutUint32(record[0:4], uint32(s.network))
	byteOrder.PutUint32(record[4:8], blockLen)
	copy(record[8:8+blockLen], rawBlock)

	checksum := crc32.Checksum(record[:fullLen-4], castagnoli)
	binary.BigEndian.PutUint32(record[fullLen-4:], checksum)
	return record
}

// writeBlockRecordAt writes a complete flat-file block record at the provided
// location.  When truncateSize is non-nil, the file is truncated after the
// record write succeeds.
func (s *blockStore) writeBlockRecordAt(fileNum, offset uint32,
	record []byte, truncateSize *uint32) error {

	// Write through the current write file's handle when the provided
	// file is the current write file and its handle is open.  Otherwise
	// the file is opened write-only just for the write.
	wc := s.writeCursor
	wc.RLock()
	curFile := wc.curFile
	writeThroughCursor := fileNum == wc.curFileNum && curFile.file != nil
	wc.RUnlock()

	if !writeThroughCursor {
		return s.writeRecordToFile(fileNum, offset, record, truncateSize)
	}

	return s.writeRecordToCursorFile(curFile, fileNum, offset, record,
		truncateSize)
}

// writeRecordToCursorFile writes the record through the provided current
// write file handle and keeps the write cursor at the end of a truncated
// file.  The handle is checked again under its lock in case a cursor rollover
// closed it after the caller's check, in which case the file is opened
// write-only instead.  The file lock is released before the write cursor is
// updated, since the cursor lock must never be acquired while holding the
// file lock, which writeBlock acquires in the opposite order.
func (s *blockStore) writeRecordToCursorFile(curFile *lockableFile,
	fileNum, offset uint32, record []byte, truncateSize *uint32) error {

	curFile.Lock()
	file := curFile.file
	if file != nil {
		err := writeRecordAt(file, fileNum, offset, record, truncateSize)
		curFile.Unlock()
		if err != nil {
			return err
		}

		s.updateWriteCursor(fileNum, truncateSize)
		return nil
	}
	curFile.Unlock()

	return s.writeRecordToFile(fileNum, offset, record, truncateSize)
}

// writeRecordToFile opens the file write-only, closing any read-only handle
// that is cached for it, and writes the record at the provided offset.  The
// overall files lock is held, following the documented lock order of overall
// files, LRU, and then the file, so a reader cannot reopen the file between
// the time its read-only handle is closed and the record is written.
func (s *blockStore) writeRecordToFile(fileNum, offset uint32,
	record []byte, truncateSize *uint32) error {

	s.obfMutex.Lock()
	defer s.obfMutex.Unlock()

	if openFile, ok := s.openBlockFiles[fileNum]; ok {
		// Close the read-only handle so the file can be opened
		// write-only below.  The file lock waits for any readers that
		// are still using the handle.
		openFile.Lock()
		_ = openFile.file.Close()
		openFile.file = nil
		openFile.Unlock()

		if elem := s.fileNumToLRUElem[fileNum]; elem != nil {
			s.lruMutex.Lock()
			s.openBlocksLRU.Remove(elem)
			s.lruMutex.Unlock()
			delete(s.fileNumToLRUElem, fileNum)
		}
		delete(s.openBlockFiles, fileNum)
	}

	file, err := s.openWriteFileFunc(fileNum)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := writeRecordAt(file, fileNum, offset, record,
		truncateSize); err != nil {

		return err
	}

	// The write cursor can point at this file without an open handle,
	// such as right after a database open before the first block is
	// written.  Keep it at the end of the rewritten file so the cursor
	// metadata matches the truncated file.
	s.updateWriteCursor(fileNum, truncateSize)
	return nil
}

// updateWriteCursor keeps the write cursor at the end of the file after a
// truncating rewrite when the file is the current write file.
func (s *blockStore) updateWriteCursor(fileNum uint32,
	truncateSize *uint32) {

	if truncateSize == nil {
		return
	}

	wc := s.writeCursor
	wc.Lock()
	defer wc.Unlock()

	if wc.curFileNum == fileNum {
		wc.curOffset = *truncateSize
	}
}

// writeRecordAt writes the record at the provided offset and truncates the
// file afterwards when a truncate size is provided.
func writeRecordAt(file filer, fileNum, offset uint32, record []byte,
	truncateSize *uint32) error {

	n, err := file.WriteAt(record, int64(offset))
	if err != nil {
		str := fmt.Sprintf("failed to write block record to file %d "+
			"at offset %d: %v", fileNum, offset, err)
		return makeDbErr(database.ErrDriverSpecific, str, err)
	}
	if n != len(record) {
		str := fmt.Sprintf("short write to file %d at offset %d: "+
			"wrote %d bytes, want %d", fileNum, offset, n,
			len(record))
		return makeDbErr(database.ErrDriverSpecific, str, nil)
	}
	if truncateSize != nil {
		if err := file.Truncate(int64(*truncateSize)); err != nil {
			str := fmt.Sprintf("failed to truncate file %d to "+
				"offset %d: %v", fileNum, *truncateSize, err)
			return makeDbErr(database.ErrDriverSpecific, str, err)
		}
	}
	return nil
}

// rewriteBlockFile replaces the bytes of an entire block file and truncates it
// to the new length.  It returns a best-effort rollback closure that restores
// the previous file bytes.
func (s *blockStore) rewriteBlockFile(fileNum uint32,
	newBytes []byte) (func(), error) {

	if len(newBytes) > int(s.maxBlockFileSize) {
		str := fmt.Sprintf("rewritten block file %d is too large: got "+
			"%d bytes, want at most %d", fileNum, len(newBytes),
			s.maxBlockFileSize)
		return nil, makeDbErr(database.ErrDriverSpecific, str, nil)
	}

	oldSize, err := s.fileSizeFunc(fileNum)
	if err != nil {
		str := fmt.Sprintf("failed to get size for block file %d: %v",
			fileNum, err)
		return nil, makeDbErr(database.ErrDriverSpecific, str, err)
	}

	blockFile, err := s.blockFile(fileNum)
	if err != nil {
		return nil, err
	}
	oldBytes := make([]byte, oldSize)
	n, err := blockFile.file.ReadAt(oldBytes, 0)
	blockFile.RUnlock()
	if err != nil {
		str := fmt.Sprintf("failed to read block file %d: %v", fileNum,
			err)
		return nil, makeDbErr(database.ErrDriverSpecific, str, err)
	}
	if n != len(oldBytes) {
		str := fmt.Sprintf("short read for block file %d: got %d bytes, "+
			"want %d", fileNum, n, len(oldBytes))
		return nil, makeDbErr(database.ErrDriverSpecific, str, nil)
	}

	newSize := uint32(len(newBytes))
	if err := s.writeBlockRecordAt(fileNum, 0, newBytes, &newSize); err != nil {
		return nil, err
	}

	restore := func() {
		if err := s.writeBlockRecordAt(fileNum, 0, oldBytes, &oldSize); err != nil {
			log.Warnf("ROLLBACK: Failed to restore block file %d: %v",
				fileNum, err)
		}
	}

	return restore, nil
}
