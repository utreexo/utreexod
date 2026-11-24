// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip183

import (
	"bytes"
	"testing"

	"github.com/btcsuite/btcd/wire/v2"
	"github.com/stretchr/testify/require"
	"github.com/utreexo/utreexod/bip182"
)

// TestMsgUtreexoTxEncodeTwice ensures BtcEncode leaves the receiver's input
// indexes untouched so the message encodes to the same bytes every time.
func TestMsgUtreexoTxEncodeTwice(t *testing.T) {
	confirmed := mainNetBlock104773.leavesPerBlock[0]

	unconfirmed := bip182.NewLeafData()
	unconfirmed.SetUnconfirmed()
	unconfirmedOutPoint := wire.OutPoint{
		Hash:  *newHashFromStr("58102e32e848fbd68c29480de00d653a88a6de077c46d8f6c37488290f2b4d43"),
		Index: 3,
	}

	msg := NewMsgUtreexoTx(1)
	msg.AddTxIn(wire.NewTxIn(&confirmed.OutPoint, nil, nil))
	msg.AddTxIn(wire.NewTxIn(&unconfirmedOutPoint, nil, nil))
	msg.LeafDatas = []bip182.LeafData{confirmed, unconfirmed}

	var first, second bytes.Buffer
	err := msg.BtcEncode(&first, 0, wire.WitnessEncoding)
	require.NoError(t, err)
	err = msg.BtcEncode(&second, 0, wire.WitnessEncoding)
	require.NoError(t, err)
	require.Equal(t, first.Bytes(), second.Bytes())

	require.Equal(t, uint32(0), msg.TxIn[0].PreviousOutPoint.Index)
	require.Equal(t, uint32(3), msg.TxIn[1].PreviousOutPoint.Index)

	// The decoded message has the original indexes and the unconfirmed flag.
	var decoded MsgUtreexoTx
	err = decoded.BtcDecode(&first, 0, wire.WitnessEncoding)
	require.NoError(t, err)
	require.Equal(t, uint32(0), decoded.TxIn[0].PreviousOutPoint.Index)
	require.Equal(t, uint32(3), decoded.TxIn[1].PreviousOutPoint.Index)
	require.False(t, decoded.LeafDatas[0].IsUnconfirmed())
	require.True(t, decoded.LeafDatas[1].IsUnconfirmed())
}

// TestMsgUtreexoTxSharedTxUnchanged ensures BtcEncode doesn't mutate the
// transaction the message was built from. pushUtreexoTxMsg builds the
// message with MsgTx copied by value from a mempool transaction, which
// shares the TxIn entries, so an in-place mutation of the input indexes
// corrupts the mempool transaction.
func TestMsgUtreexoTxSharedTxUnchanged(t *testing.T) {
	confirmed := mainNetBlock104773.leavesPerBlock[0]

	mempoolTx := wire.NewMsgTx(1)
	outPoint := wire.OutPoint{Hash: confirmed.OutPoint.Hash, Index: 5}
	mempoolTx.AddTxIn(wire.NewTxIn(&outPoint, []byte{1, 2, 3}, nil))
	mempoolTx.AddTxOut(wire.NewTxOut(1000, []byte{4, 5, 6}))
	txidBefore := mempoolTx.TxHash()

	// Build the message like pushUtreexoTxMsg does.
	msg := &MsgUtreexoTx{
		MsgTx:     *mempoolTx,
		LeafDatas: []bip182.LeafData{confirmed},
	}

	var first, second bytes.Buffer
	err := msg.BtcEncode(&first, 0, wire.WitnessEncoding)
	require.NoError(t, err)
	err = msg.BtcEncode(&second, 0, wire.WitnessEncoding)
	require.NoError(t, err)

	require.Equal(t, first.Bytes(), second.Bytes())
	require.Equal(t, uint32(5), mempoolTx.TxIn[0].PreviousOutPoint.Index)
	require.Equal(t, txidBefore, mempoolTx.TxHash())
}
