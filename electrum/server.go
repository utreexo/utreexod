// Copyright (c) 2013-2017 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Copyright (c) 2022-2023 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package electrum

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcjson"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/mempool"
	wallet "github.com/utreexo/utreexod/wallet"
	"github.com/utreexo/utreexod/wire"
)

const (
	protocolMajor = 1
	protocolMinor = 4
	protocolPatch = 2

	minProtocolMajor = 1
	minProtocolMinor = 4
	minProtocolPatch = 1

	// idleTimeout is the duration of inactivity before we time out a peer.
	idleTimeout = 10 * time.Minute

	mempoolHistogramBinSize = 30_000
)

var (
	delim                      = byte('\n')
	serverName                 = "Utreexo Electrum Server 0.1.0"
	electrumProtocolVersion    = fmt.Sprintf("%d.%d.%d", protocolMajor, protocolMinor, protocolPatch)
	minElectrumProtocolVersion = fmt.Sprintf("%d.%d", minProtocolMajor, minProtocolMinor)
)

type commandHandler func(*ElectrumServer, *btcjson.Request, net.Conn, <-chan struct{}) (interface{}, error)

// rpcHandlers maps RPC command strings to appropriate handler functions.
// This is set by init because help references rpcHandlers and thus causes
// a dependency loop.
var rpcHandlers map[string]commandHandler
var rpcHandlersBeforeInit = map[string]commandHandler{
	"blockchain.block.header":            handleBlockchainBlockHeader,
	"blockchain.block.headers":           handleBlockchainBlockHeaders,
	"blockchain.estimatefee":             handleEstimateFee,
	"blockchain.headers.subscribe":       handleHeadersSubscribe,
	"blockchain.relayfee":                handleRelayFee,
	"blockchain.scripthash.get_balance":  handleScriptHashGetBalance,
	"blockchain.scripthash.get_history":  handleScriptHashGetHistory,
	"blockchain.scripthash.get_mempool":  handleScriptHashGetMempool,
	"blockchain.scripthash.listunspent":  handleListUnspent,
	"blockchain.scripthash.subscribe":    handleScriptHashSubscribe,
	"blockchain.scripthash.unsubscribe":  handleScriptHashUnsubscribe,
	"blockchain.transaction.broadcast":   handleTransactionBroadcast,
	"blockchain.transaction.get":         handleGetTransaction,
	"blockchain.transaction.get_merkle":  handleGetMerkle,
	"blockchain.transaction.id_from_pos": handleIDFromPos,
	"mempool.get_fee_histogram":          handleMempoolGetFeeHistogram,
	"server.add_peer":                    handleServerAddPeer,
	"server.banner":                      handleBanner,
	"server.donation_address":            handleDonationAddress,
	"server.features":                    handleServerFeatures,
	"server.peers.subscribe":             handleServerPeersSubscribe,
	"server.ping":                        handlePing,
	"server.version":                     handleVersion,
}

func handleBlockchainBlockHeader(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var height int32
	err := json.Unmarshal(cmd.Params[0], &height)
	if err != nil {
		// Try to unmarshal as a string.  If it still errors, then return the error.
		var heightStr string
		err := json.Unmarshal(cmd.Params[0], &heightStr)
		if err != nil {
			return nil, err
		}

		heightInt, err := strconv.Atoi(heightStr)
		if err != nil {
			return nil, err
		}

		height = int32(heightInt)
	}
	if height < 0 {
		return nil, fmt.Errorf("Got height %d. Expected height to be non-negative.", height)
	}

	blockhash, err := s.cfg.BlockChain.BlockHashByHeight(height)
	if err != nil {
		return nil, err
	}

	header, err := s.cfg.BlockChain.HeaderByHash(blockhash)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = header.Serialize(&buf)
	if err != nil {
		return nil, err
	}

	return hex.EncodeToString(buf.Bytes()), nil
}

type HeadersResponse struct {
	Count int    `json:"count"`
	Hex   string `json:"hex"`
	Max   int    `json:"max"`
}

