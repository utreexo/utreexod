package sizehelper

// These constants are related to bitcoin.
const (
	// outpointSize is the size of an outpoint.
	//
	// This value is calculated by running the following:
	//	unsafe.Sizeof(wire.OutPoint{})
	OutpointSize = 36

	// uint64Size is the size of an uint64 allocated in memory.
	Uint64Size = 8

	// UtxoCacheBucketSize is the size of the bucket in the cache map.  Exact
	// calculation is (16 + keysize*8 + valuesize*8) where for the map of:
	// map[wire.OutPoint]*UtxoEntry would have a keysize=36 and valuesize=8.
	//
	// https://github.com/golang/go/issues/34561#issuecomment-536115805
	UtxoCacheBucketSize = 16 + Uint64Size*OutpointSize + Uint64Size*Uint64Size

	// BaseEntrySize is calculated by running the following on a 64-bit system:
	//   unsafe.Sizeof(blockchain.UtxoEntry{})
	BaseEntrySize = 40

	// PubKeyHashLen is the length of a P2PKH script.
	PubKeyHashLen = 25

	// AvgEntrySize is how much each entry we expect it to be.  Since most
	// txs are p2pkh, we can assume the entry to be more or less the size
	// of a p2pkh tx.  We add on 7 to make it 32 since 64 bit systems will
	// align by 8 bytes.
	AvgEntrySize = BaseEntrySize + (PubKeyHashLen + 7)
)
