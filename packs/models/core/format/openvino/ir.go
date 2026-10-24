package openvino

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/onnx"
)

// onnxOpset is the ai.onnx operator set of the ONNX stub a stub's graph is
// translated from: the operators' forms translate knows.
const onnxOpset = 17

// value is a tensor of the graph: the layer's port that gives it, and its
// element type.
type value struct {
	layer, port int
	typ         contract.DType
}

// builder builds an IR: its layers and edges, and its weights.
type builder struct {
	ir     IR
	bin    bytes.Buffer
	values map[string]value
	inits  map[string]onnx.Initializer
}

type attr = [2]string

// add adds a layer of an operation (its type and operation set) of the
// attributes, on the inputs, of outputs of the types; it returns its
// outputs.
func (b *builder) add(name, typ, version string, attrs []attr, inputs []value, outs ...contract.DType) []value {
	id := len(b.ir.Layers)
	l := Layer{ID: id, Name: name, Type: typ, Version: version}
	if len(attrs) > 0 {
		l.Data = &Data{}
		for _, a := range attrs {
			l.Data.Attrs = append(l.Data.Attrs, xml.Attr{Name: xml.Name{Local: a[0]}, Value: a[1]})
		}
	}
	for i, in := range inputs {
		l.Inputs = append(l.Inputs, Port{ID: i})
		b.ir.Edges = append(b.ir.Edges, Edge{FromLayer: in.layer, FromPort: in.port, ToLayer: id, ToPort: i})
	}
	var out []value
	for i, t := range outs {
		port := len(inputs) + i
		l.Outputs = append(l.Outputs, Port{ID: port, Precision: elementTypes[t][1]})
		out = append(out, value{layer: id, port: port, typ: t})
	}
	b.ir.Layers = append(b.ir.Layers, l)
	return out
}

func (b *builder) name() string { return fmt.Sprintf("axx_ov_%d", len(b.ir.Layers)) }

// constant adds a constant of the type and dimensions, its values' raw
// bytes in the weights.
func (b *builder) constant(t contract.DType, dims []int64, raw []byte) value {
	for b.bin.Len()%8 != 0 {
		b.bin.WriteByte(0)
	}
	offset := b.bin.Len()
	b.bin.Write(raw)
	return b.add(b.name(), "Const", "opset1", []attr{
		{"element_type", elementTypes[t][0]},
		{"shape", join(dims)},
		{"offset", strconv.Itoa(offset)},
		{"size", strconv.Itoa(len(raw))},
	}, nil, t)[0]
}

// ints is a constant of int64s: a vector, or a scalar of none dims.
func (b *builder) ints(dims []int64, vs ...int64) value {
	raw := make([]byte, 0, 8*len(vs))
	for _, v := range vs {
		raw = binary.LittleEndian.AppendUint64(raw, uint64(v))
	}
	return b.constant("int64", dims, raw)
}

// scalar is a constant scalar of a type.
func (b *builder) scalar(t contract.DType, v float64) value {
	return b.constant(t, nil, t.Encode([]float64{v}))
}

func join(dims []int64) string {
	s := make([]string, len(dims))
	for i, d := range dims {
		s[i] = strconv.FormatInt(d, 10)
	}
	return strings.Join(s, ",")
}

// input is the value of the graph's tensor name: a node's output, a
// Parameter, or an initializer (a constant, added once).
func (b *builder) input(name string) (value, error) {
	if v, ok := b.values[name]; ok {
		return v, nil
	}
	init, ok := b.inits[name]
	if !ok {
		return value{}, fmt.Errorf("its graph has no tensor %q", name)
	}
	v := b.constant(init.Type, init.Dims, init.Raw)
	b.values[name] = v
	return v, nil
}

// constValues are the values of the initializer name: an operator's
// argument OpenVINO takes otherwise (Pad's pads, OneHot's values).
func (b *builder) constValues(name string) ([]float64, contract.DType, error) {
	init, ok := b.inits[name]
	if !ok {
		return nil, "", fmt.Errorf("%q is not a constant", name)
	}
	return init.Values(), init.Type, nil
}

