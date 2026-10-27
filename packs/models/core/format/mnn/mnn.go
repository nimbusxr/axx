// Package mnn reads MNN models' contracts (.mnn, the models Android, iOS
// and server apps run with Alibaba's MNN) and writes stubs as MNN models:
// the ONNX stub's graph (package onnx) translated into MNN's ops, as its
// converter translates an ONNX model's (its integers int32s, its layout
// NCHW, its pooling of its packed layout), with flatbuffers and no
// generated code.
package mnn

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/onnx"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "mnn", Name: "MNN", Extensions: []string{".mnn"}, Contract: ReadContract, Write: format.File(Write)})
}

// onnxOpset is the ai.onnx operator set of the ONNX stub a model is
// translated from.
const onnxOpset = 17

// details are an MNN model's own settings in its contract.
type details struct {
	BizCode string `json:"bizCode,omitempty"`
}

// ReadContract reads the contract of the MNN model at path: its inputs
// (their names, types and dimensions), its outputs' names, and the ops it
// uses. MNN does not say its outputs' types and shapes: they are left for
// `axx models contract --output` to give. An input shaped like an image is
// given its layout.
func ReadContract(path string) (*contract.Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	r, err := readNet(b)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not an MNN model axx reads: %v", path, err).
			WithHint("an MNN model is a .mnn file, as MNN's converter (MNNConvert) writes it")
	}
	c := &contract.Contract{Format: contract.Format{Type: "mnn"}, Inputs: r.inputs}
	for _, name := range r.outputs {
		c.Outputs = append(c.Outputs, contract.Tensor{Name: name})
	}
	for i := range c.Inputs {
		c.Inputs[i].GuessImage()
	}
	for _, t := range r.ops {
		name, ok := opNames[t]
		if !ok {
			name = fmt.Sprintf("op%d", t)
		}
		c.Format.Operators = append(c.Format.Operators, name)
	}
	slices.Sort(c.Format.Operators)
	raw, err := json.Marshal(details{BizCode: r.bizCode})
	if err != nil {
		return nil, err
	}
	c.Format.Details = raw
	return c, nil
}

// Write writes a stub as an MNN model: its ONNX stub's graph, translated.
func Write(s *stub.Stub) ([]byte, error) {
	if s.Generated != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: MNN's stubs answer, and do not generate text").
			WithHint("a language model's stub is written for its runtime's format: GGUF, ONNX, safetensors, ExecuTorch, LiteRT-LM")
	}
	var d details
	if len(s.Contract.Format.Details) > 0 {
		if err := json.Unmarshal(s.Contract.Format.Details, &d); err != nil {
			return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not an MNN model's: %v", err)
		}
	}
	c := *s.Contract
	c.Format = contract.Format{Type: "onnx", IRVersion: 8, Opsets: map[string]int64{"": onnxOpset}}
	ms, err := s.For(&c)
	if err != nil {
		return nil, err
	}
	model, err := onnx.Write(ms)
	if err != nil {
		return nil, err
	}
	g, err := onnx.ReadGraph(model)
	if err != nil {
		return nil, err
	}
	n, err := translate(g, &c)
	if err != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	n.bizCode = d.BizCode
	return n.encode(), nil
}

// value is a tensor of the Net: its index, and its type (MNN's integers'
// and booleans' are int32s).
type value struct {
	index int32
	typ   contract.DType
}

// translator translates an ONNX graph's nodes into a Net's ops.
type translator struct {
	n      *net
	values map[string]value
	inits  map[string]onnx.Initializer
}

// add adds an op of the type and parameter on the inputs, its output a
// tensor of the name (the net's next, unless given) and type.
func (t *translator) add(name string, typ, param int, fields []field, out contract.DType, inputs ...value) value {
	if name == "" {
		name = fmt.Sprintf("axx_%d", len(t.n.names))
	}
	o := op{typ: typ, param: param, fields: fields, name: name}
	for _, in := range inputs {
		o.inputs = append(o.inputs, in.index)
	}
	v := value{index: t.n.tensor(name), typ: out}
	o.outputs = []int32{v.index}
	t.n.ops = append(t.n.ops, o)
	return v
}

