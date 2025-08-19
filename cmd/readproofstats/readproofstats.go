// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/utreexo/utreexod/blockchain/indexers"
)

// formatBytesToGB formats the bytes into a human readable GB format.
func formatBytesToGB(bytes uint64) string {
	const gb = 1024 * 1024 * 1024 // 1 GB in bytes
	return fmt.Sprintf("%.2f GB", float64(bytes)/float64(gb))
}

var usage string = "Usage: readproofstats <file_path>. With the --flatutreexoproofindex enabled, " +
	"the utreexoproofstats.dat file can be found under DATA_DIR/mainnet/utreexoproofstats_flat/. " +
	"For other networks, replace mainnet with the desired network (signet, testnet)."

func main() {
	if len(os.Args) < 2 {
		log.Fatal(usage)
	}

	filePath := os.Args[1]

	file, err := os.Open(filePath)
	if err != nil {
		log.Fatalf("Failed to open file: %v\n%v", err, usage)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read file: %v\n%v", err, usage)
	}

	if len(content) != indexers.ProofStatsSize {
		log.Fatalf("Expected the file to be of size %v but got %v\n%v",
			indexers.ProofStatsSize, len(content), usage)
	}

	r := bytes.NewBuffer(content)

	ps := indexers.ProofStats{}
	err = ps.Deserialize(r)
	if err != nil {
		log.Fatalf("Failed to deserialize stats: %v\n%v", err, usage)
	}

	fmt.Printf("height %d: total dels %d, leafdata size %d (%s), leaf data count %d, target size %d (%s), target count %d, proof size %d (%s), proof count %d\n",
		ps.BlockHeight, ps.TotalDels,
		ps.LdSize, formatBytesToGB(ps.LdSize), ps.LdCount,
		ps.TgSize, formatBytesToGB(ps.TgSize), ps.TgCount,
		ps.ProofSize, formatBytesToGB(ps.ProofSize), ps.ProofCount)

	fmt.Printf("height %d: average-blockoverhead %f, blockoverhead-sum %f, blockcount %d\n",
		ps.BlockHeight, ps.BlockProofOverheadSum/float64(ps.BlockProofCount),
		ps.BlockProofOverheadSum, ps.BlockProofCount)
}
