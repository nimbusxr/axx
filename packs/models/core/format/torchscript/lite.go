package torchscript

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/nimbusxr/axx/packs/models/core/internal/pickle"
)

// A PyTorch Mobile module (.ptl, as torch.jit's _save_for_lite_interpreter
// writes it) is a TorchScript archive and its forward's bytecode
// (bytecode.pkl), which PyTorch Mobile's lite interpreter runs, compiling no
// code: the stub's forward, compiled from its code here. The bytecode loads
// each call's arguments onto a stack (from registers, LOAD, and constants,
// LOADC), calls an operator of its table (OP), and stores the result in a
// register (STORE); its constants are the code's, its tensors the archive's
// constants.

// bytecodeVersion is the bytecode's version: torch 2's.
const bytecodeVersion = 8

// kind is what an expression of the code is: the overload of an operator
// is chosen by its arguments' kinds.
type kind int

const (
	kTensor kind = iota
	kInt
	kFloat
	kBool
	kNone
	kStr
	kInts
	kTensors
)

// An expression of the code.
type (
	ident    string
	constRef int
	intLit   int64
	floatLit float64
	boolLit  bool
	noneLit  struct{}
	strLit   string
	listLit  []expr
	tupleLit []expr
	plus     struct{ a, b expr }
	call     struct {
		fn   string
		args []expr
		kw   []expr // keyword arguments' values, in their order: the schema's
	}
)

type expr any

// parser reads the expressions of the code the stub's forward is.
type parser struct {
	s   string
	pos int
}

func (p *parser) space() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\n') {
		p.pos++
	}
}

func (p *parser) peek() byte {
	p.space()
	if p.pos >= len(p.s) {
		return 0
	}
	return p.s[p.pos]
}

func (p *parser) eat(c byte) error {
	if p.peek() != c {
		return fmt.Errorf("expected %q at %q", c, p.rest())
	}
	p.pos++
	return nil
}

func (p *parser) rest() string {
	r := p.s[min(p.pos, len(p.s)):]
	if len(r) > 40 {
		r = r[:40]
	}
	return r
}

func (p *parser) word() string {
	p.space()
	start := p.pos
	for p.pos < len(p.s) {
		c := rune(p.s[p.pos])
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' && c != '.' {
			break
		}
		p.pos++
	}
	return p.s[start:p.pos]
}

// expr is an expression: terms joined by +.
func (p *parser) expr() (expr, error) {
	e, err := p.term()
	if err != nil {
		return nil, err
	}
	for p.peek() == '+' {
		p.pos++
		b, err := p.term()
		if err != nil {
			return nil, err
		}
		e = plus{e, b}
	}
	return e, nil
}

func (p *parser) term() (expr, error) {
	switch c := p.peek(); {
	case c == '[':
		p.pos++
		items, err := p.items(']')
		return listLit(items), err
	case c == '(':
		p.pos++
		items, err := p.items(')')
		return tupleLit(items), err
	case c == '"':
		end := strings.IndexByte(p.s[p.pos+1:], '"')
		if end < 0 {
			return nil, fmt.Errorf("an unterminated string at %q", p.rest())
		}
		s := p.s[p.pos+1 : p.pos+1+end]
		p.pos += end + 2
		return strLit(s), nil
	case c == '-' || c >= '0' && c <= '9':
		return p.number()
	}
	w := p.word()
	switch {
	case w == "":
		return nil, fmt.Errorf("an expression expected at %q", p.rest())
	case w == "True" || w == "False":
		return boolLit(w == "True"), nil
	case w == "None":
		return noneLit{}, nil
	case w == "inf":
		return floatLit(math.Inf(1)), nil
	case strings.HasPrefix(w, "CONSTANTS.c"):
		n, err := strconv.Atoi(strings.TrimPrefix(w, "CONSTANTS.c"))
		return constRef(n), err
	case p.peek() == '(':
		p.pos++
		if w == "annotate" {
			// annotate(List[int], e): its type, then e.
			depth := 0
			for p.pos < len(p.s) && (p.s[p.pos] != ',' || depth > 0) {
				switch p.s[p.pos] {
				case '[':
					depth++
				case ']':
					depth--
				}
				p.pos++
			}
			p.pos++
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			return e, p.eat(')')
		}
		c := call{fn: w}
		for p.peek() != ')' {
			start := p.pos
			name := p.word()
			if name != "" && p.peek() == '=' {
				p.pos++
				v, err := p.expr()
				if err != nil {
					return nil, err
				}
				c.kw = append(c.kw, v)
			} else {
				p.pos = start
				v, err := p.expr()
				if err != nil {
					return nil, err
				}
				c.args = append(c.args, v)
			}
			if p.peek() == ',' {
				p.pos++
			}
		}
		p.pos++
		return c, nil
	}
	return ident(w), nil
}

