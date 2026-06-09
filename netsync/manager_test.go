package netsync

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/database"
	_ "github.com/utreexo/utreexod/database/ffldb"
	"github.com/utreexo/utreexod/mempool"
	"github.com/utreexo/utreexod/peer"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
)

// The package-level log variable is nil by default. Set it to the
// disabled logger so that log calls in the sync manager don't panic.
func init() {
	DisableLog()
}

// noopPeerNotifier is a no-op implementation of PeerNotifier for tests.
type noopPeerNotifier struct{}

func (noopPeerNotifier) AnnounceNewTransactions([]*mempool.TxDesc)            {}
func (noopPeerNotifier) UpdatePeerHeights(*chainhash.Hash, int32, *peer.Peer) {}
func (noopPeerNotifier) RelayInventory(*wire.InvVect, interface{})            {}
func (noopPeerNotifier) TransactionConfirmed(*btcutil.Tx)                     {}

// dbSetup is used to create a new db with the genesis block already inserted.
// It returns a teardown function the caller should invoke when done testing to
// clean up.  The database is stored under t.TempDir() which is automatically
// removed when the test finishes.
func dbSetup(t *testing.T, params *chaincfg.Params) (database.DB, func(), error) {
	dbPath := filepath.Join(t.TempDir(), "ffldb")
	db, err := database.Create("ffldb", dbPath, params.Net)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating db: %v", err)
	}

	teardown := func() {
		db.Close()
	}

	return db, teardown, nil
}

// chainSetup is used to create a new db and chain instance with the genesis
// block already inserted.  In addition to the new chain instance, it returns
// a teardown function the caller should invoke when done testing to clean up.
func chainSetup(t *testing.T, params *chaincfg.Params) (
	*blockchain.BlockChain, func(), error) {

	db, teardown, err := dbSetup(t, params)
	if err != nil {
		return nil, nil, err
	}

	// Copy the chain params to ensure any modifications the tests do to
	// the chain parameters do not affect the global instance.
	paramsCopy := *params

	// Deep-copy deployment starters/enders so that parallel tests don't
	// race on the shared blockClock field written by SynchronizeClock.
	for i := range paramsCopy.Deployments {
		d := &paramsCopy.Deployments[i]
		if s, ok := d.DeploymentStarter.(*chaincfg.MedianTimeDeploymentStarter); ok {
			d.DeploymentStarter = chaincfg.NewMedianTimeDeploymentStarter(
				s.StartTime())
		}
		if e, ok := d.DeploymentEnder.(*chaincfg.MedianTimeDeploymentEnder); ok {
			d.DeploymentEnder = chaincfg.NewMedianTimeDeploymentEnder(
				e.EndTime())
		}
	}

	// Create the main chain instance.
	chain, err := blockchain.New(&blockchain.Config{
		DB:          db,
		Checkpoints: paramsCopy.Checkpoints,
		ChainParams: &paramsCopy,
		TimeSource:  blockchain.NewMedianTime(),
		SigCache:    txscript.NewSigCache(1000),
	})
	if err != nil {
		teardown()
		err := fmt.Errorf("failed to create chain instance: %v", err)
		return nil, nil, err
	}
	return chain, teardown, nil
}

// loadHeaders loads headers from mainnet from 1 to 11.
func loadHeaders(t *testing.T) []*wire.BlockHeader {
	testFile := "blockheaders-mainnet-1-11.txt"
	filename := filepath.Join("testdata/", testFile)

	file, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}

	headers := make([]*wire.BlockHeader, 0, 10)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		b, err := hex.DecodeString(line)
		if err != nil {
			t.Fatalf("failed to read block headers from file %v", testFile)
		}

		r := bytes.NewReader(b)
		header := new(wire.BlockHeader)
		if err := header.Deserialize(r); err != nil {
			t.Fatalf("failed to deserialize header from file %v: %v",
				testFile, err)
		}

		headers = append(headers, header)
	}

	return headers
}

func makeMockSyncManager(t *testing.T,
	params *chaincfg.Params) (*SyncManager, func()) {

	t.Helper()

	chain, tearDown, err := chainSetup(t, params)
	require.NoError(t, err)

	sm, err := New(&Config{
		PeerNotifier: noopPeerNotifier{},
		Chain:        chain,
		ChainParams:  params,
	})
	require.NoError(t, err)

	return sm, tearDown
}

// makeMockSyncManagerUtreexo returns a SyncManager whose underlying chain has
// an empty utreexo viewpoint installed, so chain.IsUtreexoViewActive() reports
// true. fetchHeaderBlocks's queue-reuse branches only fire in this mode.
func makeMockSyncManagerUtreexo(t *testing.T,
	params *chaincfg.Params) (*SyncManager, func()) {

	t.Helper()

	db, teardown, err := dbSetup(t, params)
	require.NoError(t, err)

	paramsCopy := *params
	for i := range paramsCopy.Deployments {
		d := &paramsCopy.Deployments[i]
		if s, ok := d.DeploymentStarter.(*chaincfg.MedianTimeDeploymentStarter); ok {
			d.DeploymentStarter = chaincfg.NewMedianTimeDeploymentStarter(
				s.StartTime())
		}
		if e, ok := d.DeploymentEnder.(*chaincfg.MedianTimeDeploymentEnder); ok {
			d.DeploymentEnder = chaincfg.NewMedianTimeDeploymentEnder(
				e.EndTime())
		}
	}

	chain, err := blockchain.New(&blockchain.Config{
		DB:          db,
		Checkpoints: paramsCopy.Checkpoints,
		ChainParams: &paramsCopy,
		TimeSource:  blockchain.NewMedianTime(),
		SigCache:    txscript.NewSigCache(1000),
		UtreexoView: blockchain.NewUtreexoViewpoint(),
	})
	require.NoError(t, err)

	sm, err := New(&Config{
		PeerNotifier: noopPeerNotifier{},
		Chain:        chain,
		ChainParams:  &paramsCopy,
		MaxPeers:     8,
	})
	require.NoError(t, err)

	return sm, teardown
}

