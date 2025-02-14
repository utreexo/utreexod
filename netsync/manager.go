// Copyright (c) 2013-2017 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package netsync

import (
	"math/rand"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	"github.com/utreexo/utreexod/mempool"
	peerpkg "github.com/utreexo/utreexod/peer"
	"github.com/utreexo/utreexod/wire"
)

const (
	// minInFlightBlocks is the minimum number of blocks that should be
	// in the request queue for headers-first mode before requesting
	// more.
	minInFlightBlocks = 10

	// maxRejectedTxns is the maximum number of rejected transactions
	// hashes to store in memory.
	maxRejectedTxns = 1000

	// maxRequestedBlocks is the maximum number of requested block
	// hashes to store in memory.
	maxRequestedBlocks = wire.MaxInvPerMsg

	// maxRequestedTxns is the maximum number of requested transactions
	// hashes to store in memory.
	maxRequestedTxns = wire.MaxInvPerMsg

	// maxStallDuration is the time after which we will disconnect our
	// current sync peer if we haven't made progress.
	maxStallDuration = 3 * time.Minute

	// stallSampleInterval the interval at which we will check to see if our
	// sync has stalled.
	stallSampleInterval = 30 * time.Second
)

// zeroHash is the zero value hash (all zeros).  It is defined as a convenience.
var zeroHash chainhash.Hash

// newPeerMsg signifies a newly connected peer to the block handler.
type newPeerMsg struct {
	peer *peerpkg.Peer
}

// blockMsg packages a bitcoin block message and the peer it came from together
// so the block handler has access to that information.
type blockMsg struct {
	block *btcutil.Block
	peer  *peerpkg.Peer
	reply chan struct{}
}

// invMsg packages a bitcoin inv message and the peer it came from together
// so the block handler has access to that information.
type invMsg struct {
	inv  *wire.MsgInv
	peer *peerpkg.Peer
}

// headersMsg packages a bitcoin headers message and the peer it came from
// together so the block handler has access to that information.
type headersMsg struct {
	headers *wire.MsgHeaders
	peer    *peerpkg.Peer
}

// utreexoSummariesMsg packages a bitcoin utreexo summaries message and the peer it came from
// together so the block handler has access to that information.
type utreexoSummariesMsg struct {
	summaries *wire.MsgUtreexoSummaries
	peer      *peerpkg.Peer
}

// utreexoProofMsg packages a bitcoin utreexo proof message and the peer it came from
// together so the block handler has access to that information.
type utreexoProofMsg struct {
	proof *wire.MsgUtreexoProof
	peer  *peerpkg.Peer
}

// notFoundMsg packages a bitcoin notfound message and the peer it came from
// together so the block handler has access to that information.
type notFoundMsg struct {
	notFound *wire.MsgNotFound
	peer     *peerpkg.Peer
}

// donePeerMsg signifies a newly disconnected peer to the block handler.
type donePeerMsg struct {
	peer *peerpkg.Peer
}

// txMsg packages a bitcoin tx message and the peer it came from together
// so the block handler has access to that information.
type txMsg struct {
	tx    *btcutil.Tx
	peer  *peerpkg.Peer
	reply chan struct{}
}

// utreexoTxMsg packages a bitcoin utreexo tx message and the peer it came from together
// so the block handler has access to that information.
type utreexoTxMsg struct {
	utreexoTx *btcutil.UtreexoTx
	peer      *peerpkg.Peer
	reply     chan struct{}
}

// getSyncPeerMsg is a message type to be sent across the message channel for
// retrieving the current sync peer.
type getSyncPeerMsg struct {
	reply chan int32
}

// processBlockResponse is a response sent to the reply channel of a
// processBlockMsg.
type processBlockResponse struct {
	isOrphan bool
	err      error
}

// processBlockMsg is a message type to be sent across the message channel
// for requested a block is processed.  Note this call differs from blockMsg
// above in that blockMsg is intended for blocks that came from peers and have
// extra handling whereas this message essentially is just a concurrent safe
// way to call ProcessBlock on the internal block chain instance.
type processBlockMsg struct {
	block *btcutil.Block
	flags blockchain.BehaviorFlags
	reply chan processBlockResponse
}

// isCurrentMsg is a message type to be sent across the message channel for
// requesting whether or not the sync manager believes it is synced with the
// currently connected peers.
type isCurrentMsg struct {
	reply chan bool
}

// pauseMsg is a message type to be sent across the message channel for
// pausing the sync manager.  This effectively provides the caller with
// exclusive access over the manager until a receive is performed on the
// unpause channel.
type pauseMsg struct {
	unpause <-chan struct{}
}

// headerNode is used as a node in a list of headers that are linked together
// between checkpoints.
type headerNode struct {
	height int32
	hash   *chainhash.Hash
}

// peerSyncState stores additional information that the SyncManager tracks
// about a peer.
type peerSyncState struct {
	syncCandidate             bool
	requestQueue              []*wire.InvVect
	requestedTxns             map[chainhash.Hash]struct{}
	requestedBlocks           map[chainhash.Hash]struct{}
	requestedUtreexoSummaries map[chainhash.Hash]struct{}
	requestedUtreexoProofs    map[chainhash.Hash]struct{}
}

// limitAdd is a helper function for maps that require a maximum limit by
// evicting a random value if adding the new value would cause it to
// overflow the maximum allowed.
func limitAdd(m map[chainhash.Hash]struct{}, hash chainhash.Hash, limit int) {
	if len(m)+1 > limit {
		// Remove a random entry from the map.  For most compilers, Go's
		// range statement iterates starting at a random item although
		// that is not 100% guaranteed by the spec.  The iteration order
		// is not important here because an adversary would have to be
		// able to pull off preimage attacks on the hashing function in
		// order to target eviction of specific entries anyways.
		for txHash := range m {
			delete(m, txHash)
			break
		}
	}
	m[hash] = struct{}{}
}

// SyncManager is used to communicate block related messages with peers. The
// SyncManager is started as by executing Start() in a goroutine. Once started,
// it selects peers to sync from and starts the initial block download. Once the
// chain is in sync, the SyncManager handles incoming block and header
// notifications and relays announcements of new blocks to peers.
type SyncManager struct {
	peerNotifier   PeerNotifier
	started        int32
	shutdown       int32
	chain          *blockchain.BlockChain
	txMemPool      *mempool.TxPool
	chainParams    *chaincfg.Params
	progressLogger *blockProgressLogger
	msgChan        chan interface{}
	wg             sync.WaitGroup
	quit           chan struct{}

	// These fields should only be accessed from the blockHandler thread
	rejectedTxns     map[chainhash.Hash]struct{}
	requestedTxns    map[chainhash.Hash]struct{}
	requestedBlocks  map[chainhash.Hash]struct{}
	syncPeer         *peerpkg.Peer
	peerStates       map[*peerpkg.Peer]*peerSyncState
	lastProgressTime time.Time

	// headersBuildMode downloads and builds the entire header index.
	headersBuildMode bool

	// The following fields are used for headers-first mode.
	headersFirstMode    bool
	startHeader         *headerNode
	nextCheckpoint      *chaincfg.Checkpoint
	utreexoSummaries    map[chainhash.Hash]*wire.UtreexoBlockSummary
	numLeaves           map[int32]uint64
	queuedBlocks        map[chainhash.Hash]*blockMsg
	queuedUtreexoProofs map[chainhash.Hash]*utreexoProofMsg

	// An optional fee estimator.
	feeEstimator *mempool.FeeEstimator
}

// findNextHeaderCheckpoint returns the next checkpoint after the passed height.
// It returns nil when there is not one either because the height is already
// later than the final checkpoint or some other reason such as disabled
// checkpoints.
func (sm *SyncManager) findNextHeaderCheckpoint(height int32) *chaincfg.Checkpoint {
	checkpoints := sm.chain.Checkpoints()
	if len(checkpoints) == 0 {
		return nil
	}

	// There is no next checkpoint if the height is already after the final
	// checkpoint.
	finalCheckpoint := &checkpoints[len(checkpoints)-1]
	if height >= finalCheckpoint.Height {
		return nil
	}

	// Find the next checkpoint.
	nextCheckpoint := finalCheckpoint
	for i := len(checkpoints) - 2; i >= 0; i-- {
		if height >= checkpoints[i].Height {
			break
		}
		nextCheckpoint = &checkpoints[i]
	}
	return nextCheckpoint
}

