// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package utreexoparams defines the utreexo parameters for each network.
package utreexoparams

import (
	"encoding/hex"
	"time"

	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/utreexo/utreexo"
)

const (
	// Mainnet AssumeUtreexo and TTL heights.
	mainnetAssumeUtreexoHeight = 943_013
	mainnetTTLNumLeaves        = 943_014

	// Testnet3 AssumeUtreexo and TTL heights.
	testnetAssumeUtreexoHeight = 4_898_397
	testnetTTLNumLeaves        = 4_898_398

	// Signet AssumeUtreexo and TTL heights.
	signetAssumeUtreexoHeight = 297_353
	signetTTLNumLeaves        = 294_688
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