func TestCheckHeadersList(t *testing.T) {
	// Set params to mainnet with a checkpoint at block 11.
	params := chaincfg.MainNetParams
	checkpointHeight := int32(11)
	checkpointHash, err := chainhash.NewHashFromStr(
		"0000000097be56d606cdd9c54b04d4747e957d3608abe69198c661f2add73073")
	if err != nil {
		t.Fatal(err)
	}
	params.Checkpoints = []chaincfg.Checkpoint{
		{
			Height: checkpointHeight,
			Hash:   checkpointHash,
		},
	}

	// Create mock SyncManager.
	sm, tearDown := makeMockSyncManager(t, &params)
	defer tearDown()

	// Setup SyncManager with headers processed.
	headers := loadHeaders(t)
	for _, header := range headers {
		isMainChain, err := sm.chain.ProcessBlockHeader(
			header, blockchain.BFNone)
		if err != nil {
			t.Fatal(err)
		}

		if !isMainChain {
			t.Fatalf("expected block header %v to be in the main chain",
				header.BlockHash())
		}
	}

	// Build a map from block hash to header for creating test blocks.
	headerByHash := make(map[chainhash.Hash]*wire.BlockHeader, len(headers)+1)
	genesisHeader := &params.GenesisBlock.Header
	genesisHash := genesisHeader.BlockHash()
	headerByHash[genesisHash] = genesisHeader
	for _, hdr := range headers {
		h := hdr.BlockHash()
		headerByHash[h] = hdr
	}

	tests := []struct {
		hash              string
		isCheckpointBlock bool
		behaviorFlags     blockchain.BehaviorFlags
	}{
		{
			hash:              chaincfg.MainNetParams.GenesisHash.String(),
			isCheckpointBlock: false,
			behaviorFlags:     blockchain.BFFastAdd,
		},
		{
			// Block 10.
			hash:              "000000002c05cc2e78923c34df87fd108b22221ac6076c18f3ade378a4d915e9",
			isCheckpointBlock: false,
			behaviorFlags:     blockchain.BFFastAdd,
		},
		{
			// Block 11.
			hash:              "0000000097be56d606cdd9c54b04d4747e957d3608abe69198c661f2add73073",
			isCheckpointBlock: true,
			behaviorFlags:     blockchain.BFFastAdd,
		},
		{
			// Block 12 -- not a processed header.  Its real header
			// is not available, so the test constructs a zero-value
			// block whose hash won't match any known header.  This
			// exercises the "unknown block" early-return path.
			hash:              "0000000027c2488e2510d1acf4369787784fa20ee084c258b58d9fbd43802b5e",
			isCheckpointBlock: false,
			behaviorFlags:     blockchain.BFNone,
		},
	}

	for _, test := range tests {
		hash, err := chainhash.NewHashFromStr(test.hash)
		if err != nil {
			t.Errorf("NewHashFromStr: %v", err)
			continue
		}

		// In utreexod, checkHeadersList takes a Block. Create a
		// block with the correct header so its hash matches.
		var msgBlock *wire.MsgBlock
		if hdr, ok := headerByHash[*hash]; ok {
			msgBlock = &wire.MsgBlock{Header: *hdr}
		} else {
			// Unknown header — use a zero-value block whose hash
			// won't match any valid header.
			msgBlock = &wire.MsgBlock{}
		}
		block := btcutil.NewBlock(msgBlock)

		isCheckpoint, gotFlags := sm.checkHeadersList(block)
		require.Equal(t, test.isCheckpointBlock, isCheckpoint)
		require.Equal(t, test.behaviorFlags, gotFlags)
	}
}

// mockTimeSource is used to trick the BlockChain instance to think that we're
// in the past.  This is so that we can force it to return true for isCurrent().
type mockTimeSource struct {
	adjustedTime time.Time
}

// AdjustedTime returns the internal adjustedTime.
//
// Part of the MedianTimeSource interface implementation.
func (m *mockTimeSource) AdjustedTime() time.Time {
	return m.adjustedTime
}

// AddTimeSample isn't relevant so we just leave it as empty.
//
// Part of the MedianTimeSource interface implementation.
func (m *mockTimeSource) AddTimeSample(id string, timeVal time.Time) {
	// purposely left empty
}

// Offset isn't relevant so we just return 0.
//
// Part of the MedianTimeSource interface implementation.
func (m *mockTimeSource) Offset() time.Duration {
	return 0
}

func TestIsInHeadersFirstMode(t *testing.T) {
	tests := []struct {
		name             string
		peerState        map[*peer.Peer]*peerSyncState
		params           *chaincfg.Params
		timesource       *mockTimeSource
		headersFirstMode bool
	}{
		// Is not current, higher peers.
		{
			name:   "not current with higher peers",
			params: &chaincfg.MainNetParams,
			peerState: func() map[*peer.Peer]*peerSyncState {
				ps := make(map[*peer.Peer]*peerSyncState)
				peer := peer.NewInboundPeer(&peer.Config{})
				peer.UpdateLastBlockHeight(900_000)
				ps[peer] = &peerSyncState{
					syncCandidate:             true,
					requestedTxns:             make(map[chainhash.Hash]struct{}),
					requestedBlocks:           make(map[chainhash.Hash]struct{}),
					requestedUtreexoSummaries: make(map[chainhash.Hash]struct{}),
					requestedUtreexoProofs:    make(map[chainhash.Hash]struct{}),
					requestedUtreexoTTLs:      make(map[wire.MsgGetUtreexoTTLs]struct{}),
				}
				return ps
			}(),
			timesource:       nil,
			headersFirstMode: true,
		},
		// Is not current, no higher peers.
		{
			name:   "not current without higher peers",
			params: &chaincfg.MainNetParams,
			peerState: func() map[*peer.Peer]*peerSyncState {
				ps := make(map[*peer.Peer]*peerSyncState)
				peer := peer.NewInboundPeer(&peer.Config{})
				peer.UpdateLastBlockHeight(0)
				ps[peer] = &peerSyncState{
					syncCandidate:             true,
					requestedTxns:             make(map[chainhash.Hash]struct{}),
					requestedBlocks:           make(map[chainhash.Hash]struct{}),
					requestedUtreexoSummaries: make(map[chainhash.Hash]struct{}),
					requestedUtreexoProofs:    make(map[chainhash.Hash]struct{}),
					requestedUtreexoTTLs:      make(map[wire.MsgGetUtreexoTTLs]struct{}),
				}
				return ps
			}(),
			timesource:       nil,
			headersFirstMode: true,
		},
		// Is current, higher peers.
		{
			name: "current with higher peers",
			params: func() *chaincfg.Params {
				params := chaincfg.MainNetParams
				params.Checkpoints = nil
				return &params
			}(),
			peerState: func() map[*peer.Peer]*peerSyncState {
				ps := make(map[*peer.Peer]*peerSyncState)
				peer := peer.NewInboundPeer(&peer.Config{})
				peer.UpdateLastBlockHeight(900_000)
				ps[peer] = &peerSyncState{
					syncCandidate:             true,
					requestedTxns:             make(map[chainhash.Hash]struct{}),
					requestedBlocks:           make(map[chainhash.Hash]struct{}),
					requestedUtreexoSummaries: make(map[chainhash.Hash]struct{}),
					requestedUtreexoProofs:    make(map[chainhash.Hash]struct{}),
					requestedUtreexoTTLs:      make(map[wire.MsgGetUtreexoTTLs]struct{}),
				}
				return ps
			}(),
			timesource: &mockTimeSource{
				chaincfg.MainNetParams.GenesisBlock.Header.Timestamp,
			},
			headersFirstMode: true,
		},
		// Is current, no higher peers.
		{
			name: "current without higher peers",
			params: func() *chaincfg.Params {
				params := chaincfg.MainNetParams
				params.Checkpoints = nil
				return &params
			}(),
			peerState: func() map[*peer.Peer]*peerSyncState {
				ps := make(map[*peer.Peer]*peerSyncState)
				peer := peer.NewInboundPeer(&peer.Config{})
				peer.UpdateLastBlockHeight(0)
				ps[peer] = &peerSyncState{
					syncCandidate:             true,
					requestedTxns:             make(map[chainhash.Hash]struct{}),
					requestedBlocks:           make(map[chainhash.Hash]struct{}),
					requestedUtreexoSummaries: make(map[chainhash.Hash]struct{}),
					requestedUtreexoProofs:    make(map[chainhash.Hash]struct{}),
					requestedUtreexoTTLs:      make(map[wire.MsgGetUtreexoTTLs]struct{}),
				}
				return ps
			}(),
			timesource: &mockTimeSource{
				chaincfg.MainNetParams.GenesisBlock.Header.Timestamp,
			},
			headersFirstMode: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, tearDown, err := dbSetup(t, test.params)
			if err != nil {
				tearDown()
				t.Fatal(err)
			}

			timesource := blockchain.NewMedianTime()
			if test.timesource != nil {
				timesource = test.timesource
			}

			// Create the main chain instance.
			chain, err := blockchain.New(&blockchain.Config{
				DB:          db,
				Checkpoints: test.params.Checkpoints,
				ChainParams: test.params,
				TimeSource:  timesource,
				SigCache:    txscript.NewSigCache(1000),
			})
			if err != nil {
				tearDown()
				t.Fatal(err)
			}
			sm, err := New(&Config{
				Chain:       chain,
				ChainParams: test.params,
			})
			if err != nil {
				tearDown()
				t.Fatal(err)
			}

			// Run test and assert.
			sm.peerStates = test.peerState
			sm.startSync()
			require.Equal(t, test.headersFirstMode, sm.headersFirstMode)
			tearDown()
		})
	}
}

