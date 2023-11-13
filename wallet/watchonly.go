// Copyright (c) 2022 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package wallet

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/btcutil/hdkeychain"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/mempool"
	"github.com/utreexo/utreexod/txscript"
	"github.com/utreexo/utreexod/wire"
)

// HDVersion represents the different supported schemes of hierarchical
// derivation.
type HDVersion uint32

const (
	// HDVersionMainNetBIP0044 is the HDVersion for BIP-0044 on the main
	// network.
	HDVersionMainNetBIP0044 HDVersion = 0x0488b21e // xpub

	// HDVersionMainNetBIP0049 is the HDVersion for BIP-0049 on the main
	// network.
	HDVersionMainNetBIP0049 HDVersion = 0x049d7cb2 // ypub

	// HDVersionMainNetBIP0084 is the HDVersion for BIP-0084 on the main
	// network.
	HDVersionMainNetBIP0084 HDVersion = 0x04b24746 // zpub

	// HDVersionTestNetBIP0044 is the HDVersion for BIP-0044 on the test
	// network.
	HDVersionTestNetBIP0044 HDVersion = 0x043587cf // tpub

	// HDVersionTestNetBIP0049 is the HDVersion for BIP-0049 on the test
	// network.
	HDVersionTestNetBIP0049 HDVersion = 0x044a5262 // upub

	// HDVersionTestNetBIP0084 is the HDVersion for BIP-0084 on the test
	// network.
	HDVersionTestNetBIP0084 HDVersion = 0x045f1cf6 // vpub

	// HDVersionSimNetBIP0044 is the HDVersion for BIP-0044 on the
	// simulation test network. There aren't any other versions defined for
	// the simulation test network.
	HDVersionSimNetBIP0044 HDVersion = 0x0420bd3a // spub
)

// String converts the version to a human readable string.
func (hd HDVersion) String() string {
	var str string
	switch hd {
	case HDVersionMainNetBIP0044:
		str = "p2pkh"
	case HDVersionTestNetBIP0044:
		str = "p2pkh"
	case HDVersionSimNetBIP0044:
		str = "p2pkh"
	case HDVersionMainNetBIP0049:
		str = "p2sh"
	case HDVersionTestNetBIP0049:
		str = "p2sh"
	case HDVersionMainNetBIP0084:
		str = "p2wpkh"
	case HDVersionTestNetBIP0084:
		str = "p2wpkh"
	}

	return str
}

var (
	defaultWalletPath       = "watchonlywallet"
	defaultWalletConfigName = "watchonlywalletconfig.json"
	defaultWalletName       = "watchonlywallet.json"
)

// LeafDataExtras is all the leaf data plus all other data that an electrum server
// would need for a relevant txo.
type LeafDataExtras struct {
	// LeafData is the The underlying leaf data.
	LeafData wire.LeafData `json:"leafdata"`

	// BlockIdx is the in block position of the tx of the leafdata. Coinbases
	// are always a blockindex of 0.
	BlockIdx int `json:"blockindex"`

	// BlockHeight is the height of this leafdata. If the leaf data is for an stxo,
	// block height is where the leaf was spent.
	BlockHeight int `json:"blockheight"`
}

func (tx LeafDataExtras) MarshalJSON() ([]byte, error) {
	leafBytes, err := json.Marshal(tx.LeafData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal leafdata for outpoint %s. Error %v",
			tx.LeafData.OutPoint.String(), err)
	}

	s := struct {
		LeafData    string `json:"leafdata"`
		BlockIdx    int    `json:"blockindex"`
		BlockHeight int    `json:"blockheight"`
	}{
		LeafData:    hex.EncodeToString(leafBytes),
		BlockIdx:    tx.BlockIdx,
		BlockHeight: tx.BlockHeight,
	}

	return json.Marshal(s)
}

func (tx *LeafDataExtras) UnmarshalJSON(data []byte) error {
	s := struct {
		LeafData    string `json:"leafdata"`
		BlockIdx    int    `json:"blockindex"`
		BlockHeight int    `json:"blockheight"`
	}{}

	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	ldBytes, err := hex.DecodeString(s.LeafData)
	if err != nil {
		return fmt.Errorf("Failed to decode leafdata string to bytes. "+
			"Error: %v", err)
	}

	var ld wire.LeafData
	err = json.Unmarshal(ldBytes, &ld)
	if err != nil {
		return err
	}

	tx.LeafData = ld
	tx.BlockHeight = s.BlockHeight
	tx.BlockIdx = s.BlockIdx

	return nil
}

// RelevantTxData includes all the extra data that an electrum server could request
// that's related to a tx.
type RelevantTxData struct {
	// BlockIndex is the position in the block the tx is at.
	BlockIndex int `json:"position"`

	// BlockHeight is the block this tx was included in.
	BlockHeight int `json:"blockheight"`

	// Tx is the raw tx of for this tx.
	Tx *wire.MsgTx `json:"tx"`

	// MerkleProof is the hashes needed to hash to the merkle root commited
	// in the block.
	MerkleProof []*chainhash.Hash `json:"merkleproof"`
}

func (tx RelevantTxData) MarshalJSON() ([]byte, error) {
	merklesStr := make([]string, 0, len(tx.MerkleProof))
	for _, merkle := range tx.MerkleProof {
		merklesStr = append(merklesStr, merkle.String())
	}

	txBuf := bytes.NewBuffer(make([]byte, 0, tx.Tx.SerializeSize()))
	tx.Tx.UData = nil
	err := tx.Tx.Serialize(txBuf)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tx %s. Error %v",
			tx.Tx.TxHash(), err)
	}

	s := struct {
		BlockIndex  int      `json:"position"`
		BlockHeight int      `json:"blockheight"`
		Tx          string   `json:"tx"`
		MerkleProof []string `json:"merkleproof"`
	}{
		BlockIndex:  tx.BlockIndex,
		BlockHeight: tx.BlockHeight,
		Tx:          hex.EncodeToString(txBuf.Bytes()),
		MerkleProof: merklesStr,
	}

	return json.Marshal(s)
}

func (tx *RelevantTxData) UnmarshalJSON(data []byte) error {
	s := struct {
		BlockIndex  int      `json:"position"`
		BlockHeight int      `json:"blockheight"`
		Tx          string   `json:"tx"`
		MerkleProof []string `json:"merkleproof"`
	}{}
	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	tx.BlockIndex = s.BlockIndex
	tx.BlockHeight = s.BlockHeight

	txBytes, err := hex.DecodeString(s.Tx)
	if err != nil {
		return fmt.Errorf("Failed to decode tx string of %s. "+
			"Error: %v", s.Tx, err)
	}
	msgTx := wire.NewMsgTx(1)
	err = msgTx.Deserialize(bytes.NewBuffer(txBytes))
	if err != nil {
		return fmt.Errorf("Failed to deserialize tx hex of %s. "+
			"Error: %v", hex.EncodeToString(txBytes), err)
	}
	tx.Tx = msgTx

	merkles := make([]*chainhash.Hash, len(s.MerkleProof))
	for i := range merkles {
		hash, err := chainhash.NewHashFromStr(s.MerkleProof[i])
		if err != nil {
			return err
		}
		merkles[i] = hash
	}
	tx.MerkleProof = merkles

	return nil
}

