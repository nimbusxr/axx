package avrojson

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/iskorotkov/avro/v2"
)

// deref resolves a named-type reference to its definition.
func deref(s avro.Schema) avro.Schema {
	for {
		r, ok := s.(*avro.RefSchema)
		if !ok {
			return s
		}
		s = r.Schema()
	}
}

// logicalType returns the schema's logical type, or "".
func logicalType(s avro.Schema) avro.LogicalType {
	if lts, ok := deref(s).(avro.LogicalTypeSchema); ok {
		if ls := lts.Logical(); ls != nil {
			return ls.Type()
		}
	}
	return ""
}

// branchKey is the union key the avro library uses for a branch (its schemaTypeName):
// the full name of a named type, otherwise the type name with the logical
// type appended.
func branchKey(s avro.Schema) string {
	s = deref(s)
	if n, ok := s.(avro.NamedSchema); ok {
		return n.FullName()
	}
	name := string(s.Type())
	if lt := logicalType(s); lt != "" {
		name += "." + string(lt)
	}
	return name
}

// branchLabel is the union label of Avro's JSON encoding (Schema.getFullName
// in Java): the full name of a named type, otherwise the type name. Logical
// types never change it.
func branchLabel(s avro.Schema) string {
	s = deref(s)
	if n, ok := s.(avro.NamedSchema); ok {
		return n.FullName()
	}
	return string(s.Type())
}

// typeDesc names a schema in messages: "record ex.Order", "int (date)".
func typeDesc(s avro.Schema) string {
	s = deref(s)
	switch x := s.(type) {
	case *avro.RecordSchema:
		return "record " + x.FullName()
	case *avro.EnumSchema:
		return "enum " + x.FullName()
	case *avro.FixedSchema:
		return "fixed " + x.FullName() + " (" + strconv.Itoa(x.Size()) + " bytes)"
	case *avro.UnionSchema:
		return "union [" + strings.Join(unionLabels(x), ", ") + "]"
	case *avro.ArraySchema:
		return "array"
	case *avro.MapSchema:
		return "map"
	}
	if lt := logicalType(s); lt != "" {
		return string(s.Type()) + " (" + string(lt) + ")"
	}
	return string(s.Type())
}

func unionLabels(u *avro.UnionSchema) []string {
	out := make([]string, len(u.Types()))
	for i, t := range u.Types() {
		out[i] = branchLabel(t)
	}
	return out
}

// findBranch returns the union branch whose the avro library key or JSON label is key.
func findBranch(u *avro.UnionSchema, key string) avro.Schema {
	for _, t := range u.Types() {
		if branchKey(t) == key {
			return t
		}
	}
	for _, t := range u.Types() {
		if branchLabel(t) == key {
			return t
		}
	}
	return nil
}

// javaStringKeys reports whether a map schema asks Java to read its keys as
// java.lang.String instead of Utf8 ("avro.java.string": "String"), which
// changes their hash and so the map's iteration order.
func javaStringKeys(s avro.Schema) bool {
	ps, ok := deref(s).(avro.PropertySchema)
	if !ok {
		return false
	}
	v, _ := ps.Prop("avro.java.string").(string)
	return v == "String"
}

var byteType = reflect.TypeFor[byte]()

// fixedValue makes the [size]byte array the avro library uses for fixed values.
func fixedValue(b []byte) any {
	v := reflect.New(reflect.ArrayOf(len(b), byteType)).Elem()
	reflect.Copy(v, reflect.ValueOf(b))
	return v.Interface()
}

// byteArray returns the bytes of a [N]byte value.
func byteArray(v any) ([]byte, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Array || rv.Type().Elem().Kind() != reflect.Uint8 {
		return nil, false
	}
	out := make([]byte, rv.Len())
	reflect.Copy(reflect.ValueOf(out), rv)
	return out, true
}

// pathField appends a member name to a JSONPath.
func pathField(path, name string) string {
	if isIdent(name) {
		return path + "." + name
	}
	return path + "['" + strings.ReplaceAll(strings.ReplaceAll(name, `\`, `\\`), `'`, `\'`) + "']"
}

func pathIndex(path string, i int) string { return path + "[" + strconv.Itoa(i) + "]" }

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
		case i > 0 && (r >= '0' && r <= '9' || r == '-'):
		default:
			return false
		}
	}
	return true
}
