package peer

import (
	"testing"
	"time"

	"github.com/btcsuite/btcd/wire/v2"
	"github.com/utreexo/utreexod/bip183"
)

// TestStallHandlerUtreexoProof verifies that the stall handler disconnects a
// peer that doesn't answer a getdata, and keeps a peer that sends the utreexo
// proofs a utreexo node asks for ahead of the blocks it requests.
func TestStallHandlerUtreexoProof(t *testing.T) {
	tests := []struct {
		name           string
		replies        []wire.Message
		wantDisconnect bool
	}{
		{
			name:           "no reply",
			wantDisconnect: true,
		},
		{
			name:    "utreexo proof",
			replies: []wire.Message{&bip183.MsgUtreexoProof{}},
		},
	}

	for _, test := range tests {
		p := newPeerBase(&Config{}, false)
		p.stallTickInterval = 10 * time.Millisecond
		p.stallResponseTimeout = 50 * time.Millisecond
		go p.stallHandler()

		p.stallControl <- stallControlMsg{sccSendMessage, &wire.MsgGetData{}}
		for _, reply := range test.replies {
			p.stallControl <- stallControlMsg{sccReceiveMessage, reply}
		}

		var disconnected bool
		select {
		case <-p.quit:
			disconnected = true
		case <-time.After(10 * p.stallResponseTimeout):
		}

		// The stall handler returns once the input and output handlers
		// are done.
		close(p.inQuit)
		close(p.outQuit)

		if disconnected != test.wantDisconnect {
			t.Errorf("%s: disconnected %v, want %v", test.name,
				disconnected, test.wantDisconnect)
		}
	}
}
