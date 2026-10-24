package tensorflow

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/onnx"
)

// OnnxOpset is the ai.onnx operator set of the ONNX stub a graph is
// translated from: the operators' forms Translate knows.
const OnnxOpset = 17

// Graph is a TensorFlow graph: its nodes, each an operation of its inputs
// ("node", or "node:1" for a node's output after its first) and its
// attributes.
type Graph struct {
	Nodes []Node
}

// Node is a node of a graph.
type Node struct {
	Name, Op string
	Inputs   []string
	Attrs    map[string]Attr
}

// AttrKind is the kind of an attribute's value.
type AttrKind int

// The kinds of attributes' values.
const (
	AttrString AttrKind = iota + 1
	AttrInt
	AttrBool
	AttrType
	AttrTypes
	AttrInts
	AttrShape
	AttrTensor
)

// Attr is an attribute's value: of its kind, its field.
type Attr struct {
	Kind  AttrKind
	S     string
	I     int64
	B     bool
	Type  contract.DType
	Types []contract.DType
	Ints  []int64
	// Shape is a shape's dimensions, −1 for one of any size.
	Shape []int64
	// Tensor is a constant's value.
	Tensor *Tensor
}

// Tensor is a constant tensor: its type, dimensions and raw values (little
// endian).
type Tensor struct {
	Type contract.DType
	Dims []int64
	Raw  []byte
}

func typeAttr(t contract.DType) Attr { return Attr{Kind: AttrType, Type: t} }
func intAttr(v int64) Attr           { return Attr{Kind: AttrInt, I: v} }
func boolAttr(v bool) Attr           { return Attr{Kind: AttrBool, B: v} }
func stringAttr(s string) Attr       { return Attr{Kind: AttrString, S: s} }
func intsAttr(vs ...int64) Attr      { return Attr{Kind: AttrInts, Ints: vs} }

// Signature is how a graph's inputs and outputs are named: each of the
// contract's tensors, by its name, as the tensor of the graph it is
// ("serving_default_images:0", "StatefulPartitionedCall:1").
type Signature struct {
	Inputs  map[string]string `json:"inputs"`
	Outputs map[string]string `json:"outputs"`
}

// value is a tensor of the graph: its name, and its type.
type value struct {
	name string
	typ  contract.DType
}

// builder builds a graph.
type builder struct {
	g      Graph
	values map[string]value
	inits  map[string]onnx.Initializer
}

// add adds a node of the operation on the inputs, of the attributes; its
// name is the graph's next, unless it is given one.
func (b *builder) add(name, op string, inputs []value, attrs map[string]Attr) string {
	if name == "" {
		name = fmt.Sprintf("axx/%d/%s", len(b.g.Nodes), op)
	}
	n := Node{Name: name, Op: op, Attrs: attrs}
	for _, in := range inputs {
		n.Inputs = append(n.Inputs, in.name)
	}
	b.g.Nodes = append(b.g.Nodes, n)
	return name
}

// op adds a node of an operation on the inputs, and returns its output, of
// the type out. Its T is its first input's type, unless attrs give it: an
// attribute of no kind (Attr{}) leaves it out, of an operation of no T.
func (b *builder) op(op string, out contract.DType, inputs []value, attrs map[string]Attr) value {
	if attrs == nil {
		attrs = map[string]Attr{}
	}
	if _, ok := attrs["T"]; !ok && len(inputs) > 0 {
		attrs["T"] = typeAttr(inputs[0].typ)
	}
	for k, a := range attrs {
		if a.Kind == 0 {
			delete(attrs, k)
		}
	}
	return value{name: b.add("", op, inputs, attrs), typ: out}
}

// constant adds a constant.
func (b *builder) constant(t contract.DType, dims []int64, raw []byte) value {
	name := b.add("", "Const", nil, map[string]Attr{
		"dtype": typeAttr(t),
		"value": {Kind: AttrTensor, Tensor: &Tensor{Type: t, Dims: slices.Clone(dims), Raw: raw}},
	})
	return value{name: name, typ: t}
}

