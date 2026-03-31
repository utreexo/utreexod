// Copyright (c) 2014-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package chaincfg

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/wire"
)

// These variables are the chain proof-of-work limit parameters for each default
// network.
var (
	// bigOne is 1 represented as a big.Int.  It is defined here to avoid
	// the overhead of creating it multiple times.
	bigOne = big.NewInt(1)

	// mainPowLimit is the highest proof of work value a Bitcoin block can
	// have for the main network.  It is the value 2^224 - 1.
	mainPowLimit = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 224), bigOne)

	// regressionPowLimit is the highest proof of work value a Bitcoin block
	// can have for the regression test network.  It is the value 2^255 - 1.
	regressionPowLimit = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 255), bigOne)

	// testNet3PowLimit is the highest proof of work value a Bitcoin block
	// can have for the test network (version 3).  It is the value
	// 2^224 - 1.
	testNet3PowLimit = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 224), bigOne)

	// simNetPowLimit is the highest proof of work value a Bitcoin block
	// can have for the simulation test network.  It is the value 2^255 - 1.
	simNetPowLimit = new(big.Int).Sub(new(big.Int).Lsh(bigOne, 255), bigOne)

	// sigNetPowLimit is the highest proof of work value a bitcoin block can
	// have for the signet test network. It is the value 0x0377ae << 216.
	sigNetPowLimit = new(big.Int).Lsh(new(big.Int).SetInt64(0x0377ae), 216)

	// DefaultSignetChallenge is the byte representation of the signet
	// challenge for the default (public, Taproot enabled) signet network.
	// This is the binary equivalent of the bitcoin script
	//  1 03ad5e0edad18cb1f0fc0d28a3d4f1f3e445640337489abb10404f2d1e086be430
	//  0359ef5021964fe22d6f8e05b2463c9540ce96883fe3b278760f048f5189f2e6c4 2
	//  OP_CHECKMULTISIG
	DefaultSignetChallenge, _ = hex.DecodeString(
		"512103ad5e0edad18cb1f0fc0d28a3d4f1f3e445640337489abb10404f2d" +
			"1e086be430210359ef5021964fe22d6f8e05b2463c9540ce9688" +
			"3fe3b278760f048f5189f2e6c452ae",
	)

	// DefaultSignetDNSSeeds is the list of seed nodes for the default
	// (public, Taproot enabled) signet network.
	DefaultSignetDNSSeeds = []DNSSeed{
		{"seed.dlsouza.lol.", true},                 // Davidson Souza, supports filtering, including utreexo (1 << 24)
		{"signetseed.calvinkim.info.", true},        // Calvin Kim, supports filtering, including utreexo (1 << 24)
		{"signetmanualseed.calvinkim.info.", false}, // Only returns utreexo peers.
		{"178.128.221.177", false},
		{"2a01:7c8:d005:390::5", false},
		{"v7ajjeirttkbnt32wpy3c6w3emwnfr3fkla7hpxcfokr3ysd3kqtzmqd.onion:38333", false},
	}
)

// Checkpoint identifies a known good point in the block chain.  Using
// checkpoints allows a few optimizations for old blocks during initial download
// and also prevents forks from old blocks.
//
// Each checkpoint is selected based upon several factors.  See the
// documentation for blockchain.IsCheckpointCandidate for details on the
// selection criteria.
type Checkpoint struct {
	Height int32
	Hash   *chainhash.Hash
}

// AssumeUtreexo is all the information that's needed for a node to start off from
// a given hardcoded block.
type AssumeUtreexo struct {
	BlockHash   *chainhash.Hash // The hash of the block.
	Roots       []utreexo.Hash  // The utreexo roots at that block.
	BlockHeight int32           // The height of the block.
	Bits        uint32          // The difficulty bits of the block.
	BlockSize   uint64          // The size of the block.
	BlockWeight uint64          // The weight of the block.
	NumTxns     uint64          // The number of txns in the block.
	TotalTxns   uint64          // The total number of txns in the chain.
	NumLeaves   uint64          // The number of leaves at that block.
	MedianTime  time.Time       // Median time as per CalcPastMedianTime.
}

// TTLState is the pre-committed ttl roots that allows nodes during ibd
// to verifies the received ttls.
type TTLState struct {
	Stump []utreexo.Stump
}

// DNSSeed identifies a DNS seed.
type DNSSeed struct {
	// Host defines the hostname of the seed.
	Host string

	// HasFiltering defines whether the seed supports filtering
	// by service flags (wire.ServiceFlag).
	HasFiltering bool
}

// ConsensusDeployment defines details related to a specific consensus rule
// change that is voted in.  This is part of BIP0009.
type ConsensusDeployment struct {
	// BitNumber defines the specific bit number within the block version
	// this particular soft-fork deployment refers to.
	BitNumber uint8

	// MinActivationHeight is an optional field that when set (default
	// value being zero), modifies the traditional BIP 9 state machine by
	// only transitioning from LockedIn to Active once the block height is
	// greater than (or equal to) thus specified height.
	MinActivationHeight uint32

	// CustomActivationThreshold if set (non-zero), will _override_ the
	// existing RuleChangeActivationThreshold value set at the
	// network/chain level. This value divided by the active
	// MinerConfirmationWindow denotes the threshold required for
	// activation. A value of 1815 block denotes a 90% threshold.
	CustomActivationThreshold uint32

	// DeploymentStarter is used to determine if the given
	// ConsensusDeployment has started or not.
	DeploymentStarter ConsensusDeploymentStarter

	// DeploymentEnder is used to determine if the given
	// ConsensusDeployment has ended or not.
	DeploymentEnder ConsensusDeploymentEnder
}

// Constants that define the deployment offset in the deployments field of the
// parameters for each deployment.  This is useful to be able to get the details
// of a specific deployment by name.
const (
	// DeploymentTestDummy defines the rule change deployment ID for testing
	// purposes.
	DeploymentTestDummy = iota

	// DeploymentTestDummyMinActivation defines the rule change deployment
	// ID for testing purposes. This differs from the DeploymentTestDummy
	// in that it specifies the newer params the taproot fork used for
	// activation: a custom threshold and a min activation height.
	DeploymentTestDummyMinActivation

	// DeploymentCSV defines the rule change deployment ID for the CSV
	// soft-fork package. The CSV package includes the deployment of BIPS
	// 68, 112, and 113.
	DeploymentCSV

	// DeploymentSegwit defines the rule change deployment ID for the
	// Segregated Witness (segwit) soft-fork package. The segwit package
	// includes the deployment of BIPS 141, 142, 144, 145, 147 and 173.
	DeploymentSegwit

	// DeploymentTaproot defines the rule change deployment ID for the
	// Taproot (+Schnorr) soft-fork package. The taproot package includes
	// the deployment of BIPS 340, 341 and 342.
	DeploymentTaproot

	// NOTE: DefinedDeployments must always come last since it is used to
	// determine how many defined deployments there currently are.

	// DefinedDeployments is the number of currently defined deployments.
	DefinedDeployments
)

