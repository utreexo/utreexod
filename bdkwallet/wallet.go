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

var walletFactory WalletFactory

func factory() (WalletFactory, error) {
	if walletFactory == nil {
		return nil, ErrNoBDK
	}
	return walletFactory, nil
}

type WalletFactory interface {
	Create(dbPath string, chainParams *chaincfg.Params) (Wallet, error)
	Load(dbPath string) (Wallet, error)
}

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
	Immature         btcutil.Amount
	TrustedPending   btcutil.Amount
	UntrustedPending btcutil.Amount
	Confirmed        btcutil.Amount
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
	Height uint
	Hash   chainhash.Hash
}

// Recipient specifies the intended amount and destination address for a transaction output.
type Recipient struct {
	Amount  btcutil.Amount
	Address btcutil.Address
}

// TxInfo is information on a given transaction.
type TxInfo struct {
	Txid          chainhash.Hash
	Tx            btcutil.Tx
	Spent         btcutil.Amount
	Received      btcutil.Amount
	Confirmations *uint32
}

// UtxoInfo is information on a given transaction.
type UTXOInfo struct {
	Txid            chainhash.Hash
	Vout            uint
	Amount          btcutil.Amount
	ScriptPubKey    []byte
	IsChange        bool
	DerivationIndex uint
	Confirmations   *uint32
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
