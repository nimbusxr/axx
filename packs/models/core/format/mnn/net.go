package mnn

import (
	"errors"
	"fmt"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// A model is MNN's Net (schema/default/MNN.fbs): its ops, each of the
// indexes of its input and output tensors and its parameter (a table of
// the OpParameter union), the tensors' names, and the names of its
// outputs.

// OpTypes of the ops a stub has.
const (
	opArgMax         = 2
	opBinaryOp       = 7
	opCast           = 9
	opConcat         = 10
	opConst          = 11
	opGatherV2       = 31
	opInput          = 34
	opMatMul         = 39
	opPadding        = 45
	opPermute        = 46
	opPooling        = 47
	opRange          = 65
	opReduction      = 68
	opReshape        = 73
	opShape          = 80
	opStridedSlice   = 91
	opUnaryOp        = 101
	opUnsqueeze      = 107
	opOneHot         = 119
	opBroadcastTo    = 120
	opConvertTensor  = 129
	opGatherElements = 152
	opSelect         = 257
	opAttention      = 299
	opLayerNorm      = 603
)

// OpParameter's members, by their place in its union.
const (
	paramNone              = 0
	paramArgMax            = 2
	paramAxis              = 4
	paramBinaryOp          = 6
	paramBlob              = 7
	paramCastParam         = 8
	paramGatherV2          = 19
	paramInput             = 21
	paramMatMul            = 25
	paramPermute           = 29
	paramPool              = 31
	paramReductionParam    = 50
	paramReshape           = 55
	paramSqueezeParam      = 64
	paramStridedSliceParam = 65
	paramTensorConvertInfo = 66
	paramUnaryOp           = 70
	paramOneHotParam       = 83
	paramPadParam          = 84
	paramLayerNorm         = 88
	paramAttentionParam    = 98
)

// UnaryOp's operations of an LLM's graph.
const (
	unaryNeg  = 1
	unarySin  = 9
	unaryCos  = 10
	unarySiLU = 34
)

// Its data formats, data types, and the source an ONNX model's ops are of.
const (
	formatNCHW   = 0
	formatNC4HW4 = 2
	sourceONNX   = 3
)

// dataTypes are MNN's DataTypes of the contract's types: its integers are
// int32s, its booleans 0s and 1s of int32s, as its ops give them.
var dataTypes = map[contract.DType]int32{
	"float32": 1, "float64": 2, "int32": 3, "uint8": 4, "int16": 5, "int8": 6, "int64": 3, "bool": 3, "float16": 1,
}

// typeOf is the contract's type of an MNN DataType.
func typeOf(n int32) (contract.DType, bool) {
	switch n {
	case 1:
		return "float32", true
	case 3, 9:
		return "int32", true
	case 4:
		return "uint8", true
	case 6:
		return "int8", true
	}
	return "", false
}

// field is a parameter table's field: its slot, and how it is written.
type field struct {
	slot int
	put  func(b *flatbuffers.Builder, slot int)
	vec  func(b *flatbuffers.Builder) flatbuffers.UOffsetT // a vector's, written before its table
}

func i32(slot int, v int32) field {
	return field{slot: slot, put: func(b *flatbuffers.Builder, s int) { b.PrependInt32(v); b.Slot(s) }}
}

func boolean(slot int, v bool) field {
	return field{slot: slot, put: func(b *flatbuffers.Builder, s int) { b.PrependBool(v); b.Slot(s) }}
}

func ints(slot int, vs ...int32) field {
	return field{slot: slot, vec: func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		b.StartVector(4, len(vs), 4)
		for i := len(vs) - 1; i >= 0; i-- {
			b.PrependInt32(vs[i])
		}
		return b.EndVector(len(vs))
	}}
}

func floats(slot int, vs ...float32) field {
	return field{slot: slot, vec: func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		b.StartVector(4, len(vs), 4)
		for i := len(vs) - 1; i >= 0; i-- {
			b.PrependFloat32(vs[i])
		}
		return b.EndVector(len(vs))
	}}
}

