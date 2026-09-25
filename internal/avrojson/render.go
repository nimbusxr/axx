package avrojson

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/iskorotkov/avro/v2"

	"github.com/nimbusxr/axx/internal/compat/javafmt"
)

// Render formats a native value like Java GenericData.toString (field order,
// quoting, numbers via javafmt, bytes, maps, unions without wrappers, enums,
// fixed, logical types).
func Render(s avro.Schema, v any) string {
	var b strings.Builder
	(&renderer{b: &b}).value(s, v)
	return b.String()
}

// ObjectString formats a top-level value like Java's Object.toString() of the
// datum a GenericDatumReader (or the Confluent Avro deserializer) returns.
// For a record that is Render; otherwise Java prints the plain value: a
// string or enum symbol unquoted, a map as {key=value, ...}, an array as
// [a, b], fixed as a signed byte list and bytes as the ByteBuffer's
// description.
func ObjectString(s avro.Schema, v any) string {
	var b strings.Builder
	(&renderer{b: &b}).object(s, v)
	return b.String()
}

type renderer struct {
	b *strings.Builder
}

func (r *renderer) value(s avro.Schema, v any) {
	if v == nil {
		r.b.WriteString("null")
		return
	}
	if s == nil {
		r.untyped(v)
		return
	}
	s = deref(s)
	switch s.Type() {
	case avro.Union:
		br, inner := resolveUnion(s.(*avro.UnionSchema), v)
		r.value(br, inner)
	case avro.Record:
		get, ok := fieldGetter(v)
		if !ok {
			r.untyped(v)
			return
		}
		r.b.WriteByte('{')
		for i, f := range s.(*avro.RecordSchema).Fields() {
			if i > 0 {
				r.b.WriteString(", ")
			}
			r.quoted(f.Name())
			r.b.WriteString(": ")
			r.value(f.Type(), get(f.Name()))
		}
		r.b.WriteByte('}')
	case avro.Map:
		entries, ok := orderedEntries(asStringMap(v), !javaStringKeys(s))
		if !ok {
			r.untyped(v)
			return
		}
		values := s.(*avro.MapSchema).Values()
		r.b.WriteByte('{')
		for i, e := range entries {
			if i > 0 {
				r.b.WriteString(", ")
			}
			r.quoted(e.Key)
			r.b.WriteString(": ")
			r.value(values, e.Value)
		}
		r.b.WriteByte('}')
	case avro.Array:
		items, ok := sliceItems(v)
		if !ok {
			r.untyped(v)
			return
		}
		r.list(items, func(it any) { r.value(s.(*avro.ArraySchema).Items(), it) })
	case avro.Fixed:
		b, ok := fixedBytes(s.(*avro.FixedSchema), v)
		if !ok {
			r.untyped(v)
			return
		}
		r.signedBytes(b)
	case avro.Bytes:
		b, ok := bytesOf(s, v)
		if !ok {
			r.untyped(v)
			return
		}
		r.quoted(latin1String(b))
	case avro.String, avro.Enum:
		if str, ok := v.(string); ok {
			r.quoted(str)
			return
		}
		r.untyped(v)
	case avro.Int, avro.Long:
		n, ok := integerOf(s, v)
		if !ok {
			r.untyped(v)
			return
		}
		r.b.WriteString(strconv.FormatInt(n, 10))
	case avro.Float:
		switch f := v.(type) {
		case float32:
			r.float32(f)
		case float64:
			r.float32(float32(f))
		default:
			r.untyped(v)
		}
	case avro.Double:
		switch f := v.(type) {
		case float64:
			r.float64(f)
		case float32:
			r.float64(float64(f))
		default:
			r.untyped(v)
		}
	default: // boolean, null
		r.untyped(v)
	}
}

