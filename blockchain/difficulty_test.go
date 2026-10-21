// Copyright (c) 2014-2017 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"math/big"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg/v2"
)

// TestBigToCompact ensures BigToCompact converts big integers to the expected
// compact representation.
func TestBigToCompact(t *testing.T) {
	tests := []struct {
		in  int64
		out uint32
	}{
		{0, 0},
		{-1, 25231360},
	}

	for x, test := range tests {
		n := big.NewInt(test.in)
		r := BigToCompact(n)
		if r != test.out {
			t.Errorf("TestBigToCompact test #%d failed: got %d want %d\n",
				x, r, test.out)
			return
		}
	}
}

// TestCompactToBig ensures CompactToBig converts numbers using the compact
// representation to the expected big intergers.
func TestCompactToBig(t *testing.T) {
	tests := []struct {
		in  uint32
		out int64
	}{
		{10000000, 0},
	}

	for x, test := range tests {
		n := CompactToBig(test.in)
		want := big.NewInt(test.out)
		if n.Cmp(want) != 0 {
			t.Errorf("TestCompactToBig test #%d failed: got %d want %d\n",
				x, n.Int64(), want.Int64())
			return
		}
	}
}

// TestCalcWork ensures CalcWork calculates the expected work value from values
// in compact representation.
func TestCalcWork(t *testing.T) {
	tests := []struct {
		in  uint32
		out int64
	}{
		{10000000, 0},
	}

	for x, test := range tests {
		bits := test.in

		r := CalcWork(bits)
		if r.Int64() != test.out {
			t.Errorf("TestCalcWork test #%d failed: got %v want %d\n",
				x, r.Int64(), test.out)
			return
		}
	}
}

// TestCalcNextRequiredDifficultyNoRetargeting ensures that the difficulty does
// not retarget on networks with PoWNoRetargeting set, and still retargets on
// networks without it.
func TestCalcNextRequiredDifficultyNoRetargeting(t *testing.T) {
	tests := []struct {
		name   string
		params *chaincfg.Params
		want   uint32
	}{
		{
			// Regtest does not retarget, like Bitcoin Core.
			name:   "regtest",
			params: &chaincfg.RegressionNetParams,
			want:   chaincfg.RegressionNetParams.PowLimitBits,
		},
		{
			// Simnet has the same proof of work limit and retarget
			// parameters as regtest, but retargets.  Blocks one
			// second apart make the target four times harder.
			name:   "simnet",
			params: &chaincfg.SimNetParams,
			want:   0x201fffff,
		},
	}

	for _, test := range tests {
		chain := newFakeChain(test.params)

		// Extend the chain up to the block before the first retarget
		// height.
		node := chain.bestChain.Tip()
		blockTime := node.Header().Timestamp
		for i := int32(1); i < chain.blocksPerRetarget; i++ {
			blockTime = blockTime.Add(time.Second)
			node = newFakeNode(node, 1, test.params.PowLimitBits,
				blockTime)
			chain.index.AddNode(node)
			chain.bestChain.SetTip(node)
		}

		got, err := chain.calcNextRequiredDifficulty(node,
			blockTime.Add(time.Second))
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if got != test.want {
			t.Errorf("%s: got bits %08x, want %08x", test.name, got,
				test.want)
		}
	}
}