// createTestCoinbase creates a minimal coinbase transaction for the given
// block height.  The signature script encodes the height to ensure unique
// transaction hashes across blocks.
func createTestCoinbase(height int32, params *chaincfg.Params) *wire.MsgTx {
	tx := wire.NewMsgTx(wire.TxVersion)

	// Push the height as data to guarantee unique txids per block.
	sigScript := []byte{
		0x04,
		byte(height), byte(height >> 8),
		byte(height >> 16), byte(height >> 24),
	}

	tx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{
			Hash:  chainhash.Hash{},
			Index: wire.MaxPrevOutIndex,
		},
		SignatureScript: sigScript,
		Sequence:        wire.MaxTxInSequenceNum,
	})

	tx.AddTxOut(&wire.TxOut{
		Value:    blockchain.CalcBlockSubsidy(height, params),
		PkScript: []byte{txscript.OP_TRUE},
	})

	return tx
}

// solveTestBlock finds a nonce that satisfies the proof of work for the given
// header.  With regression test parameters the difficulty is minimal and a
// solution is found almost immediately.
func solveTestBlock(header *wire.BlockHeader, params *chaincfg.Params) bool {
	target := blockchain.CompactToBig(params.PowLimitBits)
	for nonce := uint32(0); nonce < math.MaxUint32; nonce++ {
		header.Nonce = nonce
		hash := header.BlockHash()
		if blockchain.HashToBig(&hash).Cmp(target) <= 0 {
			return true
		}
	}

	return false
}

// generateTestBlocks creates count valid blocks chaining from the genesis
// block of the given params.  Each block contains only a coinbase transaction.
func generateTestBlocks(
	t *testing.T, params *chaincfg.Params, count int) []*btcutil.Block {

	t.Helper()

	blocks := make([]*btcutil.Block, 0, count)
	prevHash := params.GenesisHash
	prevTime := params.GenesisBlock.Header.Timestamp

	for h := int32(1); h <= int32(count); h++ {
		cb := createTestCoinbase(h, params)
		merkleRoot := cb.TxHash()

		header := wire.BlockHeader{
			Version:    1,
			PrevBlock:  *prevHash,
			MerkleRoot: merkleRoot,
			Timestamp:  prevTime.Add(time.Minute),
			Bits:       params.PowLimitBits,
		}
		require.True(t, solveTestBlock(&header, params),
			"failed to solve block at height %d", h)

		msgBlock := &wire.MsgBlock{
			Header:       header,
			Transactions: []*wire.MsgTx{cb},
		}
		block := btcutil.NewBlock(msgBlock)
		blocks = append(blocks, block)

		bh := block.Hash()
		prevHash = bh
		prevTime = header.Timestamp
	}

	return blocks
}

// TestSyncStateMachine exercises the end-to-end IBD sync flow:
//
//	┌→ startSync
//	│      ↓
//	│  fetchHeaders (via PushGetHeadersMsg)
//	│      ↓
//	│  handleHeadersMsg
//	│      ↓
//	│  fetchHeaderBlocks ←┐
//	│      ↓              │ (refill)
//	│  handleBlockMsg ────┘──→ IBD complete
//	│
//	│  (stall detected at any phase above)
//	│      ↓
//	│  handleStallSample
//	│      ↓
//	└── handleDonePeerMsg
//
// It verifies that header processing transitions to block download, that the
// pipeline refill path in handleBlockMsg is exercised, and that headersFirstMode
// is properly cleared once the chain catches up to the best header.
//
// The "fresh ibd" case tests a complete sync from genesis: headers are fetched
// and then blocks are downloaded.
//
// The "stall before any headers" and "stall mid header download" cases test
// recovery when the sync peer stalls during header download.  A replacement
// peer delivers the remaining (or all) headers and then all blocks.
//
// The "headers complete peer stalls on blocks" case tests recovery when the
// sync peer delivers all headers but stalls before sending any blocks; a
// replacement peer downloads all blocks.
//
// The "stalled sync peer recovery" case tests recovery mid-block-download: a
// sync peer stops responding after some blocks, handleStallSample detects the
// inactivity, the stalled peer is disconnected, and a replacement peer
// finishes IBD.
//
// The "stall mid headers then stall on blocks" case combines both failure
// modes: one peer stalls during headers (peer 2 takes over and finishes
// headers), then peer 2 stalls during block download (peer 3 finishes blocks).
// This exercises recovery across three distinct peers.
func TestSyncStateMachine(t *testing.T) {
	t.Parallel()

	const testTotalBlocks = 2 * minInFlightBlocks

	tests := []struct {
		name        string
		totalBlocks int

		// stallHeadersAfter, when >= 0, triggers a stall during
		// header download: deliver this many headers, then stall
		// the sync peer and verify a replacement finishes header
		// download.  Set to -1 for no header stall.
		stallHeadersAfter int

		// stallAfter, when >= 0, triggers a stall during block
		// download: deliver all headers, then process this many
		// blocks before stalling.  Set to -1 for no block stall.
		stallAfter int
	}{
		{
			name:              "fresh ibd",
			totalBlocks:       testTotalBlocks,
			stallHeadersAfter: -1,
			stallAfter:        -1,
		},
		{
			name:              "stall before any headers",
			totalBlocks:       testTotalBlocks,
			stallHeadersAfter: 0,
			stallAfter:        -1,
		},
		{
			name:              "stall mid header download",
			totalBlocks:       testTotalBlocks,
			stallHeadersAfter: testTotalBlocks / 2,
			stallAfter:        -1,
		},
		{
			name:              "headers complete peer stalls on blocks",
			totalBlocks:       testTotalBlocks,
			stallHeadersAfter: -1,
			stallAfter:        0,
		},
		{
			name:              "stalled sync peer recovery",
			totalBlocks:       testTotalBlocks,
			stallHeadersAfter: -1,
			stallAfter:        5,
		},
		{
			name:              "stall mid headers then stall on blocks",
			totalBlocks:       testTotalBlocks,
			stallHeadersAfter: testTotalBlocks / 2,
			stallAfter:        5,
		},
	}

	for _, tc := range tests {
		tc := tc // capture range variable for parallel subtests
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			params := chaincfg.RegressionNetParams
			params.Checkpoints = nil

			sm, tearDown := makeMockSyncManager(t, &params)
			defer tearDown()

			blocks := generateTestBlocks(t, &params, tc.totalBlocks)

			// Register a sync candidate and call startSync,
			// which activates headersFirstMode and sends getheaders.
			peer1 := startIBD(t, sm, tc.totalBlocks)

			if tc.stallHeadersAfter >= 0 {
				// Stall during header download;
				// replacement sends remaining headers.
				peer2 := newSyncCandidate(t, sm,
					int32(tc.totalBlocks))
				syncStalledHeaderRecovery(
					t, sm, peer1, peer2,
					blocks, tc.stallHeadersAfter,
					tc.totalBlocks,
				)

				if tc.stallAfter >= 0 {
					peer3 := newSyncCandidate(t, sm,
						int32(tc.totalBlocks))
					syncStalledPeerRecovery(
						t, sm, peer2,
						peer3, blocks,
						tc.stallAfter,
						tc.totalBlocks,
					)
				} else {
					syncProcessBlocks(t, sm,
						peer2, blocks,
						tc.totalBlocks)
				}
			} else {
				syncSendHeaders(t, sm, peer1,
					blocks, tc.totalBlocks)

				if tc.stallAfter >= 0 {
					peer2 := newSyncCandidate(t, sm,
						int32(tc.totalBlocks))
					syncStalledPeerRecovery(
						t, sm, peer1,
						peer2, blocks,
						tc.stallAfter,
						tc.totalBlocks,
					)
				} else {
					syncProcessBlocks(t, sm,
						peer1, blocks,
						tc.totalBlocks)
				}
			}
		})
	}
}

