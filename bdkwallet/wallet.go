package bdkwallet

//#cgo LDFLAGS: ./target/release/libbdkgo.a -ldl -lm
import "C"

import (
	"bytes"
	"errors"

	"github.com/utreexo/utreexod/bdkwallet/bdkgo"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/mempool"
	"github.com/utreexo/utreexod/wire"
)

var ErrNoRecipient = errors.New("must have atleast one recipient")

// Balance in satoshis.
type Balance struct {
	bdkgo.Balance
}

// TrustedSpendable are funds that are safe to spend.
func (b *Balance) TrustedSpendable() uint64 {
	return b.Confirmed + b.TrustedPending
}

// Total is the total funds of the wallet.
func (b *Balance) Total() uint64 {
	return b.Immature + b.TrustedPending + b.UntrustedPending + b.Confirmed
}

// BlockId consists of a block height and a block hash. This identifies a block.
type BlockId struct {
	bdkgo.BlockId
}

// Height gets the block height.
func (id *BlockId) Height() uint32 {
	return id.BlockId.Height
}

// Hash gets the block hash.
func (id *BlockId) Hash() chainhash.Hash {
	return *(*[32]byte)(id.BlockId.Hash)
}

// Recipient specifies the intended amount and destination address for a transaction output.
type Recipient struct {
	Amount  btcutil.Amount
	Address btcutil.Address
}

// TxInfo is information on a given transaction.
type TxInfo struct{ bdkgo.TxInfo }

func (tx *TxInfo) Txid() chainhash.Hash {
	return *(*[32]byte)(tx.TxInfo.Txid)
}

func (tx *TxInfo) Tx() btcutil.Tx {
	var msgTx wire.MsgTx
	if err := msgTx.BtcDecode(bytes.NewReader(tx.TxInfo.Tx), wire.FeeFilterVersion, wire.WitnessEncoding); err != nil {
		panic("Must decode tx from rust.")
	}
	return *btcutil.NewTx(&msgTx)
}

// UtxoInfo is information on a given transaction.
type UtxoInfo struct{ bdkgo.UtxoInfo }

func (utxo *UtxoInfo) Txid() chainhash.Hash {
	return *(*[32]byte)(utxo.UtxoInfo.Txid)
}

// Wallet is a BDK wallet.
type Wallet struct {
	inner bdkgo.Wallet
}

// Create creates a fresh new wallet.
func Create(dbPath string, chainParams *chaincfg.Params) (*Wallet, error) {
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
	return &Wallet{*inner}, nil
}

// Load loads an existing wallet from file.
func Load(dbPath string) (*Wallet, error) {
	inner, err := bdkgo.WalletLoad(dbPath)
	if err != nil {
		return nil, err
	}

	// This increments the reference count of the Arc pointer in rust. We are
	// doing this due to a bug with uniffi-bindgen-go's generated code
	// decrementing this count too aggressively.
	inner.IncrementReferenceCounter()
	return &Wallet{*inner}, nil
}

// UnusedAddress returns the earliest address which have not received any funds.
func (w *Wallet) UnusedAddress() (uint32, btcutil.Address, error) {
	info, err := w.inner.LastUnusedAddress()
	if err != nil {
		return info.Index, nil, err
	}
	addr, err := btcutil.DecodeAddress(info.Address, nil)
	if err != nil {
		return info.Index, nil, err
	}
	return info.Index, addr, nil
}

// FreshAddress always returns a new address. This means it always increments
// the last derivation index even though the previous derivation indexes have
// not received funds.
func (w *Wallet) FreshAddress() (uint32, btcutil.Address, error) {
	info, err := w.inner.FreshAddress()
	if err != nil {
		return info.Index, nil, err
	}
	addr, err := btcutil.DecodeAddress(info.Address, nil)
	if err != nil {
		return info.Index, nil, err
	}
	return info.Index, addr, nil
}

// PeekAddress previews the address at the derivation index. This does not
// increment the last revealed index.
func (w *Wallet) PeekAddress(index uint32) (uint32, btcutil.Address, error) {
	info, err := w.inner.PeekAddress(index)
	if err != nil {
		return info.Index, nil, err
	}
	addr, err := btcutil.DecodeAddress(info.Address, nil)
	if err != nil {
		return info.Index, nil, err
	}
	return info.Index, addr, nil
}

// Balance returns the balance of the wallet.
func (w *Wallet) Balance() Balance {
	return Balance{w.inner.Balance()}
}

// RecentBlocks returns the most recent blocks
func (w *Wallet) RecentBlocks(count uint32) []BlockId {
	generatedCodeBlocks := w.inner.RecentBlocks(count)
	out := make([]BlockId, 0, len(generatedCodeBlocks))
	for _, block := range generatedCodeBlocks {
		out = append(out, BlockId{block})
	}
	return out
}

// ApplyBlock updates the wallet with the given block.
func (w *Wallet) ApplyBlock(block *btcutil.Block) error {
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
		txid := *(*[32]byte)(genTxid)
		log.Infof("Found relevant tx %v in block %v:%v.", txid, bheight, bhash)
	}
	return nil
}

// ApplyMempoolTransactions updates the wallet with the given mempool transactions.
func (w *Wallet) ApplyMempoolTransactions(txns []*mempool.TxDesc) error {
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
func (w *Wallet) CreateTx(feerate float32, recipients []Recipient) ([]byte, error) {
	genRecipients := make([]bdkgo.Recipient, 0, len(recipients))
	for _, r := range recipients {
		genRecipients = append(genRecipients, bdkgo.Recipient{
			ScriptPubkey: r.Address.ScriptAddress(),
			Amount:       uint64(r.Amount),
		})
	}
	return w.inner.CreateTx(feerate, genRecipients)
}

// MnemonicWords returns the mnemonic words to backup the wallet.
func (w *Wallet) MnemonicWords() []string {
	return w.inner.MnemonicWords()
}

// Transactions returns the list of wallet transactions.
func (w *Wallet) Transactions() []TxInfo {
	genOut := w.inner.Transactions()
	out := make([]TxInfo, 0, len(genOut))
	for _, info := range genOut {
		out = append(out, TxInfo{info})
	}
	return out
}

// Utxos returns the list of wallet UTXOs.
func (w *Wallet) Utxos() []UtxoInfo {
	genOut := w.inner.Utxos()
	out := make([]UtxoInfo, 0, len(genOut))
	for _, info := range genOut {
		out = append(out, UtxoInfo{info})
	}
	return out
}
