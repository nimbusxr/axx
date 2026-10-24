// Package hdf5 writes and reads the HDF5 files models keep their weights
// and configs in (Keras's model.weights.h5 and .h5 models): groups,
// attributes, and datasets of numbers and strings, as h5py writes and
// reads them. It writes files of HDF5 1.8's structures (a superblock of
// version 2, object headers of version 2, groups of compact links,
// contiguous datasets), which every h5py reads; and reads those and the
// ones of HDF5's earliest (h5py's default): symbol tables, object headers
// of version 1, strings of variable length.
package hdf5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// An Object is a group (its members) or a dataset (its data), and its
// attributes.
type Object struct {
	Attrs   []Attr
	Members []Member // a group's, in order
	Data    *Data    // a dataset's
}

// A Member is a group's member, by its name.
type Member struct {
	Name   string
	Object *Object
}

// An Attr is an attribute, by its name.
type Attr struct {
	Name string
	Data Data
}

// Data is a dataset's or an attribute's value: its type, its dimensions
// (none: a scalar), and its values' bytes, little-endian; or, of strings,
// the strings.
type Data struct {
	Type    Type
	Dims    []uint64
	Bytes   []byte
	Strings []string // of a String type: each of Type.Size bytes at most
}

// Type is a value's type.
type Type struct {
	Class  Class
	Size   int  // bytes; of a string, of each (0: of variable length, as read)
	Signed bool // of an integer
}

// Class is a type's class.
type Class int

// The classes of types.
const (
	Int Class = iota
	Float
	String
)

// Float32s is a dataset of float32s of the dimensions.
func Float32s(dims []uint64, vs []float32) *Data {
	b := make([]byte, 4*len(vs))
	for i, v := range vs {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(v))
	}
	return &Data{Type: Type{Class: Float, Size: 4}, Dims: dims, Bytes: b}
}

// Strings is a value of strings, of one dimension, each as long as the
// longest (null-padded, as numpy's bytes are).
func Strings(ss ...string) Data {
	size := 1
	for _, s := range ss {
		size = max(size, len(s))
	}
	return Data{Type: Type{Class: String, Size: size}, Dims: []uint64{uint64(len(ss))}, Strings: ss}
}

// Text is a value of a string, a scalar.
func Text(s string) Data {
	return Data{Type: Type{Class: String, Size: max(len(s), 1)}, Strings: []string{s}}
}

// undefined is an address of nothing.
const undefined = math.MaxUint64

// signature begins every HDF5 file.
var signature = []byte("\x89HDF\r\n\x1a\n")

// superblockSize is a superblock's of version 2, of 8-byte addresses and
// lengths.
const superblockSize = 48

// Write is the file of the root group.
func Write(root *Object) ([]byte, error) {
	// The objects, in order: each's header, then the datasets' values.
	var objects []*Object
	var walk func(o *Object)
	walk = func(o *Object) {
		objects = append(objects, o)
		for _, m := range o.Members {
			walk(m.Object)
		}
	}
	walk(root)
	addr := map[*Object]uint64{}
	data := map[*Object]uint64{}
	headers := make([][]byte, len(objects))
	at := uint64(superblockSize)
	for i, o := range objects {
		h, err := header(o, addr, data)
		if err != nil {
			return nil, err
		}
		addr[o] = at
		at += uint64(len(h))
		headers[i] = h
	}
	for _, o := range objects {
		if o.Data != nil {
			data[o] = at
			at += uint64(len(o.Data.bytes()))
		}
	}
	out := make([]byte, 0, at)
	out = append(out, signature...)
	out = append(out, 2, 8, 8, 0)
	out = binary.LittleEndian.AppendUint64(out, 0)
	out = binary.LittleEndian.AppendUint64(out, undefined)
	out = binary.LittleEndian.AppendUint64(out, at)
	out = binary.LittleEndian.AppendUint64(out, addr[root])
	out = binary.LittleEndian.AppendUint32(out, lookup3(out))
	for i, o := range objects {
		h, err := header(o, addr, data)
		if err != nil {
			return nil, err
		}
		if len(h) != len(headers[i]) {
			return nil, errors.New("an object header changed its size")
		}
		out = append(out, h...)
	}
	for _, o := range objects {
		if o.Data != nil {
			out = append(out, o.Data.bytes()...)
		}
	}
	return out, nil
}