// startSync will choose the best peer among the available candidate peers to
// download/sync the blockchain from.  When syncing is already running, it
// simply returns.  It also examines the candidates for any which are no longer
// candidates and removes them as needed.
func (sm *SyncManager) startSync() {
	// Return now if we're already syncing.
	if sm.syncPeer != nil {
		return
	}

	// Once the segwit soft-fork package has activated, we only
	// want to sync from peers which are witness enabled to ensure
	// that we fully validate all blockchain data.
	segwitActive, err := sm.chain.IsDeploymentActive(chaincfg.DeploymentSegwit)
	if err != nil {
		log.Errorf("Unable to query for segwit soft-fork state: %v", err)
		return
	}

	// If the current node is dependent on the utreexoView, (aka a compact state node)
	// then only connect to other utreexo nodes.
	utreexoViewActive := sm.chain.IsUtreexoViewActive()

	best := sm.chain.BestSnapshot()
	bestHeaderHash, bestHeaderHeight := sm.chain.BestHeader()
	var higherPeers, equalPeers, higherHeaderPeers []*peerpkg.Peer
	for peer, state := range sm.peerStates {
		if !state.syncCandidate {
			continue
		}

		if segwitActive && !peer.IsWitnessEnabled() {
			log.Debugf("peer %v not witness enabled, skipping", peer)
			continue
		}

		if utreexoViewActive && !peer.IsUtreexoEnabled() {
			log.Debugf("peer %v not utreexo enabled, skipping", peer)
			continue
		}

		// Figure out what the peer is based on the best block.
		switch {
		// Remove sync candidate peers that are no longer candidates due
		// to passing their latest known block.  NOTE: The < is
		// intentional as opposed to <=.  While technically the peer
		// doesn't have a later block when it's equal, it will likely
		// have one soon so it is a reasonable choice.  It also allows
		// the case where both are at 0 such as during regression test.
		case peer.LastBlock() < best.Height:
			state.syncCandidate = false

		// If the peer is at the same height as us, we'll add it a set
		// of backup peers in case we do not find one with a higher
		// height. If we are synced up with all of our peers, all of
		// them will be in this set.
		case peer.LastBlock() == best.Height:
			equalPeers = append(equalPeers, peer)

		// This peer has a height greater than our own, we'll consider
		// it in the set of better peers from which we'll randomly
		// select.
		case peer.LastBlock() > best.Height:
			higherPeers = append(higherPeers, peer)
		}

		// Figure out what the peer is based on the best header.
		switch {
		case peer.LastBlock() > bestHeaderHeight:
			higherHeaderPeers = append(higherHeaderPeers, peer)
		}
	}

	if sm.chain.IsCurrent() && len(higherPeers) == 0 {
		log.Infof("Caught up to block %s(%d)", best.Hash.String(), best.Height)
		sm.headersFirstMode = false
		return
	}

	// This means that there's extra headers that we need to download.
	if len(higherHeaderPeers) > 0 {
		var bestPeer *peerpkg.Peer
		switch {
		case len(higherHeaderPeers) > 0:
			bestPeer = higherHeaderPeers[rand.Intn(len(higherHeaderPeers))]
		}

		sm.syncPeer = bestPeer

		// Reset the last progress time now that we have a non-nil
		// syncPeer to avoid instantly detecting it as stalled in the
		// event the progress time hasn't been updated recently.
		sm.lastProgressTime = time.Now()
		sm.headersFirstMode = true

		locator, err := sm.chain.LatestBlockLocatorByHeader()
		if err != nil {
			log.Errorf("Failed to get block locator for the "+
				"latest block: %v", err)
			return
		}

		bestPeer.PushGetHeadersMsg(locator, &zeroHash)
		log.Infof("Downloading headers from %d to "+
			"%d from peer %s", best.Height+1,
			bestPeer.LastBlock(), bestPeer.Addr())

		return
	}

	// Pick randomly from the set of peers greater than our block height,
	// falling back to a random peer of the same height if none are greater.
	//
	// TODO(conner): Use a better algorithm to ranking peers based on
	// observed metrics and/or sync in parallel.
	var bestPeer *peerpkg.Peer
	switch {
	case len(higherPeers) > 0:
		bestPeer = higherPeers[rand.Intn(len(higherPeers))]

	case len(equalPeers) > 0:
		bestPeer = equalPeers[rand.Intn(len(equalPeers))]
	}

	// Start syncing from the best peer if one was selected.
	if bestPeer != nil {
		// Clear the requestedBlocks if the sync peer changes, otherwise
		// we may ignore blocks we need that the last sync peer failed
		// to send.
		//
		// We don't reset it during headersFirstMode since it's not used
		// during headersFirstMode.
		if !sm.headersFirstMode {
			sm.requestedBlocks = make(map[chainhash.Hash]struct{})
		}

		log.Infof("Syncing to block height %d from peer %v",
			bestPeer.LastBlock(), bestPeer.Addr())

		sm.syncPeer = bestPeer

		// Reset the last progress time now that we have a non-nil
		// syncPeer to avoid instantly detecting it as stalled in the
		// event the progress time hasn't been updated recently.
		sm.lastProgressTime = time.Now()

		if utreexoViewActive {
			// If we have the last utreexo summary, then we
			// should have all the previous summaries as well.
			_, have := sm.utreexoSummaries[bestHeaderHash]
			if !have {
				sm.fetchUtreexoSummaries(nil)
				return
			}
		}
		if sm.startHeader == nil {
			sm.startHeader = &headerNode{bestHeaderHeight + 1, &bestHeaderHash}
		}
		sm.fetchHeaderBlocks(nil)
	} else {
		log.Warnf("No sync peer candidates available")
	}
}

// isSyncCandidate returns whether or not the peer is a candidate to consider
// syncing from.
func (sm *SyncManager) isSyncCandidate(peer *peerpkg.Peer) bool {
	// Typically a peer is not a candidate for sync if it's not a full node,
	// however regression test is special in that the regression tool is
	// not a full node and still needs to be considered a sync candidate.
	if sm.chainParams == &chaincfg.RegressionNetParams {
		// The peer is not a candidate if it's not coming from localhost
		// or the hostname can't be determined for some reason.
		host, _, err := net.SplitHostPort(peer.Addr())
		if err != nil {
			return false
		}

		if host != "127.0.0.1" && host != "localhost" {
			return false
		}

		// Candidate if all checks passed.
		return true
	}

	// If the segwit soft-fork package has activated, then the peer must
	// also be upgraded.
	segwitActive, err := sm.chain.IsDeploymentActive(
		chaincfg.DeploymentSegwit,
	)
	if err != nil {
		log.Errorf("Unable to query for segwit soft-fork state: %v",
			err)
	}

	if segwitActive && !peer.IsWitnessEnabled() {
		return false
	}

	// If the node is dependent on the utreexoViewpoint (aka the node
	// is a compact state node), then the peer must have utreexo services
	// active.
	utreexoViewActive := sm.chain.IsUtreexoViewActive()
	if utreexoViewActive && !peer.IsUtreexoEnabled() {
		return false
	}

	var (
		nodeServices = peer.Services()
		fullNode     = nodeServices.HasFlag(wire.SFNodeNetwork)
		prunedNode   = nodeServices.HasFlag(wire.SFNodeNetworkLimited)
	)

	switch {
	case fullNode:
		// Node is a sync candidate if it has all the blocks.

	case prunedNode:
		// Even if the peer is pruned, if they have the node network
		// limited flag, they are able to serve 2 days worth of blocks
		// from the current tip. Therefore, check if our chaintip is
		// within that range.
		bestHeight := sm.chain.BestSnapshot().Height
		peerLastBlock := peer.LastBlock()

		// bestHeight+1 as we need the peer to serve us the next block,
		// not the one we already have.
		if bestHeight+1 <=
			peerLastBlock-wire.NodeNetworkLimitedBlockThreshold {

			return false
		}

	default:
		// If the peer isn't an archival node, and it's not signaling
		// NODE_NETWORK_LIMITED, we can't sync off of this node.
		return false
	}

	// Candidate if all checks passed.
	return true
}

// handleNewPeerMsg deals with new peers that have signalled they may
// be considered as a sync peer (they have already successfully negotiated).  It
// also starts syncing if needed.  It is invoked from the syncHandler goroutine.
func (sm *SyncManager) handleNewPeerMsg(peer *peerpkg.Peer) {
	// Ignore if in the process of shutting down.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	log.Infof("New valid peer %s (%s)", peer, peer.UserAgent())

	// Initialize the peer state.
	isSyncCandidate := sm.isSyncCandidate(peer)
	sm.peerStates[peer] = &peerSyncState{
		syncCandidate:             isSyncCandidate,
		requestedTxns:             make(map[chainhash.Hash]struct{}),
		requestedBlocks:           make(map[chainhash.Hash]struct{}),
		requestedUtreexoSummaries: make(map[chainhash.Hash]struct{}),
		requestedUtreexoProofs:    make(map[chainhash.Hash]struct{}),
	}

	// Start syncing by choosing the best candidate if needed.
	if isSyncCandidate && sm.syncPeer == nil {
		sm.startSync()
	}
}

// handleStallSample will switch to a new sync peer if the current one has
// stalled. This is detected when by comparing the last progress timestamp with
// the current time, and disconnecting the peer if we stalled before reaching
// their highest advertised block.
func (sm *SyncManager) handleStallSample() {
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	// If we don't have an active sync peer, exit early.
	if sm.syncPeer == nil {
		return
	}

	// If the stall timeout has not elapsed, exit early.
	if time.Since(sm.lastProgressTime) <= maxStallDuration {
		return
	}

	// Check to see that the peer's sync state exists.
	state, exists := sm.peerStates[sm.syncPeer]
	if !exists {
		return
	}

	sm.clearRequestedState(state)

	disconnectSyncPeer := sm.shouldDCStalledSyncPeer()
	sm.updateSyncPeer(disconnectSyncPeer)
}

// shouldDCStalledSyncPeer determines whether or not we should disconnect a
// stalled sync peer. If the peer has stalled and its reported height is greater
// than our own best height, we will disconnect it. Otherwise, we will keep the
// peer connected in case we are already at tip.
func (sm *SyncManager) shouldDCStalledSyncPeer() bool {
	lastBlock := sm.syncPeer.LastBlock()
	startHeight := sm.syncPeer.StartingHeight()

	var peerHeight int32
	if lastBlock > startHeight {
		peerHeight = lastBlock
	} else {
		peerHeight = startHeight
	}

	// If we've stalled out yet the sync peer reports having more blocks for
	// us we will disconnect them. This allows us at tip to not disconnect
	// peers when we are equal or they temporarily lag behind us.
	best := sm.chain.BestSnapshot()
	return peerHeight > best.Height
}

// handleDonePeerMsg deals with peers that have signalled they are done.  It
// removes the peer as a candidate for syncing and in the case where it was
// the current sync peer, attempts to select a new best peer to sync from.  It
// is invoked from the syncHandler goroutine.
func (sm *SyncManager) handleDonePeerMsg(peer *peerpkg.Peer) {
	state, exists := sm.peerStates[peer]
	if !exists {
		log.Warnf("Received done peer message for unknown peer %s", peer)
		return
	}

	// Remove the peer from the list of candidate peers.
	delete(sm.peerStates, peer)

	log.Infof("Lost peer %s", peer)

	sm.clearRequestedState(state)

	if peer == sm.syncPeer {
		// Update the sync peer. The server has already disconnected the
		// peer before signaling to the sync manager.
		sm.updateSyncPeer(false)
	}
}

