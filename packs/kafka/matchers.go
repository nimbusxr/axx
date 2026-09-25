package kafka

import (
	"fmt"
	"strconv"

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
	expected any
}

type headerMatcher struct {
	Key   string
	Value string
	re    *javare.Regexp
}

// newPayloadMatcher is hasJsonPath(path, equalToObject(coerced value)):
// the value is coerced (and the path compiled) when the step runs, so a bad
// value fails the step at once.
func newPayloadMatcher(path, value string) (payloadMatcher, error) {
	expected, err := jvalue.CoerceKafkaExpected(value)
	if err != nil {
		return payloadMatcher{}, fmt.Errorf("value %q for %s: %w", value, path, err)
	}
	if _, err := jsonx.Compile(path); err != nil {
		return payloadMatcher{}, fmt.Errorf("invalid JSONPath %q: %w", path, err)
	}
	return payloadMatcher{Path: path, Raw: value, expected: expected}, nil
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
			v, _, err := jsonx.Read(doc, p.Path)
			if err != nil {
				return false, fmt.Sprintf("%s: %v", p.Path, err)
			}
			if !jvalue.JavaEquals(v, p.expected) {
				return false, fmt.Sprintf("%s is %s, expected %s", p.Path, describeJSON(v), p.Raw)
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
