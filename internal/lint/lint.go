package lint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nimbusxr/axx/internal/axxerr"
)

// Options controls a run.
type Options struct {
	// WorkDir is what reported file paths are relative to (default: the
	// process working directory).
	WorkDir string
	// Mode, when set ("error" or "warn"), overrides the mode of every rule.
	Mode string
	// Paths, when set, limits the report to findings with at least one
	// location in these files or directories (absolute, or relative to
	// WorkDir). Every file a rule selects is still scanned, since a value
	// collides with occurrences anywhere.
	Paths []string
}

// Report is the result of a lint run.
type Report struct {
	// BaseDir is where the rules' file patterns were resolved.
	BaseDir string `json:"baseDir"`
	// Rules holds one result per rule, in configuration order.
	Rules []RuleResult `json:"rules"`
	// Summary counts rules, files and findings.
	Summary Summary `json:"summary"`
	// Notes are informational messages (nothing to fix).
	Notes []string `json:"notes,omitempty"`

	// human output truncation (lint.config.maxReportedValues/Locations)
	maxValues, maxLocations int
}

// Summary counts a report.
type Summary struct {
	Rules    int `json:"rules"`
	Files    int `json:"files"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

// RuleResult is the outcome of one rule.
type RuleResult struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	// Source is where the rule is defined (file:line).
	Source string `json:"source,omitempty"`
	// Type is regex, jsonpath, or builtin for axx's own checks.
	Type       string `json:"type"`
	Validation string `json:"validation,omitempty"`
	// Mode is the effective mode: error or warn.
	Mode string `json:"mode"`
	// Files is the number of files the rule scanned.
	Files int `json:"files"`
	// Values is the number of values the rule extracted.
	Values   int       `json:"values"`
	Findings []Finding `json:"findings"`

	files []string // absolute paths of the scanned files
}

// OK reports whether the rule has no error findings.
func (r RuleResult) OK() bool {
	for _, f := range r.Findings {
		if f.Severity == SeverityError {
			return false
		}
	}
	return true
}

// Finding severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Finding is one problem: a colliding value, a file that could not be
// checked, or a feature-file warning.
type Finding struct {
	// Code is the AXX-Exxxx code (`axx explain <code>`).
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	// Value is the colliding value of a duplicate.
	Value string `json:"value,omitempty"`
	// Locations are every occurrence, in file and line order.
	Locations []Location `json:"locations"`
}

// Location is a position in a file.
type Location struct {
	// File is relative to the working directory, with forward slashes.
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	// Text is the trimmed source line.
	Text string `json:"text,omitempty"`

	abs string
}

// String renders file:line:column.
func (l Location) String() string {
	s := l.File
	if l.Line > 0 {
		s += fmt.Sprintf(":%d", l.Line)
		if l.Column > 0 {
			s += fmt.Sprintf(":%d", l.Column)
		}
	}
	return s
}

// Abs is the absolute path of the file.
func (l Location) Abs() string { return l.abs }

// OK reports whether the report has no error findings.
func (r *Report) OK() bool { return r.Summary.Errors == 0 }

// Add appends a rule result (e.g. a builtin check) and recounts.
func (r *Report) Add(rr RuleResult) {
	r.Rules = append(r.Rules, rr)
	r.recount()
}

func (r *Report) recount() {
	r.Summary = Summary{Rules: len(r.Rules)}
	files := map[string]bool{}
	for _, rr := range r.Rules {
		for _, f := range rr.files {
			files[f] = true
		}
		for _, f := range rr.Findings {
			if f.Severity == SeverityError {
				r.Summary.Errors++
			} else {
				r.Summary.Warnings++
			}
		}
	}
	r.Summary.Files = len(files)
}

// Run executes every rule of the set.
func (s *Set) Run(opts Options) *Report {
	if opts.WorkDir == "" {
		opts.WorkDir, _ = os.Getwd()
	}
	rep := &Report{
		BaseDir: relSlash(opts.WorkDir, s.BaseDir), Rules: []RuleResult{},
		maxValues: s.Config.MaxReportedValues, maxLocations: s.Config.MaxReportedLocations,
	}
	if !s.Configured {
		rep.Notes = append(rep.Notes, "no test-data isolation rules configured (add lint.rules to axx.yaml)")
	}
	cache := map[string]*fileText{}
	for _, r := range s.Rules {
		rep.Rules = append(rep.Rules, s.runRule(r, opts, cache))
	}
	rep.filter(opts)
	rep.recount()
	return rep
}

// fileText is a file read once per run and shared by the rules scanning it.
type fileText struct {
	content  string
	runes    []rune
	starts   []int // rune offsets of line starts
	skip     bool  // binary
	tooLarge int64 // size, when over the limit
	err      error
}

func readFile(path string, maxSize int64) *fileText {
	st, err := os.Stat(path)
	if err != nil {
		return &fileText{err: err}
	}
	if st.Size() > maxSize {
		return &fileText{tooLarge: st.Size()}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return &fileText{err: err}
	}
	probe := b
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	for _, c := range probe {
		if c == 0 {
			return &fileText{skip: true}
		}
	}
	return &fileText{content: string(b)}
}

// index prepares rune offsets for regex positions.
func (f *fileText) index() {
	if f.runes != nil {
		return
	}
	f.runes = []rune(f.content)
	f.starts = []int{0}
	for i, r := range f.runes {
		if r == '\n' {
			f.starts = append(f.starts, i+1)
		}
	}
}

// position converts a rune offset into a 1-based line and column.
func (f *fileText) position(off int) (line, col int) {
	i := sort.SearchInts(f.starts, off+1) - 1 // last start <= off
	return i + 1, off - f.starts[i] + 1
}

// lineText returns the trimmed text of a 1-based line.
func (f *fileText) lineText(line int) string {
	f.index()
	if line < 1 || line > len(f.starts) {
		return ""
	}
	end := len(f.runes)
	if line < len(f.starts) {
		end = f.starts[line] - 1
	}
	return clip(javaTrim(string(f.runes[f.starts[line-1]:end])))
}

// javaTrim is String.trim(): it strips characters up to U+0020.
func javaTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
}

const maxTextLen = 200

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxTextLen {
		return s
	}
	return string([]rune(s)[:maxTextLen]) + "…"
}

type occurrence struct {
	loc  Location
	file string // absolute
}

func (s *Set) runRule(r *Rule, opts Options, cache map[string]*fileText) RuleResult {
	rr := RuleResult{
		Name: r.Name, ID: r.ID, Description: r.Description, Source: sourceString(r.Source, opts.WorkDir),
		Type: r.Type, Validation: r.Validation, Mode: r.Mode, Findings: []Finding{},
	}
	if rr.Mode == "" {
		rr.Mode = s.Config.Mode
	}
	if opts.Mode != "" {
		rr.Mode = opts.Mode
	}
	severity := SeverityError
	if rr.Mode == ModeWarn {
		severity = SeverityWarning
	}
	loc := func(abs string, line, col int, text string) Location {
		return Location{File: relSlash(opts.WorkDir, abs), Line: line, Column: col, Text: text, abs: abs}
	}
	problem := func(code, sev string, l Location, format string, args ...any) {
		rr.Findings = append(rr.Findings, Finding{Code: code, Severity: sev, Message: fmt.Sprintf(format, args...), Locations: []Location{l}})
	}

	files, err := findFiles(s.BaseDir, r.patterns)
	if err != nil {
		problem(CodeUnreadable, SeverityError, loc(s.BaseDir, 0, 0, ""), "cannot search for files: %v", err)
	}
	values := map[string][]occurrence{}
	var order []string
	add := func(v string, o occurrence) {
		if r.ignore[v] {
			return
		}
		if _, ok := values[v]; !ok {
			order = append(order, v)
		}
		values[v] = append(values[v], o)
		rr.Values++
	}
	for _, f := range files {
		if excluded(r, s.BaseDir, f) {
			continue
		}
		ft, ok := cache[f]
		if !ok {
			ft = readFile(f, s.Config.MaxFileSize)
			cache[f] = ft
		}
		switch {
		case ft.err != nil:
			problem(CodeUnreadable, SeverityError, loc(f, 0, 0, ""), "cannot read %s: %v", relSlash(opts.WorkDir, f), unwrapPathError(ft.err))
			continue
		case ft.tooLarge > 0:
			problem(CodeTooLarge, SeverityWarning, loc(f, 0, 0, ""), "skipped %s: %d bytes exceeds lint.config.maxFileSize %d", relSlash(opts.WorkDir, f), ft.tooLarge, s.Config.MaxFileSize)
			continue
		case ft.skip:
			continue // binary
		}
		rr.files = append(rr.files, f)
		if r.path != nil {
			ms, err := r.path.extract(ft.content)
			if err != nil {
				var je *JSONError
				if errors.As(err, &je) {
					problem(CodeUnparsable, SeverityError, loc(f, je.Line, je.Column, ft.lineText(je.Line)),
						"%s is not valid JSON, which a jsonpath rule requires: %s", relSlash(opts.WorkDir, f), je.Message)
				} else {
					problem(CodeUnparsable, SeverityError, loc(f, 0, 0, ""), "cannot evaluate jsonPath %s over %s: %v", r.JSONPath, relSlash(opts.WorkDir, f), err)
				}
				continue
			}
			for _, m := range ms {
				text := ""
				if m.line > 0 {
					text = ft.lineText(m.line)
				}
				add(m.value, occurrence{loc: loc(f, m.line, m.col, text), file: f})
			}
			continue
		}
		ms, err := r.regex.FindAll(ft.content)
		if err != nil {
			problem(CodeUnparsable, SeverityError, loc(f, 0, 0, ""), "cannot match the regex over %s: %v", relSlash(opts.WorkDir, f), err)
		}
		ft.index()
		for _, m := range ms {
			g := 0
			for i := 1; i < len(m.Groups); i++ {
				if m.Matched[i] {
					g = i
					break
				}
			}
			if g == 0 {
				continue
			}
			line, col := ft.position(m.Start[g])
			add(m.Groups[g], occurrence{loc: loc(f, line, col, ft.lineText(line)), file: f})
		}
	}
	rr.Files = len(rr.files)

	for _, v := range order {
		occ := duplicates(r.Validation, values[v])
		if occ == nil {
			continue
		}
		f := Finding{Code: CodeDuplicate, Severity: severity, Value: v, Message: duplicateMessage(r.Validation, v, occ, opts.WorkDir)}
		for _, o := range occ {
			f.Locations = append(f.Locations, o.loc)
		}
		rr.Findings = append(rr.Findings, f)
	}
	sortFindings(rr.Findings)
	return rr
}

// sourceString renders where a rule is defined as file:line, relative to
// workDir. Configuration locations are relative to the process directory.
func sourceString(l axxerr.Location, workDir string) string {
	if l.File == "" {
		return ""
	}
	abs := filepath.FromSlash(l.File)
	if !filepath.IsAbs(abs) {
		if wd, err := os.Getwd(); err == nil {
			abs = filepath.Join(wd, abs)
		}
	}
	l.File, l.Column = relSlash(workDir, abs), 0
	return l.String()
}

func unwrapPathError(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// excluded applies excludePatterns to the path relative to baseDir.
func excluded(r *Rule, baseDir, file string) bool {
	rel := relSlash(baseDir, file)
	for _, m := range r.excludes {
		if m.match(rel) {
			return true
		}
	}
	return false
}

// duplicates returns the occurrences to report for a value, or nil.
func duplicates(validation string, occ []occurrence) []occurrence {
	switch validation {
	case CrossFileUnique:
		files := map[string]bool{}
		for _, o := range occ {
			files[o.file] = true
		}
		if len(files) > 1 {
			return occ
		}
		return nil
	case FileUnique:
		count := map[string]int{}
		for _, o := range occ {
			count[o.file]++
		}
		var out []occurrence
		for _, o := range occ {
			if count[o.file] > 1 {
				out = append(out, o)
			}
		}
		return out
	default:
		if len(occ) > 1 {
			return occ
		}
		return nil
	}
}

func duplicateMessage(validation, v string, occ []occurrence, workDir string) string {
	files := map[string]bool{}
	for _, o := range occ {
		files[o.file] = true
	}
	switch validation {
	case CrossFileUnique:
		return fmt.Sprintf("value %q appears in %d files (cross-file-unique)", v, len(files))
	case FileUnique:
		if len(files) == 1 {
			return fmt.Sprintf("value %q appears %d times in %s (file-unique)", v, len(occ), relSlash(workDir, occ[0].file))
		}
		return fmt.Sprintf("value %q repeats within %d files (file-unique)", v, len(files))
	default:
		return fmt.Sprintf("value %q appears %d times (global-unique)", v, len(occ))
	}
}

// sortFindings orders findings by their first location, then value.
func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if len(a.Locations) > 0 && len(b.Locations) > 0 {
			la, lb := a.Locations[0], b.Locations[0]
			if la.File != lb.File {
				return la.File < lb.File
			}
			if la.Line != lb.Line {
				return la.Line < lb.Line
			}
			if la.Column != lb.Column {
				return la.Column < lb.Column
			}
		}
		return a.Value < b.Value
	})
}

// filter keeps only findings touching opts.Paths.
func (r *Report) filter(opts Options) {
	if len(opts.Paths) == 0 {
		return
	}
	var roots []string
	for _, p := range opts.Paths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(opts.WorkDir, p)
		}
		roots = append(roots, filepath.Clean(p))
	}
	within := func(abs string) bool {
		for _, root := range roots {
			if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	for i := range r.Rules {
		kept := []Finding{}
		for _, f := range r.Rules[i].Findings {
			for _, l := range f.Locations {
				if within(l.abs) {
					kept = append(kept, f)
					break
				}
			}
		}
		r.Rules[i].Findings = kept
	}
}

// Filter limits the report to findings touching paths (see Options.Paths)
// and recounts; used after adding builtin results.
func (r *Report) Filter(opts Options) {
	if opts.WorkDir == "" {
		opts.WorkDir, _ = os.Getwd()
	}
	r.filter(opts)
	r.recount()
}
