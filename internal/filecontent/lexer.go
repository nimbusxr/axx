package filecontent

import (
	"bytes"
	"strconv"
)

// A lexer reads the operators of a PDF content stream, each with its
// operands: numbers (float64), strings (pdfString), names (pdfName),
// arrays ([]any), and dictionaries, booleans and null (which text does not
// need, read as nil). Inline images are skipped.
type lexer struct {
	b []byte
	i int
}

type (
	pdfString string
	pdfName   string
)

func newLexer(b []byte) *lexer { return &lexer{b: b} }

// next returns the next operator and its operands; ok is false at the end.
func (l *lexer) next() (op string, args []any, ok bool) {
	for {
		v, isOp, more := l.token()
		if !more {
			return "", nil, false
		}
		if !isOp {
			args = append(args, v)
			continue
		}
		op = v.(string)
		switch op {
		case "BI":
			l.skipInlineImage()
			args = nil
			continue
		case "true", "false", "null":
			args = append(args, nil)
			continue
		}
		return op, args, true
	}
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '\f' || c == 0
}

func isDelim(c byte) bool {
	return bytes.IndexByte([]byte("()<>[]{}/%"), c) >= 0
}

// token reads an operand, or an operator (isOp), skipping space and
// comments.
func (l *lexer) token() (v any, isOp, more bool) {
	for l.i < len(l.b) {
		c := l.b[l.i]
		switch {
		case isSpace(c):
			l.i++
		case c == '%':
			for l.i < len(l.b) && l.b[l.i] != '\n' && l.b[l.i] != '\r' {
				l.i++
			}
		case c == '(':
			l.i++
			return l.literal(), false, true
		case c == '<' && l.i+1 < len(l.b) && l.b[l.i+1] == '<':
			l.i += 2
			l.skipDict()
			return nil, false, true
		case c == '<':
			l.i++
			return l.hex(), false, true
		case c == '/':
			l.i++
			return pdfName(l.name()), false, true
		case c == '[':
			l.i++
			return l.array(), false, true
		case c == ']' || c == '>' || c == ')' || c == '{' || c == '}':
			l.i++ // stray; ignore it
		default:
			start := l.i
			for l.i < len(l.b) && !isSpace(l.b[l.i]) && !isDelim(l.b[l.i]) {
				l.i++
			}
			word := string(l.b[start:l.i])
			if f, err := strconv.ParseFloat(word, 64); err == nil {
				return f, false, true
			}
			return word, true, true
		}
	}
	return nil, false, false
}

// array reads the operands up to the closing bracket.
func (l *lexer) array() []any {
	var out []any
	for l.i < len(l.b) {
		for l.i < len(l.b) && isSpace(l.b[l.i]) {
			l.i++
		}
		if l.i < len(l.b) && l.b[l.i] == ']' {
			l.i++
			return out
		}
		v, isOp, more := l.token()
		if !more {
			break
		}
		if !isOp {
			out = append(out, v)
		}
	}
	return out
}

// skipDict skips a dictionary, nested ones and strings included.
func (l *lexer) skipDict() {
	depth := 1
	for l.i < len(l.b) && depth > 0 {
		switch c := l.b[l.i]; {
		case c == '(':
			l.i++
			l.literal()
		case c == '<' && l.i+1 < len(l.b) && l.b[l.i+1] == '<':
			depth++
			l.i += 2
		case c == '>' && l.i+1 < len(l.b) && l.b[l.i+1] == '>':
			depth--
			l.i += 2
		case c == '<':
			l.i++
			l.hex()
		default:
			l.i++
		}
	}
}

func (l *lexer) name() string {
	var b []byte
	for l.i < len(l.b) && !isSpace(l.b[l.i]) && !isDelim(l.b[l.i]) {
		c := l.b[l.i]
		if c == '#' && l.i+2 < len(l.b) {
			if n, err := strconv.ParseUint(string(l.b[l.i+1:l.i+3]), 16, 8); err == nil {
				b = append(b, byte(n))
				l.i += 3
				continue
			}
		}
		b = append(b, c)
		l.i++
	}
	return string(b)
}

// literal reads a (string) after its opening parenthesis.
func (l *lexer) literal() pdfString {
	var b []byte
	depth := 1
	for l.i < len(l.b) {
		c := l.b[l.i]
		l.i++
		switch c {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return pdfString(b)
			}
		case '\\':
			if l.i >= len(l.b) {
				continue
			}
			e := l.b[l.i]
			l.i++
			switch e {
			case 'n':
				c = '\n'
			case 'r':
				c = '\r'
			case 't':
				c = '\t'
			case 'b':
				c = '\b'
			case 'f':
				c = '\f'
			case '\r':
				if l.i < len(l.b) && l.b[l.i] == '\n' {
					l.i++
				}
				continue
			case '\n':
				continue
			default:
				if e >= '0' && e <= '7' {
					n := int(e - '0')
					for k := 0; k < 2 && l.i < len(l.b) && l.b[l.i] >= '0' && l.b[l.i] <= '7'; k++ {
						n = n*8 + int(l.b[l.i]-'0')
						l.i++
					}
					c = byte(n)
				} else {
					c = e
				}
			}
		}
		b = append(b, c)
	}
	return pdfString(b)
}

// hex reads a <hex string> after its opening bracket.
func (l *lexer) hex() pdfString {
	var b []byte
	var hi byte
	half := false
	for l.i < len(l.b) {
		c := l.b[l.i]
		l.i++
		if c == '>' {
			break
		}
		var n byte
		switch {
		case c >= '0' && c <= '9':
			n = c - '0'
		case c >= 'a' && c <= 'f':
			n = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			n = c - 'A' + 10
		default:
			continue
		}
		if half {
			b = append(b, hi<<4|n)
		} else {
			hi = n
		}
		half = !half
	}
	if half {
		b = append(b, hi<<4)
	}
	return pdfString(b)
}

// skipInlineImage skips an inline image's parameters and data, up to its
// EI operator.
func (l *lexer) skipInlineImage() {
	for {
		v, isOp, more := l.token()
		if !more {
			return
		}
		if isOp && v == "ID" {
			break
		}
	}
	l.i++ // the single space after ID
	for l.i+2 <= len(l.b) {
		if l.b[l.i] == 'E' && l.b[l.i+1] == 'I' && isSpace(l.b[l.i-1]) &&
			(l.i+2 == len(l.b) || isSpace(l.b[l.i+2])) {
			l.i += 2
			return
		}
		l.i++
	}
	l.i = len(l.b)
}
