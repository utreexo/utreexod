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

	// Generate and process the intial 10 block headers.
	//
	// 	genesis -> 1  -> 2  -> ...  -> 10 (active)
	headers := chainedHeaders(&params.GenesisBlock.Header, params, 0, 10)
	for _, header := range headers {
		isMainChain, err := chain.ProcessBlockHeader(header, BFNone)
		if err != nil {
			t.Fatal(err)
		}

		if !isMainChain {
			t.Fatalf("expected header %v to be in the main chain",
				header.BlockHash())
		}
	}

	// Check that the tip is correct.
	lastHeader := headers[len(headers)-1]
	lastHeaderHash := lastHeader.BlockHash()
	tipNode := chain.bestHeader.Tip()
	if !tipNode.hash.IsEqual(&lastHeaderHash) {
		t.Fatalf("expected %v but got %v", lastHeaderHash.String(), tipNode.hash.String())
	}
	if tipNode.status != statusNone {
		t.Fatalf("expected statusNone but got %v", tipNode.status)
	}
	if tipNode.height != int32(len(headers)) {
		t.Fatalf("expected height of %v but got %v", len(headers), tipNode.height)
	}

	// Create sidechain block headers.
	//
	// 	genesis -> 1  -> 2  -> 3  -> 4  -> 5  -> ... -> 10 (active)
	//                                            \-> 6  -> ... -> 8 (valid-fork)
	blockHash := headers[4].BlockHash()
	node := chain.IndexLookupNode(&blockHash)
	forkHeight := node.height
	sideChainHeaders := chainedHeaders(headers[4], params, node.height, 3)
	sidechainTip := sideChainHeaders[len(sideChainHeaders)-1]

	// Test that the last block header fails as it's missing the previous block
	// header.
	_, err := chain.ProcessBlockHeader(sidechainTip, BFNone)
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
		isMainChain, err := chain.ProcessBlockHeader(header, BFNone)
		if err != nil {
			t.Fatal(err)
		}
		if isMainChain {
			t.Fatalf("expected header %v to not be in the main chain",
				header.BlockHash())
		}
	}

	// Check that the tip is still the same as before.
	if !tipNode.hash.IsEqual(&lastHeaderHash) {
		t.Fatalf("expected %v but got %v", lastHeaderHash.String(), tipNode.hash.String())
	}
	if tipNode.status != statusNone {
		t.Fatalf("expected statusNone but got %v", tipNode.status)
	}
	if tipNode.height != int32(len(headers)) {
		t.Fatalf("expected height of %v but got %v", len(headers), tipNode.height)
	}

	// Verify that the side-chain extending headers verify.
	sidechainExtendingHeaders := chainedHeaders(sidechainTip, params, forkHeight+int32(len(sideChainHeaders)), 10)
	for _, header := range sidechainExtendingHeaders {
		isMainChain, err := chain.ProcessBlockHeader(header, BFNone)
		if err != nil {
			t.Fatal(err)
		}
		blockHash := header.BlockHash()
		node := chain.IndexLookupNode(&blockHash)
		if node.height <= 10 {
			if isMainChain {
				t.Fatalf("expected header %v to not be in the main chain",
					header.BlockHash())
			}
		} else {
			if !isMainChain {
				t.Fatalf("expected header %v to be in the main chain",
					header.BlockHash())
			}
		}
	}

	// Create more sidechain block headers so that it becomes the active chain.
	//
	// 	genesis -> 1  -> 2  -> 3  -> 4  -> 5  -> ... -> 10 (valid-fork)
	//                                            \-> 6  -> ... -> 18 (active)
	lastSidechainHeader := sidechainExtendingHeaders[len(sidechainExtendingHeaders)-1]
	lastSidechainHeaderHash := lastSidechainHeader.BlockHash()

	// Check that the tip is now different.
	tipNode = chain.bestHeader.Tip()
	if !tipNode.hash.IsEqual(&lastSidechainHeaderHash) {
		t.Fatalf("expected %v but got %v", lastSidechainHeaderHash.String(), tipNode.hash.String())
	}
	if tipNode.status != statusNone {
		t.Fatalf("expected statusNone but got %v", tipNode.status)
	}
	if tipNode.height != int32(len(sideChainHeaders)+len(sidechainExtendingHeaders))+forkHeight {
		t.Fatalf("expected height of %v but got %v", len(headers), tipNode.height)
	}

	// Extend the original headers and check it still verifies.
	extendedOrigHeaders := chainedHeaders(lastHeader, params, int32(len(headers)), 2)
	for _, header := range extendedOrigHeaders {
		isMainChain, err := chain.ProcessBlockHeader(header, BFNone)
		if err != nil {
			t.Fatal(err)
		}
		if isMainChain {
			t.Fatalf("expected header %v to not be in the main chain",
				header.BlockHash())
		}
	}

	// Check that the tip didn't change.
	if !tipNode.hash.IsEqual(&lastSidechainHeaderHash) {
		t.Fatalf("expected %v but got %v", lastSidechainHeaderHash.String(), tipNode.hash.String())
	}
}