// clearRequestedState wipes all expected transactions and blocks from the sync
// manager's requested maps that were requested under a peer's sync state, This
// allows them to be rerequested by a subsequent sync peer.
func (sm *SyncManager) clearRequestedState(state *peerSyncState) {
	// Remove requested transactions from the global map so that they will
	// be fetched from elsewhere next time we get an inv.
	for txHash := range state.requestedTxns {
		delete(sm.requestedTxns, txHash)
	}

	// The global map of requestedBlocks is not used during headersFirstMode.
	if !sm.headersFirstMode {
		// Remove requested blocks from the global map so that they will
		// be fetched from elsewhere next time we get an inv.
		// TODO: we could possibly here check which peers have these
		// blocks and request them now to speed things up a little.
		for blockHash := range state.requestedBlocks {
			delete(sm.requestedBlocks, blockHash)
		}
	}
}

// updateSyncPeer choose a new sync peer to replace the current one. If
// dcSyncPeer is true, this method will also disconnect the current sync peer.
// If we are in header first mode, any header state related to prefetching is
// also reset in preparation for the next sync peer.
func (sm *SyncManager) updateSyncPeer(dcSyncPeer bool) {
	log.Debugf("Updating sync peer, no progress for: %v",
		time.Since(sm.lastProgressTime))

	// First, disconnect the current sync peer if requested.
	if dcSyncPeer {
		sm.syncPeer.Disconnect()
	}

	sm.syncPeer = nil
	sm.startSync()
}

// handleTxMsg handles transaction messages from all peers.
func (sm *SyncManager) handleTxMsg(tx *btcutil.Tx, peer *peerpkg.Peer, utreexoData *wire.UData) {
	state, exists := sm.peerStates[peer]
	if !exists {
		log.Warnf("Received tx message from unknown peer %s", peer)
		return
	}

	// NOTE:  BitcoinJ, and possibly other wallets, don't follow the spec of
	// sending an inventory message and allowing the remote peer to decide
	// whether or not they want to request the transaction via a getdata
	// message.  Unfortunately, the reference implementation permits
	// unrequested data, so it has allowed wallets that don't follow the
	// spec to proliferate.  While this is not ideal, there is no check here
	// to disconnect peers for sending unsolicited transactions to provide
	// interoperability.
	txHash := tx.Hash()

	// Ignore transactions that we have already rejected.  Do not
	// send a reject message here because if the transaction was already
	// rejected, the transaction was unsolicited.
	if _, exists = sm.rejectedTxns[*txHash]; exists {
		log.Debugf("Ignoring unsolicited previously rejected "+
			"transaction %v from %s", txHash, peer)
		return
	}

	// Process the transaction to include validation, insertion in the
	// memory pool, orphan handling, etc.
	acceptedTxs, err := sm.txMemPool.ProcessTransaction(tx, utreexoData,
		true, true, mempool.Tag(peer.ID()))

	// Remove transaction from request maps. Either the mempool/chain
	// already knows about it and as such we shouldn't have any more
	// instances of trying to fetch it, or we failed to insert and thus
	// we'll retry next time we get an inv.
	delete(state.requestedTxns, *txHash)
	delete(sm.requestedTxns, *txHash)

	if err != nil {
		// Do not request this transaction again until a new block
		// has been processed.
		limitAdd(sm.rejectedTxns, *txHash, maxRejectedTxns)

		// When the error is a rule error, it means the transaction was
		// simply rejected as opposed to something actually going wrong,
		// so log it as such.  Otherwise, something really did go wrong,
		// so log it as an actual error.
		if _, ok := err.(mempool.RuleError); ok {
			log.Debugf("Rejected transaction %v from %s: %v",
				txHash, peer, err)
		} else {
			log.Errorf("Failed to process transaction %v: %v",
				txHash, err)
		}

		// Convert the error into an appropriate reject message and
		// send it.
		code, reason := mempool.ErrToRejectErr(err)
		peer.PushRejectMsg(wire.CmdTx, code, reason, txHash, false)
		return
	}

	sm.peerNotifier.AnnounceNewTransactions(acceptedTxs)
}

// current returns true if we believe we are synced with our peers, false if we
// still have blocks to check
func (sm *SyncManager) current() bool {
	if !sm.chain.IsCurrent() {
		return false
	}

	// if blockChain thinks we are current and we have no syncPeer it
	// is probably right.
	if sm.syncPeer == nil {
		return true
	}

	// No matter what chain thinks, if we are below the block we are syncing
	// to we are not current.
	if sm.chain.BestSnapshot().Height < sm.syncPeer.LastBlock() {
		return false
	}
	return true
}

// checkHeadersList checks if the sync manager is in headers-first mode and returns
// if the given block hash is a checkpointed block and the behavior flags for this
// block. If the block is still under the checkpoint, then it's given the fast-add
// flag.
func (sm *SyncManager) checkHeadersList(block *btcutil.Block) (
	bool, blockchain.BehaviorFlags) {

	isCheckpointBlock := false
	behaviorFlags := blockchain.BFNone
	blockHash := block.Hash()

	if !sm.chain.IsValidHeader(blockHash) {
		return false, blockchain.BFNone
	}

	height, err := sm.chain.HeaderHeightByHash(*blockHash)
	if err != nil {
		return false, blockchain.BFNone
	}

	checkpoint := sm.findNextHeaderCheckpoint(height - 1)
	if checkpoint == nil {
		return false, blockchain.BFNone
	}

	behaviorFlags |= blockchain.BFFastAdd
	if blockHash.IsEqual(checkpoint.Hash) {
		isCheckpointBlock = true
	}

	return isCheckpointBlock, behaviorFlags
}

