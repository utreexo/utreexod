// Copyright (c) 2021-2022 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"reflect"
	"testing"

	"github.com/mit-dci/utreexo/accumulator"
)

func TestChainTipProofSerialize(t *testing.T) {
	tests := []struct {
		name    string
		ctp     ChainTipProof
		encoded string
	}{
		{
			name: "Signet tx 319e630... vout 0",
			ctp: ChainTipProof{
				ProvedAtHash: newHashFromStr("000000ba120e3102661f7cf6f926d4ba086d2a5ccf8df2e2489ca4cbf135c64f"),
				AccProof: &accumulator.BatchProof{
					Targets: []uint64{335559},
					Proof: []accumulator.Hash{
						accumulator.Hash(*newHashFromStr("320d0fa8a5badd7476e2a13fd19c5406edc6c04e067683c655946a4de571c987")),
						accumulator.Hash(*newHashFromStr("b4c1444961b80590151694974264fd5e9cd7cc954e0d89d0bf78364b28c0bec9")),
						accumulator.Hash(*newHashFromStr("50cf4de7bfee64fb7719829dae0dfe830ccad3d12882fb1e2a061b446d0c0910")),
						accumulator.Hash(*newHashFromStr("b94eb65e48c45e437c8e9fa272a35b243977659538aafea0c0b4978781c80f5e")),
						accumulator.Hash(*newHashFromStr("c2bd2c4d1e281431692311153cf31dadd895f58974b88a0f2c2cc9410a8d2575")),
						accumulator.Hash(*newHashFromStr("4dfedc13f324f1d2dd49d4e6eabb94eefca966fa5c2775b460650f6e4324e513")),
						accumulator.Hash(*newHashFromStr("f6b4175069ce3fc96fc30074c0e0cd01bcb71f9637824ac57c0650509f9f4ce5")),
						accumulator.Hash(*newHashFromStr("9373be4da5a8e5d52b96f5a9fcfc39e59e462ebdaf90db33e54fc426745b41e5")),
						accumulator.Hash(*newHashFromStr("9a42fe25467e562f7c10a30ee5128dbcecf8c290c36952435b3c93856e0942f8")),
						accumulator.Hash(*newHashFromStr("816a168f4037bc294dc77fcabe5b6efbed6ca7d693aae29377042fa85ec907d7")),
						accumulator.Hash(*newHashFromStr("4c954d15574ca705c42fc856a45559f50b13caa91c68296c4300fd555bb08825")),
						accumulator.Hash(*newHashFromStr("13158bc309e5acd2d71ad0a59c5c350f2cf799a6092947a2bea52de7a11dc568")),
						accumulator.Hash(*newHashFromStr("fcc74ac50d0923a0e1c83d37f782719c7ba839be6a547efab2bef6f972c7ebb4")),
						accumulator.Hash(*newHashFromStr("3b5c7f94e0ed5bbe280d449c58a5a734e67e0d24348d555daa4e38ed0589ea1b")),
						accumulator.Hash(*newHashFromStr("5d7f1af3400b88c7d0b833cf7f7e38cb9a6d05bc0928ff3b992bef45cebbcccb")),
					},
				},
				HashesProven: []accumulator.Hash{accumulator.Hash(*newHashFromStr("d48e679a5d15410ebdeeb0ec6b67701b6861812edfd2a6343a83f009e760ab4f"))},
			},
			encoded: "4fc635f1cba49c48e2f28dcf5c2a6d08bad426f9f67c1f6602310e12ba00000001c71e05000" +
				"00000000f87c971e54d6a9455c68376064ec0c6ed06549cd13fa1e27674ddbaa5a80f0d32c9b" +
				"ec0284b3678bfd0890d4e95ccd79c5efd6442979416159005b8614944c1b410090c6d441b062" +
				"a1efb8228d1d3ca0c83fe0dae9d821977fb64eebfe74dcf505e0fc8818797b4c0a0feaa38956" +
				"57739245ba372a29f8e7c435ec4485eb64eb975258d0a41c92c2c0f8ab87489f595d8ad1df33" +
				"c151123693114281e4d2cbdc213e524436e0f6560b475275cfa66a9fcee94bbeae6d449ddd2f" +
				"124f313dcfe4de54c9f9f5050067cc54a8237961fb7bc01cde0c07400c36fc93fce695017b4f" +
				"6e5415b7426c44fe533db90afbd2e469ee539fcfca9f5962bd5e5a8a54dbe7393f842096e859" +
				"33c5b435269c390c2f8ecbc8d12e50ea3107c2f567e4625fe429ad707c95ea82f047793e2aa9" +
				"3d6a76cedfb6e5bbeca7fc74d29bc37408f166a812588b05b55fd00436c29681ca9ca130bf55" +
				"955a456c82fc405a74c57154d954c68c51da1e72da5bea2472909a699f72c0f355c9ca5d01ad" +
				"7d2ace509c38b1513b4ebc772f9f6beb2fa7e546abe39a87b9c7182f7373dc8e1a023090dc54" +
				"ac7fc1bea8905ed384eaa5d558d34240d7ee634a7a5589c440d28be5bede0947f5c3bcbccbbc" +
				"e45ef2b993bff2809bc056d9acb387e7fcf33b8d0c7880b40f31a7f5d010000004fab60e709f" +
				"0833a34a6d2df2e8161681b70676becb0eebd0e41155d9a678ed4",
		},
	}

	for _, test := range tests {
		// Test String().
		gotString := test.ctp.String()
		if gotString != test.encoded {
			t.Errorf("%s: Encoded string mismatch. Expected %s but got %s",
				test.name, test.encoded, gotString)
			continue
		}

		// Test DecodeString().
		gotCtp := ChainTipProof{}
		err := gotCtp.DecodeString(test.encoded)
		if err != nil {
			t.Errorf("%s: Error %v", test.name, err)
			continue
		}

		if !reflect.DeepEqual(gotCtp, test.ctp) {
			t.Errorf("%s: Decoded chain tip proof mismatch.", test.name)
			continue
		}

	}
}
