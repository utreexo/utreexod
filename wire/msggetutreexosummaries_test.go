package wire

import (
	"bytes"
	"testing"
)

func TestMsgGetUtreexoHeaderEncode(t *testing.T) {
	beforeMsg := NewMsgGetUtreexoSummaries(5)

	// Encode.
	var buf bytes.Buffer
	err := beforeMsg.BtcEncode(&buf, 0, LatestEncoding)
	if err != nil {
		t.Fatal(err)
	}

	serialized := buf.Bytes()

	afterMsg := MsgGetUtreexoSummaries{}
	r := bytes.NewReader(serialized)
	err = afterMsg.BtcDecode(r, 0, LatestEncoding)
	if err != nil {
		t.Fatal(err)
	}

	if beforeMsg.BlockPosition != afterMsg.BlockPosition {
		t.Fatalf("expected %v but got %v",
			beforeMsg.BlockPosition, afterMsg.BlockPosition)
	}
}
