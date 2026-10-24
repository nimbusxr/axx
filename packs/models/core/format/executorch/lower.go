package executorch

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/onnx"
)

// onnxOpset is the ai.onnx operator set of the ONNX stub a program is
// lowered from: the operators' forms lower knows.
const onnxOpset = 17

// tensorInfo is a graph tensor's type and shape (ExecuTorch's are fixed),
// its value's index in the program, and its initializer's values, for a
// constant.
type tensorInfo struct {
	typ   contract.DType
	shape []int64
	value int
	init  *onnx.Initializer
}

// lowering lowers an ONNX stub's graph into a plan of a program, whose
// constants it adds to the program's.
type lowering struct {
	p    *program
	pl   *plan
	vals map[string]*tensorInfo
	ops  map[[2]string]int
}

func (l *lowering) add(v evalue) int {
	l.pl.values = append(l.pl.values, v)
	return len(l.pl.values) - 1
}

func (l *lowering) integer(v int64) int  { return l.add(evalue{kind: evInt, i: v}) }
func (l *lowering) boolean(v bool) int   { return l.add(evalue{kind: evBool, b: v}) }
func (l *lowering) double(v float64) int { return l.add(evalue{kind: evDouble, d: v}) }
func (l *lowering) null() int            { return l.add(evalue{kind: evNull}) }

// intList is a list of ints: its items are values of their own.
func (l *lowering) intList(vs ...int64) int {
	idx := make([]int64, len(vs))
	for i, v := range vs {
		idx[i] = int64(l.integer(v))
	}
	return l.add(evalue{kind: evIntList, ints: idx})
}

// tensorList is a list of tensors' values.
func (l *lowering) tensorList(vs ...int) int {
	idx := make([]int64, len(vs))
	for i, v := range vs {
		idx[i] = int64(v)
	}
	return l.add(evalue{kind: evTensorList, ints: idx})
}

// alloc is a tensor of the planned memory.
func (l *lowering) alloc(t contract.DType, shape []int64) int {
	l.pl.memory = (l.pl.memory + 15) / 16 * 16
	offset := l.pl.memory
	l.pl.memory += uint64(numel(shape) * t.Size())
	return l.add(evalue{kind: evTensor, tensor: &tensor{typ: t, sizes: slices.Clone(shape), offset: offset}})
}

// constant is a constant tensor of the program.
func (l *lowering) constant(t contract.DType, shape []int64, raw []byte) int {
	l.p.constants = append(l.p.constants, raw)
	return l.add(evalue{kind: evTensor, tensor: &tensor{typ: t, sizes: slices.Clone(shape), constant: len(l.p.constants)}})
}

// scalar is a constant tensor of one value, of no dimensions.
func (l *lowering) scalar(t contract.DType, v float64) int {
	return l.constant(t, nil, t.Encode([]float64{v}))
}

// call adds a kernel's call: its arguments, then its out tensor (its
// return, the same value, after it).
func (l *lowering) call(name, overload string, args ...int) {
	key := [2]string{name, overload}
	op, ok := l.ops[key]
	if !ok {
		op = len(l.pl.ops)
		l.ops[key] = op
		l.pl.ops = append(l.pl.ops, key)
	}
	l.pl.calls = append(l.pl.calls, kernelCall{op: op, args: append(args, args[len(args)-1])})
}

// kernel adds a call of a new out tensor of the type and shape, and returns
// the out tensor's value.
func (l *lowering) kernel(name, overload string, t contract.DType, shape []int64, args ...int) int {
	out := l.alloc(t, shape)
	l.call(name, overload, append(args, out)...)
	return out
}

func numel(shape []int64) int {
	n := 1
	for _, d := range shape {
		n *= int(d)
	}
	return n
}

// input is the info of the graph's tensor name: a node's output, an input,
// or an initializer (its constant, added once).
func (l *lowering) input(g *onnx.Graph, name string) (*tensorInfo, error) {
	if t, ok := l.vals[name]; ok {
		return t, nil
	}
	init, ok := g.Initializers[name]
	if !ok {
		return nil, fmt.Errorf("its graph has no tensor %q", name)
	}
	t := &tensorInfo{typ: init.Type, shape: init.Dims, init: &init}
	t.value = l.constant(init.Type, init.Dims, init.Raw)
	l.vals[name] = t
	return t, nil
}

