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

// IsAggZero returns true if the underlying aggregator is 0. False if not.
func (a *Agg512) IsAggZero() bool {
	return a.limbs[0] == 0 &&
		a.limbs[1] == 0 &&
		a.limbs[2] == 0 &&
		a.limbs[3] == 0 &&
		a.limbs[4] == 0 &&
		a.limbs[5] == 0 &&
		a.limbs[6] == 0 &&
		a.limbs[7] == 0
}

// InitFromBytes initializes from a little-endian [64]byte.
func (a *Agg512) InitFromBytes(b [64]byte) {
	a.limbs[0] = binary.LittleEndian.Uint64(b[0:8])
	a.limbs[1] = binary.LittleEndian.Uint64(b[8:16])
	a.limbs[2] = binary.LittleEndian.Uint64(b[16:24])
	a.limbs[3] = binary.LittleEndian.Uint64(b[24:32])
	a.limbs[4] = binary.LittleEndian.Uint64(b[32:40])
	a.limbs[5] = binary.LittleEndian.Uint64(b[40:48])
	a.limbs[6] = binary.LittleEndian.Uint64(b[48:56])
	a.limbs[7] = binary.LittleEndian.Uint64(b[56:64])
}

// Bytes writes out as little-endian [64]byte.
func (a *Agg512) Bytes() [64]byte {
	var out [64]byte
	binary.LittleEndian.PutUint64(out[0:8], a.limbs[0])
	binary.LittleEndian.PutUint64(out[8:16], a.limbs[1])
	binary.LittleEndian.PutUint64(out[16:24], a.limbs[2])
	binary.LittleEndian.PutUint64(out[24:32], a.limbs[3])
	binary.LittleEndian.PutUint64(out[32:40], a.limbs[4])
	binary.LittleEndian.PutUint64(out[40:48], a.limbs[5])
	binary.LittleEndian.PutUint64(out[48:56], a.limbs[6])
	binary.LittleEndian.PutUint64(out[56:64], a.limbs[7])

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
	a.limbs[4], carry = bits.Add64(a.limbs[4], 0, carry)
	a.limbs[5], carry = bits.Add64(a.limbs[5], 0, carry)
	a.limbs[6], carry = bits.Add64(a.limbs[6], 0, carry)
	a.limbs[7], carry = bits.Add64(a.limbs[7], 0, carry)
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
	a.limbs[4], borrow = bits.Sub64(a.limbs[4], 0, borrow)
	a.limbs[5], borrow = bits.Sub64(a.limbs[5], 0, borrow)
	a.limbs[6], borrow = bits.Sub64(a.limbs[6], 0, borrow)
	a.limbs[7], borrow = bits.Sub64(a.limbs[7], 0, borrow)
}
