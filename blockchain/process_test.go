// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blockchain

import (
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/utreexo/utreexod/chaincfg"
	"github.com/utreexo/utreexod/chaincfg/chainhash"
	"github.com/utreexo/utreexod/wire"
)

func chainedHeaders(parent *wire.BlockHeader, chainParams *chaincfg.Params,
	parentHeight int32, numHeaders int) []*wire.BlockHeader {

	headers := make([]*wire.BlockHeader, 0, numHeaders)
	tip := parent

	blockHeight := parentHeight
	for i := 0; i < numHeaders; i++ {
		// Use a timestamp that is one second after the previous block unless
		// this is the first block in which case the current time is used.
		var ts time.Time
		if blockHeight == 1 {
			ts = time.Unix(time.Now().Unix(), 0)
		} else {
			ts = tip.Timestamp.Add(time.Second)
		}

		var randBytes [4]byte
		rand.Read(randBytes[:])
		merkle := chainhash.HashH(randBytes[:])

		header := wire.BlockHeader{
			Version:    1,
			PrevBlock:  tip.BlockHash(),
			MerkleRoot: merkle,
			Bits:       chainParams.PowLimitBits,
			Timestamp:  ts,
			Nonce:      0, // To be solved.
		}
		if !SolveBlock(&header) {
			panic(fmt.Sprintf("Unable to solve block at height %d", blockHeight))
		}
		headers = append(headers, &header)
		tip = &header
	}

	return headers
}

func TestProcessBlockHeader(t *testing.T) {
	chain, params, tearDown := utxoCacheTestChain("TestProcessBlockHeader")
	defer tearDown()

	headers := chainedHeaders(&params.GenesisBlock.Header, params, 0, 10)
	for _, header := range headers {
		err := chain.ProcessBlockHeader(header, BFNone)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Create sidechain block headers.
	blockHash := headers[4].BlockHash()
	node := chain.IndexLookupNode(&blockHash)
	sideChainHeaders := chainedHeaders(headers[4], params, node.height, 3)

	// Test that the last block header fails as it's missing the previous block
	// header.
	err := chain.ProcessBlockHeader(sideChainHeaders[len(sideChainHeaders)-1], BFNone)
	if err == nil {
		err := fmt.Errorf("sideChainHeader %v passed verification but "+
			"should've failed verification"+
			"as the previous header is not known",
			sideChainHeaders[len(sideChainHeaders)-1].BlockHash().String(),
		)
		t.Fatal(err)
	}

	// Verify that the side-chain headers verify.
	for _, header := range sideChainHeaders {
		err := chain.ProcessBlockHeader(header, BFNone)
		if err != nil {
			t.Fatal(err)
		}
	}
}