// Params defines a Bitcoin network by its parameters.  These parameters may be
// used by Bitcoin applications to differentiate networks as well as addresses
// and keys for one network from those intended for use on another network.
type Params struct {
	// Name defines a human-readable identifier for the network.
	Name string

	// Net defines the magic bytes used to identify the network.
	Net wire.BitcoinNet

	// DefaultPort defines the default peer-to-peer port for the network.
	DefaultPort string

	// DNSSeeds defines a list of DNS seeds for the network that are used
	// as one method to discover peers.
	DNSSeeds []DNSSeed

	// GenesisBlock defines the first block of the chain.
	GenesisBlock *wire.MsgBlock

	// GenesisHash is the starting block hash.
	GenesisHash *chainhash.Hash

	// PowLimit defines the highest allowed proof of work value for a block
	// as a uint256.
	PowLimit *big.Int

	// PowLimitBits defines the highest allowed proof of work value for a
	// block in compact form.
	PowLimitBits uint32

	// These fields define the block heights at which the specified softfork
	// BIP became active.
	BIP0034Height int32
	BIP0065Height int32
	BIP0066Height int32

	// CoinbaseMaturity is the number of blocks required before newly mined
	// coins (coinbase transactions) can be spent.
	CoinbaseMaturity uint16

	// SubsidyReductionInterval is the interval of blocks before the subsidy
	// is reduced.
	SubsidyReductionInterval int32

	// TargetTimespan is the desired amount of time that should elapse
	// before the block difficulty requirement is examined to determine how
	// it should be changed in order to maintain the desired block
	// generation rate.
	TargetTimespan time.Duration

	// TargetTimePerBlock is the desired amount of time to generate each
	// block.
	TargetTimePerBlock time.Duration

	// RetargetAdjustmentFactor is the adjustment factor used to limit
	// the minimum and maximum amount of adjustment that can occur between
	// difficulty retargets.
	RetargetAdjustmentFactor int64

	// ReduceMinDifficulty defines whether the network should reduce the
	// minimum required difficulty after a long enough period of time has
	// passed without finding a block.  This is really only useful for test
	// networks and should not be set on a main network.
	ReduceMinDifficulty bool

	// MinDiffReductionTime is the amount of time after which the minimum
	// required difficulty should be reduced when a block hasn't been found.
	//
	// NOTE: This only applies if ReduceMinDifficulty is true.
	MinDiffReductionTime time.Duration

	// GenerateSupported specifies whether or not CPU mining is allowed.
	GenerateSupported bool

	// Checkpoints ordered from oldest to newest.
	Checkpoints []Checkpoint

	// AssumeUtreexoPoint is the utreexo roots that a utreexo node can
	// start off of.
	AssumeUtreexoPoint AssumeUtreexo

	// TTL is committed so that nodes during ibd are able to check
	// the received ttls from other peers.
	TTL TTLState

	// These fields are related to voting on consensus rule changes as
	// defined by BIP0009.
	//
	// RuleChangeActivationThreshold is the number of blocks in a threshold
	// state retarget window for which a positive vote for a rule change
	// must be cast in order to lock in a rule change. It should typically
	// be 95% for the main network and 75% for test networks.
	//
	// MinerConfirmationWindow is the number of blocks in each threshold
	// state retarget window.
	//
	// Deployments define the specific consensus rule changes to be voted
	// on.
	RuleChangeActivationThreshold uint32
	MinerConfirmationWindow       uint32
	Deployments                   [DefinedDeployments]ConsensusDeployment

	// Mempool parameters
	RelayNonStdTxs bool

	// Human-readable part for Bech32 encoded segwit addresses, as defined
	// in BIP 173.
	Bech32HRPSegwit string

	// Address encoding magics
	PubKeyHashAddrID        byte // First byte of a P2PKH address
	ScriptHashAddrID        byte // First byte of a P2SH address
	PrivateKeyID            byte // First byte of a WIF private key
	WitnessPubKeyHashAddrID byte // First byte of a P2WPKH address
	WitnessScriptHashAddrID byte // First byte of a P2WSH address

	// BIP32 hierarchical deterministic extended key magics
	HDPrivateKeyID [4]byte
	HDPublicKeyID  [4]byte

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	HDCoinType uint32
}

