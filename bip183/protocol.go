// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip183

const (
	// SFNodeUtreexo is a flag used to indicate a peer supports serving
	// utreexo inclusion proofs for new blocks, transactions as defined
	// in BIP-0183.
	SFNodeUtreexo = 1 << 12

	// SFNodeUtreexoArchive is a flag used to indicate a peer supports
	// serving historical inclusion proofs for past blocks as defined in
	// BIP-0183.
	SFNodeUtreexoArchive = 1 << 13
)
