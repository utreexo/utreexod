// Copyright (c) 2017 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package rpctest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	// compileMtx guards access to the executable path so that the project is
	// only compiled once.
	compileMtx sync.Mutex

	// executablePath is the path to the compiled executable. This is the empty
	// string until btcd is compiled. This should not be accessed directly;
	// instead use the function btcdExecutablePath().
	executablePath string
)

// utreexodExecutablePath returns a path to the utreexod executable to be used by
// rpctests. To ensure the code tests against the most up-to-date version of
// utreexod, this method compiles utreexod the first time it is called. After that, the
// generated binary is used for subsequent test harnesses. The executable file
// is not cleaned up, but since it lives at a static path in a temp directory,
// it is not a big deal.
func utreexodExecutablePath() (string, error) {
	compileMtx.Lock()
	defer compileMtx.Unlock()

	// If btcd has already been compiled, just use that.
	if len(executablePath) != 0 {
		return executablePath, nil
	}

	testDir, err := baseDir()
	if err != nil {
		return "", err
	}

	// Build btcd and output an executable in a static temp path.
	outputPath := filepath.Join(testDir, "utreexod")
	if runtime.GOOS == "windows" {
		outputPath += ".exe"
	}
	cmd := exec.Command(
		"go", "build", "-o", outputPath, "github.com/utreexo/utreexod",
	)
	// We specificy CGO_ENABLED=0 to build without the bdk wallet.
	// TODO later put in integration tests for the bdk wallet too.
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	err = cmd.Run()
	if err != nil {
		return "", fmt.Errorf("Failed to build utreexod: %v", err)
	}

	// Save executable path so future calls do not recompile.
	executablePath = outputPath
	return executablePath, nil
}