// newSyncCandidate creates and registers a sync-candidate peer at the
// given height without triggering startSync.
func newSyncCandidate(t *testing.T, sm *SyncManager,
	height int32) *peer.Peer {

	t.Helper()

	p := peer.NewInboundPeer(&peer.Config{
		ChainParams: sm.chainParams,
	})
	p.UpdateLastBlockHeight(height)
	sm.peerStates[p] = &peerSyncState{
		syncCandidate:             true,
		requestedTxns:             make(map[chainhash.Hash]struct{}),
		requestedBlocks:           make(map[chainhash.Hash]struct{}),
		requestedUtreexoSummaries: make(map[chainhash.Hash]struct{}),
		requestedUtreexoProofs:    make(map[chainhash.Hash]struct{}),
		requestedUtreexoTTLs:      make(map[wire.MsgGetUtreexoTTLs]struct{}),
	}
	return p
}

// assertIBDComplete verifies that IBD finished: chain height matches
// totalBlocks, headersFirstMode is off, and no blocks remain in-flight.
func assertIBDComplete(t *testing.T, sm *SyncManager,
	peerState *peerSyncState, totalBlocks int) {

	t.Helper()

	best := sm.chain.BestSnapshot()
	require.Equal(t, int32(totalBlocks), best.Height)
	require.False(t, sm.headersFirstMode,
		"headersFirstMode should be off after catching up")
	require.Empty(t, peerState.requestedBlocks,
		"peer should have no outstanding block requests")
}

// startIBD registers a sync peer and calls startSync, verifying that
// headersFirstMode is activated and the peer is selected.
func startIBD(t *testing.T, sm *SyncManager,
	peerHeight int) *peer.Peer {

	t.Helper()

	syncPeer := newSyncCandidate(t, sm, int32(peerHeight))

	sm.startSync()

	require.True(t, sm.syncPeer == syncPeer,
		"syncPeer should be set after startSync")
	require.True(t, sm.headersFirstMode, "headersFirstMode should be on")
	require.False(t, sm.lastProgressTime.IsZero(),
		"lastProgressTime should be set")

	return syncPeer
}

// syncSendHeaders delivers block headers to the sync manager and verifies
// that block requests are generated.
func syncSendHeaders(t *testing.T, sm *SyncManager,
	syncPeer *peer.Peer, blocks []*btcutil.Block, totalBlocks int) {

	t.Helper()

	// Record the progress time set by startIBD so we can verify
	// that handleHeadersMsg advances it.
	progressBefore := sm.lastProgressTime

	headers := wire.NewMsgHeaders()
	for _, block := range blocks {
		err := headers.AddBlockHeader(&block.MsgBlock().Header)
		require.NoError(t, err)
	}

	sm.handleHeadersMsg(&headersMsg{
		headers: headers,
		peer:    syncPeer,
	})

	_, bestHeaderHeight := sm.chain.BestHeader()
	require.Equal(t, int32(totalBlocks), bestHeaderHeight)

	require.True(t, sm.lastProgressTime.After(progressBefore),
		"handleHeadersMsg should update lastProgressTime")

	// In utreexod, fetchHeaderBlocks populates peerState.requestedBlocks
	// (not sm.requestedBlocks) during headersFirstMode.
	wantRequested := make(map[chainhash.Hash]struct{}, len(blocks))
	for _, block := range blocks {
		wantRequested[*block.Hash()] = struct{}{}
	}
	require.Equal(t, wantRequested, sm.peerStates[syncPeer].requestedBlocks)
}

// syncProcessBlocks feeds all blocks to handleBlockMsg and verifies that
// headersFirstMode remains active until the final block, at which point
// IBD completes.
func syncProcessBlocks(t *testing.T, sm *SyncManager, syncPeer *peer.Peer,
	blocks []*btcutil.Block, totalBlocks int) {

	t.Helper()

	peerState := sm.peerStates[syncPeer]

	for i, block := range blocks {
		sm.handleBlockMsg(&blockMsg{
			block: block,
			peer:  syncPeer,
			reply: make(chan struct{}, 1),
		})

		if i < len(blocks)-1 {
			require.True(t, sm.headersFirstMode,
				"headersFirstMode should still be on at height %d", i+1)
		}
	}

	assertIBDComplete(t, sm, peerState, totalBlocks)
}

// syncStalledPeerRecovery processes stallAfter blocks from stalledPeer,
// triggers stall detection, verifies that stalledPeer is removed and
// replacementPeer takes over, then feeds remaining blocks and verifies
// IBD completes.
func syncStalledPeerRecovery(t *testing.T, sm *SyncManager,
	stalledPeer, replacementPeer *peer.Peer,
	blocks []*btcutil.Block, stallAfter, totalBlocks int) {

	t.Helper()

	// Process the first stallAfter blocks from the stalled peer.
	for _, block := range blocks[:stallAfter] {
		sm.handleBlockMsg(&blockMsg{
			block: block,
			peer:  stalledPeer,
			reply: make(chan struct{}, 1),
		})
	}

	best := sm.chain.BestSnapshot()
	require.Equal(t, int32(stallAfter), best.Height)
	require.True(t, sm.headersFirstMode)

	// Trigger stall detection.
	sm.lastProgressTime = time.Now().Add(
		-(maxStallDuration + time.Minute))
	sm.handleStallSample()

	// Verify that handleStallSample called Disconnect() on the
	// stalled peer (which closes p.quit, making WaitForDisconnect
	// return immediately).
	disconnected := make(chan struct{})
	go func() {
		stalledPeer.WaitForDisconnect()
		close(disconnected)
	}()
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("Disconnect() was not called on stalled peer")
	}

	// Snapshot the stalled peer's outstanding requested blocks before
	// disconnection so we can verify they are re-requested.
	stalledState := sm.peerStates[stalledPeer]
	stalledRequested := make([]chainhash.Hash, 0, len(stalledState.requestedBlocks))
	for hash := range stalledState.requestedBlocks {
		stalledRequested = append(stalledRequested, hash)
	}
	require.NotEmpty(t, stalledRequested,
		"stalled peer should have outstanding requested blocks")

	// In utreexod, handleStallSample calls updateSyncPeer which
	// calls startSync internally.  Then handleDonePeerMsg cleans
	// up the stalled peer's state.
	sm.handleDonePeerMsg(stalledPeer)

	_, stalledTracked := sm.peerStates[stalledPeer]
	require.False(t, stalledTracked,
		"stalled peer should be removed")
	require.True(t, sm.syncPeer == replacementPeer,
		"replacement peer should take over as sync peer")
	require.True(t, sm.headersFirstMode)

	// Verify that the replacement peer re-requested the exact same
	// blocks that were outstanding from the stalled peer.
	replacementState := sm.peerStates[replacementPeer]
	require.Equal(t, len(stalledRequested),
		len(replacementState.requestedBlocks),
		"replacement peer should request same number of blocks")
	for _, hash := range stalledRequested {
		_, exists := replacementState.requestedBlocks[hash]
		require.True(t, exists,
			"block %v should be requested from replacement peer",
			hash)
	}

	// Feed remaining blocks from the replacement peer.
	for _, block := range blocks[stallAfter:] {
		sm.handleBlockMsg(&blockMsg{
			block: block,
			peer:  replacementPeer,
			reply: make(chan struct{}, 1),
		})
	}

	assertIBDComplete(t, sm, replacementState, totalBlocks)
}