// handleBlockMsg handles block messages from all peers.
func (sm *SyncManager) handleBlockMsg(bmsg *blockMsg) {
	peer := bmsg.peer
	state, exists := sm.peerStates[peer]
	if !exists {
		log.Warnf("Received block message from unknown peer %s", peer)
		return
	}

	// If we didn't ask for this block then the peer is misbehaving.
	blockHash := bmsg.block.Hash()
	if _, exists = state.requestedBlocks[*blockHash]; !exists {
		// The regression test intentionally sends some blocks twice
		// to test duplicate block insertion fails.  Don't disconnect
		// the peer or ignore the block when we're in regression test
		// mode in this case so the chain code is actually fed the
		// duplicate blocks.
		if sm.chainParams != &chaincfg.RegressionNetParams {
			log.Warnf("Got unrequested block %v from %s -- "+
				"disconnecting", blockHash, peer.Addr())
			peer.Disconnect()
			return
		}
	}

	// Check if we've received the utreexo summaries already.
	if sm.chain.IsUtreexoViewActive() {
		best := sm.chain.BestSnapshot()
		if !best.Hash.IsEqual(&bmsg.block.MsgBlock().Header.PrevBlock) {
			log.Warnf("got block %v out of order", bmsg.block.Hash())
			sm.queuedBlocks[*blockHash] = bmsg
			return
		}

		utreexoSummary, found := sm.utreexoSummaries[*bmsg.block.Hash()]
		if !found {
			log.Warnf("got block %v but don't have the associated "+
				"utreexo summary", bmsg.block.Hash())
			sm.queuedBlocks[*blockHash] = bmsg
			return
		}

		// We need the utreexo proof to be able to verify the block.
		utreexoProofMsg, found := sm.queuedUtreexoProofs[*bmsg.block.Hash()]
		if !found {
			log.Warnf("got block %v but don't have the associated "+
				"utreexo proof", bmsg.block.Hash())
			sm.queuedBlocks[*blockHash] = bmsg
			return
		}

		// We have all the data necessary to validate the block now so
		// it's safee to remove this utreexo proof from the queue.
		delete(sm.queuedUtreexoProofs, *bmsg.block.Hash())

		udata := wire.UData{
			AccProof: utreexo.Proof{
				Targets: utreexoSummary.BlockTargets,
				Proof:   utreexoProofMsg.proof.ProofHashes,
			},
			LeafDatas: utreexoProofMsg.proof.LeafDatas,
		}

		bmsg.block.MsgBlock().UData = &udata
		bmsg.block.MsgBlock().UData.AccProof.Targets = utreexoSummary.BlockTargets
	}

	// Process the block based off the headers if we're still in headers-first mode.
	isCheckpointBlock, behaviorFlags := sm.checkHeadersList(bmsg.block)

	// Remove block from request maps. Either chain will know about it and
	// so we shouldn't have any more instances of trying to fetch it, or we
	// will fail the insert and thus we'll retry next time we get an inv.
	delete(state.requestedBlocks, *blockHash)
	if !sm.headersFirstMode {
		// The global map of requestedBlocks is not used during
		// headersFirstMode.
		delete(sm.requestedBlocks, *blockHash)
	}

	// Process the block to include validation, best chain selection, orphan
	// handling, etc.
	_, isOrphan, err := sm.chain.ProcessBlock(bmsg.block, behaviorFlags)
	if err != nil {
		// When the error is a rule error, it means the block was simply
		// rejected as opposed to something actually going wrong, so log
		// it as such.  Otherwise, something really did go wrong, so log
		// it as an actual error.
		if _, ok := err.(blockchain.RuleError); ok {
			log.Infof("Rejected block %v from %s: %v", blockHash,
				peer, err)
		} else {
			log.Errorf("Failed to process block %v: %v",
				blockHash, err)
		}
		if dbErr, ok := err.(database.Error); ok && dbErr.ErrorCode ==
			database.ErrCorruption {
			panic(dbErr)
		}

		// Convert the error into an appropriate reject message and
		// send it.
		code, reason := mempool.ErrToRejectErr(err)
		peer.PushRejectMsg(wire.CmdBlock, code, reason, blockHash, false)
		return
	}

	// Meta-data about the new block this peer is reporting. We use this
	// below to update this peer's latest block height and the heights of
	// other peers based on their last announced block hash. This allows us
	// to dynamically update the block heights of peers, avoiding stale
	// heights when looking for a new sync peer. Upon acceptance of a block
	// or recognition of an orphan, we also use this information to update
	// the block heights over other peers who's invs may have been ignored
	// if we are actively syncing while the chain is not yet current or
	// who may have lost the lock announcement race.
	var heightUpdate int32
	var blkHashUpdate *chainhash.Hash

	// Request the parents for the orphan block from the peer that sent it.
	if isOrphan {
		// We've just received an orphan block from a peer. In order
		// to update the height of the peer, we try to extract the
		// block height from the scriptSig of the coinbase transaction.
		// Extraction is only attempted if the block's version is
		// high enough (ver 2+).
		header := &bmsg.block.MsgBlock().Header
		if blockchain.ShouldHaveSerializedBlockHeight(header) {
			coinbaseTx := bmsg.block.Transactions()[0]
			cbHeight, err := blockchain.ExtractCoinbaseHeight(coinbaseTx)
			if err != nil {
				log.Warnf("Unable to extract height from "+
					"coinbase tx: %v", err)
			} else {
				log.Debugf("Extracted height of %v from "+
					"orphan block", cbHeight)
				heightUpdate = cbHeight
				blkHashUpdate = blockHash
			}
		}

		orphanRoot := sm.chain.GetOrphanRoot(blockHash)
		locator, err := sm.chain.LatestBlockLocator()
		if err != nil {
			log.Warnf("Failed to get block locator for the "+
				"latest block: %v", err)
		} else {
			peer.PushGetBlocksMsg(locator, orphanRoot)
		}
	} else {
		if peer == sm.syncPeer {
			sm.lastProgressTime = time.Now()
		}

		// It's safe to delete the utreexo block summary for this block now.
		delete(sm.utreexoSummaries, *bmsg.block.Hash())

		// When the block is not an orphan, log information about it and
		// update the chain state.
		sm.progressLogger.LogBlockHeight(bmsg.block, sm.chain)

		// Update this peer's latest block height, for future
		// potential sync node candidacy.
		best := sm.chain.BestSnapshot()
		heightUpdate = best.Height
		blkHashUpdate = &best.Hash

		// Clear the rejected transactions.
		sm.rejectedTxns = make(map[chainhash.Hash]struct{})
	}

	// Update the block height for this peer. But only send a message to
	// the server for updating peer heights if this is an orphan or our
	// chain is "current". This avoids sending a spammy amount of messages
	// if we're syncing the chain from scratch.
	if blkHashUpdate != nil && heightUpdate != 0 {
		peer.UpdateLastBlockHeight(heightUpdate)
		if isOrphan || sm.current() {
			go sm.peerNotifier.UpdatePeerHeights(blkHashUpdate, heightUpdate,
				peer)
		}
	}

	// If we are not in headers first mode, it's a good time to periodically
	// flush the blockchain cache because we don't expect new blocks immediately.
	// After that, there is nothing more to do.
	if !sm.headersFirstMode {
		// Flush relevant indexes.
		if err := sm.chain.FlushIndexes(blockchain.FlushPeriodic, true); err != nil {
			log.Errorf("Error while flushing the blockchain cache: %v", err)
		}
		// Only flush if utreexoView is not active since a utreexo node does
		// not have a utxo cache.
		if !sm.chain.IsUtreexoViewActive() {
			if err := sm.chain.FlushUtxoCache(blockchain.FlushPeriodic); err != nil {
				log.Errorf("Error while flushing the blockchain cache: %v", err)
			}
		}

		return
	}

	if isCheckpointBlock {
		log.Infof("on checkpoint block %v(%v)", bmsg.block.Hash(), bmsg.block.Height())
		nextCheckpoint := sm.findNextHeaderCheckpoint(bmsg.block.Height())
		if nextCheckpoint == nil {
			log.Infof("Reached the final checkpoint -- switching to normal mode")
		}
	}

	lastHeight := sm.syncPeer.LastBlock()
	if bmsg.block.Height() < lastHeight {
		if sm.startHeader != nil && len(state.requestedBlocks) == 0 {
			sm.fetchHeaderBlocks(nil)
		}
		return
	}
	if bmsg.block.Height() >= lastHeight {
		log.Infof("Finished the initial block download and caught up to block %v(%v) "+
			"-- now listening to blocks.", bmsg.block.Hash(), bmsg.block.Height())
		sm.headersFirstMode = false
	}
}

// fetchUtreexoSummaries creates and sends a request to the syncPeer for the next
// list of utreexo summaries to be downloaded based on the current list of headers.
// Will fetch from the peer if it's not nil. Otherwise it'll default to the syncPeer.
func (sm *SyncManager) fetchUtreexoSummaries(peer *peerpkg.Peer) {
	// Nothing to do if there is no start header.
	if sm.startHeader == nil {
		log.Warnf("fetchUtreexoSummaries called with no start header")
		return
	}

	// Can't fetch if both are nil.
	if peer == nil && sm.syncPeer == nil {
		log.Warnf("fetchUtreexoSummaries called with syncPeer and peer as nil")
		return
	}

	// Default to the syncPeer unless we're given a peer by the caller.
	reqPeer := sm.syncPeer
	if peer != nil {
		reqPeer = peer
	}

	state, exists := sm.peerStates[reqPeer]
	if !exists {
		log.Warnf("Don't have peer state for request peer %s", reqPeer.String())
		return
	}

	_, bestHeaderHeight := sm.chain.BestHeader()
	bestState := sm.chain.BestSnapshot()
	for h := bestState.Height + 1; h <= bestHeaderHeight; h++ {
		hash, err := sm.chain.HeaderHashByHeight(h)
		if err != nil {
			log.Warnf("error while fetching the block hash for height %v -- %v",
				h, err)
			return
		}

		_, requested := state.requestedUtreexoSummaries[*hash]
		_, have := sm.utreexoSummaries[*hash]
		if !requested && !have {
			state.requestedUtreexoSummaries[*hash] = struct{}{}
			ghmsg := wire.NewMsgGetUtreexoHeader(*hash, true)
			reqPeer.QueueMessage(ghmsg, nil)
		}

		if len(state.requestedUtreexoSummaries) > minInFlightBlocks {
			break
		}
	}
}

// fetchHeaderBlocks creates and sends a request to the syncPeer for the next
// list of blocks to be downloaded based on the current list of headers.
// Will fetch from the peer if it's not nil. Otherwise it'll default to the syncPeer.
func (sm *SyncManager) fetchHeaderBlocks(peer *peerpkg.Peer) {
	// Nothing to do if there is no start header.
	if sm.startHeader == nil {
		log.Warnf("fetchHeaderBlocks called with no start header")
		return
	}

	// Can't fetch if both are nil.
	if peer == nil && sm.syncPeer == nil {
		log.Warnf("fetchHeaderBlocks called with syncPeer and peer as nil")
		return
	}

	// Default to the syncPeer unless we're given a peer by the caller.
	reqPeer := sm.syncPeer
	if peer != nil {
		reqPeer = peer
	}

	bestHeaderHash, bestHeaderHeight := sm.chain.BestHeader()
	bestState := sm.chain.BestSnapshot()
	length := bestHeaderHeight - bestState.Height

	// Build up a getdata request for the list of blocks the headers
	// describe.  The size hint will be limited to wire.MaxInvPerMsg by
	// the function, so no need to double check it here.
	gdmsg := wire.NewMsgGetDataSizeHint(uint(length))
	numRequested := 0

	hash, err := sm.chain.HeaderHashByHeight(bestState.Height + 1)
	if err != nil {
		return
	}

	if sm.headersFirstMode {
		log.Infof("fetching from %v(%v) to %v(%v) from %v", hash, bestState.Height+1,
			bestHeaderHash, bestHeaderHeight, reqPeer.String())
	}
	for h := bestState.Height + 1; h <= bestHeaderHeight; h++ {
		hash, err := sm.chain.HeaderHashByHeight(h)
		if err != nil {
			log.Warnf("error while fetching the block hash for height %v -- %v",
				h, err)
			return
		}

		// If we're a csn then keep track of the numleaves. We need this to construct
		// the get proof message.
		if sm.chain.IsUtreexoViewActive() {
			header := sm.utreexoSummaries[*hash]
			numLeaves := sm.numLeaves[h-1] + uint64(header.NumAdds)
			sm.numLeaves[h] = numLeaves
		}

		requested := false
		if !sm.headersFirstMode {
			if _, exists := sm.requestedBlocks[*hash]; exists {
				requested = true
			}
		}

		iv := wire.NewInvVect(wire.InvTypeBlock, hash)
		haveInv, err := sm.haveInventory(iv)
		if err != nil {
			log.Warnf("Unexpected failure when checking for "+
				"existing inventory during header block "+
				"fetch: %v", err)
		}
		if !haveInv && !requested {
			syncPeerState := sm.peerStates[reqPeer]
			syncPeerState.requestedBlocks[*hash] = struct{}{}
			if !sm.headersFirstMode {
				sm.requestedBlocks[*hash] = struct{}{}
			}

			// If we're fetching from a witness enabled peer
			// post-fork, then ensure that we receive all the
			// witness data in the blocks.
			if reqPeer.IsWitnessEnabled() {
				iv.Type = wire.InvTypeWitnessBlock

				// If we're syncing from a utreexo enabled peer, also
				// ask for the proofs.
				if reqPeer.IsUtreexoEnabled() {
					iv.Type = wire.InvTypeWitnessUtreexoBlock
				}
			} else {
				// If we're syncing from a utreexo enabled peer, also
				// ask for the proofs.
				if reqPeer.IsUtreexoEnabled() {
					iv.Type = wire.InvTypeUtreexoBlock
				}
			}

			gdmsg.AddInvVect(iv)
			numRequested++

			// Immediately queue the utreexo proof for this block if we're a
			// utreexo node.
			if sm.chain.IsUtreexoViewActive() {
				utreexoSummary, found := sm.utreexoSummaries[*hash]
				if !found {
					log.Warnf("Missing utreexo summary for %v", hash)
					return
				}
				syncPeerState.requestedUtreexoProofs[*hash] = struct{}{}

				msg := wire.ConstructGetProofMsg(
					hash, sm.numLeaves[h-1], utreexoSummary.BlockTargets)
				reqPeer.QueueMessage(msg, nil)
			}
		}

		if h+1 <= bestHeaderHeight {
			hash, err := sm.chain.HeaderHashByHeight(h + 1)
			if err != nil {
				log.Warnf("error while fetching the block hash for height %v -- %v",
					h, err)
				return
			}
			sm.startHeader = &headerNode{h + 1, hash}
		}

		if numRequested >= wire.MaxInvPerMsg {
			break
		}
	}

	if len(gdmsg.InvList) > 0 {
		reqPeer.QueueMessage(gdmsg, nil)
	}
}