// untyped renders a value by its Go type, as GenericData.toString renders
// by the datum's Java class.
func (r *renderer) untyped(v any) {
	switch x := v.(type) {
	case nil:
		r.b.WriteString("null")
	case bool:
		r.b.WriteString(strconv.FormatBool(x))
	case string:
		r.quoted(x)
	case []byte:
		r.quoted(latin1String(x))
	case float32:
		r.float32(x)
	case float64:
		r.float64(x)
	case OrderedMap, map[string]any:
		entries, _ := orderedEntries(x, true)
		r.b.WriteByte('{')
		for i, e := range entries {
			if i > 0 {
				r.b.WriteString(", ")
			}
			r.quoted(e.Key)
			r.b.WriteString(": ")
			r.untyped(e.Value)
		}
		r.b.WriteByte('}')
	case time.Time:
		r.b.WriteString(strconv.FormatInt(x.UnixMilli(), 10))
	case time.Duration:
		r.b.WriteString(strconv.FormatInt(x.Milliseconds(), 10))
	default:
		if n, ok := asInt64(v); ok {
			r.b.WriteString(strconv.FormatInt(n, 10))
			return
		}
		if b, ok := byteArray(v); ok {
			r.signedBytes(b)
			return
		}
		if items, ok := sliceItems(v); ok {
			r.list(items, r.untyped)
			return
		}
		if m := asStringMap(v); m != nil {
			r.untyped(m)
			return
		}
		r.quoted(fmt.Sprint(v))
	}
}

func (r *renderer) list(items []any, each func(any)) {
	r.b.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			r.b.WriteString(", ")
		}
		each(it)
	}
	r.b.WriteByte(']')
}

// signedBytes is GenericData.Fixed.toString: Arrays.toString of the bytes.
func (r *renderer) signedBytes(b []byte) {
	r.b.WriteByte('[')
	for i, c := range b {
		if i > 0 {
			r.b.WriteString(", ")
		}
		r.b.WriteString(strconv.Itoa(int(int8(c))))
	}
	r.b.WriteByte(']')
}

func (r *renderer) float32(f float32) {
	s := javafmt.Float(f)
	if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
		s = `"` + s + `"`
	}
	r.b.WriteString(s)
}

func (r *renderer) float64(f float64) {
	s := javafmt.Double(f)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		s = `"` + s + `"`
	}
	r.b.WriteString(s)
}

// quoted writes s in double quotes with GenericData's writeEscapedString:
// the usual JSON escapes, and \uXXXX (upper-case hex) for U+0000-U+001F,
// U+007F-U+009F and U+2000-U+20FF.
func (r *renderer) quoted(s string) {
	r.b.WriteByte('"')
	for _, u := range s { // characters above U+FFFF are never escaped
		switch u {
		case '"':
			r.b.WriteString(`\"`)
		case '\\':
			r.b.WriteString(`\\`)
		case '\b':
			r.b.WriteString(`\b`)
		case '\f':
			r.b.WriteString(`\f`)
		case '\n':
			r.b.WriteString(`\n`)
		case '\r':
			r.b.WriteString(`\r`)
		case '\t':
			r.b.WriteString(`\t`)
		default:
			if u <= 0x1F || u >= 0x7F && u <= 0x9F || u >= 0x2000 && u <= 0x20FF {
				fmt.Fprintf(r.b, `\u%04X`, u)
				continue
			}
			r.b.WriteRune(u)
		}
	}
	r.b.WriteByte('"')
}

// object is ObjectString.
func (r *renderer) object(s avro.Schema, v any) {
	if v == nil {
		r.b.WriteString("null")
		return
	}
	if s == nil {
		r.value(nil, v)
		return
	}
	s = deref(s)
	switch s.Type() {
	case avro.Union:
		br, inner := resolveUnion(s.(*avro.UnionSchema), v)
		r.object(br, inner)
	case avro.String, avro.Enum:
		if str, ok := v.(string); ok {
			r.b.WriteString(str)
			return
		}
		r.value(s, v)
	case avro.Bytes:
		if b, ok := bytesOf(s, v); ok {
			fmt.Fprintf(r.b, "java.nio.HeapByteBuffer[pos=0 lim=%d cap=%d]", len(b), len(b))
			return
		}
		r.value(s, v)
	case avro.Float:
		if f, ok := v.(float32); ok {
			r.b.WriteString(javafmt.Float(f))
			return
		}
		r.value(s, v)
	case avro.Double:
		if f, ok := v.(float64); ok {
			r.b.WriteString(javafmt.Double(f))
			return
		}
		r.value(s, v)
	case avro.Array:
		items, ok := sliceItems(v)
		if !ok {
			r.value(s, v)
			return
		}
		r.list(items, func(it any) { r.object(s.(*avro.ArraySchema).Items(), it) })
	case avro.Map:
		entries, ok := orderedEntries(asStringMap(v), !javaStringKeys(s))
		if !ok {
			r.value(s, v)
			return
		}
		r.b.WriteByte('{')
		for i, e := range entries {
			if i > 0 {
				r.b.WriteString(", ")
			}
			r.b.WriteString(e.Key)
			r.b.WriteByte('=')
			r.object(s.(*avro.MapSchema).Values(), e.Value)
		}
		r.b.WriteByte('}')
	default: // record, fixed, numbers, boolean
		r.value(s, v)
	}
}

