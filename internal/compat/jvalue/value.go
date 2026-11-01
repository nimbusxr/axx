package jvalue

import (
	"fmt"
	"math/big"
	"strconv"

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
	return jsonx.HasJSONPath(doc, path, func(x any) bool { return Equal(x, expected) })
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
		return okx && oky && rx.Cmp(ry) == 0
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
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !Equal(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return JavaEquals(a, b)
}

func isInteger(n jsonx.Number) bool {
	switch n.Kind() {
	case jsonx.Integer, jsonx.Long, jsonx.BigInteger:
		return true
	}
	return false
}

// exact is a number's exact value: a double's is the decimal Java writes
// for it, so 0.1 is a tenth.
func exact(n jsonx.Number) (*big.Rat, bool) {
	if i, ok := n.BigInt(); ok {
		return new(big.Rat).SetInt(i), true
	}
	if d, ok := n.Decimal(); ok {
		return new(big.Rat).SetString(d.String())
	}
	return new(big.Rat).SetString(strconv.FormatFloat(n.Float64(), 'g', -1, 64))
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
			if en, ok := e.(jsonx.Number); ok && isInteger(en) != isInteger(n) {
				if re, ok1 := exact(en); ok1 {
					if rn, ok2 := exact(n); ok2 && re.Cmp(rn) == 0 {
						return fmt.Sprintf("%s is %s %s: write %s", path, kindOf(actual), text, text)
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