// constant adds a Const of the values, of the type and dimensions.
func (t *translator) constant(typ contract.DType, dims []int64, vs []float64) value {
	d := make([]int32, len(dims))
	for i, x := range dims {
		d[i] = int32(x)
	}
	fields := []field{ints(0, d...), i32(2, dataTypes[typ])}
	switch {
	case typ == "uint8":
		raw := make([]byte, len(vs))
		for i, x := range vs {
			raw[i] = byte(x)
		}
		fields = append(fields, bytesField(3, raw))
	case typ.Integer():
		is := make([]int32, len(vs))
		for i, x := range vs {
			is[i] = int32(math.Max(math.MinInt32, math.Min(math.MaxInt32, x)))
		}
		fields = append(fields, ints(5, is...))
	default:
		fs := make([]float32, len(vs))
		for i, x := range vs {
			fs[i] = float32(x)
		}
		fields = append(fields, floats(7, fs...))
	}
	out := typ
	if typ.Integer() && typ != "uint8" {
		out = "int32"
	}
	return t.add("", opConst, paramBlob, fields, out)
}

func (t *translator) vector(vs ...int64) value {
	fs := make([]float64, len(vs))
	for i, v := range vs {
		fs[i] = float64(v)
	}
	return t.constant("int32", []int64{int64(len(vs))}, fs)
}

func (t *translator) scalar(typ contract.DType, v float64) value {
	return t.constant(typ, nil, []float64{v})
}

// input is the value of the ONNX graph's tensor name.
func (t *translator) input(name string) (value, error) {
	if v, ok := t.values[name]; ok {
		return v, nil
	}
	init, ok := t.inits[name]
	if !ok {
		return value{}, fmt.Errorf("its graph has no tensor %q", name)
	}
	v := t.constant(init.Type, init.Dims, init.Values())
	t.values[name] = v
	return v, nil
}

// translate is the Net of an ONNX stub's graph, of the contract's inputs,
// its outputs the contract's.
func translate(g *onnx.Graph, c *contract.Contract) (*net, error) {
	if g.Opset != onnxOpset {
		return nil, fmt.Errorf("its ONNX graph is of operator set %d, not %d", g.Opset, onnxOpset)
	}
	t := &translator{n: &net{}, values: map[string]value{}, inits: g.Initializers}
	for _, in := range c.Inputs {
		dims := make([]int32, len(in.Shape))
		for i, d := range in.Shape {
			dims[i] = -1
			if d.Fixed() {
				dims[i] = int32(d.Size)
			}
		}
		typ := in.Type
		if typ.Integer() && typ != "uint8" {
			typ = "int32"
		}
		t.values[in.Name] = t.add(in.Name, opInput, paramInput, []field{ints(0, dims...), i32(1, dataTypes[in.Type]), {slot: 2, put: func(b *flatbuffers.Builder, s int) { b.PrependInt8(formatNCHW); b.Slot(s) }}}, typ)
	}
	for _, n := range g.Nodes {
		if err := t.node(n); err != nil {
			return nil, fmt.Errorf("its %s node %s: %w", n.Op, n.Outputs[0], err)
		}
	}
	for i, name := range g.Outputs {
		v, err := t.input(name)
		if err != nil {
			return nil, err
		}
		// The output, of its contract's name: its value's tensor, named so,
		// or a copy of an input's or another output's.
		out := c.Outputs[i].Name
		if strings.HasPrefix(t.n.names[v.index], "axx_") {
			t.n.names[v.index] = out
		} else {
			v = t.add(out, opReshape, paramReshape, []field{i32(1, formatNCHW)}, v.typ, v, t.shapeOf(v))
		}
		t.n.outputs = append(t.n.outputs, out)
	}
	return t.n, nil
}

// shapeOf is a value's shape.
func (t *translator) shapeOf(v value) value {
	return t.add("", opShape, paramNone, nil, "int32", v)
}

// binaryOps are ONNX's elementwise operators, as MNN's BinaryOp's
// operations: Div REALDIV, And a product of 0s and 1s.
var binaryOps = map[string]int32{"Add": 0, "Sub": 1, "Mul": 2, "Div": 7, "Max": 9, "Min": 8}

