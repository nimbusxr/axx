package jvalue

import (
	"fmt"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// A value a feature writes means what it says, in every pack (ADR 0014):
// core.ParseValue reads it, and these functions give it, and compare it, in
// the compat model, as REST's checks always have (CoerceExpected).

// Expected is the value v in the compat model: a string, an Integer or a
// Long, a Double, a Boolean, nil for null, a map or a list. It is
// CoerceExpected of the value as written, which the rule agrees with for
// every value it accepts.
func Expected(v core.Value) (any, error) {
	if v.IsNull() || v.Undefined() {
		return nil, nil
	}
	return CoerceExpected(v.Text())
}

// ValueIs is one row of a check that a JSON document has v at path: v is
// compared with its type, strictly (JavaEquals: an Integer is not a Double,
// nor a string a number); `undefined` checks there is no such property and
// `null` that it is null.
func ValueIs(doc, path string, v core.Value) (bool, error) {
	switch {
	case v.Undefined():
		return jsonx.HasNoJSONPath(doc, path)
	case v.IsNull():
		return jsonx.HasJSONPath(doc, path, func(x any) bool { return x == nil })
	}
	expected, err := Expected(v)
	if err != nil {
		return false, err
	}
	return jsonx.HasJSONPath(doc, path, func(x any) bool { return JavaEquals(x, expected) })
}

// TypeHint says what to write when a value matched the actual one but for
// its type: `postcode is the string "53111": write "53111"`. It is empty
// when the values differ otherwise.
func TypeHint(path string, v core.Value, actual any) string {
	if v.Undefined() || v.IsNull() {
		return ""
	}
	if s, ok := actual.(string); ok {
		if _, isString := v.Str(); !isString && s == v.Text() {
			return fmt.Sprintf("%s is the string %s: write %s", path, jsonx.MarshalValue(s), jsonx.MarshalValue(s))
		}
		return ""
	}
	text := jsonx.MarshalValue(actual)
	if s, isString := v.Str(); isString && s == text {
		return fmt.Sprintf("%s is %s %s: write %s", path, kindOf(actual), text, text)
	}
	if n, ok := actual.(jsonx.Number); ok {
		if e, err := Expected(v); err == nil {
			if en, ok := e.(jsonx.Number); ok && en.Float64() == n.Float64() && en.Kind() != n.Kind() {
				return fmt.Sprintf("%s is %s %s: write %s", path, kindOf(actual), text, text)
			}
		}
	}
	return ""
}

func kindOf(v any) string {
	switch x := v.(type) {
	case bool:
		return "the boolean"
	case jsonx.Number:
		switch x.Kind() {
		case jsonx.Integer, jsonx.Long, jsonx.BigInteger:
			return "the integer"
		}
		return "the decimal"
	}
	return "the value"
}