func bytesField(slot int, vs []byte) field {
	return field{slot: slot, vec: func(b *flatbuffers.Builder) flatbuffers.UOffsetT { return b.CreateByteVector(vs) }}
}

// op is an op of a Net.
type op struct {
	typ, param int
	fields     []field
	inputs     []int32
	outputs    []int32
	name       string
}

// net is a Net being written.
type net struct {
	ops     []op
	names   []string
	outputs []string
	bizCode string
}

// tensor adds a tensor of the name, and is its index.
func (n *net) tensor(name string) int32 {
	n.names = append(n.names, name)
	return int32(len(n.names) - 1)
}

// encode is the Net's file.
func (n *net) encode() []byte {
	b := flatbuffers.NewBuilder(1 << 16)
	strs := func(ss []string) flatbuffers.UOffsetT {
		offs := make([]flatbuffers.UOffsetT, len(ss))
		for i, s := range ss {
			offs[i] = b.CreateString(s)
		}
		b.StartVector(4, len(offs), 4)
		for i := len(offs) - 1; i >= 0; i-- {
			b.PrependUOffsetT(offs[i])
		}
		return b.EndVector(len(offs))
	}
	vec32 := func(vs []int32) flatbuffers.UOffsetT {
		b.StartVector(4, len(vs), 4)
		for i := len(vs) - 1; i >= 0; i-- {
			b.PrependInt32(vs[i])
		}
		return b.EndVector(len(vs))
	}
	opOffs := make([]flatbuffers.UOffsetT, len(n.ops))
	for i, o := range n.ops {
		var param flatbuffers.UOffsetT
		if o.param != paramNone {
			vecs := map[int]flatbuffers.UOffsetT{}
			for _, f := range o.fields {
				if f.vec != nil {
					vecs[f.slot] = f.vec(b)
				}
			}
			slots := 0
			for _, f := range o.fields {
				slots = max(slots, f.slot+1)
			}
			b.StartObject(slots)
			for _, f := range o.fields {
				if f.vec != nil {
					b.PrependUOffsetTSlot(f.slot, vecs[f.slot], 0)
				} else {
					f.put(b, f.slot)
				}
			}
			param = b.EndObject()
		}
		inputs, outputs := vec32(o.inputs), vec32(o.outputs)
		name := b.CreateString(o.name)
		b.StartObject(8)
		b.PrependUOffsetTSlot(0, inputs, 0)
		if o.param != paramNone {
			b.PrependByteSlot(1, byte(o.param), 0)
			b.PrependUOffsetTSlot(2, param, 0)
		}
		b.PrependUOffsetTSlot(3, name, 0)
		b.PrependUOffsetTSlot(4, outputs, 0)
		b.PrependInt32Slot(5, int32(o.typ), 0)
		opOffs[i] = b.EndObject()
	}
	b.StartVector(4, len(opOffs), 4)
	for i := len(opOffs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(opOffs[i])
	}
	oplists := b.EndVector(len(opOffs))
	outputNames, tensorNames := strs(n.outputs), strs(n.names)
	bizCode := b.CreateString(n.bizCode)
	b.StartObject(12)
	b.PrependUOffsetTSlot(0, bizCode, 0)
	b.PrependUOffsetTSlot(3, oplists, 0)
	b.PrependUOffsetTSlot(4, outputNames, 0)
	b.PrependInt8Slot(6, sourceONNX, 0)
	b.PrependUOffsetTSlot(7, tensorNames, 0)
	b.Finish(b.EndObject())
	return b.FinishedBytes()
}

// fbt reads a flatbuffer table.
type fbt struct {
	b   []byte
	pos flatbuffers.UOffsetT
}

func (t fbt) field(slot int) flatbuffers.UOffsetT {
	tab := flatbuffers.Table{Bytes: t.b, Pos: t.pos}
	return flatbuffers.UOffsetT(tab.Offset(flatbuffers.VOffsetT(4 + 2*slot)))
}

func (t fbt) i32(slot int, def int32) int32 {
	if o := t.field(slot); o != 0 {
		return flatbuffers.GetInt32(t.b[t.pos+o:])
	}
	return def
}

