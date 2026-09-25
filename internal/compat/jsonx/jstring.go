package jsonx

import (
	"strings"
	"unicode"
	"unicode/utf16"
)

// Java strings are sequences of UTF-16 code units, and the ported code
// indexes them that way (error positions, lengths, comparisons). jstr is that
// representation.
type jstr []uint16

func toJ(s string) jstr { return jstr(utf16.Encode([]rune(s))) }

func (j jstr) String() string { return string(utf16.Decode(j)) }

// sub returns j[from:to] as a Go string, panicking like Java's substring when
// the range is invalid.
func (j jstr) sub(from, to int) string {
	if from < 0 || to > len(j) || from > to {
		throw(StringIndexOutOfBounds, "begin %d, end %d, length %d", from, to, len(j))
	}
	return j[from:to].String()
}

// at is Java's String.charAt.
func (j jstr) at(i int) uint16 {
	if i < 0 || i >= len(j) {
		throw(StringIndexOutOfBounds, "Index %d out of bounds for length %d", i, len(j))
	}
	return j[i]
}

// utf16Len is Java's String.length().
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// compareJava is Java's String.compareTo: lexicographic over UTF-16 code
// units.
func compareJava(a, b string) int {
	if a == b {
		return 0
	}
	ja, jb := toJ(a), toJ(b)
	n := min(len(ja), len(jb))
	for i := 0; i < n; i++ {
		if ja[i] != jb[i] {
			return int(ja[i]) - int(jb[i])
		}
	}
	return len(ja) - len(jb)
}

// javaTrim is Java's String.trim: strips code units up to U+0020 from both
// ends.
func javaTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
}

// isJavaWhitespace is Character.isWhitespace for a UTF-16 code unit.
func isJavaWhitespace(c uint16) bool {
	switch c {
	case '\t', '\n', 0x0B, '\f', '\r', 0x1C, 0x1D, 0x1E, 0x1F:
		return true
	case 0x00A0, 0x2007, 0x202F:
		return false
	}
	r := rune(c)
	return unicode.In(r, unicode.Zs, unicode.Zl, unicode.Zp)
}

// isJavaDigit is Character.isDigit for a UTF-16 code unit.
func isJavaDigit(c uint16) bool {
	return c >= '0' && c <= '9' || c >= 0x80 && unicode.Is(unicode.Nd, rune(c))
}

// javaSplit is Java's String.split(sep) for a single-character, non-regex
// separator: trailing empty strings are removed.
func javaSplit(s string, sep string) []string {
	parts := strings.Split(s, sep)
	for len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 && s == "" {
		return []string{""}
	}
	return parts
}

// splitCommaKeepAll is Pattern.compile("\\s*,\\s*").split(s, -1): split on
// commas with surrounding whitespace, keeping trailing empty strings.
func splitCommaKeepAll(s string) []string {
	parts := strings.Split(s, ",")
	for i, p := range parts {
		if i > 0 {
			p = strings.TrimLeftFunc(p, isRegexSpace)
		}
		if i < len(parts)-1 {
			p = strings.TrimRightFunc(p, isRegexSpace)
		}
		parts[i] = p
	}
	return parts
}

// isRegexSpace is Java regex \s: [ \t\n\x0B\f\r].
func isRegexSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == 0x0B || r == '\f' || r == '\r'
}
