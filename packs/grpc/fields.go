package grpc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/tablevalue"
)

// A table sets what its cells say (ADR 0014), and a protobuf field takes
// only its own type: nothing converts a cell to it. checkRows fails on a
// value its field cannot take, and says what to write: `postcode is a
// string field: write "10115"`.

// integerText and numberText are text a cell could have written as a
// number: what to write instead.
var (
	integerText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	numberText  = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)
)

// shape is what a path names of a field: the field itself, or an item of
// a repeated field or a map.
type shape int

const (
	single shape = iota
	list
	mapOf
)

// checkRows checks each row's value against the field of md its path
// names. A path that names no field is left to protojson, which says so.
func checkRows(md protoreflect.MessageDescriptor, rows []tablevalue.Row) error {
	for _, r := range rows {
		v, err := core.ParseValue(r.Value, r.Null, true)
		if err != nil || v.Undefined() || v.IsNull() {
			continue // tablevalue reports it, or it unsets the field
		}
		fd, sh := fieldAt(md, r.Path)
		if fd == nil {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(v.JSON()))
		dec.UseNumber()
		var x any
		if err := dec.Decode(&x); err != nil {
			return err
		}
		if err := checkField(fd, sh, x, strings.TrimPrefix(r.Path, "$.")); err != nil {
			return err
		}
	}
	return nil
}

// fieldAt is the field a path into a message names (`recipient.postcode`,
// `lines[0].reference`, `labels.express`), and whether the path names it
// whole or an item of it, or nil.
func fieldAt(md protoreflect.MessageDescriptor, path string) (protoreflect.FieldDescriptor, shape) {
	var fd protoreflect.FieldDescriptor
	sh := single
	for _, seg := range strings.Split(strings.TrimPrefix(strings.TrimPrefix(path, "$"), "."), ".") {
		name, rest, _ := strings.Cut(seg, "[")
		if name != "" {
			switch sh {
			case mapOf:
				fd, sh = fd.MapValue(), single // name is a key of the map
			case list:
				return nil, single
			default:
				if fd != nil {
					if md = fd.Message(); md == nil {
						return nil, single
					}
				}
				if fd = lookup(md, name); fd == nil {
					return nil, single
				}
				sh = shapeOf(fd)
			}
		}
		for ; rest != ""; _, rest, _ = strings.Cut(rest, "[") {
			if sh != list {
				return nil, single
			}
			sh = single
		}
	}
	return fd, sh
}

func lookup(md protoreflect.MessageDescriptor, name string) protoreflect.FieldDescriptor {
	if fd := md.Fields().ByJSONName(name); fd != nil {
		return fd
	}
	return md.Fields().ByName(protoreflect.Name(name))
}

func shapeOf(fd protoreflect.FieldDescriptor) shape {
	switch {
	case fd.IsMap():
		return mapOf
	case fd.IsList():
		return list
	}
	return single
}