func (p *parser) items(end byte) ([]expr, error) {
	var items []expr
	for p.peek() != end {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		items = append(items, e)
		if p.peek() == ',' {
			p.pos++
		}
	}
	p.pos++
	return items, nil
}

func (p *parser) number() (expr, error) {
	p.space()
	start := p.pos
	if p.s[p.pos] == '-' {
		p.pos++
		if strings.HasPrefix(p.s[p.pos:], "inf") {
			p.pos += 3
			return floatLit(math.Inf(-1)), nil
		}
	}
	for p.pos < len(p.s) && strings.IndexByte("0123456789.eE+-", p.s[p.pos]) >= 0 {
		if (p.s[p.pos] == '+' || p.s[p.pos] == '-') && p.s[p.pos-1] != 'e' && p.s[p.pos-1] != 'E' {
			break
		}
		p.pos++
	}
	text := p.s[start:p.pos]
	if strings.ContainsAny(text, ".eE") {
		v, err := strconv.ParseFloat(text, 64)
		return floatLit(v), err
	}
	v, err := strconv.ParseInt(text, 10, 64)
	return intLit(v), err
}

// operator is an entry of the bytecode's table of operators.
type operator struct {
	name, overload string
	args           int
}

// liteCompiler compiles the forward's statements to bytecode.
type liteCompiler struct {
	instructions [][3]any
	operators    []operator
	opIndex      map[operator]int
	constants    []any
	constIndex   map[string]int
	types        []string
	registers    map[string]int
	kinds        map[string]kind
	// tensors are the archive's constants, which the bytecode's share.
	tensors []constant
}

// tensorConst is a constant of the bytecode that is the code's constant n.
type tensorConst int

// intList is a constant list of ints (torch.jit._pickle.build_intlist).
type intList []int64

func (c *liteCompiler) emit(op string, x, n int) {
	c.instructions = append(c.instructions, [3]any{op, x, n})
}

func (c *liteCompiler) constant(v any) {
	key := fmt.Sprintf("%T:%v", v, v)
	if f, ok := v.(float64); ok {
		key = fmt.Sprintf("float64:%x", math.Float64bits(f))
	}
	i, ok := c.constIndex[key]
	if !ok {
		i = len(c.constants)
		c.constants = append(c.constants, v)
		c.constIndex[key] = i
	}
	c.emit("LOADC", i, 0)
}

func (c *liteCompiler) op(name, overload string, args int) {
	o := operator{name, overload, args}
	i, ok := c.opIndex[o]
	if !ok {
		i = len(c.operators)
		c.operators = append(c.operators, o)
		c.opIndex[o] = i
	}
	c.emit("OP", i, 0)
}

