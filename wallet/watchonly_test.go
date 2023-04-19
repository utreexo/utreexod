// Copyright (c) 2022-2023 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package wallet

import (
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/wire"
)

func TestLeafDataExtrasJSONMarshal(t *testing.T) {
	tests := []struct {
		name       string
		relevantTx LeafDataExtras
	}{
		{
			name: "txid 60c08dafe80150afe2cabdba24e90853b072d02bc5111546da083d80b50ed66a",
			relevantTx: LeafDataExtras{
				LeafData: wire.LeafData{
					BlockHash: func() chainhash.Hash {
						str := "00000000000000000003b05b4cafbac9c67afb76c3c3efb89aeb4db086c13f2b"
						hash, err := chainhash.NewHashFromStr(str)
						if err != nil {
							panic(err)
						}
						return *hash
					}(),
					OutPoint: func() wire.OutPoint {
						str := "60c08dafe80150afe2cabdba24e90853b072d02bc5111546da083d80b50ed66a"
						hash, err := chainhash.NewHashFromStr(str)
						if err != nil {
							panic(err)
						}
						return *wire.NewOutPoint(hash, 0)
					}(),
					Height:                784611,
					IsCoinBase:            true,
					Amount:                631465945,
					ReconstructablePkType: wire.WitnessV0PubKeyHashTy,
					PkScript: func() []byte {
						str := "001435f6de260c9f3bdee47524c473a6016c0c055cb9"
						bytes, err := hex.DecodeString(str)
						if err != nil {
							panic(err)
						}
						return bytes
					}(),
				},
				BlockIdx:    0,
				BlockHeight: 784611,
			},
		},
		{
			name: "txid 64d01c1d5493a686f3d3d8e42e58c835a8cfc3296320768f90acb8e9c5fe5d65",
			relevantTx: LeafDataExtras{
				LeafData: wire.LeafData{
					BlockHash: func() chainhash.Hash {
						str := "0000000000000000001861945b5d7ecc1c70c6b45a65ba266a3059a367b0ec39"
						hash, err := chainhash.NewHashFromStr(str)
						if err != nil {
							panic(err)
						}
						return *hash
					}(),
					OutPoint: func() wire.OutPoint {
						str := "64d01c1d5493a686f3d3d8e42e58c835a8cfc3296320768f90acb8e9c5fe5d65"
						hash, err := chainhash.NewHashFromStr(str)
						if err != nil {
							panic(err)
						}
						return *wire.NewOutPoint(hash, 0)
					}(),
					Height:                541117,
					IsCoinBase:            false,
					Amount:                1150000,
					ReconstructablePkType: wire.PubKeyHashTy,
					PkScript: func() []byte {
						str := "001435f6de260c9f3bdee47524c473a6016c0c055cb9"
						bytes, err := hex.DecodeString(str)
						if err != nil {
							panic(err)
						}
						return bytes
					}(),
				},
				BlockIdx:    175,
				BlockHeight: 541117,
			},
		},
	}

	for _, test := range tests {
		txo := test.relevantTx
		// Marshal the LeafDataExtras to JSON.
		bytes, err := json.Marshal(txo)
		if err != nil {
			t.Fatalf("failed to marshal LeafDataExtras: %v", err)
		}

		// Unmarshal the JSON bytes back into a LeafDataExtras struct.
		var txAndIndex LeafDataExtras
		if err := json.Unmarshal(bytes, &txAndIndex); err != nil {
			t.Fatalf("failed to unmarshal JSON into LeafDataExtras: %v", err)
		}

		// Ensure the unmarshaled LeafDataExtras struct is the same as the original.
		if !reflect.DeepEqual(test.relevantTx, txAndIndex) {
			t.Fatalf("failed test %s. Unmarshaled LeafDataExtras does "+
				"not match the original", test.name)
		}
	}
}

