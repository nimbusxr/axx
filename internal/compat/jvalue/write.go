package jvalue

import (
	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// A value a feature sets means what it says (ADR 0014), as one it checks
// does (value.go): it is set as written, whatever the property held before,
// and nothing is inferred from that. The functions in steps.go keep the Java
// steps' readings, which the recorded oracles replay.

// SmartValue is v as a payload json-smart parsed holds it: a string, an
// Integer, Long, BigInteger or Double, a Boolean, nil for null, or a
// *jsonx.Object or *jsonx.Array. `undefined` is nil too.
func SmartValue(v core.Value) (any, error) {
	if s, ok := v.Str(); ok {
		return s, nil
	}
	if v.IsNull() || v.Undefined() {
		return nil, nil
	}
	return jsonx.Parse(v.JSON())
}

// SetValue sets the property at path of a request payload to v:
//
//   - `undefined` removes the property, and `null` sets it to JSON null:
//     both change a property the payload has, as DeleteRequestProperty and
//     SetRequestPropertyNull do.
//   - Any other value is set as written: `10115` the number, `"10115"` the
//     string, over a property of any type.
//   - A property the payload lacks is added to its parent, and a parent the
//     payload lacks is created, as an empty object, when its path is plain
//     names (ADR 0010); a path through a missing array fails with
//     PathNotFound.
func SetValue(doc any, path string, v core.Value) error {
	switch {
	case v.Undefined():
		return DeleteRequestProperty(doc, path)
	case v.IsNull():
		return SetRequestPropertyNull(doc, path)
	}
	x, err := SmartValue(v)
	if err != nil {
		return err
	}
	err = jsonx.Set(doc, path, x)
	if isKind(err, jsonx.PathNotFound) {
		parent, key := jsonx.ParentAndKey(path)
		return putCreatingParents(doc, parent, key, x)
	}
	return err
}
