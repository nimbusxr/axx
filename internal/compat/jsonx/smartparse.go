package jsonx

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
)

// This file ports json-smart 2.5.2's JSONParserString/JSONParserMemory/
// JSONParserBase in the permissive mode Jayway uses (JSONParser.MODE_PERMISSIVE,
// i.e. every leniency flag set): single-quoted and unquoted strings, NaN,
// leading zeros, redundant commas, ignored control characters, trailing data
// after the first value, Integer storage for small integers and BigDecimal
// for long decimal literals. Positions in error messages are UTF-16 indexes,
// as in Java.

const eoi = 26 // json-smart's end-of-input marker (also the literal U+001A)

const maxDepth = 400

// stop tables: characters (below '~') that end an unquoted token.
var (
	stopKey   = stopTable(':', eoi)
	stopValue = stopTable(',', '}', eoi)
	stopArray = stopTable(',', ']', eoi)
	stopX     = stopTable(eoi)
)

func stopTable(chars ...uint16) *[126]bool {
	var t [126]bool
	for _, c := range chars {
		t[c] = true
	}
	return &t
}

// smartParseError is json-smart's ParseException.
type smartParseError struct {
	pos     int
	errType int
	obj     string
}

const (
	errUnexpectedChar    = 0
	errUnexpectedToken   = 1
	errUnexpectedEOF     = 3
	errUnexpectedUnicode = 4
	errUnexpectedDepth   = 7
)

func (e *smartParseError) message() string {
	p := strconv.Itoa(e.pos)
	switch e.errType {
	case errUnexpectedChar:
		return "Unexpected character (" + e.obj + ") at position " + p + "."
	case errUnexpectedToken:
		return "Unexpected token " + e.obj + " at position " + p + "."
	case errUnexpectedEOF:
		return "Unexpected End Of File position " + p + ": " + e.obj
	case errUnexpectedUnicode:
		return "Unexpected unicode escape sequence " + e.obj + " at position " + p + "."
	case errUnexpectedDepth:
		return "Malicious payload, having non natural depths, parsing stoped on " + e.obj + " at position " + p + "."
	}
	return "Unkown error at position " + p + "." //nolint:misspell // json-smart's message, typo included
}

type smartParser struct {
	in    jstr
	pos   int
	c     uint16
	xs    jstr
	sb    jstr
	depth int
}

func (p *smartParser) fail(pos, errType int, obj string) {
	panic(&smartParseError{pos: pos, errType: errType, obj: obj})
}

func charObj(c uint16) string { return string(utf16.Decode([]uint16{c})) }

// parseSmart parses text with json-smart in permissive mode, returning the
// value or a *smartParseError.
func parseSmart(text string) (v any, perr *smartParseError) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*smartParseError)
			if !ok {
				panic(r)
			}
			perr = e
		}
	}()
	p := &smartParser{in: toJ(text), pos: -1}
	p.read()
	return p.readFirst(), nil
}

func (p *smartParser) read() {
	p.pos++
	if p.pos >= len(p.in) {
		p.c = eoi
	} else {
		p.c = p.in[p.pos]
	}
}

func (p *smartParser) readNoEnd() {
	p.pos++
	if p.pos >= len(p.in) {
		p.c = eoi
		p.fail(p.pos-1, errUnexpectedEOF, "EOF")
	}
	p.c = p.in[p.pos]
}

func (p *smartParser) readFirst() any {
	for {
		switch p.c {
		case '\t', '\n', '\r', ' ':
			p.read()
		case '"', '\'':
			p.readString()
			return p.xs.String()
		case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			return p.readNumber(stopX)
		case ':', ']', '}':
			p.fail(p.pos, errUnexpectedChar, charObj(p.c))
		case 'N':
			p.readNQString(stopX)
			if p.xs.String() == "NaN" {
				return FloatNumber(float32(math.NaN())).withText("NaN")
			}
			return p.xs.String()
		case '[':
			return p.readArray()
		case 'f':
			p.readNQString(stopX)
			if p.xs.String() == "false" {
				return false
			}
			return p.xs.String()
		case 'n':
			p.readNQString(stopX)
			if p.xs.String() == "null" {
				return nil
			}
			return p.xs.String()
		case 't':
			p.readNQString(stopX)
			if p.xs.String() == "true" {
				return true
			}
			return p.xs.String()
		case '{':
			return p.readObject()
		default:
			p.readNQString(stopX)
			return p.xs.String()
		}
	}
}

