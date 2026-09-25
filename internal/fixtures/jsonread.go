package fixtures

import (
	"reflect"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

// parseJSON is Jackson's ObjectMapper.readTree: strict JSON with Java number
// typing (Integer, Long or BigInteger by magnitude, Double for decimals).
func parseJSON(data []byte, where string) (any, error) {
	v, err := jvalue.ParseJackson(string(data))
	if err != nil {
		return nil, genError("%s is not valid JSON: %v", where, err)
	}
	return v, nil
}

// toJSONX is the identity: the value model already is the jsonx model.
func toJSONX(v any) any { return v }

// nodeLong is JsonNode.longValue(): numbers convert, anything else is 0.
func nodeLong(v any) int64 {
	if isNumber(v) {
		return longValue(v)
	}
	return 0
}

// nodeDouble is JsonNode.doubleValue().
func nodeDouble(v any) float64 {
	if isNumber(v) {
		return doubleValue(v)
	}
	return 0
}

// plainJSON is the families' plain(JsonNode): integral numbers become Long,
// other numbers Double, containers recurse.
func plainJSON(v any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		out := jsonx.NewObject()
		for _, k := range x.Keys() {
			out.Set(k, plainJSON(get(x, k)))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plainJSON(e)
		}
		return out
	case jsonx.Number:
		switch x.Kind() {
		case jsonx.Integer, jsonx.Long, jsonx.BigInteger:
			return jsonx.LongNumber(longValue(x))
		}
		return jsonx.DoubleNumber(x.Float64())
	}
	return v
}

// plainNode is the families' schema-free passthrough: maps sorted by key,
// Integer and Long as Long, other numbers (BigInteger included) as Double.
func plainNode(v any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		out := jsonx.NewObject()
		for _, k := range sortedKeys(x) {
			out.Set(k, plainNode(get(x, k)))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plainNode(e)
		}
		return out
	case nil, bool:
		return x
	case jsonx.Number:
		if isIntegral(x) {
			return jsonx.LongNumber(longValue(x))
		}
		return jsonx.DoubleNumber(x.Float64())
	}
	return valueOf(v)
}

// fixedBytes extracts the bytes of a fixed-size byte array (the avro library's
// fixed defaults).
func fixedBytes(v any) ([]byte, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Array || rv.Type().Elem().Kind() != reflect.Uint8 {
		return nil, false
	}
	out := make([]byte, rv.Len())
	for i := range out {
		out[i] = byte(rv.Index(i).Uint())
	}
	return out, true
}
