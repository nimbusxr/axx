package avrojson

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/iskorotkov/avro/v2"
)

// Options tunes Decode.
type Options struct {
	// LenientUnions accepts a union value written without its
	// {"<branch>": value} wrapper when exactly one branch can decode it.
	// Apache Avro's JSON decoder always requires the wrapper.
	LenientUnions bool
}

// Error is a decoding failure. Path is the JSONPath of the offending JSON
// value (for a missing record field, the path it should have had).
type Error struct {
	Path string
	Msg  string
}

func (e *Error) Error() string { return e.Path + ": " + e.Msg }

// Decode parses Avro JSON encoding (Apache JsonDecoder rules: unions as
// {"<branch full name>": v} or null, bytes/fixed as ISO-8859-1 strings, enums
// as symbols, logical types) of a value of schema s into a native Go value
// that the avro library can Marshal.
func Decode(s avro.Schema, data []byte, opts Options) (any, error) {
	n, err := parseJSON(data)
	if err != nil {
		return nil, err
	}
	d := decoder{opts: opts}
	return d.value(s, n, "$")
}

type decoder struct {
	opts Options
}

func mismatch(path string, s avro.Schema, n *node) *Error {
	return &Error{Path: path, Msg: fmt.Sprintf("expected %s, got %s", typeDesc(s), n.describe())}
}

