// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package utreexoparams defines utreexod's parameters for each network: the
// utreexo parameters, and chaincfg's network parameters with utreexod's DNS
// seeds and checkpoints.
package utreexoparams

import (
	"bytes"
	"encoding/hex"
	"time"

	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/utreexo/utreexo"
)

const (
	// Mainnet AssumeUtreexo, TTL, and last checkpoint heights.
	mainnetAssumeUtreexoHeight  = 943_013
	mainnetTTLNumLeaves         = 943_014
	mainnetLastCheckpointHeight = 943_013

	// Testnet3 AssumeUtreexo, TTL, and last checkpoint heights.
	testnetAssumeUtreexoHeight  = 4_898_397
	testnetTTLNumLeaves         = 4_898_398
	testnetLastCheckpointHeight = 4_898_397

	// Signet AssumeUtreexo, TTL, and last checkpoint heights.
	signetAssumeUtreexoHeight  = 297_353
	signetTTLNumLeaves         = 294_688
	signetLastCheckpointHeight = 294_687
)

// Compile-time checks to ensure the AssumeUtreexo block height is greater
// than or equal to the TTL state height. NumLeaves - 1 is the height as
// there is one leaf per block starting from block 0.
var _ [mainnetAssumeUtreexoHeight - (mainnetTTLNumLeaves - 1)]struct{}
var _ [testnetAssumeUtreexoHeight - (testnetTTLNumLeaves - 1)]struct{}
var _ [signetAssumeUtreexoHeight - (signetTTLNumLeaves - 1)]struct{}

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

var (
	// DefaultSignetDNSSeeds is the list of seed nodes for the default
	// (public, Taproot enabled) signet network.
	DefaultSignetDNSSeeds = []chaincfg.DNSSeed{
		{Host: "signet.seed.utreexo.net.", HasFiltering: true},          // utreexo.net (Chris G.), supports utreexo service filtering
		{Host: "signet.seed.dlsouza.lol.", HasFiltering: true},          // Davidson Souza, supports filtering, including utreexo (1 << 24)
		{Host: "signetseed.calvinkim.info.", HasFiltering: true},        // Calvin Kim, supports filtering, including utreexo (1 << 24)
		{Host: "signetmanualseed.calvinkim.info.", HasFiltering: false}, // Only returns utreexo peers.
		{Host: "178.128.221.177", HasFiltering: false},
		{Host: "2a01:7c8:d005:390::5", HasFiltering: false},
		{Host: "v7ajjeirttkbnt32wpy3c6w3emwnfr3fkla7hpxcfokr3ysd3kqtzmqd.onion:38333", HasFiltering: false},
	}

	// sigNetCheckpoints are the checkpoints of the default signet network,
	// ordered from oldest to newest.
	sigNetCheckpoints = []chaincfg.Checkpoint{
		{Height: 20_000, Hash: newHashFromStr("000000d86368960eddbf7e127f8ba93a56efe71420b5dd8dbf8b0a68fa9ebbd1")},
		{Height: 40_000, Hash: newHashFromStr("0000014086ddfe6836bd52179c2ce1ce5eb8a9b85aee87c18be05c605723793c")},
		{Height: 60_000, Hash: newHashFromStr("00000130ab66a74ee232acb3f7ae35f9763dcec197b82b22f482ad0ea4c801f1")},
		{Height: 80_000, Hash: newHashFromStr("0000011f3e29efed437f858f1757a7a8ac406194cc80ea2948ed543994962416")},
		{Height: 100_000, Hash: newHashFromStr("0000008753108390007b3f5c26e5d924191567e147876b84489b0c0cf133a0bf")},
		{Height: 120_000, Hash: newHashFromStr("0000011b6acd2af1a7dc04a2c88a7b2c3980ebd9375dc8d52d331e715deeffcb")},
		{Height: 150_000, Hash: newHashFromStr("0000013d778ba3f914530f11f6b69869c9fab54acff85acd7b8201d111f19b7f")},
		{Height: 170_000, Hash: newHashFromStr("00000041c812a89f084f633e4cf47e819a2f6b1c0a15162355a930410522c99d")},
		{Height: 193_792, Hash: newHashFromStr("000000408463e4809d3a493baf8f17f25a919f883824f5b42247402cfeec1b73")},
		{Height: 231_620, Hash: newHashFromStr("0000012f65a81923ad36ee7d6a0d0ab2c7880e1390fbb4a87b2ebab5ee956d2e")},
		{Height: 270_566, Hash: newHashFromStr("00000004d85480599a5997038d256ea2a31a8e9dca48b1e4c0298063cb016fd6")},
		{Height: signetLastCheckpointHeight, Hash: newHashFromStr("000000057858aabfe204e2e2005c33567c6e310c90467d49bea36e4343f18d53")},
	}
)