// lower lowers the graph of an ONNX stub of the contract into a plan of the
// program, the method name.
func lower(g *onnx.Graph, c *contract.Contract, p *program, name string) (*plan, error) {
	l := newLowering(p, name)
	for _, in := range c.Inputs {
		shape := make([]int64, len(in.Shape))
		for i, d := range in.Shape {
			if !d.Fixed() {
				return nil, fmt.Errorf("its input %s has an open dimension (%s): ExecuTorch's programs are of fixed shapes", in.Name, in.Shape)
			}
			shape[i] = d.Size
		}
		t := &tensorInfo{typ: in.Type, shape: shape}
		t.value = l.alloc(in.Type, shape)
		l.vals[in.Name] = t
		l.pl.inputs = append(l.pl.inputs, t.value)
	}
	return l.graph(g, c)
}

// newLowering is a lowering of a new plan of the program, the method name.
func newLowering(p *program, name string) *lowering {
	return &lowering{p: p, pl: &plan{name: name}, vals: map[string]*tensorInfo{}, ops: map[[2]string]int{}}
}

// graph lowers the graph's nodes and outputs into the plan, its inputs'
// values the lowering's already (the contract's inputs'), and returns it.
func (l *lowering) graph(g *onnx.Graph, c *contract.Contract) (*plan, error) {
	if g.Opset != onnxOpset {
		return nil, fmt.Errorf("its ONNX graph is of operator set %d, not %d", g.Opset, onnxOpset)
	}
	for _, n := range g.Nodes {
		if err := l.node(g, n); err != nil {
			return nil, fmt.Errorf("its %s node %s: %w", n.Op, n.Outputs[0], err)
		}
	}
	for _, name := range g.Outputs {
		t, err := l.input(g, name)
		if err != nil {
			return nil, err
		}
		v := t.value
		if t.init != nil || slices.Contains(l.pl.inputs, v) || slices.Contains(l.pl.outputs, v) {
			// An output the program fills itself: a copy.
			v = l.kernel("dim_order_ops::_clone_dim_order", "out", t.typ, t.shape, v, l.boolean(false), l.intList(dimOrder(t.shape)...))
		}
		l.pl.outputs = append(l.pl.outputs, v)
	}
	l.pl.inMeta, l.pl.outMeta = treeSpecs(len(c.Inputs), len(g.Outputs))
	return l.pl, nil
}

// treeSpecs are a method's container metadata of its inputs and outputs
// (pytree's specs): a tuple of its inputs and no keyword arguments, and its
// output or a tuple of them.
func treeSpecs(inputs, outputs int) (in, out string) {
	leaf := `{"type": null, "context": null, "children_spec": []}`
	in = `[1, {"type": "builtins.tuple", "context": "null", "children_spec": [{"type": "builtins.tuple", "context": "null", "children_spec": [` +
		strings.Join(slices.Repeat([]string{leaf}, inputs), ", ") +
		`]}, {"type": "builtins.dict", "context": "[]", "children_spec": []}]}]`
	if outputs == 1 {
		return in, "[1, " + leaf + "]"
	}
	return in, `[1, {"type": "builtins.tuple", "context": "null", "children_spec": [` + strings.Join(slices.Repeat([]string{leaf}, outputs), ", ") + `]}]`
}

func dimOrder(shape []int64) []int64 {
	out := make([]int64, len(shape))
	for i := range out {
		out[i] = int64(i)
	}
	return out
}

// broadcast is the shape numpy's broadcasting gives shapes.
func broadcast(shapes ...[]int64) ([]int64, error) {
	rank := 0
	for _, s := range shapes {
		rank = max(rank, len(s))
	}
	out := make([]int64, rank)
	for i := range out {
		out[i] = 1
	}
	for _, s := range shapes {
		for i, d := range s {
			at := rank - len(s) + i
			switch {
			case d == out[at], d == 1:
			case out[at] == 1:
				out[at] = d
			default:
				return nil, fmt.Errorf("shapes %v do not broadcast", shapes)
			}
		}
	}
	return out, nil
}

// axis is an axis of a rank, from its negative too.
func axis(a int64, rank int) int {
	if a < 0 {
		a += int64(rank)
	}
	return int(a)
}

