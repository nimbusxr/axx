package jvalue

import (
	"fmt"
	"math"
	"math/big"
	"strings"
	"unicode/utf16"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// ParseJackson parses text like Jackson's default ObjectMapper (readTree or
// readValue into Object): strict JSON (double-quoted names, no comments, no
// trailing commas, no leading zeros, no NaN), integers as Integer, Long or
// BigInteger by magnitude, other numbers as Double, objects as *jsonx.Object
// and arrays as []any. Content after the first value is ignored, as Jackson
// ignores it by default. Empty text reads as JSON null.
func ParseJackson(text string) (any, error) {
	p := &jacksonParser{s: text}
	p.skipWS()
	if p.i >= len(p.s) {
		return nil, nil
	}
	v, err := p.value(0, true)
	if err != nil {
		return nil, err
	}
	return v, nil
}

const jacksonMaxDepth = 1000

type jacksonParser struct {
	s string
	i int
}

func (p *jacksonParser) fail(format string, args ...any) error {
	return &jsonx.Error{Kind: jsonx.JSONParse, Message: fmt.Sprintf(format, args...) + fmt.Sprintf(" (at offset %d)", p.i)}
}

func (p *jacksonParser) skipWS() {
	for p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *jacksonParser) value(depth int, root bool) (any, error) {
	if depth > jacksonMaxDepth {
		return nil, p.fail("maximum nesting depth exceeded")
	}
	p.skipWS()
	if p.i >= len(p.s) {
		return nil, p.fail("unexpected end-of-input")
	}
	switch c := p.s[p.i]; {
	case c == '{':
		return p.object(depth)
	case c == '[':
		return p.array(depth)
	case c == '"':
		return p.str()
	case c == '-' || c >= '0' && c <= '9':
		return p.number(root)
	case c == 't' || c == 'f' || c == 'n':
		return p.literal()
	}
	return nil, p.fail("unexpected character %q", p.s[p.i])
}

func (p *jacksonParser) object(depth int) (any, error) {
	p.i++ // '{'
	obj := jsonx.NewObject()
	p.skipWS()
	if p.i < len(p.s) && p.s[p.i] == '}' {
		p.i++
		return obj, nil
	}
	for {
		p.skipWS()
		if p.i >= len(p.s) {
			return nil, p.fail("unexpected end-of-input in object")
		}
		if p.s[p.i] != '"' {
			return nil, p.fail("expected double-quote to start field name")
		}
		key, err := p.str()
		if err != nil {
			return nil, err
		}
		p.skipWS()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, p.fail("expected ':'")
		}
		p.i++
		v, err := p.value(depth+1, false)
		if err != nil {
			return nil, err
		}
		obj.Set(key.(string), v)
		p.skipWS()
		if p.i >= len(p.s) {
			return nil, p.fail("unexpected end-of-input in object")
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case '}':
			p.i++
			return obj, nil
		default:
			return nil, p.fail("expected ',' or '}'")
		}
	}
}

func (p *jacksonParser) array(depth int) (any, error) {
	p.i++ // '['
	items := []any{}
	p.skipWS()
	if p.i < len(p.s) && p.s[p.i] == ']' {
		p.i++
		return items, nil
	}
	for {
		v, err := p.value(depth+1, false)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
		p.skipWS()
		if p.i >= len(p.s) {
			return nil, p.fail("unexpected end-of-input in array")
		}
		switch p.s[p.i] {
		case ',':
			p.i++
		case ']':
			p.i++
			return items, nil
		default:
			return nil, p.fail("expected ',' or ']'")
		}
	}
}

func (p *jacksonParser) str() (any, error) {
	p.i++ // opening quote
	var units []uint16
	for {
		if p.i >= len(p.s) {
			return nil, p.fail("unexpected end-of-input in string")
		}
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return string(utf16.Decode(units)), nil
		case c < 0x20:
			return nil, p.fail("illegal unquoted character in string")
		case c == '\\':
			p.i++
			if p.i >= len(p.s) {
				return nil, p.fail("unexpected end-of-input in escape")
			}
			e := p.s[p.i]
			p.i++
			switch e {
			case '"', '\\', '/':
				units = append(units, uint16(e))
			case 'b':
				units = append(units, '\b')
			case 'f':
				units = append(units, '\f')
			case 'n':
				units = append(units, '\n')
			case 'r':
				units = append(units, '\r')
			case 't':
				units = append(units, '\t')
			case 'u':
				if p.i+4 > len(p.s) {
					return nil, p.fail("unexpected end-of-input in unicode escape")
				}
				var v uint16
				for _, h := range []byte(p.s[p.i : p.i+4]) {
					d := hexDigit(h)
					if d < 0 {
						return nil, p.fail("illegal hex digit in unicode escape")
					}
					v = v<<4 | uint16(d)
				}
				p.i += 4
				units = append(units, v)
			default:
				return nil, p.fail("unrecognized character escape %q", e)
			}
		default:
			r, size := decodeRune(p.s[p.i:])
			units = append(units, utf16.Encode([]rune{r})...)
			p.i += size
		}
	}
}

