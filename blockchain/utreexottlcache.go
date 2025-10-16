package blockchain

import (
	"bytes"
	"container/heap"
	"crypto/sha256"
	"fmt"
	"slices"
	"sort"

	"github.com/emirpasic/gods/trees/redblacktree"
	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/wire"
)

// TTLHeap is a priority queue for the ttlTargets. Used to grab all the targets at the next earliest
// block height.
type TTLHeap []wire.TTLInfo

func (h TTLHeap) Len() int { return len(h) }
func (h TTLHeap) Less(i, j int) bool {
	if h[i].DeathHeight == h[j].DeathHeight {
		return h[i].DeathBlkIndex < h[j].DeathBlkIndex
	}
	return h[i].DeathHeight < h[j].DeathHeight
}
func (h TTLHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h TTLHeap) View() any {
	if len(h) == 0 {
		return nil
	}
	return h[0]
}
func (h *TTLHeap) Push(x any) { *h = append(*h, x.(wire.TTLInfo)) }
func (h *TTLHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// TTLInfoComparator provides a basic comparison on wire.TTLInfo.
func TTLInfoComparator(a, b any) int {
	aAsserted := a.(wire.TTLInfo)
	bAsserted := b.(wire.TTLInfo)

	switch {
	case aAsserted.DeathHeight > bAsserted.DeathHeight:
		return 1
	case aAsserted.DeathHeight < bAsserted.DeathHeight:
		return -1
	default:
		switch {
		case aAsserted.DeathBlkIndex > bAsserted.DeathBlkIndex:
			return 1
		case aAsserted.DeathBlkIndex < bAsserted.DeathBlkIndex:
			return -1
		default:
			return 0
		}
	}
}

// getTargetsAtHeight returns all the targets at the passed in height.
//
// NOTE: if the height given is greater than the next ttlInfo's deathHeight,
// the returned slice will be empty.
func getTargetsAtHeight(h *TTLHeap, height uint32) []uint64 {
	targets := []uint64{}
	for h.Len() > 0 {
		item := h.View().(wire.TTLInfo)
		if item.DeathHeight != height {
			break
		}

		if item.DeathHeight == height {
			targets = append(targets, item.DeathPos)
			heap.Pop(h)
		}
	}

	return targets
}

// leafDataAndHash is just the leaf data and its hash.
type leafDataAndHash struct {
	ld   wire.LeafData
	hash utreexo.Hash
}

// UtreexoTTLCache is a convenient structure to process the UtreexoTTLs and figure out
// which TTLs to cache as well as holding onto all necessary data to recreate a full
// utreexo proof.
//
// NOTE: when utilizing the UtreexoTTLCache, there shouldn't be any other cache structure
// being utilized as that may lead to failtures in generating the full utreexo proof.
type UtreexoTTLCache struct {
	// The below are used to keep stats about the cache.
	totalProofSize      uint64
	downloadedProofSize int
	allTargets          uint64
	cachedTargetCount   uint64

	// The below are static and are not changed after initialization.
	cacheCount     uint32
	ttlAccumulator *utreexo.Stump

	// The height of the best ttl processed so far.
	currentHeight int32

	// The ttls that passed verificiation at the given heights.
	ttls map[int32]wire.UtreexoTTL

	// The numleaves of the utreexo accumulator for the utxo set.
	numLeaves map[int32]uint64

	// The below are needed to calculated and cache the full target positions
	// of a utreexo proof per each block
	targetSorter TTLHeap
	targets      map[uint32][]uint64

	// The targets that are cached.
	cachedTTLs     redblacktree.Tree
	cachedTTLInfos map[uint32][]wire.TTLInfo

	// The data needed during utreexo Modify.
	leavesToCache   map[uint32][]bool
	leavesToUncache map[uint32][]wire.TTLInfo

	// The below are data needed during the request of the utreexo proofs.
	cachedTargetBitmap map[uint32][]bool
	cachedProofBitmap  map[uint32][]bool

	// The hashes for the cached targets. They're keyed by the ttlInfo as they're
	// guaranteed to be unique.
	cachedHashes map[wire.TTLInfo]leafDataAndHash
}

// verifyTTLs verifies that the ttls are included in the ttl accumulator.
func (uc *UtreexoTTLCache) verifyTTLs(msgTTL *wire.MsgUtreexoTTLs) error {
	ttls := msgTTL.TTLs

	ttlHashes := make([]utreexo.Hash, 0, len(ttls))
	ttlTargets := make([]uint64, 0, len(ttls))
	for _, ttl := range ttls {
		buf := bytes.NewBuffer(make([]byte, 0, ttl.SerializeSize()))
		err := ttl.Serialize(buf)
		if err != nil {
			return fmt.Errorf("Failed to serialize utreexo ttl at height %v. %v",
				ttl.BlockHeight, err)
		}

		ttlTargets = append(ttlTargets, uint64(ttl.BlockHeight))
		ttlHashes = append(ttlHashes, sha256.Sum256(buf.Bytes()))
	}

	proof := utreexo.Proof{Targets: ttlTargets, Proof: msgTTL.ProofHashes}
	_, err := utreexo.Verify(*uc.ttlAccumulator, ttlHashes, proof)
	if err != nil {
		return fmt.Errorf("Utreexo ttl proof failed verification. %v", err)
	}

	return nil
}

// generateTargets generates and caches the target positions for the blocks the MsgUtreexoTTLs provide.
func (uc *UtreexoTTLCache) generateTargets(msgTTL *wire.MsgUtreexoTTLs) {
	for _, ttlPerBlock := range msgTTL.TTLs {
		for _, ttlInfo := range ttlPerBlock.TTLs {
			if ttlInfo.DeathHeight == 0 {
				continue
			}
			uc.targetSorter.Push(ttlInfo)
		}
	}
	heap.Init(&uc.targetSorter)

	for _, ttlPerBlock := range msgTTL.TTLs {
		targets := getTargetsAtHeight(&uc.targetSorter, ttlPerBlock.BlockHeight)
		uc.targets[ttlPerBlock.BlockHeight] = targets
	}
}

// generateNumleaves generates numleaves for the blocks the MsgUtreexoTTLs provide.
func (uc *UtreexoTTLCache) generateNumleaves(msgTTL *wire.MsgUtreexoTTLs) {
	for _, ttlPerBlock := range msgTTL.TTLs {
		prevLeaves := uc.numLeaves[int32(ttlPerBlock.BlockHeight)-1]
		uc.numLeaves[int32(ttlPerBlock.BlockHeight)] = prevLeaves + uint64(len(ttlPerBlock.TTLs))
	}
}

// insertToCache fills in the cached ttls with the clairvoyant algorithm. The newly cached
// ttls and the ttls that are to be uncached are saved.
func (uc *UtreexoTTLCache) insertToCache(height uint32, ttlInfos []wire.TTLInfo) {
	cacheOrNot := make([]bool, len(ttlInfos))
	uncacheLeaves := make([]wire.TTLInfo, 0, len(ttlInfos))

	for i, ttlInfo := range ttlInfos {
		if ttlInfo.DeathHeight == 0 {
			continue
		}

		if uc.cachedTTLs.Size() < int(uc.cacheCount) {
			uc.cachedTTLs.Put(ttlInfo, nil)
			cacheOrNot[i] = true
			continue
		}

		node := uc.cachedTTLs.Right()
		item := node.Key.(wire.TTLInfo)
		if item.DeathHeight > ttlInfo.DeathHeight {
			uc.cachedTTLs.Remove(item)
			uc.cachedTTLs.Put(ttlInfo, nil)

			uncacheLeaves = append(uncacheLeaves, item)
			cacheOrNot[i] = true

		} else if item.DeathHeight == ttlInfo.DeathHeight &&
			item.DeathBlkIndex > ttlInfo.DeathBlkIndex {

			uc.cachedTTLs.Remove(item)
			uc.cachedTTLs.Put(ttlInfo, nil)

			uncacheLeaves = append(uncacheLeaves, item)
			cacheOrNot[i] = true
		}
	}

	uc.leavesToUncache[height] = uncacheLeaves
	uc.leavesToCache[height] = cacheOrNot
}

// generateCacheInfo generates and caches the leaves that should be cached and the leaves that
// should be uncached at the heights the MsgUtreexoTTLs provide.
func (uc *UtreexoTTLCache) generateCacheInfo(msgTTL *wire.MsgUtreexoTTLs) {
	for _, ttlPerBlock := range msgTTL.TTLs {
		var cachedTTLInfos []wire.TTLInfo

		node := uc.cachedTTLs.Left()
		for node != nil {
			item := node.Key.(wire.TTLInfo)
			if item.DeathHeight == ttlPerBlock.BlockHeight {
				uc.cachedTTLs.Remove(item)
				cachedTTLInfos = append(cachedTTLInfos, item)
				node = uc.cachedTTLs.Left()
			} else {
				break
			}
		}

		uc.cachedTTLInfos[ttlPerBlock.BlockHeight] = cachedTTLInfos

		uc.insertToCache(ttlPerBlock.BlockHeight, ttlPerBlock.TTLs)
	}
}

// getProofBitMap returns a bitmap requesting all proofs for the given targets and numleaves.
func getProofBitMap(targets, cached []uint64, numleaves uint64) ([]bool, []bool) {
	targetsCopy := make([]uint64, len(targets))
	copy(targetsCopy, targets)
	slices.Sort(targetsCopy)

	cachedCopy := make([]uint64, len(cached))
	copy(cachedCopy, cached)
	slices.Sort(cachedCopy)

	targetBitMap := make([]bool, len(targets))
	for i, target := range targets {
		idx := sort.Search(len(cachedCopy), func(i int) bool {
			return cachedCopy[i] >= target
		})
		if idx < len(cachedCopy) && cachedCopy[idx] == target {
			targetBitMap[i] = false
		} else {
			targetBitMap[i] = true
		}
	}

	proofPositions, _ := utreexo.ProofPositions(targetsCopy, numleaves, utreexo.TreeRows(numleaves))
	have, computable := utreexo.ProofPositions(cachedCopy, numleaves, utreexo.TreeRows(numleaves))

	bitMap := make([]bool, len(proofPositions))

	haveIdx := 0
	computableIdx := 0
	for i, proofPos := range proofPositions {
		for haveIdx < len(have) &&
			have[haveIdx] < proofPos {

			haveIdx++
		}
		for computableIdx < len(computable) &&
			computable[computableIdx] < proofPos {

			computableIdx++
		}

		if haveIdx < len(have) && have[haveIdx] == proofPos ||
			computableIdx < len(computable) && computable[computableIdx] == proofPos {
			bitMap[i] = false
		} else {
			bitMap[i] = true
		}
	}

	return targetBitMap, bitMap
}

// generateProofBitmap calculates and caches the bitmaps for the proof hashes and the target positions
// for the heights that the MsgUtreexoTTLs provide.
func (uc *UtreexoTTLCache) generateProofBitmap(msgTTL *wire.MsgUtreexoTTLs) {
	for _, ttlPerBlock := range msgTTL.TTLs {
		height := ttlPerBlock.BlockHeight

		cachedTTLInfos := uc.cachedTTLInfos[height]
		cachedTargets := make([]uint64, 0, len(cachedTTLInfos))
		for _, ttlInfo := range cachedTTLInfos {
			cachedTargets = append(cachedTargets, ttlInfo.DeathPos)
		}

		targets := uc.targets[height]
		numLeaves := uc.numLeaves[int32(height)-1]

		targetBitMap, proofBitMap := getProofBitMap(targets, cachedTargets, numLeaves)
		uc.totalProofSize += uint64(len(proofBitMap))
		for _, b := range proofBitMap {
			if b {
				uc.downloadedProofSize += 1
			}
		}

		uc.allTargets += uint64(len(targets))
		uc.cachedTargetCount += uint64(len(cachedTargets))

		if height%10000 == 0 {
			log.Infof("block %v, allTargets %v, cached targets %v, cached percent %.2f "+
				"totalproof size: %v, downloaded %v",
				height, uc.allTargets, uc.cachedTargetCount,
				float64(uc.cachedTargetCount)/float64(uc.allTargets),
				uc.totalProofSize, uc.downloadedProofSize)
		}
		uc.cachedTargetBitmap[ttlPerBlock.BlockHeight] = targetBitMap
		uc.cachedProofBitmap[ttlPerBlock.BlockHeight] = proofBitMap
	}
}

// ProcessTTLs verifies the MsgUtreexoTTLs and generates cache data.
func (uc *UtreexoTTLCache) ProcessTTLs(msgTTL *wire.MsgUtreexoTTLs) error {
	err := uc.verifyTTLs(msgTTL)
	if err != nil {
		return err
	}

	for _, ttlPerBlock := range msgTTL.TTLs {
		uc.ttls[int32(ttlPerBlock.BlockHeight)] = ttlPerBlock
	}

	uc.generateTargets(msgTTL)
	uc.generateNumleaves(msgTTL)
	uc.generateCacheInfo(msgTTL)
	uc.generateProofBitmap(msgTTL)

	return nil
}

// constructLeafDatas returns the full leafdatas for the given utreexo proof.
func (uc *UtreexoTTLCache) constructLeafDatas(height int32, proofMsg *wire.MsgUtreexoProof) (
	[]wire.LeafData, error) {

	targetBitmap := uc.cachedTargetBitmap[uint32(height)]
	leafDatas := make([]wire.LeafData, 0, len(targetBitmap))
	infos := uc.cachedTTLInfos[uint32(height)]

	leafIdx := 0
	infoIdx := 0
	for i, requested := range targetBitmap {
		if requested {
			if leafIdx >= len(proofMsg.LeafDatas) {
				return nil, fmt.Errorf("not enough leafdatas in the proofmsg")
			}
			leafDatas = append(leafDatas, proofMsg.LeafDatas[leafIdx])
			leafIdx++
		} else {
			if infoIdx >= len(infos) {
				return nil, fmt.Errorf("not enough cached leafdatas")
			}
			leafData, found := uc.cachedHashes[infos[infoIdx]]
			if !found {
				return nil, fmt.Errorf("don't have %v cached", infos[i])
			}
			leafDatas = append(leafDatas, leafData.ld)
			infoIdx++
		}
	}

	return leafDatas, nil
}

// GetUData returns a full utreexo data, the add leaves at this block, and the leaves to uncache at this block.
func (uc *UtreexoTTLCache) GetUData(utreexoView *UtreexoViewpoint, block *btcutil.Block,
	proofMsg *wire.MsgUtreexoProof) (*wire.UData, []utreexo.Leaf, []utreexo.Hash, error) {

	height := block.Height()
	targets := uc.targets[uint32(height)]
	haves := uc.cachedProofBitmap[uint32(height)]

	proof, err := utreexoView.MakeProofFull(targets, haves, proofMsg.ProofHashes)
	if err != nil {
		return nil, nil, nil, err
	}

	isLeafCached := uc.leavesToCache[uint32(height)]
	adds := ExtractAccumulatorAdds(block)
	if len(isLeafCached) != len(adds) {
		return nil, nil, nil, fmt.Errorf("expected %v but have %v",
			len(isLeafCached), len(adds))
	}

	ttls := uc.ttls[height].TTLs

	leaves := make([]utreexo.Leaf, len(adds))
	for i, add := range adds {
		leafHash := add.LeafHash()
		if isLeafCached[i] {
			uc.cachedHashes[ttls[i]] = leafDataAndHash{
				ld:   add,
				hash: leafHash,
			}
		}

		leaves[i] = utreexo.Leaf{
			Hash:     leafHash,
			Remember: isLeafCached[i],
		}
	}

	leafDatas, err := uc.constructLeafDatas(height, proofMsg)
	if err != nil {
		return nil, nil, nil, err
	}
	udata := wire.UData{
		AccProof:  *proof,
		LeafDatas: leafDatas,
	}

	uncacheTTLInfo := uc.leavesToUncache[uint32(height)]
	uncacheHashes := make([]utreexo.Hash, len(uncacheTTLInfo))
	for i, ttlInfo := range uncacheTTLInfo {
		ldh := uc.cachedHashes[ttlInfo]
		uncacheHashes[i] = ldh.hash
	}

	return &udata, leaves, uncacheHashes, nil
}

// FetchMsgGetUtreexoProof returns a getutreexoproof messsage based on the leaves that the cache is holding onto.
// The getutreexoproof message will not request for the cached leaves and proof hashes the utreexoview should have.
func (uc *UtreexoTTLCache) FetchMsgGetUtreexoProof(height int32, blockHash *chainhash.Hash) *wire.MsgGetUtreexoProof {
	proofBitmap := uc.cachedProofBitmap[uint32(height)]
	targetBitmap := uc.cachedTargetBitmap[uint32(height)]

	return &wire.MsgGetUtreexoProof{
		BlockHash:        *blockHash,
		TargetBool:       false,
		ProofIndexBitMap: createBitmap(proofBitmap),
		LeafIndexBitMap:  createBitmap(targetBitmap),
	}
}

// createBitmap returns a bitmap from the given slice of bools.
func createBitmap(includes []bool) []byte {
	count := len(includes) / 8
	if len(includes)%8 != 0 {
		count++
	}

	bitMap := make([]byte, count)

	bitMapIdx := 0
	for idx, include := range includes {
		bitPlace := idx % 8
		if idx != 0 && bitPlace == 0 {
			bitMapIdx++
		}

		if include {
			bitMap[bitMapIdx] |= (1 << bitPlace)
		}
	}

	return bitMap

}

// HaveTTLInfo returns if we have processed the UtreexoTTLs and have the ttl cache data for the given block.
func (uc *UtreexoTTLCache) HaveTTLInfo(height int32) bool {
	_, found := uc.ttls[height]
	return found
}

// RemoveCache removes all the associated caches for the given height.
func (uc *UtreexoTTLCache) RemoveCache(height int32) {
	// Since we calculate the next numLeaves with the current height, we can
	// only remove the previous height.
	delete(uc.numLeaves, height-1)
	delete(uc.ttls, height)
	delete(uc.cachedProofBitmap, uint32(height))
	delete(uc.cachedTargetBitmap, uint32(height))
	delete(uc.leavesToCache, uint32(height))

	ttlInfos := uc.leavesToUncache[uint32(height)]
	delete(uc.leavesToUncache, uint32(height))

	// Uncache the uncached leaves.
	for _, ttlInfo := range ttlInfos {
		delete(uc.cachedHashes, ttlInfo)
	}

	// Uncache the leaves deleted on this height.
	cleanups := uc.cachedTTLInfos[uint32(height)]
	for _, cleanup := range cleanups {
		delete(uc.cachedHashes, cleanup)
	}
	delete(uc.cachedTTLInfos, uint32(height))
}

// ValidAtHeight returns if the utreexo ttl cache is still valid to be utilized at the given height.
func (uc *UtreexoTTLCache) ValidAtHeight(height int32) bool {
	return uint64(height) <= uc.ttlAccumulator.NumLeaves-1
}

// FetchMsgGetUtreexoTTLs returns a getutreexottls message from the given start and end heights.
func (uc *UtreexoTTLCache) FetchMsgGetUtreexoTTLs(startHeight, endHeight int32) wire.MsgGetUtreexoTTLs {
	return wire.CalculateGetUtreexoTTLMsgs(
		uint32(uc.ttlAccumulator.NumLeaves), startHeight, endHeight)
}

// InitUtreexoTTLCache initializes the utreexo ttl cache with the given inputs.
func InitUtreexoTTLCache(ttlAcc *utreexo.Stump, maxCache uint32, bestHeight int32, numLeaves uint64) *UtreexoTTLCache {
	cache := &UtreexoTTLCache{
		cacheCount:         maxCache,
		currentHeight:      bestHeight,
		ttlAccumulator:     ttlAcc,
		numLeaves:          make(map[int32]uint64),
		ttls:               make(map[int32]wire.UtreexoTTL),
		targets:            make(map[uint32][]uint64),
		cachedTTLInfos:     make(map[uint32][]wire.TTLInfo),
		cachedTargetBitmap: make(map[uint32][]bool),
		cachedProofBitmap:  make(map[uint32][]bool),
		leavesToCache:      make(map[uint32][]bool),
		leavesToUncache:    make(map[uint32][]wire.TTLInfo),
		cachedHashes:       make(map[wire.TTLInfo]leafDataAndHash),
		cachedTTLs:         *redblacktree.NewWith(TTLInfoComparator),
	}
	cache.numLeaves[bestHeight] = numLeaves

	return cache
}
