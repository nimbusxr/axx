package torchscript

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/onnx"
)

// onnxOpset is the ai.onnx operator set of the ONNX stub a module's code is
// translated from: the operators' forms translate knows.
const onnxOpset = 17

// value is a value of the code: a tensor's expression and type, or a list
// of ints (a shape's, as torch.size gives it).
type value struct {
	expr string
	typ  contract.DType
	list bool
}

// coder writes a module's forward: its statements, and its constants.
type coder struct {
	lines     []string
	values    map[string]value
	inits     map[string]onnx.Initializer
	constants []constant
	n         int
}

// let adds a statement of the expression, and is its variable.
func (c *coder) let(t contract.DType, format string, args ...any) value {
	name := fmt.Sprintf("_%d", c.n)
	c.n++
	c.lines = append(c.lines, fmt.Sprintf("    %s = %s", name, fmt.Sprintf(format, args...)))
	return value{expr: name, typ: t}
}

// tensor is a value as a tensor: a list's, of int64s.
func (c *coder) tensor(v value) value {
	if !v.list {
		return v
	}
	return c.let("int64", "torch.tensor(%s, dtype=4, device=None, requires_grad=False)", v.expr)
}

// input is the value of the ONNX graph's tensor name: a node's output, an
// input, or an initializer (a constant of the module, added once).
func (c *coder) input(name string) (value, error) {
	if v, ok := c.values[name]; ok {
		return v, nil
	}
	init, ok := c.inits[name]
	if !ok {
		return value{}, fmt.Errorf("its graph has no tensor %q", name)
	}
	c.constants = append(c.constants, constant{typ: init.Type, dims: init.Dims, raw: init.Raw})
	v := value{expr: fmt.Sprintf("CONSTANTS.c%d", len(c.constants)-1), typ: init.Type}
	c.values[name] = v
	return v, nil
}