func handleBlockchainBlockHeaders(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var startHeight, count int32
	err := json.Unmarshal(cmd.Params[0], &startHeight)
	if err != nil {
		// Try to unmarshal as a string.  If it still errors, then return the error.
		var startHeightStr string
		err := json.Unmarshal(cmd.Params[0], &startHeightStr)
		if err != nil {
			return nil, err
		}

		startHeightInt, err := strconv.Atoi(startHeightStr)
		if err != nil {
			return nil, err
		}

		startHeight = int32(startHeightInt)
	}
	err = json.Unmarshal(cmd.Params[1], &count)
	if err != nil {
		// Try to unmarshal as a string.  If it still errors, then return the error.
		var countStr string
		err := json.Unmarshal(cmd.Params[1], &countStr)
		if err != nil {
			return nil, err
		}

		countInt, err := strconv.Atoi(countStr)
		if err != nil {
			return nil, err
		}

		count = int32(countInt)
	}
	if startHeight < 0 || count < 0 {
		return nil, fmt.Errorf("Expected both startHeight %d and count %d to be non-negative",
			startHeight, count)
	}

	retCount := 0
	headers := make([]wire.BlockHeader, 0, count)
	for i := startHeight; i < startHeight+count; i++ {
		blockhash, err := s.cfg.BlockChain.BlockHashByHeight(i)
		if err != nil {
			return nil, err
		}
		header, err := s.cfg.BlockChain.HeaderByHash(blockhash)
		if err != nil {
			return nil, err
		}

		headers = append(headers, header)

		retCount++

		// We only send up to 2016 headers at a time per the electrum procotol.
		if retCount >= 2016 {
			break
		}
	}

	var buf bytes.Buffer
	for _, header := range headers {
		err := header.Serialize(&buf)
		if err != nil {
			return nil, err
		}
	}

	res := HeadersResponse{
		Count: retCount,
		Hex:   hex.EncodeToString(buf.Bytes()),
		Max:   2016,
	}

	return res, nil
}

func handleEstimateFee(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var numBlocks int32
	err := json.Unmarshal(cmd.Params[0], &numBlocks)
	if err != nil {
		// Try to unmarshal as a string.  If it still errors, then return the error.
		var numBlocksStr string
		err := json.Unmarshal(cmd.Params[0], &numBlocksStr)
		if err != nil {
			return nil, err
		}

		numBlocksInt, err := strconv.Atoi(numBlocksStr)
		if err != nil {
			return nil, err
		}

		numBlocks = int32(numBlocksInt)
		return nil, err
	}

	if numBlocks < 0 {
		return nil, fmt.Errorf("Expected the number of blocks of %d to be non-negative", numBlocks)
	}

	fee, err := s.cfg.FeeEstimator.EstimateFee(uint32(numBlocks))
	if err != nil {
		return -1, nil
	}

	return fee, nil
}

type HeadersSubscribeResponse struct {
	Hex    string `json:"hex"`
	Height int    `json:"height"`
}

func handleHeadersSubscribe(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	s.headersSubscribers[conn] = struct{}{}

	bestHash := s.cfg.BlockChain.BestSnapshot().Hash
	header, err := s.cfg.BlockChain.HeaderByHash(&bestHash)
	if err != nil {
		return nil, err
	}

	height, err := s.cfg.BlockChain.BlockHeightByHash(&bestHash)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	err = header.Serialize(&buf)
	if err != nil {
		return nil, err
	}

	serializedHeaderStr := hex.EncodeToString(buf.Bytes())
	resp := HeadersSubscribeResponse{
		Height: int(height),
		Hex:    serializedHeaderStr,
	}

	return resp, nil
}

func handleRelayFee(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	return s.cfg.MinRelayFee.ToBTC(), nil
}

type Balance struct {
	Confirmed   btcutil.Amount `json:"confirmed"`
	Unconfirmed btcutil.Amount `json:"unconfirmed"`
}

func handleScriptHashGetBalance(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var hashStr string
	err := json.Unmarshal(cmd.Params[0], &hashStr)
	if err != nil {
		return nil, err
	}

	decodedHash := new(chainhash.Hash)
	err = chainhash.Decode(decodedHash, hashStr)
	if err != nil {
		return nil, err
	}

	confirmedBalance := s.cfg.WatchOnlyWallet.GetScriptHashBalance(*decodedHash)
	unConfirmedBalance := s.cfg.WatchOnlyWallet.GetMempoolBalance(*decodedHash)

	ret := Balance{
		Confirmed:   btcutil.Amount(confirmedBalance),
		Unconfirmed: btcutil.Amount(unConfirmedBalance),
	}

	return ret, nil
}

type ScriptHashHistory struct {
	Height int    `json:"height"`
	TxHash string `json:"tx_hash"`
	Fee    int    `json:"fee,omitempty"`
}

func handleScriptHashGetHistory(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var hashStr string
	err := json.Unmarshal(cmd.Params[0], &hashStr)
	if err != nil {
		return nil, err
	}

	decodedHash := new(chainhash.Hash)
	err = chainhash.Decode(decodedHash, hashStr)
	if err != nil {
		return nil, err
	}

	HashHeightAndIndexes := s.cfg.WatchOnlyWallet.GetHistory(*decodedHash)

	ret := []ScriptHashHistory{}
	for _, hhi := range HashHeightAndIndexes {
		elem := ScriptHashHistory{
			Height: int(hhi.Height),
			TxHash: hhi.Hash.String(),
		}

		ret = append(ret, elem)
	}

	HashHeightAndIndexes = s.cfg.WatchOnlyWallet.GetMempool(*decodedHash)
	for _, hhi := range HashHeightAndIndexes {
		elem := ScriptHashHistory{
			Height: int(hhi.Height),
			TxHash: hhi.Hash.String(),
			Fee:    int(hhi.Fee),
		}

		ret = append(ret, elem)
	}

	return ret, nil
}

