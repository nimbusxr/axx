package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/internal/runner"
)

// Error kinds. They are part of the agent report contract.
const (
	KindAssertion = "assertion"
	KindError     = "error"
	KindTimeout   = "timeout"
	KindPanic     = "panic"
	KindUndefined = "undefined"
	KindAmbiguous = "ambiguous"
	KindPending   = "pending"
)

// failure describes why a step or hook did not pass.
type failure struct {
	kind string
	// message is the headline. For panics it omits the "panic: " prefix.
	message string
	// values is set when an assertion carries expected/actual values.
	values           bool
	expected, actual any
	stack            string
	candidates       []string
	suggestions      []match.Suggestion
}

// describe explains a non-passing step; it returns nil for passed and
// skipped steps.
func describe(s *runner.StepResult) *failure {
	switch s.Status {
	case runner.Passed, runner.Skipped:
		return nil
	case runner.Undefined:
		return &failure{kind: KindUndefined, message: "undefined step", suggestions: s.Suggestions}
	case runner.Ambiguous:
		msg := "ambiguous step"
		if s.Err != nil {
			msg = s.Err.Error()
		}
		return &failure{kind: KindAmbiguous, message: msg, candidates: s.Candidates}
	case runner.Pending:
		msg := "pending"
		if s.Err != nil {
			msg = s.Err.Error()
		}
		return &failure{kind: KindPending, message: msg}
	}
	err := s.Err
	if err == nil {
		return &failure{kind: KindError, message: "failed"}
	}
	var pe *runner.PanicError
	if errors.As(err, &pe) {
		return &failure{kind: KindPanic, message: strings.TrimPrefix(err.Error(), "panic: "), stack: pe.Stack}
	}
	var te *runner.TimeoutError
	if errors.As(err, &te) {
		return &failure{kind: KindTimeout, message: err.Error()}
	}
	var ae *core.AssertionError
	if errors.As(err, &ae) {
		f := &failure{kind: KindAssertion, message: ae.Message, expected: ae.Expected, actual: ae.Actual}
		f.values = ae.Expected != nil || ae.Actual != nil
		// Keep context added by wrapping ("checking body: %w").
		if full := err.Error(); full != ae.Error() {
			if prefix, ok := strings.CutSuffix(full, ae.Error()); ok {
				f.message = prefix + ae.Message
			}
		}
		return f
	}
	return &failure{kind: KindError, message: err.Error()}
}

// headline is the one-line summary of a failure.
func (f *failure) headline() string {
	msg := firstLine(f.message)
	switch f.kind {
	case KindPanic:
		return "panic: " + msg
	case KindTimeout:
		return "timeout: " + msg
	case KindAssertion:
		if f.values && diffValues(f.expected, f.actual, 0) == nil {
			return joinNonEmpty(": ", msg, "expected "+renderValue(f.expected, 80)+", got "+renderValue(f.actual, 80))
		}
	}
	return msg
}

// errorType names the failure for formats with an exception type field.
func (f *failure) errorType() string {
	switch f.kind {
	case KindAssertion:
		return "AssertionError"
	case KindTimeout:
		return "TimeoutError"
	case KindPanic:
		return "Panic"
	case KindUndefined:
		return "UndefinedStep"
	case KindAmbiguous:
		return "AmbiguousStep"
	case KindPending:
		return "PendingStep"
	}
	return "Error"
}

// lines renders the failure as plain or styled text lines (without
// indentation), for pretty, progress, JUnit and Cucumber JSON.
func (f *failure) lines(st style) []string {
	var out []string
	switch f.kind {
	case KindAssertion:
		out = append(out, paintLines(st.red, f.message)...)
		if f.values {
			out = append(out,
				"  "+st.dim("expected:")+" "+renderValue(f.expected, 200),
				"  "+st.dim("actual:  ")+" "+renderValue(f.actual, 200))
			if d := diffValues(f.expected, f.actual, 3); d != nil {
				out = append(out, "  "+st.dim("diff (-expected +actual):"))
				for _, l := range d {
					out = append(out, "  "+st.diffLine(l))
				}
			}
		}
	case KindPanic:
		out = append(out, st.red("panic: "+f.message))
		for _, l := range trimStack(f.stack, 24) {
			out = append(out, "  "+st.dim(l))
		}
	case KindTimeout:
		out = append(out, paintLines(st.red, "timeout: "+f.message)...)
	case KindUndefined:
		if len(f.suggestions) > 0 {
			out = append(out, st.yellow("did you mean:"))
			for _, s := range f.suggestions {
				out = append(out, "  "+s.Expr+"  "+st.dim("("+s.ID+")"))
			}
		}
	case KindAmbiguous:
		out = append(out, st.magenta(f.message))
		for _, c := range f.candidates {
			out = append(out, "  - "+c)
		}
	case KindPending:
		if f.message != "pending" {
			out = append(out, paintLines(st.yellow, f.message)...)
		}
	default:
		out = append(out, paintLines(st.red, f.message)...)
	}
	return out
}

// paintLines splits text into lines and colors each one separately, so
// indentation added later stays outside the escape sequences.
func paintLines(paint func(string) string, text string) []string {
	lines := splitLines(text)
	for i, l := range lines {
		lines[i] = paint(l)
	}
	return lines
}

// text is the plain-text rendering used inside XML and JSON documents.
func (f *failure) text() string {
	return strings.Join(f.lines(style{}), "\n")
}

