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
	},

	AssumeUtreexoPoint: AssumeUtreexo{
		BlockHash:   newHashFromStr("00000000000000000000a174eebf5b9df9b9ebf062cc3c503a2024e7d9a618b6"),
		BlockHeight: 841_776,
		Bits:        386_085_339,
		BlockSize:   2_050_273,
		BlockWeight: 3_993_490,
		NumTxns:     4_915,
		TotalTxns:   998_085_718,
		MedianTime:  time.Unix(1_714_648_629, 0),
		NumLeaves:   2_515_124_998,
		Roots: []utreexo.Hash{
			newUtreexoHashFromStr("301566e2b5aa2af3ea1817218869808dee72f99b49f98bc9d1b6f837915c05e7"),
			newUtreexoHashFromStr("2a108c0d59f1fc00b623f4aaf4599c81208ee5a5b15bb91b638c9c604da59142"),
			newUtreexoHashFromStr("9a2c0db4419a1984966f07d21fdd265bfa73c86bcdf526e72c78f7eace0670aa"),
			newUtreexoHashFromStr("ebca53c3711de97cf3054092960d227b1abd3504d5d244b8e69f446f6c5b1bf8"),
			newUtreexoHashFromStr("701748078f1545e9376981863772c1574f6ccb5c8668ab0bab4cb6627f509122"),
			newUtreexoHashFromStr("5aa49c03c8d14cdabd17d77affbabe4eeb97a88f03c568fd7151376246ecfaaa"),
			newUtreexoHashFromStr("119d723e97eac80f7ab9e349a55d9cacafe443e20705eca06b56163dd43b4c3d"),
			newUtreexoHashFromStr("159fe72e5bb3f5037a1867f5cf2819fc6642afebe0fe99782869592e189cd39f"),
			newUtreexoHashFromStr("5561d2ff4fb4abe896e8be4246c2b50d0c092502d47ecbd17be9fcc7186547a3"),
			newUtreexoHashFromStr("ae4478f664ff3daea18eb462a2c322a4d3aa28e73544c1e36ff6d8545e90c98c"),
			newUtreexoHashFromStr("277c4287c1d203c50853b34667336a57ed17dd8e97196668f2658cf84132d715"),
			newUtreexoHashFromStr("d46a01a3e0a1f4e4b4f19243bc7e8d3f90727497cc38f544ece4a72a6a1fef09"),
			newUtreexoHashFromStr("078fafc7b0c89e75bdb5569a1e246eb712b7c734c2f9e033aff172dd31d377a3"),
			newUtreexoHashFromStr("474627a0d790ccbded523c30f3bd469935929de4d3b935bb70f43c59755a499d"),
			newUtreexoHashFromStr("72842dc330579ed0b8e0b8f938758bfff4dbb3814be240785f2bd6314c00497d"),
		},
	},

	TTL: TTLState{
		Stump: []utreexo.Stump{
			{
				Roots: []utreexo.Hash{
					newUtreexoHashFromStr("235626f78eec6d4b6758bd1a4281bc9466eb6f4c237a016a6b877151a383ad79"),
					newUtreexoHashFromStr("edd565c31eb5ccc49a349c144bfed1f4db2e6e6b308a1616bad36bdb160dd720"),
					newUtreexoHashFromStr("0fd3ab9baaf27eac36316bb82f0080a1e194cdc8e5ced28a7c3c9c566f56bd01"),
					newUtreexoHashFromStr("30b004f3ef18f94016022b458631f33b2d9f4ce25c00beed183b77d788c5cd88"),
					newUtreexoHashFromStr("6c2a9c566e66cb5b12b877851428ce1a1eefac7ed79dd4d7d686ce49c5388796"),
					newUtreexoHashFromStr("8c2e1f172b82c88bf8589d9d637e5a2e5fadf00f1e049902f5b80ecfef063ae9"),
					newUtreexoHashFromStr("5ef854e6bec2d2ea5b26e776270f80db030c66bda7a468485d4fd031d3214ff6"),
					newUtreexoHashFromStr("4a87e88e8249a2c2355a758031da7f3830c8125c761abb5f717733acef665167"),
					newUtreexoHashFromStr("269ffb380ff2a9838fc9ff43485a4ccd2b606caca0f23a8dfea551eacfddf18a"),
					newUtreexoHashFromStr("cbda0895ebc1beb38c0f8389dc610164ea0b169c01c74e38e60e02e847b6b988"),
				},
				NumLeaves: 901_337,
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
			BlockHash:   newHashFromStr("0000000953a52bbfa77538dbe50a2ba2cfbb6c1da933612d243feb0bbcb13b37"),
			BlockHeight: 264_674,
			Bits:        487_904_072,
			BlockSize:   201_066,
			BlockWeight: 595_920,
			NumTxns:     533,
			TotalTxns:   25_037_798,
			MedianTime:  time.Unix(1_754_901_979, 0),
			NumLeaves:   95_695_754,
			Roots: []utreexo.Hash{
				newUtreexoHashFromStr("b1edcdc22eddb842666b5eb10f629a33f8937a152ea8dbccdc4aa2678ff3bbc2"),
				newUtreexoHashFromStr("97f586516bfe52a9d8dc2a86fd84a1f54e16a845e7ef0c37698d30554133cee2"),
				newUtreexoHashFromStr("adfc5bb270172de242e700ba49fe4e73581e22accde3cdf2a3b34e05444de26c"),
				newUtreexoHashFromStr("7348f96d26594310041cee742e653eaa0095d34cd3c66334e895586c516a1907"),
				newUtreexoHashFromStr("9cd325ab7a003dc0e0eff7d97cd9a4d9c1c182dd740d41ab19cde887d0e67d24"),
				newUtreexoHashFromStr("8625a2b462268b13a410d38a3b573a0803677ffa488b1356900d1f389f5cdbb6"),
				newUtreexoHashFromStr("45ea587cdb045807f54566e24f822d239de7932836671bcf7815f7fbf1c01655"),
				newUtreexoHashFromStr("ccce96c19d574f18851f189f142120509932b4275dfe48bbd39a2411e3e4eaf4"),
				newUtreexoHashFromStr("c5f3a233545cdb1066d6dfd0bcd66a39473aa3e9f2663bc347afc57683d07df0"),
				newUtreexoHashFromStr("76c2bc17dd0460c3de178198fa81d55ebb3261ad4201229f90a4e6da3c3f601c"),
				newUtreexoHashFromStr("10a4790f420563e37f3ca2900648959de52b68d322a470c21e71a4484c75faca"),
				newUtreexoHashFromStr("1551ed34cf1397b0ec56b9a95bc6d5a59475dd374badcea07b796821281118a4"),
				newUtreexoHashFromStr("5b258c6c5fd22bf5d7f68111e64265507f66b7fb0e151f8d3dd381157363f8a7"),
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
						newUtreexoHashFromStr("809e49cbef1eeade7badd8b3477b9e97df7cba0b1c0eabeddf7690c0a80acb67"),
						newUtreexoHashFromStr("753f7f5ae0bcac2386eb1a1747c631afab2d6b4ffbb05e2928e62ea25e988628"),
						newUtreexoHashFromStr("94a6be116ad86bd4d98a4fd9ce2ff06b617fae01415adb35043f3514619f3ce2"),
						newUtreexoHashFromStr("947b29604e89ac7be6e1e29bda4de0042eeaefd0fe71862b2fd0824ca6809b5b"),
						newUtreexoHashFromStr("031b0537dc41de2b57825c4d9c5d25a8d603cfae987a090d1f0fa1f02062936c"),
						newUtreexoHashFromStr("6f30341345e5879f28ca7081eef177c644f4eb42bf92312f426cef7e48077fbc"),
						newUtreexoHashFromStr("a965adea2d8839c5daa4d41abc40d4eea48c72a05682bef20d788b67979b62a2"),
						newUtreexoHashFromStr("2ae278a882a2c1300da5ce47082e3e7ec21078328c800a6e3d68581897cf91a7"),
					},
					NumLeaves: 270_567,
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
