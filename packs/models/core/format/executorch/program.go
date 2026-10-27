// Package executorch reads ExecuTorch programs' contracts (.pte, the
// models PyTorch's on-device runtime runs in Android and iOS apps) and
// writes stubs as ExecuTorch programs: the ONNX stub's graph (package
// onnx), each operator one or more of the portable kernels' calls (aten's
// out variants), its tensors' memory planned, its constants in the
// program, with flatbuffers (schema/program.fbs) and no generated code.
package executorch

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// The file's identifier.
const fileID = "ET12"

// scalarTypes are ExecuTorch's ScalarType numbers of the contract's types.
var scalarTypes = map[contract.DType]int8{
	"uint8": 0, "int8": 1, "int16": 2, "int32": 3, "int64": 4, "float16": 5,
	"float32": 6, "float64": 7, "bool": 11, "uint16": 27, "uint32": 28, "uint64": 29,
}

func typeOfScalar(n int8) (contract.DType, bool) {
	for t, v := range scalarTypes {
		if v == n {
			return t, true
		}
	}
	return "", false
}

// The KernelTypes union's members, and InstructionArguments'.
const (
	evNull, evInt, evBool, evDouble, evTensor = 1, 2, 3, 4, 5
	evString, evIntList, evTensorList         = 6, 7, 10
	instrKernelCall                           = 1
)

// evalue is a value of the plan: one of its fields, by its kind.
type evalue struct {
	kind   byte
	i      int64
	b      bool
	d      float64
	s      string
	ints   []int64
	tensor *tensor
}

// tensor is a tensor value: its type and sizes, and its data: a constant's
// (its index in the constant buffers), or its place in the planned memory.
type tensor struct {
	typ      contract.DType
	sizes    []int64
	constant int // > 0: the constant buffer's index
	offset   uint64
	// bounded says its sizes are its greatest: a call's may be smaller
	// (DYNAMIC_BOUND), and kernels resize it.
	bounded bool
}

// kernelCall is an instruction: an operator's call, its arguments values'
// indices.
type kernelCall struct {
	op   int
	args []int
}

// program is a program's methods' plans, and the constants of their
// tensors.
type program struct {
	plans     []*plan
	constants [][]byte
}

// plan is an ExecutionPlan: a method's values, inputs and outputs, its
// kernels' calls, and its memory.
type plan struct {
	name    string
	values  []evalue
	inputs  []int
	outputs []int
	ops     [][2]string
	calls   []kernelCall
	memory  uint64
	inMeta  string
	outMeta string
}

