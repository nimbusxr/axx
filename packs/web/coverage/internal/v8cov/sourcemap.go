package v8cov

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Source maps (v3), decoded as @jridgewell/trace-mapping 0.3.31 decodes
// them, with the lookups v8-to-istanbul uses. Columns are UTF-16 code
// units, as V8's.

// SourceMap is a decoded source map.
type SourceMap struct {
	Version        int       `json:"version"`
	File           string    `json:"file"`
	SourceRoot     string    `json:"sourceRoot"`
	Sources        []string  `json:"sources"`
	SourcesContent []*string `json:"sourcesContent"`
	Mappings       string    `json:"mappings"`

	resolved  []string
	decoded   [][]segment   // by generated line
	bySources [][][]segment // [source][original line] = {srcCol, genLine, genCol}
}

// segment is [genCol] or [genCol, source, line, col] (+ name); in
// bySources, [srcCol, genLine, genCol].
type segment []int

const (
	biasGLB = iota // greatest lower bound
	biasLUB        // least upper bound
)

// ParseSourceMap decodes a source map's JSON.
func ParseSourceMap(b []byte) (*SourceMap, error) {
	// A map may start with a line that keeps it from running as a script.
	b = bytes.TrimPrefix(b, []byte(")]}'"))
	var raw struct {
		Sections json.RawMessage `json:"sections"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if raw.Sections != nil {
		return nil, errors.New("an indexed source map (with sections)")
	}
	var m SourceMap
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Version != 3 {
		return nil, fmt.Errorf("a source map of version %d, not 3", m.Version)
	}
	if err := m.decode(); err != nil {
		return nil, err
	}
	prefix := ""
	if m.SourceRoot != "" {
		prefix = m.SourceRoot + "/"
	}
	for _, s := range m.Sources {
		m.resolved = append(m.resolved, resolveURI(prefix+s))
	}
	return &m, nil
}

var b64 = func() [256]int {
	var t [256]int
	for i := range t {
		t[i] = -1
	}
	for i, c := range "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/" {
		t[c] = i
	}
	return t
}()

func (m *SourceMap) decode() error {
	var src, srcLine, srcCol, name int
	line := []segment{}
	s := m.Mappings
	for i := 0; i <= len(s); {
		if i == len(s) || s[i] == ';' {
			m.decoded = append(m.decoded, sortSegments(line))
			line = []segment{}
			i++
			if i > len(s) {
				break
			}
			continue
		}
		if s[i] == ',' {
			i++
			continue
		}
		// One segment: 1, 4 or 5 VLQ fields.
		var fields []int
		for i < len(s) && s[i] != ',' && s[i] != ';' {
			v, n, err := vlq(s[i:])
			if err != nil {
				return err
			}
			fields = append(fields, v)
			i += n
		}
		// The generated column is relative to the segment before, in the
		// order they are written.
		genCol := fields[0]
		if len(line) > 0 {
			genCol += line[len(line)-1][0]
		}
		seg := segment{genCol}
		if len(fields) >= 4 {
			src += fields[1]
			srcLine += fields[2]
			srcCol += fields[3]
			seg = append(seg, src, srcLine, srcCol)
			if len(fields) >= 5 {
				name += fields[4]
				seg = append(seg, name)
			}
		}
		line = append(line, seg)
	}
	return nil
}

// sortSegments sorts a line's segments by column, keeping the order of
// those on the same column, as trace-mapping does for unsorted lines.
func sortSegments(line []segment) []segment {
	sort.SliceStable(line, func(i, j int) bool { return line[i][0] < line[j][0] })
	return line
}

func vlq(s string) (value, n int, err error) {
	shift, result := 0, 0
	for {
		if n >= len(s) {
			return 0, n, errors.New("the mappings end in the middle of a number")
		}
		d := b64[s[n]]
		if d < 0 {
			return 0, n, fmt.Errorf("the mappings have a %q", s[n])
		}
		n++
		result += (d & 31) << shift
		if d&32 == 0 {
			break
		}
		shift += 5
		if shift > 60 {
			return 0, n, errors.New("the mappings have a number too large")
		}
	}
	if result&1 == 1 {
		return -(result >> 1), n, nil
	}
	return result >> 1, n, nil
}

// omapping is an original position; ok false is trace-mapping's nulls.
type omapping struct {
	source       string
	line, column int
	ok           bool
}

type gmapping struct {
	line, column int
	ok           bool
}

// binarySearch returns the index of a segment at column needle (found), or
// the last one before it.
func binarySearch(h []segment, needle int) (int, bool) {
	low, high := 0, len(h)-1
	for low <= high {
		mid := low + (high-low)>>1
		cmp := h[mid][0] - needle
		if cmp == 0 {
			return mid, true
		}
		if cmp < 0 {
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return low - 1, false
}

func traceSegment(h []segment, column, bias int) int {
	i, found := binarySearch(h, column)
	if found {
		if bias == biasLUB {
			for i+1 < len(h) && h[i+1][0] == column {
				i++
			}
		} else {
			for i-1 >= 0 && h[i-1][0] == column {
				i--
			}
		}
	} else if bias == biasLUB {
		i++
	}
	if i == -1 || i == len(h) {
		return -1
	}
	return i
}

// originalPositionFor maps a generated position (1-based line).
func (m *SourceMap) originalPositionFor(line, column, bias int) omapping {
	line--
	if line < 0 || column < 0 || line >= len(m.decoded) {
		return omapping{}
	}
	segs := m.decoded[line]
	i := traceSegment(segs, column, bias)
	if i == -1 || len(segs[i]) == 1 {
		return omapping{}
	}
	s := segs[i]
	if s[1] < 0 || s[1] >= len(m.resolved) {
		return omapping{}
	}
	return omapping{source: m.resolved[s[1]], line: s[2] + 1, column: s[3], ok: true}
}

// generatedPositionFor maps an original position (1-based line) back.
func (m *SourceMap) generatedPositionFor(source string, line, column, bias int) gmapping {
	line--
	idx := -1
	for i, s := range m.Sources {
		if s == source {
			idx = i
			break
		}
	}
	if idx == -1 {
		for i, s := range m.resolved {
			if s == source {
				idx = i
				break
			}
		}
	}
	if idx == -1 || line < 0 || column < 0 {
		return gmapping{}
	}
	if m.bySources == nil {
		m.bySources = make([][][]segment, len(m.Sources))
		for gl, segs := range m.decoded {
			for _, s := range segs {
				if len(s) == 1 {
					continue
				}
				src, sl := s[1], s[2]
				if src < 0 || src >= len(m.bySources) || sl < 0 {
					continue
				}
				for len(m.bySources[src]) <= sl {
					m.bySources[src] = append(m.bySources[src], nil)
				}
				m.bySources[src][sl] = append(m.bySources[src][sl], segment{s[3], gl, s[0]})
			}
		}
		for _, lines := range m.bySources {
			for _, l := range lines {
				sortSegments(l)
			}
		}
	}
	if line >= len(m.bySources[idx]) || m.bySources[idx][line] == nil {
		return gmapping{}
	}
	segs := m.bySources[idx][line]
	i := traceSegment(segs, column, bias)
	if i == -1 {
		return gmapping{}
	}
	return gmapping{line: segs[i][1] + 1, column: segs[i][2], ok: true}
}

// Naming the sources of a map as @jridgewell/resolve-uri 3.1.2 does with no
// base (trace-mapping's resolvedSources, for a map without a URL).

type uriType int

const (
	uriEmpty uriType = iota + 1
	uriHash
	uriQuery
	uriRelativePath
	uriAbsolutePath
	uriSchemeRelative
	uriAbsolute
)

type uri struct {
	scheme, user, host, port, path, query, hash string
	kind                                        uriType
}

var (
	schemeRE = regexp.MustCompile(`^[\w+.-]+://`)
	uriRE    = regexp.MustCompile(`^([\w+.-]+:)//([^@/#?]*@)?([^:/#?]*)(:\d+)?(/[^#?]*)?(\?[^#]*)?(#.*)?`)
)

func parseAbsoluteURI(input string) uri {
	m := uriRE.FindStringSubmatch(input)
	if m == nil {
		return uri{path: "/", kind: uriAbsolute}
	}
	u := uri{scheme: m[1], user: m[2], host: m[3], port: m[4], path: m[5], query: m[6], hash: m[7], kind: uriAbsolute}
	if u.path == "" {
		u.path = "/"
	}
	return u
}

// parseFileURI reads file:[//[host]][/]path[?query][#hash], where a host is
// not a drive letter.
func parseFileURI(input string) uri {
	rest := input[len("file:"):]
	u := uri{scheme: "file:", kind: uriAbsolute}
	if after, ok := strings.CutPrefix(rest, "//"); ok {
		rest = after
		drive := len(after) >= 2 && after[1] == ':' && (after[0]|0x20) >= 'a' && (after[0]|0x20) <= 'z'
		if !drive {
			i := strings.IndexAny(after, "/#?")
			if i < 0 {
				i = len(after)
			}
			u.host, rest = after[:i], after[i:]
		}
	}
	i := strings.IndexAny(rest, "#?")
	if i < 0 {
		i = len(rest)
	}
	u.path, rest = rest[:i], rest[i:]
	if !strings.HasPrefix(u.path, "/") {
		u.path = "/" + u.path
	}
	if strings.HasPrefix(rest, "?") {
		j := strings.IndexByte(rest, '#')
		if j < 0 {
			j = len(rest)
		}
		u.query, rest = rest[:j], rest[j:]
	}
	u.hash = rest
	return u
}

func parseURI(input string) uri {
	switch {
	case strings.HasPrefix(input, "//"):
		u := parseAbsoluteURI("http:" + input)
		u.scheme, u.kind = "", uriSchemeRelative
		return u
	case strings.HasPrefix(input, "/"):
		u := parseAbsoluteURI("http://foo.com" + input)
		u.scheme, u.host, u.kind = "", "", uriAbsolutePath
		return u
	case strings.HasPrefix(input, "file:"):
		return parseFileURI(input)
	case schemeRE.MatchString(input):
		return parseAbsoluteURI(input)
	}
	u := parseAbsoluteURI("http://foo.com/" + input)
	u.scheme, u.host = "", ""
	switch {
	case input == "":
		u.kind = uriEmpty
	case strings.HasPrefix(input, "?"):
		u.kind = uriQuery
	case strings.HasPrefix(input, "#"):
		u.kind = uriHash
	default:
		u.kind = uriRelativePath
	}
	return u
}

// normalizePath drops the empty and "." parts of a path and the parts ".."
// cancel, keeping the ".." of a relative path that go above it.
func normalizePath(u *uri, kind uriType) {
	rel := kind <= uriRelativePath
	pieces := strings.Split(u.path, "/")
	pointer, positive := 1, 0
	trailing := false
	for i := 1; i < len(pieces); i++ {
		piece := pieces[i]
		if piece == "" {
			trailing = true
			continue
		}
		trailing = false
		if piece == "." {
			continue
		}
		if piece == ".." {
			if positive > 0 {
				trailing = true
				positive--
				pointer--
			} else if rel {
				pieces[pointer] = piece
				pointer++
			}
			continue
		}
		pieces[pointer] = piece
		pointer++
		positive++
	}
	var b strings.Builder
	for i := 1; i < pointer; i++ {
		b.WriteString("/" + pieces[i])
	}
	p := b.String()
	if p == "" || (trailing && !strings.HasSuffix(p, "/..")) {
		p += "/"
	}
	u.path = p
}

func isRelativeURI(s string) bool {
	return s != "" && (s[0] == '.' || s[0] == '?' || s[0] == '#')
}

// resolveURI is resolve-uri's resolve(input, "").
func resolveURI(input string) string {
	if input == "" {
		return ""
	}
	u := parseURI(input)
	normalizePath(&u, u.kind)
	qh := u.query + u.hash
	switch u.kind {
	case uriHash, uriQuery:
		return qh
	case uriRelativePath:
		p := u.path[1:]
		if p == "" {
			if qh != "" {
				return qh
			}
			return "."
		}
		if isRelativeURI(input) && !isRelativeURI(p) {
			return "./" + p + qh
		}
		return p + qh
	case uriAbsolutePath:
		return u.path + qh
	}
	return u.scheme + "//" + u.user + u.host + u.port + u.path + qh
}
