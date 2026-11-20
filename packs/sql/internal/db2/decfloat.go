package db2

import (
	"fmt"
	"math/big"
	"strconv"
)

// decFloat reads a DECFLOAT: IEEE 754's decimal floating point of 64 or 128
// bits, most significant byte first, its coefficient densely packed (three
// digits in ten bits). It gives the number as Db2 writes it: its digits as
// they are, with an exponent when the point is far from them.
func decFloat(b []byte) (decimal, error) {
	var declets, expBits, bias int
	switch len(b) {
	case 8:
		declets, expBits, bias = 5, 8, 398
	case 16:
		declets, expBits, bias = 11, 12, 6176
	default:
		return "", fmt.Errorf("%w: a DECFLOAT of %d bytes", errProtocol, len(b))
	}
	w := new(big.Int).SetBytes(b)
	bits := len(b) * 8
	bit := func(i int) uint { return w.Bit(i) }
	field := func(from, n int) int {
		v := 0
		for i := n - 1; i >= 0; i-- {
			v = v<<1 | int(bit(from+i))
		}
		return v
	}
	sign := ""
	if bit(bits-1) == 1 {
		sign = "-"
	}
	// The five bits after the sign hold the exponent's two highest bits and
	// the coefficient's first digit, or say the number is none.
	g := field(bits-6, 5)
	switch g {
	case 0x1E:
		return decimal(sign + "Infinity"), nil
	case 0x1F:
		if bit(bits-7) == 1 {
			return decimal(sign + "sNaN"), nil
		}
		return decimal(sign + "NaN"), nil
	}
	var first, high int
	if g>>3 == 3 {
		first, high = 8+g&1, g>>1&3
	} else {
		first, high = g&7, g>>3
	}
	exponent := high<<expBits | field(declets*10, expBits)
	exponent -= bias
	digits := []byte{byte('0' + first)}
	for i := declets - 1; i >= 0; i-- {
		d := denselyPacked(field(i*10, 10))
		digits = append(digits, byte('0'+d/100), byte('0'+d/10%10), byte('0'+d%10))
	}
	return decimal(sign + scientific(digits, exponent)), nil
}

// denselyPacked is the three digits ten bits hold (IEEE 754-2008, 3.5.2).
func denselyPacked(v int) int {
	b := func(i int) int { return v >> i & 1 }
	// The bits, named as the standard names them: b(9) is p, b(0) is y.
	p, q, r, s, t, u, vv, w, x, y := b(9), b(8), b(7), b(6), b(5), b(4), b(3), b(2), b(1), b(0)
	var d2, d1, d0 int
	switch {
	case vv == 0:
		d2, d1, d0 = p<<2|q<<1|r, s<<2|t<<1|u, w<<2|x<<1|y
	case w == 0 && x == 0:
		d2, d1, d0 = p<<2|q<<1|r, s<<2|t<<1|u, 8|y
	case w == 0 && x == 1:
		d2, d1, d0 = p<<2|q<<1|r, 8|u, s<<2|t<<1|y
	case w == 1 && x == 0:
		d2, d1, d0 = 8|r, s<<2|t<<1|u, p<<2|q<<1|y
	case s == 0 && t == 0:
		d2, d1, d0 = 8|r, 8|u, p<<2|q<<1|y
	case s == 0 && t == 1:
		d2, d1, d0 = 8|r, p<<2|q<<1|u, 8|y
	case s == 1 && t == 0:
		d2, d1, d0 = p<<2|q<<1|r, 8|u, 8|y
	default:
		d2, d1, d0 = 8|r, 8|u, 8|y
	}
	return d2*100 + d1*10 + d0
}

// scientific writes a coefficient's digits and an exponent as the General
// Decimal Arithmetic specification's to-scientific-string does, which is how
// Db2 writes a DECFLOAT: 1.23, 1.2E+10, 0.00.
func scientific(digits []byte, exponent int) string {
	for len(digits) > 1 && digits[0] == '0' {
		digits = digits[1:]
	}
	adjusted := exponent + len(digits) - 1
	switch {
	case exponent <= 0 && adjusted >= -6:
		if exponent == 0 {
			return string(digits)
		}
		point := len(digits) + exponent
		if point > 0 {
			return string(digits[:point]) + "." + string(digits[point:])
		}
		zeros := make([]byte, -point)
		for i := range zeros {
			zeros[i] = '0'
		}
		return "0." + string(zeros) + string(digits)
	default:
		s := string(digits[:1])
		if len(digits) > 1 {
			s += "." + string(digits[1:])
		}
		if adjusted >= 0 {
			return s + "E+" + strconv.Itoa(adjusted)
		}
		return s + "E" + strconv.Itoa(adjusted)
	}
}
