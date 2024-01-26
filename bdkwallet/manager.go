package bdkwallet

import (
	"os"
	"path/filepath"

	"github.com/utreexo/utreexod/blockchain"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/mempool"
)

var defaultWalletPath = "bdkwallet"
var defaultWalletFileName = "default.dat"

// ManagerConfig is a configuration struct used to
type ManagerConfig struct {
	Chain       *blockchain.BlockChain
	TxMemPool   *mempool.TxPool
	ChainParams *chaincfg.Params
	DataDir     string
}

type Manager struct {
	config ManagerConfig
	wallet Wallet // wallet does not need a mutex as it's done in Rust
}

func NewManager(config ManagerConfig) (*Manager, error) {
	log.Info("Starting the BDK wallet manager.")
	walletDir := filepath.Join(config.DataDir, defaultWalletPath)
	if _, err := os.Stat(walletDir); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		os.MkdirAll(walletDir, os.ModePerm)
	}

	dbPath := filepath.Join(walletDir, defaultWalletFileName)
	var wallet *Wallet
	if _, err := os.Stat(dbPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if wallet, err = Create(dbPath, config.ChainParams); err != nil {
			return nil, err
		}
	} else {
		if wallet, err = Load(dbPath); err != nil {
			return nil, err
		}
	}

	m := Manager{
		config: config,
		wallet: *wallet,
	}
	if config.Chain != nil {
		// Subscribe to new blocks/reorged blocks.
		config.Chain.Subscribe(m.handleBlockchainNotification)
	}

	return &m, nil
}

func (m *Manager) NotifyNewTransactions(txns []*mempool.TxDesc) {
	if err := m.wallet.ApplyMempoolTransactions(txns); err != nil {
		log.Errorf("Failed to apply mempool txs to the wallet. %v", err)
	}
}

func (m *Manager) handleBlockchainNotification(notification *blockchain.Notification) {
	switch notification.Type {
	// A block has been accepted into the block chain.
	case blockchain.NTBlockConnected:
		block, ok := notification.Data.(*btcutil.Block)
		if !ok {
			log.Warnf("Chain connected notification is not a block.")
			return
		}
		err := m.wallet.ApplyBlock(block)
		if err != nil {
			log.Criticalf("Couldn't apply block to the wallet. %v", err)
		}
	}
}