// MainNetParams defines the network parameters for the main Bitcoin network.
var MainNetParams = Params{
	Name:        "mainnet",
	Net:         wire.MainNet,
	DefaultPort: "8333",
	DNSSeeds: []DNSSeed{
		{"seed.calvinkim.info.", true},        // Calvin Kim, supports filtering, including utreexo (1 << 24)
		{"manualseed.calvinkim.info.", false}, // Only returns utreexo peers.
		{"seed.bitcoin.sipa.be", true},
		{"dnsseed.bluematt.me", true},
		{"dnsseed.bitcoin.dashjr.org", false},
		{"seed.bitnodes.io", false},
		{"seed.bitcoin.jonasschnelli.ch", true},
	},

	// Chain parameters
	GenesisBlock:             &genesisBlock,
	GenesisHash:              &genesisHash,
	PowLimit:                 mainPowLimit,
	PowLimitBits:             0x1d00ffff,
	BIP0034Height:            227931, // 000000000000024b89b42a942fe0d9fea3bb44ab7bd1b19115dd6a759c0808b8
	BIP0065Height:            388381, // 000000000000000004c2b624ed5d7756c508d90fd0da2c7c679febfa6c4735f0
	BIP0066Height:            363725, // 00000000000000000379eaa19dce8c9b722d46ae6a57c2f1a988119488b50931
	CoinbaseMaturity:         100,
	SubsidyReductionInterval: 210000,
	TargetTimespan:           time.Hour * 24 * 14, // 14 days
	TargetTimePerBlock:       time.Minute * 10,    // 10 minutes
	RetargetAdjustmentFactor: 4,                   // 25% less, 400% more
	ReduceMinDifficulty:      false,
	MinDiffReductionTime:     0,
	GenerateSupported:        false,

	// Checkpoints ordered from oldest to newest.
	Checkpoints: []Checkpoint{
		{11111, newHashFromStr("0000000069e244f73d78e8fd29ba2fd2ed618bd6fa2ee92559f542fdb26e7c1d")},
		{33333, newHashFromStr("000000002dd5588a74784eaa7ab0507a18ad16a236e7b1ce69f00d7ddfb5d0a6")},
		{74000, newHashFromStr("0000000000573993a3c9e41ce34471c079dcf5f52a0e824a81e7f953b8661a20")},
		{105000, newHashFromStr("00000000000291ce28027faea320c8d2b054b2e0fe44a773f3eefb151d6bdc97")},
		{134444, newHashFromStr("00000000000005b12ffd4cd315cd34ffd4a594f430ac814c91184a0d42d2b0fe")},
		{168000, newHashFromStr("000000000000099e61ea72015e79632f216fe6cb33d7899acb35b75c8303b763")},
		{193000, newHashFromStr("000000000000059f452a5f7340de6682a977387c17010ff6e6c3bd83ca8b1317")},
		{210000, newHashFromStr("000000000000048b95347e83192f69cf0366076336c639f9b7228e9ba171342e")},
		{216116, newHashFromStr("00000000000001b4f4b433e81ee46494af945cf96014816a4e2370f11b23df4e")},
		{225430, newHashFromStr("00000000000001c108384350f74090433e7fcf79a606b8e797f065b130575932")},
		{250000, newHashFromStr("000000000000003887df1f29024b06fc2200b55f8af8f35453d7be294df2d214")},
		{267300, newHashFromStr("000000000000000a83fbd660e918f218bf37edd92b748ad940483c7c116179ac")},
		{279000, newHashFromStr("0000000000000001ae8c72a0b0c301f67e3afca10e819efa9041e458e9bd7e40")},
		{300255, newHashFromStr("0000000000000000162804527c6e9b9f0563a280525f9d08c12041def0a0f3b2")},
		{319400, newHashFromStr("000000000000000021c6052e9becade189495d1c539aa37c58917305fd15f13b")},
		{343185, newHashFromStr("0000000000000000072b8bf361d01a6ba7d445dd024203fafc78768ed4368554")},
		{352940, newHashFromStr("000000000000000010755df42dba556bb72be6a32f3ce0b6941ce4430152c9ff")},
		{382320, newHashFromStr("00000000000000000a8dc6ed5b133d0eb2fd6af56203e4159789b092defd8ab2")},
		{400000, newHashFromStr("000000000000000004ec466ce4732fe6f1ed1cddc2ed4b328fff5224276e3f6f")},
		{430000, newHashFromStr("000000000000000001868b2bb3a285f3cc6b33ea234eb70facf4dcdf22186b87")},
		{460000, newHashFromStr("000000000000000000ef751bbce8e744ad303c47ece06c8d863e4d417efc258c")},
		{490000, newHashFromStr("000000000000000000de069137b17b8d5a3dfbd5b145b2dcfb203f15d0c4de90")},
		{520000, newHashFromStr("0000000000000000000d26984c0229c9f6962dc74db0a6d525f2f1640396f69c")},
		{550000, newHashFromStr("000000000000000000223b7a2298fb1c6c75fb0efc28a4c56853ff4112ec6bc9")},
		{560000, newHashFromStr("0000000000000000002c7b276daf6efb2b6aa68e2ce3be67ef925b3264ae7122")},
		{563378, newHashFromStr("0000000000000000000f1c54590ee18d15ec70e68c8cd4cfbadb1b4f11697eee")},
		{597379, newHashFromStr("00000000000000000005f8920febd3925f8272a6a71237563d78c2edfdd09ddf")},
		{623950, newHashFromStr("0000000000000000000f2adce67e49b0b6bdeb9de8b7c3d7e93b21e7fc1e819d")},
		{654683, newHashFromStr("0000000000000000000b9d2ec5a352ecba0592946514a92f14319dc2b367fc72")},
		{691719, newHashFromStr("00000000000000000008a89e854d57e5667df88f1cdef6fde2fbca1de5b639ad")},
		{724466, newHashFromStr("000000000000000000052d314a259755ca65944e68df6b12a067ea8f1f5a7091")},
		{751565, newHashFromStr("00000000000000000009c97098b5295f7e5f183ac811fb5d1534040adb93cabd")},
		{841776, newHashFromStr("00000000000000000000a174eebf5b9df9b9ebf062cc3c503a2024e7d9a618b6")},
		{943013, newHashFromStr("00000000000000000001c595730bd4a5fb0e2b35af70882962ce7ae602f48aff")},
	},

	AssumeUtreexoPoint: AssumeUtreexo{
		BlockHash:   newHashFromStr("00000000000000000001c595730bd4a5fb0e2b35af70882962ce7ae602f48aff"),
		BlockHeight: 943_013,
		Bits:        386_013_841,
		BlockSize:   1_435_475,
		BlockWeight: 3_993_740,
		NumTxns:     1_901,
		TotalTxns:   1_331_397_202,
		MedianTime:  time.Unix(1_774_927_824, 0),
		NumLeaves:   3_082_565_786,
		Roots: []utreexo.Hash{
			newUtreexoHashFromStr("58903d7b8f355710ddb5d8dfaf17f240af6a3a14c1d606422dcd6362f8a338e3"),
			newUtreexoHashFromStr("10d6640db81dae22a18b99d8f1179ebcf7f39d8758a9183b02762a244e68099e"),
			newUtreexoHashFromStr("430e732e0d30717765e7128b6a5fe70eb22b4b3e6dc772a40b46846d9333800d"),
			newUtreexoHashFromStr("3dc7194b8069bd841aed28b33791e01297c05e46e2eb8eed8ea93f7202388980"),
			newUtreexoHashFromStr("eb41827f548a811e6dcd259f8a486e2233757d100f0e9ac63b2d9fd1559b010d"),
			newUtreexoHashFromStr("33ed5b7b9f71bee8f2e8ab03d4bc8544f362600819f13e6267b72d8a9a8fd36d"),
			newUtreexoHashFromStr("a51fa39fb261c4ba5596e7239b257241545889428cbd8e8f49c70e24645fe756"),
			newUtreexoHashFromStr("db4326c816eea0eafa495b33195a56c696f5a4ef8fdec62f85f8b756b794d53a"),
			newUtreexoHashFromStr("091f556e7803e4136025fa5d518a6c88dadd926451b90b5739483bbe49f3d090"),
			newUtreexoHashFromStr("7e770db3ace3c6298eb2a64282d57db6aed78fc8b9a4efd7c1714a3a8e23fb74"),
			newUtreexoHashFromStr("e62ea173baecb1ea1e51985cfa56dc2ffbd22161be9b10d77467e61f95e5a86e"),
			newUtreexoHashFromStr("fd2ab9a7c539d9f1e5dc94879c690980ebc710b431a367eff166792c90963545"),
			newUtreexoHashFromStr("a47bc57aea513f1a58d1e9510f61b96ec27fa9bd5cab4c532e72df9507b8641a"),
			newUtreexoHashFromStr("a88dc84b5dc1c5392191f066201849274e6cbf4e86b8f0dbce90ce3aaeab07ed"),
			newUtreexoHashFromStr("bb7c6dd45d49a024fef17dd043370bcc93e5ca969b2256a38b5a0e96220ff0ab"),
			newUtreexoHashFromStr("194411f2b062acb34454bb7f48859a6300df07ad04d8d63f39b6e34275de2e19"),
			newUtreexoHashFromStr("539fea58450d5142766460d4c40b9cde097fa18264dae636c38bda061e3f22ac"),
			newUtreexoHashFromStr("a25a97e938c05b554de2e3102622c4b4f6a66152f6d2cb2c6445d20a97e28013"),
		},
	},

	TTL: TTLState{
		Stump: []utreexo.Stump{
			{
				Roots: []utreexo.Hash{
					newUtreexoHashFromStr("b593762e5af96a24f54576fe7be8de839693bd553a41297a13eb2b7fb0e0db5b"),
					newUtreexoHashFromStr("12ecb40dab5bbd8d7496f374a0242d55295eda1a3fee2c630c04b6921431053e"),
					newUtreexoHashFromStr("c7a43e36e10f9df81055f20c75076c86a4618888e65e55d7ffbc60e9c477bc28"),
					newUtreexoHashFromStr("a26cf9125c5c7add2a1792ba393cb653399723c7825c0ec415ca9ba38a90a36a"),
					newUtreexoHashFromStr("363cc9399ea3473c4b32acd1e9c9dae2d0f1a18a8d7e9365c031a2652317ed3e"),
					newUtreexoHashFromStr("bb5bec480d663b61d6ad5ffe323ee11bea479f1a38e95e4fc7ecd5467e2d23b4"),
					newUtreexoHashFromStr("c2ee495a7bc4d02580f09ba46476b5922f1f447bb73770186bc13efd61def3a6"),
					newUtreexoHashFromStr("da9e1d2a8fe1fcbe5c274d9be3bbc579ece96281a59b3fbb2cca64816611c7fc"),
					newUtreexoHashFromStr("d8f945bf3c4db8f2986fa6d013a73983b7fcaae9b1c3597352c0717b1c0015d3"),
					newUtreexoHashFromStr("72a05f8b5ec6d0d5bc73f95e696ce9bdfa2e5923bc1e7448c2bc7ede454c2005"),
					newUtreexoHashFromStr("e5104b779dce8f5ec805bf1f2d579da3fa5117f5541c504d896cb723e100380f"),
				},
				NumLeaves: 943_014,
			},
		},
	},

	// Consensus rule change deployments.
	//
	// The miner confirmation window is defined as:
	//   target proof of work timespan / target proof of work spacing
	RuleChangeActivationThreshold: 1916, // 95% of MinerConfirmationWindow
	MinerConfirmationWindow:       2016, //
	Deployments: [DefinedDeployments]ConsensusDeployment{
		DeploymentTestDummy: {
			BitNumber: 28,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Unix(11991456010, 0), // January 1, 2008 UTC
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Unix(1230767999, 0), // December 31, 2008 UTC
			),
		},
		DeploymentTestDummyMinActivation: {
			BitNumber:                 22,
			CustomActivationThreshold: 1815,    // Only needs 90% hash rate.
			MinActivationHeight:       10_0000, // Can only activate after height 10k.
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires
			),
		},
		DeploymentCSV: {
			BitNumber: 0,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Unix(1462060800, 0), // May 1st, 2016
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Unix(1493596800, 0), // May 1st, 2017
			),
		},
		DeploymentSegwit: {
			BitNumber: 1,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Unix(1479168000, 0), // November 15, 2016 UTC
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Unix(1510704000, 0), // November 15, 2017 UTC.
			),
		},
		DeploymentTaproot: {
			BitNumber: 2,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Unix(1619222400, 0), // April 24th, 2021 UTC.
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Unix(1628640000, 0), // August 11th, 2021 UTC.
			),
			CustomActivationThreshold: 1815, // 90%
			MinActivationHeight:       709_632,
		},
	},

	// Mempool parameters
	RelayNonStdTxs: false,

	// Human-readable part for Bech32 encoded segwit addresses, as defined in
	// BIP 173.
	Bech32HRPSegwit: "bc", // always bc for main net

	// Address encoding magics
	PubKeyHashAddrID:        0x00, // starts with 1
	ScriptHashAddrID:        0x05, // starts with 3
	PrivateKeyID:            0x80, // starts with 5 (uncompressed) or K (compressed)
	WitnessPubKeyHashAddrID: 0x06, // starts with p2
	WitnessScriptHashAddrID: 0x0A, // starts with 7Xh

	// BIP32 hierarchical deterministic extended key magics
	HDPrivateKeyID: [4]byte{0x04, 0x88, 0xad, 0xe4}, // starts with xprv
	HDPublicKeyID:  [4]byte{0x04, 0x88, 0xb2, 0x1e}, // starts with xpub

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	HDCoinType: 0,
}