// ints is a constant of int64s: a vector, or a scalar of no dims.
func (b *builder) ints(dims []int64, vs ...int64) value {
	fs := make([]float64, len(vs))
	for i, v := range vs {
		fs[i] = float64(v)
	}
	return b.constant("int64", dims, contract.DType("int64").Encode(fs))
}

func (b *builder) vector(vs ...int64) value { return b.ints([]int64{int64(len(vs))}, vs...) }

// input is the value of the ONNX graph's tensor name: a node's output, an
// input, or an initializer (a constant, added once).
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
// argument TensorFlow takes as an attribute, or that sets its form.
func (b *builder) constValues(name string) ([]int64, bool) {
	init, ok := b.inits[name]
	if !ok {
		return nil, false
	}
	vs := init.Values()
	out := make([]int64, len(vs))
	for i, v := range vs {
		out[i] = int64(v)
	}
	return out, true
}

// nodeName is the node of a tensor's name: "images" of "images:0".
func nodeName(tensor string) (string, int, error) {
	name, index, ok := strings.Cut(tensor, ":")
	if !ok {
		return tensor, 0, nil
	}
	var i int
	if _, err := fmt.Sscanf(index, "%d", &i); err != nil || i < 0 {
		return "", 0, fmt.Errorf("%q is not a tensor's name (node:index)", tensor)
	}
	return name, i, nil
}

// Translate is the graph of an ONNX stub's graph, of the contract's inputs,
// named as the signature has them: each input a placeholder, each output
// its node's (an Identity, or IdentityN of a node's outputs).
func Translate(g *onnx.Graph, c *contract.Contract, sig Signature) (*Graph, error) {
	if g.Opset != OnnxOpset {
		return nil, fmt.Errorf("its ONNX graph is of operator set %d, not %d", g.Opset, OnnxOpset)
	}
	b := &builder{values: map[string]value{}, inits: g.Initializers}
	for _, in := range c.Inputs {
		node, index, err := nodeName(sig.Inputs[in.Name])
		if err != nil || node == "" || index != 0 {
			return nil, fmt.Errorf("its input %s is the tensor %q, not a placeholder's (name:0)", in.Name, sig.Inputs[in.Name])
		}
		shape := make([]int64, len(in.Shape))
		for i, d := range in.Shape {
			shape[i] = -1
			if d.Fixed() {
				shape[i] = d.Size
			}
		}
		b.add(node, "Placeholder", nil, map[string]Attr{"dtype": typeAttr(in.Type), "shape": {Kind: AttrShape, Shape: shape}})
		b.values[in.Name] = value{name: node, typ: in.Type}
	}
	for _, n := range g.Nodes {
		if err := b.node(n); err != nil {
			return nil, fmt.Errorf("its %s node %s: %w", n.Op, n.Outputs[0], err)
		}
	}
	// The outputs, by their nodes: each node's, in the order of its
	// outputs.
	byNode := map[string][]value{}
	var nodes []string
	for i, name := range g.Outputs {
		v, err := b.input(name)
		if err != nil {
			return nil, err
		}
		out := c.Outputs[i].Name
		node, index, err := nodeName(sig.Outputs[out])
		if err != nil || node == "" {
			return nil, fmt.Errorf("its output %s is the tensor %q, not a node's", out, sig.Outputs[out])
		}
		if _, ok := byNode[node]; !ok {
			nodes = append(nodes, node)
		}
		outs := byNode[node]
		for len(outs) <= index {
			outs = append(outs, value{})
		}
		outs[index] = v
		byNode[node] = outs
	}
	for _, node := range nodes {
		outs := byNode[node]
		for i, v := range outs {
			if v.name == "" {
				return nil, fmt.Errorf("its outputs are of the node %s, and none is its output %d", node, i)
			}
		}
		if len(outs) == 1 {
			b.add(node, "Identity", outs, map[string]Attr{"T": typeAttr(outs[0].typ)})
			continue
		}
		types := make([]contract.DType, len(outs))
		for i, v := range outs {
			types[i] = v.typ
		}
		b.add(node, "IdentityN", outs, map[string]Attr{"T": {Kind: AttrTypes, Types: types}})
	}
	return &b.g, nil
}