// WalletState keeps track of all the current relevant utxos.  It includes a batched
// utreexo proof of all the relevant utxos.
type WalletState struct {
	/*
	 * The below fields are all the fields that are relevant to the keys of a wallet.
	 */

	// WatchedKeys are a map of maps mapping master pubkeys to addresses and a bool
	// if true, indicates that the address is a change address.
	WatchedKeys map[string]map[string]bool `json:"watchedkeys"`

	// LastExternalIndex refers to the bip32 index used for the external wallet.
	// These are used to derive address given out to others for payment.
	LastExternalIndex map[string]uint32 `json:"lastexternalindex"`

	// LastInternalIndex refers to the bip32 index used for the internal wallet.
	// These are used for deriving change addresses.
	LastInternalIndex map[string]uint32 `json:"lastinternalindex"`

	// BestHash is the latest block hash this wallet state is synced to.
	BestHash chainhash.Hash `json:"besthash"`

	/*
	 * The below fields are all that's needed to prove the utxos this wallet
	 * controls to another utreexo node.
	 */

	// NumLeaves are the total number of leaves in the accumulator. Needed for some proof related
	// functions.
	NumLeaves uint64 `json:"numleaves"`

	// UtreexoLeaves are hashes of the utxos that this wallet controls.
	UtreexoLeaves []utreexo.Hash `json:"utreexoleaves"`

	// Utreexo is all the proof needed to prove the UtreexoLeaves.
	UtreexoProof utreexo.Proof `json:"utreexoproof"`

	// RelevantUtxos are the utxos that this wallet controls.
	RelevantUtxos map[wire.OutPoint]LeafDataExtras `json:"relevantutxos"`

	// RelevantStxos are the relevant utxos that were spent. Only needed for reorgs.
	RelevantStxos map[wire.OutPoint]LeafDataExtras `json:"relevantstxos"`

	/*
	 * The below fields keep track of tx data that's relevant for the wallet. Some data are
	 * needed by the electrum server.
	 */

	// RelevantTxs keep track of relevant data for each tx.
	RelevantTxs map[chainhash.Hash]RelevantTxData `json:"relevanttxs"`

	// RelevantMempoolTxs are the mempool txs that the wallet controls.
	RelevantMempoolTxs map[chainhash.Hash]MempoolTx `json:"relevantmempooltxs"`
}

func (wp WalletState) MarshalJSON() ([]byte, error) {
	utxos := make([]LeafDataExtras, 0, len(wp.RelevantUtxos))
	for _, v := range wp.RelevantUtxos {
		utxos = append(utxos, v)
	}

	stxos := make([]LeafDataExtras, 0, len(wp.RelevantStxos))
	for _, v := range wp.RelevantStxos {
		stxos = append(stxos, v)
	}

	relevantTxs := make(map[string]RelevantTxData, len(wp.RelevantTxs))
	for k, v := range wp.RelevantTxs {
		relevantTxs[k.String()] = v
	}

	mempoolTxs := make(map[string]MempoolTx, len(wp.RelevantMempoolTxs))
	for k, v := range wp.RelevantMempoolTxs {
		mempoolTxs[k.String()] = v
	}

	leaves := make([]string, len(wp.UtreexoLeaves))
	for i := range wp.UtreexoLeaves {
		leaves[i] = hex.EncodeToString(wp.UtreexoLeaves[i][:])
	}

	// Convert the hashes to string.
	proofString := make([]string, len(wp.UtreexoProof.Proof))
	for i := range wp.UtreexoProof.Proof {
		proofString[i] = hex.EncodeToString(wp.UtreexoProof.Proof[i][:])
	}

	s := struct {
		WatchedKeys       map[string]map[string]bool `json:"watchedkeys"`
		LastExternalIndex map[string]uint32          `json:"lastexternalindex"`
		LastInternalIndex map[string]uint32          `json:"lastinternalindex"`

		BestHash           string                    `json:"besthash"`
		RelevantUtxos      []LeafDataExtras          `json:"relevantutxos"`
		RelevantStxos      []LeafDataExtras          `json:"relevantstxos"`
		RelevantTxs        map[string]RelevantTxData `json:"relevanttxs"`
		RelevantMempoolTxs map[string]MempoolTx      `json:"relevantmempooltxs"`

		NumLeaves      uint64   `json:"numleaves"`
		UtreexoLeaves  []string `json:"utreexoleaves"`
		UtreexoTargets []uint64 `json:"utreexotargets"`
		UtreexoProof   []string `json:"utreexoproof"`
	}{
		WatchedKeys:       wp.WatchedKeys,
		LastExternalIndex: wp.LastExternalIndex,
		LastInternalIndex: wp.LastInternalIndex,

		BestHash:           wp.BestHash.String(),
		RelevantUtxos:      utxos,
		RelevantStxos:      stxos,
		RelevantTxs:        relevantTxs,
		RelevantMempoolTxs: mempoolTxs,

		NumLeaves:      wp.NumLeaves,
		UtreexoLeaves:  leaves,
		UtreexoTargets: wp.UtreexoProof.Targets,
		UtreexoProof:   proofString,
	}

	return json.Marshal(s)
}

func (wp *WalletState) UnmarshalJSON(data []byte) error {
	s := struct {
		WatchedKeys       map[string]map[string]bool `json:"watchedkeys"`
		LastExternalIndex map[string]uint32          `json:"lastexternalindex"`
		LastInternalIndex map[string]uint32          `json:"lastinternalindex"`

		BestHash           string                    `json:"besthash"`
		RelevantUtxos      []LeafDataExtras          `json:"relevantutxos"`
		RelevantStxos      []LeafDataExtras          `json:"relevantstxos"`
		RelevantTxs        map[string]RelevantTxData `json:"relevanttxs"`
		RelevantMempoolTxs map[string]MempoolTx      `json:"relevantmempooltxs"`

		NumLeaves      uint64   `json:"numleaves"`
		UtreexoLeaves  []string `json:"utreexoleaves"`
		UtreexoTargets []uint64 `json:"utreexotargets"`
		UtreexoProof   []string `json:"utreexoproof"`
	}{}

	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	bestHash, err := chainhash.NewHashFromStr(s.BestHash)
	if err != nil {
		return err
	}
	wp.BestHash = *bestHash
	wp.NumLeaves = s.NumLeaves
	wp.WatchedKeys = s.WatchedKeys
	wp.LastExternalIndex = s.LastExternalIndex
	wp.LastInternalIndex = s.LastInternalIndex

	wp.RelevantUtxos = make(map[wire.OutPoint]LeafDataExtras, len(s.RelevantUtxos))
	for _, utxo := range s.RelevantUtxos {
		wp.RelevantUtxos[utxo.LeafData.OutPoint] = utxo
	}

	wp.RelevantStxos = make(map[wire.OutPoint]LeafDataExtras, len(s.RelevantStxos))
	for _, stxo := range s.RelevantStxos {
		wp.RelevantStxos[stxo.LeafData.OutPoint] = stxo
	}

	wp.RelevantMempoolTxs = make(map[chainhash.Hash]MempoolTx, len(s.RelevantMempoolTxs))
	for k, mempoolTx := range s.RelevantMempoolTxs {
		hash, err := chainhash.NewHashFromStr(k)
		if err != nil {
			return err
		}
		wp.RelevantMempoolTxs[*hash] = mempoolTx
	}

	wp.RelevantTxs = make(map[chainhash.Hash]RelevantTxData, len(s.RelevantTxs))
	for k, v := range s.RelevantTxs {
		hash, err := chainhash.NewHashFromStr(k)
		if err != nil {
			return err
		}

		wp.RelevantTxs[*hash] = v
	}

	wp.UtreexoLeaves = make([]utreexo.Hash, len(s.UtreexoLeaves))
	for i := range wp.UtreexoLeaves {
		hash, err := hex.DecodeString(s.UtreexoLeaves[i])
		if err != nil {
			return fmt.Errorf("Failed to decode leaf string of %s. "+
				"Error: %v", s.UtreexoLeaves[i], err)
		}

		wp.UtreexoLeaves[i] = (*(*[32]byte)(hash))
	}
	wp.UtreexoProof.Targets = s.UtreexoTargets
	wp.UtreexoProof.Proof = make([]utreexo.Hash, len(s.UtreexoProof))
	for i := range wp.UtreexoProof.Proof {
		hash, err := hex.DecodeString(s.UtreexoProof[i])
		if err != nil {
			return fmt.Errorf("Failed to decode utreexo proof string of %s. "+
				"Error: %v", s.UtreexoProof[i], err)
		}

		wp.UtreexoProof.Proof[i] = (*(*[32]byte)(hash))
	}

	return nil
}

