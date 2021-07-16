// Copyright (c) 2013-2017 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Copyright (c) 2021 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcaccumulator

import "fmt"

// AccumulatorError describes an issue with
//
// This provides a mechanism for the caller to type assert the error to
// differentiate between general io errors such as io.EOF.
type AccumulatorError struct {
	Func        string // Function name
	Description string // Human readable description of the issue
}

// Error satisfies the error interface and prints human-readable errors.
func (e *AccumulatorError) Error() string {
	if e.Func != "" {
		return fmt.Sprintf("%v: %v", e.Func, e.Description)
	}
	return e.Description
}

// accumulatorError creates an error for the given function and description.
func accumulatorError(f string, desc string) *AccumulatorError {
	return &AccumulatorError{Func: f, Description: desc}
}