// handleHeadersMsg handles block header messages from all peers.  Headers are
// requested when performing a headers-first sync.
func (sm *SyncManager) handleHeadersMsg(hmsg *headersMsg) {
	peer := hmsg.peer
	_, exists := sm.peerStates[peer]
	if !exists {
		log.Warnf("Received headers message from unknown peer %s", peer)
		return
	}

	msg := hmsg.headers
	numHeaders := len(msg.Headers)

	// Nothing to do for an empty headers message.
	if numHeaders == 0 {
		return
	}

	utreexoViewActive := sm.chain.IsUtreexoViewActive()

	shouldFetchBlocks, shouldFetchHeaders := false, false
	for _, blockHeader := range msg.Headers {
		isMainChain, err := sm.chain.ProcessBlockHeader(blockHeader, blockchain.BFNone)
		if err != nil {
			log.Warnf("Received block header from peer %v "+
				"failed header verification -- disconnecting",
				peer.Addr())
			peer.Disconnect()
			return
		}
		shouldFetchHeaders = isMainChain

		sm.progressLogger.SetLastLogTime(time.Now())
	}

	bestHash, bestHeight := sm.chain.BestHeader()
	if sm.nextCheckpoint != nil {
		if bestHeight == sm.nextCheckpoint.Height {
			if bestHash.IsEqual(sm.nextCheckpoint.Hash) {
				log.Infof("Verified downloaded block "+
					"header against checkpoint at height "+
					"%d/hash %s", bestHeight, bestHash)
			} else {
				log.Warnf("Block header at height %d/hash "+
					"%s from peer %s does NOT match "+
					"expected checkpoint hash of %s -- "+
					"disconnecting", bestHeight,
					bestHash, peer.Addr(),
					sm.nextCheckpoint.Hash)
				peer.Disconnect()
				return
			}
			sm.nextCheckpoint = sm.findNextHeaderCheckpoint(bestHeight)
		}
	}

	if sm.headersBuildMode && bestHeight >= sm.chain.AssumeUtreexoHeight() {
		assumeUtreexoHash := sm.chain.AssumeUtreexoHash()
		if !bestHash.IsEqual(&assumeUtreexoHash) {
			log.Warnf("The node had hash %v hardcoded in but the valid proof-of-work "+
				"chain has the hash %v at height %v. The user should not trust this "+
				"software as genuine and there may be attempts to steal funds. The user "+
				"should delete the datadir", sm.chain.AssumeUtreexoHash().String(),
				bestHash.String(), bestHeight)
			os.Exit(1)
		}

		// We're done downloading headers.
		sm.headersBuildMode = false

		// No more headers first mode either.
		sm.headersFirstMode = false

		// Set the best state and the utreexo state.
		sm.chain.SetNewBestStateFromAssumedUtreexoPoint()
		sm.chain.SetUtreexoStateFromAssumePoint()

		bestState := sm.chain.BestSnapshot()
		log.Infof("Finished building headers. Initialized assumed utreexo point "+
			"at block %v(%d)", bestState.Hash.String(), bestState.Height)

		locator := blockchain.BlockLocator([]*chainhash.Hash{&bestState.Hash})
		err := peer.PushGetBlocksMsg(locator, &zeroHash)
		if err != nil {
			log.Warnf("Failed to send getblocks message to "+
				"peer %s: %v", peer.Addr(), err)
			return
		}

		return
	}

	if sm.headersFirstMode {
		if bestHeight < sm.syncPeer.LastBlock() {
			shouldFetchHeaders = true
		}

		if bestHeight >= sm.syncPeer.LastBlock() {
			shouldFetchBlocks = true
		}
	}

	if !sm.headersFirstMode && !sm.headersBuildMode {
		shouldFetchBlocks = true
	}

	if shouldFetchBlocks {
		bestState := sm.chain.BestSnapshot()
		sm.nextCheckpoint = sm.findNextHeaderCheckpoint(bestState.Height)
		if sm.startHeader == nil {
			hash, err := sm.chain.HeaderHashByHeight(bestState.Height + 1)
			if err != nil {
				return
			}
			sm.startHeader = &headerNode{bestState.Height + 1, hash}
		}

		bestHeaderHash, bestHeaderHeight := sm.chain.BestHeader()
		if utreexoViewActive {
			log.Infof("fetching utreexo summaries to %v(%v) from peer %v",
				bestHeaderHash, bestHeaderHeight, hmsg.peer.String())
			sm.fetchUtreexoSummaries(hmsg.peer)
		} else {
			log.Infof("fetching blocks to %v(%v) from peer %v",
				bestHeaderHash, bestHeaderHeight, hmsg.peer.String())
			sm.fetchHeaderBlocks(hmsg.peer)
		}

		return
	}

	// If we don't have the headers chain caught up to our peer, ask for more headers.
	if shouldFetchHeaders {
		locator := blockchain.BlockLocator([]*chainhash.Hash{&bestHash})

		stopHash := zeroHash
		if sm.headersBuildMode {
			stopHash = sm.chain.AssumeUtreexoHash()
		} else if sm.nextCheckpoint != nil {
			stopHash = *sm.nextCheckpoint.Hash
		}

		hmsg.peer.PushGetHeadersMsg(locator, &stopHash)
		return
	}
}

// handleUtreexoSummariesMsg is called during utreexo summaries first download. It checks that
// each summary was asked for and is from a known peer.
func (sm *SyncManager) handleUtreexoSummariesMsg(hmsg *utreexoSummariesMsg) {
	peer := hmsg.peer
	peerState, exists := sm.peerStates[peer]
	if !exists {
		log.Warnf("Received utreexo summary message from unknown peer %s", peer)
		return
	}
	msg := hmsg.summaries

	// If we're in headers first, check if we have the final utreexo block summary. If not,
	// ask for more utreexo block summaries.
	if sm.headersFirstMode {
		for _, summary := range msg.Summaries {
			_, found := peerState.requestedUtreexoSummaries[summary.BlockHash]
			if !found {
				log.Warnf("Got unrequested utreexo block summary from %s -- "+
					"disconnecting", peer.Addr())
				peer.Disconnect()
				return
			}

			// Since we received it, it's no longer requested.
			delete(peerState.requestedUtreexoSummaries, summary.BlockHash)

			sm.utreexoSummaries[summary.BlockHash] = summary
			log.Debugf("accepted utreexo summary for block %v. have %v summaries",
				summary.BlockHash, len(sm.utreexoSummaries))
		}

		sm.progressLogger.SetLastLogTime(time.Now())
		sm.lastProgressTime = time.Now()

		lastSummary := msg.Summaries[len(msg.Summaries)-1]
		height, err := sm.chain.HeaderHeightByHash(lastSummary.BlockHash)
		if err != nil {
			log.Warnf("error while fetching the block height for hash %v -- %v",
				lastSummary.BlockHash, err)
			return
		}

		if height == peer.LastBlock() {
			log.Infof("Received utreexo summaries to block "+
				"%d/hash %s. Fetching blocks",
				height, lastSummary.BlockHash)
			sm.fetchHeaderBlocks(nil)
			return
		}

		if len(peerState.requestedUtreexoSummaries) < minInFlightBlocks {
			sm.fetchUtreexoSummaries(nil)
		}

		return
	}

	for _, summary := range msg.Summaries {
		delete(peerState.requestedUtreexoSummaries, summary.BlockHash)
		sm.utreexoSummaries[summary.BlockHash] = summary
		log.Debugf("accepted utreexo summary for block %v. have %v headers",
			summary.BlockHash, len(sm.utreexoSummaries))
	}

	// We're not in headers-first mode. When we receive a utreexo summary,
	// immediately ask for the block.
	sm.fetchHeaderBlocks(peer)
}

// handleUtreexoProofMsg queues the utreexo proof and if we already have the block,
// it'll send that block to be processed by the block handler.
func (sm *SyncManager) handleUtreexoProofMsg(hmsg *utreexoProofMsg) {
	peer := hmsg.peer
	state, exists := sm.peerStates[peer]
	if !exists {
		log.Warnf("Received utreexo proof message from unknown peer %s", peer)
		return
	}

	blockHash := hmsg.proof.BlockHash
	if _, exists = state.requestedUtreexoProofs[blockHash]; !exists {
		log.Warnf("Got unrequested utreexo proof %v from %s -- "+
			"disconnecting", blockHash, peer.Addr())
		peer.Disconnect()
		return
	}

	sm.queuedUtreexoProofs[blockHash] = hmsg

	bmsg, haveBlock := sm.queuedBlocks[blockHash]
	if haveBlock {
		bmsg.reply = make(chan struct{}, 1)
		sm.msgChan <- bmsg
	}
}