// WalletConfig defines the config for the wallet.
type WalletConfig struct {
	Net          string
	ExtendedKeys map[string]HDVersion
	Addresses    map[string]struct{}
	GapLimit     uint32
}

func (ws WalletConfig) MarshalJSON() ([]byte, error) {
	addresses := make([]string, 0, len(ws.Addresses))
	for k := range ws.Addresses {
		addresses = append(addresses, k)
	}

	s := struct {
		Net          string               `json:"net"`
		ExtendedKeys map[string]HDVersion `json:"extendedkeys"`
		Addresses    []string             `json:"addresses"`
		GapLimit     uint32               `json:"gaplimit"`
	}{
		Net:          ws.Net,
		ExtendedKeys: ws.ExtendedKeys,
		GapLimit:     ws.GapLimit,
		Addresses:    addresses,
	}

	return json.Marshal(s)
}

func (ws *WalletConfig) UnmarshalJSON(data []byte) error {
	s := struct {
		Net          string            `json:"net"`
		ExtendedKeys map[string]uint32 `json:"extendedkeys"`
		Addresses    []string          `json:"addresses"`
		GapLimit     uint32            `json:"gaplimit"`
	}{}
	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	ws.Net = s.Net

	ws.ExtendedKeys = make(map[string]HDVersion, len(s.ExtendedKeys))
	for k, v := range s.ExtendedKeys {
		ws.ExtendedKeys[k] = HDVersion(v)
	}

	var params chaincfg.Params
	switch ws.Net {
	case chaincfg.MainNetParams.Name:
		params = chaincfg.MainNetParams
	case chaincfg.TestNet3Params.Name:
		params = chaincfg.TestNet3Params
	case chaincfg.RegressionNetParams.Name:
		params = chaincfg.RegressionNetParams
	case chaincfg.SimNetParams.Name:
		params = chaincfg.SimNetParams
	case chaincfg.SigNetParams.Name:
		params = chaincfg.SigNetParams
	default:
		params = chaincfg.MainNetParams
	}

	ws.Addresses = make(map[string]struct{}, len(s.Addresses))
	for _, address := range s.Addresses {
		addr, err := btcutil.DecodeAddress(address, &params)
		if err != nil {
			return fmt.Errorf("Failed to parse the passed in address. Error: %v", err)
		}

		ws.Addresses[addr.String()] = struct{}{}
	}
	ws.GapLimit = s.GapLimit

	return nil
}

// WatchOnlyWalletManager is used to keep track of utxos and the utreexo proofs for
// those utxos.
type WatchOnlyWalletManager struct {
	started int32
	stopped int32
	wg      sync.WaitGroup

	config *Config

	// walletLock locks the below wallet related fields.
	walletLock   sync.RWMutex
	wallet       WalletState
	walletConfig WalletConfig

	// scriptHashSubscribers are the subscribers we need to push updates to.
	scriptHashSubscribers []chan interface{}
}

// Getbalance totals up the balance in the utxos it's keeping track of.
func (wm *WatchOnlyWalletManager) Getbalance() int64 {
	totalAmount := int64(0)
	for _, out := range wm.wallet.RelevantUtxos {
		totalAmount += out.LeafData.Amount
	}

	return totalAmount
}

// GetScriptHashBalance returns the balance of the wanted hash for the utxos this
// wallet is tracking.
func (wm *WatchOnlyWalletManager) GetScriptHashBalance(wantHash chainhash.Hash) int64 {
	totalAmount := int64(0)
	for _, out := range wm.wallet.RelevantUtxos {
		gotHash := sha256.Sum256(out.LeafData.PkScript)
		if wantHash == gotHash {
			totalAmount += out.LeafData.Amount
		}
	}

	return totalAmount
}

// GetScriptHash returns a hash of all the utxos that the wantHash address controls.
func (wm *WatchOnlyWalletManager) GetScriptHash(wantHash chainhash.Hash) []byte {
	txDatas := wm.GetHistory(wantHash)

	str := ""
	for _, txData := range txDatas {
		str += fmt.Sprintf("%s:%d:", txData.Hash.String(), int(txData.Height))
	}

	mempoolDatas := wm.GetMempool(wantHash)

	for _, mempoolData := range mempoolDatas {
		str += fmt.Sprintf("%s:%d:", mempoolData.Hash.String(), mempoolData.Height)
	}

	var hash []byte
	if str != "" {
		chainHash := sha256.Sum256([]byte(str))
		hash = chainHash[:]
	}

	return hash
}

// GetUnspent returns the tx data of the unspent txs that this watch only wallet
// is keeping track of.
func (wm *WatchOnlyWalletManager) GetUnspent(wantHash chainhash.Hash) []LeafDataExtras {
	unspents := []LeafDataExtras{}

	for _, txData := range wm.wallet.RelevantUtxos {
		leaf := txData.LeafData
		gotHash := sha256.Sum256(leaf.PkScript)
		if wantHash == gotHash {
			unspents = append(unspents, txData)
		}
	}

	// Sort by the "blockchain ordering" which means to sort by height and then
	// the implied transaction index inside of a block.
	sort.Slice(unspents, func(i, j int) bool {
		if unspents[i].BlockHeight == unspents[j].BlockHeight {
			return unspents[i].BlockIdx < unspents[j].BlockIdx
		} else {
			return unspents[i].BlockHeight < unspents[j].BlockHeight
		}
	})

	return unspents
}

// TxData is all the data for a tx that an electrum server would need access to
// to satisfy the electrum protocol methods. This struct is mainly used to communicate
// between the wallet and the electrum server.
type TxData struct {
	// Hash is the txid of the transaction.
	Hash chainhash.Hash

	// Idx is the output index of the tx.  If we're referring to a txin, the
	// idx is -1.
	Idx int

	// BlockIndex is the index this tx is at the block level.
	BlockIndex int

	// Amount is the value in satoshis a txo is worth.  -1 if we're referring to
	// a txin.
	Amount int64

	// Height is where this tx is confirmed at. -1 if the tx is in the mempool.
	Height int32

	// Fee is the fee paid for this tx.
	Fee int64

	// AllInputsConfirmed of true states that this tx isn't spending an
	// unconfirmed tx.
	AllInputsConfirmed bool
}

