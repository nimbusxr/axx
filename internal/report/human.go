package report

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/runner"
)

const (
	stepIndent   = "    "
	detailIndent = "        "
	// showDuration is the threshold above which step durations are shown.
	showDuration = 100 * time.Millisecond
	// Limits for step arguments, logs and attachments in human output.
	maxArgLines  = 40
	maxBodyLines = 10
	maxBodyBytes = 1024
)

// human renders scenario blocks and run summaries for pretty and progress.
type human struct {
	out     *writer
	opts    Options
	st      style
	lastURI string
}

func newHuman(out *writer, opts Options) *human {
	return &human{out: out, opts: opts, st: style{color: opts.Color}}
}

// block renders one scenario. The feature header is included when the
// scenario belongs to a different feature than the previous block.
func (h *human) block(r *runner.ScenarioResult) string {
	var b strings.Builder
	if uri := h.opts.uri(r); uri != h.lastURI {
		h.lastURI = uri
		kw := "Feature"
		if p := r.Pickle; p != nil && p.Doc != nil && p.Doc.AST != nil && p.Doc.AST.Feature != nil {
			kw = p.Doc.AST.Feature.Keyword
		}
		b.WriteString(h.st.bold(kw+":") + " " + featureName(r) + "\n\n")
	}
	b.WriteString("  " + h.st.bold(scenarioKeyword(r)+":") + " " + scenarioName(r) + "  " + h.st.dim("# "+h.opts.scenarioLoc(r)) + "\n")
	allSteps(r, func(s *runner.StepResult, p phase) {
		// Hooks are only interesting when they did not pass, or said something.
		if p != phaseStep && (s.Status == runner.Passed || s.Status == runner.Skipped) && len(s.Logs) == 0 {
			return
		}
		h.step(&b, s, p)
	})
	b.WriteString("\n")
	return b.String()
}

func (h *human) step(b *strings.Builder, s *runner.StepResult, p phase) {
	title := stepTitle(s, p)
	if s.Status == runner.Skipped {
		title = h.st.dim(title)
	}
	parts := []string{h.st.glyph(s.Status) + " " + title}
	if s.Background {
		parts = append(parts, h.st.dim("(background)"))
	}
	switch s.Status {
	case runner.Undefined, runner.Pending, runner.Ambiguous:
		parts = append(parts, h.st.status(s.Status, "("+s.Status.String()+")"))
	}
	if s.Duration >= showDuration {
		parts = append(parts, h.st.dim("("+fmtDuration(s.Duration)+")"))
	}
	b.WriteString(stepIndent + strings.Join(parts, "  ") + "\n")

	if s.Table != nil {
		writeIndented(b, detailIndent, tableLines(s.Table))
	}
	if s.DocString != nil {
		writeIndented(b, detailIndent, docStringLines(s.DocString))
	}
	for _, l := range s.Logs {
		lines := truncateBody(l)
		b.WriteString(detailIndent + h.st.dim("log:") + " " + lines[0] + "\n")
		writeIndented(b, detailIndent+"     ", lines[1:])
	}
	for _, a := range s.Attachments {
		b.WriteString(detailIndent + h.st.dim("attachment:") + " " + attachmentLabel(a) + "\n")
		if isTextMedia(a.MediaType) {
			writeIndented(b, detailIndent+"  ", truncateBody(string(a.Body)))
		}
	}
	if f := describe(s); f != nil {
		writeIndented(b, detailIndent, f.lines(h.st))
	}
}

// summary renders the end-of-run summary shared by pretty and progress.
func (h *human) summary(res *runner.RunResult) string {
	var b strings.Builder
	var failed []*runner.ScenarioResult
	for _, r := range res.Scenarios {
		if failing(r.Status) {
			failed = append(failed, r)
		}
	}
	if len(failed) > 0 {
		b.WriteString(h.st.bold("Failed scenarios:") + "\n")
		for _, r := range failed {
			line := "  " + h.st.glyph(r.Status) + " " + scenarioName(r) + "  " + h.st.dim("# "+h.opts.scenarioLoc(r))
			if r.Status != runner.Failed {
				line += "  " + h.st.status(r.Status, "("+r.Status.String()+")")
			}
			b.WriteString(line + "\n")
			if cmd := h.opts.rerun(r); cmd != "" {
				b.WriteString("      " + h.st.dim("rerun:") + " " + cmd + "\n")
			}
		}
		b.WriteString("\n")
	}
	if len(res.RunErrors) > 0 {
		b.WriteString(h.st.bold("Run failures:") + "\n")
		for _, re := range res.RunErrors {
			lines := strings.Split(strings.TrimRight(re.Message, "\n"), "\n")
			b.WriteString("  " + h.st.glyph(runner.Failed) + " " + lines[0] + "  " + h.st.dim("("+re.Source+")") + "\n")
			for _, l := range lines[1:] {
				if strings.TrimSpace(l) == "" {
					b.WriteString("\n")
					continue
				}
				b.WriteString("      " + l + "\n")
			}
		}
		b.WriteString("\n")
	}
	c := res.Counts()
	steps := 0
	for _, n := range c.Steps {
		steps += n
	}
	b.WriteString(h.countLine(len(res.Scenarios), "scenario", c.Scenarios) + "\n")
	b.WriteString(h.countLine(steps, "step", c.Steps) + "\n")
	if res.NotRun > 0 {
		b.WriteString(h.st.yellow("NOT RUN "+strconv.Itoa(res.NotRun)) + "\n")
	}
	if res.Interrupted {
		b.WriteString(h.st.yellow("Interrupted") + "\n")
	}
	if res.DryRun {
		b.WriteString("Dry run: steps were matched but not executed\n")
	}
	b.WriteString("Finished in " + fmtDuration(res.Duration) + "\n")
	return b.String()
}

