// Package pb writes and reads protobuf messages by their fields' numbers,
// with the wire format and no generated code, for the formats that are
// protobufs (ONNX, Core ML): a model is a handful of fields.
package pb

import (
	"errors"
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// Msg builds a protobuf message.
type Msg []byte

func (m Msg) Bytes(f protowire.Number, b []byte) Msg {
	m = protowire.AppendTag(m, f, protowire.BytesType)
	return protowire.AppendBytes(m, b)
}

func (m Msg) Str(f protowire.Number, s string) Msg { return m.Bytes(f, []byte(s)) }

func (m Msg) Varint(f protowire.Number, v uint64) Msg {
	m = protowire.AppendTag(m, f, protowire.VarintType)
	return protowire.AppendVarint(m, v)
}

// Ints writes a repeated int64 field unpacked, as onnx.proto (proto2)
// declares them.
func (m Msg) Ints(f protowire.Number, vs []int64) Msg {
	for _, v := range vs {
		m = m.Varint(f, uint64(v))
	}
	return m
}

// Fields reads a message's fields, calling each with its number, its wire
// type and its value: a varint's, or a bytes field's content.
func Fields(b []byte, each func(f protowire.Number, t protowire.Type, v uint64, data []byte) error) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return errors.New("not a protobuf message")
		}
		b = b[n:]
		var v uint64
		var data []byte
		switch typ {
		case protowire.VarintType:
			v, n = protowire.ConsumeVarint(b)
		case protowire.BytesType:
			data, n = protowire.ConsumeBytes(b)
		case protowire.Fixed32Type:
			var f uint32
			f, n = protowire.ConsumeFixed32(b)
			v = uint64(f)
		case protowire.Fixed64Type:
			v, n = protowire.ConsumeFixed64(b)
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
		}
		if n < 0 {
			return fmt.Errorf("field %d cannot be read", num)
		}
		b = b[n:]
		if err := each(num, typ, v, data); err != nil {
			return err
		}
	}
	return nil
}

func Float32Bytes(vs []float64) []byte {
	out := make([]byte, 0, 4*len(vs))
	for _, v := range vs {
		out = protowire.AppendFixed32(out, math.Float32bits(float32(v)))
	}
	return out
}

// Packed writes a repeated varint field packed, as proto3 has them.
func (m Msg) Packed(f protowire.Number, vs []int64) Msg {
	var b []byte
	for _, v := range vs {
		b = protowire.AppendVarint(b, uint64(v))
	}
	return m.Bytes(f, b)
}

// Floats writes a repeated float field packed, as proto3 has them.
func (m Msg) Floats(f protowire.Number, vs []float64) Msg {
	return m.Bytes(f, Float32Bytes(vs))
}

// Float writes a float field.
func (m Msg) Float(f protowire.Number, v float32) Msg {
	m = protowire.AppendTag(m, f, protowire.Fixed32Type)
	return protowire.AppendFixed32(m, math.Float32bits(v))
}

// Varints reads a repeated varint field, packed (a bytes field) or not.
func Varints(t protowire.Type, v uint64, data []byte) []int64 {
	if t == protowire.VarintType {
		return []int64{int64(v)}
	}
	var out []int64
	for len(data) > 0 {
		x, n := protowire.ConsumeVarint(data)
		if n < 0 {
			return out
		}
		out = append(out, int64(x))
		data = data[n:]
	}
	return out
}
