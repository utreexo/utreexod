// Copyright (c) 2022 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package wallet

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

var (
	defaultWalletPath       = "watchonlywallet"
	defaultWalletConfigName = "watchonlywalletconfig.json"
	defaultWalletName       = "watchonlywallet.json"
)

// WalletState keeps track of all the current relevant utxos.  It includes a batched
// utreexo proof of all the relevant utxos.
type WalletState struct {
	BestHash      chainhash.Hash
	RelevantUtxos map[wire.OutPoint]wire.LeafData `json:"relevantutxos"`
	UtreexoLeaves []utreexo.Hash                  `json:"utreexoleaves"`
	UtreexoProof  utreexo.Proof                   `json:"utreexoproof"`
}

func (wp *WalletState) MarshalJSON() ([]byte, error) {
	utxos := make([]wire.LeafData, 0, len(wp.RelevantUtxos))
	for _, v := range wp.RelevantUtxos {
		utxos = append(utxos, v)
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
		RelevantUtxos  []wire.LeafData `json:"relevantutxos"`
		UtreexoLeaves  []string        `json:"utreexoleaves"`
		UtreexoTargets []uint64        `json:"utreexotargets"`
		UtreexoProof   []string        `json:"utreexoproof"`
	}{
		RelevantUtxos:  utxos,
		UtreexoLeaves:  leaves,
		UtreexoTargets: wp.UtreexoProof.Targets,
		UtreexoProof:   proofString,
	}

	return json.Marshal(s)
}

func (wp *WalletState) UnmarshalJSON(data []byte) error {
	s := struct {
		RelevantUtxos  []wire.LeafData `json:"relevantutxos"`
		UtreexoLeaves  []string        `json:"utreexoleaves"`
		UtreexoTargets []uint64        `json:"utreexotargets"`
		UtreexoProof   []string        `json:"utreexoproof"`
	}{}

	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	wp.RelevantUtxos = make(map[wire.OutPoint]wire.LeafData, len(s.RelevantUtxos))
	for _, utxo := range s.RelevantUtxos {
		wp.RelevantUtxos[utxo.OutPoint] = utxo
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
	ExtendedKeys []*hdkeychain.ExtendedKey
	Addresses    map[string]struct{}
	GapLimit     int32
}

func (ws *WalletConfig) MarshalJSON() ([]byte, error) {
	xKeys := make([]string, 0, len(ws.ExtendedKeys))
	for i := range xKeys {
		xKeys[i] = ws.ExtendedKeys[i].String()
	}

	s := struct {
		Net          string              `json:"net"`
		ExtendedKeys []string            `json:"extendedkeys"`
		Addresses    map[string]struct{} `json:"addresses"`
		GapLimit     int32               `json:"gaplimit"`
	}{
		Net:          ws.Net,
		ExtendedKeys: xKeys,
		GapLimit:     ws.GapLimit,
		Addresses:    ws.Addresses,
	}

	return json.Marshal(s)
}

func (ws *WalletConfig) UnmarshalJSON(data []byte) error {
	s := struct {
		Net          string              `json:"net"`
		ExtendedKeys []string            `json:"extendedkeys"`
		Addresses    map[string]struct{} `json:"addresses"`
		GapLimit     int32               `json:"gaplimit"`
	}{}
	err := json.Unmarshal(data, &s)
	if err != nil {
		return err
	}

	ws.Net = s.Net

	for _, extendedKey := range s.ExtendedKeys {
		xkey, err := hdkeychain.NewKeyFromString(extendedKey)
		if err != nil {
			return fmt.Errorf("Failed to parse the passed in key. Error: %v", err)
		}

		ws.ExtendedKeys = append(ws.ExtendedKeys, xkey)
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
	for address := range s.Addresses {
		addr, err := btcutil.DecodeAddress(address, &params)
		if err != nil {
			return fmt.Errorf("Failed to parse the passed in key. Error: %v", err)
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
}

// Getbalance totals up the balance in the utxos it's keeping track of.
func (wm *WatchOnlyWalletManager) Getbalance() int64 {
	totalAmount := int64(0)
	for _, out := range wm.wallet.RelevantUtxos {
		totalAmount += out.Amount
	}

	return totalAmount
}

// filterBlock filters the passed in block for inputs or outputs that is relevant
// to this wallet. It returns the indexes of the new outputs that is relevant for
// this wallet.
func (wm *WatchOnlyWalletManager) filterBlock(block *btcutil.Block) []uint32 {
	_, _, inskip, outskip := blockchain.DedupeBlock(block)

	remembers := []uint32{}

	var txonum uint32
	for idx, tx := range block.Transactions() {
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

			_, addrs, _, err := txscript.ExtractPkScriptAddrs(
				out.PkScript, wm.config.ChainParams,
			)
			if err != nil {
				log.Warnf("Could not parse output script in %s:%d: %v",
					tx.Hash(), outIdx, err)
				continue
			}

			var found bool
			for _, addr := range addrs {
				addrString := addr.String()
				_, f := wm.walletConfig.Addresses[addrString]
				if f {
					found = f
				}
			}
			if !found {
				continue
			}
			remembers = append(remembers, uint32(outIdx))

			leaf := wire.LeafData{
				BlockHash:  *block.Hash(),
				OutPoint:   outPoint,
				Amount:     out.Value,
				PkScript:   out.PkScript,
				Height:     block.Height(),
				IsCoinBase: idx == 0,
			}
			wm.wallet.RelevantUtxos[outPoint] = leaf
		}

		var inIdx uint32
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

			_, found := wm.wallet.RelevantUtxos[in.PreviousOutPoint]
			if !found {
				continue
			}

			delete(wm.wallet.RelevantUtxos, in.PreviousOutPoint)
		}
	}

	return remembers
}

// filterBlockForUndo filters the block to undo that block. The returned indexes are
// of the targets that were deleted in the block that needs to be re-added as this
// watch only wallet controlls it.
func (wm *WatchOnlyWalletManager) filterBlockForUndo(block *btcutil.Block) []uint64 {
	_, _, inskip, outskip := blockchain.DedupeBlock(block)

	var targetsToProve []uint64

	var txonum uint32
	for idx, tx := range block.Transactions() {
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

			_, addrs, _, err := txscript.ExtractPkScriptAddrs(
				out.PkScript, wm.config.ChainParams,
			)
			if err != nil {
				log.Warnf("Could not parse output script in %s:%d: %v",
					tx.Hash(), outIdx, err)
				continue
			}

			for _, addr := range addrs {
				addrString := addr.String()
				_, f := wm.walletConfig.Addresses[addrString]
				if f {
					delete(wm.wallet.RelevantUtxos, outPoint)
				}
			}
		}

		var inIdx uint32
		var leafIdx int
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

			target := block.MsgBlock().UData.AccProof.Targets[leafIdx]
			targetsToProve = append(targetsToProve, target)

			_, found := wm.wallet.RelevantUtxos[in.PreviousOutPoint]
			if !found {
				leafIdx++
				continue
			}
			ld := block.MsgBlock().UData.LeafDatas[leafIdx]

			wm.wallet.RelevantUtxos[in.PreviousOutPoint] = ld
			leafIdx++
		}
	}

	return targetsToProve
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

		remembers := wm.filterBlock(block)

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
		numLeaves := updateData.PrevNumLeaves

		delHashes := make([]utreexo.Hash, 0, len(ud.LeafDatas))
		for _, ld := range ud.LeafDatas {
			delHashes = append(delHashes, ld.LeafHash())
		}

		var err error
		wm.wallet.UtreexoLeaves, err = wm.wallet.UtreexoProof.Undo(numAdds,
			numLeaves, targets, delHashes, wm.wallet.UtreexoLeaves,
			updateData.ToDestroy, ud.AccProof)
		if err != nil {
			log.Criticalf("Couldn't undo utreexo proof for block %s.", block.Hash())
			break
		}

		targetsToProve := wm.filterBlockForUndo(block)

		hashes, proof, err := utreexo.GetProofSubset(
			ud.AccProof, delHashes, targetsToProve, updateData.PrevNumLeaves)
		if err != nil {
			log.Criticalf("Couldn't grab the utreexo proof for previous "+
				"spends %s.", block.Hash())
			break
		}

		wm.wallet.UtreexoLeaves, wm.wallet.UtreexoProof = utreexo.AddProof(
			wm.wallet.UtreexoProof, proof, wm.wallet.UtreexoLeaves,
			hashes, updateData.PrevNumLeaves)

		// Write the new state to the disk to overwrite the older state.
		err = wm.writeToDisk()
		if err != nil {
			log.Criticalf("Couldn't write to the wallet file after undoing block %s",
				block.Hash().String())
			break
		}
	}
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

