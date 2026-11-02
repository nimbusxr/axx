package jvalue

import (
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// A value a feature sets means what it says (ADR 0014), as one it checks
// does (value.go): it is set as written, whatever the property held before,
// and nothing is inferred from that. The functions in steps.go keep the Java
// steps' readings, which the recorded oracles replay.

// SmartValue is v as a payload json-smart parsed holds it: a string, an
// Integer, Long or BigInteger, a BigDecimal for a decimal, every digit kept
// where a Double would round it, a Boolean, nil for null, or a *jsonx.Object
// or *jsonx.Array. `undefined` is nil too.
func SmartValue(v core.Value) (any, error) {
	if s, ok := v.Str(); ok {
		return s, nil
	}
	if v.IsNull() || v.Undefined() {
		return nil, nil
	}
	x, err := jsonx.Parse(v.JSON())
	if err != nil {
		return nil, err
	}
	return keepDigits(x)
}

// keepDigits replaces the Doubles json-smart read in x by BigDecimals of the
// text they were read from: a Double writes what Java writes for it, which
// rounds 9007199254740993.0 and makes 100000000.0 1.0E8.
func keepDigits(x any) (any, error) {
	switch t := x.(type) {
	case jsonx.Number:
		if t.Kind() != jsonx.Double {
			return t, nil
		}
		d, err := javafmt.ParseBigDecimal(t.Text())
		if err != nil {
			return nil, core.NewValueError(fmt.Sprintf(`%s has an exponent past what a number here can hold: write "%s" for the text`, t.Text(), t.Text()))
		}
		return jsonx.BigDecimalNumber(d), nil
	case *jsonx.Object:
		for _, k := range t.Keys() {
			member, _ := t.Get(k)
			kept, err := keepDigits(member)
			if err != nil {
				return nil, err
			}
			t.Set(k, kept)
		}
	case *jsonx.Array:
		for i, item := range t.Items() {
			kept, err := keepDigits(item)
			if err != nil {
				return nil, err
			}
			t.Set(i, kept)
		}
	}
	return x, nil
}

// SetValue sets the property at path of a request payload to v:
//
//   - `undefined` removes the property; one the payload lacks is already left
//     out, and nothing changes.
//   - Any other value is set as written: `10115` the number, `"10115"` the
//     string and `null` JSON null, over a property of any type.
//   - A property the payload lacks is added to its parent, and a parent the
//     payload lacks is created, as an empty object, when its path is plain
//     names (ADR 0010). A path through a missing array, or an index past an
//     array's end, fails with PathNotFound.
func SetValue(doc any, path string, v core.Value) error {
	if v.Undefined() {
		err := jsonx.Delete(doc, path)
		if isKind(err, jsonx.PathNotFound) {
			return nil
		}
		return notFoundWrap(err)
	}
	x, err := SmartValue(v)
	if err != nil {
		return err
	}
	err = jsonx.Set(doc, path, x)
	if isKind(err, jsonx.PathNotFound) {
		parent, key := jsonx.ParentAndKey(path)
		if strings.ContainsAny(key, "[]") {
			return err // an index has no property of that name to add
		}
		return putCreatingParents(doc, parent, key, x)
	}
	return err
}
