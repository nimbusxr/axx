package javafmt

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Double renders f exactly like Java's Double.toString(double).
func Double(f float64) string {
	return formatJava(f, 64)
}

// Float renders f exactly like Java's Float.toString(float).
func Float(f float32) string {
	return formatJava(float64(f), 32)
}

// formatJava implements the JDK 19+ Double/Float.toString specification:
// pick the shortest decimal that rounds to the value (considering two-digit
// candidates too when the shortest has a single digit, and preferring the
// closest one), then render it plainly for magnitudes in [1e-3, 1e7) and in
// computerized scientific notation otherwise.
func formatJava(f float64, bitSize int) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	digits, exp := shortestDigits(f, bitSize)
	var sb strings.Builder
	if f < 0 {
		sb.WriteByte('-')
	}
	abs := math.Abs(f)
	// exp is the decimal exponent of the first digit: value = d.ddd × 10^exp.
	if abs >= 1e-3 && abs < 1e7 {
		if exp >= 0 {
			intLen := exp + 1
			if len(digits) <= intLen {
				sb.WriteString(digits)
				sb.WriteString(strings.Repeat("0", intLen-len(digits)))
				sb.WriteString(".0")
			} else {
				sb.WriteString(digits[:intLen])
				sb.WriteByte('.')
				sb.WriteString(digits[intLen:])
			}
		} else {
			sb.WriteString("0.")
			sb.WriteString(strings.Repeat("0", -exp-1))
			sb.WriteString(digits)
		}
		return sb.String()
	}
	sb.WriteByte(digits[0])
	sb.WriteByte('.')
	if len(digits) > 1 {
		sb.WriteString(digits[1:])
	} else {
		sb.WriteByte('0')
	}
	sb.WriteByte('E')
	sb.WriteString(strconv.Itoa(exp))
	return sb.String()
}

// shortestDigits returns the significant decimal digits (without trailing
// zeros) and the decimal exponent of the first digit selected by Java's
// algorithm for the finite, non-zero value f.
func shortestDigits(f float64, bitSize int) (string, int) {
	digits, exp := splitE(strconv.FormatFloat(math.Abs(f), 'e', -1, bitSize))
	if len(digits) != 1 {
		return digits, exp
	}
	// Java also considers two-digit decimals when the shortest has one digit
	// and picks whichever candidate is closest to the exact value (ties go to
	// an even least significant digit). Double.MIN_VALUE is "4.9E-324", not
	// "5.0E-324".
	two := strconv.FormatFloat(math.Abs(f), 'e', 1, bitSize)
	d2, e2 := splitE(two)
	if len(d2) < 2 {
		return digits, exp
	}
	if back, err := strconv.ParseFloat(two, bitSize); err != nil || back != math.Abs(f) {
		return digits, exp
	}
	exact := new(big.Rat)
	if bitSize == 32 {
		exact.SetFloat64(float64(float32(math.Abs(f))))
	} else {
		exact.SetFloat64(math.Abs(f))
	}
	dist1 := distance(digits, exp, exact)
	dist2 := distance(d2, e2, exact)
	switch dist2.Cmp(dist1) {
	case -1:
		return d2, e2
	case 0:
		if (d2[len(d2)-1]-'0')%2 == 0 && (digits[0]-'0')%2 != 0 {
			return d2, e2
		}
	}
	return digits, exp
}

// splitE splits strconv's 'e' format ("1.2345e+06") into digits without a
// decimal point and trailing zeros ("12345") and the exponent (6).
func splitE(s string) (string, int) {
	mant, expText, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expText)
	digits := strings.Replace(mant, ".", "", 1)
	digits = strings.TrimRight(digits, "0")
	if digits == "" {
		digits = "0"
	}
	return digits, exp
}

// distance returns |0.digits × 10^(exp+1) - exact|.
func distance(digits string, exp int, exact *big.Rat) *big.Rat {
	v := decimalRat(digits, exp-len(digits)+1)
	return v.Abs(v.Sub(v, exact))
}

// decimalRat returns digits × 10^pow10 as an exact rational.
func decimalRat(digits string, pow10 int) *big.Rat {
	n, _ := new(big.Int).SetString(digits, 10)
	r := new(big.Rat).SetInt(n)
	p := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(pow10))), nil)
	if pow10 >= 0 {
		return r.Mul(r, new(big.Rat).SetInt(p))
	}
	return r.Quo(r, new(big.Rat).SetInt(p))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