// encode is the program's file.
func (p *program) encode() []byte {
	b := flatbuffers.NewBuilder(1 << 16)
	ints32 := func(vs []int) flatbuffers.UOffsetT {
		b.StartVector(4, len(vs), 4)
		for i := len(vs) - 1; i >= 0; i-- {
			b.PrependInt32(int32(vs[i]))
		}
		return b.EndVector(len(vs))
	}
	ints64 := func(vs []int64) flatbuffers.UOffsetT {
		b.StartVector(8, len(vs), 8)
		for i := len(vs) - 1; i >= 0; i-- {
			b.PrependInt64(vs[i])
		}
		return b.EndVector(len(vs))
	}
	tables := func(offs []flatbuffers.UOffsetT) flatbuffers.UOffsetT {
		b.StartVector(4, len(offs), 4)
		for i := len(offs) - 1; i >= 0; i-- {
			b.PrependUOffsetT(offs[i])
		}
		return b.EndVector(len(offs))
	}

	var planOffs []flatbuffers.UOffsetT
	for _, pl := range p.plans {
		values := make([]flatbuffers.UOffsetT, len(pl.values))
		for i, v := range pl.values {
			var val flatbuffers.UOffsetT
			switch v.kind {
			case evNull:
				b.StartObject(0)
				val = b.EndObject()
			case evInt:
				b.StartObject(1)
				b.PrependInt64Slot(0, v.i, 0)
				val = b.EndObject()
			case evBool:
				b.StartObject(1)
				b.PrependBoolSlot(0, v.b, false)
				val = b.EndObject()
			case evDouble:
				b.StartObject(1)
				b.PrependFloat64Slot(0, v.d, 0)
				val = b.EndObject()
			case evString:
				str := b.CreateString(v.s)
				b.StartObject(1)
				b.PrependUOffsetTSlot(0, str, 0)
				val = b.EndObject()
			case evIntList:
				items := ints64(v.ints)
				b.StartObject(1)
				b.PrependUOffsetTSlot(0, items, 0)
				val = b.EndObject()
			case evTensorList:
				idx := make([]int, len(v.ints))
				for k, x := range v.ints {
					idx[k] = int(x)
				}
				items := ints32(idx)
				b.StartObject(1)
				b.PrependUOffsetTSlot(0, items, 0)
				val = b.EndObject()
			case evTensor:
				t := v.tensor
				sizes := make([]int, len(t.sizes))
				order := make([]byte, len(t.sizes))
				for k, s := range t.sizes {
					sizes[k], order[k] = int(s), byte(k)
				}
				sizesVec := ints32(sizes)
				orderVec := b.CreateByteVector(order)
				var alloc flatbuffers.UOffsetT
				if t.constant == 0 {
					b.StartObject(3)
					b.PrependUint32Slot(0, 1, 0)
					b.PrependUint32Slot(1, uint32(t.offset), 0)
					b.PrependUint32Slot(2, uint32(t.offset>>32), 0)
					alloc = b.EndObject()
				}
				b.StartObject(10)
				b.PrependInt8Slot(0, scalarTypes[t.typ], 0)
				b.PrependUOffsetTSlot(2, sizesVec, 0)
				b.PrependUOffsetTSlot(3, orderVec, 0)
				b.PrependUint32Slot(5, uint32(t.constant), 0)
				if alloc != 0 {
					b.PrependUOffsetTSlot(6, alloc, 0)
				}
				if t.bounded {
					b.PrependInt8Slot(8, 1, 0)
				}
				val = b.EndObject()
			}
			b.StartObject(2)
			b.PrependByteSlot(0, v.kind, 0)
			b.PrependUOffsetTSlot(1, val, 0)
			values[i] = b.EndObject()
		}
		valuesVec := tables(values)

		instrs := make([]flatbuffers.UOffsetT, len(pl.calls))
		for i, c := range pl.calls {
			args := ints32(c.args)
			b.StartObject(2)
			b.PrependInt32Slot(0, int32(c.op), 0)
			b.PrependUOffsetTSlot(1, args, 0)
			call := b.EndObject()
			b.StartObject(2)
			b.PrependByteSlot(0, instrKernelCall, 0)
			b.PrependUOffsetTSlot(1, call, 0)
			instrs[i] = b.EndObject()
		}
		instrVec := tables(instrs)
		emptyIn, emptyOut := ints32(nil), ints32(nil)
		b.StartObject(4)
		b.PrependUOffsetTSlot(0, emptyIn, 0)
		b.PrependUOffsetTSlot(1, emptyOut, 0)
		b.PrependUOffsetTSlot(2, instrVec, 0)
		chain := b.EndObject()
		chains := tables([]flatbuffers.UOffsetT{chain})

		ops := make([]flatbuffers.UOffsetT, len(pl.ops))
		for i, o := range pl.ops {
			name, overload := b.CreateString(o[0]), b.CreateString(o[1])
			b.StartObject(2)
			b.PrependUOffsetTSlot(0, name, 0)
			b.PrependUOffsetTSlot(1, overload, 0)
			ops[i] = b.EndObject()
		}
		opsVec := tables(ops)
		inputs, outputs := ints32(pl.inputs), ints32(pl.outputs)
		sizes := ints64([]int64{0, int64(pl.memory)})
		inMeta, outMeta := b.CreateString(pl.inMeta), b.CreateString(pl.outMeta)
		b.StartObject(2)
		b.PrependUOffsetTSlot(0, inMeta, 0)
		b.PrependUOffsetTSlot(1, outMeta, 0)
		meta := b.EndObject()
		name := b.CreateString(pl.name)
		noDelegates := tables(nil)
		b.StartObject(10)
		b.PrependUOffsetTSlot(0, name, 0)
		b.PrependUOffsetTSlot(1, meta, 0)
		b.PrependUOffsetTSlot(2, valuesVec, 0)
		b.PrependUOffsetTSlot(3, inputs, 0)
		b.PrependUOffsetTSlot(4, outputs, 0)
		b.PrependUOffsetTSlot(5, chains, 0)
		b.PrependUOffsetTSlot(6, opsVec, 0)
		b.PrependUOffsetTSlot(7, noDelegates, 0)
		b.PrependUOffsetTSlot(8, sizes, 0)
		planOffs = append(planOffs, b.EndObject())
	}
	plans := tables(planOffs)

	// The constants: a segment after the program, each tensor's place in
	// it padded to 16 bytes (the first, of none, buffer 0's).
	var segment []byte
	offsets := []uint64{0}
	for _, data := range p.constants {
		offsets = append(offsets, uint64(len(segment)))
		segment = append(segment, data...)
		for len(segment)%16 != 0 {
			segment = append(segment, 0)
		}
	}
	b.StartObject(2)
	b.PrependUint64Slot(0, 0, 1)
	b.PrependUint64Slot(1, uint64(len(segment)), 1)
	dataSegment := b.EndObject()
	segmentsVec := tables([]flatbuffers.UOffsetT{dataSegment})
	offsetsVec := ints64(asInt64(offsets))
	b.StartObject(2)
	b.PrependUint32Slot(0, 0, 0)
	b.PrependUOffsetTSlot(1, offsetsVec, 0)
	constantSegment := b.EndObject()
	noBuffers, noDelegateData, noMutable, noNamed := tables(nil), tables(nil), tables(nil), tables(nil)
	b.StartObject(8)
	b.PrependUint32Slot(0, 0, 0)
	b.PrependUOffsetTSlot(1, plans, 0)
	b.PrependUOffsetTSlot(2, noBuffers, 0)
	b.PrependUOffsetTSlot(3, noDelegateData, 0)
	b.PrependUOffsetTSlot(6, noMutable, 0)
	b.PrependUOffsetTSlot(7, noNamed, 0)
	b.PrependUOffsetTSlot(4, segmentsVec, 0)
	b.PrependUOffsetTSlot(5, constantSegment, 0)
	b.FinishWithFileIdentifier(b.EndObject(), []byte(fileID))
	return withSegment(b.FinishedBytes(), segment)
}