// syncStalledHeaderRecovery simulates a stall during header download.
// It optionally delivers headersSent headers from stalledPeer, triggers stall
// detection, verifies that stalledPeer is removed and replacementPeer takes
// over, then delivers remaining headers and verifies block requests are
// generated.  The caller is responsible for the block-download phase.
func syncStalledHeaderRecovery(t *testing.T, sm *SyncManager,
	stalledPeer, replacementPeer *peer.Peer,
	blocks []*btcutil.Block, headersSent, totalBlocks int) {

	t.Helper()

	// Deliver partial headers from the stalled peer.  When
	// headersSent is 0, this is a no-op (peer stalls immediately).
	if headersSent > 0 {
		headers := wire.NewMsgHeaders()
		for _, block := range blocks[:headersSent] {
			err := headers.AddBlockHeader(
				&block.MsgBlock().Header)
			require.NoError(t, err)
		}

		sm.handleHeadersMsg(&headersMsg{
			headers: headers,
			peer:    stalledPeer,
		})

		_, bestHeaderHeight := sm.chain.BestHeader()
		require.Equal(t, int32(headersSent), bestHeaderHeight)
	}

	// No blocks should have been requested during header download
	// since the headers haven't caught up to the peer's height yet.
	for p, state := range sm.peerStates {
		require.Empty(t, state.requestedBlocks,
			"peer %v should have no requested blocks during header download", p)
	}

	// Trigger stall detection.
	sm.lastProgressTime = time.Now().Add(
		-(maxStallDuration + time.Minute))
	sm.handleStallSample()

	// Verify that handleStallSample called Disconnect() on the
	// stalled peer.
	disconnected := make(chan struct{})
	go func() {
		stalledPeer.WaitForDisconnect()
		close(disconnected)
	}()
	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("Disconnect() was not called on stalled peer")
	}

	// Complete peer removal.  handleDonePeerMsg clears state and
	// triggers startSync which selects the replacement peer.
	sm.handleDonePeerMsg(stalledPeer)

	_, stalledTracked := sm.peerStates[stalledPeer]
	require.False(t, stalledTracked,
		"stalled peer should be removed")
	require.True(t, sm.syncPeer == replacementPeer,
		"replacement peer should take over as sync peer")
	require.True(t, sm.headersFirstMode)

	// Deliver remaining headers from the replacement peer.  When
	// headersSent is 0, this is all headers.
	remainingHeaders := wire.NewMsgHeaders()
	for _, block := range blocks[headersSent:] {
		err := remainingHeaders.AddBlockHeader(
			&block.MsgBlock().Header)
		require.NoError(t, err)
	}
	sm.handleHeadersMsg(&headersMsg{
		headers: remainingHeaders,
		peer:    replacementPeer,
	})

	_, bestHeaderHeight := sm.chain.BestHeader()
	require.Equal(t, int32(totalBlocks), bestHeaderHeight)

	// Verify all blocks were requested from the replacement peer.
	wantRequested := make(map[chainhash.Hash]struct{}, len(blocks))
	for _, block := range blocks {
		wantRequested[*block.Hash()] = struct{}{}
	}
	replacementState := sm.peerStates[replacementPeer]
	require.Equal(t, wantRequested, replacementState.requestedBlocks)
}

// TestStartSyncBlockFallback verifies the startSync fallback path where
// headers are already caught up but the block chain lags behind.  In this
// case startSync should skip header download and directly request blocks.
func TestStartSyncBlockFallback(t *testing.T) {
	t.Parallel()

	params := chaincfg.RegressionNetParams
	params.Checkpoints = nil

	sm, tearDown := makeMockSyncManager(t, &params)
	defer tearDown()

	// Process headers so the header chain is at numBlocks while the
	// block chain stays at genesis.
	const numBlocks = 11
	blocks := generateTestBlocks(t, &params, numBlocks)
	for _, block := range blocks {
		_, err := sm.chain.ProcessBlockHeader(
			&block.MsgBlock().Header, blockchain.BFNone)
		require.NoError(t, err)
	}

	// Add a peer whose height equals the header height.
	syncPeer := peer.NewInboundPeer(&peer.Config{})
	syncPeer.UpdateLastBlockHeight(int32(numBlocks))
	sm.peerStates[syncPeer] = &peerSyncState{
		syncCandidate:             true,
		requestedTxns:             make(map[chainhash.Hash]struct{}),
		requestedBlocks:           make(map[chainhash.Hash]struct{}),
		requestedUtreexoSummaries: make(map[chainhash.Hash]struct{}),
		requestedUtreexoProofs:    make(map[chainhash.Hash]struct{}),
		requestedUtreexoTTLs:      make(map[wire.MsgGetUtreexoTTLs]struct{}),
	}

	sm.startSync()

	require.NotNil(t, sm.syncPeer,
		"sync peer should be set for block download")
	// In utreexod, fetchHeaderBlocks populates peerState.requestedBlocks.
	peerState := sm.peerStates[syncPeer]
	require.NotEmpty(t, peerState.requestedBlocks,
		"blocks should be requested via fetchHeaderBlocks")
}

// TestStallNoDisconnectAtSameHeight verifies that handleStallSample does
// not disconnect a sync peer whose advertised height equals our own.
func TestStallNoDisconnectAtSameHeight(t *testing.T) {
	t.Parallel()

	params := chaincfg.RegressionNetParams
	params.Checkpoints = nil

	sm, tearDown := makeMockSyncManager(t, &params)
	defer tearDown()

	p := peer.NewInboundPeer(&peer.Config{})
	p.UpdateLastBlockHeight(0) // Same height as our genesis chain.
	sm.peerStates[p] = &peerSyncState{}
	sm.syncPeer = p
	sm.headersFirstMode = true
	sm.lastProgressTime = time.Now().Add(
		-(maxStallDuration + time.Minute))

	sm.handleStallSample()

	_, tracked := sm.peerStates[p]
	require.True(t, tracked,
		"peer at same height should not be disconnected")
	require.Nil(t, sm.syncPeer,
		"we should have nil syncPeer after handleStallSample")
}