// handleNotFoundMsg handles notfound messages from all peers.
func (sm *SyncManager) handleNotFoundMsg(nfmsg *notFoundMsg) {
	peer := nfmsg.peer
	state, exists := sm.peerStates[peer]
	if !exists {
		log.Warnf("Received notfound message from unknown peer %s", peer)
		return
	}
	for _, inv := range nfmsg.notFound.InvList {
		// verify the hash was actually announced by the peer
		// before deleting from the global requested maps.
		switch inv.Type {
		case wire.InvTypeUtreexoBlock:
			fallthrough
		case wire.InvTypeWitnessUtreexoBlock:
			fallthrough
		case wire.InvTypeWitnessBlock:
			fallthrough
		case wire.InvTypeBlock:
			if _, exists := state.requestedBlocks[inv.Hash]; exists {
				delete(state.requestedBlocks, inv.Hash)
				// The global map of requestedBlocks is not used
				// during headersFirstMode.
				if !sm.headersFirstMode {
					delete(sm.requestedBlocks, inv.Hash)
				}
			}

		case wire.InvTypeWitnessTx:
			fallthrough
		case wire.InvTypeUtreexoTx:
			fallthrough
		case wire.InvTypeWitnessUtreexoTx:
			fallthrough
		case wire.InvTypeTx:
			if _, exists := state.requestedTxns[inv.Hash]; exists {
				delete(state.requestedTxns, inv.Hash)
				delete(sm.requestedTxns, inv.Hash)
			}
		}
	}
}

// haveInventory returns whether or not the inventory represented by the passed
// inventory vector is known.  This includes checking all of the various places
// inventory can be when it is in different states such as blocks that are part
// of the main chain, on a side chain, in the orphan pool, and transactions that
// are in the memory pool (either the main pool or orphan pool).
func (sm *SyncManager) haveInventory(invVect *wire.InvVect) (bool, error) {
	switch invVect.Type {
	case wire.InvTypeUtreexoBlock:
		fallthrough
	case wire.InvTypeWitnessUtreexoBlock:
		fallthrough
	case wire.InvTypeWitnessBlock:
		fallthrough
	case wire.InvTypeBlock:
		// Ask chain if the block is known to it in any form (main
		// chain, side chain, or orphan).
		return sm.chain.HaveBlock(&invVect.Hash)

	case wire.InvTypeWitnessTx:
		fallthrough
	case wire.InvTypeUtreexoTx:
		fallthrough
	case wire.InvTypeWitnessUtreexoTx:
		fallthrough
	case wire.InvTypeTx:
		// Ask the transaction memory pool if the transaction is known
		// to it in any form (main pool or orphan).
		if sm.txMemPool.HaveTransaction(&invVect.Hash) {
			return true, nil
		}

		// Check if the transaction exists from the point of view of the
		// end of the main chain.  Note that this is only a best effort
		// since it is expensive to check existence of every output and
		// the only purpose of this check is to avoid downloading
		// already known transactions.  Only the first two outputs are
		// checked because the vast majority of transactions consist of
		// two outputs where one is some form of "pay-to-somebody-else"
		// and the other is a change output.
		//
		// If UtreexoView is active, then just return false.
		// TODO This checking should really be replaced with a
		// recently confirmed tx list like Bitcoin Core.
		// github.com/bitcoin/bitcoin/blob/1847ce2d49e13f76824bb6b52985e8ef5fbcd1db/src/net_processing.cpp#L1640-L1661
		if sm.chain.IsUtreexoViewActive() {
			return false, nil
		}
		prevOut := wire.OutPoint{Hash: invVect.Hash}
		for i := uint32(0); i < 2; i++ {
			prevOut.Index = i
			entry, err := sm.chain.FetchUtxoEntry(prevOut)
			if err != nil {
				return false, err
			}
			if entry != nil && !entry.IsSpent() {
				return true, nil
			}
		}

		return false, nil
	}

	// The requested inventory is is an unsupported type, so just claim
	// it is known to avoid requesting it.
	return true, nil
}

// handleInvMsg handles inv messages from all peers.
// We examine the inventory advertised by the remote peer and act accordingly.
func (sm *SyncManager) handleInvMsg(imsg *invMsg) {
	peer := imsg.peer
	state, exists := sm.peerStates[peer]
	if !exists {
		log.Warnf("Received inv message from unknown peer %s", peer)
		return
	}

	// Attempt to find the final block in the inventory list.  There may
	// not be one.
	lastBlock := -1
	invVects := imsg.inv.InvList
	for i := len(invVects) - 1; i >= 0; i-- {
		if invVects[i].Type == wire.InvTypeBlock {
			lastBlock = i
			break
		}
	}

	// If this inv contains a block announcement, and this isn't coming from
	// our current sync peer or we're current, then update the last
	// announced block for this peer. We'll use this information later to
	// update the heights of peers based on blocks we've accepted that they
	// previously announced.
	if lastBlock != -1 && (peer != sm.syncPeer || sm.current()) {
		peer.UpdateLastAnnouncedBlock(&invVects[lastBlock].Hash)
	}

	// Ignore invs from peers that aren't the sync if we are not current.
	// Helps prevent fetching a mass of orphans.
	if peer != sm.syncPeer && !sm.current() {
		return
	}

	// If we're a utreexo compact state node and our peer is not utreexo enabled,
	// we won't be able to validate blocks from this peer.
	if sm.chain.IsUtreexoViewActive() && !peer.IsUtreexoEnabled() {
		return
	}

	// Ignore invs when we're in headers build mode.
	if sm.headersBuildMode {
		return
	}

	// If our chain is current and a peer announces a block we already
	// know of, then update their current block height.
	if lastBlock != -1 && sm.current() {
		blkHeight, err := sm.chain.BlockHeightByHash(&invVects[lastBlock].Hash)
		if err == nil {
			peer.UpdateLastBlockHeight(blkHeight)
		}
	}

	// Don't request on inventory messages when we're in headers-first mode.
	if sm.headersFirstMode {
		return
	}

	// Request the advertised inventory if we don't already have it.  Also,
	// request parent blocks of orphans if we receive one we already have.
	// Finally, attempt to detect potential stalls due to long side chains
	// we already have and request more blocks to prevent them.
	for i := 0; i < len(invVects); i++ {
		iv := invVects[i]

		// Ignore unsupported inventory types.
		switch iv.Type {
		case wire.InvTypeBlock:
		case wire.InvTypeTx:
		case wire.InvTypeWitnessBlock:
		case wire.InvTypeWitnessUtreexoBlock:
		case wire.InvTypeUtreexoBlock:
		case wire.InvTypeWitnessTx:
		case wire.InvTypeUtreexoTx:
		case wire.InvTypeWitnessUtreexoTx:
		case wire.InvTypeUtreexoProofHash:
			// If the inv is a utreexo proof hash, then it means that
			// we've already skipped/added the tx that it belongs to or
			// we're in headers first mode.
			continue
		default:
			continue
		}

		// Ignore txs when we're not current as we can't verify them
		// and they'll just go in the orphan pool.
		if iv.Type == wire.InvTypeWitnessTx ||
			iv.Type == wire.InvTypeTx {

			if !sm.current() {
				continue
			}
		}

		addIv := *iv
		addIv.Type &^= wire.InvUtreexoFlag
		// Add the inventory to the cache of known inventory
		// for the peer.
		peer.AddKnownInventory(&addIv)

		// Request the inventory if we don't already have it.
		haveInv, err := sm.haveInventory(iv)
		if err != nil {
			log.Warnf("Unexpected failure when checking for "+
				"existing inventory during inv message "+
				"processing: %v", err)
			continue
		}
		if !haveInv {
			if iv.Type == wire.InvTypeTx {
				// Skip the transaction if it has already been
				// rejected.
				if _, exists := sm.rejectedTxns[iv.Hash]; exists {
					continue
				}
			}

			// Ignore invs block invs from non-witness enabled
			// peers, as after segwit activation we only want to
			// download from peers that can provide us full witness
			// data for blocks.
			if !peer.IsWitnessEnabled() && iv.Type == wire.InvTypeBlock {
				continue
			}

			// Add it to the request queue.
			state.requestQueue = append(state.requestQueue, iv)

			if sm.chain.IsUtreexoViewActive() {
				switch iv.Type {
				case wire.InvTypeTx:
				case wire.InvTypeWitnessTx:
				case wire.InvTypeUtreexoTx:
				case wire.InvTypeWitnessUtreexoTx:
				default:
					continue
				}

				for j := i + 1; j < len(invVects); j++ {
					if invVects[j].Type != wire.InvTypeUtreexoProofHash {
						break
					}
					state.requestQueue = append(state.requestQueue, invVects[j])
				}
			}

			continue
		}

		if iv.Type == wire.InvTypeBlock || iv.Type == wire.InvTypeUtreexoBlock {
			// The block is an orphan block that we already have.
			// When the existing orphan was processed, it requested
			// the missing parent blocks.  When this scenario
			// happens, it means there were more blocks missing
			// than are allowed into a single inventory message.  As
			// a result, once this peer requested the final
			// advertised block, the remote peer noticed and is now
			// resending the orphan block as an available block
			// to signal there are more missing blocks that need to
			// be requested.
			if sm.chain.IsKnownOrphan(&iv.Hash) {
				// Request blocks starting at the latest known
				// up to the root of the orphan that just came
				// in.
				orphanRoot := sm.chain.GetOrphanRoot(&iv.Hash)
				locator, err := sm.chain.LatestBlockLocator()
				if err != nil {
					log.Errorf("PEER: Failed to get block "+
						"locator for the latest block: "+
						"%v", err)
					continue
				}
				peer.PushGetBlocksMsg(locator, orphanRoot)
				continue
			}

			// We already have the final block advertised by this
			// inventory message, so force a request for more.  This
			// should only happen if we're on a really long side
			// chain.
			if i == lastBlock {
				// Request blocks after this one up to the
				// final one the remote peer knows about (zero
				// stop hash).
				locator := sm.chain.BlockLocatorFromHash(&iv.Hash)
				peer.PushGetBlocksMsg(locator, &zeroHash)
			}
		}
	}

	// Request as much as possible at once.  Anything that won't fit into
	// the request will be requested on the next inv message.
	numRequested := 0
	gdmsg := wire.NewMsgGetData()
	requestQueue := state.requestQueue
	for len(requestQueue) != 0 {
		iv := requestQueue[0]
		requestQueue[0] = nil
		requestQueue = requestQueue[1:]

		switch iv.Type {
		case wire.InvTypeUtreexoBlock:
			fallthrough
		case wire.InvTypeWitnessUtreexoBlock:
			fallthrough
		case wire.InvTypeWitnessBlock:
			fallthrough
		case wire.InvTypeBlock:
			// Request the block if there is not already a pending
			// request.
			//
			// No check for if we're in headers first since it's
			// already done so earlier in the method.
			if _, exists := sm.requestedBlocks[iv.Hash]; !exists {
				amUtreexoNode := sm.chain.IsUtreexoViewActive()
				if amUtreexoNode {
					ghmsg := wire.NewMsgGetUtreexoHeader(iv.Hash, true)
					peer.QueueMessage(ghmsg, nil)
					continue
				}

				limitAdd(sm.requestedBlocks, iv.Hash, maxRequestedBlocks)
				limitAdd(state.requestedBlocks, iv.Hash, maxRequestedBlocks)

				if peer.IsWitnessEnabled() {
					iv.Type = wire.InvTypeWitnessBlock
				}

				gdmsg.AddInvVect(iv)
				numRequested++
			}

		case wire.InvTypeWitnessTx:
			fallthrough
		case wire.InvTypeWitnessUtreexoTx:
			fallthrough
		case wire.InvTypeUtreexoTx:
			fallthrough
		case wire.InvTypeTx:
			amUtreexoNode := sm.chain.IsUtreexoViewActive()
			if !amUtreexoNode {
				// Request the transaction if there is not already a
				// pending request.
				if _, exists := sm.requestedTxns[iv.Hash]; !exists {
					limitAdd(sm.requestedTxns, iv.Hash, maxRequestedTxns)
					limitAdd(state.requestedTxns, iv.Hash, maxRequestedTxns)

					// If the peer is capable, request the txn
					// including all witness data.
					if peer.IsWitnessEnabled() {
						iv.Type = wire.InvTypeWitnessTx
					}

					gdmsg.AddInvVect(iv)
					numRequested++
				}
			} else {
				// Request the transaction if there is not already a
				// pending request.
				if _, exists := sm.requestedTxns[iv.Hash]; !exists {
					// Pop off all the utreexo proof hash invs that's related to this
					// tx from the request queue. These represent the positions of the
					// inputs.
					var targetPositions []chainhash.Hash
					for len(requestQueue) > 0 && requestQueue[0].Type == wire.InvTypeUtreexoProofHash {
						proofInv := requestQueue[0]
						targetPositions = append(targetPositions, proofInv.Hash)

						requestQueue[0] = nil
						requestQueue = requestQueue[1:]
					}

					log.Debugf("for tx %s(%v), got %v packed positions, which are %v",
						iv.Hash, iv.Type.String(),
						targetPositions, chainhash.PackedHashesToUint64(targetPositions))

					// Check that the proof invs+the current tx inv and all
					// other requested invs do not go over the max inv per
					// message limit.
					if len(targetPositions)+1+numRequested+1 >= wire.MaxInvPerMsg {
						break
					}

					var neededPositions []chainhash.Hash
					if len(targetPositions) > 0 {
						neededPositions = sm.chain.GetNeededPositions(targetPositions)
					}

					log.Debugf("need %v to prove tx %v", chainhash.PackedHashesToUint64(neededPositions), iv.Hash)

					limitAdd(sm.requestedTxns, iv.Hash, maxRequestedTxns)
					limitAdd(state.requestedTxns, iv.Hash, maxRequestedTxns)

					// If the peer is capable, request the txn
					// including all witness data.
					if peer.IsWitnessEnabled() {
						iv.Type = wire.InvTypeWitnessTx
					}

					// Add in the utreexo flag then add the tx inv.
					iv.Type = wire.InvTypeUtreexoTx
					gdmsg.AddInvVect(iv)
					numRequested++

					// Then add all the packed proof positions inv.
					for i := range neededPositions {
						gdmsg.AddInvVect(wire.NewInvVect(
							wire.InvTypeUtreexoProofHash,
							&neededPositions[i]),
						)
						numRequested++
					}
				}
			}
		case wire.InvTypeUtreexoProofHash:
			// Purposely left empty. Utreexo proof hash invs are not useful on their own.
			continue
		}

		if numRequested >= wire.MaxInvPerMsg {
			break
		}
	}
	state.requestQueue = requestQueue
	if len(gdmsg.InvList) > 0 {
		peer.QueueMessage(gdmsg, nil)
	}
}