func handleScriptHashGetMempool(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var hashStr string
	err := json.Unmarshal(cmd.Params[0], &hashStr)
	if err != nil {
		return nil, err
	}

	decodedHash := new(chainhash.Hash)
	err = chainhash.Decode(decodedHash, hashStr)
	if err != nil {
		return nil, err
	}

	HashHeightAndIndexes := s.cfg.WatchOnlyWallet.GetMempool(*decodedHash)

	ret := make([]ScriptHashHistory, 0, len(HashHeightAndIndexes))
	for _, hhi := range HashHeightAndIndexes {
		elem := ScriptHashHistory{
			Height: int(hhi.Height),
			TxHash: hhi.Hash.String(),
			Fee:    int(hhi.Fee),
		}

		ret = append(ret, elem)
	}

	return ret, nil
}

type Unspent struct {
	TxPos  int    `json:"tx_pos"`
	Value  int    `json:"value"`
	Height int    `json:"height"`
	TxHash string `json:"tx_hash"`
}

func handleListUnspent(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var hashStr string
	err := json.Unmarshal(cmd.Params[0], &hashStr)
	if err != nil {
		return nil, err
	}

	decodedHash := new(chainhash.Hash)
	err = chainhash.Decode(decodedHash, hashStr)
	if err != nil {
		return nil, err
	}

	leafDataExtras := s.cfg.WatchOnlyWallet.GetUnspent(*decodedHash)
	unspents := make([]Unspent, 0, len(leafDataExtras))

	for _, leafDataExtra := range leafDataExtras {
		unspents = append(unspents,
			Unspent{
				TxPos:  leafDataExtra.BlockIdx,
				Value:  int(leafDataExtra.LeafData.Amount),
				Height: leafDataExtra.BlockHeight,
				TxHash: leafDataExtra.LeafData.OutPoint.Hash.String(),
			},
		)
	}

	hhis := s.cfg.WatchOnlyWallet.GetMempool(*decodedHash)
	for _, hhi := range hhis {
		// index of less than 0 means that a txIn is relvant for this tx.
		// It's gonna be spent so don't include it.
		if hhi.Idx < 0 {
			continue
		}
		unspents = append(unspents,
			Unspent{
				TxPos:  hhi.Idx,
				Value:  int(hhi.Amount),
				TxHash: hhi.Hash.String(),
				Height: 0,
			},
		)
	}

	return unspents, nil
}

func handleScriptHashSubscribe(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var hashStr string
	err := json.Unmarshal(cmd.Params[0], &hashStr)
	if err != nil {
		return nil, err
	}

	decodedHash := new(chainhash.Hash)
	err = chainhash.Decode(decodedHash, hashStr)
	if err != nil {
		return nil, err
	}

	hash := s.cfg.WatchOnlyWallet.GetScriptHash(*decodedHash)

	scriptHashMap, found := s.scriptHashSubscribers[conn]
	if !found {
		scriptHashMap = make(map[chainhash.Hash]struct{})
		scriptHashMap[*decodedHash] = struct{}{}
		s.scriptHashSubscribers[conn] = scriptHashMap

		log.Debugf("total subs for conn %s: %d. new sub for: raw hash %s, decoded hash %s, returned hash: %s\n",
			conn.RemoteAddr().String(),
			len(s.scriptHashSubscribers[conn]),
			hashStr,
			decodedHash.String(),
			hex.EncodeToString(hash[:]))
	} else {
		scriptHashMap[*decodedHash] = struct{}{}

		log.Debugf("total subs for conn %s: %d. new sub for: raw hash %s, decoded hash %s, returned hash: %s\n",
			conn.RemoteAddr().String(),
			len(scriptHashMap),
			hashStr,
			decodedHash.String(),
			hex.EncodeToString(hash[:]))
	}

	if len(hash) == 0 {
		return nil, nil
	}

	return hex.EncodeToString(hash[:]), nil
}

func handleScriptHashUnsubscribe(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	delete(s.scriptHashSubscribers, conn)
	return nil, nil
}

