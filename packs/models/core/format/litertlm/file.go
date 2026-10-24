// Package litertlm reads and writes LiteRT-LM models (.litertlm), the
// on-device language models Android and iOS apps run with LiteRT-LM (and
// MediaPipe's LLM inference): a header of their system metadata and their
// sections (a TFLite model of prefill and decode signatures, the tokenizer,
// the LLM metadata), then the sections.
package litertlm

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	flatbuffers "github.com/google/flatbuffers/go"
)

// Magic begins a LiteRT-LM file.
const Magic = "LITERTLM"

// Section types (litertlm_header_schema.fbs's AnySectionDataType).
const (
	TFLiteModel      = 3
	SPTokenizer      = 4
	LlmMetadataProto = 5
	HFTokenizerZlib  = 6
)

// sectionAlign is where sections begin: LiteRT-LM maps them from the file.
const sectionAlign = 16384

// File is a LiteRT-LM file.
type File struct {
	Major, Minor, Patch uint32
	System              []KV
	Sections            []Section
}

// Section is a section: its type, its items and its data.
type Section struct {
	Type  uint8
	Items []KV
	Data  []byte
}

// KV is a key and its value: a string, a bool, an int32, a uint32, an
// int64, a uint64, a float32 or a float64.
type KV struct {
	Key   string
	Value any
}

// The values' types (VData) and their tables' fields.
const (
	vUInt8, vInt8, vUInt16, vInt16, vUInt32, vInt32 = 1, 2, 3, 4, 5, 6
	vFloat32, vBool, vString, vUInt64, vInt64       = 7, 8, 9, 10, 11
	vDouble                                         = 12
)

// Section returns the first section of a type, or nil.
func (f *File) Section(t uint8) *Section {
	for i := range f.Sections {
		if f.Sections[i].Type == t {
			return &f.Sections[i]
		}
	}
	return nil
}

// Read reads a LiteRT-LM file.
func Read(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parse(b)
}

func parse(b []byte) (f *File, err error) {
	defer func() {
		if r := recover(); r != nil {
			f, err = nil, fmt.Errorf("its header is not a LiteRT-LM file's: %v", r)
		}
	}()
	if len(b) < 32 || string(b[:8]) != Magic {
		return nil, errors.New("it is not a LiteRT-LM file")
	}
	f = &File{Major: binary.LittleEndian.Uint32(b[8:]), Minor: binary.LittleEndian.Uint32(b[12:]), Patch: binary.LittleEndian.Uint32(b[16:])}
	if f.Major != 1 {
		return nil, fmt.Errorf("it is a LiteRT-LM file of version %d; axx reads version 1", f.Major)
	}
	end := binary.LittleEndian.Uint64(b[24:])
	if end < 32 || end > uint64(len(b)) {
		return nil, errors.New("its header's end is outside it")
	}
	header := b[32:end]
	root := table(header, flatbuffers.GetUOffsetT(header))
	if sys, ok := root.sub(0); ok {
		f.System = kvs(sys.subs(0))
	}
	if secs, ok := root.sub(1); ok {
		for _, o := range secs.subs(0) {
			begin, finish := o.uint64(1), o.uint64(2)
			if begin > finish || finish > uint64(len(b)) {
				return nil, errors.New("a section is outside the file")
			}
			f.Sections = append(f.Sections, Section{Type: o.uint8(3), Items: kvs(o.subs(0)), Data: b[begin:finish]})
		}
	}
	return f, nil
}

func kvs(ts []fbTable) []KV {
	var out []KV
	for _, t := range ts {
		kv := KV{Key: t.str(0)}
		if v, ok := t.sub(2); ok {
			switch t.uint8(1) {
			case vString:
				kv.Value = v.str(0)
			case vBool:
				kv.Value = v.uint8(0) != 0
			case vInt32:
				kv.Value = int32(v.uint32(0))
			case vUInt32:
				kv.Value = v.uint32(0)
			case vInt64:
				kv.Value = int64(v.uint64(0))
			case vUInt64:
				kv.Value = v.uint64(0)
			case vFloat32:
				kv.Value = math.Float32frombits(v.uint32(0))
			case vDouble:
				kv.Value = math.Float64frombits(v.uint64(0))
			default:
				continue
			}
		}
		out = append(out, kv)
	}
	return out
}

// Write writes the file: its header, then its sections, each at a multiple
// of 16 KiB.
func (f *File) Write(w io.Writer) error {
	// The sections' offsets depend on the header's size, which depends on
	// the offsets' values only as varints do not: flatbuffers' are fixed.
	header := f.header(make([]uint64, 2*len(f.Sections)))
	at := align(uint64(32 + len(header)))
	offsets := make([]uint64, 2*len(f.Sections))
	for i, s := range f.Sections {
		offsets[2*i], offsets[2*i+1] = at, at+uint64(len(s.Data))
		at = align(at + uint64(len(s.Data)))
	}
	header = f.header(offsets)
	var out bytes.Buffer
	out.WriteString(Magic)
	for _, v := range []uint32{f.Major, f.Minor, f.Patch, 0} {
		_ = binary.Write(&out, binary.LittleEndian, v)
	}
	_ = binary.Write(&out, binary.LittleEndian, uint64(32+len(header)))
	out.Write(header)
	for i, s := range f.Sections {
		out.Write(make([]byte, offsets[2*i]-uint64(out.Len())))
		out.Write(s.Data)
	}
	_, err := w.Write(out.Bytes())
	return err
}