// RegressionNetParams defines the network parameters for the regression test
// Bitcoin network.  Not to be confused with the test Bitcoin network (version
// 3), this network is sometimes simply called "testnet".
var RegressionNetParams = Params{
	Name:        "regtest",
	Net:         wire.TestNet,
	DefaultPort: "18444",
	DNSSeeds:    []DNSSeed{},

	// Chain parameters
	GenesisBlock:             &regTestGenesisBlock,
	GenesisHash:              &regTestGenesisHash,
	PowLimit:                 regressionPowLimit,
	PowLimitBits:             0x207fffff,
	CoinbaseMaturity:         100,
	BIP0034Height:            100000000, // Not active - Permit ver 1 blocks
	BIP0065Height:            1351,      // Used by regression tests
	BIP0066Height:            1251,      // Used by regression tests
	SubsidyReductionInterval: 150,
	TargetTimespan:           time.Hour * 24 * 14, // 14 days
	TargetTimePerBlock:       time.Minute * 10,    // 10 minutes
	RetargetAdjustmentFactor: 4,                   // 25% less, 400% more
	ReduceMinDifficulty:      true,
	MinDiffReductionTime:     time.Minute * 20, // TargetTimePerBlock * 2
	GenerateSupported:        true,

	// Checkpoints ordered from oldest to newest.
	Checkpoints: nil,

	// Consensus rule change deployments.
	//
	// The miner confirmation window is defined as:
	//   target proof of work timespan / target proof of work spacing
	RuleChangeActivationThreshold: 108, // 75%  of MinerConfirmationWindow
	MinerConfirmationWindow:       144,
	Deployments: [DefinedDeployments]ConsensusDeployment{
		DeploymentTestDummy: {
			BitNumber: 28,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires
			),
		},
		DeploymentTestDummyMinActivation: {
			BitNumber:                 22,
			CustomActivationThreshold: 72,  // Only needs 50% hash rate.
			MinActivationHeight:       600, // Can only activate after height 600.
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires
			),
		},
		DeploymentCSV: {
			BitNumber: 0,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires
			),
		},
		DeploymentSegwit: {
			BitNumber: 1,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires.
			),
		},
		DeploymentTaproot: {
			BitNumber: 2,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires.
			),
			CustomActivationThreshold: 1512, // 75%
		},
	},

	// Mempool parameters
	RelayNonStdTxs: true,

	// Human-readable part for Bech32 encoded segwit addresses, as defined in
	// BIP 173.
	Bech32HRPSegwit: "bcrt", // always bcrt for reg test net

	// Address encoding magics
	PubKeyHashAddrID: 0x6f, // starts with m or n
	ScriptHashAddrID: 0xc4, // starts with 2
	PrivateKeyID:     0xef, // starts with 9 (uncompressed) or c (compressed)

	// BIP32 hierarchical deterministic extended key magics
	HDPrivateKeyID: [4]byte{0x04, 0x35, 0x83, 0x94}, // starts with tprv
	HDPublicKeyID:  [4]byte{0x04, 0x35, 0x87, 0xcf}, // starts with tpub

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	HDCoinType: 1,
}

