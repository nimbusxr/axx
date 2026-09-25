package javafmt

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode"
)

// NumberFormatError reports input that a Java number parser rejects. Its
// message matches java.lang.NumberFormatException's.
type NumberFormatError struct {
	Message string
}

func (e *NumberFormatError) Error() string { return e.Message }

func forInputString(s string) error {
	return &NumberFormatError{Message: `For input string: "` + s + `"`}
}

// ParseInt parses s like Java's Integer.parseInt(s): an optional '+' or '-'
// followed by decimal digits (any Unicode decimal digit, as
// Character.digit(ch, 10) accepts), no surrounding whitespace, no overflow.
func ParseInt(s string) (int32, error) {
	v, err := parseJavaInteger(s, math.MinInt32, math.MaxInt32)
	return int32(v), err
}

// ParseLong parses s like Java's Long.parseLong(s).
func ParseLong(s string) (int64, error) {
	return parseJavaInteger(s, math.MinInt64, math.MaxInt64)
}

func parseJavaInteger(s string, lo, hi int64) (int64, error) {
	if s == "" {
		return 0, forInputString(s)
	}
	runes := []rune(s)
	neg := false
	i := 0
	switch runes[0] {
	case '-':
		neg = true
		i = 1
	case '+':
		i = 1
	}
	if i == len(runes) {
		return 0, forInputString(s)
	}
	// Java's algorithm: accumulate negatively so the minimum value parses.
	limit := -hi
	if neg {
		limit = lo
	}
	multmin := limit / 10
	var acc int64
	for ; i < len(runes); i++ {
		d := int64(DigitValue(runes[i]))
		if d < 0 || acc < multmin {
			return 0, forInputString(s)
		}
		acc *= 10
		if acc < limit+d {
			return 0, forInputString(s)
		}
		acc -= d
	}
	if neg {
		return acc, nil
	}
	return -acc, nil
}

// DigitValue returns the decimal value of r as Java's Character.digit(r, 10)
// computes it for a char: 0-9 for any Unicode decimal digit (general category
// Nd) in the Basic Multilingual Plane, -1 otherwise.
func DigitValue(r rune) int {
	if r >= '0' && r <= '9' {
		return int(r - '0')
	}
	if r > 0xFFFF || !unicode.Is(unicode.Nd, r) {
		return -1
	}
	// Decimal digits come in contiguous runs of ten starting at zero.
	k := 0
	for k < 9 && unicode.Is(unicode.Nd, r-rune(k+1)) {
		k++
	}
	return k
}

// ParseBoolean is Java's Boolean.parseBoolean: true only for "true", ignoring
// case.
func ParseBoolean(s string) bool {
	return strings.EqualFold(s, "true")
}

// ParseBigInteger parses s like Java's new BigInteger(s): an optional sign and
// Unicode decimal digits.
func ParseBigInteger(s string) (*big.Int, error) {
	runes := []rune(s)
	i := 0
	if len(runes) > 0 && (runes[0] == '-' || runes[0] == '+') {
		i = 1
	}
	if i == len(runes) {
		return nil, &NumberFormatError{Message: "Zero length BigInteger"}
	}
	var b strings.Builder
	if runes[0] == '-' {
		b.WriteByte('-')
	}
	for ; i < len(runes); i++ {
		d := DigitValue(runes[i])
		if d < 0 {
			return nil, &NumberFormatError{Message: "Illegal digit"}
		}
		b.WriteByte(byte('0' + d))
	}
	n, _ := new(big.Int).SetString(b.String(), 10)
	return n, nil
}

// ParseDouble parses s like Java's Double.parseDouble(s): leading and
// trailing characters up to U+0020 are ignored; accepted forms are an
// optional sign followed by "NaN", "Infinity", a decimal significand with an
// optional exponent, or a hexadecimal significand with a binary exponent,
// each optionally followed by one of f, F, d or D. The result is correctly
// rounded.
func ParseDouble(s string) (float64, error) {
	return parseJavaFloat(s, 64)
}

// ParseFloat parses s like Java's Float.parseFloat(s).
func ParseFloat(s string) (float32, error) {
	f, err := parseJavaFloat(s, 32)
	return float32(f), err
}

func parseJavaFloat(s string, bitSize int) (float64, error) {
	in := strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
	if in == "" {
		return 0, &NumberFormatError{Message: "empty String"}
	}
	fail := func() (float64, error) { return 0, forInputString(in) }
	i := 0
	neg := false
	if in[0] == '+' || in[0] == '-' {
		neg = in[0] == '-'
		i++
	}
	rest := in[i:]
	sign := 1.0
	if neg {
		sign = -1
	}
	switch {
	case rest == "NaN":
		return math.NaN(), nil
	case rest == "Infinity":
		return math.Inf(int(sign)), nil
	case strings.HasPrefix(rest, "0x") || strings.HasPrefix(rest, "0X"):
		return parseHexFloat(in, rest[2:], neg, bitSize)
	}
	// Decimal: digits [. digits] [(e|E) [+-] digits] [fFdD]
	j := 0
	digitsBefore := 0
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
		digitsBefore++
	}
	digitsAfter := 0
	if j < len(rest) && rest[j] == '.' {
		j++
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			j++
			digitsAfter++
		}
	}
	if digitsBefore+digitsAfter == 0 {
		return fail()
	}
	if j < len(rest) && (rest[j] == 'e' || rest[j] == 'E') {
		j++
		if j < len(rest) && (rest[j] == '+' || rest[j] == '-') {
			j++
		}
		expDigits := 0
		for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
			j++
			expDigits++
		}
		if expDigits == 0 {
			return fail()
		}
	}
	numEnd := j
	if j < len(rest) && strings.ContainsRune("fFdD", rune(rest[j])) {
		j++
	}
	if j != len(rest) {
		return fail()
	}
	text := rest[:numEnd]
	if neg {
		text = "-" + text
	}
	f, err := strconv.ParseFloat(text, bitSize)
	if err != nil && !isRangeErr(err) {
		return fail()
	}
	return f, nil
}

func parseHexFloat(in, hex string, neg bool, bitSize int) (float64, error) {
	fail := func() (float64, error) { return 0, forInputString(in) }
	j := 0
	n := 0
	for j < len(hex) && isHex(hex[j]) {
		j++
		n++
	}
	if j < len(hex) && hex[j] == '.' {
		j++
		for j < len(hex) && isHex(hex[j]) {
			j++
			n++
		}
	}
	if n == 0 || j >= len(hex) || (hex[j] != 'p' && hex[j] != 'P') {
		return fail()
	}
	j++
	if j < len(hex) && (hex[j] == '+' || hex[j] == '-') {
		j++
	}
	expDigits := 0
	for j < len(hex) && hex[j] >= '0' && hex[j] <= '9' {
		j++
		expDigits++
	}
	if expDigits == 0 {
		return fail()
	}
	numEnd := j
	if j < len(hex) && strings.ContainsRune("fFdD", rune(hex[j])) {
		j++
	}
	if j != len(hex) {
		return fail()
	}
	text := "0x" + hex[:numEnd]
	if neg {
		text = "-" + text
	}
	f, err := strconv.ParseFloat(text, bitSize)
	if err != nil && !isRangeErr(err) {
		return fail()
	}
	return f, nil
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func isRangeErr(err error) bool {
	return errors.Is(err, strconv.ErrRange)
}