// blockHandler is the main handler for the sync manager.  It must be run as a
// goroutine.  It processes block and inv messages in a separate goroutine
// from the peer handlers so the block (MsgBlock) messages are handled by a
// single thread without needing to lock memory data structures.  This is
// important because the sync manager controls which blocks are needed and how
// the fetching should proceed.
func (sm *SyncManager) blockHandler() {
	stallTicker := time.NewTicker(stallSampleInterval)
	defer stallTicker.Stop()

out:
	for {
		select {
		case m := <-sm.msgChan:
			switch msg := m.(type) {
			case *newPeerMsg:
				sm.handleNewPeerMsg(msg.peer)

			case *txMsg:
				sm.handleTxMsg(msg.tx, msg.peer, nil)
				msg.reply <- struct{}{}

			case *utreexoTxMsg:
				sm.handleTxMsg(&msg.utreexoTx.Tx, msg.peer,
					&wire.UData{
						AccProof:  msg.utreexoTx.MsgUtreexoTx().AccProof,
						LeafDatas: msg.utreexoTx.MsgUtreexoTx().LeafDatas,
					})
				msg.reply <- struct{}{}

			case *blockMsg:
				sm.handleBlockMsg(msg)
				msg.reply <- struct{}{}

			case *invMsg:
				sm.handleInvMsg(msg)

			case *headersMsg:
				sm.handleHeadersMsg(msg)

			case *utreexoSummariesMsg:
				sm.handleUtreexoSummariesMsg(msg)

			case *utreexoProofMsg:
				sm.handleUtreexoProofMsg(msg)

			case *notFoundMsg:
				sm.handleNotFoundMsg(msg)

			case *donePeerMsg:
				sm.handleDonePeerMsg(msg.peer)

			case getSyncPeerMsg:
				var peerID int32
				if sm.syncPeer != nil {
					peerID = sm.syncPeer.ID()
				}
				msg.reply <- peerID

			case processBlockMsg:
				_, isOrphan, err := sm.chain.ProcessBlock(
					msg.block, msg.flags)
				if err != nil {
					msg.reply <- processBlockResponse{
						isOrphan: false,
						err:      err,
					}
				}

				msg.reply <- processBlockResponse{
					isOrphan: isOrphan,
					err:      nil,
				}

			case isCurrentMsg:
				msg.reply <- sm.current()

			case pauseMsg:
				// Wait until the sender unpauses the manager.
				<-msg.unpause

			default:
				log.Warnf("Invalid message type in block "+
					"handler: %T", msg)
			}

		case <-stallTicker.C:
			sm.handleStallSample()

		case <-sm.quit:
			break out
		}
	}

	// Only try to flush utxo cache if it exists.  A utreexo node doesn't have
	// a utxo cache.
	if !sm.chain.IsUtreexoViewActive() {
		log.Debug("Block handler shutting down: flushing blockchain caches...")
		if err := sm.chain.FlushUtxoCache(blockchain.FlushRequired); err != nil {
			log.Errorf("Error while flushing blockchain caches: %v", err)
		}
	}

	sm.wg.Done()
	log.Trace("Block handler done")
}