// resolveUnion picks the union branch of v: the branch named by a
// single-entry wrapper map, the null branch for nil, or else the first
// branch that accepts v's Go type.
func resolveUnion(u *avro.UnionSchema, v any) (avro.Schema, any) {
	if m, ok := v.(map[string]any); ok && len(m) == 1 {
		for k, inner := range m {
			if br := findBranch(u, k); br != nil {
				return br, inner
			}
		}
	}
	if v == nil {
		return nil, nil
	}
	for _, strict := range []bool{true, false} {
		for _, t := range u.Types() {
			if accepts(deref(t), v, strict) {
				return t, v
			}
		}
	}
	return nil, v
}

// accepts reports whether v's Go type suits schema s (strict: exactly; not
// strict: after a widening conversion).
func accepts(s avro.Schema, v any, strict bool) bool {
	lt := logicalType(s)
	switch s.Type() {
	case avro.Null:
		return v == nil
	case avro.Boolean:
		_, ok := v.(bool)
		return ok
	case avro.Int:
		switch v.(type) {
		case int, int8, int16, int32, uint8, uint16:
			return true
		case time.Time:
			return lt == avro.Date
		case time.Duration:
			return lt == avro.TimeMillis
		case int64:
			return !strict
		}
	case avro.Long:
		switch v.(type) {
		case int64, uint32:
			return true
		case int, int32:
			return !strict
		case time.Time:
			return lt == avro.TimestampMillis || lt == avro.TimestampMicros ||
				lt == avro.LocalTimestampMillis || lt == avro.LocalTimestampMicros
		case time.Duration:
			return lt == avro.TimeMicros
		}
	case avro.Float:
		switch v.(type) {
		case float32:
			return true
		case float64:
			return !strict
		}
	case avro.Double:
		switch v.(type) {
		case float64:
			return true
		case float32:
			return !strict
		}
	case avro.String:
		_, ok := v.(string)
		return ok
	case avro.Enum:
		str, ok := v.(string)
		if !ok {
			return false
		}
		for _, sym := range s.(*avro.EnumSchema).Symbols() {
			if sym == str {
				return true
			}
		}
		return !strict
	case avro.Bytes:
		switch v.(type) {
		case []byte:
			return true
		case *big.Rat:
			return lt == avro.Decimal
		}
	case avro.Fixed:
		fs := s.(*avro.FixedSchema)
		if b, ok := byteArray(v); ok {
			return len(b) == fs.Size()
		}
		switch x := v.(type) {
		case []byte:
			return !strict && len(x) == fs.Size()
		case avro.LogicalDuration:
			return lt == avro.Duration
		case *big.Rat:
			return lt == avro.Decimal
		}
	case avro.Array:
		_, ok := sliceItems(v)
		return ok
	case avro.Map:
		switch v.(type) {
		case OrderedMap:
			return true
		case map[string]any:
			return !strict
		}
		return asStringMap(v) != nil && !strict
	case avro.Record:
		m, ok := v.(map[string]any)
		if !ok {
			return false
		}
		if !strict {
			return true
		}
		for _, f := range s.(*avro.RecordSchema).Fields() {
			if _, ok := m[f.Name()]; !ok {
				return false
			}
		}
		return true
	}
	return false
}

