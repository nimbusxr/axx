package grpc

import (
	"bytes"
	"encoding/json"
	"regexp"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Proto JSON writes a 64-bit integer as a string ("weightGrams": "1200"), for
// JavaScript's sake, and a whole double or float as an integer ("weightKg":
// 6). A check reads a message's values with their types (ADR 0014), so
// typedNumbers writes them as the numbers they are, by the message's
// descriptors: 1200, and the decimal 6.0. NaN and the infinities, which JSON
// lacks, stay the strings proto JSON writes.

var integerJSON = regexp.MustCompile(`^"-?[0-9]+"$`)

// typedNumbers rewrites the 64-bit integers, floats and doubles of a message's
// proto JSON, md's, anywhere in it, keeping everything else as it is. types
// resolves the messages in its Any fields.
func typedNumbers(b []byte, md protoreflect.MessageDescriptor, types protoregistry.MessageTypeResolver) []byte {
	out, err := numbers{types}.message(b, md)
	if err != nil {
		return b
	}
	return out
}

// numbers walks proto JSON by its descriptors.
type numbers struct {
	types protoregistry.MessageTypeResolver
}

func (n numbers) message(raw json.RawMessage, md protoreflect.MessageDescriptor) (json.RawMessage, error) {
	switch md.FullName() {
	case "google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		return number(raw), nil
	case "google.protobuf.DoubleValue", "google.protobuf.FloatValue":
		return decimal(raw), nil
	case "google.protobuf.Struct", "google.protobuf.Value", "google.protobuf.ListValue":
		// Plain JSON, whose keys are not fields, and whose numbers are all
		// doubles: they say nothing of a type.
		return raw, nil
	case "google.protobuf.Any":
		return n.anyMessage(raw)
	}
	return object(raw, func(key string, v json.RawMessage) (json.RawMessage, error) {
		return n.named(md, key, v)
	})
}

// anyMessage is the JSON of an Any: the fields of the message it holds, with
// its type, in "@type", or that message's own JSON in "value", for the types
// proto JSON writes as something else than an object of fields. An Any whose
// type does not resolve stays as it is.
func (n numbers) anyMessage(raw json.RawMessage) (json.RawMessage, error) {
	var head struct {
		Type string `json:"@type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, err
	}
	mt, err := n.types.FindMessageByURL(head.Type)
	if err != nil {
		return raw, nil //nolint:nilerr // proto JSON found it to write the Any: it stays as it is otherwise
	}
	md := mt.Descriptor()
	return object(raw, func(key string, v json.RawMessage) (json.RawMessage, error) {
		switch {
		case key == "@type":
			return v, nil
		case key == "value" && md.FullName() == "google.protobuf.Any":
			return n.anyMessage(v)
		}
		// A wrapper's "value" is its field of that name.
		return n.named(md, key, v)
	})
}

// named is the value of md's field of that JSON key, if it has one.
func (n numbers) named(md protoreflect.MessageDescriptor, key string, v json.RawMessage) (json.RawMessage, error) {
	fd := md.Fields().ByJSONName(key)
	if fd == nil {
		fd = md.Fields().ByName(protoreflect.Name(key))
	}
	if fd == nil {
		return v, nil
	}
	return n.field(v, fd)
}

func (n numbers) field(v json.RawMessage, fd protoreflect.FieldDescriptor) (json.RawMessage, error) {
	switch {
	case fd.IsMap():
		vd := fd.MapValue()
		return object(v, func(_ string, e json.RawMessage) (json.RawMessage, error) {
			return n.single(e, vd)
		})
	case fd.IsList():
		var items []json.RawMessage
		if err := json.Unmarshal(v, &items); err != nil {
			return nil, err
		}
		for i, e := range items {
			r, err := n.single(e, fd)
			if err != nil {
				return nil, err
			}
			items[i] = r
		}
		return json.Marshal(items)
	}
	return n.single(v, fd)
}

// single is one value of a field: the field's, an item of a list, or a value
// of a map.
func (n numbers) single(v json.RawMessage, fd protoreflect.FieldDescriptor) (json.RawMessage, error) {
	switch fd.Kind() {
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return number(v), nil
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return decimal(v), nil
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return n.message(v, fd.Message())
	}
	return v, nil
}

// object rewrites the values of a JSON object, keeping its keys' order. Any
// other JSON, like a Timestamp's string, stays as it is.
func object(raw json.RawMessage, value func(key string, v json.RawMessage) (json.RawMessage, error)) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return raw, err
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for first := true; dec.More(); first = false {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		if v, err = value(key, v); err != nil {
			return nil, err
		}
		if !first {
			out.WriteByte(',')
		}
		k, _ := json.Marshal(key)
		out.Write(k)
		out.WriteByte(':')
		out.Write(v)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// number is a quoted integer as the number it is.
func number(v json.RawMessage) json.RawMessage {
	if integerJSON.Match(bytes.TrimSpace(v)) {
		s := bytes.TrimSpace(v)
		return json.RawMessage(s[1 : len(s)-1])
	}
	return v
}

// decimal is a double or a float as a decimal, a whole one too (6.0, not 6).
// One with an exponent (1e+21) is one already, and NaN and the infinities are
// strings.
func decimal(v json.RawMessage) json.RawMessage {
	s := bytes.TrimSpace(v)
	if len(s) == 0 || s[0] != '-' && (s[0] < '0' || s[0] > '9') || bytes.ContainsAny(s, ".eE") {
		return v
	}
	return append(append(json.RawMessage(nil), s...), ".0"...)
}
