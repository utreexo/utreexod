package blockchain

import (
	"bytes"
	"crypto/sha256"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/wire"
)

func genTTLAcc(t *testing.T, ttls []wire.UtreexoTTL) utreexo.Pollard {
	p := utreexo.NewAccumulator()

	// Add ttl for block 0. This is so that the ttl on height 1 also has a target
	// of 1.
	emptyTTL := wire.UtreexoTTL{}
	buf := bytes.NewBuffer(make([]byte, 0, emptyTTL.SerializeSize()))
	err := emptyTTL.Serialize(buf)
	if err != nil {
		t.Fatal(err)
	}
	rootHash := sha256.Sum256(buf.Bytes())
	err = p.Modify([]utreexo.Leaf{{Hash: rootHash}}, nil, utreexo.Proof{})
	if err != nil {
		t.Fatal(err)
	}

	for _, ttl := range ttls {
		buf := bytes.NewBuffer(make([]byte, 0, ttl.SerializeSize()))
		err = ttl.Serialize(buf)
		if err != nil {
			t.Fatal(err)
		}
		rootHash := sha256.Sum256(buf.Bytes())

		err = p.Modify([]utreexo.Leaf{{Hash: rootHash}}, nil, utreexo.Proof{})
		if err != nil {
			t.Fatal(err)
		}
	}

	return p
}

func cmpTTLInfo(a, b wire.TTLInfo) int {
	switch {
	case a.DeathHeight > b.DeathHeight:
		return 1
	case a.DeathHeight < b.DeathHeight:
		return -1
	default:
		switch {
		case a.DeathBlkIndex > b.DeathBlkIndex:
			return 1
		case a.DeathBlkIndex < b.DeathBlkIndex:
			return -1
		default:
			return 0
		}
	}
}

func getCachedTargets(maxCache int) ([][]wire.TTLInfo, []wire.TTLInfo, [][]wire.TTLInfo, []uint64, [][]bool) {
	cachePile := make([]wire.TTLInfo, 0, len(testTTLs))
	cachedTTLInfos := make([][]wire.TTLInfo, len(testTTLs))
	uncached := make([][]wire.TTLInfo, len(testTTLs))
	numLeaves := make([]uint64, len(testTTLs))
	toCaches := make([][]bool, len(testTTLs))

	for i, testTTL := range testTTLs {
		if i == 0 {
			numLeaves[i] = uint64(len(testTTL.TTLs))
		} else {
			numLeaves[i] = numLeaves[i-1] + uint64(len(testTTL.TTLs))
		}

		// Pop off deleted ones.
		idx := 0
		for idx = range cachePile {
			if cachePile[idx].DeathHeight == testTTL.BlockHeight {
				cachedTTLInfos[i] = append(cachedTTLInfos[i], cachePile[idx])
				if idx == len(cachePile)-1 {
					// Need to +1 so that the entire slice gets reset.
					idx++
				}
			} else {
				break
			}
		}
		cachePile = cachePile[idx:]

		// Add new ones.
		toCaches[i] = make([]bool, len(testTTL.TTLs))
		for j, ttlInfo := range testTTL.TTLs {
			if ttlInfo.DeathHeight == 0 {
				continue
			}

			if len(cachePile) < maxCache {
				cachePile = append(cachePile, ttlInfo)
				toCaches[i][j] = true
			} else {
				maxItem := cachePile[maxCache-1]
				if cmpTTLInfo(ttlInfo, maxItem) < 0 {
					uncached[i] = append(uncached[i], maxItem)
					cachePile[maxCache-1] = ttlInfo
					toCaches[i][j] = true
				}
			}

			// Sort.
			slices.SortFunc(cachePile, cmpTTLInfo)
		}
	}

	// Cast []wire.TTLInfo(nil) to []wire.TTLInfo{}.
	// This is so that the tests pass.
	for i := range uncached {
		if len(uncached[i]) == 0 {
			uncached[i] = []wire.TTLInfo{}
		}
	}

	return cachedTTLInfos, cachePile, uncached, numLeaves, toCaches
}

var testTargets = [][]uint64{
	{},              // 1
	{},              // 2
	{4},             // 3
	{5, 6},          // 4
	{0, 1, 2, 7, 8}, // 5
	{9, 3},          // 6
}