// The types of an object header's messages.
const (
	msgNil          = 0x00
	msgDataspace    = 0x01
	msgLinkInfo     = 0x02
	msgDatatype     = 0x03
	msgFillValue    = 0x05
	msgLink         = 0x06
	msgLayout       = 0x08
	msgGroupInfo    = 0x0a
	msgAttribute    = 0x0c
	msgContinuation = 0x10
	msgSymbolTable  = 0x11
	msgAttrInfo     = 0x15
)

// header is an object's header (of version 2), of the addresses of the
// objects and of the datasets' values (of their sizes alone, unknown).
func header(o *Object, addr, data map[*Object]uint64) ([]byte, error) {
	type message struct {
		typ  byte
		body []byte
	}
	var msgs []message
	if o.Data != nil {
		layout := []byte{3, 1}
		layout = binary.LittleEndian.AppendUint64(layout, data[o])
		layout = binary.LittleEndian.AppendUint64(layout, uint64(len(o.Data.bytes())))
		msgs = append(msgs,
			message{msgDataspace, dataspace(o.Data.Dims)},
			message{msgDatatype, o.Data.Type.encode()},
			message{msgFillValue, []byte{3, 0x0a}},
			message{msgLayout, layout},
		)
	} else {
		info := []byte{0, 0}
		info = binary.LittleEndian.AppendUint64(info, undefined)
		info = binary.LittleEndian.AppendUint64(info, undefined)
		msgs = append(msgs, message{msgLinkInfo, info}, message{msgGroupInfo, []byte{0, 0}})
		for _, m := range o.Members {
			var link []byte
			switch n := len(m.Name); {
			case n == 0:
				return nil, errors.New("a member of no name")
			case n < 1<<8:
				link = append([]byte{1, 0}, byte(n))
			case n < 1<<16:
				link = binary.LittleEndian.AppendUint16([]byte{1, 1}, uint16(n))
			default:
				return nil, fmt.Errorf("a member's name of %d bytes", n)
			}
			link = append(link, m.Name...)
			msgs = append(msgs, message{msgLink, binary.LittleEndian.AppendUint64(link, addr[m.Object])})
		}
	}
	for _, a := range o.Attrs {
		name := append([]byte(a.Name), 0)
		dt, ds := a.Data.Type.encode(), dataspace(a.Data.Dims)
		attr := []byte{3, 0}
		attr = binary.LittleEndian.AppendUint16(attr, uint16(len(name)))
		attr = binary.LittleEndian.AppendUint16(attr, uint16(len(dt)))
		attr = binary.LittleEndian.AppendUint16(attr, uint16(len(ds)))
		attr = append(attr, 0)
		attr = append(attr, name...)
		attr = append(attr, dt...)
		attr = append(attr, ds...)
		attr = append(attr, a.Data.bytes()...)
		if len(attr) >= 1<<16 {
			return nil, fmt.Errorf("attribute %s is of %d bytes: an object header's are of 65535 at most", a.Name, len(attr))
		}
		msgs = append(msgs, message{msgAttribute, attr})
	}
	var body []byte
	for _, m := range msgs {
		body = append(body, m.typ)
		body = binary.LittleEndian.AppendUint16(body, uint16(len(m.body)))
		body = append(body, 0)
		body = append(body, m.body...)
	}
	h := append([]byte("OHDR"), 2, 2)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(body)))
	h = append(h, body...)
	return binary.LittleEndian.AppendUint32(h, lookup3(h)), nil
}