// GetHistory returns all the txs that this watch only wallet is keeping track of
// for the given want hash.
func (wm *WatchOnlyWalletManager) GetHistory(wantHash chainhash.Hash) []TxData {
	ret := []TxData{}

	for txHash, txData := range wm.wallet.RelevantTxs {
		for _, txIn := range txData.Tx.TxIn {
			stxoData, found := wm.wallet.RelevantStxos[txIn.PreviousOutPoint]
			if !found {
				continue
			}
			gotHash := sha256.Sum256(stxoData.LeafData.PkScript)
			if gotHash == wantHash {
				ret = append(ret, TxData{
					Hash:       txHash,
					Height:     int32(txData.BlockHeight),
					BlockIndex: txData.BlockIndex},
				)
			}
		}

		for _, txo := range txData.Tx.TxOut {
			gotHash := sha256.Sum256(txo.PkScript)
			if gotHash == wantHash {
				ret = append(ret, TxData{
					Hash:       txHash,
					Height:     int32(txData.BlockHeight),
					BlockIndex: txData.BlockIndex},
				)
			}
		}
	}

	// Sort by the "blockchain ordering" which means to sort by height and then
	// the implied transaction index inside of a block.
	sort.Slice(ret, func(i, j int) bool {
		if ret[i].Height == ret[j].Height {
			return ret[i].BlockIndex < ret[j].BlockIndex
		} else {
			return ret[i].Height < ret[j].Height
		}
	})

	return ret
}

// GetMempool returns all the mempool txs that is relevant to the given want hash.
func (wm *WatchOnlyWalletManager) GetMempool(wantHash chainhash.Hash) []TxData {
	ret := []TxData{}

	for _, tx := range wm.wallet.RelevantMempoolTxs {
		txHash := tx.Tx.Tx.Hash()

		// If all the inputs are confirmed, the height is 0 per the electrum protocol
		// spec. Otherwise it's -1.
		height := int32(-1)
		if tx.Confirmed {
			height = 0
		}

		for _, in := range tx.Tx.Tx.MsgTx().TxIn {
			// If the tx is confirmed, we will have the utxo.
			if tx.Confirmed {
				txData, found := wm.wallet.RelevantUtxos[in.PreviousOutPoint]
				if !found {
					continue
				}
				gotHash := sha256.Sum256(txData.LeafData.PkScript)
				if gotHash == wantHash {
					ret = append(ret, TxData{
						Hash:               *txHash,
						Height:             height,
						Fee:                tx.Tx.Fee,
						Idx:                -1,
						AllInputsConfirmed: tx.Confirmed,
					})
				}
			} else {
				pkScript := tx.Pkscript[in.PreviousOutPoint]
				gotHash := sha256.Sum256(pkScript)
				if gotHash == wantHash {
					ret = append(ret, TxData{
						Hash:               *txHash,
						Height:             height,
						Fee:                tx.Tx.Fee,
						Idx:                -1,
						AllInputsConfirmed: tx.Confirmed,
					})
				}
			}
		}

		for outIdx, out := range tx.Tx.Tx.MsgTx().TxOut {
			gotHash := sha256.Sum256(out.PkScript)
			if gotHash == wantHash {
				ret = append(ret, TxData{
					Hash:               *txHash,
					Height:             height,
					Fee:                tx.Tx.Fee,
					Idx:                outIdx,
					AllInputsConfirmed: tx.Confirmed,
				})
			}
		}
	}

	return ret
}

// GetMempoolBalance returns the summed balance of all the unconfirmed txos relevant
// to the given want hash.
func (wm *WatchOnlyWalletManager) GetMempoolBalance(wantHash chainhash.Hash) int64 {
	var balance int64
	for _, tx := range wm.wallet.RelevantMempoolTxs {
		for _, out := range tx.Tx.Tx.MsgTx().TxOut {
			gotHash := sha256.Sum256(out.PkScript)
			if gotHash == wantHash {
				balance += out.Value
			}
		}
	}

	return balance
}

// GetTXIDFromBlockPos returns the txid of tx given its position in the block.
// Passing in true for getMerkleProof will have the function return a non-nil
// slice of merkle nodes.
func (wm *WatchOnlyWalletManager) GetTXIDFromBlockPos(blockHeight, posInBlock int,
	getMerkleProof bool) (chainhash.Hash, []*chainhash.Hash) {

	for hash, tx := range wm.wallet.RelevantTxs {
		if tx.BlockHeight == blockHeight && posInBlock == tx.BlockIndex {
			return hash, tx.MerkleProof
		}
	}

	return chainhash.Hash{}, nil
}

// GetMerkle returns the merkle proof, block height, and the position of the tx in the block.
func (wm *WatchOnlyWalletManager) GetMerkle(txHash chainhash.Hash) ([]*chainhash.Hash, int, int) {
	txData, found := wm.wallet.RelevantTxs[txHash]
	if !found {
		return nil, 0, 0
	}

	return txData.MerkleProof, txData.BlockHeight, txData.BlockIndex
}

// Get tx returns the tx for the given tx hash. Returns nil if the watch only wallet
// does not have it.
func (wm *WatchOnlyWalletManager) GetTx(txHash chainhash.Hash) *wire.MsgTx {
	tx, found := wm.wallet.RelevantTxs[txHash]
	if found {
		return tx.Tx
	}

	mempoolTx, found := wm.wallet.RelevantMempoolTxs[txHash]
	if found {
		return mempoolTx.Tx.Tx.MsgTx()
	}

	return nil
}

// ProveTx generates a udata that will prove the given tx to another utreexo node.
func (wm *WatchOnlyWalletManager) ProveTx(tx *btcutil.Tx) (*wire.UData, error) {
	targetsToProve := []uint64{}
	leaves := []wire.LeafData{}
	leafHashes := []utreexo.Hash{}

	for _, in := range tx.MsgTx().TxIn {
		outPoint := in.PreviousOutPoint
		txData, found := wm.wallet.RelevantUtxos[outPoint]
		if !found {
			// We move on as the input may be unconfirmed and thus it doesn't
			// have a proof.
			log.Warnf("Didn't find input of %s while verifying tx %s ",
				outPoint.String(), tx.Hash())
			continue
		}
		leaves = append(leaves, txData.LeafData)

		// We're gonna hash it.
		leafHash := txData.LeafData.LeafHash()
		leafHashes = append(leafHashes, leafHash)

		for idx, hash := range wm.wallet.UtreexoLeaves {
			if leafHash == hash {
				target := wm.wallet.UtreexoProof.Targets[idx]
				targetsToProve = append(targetsToProve, target)
			}
		}
	}

	// Extract only the proof needed to prove the targets for this tx from
	// the batched proof we're keeping.
	_, proof, err := utreexo.GetProofSubset(
		wm.wallet.UtreexoProof, wm.wallet.UtreexoLeaves, targetsToProve, wm.wallet.NumLeaves)
	if err != nil {
		return nil, fmt.Errorf("Couldn't grab the utreexo proof for tx "+
			"%s. Error: %v", tx.Hash(), err)
	}
	ud := wire.UData{
		AccProof:  proof,
		LeafDatas: leaves,
	}

	// Verify that the generated proof passes verification.
	err = wm.config.Chain.VerifyUData(&ud, tx.MsgTx().TxIn, false)
	if err != nil {
		return nil, fmt.Errorf("Couldn't prove tx %s. Generated proof "+
			"fails verification. Error: %v", tx.Hash(), err)
	}

	return &ud, nil
}

// scanForScript scans all the addresses and the extended pubkeys for the script passed in.
// Returns true if we have the key for the script and creates the next key in the gap if the
// script belongs to an extended pubkey.
func (wm *WatchOnlyWalletManager) scanForScript(pkScript []byte) (bool, error) {
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkScript, wm.config.ChainParams)
	if err != nil {
		return false, err
	}

	var found bool
	for _, addr := range addrs {
		addrString := addr.String()

		for xkey, addrMap := range wm.wallet.WatchedKeys {
			isChange, f := addrMap[addrString]
			if f {
				found = f
				hdKey, err := hdkeychain.NewKeyFromString(xkey)
				if err != nil {
					return found, err
				}

				err = wm.nextAddress(hdKey, isChange)
				if err != nil {
					return found, err
				}
			}
		}

		_, f := wm.walletConfig.Addresses[addrString]
		if f {
			found = f
		}
	}

	return found, nil
}