func (d decoder) value(s avro.Schema, n *node, path string) (any, error) {
	s = deref(s)
	switch s.Type() {
	case avro.Null:
		if n.kind != jNull {
			return nil, mismatch(path, s, n)
		}
		return nil, nil
	case avro.Boolean:
		if n.kind != jBool {
			return nil, mismatch(path, s, n)
		}
		return n.b, nil
	case avro.Int:
		v, err := decodeInt(s, n, path)
		if err != nil {
			return nil, err
		}
		return int(v), nil
	case avro.Long:
		v, err := decodeLong(s, n, path)
		if err != nil {
			return nil, err
		}
		if logicalType(s) == avro.TimeMicros {
			// the avro library encodes time-micros from a time.Duration only.
			if v > math.MaxInt64/1000 || v < math.MinInt64/1000 {
				return nil, &Error{Path: path, Msg: fmt.Sprintf("time-micros value %d is out of range", v)}
			}
			return time.Duration(v) * time.Microsecond, nil
		}
		return v, nil
	case avro.Float:
		f, err := decodeFloating(s, n, path, 32)
		if err != nil {
			return nil, err
		}
		return float32(f), nil
	case avro.Double:
		return decodeFloating(s, n, path, 64)
	case avro.String:
		if n.kind != jString {
			return nil, mismatch(path, s, n)
		}
		return n.s, nil
	case avro.Bytes:
		if n.kind != jString {
			return nil, mismatch(path, s, n)
		}
		return latin1Bytes(n.s), nil
	case avro.Fixed:
		fs := s.(*avro.FixedSchema)
		if n.kind != jString {
			return nil, mismatch(path, s, n)
		}
		b := latin1Bytes(n.s)
		if len(b) != fs.Size() {
			return nil, &Error{Path: path, Msg: fmt.Sprintf("fixed %s needs exactly %d bytes, got %d", fs.FullName(), fs.Size(), len(b))}
		}
		return fixedValue(b), nil
	case avro.Enum:
		es := s.(*avro.EnumSchema)
		if n.kind != jString {
			return nil, mismatch(path, s, n)
		}
		for _, sym := range es.Symbols() {
			if sym == n.s {
				return sym, nil
			}
		}
		return nil, &Error{Path: path, Msg: fmt.Sprintf("unknown symbol %q for enum %s (symbols: %s)", n.s, es.FullName(), strings.Join(es.Symbols(), ", "))}
	case avro.Array:
		if n.kind != jArray {
			return nil, mismatch(path, s, n)
		}
		items := s.(*avro.ArraySchema).Items()
		out := make([]any, 0, len(n.items))
		for i, it := range n.items {
			v, err := d.value(items, it, pathIndex(path, i))
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case avro.Map:
		if n.kind != jObject {
			return nil, mismatch(path, s, n)
		}
		values := s.(*avro.MapSchema).Values()
		out := make(map[string]any, len(n.members))
		for _, m := range n.members {
			v, err := d.value(values, m.val, pathField(path, m.key))
			if err != nil {
				return nil, err
			}
			out[m.key] = v // a repeated key keeps its last value, as in Java's HashMap
		}
		return out, nil
	case avro.Record:
		rs := s.(*avro.RecordSchema)
		if n.kind != jObject {
			return nil, mismatch(path, s, n)
		}
		out := make(map[string]any, len(rs.Fields()))
		for _, f := range rs.Fields() {
			fp := pathField(path, f.Name())
			fv, ok := n.first(f.Name())
			if !ok {
				msg := fmt.Sprintf("missing field %q (%s) of record %s", f.Name(), typeDesc(f.Type()), rs.FullName())
				if f.HasDefault() {
					msg += "; Avro's JSON encoding needs every field, even those with a default"
				}
				return nil, &Error{Path: fp, Msg: msg}
			}
			v, err := d.value(f.Type(), fv, fp)
			if err != nil {
				return nil, err
			}
			out[f.Name()] = v
		}
		return out, nil
	case avro.Union:
		return d.union(s.(*avro.UnionSchema), n, path)
	}
	return nil, &Error{Path: path, Msg: fmt.Sprintf("unsupported schema type %s", s.Type())}
}

func (d decoder) union(u *avro.UnionSchema, n *node, path string) (any, error) {
	labels := unionLabels(u)
	if n.kind == jNull {
		for _, t := range u.Types() {
			if deref(t).Type() == avro.Null {
				return nil, nil
			}
		}
		return nil, &Error{Path: path, Msg: fmt.Sprintf("null is not a branch of the union [%s]", strings.Join(labels, ", "))}
	}
	if n.kind == jObject && len(n.members) > 0 {
		label := n.members[0].key
		for _, t := range u.Types() {
			if branchLabel(t) != label {
				continue
			}
			// Only the first entry is read, as in Apache's decoder.
			v, err := d.value(t, n.members[0].val, path)
			if err != nil {
				return nil, err
			}
			return wrap(t, v), nil
		}
		if !d.opts.LenientUnions {
			msg := fmt.Sprintf("unknown union branch %q (branches: %s)", label, strings.Join(labels, ", "))
			if full := shortNameMatch(u, label); full != "" {
				msg += fmt.Sprintf("; named types are labelled with their full name, e.g. %q", full)
			}
			return nil, &Error{Path: path, Msg: msg}
		}
	}
	if !d.opts.LenientUnions {
		return nil, &Error{Path: path, Msg: fmt.Sprintf(
			"expected a union value, i.e. null or {\"<branch>\": value} with a branch of [%s], got %s",
			strings.Join(labels, ", "), n.describe())}
	}
	var (
		fits  []avro.Schema
		value any
	)
	for _, t := range u.Types() {
		if deref(t).Type() == avro.Null {
			continue
		}
		v, err := d.value(t, n, path)
		if err != nil {
			continue
		}
		if len(fits) == 0 {
			value = wrap(t, v)
		}
		fits = append(fits, t)
	}
	switch len(fits) {
	case 1:
		return value, nil
	case 0:
		return nil, &Error{Path: path, Msg: fmt.Sprintf("%s fits no branch of the union [%s]", n.describe(), strings.Join(labels, ", "))}
	}
	names := make([]string, len(fits))
	for i, t := range fits {
		names[i] = branchLabel(t)
	}
	return nil, &Error{Path: path, Msg: fmt.Sprintf(
		"%s fits several branches of the union (%s); wrap it as {\"<branch>\": value}", n.describe(), strings.Join(names, ", "))}
}

func wrap(branch avro.Schema, v any) any {
	if deref(branch).Type() == avro.Null {
		return nil
	}
	return map[string]any{branchKey(branch): v}
}

// shortNameMatch returns the full name of a named branch whose short name is
// label, to hint at the full-name rule.
func shortNameMatch(u *avro.UnionSchema, label string) string {
	for _, t := range u.Types() {
		if n, ok := deref(t).(avro.NamedSchema); ok && n.Name() == label {
			return n.FullName()
		}
	}
	return ""
}

// latin1Bytes encodes s as ISO-8859-1 the way Java's String.getBytes does:
// every UTF-16 unit above U+00FF becomes '?'.
func latin1Bytes(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		switch {
		case r <= 0xFF:
			out = append(out, byte(r))
		case r > 0xFFFF:
			out = append(out, '?', '?') // a surrogate pair is two chars
		default:
			out = append(out, '?')
		}
	}
	return out
}

