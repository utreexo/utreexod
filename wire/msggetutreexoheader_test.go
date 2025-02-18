package wire

import (
	"bytes"
	"testing"

	"github.com/utreexo/utreexod/chaincfg/chainhash"
)

func TestMsgGetUtreexoHeaderEncode(t *testing.T) {
	testCases := []struct {
		hash         chainhash.Hash
		includeProof bool
	}{
		{
			hash:         genesisHash,
			includeProof: false,
		},
		{
			hash:         genesisHash,
			includeProof: true,
		},
	}

	for _, testCase := range testCases {
		beforeMsg := NewMsgGetUtreexoHeader(testCase.hash, testCase.includeProof)

		// Encode.
		var buf bytes.Buffer
		err := beforeMsg.BtcEncode(&buf, 0, LatestEncoding)
		if err != nil {
			t.Fatal(err)
		}

		serialized := buf.Bytes()

		afterMsg := MsgGetUtreexoHeader{}
		r := bytes.NewReader(serialized)
		err = afterMsg.BtcDecode(r, 0, LatestEncoding)
		if err != nil {
			t.Fatal(err)
		}

		if !afterMsg.BlockHash.IsEqual(&testCase.hash) {
			t.Fatalf("expected %v but got %v",
				testCase.hash, afterMsg.BlockHash)
		}

		if afterMsg.IncludeProof != testCase.includeProof {
			t.Fatalf("expected %v but got %v",
				testCase.includeProof, afterMsg.IncludeProof)
		}
	}
}
