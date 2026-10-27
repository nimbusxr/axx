// Package pickle writes and reads Python's pickles of the values PyTorch's
// files have (torch.save's and torch.jit.save's data.pkl): dicts, tuples,
// strings, numbers and tensors, a tensor its storage's persistent id.
package pickle

import (
	"bytes"
	"encoding/binary"
	"math"
)

// Writer writes a pickle of protocol 2.
type Writer struct{ bytes.Buffer }

// Op writes opcodes (and their bytes).
func (p *Writer) Op(b ...byte) { p.Write(b) }

// Global writes a global, a class or a function: its module and name.
func (p *Writer) Global(module, name string) {
	p.Op('c')
	p.WriteString(module + "\n" + name + "\n")
}

// Str writes a string.
func (p *Writer) Str(s string) {
	p.Op('X')
	_ = binary.Write(p, binary.LittleEndian, uint32(len(s)))
	p.WriteString(s)
}

// Int writes an integer.
func (p *Writer) Int(n int64) {
	switch {
	case n >= 0 && n < 256:
		p.Op('K', byte(n))
	case n >= 0 && n < 65536:
		p.Op('M')
		_ = binary.Write(p, binary.LittleEndian, uint16(n))
	case n >= math.MinInt32 && n <= math.MaxInt32:
		p.Op('J')
		_ = binary.Write(p, binary.LittleEndian, int32(n))
	default:
		p.Op(0x8a, 8) // LONG1 of 8 bytes
		_ = binary.Write(p, binary.LittleEndian, n)
	}
}

// Ints writes a tuple of integers.
func (p *Writer) Ints(ns []int64) {
	p.Op('(')
	for _, n := range ns {
		p.Int(n)
	}
	p.Op('t')
}

// Tensor writes a tensor as torch's _rebuild_tensor_v2 of its storage
// (persistent id: ("storage", its class, its key, "cpu", its elements)),
// contiguous, of the dimensions.
func (p *Writer) Tensor(storage, key string, numel int64, dims []int64) {
	strides := make([]int64, len(dims))
	stride := int64(1)
	for d := len(dims) - 1; d >= 0; d-- {
		strides[d] = stride
		stride *= dims[d]
	}
	p.Global("torch._utils", "_rebuild_tensor_v2")
	p.Op('(', '(')
	p.Str("storage")
	p.Global("torch", storage)
	p.Str(key)
	p.Str("cpu")
	p.Int(numel)
	p.Op('t', 'Q')
	p.Int(0)
	p.Ints(dims)
	p.Ints(strides)
	p.Op(0x89)
	p.Global("collections", "OrderedDict")
	p.Op(')', 'R', 't', 'R')
}