// TestNet3Params defines the network parameters for the test Bitcoin network
// (version 3).  Not to be confused with the regression test network, this
// network is sometimes simply called "testnet".
var TestNet3Params = Params{
	Name:        "testnet3",
	Net:         wire.TestNet3,
	DefaultPort: "18333",
	DNSSeeds: []DNSSeed{
		{"testnetseed.calvinkim.info.", true},        // Calvin Kim, supports filtering, including utreexo (1 << 24)
		{"testnetmanualseed.calvinkim.info.", false}, // Only returns utreexo peers.
		{"testnet-seed.bitcoin.jonasschnelli.ch", true},
		{"testnet-seed.bitcoin.schildbach.de", false},
		{"seed.tbtc.petertodd.org", true},
		{"testnet-seed.bluematt.me", false},
	},

	// Chain parameters
	GenesisBlock:             &testNet3GenesisBlock,
	GenesisHash:              &testNet3GenesisHash,
	PowLimit:                 testNet3PowLimit,
	PowLimitBits:             0x1d00ffff,
	BIP0034Height:            21111,  // 0000000023b3a96d3484e5abb3755c413e7d41500f8e2a5c3f0dd01299cd8ef8
	BIP0065Height:            581885, // 00000000007f6655f22f98e72ed80d8b06dc761d5da09df0fa1dc4be4f861eb6
	BIP0066Height:            330776, // 000000002104c8c45e99a8853285a3b592602a3ccde2b832481da85e9e4ba182
	CoinbaseMaturity:         100,
	SubsidyReductionInterval: 210000,
	TargetTimespan:           time.Hour * 24 * 14, // 14 days
	TargetTimePerBlock:       time.Minute * 10,    // 10 minutes
	RetargetAdjustmentFactor: 4,                   // 25% less, 400% more
	ReduceMinDifficulty:      true,
	MinDiffReductionTime:     time.Minute * 20, // TargetTimePerBlock * 2
	GenerateSupported:        false,

	// Checkpoints ordered from oldest to newest.
	Checkpoints: []Checkpoint{
		{546, newHashFromStr("000000002a936ca763904c3c35fce2f3556c559c0214345d31b1bcebf76acb70")},
		{100000, newHashFromStr("00000000009e2958c15ff9290d571bf9459e93b19765c6801ddeccadbb160a1e")},
		{200000, newHashFromStr("0000000000287bffd321963ef05feab753ebe274e1d78b2fd4e2bfe9ad3aa6f2")},
		{300001, newHashFromStr("0000000000004829474748f3d1bc8fcf893c88be255e6d7f571c548aff57abf4")},
		{400002, newHashFromStr("0000000005e2c73b8ecb82ae2dbc2e8274614ebad7172b53528aba7501f5a089")},
		{500011, newHashFromStr("00000000000929f63977fbac92ff570a9bd9e7715401ee96f2848f7b07750b02")},
		{600002, newHashFromStr("000000000001f471389afd6ee94dcace5ccc44adc18e8bff402443f034b07240")},
		{700000, newHashFromStr("000000000000406178b12a4dea3b27e13b3c4fe4510994fd667d7c1e6a3f4dc1")},
		{800010, newHashFromStr("000000000017ed35296433190b6829db01e657d80631d43f5983fa403bfdb4c1")},
		{900000, newHashFromStr("0000000000356f8d8924556e765b7a94aaebc6b5c8685dcfa2b1ee8b41acd89b")},
		{1000007, newHashFromStr("00000000001ccb893d8a1f25b70ad173ce955e5f50124261bbbc50379a612ddf")},
		{1100007, newHashFromStr("00000000000abc7b2cd18768ab3dee20857326a818d1946ed6796f42d66dd1e8")},
		{1200007, newHashFromStr("00000000000004f2dc41845771909db57e04191714ed8c963f7e56713a7b6cea")},
		{1300007, newHashFromStr("0000000072eab69d54df75107c052b26b0395b44f77578184293bf1bb1dbd9fa")},
		{1354312, newHashFromStr("0000000000000037a8cd3e06cd5edbfe9dd1dbcc5dacab279376ef7cfc2b4c75")},
		{1580000, newHashFromStr("00000000000000b7ab6ce61eb6d571003fbe5fe892da4c9b740c49a07542462d")},
		{1692000, newHashFromStr("000000000000056c49030c174179b52a928c870e6e8a822c75973b7970cfbd01")},
		{1864000, newHashFromStr("000000000000006433d1efec504c53ca332b64963c425395515b01977bd7b3b0")},
		{2010000, newHashFromStr("0000000000004ae2f3896ca8ecd41c460a35bf6184e145d91558cece1c688a76")},
		{2143398, newHashFromStr("00000000000163cfb1f97c4e4098a3692c8053ad9cab5ad9c86b338b5c00b8b7")},
		{2344474, newHashFromStr("0000000000000004877fa2d36316398528de4f347df2f8a96f76613a298ce060")},
		{2810937, newHashFromStr("000000000000005a4f7ec7942d57353c6200a284e021bc5c6be0bf1415890875")},
	},

	AssumeUtreexoPoint: AssumeUtreexo{
		BlockHash:   newHashFromStr("000000000000005a4f7ec7942d57353c6200a284e021bc5c6be0bf1415890875"),
		BlockHeight: 2_810_937,
		Bits:        436_273_151,
		BlockSize:   1_318_898,
		BlockWeight: 3_844_868,
		NumTxns:     3_297,
		TotalTxns:   124_627_332,
		MedianTime:  time.Unix(1_714_714_572, 0),
		NumLeaves:   253_929_582,
		Roots: []utreexo.Hash{
			newUtreexoHashFromStr("280cd64d7e4c18222ddfb00e53377ec12170d9361e85eea92451d21d0895fda2"),
			newUtreexoHashFromStr("12e642772d82892b97ffef3b1313c003fb5654eb0ee7742419d984b37cf49dd6"),
			newUtreexoHashFromStr("d2f3f632f188408c3087cc72f0cb5439f18d3e4778facefc12c0dcc4edc4f9b0"),
			newUtreexoHashFromStr("42e0770222f8c0e233e564af14584b26322a039b2cb92d8cac55972c5dcfe7d4"),
			newUtreexoHashFromStr("f35d0a8b2e10f3ab35679f0f87a58ba6fd514289d96a389844397ac0b126e130"),
			newUtreexoHashFromStr("3cf92c2cbc0a1a79a0e334c76d0c1ae2ba00dedebad64b824ea09488b3af2552"),
			newUtreexoHashFromStr("7e91cd2c01418497abeb26c583f09fedfebd800aca55715599d1b4961ede59f8"),
			newUtreexoHashFromStr("522fcf0b27a5e7173b3fe5b2fdde77e2f038397793a6e3773f11d1d29df2b433"),
			newUtreexoHashFromStr("30010d1bb681d868485cb7669bb83783457154ccfbd21e8a5fc229e7b0547c7f"),
			newUtreexoHashFromStr("bb397cca3d07a8d7e0646ee49503af771344eb602a67372adce5c092c5493c5c"),
			newUtreexoHashFromStr("bd779aef8f852a156e5d4fb3dbbd4a7b3d210417844cbe8dc7a2331e6e0fb89f"),
			newUtreexoHashFromStr("bd874177c6c0ebeede72a80d37247b7a6b7a10bb944009b2aa5834c97fa1d4a5"),
			newUtreexoHashFromStr("0f2082ab3c1d3d5afa980708e68daaab7a2ac4e61c5a138e71cedada6ac5cb82"),
			newUtreexoHashFromStr("36497c78ca41c651cf296707d60dba90735a082c7c1cb3bd39697104f0b09ad3"),
		},
	},

	// Consensus rule change deployments.
	//
	// The miner confirmation window is defined as:
	//   target proof of work timespan / target proof of work spacing
	RuleChangeActivationThreshold: 1512, // 75% of MinerConfirmationWindow
	MinerConfirmationWindow:       2016,
	Deployments: [DefinedDeployments]ConsensusDeployment{
		DeploymentTestDummy: {
			BitNumber: 28,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Unix(1199145601, 0), // January 1, 2008 UTC
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Unix(1230767999, 0), // December 31, 2008 UTC
			),
		},
		DeploymentTestDummyMinActivation: {
			BitNumber:                 22,
			CustomActivationThreshold: 1815,    // Only needs 90% hash rate.
			MinActivationHeight:       10_0000, // Can only activate after height 10k.
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires
			),
		},
		DeploymentCSV: {
			BitNumber: 0,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Unix(1456790400, 0), // March 1st, 2016
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Unix(1493596800, 0), // May 1st, 2017
			),
		},
		DeploymentSegwit: {
			BitNumber: 1,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Unix(1462060800, 0), // May 1, 2016 UTC
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Unix(1493596800, 0), // May 1, 2017 UTC.
			),
		},
		DeploymentTaproot: {
			BitNumber: 2,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Unix(1619222400, 0), // April 24th, 2021 UTC.
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Unix(1628640000, 0), // August 11th, 2021 UTC
			),
			CustomActivationThreshold: 1512, // 75%
		},
	},

	// Mempool parameters
	RelayNonStdTxs: true,

	// Human-readable part for Bech32 encoded segwit addresses, as defined in
	// BIP 173.
	Bech32HRPSegwit: "tb", // always tb for test net

	// Address encoding magics
	PubKeyHashAddrID:        0x6f, // starts with m or n
	ScriptHashAddrID:        0xc4, // starts with 2
	WitnessPubKeyHashAddrID: 0x03, // starts with QW
	WitnessScriptHashAddrID: 0x28, // starts with T7n
	PrivateKeyID:            0xef, // starts with 9 (uncompressed) or c (compressed)

	// BIP32 hierarchical deterministic extended key magics
	HDPrivateKeyID: [4]byte{0x04, 0x35, 0x83, 0x94}, // starts with tprv
	HDPublicKeyID:  [4]byte{0x04, 0x35, 0x87, 0xcf}, // starts with tpub

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	HDCoinType: 1,
}