func decodeRune(s string) (rune, int) {
	for _, r := range s {
		n := len(string(r))
		return r, n
	}
	return 0, 1
}

func hexDigit(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

func (p *jacksonParser) literal() (any, error) {
	for _, lit := range []struct {
		text string
		v    any
	}{{"true", true}, {"false", false}, {"null", nil}} {
		if strings.HasPrefix(p.s[p.i:], lit.text) {
			end := p.i + len(lit.text)
			if end < len(p.s) && isIdentChar(p.s[end]) {
				break
			}
			p.i = end
			return lit.v, nil
		}
	}
	return nil, p.fail("unrecognized token")
}

func isIdentChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c >= 0x80
}

func (p *jacksonParser) number(root bool) (any, error) {
	start := p.i
	if p.s[p.i] == '-' {
		p.i++
	}
	digits := func() int {
		n := 0
		for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
			p.i++
			n++
		}
		return n
	}
	if p.i >= len(p.s) {
		return nil, p.fail("unexpected end-of-input in number")
	}
	intStart := p.i
	n := digits()
	if n == 0 {
		return nil, p.fail("expected digit after '-'")
	}
	if n > 1 && p.s[intStart] == '0' {
		return nil, p.fail("leading zeroes not allowed")
	}
	isFloat := false
	if p.i < len(p.s) && p.s[p.i] == '.' {
		isFloat = true
		p.i++
		if digits() == 0 {
			return nil, p.fail("decimal point must be followed by a digit")
		}
	}
	if p.i < len(p.s) && (p.s[p.i] == 'e' || p.s[p.i] == 'E') {
		isFloat = true
		p.i++
		if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
			p.i++
		}
		if p.i >= len(p.s) {
			return nil, p.fail("unexpected end-of-input in exponent")
		}
		if digits() == 0 {
			return nil, p.fail("exponent must be followed by a digit")
		}
	}
	if root && p.i < len(p.s) {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
		default:
			return nil, p.fail("expected space separating root-level values")
		}
	}
	text := p.s[start:p.i]
	if isFloat {
		f, err := javafmt.ParseDouble(text)
		if err != nil {
			return nil, p.fail("invalid number")
		}
		return jsonx.DoubleNumber(f), nil
	}
	if v, err := javafmt.ParseInt(text); err == nil {
		return jsonx.IntegerNumber(v), nil
	}
	if v, err := javafmt.ParseLong(text); err == nil {
		return jsonx.LongNumber(v), nil
	}
	b, _ := new(big.Int).SetString(text, 10)
	return jsonx.BigIntegerNumber(b), nil
}

// Stringify is Jackson's JsonNode.asText() for a scalar: strings as is,
// numbers as Java prints them (Double.toString for decimals), booleans as
// "true"/"false", null as "null"; objects and arrays give "".
func Stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case jsonx.Number:
		return x.String()
	}
	return ""
}

// StringifyJSON is the Postgres JSON-column steps' convertJsonValuesToStrings:
// the text is read with Jackson, every non-null scalar inside objects and
// arrays is replaced by its Stringify text, and the tree is written back as
// compact JSON. A scalar document is written back unchanged.
func StringifyJSON(text string) (string, error) {
	v, err := ParseJackson(text)
	if err != nil {
		return "", err
	}
	stringifyScalars(v)
	var sb strings.Builder
	writeJackson(&sb, v)
	return sb.String(), nil
}

func stringifyScalars(v any) {
	switch x := v.(type) {
	case *jsonx.Object:
		for _, k := range x.Keys() {
			e, _ := x.Get(k)
			if isValueNode(e) {
				x.Set(k, Stringify(e))
			} else {
				stringifyScalars(e)
			}
		}
	case []any:
		for i, e := range x {
			if isValueNode(e) {
				x[i] = Stringify(e)
			} else {
				stringifyScalars(e)
			}
		}
	}
}

func isValueNode(v any) bool {
	switch v.(type) {
	case string, bool, jsonx.Number:
		return true
	}
	return false
}

// writeJackson writes compact JSON as Jackson's ObjectMapper does.
func writeJackson(sb *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		sb.WriteString("null")
	case string:
		writeJacksonString(sb, x)
	case bool:
		if x {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case jsonx.Number:
		if x.Kind() == jsonx.Double && (math.IsNaN(x.Float64()) || math.IsInf(x.Float64(), 0)) {
			writeJacksonString(sb, x.String())
		} else {
			sb.WriteString(x.String())
		}
	case *jsonx.Object:
		sb.WriteByte('{')
		for i, k := range x.Keys() {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJacksonString(sb, k)
			sb.WriteByte(':')
			e, _ := x.Get(k)
			writeJackson(sb, e)
		}
		sb.WriteByte('}')
	case []any:
		sb.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			writeJackson(sb, e)
		}
		sb.WriteByte(']')
	}
}

func writeJacksonString(sb *strings.Builder, s string) {
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\b':
			sb.WriteString(`\b`)
		case '\t':
			sb.WriteString(`\t`)
		case '\n':
			sb.WriteString(`\n`)
		case '\f':
			sb.WriteString(`\f`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(sb, `\u%04X`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
}