func handleTransactionBroadcast(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var hexStr string
	err := json.Unmarshal(cmd.Params[0], &hexStr)
	if err != nil {
		return nil, err
	}

	if len(hexStr)%2 != 0 {
		hexStr = "0" + hexStr
	}

	serializedTx, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, err
	}

	var msgTx wire.MsgTx
	err = msgTx.Deserialize(bytes.NewReader(serializedTx))
	if err != nil {
		return nil, &btcjson.RPCError{
			Code:    btcjson.ErrRPCDeserialization,
			Message: "TX decode failed: " + err.Error(),
		}
	}

	tx := btcutil.NewTx(&msgTx)
	udata, err := s.cfg.WatchOnlyWallet.ProveTx(tx)
	if err != nil {
		return nil, &btcjson.RPCError{
			Code:    btcjson.ErrRPCDeserialization,
			Message: "Failed to prove the tx in the utreexo accumulator: " + err.Error(),
		}
	}
	tx.MsgTx().UData = udata

	acceptedTxs, err := s.cfg.Mempool.ProcessTransaction(tx, false, false, 0)
	if err != nil {
		// When the error is a rule error, it means the transaction was
		// simply rejected as opposed to something actually going wrong,
		// so log it as such. Otherwise, something really did go wrong,
		// so log it as an actual error and return.
		ruleErr, ok := err.(mempool.RuleError)
		if !ok {
			log.Errorf("Failed to process transaction %v: %v",
				tx.Hash(), err)

			return nil, &btcjson.RPCError{
				Code:    btcjson.ErrRPCTxError,
				Message: "TX rejected: " + err.Error(),
			}
		}

		log.Debugf("Rejected transaction %v: %v", tx.Hash(), err)

		// We'll then map the rule error to the appropriate RPC error,
		// matching bitcoind's behavior.
		code := btcjson.ErrRPCTxError
		if txRuleErr, ok := ruleErr.Err.(mempool.TxRuleError); ok {
			errDesc := txRuleErr.Description
			switch {
			case strings.Contains(
				strings.ToLower(errDesc), "orphan transaction",
			):
				code = btcjson.ErrRPCTxError

			case strings.Contains(
				strings.ToLower(errDesc), "transaction already exists",
			):
				code = btcjson.ErrRPCTxAlreadyInChain

			default:
				code = btcjson.ErrRPCTxRejected
			}
		}

		return nil, &btcjson.RPCError{
			Code:    code,
			Message: "TX rejected: " + err.Error(),
		}
	}

	// When the transaction was accepted it should be the first item in the
	// returned array of accepted transactions.  The only way this will not
	// be true is if the API for ProcessTransaction changes and this code is
	// not properly updated, but ensure the condition holds as a safeguard.
	//
	// Also, since an error is being returned to the caller, ensure the
	// transaction is removed from the memory pool.
	if len(acceptedTxs) == 0 || !acceptedTxs[0].Tx.Hash().IsEqual(tx.Hash()) {
		s.cfg.Mempool.RemoveTransaction(tx, true)

		errStr := fmt.Errorf("transaction %v is not in accepted list",
			tx.Hash())
		return nil, errStr
	}

	// Generate and relay inventory vectors for all newly accepted
	// transactions into the memory pool due to the original being
	// accepted.
	s.cfg.RelayTransactions(acceptedTxs)

	// Keep track of all the sendrawtransaction request txns so that they
	// can be rebroadcast if they don't make their way into a block.
	txD := acceptedTxs[0]
	iv := wire.NewInvVect(wire.InvTypeTx, txD.Tx.Hash())
	s.cfg.AddRebroadcastInventory(iv, txD)
	s.cfg.AnnounceNewTransactions([]*mempool.TxDesc{txD})

	return tx.Hash().String(), nil
}

func handleGetTransaction(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var hashStr string
	err := json.Unmarshal(cmd.Params[0], &hashStr)
	if err != nil {
		return nil, err
	}

	decodedHash := new(chainhash.Hash)
	err = chainhash.Decode(decodedHash, hashStr)
	if err != nil {
		return nil, err
	}

	tx := s.cfg.WatchOnlyWallet.GetTx(*decodedHash)
	if tx == nil {
		return nil, nil
	}

	txBuf := bytes.NewBuffer(make([]byte, 0, tx.SerializeSize()))
	err = tx.Serialize(txBuf)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tx %s. Error %v",
			tx.TxHash(), err)
	}

	return hex.EncodeToString(txBuf.Bytes()), nil
}

type GetMerkleRes struct {
	Merkle      []string `json:"merkle"`
	BlockHeight int      `json:"block_height"`
	Pos         int      `json:"pos"`
}

func handleGetMerkle(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var hashStr string
	err := json.Unmarshal(cmd.Params[0], &hashStr)
	if err != nil {
		return nil, err
	}
	var height int
	err = json.Unmarshal(cmd.Params[1], &height)
	if err != nil {
		// Try to unmarshal as a string.  If it still errors, then return the error.
		var heightStr string
		err := json.Unmarshal(cmd.Params[1], &heightStr)
		if err != nil {
			return nil, err
		}

		height, err = strconv.Atoi(heightStr)
		if err != nil {
			return nil, err
		}
	}
	if height < 0 {
		return nil, fmt.Errorf("Got height %d. Expected height to be non-negative.", height)
	}

	decodedHash := new(chainhash.Hash)
	err = chainhash.Decode(decodedHash, hashStr)
	if err != nil {
		return nil, err
	}

	merkles, blockHeight, index := s.cfg.WatchOnlyWallet.GetMerkle(*decodedHash)
	if len(merkles) == 0 {
		return GetMerkleRes{}, nil
	}

	if blockHeight != height {
		return nil, fmt.Errorf("tx %s not at height %d", hashStr, height)
	}

	merklesStr := make([]string, 0, len(merkles))
	for _, merkle := range merkles {
		merklesStr = append(merklesStr, merkle.String())
	}
	return GetMerkleRes{Merkle: merklesStr, BlockHeight: height, Pos: index}, nil
}

