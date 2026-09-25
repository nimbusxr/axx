package lint

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// JSON extraction for `type: jsonpath` rules.
//
// Files are read with a small strict JSON scanner that records the line and
// column of every value. It accepts what Jackson's default parser accepts: standard JSON, several root values in one file and
// duplicate keys; comments, single quotes, trailing commas, leading zeros,
// NaN and unescaped control characters are errors. A leading byte-order mark
// is skipped.
//
// Paths in a simple subset are matched against the scanned tree directly:
//
//	[$.]field(.field)*   where each field may carry [N] or [*]
//
// e.g. `order.id`, `payments[*].id`, `$.items[0].sku`. A path is absolute
// (the first field is a member of a root object), a plain field never
// matches array elements (`payments.id` does not look inside an array), an
// indexed field must be an array, and only scalar values (strings, numbers
// as written, true/false) are extracted; nulls, objects and arrays are not.
//
// Any other path (`$..id`, `$.*.id`, `$['a b']`, filters, slices,
// functions) is evaluated by the Jayway JsonPath port (compat/jsonx), and
// each result is located in the scanned tree by its normalized path so it
// still carries a line number. Results that cannot be located (function
// values) are reported without one.

type jsonKind byte

const (
	jsonObject jsonKind = iota + 1
	jsonArray
	jsonString
	jsonNumber
	jsonBool
	jsonNull
)

type jsonNode struct {
	kind      jsonKind
	text      string // scalars: decoded string, number literal, "true"/"false"
	line, col int
	keys      []string    // object member names
	kids      []*jsonNode // object member values or array elements
}

func (n *jsonNode) scalar() bool {
	return n.kind == jsonString || n.kind == jsonNumber || n.kind == jsonBool
}

// JSONError is a syntax error with its position.
type JSONError struct {
	Line, Column int
	Message      string
}

func (e *JSONError) Error() string {
	return fmt.Sprintf("invalid JSON at line %d, column %d: %s", e.Line, e.Column, e.Message)
}

type jsonScanner struct {
	s         string
	pos       int
	line, col int
	depth     int
}

const maxJSONDepth = 1000

// parseJSON scans every root value of s.
func parseJSON(s string) ([]*jsonNode, error) {
	sc := &jsonScanner{s: strings.TrimPrefix(s, "\ufeff"), line: 1, col: 1}
	var roots []*jsonNode
	for {
		if err := sc.skipSpace(); err != nil {
			return nil, err
		}
		if sc.pos >= len(sc.s) {
			return roots, nil
		}
		n, err := sc.value()
		if err != nil {
			return nil, err
		}
		roots = append(roots, n)
		if n.kind == jsonNumber && sc.pos < len(sc.s) && !strings.ContainsRune(" \t\r\n", rune(sc.s[sc.pos])) {
			return nil, sc.errorf("expected space separating root-level values")
		}
	}
}

func (sc *jsonScanner) errorf(format string, args ...any) error {
	return &JSONError{Line: sc.line, Column: sc.col, Message: fmt.Sprintf(format, args...)}
}

func (sc *jsonScanner) quoteChar() string {
	if sc.pos >= len(sc.s) {
		return "end of input"
	}
	r, _ := utf8.DecodeRuneInString(sc.s[sc.pos:])
	return strconv.Quote(string(r))
}

// advance moves over n bytes of the current line.
func (sc *jsonScanner) advance(n int) {
	for i := 0; i < n; i++ {
		if sc.s[sc.pos]&0xC0 != 0x80 {
			sc.col++
		}
		sc.pos++
	}
}

func (sc *jsonScanner) skipSpace() error {
	for sc.pos < len(sc.s) {
		switch sc.s[sc.pos] {
		case ' ', '\t':
			sc.pos++
			sc.col++
		case '\n':
			sc.pos++
			sc.line, sc.col = sc.line+1, 1
		case '\r': // like Jackson, CR and CRLF end a line too
			sc.pos++
			if sc.pos < len(sc.s) && sc.s[sc.pos] == '\n' {
				sc.pos++
			}
			sc.line, sc.col = sc.line+1, 1
		case '/', '#':
			return sc.errorf("comments are not allowed in JSON")
		default:
			if sc.s[sc.pos] < 0x20 {
				return sc.errorf("illegal character %s between tokens", sc.quoteChar())
			}
			return nil
		}
	}
	return nil
}