// checkField checks a value a row sets (a plain JSON value, its numbers
// json.Number) against the field, or an item of it.
func checkField(fd protoreflect.FieldDescriptor, sh shape, x any, path string) error {
	switch sh {
	case list:
		items, ok := x.([]any)
		if !ok {
			return fmt.Errorf("%s is a repeated field: write a JSON array, or set its items (%s[0])", path, path)
		}
		for i, e := range items {
			if err := checkValue(fd, e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case mapOf:
		m, ok := x.(map[string]any)
		if !ok {
			return fmt.Errorf("%s is a map field: write a JSON object, or set its entries (%s.<key>)", path, path)
		}
		for k, e := range m {
			if err := checkValue(fd.MapValue(), e, path+"."+k); err != nil {
				return err
			}
		}
		return nil
	}
	return checkValue(fd, x, path)
}

// checkValue checks one value against a field's type, as proto JSON takes
// it: a string for a string or bytes field, a boolean for a bool, a whole
// number for an integer, a number (or "NaN", "Infinity", "-Infinity") for
// a float, a name or a number for an enum, an object for a message.
func checkValue(fd protoreflect.FieldDescriptor, x any, path string) error {
	if x == nil {
		return nil // null leaves a field unset
	}
	what := fieldType(fd)
	switch fd.Kind() {
	case protoreflect.StringKind, protoreflect.BytesKind:
		if _, ok := x.(string); !ok {
			return fmt.Errorf("%s is %s field: write \"%s\"", path, what, plainText(x))
		}
	case protoreflect.BoolKind:
		if _, ok := x.(bool); ok {
			return nil
		}
		if s, ok := x.(string); ok && (s == "true" || s == "false") {
			return fmt.Errorf("%s is %s field: write %s", path, what, s)
		}
		return fmt.Errorf("%s is %s field: write true or false", path, what)
	case protoreflect.EnumKind:
		switch y := x.(type) {
		case string:
			return nil
		case json.Number:
			if isInteger(y) {
				return nil
			}
		}
		return fmt.Errorf("%s is %s field: write the name of one of its values", path, what)
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		switch y := x.(type) {
		case json.Number:
			return nil
		case string:
			switch y {
			case "NaN", "Infinity", "-Infinity":
				return nil // proto JSON's own forms
			}
			if numberText.MatchString(y) {
				return fmt.Errorf("%s is %s field: write %s", path, what, y)
			}
		}
		return fmt.Errorf("%s is %s field: write a number", path, what)
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return checkMessage(fd, x, path)
	default: // the integers
		switch y := x.(type) {
		case json.Number:
			if isInteger(y) {
				return nil
			}
		case string:
			if integerText.MatchString(y) {
				return fmt.Errorf("%s is %s field: write %s", path, what, y)
			}
		}
		return fmt.Errorf("%s is %s field: write a whole number", path, what)
	}
	return nil
}

// checkMessage checks a message field's value: an object of its fields.
// Well-known types are proto JSON's own forms (a Timestamp's text, a
// Struct's object), which protojson checks; a wrapper takes the value it
// wraps.
func checkMessage(fd protoreflect.FieldDescriptor, x any, path string) error {
	md := fd.Message()
	if wrapper(md) {
		return checkValue(md.Fields().ByName("value"), x, path)
	}
	if strings.HasPrefix(string(md.FullName()), "google.protobuf.") {
		return nil
	}
	m, ok := x.(map[string]any)
	if !ok {
		return fmt.Errorf("%s is a %s field: write a JSON object, or set its fields (%s.<field>)", path, md.FullName(), path)
	}
	for k, e := range m {
		f := lookup(md, k)
		if f == nil || e == nil {
			continue // protojson says it is not a field
		}
		if err := checkField(f, shapeOf(f), e, path+"."+k); err != nil {
			return err
		}
	}
	return nil
}

// wrapper says md is one of protobuf's wrappers of a scalar, which proto
// JSON writes as the scalar.
func wrapper(md protoreflect.MessageDescriptor) bool {
	switch md.FullName() {
	case "google.protobuf.DoubleValue", "google.protobuf.FloatValue", "google.protobuf.Int64Value",
		"google.protobuf.UInt64Value", "google.protobuf.Int32Value", "google.protobuf.UInt32Value",
		"google.protobuf.BoolValue", "google.protobuf.StringValue", "google.protobuf.BytesValue":
		return true
	}
	return false
}

// fieldType names a field's type with its article: "a string", "an
// int64", "a parcels.tracking.v1.Status".
func fieldType(fd protoreflect.FieldDescriptor) string {
	name := fd.Kind().String()
	if fd.Kind() == protoreflect.EnumKind {
		name = string(fd.Enum().FullName())
	}
	for _, p := range []string{"int", "sint", "sfixed"} {
		if strings.HasPrefix(name, p) {
			return "an " + name
		}
	}
	return "a " + name
}

func isInteger(n json.Number) bool {
	return !strings.ContainsAny(n.String(), ".eE")
}

// plainText is a value's text, as a string would have it.
func plainText(x any) string {
	switch y := x.(type) {
	case json.Number:
		return y.String()
	case bool:
		if y {
			return "true"
		}
		return "false"
	}
	b, _ := json.Marshal(x)
	return string(b)
}