// TestFetchHeaderBlocksNoDuplicateRequests verifies that when multiple peers
// announce the same block headers while not in headers-first mode (i.e., at the
// chain tip), blocks are only requested from the first announcing peer.
//
// Without this deduplication via the global sm.requestedBlocks map, every
// announcing peer triggers a redundant block download, resulting in wasted
// bandwidth and "already have block" rejections for all but the first response.
func TestFetchHeaderBlocksNoDuplicateRequests(t *testing.T) {
	t.Parallel()

	params := chaincfg.RegressionNetParams
	params.Checkpoints = nil

	sm, tearDown := makeMockSyncManager(t, &params)
	defer tearDown()

	// Generate test blocks.
	const numBlocks = 3
	blocks := generateTestBlocks(t, &params, numBlocks)

	// Simulate the node being caught up at the chain tip (not in
	// headers-first mode).  This is the state where the bug manifests:
	// multiple peers announce a new block via headers, and each
	// announcement triggers fetchHeaderBlocks for that peer.
	sm.headersFirstMode = false

	// Create multiple peers that will each announce the same headers.
	peer1 := newSyncCandidate(t, sm, int32(numBlocks))
	peer2 := newSyncCandidate(t, sm, int32(numBlocks))
	peer3 := newSyncCandidate(t, sm, int32(numBlocks))

	// Build a headers message containing the new blocks.
	headers := wire.NewMsgHeaders()
	for _, block := range blocks {
		err := headers.AddBlockHeader(&block.MsgBlock().Header)
		require.NoError(t, err)
	}

	// Peer 1 sends headers — fetchHeaderBlocks should request the
	// blocks from peer1.
	sm.handleHeadersMsg(&headersMsg{
		headers: headers,
		peer:    peer1,
	})

	peer1State := sm.peerStates[peer1]
	wantRequested := make(map[chainhash.Hash]struct{}, numBlocks)
	for _, block := range blocks {
		wantRequested[*block.Hash()] = struct{}{}
	}
	require.Equal(t, wantRequested, peer1State.requestedBlocks,
		"peer1 should have requested all blocks")

	// Verify global requestedBlocks is populated.
	require.Equal(t, wantRequested, sm.requestedBlocks,
		"global requestedBlocks should track peer1's requests")

	// Peer 2 sends the same headers — fetchHeaderBlocks should NOT
	// request the same blocks because they are already tracked in
	// the global requestedBlocks map.
	sm.handleHeadersMsg(&headersMsg{
		headers: headers,
		peer:    peer2,
	})

	peer2State := sm.peerStates[peer2]
	require.Empty(t, peer2State.requestedBlocks,
		"peer2 should NOT request blocks already requested from peer1")

	// Peer 3 sends the same headers — same expectation.
	sm.handleHeadersMsg(&headersMsg{
		headers: headers,
		peer:    peer3,
	})

	peer3State := sm.peerStates[peer3]
	require.Empty(t, peer3State.requestedBlocks,
		"peer3 should NOT request blocks already requested from peer1")
}

// TestStartSyncChainCurrent verifies that startSync does not set syncPeer
// or headersFirstMode when the chain is current and no peer is strictly higher.
// IsCurrent()==true with no higher peers causes startSync to exit immediately.
func TestStartSyncChainCurrent(t *testing.T) {
	t.Parallel()

	params := chaincfg.RegressionNetParams
	params.Checkpoints = nil

	sm, tearDown := makeMockSyncManager(t, &params)
	defer tearDown()

	// Mine a single block with a recent timestamp so
	// IsCurrent() returns true.
	cb := createTestCoinbase(1, &params)
	header := wire.BlockHeader{
		Version:    1,
		PrevBlock:  *params.GenesisHash,
		MerkleRoot: cb.TxHash(),
		Timestamp:  time.Now().Truncate(time.Second),
		Bits:       params.PowLimitBits,
	}
	require.True(t, solveTestBlock(&header, &params))

	block := btcutil.NewBlock(&wire.MsgBlock{
		Header:       header,
		Transactions: []*wire.MsgTx{cb},
	})
	_, _, err := sm.chain.ProcessBlock(block, blockchain.BFNone)
	require.NoError(t, err)
	require.True(t, sm.chain.IsCurrent())

	// Peer at our height — not higher.
	newSyncCandidate(t, sm, 1)

	sm.startSync()

	require.Nil(t, sm.syncPeer,
		"syncPeer should not be set when chain is already current")
	require.False(t, sm.headersFirstMode,
		"headersFirstMode should not be activated when chain is already current")
}

// TestBlockConnectedCleansMempool verifies that the NTBlockConnected
// notification always removes double-spending transactions from the mempool,
// even when the node is not current (e.g. during IBD).
//
// This is a regression test for a bug where the entire NTBlockConnected handler
// was gated behind sm.current(), causing mempool cleanup to be skipped during
// initial sync.  When a reorg later disconnected such a block, the stale
// mempool entries would conflict with re-added block transactions, triggering
// an RBF code path that crashed with a nil pointer dereference.
func TestBlockConnectedCleansMempool(t *testing.T) {
	t.Parallel()

	params := chaincfg.RegressionNetParams
	params.Checkpoints = nil

	chain, tearDown, err := chainSetup(t, &params)
	require.NoError(t, err)
	defer tearDown()

	txPool := mempool.New(&mempool.Config{
		Policy: mempool.Policy{
			DisableRelayPriority: true,
			FreeTxRelayLimit:     15.0,
			MaxOrphanTxs:         5,
			MaxOrphanTxSize:      1000,
			MaxSigOpCostPerTx:    blockchain.MaxBlockSigOpsCost / 4,
			MinRelayTxFee:        1000,
			MaxTxVersion:         2,
			AcceptNonStd:         true,
		},
		ChainParams:   &params,
		FetchUtxoView: chain.FetchUtxoView,
		BestHeight:    func() int32 { return chain.BestSnapshot().Height },
		MedianTimePast: func() time.Time {
			return chain.BestSnapshot().MedianTime
		},
		CalcSequenceLock: func(tx *btcutil.Tx, view *blockchain.UtxoViewpoint) (*blockchain.SequenceLock, error) {
			return chain.CalcSequenceLock(tx, view, true)
		},
		IsDeploymentActive:  chain.IsDeploymentActive,
		IsUtreexoViewActive: func() bool { return false },
		SigCache:            txscript.NewSigCache(1000),
	})

	sm, err := New(&Config{
		PeerNotifier: noopPeerNotifier{},
		Chain:        chain,
		TxMemPool:    txPool,
		ChainParams:  &params,
	})
	require.NoError(t, err)

	// Mine enough blocks to mature the first coinbase.
	blocks := generateTestBlocks(t, &params, int(params.CoinbaseMaturity)+1)
	for _, blk := range blocks {
		_, _, err := chain.ProcessBlock(blk, blockchain.BFNone)
		require.NoError(t, err)
	}

	// Grab the mature coinbase outpoint.
	cbOutpoint := wire.OutPoint{
		Hash:  *blocks[0].Transactions()[0].Hash(),
		Index: 0,
	}

	// Create a tx spending the coinbase and add it to the mempool.
	mempoolSpend := wire.NewMsgTx(1)
	mempoolSpend.AddTxIn(&wire.TxIn{
		PreviousOutPoint: cbOutpoint,
		Sequence:         wire.MaxTxInSequenceNum,
	})
	mempoolSpend.AddTxOut(&wire.TxOut{
		Value:    100_000_000,
		PkScript: []byte{txscript.OP_TRUE},
	})
	mempoolTx := btcutil.NewTx(mempoolSpend)
	_, err = txPool.ProcessTransaction(mempoolTx, nil, false, false, 0)
	require.NoError(t, err)
	require.True(t, txPool.HaveTransaction(mempoolTx.Hash()))

	// Make the node NOT current by adding a peer at a much higher height.
	newSyncCandidate(t, sm, 900_000)
	require.False(t, sm.current())

	// Build and connect a block containing a DIFFERENT tx that spends the
	// same coinbase (a double-spend).  ProcessBlock fires NTBlockConnected
	// synchronously, which calls handleBlockchainNotification.
	blockSpend := wire.NewMsgTx(1)
	blockSpend.AddTxIn(&wire.TxIn{
		PreviousOutPoint: cbOutpoint,
		Sequence:         wire.MaxTxInSequenceNum,
	})
	blockSpend.AddTxOut(&wire.TxOut{
		Value:    90_000_000,
		PkScript: []byte{txscript.OP_TRUE},
	})

	snap := chain.BestSnapshot()
	cb := createTestCoinbase(snap.Height+1, &params)
	blockTxns := []*wire.MsgTx{cb, blockSpend}
	merkleRoot := blockchain.CalcMerkleRoot(
		[]*btcutil.Tx{btcutil.NewTx(cb), btcutil.NewTx(blockSpend)},
		false,
	)
	header := wire.BlockHeader{
		Version:    1,
		PrevBlock:  snap.Hash,
		MerkleRoot: merkleRoot,
		Timestamp:  blocks[len(blocks)-1].MsgBlock().Header.Timestamp.Add(time.Minute),
		Bits:       params.PowLimitBits,
	}
	require.True(t, solveTestBlock(&header, &params))
	_, _, err = chain.ProcessBlock(
		btcutil.NewBlock(&wire.MsgBlock{Header: header, Transactions: blockTxns}),
		blockchain.BFNone,
	)
	require.NoError(t, err)

	// The mempool tx must have been evicted, even though we were not current.
	require.False(t, txPool.HaveTransaction(mempoolTx.Hash()),
		"mempool should have removed the double-spending tx on block connect")
}