func (sc *jsonScanner) value() (*jsonNode, error) {
	if sc.pos >= len(sc.s) {
		return nil, sc.errorf("unexpected end of input, expected a value")
	}
	n := &jsonNode{line: sc.line, col: sc.col}
	switch c := sc.s[sc.pos]; {
	case c == '{':
		return n, sc.object(n)
	case c == '[':
		return n, sc.array(n)
	case c == '"':
		s, err := sc.str()
		n.kind, n.text = jsonString, s
		return n, err
	case c == '-' || c >= '0' && c <= '9':
		t, err := sc.number()
		n.kind, n.text = jsonNumber, t
		return n, err
	default:
		for _, lit := range []struct {
			word string
			kind jsonKind
		}{{"true", jsonBool}, {"false", jsonBool}, {"null", jsonNull}} {
			if strings.HasPrefix(sc.s[sc.pos:], lit.word) && !identByte(sc.s, sc.pos+len(lit.word)) {
				sc.advance(len(lit.word))
				n.kind, n.text = lit.kind, lit.word
				return n, nil
			}
		}
		end := sc.pos
		for end < len(sc.s) && identByte(sc.s, end) {
			end++
		}
		if end > sc.pos {
			return nil, sc.errorf("unrecognized token '%s', expected a value (string, number, object, array, true, false or null)", sc.s[sc.pos:end])
		}
		return nil, sc.errorf("unexpected character %s, expected a value", sc.quoteChar())
	}
}