// MainNetParams is chaincfg.MainNetParams with utreexod's DNS seeds and
// checkpoints.
var MainNetParams = func() chaincfg.Params {
	params := chaincfg.MainNetParams
	params.DNSSeeds = []chaincfg.DNSSeed{
		{Host: "seed.calvinkim.info.", HasFiltering: true},        // Calvin Kim, supports filtering, including utreexo (1 << 24)
		{Host: "manualseed.calvinkim.info.", HasFiltering: false}, // Only returns utreexo peers.
		{Host: "seed.bitcoin.sipa.be", HasFiltering: true},
		{Host: "dnsseed.bluematt.me", HasFiltering: true},
		{Host: "dnsseed.bitcoin.dashjr.org", HasFiltering: false},
		{Host: "seed.bitnodes.io", HasFiltering: false},
		{Host: "seed.bitcoin.jonasschnelli.ch", HasFiltering: true},
	}

	// Checkpoints ordered from oldest to newest.
	params.Checkpoints = []chaincfg.Checkpoint{
		{Height: 11111, Hash: newHashFromStr("0000000069e244f73d78e8fd29ba2fd2ed618bd6fa2ee92559f542fdb26e7c1d")},
		{Height: 33333, Hash: newHashFromStr("000000002dd5588a74784eaa7ab0507a18ad16a236e7b1ce69f00d7ddfb5d0a6")},
		{Height: 74000, Hash: newHashFromStr("0000000000573993a3c9e41ce34471c079dcf5f52a0e824a81e7f953b8661a20")},
		{Height: 105000, Hash: newHashFromStr("00000000000291ce28027faea320c8d2b054b2e0fe44a773f3eefb151d6bdc97")},
		{Height: 134444, Hash: newHashFromStr("00000000000005b12ffd4cd315cd34ffd4a594f430ac814c91184a0d42d2b0fe")},
		{Height: 168000, Hash: newHashFromStr("000000000000099e61ea72015e79632f216fe6cb33d7899acb35b75c8303b763")},
		{Height: 193000, Hash: newHashFromStr("000000000000059f452a5f7340de6682a977387c17010ff6e6c3bd83ca8b1317")},
		{Height: 210000, Hash: newHashFromStr("000000000000048b95347e83192f69cf0366076336c639f9b7228e9ba171342e")},
		{Height: 216116, Hash: newHashFromStr("00000000000001b4f4b433e81ee46494af945cf96014816a4e2370f11b23df4e")},
		{Height: 225430, Hash: newHashFromStr("00000000000001c108384350f74090433e7fcf79a606b8e797f065b130575932")},
		{Height: 250000, Hash: newHashFromStr("000000000000003887df1f29024b06fc2200b55f8af8f35453d7be294df2d214")},
		{Height: 267300, Hash: newHashFromStr("000000000000000a83fbd660e918f218bf37edd92b748ad940483c7c116179ac")},
		{Height: 279000, Hash: newHashFromStr("0000000000000001ae8c72a0b0c301f67e3afca10e819efa9041e458e9bd7e40")},
		{Height: 300255, Hash: newHashFromStr("0000000000000000162804527c6e9b9f0563a280525f9d08c12041def0a0f3b2")},
		{Height: 319400, Hash: newHashFromStr("000000000000000021c6052e9becade189495d1c539aa37c58917305fd15f13b")},
		{Height: 343185, Hash: newHashFromStr("0000000000000000072b8bf361d01a6ba7d445dd024203fafc78768ed4368554")},
		{Height: 352940, Hash: newHashFromStr("000000000000000010755df42dba556bb72be6a32f3ce0b6941ce4430152c9ff")},
		{Height: 382320, Hash: newHashFromStr("00000000000000000a8dc6ed5b133d0eb2fd6af56203e4159789b092defd8ab2")},
		{Height: 400000, Hash: newHashFromStr("000000000000000004ec466ce4732fe6f1ed1cddc2ed4b328fff5224276e3f6f")},
		{Height: 430000, Hash: newHashFromStr("000000000000000001868b2bb3a285f3cc6b33ea234eb70facf4dcdf22186b87")},
		{Height: 460000, Hash: newHashFromStr("000000000000000000ef751bbce8e744ad303c47ece06c8d863e4d417efc258c")},
		{Height: 490000, Hash: newHashFromStr("000000000000000000de069137b17b8d5a3dfbd5b145b2dcfb203f15d0c4de90")},
		{Height: 520000, Hash: newHashFromStr("0000000000000000000d26984c0229c9f6962dc74db0a6d525f2f1640396f69c")},
		{Height: 550000, Hash: newHashFromStr("000000000000000000223b7a2298fb1c6c75fb0efc28a4c56853ff4112ec6bc9")},
		{Height: 560000, Hash: newHashFromStr("0000000000000000002c7b276daf6efb2b6aa68e2ce3be67ef925b3264ae7122")},
		{Height: 563378, Hash: newHashFromStr("0000000000000000000f1c54590ee18d15ec70e68c8cd4cfbadb1b4f11697eee")},
		{Height: 597379, Hash: newHashFromStr("00000000000000000005f8920febd3925f8272a6a71237563d78c2edfdd09ddf")},
		{Height: 623950, Hash: newHashFromStr("0000000000000000000f2adce67e49b0b6bdeb9de8b7c3d7e93b21e7fc1e819d")},
		{Height: 654683, Hash: newHashFromStr("0000000000000000000b9d2ec5a352ecba0592946514a92f14319dc2b367fc72")},
		{Height: 691719, Hash: newHashFromStr("00000000000000000008a89e854d57e5667df88f1cdef6fde2fbca1de5b639ad")},
		{Height: 724466, Hash: newHashFromStr("000000000000000000052d314a259755ca65944e68df6b12a067ea8f1f5a7091")},
		{Height: 751565, Hash: newHashFromStr("00000000000000000009c97098b5295f7e5f183ac811fb5d1534040adb93cabd")},
		{Height: 841776, Hash: newHashFromStr("00000000000000000000a174eebf5b9df9b9ebf062cc3c503a2024e7d9a618b6")},
		{Height: mainnetLastCheckpointHeight, Hash: newHashFromStr("00000000000000000001c595730bd4a5fb0e2b35af70882962ce7ae602f48aff")},
	}

	return params
}()

