// Package jsonassert implements the JSON property assertions shared by the
// packs: JSONPath lookups with Jayway semantics and text comparison of
// scalars.
package jsonassert

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

// Properties asserts each path/value row of t against the JSON document.
// Every scalar is compared as text; `null` means JSON null and `undefined`
// means the path is absent. With regex, values are Java regular
// expressions that must match the whole text. All failing rows are
// reported together, like Hamcrest's allOf.
func Properties(doc string, t *core.Table, regex bool) error {
	pairs, err := t.Pairs()
	if err != nil {
		return err
	}
	// The text view of the document (every scalar as a string), which is
	// what the expectations compare against.
	text, err := jvalue.StringifyJSON(doc)
	if err != nil {
		return couldNot(err)
	}
	var failed []string
	expected := map[string]any{}
	actual := map[string]any{}
	for _, p := range pairs {
		want := p.Value
		if p.Null {
			want = "null" // an empty cell is Java null: equalTo(null)
		}
		var ok bool
		if regex {
			if p.Null {
				return couldNot(fmt.Errorf("the pattern for %s is empty", p.Key))
			}
			ok, err = jvalue.PostgresPropertyMatches(doc, p.Key, want)
		} else {
			ok, err = jvalue.PostgresPropertyIs(doc, p.Key, want)
		}
		if err != nil {
			return unwrap(err)
		}
		if !ok {
			failed = append(failed, p.Key)
			expected[p.Key] = describeExpected(want, regex)
			actual[p.Key] = describeActual(text, p.Key)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	verb := "have"
	if regex {
		verb = "match"
	}
	return core.Fail(fmt.Sprintf("JSON properties do not %s the expected values: %s", verb, strings.Join(failed, ", ")), expected, actual)
}

func describeExpected(want string, regex bool) any {
	switch {
	case regex:
		return "matches " + want
	case strings.EqualFold(want, "undefined"):
		return "<absent>"
	case strings.EqualFold(want, "null"):
		return nil
	}
	return want
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
