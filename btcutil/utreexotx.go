// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcutil

import (
	"bytes"
	"io"

	"github.com/utreexo/utreexod/bip183"
)

// UtreexoTx defines a utreexo bitcoin transaction that provides easier and more
// efficient manipulation of raw transactions. It also memoizes the hash for the
// transaction on its first access so subsequent accesses don't have to repeat
// the relatively expensive hashing operations.
type UtreexoTx struct {
	Tx
	msgUtreexoTx *bip183.MsgUtreexoTx
}

// MsgUtreexoTx returns the underlying bip183.MsgUtreexoTx for the utreexo transaction.
func (t *UtreexoTx) MsgUtreexoTx() *bip183.MsgUtreexoTx {
	return t.msgUtreexoTx
}

// NewUtreexoTx returns a new instance of a bitcoin transaction given an underlying
// bip183.MsgUtreexoTx. See UtreexoTx.
func NewUtreexoTx(msgUtreexoTx *bip183.MsgUtreexoTx) *UtreexoTx {
	return &UtreexoTx{
		Tx:           *NewTx(&msgUtreexoTx.MsgTx),
		msgUtreexoTx: msgUtreexoTx,
	}
}

// NewUtreexoTxFromBytes returns a new instance of a bitcoin utreexo transaction given the
// serialized bytes. See UtreexoTx.
func NewUtreexoTxFromBytes(serializedUtreexoTx []byte) (*UtreexoTx, error) {
	br := bytes.NewReader(serializedUtreexoTx)
	return NewUtreexoTxFromReader(br)
}

// NewUtreexoTxFromReader returns a new instance of a bitcoin transaction given a
// Reader to deserialize the transaction. See UtreexoTx.
func NewUtreexoTxFromReader(r io.Reader) (*UtreexoTx, error) {
	// Deserialize the bytes into a MsgUtreexoTx.
	var msgUtreexoTx bip183.MsgUtreexoTx
	err := msgUtreexoTx.Deserialize(r)
	if err != nil {
		return nil, err
	}

	tx := NewTx(&msgUtreexoTx.MsgTx)

	t := UtreexoTx{
		Tx:           *tx,
		msgUtreexoTx: &msgUtreexoTx,
	}
	return &t, nil
}
