package v8cov

import (
	"regexp"
	"strconv"
	"unicode/utf16"
)

// A source as v8-to-istanbul's CovSource sees it: lines with their start and
// end offsets in UTF-16 code units, which is what V8's ranges and source
// maps' columns count.

type covLine struct {
	line             int // 1-based
	startCol, endCol int // absolute offsets; endCol leaves out the line break
	count            int
	ignore           bool
}

type covSource struct {
	lines []*covLine
	eof   int
}

func newCovSource(src string) *covSource {
	u := trimEndJS(utf16.Encode([]rune(src)))
	s := &covSource{eof: len(u)}
	s.buildLines(u)
	return s
}

// trimEndJS is String.prototype.trimEnd: JavaScript's white space and line
// terminators.
func trimEndJS(u []uint16) []uint16 {
	for len(u) > 0 && isJSSpace(u[len(u)-1]) {
		u = u[:len(u)-1]
	}
	return u
}

func isJSSpace(c uint16) bool {
	switch c {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return c >= 0x2000 && c <= 0x200a
}

// The comments that leave lines out of the coverage, as c8 reads them.
var (
	ignoreNextN   = regexp.MustCompile(`^\W*/\* (?:[cv]8|node:coverage) ignore next (?P<count>[0-9]+)`)
	ignoreNextOwn = regexp.MustCompile(`^\W*/\* (?:[cv]8|node:coverage) ignore next`)
	ignoreNextIn  = regexp.MustCompile(`/\* ([cv]8|node:coverage) ignore next`)
	ignoreStartSt = regexp.MustCompile(`/\* [c|v]8 ignore (?P<mode>start|stop)`)
	ignoreNodeEn  = regexp.MustCompile(`/\* node:coverage (?P<mode>enable|disable)`)
)

type ignoreToken struct {
	count       int
	hasCount    bool
	start, stop bool
}

func parseIgnore(line string) *ignoreToken {
	if m := ignoreNextN.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[1])
		return &ignoreToken{count: n, hasCount: true}
	}
	if ignoreNextOwn.MatchString(line) {
		return &ignoreToken{count: 1, hasCount: true}
	}
	if ignoreNextIn.MatchString(line) {
		return &ignoreToken{count: 0, hasCount: true}
	}
	if m := ignoreStartSt.FindStringSubmatch(line); m != nil {
		return &ignoreToken{start: m[1] == "start", stop: m[1] == "stop"}
	}
	if m := ignoreNodeEn.FindStringSubmatch(line); m != nil {
		return &ignoreToken{start: m[1] == "disable", stop: m[1] == "enable"}
	}
	return nil
}

func (s *covSource) buildLines(u []uint16) {
	position, ignoreCount, ignoreAll := 0, 0, false
	n := 0
	for i := 0; i <= len(u); {
		// A line runs to its \n, which it keeps (split(/(?<=\r?\n)/)).
		j := i
		for j < len(u) && u[j] != '\n' {
			j++
		}
		if j < len(u) {
			j++
		}
		piece := u[i:j]
		nl := 0
		if len(piece) > 0 && piece[len(piece)-1] == '\n' {
			nl = 1
			if len(piece) > 1 && piece[len(piece)-2] == '\r' {
				nl = 2
			}
		}
		n++
		line := &covLine{line: n, startCol: position, endCol: position + len(piece) - nl, count: 1}
		if ignoreCount > 0 {
			line.ignore = true
			ignoreCount--
		} else if ignoreAll {
			line.ignore = true
		}
		s.lines = append(s.lines, line)
		position += len(piece)
		if tok := parseIgnore(string(utf16.Decode(piece))); tok != nil {
			line.ignore = true
			if tok.hasCount {
				ignoreCount = tok.count
			}
			if tok.start || tok.stop {
				ignoreAll = tok.start
				ignoreCount = 0
			}
		}
		if j == len(u) {
			break
		}
		i = j
	}
}

// sliceRange is v8-to-istanbul's: the lines a range touches.
func sliceRange(lines []*covLine, startCol, endCol int, inclusive bool) []*covLine {
	start, end := 0, len(lines)
	if inclusive {
		startCol--
	}
search:
	for start < end {
		mid := (start + end) >> 1
		switch {
		case startCol >= lines[mid].endCol:
			start = mid + 1
		case endCol < lines[mid].startCol:
			end = mid - 1
		default:
			end = mid
			for mid >= 0 && startCol < lines[mid].endCol && endCol >= lines[mid].startCol {
				mid--
			}
			start = mid + 1
			break search
		}
	}
	for end >= 0 && end < len(lines) && startCol < lines[end].endCol && endCol >= lines[end].startCol {
		end++
	}
	if start > end || start < 0 {
		return nil
	}
	return lines[start:end]
}

// relativeToOffset turns a line (1-based) and a column into an offset; ok
// false is a position the source map has none for (the offset is then
// eof).
func (s *covSource) relativeToOffset(line, relCol int, ok bool) int {
	if !ok {
		return s.eof
	}
	line = max(line, 1)
	if line-1 >= len(s.lines) {
		return s.eof
	}
	l := s.lines[line-1]
	return min(l.startCol+relCol, l.endCol)
}