func (p *smartParser) readMain(stop *[126]bool) any {
	for {
		switch p.c {
		case '\t', '\n', '\r', ' ':
			p.read()
		case '"', '\'':
			p.readString()
			return p.xs.String()
		case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			return p.readNumber(stop)
		case ':', ']', '}':
			p.fail(p.pos, errUnexpectedChar, charObj(p.c))
		case 'N':
			p.readNQString(stop)
			if p.xs.String() == "NaN" {
				return FloatNumber(float32(math.NaN())).withText("NaN")
			}
			return p.xs.String()
		case '[':
			return p.readArray()
		case 'f':
			p.readNQString(stop)
			if p.xs.String() == "false" {
				return false
			}
			return p.xs.String()
		case 'n':
			p.readNQString(stop)
			if p.xs.String() == "null" {
				return nil
			}
			return p.xs.String()
		case 't':
			p.readNQString(stop)
			if p.xs.String() == "true" {
				return true
			}
			return p.xs.String()
		case '{':
			return p.readObject()
		default:
			p.readNQString(stop)
			return p.xs.String()
		}
	}
}

func (p *smartParser) readArray() any {
	p.depth++
	if p.depth > maxDepth {
		p.fail(p.pos, errUnexpectedDepth, charObj(p.c))
	}
	arr := &Array{}
	p.read()
	for {
		switch p.c {
		case '\t', '\n', '\r', ' ':
			p.read()
		case eoi:
			p.fail(p.pos-1, errUnexpectedEOF, "EOF")
		case ',':
			p.read()
		case ':', '}':
			p.fail(p.pos, errUnexpectedChar, charObj(p.c))
		case ']':
			p.depth--
			p.read()
			return arr
		default:
			arr.items = append(arr.items, p.readMain(stopArray))
		}
	}
}

func (p *smartParser) readObject() any {
	p.depth++
	if p.depth > maxDepth {
		p.fail(p.pos, errUnexpectedDepth, charObj(p.c))
	}
	obj := NewObject()
	for {
		p.read()
		switch p.c {
		case '\t', '\n', '\r', ' ':
		case ',':
		case ':', '[', ']', '{':
			p.fail(p.pos, errUnexpectedChar, charObj(p.c))
		case '}':
			p.depth--
			p.read()
			return obj
		default:
			if p.c != '"' && p.c != '\'' {
				p.readNQString(stopKey)
			} else {
				p.readString()
			}
			key := p.xs.String()
			p.skipSpace()
			if p.c != ':' {
				if p.c == eoi {
					p.fail(p.pos-1, errUnexpectedEOF, "null")
				}
				p.fail(p.pos-1, errUnexpectedChar, charObj(p.c))
			}
			p.readNoEnd()
			value := p.readMain(stopValue)
			obj.Set(key, value)
			p.skipSpace()
			if p.c == '}' {
				p.depth--
				p.read()
				return obj
			}
			if p.c == eoi {
				p.fail(p.pos-1, errUnexpectedEOF, "null")
			}
			if p.c != ',' {
				p.fail(p.pos-1, errUnexpectedToken, charObj(p.c))
			}
		}
	}
}

func (p *smartParser) readString() {
	sep := p.c
	end := -1
	for i := p.pos + 1; i < len(p.in); i++ {
		if p.in[i] == sep {
			end = i
			break
		}
	}
	if end == -1 {
		p.fail(len(p.in), errUnexpectedEOF, "null")
	}
	p.xs = p.in[p.pos+1 : end]
	hasEscape := false
	for _, c := range p.xs {
		if c == '\\' {
			hasEscape = true
			break
		}
	}
	if !hasEscape {
		p.pos = end
		p.read()
		return
	}
	p.sb = p.sb[:0]
	p.readString2()
}

func (p *smartParser) readString2() {
	sep := p.c
	for {
		p.read()
		switch {
		case p.c == eoi:
			p.fail(p.pos-1, errUnexpectedEOF, "null")
		case p.c < 32 || p.c == 127:
			// Control characters and DEL are dropped (IGNORE_CONTROL_CHAR).
		case p.c == '"' || p.c == '\'':
			if p.c == sep {
				p.read()
				p.xs = append(jstr(nil), p.sb...)
				return
			}
			p.sb = append(p.sb, p.c)
		case p.c == '\\':
			p.read()
			switch p.c {
			case '"', '\'', '/', '\\':
				p.sb = append(p.sb, p.c)
			case 'b':
				p.sb = append(p.sb, '\b')
			case 'f':
				p.sb = append(p.sb, '\f')
			case 'n':
				p.sb = append(p.sb, '\n')
			case 'r':
				p.sb = append(p.sb, '\r')
			case 't':
				p.sb = append(p.sb, '\t')
			case 'u':
				p.sb = append(p.sb, p.readUnicode(4))
			case 'x':
				p.sb = append(p.sb, p.readUnicode(2))
			default:
				// Unknown escapes are dropped entirely.
			}
		default:
			p.sb = append(p.sb, p.c)
		}
	}
}

func (p *smartParser) readUnicode(n int) uint16 {
	value := 0
	for i := 0; i < n; i++ {
		value *= 16
		p.read()
		switch {
		case p.c >= '0' && p.c <= '9':
			value += int(p.c - '0')
		case p.c >= 'A' && p.c <= 'F':
			value += int(p.c-'A') + 10
		case p.c >= 'a' && p.c <= 'f':
			value += int(p.c-'a') + 10
		case p.c == eoi:
			p.fail(p.pos, errUnexpectedEOF, "EOF")
		default:
			p.fail(p.pos, errUnexpectedUnicode, charObj(p.c))
		}
	}
	return uint16(value)
}