// value pushes an expression's value, and is its kind.
func (c *liteCompiler) value(e expr) (kind, error) {
	switch e := e.(type) {
	case ident:
		r, ok := c.registers[string(e)]
		if !ok {
			return 0, fmt.Errorf("the code has no variable %s", e)
		}
		c.emit("LOAD", r, 0)
		return c.kinds[string(e)], nil
	case constRef:
		c.constant(tensorConst(e))
		return kTensor, nil
	case intLit:
		c.constant(int64(e))
		return kInt, nil
	case floatLit:
		c.constant(float64(e))
		return kFloat, nil
	case boolLit:
		c.constant(bool(e))
		return kBool, nil
	case noneLit:
		c.constant(nil)
		return kNone, nil
	case strLit:
		c.constant(string(e))
		return kStr, nil
	case listLit:
		ints := make(intList, 0, len(e))
		for _, item := range e {
			if n, ok := item.(intLit); ok {
				ints = append(ints, int64(n))
			}
		}
		if len(ints) == len(e) {
			c.constant(ints)
			return kInts, nil
		}
		items := kInts
		for _, item := range e {
			k, err := c.value(item)
			if err != nil {
				return 0, err
			}
			if k == kTensor {
				items = kTensors
			}
		}
		typ := "List[int]"
		if items == kTensors {
			typ = "List[Tensor]"
		}
		c.types = append(c.types, typ)
		c.emit("LIST_CONSTRUCT", len(c.types)-1, len(e))
		return items, nil
	case plus:
		for _, side := range []expr{e.a, e.b} {
			if k, err := c.value(side); err != nil {
				return 0, err
			} else if k != kInts {
				return 0, fmt.Errorf("a + of other than lists")
			}
		}
		c.op("aten::add", "t", 2)
		return kInts, nil
	case call:
		var kinds []kind
		for _, a := range e.args {
			k, err := c.value(a)
			if err != nil {
				return 0, err
			}
			kinds = append(kinds, k)
		}
		for _, a := range e.kw {
			if _, err := c.value(a); err != nil {
				return 0, err
			}
		}
		name, overload, result, err := overloadOf(e.fn, kinds, len(e.kw))
		if err != nil {
			return 0, err
		}
		c.op(name, overload, len(e.args)+len(e.kw))
		return result, nil
	}
	return 0, fmt.Errorf("an expression of %T", e)
}

// overloadOf is the operator a call of the code is, by its arguments' kinds
// (and how many keyword arguments it has), and its result's kind.
func overloadOf(fn string, args []kind, kw int) (name, overload string, result kind, err error) {
	first := kNone
	if len(args) > 0 {
		first = args[0]
	}
	switch fn {
	case "int":
		return "aten::Int", "Tensor", kInt, nil
	case "float":
		return "aten::Float", "Tensor", kFloat, nil
	}
	op, ok := strings.CutPrefix(fn, "torch.")
	if !ok {
		return "", "", 0, fmt.Errorf("a call of %s", fn)
	}
	name = "aten::" + op
	switch op {
	case "add", "sub", "mul":
		if first == kInts {
			return name, "t", kInts, nil
		}
		return name, "Tensor", kTensor, nil
	case "div":
		if kw > 0 {
			return name, "Tensor_mode", kTensor, nil
		}
		return name, "Tensor", kTensor, nil
	case "eq", "gt", "lt":
		return name, "Tensor", kTensor, nil
	case "mean":
		return name, "dim", kTensor, nil
	case "flatten":
		return name, "using_ints", kTensor, nil
	case "select":
		return name, "int", kTensor, nil
	case "size":
		if len(args) > 1 {
			return name, "int", kInt, nil
		}
		return name, "", kInts, nil
	case "slice":
		if first == kInts {
			return name, "t", kInts, nil
		}
		return name, "Tensor", kTensor, nil
	case "tensor":
		switch first {
		case kInt:
			return name, "int", kTensor, nil
		case kFloat:
			return name, "float", kTensor, nil
		case kBool:
			return name, "bool", kTensor, nil
		}
		return name, "", kTensor, nil
	case "to":
		return name, "dtype", kTensor, nil
	case "tolist":
		return "prim::tolist", "", kInts, nil
	case "where":
		return name, "self", kTensor, nil
	case "arange":
		if len(args) == 3 {
			return name, "start_step", kTensor, nil
		}
		return name, "", kTensor, nil
	case "amax", "amin", "argmax", "avg_pool2d", "cat", "constant_pad_nd", "gather", "index_select",
		"log", "logical_and", "logical_not", "logical_or", "matmul", "maximum", "minimum", "permute",
		"reshape", "round", "sqrt", "unsqueeze", "zeros":
		return name, "", kTensor, nil
	}
	return "", "", 0, fmt.Errorf("a call of %s, which the stub's bytecode does not have", fn)
}

