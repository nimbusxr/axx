// Package detmath has the math functions a stub's build uses, the same bit
// for bit on every machine. Go may fuse a multiply and an add into one
// instruction where the processor has one (arm64's, not amd64's), which
// rounds once where there were two roundings, and its math package's
// functions are compiled so: their results differ in their last bits from one
// processor to another, and so would a stub built with them. Here each product
// is rounded on its own (an explicit conversion, which Go does not fuse), and
// the results are within an ulp or two of math's.
package detmath

import "math"

const (
	ln2Hi  = 6.93147180369123816490e-01 // ln 2's high bits: its multiples by an integer are exact
	ln2Lo  = 1.90821492927058770002e-10 // the rest of ln 2
	invLn2 = 1.44269504088896338700e+00

	pio2Hi    = 1.57079632673412561417e+00 // π/2's first 33 bits
	pio2Mid   = 6.07710050650619224932e-11 // its next 33
	pio2Lo    = 2.02226624879595063154e-21 // the rest
	twoOverPi = 6.36619772367581382433e-01

	// reduceMax bounds the angles Sin and Cos reduce here, whose multiples of
	// π/2's first bits are exact: larger ones are math's.
	reduceMax = 1 << 28
)

// Exp is e to the x.
func Exp(x float64) float64 {
	switch {
	case math.IsNaN(x) || math.IsInf(x, 1):
		return x
	case x > 709.782712893384:
		return math.Inf(1)
	case x < -745.1332191019412:
		return 0
	}
	// x = k·ln 2 + r, |r| ≤ ln 2 / 2 (about), and e^r by its series, to r^14/14!
	// (below an ulp of the sum).
	k := math.Round(float64(x * invLn2))
	r := float64(x-float64(k*ln2Hi)) - float64(k*ln2Lo)
	p := 1.0
	for n := 14; n >= 1; n-- {
		p = 1 + float64(r*p)/float64(n)
	}
	return math.Ldexp(p, int(k))
}

// Log is the natural logarithm of x.
func Log(x float64) float64 {
	switch {
	case math.IsNaN(x) || math.IsInf(x, 1):
		return x
	case x < 0:
		return math.NaN()
	case x == 0:
		return math.Inf(-1)
	}
	// x = f·2^e, √½ ≤ f < √2. With g = f − 1 and s = g/(2+g), log f = 2·atanh(s)
	// = g − (g²/2 − s·(g²/2 + R)), R = 2(s²/3 + s⁴/5 + …), to s²⁴ (|s| ≤ 0.172:
	// below an ulp of the sum).
	f, e := math.Frexp(x)
	if f < math.Sqrt2/2 {
		f *= 2
		e--
	}
	g := f - 1
	s := g / (2 + g)
	z := float64(s * s)
	R := 0.0
	for n := 25; n >= 3; n -= 2 {
		R = float64(z * float64(2/float64(n)+R))
	}
	hfsq := float64(0.5 * float64(g*g))
	k := float64(e)
	lo := float64(s*float64(hfsq+R)) + float64(k*ln2Lo)
	return float64(k*ln2Hi) - ((hfsq - lo) - g)
}

// Log10 is the base-10 logarithm of x.
func Log10(x float64) float64 { return Log(x) / math.Ln10 }

// Pow is x to the y, of x > 0 (others are math's).
func Pow(x, y float64) float64 {
	if !(x > 0) || math.IsInf(x, 0) || math.IsNaN(y) || math.IsInf(y, 0) {
		return math.Pow(x, y)
	}
	return Exp(float64(y * Log(x)))
}

// Sin is the sine of x, in radians.
func Sin(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) >= reduceMax {
		return math.Sin(x)
	}
	r, j := reduce(x)
	switch j {
	case 0:
		return sin(r)
	case 1:
		return cos(r)
	case 2:
		return -sin(r)
	default:
		return -cos(r)
	}
}

// Cos is the cosine of x, in radians.
func Cos(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) >= reduceMax {
		return math.Cos(x)
	}
	r, j := reduce(x)
	switch j {
	case 0:
		return cos(r)
	case 1:
		return -sin(r)
	case 2:
		return -cos(r)
	default:
		return sin(r)
	}
}

// reduce is x − j·π/2, |r| ≤ π/4 (about), and j mod 4.
func reduce(x float64) (float64, int) {
	j := math.Round(float64(x * twoOverPi))
	r := float64(float64(x-float64(j*pio2Hi))-float64(j*pio2Mid)) - float64(j*pio2Lo)
	return r, int(int64(j) & 3)
}

// sin is the sine of r, |r| ≤ π/4, by its series to r^21/21!:
// r·(1 − r²/(2·3)·(1 − r²/(4·5)·(1 − …))).
func sin(r float64) float64 {
	r2 := float64(r * r)
	q := 1.0
	for n := 20; n >= 2; n -= 2 {
		q = 1 - float64(r2*q)/float64(n*(n+1))
	}
	return float64(r * q)
}

// cos is the cosine of r, |r| ≤ π/4, by its series to r^20/20!:
// 1 − r²/(1·2)·(1 − r²/(3·4)·(1 − …)).
func cos(r float64) float64 {
	r2 := float64(r * r)
	q := 1.0
	for n := 19; n >= 1; n -= 2 {
		q = 1 - float64(r2*q)/float64(n*(n+1))
	}
	return q
}