func (p *smartParser) readNQString(stop *[126]bool) {
	start := p.pos
	p.skipNQString(stop)
	p.extractStringTrim(start, p.pos)
}

func (p *smartParser) skipNQString(stop *[126]bool) {
	for p.c != eoi && (p.c >= '~' || !stop[p.c]) {
		p.read()
	}
}

func (p *smartParser) skipSpace() {
	for p.c <= ' ' && p.c != eoi {
		p.read()
	}
}

func (p *smartParser) skipDigits() {
	for p.c >= '0' && p.c <= '9' {
		p.read()
	}
}

func (p *smartParser) extractStringTrim(start, stop int) {
	for start < stop-1 && isJavaWhitespace(p.in[start]) {
		start++
	}
	for stop-1 > start && isJavaWhitespace(p.in[stop-1]) {
		stop--
	}
	p.xs = p.in[start:stop]
}

// continuesToken reports whether c continues an unquoted token after what
// looked like a number, turning the whole token into a string.
func continuesToken(c uint16, stop *[126]bool) bool {
	return c < '~' && !stop[c] && c != eoi
}

func (p *smartParser) readNumber(stop *[126]bool) any {
	start := p.pos
	p.read()
	p.skipDigits()
	if p.c != '.' && p.c != 'E' && p.c != 'e' {
		p.skipSpace()
		if continuesToken(p.c, stop) {
			p.skipNQString(stop)
			p.extractStringTrim(start, p.pos)
			return p.xs.String()
		}
		p.extractStringTrim(start, p.pos)
		return p.parseNumber(p.xs.String())
	}
	if p.c == '.' {
		p.read()
		p.skipDigits()
	}
	if p.c != 'E' && p.c != 'e' {
		p.skipSpace()
		if continuesToken(p.c, stop) {
			p.skipNQString(stop)
			p.extractStringTrim(start, p.pos)
			return p.xs.String()
		}
		p.extractStringTrim(start, p.pos)
		return p.extractFloat()
	}
	p.read()
	if p.c == '+' || p.c == '-' || p.c >= '0' && p.c <= '9' {
		p.read()
		p.skipDigits()
		p.skipSpace()
		if continuesToken(p.c, stop) {
			p.skipNQString(stop)
			p.extractStringTrim(start, p.pos)
			return p.xs.String()
		}
		p.extractStringTrim(start, p.pos)
		return p.extractFloat()
	}
	p.skipNQString(stop)
	p.extractStringTrim(start, p.pos)
	return p.xs.String()
}

func (p *smartParser) extractFloat() any {
	s := p.xs.String()
	if len(p.xs) > 18 {
		d, err := javafmt.ParseBigDecimal(s)
		if err != nil {
			p.fail(p.pos, errUnexpectedToken, s)
		}
		return BigDecimalNumber(d).withText(s)
	}
	f, err := javafmt.ParseDouble(s)
	if err != nil {
		p.fail(p.pos, errUnexpectedToken, s)
	}
	return DoubleNumber(f).withText(s)
}

// parseNumber is JSONParserBase.parseNumber: Integer when the value fits,
// Long, or BigInteger past 19 digits or on overflow.
func (p *smartParser) parseNumber(s string) any {
	if strings.TrimLeft(s, "-0123456789") != "" {
		// Unreachable for input produced by readNumber.
		p.fail(p.pos, errUnexpectedToken, s)
	}
	pos := 0
	l := len(s)
	limit := 19
	neg := false
	if s[0] == '-' {
		pos++
		limit++
		neg = true
	}
	mustCheck := false
	if l < limit {
		limit = l
	} else {
		if l > limit {
			return bigIntegerFromText(s)
		}
		limit = l - 1
		mustCheck = true
	}
	var r int64
	for pos < limit {
		r = r*10 + int64('0'-int(s[pos]))
		pos++
	}
	if mustCheck {
		var isBig bool
		switch {
		case r > -922337203685477580:
			isBig = false
		case r < -922337203685477580:
			isBig = true
		case neg:
			isBig = s[pos] > '8'
		default:
			isBig = s[pos] > '7'
		}
		if isBig {
			return bigIntegerFromText(s)
		}
		r = r*10 + int64('0'-int(s[pos]))
	}
	if neg {
		if r >= math.MinInt32 {
			return IntegerNumber(int32(r)).withText(s)
		}
		return LongNumber(r).withText(s)
	}
	r = -r
	if r <= math.MaxInt32 {
		return IntegerNumber(int32(r)).withText(s)
	}
	return LongNumber(r).withText(s)
}

func bigIntegerFromText(s string) any {
	b, _ := javafmt.ParseBigInteger(s)
	return BigIntegerNumber(b).withText(s)
}