// filterBlock filters the passed in block for inputs or outputs that is relevant
// to this wallet. It returns the indexes of the new outputs that is relevant for
// this wallet.
func (wm *WatchOnlyWalletManager) filterBlock(block *btcutil.Block) ([]uint32, [][]byte) {
	_, _, inskip, outskip := blockchain.DedupeBlock(block)

	remembers := []uint32{}
	updates := [][]byte{}

	var txonum, leafNum, inIdx uint32
	for idx, tx := range block.Transactions() {
		delete(wm.wallet.RelevantMempoolTxs, *tx.Hash())

		for _, in := range tx.MsgTx().TxIn {
			if idx == 0 {
				// coinbase can have many inputs
				inIdx += uint32(len(tx.MsgTx().TxIn))
				continue
			}

			// Skip txos on the skip list
			if len(inskip) > 0 && inskip[0] == inIdx {
				inskip = inskip[1:]
				inIdx++
				continue
			}

			txData, found := wm.wallet.RelevantUtxos[in.PreviousOutPoint]
			if !found {
				inIdx++
				continue
			}

			updates = append(updates, txData.LeafData.PkScript)

			// The tx data now belongs in the relevant stxo set.
			wm.wallet.RelevantStxos[in.PreviousOutPoint] = txData

			// Remove the utxo from the relevant utxo set.
			delete(wm.wallet.RelevantUtxos, in.PreviousOutPoint)

			// Add merkle proof.
			merkles := blockchain.BuildMerkleTreeStore(block.Transactions(), false)
			merkles = blockchain.ExtractMerkleBranch(merkles, *tx.Hash())

			relevantTx := RelevantTxData{
				BlockIndex:  idx,
				BlockHeight: int(block.Height()),
				MerkleProof: merkles,
				Tx:          tx.MsgTx(),
			}

			// Explicitly set to nil as we won't be fetching the udata from here.
			relevantTx.Tx.UData = nil
			wm.wallet.RelevantTxs[*tx.Hash()] = relevantTx

			inIdx++
		}

		for outIdx, out := range tx.MsgTx().TxOut {
			// Skip all the OP_RETURNs
			if blockchain.IsUnspendable(out) {
				txonum++
				continue
			}
			// Skip txos on the skip list
			if len(outskip) > 0 && outskip[0] == txonum {
				outskip = outskip[1:]
				txonum++
				continue
			}

			outPoint := wire.OutPoint{
				Hash:  *tx.Hash(),
				Index: uint32(outIdx),
			}

			found, err := wm.scanForScript(out.PkScript)
			if err != nil {
				log.Warnf("Couldn't scan output script in %s:%d: %v",
					tx.Hash(), outIdx, err)
				txonum++
				leafNum++
				continue
			}
			if !found {
				txonum++
				leafNum++
				continue
			}
			remembers = append(remembers, leafNum)

			leaf := wire.LeafData{
				BlockHash:  *block.Hash(),
				OutPoint:   outPoint,
				Amount:     out.Value,
				PkScript:   out.PkScript,
				Height:     block.Height(),
				IsCoinBase: idx == 0,
			}
			wm.wallet.RelevantUtxos[outPoint] = LeafDataExtras{
				BlockIdx:    idx,
				BlockHeight: int(block.Height()),
				LeafData:    leaf,
			}

			updates = append(updates, leaf.PkScript)

			// Add merkle proof.
			merkles := blockchain.BuildMerkleTreeStore(block.Transactions(), false)
			merkles = blockchain.ExtractMerkleBranch(merkles, *tx.Hash())
			wm.wallet.RelevantTxs[*tx.Hash()] = RelevantTxData{
				BlockIndex:  idx,
				BlockHeight: int(block.Height()),
				MerkleProof: merkles,
				Tx:          tx.MsgTx(),
			}

			txonum++
			leafNum++
		}
	}

	return remembers, updates
}

// filterBlockForUndo filters the block to undo that block. The returned indexes are
// of the targets that were deleted in the block that needs to be re-added as this
// watch only wallet controls it.
func (wm *WatchOnlyWalletManager) filterBlockForUndo(block *btcutil.Block) ([]uint64, [][]byte) {
	_, _, inskip, outskip := blockchain.DedupeBlock(block)

	var targetsToProve []uint64
	updates := [][]byte{}

	var txonum, leafIdx, inIdx uint32
	for idx, tx := range block.Transactions() {
		_, found := wm.wallet.RelevantTxs[*tx.Hash()]
		if found {
			delete(wm.wallet.RelevantTxs, *tx.Hash())
		}

		for _, in := range tx.MsgTx().TxIn {
			if idx == 0 {
				// coinbase can have many inputs
				inIdx += uint32(len(tx.MsgTx().TxIn))
				continue
			}

			// Skip txos on the skip list.
			if len(inskip) > 0 && inskip[0] == inIdx {
				inskip = inskip[1:]
				inIdx++
				continue
			}

			// The transaction we're going through doesn't exist now so the inputs
			// are utxos again. Look through all the stxos and make them utxos.
			txData, found := wm.wallet.RelevantStxos[in.PreviousOutPoint]
			if found {
				target := block.MsgBlock().UData.AccProof.Targets[leafIdx]
				targetsToProve = append(targetsToProve, target)

				wm.wallet.RelevantUtxos[in.PreviousOutPoint] = txData

				updates = append(updates, txData.LeafData.PkScript)
			}

			leafIdx++
			inIdx++
		}

		for outIdx, out := range tx.MsgTx().TxOut {
			outPoint := wire.OutPoint{
				Hash:  *tx.Hash(),
				Index: uint32(outIdx),
			}

			// Skip all the OP_RETURNs
			if blockchain.IsUnspendable(out) {
				txonum++
				continue
			}
			// Skip txos on the skip list
			if len(outskip) > 0 && outskip[0] == txonum {
				outskip = outskip[1:]
				txonum++
				continue
			}

			updates = append(updates, out.PkScript)

			// The outpoints that happened in this block no longer exist
			// so we remove them from the relevant utxos.
			delete(wm.wallet.RelevantUtxos, outPoint)

			txonum++
		}
	}

	// Sort before returning.
	sort.Slice(targetsToProve, func(i, j int) bool { return targetsToProve[i] < targetsToProve[j] })

	return targetsToProve, updates
}

// ScriptHashSubscribe will add the given channel to be sent a status update whenever
// a new event has happened in the watch only wallet.
func (wm *WatchOnlyWalletManager) ScriptHashSubscribe(receiveChan chan interface{}) {
	wm.scriptHashSubscribers = append(wm.scriptHashSubscribers, receiveChan)
}

// notifyNewScripts notifies every channel that's subscribed new scripthash updates with
// the new adds and the new spends.
func (wm *WatchOnlyWalletManager) notifyNewScripts(updates [][]byte) {
	for _, updated := range updates {
		scriptHash := sha256.Sum256(updated)
		statusHash := wm.GetScriptHash(scriptHash)

		for _, subscriber := range wm.scriptHashSubscribers {
			subscriber <- StatusUpdate{
				ScriptHash: scriptHash,
				Status:     statusHash,
			}
		}
	}
}

