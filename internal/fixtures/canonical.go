package fixtures

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// canonicalJSON is the single canonical JSON writer (CanonicalJson): Jackson's
// DefaultPrettyPrinter with 2-space indentation for objects and arrays,
// "key": value separators, {} and [] for empty containers, numbers as Java
// prints them, LF line endings and one trailing newline. Same tree in,
// identical bytes out, which is what makes drift a byte comparison.
func canonicalJSON(v any) []byte {
	var b strings.Builder
	writeJSON(&b, v, 0)
	b.WriteByte('\n')
	return []byte(b.String())
}

func indent(b *strings.Builder, level int) {
	b.WriteByte('\n')
	for i := 0; i < level; i++ {
		b.WriteString("  ")
	}
}

func writeJSON(b *strings.Builder, v any, level int) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case string:
		writeJSONString(b, x)
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case jsonx.Number:
		writeJSONNumber(b, x)
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		writeJSONNumber(b, jsonx.DoubleNumber(x))
	case *big.Int:
		b.WriteString(x.String())
	case []byte:
		// Jackson writes binary as a base64 string.
		writeJSONString(b, base64Std(x))
	case *jsonx.Object:
		b.WriteByte('{')
		if x.Len() == 0 {
			b.WriteByte('}')
			return
		}
		for i, k := range x.Keys() {
			if i > 0 {
				b.WriteByte(',')
			}
			indent(b, level+1)
			writeJSONString(b, k)
			b.WriteString(": ")
			writeJSON(b, get(x, k), level+1)
		}
		indent(b, level)
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		if len(x) == 0 {
			b.WriteByte(']')
			return
		}
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			indent(b, level+1)
			writeJSON(b, e, level+1)
		}
		indent(b, level)
		b.WriteByte(']')
	default:
		panic(fmt.Sprintf("canonicalJSON: unsupported %T", v))
	}
}

func writeJSONNumber(b *strings.Builder, n jsonx.Number) {
	if n.Kind() == jsonx.Double || n.Kind() == jsonx.Float {
		f := n.Float64()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			// WRITE_NAN_AS_STRINGS is on by default.
			writeJSONString(b, javafmt.Double(f))
			return
		}
	}
	b.WriteString(n.String())
}

// writeJSONString escapes like Jackson: quote, backslash, the short escapes
// for \b \t \n \f \r, \u00XX for other control characters, everything else
// verbatim.
func writeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// compactJSON is ObjectMapper.writeValueAsBytes: the same values without
// whitespace.
func compactJSON(v any) []byte {
	var b strings.Builder
	writeCompact(&b, v)
	return []byte(b.String())
}

func writeCompact(b *strings.Builder, v any) {
	switch x := v.(type) {
	case *jsonx.Object:
		b.WriteByte('{')
		for i, k := range x.Keys() {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, k)
			b.WriteByte(':')
			writeCompact(b, get(x, k))
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCompact(b, e)
		}
		b.WriteByte(']')
	default:
		writeJSON(b, v, 0)
	}
}
