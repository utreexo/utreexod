// Copyright (c) 2021-2022 The utreexo developer
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package indexers

import (
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"
	"time"
)

func TestProofStatSerialize(t *testing.T) {
	tmpDir := t.TempDir()
	fileName := filepath.Join(tmpDir, "TestProofStatSerializeFile")

	f, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		t.Fatal(err)
	}
	ff := FlatFileState{dataFile: f}

	// Get random seed.
	time := time.Now().UnixNano()
	seed := rand.NewSource(time)
	rand := rand.New(seed)

	proofStatsVal, ok := quick.Value(reflect.TypeOf(proofStats{}), rand)
	if !ok {
		t.Fatalf("Couldn't create random proofStats")
	}

	pStats := proofStatsVal.Interface().(proofStats)

	err = pStats.WritePStats(&ff)
	if err != nil {
		t.Fatal(err)
	}

	newPStats := proofStats{}
	err = newPStats.InitPStats(&ff)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(pStats, newPStats) {
		t.Fatalf("Proof stats do not equal after re-initialization")
	}
}