// SimNetParams defines the network parameters for the simulation test Bitcoin
// network.  This network is similar to the normal test network except it is
// intended for private use within a group of individuals doing simulation
// testing.  The functionality is intended to differ in that the only nodes
// which are specifically specified are used to create the network rather than
// following normal discovery rules.  This is important as otherwise it would
// just turn into another public testnet.
var SimNetParams = Params{
	Name:        "simnet",
	Net:         wire.SimNet,
	DefaultPort: "18555",
	DNSSeeds:    []DNSSeed{}, // NOTE: There must NOT be any seeds.

	// Chain parameters
	GenesisBlock:             &simNetGenesisBlock,
	GenesisHash:              &simNetGenesisHash,
	PowLimit:                 simNetPowLimit,
	PowLimitBits:             0x207fffff,
	BIP0034Height:            0, // Always active on simnet
	BIP0065Height:            0, // Always active on simnet
	BIP0066Height:            0, // Always active on simnet
	CoinbaseMaturity:         100,
	SubsidyReductionInterval: 210000,
	TargetTimespan:           time.Hour * 24 * 14, // 14 days
	TargetTimePerBlock:       time.Minute * 10,    // 10 minutes
	RetargetAdjustmentFactor: 4,                   // 25% less, 400% more
	ReduceMinDifficulty:      true,
	MinDiffReductionTime:     time.Minute * 20, // TargetTimePerBlock * 2
	GenerateSupported:        true,

	// Checkpoints ordered from oldest to newest.
	Checkpoints: nil,

	// Consensus rule change deployments.
	//
	// The miner confirmation window is defined as:
	//   target proof of work timespan / target proof of work spacing
	RuleChangeActivationThreshold: 75, // 75% of MinerConfirmationWindow
	MinerConfirmationWindow:       100,
	Deployments: [DefinedDeployments]ConsensusDeployment{
		DeploymentTestDummy: {
			BitNumber: 28,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires
			),
		},
		DeploymentTestDummyMinActivation: {
			BitNumber:                 22,
			CustomActivationThreshold: 50,  // Only needs 50% hash rate.
			MinActivationHeight:       600, // Can only activate after height 600.
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires
			),
		},
		DeploymentCSV: {
			BitNumber: 0,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires
			),
		},
		DeploymentSegwit: {
			BitNumber: 1,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires.
			),
		},
		DeploymentTaproot: {
			BitNumber: 2,
			DeploymentStarter: NewMedianTimeDeploymentStarter(
				time.Time{}, // Always available for vote
			),
			DeploymentEnder: NewMedianTimeDeploymentEnder(
				time.Time{}, // Never expires.
			),
			CustomActivationThreshold: 1815, // 90%
		},
	},

	// Mempool parameters
	RelayNonStdTxs: true,

	// Human-readable part for Bech32 encoded segwit addresses, as defined in
	// BIP 173.
	Bech32HRPSegwit: "sb", // always sb for sim net

	// Address encoding magics
	PubKeyHashAddrID:        0x3f, // starts with S
	ScriptHashAddrID:        0x7b, // starts with s
	PrivateKeyID:            0x64, // starts with 4 (uncompressed) or F (compressed)
	WitnessPubKeyHashAddrID: 0x19, // starts with Gg
	WitnessScriptHashAddrID: 0x28, // starts with ?

	// BIP32 hierarchical deterministic extended key magics
	HDPrivateKeyID: [4]byte{0x04, 0x20, 0xb9, 0x00}, // starts with sprv
	HDPublicKeyID:  [4]byte{0x04, 0x20, 0xbd, 0x3a}, // starts with spub

	// BIP44 coin type used in the hierarchical deterministic path for
	// address generation.
	HDCoinType: 115, // ASCII for s
}

// SigNetParams defines the network parameters for the default public signet
// Bitcoin network. Not to be confused with the regression test network, this
// network is sometimes simply called "signet" or "taproot signet".
var SigNetParams = CustomSignetParams(
	DefaultSignetChallenge, DefaultSignetDNSSeeds,
)