// pendingEntry returns the pending block for h, creating it if needed, so a
// test can seed either half.
func pendingEntry(sm *SyncManager, h chainhash.Hash) *pendingBlock {
	pb := sm.pending[h]
	if pb == nil {
		pb = &pendingBlock{}
		sm.pending[h] = pb
	}
	return pb
}

// queueBlock parks a block half in sm.pending under the block's hash so the
// test can simulate a partial delivery from a previous sync peer.
func queueBlock(sm *SyncManager, p *peer.Peer, b *btcutil.Block) {
	pb := pendingEntry(sm, *b.Hash())
	pb.block = b
	pb.blockPeer = p
}

// queueProof parks a synthetic proof half in sm.pending.
func queueProof(sm *SyncManager, p *peer.Peer, h chainhash.Hash) {
	pb := pendingEntry(sm, h)
	pb.proof = &wire.MsgUtreexoProof{BlockHash: h}
	pb.proofPeer = p
}

// deliverProof drives an empty proof for h through handleUtreexoProofMsg
// as if peer p had just responded to a MsgGetUtreexoProof.
func deliverProof(sm *SyncManager, p *peer.Peer, h chainhash.Hash) {
	sm.handleUtreexoProofMsg(&utreexoProofMsg{
		proof: &wire.MsgUtreexoProof{BlockHash: h},
		peer:  p,
	})
}

// deliverBlock drives b through handleBlockMsg as if peer p had just
// responded to a getdata.
func deliverBlock(sm *SyncManager, p *peer.Peer, b *btcutil.Block) {
	sm.handleBlockMsg(&blockMsg{
		block: b, peer: p, reply: make(chan struct{}, 1),
	})
}

// TestFetchHeaderBlocksReusesQueuedData covers the path where a sync peer
// disconnects mid-IBD with blocks and/or proofs already in the queues. The
// next sync peer should not re-download what we already have.
//
// Each sub-test exercises one of the four reuse cases: both halves queued,
// block only, proof only, or neither. The sub-test asserts fetchHeaderBlocks's
// request decisions and then drives the case through to chain advancement so
// the reused payloads are shown to validate.
func TestFetchHeaderBlocksReusesQueuedData(t *testing.T) {
	t.Parallel()

	params := chaincfg.RegressionNetParams
	params.Checkpoints = nil

	cases := []struct {
		name      string
		seedBlock bool
		seedProof bool
	}{
		{name: "both halves queued", seedBlock: true, seedProof: true},
		{name: "only block queued", seedBlock: true, seedProof: false},
		{name: "only proof queued", seedBlock: false, seedProof: true},
		{name: "neither queued", seedBlock: false, seedProof: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			sm, tearDown := makeMockSyncManagerUtreexo(t, &params)
			defer tearDown()

			block := generateTestBlocks(t, &params, 1)[0]
			hash := *block.Hash()

			// Run outside headers-first mode so handleHeadersMsg
			// drives fetchHeaderBlocks without needing
			// sm.syncPeer.LastBlock().
			sm.headersFirstMode = false

			// Seed peer1's partial delivery, then disconnect.
			peer1 := newSyncCandidate(t, sm, 1)
			if c.seedBlock {
				queueBlock(sm, peer1, block)
			}
			if c.seedProof {
				queueProof(sm, peer1, hash)
			}
			sm.handleDonePeerMsg(peer1)

			// peer2 announces the header; fetchHeaderBlocks should
			// decide what to reuse vs ask for.
			peer2 := newSyncCandidate(t, sm, 1)
			headers := wire.NewMsgHeaders()
			require.NoError(t, headers.AddBlockHeader(
				&block.MsgBlock().Header))
			sm.handleHeadersMsg(&headersMsg{
				headers: headers,
				peer:    peer2,
			})
			peer2State := sm.peerStates[peer2]

			if c.seedBlock {
				require.NotContains(t, peer2State.requestedBlocks, hash,
					"queued block should not be marked requested")
			} else {
				require.Contains(t, peer2State.requestedBlocks, hash,
					"block should be marked requested for peer2")
			}
			if c.seedProof {
				require.NotContains(t,
					peer2State.requestedUtreexoProofs, hash,
					"proof should not be re-requested")
			} else {
				require.Contains(t,
					peer2State.requestedUtreexoProofs, hash,
					"proof should be re-requested")
			}

			// Drive the case through to chain advancement.
			switch {
			case c.seedBlock && c.seedProof:
				// The test pre-seeded both halves without going through
				// the normal block/proof handlers, so drain explicitly.
				sm.connectReadyBlocks()
			case c.seedBlock:
				deliverProof(sm, peer2, hash)
			case c.seedProof:
				deliverBlock(sm, peer2, block)
			default:
				deliverProof(sm, peer2, hash)
				deliverBlock(sm, peer2, block)
			}

			best := sm.chain.BestSnapshot()
			require.Equal(t, int32(1), best.Height,
				"chain should advance to height 1")
			require.Equal(t, hash, best.Hash,
				"chain tip should be the test block's hash")
			require.NotContains(t, sm.pending, hash,
				"pending must be cleared after success")
		})
	}
}

// setupCSNChain builds a utreexo sync manager, a sync peer, and n coinbase-only
// blocks with their headers already known to the chain. Every block and proof is
// marked requested on the peer so the delivery handlers accept them.
// headersFirstMode is left off so deliveries drive the pending pool directly
// without triggering refills.
func setupCSNChain(t *testing.T, n int) (
	*SyncManager, func(), *peer.Peer, []*btcutil.Block) {

	t.Helper()

	params := chaincfg.RegressionNetParams
	params.Checkpoints = nil

	sm, tearDown := makeMockSyncManagerUtreexo(t, &params)

	blocks := generateTestBlocks(t, &params, n)
	for _, b := range blocks {
		_, err := sm.chain.ProcessBlockHeader(
			&b.MsgBlock().Header, blockchain.BFNone)
		require.NoError(t, err)
	}

	p := newSyncCandidate(t, sm, int32(n))
	sm.syncPeer = p
	st := sm.peerStates[p]
	for _, b := range blocks {
		st.requestedBlocks[*b.Hash()] = struct{}{}
		st.requestedUtreexoProofs[*b.Hash()] = struct{}{}
	}

	return sm, tearDown, p, blocks
}