// countLine renders e.g. "9 scenarios (1 failed, 8 passed)".
func (h *human) countLine(total int, noun string, counts map[runner.Status]int) string {
	s := strconv.Itoa(total) + " " + noun
	if total != 1 {
		s += "s"
	}
	var parts []string
	for _, st := range statusOrder {
		if n := counts[st]; n > 0 {
			parts = append(parts, h.st.status(st, strconv.Itoa(n)+" "+st.String()))
		}
	}
	if len(parts) > 0 {
		s += " (" + strings.Join(parts, ", ") + ")"
	}
	return s
}

func writeIndented(b *strings.Builder, indent string, lines []string) {
	for _, l := range lines {
		if l == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(indent + l + "\n")
	}
}

// tableLines renders a data table with aligned columns.
func tableLines(t *core.Table) []string {
	widths := map[int]int{}
	cells := make([][]string, len(t.Rows))
	for i, row := range t.Rows {
		cells[i] = make([]string, len(row))
		for j, c := range row {
			c = strings.NewReplacer(`\`, `\\`, "|", `\|`, "\n", `\n`).Replace(c)
			cells[i][j] = c
			widths[j] = max(widths[j], utf8.RuneCountInString(c))
		}
	}
	out := make([]string, 0, len(cells))
	for _, row := range cells {
		var b strings.Builder
		b.WriteString("|")
		for j, c := range row {
			b.WriteString(" " + c + strings.Repeat(" ", widths[j]-utf8.RuneCountInString(c)) + " |")
		}
		out = append(out, b.String())
	}
	return capLines(out, maxArgLines)
}

func docStringLines(d *core.DocString) []string {
	lines := capLines(splitLines(d.Content), maxArgLines)
	out := make([]string, 0, len(lines)+2)
	out = append(out, `"""`+d.MediaType)
	out = append(out, lines...)
	return append(out, `"""`)
}

// truncateBody limits a log or attachment body for display.
func truncateBody(s string) []string {
	s = strings.TrimRight(stripANSI(s), "\n")
	cut := 0
	if len(s) > maxBodyBytes {
		cut = len(s) - maxBodyBytes
		s = strings.ToValidUTF8(s[:maxBodyBytes], "")
	}
	lines := capLines(splitLines(s), maxBodyLines)
	if cut > 0 {
		lines = append(lines, fmt.Sprintf("… (%d more bytes)", cut))
	}
	return lines
}

func capLines(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return append(lines[:n:n], fmt.Sprintf("… (%d more lines)", len(lines)-n))
}

func attachmentLabel(a runner.Attachment) string {
	name := a.Name
	if name == "" {
		name = "(unnamed)"
	}
	return name + " (" + a.MediaType + ", " + fmtBytes(len(a.Body)) + ")"
}

func fmtBytes(n int) string {
	switch {
	case n < 1024:
		return strconv.Itoa(n) + " B"
	case n < 1024*1024:
		return strconv.FormatFloat(float64(n)/1024, 'f', 1, 64) + " KB"
	default:
		return strconv.FormatFloat(float64(n)/(1024*1024), 'f', 1, 64) + " MB"
	}
}

// isTextMedia reports whether an attachment body is human-readable text.
func isTextMedia(mediaType string) bool {
	mt := strings.ToLower(mediaType)
	return strings.HasPrefix(mt, "text/") || strings.Contains(mt, "json") || strings.Contains(mt, "xml") ||
		strings.Contains(mt, "yaml") || strings.HasSuffix(mt, "+plain")
}