// translate is the IR of an ONNX stub's graph, of the contract's inputs,
// and its weights.
func translate(g *onnx.Graph, c *contract.Contract) ([]byte, []byte, error) {
	if g.Opset != onnxOpset {
		return nil, nil, fmt.Errorf("its ONNX graph is of operator set %d, not %d", g.Opset, onnxOpset)
	}
	b := &builder{ir: IR{Name: "axx-stub", Version: 11}, values: map[string]value{}, inits: g.Initializers}
	for _, in := range c.Inputs {
		dims := make([]string, len(in.Shape))
		shape := make([]string, len(in.Shape))
		for i, d := range in.Shape {
			dims[i], shape[i] = "-1", "?"
			if d.Fixed() {
				dims[i] = strconv.FormatInt(d.Size, 10)
				shape[i] = dims[i]
			}
		}
		v := b.add(in.Name, "Parameter", "opset1", []attr{{"shape", strings.Join(shape, ",")}, {"element_type", elementTypes[in.Type][0]}}, nil, in.Type)[0]
		l := &b.ir.Layers[v.layer]
		l.Outputs[0].Names, l.Outputs[0].Dims = in.Name, dims
		b.values[in.Name] = v
	}
	for _, n := range g.Nodes {
		if err := b.node(n); err != nil {
			return nil, nil, fmt.Errorf("its %s node %s: %w", n.Op, n.Outputs[0], err)
		}
	}
	for _, name := range g.Outputs {
		v, err := b.input(name)
		if err != nil {
			return nil, nil, err
		}
		p := &b.ir.Layers[v.layer].Outputs[v.port-len(b.ir.Layers[v.layer].Inputs)]
		if p.Names == "" {
			p.Names = name
		} else {
			p.Names += "," + name
		}
		b.add(name+"/sink", "Result", "opset1", nil, []value{v})
	}
	out, err := xml.MarshalIndent(b.ir, "", " ")
	if err != nil {
		return nil, nil, err
	}
	return append([]byte(xml.Header), append(out, '\n')...), b.bin.Bytes(), nil
}

// binary are ONNX's elementwise operators of two inputs, as OpenVINO's.
var binaryOps = map[string]string{
	"Add": "Add", "Sub": "Subtract", "Mul": "Multiply", "Div": "Divide",
	"Max": "Maximum", "Min": "Minimum",
}

var compareOps = map[string]string{"Equal": "Equal", "Greater": "Greater", "Less": "Less", "And": "LogicalAnd"}

var reduceOps = map[string]string{"ReduceMax": "ReduceMax", "ReduceMin": "ReduceMin", "ReduceMean": "ReduceMean"}

