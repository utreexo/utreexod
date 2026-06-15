// Copyright (c) 2013-2017 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package netsync

import (
	"bytes"
	"crypto/sha256"
	"errors"
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

// maxPendingBlocks caps the depth of the block pipeline: received-but-unconnected
// blocks plus outstanding block requests. fetchHeaderBlocks stops requesting once
// the pipeline reaches this size, which bounds the number of full blocks held in
// memory when a peer delivers blocks faster than their proofs. It is a var so
// tests can lower it.
var maxPendingBlocks = 500

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

// utreexoProofMsg packages a bitcoin utreexo proof message and the peer it came from
// together so the block handler has access to that information.
type utreexoProofMsg struct {
	proof *wire.MsgUtreexoProof
	peer  *peerpkg.Peer
}

// pendingBlock holds the halves of a block that must be assembled before the
// block can connect. A compact state node needs both the block and its utreexo
// proof, which arrive as separate messages and may come from different peers. A
// full node only needs the block, so proof and proofPeer stay nil.
type pendingBlock struct {
	block     *btcutil.Block
	proof     *wire.MsgUtreexoProof
	blockPeer *peerpkg.Peer
	proofPeer *peerpkg.Peer
}

// complete reports whether every half needed to connect the block is present.
func (p *pendingBlock) complete(utreexoActive bool) bool {
	return p.block != nil && (!utreexoActive || p.proof != nil)
}

// utreexoTTLsMsg packages a bitcoin utreexo ttls message and the peer it came from
// together so the block handler has access to that information.
type utreexoTTLsMsg struct {
	ttls *wire.MsgUtreexoTTLs
	peer *peerpkg.Peer
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
	requestedUtreexoTTLs      map[wire.MsgGetUtreexoTTLs]struct{}
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
	headersFirstMode bool
	committedTTLAcc  *utreexo.Stump
	queuedTTLs       map[int32]wire.UtreexoTTL

	// pending holds blocks and utreexo proofs that have arrived but cannot
	// connect yet because a half is missing or the parent is not the tip. It
	// is keyed by blockhash so the two halves are matched regardless of
	// arrival order or which peer delivered them.
	pending map[chainhash.Hash]*pendingBlock

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
	_, bestHeaderHeight := sm.chain.BestHeader()
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

	sm.headersFirstMode = true

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
		requestedUtreexoTTLs:      make(map[wire.MsgGetUtreexoTTLs]struct{}),
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

	// Clear the outstanding utreexo proof requests so they can be re-requested.
	// When a stalled sync peer is kept rather than disconnected this state
	// survives, so without clearing it a dropped proof would never be asked for
	// again.
	for blockHash := range state.requestedUtreexoProofs {
		delete(state.requestedUtreexoProofs, blockHash)
	}
}

// updateSyncPeer choose a new sync peer to replace the current one. If
// dcSyncPeer is true, this method will also disconnect the current sync peer.
// If we are in header first mode, any header state related to prefetching is
// also reset in preparation for the next sync peer.
func (sm *SyncManager) updateSyncPeer(dcSyncPeer bool) {
	log.Debugf("Updating sync peer, no progress for: %v",
		time.Since(sm.lastProgressTime))

	// First, disconnect the current sync peer if requested. The peer stays
	// in peerStates until the disconnect completes and the done-peer
	// cleanup runs, so mark it ineligible to keep the startSync below from
	// re-picking the peer being disconnected and sending requests into a
	// closing connection.
	if dcSyncPeer {
		sm.syncPeer.Disconnect()
		if state, ok := sm.peerStates[sm.syncPeer]; ok {
			state.syncCandidate = false
		}
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

	ttls, found := sm.queuedTTLs[height]
	if found {
		block.SetUtreexoTTLs(&ttls)
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

// handleBlockMsg deposits a received block into the pending pool and connects any
// blocks that are now ready. The block can only connect once its parent is the
// tip and, for a compact state node, once its utreexo proof has also arrived.
func (sm *SyncManager) handleBlockMsg(bmsg *blockMsg) {
	peer := bmsg.peer
	blockHash := bmsg.block.Hash()
	state, exists := sm.peerStates[peer]
	if !exists {
		log.Warnf("Received block message from unknown peer %s", peer)
		return
	}

	// If we didn't ask for this block then the peer is misbehaving.
	_, requested := state.requestedBlocks[*blockHash]
	if !requested {
		// On any network but the regression test network an unrequested
		// block means the peer is misbehaving.
		if sm.chainParams != &chaincfg.RegressionNetParams {
			log.Warnf("Got unrequested block %v from %s -- disconnecting",
				blockHash, peer.Addr())
			peer.Disconnect()
			return
		}

		// The regression test framework intentionally sends blocks twice to
		// confirm duplicate insertion fails, so feed the block straight to
		// the chain instead of parking it in the pending pool. The block
		// can make held pending blocks connectable when it connects.
		sm.connectPendingBlock(&pendingBlock{block: bmsg.block, blockPeer: peer})
		sm.connectReadyBlocks()
		sm.maybeFetchMoreBlocks()
		return
	}

	// The block half is in hand so it no longer counts as outstanding,
	// regardless of whether its proof has arrived. This keeps the request
	// pipeline refilling even while a proof is still in flight.
	delete(state.requestedBlocks, *blockHash)
	if !sm.headersFirstMode {
		// The global map of requestedBlocks is not used during
		// headersFirstMode.
		delete(sm.requestedBlocks, *blockHash)
	}

	// The block can already be in the main chain when a copy held in the
	// pending pool connected after this peer was asked for it. There is
	// nothing left to do with it.
	if sm.chain.MainChainHasBlock(blockHash) {
		delete(sm.pending, *blockHash)
		return
	}

	pb := sm.pending[*blockHash]
	if pb == nil {
		pb = &pendingBlock{}
		sm.pending[*blockHash] = pb
	}
	pb.block = bmsg.block
	pb.blockPeer = peer

	sm.connectReadyBlocks()
	sm.maybeFetchMoreBlocks()
}

// connectReadyBlocks hands assembled pending blocks to the chain in
// parent-first order and keeps going as handing over one block makes more
// pending blocks ready. A block is ready once its parent block data is
// available locally: the parent is either connected or stored as a side-chain
// block, which is all the chain needs to store the child and reorganize once
// the child's branch carries the most work. Handing children over only after
// their parents keeps the utreexo accumulator updated strictly in sequence
// and guarantees a reorganization never reaches for an ancestor that hasn't
// been stored yet. A block whose parent is not a known pending header is also
// handed over so the chain can classify it as an orphan or as extending an
// invalid branch. Pending payloads may outlive the peer that delivered them,
// so this path never depends on the delivering peer still being connected.
func (sm *SyncManager) connectReadyBlocks() {
	utreexoActive := sm.chain.IsUtreexoViewActive()
	for {
		var ready *pendingBlock
		var readyHash chainhash.Hash
		for hash, pb := range sm.pending {
			if !pb.complete(utreexoActive) {
				continue
			}

			// Hold the block while its parent is a valid header whose
			// block is still being downloaded ahead of this one.
			prevHash := &pb.block.MsgBlock().Header.PrevBlock
			haveParent, err := sm.chain.HaveBlock(prevHash)
			if err != nil {
				log.Warnf("Unexpected failure when checking for the "+
					"parent of block %v: %v", hash, err)
				continue
			}
			if !haveParent && sm.chain.IsValidHeader(prevHash) {
				continue
			}

			ready, readyHash = pb, hash
			break
		}

		if ready == nil {
			return
		}

		delete(sm.pending, readyHash)
		sm.connectPendingBlock(ready)
	}
}

// connectPendingBlock validates and connects a single assembled block, updates
// the delivering peer's height, and attributes any failure to the responsible
// peer. connectReadyBlocks calls it only once the parent is the tip and, for a
// compact state node, the proof is present; the regression-test path also feeds
// an unrequested block here without a proof, which ProcessBlock then rejects.
func (sm *SyncManager) connectPendingBlock(pb *pendingBlock) {
	block := pb.block
	blockHash := block.Hash()

	// pb.proof is always present here for the in-order driver, but an
	// unrequested regression-test block is fed straight in without one. When
	// it's missing, ProcessBlock fails the "no utreexo proof" check and the
	// failure is attributed below rather than panicking.
	if sm.chain.IsUtreexoViewActive() && pb.proof != nil {
		udata := wire.UData{
			AccProof: utreexo.Proof{
				Targets: pb.proof.Targets,
				Proof:   pb.proof.ProofHashes,
			},
			LeafDatas: pb.proof.LeafDatas,
		}
		block.SetUtreexoData(&udata)
	}

	// Process the block based off the headers if we're still in headers-first
	// mode. This also attaches the ttls for the block when downloading them.
	isCheckpointBlock, behaviorFlags := sm.checkHeadersList(block)

	// Process the block to include validation, best chain selection, etc.
	isMainChain, isOrphan, err := sm.chain.ProcessBlock(block, behaviorFlags)
	if err != nil {
		if dbErr, ok := err.(database.Error); ok &&
			dbErr.ErrorCode == database.ErrCorruption {
			panic(dbErr)
		}
		sm.attributeConnectFailure(pb, blockHash, err)
		return
	}

	// An orphan's parent header is unknown, so ask the peer that delivered
	// the orphan for the blocks between our tip and the orphan's root. The
	// orphan's coinbase carries its height, which keeps the delivering
	// peer's height current for sync candidacy and updates the heights of
	// other peers that announced the same block.
	if isOrphan {
		bp := pb.blockPeer
		if bp == nil {
			return
		}
		if _, ok := sm.peerStates[bp]; !ok {
			return
		}

		header := &block.MsgBlock().Header
		if blockchain.ShouldHaveSerializedBlockHeight(header) {
			coinbaseTx := block.Transactions()[0]
			cbHeight, err := blockchain.ExtractCoinbaseHeight(coinbaseTx)
			if err != nil {
				log.Warnf("Unable to extract height from "+
					"coinbase tx: %v", err)
			} else {
				bp.UpdateLastBlockHeight(cbHeight)
				go sm.peerNotifier.UpdatePeerHeights(blockHash,
					cbHeight, bp)
			}
		}

		orphanRoot := sm.chain.GetOrphanRoot(blockHash)
		locator, err := sm.chain.LatestBlockLocator()
		if err != nil {
			log.Warnf("Failed to get block locator for the "+
				"latest block: %v", err)
		} else {
			bp.PushGetBlocksMsg(locator, orphanRoot)
		}
		return
	}

	// A block that extends the main chain consumes the queued ttl for its
	// height, so the ttl is no longer needed. A side chain block leaves it in
	// place for the block that does extend the main chain at that height.
	// Keeping the ttl until this point also lets a connect failure retry the
	// block with the ttl still attached.
	if isMainChain {
		delete(sm.queuedTTLs, block.Height())
	}

	// Connecting a block is forward progress, so reset the stall timer and the
	// rejected transactions.
	sm.lastProgressTime = time.Now()
	sm.progressLogger.LogBlockHeight(block, sm.chain)
	sm.rejectedTxns = make(map[chainhash.Hash]struct{})

	best := sm.chain.BestSnapshot()

	// Update the block height of the peer that delivered the block so that it
	// stays a viable sync candidate. Only announce heights once the chain is
	// current to avoid spamming during the initial block download.
	if bp := pb.blockPeer; bp != nil {
		if _, ok := sm.peerStates[bp]; ok {
			bp.UpdateLastBlockHeight(best.Height)
			if sm.current() {
				go sm.peerNotifier.UpdatePeerHeights(&best.Hash,
					best.Height, bp)
			}
		}
	}

	// If we're at the block when the swiftsync part of ibd ends, make sure the
	// aggregator is zero. Only a block that extends the main chain reaches the
	// swiftsync checkpoint height on the active chain.
	if isMainChain && sm.committedTTLAcc != nil &&
		block.Height() == int32(sm.committedTTLAcc.NumLeaves-1) {
		if !sm.chain.IsAggZero() {
			log.Warnf("The swiftsync aggregator is non-zero at swiftsync checkpoint at block %v(%v). "+
				"The binary is likely corrupted and the user should not trust this "+
				"software as genuine and there may be attempts to steal funds. The user "+
				"should delete the datadir and the binary from their computer.",
				block.Hash(), block.Height())
			os.Exit(1)
		}

		log.Infof("Verified swiftsync checkpoint block at %v(%v)", block.Hash(), block.Height())
	}

	// When not in headers-first mode it's a good time to periodically flush the
	// blockchain cache because we don't expect new blocks immediately.
	if !sm.headersFirstMode {
		if err := sm.chain.FlushIndexes(blockchain.FlushPeriodic, true); err != nil {
			log.Errorf("Error while flushing the blockchain cache: %v", err)
		}
		// Only flush the utxo cache for a full node since a utreexo node does
		// not have one.
		if !sm.chain.IsUtreexoViewActive() {
			if err := sm.chain.FlushUtxoCache(blockchain.FlushPeriodic); err != nil {
				log.Errorf("Error while flushing the blockchain cache: %v", err)
			}
		}
		return
	}

	if isCheckpointBlock {
		log.Infof("on checkpoint block %v(%v)", block.Hash(), block.Height())
		nextCheckpoint := sm.findNextHeaderCheckpoint(block.Height())
		if nextCheckpoint == nil {
			log.Infof("Reached the final checkpoint -- switching to normal mode")
		}
	}
}

// attributeConnectFailure assigns blame for a failed block connection without
// punishing a peer for the half it did not send. A block and its utreexo proof
// can arrive from different peers, and a combined verification failure is not
// always attributable to one half.
func (sm *SyncManager) attributeConnectFailure(pb *pendingBlock,
	blockHash *chainhash.Hash, err error) {

	// A consensus rule violation is unambiguously the block sender's fault, so
	// reject the block to that peer as is done for any bad block.
	if _, ok := err.(blockchain.RuleError); ok {
		log.Infof("Rejected block %v from %s: %v", blockHash, pb.blockPeer, err)
		sm.rejectBlockTo(pb.blockPeer, blockHash, err)
		return
	}

	log.Errorf("Failed to process block %v: %v", blockHash, err)

	// A reorganization that fails while attaching a different block says
	// nothing about the halves this pair's peers supplied, so nobody is
	// punished. The named block's stored data heals through the fetch pass
	// re-requesting its proof.
	var reorgErr blockchain.ReorgAttachError
	if errors.As(err, &reorgErr) && !reorgErr.BlockHash.IsEqual(blockHash) {
		return
	}

	// A block half loaded from the local store is hash-bound data that
	// passed proof of work and merkle checks, so when only the proof came
	// from the network the verification failure is the proof sender's
	// fault.
	if pb.blockPeer == nil && pb.proofPeer != nil {
		log.Warnf("Utreexo proof from %s does not validate against the "+
			"stored block %v -- disconnecting", pb.proofPeer, blockHash)
		pb.proofPeer.Disconnect()
		return
	}

	// When the block and proof came from different peers the bad half cannot be
	// identified, so drop both halves and re-fetch the pair from the sync peer
	// rather than rejecting an innocent peer. The block and proof were already
	// removed from the request maps on arrival, so a normal fetch re-requests
	// both, and the next failure then comes from a single peer.
	if pb.proofPeer != nil && pb.proofPeer != pb.blockPeer {
		sm.fetchHeaderBlocks(nil)
		return
	}

	// A single peer supplied a block and a utreexo proof that do not validate
	// together, which is a protocol violation, so disconnect it.
	if pb.proofPeer != nil {
		if bp := pb.blockPeer; bp != nil {
			log.Warnf("Block %v and its utreexo proof from %s do not "+
				"validate together -- disconnecting", blockHash, bp)
			bp.Disconnect()
		}
		return
	}

	// A full-node failure that isn't a rule error belongs to the block sender.
	sm.rejectBlockTo(pb.blockPeer, blockHash, err)
}

// rejectBlockTo sends a block reject message to the peer when it is still
// connected.
func (sm *SyncManager) rejectBlockTo(peer *peerpkg.Peer,
	blockHash *chainhash.Hash, err error) {

	if peer == nil {
		return
	}
	if _, ok := sm.peerStates[peer]; !ok {
		return
	}
	code, reason := mempool.ErrToRejectErr(err)
	peer.PushRejectMsg(wire.CmdBlock, code, reason, blockHash, false)
}

// maybeFetchMoreBlocks refills the block and proof request pipeline for the sync
// peer and leaves headers-first mode once the best chain reaches the best header.
// It keys the refill on outstanding block requests rather than on assembled
// blocks so that a missing proof never stalls the pipeline.
func (sm *SyncManager) maybeFetchMoreBlocks() {
	if !sm.headersFirstMode || sm.syncPeer == nil {
		return
	}
	state, ok := sm.peerStates[sm.syncPeer]
	if !ok {
		return
	}

	// The initial block download is done once the best chain tip is the
	// best header. Comparing the tips by hash matters during a
	// reorganization to a heavier branch with a lower tip height: the best
	// chain height can match or exceed the best header height while the
	// branch still has blocks left to download.
	best := sm.chain.BestSnapshot()
	if bestHeaderHash, _ := sm.chain.BestHeader(); best.Hash.IsEqual(&bestHeaderHash) {
		log.Infof("Finished the initial block download and caught up to block %v(%v) "+
			"-- now listening to blocks.", best.Hash, best.Height)
		sm.headersFirstMode = false
		return
	}

	// Bound the memory held by received-but-unconnected blocks when proofs lag
	// behind blocks.
	if len(sm.pending) >= maxPendingBlocks {
		return
	}

	if len(state.requestedBlocks) < minInFlightBlocks {
		sm.fetchHeaderBlocks(nil)
	}
}

// fetchUtreexoTTLs constructs a request for ttls that fetches as much as we could
// until we reach the end of the committed ttl accumulator state.
func (sm *SyncManager) fetchUtreexoTTLs(peer *peerpkg.Peer) {
	// Can't fetch if both are nil.
	if peer == nil && sm.syncPeer == nil {
		log.Warnf("fetchUtreexoTTLs called with syncPeer and peer as nil")
		return
	}

	if sm.committedTTLAcc == nil {
		log.Warnf("fetchUtreexoTTLs called with nil sm.committedTTLAcc")
		return
	}

	// Default to the syncPeer unless we're given a peer by the caller.
	reqPeer := sm.syncPeer
	if peer != nil {
		reqPeer = peer
	}

	peerState, exists := sm.peerStates[reqPeer]
	if !exists {
		log.Warnf("Don't have peer state for request peer %s", reqPeer.String())
		return
	}

	stump := *sm.committedTTLAcc

	bestState := sm.chain.BestSnapshot()
	gtmsg := wire.CalculateGetUtreexoTTLMsgs(
		uint32(stump.NumLeaves), bestState.Height+1,
		bestState.Height+wire.MaxUtreexoTTLsPerMsg)

	_, found := peerState.requestedUtreexoTTLs[gtmsg]
	if !found {
		peerState.requestedUtreexoTTLs[gtmsg] = struct{}{}

		log.Debugf("fetching ttls %v - %v from peer %v",
			gtmsg.StartHeight,
			gtmsg.StartHeight+(1<<gtmsg.MaxReceiveExponent),
			reqPeer.String())
		reqPeer.QueueMessage(&gtmsg, nil)
	}
}

// fetchHeaderBlocks creates and sends a request to the syncPeer for the next
// list of blocks to be downloaded based on the current list of headers.
// Will fetch from the peer if it's not nil. Otherwise it'll default to the syncPeer.
func (sm *SyncManager) fetchHeaderBlocks(peer *peerpkg.Peer) {
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

	peerState, exists := sm.peerStates[reqPeer]
	if !exists {
		log.Warnf("Don't have peer state for request peer %s", reqPeer.String())
		return
	}

	_, bestHeaderHeight := sm.chain.BestHeader()

	// Start fetching from the fork point between the best chain and
	// the best header chain rather than from the best chain height.
	// When the best header chain has diverged (e.g. due to a reorg),
	// blocks between the fork point and the current height on the new
	// chain are different and must also be downloaded.
	forkHeight := sm.chain.BestChainHeaderForkHeight()
	if bestHeaderHeight < forkHeight {
		return
	}
	length := bestHeaderHeight - forkHeight

	// Check if we have the ttl for the next block lined up to be downloaded.
	// Note: when committedTTLAcc is set, reorgs cannot happen below its
	// height as it acts as a checkpoint, so forkHeight will always equal
	// the best chain height in this case.
	if sm.chain.IsUtreexoViewActive() && sm.committedTTLAcc != nil &&
		forkHeight+1 <= int32(sm.committedTTLAcc.NumLeaves)-1 {

		// If we have no ttls, fetch for more.
		if len(sm.queuedTTLs) == 0 {
			sm.fetchUtreexoTTLs(reqPeer)
			return
		}

		// If we're downloading ttl messages before asking for blocks,
		// then the maximum amount of blocks we are able to download is
		// the max utreexo ttls per message.
		length = wire.MaxUtreexoTTLsPerMsg
	}

	// Build up a getdata request for the list of blocks the headers
	// describe.  The size hint will be limited to wire.MaxInvPerMsg by
	// the function, so no need to double check it here.
	gdmsg := wire.NewMsgGetDataSizeHint(uint(length))
	numRequested := 0

	for h := forkHeight + 1; h <= bestHeaderHeight; h++ {
		// Stop once the pipeline (pending blocks plus outstanding block
		// requests) reaches the cap so the number of full blocks held in
		// memory stays bounded when proofs lag behind blocks.
		if len(sm.pending)+len(peerState.requestedBlocks) >= maxPendingBlocks {
			break
		}

		if sm.chain.IsUtreexoViewActive() && sm.committedTTLAcc != nil {
			// Break if we ran out of ttls. Note: when committedTTLAcc
			// is set, reorgs cannot happen below its height as it acts
			// as a checkpoint, so forkHeight will always equal the best
			// chain height in this case.
			if h <= int32(sm.committedTTLAcc.NumLeaves)-1 &&
				h > forkHeight+int32(len(sm.queuedTTLs)) {
				break
			}
		}

		hash, err := sm.chain.HeaderHashByHeight(h)
		if err != nil {
			log.Warnf("error while fetching the block hash for height %v -- %v",
				h, err)
			return
		}

		// Nothing past a block that failed validation can connect, so
		// stop requesting at it. Progress resumes once a header branch
		// without the invalid block becomes the heaviest.
		if !sm.chain.IsValidHeader(hash) {
			break
		}

		iv := wire.NewInvVect(wire.InvTypeBlock, hash)
		haveInv, err := sm.haveInventory(iv)
		if err != nil {
			log.Warnf("Unexpected failure when checking for "+
				"existing inventory during header block "+
				"fetch: %v", err)
		}

		// Only fetch halves we don't already have. The block and its proof
		// are requested independently so that a half already held in the
		// pending pool (possibly delivered by an earlier sync peer) is
		// reused rather than re-requested.
		pb := sm.pending[*hash]
		if haveInv {
			// A block above the fork point whose data is stored is one
			// that previously failed to connect, e.g. because its utreexo
			// proof was bad or missing. Reuse the stored data as the block
			// half so a fresh proof is all that is needed to connect it.
			// The stale proof stored alongside the block is not reused.
			if sm.chain.IsUtreexoViewActive() &&
				(pb == nil || pb.block == nil) {

				block, err := sm.chain.StoredBlockByHash(hash)
				if err != nil {
					log.Warnf("Unable to load stored block %v: %v",
						hash, err)
					continue
				}
				if pb == nil {
					pb = &pendingBlock{}
					sm.pending[*hash] = pb
				}
				pb.block = block
			}
		} else {
			// Request the block half unless it has already arrived or is
			// already outstanding. At the chain tip the global map dedupes
			// the block across peers.
			_, blockRequested := peerState.requestedBlocks[*hash]
			if !sm.headersFirstMode {
				if _, g := sm.requestedBlocks[*hash]; g {
					blockRequested = true
				}
			}
			if (pb == nil || pb.block == nil) && !blockRequested {
				peerState.requestedBlocks[*hash] = struct{}{}

				// Track in the global map when not in headers-first
				// mode so other peers won't request the same block.
				if !sm.headersFirstMode {
					limitAdd(sm.requestedBlocks, *hash, maxRequestedBlocks)
				}

				iv.Type = wire.InvTypeBlock

				// If we're fetching from a witness enabled peer
				// post-fork, then ensure that we receive all the
				// witness data in the blocks.
				if reqPeer.IsWitnessEnabled() {
					iv.Type = wire.InvTypeWitnessBlock
				}

				gdmsg.AddInvVect(iv)
				numRequested++
			}
		}

		// Request the proof half for a compact state node unless it has
		// already arrived or is already outstanding.
		if sm.chain.IsUtreexoViewActive() {
			_, proofRequested := peerState.requestedUtreexoProofs[*hash]
			if (pb == nil || pb.proof == nil) && !proofRequested {
				peerState.requestedUtreexoProofs[*hash] = struct{}{}

				// If we still have ttls left to download, then we only
				// need the utreexo proof data since we're in swiftsync ibd.
				msg := wire.MsgGetUtreexoProof{BlockHash: *hash}
				if sm.committedTTLAcc != nil &&
					h <= int32(sm.committedTTLAcc.NumLeaves)-1 {
					msg.SetLeafDataRequestBit()
				} else {
					// If not, we need all the data.
					msg.SetTargetRequestBit()
					msg.SetProofHashRequestBit()
					msg.SetLeafDataRequestBit()
				}

				reqPeer.QueueMessage(&msg, nil)
			}
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

	// Update the last progress time to prevent the stall handler
	// from disconnecting the sync peer during header download.
	if peer == sm.syncPeer {
		sm.lastProgressTime = time.Now()
	}

	bestHash, bestHeight := sm.chain.BestHeader()
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

		// We're done downloading headers up to the assumed utreexo point.
		sm.headersBuildMode = false

		// Set the best state and the utreexo state.
		sm.chain.SetNewBestStateFromAssumedUtreexoPoint()
		sm.chain.SetUtreexoStateFromAssumePoint()

		bestState := sm.chain.BestSnapshot()
		log.Infof("Initialized assumed utreexo point at block %v(%d)",
			bestState.Hash.String(), bestState.Height)
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
		sm.fetchHeaderBlocks(hmsg.peer)
		return
	}

	// If we don't have the headers chain caught up to our peer, ask for more headers.
	if shouldFetchHeaders {
		locator := blockchain.BlockLocator([]*chainhash.Hash{&bestHash})
		stopHash := zeroHash
		if sm.headersBuildMode {
			stopHash = sm.chain.AssumeUtreexoHash()
		}
		hmsg.peer.PushGetHeadersMsg(locator, &stopHash)
		return
	}
}

// handleUtreexoProofMsg deposits the proof half into the pending pool and
// connects any blocks that are now ready.
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

	// The proof half is in hand so it no longer counts as outstanding.
	delete(state.requestedUtreexoProofs, blockHash)

	// The block can already be in the main chain when a copy held in the
	// pending pool connected after this peer was asked for its proof.
	// There is nothing left to do with it.
	if sm.chain.MainChainHasBlock(&blockHash) {
		delete(sm.pending, blockHash)
		return
	}

	pb := sm.pending[blockHash]
	if pb == nil {
		pb = &pendingBlock{}
		sm.pending[blockHash] = pb
	}
	pb.proof = hmsg.proof
	pb.proofPeer = peer

	sm.connectReadyBlocks()
	sm.maybeFetchMoreBlocks()
}

// handleUtreexoTTLsMsg handles the utreexottl message from all peers. Rejects the ttl message
// if the proof fails.
func (sm *SyncManager) handleUtreexoTTLsMsg(tmsg *utreexoTTLsMsg) {
	peer := tmsg.peer
	state, exists := sm.peerStates[peer]
	if !exists {
		log.Warnf("Received utreexo ttl message from unknown peer %s", peer)
		return
	}

	if sm.chain.GetUtreexoView() == nil {
		log.Warnf("Received unrequested utreexo ttl message from unknown peer %s "+
			"when the node isn't a utreexo node -- disconnecting", peer)
		peer.Disconnect()
		return
	}

	if sm.committedTTLAcc == nil {
		log.Warnf("Received unrequested utreexo ttl message from unknown peer %s "+
			"when the node has ttl messages disabled -- disconnecting", peer)
		peer.Disconnect()
		return
	}

	ttls := tmsg.ttls.TTLs
	startHeight := int32(ttls[0].BlockHeight)
	endHeight := int32(ttls[len(ttls)-1].BlockHeight)
	stump := *sm.committedTTLAcc

	// Construct the get message that would've gave us this ttl message.
	gotGtMsg := wire.CalculateGetUtreexoTTLMsgs(
		uint32(stump.NumLeaves), startHeight, endHeight)

	// Disconnect if we didn't request these.
	if _, exists = state.requestedUtreexoTTLs[gotGtMsg]; !exists {
		log.Warnf("Got unrequested utreexo ttls %v - %v from %s -- "+
			"disconnecting", startHeight, endHeight, peer.Addr())
		peer.Disconnect()
		return
	}

	// We can remove the request queue as we got the ttl message.
	delete(state.requestedUtreexoTTLs, gotGtMsg)

	ttlHashes := make([]utreexo.Hash, 0, len(ttls))
	ttlTargets := make([]uint64, 0, len(ttls))
	for _, ttl := range ttls {
		buf := bytes.NewBuffer(make([]byte, 0, ttl.SerializeSize()))
		err := ttl.Serialize(buf)
		if err != nil {
			log.Warnf("Failed to serialize utreexo ttl %v from %s. %v",
				ttl.BlockHeight, peer.Addr(), err)
			return
		}

		ttlTargets = append(ttlTargets, uint64(ttl.BlockHeight))
		ttlHashes = append(ttlHashes, sha256.Sum256(buf.Bytes()))
	}

	proof := utreexo.Proof{Targets: ttlTargets, Proof: tmsg.ttls.ProofHashes}
	_, err := utreexo.Verify(stump, ttlHashes, proof)
	if err != nil {
		log.Warnf("Utreexo ttl proof from %s failed verification -- "+
			"disconnecting", peer.Addr())
		peer.Disconnect()
		return
	}

	log.Debugf("verified proof for ttls %v - %v", startHeight, endHeight)

	// Accept the ttls.
	for _, ttlPerBlock := range ttls {
		sm.queuedTTLs[int32(ttlPerBlock.BlockHeight)] = ttlPerBlock
	}

	sm.fetchHeaderBlocks(nil)
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

		if iv.Type == wire.InvTypeBlock {
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
		case wire.InvTypeWitnessBlock:
			fallthrough
		case wire.InvTypeBlock:
			// Request the block if there is not already a pending
			// request.
			if _, exists := sm.requestedBlocks[iv.Hash]; !exists {
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

			case *utreexoProofMsg:
				sm.handleUtreexoProofMsg(msg)

			case *utreexoTTLsMsg:
				sm.handleUtreexoTTLsMsg(msg)

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

// QueueUtreexoProof adds the utreexo proof to the block handling queue.
func (sm *SyncManager) QueueUtreexoProof(proof *wire.MsgUtreexoProof, peer *peerpkg.Peer) {
	// No channel handling here because peers do not need to block on
	// headers messages.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &utreexoProofMsg{proof: proof, peer: peer}
}

// QueueUtreexoTTLs adds the utreexo ttls to the block handling queue.
func (sm *SyncManager) QueueUtreexoTTLs(ttls *wire.MsgUtreexoTTLs, peer *peerpkg.Peer) {
	// No channel handling here because peers do not need to block on
	// utreexo ttl messages.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &utreexoTTLsMsg{ttls: ttls, peer: peer}
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
		peerNotifier:    config.PeerNotifier,
		chain:           config.Chain,
		txMemPool:       config.TxMemPool,
		chainParams:     config.ChainParams,
		rejectedTxns:    make(map[chainhash.Hash]struct{}),
		requestedTxns:   make(map[chainhash.Hash]struct{}),
		requestedBlocks: make(map[chainhash.Hash]struct{}),
		queuedTTLs:      make(map[int32]wire.UtreexoTTL),
		pending:         make(map[chainhash.Hash]*pendingBlock),
		peerStates:      make(map[*peerpkg.Peer]*peerSyncState),
		progressLogger:  newBlockProgressLogger("Processed", log),
		msgChan:         make(chan interface{}, config.MaxPeers*3),
		quit:            make(chan struct{}),
		feeEstimator:    config.FeeEstimator,
	}

	if sm.chain.IsUtreexoViewActive() {
		if len(sm.chainParams.TTL.Stump) > 0 {
			// Initialize the committed ttl state.
			sm.committedTTLAcc = &sm.chainParams.TTL.Stump[len(sm.chainParams.TTL.Stump)-1]

			log.Info("TTL downloading initialized")
		}
	}

	if config.DisableCheckpoints {
		log.Info("Checkpoints are disabled")
	}

	// If we're at assume utreexo mode, build headers first.
	if sm.chain.IsUtreexoViewActive() && sm.chain.IsAssumeUtreexo() {
		log.Info("Assumed Utreexo is enabled. Downloading headers...")
		sm.headersBuildMode = true
	}

	sm.chain.Subscribe(sm.handleBlockchainNotification)

	return &sm, nil
}
