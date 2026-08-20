// Copyright (c) 2024 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package peer

import (
	"testing"
	"time"
)

// TestProofDeadlineExpired covers the per-request getutreexoproof deadline used
// by the stall handler. Each outstanding request keeps its own deadline, so
// dropping any proof in a batch must still trip the check even after earlier
// proofs in the batch have arrived.
func TestProofDeadlineExpired(t *testing.T) {
	base := time.Unix(1000, 0)

	if proofDeadlineExpired(nil, base, 0) {
		t.Fatal("no outstanding deadlines should never be expired")
	}

	// Two outstanding requests; the oldest deadline governs.
	deadlines := []time.Time{
		base.Add(30 * time.Second),
		base.Add(60 * time.Second),
	}
	if proofDeadlineExpired(deadlines, base.Add(10*time.Second), 0) {
		t.Fatal("should not expire before the oldest deadline")
	}
	if !proofDeadlineExpired(deadlines, base.Add(31*time.Second), 0) {
		t.Fatal("should expire once the oldest deadline passes")
	}

	// One proof received pops the front; a later dropped proof in the same
	// batch is still detected by the remaining deadline.
	remaining := deadlines[1:]
	if proofDeadlineExpired(remaining, base.Add(31*time.Second), 0) {
		t.Fatal("should not expire before the remaining deadline")
	}
	if !proofDeadlineExpired(remaining, base.Add(61*time.Second), 0) {
		t.Fatal("should expire once the remaining deadline passes")
	}

	// The handler-time offset pushes the deadline out.
	if proofDeadlineExpired(deadlines, base.Add(31*time.Second), 5*time.Second) {
		t.Fatal("offset should delay expiry")
	}
	if !proofDeadlineExpired(deadlines, base.Add(36*time.Second), 5*time.Second) {
		t.Fatal("should expire once now exceeds deadline plus offset")
	}
}
