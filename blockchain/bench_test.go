// Copyright (c) 2015 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"testing"

	"github.com/utreexo/utreexod/btcutil"
)

// BenchmarkIsCoinBase performs a simple benchmark against the IsCoinBase
// function.
func BenchmarkIsCoinBase(b *testing.B) {
	tx, _ := btcutil.NewBlock(&Block100000).Tx(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsCoinBase(tx)
	}
}

// BenchmarkIsCoinBaseTx performs a simple benchmark against the IsCoinBaseTx
// function.
func BenchmarkIsCoinBaseTx(b *testing.B) {
	tx := Block100000.Transactions[1]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsCoinBaseTx(tx)
	}
}

func BenchmarkAncestor(b *testing.B) {
	height := 1 << 19
	blockNodes := chainedNodes(nil, height)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blockNodes[len(blockNodes)-1].Ancestor(0)
		for j := 0; j <= 19; j++ {
			blockNodes[len(blockNodes)-1].Ancestor(1 << j)
		}
	}
}
