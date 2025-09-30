package wire

import (
	"bytes"
	"testing"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

func TestMsgGetUtreexoSummariesEncode(t *testing.T) {
	testCases := []struct {
		hash        chainhash.Hash
		maxExponent uint8
		shouldErr   bool
	}{
		{
			hash:        genesisHash,
			maxExponent: 1,
			shouldErr:   false,
		},
		{
			hash:        genesisHash,
			maxExponent: 1,
			shouldErr:   false,
		},
		{
			hash:        genesisHash,
			maxExponent: 0,
			shouldErr:   false,
		},
		{
			hash:        genesisHash,
			maxExponent: 0,
			shouldErr:   false,
		},
		{
			hash:        genesisHash,
			maxExponent: 7,
			shouldErr:   false,
		},
		{
			hash:        genesisHash,
			maxExponent: 7,
			shouldErr:   false,
		},
		{
			hash:        genesisHash,
			maxExponent: 8,
			shouldErr:   true,
		},
		{
			hash:        genesisHash,
			maxExponent: 255,
			shouldErr:   true,
		},
	}

	for _, testCase := range testCases {
		beforeMsg := NewMsgGetUtreexoSummaries(testCase.hash, testCase.maxExponent)

		// Encode.
		var buf bytes.Buffer
		err := beforeMsg.BtcEncode(&buf, 0, LatestEncoding)
		if err != nil {
			if !testCase.shouldErr {
				t.Fatal(err)
			}
			continue
		} else {
			if testCase.shouldErr {
				t.Fatal("expected to error but didn't")
			}
		}

		serialized := buf.Bytes()

		afterMsg := MsgGetUtreexoSummaries{}
		r := bytes.NewReader(serialized)
		err = afterMsg.BtcDecode(r, 0, LatestEncoding)
		if err != nil {
			t.Fatal(err)
		}

		if !afterMsg.StartHash.IsEqual(&testCase.hash) {
			t.Fatalf("expected %v but got %v",
				testCase.hash, afterMsg.StartHash)
		}

		if afterMsg.MaxReceiveExponent != testCase.maxExponent {
			t.Fatalf("expected %v but got %v",
				testCase.maxExponent, afterMsg.MaxReceiveExponent)
		}
	}
}
