package kafka

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/javare"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

// label is a named expectation ("the X kafka event named L ..."): every
// assertion step adds matchers to it, and passes when one record of the
// topic satisfies all of them at once.
type label struct {
	Name    string
	Keys    []string
	Payload []payloadMatcher
	Headers []headerMatcher
	// Matched is the record the last passing assertion found.
	Matched *recordInfo
}

type payloadMatcher struct {
	Path     string
	Raw      string
	v        core.Value
	expected any
}

type headerMatcher struct {
	Key   string
	Value string
	re    *javare.Regexp
}

// newPayloadMatcher expects the property at path to have a value as a
// feature writes it, which means what it says (ADR 0014): `42` the integer,
// `"42"` the string, `null` JSON null, `undefined` no such property. The
// value is read (and the path compiled) when the step runs, so a bad value
// fails the step at once; empty says the cell was empty, which fails too.
func newPayloadMatcher(path, value string, empty bool) (payloadMatcher, error) {
	v, err := core.ParseValue(value, empty, true)
	if err != nil {
		return payloadMatcher{}, fmt.Errorf("%s: %w", path, err)
	}
	expected, err := jvalue.Expected(v)
	if err != nil {
		return payloadMatcher{}, fmt.Errorf("value %q for %s: %w", value, path, err)
	}
	if _, err := jsonx.Compile(path); err != nil {
		return payloadMatcher{}, fmt.Errorf("invalid JSONPath %q: %w", path, err)
	}
	return payloadMatcher{Path: path, Raw: value, v: v, expected: expected}, nil
}

// mismatch says why the payload doc does not have the property, or "" when
// it does.
func (m payloadMatcher) mismatch(doc any) string {
	v, _, err := jsonx.Read(doc, m.Path)
	switch {
	case m.v.Undefined() && err != nil && jsonx.IsJsonPathException(err):
		return ""
	case err != nil:
		return readFailure(doc, m.Path, err)
	case m.v.Undefined():
		return fmt.Sprintf("%s is %s, expected no such property", m.Path, describeJSON(v))
	case jvalue.JavaEquals(v, m.expected):
		return ""
	}
	msg := fmt.Sprintf("%s is %s, expected %s", m.Path, describeJSON(v), m.Raw)
	if h := jvalue.TypeHint(m.Path, m.v, v); h != "" {
		msg += " (" + h + ")"
	}
	return msg
}

func newHeaderRegex(key, pattern string) (headerMatcher, error) {
	re, err := javare.Compile(pattern)
	if err != nil {
		return headerMatcher{}, fmt.Errorf("header %s: invalid regular expression %q: %w", key, pattern, err)
	}
	return headerMatcher{Key: key, Value: pattern, re: re}, nil
}

func (m payloadMatcher) String() string { return m.Path + " == " + m.Raw }

func (m headerMatcher) String() string {
	if m.re != nil {
		return "header " + m.Key + " matches " + m.Value
	}
	return "header " + m.Key + " == " + strconv.Quote(m.Value)
}

// matchers lists the label's expectations for reports.
func (l *label) matchers() []string {
	var out []string
	for _, k := range l.Keys {
		out = append(out, "key == "+strconv.Quote(k))
	}
	for _, p := range l.Payload {
		out = append(out, p.String())
	}
	for _, h := range l.Headers {
		out = append(out, h.String())
	}
	return out
}

// eval checks a record against every matcher, in order (key, payload,
// headers), and says why the first failing one failed.
func (l *label) eval(d *decoded, distinctHeaders bool) (bool, string) {
	for _, k := range l.Keys {
		switch {
		case d.KeyErr != "":
			return false, "key: " + d.KeyErr
		case d.Key == nil:
			return false, fmt.Sprintf("key is null, expected %q", k)
		case *d.Key != k:
			return false, fmt.Sprintf("key is %q, expected %q", *d.Key, k)
		}
	}
	if len(l.Payload) > 0 {
		if d.ValueErr != "" {
			return false, "value: " + d.ValueErr
		}
		doc, err := d.payloadDoc()
		if err != nil {
			return false, "payload is not JSON: " + err.Error()
		}
		for _, p := range l.Payload {
			if why := p.mismatch(doc); why != "" {
				return false, why
			}
		}
	}
	for _, h := range l.Headers {
		vals := d.headerValues(h.Key, distinctHeaders)
		if len(vals) != 1 {
			return false, fmt.Sprintf("header %s has %d value(s), expected exactly one", h.Key, len(vals))
		}
		if vals[0] == nil {
			return false, fmt.Sprintf("header %s is null", h.Key)
		}
		got := *vals[0]
		if h.re == nil {
			if got != h.Value {
				return false, fmt.Sprintf("header %s is %q, expected %q", h.Key, got, h.Value)
			}
			continue
		}
		ok, err := h.re.FullMatch(got)
		if err != nil {
			return false, fmt.Sprintf("header %s: %v", h.Key, err)
		}
		if !ok {
			return false, fmt.Sprintf("header %s is %q, which does not match %s", h.Key, got, h.Value)
		}
	}
	return true, ""
}

func describeJSON(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	if v == nil {
		return "null"
	}
	return jsonx.MarshalValue(v)
}

// readFailure says why a JSONPath of a check found nothing in an event's
// payload: a path that is not there names the properties the payload has,
// so a misspelled or misplaced path shows.
func readFailure(doc any, path string, err error) string {
	var je *jsonx.Error
	if !errors.As(err, &je) || !je.Kind.Is(jsonx.PathNotFound) {
		return fmt.Sprintf("%s: %v", path, err)
	}
	msg := path + " is not in the event's payload"
	if o, ok := doc.(*jsonx.Object); ok && len(o.Keys()) > 0 {
		msg += " (its properties: " + strings.Join(o.Keys(), ", ") + ")"
	}
	return msg
}