func asInt64(vs []uint64) []int64 {
	out := make([]int64, len(vs))
	for i, v := range vs {
		out[i] = int64(v)
	}
	return out
}

// The extended header's length, and the alignment of the segments.
const (
	headerLength     = 32
	segmentAlignment = 128
)

// withSegment is a program's file of its flatbuffer and its segment: the
// extended header (eh00: the program's size, where the segment begins,
// and its size) after the file's identifier, then the segment.
func withSegment(fb, segment []byte) []byte {
	programSize := uint64(headerLength + len(fb))
	base := (programSize + segmentAlignment - 1) / segmentAlignment * segmentAlignment
	out := make([]byte, 0, int(base)+len(segment))
	out = binary.LittleEndian.AppendUint32(out, binary.LittleEndian.Uint32(fb[0:4])+headerLength)
	out = append(out, fb[4:8]...)
	out = append(out, "eh00"...)
	out = binary.LittleEndian.AppendUint32(out, headerLength)
	out = binary.LittleEndian.AppendUint64(out, programSize)
	out = binary.LittleEndian.AppendUint64(out, base)
	out = binary.LittleEndian.AppendUint64(out, uint64(len(segment)))
	out = append(out, fb[8:]...)
	out = append(out, make([]byte, int(base)-len(out))...)
	return append(out, segment...)
}

// method is a program's method: its plan's name, and its inputs and outputs
// (their types and sizes), as a contract has them.
type method struct {
	Name    string            `json:"name"`
	Inputs  []contract.Tensor `json:"inputs"`
	Outputs []contract.Tensor `json:"outputs"`
	// Bounded are its inputs' dimensions of any size up to theirs (by
	// input, then dimension): a language model's tokens'.
	Bounded [][]bool `json:"bounded,omitempty"`
	// Values are the outputs of a method of no inputs that gives values,
	// not tensors (a language model's get_max_seq_len, get_eos_ids...).
	Values []constant `json:"values,omitempty"`
}

// constant is a value a method gives: an int, a bool, a string, or ints.
type constant struct {
	Int    *int64  `json:"int,omitempty"`
	Bool   *bool   `json:"bool,omitempty"`
	String *string `json:"string,omitempty"`
	Ints   []int64 `json:"ints,omitempty"`
}