// TestCSNAssemblyOrdering proves that block and proof halves assemble and connect
// in chain order regardless of the order in which the two messages arrive.
func TestCSNAssemblyOrdering(t *testing.T) {
	t.Parallel()

	const n = 4

	// ev is a single delivery: a block half or a proof half for blocks[i].
	type ev struct {
		proof bool
		i     int
	}

	strict := func() []ev {
		s := make([]ev, 0, 2*n)
		for i := 0; i < n; i++ {
			s = append(s, ev{false, i}, ev{true, i})
		}
		return s
	}
	proofFirst := func() []ev {
		s := make([]ev, 0, 2*n)
		for i := 0; i < n; i++ {
			s = append(s, ev{true, i}, ev{false, i})
		}
		return s
	}
	allBlocksThenProofs := func() []ev {
		s := make([]ev, 0, 2*n)
		for i := 0; i < n; i++ {
			s = append(s, ev{false, i})
		}
		for i := 0; i < n; i++ {
			s = append(s, ev{true, i})
		}
		return s
	}
	allProofsThenBlocks := func() []ev {
		s := make([]ev, 0, 2*n)
		for i := 0; i < n; i++ {
			s = append(s, ev{true, i})
		}
		for i := 0; i < n; i++ {
			s = append(s, ev{false, i})
		}
		return s
	}
	shuffled := func() []ev {
		return []ev{
			{false, 2}, {false, 0}, {true, 1}, {false, 1},
			{true, 0}, {true, 2}, {false, 3}, {true, 3},
		}
	}

	cases := []struct {
		name  string
		order func() []ev
	}{
		{"strict", strict},
		{"proof first per block", proofFirst},
		{"all blocks then all proofs", allBlocksThenProofs},
		{"all proofs then all blocks", allProofsThenBlocks},
		{"shuffled", shuffled},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			sm, tearDown, p, blocks := setupCSNChain(t, n)
			defer tearDown()

			prevHeight := int32(0)
			for _, e := range c.order() {
				if e.proof {
					deliverProof(sm, p, *blocks[e.i].Hash())
				} else {
					deliverBlock(sm, p, blocks[e.i])
				}

				// The tip must never skip a height: it either stays
				// put or advances one block at a time.
				h := sm.chain.BestSnapshot().Height
				require.GreaterOrEqual(t, h, prevHeight)
				prevHeight = h
			}

			require.Equal(t, int32(n), sm.chain.BestSnapshot().Height,
				"chain should reach the tip for order %q", c.name)
			require.Empty(t, sm.pending,
				"pending should be empty for order %q", c.name)
		})
	}
}

// TestCSNSilentProofDrop is the primary regression: a single proof never arrives,
// yet every block does. The pipeline must not wedge, and delivering the missing
// proof must let the chain catch up.
func TestCSNSilentProofDrop(t *testing.T) {
	t.Parallel()

	const n = 5
	const gap = 2 // index of the block whose proof is dropped

	sm, tearDown, p, blocks := setupCSNChain(t, n)
	defer tearDown()
	st := sm.peerStates[p]

	for _, b := range blocks {
		deliverBlock(sm, p, b)
	}
	for i, b := range blocks {
		if i == gap {
			continue
		}
		deliverProof(sm, p, *b.Hash())
	}

	require.Equal(t, int32(gap), sm.chain.BestSnapshot().Height,
		"tip should stop just below the missing proof")
	require.Empty(t, st.requestedBlocks,
		"every block arrived so none should remain outstanding")
	require.Contains(t, st.requestedUtreexoProofs, *blocks[gap].Hash(),
		"the dropped proof should still be outstanding for re-request")
	require.NotEmpty(t, sm.pending,
		"downstream halves should remain pending")

	// Delivering the missing proof unblocks the whole downstream chain.
	deliverProof(sm, p, *blocks[gap].Hash())

	require.Equal(t, int32(n), sm.chain.BestSnapshot().Height,
		"chain should catch up once the gap is filled")
	require.Empty(t, sm.pending, "pending should drain")
}

// TestCSNMissingBlock is the mirror of the proof-drop case: every proof arrives
// but one block lags, then arrives and unblocks the chain.
func TestCSNMissingBlock(t *testing.T) {
	t.Parallel()

	const n = 5
	const gap = 2

	sm, tearDown, p, blocks := setupCSNChain(t, n)
	defer tearDown()

	for _, b := range blocks {
		deliverProof(sm, p, *b.Hash())
	}
	for i, b := range blocks {
		if i == gap {
			continue
		}
		deliverBlock(sm, p, b)
	}

	require.Equal(t, int32(gap), sm.chain.BestSnapshot().Height,
		"tip should stop just below the missing block")

	deliverBlock(sm, p, blocks[gap])

	require.Equal(t, int32(n), sm.chain.BestSnapshot().Height,
		"chain should catch up once the block arrives")
	require.Empty(t, sm.pending, "pending should drain")
}

// TestCSNDuplicateBlockHalf proves a block delivered twice connects once and the
// unrequested second copy (which has no proof) is handled without panicking.
func TestCSNDuplicateBlockHalf(t *testing.T) {
	t.Parallel()

	sm, tearDown, p, blocks := setupCSNChain(t, 1)
	defer tearDown()
	block := blocks[0]
	hash := *block.Hash()

	// First, requested copy parks the block half in pending.
	deliverBlock(sm, p, block)
	// Second copy is unrequested; in regtest it is fed straight to the chain
	// without a proof and must not connect or panic.
	deliverBlock(sm, p, block)

	require.Equal(t, int32(0), sm.chain.BestSnapshot().Height,
		"block must not connect without its proof")

	// The proof completes the originally parked half.
	deliverProof(sm, p, hash)
	require.Equal(t, int32(1), sm.chain.BestSnapshot().Height,
		"block connects once its proof arrives")
	require.Empty(t, sm.pending)
}

// TestCSNPendingCapStopsFetch verifies that fetchHeaderBlocks stops requesting
// once the block pipeline reaches maxPendingBlocks, bounding memory when proofs
// lag behind blocks.
func TestCSNPendingCapStopsFetch(t *testing.T) {
	// Not parallel: it temporarily lowers the package-level cap.
	defer func(orig int) { maxPendingBlocks = orig }(maxPendingBlocks)
	maxPendingBlocks = 3

	params := chaincfg.RegressionNetParams
	params.Checkpoints = nil
	sm, tearDown := makeMockSyncManagerUtreexo(t, &params)
	defer tearDown()

	const n = 6
	blocks := generateTestBlocks(t, &params, n)
	for _, b := range blocks {
		_, err := sm.chain.ProcessBlockHeader(
			&b.MsgBlock().Header, blockchain.BFNone)
		require.NoError(t, err)
	}

	p := newSyncCandidate(t, sm, n)
	sm.syncPeer = p
	sm.headersFirstMode = true

	sm.fetchHeaderBlocks(nil)

	st := sm.peerStates[p]
	require.Len(t, st.requestedBlocks, maxPendingBlocks,
		"fetch must request exactly up to the pending cap")
}