type IDFromPosRes struct {
	TxHash string   `json:"tx_hash"`
	Merkle []string `json:"merkle"`
}

func handleIDFromPos(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	var height, txPos int
	var getMerkles bool

	err := json.Unmarshal(cmd.Params[0], &height)
	if err != nil {
		return nil, err
	}
	if height < 0 {
		return nil, fmt.Errorf("Expected received height of %d to be non-negative", height)
	}
	err = json.Unmarshal(cmd.Params[1], &txPos)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(cmd.Params[2], &getMerkles)
	if err != nil {
		return nil, err
	}

	txHash, merkles := s.cfg.WatchOnlyWallet.GetTXIDFromBlockPos(height, txPos, getMerkles)

	merklesStr := make([]string, len(merkles))
	for _, merkle := range merkles {
		merklesStr = append(merklesStr, merkle.String())
	}

	return IDFromPosRes{TxHash: txHash.String(), Merkle: merklesStr}, nil
}

func handleMempoolGetFeeHistogram(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	txs := s.cfg.Mempool.MiningDescs()

	type feeAndSize struct {
		fee  int64
		size int64
	}

	feeAndSizes := make([]feeAndSize, len(txs))
	for i, tx := range txs {
		feeAndSizes[i] = feeAndSize{
			fee:  tx.FeePerKB / 1000,
			size: mempool.GetTxVirtualSize(tx.Tx),
		}
	}

	sort.Slice(feeAndSizes, func(a, b int) bool {
		return feeAndSizes[a].fee < feeAndSizes[b].fee
	})

	binSize := int64(mempoolHistogramBinSize)
	histogram := make([][]int64, 0, len(txs))
	var prevFeeRate, cumSize int64
	for _, fs := range feeAndSizes {
		//    If there is a big lump of txns at this specific size,
		//    consider adding the previous item now (if not added already)
		if fs.size > 2*binSize &&
			prevFeeRate != 0 &&
			cumSize > 0 {

			histogram = append(histogram, []int64{prevFeeRate, cumSize})
			cumSize = 0
			binSize = (binSize * 11) / 10
		}
		// Now consider adding this item
		cumSize += fs.size
		if cumSize > binSize {
			histogram = append(histogram, []int64{fs.fee, cumSize})
			cumSize = 0
			binSize = (binSize * 11) / 10
		}
		prevFeeRate = fs.fee
	}

	return histogram, nil
}

func handleServerAddPeer(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	// We always return false as we don't supoprt adding peers.
	return false, nil
}

func handleBanner(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	// (kcalvinalvin) Best I can do. Looks awesome imo.
	str := `
	 /$$     /$$ /$$$$$$$$ /$$$$$$   /$$$$$$$$ /$$$$$$$$ /$$   /$$  /$$$$$$
	| $$    / $$|__  $$__/| $$__  $$| $$_____/| $$_____/| $$  / $$ /$$__  $$
	| $$    | $$   | $$   | $$  \ $$| $$$$$   | $$$$$   \ $$  | $$| $$  \ $$
	| $$    | $$   | $$   | $$$$$$$ | $$__/   | $$__/    |  $$$$  | $$  | $$
	| $$    | $$   | $$   | $$__  $$| $$      | $$      / $$__  $$| $$  | $$
	| $$    | $$   | $$   | $$  \ $$| $$      | $$      | $$  \ $$| $$  | $$
	|  $$$$$$$ /   | $$   | $$  | $$| $$$$$$$$| $$$$$$$$| $$  | $$|  $$$$$$/
	 \________/    |__/   |__/  |__/|________/|________/|__/  |__/ \______/ `
	return str, nil
}

func handleDonationAddress(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	switch s.cfg.Params.Net {
	case chaincfg.MainNetParams.Net:
		return "bc1qhjeu95vx0c7zycfumv6jarl32dvcsyfd0egeue", nil
	case chaincfg.TestNet3Params.Net:
		return "tb1q4280xax2lt0u5a5s9hd4easuvzalm8v9ege9ge", nil
	case chaincfg.SigNetParams.Net:
		return "tb1qlh279t5a0067n3sg6l56e4tq3yghy687c48tm9", nil
	case chaincfg.RegressionNetParams.Net:
		return "bcrt1qduc2gmuwkun9wnlcfp6ak8zzphmyee4dakgnlk", nil
	}

	// Just return this if we didn't match anything.
	return "bc1qhjeu95vx0c7zycfumv6jarl32dvcsyfd0egeue", nil
}