func fieldGetter(v any) (func(string) any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return func(k string) any { return m[k] }, true
	case OrderedMap:
		return func(k string) any { x, _ := m.Get(k); return x }, true
	}
	if m, ok := asStringMap(v).(map[string]any); ok {
		return func(k string) any { return m[k] }, true
	}
	return nil, false
}

// asStringMap converts other string-keyed Go maps to map[string]any.
func asStringMap(v any) any {
	switch v.(type) {
	case map[string]any, OrderedMap:
		return v
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil
	}
	out := make(map[string]any, rv.Len())
	it := rv.MapRange()
	for it.Next() {
		out[it.Key().String()] = it.Value().Interface()
	}
	return out
}

func sliceItems(v any) ([]any, bool) {
	if items, ok := v.([]any); ok {
		return items, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice || rv.Type().Elem().Kind() == reflect.Uint8 {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	}
	return 0, false
}

// integerOf returns the underlying number of an int or long value, turning
// the avro library's logical-type values back into what Java reads without conversions.
func integerOf(s avro.Schema, v any) (int64, bool) {
	if n, ok := asInt64(v); ok {
		return n, true
	}
	switch x := v.(type) {
	case time.Time:
		switch logicalType(s) {
		case avro.Date:
			return floorDiv(x.Unix(), 86400), true
		case avro.TimestampMicros:
			return x.UnixMicro(), true
		case avro.LocalTimestampMillis:
			return wallClockUTC(x).UnixMilli(), true
		case avro.LocalTimestampMicros:
			return wallClockUTC(x).UnixMicro(), true
		}
		return x.UnixMilli(), true
	case time.Duration:
		if logicalType(s) == avro.TimeMicros {
			return x.Microseconds(), true
		}
		return x.Milliseconds(), true
	}
	return 0, false
}

func wallClockUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// bytesOf returns the bytes of a bytes value (a decimal *big.Rat becomes its
// unscaled two's-complement bytes).
func bytesOf(s avro.Schema, v any) ([]byte, bool) {
	switch x := v.(type) {
	case []byte:
		return x, true
	case *big.Rat:
		if ps, ok := deref(s).(*avro.PrimitiveSchema); ok {
			if d, ok := ps.Logical().(*avro.DecimalLogicalSchema); ok {
				return twosComplement(unscaled(x, d.Scale()), 0), true
			}
		}
	}
	return byteArray(v)
}

func fixedBytes(fs *avro.FixedSchema, v any) ([]byte, bool) {
	switch x := v.(type) {
	case []byte:
		return x, true
	case avro.LogicalDuration:
		b := make([]byte, 12)
		binary.LittleEndian.PutUint32(b, x.Months)
		binary.LittleEndian.PutUint32(b[4:], x.Days)
		binary.LittleEndian.PutUint32(b[8:], x.Milliseconds)
		return b, true
	case *big.Rat:
		if d, ok := fs.Logical().(*avro.DecimalLogicalSchema); ok {
			return twosComplement(unscaled(x, d.Scale()), fs.Size()), true
		}
	}
	return byteArray(v)
}

func unscaled(r *big.Rat, scale int) *big.Int {
	n := new(big.Int).Mul(r.Num(), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil))
	return n.Quo(n, r.Denom())
}

// twosComplement is BigInteger.toByteArray, sign-extended to size bytes when
// size is larger.
func twosComplement(n *big.Int, size int) []byte {
	var b []byte
	if n.Sign() >= 0 {
		b = n.Bytes()
		if len(b) == 0 || b[0]&0x80 != 0 {
			b = append([]byte{0}, b...)
		}
	} else {
		// -n - 1 in magnitude, inverted.
		m := new(big.Int).Sub(new(big.Int).Neg(n), big.NewInt(1)).Bytes()
		for i := range m {
			m[i] = ^m[i]
		}
		if len(m) == 0 || m[0]&0x80 == 0 {
			m = append([]byte{0xFF}, m...)
		}
		b = m
	}
	for len(b) < size {
		pad := byte(0)
		if n.Sign() < 0 {
			pad = 0xFF
		}
		b = append([]byte{pad}, b...)
	}
	return b
}

func latin1String(b []byte) string {
	rs := make([]rune, len(b))
	for i, c := range b {
		rs[i] = rune(c)
	}
	return string(rs)
}