func TestWalletConfigMarshal(t *testing.T) {
	tests := []struct {
		name   string
		config WalletConfig
	}{
		{
			name: "empty",
			config: WalletConfig{
				Net:          "mainnet",
				Addresses:    make(map[string]struct{}),
				ExtendedKeys: make(map[string]HDVersion),
				GapLimit:     20,
			},
		},
		{
			name: "signet example",
			config: WalletConfig{
				Net: "signet",
				ExtendedKeys: func() map[string]HDVersion {
					key1Str := "tpubDC6ej6KDdtPrhrGN1EHDymPL6pK6hcRyWE3r3tLn4W6ePHvk7LUEi86ynguY4KJjRD4kUmHYhC2C4UEdiyM9TT9vYgaRRsqezkQUkypSFUg"
					key2Str := "vpub5VJ2xNkCf2kjV1nymGNt5A4EYgefN9ktrWrJEwC7pRzkmjQG1Wy1GrZmCQPhsSkmp1MHtxEKakDvT23CDhUH5b5SH3BQvhLZWu9EtPJfU6U"

					m := make(map[string]HDVersion, 2)
					m[key1Str] = HDVersionTestNetBIP0084
					m[key2Str] = HDVersionTestNetBIP0084

					return m
				}(),
				Addresses: func() map[string]struct{} {
					m := make(map[string]struct{})
					m["tb1q6ruejymgrt8qdmfmc2c9t7ukl0kwk0hnsvfg74"] = struct{}{}
					m["tb1qu2ux2hwyp734ng039h7kkdnley2sn0gdfv94aw"] = struct{}{}
					return m
				}(),
				GapLimit: 20,
			},
		},
	}

	for _, test := range tests {
		bytes, err := json.Marshal(test.config)
		if err != nil {
			t.Fatalf("failed to marshal WalletConfig: %v", err)
		}

		// Unmarshal the JSON bytes back into a WalletConfig struct.
		var gotConfig WalletConfig
		if err := json.Unmarshal(bytes, &gotConfig); err != nil {
			t.Fatalf("failed to unmarshal JSON into WalletConfig: %v", err)
		}

		// Ensure the unmarshaled WalletConfig struct is the same as the original.
		if !reflect.DeepEqual(test.config, gotConfig) {
			t.Fatalf("unmarshaled walletConfig does not match the original")
		}
	}
}