type HostPort struct {
	TCPPort int `json:"tcp_port"`
	SslPort int `json:"ssl_port"`
}

type ServerFeaturesResponse struct {
	GenesisHash   string              `json:"genesis_hash"`
	Hosts         map[string]HostPort `json:"hosts"`
	ProtocolMax   string              `json:"protocol_max"`
	ProtocolMin   string              `json:"protocol_min"`
	Pruning       bool                `json:"pruning,omitempty"`
	ServerVersion string              `json:"server_version"`
	HashFunction  string              `json:"hash_function"`
}

func handleServerFeatures(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	hostsMap := make(map[string]HostPort)

	for i, listener := range s.cfg.Listeners {
		hostAddr, port, err := net.SplitHostPort(listener.Addr().String())
		if err != nil {
			log.Errorf("handleServerFeatures error. Err: %v", err)
			continue
		}
		portNum, _ := strconv.Atoi(port)

		if s.cfg.ListenerTLS[i] {
			host, found := hostsMap[hostAddr]
			if found {
				host.SslPort = portNum
			} else {
				host = HostPort{
					SslPort: portNum,
				}
			}

			hostsMap[hostAddr] = host
		} else {
			host, found := hostsMap[hostAddr]
			if found {
				host.TCPPort = portNum
			} else {
				host = HostPort{
					TCPPort: portNum,
				}
			}

			hostsMap[hostAddr] = host
		}
	}

	ret := ServerFeaturesResponse{
		GenesisHash:   s.cfg.Params.GenesisHash.String(),
		Hosts:         hostsMap,
		ProtocolMin:   minElectrumProtocolVersion,
		ProtocolMax:   electrumProtocolVersion,
		Pruning:       true,
		ServerVersion: "utreexod-electrum-server v0.1.0",
		HashFunction:  "sha256",
	}

	return ret, nil
}

func handleServerPeersSubscribe(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	// Purposely left as an emtpy []string because we don't support having peers.
	return []string{}, nil
}

func handlePing(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	return nil, nil
}

func handleVersion(s *ElectrumServer, cmd *btcjson.Request, conn net.Conn, closeChan <-chan struct{}) (interface{}, error) {
	return []string{serverName, minElectrumProtocolVersion}, nil
}

type connAndBytes struct {
	conn  net.Conn
	bytes []byte
}

type ElectrumServer struct {
	started     int32
	shutdown    int32
	cfg         Config
	quit        chan int
	statusLock  sync.RWMutex
	statusLines map[int]string
	wg          sync.WaitGroup
	writeChan   chan []connAndBytes

	headersSubscribers    map[net.Conn]struct{}
	scriptHashSubscribers map[net.Conn]map[chainhash.Hash]struct{}

	utxoDataChan   chan interface{}
	scriptHashChan chan interface{}
}

func (s *ElectrumServer) writeErrorResponse(conn net.Conn, rpcError btcjson.RPCError, pid *interface{}) {
	resp := btcjson.Response{
		Jsonrpc: btcjson.RpcVersion2,
		Error:   &rpcError,
		ID:      pid,
	}
	bytes, err := json.Marshal(resp)
	if err != nil {
		log.Warnf("error while marhsalling rpc invalid request. "+
			"Error: %s", err)
		return
	}
	bytes = append(bytes, delim)
	s.writeChan <- []connAndBytes{{conn, bytes}}
}

