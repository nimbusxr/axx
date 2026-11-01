// Package jsonassert implements the JSON property assertions shared by the
// packs: JSONPath lookups with Jayway semantics, and values that mean what
// they say (ADR 0014), compared with their types.
package jsonassert

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

// Properties asserts each path/value row of t against the JSON document.
// A value is read by the rule every pack follows (core.ParseValue: `42` an
// integer, `"42"` the string, `null`, `undefined` for no such property) and
// compared with its type, strictly. With regex, values are Java regular
// expressions that must match the whole text of the value. All failing rows
// are reported together, like Hamcrest's allOf.
func Properties(doc string, t *core.Table, regex bool) error {
	pairs, err := t.Pairs()
	if err != nil {
		return err
	}
	if regex {
		return patterns(doc, pairs)
	}
	if _, err := jvalue.ParseJackson(doc); err != nil {
		return couldNot(err)
	}
	var failed, hints []string
	expected := map[string]any{}
	actual := map[string]any{}
	for _, p := range pairs {
		v, err := core.ParseValue(p.Value, p.Null, true)
		if err != nil {
			return fmt.Errorf("%s: %w", p.Key, err)
		}
		ok, err := jvalue.ValueIs(doc, p.Key, v)
		if err != nil {
			return unwrap(err)
		}
		if ok {
			continue
		}
		failed = append(failed, p.Key)
		expected[p.Key] = shownValue(v)
		got, value, found := valueAt(doc, p.Key)
		actual[p.Key] = got
		if found {
			if h := jvalue.TypeHint(p.Key, v, value); h != "" {
				hints = append(hints, h)
			}
		}
	}
	if len(failed) == 0 {
		return nil
	}
	msg := fmt.Sprintf("JSON properties do not have the expected values: %s", strings.Join(failed, ", "))
	if len(hints) > 0 {
		msg += " (" + strings.Join(hints, "; ") + ")"
	}
	return core.Fail(msg, expected, actual)
}

// patterns asserts each path/pattern row: the value's text must fully match.
func patterns(doc string, pairs []core.Pair) error {
	text, err := jvalue.StringifyJSON(doc)
	if err != nil {
		return couldNot(err)
	}
	var failed []string
	expected := map[string]any{}
	actual := map[string]any{}
	for _, p := range pairs {
		if p.Null {
			return couldNot(fmt.Errorf("the pattern for %s is empty", p.Key))
		}
		ok, err := jvalue.PostgresPropertyMatches(doc, p.Key, p.Value)
		if err != nil {
			return unwrap(err)
		}
		if !ok {
			failed = append(failed, p.Key)
			expected[p.Key] = "matches " + p.Value
			actual[p.Key] = describeActual(text, p.Key)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return core.Fail(fmt.Sprintf("JSON properties do not match the expected values: %s", strings.Join(failed, ", ")), expected, actual)
}

// rawJSON is a value's JSON text in a failure: reporters print it as it is,
// and --json output has the JSON value itself.
type rawJSON string

func (r rawJSON) String() string { return string(r) }

func (r rawJSON) MarshalJSON() ([]byte, error) {
	if json.Valid([]byte(r)) {
		return []byte(r), nil
	}
	return json.Marshal(string(r))
}

func shownValue(v core.Value) any {
	if v.Undefined() {
		return "<absent>"
	}
	return rawJSON(v.JSON())
}

// valueAt is the value at path, as a failure shows it, and as it is.
func valueAt(doc, path string) (shown, value any, found bool) {
	d, err := jsonx.Parse(doc)
	if err != nil {
		return "<unreadable>", nil, false
	}
	v, _, err := jsonx.Read(d, path)
	if err != nil {
		return "<absent>", nil, false
	}
	return rawJSON(jsonx.MarshalValue(v)), v, true
}

func describeActual(text, path string) any {
	doc, err := jsonx.Parse(text)
	if err != nil {
		return "<unreadable>"
	}
	v, _, err := jsonx.Read(doc, path)
	if err != nil {
		return "<absent>"
	}
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		return x
	}
	return jsonx.MarshalValue(v)
}

// unwrap keeps the message ("Could not perform selection") and adds the
// cause.
func unwrap(err error) error {
	var se *jvalue.StepError
	if errors.As(err, &se) && se.Cause != nil {
		return fmt.Errorf("%s: %w", se.Message, se.Cause)
	}
	return err
}

func couldNot(err error) error {
	return fmt.Errorf("Could not perform selection: %w", err) //nolint:staticcheck // user-facing message
}
