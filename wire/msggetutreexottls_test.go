// Copyright (c) 2025 The utreexo developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsgGetUtreexoTTLsEncode(t *testing.T) {
	testCases := []struct {
		version     uint32
		startHeight uint32
		maxExponent uint8
		shouldErr   bool
	}{
		{
			version:     848_248,
			startHeight: 5,
			maxExponent: 1,
			shouldErr:   false,
		},
		{
			version:     84_248,
			startHeight: 6_878,
			maxExponent: 0,
			shouldErr:   false,
		},
		{
			version:     684_248,
			startHeight: 878,
			maxExponent: 0,
			shouldErr:   false,
		},
		{
			version:     284_248,
			startHeight: 112_878,
			maxExponent: 4,
			shouldErr:   false,
		},
		{
			version:     904_248,
			startHeight: 412_878,
			maxExponent: 4,
			shouldErr:   false,
		},
		{
			version:     704_248,
			startHeight: 212_878,
			maxExponent: 5,
			shouldErr:   true,
		},
		{
			version:     104_248,
			startHeight: 92_878,
			maxExponent: 255,
			shouldErr:   true,
		},
		{
			version:     74_248,
			startHeight: 771_878,
			maxExponent: 1,
			shouldErr:   true,
		},
	}

	for _, testCase := range testCases {
		beforeMsg := NewMsgGetUtreexoTTLs(testCase.version, testCase.startHeight, testCase.maxExponent)

		// Encode.
		var buf bytes.Buffer
		err := beforeMsg.BtcEncode(&buf, 0, LatestEncoding)
		if err != nil {
			if !testCase.shouldErr {
				t.Fatal(err)
			}
			continue
		} else {
			if testCase.shouldErr {
				t.Fatal("expected to error but didn't")
			}
		}

		serialized := buf.Bytes()

		afterMsg := MsgGetUtreexoTTLs{}
		r := bytes.NewReader(serialized)
		err = afterMsg.BtcDecode(r, 0, LatestEncoding)
		if err != nil {
			t.Fatal(err)
		}

		require.Equal(t, testCase.version, afterMsg.Version)
		require.Equal(t, testCase.startHeight, afterMsg.StartHeight)
		require.Equal(t, testCase.maxExponent, afterMsg.MaxReceiveExponent)
	}
}

func TestGetUtreexoTTLHeights(t *testing.T) {
	testCases := []struct {
		height          int32
		bestHeight      int32
		exponent        uint8
		expectedHeights []int32
	}{
		{
			height:          0,
			bestHeight:      2,
			exponent:        1,
			expectedHeights: []int32{0, 1},
		},

		{
			height:          1,
			bestHeight:      1,
			exponent:        1,
			expectedHeights: []int32{1},
		},

		{
			height:          1,
			bestHeight:      1,
			exponent:        0,
			expectedHeights: []int32{1},
		},

		{
			height:          8,
			bestHeight:      20,
			exponent:        3,
			expectedHeights: []int32{8, 9, 10, 11, 12, 13, 14, 15},
		},

		{
			height:          8,
			bestHeight:      20,
			exponent:        3,
			expectedHeights: []int32{8, 9, 10, 11, 12, 13, 14, 15},
		},

		{
			height:          9,
			bestHeight:      20,
			exponent:        3,
			expectedHeights: []int32{9, 10, 11, 12, 13, 14, 15},
		},
	}

	for _, testCase := range testCases {
		got, err := GetUtreexoTTLHeights(testCase.height, testCase.bestHeight, testCase.exponent)
		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(testCase.expectedHeights, got) {
			t.Fatalf("expected %v, got %v", testCase.expectedHeights, got)
		}
	}
}