func (s *ElectrumServer) handleConnection(conn net.Conn) {
	// The timer is stopped when a new message is received and reset after it
	// is processed.
	idleTimer := time.AfterFunc(idleTimeout, func() {
		log.Infof("Electrum client %s no answer for %s -- disconnecting",
			conn.RemoteAddr(), idleTimeout)
		conn.Close()
	})

	defer conn.Close()
	reader := bufio.NewReader(conn)
	for atomic.LoadInt32(&s.shutdown) == 0 {
		line, err := reader.ReadBytes(delim)
		if err != nil {
			if err != io.EOF {
				log.Warnf("error while reading message from peer. "+
					"Error: %v", err)
			}

			break
		}
		idleTimer.Stop()

		msg := btcjson.Request{}
		err = msg.UnmarshalJSON(line)
		if err != nil {
			log.Warnf("error while unmarshalling %v. Sending error message to %v. Error: %v",
				hex.EncodeToString(line), conn.RemoteAddr().String(), err)
			if e, ok := err.(*json.SyntaxError); ok {
				log.Warnf("syntax error at byte offset %d", e.Offset)
			}

			pid := &msg.ID
			s.writeErrorResponse(conn, *btcjson.ErrRPCParse, pid)
			continue
		}

		log.Debugf("unmarshalled %v from conn %s\n", msg, conn.RemoteAddr().String())
		handler, found := rpcHandlers[msg.Method]
		if !found || handler == nil {
			log.Warnf("handler not found for method %v. Sending error msg to %v",
				msg.Method, conn.RemoteAddr().String())
			pid := &msg.ID
			s.writeErrorResponse(conn, *btcjson.ErrRPCMethodNotFound, pid)
			idleTimer.Reset(idleTimeout)
			continue
		}

		result, err := handler(s, &msg, conn, nil)
		if err != nil {
			log.Warnf("Errored while handling method %s. Sending error message to %v err: %v\n",
				msg.Method, conn.RemoteAddr().String(), err)

			pid := &msg.ID
			rpcError := btcjson.RPCError{
				Code:    1,
				Message: err.Error(),
			}
			s.writeErrorResponse(conn, rpcError, pid)
			idleTimer.Reset(idleTimeout)
			continue
		}

		marshalledResult, err := json.Marshal(result)
		if err != nil {
			log.Warnf("Errored while marshaling result for method %s. Sending error message to %v err: %v\n",
				msg.Method, conn.RemoteAddr().String(), err)
			rpcError := btcjson.RPCError{
				Code: btcjson.ErrRPCInternal.Code,
				Message: fmt.Sprintf("%s: error: %s",
					btcjson.ErrRPCInternal.Message, err.Error()),
			}
			pid := &msg.ID
			s.writeErrorResponse(conn, rpcError, pid)
			continue
		}
		pid := &msg.ID
		resp := btcjson.Response{
			Jsonrpc: btcjson.RpcVersion2,
			Result:  json.RawMessage(marshalledResult),
			ID:      pid,
		}
		bytes, err := json.Marshal(resp)
		if err != nil {
			log.Warnf("Errored while marshaling response for method %s. Sending error message to %v err: %v\n",
				msg.Method, conn.RemoteAddr().String(), err)
			rpcError := btcjson.RPCError{
				Code: btcjson.ErrRPCInternal.Code,
				Message: fmt.Sprintf("%s: error: %s",
					btcjson.ErrRPCInternal.Message, err.Error()),
			}
			pid := &msg.ID
			s.writeErrorResponse(conn, rpcError, pid)
			idleTimer.Reset(idleTimeout)
			continue
		}
		bytes = append(bytes, delim)

		log.Debugf("put %v to be written to %v\n", result, conn.RemoteAddr().String())
		s.writeChan <- []connAndBytes{{conn, bytes}}

		idleTimer.Reset(idleTimeout)
	}

	idleTimer.Stop()

	delete(s.headersSubscribers, conn)
	delete(s.scriptHashSubscribers, conn)

	log.Infof("Electrum client %s closed connection", conn.RemoteAddr())
}

func (s *ElectrumServer) writeToClient() {
	for {
		select {
		case <-s.quit:
			return
		case d := <-s.writeChan:
			for _, cb := range d {
				_, err := cb.conn.Write(cb.bytes)
				if err != nil {
					log.Warnf("error while writing to %s. Error: %s",
						cb.conn.RemoteAddr().String(), err)
					continue
				}
			}
		}
	}
}

func (s *ElectrumServer) listen(listener net.Listener) {
	defer s.wg.Done()

	var tempDelay time.Duration // how long to sleep on accept failure
	for atomic.LoadInt32(&s.shutdown) == 0 {
		conn, err := listener.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				log.Infof("Electrum server: Accept error: %v; "+
					"retrying in %v", err, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return
		}

		go s.handleConnection(conn)
	}
}

func (s *ElectrumServer) handleBlockChainNotification(notification *blockchain.Notification) {
	switch notification.Type {
	// A block has been accepted into the block chain.
	case blockchain.NTBlockConnected:
		block, ok := notification.Data.(*btcutil.Block)
		if !ok {
			log.Warnf("Chain connected notification is not a block.")
			return
		}

		var buf bytes.Buffer
		err := block.MsgBlock().Header.Serialize(&buf)
		if err != nil {
			log.Warnf("Couldn't serialize the header for block %s",
				block.Hash().String())
			return
		}
		params := HeadersSubscribeResponse{
			Hex:    hex.EncodeToString(buf.Bytes()),
			Height: int(block.Height()),
		}
		paramBytes, err := json.Marshal(params)
		if err != nil {
			log.Warnf("error while marshalling json response for block header %s. "+
				"Error: %v", block.Hash().String(), err)
			return
		}

		resp := btcjson.Request{
			Jsonrpc: btcjson.RpcVersion2,
			Method:  "blockchain.headers.subscribe",
			Params:  []json.RawMessage{paramBytes},
		}

		bytes, err := json.Marshal(resp)
		if err != nil {
			log.Warnf("error while marshalling block header %s. "+
				"Error: %v", block.Hash().String(), err)
			return
		}
		bytes = append(bytes, delim)

		cb := make([]connAndBytes, 0, len(s.headersSubscribers))
		for conn := range s.headersSubscribers {
			log.Debugf("blockchain.headers.subscribe handler: put %v to be written to %v\n",
				resp, conn.RemoteAddr().String())
			cb = append(cb, connAndBytes{conn, bytes})
		}

		s.writeChan <- cb
	}
}

