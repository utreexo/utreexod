package aggregator

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func littleEndianBytesToBigInt(b []byte) *big.Int {
	be := make([]byte, len(b))
	for i := range b {
		be[len(b)-1-i] = b[i]
	}
	return new(big.Int).SetBytes(be)
}

func bigIntToLittleEndian64(x *big.Int) [64]byte {
	var out [64]byte
	be := x.Bytes()
	if len(be) > len(out) {
		panic("integer does not fit into 64 bytes")
	}
	for i := 0; i < len(be); i++ {
		out[i] = be[len(be)-1-i]
	}
	return out
}

func TestAgg512Bytes(t *testing.T) {
	tests := []struct {
		name string
		in   [64]byte
	}{
		{name: "all-zero", in: [64]byte{}},
		{name: "all-ff", in: func() (b [64]byte) {
			for i := range b {
				b[i] = 0xFF
			}
			return
		}()},
		{name: "lsb-only", in: func() (b [64]byte) { b[0] = 0x01; return }()},
		{name: "msb-only", in: func() (b [64]byte) { b[63] = 0x80; return }()},
		{name: "pattern", in: func() (b [64]byte) {
			for i := range b {
				b[i] = byte(i*7 + 3)
			}
			return
		}()},
	}

	for _, tt := range tests {
		var agg Agg512
		agg.InitFromBytes(tt.in)

		out := agg.Bytes()
		require.Equal(t, tt.in, out)
	}
}

func TestAgg512AddSub(t *testing.T) {
	tests := []struct {
		name string
		acc  [64]byte
		adds [][32]byte
	}{
		{
			name: "carry-within-low",
			acc: func() (b [64]byte) {
				for i := 0; i < 32; i++ {
					b[i] = 0xFF
				}
				return
			}(),
			adds: func() [][32]byte {
				b := [][32]byte{
					{0x1},
				}
				return b
			}(),
		},
		{
			name: "patterned",
			acc: func() (b [64]byte) {
				for i := range b {
					b[i] = byte(i*13 + 5)
				}
				return
			}(),
			adds: func() [][32]byte {
				adds := make([][32]byte, 1)
				for i := range adds[0] {
					adds[0][i] = byte(i*17 + 9)
				}
				return adds
			}(),
		},
		{
			name: "double-add-cross-half",
			acc: func() (b [64]byte) {
				b[32] = 0xAA
				b[45] = 0x11
				return
			}(),
			adds: func() [][32]byte {
				adds := make([][32]byte, 2)
				for i := range adds[0] {
					adds[0][i] = 0xFF
				}
				adds[1][0] = 0x01
				return adds
			}(),
		},
		{
			name: "full-wrap-around",
			acc: func() (b [64]byte) {
				for i := range b {
					b[i] = 0xFF
				}
				return
			}(),
			adds: func() [][32]byte {
				b := [][32]byte{
					{0x1},
				}
				return b
			}(),
		},
		{
			name: "multi-add-patterns",
			acc: func() (b [64]byte) {
				for i := range b {
					b[i] = byte(i*5 + 7)
				}
				return
			}(),
			adds: func() [][32]byte {
				adds := make([][32]byte, 3)
				for i := range adds {
					for j := range adds[i] {
						adds[i][j] = byte(i*29 + j*13 + 3)
					}
				}
				return adds
			}(),
		},
	}

	for _, tt := range tests {
		var agg Agg512
		agg.InitFromBytes(tt.acc)
		expectedAdd := littleEndianBytesToBigInt(tt.acc[:])

		for _, add := range tt.adds {
			agg.Add256(&add)

			addInt := littleEndianBytesToBigInt(add[:])
			expectedAdd.Add(expectedAdd, addInt)

			// Probably never needed since we won't ever go over 2**512
			// but why not just test it.
			var mod512 = new(big.Int).Lsh(big.NewInt(1), 512)
			expectedAdd.Mod(expectedAdd, mod512)
		}

		require.Equal(t, bigIntToLittleEndian64(expectedAdd), agg.Bytes())
		require.NotEqual(t, tt.acc, agg.Bytes())

		for _, add := range tt.adds {
			agg.Sub256(&add)
		}

		require.Equal(t, tt.acc, agg.Bytes())
	}
}