func decodeInt(s avro.Schema, n *node, path string) (int32, error) {
	switch n.kind {
	case jInt:
		v, err := strconv.ParseInt(n.s, 10, 32)
		if err != nil {
			return 0, &Error{Path: path, Msg: fmt.Sprintf("number %s is out of range of int (-2147483648 to 2147483647)", n.s)}
		}
		return int32(v), nil
	case jFloat:
		// JsonDecoder.readInt reads a decimal literal as a float and accepts
		// it when Math.round leaves it unchanged.
		f64, _ := strconv.ParseFloat(n.s, 32)
		f := float32(f64)
		r := javaRoundFloat(f)
		if float32(math.Abs(float64(f-float32(r)))) <= math.SmallestNonzeroFloat32 {
			return r, nil
		}
	}
	return 0, mismatch(path, s, n)
}

func decodeLong(s avro.Schema, n *node, path string) (int64, error) {
	switch n.kind {
	case jInt:
		v, err := strconv.ParseInt(n.s, 10, 64)
		if err != nil {
			return 0, &Error{Path: path, Msg: fmt.Sprintf("number %s is out of range of long", n.s)}
		}
		return v, nil
	case jFloat:
		f, _ := strconv.ParseFloat(n.s, 64)
		r := javaRoundDouble(f)
		if math.Abs(f-float64(r)) <= math.SmallestNonzeroFloat64 {
			return r, nil
		}
	}
	return 0, mismatch(path, s, n)
}

func decodeFloating(s avro.Schema, n *node, path string, bits int) (float64, error) {
	switch n.kind {
	case jFloat:
		f, _ := strconv.ParseFloat(n.s, bits) // out of range gives ±Inf or 0, as in Java
		return f, nil
	case jInt:
		i, ok := new(big.Int).SetString(n.s, 10)
		if !ok {
			return 0, mismatch(path, s, n)
		}
		bf := new(big.Float).SetInt(i)
		if bits == 32 {
			f, _ := bf.Float32()
			return float64(f), nil
		}
		f, _ := bf.Float64()
		return f, nil
	case jString:
		switch n.s {
		case "NaN":
			return javaNaN, nil
		case "Infinity":
			return math.Inf(1), nil
		case "-Infinity":
			return math.Inf(-1), nil
		}
		return 0, &Error{Path: path, Msg: fmt.Sprintf(
			"expected %s, got %s (the only strings allowed are \"NaN\", \"Infinity\" and \"-Infinity\")", typeDesc(s), n.describe())}
	}
	return 0, mismatch(path, s, n)
}

// javaNaN is Double.NaN (0x7ff8000000000000); Go's math.NaN() has another
// bit pattern, which Avro's binary encoding would expose.
var javaNaN = math.Float64frombits(0x7ff8000000000000)

// javaRoundFloat is Java's Math.round(float): floor(a + 1/2), saturating.
func javaRoundFloat(a float32) int32 {
	f := float64(a)
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f <= math.MinInt32:
		return math.MinInt32
	}
	r := math.Floor(f)
	if f-r >= 0.5 {
		r++
	}
	return int32(r)
}

// javaRoundDouble is Java's Math.round(double): floor(a + 1/2), saturating.
func javaRoundDouble(a float64) int64 {
	switch {
	case math.IsNaN(a):
		return 0
	case a >= math.MaxInt64: // 2^63
		return math.MaxInt64
	case a <= math.MinInt64:
		return math.MinInt64
	}
	r := math.Floor(a)
	if a-r >= 0.5 {
		r++
	}
	return int64(r)
}
