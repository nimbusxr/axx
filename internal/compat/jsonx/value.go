package jsonx

import (
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
)

// The JSON model. A value is one of:
//
//	nil        JSON null
//	bool       java.lang.Boolean
//	string     java.lang.String
//	Number     a JSON number with its Java class (see NumberKind)
//	*Object    java.util.LinkedHashMap (insertion-ordered)
//	*Array     net.minidev.json.JSONArray (what json-smart produces)
//	[]any      java.util.ArrayList (what Jackson produces)
//	KeySet     the key set returned by Jayway's keys() function
//
// Objects and arrays are mutable and shared by reference, as in Java.

// NumberKind is the Java class of a number.
type NumberKind uint8

// Number kinds.
const (
	Integer    NumberKind = iota + 1 // java.lang.Integer
	Long                             // java.lang.Long
	BigInteger                       // java.math.BigInteger
	Double                           // java.lang.Double
	Float                            // java.lang.Float
	BigDecimal                       // java.math.BigDecimal
)

func (k NumberKind) String() string {
	switch k {
	case Integer:
		return "Integer"
	case Long:
		return "Long"
	case BigInteger:
		return "BigInteger"
	case Double:
		return "Double"
	case Float:
		return "Float"
	case BigDecimal:
		return "BigDecimal"
	}
	return "Number"
}

// JavaClassName returns the fully qualified Java class name.
func (k NumberKind) JavaClassName() string {
	switch k {
	case BigInteger, BigDecimal:
		return "java.math." + k.String()
	}
	return "java.lang." + k.String()
}

// Number is a JSON number: its Java class, its value and, when it was parsed,
// the literal text it came from.
type Number struct {
	kind NumberKind
	text string
	i    int64
	f    float64
	bi   *big.Int
	dec  javafmt.BigDecimal
}

// IntegerNumber returns a java.lang.Integer.
func IntegerNumber(v int32) Number { return Number{kind: Integer, i: int64(v)} }

// LongNumber returns a java.lang.Long.
func LongNumber(v int64) Number { return Number{kind: Long, i: v} }

// BigIntegerNumber returns a java.math.BigInteger.
func BigIntegerNumber(v *big.Int) Number {
	return Number{kind: BigInteger, bi: new(big.Int).Set(v)}
}

// DoubleNumber returns a java.lang.Double.
func DoubleNumber(v float64) Number { return Number{kind: Double, f: v} }

// FloatNumber returns a java.lang.Float.
func FloatNumber(v float32) Number { return Number{kind: Float, f: float64(v)} }

// BigDecimalNumber returns a java.math.BigDecimal.
func BigDecimalNumber(v javafmt.BigDecimal) Number { return Number{kind: BigDecimal, dec: v} }

func (n Number) withText(text string) Number {
	n.text = text
	return n
}

// Kind returns the Java class of the number.
func (n Number) Kind() NumberKind { return n.kind }

// Text returns the literal the number was parsed from, or its Java string
// form when it was constructed.
func (n Number) Text() string {
	if n.text != "" {
		return n.text
	}
	return n.String()
}

// String returns Java's toString() of the number (what json-smart writes).
func (n Number) String() string {
	switch n.kind {
	case Integer, Long:
		return strconv.FormatInt(n.i, 10)
	case BigInteger:
		return n.bi.String()
	case Double:
		return javafmt.Double(n.f)
	case Float:
		return javafmt.Float(float32(n.f))
	case BigDecimal:
		return n.dec.String()
	}
	return "0"
}

// JavaString implements javafmt.JavaStringer.
func (n Number) JavaString() string { return n.String() }

// Int64 returns the value of an Integer or Long.
func (n Number) Int64() (int64, bool) {
	if n.kind == Integer || n.kind == Long {
		return n.i, true
	}
	return 0, false
}

// BigInt returns the value of an Integer, Long or BigInteger.
func (n Number) BigInt() (*big.Int, bool) {
	switch n.kind {
	case Integer, Long:
		return big.NewInt(n.i), true
	case BigInteger:
		return new(big.Int).Set(n.bi), true
	}
	return nil, false
}

// Decimal returns the value of a BigDecimal.
func (n Number) Decimal() (javafmt.BigDecimal, bool) {
	return n.dec, n.kind == BigDecimal
}

// Float64 returns Java's Number.doubleValue().
func (n Number) Float64() float64 {
	switch n.kind {
	case Integer, Long:
		return float64(n.i)
	case BigInteger:
		f, _ := new(big.Float).SetInt(n.bi).Float64()
		return f
	case Double, Float:
		return n.f
	case BigDecimal:
		return n.dec.Float64()
	}
	return 0
}

// javaEquals is Java's equals between two boxed numbers: same class and
// same value (Double compares bit patterns, so NaN equals NaN and 0.0 does
// not equal -0.0; BigDecimal also compares scale).
func (n Number) javaEquals(o Number) bool {
	if n.kind != o.kind {
		return false
	}
	switch n.kind {
	case Integer, Long:
		return n.i == o.i
	case BigInteger:
		return n.bi.Cmp(o.bi) == 0
	case Double:
		return canonicalBits(n.f) == canonicalBits(o.f)
	case Float:
		return canonicalBits32(float32(n.f)) == canonicalBits32(float32(o.f))
	case BigDecimal:
		return n.dec.Equal(o.dec)
	}
	return false
}

