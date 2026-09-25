package javafmt

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// BigDecimal is a java.math.BigDecimal value: Unscaled × 10^-Scale. The zero
// value is 0 with scale 0.
type BigDecimal struct {
	unscaled *big.Int
	scale    int32
}

// NewBigDecimal returns unscaled × 10^-scale.
func NewBigDecimal(unscaled *big.Int, scale int32) BigDecimal {
	return BigDecimal{unscaled: new(big.Int).Set(unscaled), scale: scale}
}

// ParseBigDecimal parses s like Java's new BigDecimal(String): an optional
// sign, decimal digits (any Unicode decimal digit) with at most one '.', and
// an optional exponent introduced by 'e' or 'E'.
func ParseBigDecimal(s string) (BigDecimal, error) {
	runes := []rune(s)
	i := 0
	neg := false
	if i < len(runes) && (runes[i] == '-' || runes[i] == '+') {
		neg = runes[i] == '-'
		i++
	}
	var digits strings.Builder
	frac := 0
	seenDot := false
	nDigits := 0
	for ; i < len(runes); i++ {
		r := runes[i]
		if d := DigitValue(r); d >= 0 {
			digits.WriteByte(byte('0' + d))
			nDigits++
			if seenDot {
				frac++
			}
			continue
		}
		if r == '.' {
			if seenDot {
				return BigDecimal{}, &NumberFormatError{Message: "Character array contains more than one decimal point."}
			}
			seenDot = true
			continue
		}
		if r == 'e' || r == 'E' {
			break
		}
		return BigDecimal{}, &NumberFormatError{Message: "Character " + string(r) +
			` is neither a decimal digit number, decimal point, nor "e" notation exponential mark.`}
	}
	if nDigits == 0 {
		return BigDecimal{}, &NumberFormatError{Message: "No digits found."}
	}
	exp := int64(0)
	if i < len(runes) {
		i++ // 'e' or 'E'
		expNeg := false
		if i < len(runes) && (runes[i] == '-' || runes[i] == '+') {
			expNeg = runes[i] == '-'
			i++
		}
		if i == len(runes) {
			return BigDecimal{}, &NumberFormatError{Message: "No exponent digits."}
		}
		for ; i < len(runes); i++ {
			d := DigitValue(runes[i])
			if d < 0 {
				return BigDecimal{}, &NumberFormatError{Message: "Exponent contains non-digit character."}
			}
			exp = exp*10 + int64(d)
			if exp > math.MaxInt32 {
				return BigDecimal{}, &NumberFormatError{Message: "Exponent overflow."}
			}
		}
		if expNeg {
			exp = -exp
		}
	}
	scale := int64(frac) - exp
	if scale > math.MaxInt32 || scale < math.MinInt32 {
		return BigDecimal{}, &NumberFormatError{Message: "Scale out of range."}
	}
	u, _ := new(big.Int).SetString(digits.String(), 10)
	if neg {
		u.Neg(u)
	}
	return BigDecimal{unscaled: u, scale: int32(scale)}, nil
}

func (d BigDecimal) unscaledValue() *big.Int {
	if d.unscaled == nil {
		return new(big.Int)
	}
	return d.unscaled
}

// Unscaled returns a copy of the unscaled value.
func (d BigDecimal) Unscaled() *big.Int { return new(big.Int).Set(d.unscaledValue()) }

// Scale returns the scale.
func (d BigDecimal) Scale() int32 { return d.scale }

// String renders d like BigDecimal.toString(): plain notation when the scale
// is non-negative and the adjusted exponent is at least -6, scientific
// notation ("1E+5", "1.23E-7") otherwise.
func (d BigDecimal) String() string {
	u := d.unscaledValue()
	coeff := new(big.Int).Abs(u).String()
	var sb strings.Builder
	if u.Sign() < 0 {
		sb.WriteByte('-')
	}
	adjusted := -int64(d.scale) + int64(len(coeff)-1)
	if d.scale >= 0 && adjusted >= -6 {
		switch {
		case d.scale == 0:
			sb.WriteString(coeff)
		case len(coeff) > int(d.scale):
			point := len(coeff) - int(d.scale)
			sb.WriteString(coeff[:point])
			sb.WriteByte('.')
			sb.WriteString(coeff[point:])
		default:
			sb.WriteString("0.")
			sb.WriteString(strings.Repeat("0", int(d.scale)-len(coeff)))
			sb.WriteString(coeff)
		}
		return sb.String()
	}
	sb.WriteByte(coeff[0])
	if len(coeff) > 1 {
		sb.WriteByte('.')
		sb.WriteString(coeff[1:])
	}
	sb.WriteByte('E')
	if adjusted > 0 {
		sb.WriteByte('+')
	}
	sb.WriteString(strconv.FormatInt(adjusted, 10))
	return sb.String()
}

// PlainString renders d like BigDecimal.toPlainString(): no exponent.
func (d BigDecimal) PlainString() string {
	u := d.unscaledValue()
	switch {
	case d.scale == 0:
		return u.String()
	case d.scale < 0:
		if u.Sign() == 0 {
			return "0"
		}
		return u.String() + strings.Repeat("0", int(-int64(d.scale)))
	}
	coeff := new(big.Int).Abs(u).String()
	sign := ""
	if u.Sign() < 0 {
		sign = "-"
	}
	scale := int(d.scale)
	if len(coeff) > scale {
		return sign + coeff[:len(coeff)-scale] + "." + coeff[len(coeff)-scale:]
	}
	return sign + "0." + strings.Repeat("0", scale-len(coeff)) + coeff
}

// Cmp compares the numeric values of d and o (BigDecimal.compareTo), ignoring
// scale: 1.0 and 1.00 compare equal.
func (d BigDecimal) Cmp(o BigDecimal) int {
	a, b := d.unscaledValue(), o.unscaledValue()
	switch {
	case d.scale == o.scale:
		return a.Cmp(b)
	case d.scale < o.scale:
		return new(big.Int).Mul(a, pow10(int64(o.scale)-int64(d.scale))).Cmp(b)
	default:
		return a.Cmp(new(big.Int).Mul(b, pow10(int64(d.scale)-int64(o.scale))))
	}
}

// Equal is BigDecimal.equals: equal value and equal scale.
func (d BigDecimal) Equal(o BigDecimal) bool {
	return d.scale == o.scale && d.unscaledValue().Cmp(o.unscaledValue()) == 0
}

// IntValue is BigDecimal.intValue(): the value truncated toward zero, then
// reduced to its low-order 32 bits.
func (d BigDecimal) IntValue() int32 {
	n := d.toBigInt()
	low := new(big.Int).And(n, big.NewInt(0xFFFFFFFF))
	return int32(uint32(low.Uint64()))
}

// Float64 is BigDecimal.doubleValue().
func (d BigDecimal) Float64() float64 {
	f, _ := strconv.ParseFloat(d.String(), 64)
	return f
}

func (d BigDecimal) toBigInt() *big.Int {
	u := d.unscaledValue()
	if d.scale <= 0 {
		return new(big.Int).Mul(u, pow10(-int64(d.scale)))
	}
	return new(big.Int).Quo(u, pow10(int64(d.scale)))
}

func pow10(n int64) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(n), nil)
}