func TestWalletStateMarshal(t *testing.T) {
	tests := []struct {
		name  string
		state WalletState
	}{
		{
			name: "emtpy mainnet",
			state: WalletState{
				BestHash:           *chaincfg.MainNetParams.GenesisHash,
				WatchedKeys:        make(map[string]map[string]bool),
				LastExternalIndex:  make(map[string]uint32),
				LastInternalIndex:  make(map[string]uint32),
				RelevantUtxos:      make(map[wire.OutPoint]LeafDataExtras),
				RelevantStxos:      make(map[wire.OutPoint]LeafDataExtras),
				RelevantTxs:        make(map[chainhash.Hash]RelevantTxData),
				RelevantMempoolTxs: make(map[chainhash.Hash]MempoolTx),
				UtreexoLeaves:      []utreexo.Hash{},
				UtreexoProof:       utreexo.Proof{Targets: []uint64{}, Proof: []utreexo.Hash{}},
			},
		},
		{
			name: "signet state",
			state: func() WalletState {
				bestHash, err := chainhash.NewHashFromStr("000000edbf4fcf082d63312f21249b987407a6a8b27b8dec3101f616d104565a")
				if err != nil {
					panic(err)
				}

				utreexoLeavesStr := []string{
					"b82cd2700652772c3330f846465a323f3d5c0d762ab33c9fb32bca72c45f52b4",
					"9fd47a39f1b639d71bd1e737ebd08cd445167814e3ffe4fed0f5f19ecbf2ce47",
					"68b6062e4ee54aa65af302df66f7b4ce5749ecf1aa5e5e46d9ad7e5a029e20bd",
					"23ab4cb0766b1caaee6c198d4abda580286054be4126df093dacdd0af54216ba",
					"0ea6ac936628e9dd0d6a84a7452ac07c577bdc0237c891e73d84b0e74aaecb03",
					"863f83f43480cdace724e497d7ead84eb5a4b1658900ace0d6c82e4eaaa8fabe",
					"0936eda8080d08a880cee2748e0cc187a6074323a0d699cac186194c54cdef28",
					"e9a48a26a1e18fe58208be067dd8b1c1401838e55ef9b195ad880536c7eb8134",
					"553fdeff7829998906eda4d7c4168c8e7b0517dc8db6a281ae10eec924371d46",
					"fbe29fb5d960b0f6d493c3010889bd3076d56f0ee6e61327671df1b42ea60114",
					"7add5e84f88269449142b5cc1863bfd224f410a476e09e40137e7fb2f51e4547",
					"e35b7c758aab25e759dbb76546b770168f61bc50d02d42e37b494076f015d646",
					"bc6067bf63300c0ee0e96b8512330b880090993debf771770e485f2a74b52201",
					"c81829d74dcf16091e4ba148d83a7ceac0cfe626ea539463107e80b78450e533",
					"fcfb27a56c7b3ed766d3b83c06f4926537a0abc47700de681e7f23d8b21dd3f9",
					"bbc67fe8638aa6d113ee4b7a81256c5e580841100327ad2361a8437951abff1b",
					"9c8b9f392d7905b34d98d78b03261a64e718952faadde3e3328d4f7ddb078dd3",
					"c710cba6c8f7513de8f5062005f7d9284cb3370de56e9fbede678f79000343ea",
					"d11698d6ee36c47fb451f549740eed824676c634ea7dcaca3501cefb398355da",
					"f955e3dca47b096d3bddf51d8546585f554c8637ed1d77493a8014ff4422329f",
					"a0362df070c660a969bf828ca1d2801c098e3bd91667104f602d058b52a377a2",
					"903b04fc254cbbcbfb345a0f59a40797280ae2a9a0e97e430a60391baf94e94f",
					"1edb400082fdf81bb8a1fbece0c128458a47ab1e25d6db024573afe3191030d4",
					"795d65bb3bd8a65215f966fc7831490118c8bf52b8ee7d53e369d3ab888046fb",
				}

				utreexoLeaves := make([]utreexo.Hash, len(utreexoLeavesStr))
				for i, str := range utreexoLeavesStr {
					hash, err := hex.DecodeString(str)
					if err != nil {
						panic(err)
					}

					utreexoLeaves[i] = (*(*[32]byte)(hash))
				}

				utreexoTargets := []uint64{
					3487104, 3487105, 3514204, 5932023, 5934312, 5951368, 5951369, 5951654,
					5951948, 5951949, 7161152, 7163620, 7168423, 7168513, 7169999, 7170137,
					7170139, 7170279, 7774460, 7774832, 7774938, 7774939, 7775018, 8081531,
				}

				utreexoProofStr := []string{
					"9fd95cbd96de33f32fad374d32852305f3500fbb34604bac9daf786707716cb3",
					"6afb56ac7e21c1db49f1157f057f6b994c185da31a580fecc7585c40df1cf286",
					"d0093c5ed8ea4eeb533f75c6f23bf1842862c47469c4b8f2b9dfe3c5960154c5",
					"24bffc5a14c2e9c14f23fc2bf7cc91e2e6a50c5c0ef0c67f8e25a190d8c5b5a1",
					"9afee485de937d10a9c92445258ca2e8df9db5196527628860c2bf2560bc5a2d",
					"768fff0ddcbfb147f9512f64d7b46a816a3028ed71b33cc419539a80b181e474",
					"29aa634f455b04b767a75cf84495e4832106a157a2ebbd1abb687ec217679bf5",
					"5aca555d08869fca1bfd55896179997aa5a3d91347c24b9c8e85a13ad1c21369",
					"a283f29ec5e6ce35eeb1f4ac6f8f1196ed0bd6cac26ac7a0c6170a4949a75fbd",
					"2f2cd6e4913c40f2060c125dbf03637c3a987a57077c818aed6466a0430aa1c4",
					"21e619ebd69e312ef801c4d95f1e7aec869d2d76386e4d8ed56eb985eee49bf2",
					"6a67de47a5cc94a629b961bd6142f6f9a5cd2420efbbdbecb012b07a5616ac3a",
					"222fe8383cd006cef190bdc2247a1a0e90863b152c1776d0dc6320c40d8c6726",
					"9133f94f852ce48d7f330ff55b943ac582c80e3e9c461975a9ea07dfe601f243",
					"3b16b668ac0f18e1663e9e10b8f2337c5d91cf574de715deef873090a64283e7",
					"ec26ca2ea24b23328e85c562db736e3681972bf683b94ea91fa67986e9aedbca",
					"237100a5421d2dd43a02191cc5235bd0ac2a29a9c5431f9322853bd32ac4fca2",
					"6aaf174e2bcb5eb89b155e20c4c119268949c1de6612657637048688ac277f29",
					"1e3dcf1beebc4d0b042b5f9f4fea19f9f4a51afb1e7d6c680048ccc3a6b947c3",
					"37b3a687a1fdff80f1ee997712a052bc2cc253b1f65d208856cd527fbfbb317a",
					"b277ba63e3382c1a3441ef2dbe6c443a985b0affe1f31c09a8780363d028e7e2",
					"cbef4c1ca08326847bc7f47a3d142064200bdad329539278ded940c69ad072a6",
					"61d9486a846e4b4bcd697131928a6e3f2cde109fd242662e925151551de3e8f9",
					"bb5d6fdfa2e65881ce4f2e1f1ac3d21d69a477c871dcb432259ff032ac00ea4f",
					"60fb3b29f5a4f68093509a09045e349a71edde61969e1b3e077ed67bbe0ba8ae",
					"df308cf83c22fbb0a1cf6461858f05043ff1ba4146c6f645faa61c15fdcf11f1",
					"151be815be2f58c2a9fff6470f3741a3e1a2d93aafedfa53f63fd48c3902f321",
					"6e7c590c905db0657286c0a113f8761d65e1b90e1dad59dc32a1a08284734c7b",
					"b98b22009fa4f57bab17af803285b3ceb8521df8824fd1f7c7c5bacf73bd314c",
					"bd5fe8e88464df3a121faebc8f9960f6b66e56c3dad1c05a15a5e63743a80074",
					"2b38bc3e6ca68beb177c804f3bab1477328cd44ab891523d5724540714667a8f",
					"dd1b97637954e08fb2252b23c3914ec336d408999113079511876b587f975e88",
					"b5cd4d0f9d22312d5a5a252c794bc4b0b9dd41b746b8a73ab0906c4693abd454",
					"5950e3a2fdb0a138cb46eb430e78e2833db1cfc0113e139001e862e62b80cfb8",
					"f11722bb84ad06c2a06ec20106ef523ec01dabebd8fad1d85d5636ad0b018fcd",
					"57653b613cae22bc2419579fe516fcca6d1775f2ebf67125155f6f40f30b1682",
					"7d7e98f836817070881559f6d0e80a54d3a7e64e285094cb0ca89956915a1ad2",
					"c05bcfed587b98eea26af945a1e769272f61458a1ff3267416dc3e6e40415fae",
					"c6c19cc7e2c1008f1a6a0871596b96988385b2e8c24dddb25f232514207221bb",
					"40a64d2ae3037a02342fdc88832336d78ef1b1d19270407c55e2052ede526589",
					"2470bf35261f48f407fdba110923bd610546bb571e7c5e739eb60409808b9607",
					"1d3adeebdc8e257819bd9ac1b0046c32c6918cf25475fb732a349bacbac19d01",
					"d28b23a644f38a897d4b56950cad203275df50996248552e4fa470010bc45f15",
					"2f47c1cbac3fa3816dbb9f2354f08586c1e2703b8fb30fbfe834387bc9e89c46",
					"eafbff548f002cc39f4cd1da70e8f08602381baf87a54c866dfbfeb9dee22299",
					"d39123e233068297507cbf9526fa2e228946c466976b45d67b13a04d60b1dd5b",
					"7728885818c0392f0c7520a6c54268af56f20b079d3a55d01d6bd7a99c49fac4",
					"759df58288fd7e78117940e222254404d700747f98ba2b0b7dab7ff2888677ff",
					"8ab1b278ffc70ffd0ac3addd11fc31f196e799a1aba7941e7827cb92fbd75e57",
					"ee65d1ffacfcfc669c20c76419aea696de8dc72d85b386d6e686e493af9fabcf",
					"d8cc1a84822c5f2a7da45312b20e633c94006b5b65c677b06998e654421d3682",
					"a427a17dfe96eb6752836acbf5ef1fa3e880e2e52fb8180a3d70e8a01d8c2e5f",
					"051680fbd8a4d07a0e3e05031452dcf5fbc04f9f9c340b61f1127f19028e470e",
					"32d3ffe77847d58511f7602d943e4e67b41c778212313bfbaf90fc979ebd0ab0",
					"27aabf2b3fbd313e53d9e1b3d751dc51970f1450e3a3dc79971a5e0744e1faa4",
					"ddd20c8884b40c09369177405541c1b81e104e6f60a3fc4dcbfcdc0902822d70",
					"ba2be200dfd613eda36e798e79503bde65810e63a9a9be0ce111136372c9050c",
					"be08c021907d3423e67b43bba3365ef1282dc5da67f5ae32d28c462227fe43ec",
					"0dd1e7934fd17ca0ace56a045a099422a6a2e4a2acab8b365e359c9952dea75c",
					"7079d074d2c9393faca8f67a1f480425b3a0f8fba3c236d5b39e6217fb9beeac",
					"559dd06aef5b7f51f203bfb61fd41e6278aa1fd1f79bfd9b9e7645509f9a51e2",
					"e49afe65e5dac968418bdd5cb540718a727a56aa3b848958ef1f34efcdde7de1",
					"d900f66805f011e9e14941f3c25617ec9d0bdfcf0d8fa8a17eb37e722fc259c5",
					"27d8691018bd0f95a9f899fb09cedaa1e970e59886276c35cd20c2cb5bc13d2c",
					"b99344b05cc7324f7ee8ee08fbf214accce2e48e3441076ce1ea87c846d82309",
					"789fe5cbb07cc276fb8ad75321aa807f42e82ab4d59f1ca8f9d15a5d58fef5d4",
					"24c0b6cbbb3582a4217516f032e7feb021dbbf3f62de9b13630d5e1e3569508d",
					"0a2f98f4c9703cbb35545c6ad0f0469dfb3b61d3ae050df6a75f00f42048fbae",
					"8b43b3d1f39cdc09d9d32801ca43ab854c09b55366325108bc75c18315853d34",
					"a87a469f861dc6b1c444001045858c3c32db68428ec438e9aa92c40256200544",
					"3e716d5a65d2fea35792d52d2e80ff2f3dc8b7bcc1e87965488c4e0ffe98364d",
					"1eb189c3158eb45b70054f5c266a90feceacacc58607245c1c4fd4152420adbb",
					"61f535c4d4e9d5c3dfa8ca27f53daf287af28ba637fcbc197135b3af64b78c1b",
					"c6848b1a846bf4bbdf5d02fa10c1ea3ef15a088677e619d5997c84899a86006c",
					"c505b2927313b767c3df595ba9f4d6567ab473063c88856ec66ab46987b5e25b",
					"8743796bf0788f9eeb7326af2e8e2b22d075dfea84aa363711995962702282af",
					"e837612242b45b6c4a458a37572ccf726bc67f8b5522489e7bf75446e2d97d91",
					"14d3936a1ff061c6470365de1ea6de30dedb83bcd07cad08a7bb82ccf79d88cb",
					"3e516b2b642571a69e23cc26e25f9def8417cc0f08283247e8112deefaf72fa7",
					"7f9424feff06a9dd8ef49d5f7f37cd7c77be86bf9520491d0ff222c26ea160a7",
					"780526142e5735858f3ac41c28601346caae621c4a62cb2a26200458dd3a325a",
					"ec3e2e045557792a70736dc345fd002af32744c512067047c5c3f6270e51d39b",
					"22c5fa07f42e38e57c11867e8ac118e394ea3fbbf55b7bcaec80ff40a3afedec",
					"33b3649c7ef44c7ef528a565ad316fe52a31adebffce6a3138490a8ec3f9e398",
					"a97a0965be1e6581452b61e5e640e797b7d15804916553b1bcc12e780ecf6a26",
					"a2f7ec36af1467d775a9994302d31d7994c064634460d7d6aa728187f092222e",
					"fe445e47b11c22411da480935080d8eac1f3136445a0d65ac93dc10bdb6c70ca",
					"7e02606ab14ab8d12465d57de87d77a89e5757f3a7296188cc30938fc182a2f0",
					"e4526cb78b376fde908115d2880517d2f8c0d4232151902516a42e81186f4659",
					"0d38c7b175f1a98810959fc43ada086871cdb79b96b43283c93c73027fb3bb3a",
					"7b01f0b4958ba5baec2b2990bb35b157f487e71d2f11763377731ba9066ef789",
					"04aca132ce01f1e1e70b6cdd20476305370388cd703a7c7adf9cd82ef5b26a7b",
					"f2550649740edf85c0f30998fefa8fc5f428bc295d84c8476e96ef67db235c0f",
					"3941e00db503283b14c14d44f2000da140e51cf2986f69a757e6c3672aa5c266",
					"0d58a0900e13193b9e93152150bd4acf86597f67559013ea158fc60978be9185",
					"86dd59ed57006a98e072761ef4bf19a451eef5308767235de49b9c495de71d97",
					"3b93d1e0eb767688be54c394c0ca6ac01ed84dcf7e72cfafbba08487ccac8d8f",
					"16d7c60e3173e7066c0470950f55c1d8cafd29af8888670a3e3df2caa71468f5",
					"e84b219ad6901204953f1990bdecee5a8e830acd10806dfe29cc02df6b89d655",
					"b610966afc00f36291787c6581329971540f3b8c8190fcdad74ca40abbe48b71",
					"70819def9262633866419e7865e2a5d5096bbbc50ddb5ee02ae02e44559139f0",
					"fb417c020808407d544a75f5989f46c53a6c9ea001eb84b9fd27e74dbc08c654",
					"a0fbaaad90034209d5b84357381047fa5eee0955fd624c03639a67f5200c000c",
					"6a377550dc25bbc89d97e5770421503a2817ab904de380d5eff8fb99a3786975",
					"66fe23a15b46c57664a8fcd2aed6dd594b8410db975d789632f118d44d2c2a44",
					"929f38312d7926fe13aab15aa3025fedb8ca4aadd87b9b122f23e48944522855",
					"a7dd2bf718ff5c341fe4241763a7c3a410fd885a22061f2f24bf309583e06f9b",
					"499b2f4e00b8208aa476785b0a18b770be7108180bef4a9d7584ad06cb60cc13",
					"f732b889fa95bee3fdcd5f1f4bc99b5365d268e69072fe6c564d2c3d7e72e2d0",
					"d6999af7ac857cd39c3246a75f1e4e731716a59279ff59e89ae220df5777a83c",
					"518f1c98c6e5b6d94f32ead347effbdff53f962c313cdeb62e7c4f6e558013a2",
					"b028cfba11cb36eac3344584123ed2ebfefce98a78c3c17f473a82c5565fda48",
					"5704b3c362962886c8dbb950e21bbec2e158e76c54ee36c4f00378b9e5dac035",
					"410c3c82ee1e77593b94795ee4c3964ec3ff96b44403d9128853916343a8a9b0",
					"b0a2a4a39447f16ce19f96440f54407c797297a66ea864b165659688121c4463",
				}

				utreexoProof := make([]utreexo.Hash, len(utreexoProofStr))
				for i, str := range utreexoProofStr {
					hash, err := hex.DecodeString(str)
					if err != nil {
						panic(err)
					}

					utreexoProof[i] = (*(*[32]byte)(hash))
				}

				return WalletState{
					WatchedKeys: map[string]map[string]bool{
						"tpubDC6ej6KDdtPrbY5mmPAytdLNTW" +
							"2Cdc3m9u6fzFbrVGwruViSNwqMez43" +
							"6B1A4BdsFd8birpbNPMeE4iWTFW3yP" +
							"VTM2a7EHXoKtQwsxG8nEz": {
							"tb1q0cy0tt4spfdlsca80latrfgldsp85pj8rujtt2": false,
							"tb1q2ee677a3fkruuxlmua52qcvary2nhv8mdvjnwt": true,
						},
					},
					LastExternalIndex: map[string]uint32{
						"tpubDC6ej6KDdtPrbY5mmPAytdLNTW" +
							"2Cdc3m9u6fzFbrVGwruViSNwqMez43" +
							"6B1A4BdsFd8birpbNPMeE4iWTFW3yP" +
							"VTM2a7EHXoKtQwsxG8nEz": 20,
					},
					LastInternalIndex: map[string]uint32{
						"tpubDC6ej6KDdtPrbY5mmPAytdLNTW" +
							"2Cdc3m9u6fzFbrVGwruViSNwqMez43" +
							"6B1A4BdsFd8birpbNPMeE4iWTFW3yP" +
							"VTM2a7EHXoKtQwsxG8nEz": 20,
					},
					BestHash:           *bestHash,
					NumLeaves:          3516184,
					UtreexoLeaves:      utreexoLeaves,
					UtreexoProof:       utreexo.Proof{Targets: utreexoTargets, Proof: utreexoProof},
					RelevantUtxos:      make(map[wire.OutPoint]LeafDataExtras),
					RelevantStxos:      make(map[wire.OutPoint]LeafDataExtras),
					RelevantTxs:        make(map[chainhash.Hash]RelevantTxData),
					RelevantMempoolTxs: make(map[chainhash.Hash]MempoolTx),
				}
			}(),
		},
		{
			name: "random #0",
			state: func() WalletState {
				rand := rand.New(rand.NewSource(0))
				value, ok := quick.Value(reflect.TypeOf(WalletState{}), rand)
				if !ok {
					panic("failed to create a random value")
				}

				ws := value.Interface().(WalletState)
				for k, v := range ws.RelevantUtxos {
					v.LeafData.OutPoint = k
					ws.RelevantUtxos[k] = v
				}
				for k, v := range ws.RelevantStxos {
					v.LeafData.OutPoint = k
					ws.RelevantStxos[k] = v
				}
				return ws
			}(),
		},
		{
			name: "random #2",
			state: func() WalletState {
				rand := rand.New(rand.NewSource(2))
				value, ok := quick.Value(reflect.TypeOf(WalletState{}), rand)
				if !ok {
					panic("failed to create a random value")
				}

				ws := value.Interface().(WalletState)
				for k, v := range ws.RelevantUtxos {
					v.LeafData.OutPoint = k
					ws.RelevantUtxos[k] = v
				}
				for k, v := range ws.RelevantStxos {
					v.LeafData.OutPoint = k
					ws.RelevantStxos[k] = v
				}

				for k, v := range ws.RelevantTxs {
					if v.Tx == nil {
						value, ok := quick.Value(reflect.TypeOf(wire.MsgTx{}), rand)
						if !ok {
							panic("failed to create a random value")
						}
						tx := value.Interface().(wire.MsgTx)
						for i, in := range tx.TxIn {
							if in == nil {
								value, ok := quick.Value(reflect.TypeOf(wire.TxIn{}), rand)
								if !ok {
									panic("failed to create a random value")
								}
								in := value.Interface().(wire.TxIn)
								tx.TxIn[i] = &in
							}
						}
						for i, out := range tx.TxOut {
							if out == nil {
								value, ok := quick.Value(reflect.TypeOf(wire.TxOut{}), rand)
								if !ok {
									panic("failed to create a random value")
								}
								out := value.Interface().(wire.TxOut)
								tx.TxOut[i] = &out
							}
						}

						v.Tx = &tx
						ws.RelevantTxs[k] = v
					}
				}

				return ws
			}(),
		},
	}

	for _, test := range tests {
		bytes, err := json.Marshal(test.state)
		if err != nil {
			t.Fatalf("fail on test %s. failed to marshal WalletState: %v", test.name, err)
		}

		// Unmarshal the JSON bytes back into a WalletState struct.
		var gotState WalletState
		if err := json.Unmarshal(bytes, &gotState); err != nil {
			t.Fatalf("fail on test %s. failed to unmarshal JSON into WalletState: %v", test.name, err)
		}

		// Ensure the unmarshaled WalletState struct is the same as the original.
		if !reflect.DeepEqual(test.state, gotState) {
			t.Fatalf("unmarshaled WalletState does not match the original")
		}
	}
}