func canonicalBits(f float64) uint64 {
	if math.IsNaN(f) {
		return 0x7ff8000000000000
	}
	return math.Float64bits(f)
}

func canonicalBits32(f float32) uint32 {
	if f != f {
		return 0x7fc00000
	}
	return math.Float32bits(f)
}

// Object is an insertion-ordered JSON object (java.util.LinkedHashMap).
type Object struct {
	keys []string
	vals map[string]any
}

// NewObject returns an empty object.
func NewObject() *Object { return &Object{vals: map[string]any{}} }

// Len returns the number of members.
func (o *Object) Len() int { return len(o.keys) }

// Keys returns the member names in insertion order.
func (o *Object) Keys() []string { return append([]string(nil), o.keys...) }

// Get returns the member value and whether the member exists.
func (o *Object) Get(key string) (any, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Has reports whether the member exists.
func (o *Object) Has(key string) bool {
	_, ok := o.vals[key]
	return ok
}

// Set adds or replaces a member. Like LinkedHashMap.put, replacing keeps the
// member's position.
func (o *Object) Set(key string, value any) {
	if o.vals == nil {
		o.vals = map[string]any{}
	}
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
}

// Delete removes a member and reports whether it existed.
func (o *Object) Delete(key string) bool {
	if _, ok := o.vals[key]; !ok {
		return false
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
	return true
}

// JavaString renders the object like java.util.AbstractMap.toString:
// "{a=1, b=[1,2]}".
func (o *Object) JavaString() string {
	return javafmt.MapString(o.keys, func(k string) any { return o.vals[k] })
}

// Array is a JSON array as json-smart produces it (net.minidev.json.JSONArray).
type Array struct {
	items []any
}

// NewArray returns an array holding items.
func NewArray(items ...any) *Array { return &Array{items: items} }

// Len returns the number of elements.
func (a *Array) Len() int { return len(a.items) }

// Get returns element i; it panics if i is out of range.
func (a *Array) Get(i int) any { return a.items[i] }

// Set replaces element i; it panics if i is out of range.
func (a *Array) Set(i int, v any) { a.items[i] = v }

// Append adds v at the end.
func (a *Array) Append(v any) { a.items = append(a.items, v) }

// Remove deletes element i; it panics if i is out of range.
func (a *Array) Remove(i int) { a.items = append(a.items[:i], a.items[i+1:]...) }

// Items returns the elements. The slice aliases the array's storage.
func (a *Array) Items() []any { return a.items }

// JavaString renders the array like JSONArray.toString(): compact JSON with
// json-smart's default escaping, which escapes '/' as "\/".
func (a *Array) JavaString() string {
	var sb strings.Builder
	writeValue(&sb, a, styleNoCompress)
	return sb.String()
}

// KeySet is the result of Jayway's keys() function: the key set of an object
// (java.util.LinkedHashMap$LinkedKeySet).
type KeySet []string

// JavaString renders the set like java.util.AbstractCollection.toString.
func (k KeySet) JavaString() string {
	items := make([]any, len(k))
	for i, s := range k {
		items[i] = s
	}
	return javafmt.ListString(items)
}

// JavaClassName returns the fully qualified name of the Java class v stands
// for, or "null" for nil.
func JavaClassName(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "java.lang.String"
	case bool:
		return "java.lang.Boolean"
	case Number:
		return x.kind.JavaClassName()
	case *Object:
		return "java.util.LinkedHashMap"
	case *Array:
		return "net.minidev.json.JSONArray"
	case []any:
		return "java.util.ArrayList"
	case KeySet:
		return "java.util.LinkedHashMap$LinkedKeySet"
	}
	return "java.lang.Object"
}

func isMap(v any) bool {
	_, ok := v.(*Object)
	return ok
}

func isList(v any) bool {
	switch v.(type) {
	case *Array, []any:
		return true
	}
	return false
}

// listItems returns the elements of a list value.
func listItems(v any) []any {
	switch x := v.(type) {
	case *Array:
		return x.items
	case []any:
		return x
	}
	return nil
}

// JavaEquals reports whether Java's a.equals(b) holds for the Java values a
// and b model (with null handled as hamcrest's IsEqual does): numbers must
// have the same class and value, maps compare as sets of entries regardless
// of order, lists compare element by element regardless of list class.
func JavaEquals(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case Number:
		y, ok := b.(Number)
		return ok && x.javaEquals(y)
	case *Object:
		y, ok := b.(*Object)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, k := range x.keys {
			yv, ok := y.vals[k]
			if !ok || !JavaEquals(x.vals[k], yv) {
				return false
			}
		}
		return true
	case *Array, []any:
		if !isList(b) {
			return false
		}
		xs, ys := listItems(a), listItems(b)
		if len(xs) != len(ys) {
			return false
		}
		for i := range xs {
			if !JavaEquals(xs[i], ys[i]) {
				return false
			}
		}
		return true
	case KeySet:
		y, ok := b.(KeySet)
		if !ok || len(x) != len(y) {
			return false
		}
		set := map[string]bool{}
		for _, k := range x {
			set[k] = true
		}
		for _, k := range y {
			if !set[k] {
				return false
			}
		}
		return true
	}
	return false
}