// dataspace is a dataspace message (version 2) of the dimensions.
func dataspace(dims []uint64) []byte {
	kind := byte(1)
	if len(dims) == 0 {
		kind = 0
	}
	b := []byte{2, byte(len(dims)), 0, kind}
	for _, d := range dims {
		b = binary.LittleEndian.AppendUint64(b, d)
	}
	return b
}

// encode is a datatype message (version 1) of the type.
func (t Type) encode() []byte {
	b := make([]byte, 0, 20)
	switch t.Class {
	case Int:
		var signed byte
		if t.Signed {
			signed = 0x08
		}
		b = append(b, 0x10, signed, 0, 0)
		b = binary.LittleEndian.AppendUint32(b, uint32(t.Size))
		b = binary.LittleEndian.AppendUint16(b, 0)
		return binary.LittleEndian.AppendUint16(b, uint16(8*t.Size))
	case Float:
		// Its sign's, exponent's and mantissa's places, and its bias.
		layout := map[int][4]int{2: {15, 10, 5, 15}, 4: {31, 23, 8, 127}, 8: {63, 52, 11, 1023}}[t.Size]
		b = append(b, 0x11, 0x20, byte(layout[0]), 0)
		b = binary.LittleEndian.AppendUint32(b, uint32(t.Size))
		b = binary.LittleEndian.AppendUint16(b, 0)
		b = binary.LittleEndian.AppendUint16(b, uint16(8*t.Size))
		b = append(b, byte(layout[1]), byte(layout[2]), 0, byte(layout[1]))
		return binary.LittleEndian.AppendUint32(b, uint32(layout[3]))
	default:
		b = append(b, 0x13, 0x01, 0, 0) // null-padded, ASCII
		return binary.LittleEndian.AppendUint32(b, uint32(t.Size))
	}
}

// bytes are the data's values' bytes.
func (d *Data) bytes() []byte {
	if d.Type.Class != String {
		return d.Bytes
	}
	b := make([]byte, d.Type.Size*len(d.Strings))
	for i, s := range d.Strings {
		copy(b[i*d.Type.Size:(i+1)*d.Type.Size], s)
	}
	return b
}

// lookup3 is Bob Jenkins's hash of b (hashlittle, of 0), HDF5's checksum
// of its metadata.
func lookup3(b []byte) uint32 {
	rot := func(x uint32, k uint) uint32 { return x<<k | x>>(32-k) }
	a := 0xdeadbeef + uint32(len(b))
	bb, c := a, a
	word := func(p []byte) uint32 {
		var w uint32
		for i, x := range p {
			w |= uint32(x) << (8 * i)
		}
		return w
	}
	for len(b) > 12 {
		a += word(b[0:4])
		bb += word(b[4:8])
		c += word(b[8:12])
		a -= c
		a ^= rot(c, 4)
		c += bb
		bb -= a
		bb ^= rot(a, 6)
		a += c
		c -= bb
		c ^= rot(bb, 8)
		bb += a
		a -= c
		a ^= rot(c, 16)
		c += bb
		bb -= a
		bb ^= rot(a, 19)
		a += c
		c -= bb
		c ^= rot(bb, 4)
		bb += a
		b = b[12:]
	}
	if len(b) == 0 {
		return c
	}
	a += word(b[:min(len(b), 4)])
	if len(b) > 4 {
		bb += word(b[4:min(len(b), 8)])
	}
	if len(b) > 8 {
		c += word(b[8:])
	}
	c ^= bb
	c -= rot(bb, 14)
	a ^= c
	a -= rot(c, 11)
	bb ^= a
	bb -= rot(a, 25)
	c ^= bb
	c -= rot(bb, 16)
	a ^= c
	a -= rot(c, 4)
	bb ^= a
	bb -= rot(a, 14)
	c ^= bb
	c -= rot(bb, 24)
	return c
}