func align(n uint64) uint64 { return (n + sectionAlign - 1) / sectionAlign * sectionAlign }

// header is the header's flatbuffer (LiteRTLMMetaData), of the sections'
// offsets.
func (f *File) header(offsets []uint64) []byte {
	b := flatbuffers.NewBuilder(1024)
	pairs := func(kvs []KV) flatbuffers.UOffsetT {
		offs := make([]flatbuffers.UOffsetT, 0, len(kvs))
		for _, kv := range kvs {
			var vt byte
			var val flatbuffers.UOffsetT
			switch v := kv.Value.(type) {
			case string:
				s := b.CreateString(v)
				b.StartObject(1)
				b.PrependUOffsetTSlot(0, s, 0)
				vt, val = vString, b.EndObject()
			case bool:
				b.StartObject(1)
				b.PrependBoolSlot(0, v, false)
				vt, val = vBool, b.EndObject()
			case int32:
				b.StartObject(1)
				b.PrependInt32Slot(0, v, 0)
				vt, val = vInt32, b.EndObject()
			case uint32:
				b.StartObject(1)
				b.PrependUint32Slot(0, v, 0)
				vt, val = vUInt32, b.EndObject()
			case int64:
				b.StartObject(1)
				b.PrependInt64Slot(0, v, 0)
				vt, val = vInt64, b.EndObject()
			case uint64:
				b.StartObject(1)
				b.PrependUint64Slot(0, v, 0)
				vt, val = vUInt64, b.EndObject()
			case float32:
				b.StartObject(1)
				b.PrependFloat32Slot(0, v, 0)
				vt, val = vFloat32, b.EndObject()
			case float64:
				b.StartObject(1)
				b.PrependFloat64Slot(0, v, 0)
				vt, val = vDouble, b.EndObject()
			default:
				continue
			}
			key := b.CreateString(kv.Key)
			b.StartObject(3)
			b.PrependUOffsetTSlot(0, key, 0)
			b.PrependByteSlot(1, vt, 0)
			b.PrependUOffsetTSlot(2, val, 0)
			offs = append(offs, b.EndObject())
		}
		return vector(b, offs)
	}
	objects := make([]flatbuffers.UOffsetT, len(f.Sections))
	for i, s := range f.Sections {
		items := pairs(s.Items)
		b.StartObject(4)
		b.PrependUOffsetTSlot(0, items, 0)
		// Offsets are written whatever their value, so that the header is as
		// long whatever they are.
		b.PrependUint64(offsets[2*i])
		b.Slot(1)
		b.PrependUint64(offsets[2*i+1])
		b.Slot(2)
		b.PrependByteSlot(3, s.Type, 0)
		objects[i] = b.EndObject()
	}
	objectsVec := vector(b, objects)
	b.StartObject(1)
	b.PrependUOffsetTSlot(0, objectsVec, 0)
	sections := b.EndObject()
	entries := pairs(f.System)
	b.StartObject(1)
	b.PrependUOffsetTSlot(0, entries, 0)
	system := b.EndObject()
	b.StartObject(2)
	b.PrependUOffsetTSlot(0, system, 0)
	b.PrependUOffsetTSlot(1, sections, 0)
	b.Finish(b.EndObject())
	return b.FinishedBytes()
}

func vector(b *flatbuffers.Builder, offs []flatbuffers.UOffsetT) flatbuffers.UOffsetT {
	b.StartVector(4, len(offs), 4)
	for i := len(offs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(offs[i])
	}
	return b.EndVector(len(offs))
}

// fbTable is a flatbuffer table.
type fbTable struct{ t flatbuffers.Table }

func table(buf []byte, pos flatbuffers.UOffsetT) fbTable {
	return fbTable{flatbuffers.Table{Bytes: buf, Pos: pos}}
}

func (t fbTable) field(slot int) flatbuffers.UOffsetT {
	return flatbuffers.UOffsetT(t.t.Offset(flatbuffers.VOffsetT(4 + 2*slot)))
}

func (t fbTable) uint8(slot int) uint8 {
	if o := t.field(slot); o != 0 {
		return t.t.GetUint8(t.t.Pos + o)
	}
	return 0
}

func (t fbTable) uint32(slot int) uint32 {
	if o := t.field(slot); o != 0 {
		return t.t.GetUint32(t.t.Pos + o)
	}
	return 0
}

func (t fbTable) uint64(slot int) uint64 {
	if o := t.field(slot); o != 0 {
		return t.t.GetUint64(t.t.Pos + o)
	}
	return 0
}

func (t fbTable) str(slot int) string {
	if o := t.field(slot); o != 0 {
		return string(t.t.ByteVector(t.t.Pos + o))
	}
	return ""
}

func (t fbTable) sub(slot int) (fbTable, bool) {
	o := t.field(slot)
	if o == 0 {
		return fbTable{}, false
	}
	return table(t.t.Bytes, t.t.Indirect(t.t.Pos+o)), true
}

func (t fbTable) subs(slot int) []fbTable {
	o := t.field(slot)
	if o == 0 {
		return nil
	}
	start := t.t.Vector(o)
	n := t.t.VectorLen(o)
	out := make([]fbTable, n)
	for i := range n {
		at := start + flatbuffers.UOffsetT(4*i)
		out[i] = table(t.t.Bytes, t.t.Indirect(at))
	}
	return out
}
