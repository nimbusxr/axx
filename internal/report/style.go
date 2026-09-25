package report

import (
	"strings"

	"github.com/nimbusxr/axx/internal/runner"
)

// style applies ANSI colors when enabled; the zero value renders plain text.
type style struct{ color bool }

func (s style) paint(code, text string) string {
	if !s.color || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s style) red(t string) string     { return s.paint("31", t) }
func (s style) green(t string) string   { return s.paint("32", t) }
func (s style) yellow(t string) string  { return s.paint("33", t) }
func (s style) magenta(t string) string { return s.paint("35", t) }
func (s style) cyan(t string) string    { return s.paint("36", t) }
func (s style) dim(t string) string     { return s.paint("90", t) }
func (s style) bold(t string) string    { return s.paint("1", t) }

// status colors text by step or scenario status.
func (s style) status(st runner.Status, t string) string {
	switch st {
	case runner.Passed:
		return s.green(t)
	case runner.Failed:
		return s.red(t)
	case runner.Skipped:
		return s.cyan(t)
	case runner.Pending, runner.Undefined:
		return s.yellow(t)
	case runner.Ambiguous:
		return s.magenta(t)
	}
	return t
}

// glyph is the status marker; plain output uses ASCII fallbacks.
func (s style) glyph(st runner.Status) string {
	uni, ascii := "✓", "+"
	switch st {
	case runner.Failed:
		uni, ascii = "✗", "x"
	case runner.Skipped:
		uni, ascii = "↷", "-"
	case runner.Pending, runner.Undefined:
		uni, ascii = "?", "?"
	case runner.Ambiguous:
		uni, ascii = "!", "!"
	}
	if s.color {
		return s.status(st, uni)
	}
	return ascii
}

// diffLine colors one line of a unified diff.
func (s style) diffLine(l string) string {
	switch {
	case strings.HasPrefix(l, "@@"):
		return s.cyan(l)
	case strings.HasPrefix(l, "-"):
		return s.red(l)
	case strings.HasPrefix(l, "+"):
		return s.green(l)
	}
	return l
}

// stripANSI removes ANSI escape sequences (step output may contain them).
func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		// ESC [ params final-byte
		if i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		i++ // two-byte sequence
	}
	return b.String()
}
