package bdkwallet

//#cgo LDFLAGS: ./target/release/libbdkgo.a -ldl -lm
import "C"

import (
	"bytes"

	"github.com/utreexo/utreexod/bdkwallet/bdkgo"
	"github.com/utreexo/utreexod/btcutil"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/wire"
)

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

type BlockId struct {
	bdkgo.BlockId
}

func (id *BlockId) Height() uint32 {
	return id.BlockId.Height
}

func (id *BlockId) Hash() chainhash.Hash {
	return *(*[32]byte)(id.BlockId.Hash)
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
	return &Wallet{*inner}, nil
}

// Load loads an existing wallet from file.
func Load(dbPath string) (*Wallet, error) {
	inner, err := bdkgo.WalletLoad(dbPath)
	if err != nil {
		return nil, err
	}
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
	log.Infof("Got block [%v:%v]", block.Height(), block.Hash())
	var b bytes.Buffer
	if err := block.MsgBlock().BtcEncode(&b, wire.FeeFilterVersion, wire.WitnessEncoding); err != nil {
		return err
	}

	bb := bdkgo.NewBlock(b.Bytes()[:b.Len()])
	bheight := uint32(block.Height())
	bhash, err := chainhash.NewHash(bb.Hash())
	if err != nil {
		return err
	}
	log.Infof("Applying [%v:%v] from %v bytes", bheight, bhash, b.Len())
	err = w.inner.ApplyBlock(bheight, bb)
	log.Info("Applied block!")
	bb = nil
	log.Info("Block deleted?")
	return err
}