// node adds the layers of a node.
func (b *builder) node(n onnx.Node) error {
	var ins []value
	for _, name := range n.Inputs {
		if name == "" {
			ins = append(ins, value{layer: -1})
			continue
		}
		v, err := b.input(name)
		if err != nil {
			return err
		}
		ins = append(ins, v)
	}
	out := n.Outputs[0]
	set := func(v value) error {
		b.values[out] = v
		return nil
	}
	numpy := []attr{{"auto_broadcast", "numpy"}}
	switch op := n.Op; {
	case binaryOps[op] != "":
		v := ins[0]
		for _, in := range ins[1:] {
			v = b.add(b.name(), binaryOps[op], "opset1", numpy, []value{v, in}, ins[0].typ)[0]
		}
		return set(v)
	case compareOps[op] != "":
		return set(b.add(out, compareOps[op], "opset1", numpy, ins[:2], "bool")[0])
	case op == "Not":
		return set(b.add(out, "LogicalNot", "opset1", nil, ins[:1], "bool")[0])
	case op == "Where":
		return set(b.add(out, "Select", "opset1", numpy, ins[:3], ins[1].typ)[0])
	case op == "Sqrt" || op == "Log":
		return set(b.add(out, op, "opset1", nil, ins[:1], ins[0].typ)[0])
	case op == "Round":
		return set(b.add(out, "Round", "opset5", []attr{{"mode", "half_to_even"}}, ins[:1], ins[0].typ)[0])
	case op == "Identity":
		return set(ins[0])
	case op == "MatMul":
		return set(b.add(out, "MatMul", "opset1", []attr{{"transpose_a", "false"}, {"transpose_b", "false"}}, ins[:2], ins[0].typ)[0])
	case op == "Cast":
		t, ok := onnx.TypeOf(n.Attrs["to"].Int)
		if !ok {
			return fmt.Errorf("a cast to ONNX type %d", n.Attrs["to"].Int)
		}
		return set(b.add(out, "Convert", "opset1", []attr{{"destination_type", elementTypes[t][0]}}, ins[:1], t)[0])
	case op == "Reshape":
		return set(b.add(out, "Reshape", "opset1", []attr{{"special_zero", "true"}}, ins[:2], ins[0].typ)[0])
	case op == "Transpose":
		perm := n.Attrs["perm"].Ints
		return set(b.add(out, "Transpose", "opset1", nil, []value{ins[0], b.ints([]int64{int64(len(perm))}, perm...)}, ins[0].typ)[0])
	case op == "Unsqueeze":
		return set(b.add(out, "Unsqueeze", "opset1", nil, ins[:2], ins[0].typ)[0])
	case op == "Expand":
		return set(b.add(out, "Broadcast", "opset3", []attr{{"mode", "bidirectional"}}, ins[:2], ins[0].typ)[0])
	case op == "Shape":
		return set(b.add(out, "ShapeOf", "opset3", []attr{{"output_type", "i64"}}, ins[:1], "int64")[0])
	case op == "Range":
		return set(b.add(out, "Range", "opset4", []attr{{"output_type", elementTypes[ins[0].typ][0]}}, ins[:3], ins[0].typ)[0])
	case op == "Concat":
		return set(b.add(out, "Concat", "opset1", []attr{{"axis", strconv.FormatInt(n.Attrs["axis"].Int, 10)}}, ins, ins[0].typ)[0])
	case op == "Gather":
		return set(b.add(out, "Gather", "opset8", []attr{{"batch_dims", "0"}}, []value{ins[0], ins[1], b.ints(nil, n.Attrs["axis"].Int)}, ins[0].typ)[0])
	case op == "GatherElements":
		return set(b.add(out, "GatherElements", "opset6", []attr{{"axis", strconv.FormatInt(n.Attrs["axis"].Int, 10)}}, ins[:2], ins[0].typ)[0])
	case op == "AveragePool":
		k, st := n.Attrs["kernel_shape"].Ints, n.Attrs["strides"].Ints
		zeros := join(make([]int64, len(k)))
		return set(b.add(out, "AvgPool", "opset14", []attr{
			{"kernel", join(k)},
			{"strides", join(st)},
			{"pads_begin", zeros},
			{"pads_end", zeros},
			{"exclude-pad", "true"},
			{"rounding_type", "floor"},
			{"auto_pad", "explicit"},
		}, ins[:1], ins[0].typ)[0])
	case op == "ArgMax":
		axis := n.Attrs["axis"].Int
		top := b.add(b.name(), "TopK", "opset11", []attr{
			{"axis", strconv.FormatInt(axis, 10)}, {"mode", "max"}, {"sort", "value"}, {"index_element_type", "i64"}, {"stable", "true"},
		}, []value{ins[0], b.ints(nil, 1)}, ins[0].typ, "int64")
		keep, ok := n.Attrs["keepdims"]
		if ok && keep.Int == 0 {
			return set(b.add(out, "Squeeze", "opset1", nil, []value{top[1], b.ints([]int64{1}, axis)}, "int64")[0])
		}
		return set(top[1])
	case reduceOps[op] != "":
		keep := "true"
		if k, ok := n.Attrs["keepdims"]; ok && k.Int == 0 {
			keep = "false"
		}
		var axes value
		if a, ok := n.Attrs["axes"]; ok {
			axes = b.ints([]int64{int64(len(a.Ints))}, a.Ints...)
		} else {
			// Every axis: 0 to its rank.
			shape := b.add(b.name(), "ShapeOf", "opset3", []attr{{"output_type", "i64"}}, ins[:1], "int64")[0]
			rank := b.add(b.name(), "ShapeOf", "opset3", []attr{{"output_type", "i64"}}, []value{shape}, "int64")[0]
			rank = b.add(b.name(), "Squeeze", "opset1", nil, []value{rank, b.ints([]int64{1}, 0)}, "int64")[0]
			axes = b.add(b.name(), "Range", "opset4", []attr{{"output_type", "i64"}}, []value{b.ints(nil, 0), rank, b.ints(nil, 1)}, "int64")[0]
		}
		return set(b.add(out, reduceOps[op], "opset1", []attr{{"keep_dims", keep}}, []value{ins[0], axes}, ins[0].typ)[0])
	case op == "Slice":
		// Its axes (all of its starts', when it gives none), and a step of 1
		// on each.
		var axes value
		var count int
		if len(n.Inputs) > 3 && n.Inputs[3] != "" {
			ax, _, err := b.constValues(n.Inputs[3])
			if err != nil {
				return fmt.Errorf("its axes: %w", err)
			}
			axes, count = ins[3], len(ax)
		} else {
			starts, _, err := b.constValues(n.Inputs[1])
			if err != nil {
				return fmt.Errorf("its starts: %w", err)
			}
			all := make([]int64, len(starts))
			for i := range all {
				all[i] = int64(i)
			}
			axes, count = b.ints([]int64{int64(len(all))}, all...), len(all)
		}
		if len(n.Inputs) > 4 && n.Inputs[4] != "" {
			return fmt.Errorf("a slice of steps")
		}
		ones := make([]int64, count)
		for i := range ones {
			ones[i] = 1
		}
		steps := b.ints([]int64{int64(count)}, ones...)
		return set(b.add(out, "Slice", "opset8", nil, []value{ins[0], ins[1], ins[2], steps, axes}, ins[0].typ)[0])
	case op == "Pad":
		pads, _, err := b.constValues(n.Inputs[1])
		if err != nil {
			return fmt.Errorf("its pads: %w", err)
		}
		half := len(pads) / 2
		begin, end := make([]int64, half), make([]int64, half)
		for i := range half {
			begin[i], end[i] = int64(pads[i]), int64(pads[half+i])
		}
		padValue := b.scalar(ins[0].typ, 0)
		if len(ins) > 2 && ins[2].layer >= 0 {
			padValue = ins[2]
		}
		return set(b.add(out, "Pad", "opset12", []attr{{"pad_mode", "constant"}}, []value{
			ins[0], b.ints([]int64{int64(half)}, begin...), b.ints([]int64{int64(half)}, end...), padValue,
		}, ins[0].typ)[0])
	case op == "OneHot":
		depth, _, err := b.constValues(n.Inputs[1])
		if err != nil {
			return fmt.Errorf("its depth: %w", err)
		}
		vals, t, err := b.constValues(n.Inputs[2])
		if err != nil || len(vals) != 2 {
			return fmt.Errorf("its values: %w", err)
		}
		axis := int64(-1)
		if a, ok := n.Attrs["axis"]; ok {
			axis = a.Int
		}
		return set(b.add(out, "OneHot", "opset1", []attr{{"axis", strconv.FormatInt(axis, 10)}}, []value{
			ins[0], b.ints(nil, int64(depth[0])), b.scalar(t, vals[1]), b.scalar(t, vals[0]),
		}, t)[0])
	}
	return fmt.Errorf("OpenVINO's stubs have no translation of ONNX's %s", n.Op)
}
