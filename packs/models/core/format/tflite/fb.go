// Package tflite reads TFLite (LiteRT) models' contracts and writes stubs as
// TFLite models, with the flatbuffers of LiteRT's schema
// (tensorflow/compiler/mlir/lite/schema/schema.fbs) and of its metadata
// (tflite-support's metadata_schema.fbs), and no generated code: a field is
// read and written by its slot, its place in its table's declaration (a
// union takes two, its type's and its value's).
package tflite

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

// table is a flatbuffer table, read by its fields' slots.
type table struct{ t flatbuffers.Table }

func root(buf []byte) table {
	return table{flatbuffers.Table{Bytes: buf, Pos: flatbuffers.GetUOffsetT(buf)}}
}

// field is the offset of the field of the slot from the table, or 0 when
// the table does not have it.
func (t table) field(slot int) flatbuffers.UOffsetT {
	return flatbuffers.UOffsetT(t.t.Offset(flatbuffers.VOffsetT(4 + 2*slot)))
}

func (t table) uint32(slot int) uint32 {
	if o := t.field(slot); o != 0 {
		return t.t.GetUint32(t.t.Pos + o)
	}
	return 0
}

func (t table) int32(slot int, def int32) int32 {
	if o := t.field(slot); o != 0 {
		return t.t.GetInt32(t.t.Pos + o)
	}
	return def
}

func (t table) float32(slot int, def float32) float32 {
	if o := t.field(slot); o != 0 {
		return t.t.GetFloat32(t.t.Pos + o)
	}
	return def
}

func (t table) bool(slot int) bool { return t.uint8(slot) != 0 }

func (t table) int8(slot int) int8 {
	if o := t.field(slot); o != 0 {
		return t.t.GetInt8(t.t.Pos + o)
	}
	return 0
}

func (t table) uint8(slot int) uint8 {
	if o := t.field(slot); o != 0 {
		return t.t.GetUint8(t.t.Pos + o)
	}
	return 0
}

func (t table) uint64(slot int, def uint64) uint64 {
	if o := t.field(slot); o != 0 {
		return t.t.GetUint64(t.t.Pos + o)
	}
	return def
}

func (t table) str(slot int) string {
	if o := t.field(slot); o != 0 {
		return string(t.t.ByteVector(t.t.Pos + o))
	}
	return ""
}

func (t table) sub(slot int) (table, bool) {
	o := t.field(slot)
	if o == 0 {
		return table{}, false
	}
	return table{flatbuffers.Table{Bytes: t.t.Bytes, Pos: t.t.Indirect(t.t.Pos + o)}}, true
}

func (t table) subs(slot int) []table {
	o := t.field(slot)
	if o == 0 {
		return nil
	}
	start, n := t.t.Vector(o), t.t.VectorLen(o)
	out := make([]table, n)
	for i := range out {
		at := start + flatbuffers.UOffsetT(i*4)
		out[i] = table{flatbuffers.Table{Bytes: t.t.Bytes, Pos: t.t.Indirect(at)}}
	}
	return out
}

func (t table) int32s(slot int) []int32 {
	o := t.field(slot)
	if o == 0 {
		return nil
	}
	start, n := t.t.Vector(o), t.t.VectorLen(o)
	out := make([]int32, n)
	for i := range out {
		out[i] = t.t.GetInt32(start + flatbuffers.UOffsetT(i*4))
	}
	return out
}

func (t table) int64s(slot int) []int64 {
	o := t.field(slot)
	if o == 0 {
		return nil
	}
	start, n := t.t.Vector(o), t.t.VectorLen(o)
	out := make([]int64, n)
	for i := range out {
		out[i] = t.t.GetInt64(start + flatbuffers.UOffsetT(i*8))
	}
	return out
}

func (t table) float32s(slot int) []float32 {
	o := t.field(slot)
	if o == 0 {
		return nil
	}
	start, n := t.t.Vector(o), t.t.VectorLen(o)
	out := make([]float32, n)
	for i := range out {
		out[i] = t.t.GetFloat32(start + flatbuffers.UOffsetT(i*4))
	}
	return out
}

func (t table) bytes(slot int) []byte {
	if o := t.field(slot); o != 0 {
		return t.t.ByteVector(t.t.Pos + o)
	}
	return nil
}

// builder builds a flatbuffer, bottom up: what a table refers to before it.
type builder struct{ *flatbuffers.Builder }

func (b builder) offsets(offs []flatbuffers.UOffsetT) flatbuffers.UOffsetT {
	b.StartVector(4, len(offs), 4)
	for i := len(offs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(offs[i])
	}
	return b.EndVector(len(offs))
}

func (b builder) int32s(vs []int32) flatbuffers.UOffsetT {
	b.StartVector(4, len(vs), 4)
	for i := len(vs) - 1; i >= 0; i-- {
		b.PrependInt32(vs[i])
	}
	return b.EndVector(len(vs))
}

func (b builder) int64s(vs []int64) flatbuffers.UOffsetT {
	b.StartVector(8, len(vs), 8)
	for i := len(vs) - 1; i >= 0; i-- {
		b.PrependInt64(vs[i])
	}
	return b.EndVector(len(vs))
}

func (b builder) float32s(vs []float32) flatbuffers.UOffsetT {
	b.StartVector(4, len(vs), 4)
	for i := len(vs) - 1; i >= 0; i-- {
		b.PrependFloat32(vs[i])
	}
	return b.EndVector(len(vs))
}

// data is a buffer's data, aligned to 16 bytes as the schema asks.
func (b builder) data(d []byte) flatbuffers.UOffsetT {
	b.StartVector(1, len(d), 16)
	for i := len(d) - 1; i >= 0; i-- {
		b.PrependByte(d[i])
	}
	return b.EndVector(len(d))
}
