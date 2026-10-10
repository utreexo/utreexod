// Copyright (c) 2026 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package btcdwire holds wire code that bip182 and bip183 need but btcd's wire
// package doesn't export.
//
// NOTHING HERE IS UTREEXO CODE. message.go is copied from btcd's wire, and
// common.go stands in for wire's unexported serialization helpers. Don't add
// utreexo logic here, and keep message.go in sync with btcd's wire.
package btcdwire