// CustomSignetParams creates network parameters for a custom signet network
// from a challenge. The challenge is the binary compiled version of the block
// challenge script.
func CustomSignetParams(challenge []byte, dnsSeeds []DNSSeed) Params {
	// The message start is defined as the first four bytes of the sha256d
	// of the challenge script, as a single push (i.e. prefixed with the
	// challenge script length).
	challengeLength := byte(len(challenge))
	hashDouble := chainhash.DoubleHashB(
		append([]byte{challengeLength}, challenge...),
	)

	assumeUtreexoPoint := AssumeUtreexo{}
	checkPoints := []Checkpoint{}
	if bytes.Equal(challenge, DefaultSignetChallenge) {
		assumeUtreexoPoint = AssumeUtreexo{
			BlockHash:   newHashFromStr("0000000d0ffcda2be44be940b3f2ba0b9e1a5e60dde6d074a7dd749ef2af2061"),
			BlockHeight: 297_353,
			Bits:        487_943_282,
			BlockSize:   116_455,
			BlockWeight: 315_976,
			NumTxns:     113,
			TotalTxns:   28_981_192,
			MedianTime:  time.Unix(1_774_594_313, 0),
			NumLeaves:   126_397_012,
			Roots: []utreexo.Hash{
				newUtreexoHashFromStr("3c9008926e6c9fbd6599dc964b3607352fdb293118685809d47346259714c40c"),
				newUtreexoHashFromStr("4f968c4a013292a009bc057ec4cca99b0fcb3b416619b77be2cbf4fbf85450f1"),
				newUtreexoHashFromStr("6c9112c60a99ddbd25d29291bb9f67e08682df5ca75318c4f6f6003f0130bf55"),
				newUtreexoHashFromStr("0aa511752ef5e942a51cfa3fd01c96bb4f331a1cf4e071566b4482a98ffe6d03"),
				newUtreexoHashFromStr("3672ff6d9dcfb98cf163fecfb12bc969f7500c604a6983ede7ffb1617c4c7221"),
				newUtreexoHashFromStr("1aae0dc3baa7db1631f0b3f00156226b64b70e259fa3131f701fb4efc0228388"),
				newUtreexoHashFromStr("4e05ed97fa27af0b4ec97624e4725c6c4c8fdcbac67fe502dd419d93441ee96b"),
				newUtreexoHashFromStr("4ec2a1506504148d8a922f3eb9e52d182ce7bf8086feb8656f5a133554ed28b5"),
				newUtreexoHashFromStr("4cfbdd2e4bf2004d3b9e3c9638375ab90d6b3aca1c2b0c8fe6ae7abb2062d894"),
				newUtreexoHashFromStr("01e4ae0f943115a3371fd90c71966dc5410a43741a1141ea5e8f2297f654c48d"),
				newUtreexoHashFromStr("ca0747234af7114f37008347147be5a1df6f6282e09897dc0cc6e14b3435595c"),
				newUtreexoHashFromStr("3f35a2d17dea7b7ea387255b67cf632618b51915149d0e6a6abce42146e67ee7"),
			},
		}

		checkPoints = []Checkpoint{
			{20_000, newHashFromStr("000000d86368960eddbf7e127f8ba93a56efe71420b5dd8dbf8b0a68fa9ebbd1")},
			{40_000, newHashFromStr("0000014086ddfe6836bd52179c2ce1ce5eb8a9b85aee87c18be05c605723793c")},
			{60_000, newHashFromStr("00000130ab66a74ee232acb3f7ae35f9763dcec197b82b22f482ad0ea4c801f1")},
			{80_000, newHashFromStr("0000011f3e29efed437f858f1757a7a8ac406194cc80ea2948ed543994962416")},
			{100_000, newHashFromStr("0000008753108390007b3f5c26e5d924191567e147876b84489b0c0cf133a0bf")},
			{120_000, newHashFromStr("0000011b6acd2af1a7dc04a2c88a7b2c3980ebd9375dc8d52d331e715deeffcb")},
			{150_000, newHashFromStr("0000013d778ba3f914530f11f6b69869c9fab54acff85acd7b8201d111f19b7f")},
			{170_000, newHashFromStr("00000041c812a89f084f633e4cf47e819a2f6b1c0a15162355a930410522c99d")},
			{193_792, newHashFromStr("000000408463e4809d3a493baf8f17f25a919f883824f5b42247402cfeec1b73")},
			{231_620, newHashFromStr("0000012f65a81923ad36ee7d6a0d0ab2c7880e1390fbb4a87b2ebab5ee956d2e")},
			{270_566, newHashFromStr("00000004d85480599a5997038d256ea2a31a8e9dca48b1e4c0298063cb016fd6")},
			{294_687, newHashFromStr("000000057858aabfe204e2e2005c33567c6e310c90467d49bea36e4343f18d53")},
		}
	}

	// We use little endian encoding of the hash prefix to be in line with
	// the other wire network identities.
	net := binary.LittleEndian.Uint32(hashDouble[0:4])
	return Params{
		Name:        "signet",
		Net:         wire.BitcoinNet(net),
		DefaultPort: "38333",
		DNSSeeds:    dnsSeeds,

		// Chain parameters
		GenesisBlock:             &sigNetGenesisBlock,
		GenesisHash:              &sigNetGenesisHash,
		PowLimit:                 sigNetPowLimit,
		PowLimitBits:             0x1e0377ae,
		BIP0034Height:            1,
		BIP0065Height:            1,
		BIP0066Height:            1,
		CoinbaseMaturity:         100,
		SubsidyReductionInterval: 210000,
		TargetTimespan:           time.Hour * 24 * 14, // 14 days
		TargetTimePerBlock:       time.Minute * 10,    // 10 minutes
		RetargetAdjustmentFactor: 4,                   // 25% less, 400% more
		ReduceMinDifficulty:      false,
		MinDiffReductionTime:     time.Minute * 20, // TargetTimePerBlock * 2
		GenerateSupported:        false,

		// Checkpoints ordered from oldest to newest.
		Checkpoints: checkPoints,

		AssumeUtreexoPoint: assumeUtreexoPoint,

		TTL: TTLState{
			Stump: []utreexo.Stump{
				{
					Roots: []utreexo.Hash{
						newUtreexoHashFromStr("d2d236de5dfc51f6d19e6032051e23d17190a163bd141e20fec95245daf5aeb3"),
						newUtreexoHashFromStr("c162ca415897569c3587a9ed27ae2c048ae5b7beb00a7d85516b1977ef82ff5d"),
						newUtreexoHashFromStr("6513784f8fa270f944d6141c7c4767b24ad41c0c8aa932ee88191b47ab9c8b24"),
						newUtreexoHashFromStr("95239c0dc67fa634b46349d24ac16c4120196931a7b17022a49a71be276ab435"),
						newUtreexoHashFromStr("b37dc4d51ce269e4f2510a35492d36a0fd589b5b92806af6ffa4cd9d240ef9cf"),
						newUtreexoHashFromStr("d172d80ceff3674bbc94676db958cf5664d561f1424077bbf9ae16d870e474ae"),
						newUtreexoHashFromStr("dfd16dcf4320b308358e878ebf55b5ea35a6e24b3db76d098e055160bf552adf"),
						newUtreexoHashFromStr("291a2a4595f158eecbbf791fde3f12605671eaa091dec625eaae0684cff39809"),
						newUtreexoHashFromStr("56ce0aa0aaa20f8d0f3f6480ffb8f9f80d7a530d549e50a889d430d51db91b3f"),
					},
					NumLeaves: 294_688,
				},
			},
		},

		// Consensus rule change deployments.
		//
		// The miner confirmation window is defined as:
		//   target proof of work timespan / target proof of work spacing
		RuleChangeActivationThreshold: 1916, // 95% of 2016
		MinerConfirmationWindow:       2016,
		Deployments: [DefinedDeployments]ConsensusDeployment{
			DeploymentTestDummy: {
				BitNumber: 28,
				DeploymentStarter: NewMedianTimeDeploymentStarter(
					time.Unix(1199145601, 0), // January 1, 2008 UTC
				),
				DeploymentEnder: NewMedianTimeDeploymentEnder(
					time.Unix(1230767999, 0), // December 31, 2008 UTC
				),
			},
			DeploymentTestDummyMinActivation: {
				BitNumber:                 22,
				CustomActivationThreshold: 1815,    // Only needs 90% hash rate.
				MinActivationHeight:       10_0000, // Can only activate after height 10k.
				DeploymentStarter: NewMedianTimeDeploymentStarter(
					time.Time{}, // Always available for vote
				),
				DeploymentEnder: NewMedianTimeDeploymentEnder(
					time.Time{}, // Never expires
				),
			},
			DeploymentCSV: {
				BitNumber: 29,
				DeploymentStarter: NewMedianTimeDeploymentStarter(
					time.Time{}, // Always available for vote
				),
				DeploymentEnder: NewMedianTimeDeploymentEnder(
					time.Time{}, // Never expires
				),
			},
			DeploymentSegwit: {
				BitNumber: 29,
				DeploymentStarter: NewMedianTimeDeploymentStarter(
					time.Time{}, // Always available for vote
				),
				DeploymentEnder: NewMedianTimeDeploymentEnder(
					time.Time{}, // Never expires
				),
			},
			DeploymentTaproot: {
				BitNumber: 29,
				DeploymentStarter: NewMedianTimeDeploymentStarter(
					time.Time{}, // Always available for vote
				),
				DeploymentEnder: NewMedianTimeDeploymentEnder(
					time.Time{}, // Never expires
				),
			},
		},

		// Mempool parameters
		RelayNonStdTxs: false,

		// Human-readable part for Bech32 encoded segwit addresses, as defined in
		// BIP 173.
		Bech32HRPSegwit: "tb", // always tb for test net

		// Address encoding magics
		PubKeyHashAddrID:        0x6f, // starts with m or n
		ScriptHashAddrID:        0xc4, // starts with 2
		WitnessPubKeyHashAddrID: 0x03, // starts with QW
		WitnessScriptHashAddrID: 0x28, // starts with T7n
		PrivateKeyID:            0xef, // starts with 9 (uncompressed) or c (compressed)

		// BIP32 hierarchical deterministic extended key magics
		HDPrivateKeyID: [4]byte{0x04, 0x35, 0x83, 0x94}, // starts with tprv
		HDPublicKeyID:  [4]byte{0x04, 0x35, 0x87, 0xcf}, // starts with tpub

		// BIP44 coin type used in the hierarchical deterministic path for
		// address generation.
		HDCoinType: 1,
	}
}