// readProgram reads a program's methods, and the operators and delegates
// they call.
func readProgram(b []byte) (methods []method, ops []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			methods, ops, err = nil, nil, fmt.Errorf("it is not an ExecuTorch program: %v", r)
		}
	}()
	if len(b) < 8 || string(b[4:8]) != fileID {
		return nil, nil, errors.New("it is not an ExecuTorch program (its identifier is not ET12)")
	}
	root := fbt{b, flatbuffers.GetUOffsetT(b)}
	plans := root.tables(1)
	if len(plans) == 0 {
		return nil, nil, errors.New("it has no execution plan")
	}
	seen := map[string]bool{}
	for _, plan := range plans {
		values := plan.tables(2)
		tensorAt := func(i int32, name string) (contract.Tensor, error) {
			if int(i) >= len(values) {
				return contract.Tensor{}, fmt.Errorf("its %s is no value", name)
			}
			v := values[i]
			if v.u8(0) != evTensor {
				return contract.Tensor{}, fmt.Errorf("its %s is not a tensor", name)
			}
			t := v.val()
			typ, ok := typeOfScalar(t.i8(0))
			if !ok {
				return contract.Tensor{}, fmt.Errorf("its %s is of scalar type %d", name, t.i8(0))
			}
			out := contract.Tensor{Name: name, Type: typ}
			for _, s := range t.ints32(2) {
				out.Shape = append(out.Shape, contract.Dim{Size: int64(s)})
			}
			return out, nil
		}
		m := method{Name: plan.str(0)}
		for k, i := range plan.ints32(3) {
			t, err := tensorAt(i, fmt.Sprintf("input%d", k))
			if err != nil {
				return nil, nil, fmt.Errorf("its method %s: %w", m.Name, err)
			}
			m.Inputs = append(m.Inputs, t)
			bounded := values[i].val().i8(8) == 1
			dims := make([]bool, len(t.Shape))
			for d := range dims {
				dims[d] = bounded && t.Shape[d].Size > 1
			}
			m.Bounded = append(m.Bounded, dims)
		}
		if !slices.ContainsFunc(m.Bounded, func(b []bool) bool { return slices.Contains(b, true) }) {
			m.Bounded = nil
		}
		for k, i := range plan.ints32(4) {
			if len(m.Inputs) == 0 && int(i) < len(values) && values[i].u8(0) != evTensor {
				v := values[i]
				var c constant
				switch val := v.val(); v.u8(0) {
				case evInt:
					n := val.i64(0)
					c.Int = &n
				case evBool:
					b := val.u8(0) != 0
					c.Bool = &b
				case evString:
					str := val.str(0)
					c.String = &str
				case evIntList:
					// Its items are the values of its ints.
					for _, at := range val.ints64(0) {
						if at < 0 || int(at) >= len(values) || values[at].u8(0) != evInt {
							return nil, nil, fmt.Errorf("its method %s gives a list of ints whose item %d is not an int", m.Name, at)
						}
						c.Ints = append(c.Ints, values[at].val().i64(0))
					}
				default:
					return nil, nil, fmt.Errorf("its method %s gives a value of kind %d", m.Name, v.u8(0))
				}
				m.Values = append(m.Values, c)
				continue
			}
			t, err := tensorAt(i, fmt.Sprintf("output%d", k))
			if err != nil {
				return nil, nil, fmt.Errorf("its method %s: %w", m.Name, err)
			}
			m.Outputs = append(m.Outputs, t)
		}
		methods = append(methods, m)
		var names []string
		for _, o := range plan.tables(6) {
			names = append(names, o.str(0)+"."+o.str(1))
		}
		// Its delegates' backends (XNNPACK's, Core ML's...), which run
		// parts of it in their own kernels.
		for _, d := range plan.tables(7) {
			names = append(names, "delegate:"+d.str(0))
		}
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				ops = append(ops, n)
			}
		}
	}
	return methods, ops, nil
}

// fbt is a flatbuffer table: its buffer and position.
type fbt struct {
	b   []byte
	pos flatbuffers.UOffsetT
}

func (t fbt) field(slot int) flatbuffers.UOffsetT {
	tab := flatbuffers.Table{Bytes: t.b, Pos: t.pos}
	return flatbuffers.UOffsetT(tab.Offset(flatbuffers.VOffsetT(4 + 2*slot)))
}

func (t fbt) u8(slot int) byte {
	if o := t.field(slot); o != 0 {
		return t.b[t.pos+o]
	}
	return 0
}

func (t fbt) i8(slot int) int8 { return int8(t.u8(slot)) }

// val is an EValue's value: the table of its union's field.
func (t fbt) val() fbt {
	o := t.field(1)
	if o == 0 {
		panic("an EValue has no value")
	}
	tab := flatbuffers.Table{Bytes: t.b, Pos: t.pos}
	return fbt{t.b, tab.Indirect(t.pos + o)}
}

func (t fbt) vector(slot int) (start flatbuffers.UOffsetT, n int) {
	o := t.field(slot)
	if o == 0 {
		return 0, 0
	}
	tab := flatbuffers.Table{Bytes: t.b, Pos: t.pos}
	return tab.Vector(o), tab.VectorLen(o)
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

func (t fbt) ints32(slot int) []int32 {
	start, n := t.vector(slot)
	out := make([]int32, n)
	for i := range n {
		out[i] = flatbuffers.GetInt32(t.b[start+flatbuffers.UOffsetT(4*i):])
	}
	return out
}

func (t fbt) str(slot int) string {
	start, n := t.vector(slot)
	return string(t.b[start : int(start)+n])
}

func (t fbt) i64(slot int) int64 {
	if o := t.field(slot); o != 0 {
		return flatbuffers.GetInt64(t.b[t.pos+o:])
	}
	return 0
}

func (t fbt) ints64(slot int) []int64 {
	start, n := t.vector(slot)
	out := make([]int64, n)
	for i := range n {
		out[i] = flatbuffers.GetInt64(t.b[start+flatbuffers.UOffsetT(8*i):])
	}
	return out
}