// ints are the initializer name's values, as ints: an operator's argument
// TorchScript takes as a list.
func (c *coder) ints(name string) ([]int64, bool) {
	init, ok := c.inits[name]
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

// list is a list literal of ints.
func list(vs ...int64) string {
	s := make([]string, len(vs))
	for i, v := range vs {
		s[i] = strconv.FormatInt(v, 10)
	}
	return "[" + strings.Join(s, ", ") + "]"
}

// scalar is a number literal of TorchScript.
func scalar(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "inf"
	case math.IsInf(v, -1):
		return "-inf"
	case v == math.Trunc(v) && math.Abs(v) < 1e15:
		return strconv.FormatFloat(v, 'f', 1, 64)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// translate is a module's forward of an ONNX stub's graph: its parameters
// the contract's inputs, in their order, its return its outputs (a tuple of
// more than one).
func translate(g *onnx.Graph, c *contract.Contract) (string, []constant, error) {
	if g.Opset != onnxOpset {
		return "", nil, fmt.Errorf("its ONNX graph is of operator set %d, not %d", g.Opset, onnxOpset)
	}
	cd := &coder{values: map[string]value{}, inits: g.Initializers}
	for _, in := range c.Inputs {
		cd.values[in.Name] = value{expr: in.Name, typ: in.Type}
	}
	for _, n := range g.Nodes {
		if err := cd.node(n); err != nil {
			return "", nil, fmt.Errorf("its %s node %s: %w", n.Op, n.Outputs[0], err)
		}
	}
	var outs []string
	for _, name := range g.Outputs {
		v, err := cd.input(name)
		if err != nil {
			return "", nil, err
		}
		outs = append(outs, cd.tensor(v).expr)
	}
	ret := outs[0]
	if len(outs) > 1 {
		ret = "(" + strings.Join(outs, ", ") + ")"
	}
	cd.lines = append(cd.lines, "    return "+ret)
	return strings.Join(cd.lines, "\n") + "\n", cd.constants, nil
}

var binaryOps = map[string]string{"Add": "torch.add", "Sub": "torch.sub", "Mul": "torch.mul", "Max": "torch.maximum", "Min": "torch.minimum"}

var compareOps = map[string]string{"Equal": "torch.eq", "Greater": "torch.gt", "Less": "torch.lt", "And": "torch.logical_and"}

var unaryOps = map[string]string{"Sqrt": "torch.sqrt", "Log": "torch.log", "Round": "torch.round", "Not": "torch.logical_not"}

var reduceOps = map[string]string{"ReduceMax": "torch.amax", "ReduceMin": "torch.amin", "ReduceMean": "torch.mean"}

// node adds the statements of a node.
func (c *coder) node(n onnx.Node) error {
	var ins []value
	for _, name := range n.Inputs {
		if name == "" {
			ins = append(ins, value{})
			continue
		}
		v, err := c.input(name)
		if err != nil {
			return err
		}
		ins = append(ins, v)
	}
	set := func(v value) error {
		c.values[n.Outputs[0]] = v
		return nil
	}
	attr := func(name string, def int64) int64 {
		if a, ok := n.Attrs[name]; ok {
			return a.Int
		}
		return def
	}
	constInts := func(i int) ([]int64, error) {
		if i >= len(n.Inputs) {
			return nil, fmt.Errorf("it has no input %d", i)
		}
		vs, ok := c.ints(n.Inputs[i])
		if !ok {
			return nil, fmt.Errorf("its input %d is not a constant", i)
		}
		return vs, nil
	}
	// shapeOf is a shape input as a list: a list value's, a constant's, or
	// a tensor's.
	shapeOf := func(i int) string {
		if vs, ok := c.ints(n.Inputs[i]); ok {
			return list(vs...)
		}
		if ins[i].list {
			return ins[i].expr
		}
		return c.let("int64", "annotate(List[int], torch.tolist(%s, 1, 4))", ins[i].expr).expr
	}
	x := ins[0]

	switch op := n.Op; {
	case op == "Shape" && len(n.Attrs) == 0:
		return set(value{expr: fmt.Sprintf("torch.size(%s)", x.expr), typ: "int64", list: true})
	case op == "Slice" && x.list:
		starts, err := constInts(1)
		if err != nil {
			return err
		}
		ends, err := constInts(2)
		if err != nil {
			return err
		}
		return set(c.listOf(fmt.Sprintf("torch.slice(%s, %d, %d)", x.expr, starts[0], min(ends[0], math.MaxInt32))))
	case op == "Concat" && slices.ContainsFunc(ins, func(v value) bool { return v.list }):
		var parts []string
		for i, v := range ins {
			if v.list {
				parts = append(parts, v.expr)
			} else if vs, ok := c.ints(n.Inputs[i]); ok {
				parts = append(parts, list(vs...))
			} else {
				parts = append(parts, c.let("int64", "annotate(List[int], torch.tolist(%s, 1, 4))", v.expr).expr)
			}
		}
		return set(c.listOf(strings.Join(parts, " + ")))
	}
	for i := range ins {
		if ins[i].list {
			ins[i] = c.tensor(ins[i])
		}
	}
	x = ins[0]

	switch op := n.Op; {
	case binaryOps[op] != "":
		v := x
		for _, y := range ins[1:] {
			v = c.let(x.typ, "%s(%s, %s)", binaryOps[op], v.expr, y.expr)
		}
		return set(v)
	case op == "Div":
		if x.typ.Integer() {
			return set(c.let(x.typ, "torch.div(%s, %s, rounding_mode=\"trunc\")", x.expr, ins[1].expr))
		}
		return set(c.let(x.typ, "torch.div(%s, %s)", x.expr, ins[1].expr))
	case compareOps[op] != "":
		return set(c.let("bool", "%s(%s, %s)", compareOps[op], x.expr, ins[1].expr))
	case unaryOps[op] != "":
		t := x.typ
		if op == "Not" {
			t = "bool"
		}
		return set(c.let(t, "%s(%s)", unaryOps[op], x.expr))
	case op == "Identity":
		return set(x)
	case op == "Where":
		return set(c.let(ins[1].typ, "torch.where(%s, %s, %s)", x.expr, ins[1].expr, ins[2].expr))
	case op == "MatMul":
		return set(c.let(x.typ, "torch.matmul(%s, %s)", x.expr, ins[1].expr))
	case op == "Cast":
		t, ok := onnx.TypeOf(attr("to", 0))
		if !ok {
			return fmt.Errorf("a cast to ONNX type %d", attr("to", 0))
		}
		return set(c.let(t, "torch.to(%s, %d)", x.expr, scalarTypes[t]))
	case op == "Reshape":
		shape := shapeOf(1)
		if want, ok := c.ints(n.Inputs[1]); ok && slices.Contains(want, 0) {
			// ONNX's 0 keeps the input's dimension.
			parts := make([]string, len(want))
			for i, d := range want {
				parts[i] = strconv.FormatInt(d, 10)
				if d == 0 {
					parts[i] = fmt.Sprintf("torch.size(%s, %d)", x.expr, i)
				}
			}
			shape = "[" + strings.Join(parts, ", ") + "]"
		}
		return set(c.let(x.typ, "torch.reshape(%s, %s)", x.expr, shape))
	case op == "Transpose":
		return set(c.let(x.typ, "torch.permute(%s, %s)", x.expr, list(n.Attrs["perm"].Ints...)))
	case op == "Unsqueeze":
		axes, err := constInts(1)
		if err != nil {
			return err
		}
		slices.Sort(axes)
		v := x
		for _, a := range axes {
			if a < 0 {
				return fmt.Errorf("an axis of its end (%d)", a)
			}
			v = c.let(x.typ, "torch.unsqueeze(%s, %d)", v.expr, a)
		}
		return set(v)
	case op == "Expand":
		// ONNX's broadcasts both ways: to the shape both broadcast to.
		if x.typ == "bool" {
			return set(c.let(x.typ, "torch.logical_or(%s, torch.zeros(%s, dtype=11, layout=None, device=None, pin_memory=None))", x.expr, shapeOf(1)))
		}
		return set(c.let(x.typ, "torch.add(%s, torch.zeros(%s, dtype=%d, layout=None, device=None, pin_memory=None))", x.expr, shapeOf(1), scalarTypes[x.typ]))
	case op == "Shape":
		return set(c.let("int64", "torch.tensor(torch.size(%s), dtype=4, device=None, requires_grad=False)", x.expr))
	case op == "Range":
		kind := "int"
		if !x.typ.Integer() {
			kind = "float"
		}
		return set(c.let(x.typ, "torch.arange(%s(%s), %s(%s), %s(%s), dtype=%d, layout=None, device=None, pin_memory=None)",
			kind, x.expr, kind, ins[1].expr, kind, ins[2].expr, scalarTypes[x.typ]))
	case op == "Concat":
		parts := make([]string, len(ins))
		for i, v := range ins {
			parts[i] = v.expr
		}
		return set(c.let(x.typ, "torch.cat([%s], %d)", strings.Join(parts, ", "), attr("axis", 0)))
	case op == "Gather":
		a := attr("axis", 0)
		if vs, ok := c.ints(n.Inputs[1]); ok && len(c.inits[n.Inputs[1]].Dims) == 0 {
			return set(c.let(x.typ, "torch.select(%s, %d, %d)", x.expr, a, vs[0]))
		}
		picked := c.let(x.typ, "torch.index_select(%s, %d, torch.flatten(%s))", x.expr, a, ins[1].expr)
		return set(c.let(x.typ, "torch.reshape(%s, torch.slice(torch.size(%s), 0, %d) + torch.size(%s) + torch.slice(torch.size(%s), %d))",
			picked.expr, x.expr, a, ins[1].expr, x.expr, a+1))
	case op == "GatherElements":
		return set(c.let(x.typ, "torch.gather(%s, %d, %s)", x.expr, attr("axis", 0), ins[1].expr))
	case op == "AveragePool":
		k, st := n.Attrs["kernel_shape"].Ints, n.Attrs["strides"].Ints
		if len(k) != 2 {
			return fmt.Errorf("a pool of %d dimensions", len(k))
		}
		return set(c.let(x.typ, "torch.avg_pool2d(%s, %s, %s, [0, 0], False, True, None)", x.expr, list(k...), list(st...)))
	case op == "ArgMax":
		return set(c.let("int64", "torch.argmax(%s, %d, %s)", x.expr, attr("axis", 0), boolean(attr("keepdims", 1) == 1)))
	case reduceOps[op] != "":
		keep := boolean(attr("keepdims", 1) == 1)
		axes := "None"
		if a, ok := n.Attrs["axes"]; ok {
			axes = list(a.Ints...)
		}
		if op == "ReduceMean" {
			return set(c.let(x.typ, "torch.mean(%s, %s, %s, dtype=None)", x.expr, axes, keep))
		}
		if axes == "None" {
			axes = "annotate(List[int], [])"
		}
		return set(c.let(x.typ, "%s(%s, %s, %s)", reduceOps[op], x.expr, axes, keep))
	case op == "Slice":
		starts, err := constInts(1)
		if err != nil {
			return err
		}
		ends, err := constInts(2)
		if err != nil {
			return err
		}
		axes := make([]int64, len(starts))
		for i := range axes {
			axes[i] = int64(i)
		}
		if len(n.Inputs) > 3 && n.Inputs[3] != "" {
			if axes, err = constInts(3); err != nil {
				return err
			}
		}
		if len(n.Inputs) > 4 && n.Inputs[4] != "" {
			steps, err := constInts(4)
			if err != nil || slices.ContainsFunc(steps, func(s int64) bool { return s != 1 }) {
				return fmt.Errorf("a slice of steps")
			}
		}
		v := x
		for i, a := range axes {
			v = c.let(x.typ, "torch.slice(%s, %d, %d, %d, 1)", v.expr, a, starts[i], ends[i])
		}
		return set(v)
	case op == "Pad":
		pads, err := constInts(1)
		if err != nil {
			return err
		}
		fill := 0.0
		if len(n.Inputs) > 2 && n.Inputs[2] != "" {
			init, ok := c.inits[n.Inputs[2]]
			if !ok {
				return fmt.Errorf("its value is not a constant")
			}
			fill = init.Values()[0]
		}
		r := len(pads) / 2
		var torchPads []int64
		for i := r - 1; i >= 0; i-- {
			torchPads = append(torchPads, pads[i], pads[r+i])
		}
		return set(c.let(x.typ, "torch.constant_pad_nd(%s, %s, %s)", x.expr, list(torchPads...), scalar(fill)))
	case op == "OneHot":
		depth, err := constInts(1)
		if err != nil {
			return err
		}
		vals, ok := c.inits[n.Inputs[2]]
		if !ok {
			return fmt.Errorf("its values are not a constant")
		}
		if a := attr("axis", -1); a != -1 {
			return fmt.Errorf("a one-hot of axis %d", a)
		}
		onOff := vals.Values()
		t := vals.Type
		idx := c.let("int64", "torch.unsqueeze(torch.to(%s, 4), -1)", x.expr)
		hot := c.let("bool", "torch.eq(%s, torch.arange(%d, dtype=4, layout=None, device=None, pin_memory=None))", idx.expr, depth[0])
		on := c.let(t, "torch.tensor(%s, dtype=%d, device=None, requires_grad=False)", scalar(onOff[1]), scalarTypes[t])
		off := c.let(t, "torch.tensor(%s, dtype=%d, device=None, requires_grad=False)", scalar(onOff[0]), scalarTypes[t])
		return set(c.let(t, "torch.where(%s, %s, %s)", hot.expr, on.expr, off.expr))
	}
	return fmt.Errorf("TorchScript's stubs have no translation of ONNX's %s", n.Op)
}

// listOf is a list value of the expression, as a variable.
func (c *coder) listOf(expr string) value {
	v := c.let("int64", "%s", expr)
	v.list = true
	return v
}

func boolean(b bool) string {
	if b {
		return "True"
	}
	return "False"
}