// binaryOps are ONNX's elementwise operators of two inputs, as
// TensorFlow's.
var binaryOps = map[string]string{"Add": "AddV2", "Sub": "Sub", "Mul": "Mul", "Div": "RealDiv", "Max": "Maximum", "Min": "Minimum"}

var compareOps = map[string]string{"Equal": "Equal", "Greater": "Greater", "Less": "Less"}

var reduceOps = map[string]string{"ReduceMax": "Max", "ReduceMin": "Min", "ReduceMean": "Mean"}

// node adds the nodes of an ONNX node.
func (b *builder) node(n onnx.Node) error {
	var ins []value
	for _, name := range n.Inputs {
		if name == "" {
			ins = append(ins, value{})
			continue
		}
		v, err := b.input(name)
		if err != nil {
			return err
		}
		ins = append(ins, v)
	}
	set := func(v value) error {
		b.values[n.Outputs[0]] = v
		return nil
	}
	attr := func(name string, def int64) int64 {
		if a, ok := n.Attrs[name]; ok {
			return a.Int
		}
		return def
	}
	x := ins[0]
	long := typeAttr("int64")

	switch op := n.Op; {
	case binaryOps[op] != "":
		tf := binaryOps[op]
		if op == "Div" && x.typ.Integer() {
			tf = "FloorDiv"
		}
		v := x
		for _, y := range ins[1:] {
			v = b.op(tf, x.typ, []value{v, y}, nil)
		}
		return set(v)
	case compareOps[op] != "":
		return set(b.op(compareOps[op], "bool", ins[:2], nil))
	case op == "And":
		return set(b.op("LogicalAnd", "bool", ins[:2], map[string]Attr{"T": {}}))
	case op == "Not":
		return set(b.op("LogicalNot", "bool", ins[:1], map[string]Attr{"T": {}}))
	case op == "Where":
		return set(b.op("SelectV2", ins[1].typ, ins[:3], map[string]Attr{"T": typeAttr(ins[1].typ)}))
	case op == "Sqrt" || op == "Log" || op == "Round":
		return set(b.op(op, x.typ, ins[:1], nil))
	case op == "Identity":
		return set(x)
	case op == "MatMul":
		return set(b.op("BatchMatMulV2", x.typ, ins[:2], nil))
	case op == "Cast":
		t, ok := onnx.TypeOf(attr("to", 0))
		if !ok {
			return fmt.Errorf("a cast to ONNX type %d", attr("to", 0))
		}
		return set(b.op("Cast", t, ins[:1], map[string]Attr{"T": {}, "SrcT": typeAttr(x.typ), "DstT": typeAttr(t)}))
	case op == "Reshape":
		shape := ins[1]
		if want, ok := b.constValues(n.Inputs[1]); ok && slices.Contains(want, 0) {
			// ONNX's 0 keeps the input's dimension: of its dimensions, as
			// many as the shape has (padded: the input may have fewer).
			dims := b.op("Shape", "int64", []value{x}, map[string]Attr{"out_type": long})
			dims = b.op("ConcatV2", "int64", []value{dims, b.vector(make([]int64, len(want))...), b.ints(nil, 0)}, map[string]Attr{"N": intAttr(2), "Tidx": long})
			first := b.op("StridedSlice", "int64", []value{dims, b.vector(0), b.vector(int64(len(want))), b.vector(1)}, map[string]Attr{"Index": long})
			keep := b.op("Equal", "bool", []value{shape, b.ints(nil, 0)}, nil)
			shape = b.op("SelectV2", "int64", []value{keep, first, shape}, map[string]Attr{"T": long})
		}
		return set(b.op("Reshape", x.typ, []value{x, shape}, map[string]Attr{"Tshape": long}))
	case op == "Transpose":
		perm := n.Attrs["perm"].Ints
		return set(b.op("Transpose", x.typ, []value{x, b.vector(perm...)}, map[string]Attr{"Tperm": long}))
	case op == "Unsqueeze":
		axes, ok := b.constValues(n.Inputs[1])
		if !ok {
			return fmt.Errorf("its axes are not a constant")
		}
		slices.Sort(axes)
		v := x
		for _, a := range axes {
			if a < 0 {
				return fmt.Errorf("an axis of its end (%d)", a)
			}
			v = b.op("ExpandDims", x.typ, []value{v, b.ints(nil, a)}, map[string]Attr{"Tdim": long})
		}
		return set(v)
	case op == "Expand":
		// ONNX's broadcasts both ways: to the shape both broadcast to.
		dims := b.op("Shape", "int64", []value{x}, map[string]Attr{"out_type": long})
		to := b.op("BroadcastArgs", "int64", []value{dims, ins[1]}, map[string]Attr{"T": long})
		return set(b.op("BroadcastTo", x.typ, []value{x, to}, map[string]Attr{"Tidx": long}))
	case op == "Shape":
		return set(b.op("Shape", "int64", ins[:1], map[string]Attr{"out_type": long}))
	case op == "Range":
		return set(b.op("Range", x.typ, ins[:3], map[string]Attr{"T": {}, "Tidx": typeAttr(x.typ)}))
	case op == "Concat":
		return set(b.op("ConcatV2", x.typ, append(slices.Clone(ins), b.ints(nil, attr("axis", 0))), map[string]Attr{"N": intAttr(int64(len(ins))), "Tidx": long}))
	case op == "Gather":
		return set(b.op("GatherV2", x.typ, []value{x, ins[1], b.ints(nil, attr("axis", 0))}, map[string]Attr{
			"T": {}, "Tparams": typeAttr(x.typ), "Tindices": typeAttr(ins[1].typ), "Taxis": long, "batch_dims": intAttr(0),
		}))
	case op == "AveragePool":
		k, st := n.Attrs["kernel_shape"].Ints, n.Attrs["strides"].Ints
		if len(k) != 2 {
			return fmt.Errorf("a pool of %d dimensions", len(k))
		}
		// TensorFlow pools channels last.
		last := b.op("Transpose", x.typ, []value{x, b.vector(0, 2, 3, 1)}, map[string]Attr{"Tperm": long})
		pooled := b.op("AvgPool", x.typ, []value{last}, map[string]Attr{
			"ksize": intsAttr(1, k[0], k[1], 1), "strides": intsAttr(1, st[0], st[1], 1),
			"padding": stringAttr("VALID"), "data_format": stringAttr("NHWC"),
		})
		return set(b.op("Transpose", x.typ, []value{pooled, b.vector(0, 3, 1, 2)}, map[string]Attr{"Tperm": long}))
	case op == "ArgMax":
		a := attr("axis", 0)
		v := b.op("ArgMax", "int64", []value{x, b.ints(nil, a)}, map[string]Attr{"Tidx": long, "output_type": long})
		if attr("keepdims", 1) == 1 {
			v = b.op("ExpandDims", "int64", []value{v, b.ints(nil, a)}, map[string]Attr{"Tdim": long})
		}
		return set(v)
	case reduceOps[op] != "":
		var axes value
		if a, ok := n.Attrs["axes"]; ok {
			axes = b.vector(a.Ints...)
		} else {
			// Every axis: 0 to its rank.
			rank := b.op("Cast", "int64", []value{b.op("Rank", "int32", []value{x}, nil)}, map[string]Attr{"T": {}, "SrcT": typeAttr("int32"), "DstT": long})
			axes = b.op("Range", "int64", []value{b.ints(nil, 0), rank, b.ints(nil, 1)}, map[string]Attr{"T": {}, "Tidx": long})
		}
		return set(b.op(reduceOps[op], x.typ, []value{x, axes}, map[string]Attr{"Tidx": long, "keep_dims": boolAttr(attr("keepdims", 1) == 1)}))
	case op == "Slice":
		return b.slice(n, ins, set)
	case op == "Pad":
		pads, ok := b.constValues(n.Inputs[1])
		if !ok {
			return fmt.Errorf("its pads are not a constant")
		}
		r := len(pads) / 2
		paddings := make([]int64, 0, 2*r)
		for i := range r {
			paddings = append(paddings, pads[i], pads[r+i])
		}
		fill := b.constant(x.typ, nil, x.typ.Encode([]float64{0}))
		if len(ins) > 2 && ins[2].name != "" {
			fill = ins[2]
		}
		return set(b.op("PadV2", x.typ, []value{x, b.ints([]int64{int64(r), 2}, paddings...), fill}, map[string]Attr{"Tpaddings": long}))
	case op == "OneHot":
		depth, ok := b.constValues(n.Inputs[1])
		if !ok {
			return fmt.Errorf("its depth is not a constant")
		}
		vals, ok := b.inits[n.Inputs[2]]
		if !ok {
			return fmt.Errorf("its values are not a constant")
		}
		onOff := vals.Values()
		t := vals.Type
		depthValue := b.constant("int32", nil, contract.DType("int32").Encode([]float64{float64(depth[0])}))
		return set(b.op("OneHot", t, []value{x, depthValue, b.constant(t, nil, t.Encode(onOff[1:2])), b.constant(t, nil, t.Encode(onOff[:1]))}, map[string]Attr{
			"T": typeAttr(t), "TI": typeAttr(x.typ), "axis": intAttr(attr("axis", -1)),
		}))
	}
	return fmt.Errorf("TensorFlow's stubs have no translation of ONNX's %s", n.Op)
}

