// Copyright (c) 2021-2022 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"reflect"
	"testing"

	"github.com/utreexo/utreexo"
)

func TestChainTipProofSerialize(t *testing.T) {
	tests := []struct {
		name    string
		ctp     ChainTipProof
		encoded string
	}{
		{
			name: "Signet tx 5939fa8344355201cf0e2c3a6327a3fa530860a50ded2b0edc4a409589e0e986 vout 0",
			ctp: ChainTipProof{
				ProvedAtHash: newHashFromStr("000000eee99f783553176a208f16460901d9f12fe9163bc1421737c290ca2dc4"),
				AccProof: &utreexo.Proof{
					Targets: []uint64{3378993},
					Proof: []utreexo.Hash{
						utreexo.Hash(*newHashFromStr("145c67c2461101ce37b8c86516f17fb71ad0f42a7a60b7b5164605537faa86c9")),
						utreexo.Hash(*newHashFromStr("849539e61119384f64d9994320cdc697ceb78ef2685a0075a513100b4006b7ab")),
						utreexo.Hash(*newHashFromStr("9284130744b0f52b5b4ba092f64a6241ab4c8e949542ef206c8f85f02368aeb7")),
					},
				},
				HashesProven: []utreexo.Hash{utreexo.Hash(*newHashFromStr("b1544ba433103c2bca6a489d9d878b398d4447dfad3c40c6ac085105bd9eddef"))},
			},
			encoded: "c42dca90c2371742c13b16e92ff1d9010946168f206a1" +
				"75335789fe9ee00000001fe318f330003c986aa7f53054" +
				"616b5b7607a2af4d01ab77ff11665c8b837ce011146c26" +
				"75c14abb706400b1013a575005a68f28eb7ce97c6cd204" +
				"399d9644f381911e6399584b7ae6823f0858f6c20ef429" +
				"5948e4cab41624af692a04b5b2bf5b0440713849201000" +
				"000efdd9ebd055108acc6403caddf47448d398b879d9d4" +
				"86aca2b3c1033a44b54b1",
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
