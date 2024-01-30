//go:build bdkwallet

package bdkwallet

//#cgo LDFLAGS: ./target/release/libbdkgo.a -ldl -lm
import "C"

import (
	"bytes"

	"github.com/utreexo/utreexod/bdkwallet/bdkgo"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/mempool"
	"github.com/utreexo/utreexod/wire"
)

func init() {
	walletFactory = &BDKWalletFactory{}
}

type BDKWalletFactory struct{}

func (*BDKWalletFactory) Create(dbPath string, chainParams *chaincfg.Params) (Wallet, error) {
	// used for the address format
	// this is parsed as `bitcoin::Network` in rust
	// supported strings: bitcoin, testnet, signet, regtest
	// https://docs.rs/bitcoin/latest/bitcoin/network/enum.Network.html
	network := chainParams.Name
	log.Infof("Creating wallet with network: %v", network)
	switch network {
	case "mainnet":
		network = "bitcoin"
	case "testnet3":
		network = "testnet"
	}

	genesisHash := chainParams.GenesisHash.CloneBytes()

	inner, err := bdkgo.WalletCreateNew(dbPath, network, genesisHash)
	if err != nil {
		return nil, err
	}

	// This increments the reference count of the Arc pointer in rust. We are
	// doing this due to a bug with uniffi-bindgen-go's generated code
	// decrementing this count too aggressively.
	inner.IncrementReferenceCounter()
	return &BDKWallet{*inner}, nil
}

func (*BDKWalletFactory) Load(dbPath string) (Wallet, error) {
	inner, err := bdkgo.WalletLoad(dbPath)
	if err != nil {
		return nil, err
	}

	// This increments the reference count of the Arc pointer in rust. We are
	// doing this due to a bug with uniffi-bindgen-go's generated code
	// decrementing this count too aggressively.
	inner.IncrementReferenceCounter()
	return &BDKWallet{*inner}, nil
}

// Wallet is a BDK wallet.
type BDKWallet struct {
	inner bdkgo.Wallet
}

// UnusedAddress returns the earliest address which have not received any funds.
func (w *BDKWallet) UnusedAddress() (uint, btcutil.Address, error) {
	info, err := w.inner.LastUnusedAddress()
	if err != nil {
		return uint(info.Index), nil, err
	}
	addr, err := btcutil.DecodeAddress(info.Address, nil)
	if err != nil {
		return uint(info.Index), nil, err
	}
	return uint(info.Index), addr, nil
}

// FreshAddress always returns a new address. This means it always increments
// the last derivation index even though the previous derivation indexes have
// not received funds.
func (w *BDKWallet) FreshAddress() (uint, btcutil.Address, error) {
	info, err := w.inner.FreshAddress()
	if err != nil {
		return uint(info.Index), nil, err
	}
	addr, err := btcutil.DecodeAddress(info.Address, nil)
	if err != nil {
		return uint(info.Index), nil, err
	}
	return uint(info.Index), addr, nil
}

// PeekAddress previews the address at the derivation index. This does not
// increment the last revealed index.
func (w *BDKWallet) PeekAddress(index uint32) (uint, btcutil.Address, error) {
	info, err := w.inner.PeekAddress(uint32(index))
	if err != nil {
		return uint(info.Index), nil, err
	}
	addr, err := btcutil.DecodeAddress(info.Address, nil)
	if err != nil {
		return uint(info.Index), nil, err
	}
	return uint(info.Index), addr, nil
}

// Balance returns the balance of the wallet.
func (w *BDKWallet) Balance() Balance {
	balance := w.inner.Balance()
	return Balance{
		Immature:         btcutil.Amount(balance.Immature),
		TrustedPending:   btcutil.Amount(balance.TrustedPending),
		UntrustedPending: btcutil.Amount(balance.UntrustedPending),
		Confirmed:        btcutil.Amount(balance.Confirmed),
	}
}