// compactLines renders the failure for the compact reporter: no color, no
// stack, a tight diff. baseDir shortens the file of a panic site.
func (f *failure) compactLines(baseDir string) []string {
	switch f.kind {
	case KindAssertion:
		switch d := diffValues(f.expected, f.actual, 1); {
		case !f.values:
			return splitLines(f.message)
		case d != nil:
			return append(append(splitLines(f.message), "diff -expected +actual"), d...)
		case strings.Contains(f.message, "\n"):
			return append(splitLines(f.message), "expected "+renderValue(f.expected, 80)+", got "+renderValue(f.actual, 80))
		}
		return []string{f.headline()}
	case KindPanic:
		out := []string{"panic: " + f.message}
		if site := panicSite(f.stack, baseDir); site != "" {
			out = append(out, "at "+site)
		}
		return out
	case KindTimeout:
		return []string{"timeout: " + f.message}
	case KindUndefined:
		out := make([]string, 0, len(f.suggestions))
		for _, s := range f.suggestions {
			out = append(out, "did you mean: "+s.Expr)
		}
		return out
	case KindAmbiguous:
		out := []string{f.message}
		for _, c := range f.candidates {
			out = append(out, "matches: "+c)
		}
		return out
	}
	return splitLines(f.message)
}

// renderValue renders an expected/actual value on one line: strings are
// quoted, JSON documents and composite values are compact JSON.
func renderValue(v any, maxRunes int) string {
	var s string
	switch x := v.(type) {
	case nil:
		s = "nil"
	case string:
		s = renderString(x)
	case []byte:
		s = renderString(string(x))
	case error:
		s = x.Error()
	default:
		if isComposite(v) {
			if b, err := marshalNoEscape(v); err == nil {
				s = string(b)
				break
			}
		}
		s = fmt.Sprintf("%v", v)
	}
	return truncateRunes(s, maxRunes)
}

func renderString(s string) string {
	if isJSONDocument(s) {
		var b bytes.Buffer
		if json.Compact(&b, []byte(strings.TrimSpace(s))) == nil {
			return b.String()
		}
	}
	return strconv.Quote(s)
}

// isJSONDocument reports whether s is a JSON object or array.
func isJSONDocument(s string) bool {
	t := strings.TrimSpace(s)
	return (strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")) && json.Valid([]byte(t))
}

func isComposite(v any) bool {
	t := reflect.TypeOf(v)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return false
	}
	switch t.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		return true
	}
	return false
}

// jsonLines pretty-prints v when it is a JSON document or a composite value,
// normalizing key order so equivalent documents compare equal.
func jsonLines(v any) ([]string, bool) {
	var raw []byte
	switch x := v.(type) {
	case nil:
		return nil, false
	case string:
		if !isJSONDocument(x) {
			return nil, false
		}
		raw = []byte(x)
	case []byte:
		if !isJSONDocument(string(x)) {
			return nil, false
		}
		raw = x
	default:
		if !isComposite(v) {
			return nil, false
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		raw = b
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, false
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, false
	}
	return strings.Split(strings.TrimRight(buf.String(), "\n"), "\n"), true
}

// diffValues returns a unified line diff of expected and actual when both
// are JSON documents or strings spanning several lines, else nil.
func diffValues(expected, actual any, context int) []string {
	a, aok := jsonLines(expected)
	b, bok := jsonLines(actual)
	if !aok || !bok {
		es, eok := expected.(string)
		as, sok := actual.(string)
		if !eok || !sok || (!strings.Contains(es, "\n") && !strings.Contains(as, "\n")) {
			return nil
		}
		a, b = splitLines(strings.TrimSuffix(es, "\n")), splitLines(strings.TrimSuffix(as, "\n"))
	}
	return unifiedDiff(a, b, context)
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// trimStack drops the frames of the panic machinery (everything up to and
// including runtime.panic) and caps the result.
func trimStack(stack string, maxLines int) []string {
	lines := splitLines(strings.TrimRight(stack, "\n"))
	for i, l := range lines {
		if strings.HasPrefix(l, "panic(") {
			lines = lines[min(i+2, len(lines)):]
			break
		}
	}
	out := make([]string, 0, min(len(lines), maxLines+1))
	for i, l := range lines {
		if i == maxLines {
			out = append(out, fmt.Sprintf("… (%d more lines)", len(lines)-maxLines))
			break
		}
		out = append(out, strings.Replace(l, "\t", "  ", 1))
	}
	return out
}

// panicSite returns "function (file:line)" for the frame that panicked,
// without call arguments and with file relative to baseDir when possible.
func panicSite(stack, baseDir string) string {
	lines := trimStack(stack, 2)
	if len(lines) < 2 {
		return ""
	}
	fn := strings.TrimSpace(lines[0])
	if i := strings.LastIndex(fn, "("); i > 0 && strings.HasSuffix(fn, ")") {
		fn = fn[:i]
	}
	file := strings.TrimSpace(lines[1])
	if i := strings.LastIndex(file, " +0x"); i > 0 {
		file = file[:i]
	}
	// Rooted paths too: on Windows, /work/x.go is not absolute but is not relative either.
	if baseDir != "" && (filepath.IsAbs(file) || strings.HasPrefix(file, "/")) {
		if rel, err := filepath.Rel(baseDir, file); err == nil && !strings.HasPrefix(rel, "..") {
			file = filepath.ToSlash(rel)
		}
	}
	return fn + " (" + file + ")"
}

func splitLines(s string) []string { return strings.Split(s, "\n") }

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

func truncateRunes(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