// handleBlockchainNotification is the main workhorse for updating the utreexo proofs
// for the wallet. It handles new blocks as well as reorgs.
func (wm *WatchOnlyWalletManager) handleBlockchainNotification(notification *blockchain.Notification) {
	wm.walletLock.Lock()
	defer wm.walletLock.Unlock()

	switch notification.Type {
	// A block has been accepted into the block chain.
	case blockchain.NTBlockConnected:
		block, ok := notification.Data.(*btcutil.Block)
		if !ok {
			log.Warnf("Chain connected notification is not a block.")
			break
		}

		remembers, updates := wm.filterBlock(block)

		ud := block.MsgBlock().UData
		targets := ud.AccProof.Targets
		adds := block.UtreexoAdds()
		updateData := block.UtreexoUpdateData()

		var err error
		wm.wallet.UtreexoLeaves, err = wm.wallet.UtreexoProof.Update(
			wm.wallet.UtreexoLeaves, adds, targets, remembers, *updateData)
		if err != nil {
			log.Criticalf("Couldn't update utreexo proof for block %s.", block.Hash())
			break
		}
		wm.wallet.BestHash = *block.Hash()
		wm.wallet.NumLeaves = updateData.PrevNumLeaves + uint64(len(adds))

		err = wm.config.Chain.GetUtreexoView().VerifyAccProof(wm.wallet.UtreexoLeaves, &wm.wallet.UtreexoProof)
		if err != nil {
			retErr := fmt.Errorf("wallet proof invalid at block %d, hash %s. Error %v",
				block.Height(), block.Hash().String(), err)
			log.Errorf(retErr.Error())
		}

		// Write the new state to the disk to overwrite the older state.
		err = wm.writeToDisk()
		if err != nil {
			log.Criticalf("Couldn't write to the wallet file after connecting block %s. Error: %v",
				block.Hash().String(), err)
			break
		}

		// Notify of the new utxos and stxos to the subscribers.
		wm.notifyNewScripts(updates)

	// A block has been disconnected from the main block chain.
	case blockchain.NTBlockDisconnected:
		block, ok := notification.Data.(*btcutil.Block)
		if !ok {
			log.Warnf("Chain disconnected notification is not a block.")
			break
		}

		ud := block.MsgBlock().UData
		targets := ud.AccProof.Targets
		updateData := block.UtreexoUpdateData()
		numAdds := uint64(len(block.UtreexoAdds()))
		numLeaves := updateData.PrevNumLeaves + numAdds

		delHashes := make([]utreexo.Hash, 0, len(ud.LeafDatas))
		for _, ld := range ud.LeafDatas {
			delHashes = append(delHashes, ld.LeafHash())
		}

		var err error
		wm.wallet.UtreexoLeaves, err = wm.wallet.UtreexoProof.Undo(numAdds,
			numLeaves, targets, delHashes, wm.wallet.UtreexoLeaves,
			updateData.ToDestroy, ud.AccProof)
		if err != nil {
			log.Criticalf("Couldn't undo utreexo proof for block %s. Error: %v",
				block.Hash(), err)
			break
		}
		targetsToProve, updates := wm.filterBlockForUndo(block)

		hashes, proof, err := utreexo.GetProofSubset(
			ud.AccProof, delHashes, targetsToProve, updateData.PrevNumLeaves)
		if err != nil {
			log.Criticalf("Couldn't grab the utreexo proof for previous "+
				"spends %s. Error: %v", block.Hash(), err)
			break
		}

		wm.wallet.UtreexoLeaves, wm.wallet.UtreexoProof = utreexo.AddProof(
			wm.wallet.UtreexoProof, proof, wm.wallet.UtreexoLeaves,
			hashes, updateData.PrevNumLeaves)

		wm.wallet.NumLeaves = updateData.PrevNumLeaves
		wm.wallet.BestHash = block.MsgBlock().Header.PrevBlock

		// Write the new state to the disk to overwrite the older state.
		err = wm.writeToDisk()
		if err != nil {
			log.Criticalf("Couldn't write to the wallet file after undoing block %s",
				block.Hash().String())
			break
		}

		wm.notifyNewScripts(updates)
	}
}

// MempoolTx is the underlying tx descriptor with data useful for electrum servers.
type MempoolTx struct {
	// Tx is the raw tx along with metadata useful for memepool txs.
	Tx *mempool.TxDesc `json:"tx"`

	// Confirmed states if all the inputs are confirmed or not.
	Confirmed bool `json:"confirmed"`

	// Pkscript is the scripts for the inputs of the tx.
	Pkscript map[wire.OutPoint][]byte `json:"pkscripts"`
}

func (tx MempoolTx) MarshalJSON() ([]byte, error) {
	pkScript := make(map[string][]byte, len(tx.Pkscript))
	for k, v := range tx.Pkscript {
		pkScript[k.String()] = v
	}

	s := struct {
		Tx        *mempool.TxDesc   `json:"tx"`
		Confirmed bool              `json:"confirmed"`
		Pkscript  map[string][]byte `json:"pkscripts"`
	}{
		Tx:        tx.Tx,
		Confirmed: tx.Confirmed,
		Pkscript:  pkScript,
	}

	return json.Marshal(s)
}

func (tx *MempoolTx) UnmarshalJSON(data []byte) error {
	s := struct {
		Tx        *mempool.TxDesc   `json:"tx"`
		Confirmed bool              `json:"confirmed"`
		Pkscript  map[string][]byte `json:"pkscripts"`
	}{}

	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	pkScript := make(map[wire.OutPoint][]byte, len(s.Pkscript))
	for k, v := range s.Pkscript {
		strs := strings.Split(k, ":")
		hash, err := chainhash.NewHashFromStr(strs[0])
		if err != nil {
			return err
		}
		index, err := strconv.ParseUint(strs[1], 10, 32)
		if err != nil {
			return err
		}
		op := wire.OutPoint{Hash: *hash, Index: uint32(index)}
		pkScript[op] = v
	}

	tx.Tx = s.Tx
	tx.Confirmed = s.Confirmed
	tx.Pkscript = pkScript

	return nil
}

// StatusUpdate the two pieces of data needed to alert an electrum client that an address had
// coins spent/received.
type StatusUpdate struct {
	// ScriptHash is the hash of an address. Refer to the electrum protocol doc
	// for more info: electrumx-spesmilo.readthedocs.io/en/latest/protocol-methods.html
	ScriptHash chainhash.Hash

	// Status is the hash of all the txs. This is what's needed for
	// electrum servers to indicate a receive/spend happened to an
	// address.
	Status []byte
}