// RecentBlocks returns the most recent blocks
func (w *BDKWallet) RecentBlocks(count uint32) []BlockId {
	generatedCodeBlocks := w.inner.RecentBlocks(uint32(count))
	out := make([]BlockId, 0, len(generatedCodeBlocks))
	for _, block := range generatedCodeBlocks {
		out = append(out, BlockId{
			Height: uint(block.Height),
			Hash:   hashFromBytes(block.Hash),
		})
	}
	return out
}

// ApplyBlock updates the wallet with the given block.
func (w *BDKWallet) ApplyBlock(block *btcutil.Block) error {
	var b bytes.Buffer
	if err := block.MsgBlock().BtcEncode(&b, wire.FeeFilterVersion, wire.WitnessEncoding); err != nil {
		return err
	}
	bheight := uint32(block.Height())
	res, err := w.inner.ApplyBlock(bheight, b.Bytes())
	if err != nil {
		return err
	}
	bhash := block.Hash()
	for _, genTxid := range res.RelevantTxids {
		txid := hashFromBytes(genTxid)
		log.Infof("Found relevant tx %v in block %v:%v.", txid, bheight, bhash)
	}
	return nil
}

// ApplyMempoolTransactions updates the wallet with the given mempool transactions.
func (w *BDKWallet) ApplyMempoolTransactions(txns []*mempool.TxDesc) error {
	if len(txns) == 0 {
		return nil
	}
	genTxns := make([]bdkgo.MempoolTx, 0, len(txns))
	for _, tx := range txns {
		var txb bytes.Buffer
		if err := tx.Tx.MsgTx().BtcEncode(&txb, wire.FeeFilterVersion, wire.WitnessEncoding); err != nil {
			return err
		}
		genTxns = append(genTxns, bdkgo.MempoolTx{
			Tx:        txb.Bytes(),
			AddedUnix: uint64(tx.Added.Unix()),
		})
	}
	res, err := w.inner.ApplyMempool(genTxns)
	if err != nil {
		return err
	}
	for _, genTxid := range res.RelevantTxids {
		txid := *(*[32]byte)(genTxid)
		log.Infof("Found relevant tx %v in mempool.", txid)
	}
	return nil
}

// CreateTx creates and signs a transaction spending from the wallet.
func (w *BDKWallet) CreateTx(feerate float32, recipients []Recipient) ([]byte, error) {
	genRecipients := make([]bdkgo.Recipient, 0, len(recipients))
	for _, r := range recipients {
		genRecipients = append(genRecipients, bdkgo.Recipient{
			Address: r.Address,
			Amount:  uint64(r.Amount),
		})
	}
	return w.inner.CreateTx(feerate, genRecipients)
}

// MnemonicWords returns the mnemonic words to backup the wallet.
func (w *BDKWallet) MnemonicWords() []string {
	return w.inner.MnemonicWords()
}

// Transactions returns the list of wallet transactions.
func (w *BDKWallet) Transactions() []TxInfo {
	genOut := w.inner.Transactions()
	out := make([]TxInfo, 0, len(genOut))
	for _, info := range genOut {
		out = append(out, TxInfo{
			Txid:          hashFromBytes(info.Txid),
			Tx:            txFromBytes(info.Tx),
			Spent:         btcutil.Amount(info.Spent),
			Received:      btcutil.Amount(info.Received),
			Confirmations: uint(info.Confirmations),
		})
	}
	return out
}

// Utxos returns the list of wallet UTXOs.
func (w *BDKWallet) UTXOs() []UTXOInfo {
	genOut := w.inner.Utxos()
	out := make([]UTXOInfo, 0, len(genOut))
	for _, info := range genOut {
		out = append(out, UTXOInfo{
			Txid:            hashFromBytes(info.Txid),
			Vout:            uint(info.Vout),
			Amount:          btcutil.Amount(info.Amount),
			ScriptPubKey:    info.ScriptPubkey,
			IsChange:        info.IsChange,
			DerivationIndex: uint(info.DerivationIndex),
			Confirmations:   uint(info.Confirmations),
		})
	}
	return out
}
