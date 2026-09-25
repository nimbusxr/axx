package lsp

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

// utf16Len is the length of s in UTF-16 code units, the unit of LSP
// character offsets.
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

// byteOffset converts a UTF-16 offset within line to a byte offset,
// clamped to the line.
func byteOffset(line string, units int) int {
	n := 0
	for i, r := range line {
		if n >= units {
			return i
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return len(line)
}

// splitLines splits text into lines without their line terminators.
func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// span is a range within one line, in byte offsets.
func span(lines []string, line, from, to int) textRange {
	l := ""
	if line >= 0 && line < len(lines) {
		l = lines[line]
	}
	from, to = clamp(l, from), clamp(l, to)
	return textRange{
		Start: position{Line: line, Character: utf16Len(l[:from])},
		End:   position{Line: line, Character: utf16Len(l[:to])},
	}
}

// wholeLine is the range of line's content without leading and trailing
// whitespace (or the start of the line when it is blank).
func wholeLine(lines []string, line int) textRange {
	l := ""
	if line >= 0 && line < len(lines) {
		l = lines[line]
	}
	start := len(l) - len(strings.TrimLeft(l, " \t"))
	end := len(strings.TrimRight(l, " \t"))
	if end < start {
		end = start
	}
	return span(lines, line, start, end)
}

// clamp limits the byte offset i to l and moves it back to the start of a
// character.
func clamp(l string, i int) int {
	switch {
	case i < 0:
		return 0
	case i >= len(l):
		return len(l)
	}
	for i > 0 && !utf8.RuneStart(l[i]) {
		i--
	}
	return i
}

// uriToPath converts a file:// URI to a local path ("" for other schemes).
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}