// compileLite compiles the forward of the module's code (its body's
// statements: "_N = e", then "return e") to its bytecode, of the forward's
// parameters (inputs) and class (the module's).
func compileLite(body string, tensors []constant, inputs []string, class string, outputs int) ([]byte, error) {
	c := &liteCompiler{opIndex: map[operator]int{}, constIndex: map[string]int{}, registers: map[string]int{}, kinds: map[string]kind{}, tensors: tensors}
	// self, then the inputs, in registers from 1; self is dropped.
	c.emit("STOREN", 1, 1+len(inputs))
	c.emit("DROPR", 1, 0)
	for i, in := range inputs {
		c.registers[in] = 2 + i
		c.kinds[in] = kTensor
	}
	next := 2 + len(inputs)
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		line = strings.TrimSpace(line)
		if ret, ok := strings.CutPrefix(line, "return "); ok {
			p := &parser{s: ret}
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			if t, ok := e.(tupleLit); ok {
				for _, item := range t {
					if _, err := c.value(item); err != nil {
						return nil, err
					}
				}
				c.emit("TUPLE_CONSTRUCT", len(t), 0)
			} else if _, err := c.value(e); err != nil {
				return nil, err
			}
			c.emit("RET", 0, 0)
			break
		}
		name, rhs, ok := strings.Cut(line, " = ")
		if !ok {
			return nil, fmt.Errorf("a statement %q", line)
		}
		p := &parser{s: rhs}
		e, err := p.expr()
		if err != nil {
			return nil, fmt.Errorf("%q: %w", line, err)
		}
		k, err := c.value(e)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", line, err)
		}
		c.registers[name], c.kinds[name] = next, k
		c.emit("STORE", next, 0)
		next++
	}
	return c.pickle(class, inputs, outputs, next-1), nil
}

// pickle writes the bytecode's pickle: its version, then the forward's
// name, code (instructions, operators, constants, types, register size) and
// schema.
func (c *liteCompiler) pickle(class string, inputs []string, outputs, registers int) []byte {
	var p pickle.Writer
	p.Op(0x80, 2, '(')
	p.Int(bytecodeVersion)
	p.Op('(')
	p.Str(class + ".forward")
	p.Op('(')
	pair := func(name string, body func()) {
		p.Op('(')
		p.Str(name)
		body()
		p.Op('t')
	}
	pair("instructions", func() {
		p.Op('(')
		for _, in := range c.instructions {
			p.Op('(')
			p.Str(in[0].(string))
			p.Int(int64(in[1].(int)))
			p.Int(int64(in[2].(int)))
			p.Op('t')
		}
		p.Op('t')
	})
	pair("operators", func() {
		p.Op('(')
		for _, o := range c.operators {
			p.Op('(')
			p.Str(o.name)
			p.Str(o.overload)
			p.Int(int64(o.args))
			p.Op('t')
		}
		p.Op('t')
	})
	pair("constants", func() {
		p.Op('(')
		for _, v := range c.constants {
			switch v := v.(type) {
			case tensorConst:
				t := c.tensors[v]
				p.Tensor(storages[t.typ], fmt.Sprint(int(v)), int64(len(t.raw)/t.typ.Size()), t.dims)
			case int64:
				p.Int(v)
			case float64:
				p.Op('G')
				_ = binary.Write(&p, binary.BigEndian, v)
			case bool:
				if v {
					p.Op(0x88)
				} else {
					p.Op(0x89)
				}
			case nil:
				p.Op('N')
			case string:
				p.Str(v)
			case intList:
				p.Global("torch.jit._pickle", "build_intlist")
				p.Op('(', ']', '(')
				for _, n := range v {
					p.Int(n)
				}
				p.Op('e', 't', 'R')
			}
		}
		p.Op('t')
	})
	pair("types", func() {
		p.Op('(')
		for _, t := range c.types {
			p.Str(t)
		}
		p.Op('t')
	})
	pair("register_size", func() { p.Int(int64(registers)) })
	p.Op('t')
	// The schema: self and the inputs, and the return.
	argument := func(name, typ string) {
		p.Op('(', '(')
		p.Str("name")
		p.Str(name)
		p.Op('t', '(')
		p.Str("type")
		p.Str(typ)
		p.Op('t', '(')
		p.Str("default_value")
		p.Op('N', 't', 't')
	}
	p.Op('(')
	pair("arguments", func() {
		p.Op('(')
		argument("self", class)
		for _, in := range inputs {
			argument(in, "Tensor")
		}
		p.Op('t')
	})
	ret := "Tensor"
	if outputs > 1 {
		ret = "Tuple[" + strings.TrimSuffix(strings.Repeat("Tensor, ", outputs), ", ") + "]"
	}
	pair("returns", func() {
		p.Op('(')
		argument("", ret)
		p.Op('t')
	})
	p.Op('t', 't', 't', '.')
	return p.Bytes()
}