func (s *ElectrumServer) handleUTXOActivity() {
	for {
		select {
		case <-s.quit:
			return

		case d := <-s.scriptHashChan:
			data, ok := d.(wallet.StatusUpdate)
			if !ok {
				log.Warnf("Received data is not wallet.StatusUpdate")
				break
			}

			cb := make([]connAndBytes, 0, len(data.ScriptHash))
			for conn, hashMap := range s.scriptHashSubscribers {
				_, found := hashMap[data.ScriptHash]
				if !found {
					continue
				}
				params := []string{data.ScriptHash.String(), hex.EncodeToString(data.Status[:])}

				rawParams := make([]json.RawMessage, 0, len(params))
				for _, param := range params {
					marshalledParam, err := json.Marshal(param)
					if err != nil {
						log.Warnf("error while marshalling json response for script status for: %s. "+
							"Error: %v", hex.EncodeToString(data.ScriptHash[:]), err)
						break
					}
					rawMessage := json.RawMessage(marshalledParam)
					rawParams = append(rawParams, rawMessage)
				}
				resp := btcjson.Request{
					Jsonrpc: btcjson.RpcVersion2,
					Method:  "blockchain.scripthash.subscribe",
					Params:  rawParams,
				}
				bytes, err := json.Marshal(resp)
				if err != nil {
					log.Warnf("error while marshalling script status for script hash %s. "+
						"Error: %v", hex.EncodeToString(data.ScriptHash[:]), err)
					break
				}
				bytes = append(bytes, delim)
				cb = append(cb, connAndBytes{conn, bytes})

			}

			s.writeChan <- cb
		}
	}
}

func (s *ElectrumServer) Start() {
	if atomic.AddInt32(&s.started, 1) != 1 {
		return
	}

	log.Infof("Starting the electrum server")

	for i, listener := range s.cfg.Listeners {
		s.wg.Add(1)
		log.Infof("Electrum server listening on %s. ssl %v",
			listener.Addr().String(), s.cfg.ListenerTLS[i])

		go func(l net.Listener) {
			go s.listen(l)
		}(listener)
	}

	s.cfg.WatchOnlyWallet.ScriptHashSubscribe(s.scriptHashChan)
}

func (s *ElectrumServer) Stop() {
	if atomic.AddInt32(&s.shutdown, 1) != 1 {
		log.Infof("Electrum server is already in the process of shutting down")
		return
	}
	log.Infof("Stopping Electrum server...")

	for _, listener := range s.cfg.Listeners {
		err := listener.Close()
		if err != nil {
			log.Errorf("Problem shutting down electrum server: %v", err)
		}
	}

	close(s.quit)
	s.wg.Wait()

	log.Infof("Electrum server stopped")
}

// Config is a configuration struct used to initialize a new electrum server.
type Config struct {
	// Listeners defines a slice of listeners for which the RPC server will
	// take ownership of and accept connections.  Since the RPC server takes
	// ownership of these listeners, they will be closed when the RPC server
	// is stopped.
	Listeners []net.Listener

	ListenerTLS []bool

	// MaxClients is the amount of clients that are allowed to connect to the
	// electrum server. Set to -1 to have no limits.
	MaxClients int32

	// WatchOnlyWallet is the wallet that keeps track of the balances and relevant
	// utxos.
	WatchOnlyWallet *wallet.WatchOnlyWalletManager

	Params       *chaincfg.Params
	BlockChain   *blockchain.BlockChain
	FeeEstimator *mempool.FeeEstimator
	Mempool      *mempool.TxPool
	MinRelayFee  btcutil.Amount

	AddRebroadcastInventory func(iv *wire.InvVect, data interface{})
	RelayTransactions       func(txns []*mempool.TxDesc)
	AnnounceNewTransactions func(txns []*mempool.TxDesc)
}

// New constructs a new instance of the electrum server.
func New(config *Config) (*ElectrumServer, error) {
	s := ElectrumServer{
		cfg:                   *config,
		quit:                  make(chan int),
		writeChan:             make(chan []connAndBytes, 1),
		headersSubscribers:    make(map[net.Conn]struct{}),
		scriptHashSubscribers: make(map[net.Conn]map[chainhash.Hash]struct{}),
		utxoDataChan:          make(chan interface{}, 1),
		scriptHashChan:        make(chan interface{}, 1),
	}

	s.cfg.BlockChain.Subscribe(s.handleBlockChainNotification)

	go s.writeToClient()
	go s.handleUTXOActivity()

	return &s, nil
}

func init() {
	rpcHandlers = rpcHandlersBeforeInit
	rand.Seed(time.Now().UnixNano())
}
