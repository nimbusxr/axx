package fixtures

import (
	"bytes"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// Values follow the Java value model (see package jsonx):
// *jsonx.Object is a LinkedHashMap, []any an ArrayList, jsonx.Number a boxed
// number with its Java class, plus string, bool, nil and []byte (YAML
// !!binary). Keeping the Java types matters: rendering (Double.toString),
// equality (Integer 1 is not Long 1) and family rules (an int field accepts
// Integer and Long only) all depend on them.

// newObject builds an object from alternating keys and values.
func newObject(kv ...any) *jsonx.Object {
	o := jsonx.NewObject()
	for i := 0; i+1 < len(kv); i += 2 {
		o.Set(kv[i].(string), kv[i+1])
	}
	return o
}

// get returns the member of an object, or nil.
func get(o *jsonx.Object, key string) any {
	if o == nil {
		return nil
	}
	v, _ := o.Get(key)
	return v
}

// has reports whether an object holds key (even with a null value).
func has(o *jsonx.Object, key string) bool {
	return o != nil && o.Has(key)
}

// keys returns an object's keys (nil-safe).
func keys(o *jsonx.Object) []string {
	if o == nil {
		return nil
	}
	return o.Keys()
}

// deepMerge is ValuePaths.deepMerge: maps merge recursively, everything else
// (lists, scalars) replaces wholesale. Neither input is modified.
func deepMerge(base, overlay *jsonx.Object) *jsonx.Object {
	out := jsonx.NewObject()
	for _, k := range keys(base) {
		out.Set(k, copyValue(get(base, k)))
	}
	for _, k := range keys(overlay) {
		v := get(overlay, k)
		existing, _ := out.Get(k)
		e, eok := existing.(*jsonx.Object)
		o, ook := v.(*jsonx.Object)
		if eok && ook {
			out.Set(k, deepMerge(e, o))
		} else {
			out.Set(k, copyValue(v))
		}
	}
	return out
}

// copyValue deep-copies maps and lists.
func copyValue(v any) any {
	switch x := v.(type) {
	case *jsonx.Object:
		out := jsonx.NewObject()
		for _, k := range x.Keys() {
			out.Set(k, copyValue(get(x, k)))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = copyValue(e)
		}
		return out
	}
	return v
}

// valueOf is String.valueOf for the value model.
func valueOf(v any) string {
	switch x := v.(type) {
	case *jsonx.Object:
		return javafmt.MapString(x.Keys(), func(k string) any { return get(x, k) })
	case []any:
		items := make([]string, len(x))
		for i, e := range x {
			items[i] = valueOf(e)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case []byte:
		return "[B@" + javafmt.ValueOf(len(x))
	}
	return javafmt.ValueOf(v)
}

// deepEquals is Java's equals over the value model, with byte arrays
// compared by content (the adoption round trip's deepEquals).
func deepEquals(a, b any) bool {
	switch x := a.(type) {
	case protoValue:
		eq, err := protoEquals(x, b)
		return err == nil && eq
	case []byte:
		y, ok := b.([]byte)
		return ok && bytes.Equal(x, y)
	case *jsonx.Object:
		y, ok := b.(*jsonx.Object)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, k := range x.Keys() {
			if !y.Has(k) || !deepEquals(get(x, k), get(y, k)) {
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
			if !deepEquals(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any: // decoded Avro records and maps
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok || !deepEquals(v, w) {
				return false
			}
		}
		return true
	case nil, string, bool, jsonx.Number:
		return jsonx.JavaEquals(a, b)
	}
	// Other decoded native values (Avro ints, floats, fixed arrays).
	return reflect.DeepEqual(a, b)
}

// javaSimpleName is getClass().getSimpleName() for the value model.
func javaSimpleName(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "String"
	case bool:
		return "Boolean"
	case jsonx.Number:
		return x.Kind().String()
	case *jsonx.Object:
		return "LinkedHashMap"
	case []any:
		return "ArrayList"
	case []byte:
		return "byte[]"
	}
	return "Object"
}

// describe renders a value with its type.
func describe(v any) string {
	if v == nil {
		return "null"
	}
	return javaSimpleName(v) + " " + valueOf(v)
}

// Number helpers.

func isNumber(v any) bool {
	_, ok := v.(jsonx.Number)
	return ok
}

// isIntegral reports whether v is an Integer or a Long (instanceof Integer
// || instanceof Long).
func isIntegral(v any) bool {
	n, ok := v.(jsonx.Number)
	return ok && (n.Kind() == jsonx.Integer || n.Kind() == jsonx.Long)
}

// longValue is Number.longValue().
func longValue(v any) int64 {
	n := v.(jsonx.Number)
	switch n.Kind() {
	case jsonx.Integer, jsonx.Long:
		i, _ := n.Int64()
		return i
	case jsonx.BigInteger:
		b, _ := n.BigInt()
		return lowBits(b)
	case jsonx.BigDecimal:
		d, _ := n.Decimal()
		return d2l(d.Float64())
	}
	return d2l(n.Float64())
}

// d2l is Java's (long) cast of a double.
func d2l(f float64) int64 {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= 9.223372036854775807e18:
		return math.MaxInt64
	case f <= -9.223372036854775808e18:
		return math.MinInt64
	}
	return int64(f)
}

func lowBits(b *big.Int) int64 {
	u := new(big.Int).And(b, new(big.Int).SetUint64(math.MaxUint64))
	return int64(u.Uint64())
}

// doubleValue is Number.doubleValue().
func doubleValue(v any) float64 { return v.(jsonx.Number).Float64() }

func longNode(v int64) jsonx.Number { return jsonx.LongNumber(v) }

func itoa64(v int64) string             { return strconv.FormatInt(v, 10) }
func doubleNode(v float64) jsonx.Number { return jsonx.DoubleNumber(v) }

// javaCompare is String.compareTo: lexicographic over UTF-16 code units.
func javaCompare(a, b string) int {
	if isASCII(a) && isASCII(b) {
		return strings.Compare(a, b)
	}
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return int(ua[i]) - int(ub[i])
		}
	}
	return len(ua) - len(ub)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// sortedKeys returns the keys in Java String order (a TreeMap's order).
func sortedKeys(o *jsonx.Object) []string {
	ks := keys(o)
	sortStrings(ks)
	return ks
}

func sortStrings(s []string) {
	sort.SliceStable(s, func(i, j int) bool { return javaCompare(s[i], s[j]) < 0 })
}

// javaListString renders a list of strings like List.toString.
func javaListString(items []string) string {
	return "[" + strings.Join(items, ", ") + "]"
}

// javaSetString renders keys like a key set's toString.
func javaSetString(o *jsonx.Object) string { return javaListString(keys(o)) }