// identByte reports whether s[i] continues a bare token such as "true".
func identByte(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	c := s[i]
	return c == '_' || c == '-' || c == '+' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func (sc *jsonScanner) enter() error {
	sc.depth++
	if sc.depth > maxJSONDepth {
		return sc.errorf("nesting deeper than %d levels", maxJSONDepth)
	}
	return nil
}

func (sc *jsonScanner) object(n *jsonNode) error {
	n.kind = jsonObject
	if err := sc.enter(); err != nil {
		return err
	}
	defer func() { sc.depth-- }()
	sc.advance(1)
	if err := sc.skipSpace(); err != nil {
		return err
	}
	if sc.pos < len(sc.s) && sc.s[sc.pos] == '}' {
		sc.advance(1)
		return nil
	}
	for {
		if sc.pos >= len(sc.s) {
			return sc.errorf("unexpected end of input in an object")
		}
		if sc.s[sc.pos] != '"' {
			return sc.errorf("unexpected character %s, expected a double-quoted field name", sc.quoteChar())
		}
		key, err := sc.str()
		if err != nil {
			return err
		}
		if err := sc.skipSpace(); err != nil {
			return err
		}
		if sc.pos >= len(sc.s) || sc.s[sc.pos] != ':' {
			return sc.errorf("unexpected character %s, expected ':' after a field name", sc.quoteChar())
		}
		sc.advance(1)
		if err := sc.skipSpace(); err != nil {
			return err
		}
		v, err := sc.value()
		if err != nil {
			return err
		}
		n.keys = append(n.keys, key)
		n.kids = append(n.kids, v)
		if err := sc.skipSpace(); err != nil {
			return err
		}
		if sc.pos >= len(sc.s) {
			return sc.errorf("unexpected end of input in an object")
		}
		switch sc.s[sc.pos] {
		case ',':
			sc.advance(1)
			if err := sc.skipSpace(); err != nil {
				return err
			}
		case '}':
			sc.advance(1)
			return nil
		default:
			return sc.errorf("unexpected character %s, expected ',' or '}'", sc.quoteChar())
		}
	}
}

func (sc *jsonScanner) array(n *jsonNode) error {
	n.kind = jsonArray
	if err := sc.enter(); err != nil {
		return err
	}
	defer func() { sc.depth-- }()
	sc.advance(1)
	if err := sc.skipSpace(); err != nil {
		return err
	}
	if sc.pos < len(sc.s) && sc.s[sc.pos] == ']' {
		sc.advance(1)
		return nil
	}
	for {
		v, err := sc.value()
		if err != nil {
			return err
		}
		n.kids = append(n.kids, v)
		if err := sc.skipSpace(); err != nil {
			return err
		}
		if sc.pos >= len(sc.s) {
			return sc.errorf("unexpected end of input in an array")
		}
		switch sc.s[sc.pos] {
		case ',':
			sc.advance(1)
			if err := sc.skipSpace(); err != nil {
				return err
			}
		case ']':
			sc.advance(1)
			return nil
		default:
			return sc.errorf("unexpected character %s, expected ',' or ']'", sc.quoteChar())
		}
	}
}

func (sc *jsonScanner) str() (string, error) {
	sc.advance(1) // opening quote
	var b strings.Builder
	for {
		if sc.pos >= len(sc.s) {
			return "", sc.errorf("unexpected end of input in a string")
		}
		c := sc.s[sc.pos]
		switch {
		case c == '"':
			sc.advance(1)
			return b.String(), nil
		case c < 0x20:
			return "", sc.errorf("unescaped control character %s in a string", sc.quoteChar())
		case c == '\\':
			if sc.pos+1 >= len(sc.s) {
				return "", sc.errorf("unexpected end of input in a string")
			}
			e := sc.s[sc.pos+1]
			if r, ok := simpleEscapes[e]; ok {
				b.WriteByte(r)
				sc.advance(2)
				continue
			}
			if e != 'u' {
				return "", sc.errorf("unrecognized character escape '\\%c'", e)
			}
			r, err := sc.hex4(sc.pos + 2)
			if err != nil {
				return "", err
			}
			sc.advance(6)
			if utf16.IsSurrogate(r) && strings.HasPrefix(sc.s[sc.pos:], `\u`) {
				if r2, err := sc.hex4(sc.pos + 2); err == nil {
					if dec := utf16.DecodeRune(r, r2); dec != utf8.RuneError {
						sc.advance(6)
						r = dec
					}
				}
			}
			b.WriteRune(r) // a lone surrogate becomes U+FFFD
		default:
			start := sc.pos
			for sc.pos < len(sc.s) && sc.s[sc.pos] != '"' && sc.s[sc.pos] != '\\' && sc.s[sc.pos] >= 0x20 {
				sc.pos++
			}
			chunk := sc.s[start:sc.pos]
			sc.col += utf8.RuneCountInString(chunk)
			b.WriteString(strings.ToValidUTF8(chunk, "\uFFFD"))
		}
	}
}

var simpleEscapes = map[byte]byte{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}

func (sc *jsonScanner) hex4(at int) (rune, error) {
	if at+4 > len(sc.s) {
		return 0, sc.errorf("unexpected end of input in a \\u escape")
	}
	v, err := strconv.ParseUint(sc.s[at:at+4], 16, 32)
	if err != nil {
		return 0, sc.errorf("invalid \\u escape %q", sc.s[at:at+4])
	}
	return rune(v), nil
}

var numberRE = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?`)

func (sc *jsonScanner) number() (string, error) {
	lit := numberRE.FindString(sc.s[sc.pos:])
	if lit == "" || identByte(sc.s, sc.pos+len(lit)) { // "-", "01", "1.", "1e", "1.5.3", "12ab"
		tok := sc.pos
		for tok < len(sc.s) && identByte(sc.s, tok) {
			tok++
		}
		if tok == sc.pos {
			tok = sc.pos + 1
		}
		return "", sc.errorf("invalid number '%s'", sc.s[sc.pos:tok])
	}
	sc.advance(len(lit))
	return lit, nil
}

// ---- paths ----

// jsonSegment is one field of a subset path: a name and an optional index.
type jsonSegment struct {
	name  string
	index int // indexNone, indexAny or >= 0
}

const (
	indexNone = -1
	indexAny  = -2
)

// jsonPath is a compiled jsonPath: a subset path or a Jayway path.
type jsonPath struct {
	raw      string
	segments []jsonSegment // subset form; nil when jayway is used
	jayway   *jsonx.Path
}

var subsetSegment = regexp.MustCompile(`^([^.\[\]*?@()'",]+)(?:\[(\*|[0-9]+)\])?$`)

// compileJSONPath recognizes the simple subset and hands anything else to
// the Jayway evaluator.
func compileJSONPath(path string) (*jsonPath, error) {
	jp := &jsonPath{raw: path}
	body := strings.TrimPrefix(path, "$.")
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("jsonPath must name at least one field: %q", path)
	}
	subset := body != "$"
	for _, raw := range strings.Split(body, ".") {
		m := subsetSegment.FindStringSubmatch(raw)
		if m == nil || strings.TrimSpace(m[1]) == "" {
			subset = false
			break
		}
		seg := jsonSegment{name: m[1], index: indexNone}
		switch m[2] {
		case "":
		case "*":
			seg.index = indexAny
		default:
			n, err := strconv.Atoi(m[2])
			if err != nil || n > 1<<31-1 {
				return nil, fmt.Errorf("bad index %q in %s", m[2], path)
			}
			seg.index = n
		}
		jp.segments = append(jp.segments, seg)
	}
	if subset {
		return jp, nil
	}
	jp.segments = nil
	p, err := jsonx.Compile(path)
	if err != nil {
		return nil, err
	}
	jp.jayway = p
	return jp, nil
}

// jsonMatch is an extracted value.
type jsonMatch struct {
	value     string
	line, col int
}

// extract returns the scalar values path selects in content.
func (jp *jsonPath) extract(content string) ([]jsonMatch, error) {
	roots, err := parseJSON(content)
	if err != nil {
		return nil, err
	}
	var out []jsonMatch
	if jp.jayway == nil {
		for _, r := range roots {
			jp.walk(r, nil, &out)
		}
		return out, nil
	}
	if len(roots) == 0 || roots[0].kind != jsonObject && roots[0].kind != jsonArray {
		return nil, nil
	}
	doc, err := jsonx.Parse(strings.TrimPrefix(content, "\ufeff"))
	if err != nil {
		return nil, err
	}
	paths, values, err := jsonx.ReadPaths(doc, jp.raw)
	if err != nil {
		return nil, err
	}
	for i, p := range paths {
		if n := locate(roots[0], p); n != nil {
			if n.scalar() {
				out = append(out, jsonMatch{value: n.text, line: n.line, col: n.col})
			}
			continue
		}
		switch v := values[i].(type) {
		case string:
			out = append(out, jsonMatch{value: v})
		case bool:
			out = append(out, jsonMatch{value: strconv.FormatBool(v)})
		case jsonx.Number:
			out = append(out, jsonMatch{value: v.Text()})
		}
	}
	return out, nil
}

// pathElem is a field name or an array index on the way to a value.
type pathElem struct {
	field string
	index int // -1 for fields
}

func (jp *jsonPath) walk(n *jsonNode, path []pathElem, out *[]jsonMatch) {
	switch n.kind {
	case jsonObject:
		for i, k := range n.keys {
			jp.walk(n.kids[i], append(path, pathElem{field: k, index: -1}), out)
		}
	case jsonArray:
		for i, kid := range n.kids {
			jp.walk(kid, append(path, pathElem{index: i}), out)
		}
	case jsonNull:
	default:
		if jp.matches(path) {
			*out = append(*out, jsonMatch{value: n.text, line: n.line, col: n.col})
		}
	}
}

// matches is JsonPathExtractor.matches: the live path against the segments.
func (jp *jsonPath) matches(path []pathElem) bool {
	p := 0
	for i := 0; i < len(path); i++ {
		el := path[i]
		if el.index >= 0 {
			return false // an index with no field before it (e.g. a root array)
		}
		if p >= len(jp.segments) || jp.segments[p].name != el.field {
			return false
		}
		idx := -1
		if i+1 < len(path) && path[i+1].index >= 0 {
			idx = path[i+1].index
		}
		seg := jp.segments[p]
		if seg.index == indexNone {
			if idx >= 0 {
				return false // a plain field in the pattern, an array element in the document
			}
		} else {
			if idx < 0 || seg.index != indexAny && seg.index != idx {
				return false
			}
			i++
		}
		p++
	}
	return p == len(jp.segments)
}

// locate finds the node at a normalized Jayway path ("$['a'][0]"). Keys are
// not escaped in that form, so candidate keys are tried against the text.
func locate(n *jsonNode, path string) *jsonNode {
	if !strings.HasPrefix(path, "$") {
		return nil
	}
	return locateFrom(n, path[1:])
}

func locateFrom(n *jsonNode, rest string) *jsonNode {
	if rest == "" {
		return n
	}
	switch {
	case n.kind == jsonObject && strings.HasPrefix(rest, "['"):
		for i := len(n.keys) - 1; i >= 0; i-- { // the last duplicate key wins, as in a map
			tok := "['" + n.keys[i] + "']"
			if strings.HasPrefix(rest, tok) {
				if found := locateFrom(n.kids[i], rest[len(tok):]); found != nil {
					return found
				}
			}
		}
	case n.kind == jsonArray && strings.HasPrefix(rest, "["):
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return nil
		}
		i, err := strconv.Atoi(rest[1:end])
		if err != nil || i < 0 || i >= len(n.kids) {
			return nil
		}
		return locateFrom(n.kids[i], rest[end+1:])
	}
	return nil
}