var testTTLs []wire.UtreexoTTL = []wire.UtreexoTTL{
	{
		BlockHeight: 1,
		TTLs: []wire.TTLInfo{
			{
				DeathHeight:   5,
				DeathBlkIndex: 0,
				DeathPos:      0,
			},
			{
				DeathHeight:   5,
				DeathBlkIndex: 1,
				DeathPos:      1,
			},
			{
				DeathHeight:   5,
				DeathBlkIndex: 2,
				DeathPos:      2,
			},
			{
				DeathHeight:   6,
				DeathBlkIndex: 1,
				DeathPos:      3,
			},
		},
	},
	{
		BlockHeight: 2,
		TTLs: []wire.TTLInfo{
			{
				DeathHeight:   0,
				DeathBlkIndex: 0,
				DeathPos:      0,
			},
			{
				DeathHeight:   3,
				DeathBlkIndex: 0,
				DeathPos:      4,
			},
		},
	},
	{
		BlockHeight: 3,
		TTLs: []wire.TTLInfo{
			{
				DeathHeight:   4,
				DeathBlkIndex: 0,
				DeathPos:      5,
			},
			{
				DeathHeight:   4,
				DeathBlkIndex: 1,
				DeathPos:      6,
			},
		},
	},
	{
		BlockHeight: 4,
		TTLs: []wire.TTLInfo{
			{
				DeathHeight:   5,
				DeathBlkIndex: 3,
				DeathPos:      7,
			},
			{
				DeathHeight:   5,
				DeathBlkIndex: 4,
				DeathPos:      8,
			},
		},
	},
	{
		BlockHeight: 5,
		TTLs: []wire.TTLInfo{
			{
				DeathHeight:   6,
				DeathBlkIndex: 0,
				DeathPos:      9,
			},
		},
	},
	{
		BlockHeight: 6,
		TTLs: []wire.TTLInfo{
			{
				DeathHeight:   0,
				DeathBlkIndex: 0,
				DeathPos:      0,
			},
		},
	},
}

func TestProcessTTLs(t *testing.T) {
	p := genTTLAcc(t, testTTLs)
	stump := utreexo.Stump{
		Roots:     p.GetRoots(),
		NumLeaves: p.NumLeaves,
	}

	tests := []struct {
		maxCache   uint32
		bestHeight int32
		msgTTL     *wire.MsgUtreexoTTLs
	}{
		{
			maxCache:   3,
			bestHeight: 6,
			msgTTL: func() *wire.MsgUtreexoTTLs {
				hashes := make([]utreexo.Hash, 0, len(testTTLs))
				for _, ttl := range testTTLs {
					buf := bytes.NewBuffer(make([]byte, 0, ttl.SerializeSize()))
					err := ttl.Serialize(buf)
					if err != nil {
						t.Fatal(err)
					}
					rootHash := sha256.Sum256(buf.Bytes())
					hashes = append(hashes, rootHash)
				}

				proof, err := p.Prove(hashes)
				if err != nil {
					t.Fatal(err)
				}

				return &wire.MsgUtreexoTTLs{
					TTLs:        testTTLs,
					ProofHashes: proof.Proof,
				}
			}(),
		},
	}

	for _, test := range tests {
		uc := InitUtreexoTTLCache(&stump, test.maxCache, 0, 0)
		err := uc.ProcessTTLs(test.msgTTL)
		if err != nil {
			t.Fatal(err)
		}

		for _, utreexoTTL := range test.msgTTL.TTLs {
			// Test HaveTTLInfo.
			height := utreexoTTL.BlockHeight
			require.True(t, uc.HaveTTLInfo(int32(height)))

			// Assert targets.
			targets, found := uc.targets[height]
			require.True(t, found)
			require.Equal(t, testTargets[height-1], targets)
		}

		// Assert cached targets and targets to uncache.
		cachedTTLInfos, currentCache, uncached, numLeaves, toCaches := getCachedTargets(int(test.maxCache))
		for i := range cachedTTLInfos {
			gotCached := uc.cachedTTLInfos[uint32(i+1)]
			require.Equal(t, cachedTTLInfos[i], gotCached)

			gotUncache := uc.leavesToUncache[uint32(i+1)]
			require.Equal(t, uncached[i], gotUncache)

			gotNumLeaves := uc.numLeaves[int32(i+1)]
			require.Equal(t, numLeaves[i], gotNumLeaves)

			gotToCache := uc.leavesToCache[uint32(i+1)]
			require.Equal(t, toCaches[i], gotToCache)
		}

		// Assert current cached ttls,
		values := uc.cachedTTLs.Values()
		currentCachedTTLInfos := make([]wire.TTLInfo, len(values))
		for i := range currentCachedTTLInfos {
			currentCachedTTLInfos[i] = values[i].(wire.TTLInfo)
		}
		require.Equal(t, currentCache, currentCachedTTLInfos)

		for _, utreexoTTL := range test.msgTTL.TTLs {
			height := utreexoTTL.BlockHeight
			uc.RemoveCache(int32(height))

			require.False(t, uc.HaveTTLInfo(int32(height)))
		}
	}
}
