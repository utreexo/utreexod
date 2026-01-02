package aggregator

import (
	"encoding/binary"
	"math/bits"
)

// Agg512 keeps a 512-bit aggregator as 8 little-endian uint64 limbs.
// limbs[0] is the least-significant limb.
type Agg512 struct {
	limbs [8]uint64
}

// InitFromBytes initializes from a little-endian [64]byte.
func (a *Agg512) InitFromBytes(b [64]byte) {
	for i := 0; i < 8; i++ {
		a.limbs[i] = binary.LittleEndian.Uint64(b[i*8 : i*8+8])
	}
}

// Bytes writes out as little-endian [64]byte.
func (a *Agg512) Bytes() [64]byte {
	var out [64]byte
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint64(out[i*8:i*8+8], a.limbs[i])
	}

	return out
}

// Add256 adds the numerical value of a [32]byte interpreted as little-endian.
func (a *Agg512) Add256(h *[32]byte) {
	w0 := binary.LittleEndian.Uint64(h[0:8])
	w1 := binary.LittleEndian.Uint64(h[8:16])
	w2 := binary.LittleEndian.Uint64(h[16:24])
	w3 := binary.LittleEndian.Uint64(h[24:32])

	var carry uint64
	a.limbs[0], carry = bits.Add64(a.limbs[0], w0, carry)
	a.limbs[1], carry = bits.Add64(a.limbs[1], w1, carry)
	a.limbs[2], carry = bits.Add64(a.limbs[2], w2, carry)
	a.limbs[3], carry = bits.Add64(a.limbs[3], w3, carry)

	for i := 4; i < 8 && carry != 0; i++ {
		a.limbs[i], carry = bits.Add64(a.limbs[i], 0, carry)
	}
}

// Sub256 subtracts the numerical value of a [32]byte interpreted as little-endian.
func (a *Agg512) Sub256(h *[32]byte) {
	w0 := binary.LittleEndian.Uint64(h[0:8])
	w1 := binary.LittleEndian.Uint64(h[8:16])
	w2 := binary.LittleEndian.Uint64(h[16:24])
	w3 := binary.LittleEndian.Uint64(h[24:32])

	var borrow uint64
	a.limbs[0], borrow = bits.Sub64(a.limbs[0], w0, borrow)
	a.limbs[1], borrow = bits.Sub64(a.limbs[1], w1, borrow)
	a.limbs[2], borrow = bits.Sub64(a.limbs[2], w2, borrow)
	a.limbs[3], borrow = bits.Sub64(a.limbs[3], w3, borrow)

	for i := 4; i < 8 && borrow != 0; i++ {
		a.limbs[i], borrow = bits.Sub64(a.limbs[i], 0, borrow)
	}
}