// TestNet3Params is chaincfg.TestNet3Params with utreexod's DNS seeds and
// checkpoints.
var TestNet3Params = func() chaincfg.Params {
	params := chaincfg.TestNet3Params
	params.DNSSeeds = []chaincfg.DNSSeed{
		{Host: "testnetseed.calvinkim.info.", HasFiltering: true},        // Calvin Kim, supports filtering, including utreexo (1 << 24)
		{Host: "testnetmanualseed.calvinkim.info.", HasFiltering: false}, // Only returns utreexo peers.
		{Host: "testnet-seed.bitcoin.jonasschnelli.ch", HasFiltering: true},
		{Host: "testnet-seed.bitcoin.schildbach.de", HasFiltering: false},
		{Host: "seed.tbtc.petertodd.org", HasFiltering: true},
		{Host: "testnet-seed.bluematt.me", HasFiltering: false},
	}

	// Checkpoints ordered from oldest to newest.
	params.Checkpoints = []chaincfg.Checkpoint{
		{Height: 546, Hash: newHashFromStr("000000002a936ca763904c3c35fce2f3556c559c0214345d31b1bcebf76acb70")},
		{Height: 100000, Hash: newHashFromStr("00000000009e2958c15ff9290d571bf9459e93b19765c6801ddeccadbb160a1e")},
		{Height: 200000, Hash: newHashFromStr("0000000000287bffd321963ef05feab753ebe274e1d78b2fd4e2bfe9ad3aa6f2")},
		{Height: 300001, Hash: newHashFromStr("0000000000004829474748f3d1bc8fcf893c88be255e6d7f571c548aff57abf4")},
		{Height: 400002, Hash: newHashFromStr("0000000005e2c73b8ecb82ae2dbc2e8274614ebad7172b53528aba7501f5a089")},
		{Height: 500011, Hash: newHashFromStr("00000000000929f63977fbac92ff570a9bd9e7715401ee96f2848f7b07750b02")},
		{Height: 600002, Hash: newHashFromStr("000000000001f471389afd6ee94dcace5ccc44adc18e8bff402443f034b07240")},
		{Height: 700000, Hash: newHashFromStr("000000000000406178b12a4dea3b27e13b3c4fe4510994fd667d7c1e6a3f4dc1")},
		{Height: 800010, Hash: newHashFromStr("000000000017ed35296433190b6829db01e657d80631d43f5983fa403bfdb4c1")},
		{Height: 900000, Hash: newHashFromStr("0000000000356f8d8924556e765b7a94aaebc6b5c8685dcfa2b1ee8b41acd89b")},
		{Height: 1000007, Hash: newHashFromStr("00000000001ccb893d8a1f25b70ad173ce955e5f50124261bbbc50379a612ddf")},
		{Height: 1100007, Hash: newHashFromStr("00000000000abc7b2cd18768ab3dee20857326a818d1946ed6796f42d66dd1e8")},
		{Height: 1200007, Hash: newHashFromStr("00000000000004f2dc41845771909db57e04191714ed8c963f7e56713a7b6cea")},
		{Height: 1300007, Hash: newHashFromStr("0000000072eab69d54df75107c052b26b0395b44f77578184293bf1bb1dbd9fa")},
		{Height: 1354312, Hash: newHashFromStr("0000000000000037a8cd3e06cd5edbfe9dd1dbcc5dacab279376ef7cfc2b4c75")},
		{Height: 1580000, Hash: newHashFromStr("00000000000000b7ab6ce61eb6d571003fbe5fe892da4c9b740c49a07542462d")},
		{Height: 1692000, Hash: newHashFromStr("000000000000056c49030c174179b52a928c870e6e8a822c75973b7970cfbd01")},
		{Height: 1864000, Hash: newHashFromStr("000000000000006433d1efec504c53ca332b64963c425395515b01977bd7b3b0")},
		{Height: 2010000, Hash: newHashFromStr("0000000000004ae2f3896ca8ecd41c460a35bf6184e145d91558cece1c688a76")},
		{Height: 2143398, Hash: newHashFromStr("00000000000163cfb1f97c4e4098a3692c8053ad9cab5ad9c86b338b5c00b8b7")},
		{Height: 2344474, Hash: newHashFromStr("0000000000000004877fa2d36316398528de4f347df2f8a96f76613a298ce060")},
		{Height: 2810937, Hash: newHashFromStr("000000000000005a4f7ec7942d57353c6200a284e021bc5c6be0bf1415890875")},
		{Height: testnetLastCheckpointHeight, Hash: newHashFromStr("000000006e2ff491ed5b4254949d1217eb5e642fd9935e62b89f10990c450e1e")},
	}

	return params
}()

