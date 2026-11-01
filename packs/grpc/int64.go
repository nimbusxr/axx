package grpc

import (
	"bytes"
	"encoding/json"
	"regexp"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// Proto JSON writes a 64-bit integer as a string ("weightGrams": "1200"),
// for JavaScript's sake; a check reads a message's values with their types
// (ADR 0014), so int64Numbers writes them as the numbers they are.

var integerJSON = regexp.MustCompile(`^"-?[0-9]+"$`)

// int64Numbers rewrites the 64-bit integer fields of a message's proto
// JSON, md's, as JSON numbers, keeping everything else as it is.
func int64Numbers(b []byte, md protoreflect.MessageDescriptor) []byte {
	out, err := messageNumbers(b, md)
	if err != nil {
		return b
	}
	return out
}

func messageNumbers(raw json.RawMessage, md protoreflect.MessageDescriptor) (json.RawMessage, error) {
	if wrapsInt64(md) {
		return number(raw), nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
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
		fd := md.Fields().ByJSONName(key)
		if fd == nil {
			fd = md.Fields().ByName(protoreflect.Name(key))
		}
		if fd != nil {
			if v, err = fieldNumbers(v, fd); err != nil {
				return nil, err
			}
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

func fieldNumbers(v json.RawMessage, fd protoreflect.FieldDescriptor) (json.RawMessage, error) {
	one := func(v json.RawMessage) (json.RawMessage, error) {
		switch {
		case is64Bit(fd.Kind()):
			return number(v), nil
		case fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind:
			return messageNumbers(v, fd.Message())
		}
		return v, nil
	}
	switch {
	case fd.IsMap():
		vd := fd.MapValue()
		if !is64Bit(vd.Kind()) && vd.Kind() != protoreflect.MessageKind {
			return v, nil
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(v, &m); err != nil {
			return nil, err
		}
		keys := orderedKeys(v)
		var out bytes.Buffer
		out.WriteByte('{')
		for i, k := range keys {
			e := m[k]
			if is64Bit(vd.Kind()) {
				e = number(e)
			} else if r, err := messageNumbers(e, vd.Message()); err == nil {
				e = r
			}
			if i > 0 {
				out.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			out.Write(kb)
			out.WriteByte(':')
			out.Write(e)
		}
		out.WriteByte('}')
		return out.Bytes(), nil
	case fd.IsList():
		var items []json.RawMessage
		if err := json.Unmarshal(v, &items); err != nil {
			return nil, err
		}
		for i, e := range items {
			r, err := one(e)
			if err != nil {
				return nil, err
			}
			items[i] = r
		}
		return json.Marshal(items)
	}
	return one(v)
}

// orderedKeys are an object's keys in the order the JSON has them.
func orderedKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var keys []string
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil
	}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return keys
		}
		keys = append(keys, t.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return keys
		}
	}
	return keys
}

func is64Bit(k protoreflect.Kind) bool {
	switch k {
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return true
	}
	return false
}

// wrapsInt64 says md is a wrapper of a 64-bit integer, which proto JSON
// writes as the integer's string.
func wrapsInt64(md protoreflect.MessageDescriptor) bool {
	switch md.FullName() {
	case "google.protobuf.Int64Value", "google.protobuf.UInt64Value":
		return true
	}
	return false
}

// number is a quoted integer as the number it is.
func number(v json.RawMessage) json.RawMessage {
	if integerJSON.Match(bytes.TrimSpace(v)) {
		s := bytes.TrimSpace(v)
		return json.RawMessage(s[1 : len(s)-1])
	}
	return v
}
