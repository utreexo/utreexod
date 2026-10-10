// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bip183

import "github.com/utreexo/utreexod/wire"

const (
	// InvUtreexoFlag denotes that the inventory vector type is requesting,
	// or sending a version which includes the utreexo accumulator data.
	InvUtreexoFlag = 1 << 24
)

// These constants define the BIP-183 inventory vector types.
const (
	InvTypeUtreexoProofHash wire.InvType = 6
	InvTypeUtreexoTx        wire.InvType = wire.InvTypeTx | InvUtreexoFlag
	InvTypeWitnessUtreexoTx wire.InvType = wire.InvTypeTx | wire.InvWitnessFlag | InvUtreexoFlag
)
