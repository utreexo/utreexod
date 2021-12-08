// Copyright (c) 2015-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ffldb

import (
	"fmt"
	"hash/crc32"

	"github.com/utreexo/utreexod/database"
)

// The serialized write cursor location format is:
//
//  [0:4]  Block file (4 bytes)
//  [4:8]  File offset (4 bytes)
//  [8:12] Castagnoli CRC-32 checksum (4 bytes)

// serializeWriteRow serialize the current block file and offset where new
// will be written into a format suitable for storage into the metadata.
func serializeWriteRow(curBlockFileNum, curFileOffset uint32) []byte {
	var serializedRow [12]byte
	byteOrder.PutUint32(serializedRow[0:4], curBlockFileNum)
	byteOrder.PutUint32(serializedRow[4:8], curFileOffset)
	checksum := crc32.Checksum(serializedRow[:8], castagnoli)
	byteOrder.PutUint32(serializedRow[8:12], checksum)
	return serializedRow[:]
}

// deserializeWriteRow deserializes the write cursor location stored in the
// metadata.  Returns ErrCorruption if the checksum of the entry doesn't match.
func deserializeWriteRow(writeRow []byte) (uint32, uint32, error) {
	// Ensure the checksum matches.  The checksum is at the end.
	gotChecksum := crc32.Checksum(writeRow[:8], castagnoli)
	wantChecksumBytes := writeRow[8:12]
	wantChecksum := byteOrder.Uint32(wantChecksumBytes)
	if gotChecksum != wantChecksum {
		str := fmt.Sprintf("metadata for write cursor does not match "+
			"the expected checksum - got %d, want %d", gotChecksum,
			wantChecksum)
		return 0, 0, makeDbErr(database.ErrCorruption, str, nil)
	}

	fileNum := byteOrder.Uint32(writeRow[0:4])
	fileOffset := byteOrder.Uint32(writeRow[4:8])
	return fileNum, fileOffset, nil
}

// reconcileDB reconciles the metadata with the flat block files on disk.  It
// will also initialize the underlying database if the create flag is set.
func reconcileDB(pdb *db, create bool) (database.DB, error) {
	// Perform initial internal bucket and value creation during database
	// creation.
	if create {
		if err := initDB(pdb.cache.ldb); err != nil {
			return nil, err
		}
	}

	// Load the current write cursor position from the metadata.
	var blkCurFileNum, blkCurOffset uint32
	var sjCurFileNum, sjCurOffset uint32
	err := pdb.View(func(tx database.Tx) error {
		// Get write row for blocks.
		blkWriteRow := tx.Metadata().Get(blkWriteLocKeyName)
		if blkWriteRow == nil {
			str := "block write cursor does not exist"
			return makeDbErr(database.ErrCorruption, str, nil)
		}

		var err error
		blkCurFileNum, blkCurOffset, err = deserializeWriteRow(blkWriteRow)
		if err != nil {
			return err
		}

		// Get write row for spend journals.
		sjWriteRow := tx.Metadata().Get(sjWriteLocKeyName)
		if sjWriteRow == nil {
			str := "spend journal write cursor does not exist"
			return makeDbErr(database.ErrCorruption, str, nil)
		}

		sjCurFileNum, sjCurOffset, err = deserializeWriteRow(sjWriteRow)
		if err != nil {
			return err
		}

		return err
	})
	if err != nil {
		return nil, err
	}

	// Bool to check if we need to roll back.
	needsRollBack := false

	// When the write cursor position found by scanning the block files on
	// disk is AFTER the position the metadata believes to be true, truncate
	// the files on disk to match the metadata.  This can be a fairly common
	// occurrence in unclean shutdown scenarios while the block files are in
	// the middle of being written.  Since the metadata isn't updated until
	// after the block data is written, this is effectively just a rollback
	// to the known good point before the unclean shutdown.
	blkWC := pdb.blkStore.writeCursor
	if blkWC.curFileNum > blkCurFileNum || (blkWC.curFileNum == blkCurFileNum &&
		blkWC.curOffset > blkCurOffset) {

		log.Info("Detected unclean shutdown for block files- Repairing...")
		log.Debugf("Metadata claims file %d, offset %d. Block data is "+
			"at file %d, offset %d", blkCurFileNum, blkCurOffset,
			blkWC.curFileNum, blkWC.curOffset)
		needsRollBack = true
	}

	// When the write cursor position found by scanning the block files on
	// disk is BEFORE the position the metadata believes to be true, return
	// a corruption error.  Since sync is called after each block is written
	// and before the metadata is updated, this should only happen in the
	// case of missing, deleted, or truncated block files, which generally
	// is not an easily recoverable scenario.  In the future, it might be
	// possible to rescan and rebuild the metadata from the block files,
	// however, that would need to happen with coordination from a higher
	// layer since it could invalidate other metadata.
	if blkWC.curFileNum < blkCurFileNum || (blkWC.curFileNum == blkCurFileNum &&
		blkWC.curOffset < blkCurOffset) {

		str := fmt.Sprintf("metadata claims file %d, offset %d, but "+
			"block data is at file %d, offset %d", blkCurFileNum,
			blkCurOffset, blkWC.curFileNum, blkWC.curOffset)
		log.Warnf("***Database corruption detected***: %v", str)
		return nil, makeDbErr(database.ErrCorruption, str, nil)
	}

	// Same thing for undo block files.
	sjWC := pdb.sjStore.writeCursor
	if sjWC.curFileNum > sjCurFileNum || (sjWC.curFileNum == sjCurFileNum &&
		sjWC.curOffset > sjCurOffset) {

		log.Info("Detected unclean shutdown for undo files - Repairing...")
		log.Debugf("Metadata claims file %d, offset %d. Spend journal data is "+
			"at file %d, offset %d", sjCurFileNum, sjCurOffset,
			sjWC.curFileNum, sjWC.curOffset)
		needsRollBack = true
	}
	if sjWC.curFileNum < sjCurFileNum || (sjWC.curFileNum == sjCurFileNum &&
		sjWC.curOffset < sjCurOffset) {

		str := fmt.Sprintf("metadata claims file %d, offset %d, but "+
			"spend journal data is at file %d, offset %d", sjCurFileNum,
			sjCurOffset, sjWC.curFileNum, sjWC.curOffset)
		log.Warnf("***Database corruption detected***: %v", str)
		return nil, makeDbErr(database.ErrCorruption, str, nil)
	}

	if needsRollBack {
		pdb.blkStore.handleRollback(blkCurFileNum, blkCurOffset)
		pdb.sjStore.handleRollback(sjCurFileNum, sjCurOffset)
		log.Infof("Database sync complete")
	}

	return pdb, nil
}