var elementwise = map[string][3]string{
	"Add": {"aten::add", "out", "alpha"}, "Sub": {"aten::sub", "out", "alpha"},
	"Mul": {"aten::mul", "out"}, "Div": {"aten::div", "out"},
	"Max": {"aten::maximum", "out"}, "Min": {"aten::minimum", "out"},
	"Equal": {"aten::eq", "Tensor_out", "bool"}, "Greater": {"aten::gt", "Tensor_out", "bool"},
	"Less": {"aten::lt", "Tensor_out", "bool"}, "And": {"aten::logical_and", "out", "bool"},
}

var unary = map[string][2]string{
	"Sqrt": {"aten::sqrt", "out"}, "Log": {"aten::log", "out"}, "Round": {"aten::round", "out"},
	"Not": {"aten::logical_not", "out"},
}

var reduces = map[string]string{"ReduceMax": "aten::amax", "ReduceMin": "aten::amin", "ReduceMean": "aten::mean"}

// node lowers a node: its out tensors' shapes, and its kernels' calls.
func (l *lowering) node(g *onnx.Graph, n onnx.Node) error {
	var ins []*tensorInfo
	for _, name := range n.Inputs {
		if name == "" {
			ins = append(ins, nil)
			continue
		}
		t, err := l.input(g, name)
		if err != nil {
			return err
		}
		ins = append(ins, t)
	}
	set := func(t contract.DType, shape []int64, value int) error {
		l.vals[n.Outputs[0]] = &tensorInfo{typ: t, shape: shape, value: value}
		return nil
	}
	x := ins[0]
	attr := func(name string, def int64) int64 {
		if a, ok := n.Attrs[name]; ok {
			return a.Int
		}
		return def
	}
	constInts := func(i int) ([]int64, error) {
		if i >= len(ins) || ins[i] == nil || ins[i].init == nil {
			return nil, fmt.Errorf("its input %d is not a constant", i)
		}
		vs := ins[i].init.Values()
		out := make([]int64, len(vs))
		for k, v := range vs {
			out[k] = int64(v)
		}
		return out, nil
	}

	switch op := n.Op; {
	case elementwise[op] != [3]string{}:
		e := elementwise[op]
		v, t, shape := x.value, x.typ, x.shape
		for _, y := range ins[1:] {
			s, err := broadcast(shape, y.shape)
			if err != nil {
				return err
			}
			out := t
			if e[2] == "bool" {
				out = "bool"
			}
			args := []int{v, y.value}
			if e[2] == "alpha" {
				args = append(args, l.integer(1))
			}
			v, shape, t = l.kernel(e[0], e[1], out, s, args...), s, out
		}
		return set(t, shape, v)
	case unary[op] != [2]string{}:
		u := unary[op]
		t := x.typ
		if op == "Not" {
			t = "bool"
		}
		return set(t, x.shape, l.kernel(u[0], u[1], t, x.shape, x.value))
	case op == "Identity":
		l.vals[n.Outputs[0]] = x
		return nil
	case op == "Where":
		s, err := broadcast(x.shape, ins[1].shape, ins[2].shape)
		if err != nil {
			return err
		}
		return set(ins[1].typ, s, l.kernel("aten::where", "self_out", ins[1].typ, s, x.value, ins[1].value, ins[2].value))
	case op == "Cast":
		t, ok := onnx.TypeOf(attr("to", 0))
		if !ok {
			return fmt.Errorf("a cast to ONNX type %d", attr("to", 0))
		}
		return set(t, x.shape, l.kernel("dim_order_ops::_to_dim_order_copy", "out", t, x.shape, x.value, l.boolean(false), l.intList(dimOrder(x.shape)...)))
	case op == "Reshape":
		want, err := constInts(1)
		if err != nil {
			return err
		}
		shape := make([]int64, len(want))
		known, infer := 1, -1
		for i, d := range want {
			switch d {
			case 0:
				shape[i] = x.shape[i]
			case -1:
				infer = i
				continue
			default:
				shape[i] = d
			}
			known *= int(shape[i])
		}
		if infer >= 0 {
			shape[infer] = int64(numel(x.shape) / max(known, 1))
		}
		return set(x.typ, shape, l.kernel("aten::view_copy", "out", x.typ, shape, x.value, l.intList(shape...)))
	case op == "Transpose":
		perm := n.Attrs["perm"].Ints
		shape := make([]int64, len(perm))
		for i, p := range perm {
			shape[i] = x.shape[p]
		}
		return set(x.typ, shape, l.kernel("aten::permute_copy", "out", x.typ, shape, x.value, l.intList(perm...)))
	case op == "Unsqueeze":
		axes, err := constInts(1)
		if err != nil {
			return err
		}
		rank := len(x.shape) + len(axes)
		norm := make([]int, len(axes))
		for i, a := range axes {
			norm[i] = axis(a, rank)
		}
		slices.Sort(norm)
		v, shape := x.value, slices.Clone(x.shape)
		for _, a := range norm {
			shape = slices.Insert(shape, a, 1)
			v = l.kernel("aten::unsqueeze_copy", "out", x.typ, shape, v, l.integer(int64(a)))
		}
		return set(x.typ, shape, v)
	case op == "Expand":
		want, err := constInts(1)
		if err != nil {
			return err
		}
		shape, err := broadcast(x.shape, want)
		if err != nil {
			return err
		}
		return set(x.typ, shape, l.kernel("aten::expand_copy", "out", x.typ, shape, x.value, l.intList(shape...), l.boolean(false)))
	case op == "AveragePool":
		k, st := n.Attrs["kernel_shape"].Ints, n.Attrs["strides"].Ints
		if len(k) != 2 || len(x.shape) != 4 {
			return fmt.Errorf("a pool of %d dimensions", len(k))
		}
		shape := []int64{x.shape[0], x.shape[1], (x.shape[2]-k[0])/st[0] + 1, (x.shape[3]-k[1])/st[1] + 1}
		return set(x.typ, shape, l.kernel("aten::avg_pool2d", "out", x.typ, shape,
			x.value, l.intList(k...), l.intList(st...), l.intList(0, 0), l.boolean(false), l.boolean(true), l.null()))
	case op == "MatMul":
		y := ins[1]
		if len(y.shape) != 2 || len(x.shape) < 2 {
			return fmt.Errorf("a product of %v and %v", x.shape, y.shape)
		}
		rows := int64(numel(x.shape[:len(x.shape)-1]))
		shape := append(slices.Clone(x.shape[:len(x.shape)-1]), y.shape[1])
		a := x.value
		if len(x.shape) > 2 {
			a = l.kernel("aten::view_copy", "out", x.typ, []int64{rows, x.shape[len(x.shape)-1]}, a, l.intList(rows, x.shape[len(x.shape)-1]))
		}
		v := l.kernel("aten::mm", "out", x.typ, []int64{rows, y.shape[1]}, a, y.value)
		if len(x.shape) > 2 {
			v = l.kernel("aten::view_copy", "out", x.typ, shape, v, l.intList(shape...))
		}
		return set(x.typ, shape, v)
	case op == "ArgMax":
		a := axis(attr("axis", 0), len(x.shape))
		keep := attr("keepdims", 1) == 1
		shape := slices.Clone(x.shape)
		if keep {
			shape[a] = 1
		} else {
			shape = slices.Delete(shape, a, a+1)
		}
		return set("int64", shape, l.kernel("aten::argmax", "out", "int64", shape, x.value, l.integer(int64(a)), l.boolean(keep)))
	case reduces[op] != "":
		axes := n.Attrs["axes"].Ints
		if _, ok := n.Attrs["axes"]; !ok {
			axes = dimOrder(x.shape)
		}
		keep := attr("keepdims", 1) == 1
		var drop []int
		shape := slices.Clone(x.shape)
		for _, a := range axes {
			i := axis(a, len(x.shape))
			shape[i] = 1
			drop = append(drop, i)
		}
		if !keep {
			slices.Sort(drop)
			for k := len(drop) - 1; k >= 0; k-- {
				shape = slices.Delete(shape, drop[k], drop[k]+1)
			}
		}
		args := []int{x.value, l.intList(axes...), l.boolean(keep)}
		if op == "ReduceMean" {
			args = append(args, l.null())
		}
		return set(x.typ, shape, l.kernel(reduces[op], "out", x.typ, shape, args...))
	case op == "Gather":
		a := axis(attr("axis", 0), len(x.shape))
		idx := ins[1]
		shape := append(append(slices.Clone(x.shape[:a]), idx.shape...), x.shape[a+1:]...)
		if len(idx.shape) == 0 && idx.init != nil {
			return set(x.typ, shape, l.kernel("aten::select_copy", "int_out", x.typ, shape, x.value, l.integer(int64(a)), l.integer(int64(idx.init.Values()[0]))))
		}
		flat := idx.value
		count := int64(numel(idx.shape))
		if len(idx.shape) != 1 {
			flat = l.kernel("aten::view_copy", "out", idx.typ, []int64{count}, flat, l.intList(count))
		}
		picked := append(append(slices.Clone(x.shape[:a]), count), x.shape[a+1:]...)
		v := l.kernel("aten::index_select", "out", x.typ, picked, x.value, l.integer(int64(a)), flat)
		if len(idx.shape) != 1 {
			v = l.kernel("aten::view_copy", "out", x.typ, shape, v, l.intList(shape...))
		}
		return set(x.typ, shape, v)
	case op == "Pad":
		pads, err := constInts(1)
		if err != nil {
			return err
		}
		value := 0.0
		if len(ins) > 2 && ins[2] != nil && ins[2].init != nil {
			value = ins[2].init.Values()[0]
		}
		r := len(x.shape)
		shape := slices.Clone(x.shape)
		var list []int64
		for i := r - 1; i >= 0; i-- {
			list = append(list, pads[i], pads[r+i])
		}
		for i := range shape {
			shape[i] += pads[i] + pads[r+i]
		}
		return set(x.typ, shape, l.kernel("aten::constant_pad_nd", "out", x.typ, shape, x.value, l.intList(list...), l.double(value)))
	case op == "Concat":
		a := axis(attr("axis", 0), len(x.shape))
		shape := slices.Clone(x.shape)
		vs := []int{x.value}
		for _, y := range ins[1:] {
			shape[a] += y.shape[a]
			vs = append(vs, y.value)
		}
		return set(x.typ, shape, l.kernel("aten::cat", "out", x.typ, shape, l.tensorList(vs...), l.integer(int64(a))))
	case op == "Slice":
		starts, err := constInts(1)
		if err != nil {
			return err
		}
		ends, err := constInts(2)
		if err != nil {
			return err
		}
		axes := dimOrder(starts)
		if len(ins) > 3 && ins[3] != nil {
			if axes, err = constInts(3); err != nil {
				return err
			}
		}
		if len(ins) > 4 && ins[4] != nil {
			return fmt.Errorf("a slice of steps")
		}
		v, shape := x.value, slices.Clone(x.shape)
		for i, a := range axes {
			d := axis(a, len(shape))
			size := shape[d]
			clamp := func(p int64) int64 {
				if p < 0 {
					p += size
				}
				return max(0, min(p, size))
			}
			from, to := clamp(starts[i]), clamp(ends[i])
			shape[d] = max(0, to-from)
			v = l.kernel("aten::slice_copy", "Tensor_out", x.typ, shape, v, l.integer(int64(d)), l.integer(from), l.integer(to), l.integer(1))
		}
		return set(x.typ, shape, v)
	case op == "OneHot":
		depth, err := constInts(1)
		if err != nil {
			return err
		}
		vals := ins[2]
		if vals == nil || vals.init == nil {
			return fmt.Errorf("its values are not a constant")
		}
		onOff := vals.init.Values()
		if a := attr("axis", -1); a != -1 && axis(a, len(x.shape)+1) != len(x.shape) {
			return fmt.Errorf("a one-hot of axis %d", a)
		}
		indices := x.value
		if x.typ != "int64" {
			indices = l.kernel("dim_order_ops::_to_dim_order_copy", "out", "int64", x.shape, indices, l.boolean(false), l.intList(dimOrder(x.shape)...))
		}
		col := append(slices.Clone(x.shape), 1)
		indices = l.kernel("aten::unsqueeze_copy", "out", "int64", col, indices, l.integer(int64(len(x.shape))))
		ar := l.kernel("aten::arange", "start_out", "int64", []int64{depth[0]}, l.integer(0), l.integer(depth[0]), l.integer(1))
		shape := append(slices.Clone(x.shape), depth[0])
		hot := l.kernel("aten::eq", "Tensor_out", "bool", shape, indices, ar)
		return set(vals.typ, shape, l.kernel("aten::where", "self_out", vals.typ, shape, hot, l.scalar(vals.typ, onOff[1]), l.scalar(vals.typ, onOff[0])))
	}
	return fmt.Errorf("ExecuTorch's stubs have no lowering of ONNX's %s", n.Op)
}