// SigNetParams is chaincfg.SigNetParams with utreexod's DNS seeds and
// checkpoints.
var SigNetParams = CustomSignetParams(
	chaincfg.DefaultSignetChallenge, DefaultSignetDNSSeeds,
)

// CustomSignetParams returns chaincfg.CustomSignetParams for the challenge and
// DNS seeds, with utreexod's checkpoints when the challenge is the default
// signet's.
func CustomSignetParams(challenge []byte, dnsSeeds []chaincfg.DNSSeed) chaincfg.Params {
	params := chaincfg.CustomSignetParams(challenge, dnsSeeds)
	if bytes.Equal(challenge, chaincfg.DefaultSignetChallenge) {
		params.Checkpoints = sigNetCheckpoints
	}

	return params
}

// netParams holds the utreexo parameters for a network.
type netParams struct {
	// assumeUtreexoPoint is the utreexo roots that a utreexo node can
	// start off of.
	assumeUtreexoPoint AssumeUtreexo

	// ttl is committed so that nodes during ibd are able to check
	// the received ttls from other peers.
	ttl TTLState
}

// params holds the utreexo parameters of the networks that have them.
var params = map[wire.BitcoinNet]netParams{
	wire.MainNet: {
		assumeUtreexoPoint: AssumeUtreexo{
			BlockHash:   newHashFromStr("00000000000000000001c595730bd4a5fb0e2b35af70882962ce7ae602f48aff"),
			BlockHeight: mainnetAssumeUtreexoHeight,
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
		ttl: TTLState{
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
					NumLeaves: mainnetTTLNumLeaves,
				},
			},
		},
	},
	wire.TestNet3: {
		assumeUtreexoPoint: AssumeUtreexo{
			BlockHash:   newHashFromStr("000000006e2ff491ed5b4254949d1217eb5e642fd9935e62b89f10990c450e1e"),
			BlockHeight: testnetAssumeUtreexoHeight,
			Bits:        486_604_799,
			BlockSize:   7_573,
			BlockWeight: 22_324,
			NumTxns:     26,
			TotalTxns:   536_226_015,
			MedianTime:  time.Unix(1_774_936_634, 0),
			NumLeaves:   666_424_427,
			Roots: []utreexo.Hash{
				newUtreexoHashFromStr("d8f8d92d389122c7fadc84d0dfb5bef908ad120cae59bde29ba6f8b34758f610"),
				newUtreexoHashFromStr("20de6d3093232d3a5a7e34a1c97214e71b623ba6f0bd8d67a7c966cda8ff83d0"),
				newUtreexoHashFromStr("302f7fa4d9cf7979d17204b70cba9ded09b406a29491b565d5a9582ea6751139"),
				newUtreexoHashFromStr("d0ec6f7b57384b8c4fdc7346b41fd892f9b77a53511e41ca57a3286bed5f9215"),
				newUtreexoHashFromStr("46332adcabcb02b61b1b7aaa80ee2843ec8531de5ac0b6f9b882899bb3c242bd"),
				newUtreexoHashFromStr("c1b1cb0a622f7ae1d56cd21933e11e32d8ce287ca7b7ee58ed6ee2ab1ca49b4b"),
				newUtreexoHashFromStr("51d0bbb2b733101e33817ef9b6037388f826a07e8c07241ec56db112eed96b53"),
				newUtreexoHashFromStr("cae060dd671ed06f4d603410271f6150da6dbe1854234fe71dcc5020a2ab1e7e"),
				newUtreexoHashFromStr("c3afbc239780aec298fd58bacbb86cf8aeada9306ce9cbc818a74a1b3f6904c5"),
				newUtreexoHashFromStr("be1cadde773293c50a53e69a07f5d0edbf87414e9ef2af950a947600453dc597"),
				newUtreexoHashFromStr("95ec9ca7fbd21025741c5c0a5fc8890f6c05ca1dd43407a0dd20f658c71985c7"),
				newUtreexoHashFromStr("d752187324c3cf9f723870fee0ef80d30531f49065661acbdcc825098a8c160e"),
				newUtreexoHashFromStr("a861eb39ba41580cbd431452fa0260bd5343dfd1a9025098e958f7b37a91c332"),
				newUtreexoHashFromStr("30551eedd678e3a06f3c3d5b62ef74086aff8b004ca883d8707355f9cb73e259"),
				newUtreexoHashFromStr("8a90b8d24e6bfa83d9ccbccbb95d10fb8538b7cda2113b0bf982e9fdb58289a7"),
				newUtreexoHashFromStr("26a06ee46fb1e4e0669fcfd70cfe6d00d93f583f4f9c9abb6ee30dca43e52683"),
				newUtreexoHashFromStr("5bfbfe4895f4dff67ffbb4adccf6482422c847717470d6ed44a2dcaa19a8ac4e"),
			},
		},
		ttl: TTLState{
			Stump: []utreexo.Stump{
				{
					Roots: []utreexo.Hash{
						newUtreexoHashFromStr("96dc8c4d526a4037f9c9a4dabc0a129d40d79129b105ac3bec1697e0e8e82521"),
						newUtreexoHashFromStr("d218da67ec28ecac71331511cbb5249fb89f6fbddced1204d9f4c5dd54641310"),
						newUtreexoHashFromStr("6c4d4f5b268a4968c685facb7dda84f9c611b327f544ece63e154944b308f396"),
						newUtreexoHashFromStr("44d174b4d5c7295dd455ce6d7ea8f1d7eee49d5be227b88caac11ac0f247fbf8"),
						newUtreexoHashFromStr("360afb726247f03e9b44ccda2d0caf39d4b96fac5b41eb51a63c5968c55f89c3"),
						newUtreexoHashFromStr("1dcb85501c5547db4727c537fddd93dcd7e25e150ce36551bf757dc61f0ea90c"),
						newUtreexoHashFromStr("b5a3c5dce6da86d2054a5ec23d97c210ab061d1afb85619561aba96ed463ca5a"),
						newUtreexoHashFromStr("0295f5f121623282fc6d8c1ea1014f747b1a87e0775a40e26f6ebe7cc3fba8c4"),
						newUtreexoHashFromStr("eb9f68071f281492fd8af745fa72e2c9baaa64196329a0c44f398c61c01c2c31"),
						newUtreexoHashFromStr("e28f158c8fc9b4c0670c3b4c12b77412fb4c4a69e11ed4c13bdba3bd6f1a6bc9"),
						newUtreexoHashFromStr("149b98866f4613389a9291fc810277a5e584d483dbb0fb606e67af3b8c3543c1"),
						newUtreexoHashFromStr("a8db6a0e050cbfabd5d1d18d14fea803080338394a31958bebcc6039ea22a463"),
						newUtreexoHashFromStr("693a82217188a277387170c37d3ec71d42b598666e8df11b011c2a545a8b7abd"),
						newUtreexoHashFromStr("376664a8c8995362918b3897570f6f5b66d8c167ce327a00b95fa28ba88a287c"),
					},
					NumLeaves: testnetTTLNumLeaves,
				},
			},
		},
	},
	wire.SigNet: {
		assumeUtreexoPoint: AssumeUtreexo{
			BlockHash:   newHashFromStr("0000000d0ffcda2be44be940b3f2ba0b9e1a5e60dde6d074a7dd749ef2af2061"),
			BlockHeight: signetAssumeUtreexoHeight,
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
		},
		ttl: TTLState{
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
					NumLeaves: signetTTLNumLeaves,
				},
			},
		},
	},
}

// AssumeUtreexoPoint returns the utreexo roots that a utreexo node on the
// network can start off of.  It returns an empty AssumeUtreexo for a network
// without one.
func AssumeUtreexoPoint(net wire.BitcoinNet) AssumeUtreexo {
	return params[net].assumeUtreexoPoint
}

// TTL returns the ttls committed for the network so that nodes during ibd are
// able to check the received ttls from other peers.  It returns an empty
// TTLState for a network without them.
func TTL(net wire.BitcoinNet) TTLState {
	return params[net].ttl
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