// NotifyNewTransactions notifies all the subscribers of the passed in txns. This function
// is provided so that the main utreexod server can notify the watch only wallet that there
// are new txns verified and in the mempool.
func (m *WatchOnlyWalletManager) NotifyNewTransactions(txns []*mempool.TxDesc) {
	updates := [][]byte{}

	for _, tx := range txns {
		txHash := tx.Tx.Hash()
		log.Debugf("NotifyNewTransactions: received new tx: %s\n", txHash.String())

		lds := tx.Tx.MsgTx().UData.LeafDatas
		if lds == nil {
			log.Warnf("NotifyNewTransactions: received tx %s with no Udata", txHash.String())
			continue
		}
		if len(lds) != len(tx.Tx.MsgTx().TxIn) {
			log.Warnf("NotifyNewTransactions: length of txIns and LeafDatas differ. "+
				"%d txIns, but %d LeafDatas. TxIns PreviousOutPoints are:\n",
				len(tx.Tx.MsgTx().TxIn), len(lds))
			continue
		}

		prevScripts := make(map[wire.OutPoint][]byte)

		// comfirmed states whether or not all the txIns are confirmed.
		confirmed, relevant := true, false
		for inIdx, txIn := range tx.Tx.MsgTx().TxIn {
			ld := lds[inIdx]
			if !ld.IsUnconfirmed() {
				// This txIn is relevant if it's spending one of our utxos.
				txData, found := m.wallet.RelevantUtxos[txIn.PreviousOutPoint]
				if !found {
					continue
				}
				relevant = true

				updates = append(updates, txData.LeafData.PkScript)
				continue
			}

			confirmed = false

			// Check if this txIn is referencing an unconfirmed tx.
			found := m.config.TxMemPool.HaveTransaction(&txIn.PreviousOutPoint.Hash)
			if !found {
				// Just continue if we don't have the tx.  This may be a relevant
				// tx for us but there's nothing we can do without the pkscript.
				continue
			}

			// The txIn is referencing an unconfirmed tx so grab it and get the txOut.
			tx, err := m.config.TxMemPool.FetchTransaction(&txIn.PreviousOutPoint.Hash)
			if err != nil {
				log.Warnf("NotifyNewTransactions error: %v", err)
				continue
			}
			txOut := tx.MsgTx().TxOut[txIn.PreviousOutPoint.Index]

			// Scan the wallet to see if we control the txOut.
			found, err = m.scanForScript(txOut.PkScript)
			if !found {
				continue
			}
			relevant = true
			prevPkScript := make([]byte, len(txOut.PkScript))
			copy(prevPkScript, txOut.PkScript)

			prevScripts[txIn.PreviousOutPoint] = prevPkScript
			updates = append(updates, prevPkScript)
		}

		for _, txOut := range tx.Tx.MsgTx().TxOut {
			// This txOut is relevant if we control it.
			found, err := m.scanForScript(txOut.PkScript)
			if err != nil {
				log.Warnf("NotifyNewTransactions error: %v", err)
				continue
			}
			if !found {
				continue
			}

			relevant = true
			updates = append(updates, txOut.PkScript)
		}

		if relevant {
			log.Debugf("NotifyNewTransactions: found new relevant tx %s\n", tx.Tx.Hash().String())
			m.wallet.RelevantMempoolTxs[*txHash] = MempoolTx{tx, confirmed, prevScripts}
		}
	}

	m.notifyNewScripts(updates)
}

// deriveNextExKey provides a wrapper function to create a new extended key with the given index.
func (m *WatchOnlyWalletManager) deriveNextExKey(xKey *hdkeychain.ExtendedKey, idx uint32, changeAddress bool) (
	*hdkeychain.ExtendedKey, error) {

	var receiving *hdkeychain.ExtendedKey
	var err error
	if changeAddress {
		receiving, err = xKey.Derive(1)
		if err != nil {
			return nil, err
		}
	} else {
		receiving, err = xKey.Derive(0)
		if err != nil {
			return nil, err
		}
	}

	key, err := receiving.Derive(idx)
	if err != nil {
		// Since there's a chance (even if it's less than 1/2^127), handle
		// the invalid child error by deriving until we don't get an invalid
		// child error.
		idx++
		for err == hdkeychain.ErrInvalidChild {
			key, err = receiving.Derive(idx)
			idx++
		}
		return nil, err
	}

	return key, nil
}

// writeToDisk writes the entire wallet state to disk in json.
func (m *WatchOnlyWalletManager) writeToDisk() error {
	walletBytes, err := json.Marshal(&m.wallet)
	if err != nil {
		return fmt.Errorf("Cannot serialize the wallet state. Error: %v", err)
	}

	configBytes, err := json.Marshal(&m.walletConfig)
	if err != nil {
		return fmt.Errorf("Cannot serialize the wallet state. Error: %v", err)
	}

	basePath := filepath.Join(m.config.DataDir, defaultWalletPath)
	if _, err := os.Stat(basePath); err != nil {
		os.MkdirAll(basePath, os.ModePerm)
	}
	walletConfigName := filepath.Join(basePath, defaultWalletConfigName)
	configFile, err := os.OpenFile(walletConfigName, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("Cannot open file to write watch only wallet. Error: %v", err)
	}
	walletName := filepath.Join(basePath, defaultWalletName)
	walletFile, err := os.OpenFile(walletName, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("Cannot open file to write watch only wallet. Error: %v", err)
	}

	err = configFile.Truncate(0)
	if err != nil {
		return fmt.Errorf("Failed to write to the watch only wallet. Error: %v", err)
	}
	err = walletFile.Truncate(0)
	if err != nil {
		return fmt.Errorf("Failed to write to the watch only wallet. Error: %v", err)
	}

	_, err = configFile.WriteAt(configBytes, 0)
	if err != nil {
		return fmt.Errorf("Failed to write to the watch only wallet. Error: %v", err)
	}

	_, err = walletFile.WriteAt(walletBytes, 0)
	if err != nil {
		return fmt.Errorf("Failed to write to the watch only wallet. Error: %v", err)
	}

	return nil
}

// nextAddress generates a new address based on the stored index and adds it to the
// watch keys to be watched. The dervied address is based on the hd version this wallet
// was created with.
func (wm *WatchOnlyWalletManager) nextAddress(xkey *hdkeychain.ExtendedKey, isChange bool) error {
	xkeyStr := xkey.String()
	lastIdx := uint32(0)
	if isChange {
		lastIdx = wm.wallet.LastInternalIndex[xkeyStr]
		lastIdx++
		wm.wallet.LastInternalIndex[xkeyStr] = lastIdx
	} else {
		lastIdx = wm.wallet.LastExternalIndex[xkeyStr]
		lastIdx++
		wm.wallet.LastExternalIndex[xkeyStr] = lastIdx
	}

	key, err := wm.deriveNextExKey(xkey, lastIdx-1, isChange)
	if err != nil {
		return err
	}

	pubKey, err := key.ECPubKey()
	if err != nil {
		return err
	}

	var addr btcutil.Address
	switch wm.walletConfig.ExtendedKeys[xkeyStr] {
	case HDVersionMainNetBIP0044, HDVersionSimNetBIP0044,
		HDVersionTestNetBIP0044:
		hash := btcutil.Hash160(pubKey.SerializeCompressed())
		addr, err = btcutil.NewAddressPubKeyHash(hash, wm.config.ChainParams)
		if err != nil {
			return err
		}

	case HDVersionMainNetBIP0049, HDVersionTestNetBIP0049:
		hash := btcutil.Hash160(pubKey.SerializeCompressed())
		witnessAddr, err := btcutil.NewAddressWitnessPubKeyHash(hash, &chaincfg.MainNetParams)
		if err != nil {
			return err
		}

		script, err := txscript.PayToAddrScript(witnessAddr)
		if err != nil {
			return err
		}
		addr, err = btcutil.NewAddressScriptHash(script, &chaincfg.MainNetParams)
		if err != nil {
			return err
		}

	case HDVersionMainNetBIP0084, HDVersionTestNetBIP0084:
		hash := btcutil.Hash160(pubKey.SerializeCompressed())
		addr, err = btcutil.NewAddressWitnessPubKeyHash(hash, wm.config.ChainParams)
		if err != nil {
			return err
		}

	default:
		return fmt.Errorf("Unsupported hd version. Wallet is likely corrupted")
	}

	addrMap, found := wm.wallet.WatchedKeys[xkeyStr]
	if found {
		addrMap[addr.String()] = isChange
	} else {
		wm.wallet.WatchedKeys[xkeyStr] = map[string]bool{addr.String(): isChange}
	}

	log.Debugf("Watching addr %s with xpub %s\n", addr.String(), xkeyStr)

	return nil
}

