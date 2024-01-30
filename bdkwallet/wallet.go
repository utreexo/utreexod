package bdkwallet

import (
	"bytes"
	"errors"

	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/mempool"
	"github.com/utreexo/utreexod/wire"
)

var (
	ErrNoRecipient = errors.New("must have atleast one recipient")
	ErrNoBDK       = errors.New("utreexod must be built with the 'bdkwallet' tag to enable the BDK wallet")
)

// walletFactory is nil unless we build with the 'bdkwallet' build tag.
var walletFactory WalletFactory

// factory returns the wallet factory (if it exists). Otherwise, an error will be returned.
func factory() (WalletFactory, error) {
	if walletFactory == nil {
		return nil, ErrNoBDK
	}
	return walletFactory, nil
}

// WalletFactory creates wallets.
type WalletFactory interface {
	Create(dbPath string, chainParams *chaincfg.Params) (Wallet, error)
	Load(dbPath string) (Wallet, error)
}

// Wallet tracks addresses and transactions sending/receiving to/from those addresses. The wallet is
// updated by incoming blocks and new mempool transactions.
type Wallet interface {
	UnusedAddress() (uint, btcutil.Address, error)
	FreshAddress() (uint, btcutil.Address, error)
	PeekAddress(index uint32) (uint, btcutil.Address, error)
	Balance() Balance
	RecentBlocks(count uint32) []BlockId
	ApplyBlock(block *btcutil.Block) error
	ApplyMempoolTransactions(txns []*mempool.TxDesc) error
	CreateTx(feerate float32, recipients []Recipient) ([]byte, error)
	MnemonicWords() []string
	Transactions() []TxInfo
	UTXOs() []UTXOInfo
}

// Balance in satoshis.
type Balance struct {
	Immature         btcutil.Amount // immature coinbase balance
	TrustedPending   btcutil.Amount // unconfirmed balance that is part of our change keychain
	UntrustedPending btcutil.Amount // unconfirmed balance that is part of our public keychain
	Confirmed        btcutil.Amount // confirmed balance
}

// TrustedSpendable are funds that are safe to spend.
func (b *Balance) TrustedSpendable() btcutil.Amount {
	return b.Confirmed + b.TrustedPending
}

// Total is the total funds of the wallet.
func (b *Balance) Total() btcutil.Amount {
	return b.Immature + b.TrustedPending + b.UntrustedPending + b.Confirmed
}

// BlockId consists of a block height and a block hash. This identifies a block.
type BlockId struct {
	Height uint           // block height
	Hash   chainhash.Hash // block hash
}

// Recipient specifies the intended amount and destination address for a transaction output.
type Recipient struct {
	Amount  btcutil.Amount // amount to send
	Address string         // recipient address to send to (in human-readable form)
}

// TxInfo is information on a given transaction.
type TxInfo struct {
	Txid          chainhash.Hash
	Tx            btcutil.Tx
	Spent         btcutil.Amount // sum of owned inputs
	Received      btcutil.Amount // sum of owned outputs
	Confirmations uint           // number of confirmations for this tx
}

// UtxoInfo is information on a given transaction.
type UTXOInfo struct {
	Txid            chainhash.Hash
	Vout            uint
	Amount          btcutil.Amount
	ScriptPubKey    []byte
	IsChange        bool
	DerivationIndex uint
	Confirmations   uint // number of confirmations for this utxo
}

func hashFromBytes(b []byte) chainhash.Hash {
	return *(*[32]byte)(b)
}

func txFromBytes(b []byte) btcutil.Tx {
	var msgTx wire.MsgTx
	if err := msgTx.BtcDecode(bytes.NewReader(b), wire.FeeFilterVersion, wire.WitnessEncoding); err != nil {
		panic("must decode tx consensus bytes from rust")
	}
	return *btcutil.NewTx(&msgTx)
}

func uintPointerFromUint32Pointer(v *uint32) *uint {
	if v == nil {
		return nil
	}

	v2 := uint(*v)
	return &v2
}
