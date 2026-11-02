package jvalue

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// A value a feature writes means what it says, in every pack (ADR 0014):
// core.ParseValue reads it, as REST's checks always have (CoerceExpected),
// and these functions give it, and compare it, in the compat model.

// Expected is the value v in the compat model, read as a check reads a
// document (json-smart): a string, a number, a Boolean, nil for null, an
// object or a list.
func Expected(v core.Value) (any, error) {
	if v.IsNull() || v.Undefined() {
		return nil, nil
	}
	return jsonx.Parse(v.JSON())
}

// ValueIs is one row of a check that a JSON document has v at path: v is
// compared with its type, strictly (Equal: an integer is not a decimal, nor
// a string a number); `undefined` checks there is no such property and
// `null` that it is null. A document of null is a value too: `$` is null, and
// any path under it is absent.
func ValueIs(doc, path string, v core.Value) (bool, error) {
	if NullDocument(doc) {
		if _, err := jsonx.Compile(path); err != nil {
			return false, err
		}
		if path == "$" {
			return v.IsNull(), nil
		}
		return v.Undefined(), nil
	}
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
	return jsonx.HasJSONPath(doc, path, func(x any) bool { return Equal(x, expected) })
}

// NullDocument says the JSON document is null, which json-smart refuses to
// read.
func NullDocument(doc string) bool {
	return strings.TrimSpace(doc) == "null"
}

// Equal is the rule's equality of two values of the compat model: the same
// type and the same value. A number is an integer or a decimal, whichever
// Java class holds it (an Integer, a Long and a BigInteger are integers, a
// Double and a BigDecimal decimals), and compares by its exact value; an
// object's members compare regardless of order, a list's in order.
func Equal(a, b any) bool {
	switch x := a.(type) {
	case jsonx.Number:
		y, ok := b.(jsonx.Number)
		if !ok || isInteger(x) != isInteger(y) {
			return false
		}
		rx, okx := exact(x)
		ry, oky := exact(y)
		return okx && oky && rx == ry
	case *jsonx.Object:
		y, ok := b.(*jsonx.Object)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, k := range x.Keys() {
			xv, _ := x.Get(k)
			yv, ok := y.Get(k)
			if !ok || !Equal(xv, yv) {
				return false
			}
		}
		return true
	case *jsonx.Array, []any:
		xs, _ := items(x)
		ys, ok := items(b)
		if !ok || len(xs) != len(ys) {
			return false
		}
		for i := range xs {
			if !Equal(xs[i], ys[i]) {
				return false
			}
		}
		return true
	}
	return JavaEquals(a, b)
}

// items are the elements of a list, as json-smart or Jackson holds it.
func items(v any) ([]any, bool) {
	switch x := v.(type) {
	case *jsonx.Array:
		return x.Items(), true
	case []any:
		return x, true
	}
	return nil, false
}

func isInteger(n jsonx.Number) bool {
	switch n.Kind() {
	case jsonx.Integer, jsonx.Long, jsonx.BigInteger:
		return true
	}
	return false
}

// exact is a number's exact value, from its text, not the double that holds
// it: the digits it was written with, or, for a double it was not read from,
// those Java writes for it, so 0.1 is a tenth.
func exact(n jsonx.Number) (string, bool) {
	if i, ok := n.BigInt(); ok {
		return exactText(i.String())
	}
	return exactText(n.Text())
}

var numberText = regexp.MustCompile(`^(-?)([0-9]*)\.?([0-9]*)(?:[eE]([-+]?[0-9]+))?$`)

// exactText is a number's text in the one form all its notations share: its
// significant digits and the power of ten that scales them, so 1.50, 15e-1
// and 0.15E1 are the same, whatever the exponent's size, with nothing
// computed but the exponent.
func exactText(text string) (string, bool) {
	m := numberText.FindStringSubmatch(text)
	if m == nil || m[2]+m[3] == "" {
		return "", false
	}
	significant := strings.TrimLeft(m[2]+m[3], "0")
	if significant == "" {
		return "0", true
	}
	exponent := new(big.Int)
	if m[4] != "" {
		exponent.SetString(m[4], 10)
	}
	digits := strings.TrimRight(significant, "0")
	exponent.Add(exponent, big.NewInt(int64(len(significant)-len(digits)-len(m[3]))))
	return m[1] + digits + "e" + exponent.String(), true
}

// TypeHint says what to write in a table's cell when a value matched the
// actual one but for its type: `postcode is the string "53111": write
// "53111"`. It is empty when the values differ otherwise.
func TypeHint(path string, v core.Value, actual any) string {
	return typeHint(path, v, actual, func(form string) string { return form })
}

// StepTypeHint is TypeHint for a value in a step's own text, whose quotes are
// the step's: `postcode is the string "53111": write '"53111"'`.
func StepTypeHint(path string, v core.Value, actual any) string {
	return typeHint(path, v, actual, core.StepText)
}

func typeHint(path string, v core.Value, actual any, write func(string) string) string {
	if v.Undefined() || v.IsNull() {
		return ""
	}
	if s, ok := actual.(string); ok {
		if _, isString := v.Str(); !isString && s == v.Text() {
			return fmt.Sprintf("%s is the string %s: write %s", path, jsonx.MarshalValue(s), write(jsonx.MarshalValue(s)))
		}
		return ""
	}
	text := jsonx.MarshalValue(actual)
	n, isNumber := actual.(jsonx.Number)
	if isNumber {
		text = n.Text() // as the document has it, every digit
	}
	if s, isString := v.Str(); isString && s == text {
		return fmt.Sprintf("%s is %s %s: write %s", path, kindOf(actual), text, write(text))
	}
	if isNumber {
		if e, err := Expected(v); err == nil {
			if en, ok := e.(jsonx.Number); ok && isInteger(en) != isInteger(n) {
				if re, ok1 := exact(en); ok1 {
					if rn, ok2 := exact(n); ok2 && re == rn {
						return fmt.Sprintf("%s is %s %s: write %s", path, kindOf(actual), text, write(text))
					}
				}
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