// RegisterExtendedPubkey registers an extended pubkey for the watch only wallet to keep track of.
func (wm *WatchOnlyWalletManager) RegisterExtendedPubkey(extendedKey string, hdVersion *HDVersion) error {
	xkey, err := hdkeychain.NewKeyFromString(extendedKey)
	if err != nil {
		return fmt.Errorf("Failed to parse the passed in key. Error: %v", err)
	}

	if xkey.IsPrivate() {
		return fmt.Errorf("Registering private keys are not supported")
	}

	// Depth of 3 as that's what most wallets will export following BIP0044, BIP0049,
	// BIP0084, and BIP0141.
	// Bitcoind and Electrum follow bip32 and will produce extended pubkeys with depth
	// of 1.
	if xkey.Depth() != 3 && xkey.Depth() != 1 {
		return fmt.Errorf("Received an extended pubkey with depth %d. "+
			"Expected one with a depth of 1 or 3", xkey.Depth())
	}

	_, found := wm.walletConfig.ExtendedKeys[extendedKey]
	if found {
		log.Infof("Extended pubkey: %s is already registered", xkey.String())
		return nil
	}

	wm.wallet.LastInternalIndex[extendedKey] = 0
	wm.wallet.LastExternalIndex[extendedKey] = 0

	var version = HDVersion(binary.BigEndian.Uint32(xkey.Version()))
	if hdVersion != nil {
		version = *hdVersion
	}

	wm.walletConfig.ExtendedKeys[extendedKey] = version

	// Generate addresses to be watched up til the gap limit.
	for i := uint32(0); i < wm.walletConfig.GapLimit; i++ {
		err := wm.nextAddress(xkey, true)
		if err != nil {
			return err
		}

		err = wm.nextAddress(xkey, false)
		if err != nil {
			return err
		}
	}

	log.Infof("Registered extended pubkey: %s:%s", xkey.String(), version.String())

	return nil
}

// RegisterAddress registers an address for the watch only wallet to keep track of.
func (wm *WatchOnlyWalletManager) RegisterAddress(address string) error {
	addr, err := btcutil.DecodeAddress(address, wm.config.ChainParams)
	if err != nil {
		return fmt.Errorf("Failed to parse the passed in address %s. Error: %v", address, err)
	}

	_, exists := wm.walletConfig.Addresses[addr.String()]
	if !exists {
		log.Infof("Registered address: %s", addr.String())
		wm.walletConfig.Addresses[addr.String()] = struct{}{}
	} else {
		log.Infof("Address: %s is already registered", addr.String())
	}

	return nil
}

// GetProof returns a proof that can be used to verify the utreexo leaves.
func (wm *WatchOnlyWalletManager) GetProof() blockchain.ChainTipProof {
	wm.walletLock.RLock()
	defer wm.walletLock.RUnlock()

	return blockchain.ChainTipProof{
		ProvedAtHash: &wm.wallet.BestHash,
		AccProof:     &wm.wallet.UtreexoProof,
		HashesProven: wm.wallet.UtreexoLeaves,
	}
}

// Start starts the watch only wallet manager.
func (m *WatchOnlyWalletManager) Start() {
	// Already started?
	if atomic.AddInt32(&m.started, 1) != 1 {
		return
	}

	log.Infof("Watch only wallet started")
}

// Stop stops the watch only wallet manager.
func (m *WatchOnlyWalletManager) Stop() {
	if atomic.AddInt32(&m.stopped, 1) != 1 {
		log.Warnf("Watch only wallet manager already stopped")
		return
	}

	m.wg.Wait()

	err := m.writeToDisk()
	if err != nil {
		log.Criticalf("Cannot write the wallet state. Error: %v", err)
		return
	}

	log.Info("Watch only wallet stopped")
}

// Config is a configuration struct used to initialize a new WatchOnlyWalletManager.
type Config struct {
	Chain       *blockchain.BlockChain
	TxMemPool   *mempool.TxPool
	ChainParams *chaincfg.Params
	DataDir     string
	GapLimit    uint32
}

// New constructs a new instance of the watch-only wallet manager.
func New(config *Config) (*WatchOnlyWalletManager, error) {
	wm := WatchOnlyWalletManager{
		config: config,
	}

	walletDir := filepath.Join(config.DataDir, defaultWalletPath)

	walletConfig := WalletConfig{
		Net:          config.ChainParams.Name,
		ExtendedKeys: make(map[string]HDVersion),
		Addresses:    make(map[string]struct{}),
		GapLimit:     20, // 20 is the default.
	}

	// Check if the wallet config already exists on disk.
	walletConfigName := filepath.Join(walletDir, defaultWalletConfigName)
	_, err := os.Stat(walletConfigName)
	if err != nil && os.IsNotExist(err) {
	} else if err != nil {
		return nil, err
	} else {
		// If the wallet config already exists on disk, read/load it.
		data, err := os.ReadFile(walletConfigName)
		if err != nil {
			return nil, fmt.Errorf("Couldn't read wallet config at %s. Error: %v",
				walletConfigName, err)
		}

		err = json.Unmarshal(data, &walletConfig)
		if err != nil {
			return nil, fmt.Errorf("Couldn't unmarshal contents from "+
				"wallet at %s. Error: %v", walletConfigName, err)
		}

		// Give a warning if the gaplimit requested is smaller than the gaplimit
		// previously saved.
		if config.GapLimit != 0 && config.GapLimit < walletConfig.GapLimit {
			log.Infof("Gap limit saved in config file is %d but the requested "+
				"gap limit is %d. This smaller gap limit may result in funds "+
				"not showing up", walletConfig.GapLimit, config.GapLimit)

			walletConfig.GapLimit = config.GapLimit
		}
	}
	wm.walletConfig = walletConfig

	wallet := WalletState{
		BestHash:           *config.ChainParams.GenesisHash,
		WatchedKeys:        make(map[string]map[string]bool),
		LastExternalIndex:  make(map[string]uint32),
		LastInternalIndex:  make(map[string]uint32),
		RelevantUtxos:      make(map[wire.OutPoint]LeafDataExtras),
		RelevantStxos:      make(map[wire.OutPoint]LeafDataExtras),
		RelevantTxs:        make(map[chainhash.Hash]RelevantTxData),
		RelevantMempoolTxs: make(map[chainhash.Hash]MempoolTx),
	}
	// Check if the wallet state exists on disk.
	walletName := filepath.Join(walletDir, defaultWalletName)
	_, err = os.Stat(walletName)
	if !os.IsNotExist(err) {
		data, err := os.ReadFile(walletName)
		if err != nil {
			return nil, fmt.Errorf("Couldn't read wallet at %s. Error: %v",
				walletConfigName, err)
		}

		err = json.Unmarshal(data, &wallet)
		if err != nil {
			return nil, fmt.Errorf("Couldn't unmarshal contents from "+
				"wallet at %s. Error: %v", walletName, err)
		}
	}
	wm.wallet = wallet

	// Print out the addresses tracked to the log.
	addrStr := ""
	addrIdx := 0
	for address := range walletConfig.Addresses {
		if len(walletConfig.Addresses)-1 == addrIdx {
			addrStr += address
		} else {
			addrStr += address + ", "
		}
		addrIdx++
	}

	if config.Chain != nil {
		// Subscribe to new blocks/reorged blocks.
		wm.config.Chain.Subscribe(wm.handleBlockchainNotification)
	}

	return &wm, nil
}
