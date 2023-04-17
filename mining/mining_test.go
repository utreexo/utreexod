// Copyright (c) 2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mining

import (
	"container/heap"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/utreexo/utreexod/btcutil"
)

// TestTxFeePrioHeap ensures the priority queue for transaction fees and
// priorities works as expected.
func TestTxFeePrioHeap(t *testing.T) {
	// Create some fake priority items that exercise the expected sort
	// edge conditions.
	testItems := []*txPrioItem{
		{feePerKB: 5678, priority: 3},
		{feePerKB: 5678, priority: 1},
		{feePerKB: 5678, priority: 1}, // Duplicate fee and prio
		{feePerKB: 5678, priority: 5},
		{feePerKB: 5678, priority: 2},
		{feePerKB: 1234, priority: 3},
		{feePerKB: 1234, priority: 1},
		{feePerKB: 1234, priority: 5},
		{feePerKB: 1234, priority: 5}, // Duplicate fee and prio
		{feePerKB: 1234, priority: 2},
		{feePerKB: 10000, priority: 0}, // Higher fee, smaller prio
		{feePerKB: 0, priority: 10000}, // Higher prio, lower fee
	}

	// Add random data in addition to the edge conditions already manually
	// specified.
	randSeed := rand.Int63()
	defer func() {
		if t.Failed() {
			t.Logf("Random numbers using seed: %v", randSeed)
		}
	}()
	prng := rand.New(rand.NewSource(randSeed))
	for i := 0; i < 1000; i++ {
		testItems = append(testItems, &txPrioItem{
			feePerKB: int64(prng.Float64() * btcutil.SatoshiPerBitcoin),
			priority: prng.Float64() * 100,
		})
	}

	// Test sorting by fee per KB then priority.
	var highest *txPrioItem
	priorityQueue := newTxPriorityQueue(len(testItems), true)
	for i := 0; i < len(testItems); i++ {
		prioItem := testItems[i]
		if highest == nil {
			highest = prioItem
		}
		if prioItem.feePerKB >= highest.feePerKB &&
			prioItem.priority > highest.priority {

			highest = prioItem
		}
		heap.Push(priorityQueue, prioItem)
	}

	for i := 0; i < len(testItems); i++ {
		prioItem := heap.Pop(priorityQueue).(*txPrioItem)
		if prioItem.feePerKB >= highest.feePerKB &&
			prioItem.priority > highest.priority {

			t.Fatalf("fee sort: item (fee per KB: %v, "+
				"priority: %v) higher than than prev "+
				"(fee per KB: %v, priority %v)",
				prioItem.feePerKB, prioItem.priority,
				highest.feePerKB, highest.priority)
		}
		highest = prioItem
	}

	// Test sorting by priority then fee per KB.
	highest = nil
	priorityQueue = newTxPriorityQueue(len(testItems), false)
	for i := 0; i < len(testItems); i++ {
		prioItem := testItems[i]
		if highest == nil {
			highest = prioItem
		}
		if prioItem.priority >= highest.priority &&
			prioItem.feePerKB > highest.feePerKB {

			highest = prioItem
		}
		heap.Push(priorityQueue, prioItem)
	}

	for i := 0; i < len(testItems); i++ {
		prioItem := heap.Pop(priorityQueue).(*txPrioItem)
		if prioItem.priority >= highest.priority &&
			prioItem.feePerKB > highest.feePerKB {

			t.Fatalf("priority sort: item (fee per KB: %v, "+
				"priority: %v) higher than than prev "+
				"(fee per KB: %v, priority %v)",
				prioItem.feePerKB, prioItem.priority,
				highest.feePerKB, highest.priority)
		}
		highest = prioItem
	}
}

func TestTxDescMarshal(t *testing.T) {
	tests := []struct {
		name string
		tx   TxDesc
	}{
		{
			name: "txid 114351cae78bdb68d718c27a221f0127816810c124a096d6355d6f958264bbb7 on signet",
			tx: TxDesc{
				Tx: func() *btcutil.Tx {
					rawStr := "020000000001014c4dcdaa45e3711f6e2147efccd1b106f81dc4b5205f306dff9e8c88d6b95dac0" +
						"000000000fdffffff0321ff5f101b000000160014447fde1e37d97255b5821d2dee816e8f18f6bac9" +
						"7cd1000000000000160014a8628755900c899f9de1a1bb07fa76ee9d773e5d00000000000000000f6" +
						"a0d6c6561726e20426974636f696e0247304402207e7088b7a528089842feed081f0bb296ac2c0303" +
						"7ea74918747def43025e666a02201d3a3000013509e0ce4ba73e8363aaaa51114eab6f66820aa430f" +
						"604e599fa8c012102a8c3fa3dbc022ca7c9a2214c5e673833317b3cff37c0fc170fc347f1a2f6b6e2" +
						"00000000"
					bytes, err := hex.DecodeString(rawStr)
					if err != nil {
						panic(err)
					}

					tx, err := btcutil.NewTxFromBytes(bytes)
					if err != nil {
						panic(err)
					}

					return tx
				}(),
				Added: func() time.Time {
					bytes := []byte("2023-04-17T13:26:48.250092856+09:00")

					var t time.Time
					err := t.UnmarshalText(bytes)
					if err != nil {
						panic(err)
					}

					return t
				}(),
				Height:   138955,
				Fee:      165,
				FeePerKB: 1000,
			},
		},
	}

	for _, test := range tests {
		// Marshal the TxDesc to JSON.
		bytes, err := json.Marshal(test.tx)
		if err != nil {
			t.Fatalf("failed to marshal TxDesc: %v", err)
		}

		// Unmarshal the JSON bytes back into a TxDesc struct.
		var tx TxDesc
		if err := json.Unmarshal(bytes, &tx); err != nil {
			t.Fatalf("failed to unmarshal JSON into TxDesc: %v", err)
		}

		// Ensure the unmarshaled TxDesc struct is the same as the original.
		if !reflect.DeepEqual(test.tx, tx) {
			t.Fatalf("unmarshaled TxDesc does not match the original")
		}
	}
}