// handleBlockchainNotification handles notifications from blockchain.  It does
// things such as request orphan block parents and relay accepted blocks to
// connected peers.
func (sm *SyncManager) handleBlockchainNotification(notification *blockchain.Notification) {
	switch notification.Type {
	// A block has been accepted into the block chain.  Relay it to other
	// peers.
	case blockchain.NTBlockAccepted:
		// Don't relay if we are not current. Other peers that are
		// current should already know about it.
		if !sm.current() {
			return
		}

		block, ok := notification.Data.(*btcutil.Block)
		if !ok {
			log.Warnf("Chain accepted notification is not a block.")
			break
		}

		// Generate the inventory vector and relay it.
		iv := wire.NewInvVect(wire.InvTypeBlock, block.Hash())
		sm.peerNotifier.RelayInventory(iv, block.MsgBlock().Header)

	// A block has been connected to the main block chain.
	case blockchain.NTBlockConnected:
		// Don't relay if we are not current. Other peers that are
		// current should already know about it.
		if !sm.current() {
			return
		}

		block, ok := notification.Data.(*btcutil.Block)
		if !ok {
			log.Warnf("Chain connected notification is not a block.")
			break
		}

		// Remove all of the transactions (except the coinbase) in the
		// connected block from the transaction pool.  Secondly, remove any
		// transactions which are now double spends as a result of these
		// new transactions.  Finally, remove any transaction that is
		// no longer an orphan. Transactions which depend on a confirmed
		// transaction are NOT removed recursively because they are still
		// valid.
		for _, tx := range block.Transactions()[1:] {
			sm.txMemPool.RemoveTransaction(tx, false)
			sm.txMemPool.RemoveDoubleSpends(tx)
			sm.txMemPool.RemoveOrphan(tx)
			sm.peerNotifier.TransactionConfirmed(tx)
			acceptedTxs := sm.txMemPool.ProcessOrphans(tx)
			sm.peerNotifier.AnnounceNewTransactions(acceptedTxs)
		}

		// Register block with the fee estimator, if it exists.
		if sm.feeEstimator != nil {
			err := sm.feeEstimator.RegisterBlock(block)

			// If an error is somehow generated then the fee estimator
			// has entered an invalid state. Since it doesn't know how
			// to recover, create a new one.
			if err != nil {
				sm.feeEstimator = mempool.NewFeeEstimator(
					mempool.DefaultEstimateFeeMaxRollback,
					mempool.DefaultEstimateFeeMinRegisteredBlocks)
			}
		}

	// A block has been disconnected from the main block chain.
	case blockchain.NTBlockDisconnected:
		block, ok := notification.Data.(*btcutil.Block)
		if !ok {
			log.Warnf("Chain disconnected notification is not a block.")
			break
		}

		// Reinsert all of the transactions (except the coinbase) into
		// the transaction pool.
		//
		// TODO handle txs here for utreexo nodes.
		for _, tx := range block.Transactions()[1:] {
			_, _, err := sm.txMemPool.MaybeAcceptTransaction(tx, nil,
				false, false)
			if err != nil {
				// Remove the transaction and all transactions
				// that depend on it if it wasn't accepted into
				// the transaction pool.
				sm.txMemPool.RemoveTransaction(tx, true)
			}
		}

		// Rollback previous block recorded by the fee estimator.
		if sm.feeEstimator != nil {
			sm.feeEstimator.Rollback(block.Hash())
		}
	}
}

// NewPeer informs the sync manager of a newly active peer.
func (sm *SyncManager) NewPeer(peer *peerpkg.Peer) {
	// Ignore if we are shutting down.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}
	sm.msgChan <- &newPeerMsg{peer: peer}
}

// QueueTx adds the passed transaction message and peer to the block handling
// queue. Responds to the done channel argument after the tx message is
// processed.
func (sm *SyncManager) QueueTx(tx *btcutil.Tx, peer *peerpkg.Peer, done chan struct{}) {
	// Don't accept more transactions if we're shutting down.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		done <- struct{}{}
		return
	}

	sm.msgChan <- &txMsg{tx: tx, peer: peer, reply: done}
}

// QueueUtreexoTx adds the passed transaction message and peer to the block handling
// queue. Responds to the done channel argument after the utreexo tx message is
// processed.
func (sm *SyncManager) QueueUtreexoTx(tx *btcutil.UtreexoTx, peer *peerpkg.Peer, done chan struct{}) {
	// Don't accept more transactions if we're shutting down.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		done <- struct{}{}
		return
	}

	sm.msgChan <- &utreexoTxMsg{utreexoTx: tx, peer: peer, reply: done}
}

// QueueBlock adds the passed block message and peer to the block handling
// queue. Responds to the done channel argument after the block message is
// processed.
func (sm *SyncManager) QueueBlock(block *btcutil.Block, peer *peerpkg.Peer, done chan struct{}) {
	// Don't accept more blocks if we're shutting down.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		done <- struct{}{}
		return
	}

	sm.msgChan <- &blockMsg{block: block, peer: peer, reply: done}
}

// QueueInv adds the passed inv message and peer to the block handling queue.
func (sm *SyncManager) QueueInv(inv *wire.MsgInv, peer *peerpkg.Peer) {
	// No channel handling here because peers do not need to block on inv
	// messages.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &invMsg{inv: inv, peer: peer}
}

// QueueHeaders adds the passed headers message and peer to the block handling
// queue.
func (sm *SyncManager) QueueHeaders(headers *wire.MsgHeaders, peer *peerpkg.Peer) {
	// No channel handling here because peers do not need to block on
	// headers messages.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &headersMsg{headers: headers, peer: peer}
}

// QueueUtreexoSummaries adds the passed utreexo summaries message and peer to the block handling
// queue.
func (sm *SyncManager) QueueUtreexoSummaries(summaries *wire.MsgUtreexoSummaries, peer *peerpkg.Peer) {
	// No channel handling here because peers do not need to block on
	// summaries messages.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &utreexoSummariesMsg{summaries: summaries, peer: peer}
}

// QueueUtreexoProof adds the utreexo proof to the block handling queue.
func (sm *SyncManager) QueueUtreexoProof(proof *wire.MsgUtreexoProof, peer *peerpkg.Peer) {
	// No channel handling here because peers do not need to block on
	// headers messages.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &utreexoProofMsg{proof: proof, peer: peer}
}

// QueueNotFound adds the passed notfound message and peer to the block handling
// queue.
func (sm *SyncManager) QueueNotFound(notFound *wire.MsgNotFound, peer *peerpkg.Peer) {
	// No channel handling here because peers do not need to block on
	// reject messages.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &notFoundMsg{notFound: notFound, peer: peer}
}

// DonePeer informs the blockmanager that a peer has disconnected.
func (sm *SyncManager) DonePeer(peer *peerpkg.Peer) {
	// Ignore if we are shutting down.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &donePeerMsg{peer: peer}
}

// Start begins the core block handler which processes block and inv messages.
func (sm *SyncManager) Start() {
	// Already started?
	if atomic.AddInt32(&sm.started, 1) != 1 {
		return
	}

	log.Trace("Starting sync manager")
	sm.wg.Add(1)
	go sm.blockHandler()
}

// Stop gracefully shuts down the sync manager by stopping all asynchronous
// handlers and waiting for them to finish.
func (sm *SyncManager) Stop() error {
	if atomic.AddInt32(&sm.shutdown, 1) != 1 {
		log.Warnf("Sync manager is already in the process of " +
			"shutting down")
		return nil
	}

	log.Infof("Sync manager shutting down")
	close(sm.quit)
	sm.wg.Wait()
	return nil
}

// SyncPeerID returns the ID of the current sync peer, or 0 if there is none.
func (sm *SyncManager) SyncPeerID() int32 {
	reply := make(chan int32)
	sm.msgChan <- getSyncPeerMsg{reply: reply}
	return <-reply
}

// ProcessBlock makes use of ProcessBlock on an internal instance of a block
// chain.
func (sm *SyncManager) ProcessBlock(block *btcutil.Block, flags blockchain.BehaviorFlags) (bool, error) {
	reply := make(chan processBlockResponse, 1)
	sm.msgChan <- processBlockMsg{block: block, flags: flags, reply: reply}
	response := <-reply
	return response.isOrphan, response.err
}

// IsCurrent returns whether or not the sync manager believes it is synced with
// the connected peers.
func (sm *SyncManager) IsCurrent() bool {
	reply := make(chan bool)
	sm.msgChan <- isCurrentMsg{reply: reply}
	return <-reply
}

// Pause pauses the sync manager until the returned channel is closed.
//
// Note that while paused, all peer and block processing is halted.  The
// message sender should avoid pausing the sync manager for long durations.
func (sm *SyncManager) Pause() chan<- struct{} {
	c := make(chan struct{})
	sm.msgChan <- pauseMsg{c}
	return c
}

// New constructs a new SyncManager. Use Start to begin processing asynchronous
// block, tx, and inv updates.
func New(config *Config) (*SyncManager, error) {
	sm := SyncManager{
		peerNotifier:        config.PeerNotifier,
		chain:               config.Chain,
		txMemPool:           config.TxMemPool,
		chainParams:         config.ChainParams,
		rejectedTxns:        make(map[chainhash.Hash]struct{}),
		requestedTxns:       make(map[chainhash.Hash]struct{}),
		requestedBlocks:     make(map[chainhash.Hash]struct{}),
		numLeaves:           make(map[int32]uint64),
		utreexoSummaries:    make(map[chainhash.Hash]*wire.UtreexoBlockSummary),
		queuedBlocks:        make(map[chainhash.Hash]*blockMsg),
		queuedUtreexoProofs: make(map[chainhash.Hash]*utreexoProofMsg),
		peerStates:          make(map[*peerpkg.Peer]*peerSyncState),
		progressLogger:      newBlockProgressLogger("Processed", log),
		msgChan:             make(chan interface{}, config.MaxPeers*3),
		quit:                make(chan struct{}),
		feeEstimator:        config.FeeEstimator,
	}

	best := sm.chain.BestSnapshot()
	if !config.DisableCheckpoints {
		// Initialize the next checkpoint based on the current height.
		sm.nextCheckpoint = sm.findNextHeaderCheckpoint(best.Height)
	} else {
		log.Info("Checkpoints are disabled")
	}

	// If we're at assume utreexo mode, build headers first.
	if sm.chain.IsUtreexoViewActive() && sm.chain.IsAssumeUtreexo() {
		log.Info("Assumed Utreexo is enabled. Downloading headers...")
		sm.headersBuildMode = true
	}

	// The utreexo summary contains the number of added leaves in the block. This
	// number added with the numleaves from the previous block gets us the numleaves
	// for the current block. Since the numLeaves map is empty on startup, we need
	// to put the numleaves for the best state here.
	if sm.chain.IsUtreexoViewActive() {
		utreexoView, err := sm.chain.FetchUtreexoViewpoint(&best.Hash)
		if err != nil {
			return nil, err
		}
		sm.numLeaves[best.Height] = utreexoView.NumLeaves()
	}

	sm.chain.Subscribe(sm.handleBlockchainNotification)

	return &sm, nil
}