// slice adds an ONNX Slice's nodes: a StridedSlice of each of its axes
// (their starts and ends as they are, which TensorFlow clamps as ONNX does),
// its steps 1.
func (b *builder) slice(n onnx.Node, ins []value, set func(value) error) error {
	x := ins[0]
	if len(ins) > 4 && ins[4].name != "" {
		if steps, ok := b.constValues(n.Inputs[4]); !ok || slices.ContainsFunc(steps, func(s int64) bool { return s != 1 }) {
			return fmt.Errorf("a slice of steps")
		}
	}
	var axes []int64
	if len(ins) > 3 && ins[3].name != "" {
		var ok bool
		if axes, ok = b.constValues(n.Inputs[3]); !ok {
			return fmt.Errorf("its axes are not a constant")
		}
	} else {
		starts, ok := b.constValues(n.Inputs[1])
		if !ok {
			return fmt.Errorf("its starts are not a constant, and it gives no axes")
		}
		for i := range starts {
			axes = append(axes, int64(i))
		}
	}
	long := typeAttr("int64")
	// bound is the vector of a bound of the axis at i: zeros before it,
	// then its own.
	bound := func(of value, name string, i int, a int64) value {
		if vs, ok := b.constValues(name); ok {
			return b.vector(append(make([]int64, a), vs[i])...)
		}
		own := b.op("StridedSlice", "int64", []value{of, b.vector(int64(i)), b.vector(int64(i + 1)), b.vector(1)}, map[string]Attr{"Index": long})
		if a == 0 {
			return own
		}
		return b.op("ConcatV2", "int64", []value{b.vector(make([]int64, a)...), own, b.ints(nil, 0)}, map[string]Attr{"N": intAttr(2), "Tidx": long})
	}
	v := x
	for i, a := range axes {
		if a < 0 {
			return fmt.Errorf("an axis of its end (%d)", a)
		}
		ones := make([]int64, a+1)
		for k := range ones {
			ones[k] = 1
		}
		before := int64(1)<<a - 1
		v = b.op("StridedSlice", x.typ, []value{v, bound(ins[1], n.Inputs[1], i, a), bound(ins[2], n.Inputs[2], i, a), b.vector(ones...)}, map[string]Attr{
			"Index": long, "begin_mask": intAttr(before), "end_mask": intAttr(before),
			"ellipsis_mask": intAttr(0), "new_axis_mask": intAttr(0), "shrink_axis_mask": intAttr(0),
		})
	}
	return set(v)
}