// RegisterExtendedPubkey registers an extended pubkey for the watch only wallet to keep track of.
func (wm *WatchOnlyWalletManager) RegisterExtendedPubkey(extendedKey string) error {
	xkey, err := hdkeychain.NewKeyFromString(extendedKey)
	if err != nil {
		return fmt.Errorf("Failed to parse the passed in key. Error: %v", err)
	}

	if xkey.IsPrivate() {
		return fmt.Errorf("Registering private keys are not supported")
	}

	wm.walletConfig.ExtendedKeys = append(wm.walletConfig.ExtendedKeys, xkey)

	return nil
}

// RegisterAddress registers an address for the watch only wallet to keep track of.
func (wm *WatchOnlyWalletManager) RegisterAddress(address string) error {
	addr, err := btcutil.DecodeAddress(address, wm.config.ChainParams)
	if err != nil {
		return fmt.Errorf("Failed to parse the passed in address. Error: %v", err)
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
	GapLimit    int32
}

// New constructs a new instance of the watch-only wallet manager.
func New(config *Config) (*WatchOnlyWalletManager, error) {
	wm := WatchOnlyWalletManager{
		config: config,
	}

	walletDir := filepath.Join(config.DataDir, defaultWalletPath)

	walletConfig := WalletConfig{
		Net:       config.ChainParams.Name,
		Addresses: make(map[string]struct{}),
		GapLimit:  20,
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
		BestHash:      *config.ChainParams.GenesisHash,
		RelevantUtxos: make(map[wire.OutPoint]wire.LeafData),
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
	log.Infof("Keeping track of these addresses: %s", addrStr)

	// Subscribe to new blocks/reorged blocks.
	wm.config.Chain.Subscribe(wm.handleBlockchainNotification)

	return &wm, nil
}