var (
	// ErrDuplicateNet describes an error where the parameters for a Bitcoin
	// network could not be set due to the network already being a standard
	// network or previously-registered into this package.
	ErrDuplicateNet = errors.New("duplicate Bitcoin network")

	// ErrUnknownHDKeyID describes an error where the provided id which
	// is intended to identify the network for a hierarchical deterministic
	// private extended key is not registered.
	ErrUnknownHDKeyID = errors.New("unknown hd private extended key bytes")

	// ErrInvalidHDKeyID describes an error where the provided hierarchical
	// deterministic version bytes, or hd key id, is malformed.
	ErrInvalidHDKeyID = errors.New("invalid hd extended key version bytes")
)

var (
	registeredNets       = make(map[wire.BitcoinNet]struct{})
	pubKeyHashAddrIDs    = make(map[byte]struct{})
	scriptHashAddrIDs    = make(map[byte]struct{})
	bech32SegwitPrefixes = make(map[string]struct{})
	hdPrivToPubKeyIDs    = make(map[[4]byte][]byte)
)

// String returns the hostname of the DNS seed in human-readable form.
func (d DNSSeed) String() string {
	return d.Host
}

// Register registers the network parameters for a Bitcoin network.  This may
// error with ErrDuplicateNet if the network is already registered (either
// due to a previous Register call, or the network being one of the default
// networks).
//
// Network parameters should be registered into this package by a main package
// as early as possible.  Then, library packages may lookup networks or network
// parameters based on inputs and work regardless of the network being standard
// or not.
func Register(params *Params) error {
	if _, ok := registeredNets[params.Net]; ok {
		return ErrDuplicateNet
	}
	registeredNets[params.Net] = struct{}{}
	pubKeyHashAddrIDs[params.PubKeyHashAddrID] = struct{}{}
	scriptHashAddrIDs[params.ScriptHashAddrID] = struct{}{}

	err := RegisterHDKeyID(params.HDPublicKeyID[:], params.HDPrivateKeyID[:])
	if err != nil {
		return err
	}

	// A valid Bech32 encoded segwit address always has as prefix the
	// human-readable part for the given net followed by '1'.
	bech32SegwitPrefixes[params.Bech32HRPSegwit+"1"] = struct{}{}
	return nil
}

// mustRegister performs the same function as Register except it panics if there
// is an error.  This should only be called from package init functions.
func mustRegister(params *Params) {
	if err := Register(params); err != nil {
		panic("failed to register network: " + err.Error())
	}
}

// IsPubKeyHashAddrID returns whether the id is an identifier known to prefix a
// pay-to-pubkey-hash address on any default or registered network.  This is
// used when decoding an address string into a specific address type.  It is up
// to the caller to check both this and IsScriptHashAddrID and decide whether an
// address is a pubkey hash address, script hash address, neither, or
// undeterminable (if both return true).
func IsPubKeyHashAddrID(id byte) bool {
	_, ok := pubKeyHashAddrIDs[id]
	return ok
}

// IsScriptHashAddrID returns whether the id is an identifier known to prefix a
// pay-to-script-hash address on any default or registered network.  This is
// used when decoding an address string into a specific address type.  It is up
// to the caller to check both this and IsPubKeyHashAddrID and decide whether an
// address is a pubkey hash address, script hash address, neither, or
// undeterminable (if both return true).
func IsScriptHashAddrID(id byte) bool {
	_, ok := scriptHashAddrIDs[id]
	return ok
}

// IsBech32SegwitPrefix returns whether the prefix is a known prefix for segwit
// addresses on any default or registered network.  This is used when decoding
// an address string into a specific address type.
func IsBech32SegwitPrefix(prefix string) bool {
	prefix = strings.ToLower(prefix)
	_, ok := bech32SegwitPrefixes[prefix]
	return ok
}

// RegisterHDKeyID registers a public and private hierarchical deterministic
// extended key ID pair.
//
// Non-standard HD version bytes, such as the ones documented in SLIP-0132,
// should be registered using this method for library packages to lookup key
// IDs (aka HD version bytes). When the provided key IDs are invalid, the
// ErrInvalidHDKeyID error will be returned.
//
// Reference:
//
//	SLIP-0132 : Registered HD version bytes for BIP-0032
//	https://github.com/satoshilabs/slips/blob/master/slip-0132.md
func RegisterHDKeyID(hdPublicKeyID []byte, hdPrivateKeyID []byte) error {
	if len(hdPublicKeyID) != 4 || len(hdPrivateKeyID) != 4 {
		return ErrInvalidHDKeyID
	}

	var keyID [4]byte
	copy(keyID[:], hdPrivateKeyID)
	hdPrivToPubKeyIDs[keyID] = hdPublicKeyID

	return nil
}

// HDPrivateKeyToPublicKeyID accepts a private hierarchical deterministic
// extended key id and returns the associated public key id.  When the provided
// id is not registered, the ErrUnknownHDKeyID error will be returned.
func HDPrivateKeyToPublicKeyID(id []byte) ([]byte, error) {
	if len(id) != 4 {
		return nil, ErrUnknownHDKeyID
	}

	var key [4]byte
	copy(key[:], id)
	pubBytes, ok := hdPrivToPubKeyIDs[key]
	if !ok {
		return nil, ErrUnknownHDKeyID
	}

	return pubBytes, nil
}

// newHashFromStr converts the passed big-endian hex string into a
// chainhash.Hash.  It only differs from the one available in chainhash in that
// it panics on an error since it will only (and must only) be called with
// hard-coded, and therefore known good, hashes.
func newHashFromStr(hexStr string) *chainhash.Hash {
	hash, err := chainhash.NewHashFromStr(hexStr)
	if err != nil {
		// Ordinarily I don't like panics in library code since it
		// can take applications down without them having a chance to
		// recover which is extremely annoying, however an exception is
		// being made in this case because the only way this can panic
		// is if there is an error in the hard-coded hashes.  Thus it
		// will only ever potentially panic on init and therefore is
		// 100% predictable.
		panic(err)
	}
	return hash
}

// newUtreexoHashFromStr is just returing a hash from string. Main difference from
// the newHashFromStr function is that this doesn't reverse the string.
func newUtreexoHashFromStr(hexStr string) utreexo.Hash {
	hash, err := hex.DecodeString(hexStr)
	if err != nil {
		// Ordinarily I don't like panics in library code since it
		// can take applications down without them having a chance to
		// recover which is extremely annoying, however an exception is
		// being made in this case because the only way this can panic
		// is if there is an error in the hard-coded hashes.  Thus it
		// will only ever potentially panic on init and therefore is
		// 100% predictable.
		panic(err)
	}
	return *(*[32]byte)(hash)
}

func init() {
	// Register all default networks when the package is initialized.
	mustRegister(&MainNetParams)
	mustRegister(&TestNet3Params)
	mustRegister(&RegressionNetParams)
	mustRegister(&SimNetParams)
}