var compareOps = map[string]int32{"Equal": 15, "Greater": 10, "Less": 12, "And": 2}

var unaryOps = map[string]int32{"Sqrt": 5, "Log": 8, "Round": 23}

var reductions = map[string]int32{"ReduceMax": 4, "ReduceMin": 5, "ReduceMean": 3}

// node adds a node's ops.
func (t *translator) node(n onnx.Node) error {
	var ins []value
	for _, name := range n.Inputs {
		if name == "" {
			ins = append(ins, value{index: -1})
			continue
		}
		v, err := t.input(name)
		if err != nil {
			return err
		}
		ins = append(ins, v)
	}
	out := n.Outputs[0]
	set := func(v value) error {
		t.values[out] = v
		return nil
	}
	attr := func(name string, def int64) int64 {
		if a, ok := n.Attrs[name]; ok {
			return a.Int
		}
		return def
	}
	constInts := func(i int) ([]int64, error) {
		init, ok := t.inits[n.Inputs[i]]
		if !ok {
			return nil, fmt.Errorf("its input %d is not a constant", i)
		}
		vs := init.Values()
		res := make([]int64, len(vs))
		for k, v := range vs {
			res[k] = int64(v)
		}
		return res, nil
	}
	x := ins[0]
	binary := func(opType int32, a, b value, out contract.DType) value {
		return t.add("", opBinaryOp, paramBinaryOp, []field{i32(0, opType), i32(1, dataTypes["float32"])}, out, a, b)
	}

	if opType, ok := compareOps[n.Op]; ok {
		return set(binary(opType, x, ins[1], "int32"))
	}
	if opType, ok := binaryOps[n.Op]; ok {
		v := x
		for _, y := range ins[1:] {
			v = binary(opType, v, y, x.typ)
		}
		return set(v)
	}
	switch op := n.Op; {
	case op == "Not":
		// 1 − x: (x − 1), negated.
		less := binary(binaryOps["Sub"], x, t.scalar("int32", 1), "int32")
		return set(t.add("", opUnaryOp, paramUnaryOp, []field{i32(0, 1)}, "int32", less))
	case unaryOps[op] != 0:
		return set(t.add("", opUnaryOp, paramUnaryOp, []field{i32(0, unaryOps[op])}, x.typ, x))
	case op == "Identity":
		return set(x)
	case op == "Where":
		return set(t.add("", opSelect, paramNone, nil, ins[1].typ, ins[0], ins[1], ins[2]))
	case op == "MatMul":
		return set(t.add("", opMatMul, paramMatMul, []field{i32(0, dataTypes["float32"])}, x.typ, x, ins[1]))
	case op == "Cast":
		to, ok := onnx.TypeOf(attr("to", 0))
		if !ok {
			return fmt.Errorf("a cast to ONNX type %d", attr("to", 0))
		}
		typ := to
		if to.Integer() && to != "uint8" {
			typ = "int32"
		}
		return set(t.add("", opCast, paramCastParam, []field{i32(1, dataTypes[to])}, typ, x))
	case op == "Reshape":
		return set(t.add("", opReshape, paramReshape, []field{i32(1, formatNCHW)}, x.typ, x, ins[1]))
	case op == "Transpose":
		perm := n.Attrs["perm"].Ints
		p := make([]int32, len(perm))
		for i, v := range perm {
			p[i] = int32(v)
		}
		return set(t.add("", opPermute, paramPermute, []field{ints(0, p...)}, x.typ, x))
	case op == "Unsqueeze":
		return set(t.add("", opUnsqueeze, paramSqueezeParam, nil, x.typ, x, ins[1]))
	case op == "Expand":
		return set(t.add("", opBroadcastTo, paramNone, nil, x.typ, x, ins[1]))
	case op == "Shape":
		return set(t.shapeOf(x))
	case op == "Range":
		return set(t.add("", opRange, paramNone, nil, x.typ, ins[0], ins[1], ins[2]))
	case op == "Concat":
		return set(t.add("", opConcat, paramAxis, []field{i32(0, int32(attr("axis", 0)))}, x.typ, ins...))
	case op == "Gather":
		return set(t.add("", opGatherV2, paramGatherV2, nil, x.typ, x, ins[1], t.scalar("int32", float64(attr("axis", 0)))))
	case op == "GatherElements":
		return set(t.add("", opGatherElements, paramNone, nil, x.typ, x, ins[1], t.scalar("int32", float64(attr("axis", 0)))))
	case op == "AveragePool":
		k, st := n.Attrs["kernel_shape"].Ints, n.Attrs["strides"].Ints
		if len(k) != 2 {
			return fmt.Errorf("a pool of %d dimensions", len(k))
		}
		convert := func(v value, from, to int8) value {
			return t.add("", opConvertTensor, paramTensorConvertInfo, []field{
				{slot: 0, put: func(b *flatbuffers.Builder, s int) { b.PrependInt8(from); b.Slot(s) }},
				{slot: 1, put: func(b *flatbuffers.Builder, s int) { b.PrependInt8(to); b.Slot(s) }},
			}, v.typ, v)
		}
		packed := convert(x, formatNCHW, formatNC4HW4)
		pooled := t.add("", opPooling, paramPool, []field{
			i32(3, int32(k[1])), i32(4, int32(k[0])), i32(5, int32(st[1])), i32(6, int32(st[0])),
			{slot: 7, put: func(b *flatbuffers.Builder, s int) { b.PrependInt8(1); b.Slot(s) }}, // AVEPOOL
			i32(9, dataTypes["float32"]), boolean(10, false),
		}, x.typ, packed)
		return set(convert(pooled, formatNC4HW4, formatNCHW))
	case op == "ArgMax":
		a := int32(attr("axis", 0))
		v := t.add("", opArgMax, paramArgMax, []field{i32(1, 1), i32(2, a)}, "int32", x)
		if attr("keepdims", 1) == 1 {
			v = t.add("", opUnsqueeze, paramSqueezeParam, []field{ints(0, a)}, "int32", v)
		}
		return set(v)
	case reductions[op] != 0:
		fields := []field{i32(0, reductions[op]), boolean(3, attr("keepdims", 1) == 1), i32(4, dataTypes["float32"])}
		if a, ok := n.Attrs["axes"]; ok {
			axes := make([]int32, len(a.Ints))
			for i, v := range a.Ints {
				axes[i] = int32(v)
			}
			fields = append(fields, ints(1, axes...))
		}
		return set(t.add("", opReduction, paramReductionParam, fields, x.typ, x))
	case op == "Slice":
		// ONNX's own starts, ends, axes and steps: fromType 1.
		args := []value{x}
		for i := 1; i < len(ins) && i <= 4; i++ {
			if ins[i].index < 0 {
				break
			}
			args = append(args, ins[i])
		}
		return set(t.add("", opStridedSlice, paramStridedSliceParam, []field{i32(7, 1)}, x.typ, args...))
	case op == "Pad":
		pads, err := constInts(1)
		if err != nil {
			return err
		}
		r := len(pads) / 2
		var tf []int64
		for i := range r {
			tf = append(tf, pads[i], pads[r+i])
		}
		fill := t.scalar(x.typ, 0)
		if len(ins) > 2 && ins[2].index >= 0 {
			fill = ins[2]
		}
		return set(t.add("", opPadding, paramPadParam, []field{i32(0, 0)}, x.typ, x, t.vector(tf...), fill))
	case op == "OneHot":
		depth, err := constInts(1)
		if err != nil {
			return err
		}
		vals, ok := t.inits[n.Inputs[2]]
		if !ok {
			return fmt.Errorf("its values are not a constant")
		}
		onOff := vals.Values()
		typ := vals.Type
		return set(t.add("", opOneHot, paramOneHotParam, []field{i32(0, dataTypes[typ]), i32(1, int32(attr("axis", -1)))}, typ,
			x, t.scalar("int32", float64(depth[0])), t.scalar(typ, onOff[1]), t.scalar(typ, onOff[0])))
	}
	return fmt.Errorf("MNN's stubs have no translation of ONNX's %s", n.Op)
}