func (t fbt) table(slot int) (fbt, bool) {
	o := t.field(slot)
	if o == 0 {
		return fbt{}, false
	}
	tab := flatbuffers.Table{Bytes: t.b, Pos: t.pos}
	return fbt{t.b, tab.Indirect(t.pos + o)}, true
}

func (t fbt) vector(slot int) (flatbuffers.UOffsetT, int) {
	o := t.field(slot)
	if o == 0 {
		return 0, 0
	}
	tab := flatbuffers.Table{Bytes: t.b, Pos: t.pos}
	return tab.Vector(o), tab.VectorLen(o)
}

func (t fbt) ints(slot int) []int32 {
	start, n := t.vector(slot)
	out := make([]int32, n)
	for i := range n {
		out[i] = flatbuffers.GetInt32(t.b[start+flatbuffers.UOffsetT(4*i):])
	}
	return out
}

func (t fbt) str(slot int) string {
	o := t.field(slot)
	if o == 0 {
		return ""
	}
	tab := flatbuffers.Table{Bytes: t.b, Pos: t.pos}
	return string(tab.ByteVector(t.pos + o))
}

func (t fbt) strs(slot int) []string {
	start, n := t.vector(slot)
	out := make([]string, n)
	for i := range n {
		at := start + flatbuffers.UOffsetT(4*i)
		s := at + flatbuffers.GetUOffsetT(t.b[at:])
		l := flatbuffers.GetUint32(t.b[s:])
		out[i] = string(t.b[s+4 : s+4+flatbuffers.UOffsetT(l)])
	}
	return out
}

func (t fbt) tables(slot int) []fbt {
	start, n := t.vector(slot)
	out := make([]fbt, n)
	for i := range n {
		at := start + flatbuffers.UOffsetT(4*i)
		out[i] = fbt{t.b, at + flatbuffers.GetUOffsetT(t.b[at:])}
	}
	return out
}

// read is what a Net says of its inputs and outputs: each input's name,
// type and dimensions, its outputs' names, its ops' types, and its
// bizCode.
type read struct {
	inputs  []contract.Tensor
	outputs []string
	ops     []int32
	bizCode string
}

// readNet reads a Net.
func readNet(b []byte) (r *read, err error) {
	defer func() {
		if e := recover(); e != nil {
			r, err = nil, fmt.Errorf("it is not an MNN model: %v", e)
		}
	}()
	if len(b) < 8 {
		return nil, errors.New("it is not an MNN model")
	}
	root := fbt{b, flatbuffers.GetUOffsetT(b)}
	names := root.strs(7)
	r = &read{outputs: root.strs(4), bizCode: root.str(0)}
	seen := map[int32]bool{}
	consumed := map[int32]bool{}
	var produced []int32
	for _, o := range root.tables(3) {
		for _, i := range o.ints(0) {
			consumed[i] = true
		}
		produced = append(produced, o.ints(4)...)
		typ := o.i32(5, 0)
		if !seen[typ] {
			seen[typ] = true
			r.ops = append(r.ops, typ)
		}
		if typ != opInput {
			continue
		}
		outs := o.ints(4)
		if len(outs) != 1 || int(outs[0]) >= len(names) {
			return nil, errors.New("an input of it has no tensor")
		}
		t := contract.Tensor{Name: names[outs[0]], Type: "float32"}
		if p, ok := o.table(2); ok {
			if typ, ok := typeOf(p.i32(1, 1)); ok {
				t.Type = typ
			}
			for _, d := range p.ints(0) {
				if d < 0 {
					t.Shape = append(t.Shape, contract.Dim{Name: "?"})
				} else {
					t.Shape = append(t.Shape, contract.Dim{Size: int64(d)})
				}
			}
		}
		r.inputs = append(r.inputs, t)
	}
	if len(r.inputs) == 0 {
		return nil, errors.New("it has no input")
	}
	if len(r.outputs) == 0 {
		// A Net that names no outputs gives those of its tensors no op
		// takes, as MNN's loader does.
		for _, i := range produced {
			if !consumed[i] && int(i) < len(names) {
				r.outputs = append(r.outputs, names[i])
			}
		}
	}
	return r, nil
}
