package pickle

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

// Dict is a pickle's dict, its items in their order.
type Dict struct {
	Keys   []any
	Values []any
}

// Get is the value of a string key.
func (d *Dict) Get(key string) (any, bool) {
	for i, k := range d.Keys {
		if k == key {
			return d.Values[i], true
		}
	}
	return nil, false
}

// Global is a global a pickle names: a class or a function.
type Global struct{ Module, Name string }

// Object is what a pickle builds of a global and its arguments (a
// tensor, an OrderedDict...): kept as they are, of no meaning here.
type Object struct {
	Callable any
	Args     any
}

// PersistentID is a persistent id (a tensor's storage's, in torch's).
type PersistentID struct{ ID any }

type mark struct{}

// Read reads a pickle's value: of dicts, lists and tuples ([]any), strings,
// integers, floats, booleans, None (nil), and globals' objects, which it
// keeps without building them.
func Read(b []byte) (any, error) {
	var stack []any
	memo := map[uint32]any{}
	pos := 0
	need := func(n int) ([]byte, error) {
		if pos+n > len(b) {
			return nil, errors.New("a pickle cut short")
		}
		out := b[pos : pos+n]
		pos += n
		return out, nil
	}
	pop := func() (any, error) {
		if len(stack) == 0 {
			return nil, errors.New("a pickle pops an empty stack")
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v, nil
	}
	popMark := func() ([]any, error) {
		for i := len(stack) - 1; i >= 0; i-- {
			if _, ok := stack[i].(mark); ok {
				items := append([]any(nil), stack[i+1:]...)
				stack = stack[:i]
				return items, nil
			}
		}
		return nil, errors.New("a pickle of no mark")
	}
	top := func() (any, error) {
		if len(stack) == 0 {
			return nil, errors.New("a pickle's stack is empty")
		}
		return stack[len(stack)-1], nil
	}
	setItems := func(d any, items []any) error {
		dict, ok := d.(*Dict)
		if !ok {
			if o, ok := d.(*Object); ok {
				// An OrderedDict, its items set.
				if o.Args == nil {
					o.Args = &Dict{}
				}
				dict, ok = o.Args.(*Dict)
				if !ok {
					dict = &Dict{}
					o.Args = dict
				}
			} else {
				return fmt.Errorf("a pickle sets items of a %T", d)
			}
		}
		for i := 0; i+1 < len(items); i += 2 {
			dict.Keys = append(dict.Keys, items[i])
			dict.Values = append(dict.Values, items[i+1])
		}
		return nil
	}
	for pos < len(b) {
		op := b[pos]
		pos++
		switch op {
		case 0x80: // PROTO
			if _, err := need(1); err != nil {
				return nil, err
			}
		case 0x95: // FRAME
			if _, err := need(8); err != nil {
				return nil, err
			}
		case '.': // STOP
			return pop()
		case '}':
			stack = append(stack, &Dict{})
		case ']', ')':
			stack = append(stack, []any{})
		case '(':
			stack = append(stack, mark{})
		case 'X', 0x8c, 0x8d: // BINUNICODE, SHORT_BINUNICODE, BINUNICODE8
			var n int
			switch op {
			case 'X':
				l, err := need(4)
				if err != nil {
					return nil, err
				}
				n = int(binary.LittleEndian.Uint32(l))
			case 0x8c:
				l, err := need(1)
				if err != nil {
					return nil, err
				}
				n = int(l[0])
			default:
				l, err := need(8)
				if err != nil {
					return nil, err
				}
				n = int(binary.LittleEndian.Uint64(l))
			}
			s, err := need(n)
			if err != nil {
				return nil, err
			}
			stack = append(stack, string(s))
		case 'K':
			v, err := need(1)
			if err != nil {
				return nil, err
			}
			stack = append(stack, int64(v[0]))
		case 'M':
			v, err := need(2)
			if err != nil {
				return nil, err
			}
			stack = append(stack, int64(binary.LittleEndian.Uint16(v)))
		case 'J':
			v, err := need(4)
			if err != nil {
				return nil, err
			}
			stack = append(stack, int64(int32(binary.LittleEndian.Uint32(v))))
		case 0x8a: // LONG1
			l, err := need(1)
			if err != nil {
				return nil, err
			}
			v, err := need(int(l[0]))
			if err != nil {
				return nil, err
			}
			var n int64
			for i := len(v) - 1; i >= 0; i-- {
				n = n<<8 | int64(v[i])
			}
			if len(v) > 0 && len(v) < 8 && v[len(v)-1]&0x80 != 0 {
				n -= 1 << (8 * len(v))
			}
			stack = append(stack, n)
		case 'G': // BINFLOAT
			v, err := need(8)
			if err != nil {
				return nil, err
			}
			stack = append(stack, math.Float64frombits(binary.BigEndian.Uint64(v)))
		case 'N':
			stack = append(stack, nil)
		case 0x88:
			stack = append(stack, true)
		case 0x89:
			stack = append(stack, false)
		case 'c': // GLOBAL
			end := strings.IndexByte(string(b[pos:]), '\n')
			if end < 0 {
				return nil, errors.New("a pickle's global is cut short")
			}
			module := string(b[pos : pos+end])
			pos += end + 1
			end = strings.IndexByte(string(b[pos:]), '\n')
			if end < 0 {
				return nil, errors.New("a pickle's global is cut short")
			}
			stack = append(stack, Global{module, string(b[pos : pos+end])})
			pos += end + 1
		case 0x93: // STACK_GLOBAL
			name, err := pop()
			if err != nil {
				return nil, err
			}
			module, err := pop()
			if err != nil {
				return nil, err
			}
			stack = append(stack, Global{fmt.Sprint(module), fmt.Sprint(name)})
		case 'R', 0x81: // REDUCE, NEWOBJ
			args, err := pop()
			if err != nil {
				return nil, err
			}
			f, err := pop()
			if err != nil {
				return nil, err
			}
			if args, ok := args.([]any); ok && len(args) == 0 {
				stack = append(stack, &Object{Callable: f})
			} else {
				stack = append(stack, &Object{Callable: f, Args: args})
			}
		case 'b': // BUILD
			if _, err := pop(); err != nil {
				return nil, err
			}
		case 't':
			items, err := popMark()
			if err != nil {
				return nil, err
			}
			stack = append(stack, items)
		case 0x85, 0x86, 0x87: // TUPLE1..3
			n := int(op - 0x84)
			if len(stack) < n {
				return nil, errors.New("a pickle's tuple is short")
			}
			items := append([]any(nil), stack[len(stack)-n:]...)
			stack = append(stack[:len(stack)-n], items)
		case 's':
			v, err := pop()
			if err != nil {
				return nil, err
			}
			k, err := pop()
			if err != nil {
				return nil, err
			}
			d, err := top()
			if err != nil {
				return nil, err
			}
			if err := setItems(d, []any{k, v}); err != nil {
				return nil, err
			}
		case 'u':
			items, err := popMark()
			if err != nil {
				return nil, err
			}
			d, err := top()
			if err != nil {
				return nil, err
			}
			if err := setItems(d, items); err != nil {
				return nil, err
			}
		case 'a':
			v, err := pop()
			if err != nil {
				return nil, err
			}
			if len(stack) > 0 {
				if l, ok := stack[len(stack)-1].([]any); ok {
					stack[len(stack)-1] = append(l, v)
				}
			}
		case 'e':
			items, err := popMark()
			if err != nil {
				return nil, err
			}
			if len(stack) > 0 {
				if l, ok := stack[len(stack)-1].([]any); ok {
					stack[len(stack)-1] = append(l, items...)
				}
			}
		case 'q', 'r', 0x94: // BINPUT, LONG_BINPUT, MEMOIZE
			var key uint32
			switch op {
			case 'q':
				v, err := need(1)
				if err != nil {
					return nil, err
				}
				key = uint32(v[0])
			case 'r':
				v, err := need(4)
				if err != nil {
					return nil, err
				}
				key = binary.LittleEndian.Uint32(v)
			default:
				key = uint32(len(memo))
			}
			v, err := top()
			if err != nil {
				return nil, err
			}
			memo[key] = v
		case 'h', 'j': // BINGET, LONG_BINGET
			var key uint32
			if op == 'h' {
				v, err := need(1)
				if err != nil {
					return nil, err
				}
				key = uint32(v[0])
			} else {
				v, err := need(4)
				if err != nil {
					return nil, err
				}
				key = binary.LittleEndian.Uint32(v)
			}
			v, ok := memo[key]
			if !ok {
				return nil, fmt.Errorf("a pickle gets %d, which it has not put", key)
			}
			stack = append(stack, v)
		case 'Q': // BINPERSID
			id, err := pop()
			if err != nil {
				return nil, err
			}
			stack = append(stack, PersistentID{id})
		default:
			return nil, fmt.Errorf("a pickle's opcode %#x, which axx does not read", op)
		}
	}
	return nil, errors.New("a pickle of no end")
}
