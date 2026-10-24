// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package utreexoblock provides a bitcoin block that carries its utreexo data:
// the utreexo proof and leaf datas that verify the block, the ttls of the
// leaves it creates, and the utreexo update data from connecting it.
package utreexoblock

import (
	"bytes"

	"github.com/utreexo/utreexo"
	"github.com/utreexo/utreexod/bip182"
	"github.com/utreexo/utreexod/bip183"
	"github.com/utreexo/utreexod/btcutil"
)

// Block is a bitcoin block along with its utreexo data.
type Block struct {
	*btcutil.Block

	updateData *utreexo.UpdateData // Utreexo update data for this block.
	adds       []utreexo.Hash      // Hashes of the utreexo leaves being added.
	leafTTLs   *bip183.UtreexoTTL  // The ttls of the leaves created in this block.
	leafDatas  []bip182.LeafData   // The leaves of the inputs in this block.
	proofData  *utreexo.Proof      // Utreexo proof for this block.
}

// New returns a Block for the given block with no utreexo data.
func New(block *btcutil.Block) *Block {
	return &Block{Block: block}
}

// SetUtreexoData stores the serialized Utreexo proof data for this block.
func (b *Block) SetUtreexoData(data *bip183.UData) {
	if data == nil {
		b.proofData = nil
		b.leafDatas = nil
		return
	}

	b.proofData = &data.AccProof
	b.leafDatas = data.LeafDatas
}

// UtreexoData returns the serialized Utreexo proof data for the block.
func (b *Block) UtreexoData() *bip183.UData {
	if b.proofData == nil {
		return nil
	}

	return &bip183.UData{
		AccProof:  *b.proofData,
		LeafDatas: b.leafDatas,
	}
}

// SetUtreexoUpdateData sets the utreexo update data of the block in the block chain.
func (b *Block) SetUtreexoUpdateData(data *utreexo.UpdateData) {
	b.updateData = data
}

// UtreexoUpdateData returns the utreexo update data for the block in the block chain.
func (b *Block) UtreexoUpdateData() *utreexo.UpdateData {
	return b.updateData
}

// SetUtreexoAdds sets the hashes of the utreexo leaves being added in this block.
func (b *Block) SetUtreexoAdds(adds []utreexo.Leaf) {
	addHashes := make([]utreexo.Hash, 0, len(adds))
	for _, add := range adds {
		addHashes = append(addHashes, add.Hash)
	}
	b.adds = addHashes
}

// UtreexoAdds returns the hashes of the utreexo leaves added in this block.
func (b *Block) UtreexoAdds() []utreexo.Hash {
	return b.adds
}

// SetUtreexoTTLs sets the ttls for this block.
func (b *Block) SetUtreexoTTLs(ttls *bip183.UtreexoTTL) {
	b.leafTTLs = ttls
}

// UtreexoTTLs returns the ttls for this block.
func (b *Block) UtreexoTTLs() *bip183.UtreexoTTL {
	return b.leafTTLs
}

// ParseUtreexoData attempts to read serialized utreexo data appended after the
// base block in the cached serialized bytes.  This must be called explicitly by
// utreexo nodes that store proof data alongside the block; non-utreexo nodes
// should never call this.
//
// If the trailing bytes cannot be deserialized or are not fully consumed (e.g.
// an older incompatible format), they are silently ignored and the block is
// left without utreexo data.
func (b *Block) ParseUtreexoData() {
	serialized, err := b.Bytes()
	if err != nil {
		return
	}

	baseLen := b.MsgBlock().SerializeSize()
	if baseLen >= len(serialized) {
		return
	}

	extra := serialized[baseLen:]
	r := bytes.NewReader(extra)

	ud := new(bip183.UData)
	if err := ud.Deserialize(r); err != nil {
		return
	}

	// If there are unconsumed bytes the trailing data is not in the
	// expected format (e.g. an older serialization that includes
	// remember indexes or non-compact leaf data).  Discard the result.
	if r.Len() != 0 {
		return
	}

	b.SetUtreexoData(ud)
}
