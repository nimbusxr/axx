package hdf5

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

// A File is an HDF5 file being read.
type File struct {
	b              []byte
	offset, length int // sizes of its addresses and lengths
}

// A Node is a group or a dataset of a file being read: its header's
// messages.
type Node struct {
	f    *File
	msgs []message
}

type message struct {
	typ  int
	body []byte
}

// Open reads a file's superblock.
func Open(b []byte) (*File, error) {
	at := bytes.Index(b[:min(len(b), 1<<16)], signature)
	if at < 0 {
		return nil, errors.New("it is not an HDF5 file")
	}
	b = b[at:]
	if len(b) < 64 {
		return nil, errors.New("its superblock is cut short")
	}
	f := &File{b: b}
	switch b[8] {
	case 0, 1:
		f.offset, f.length = int(b[13]), int(b[14])
	case 2, 3:
		f.offset, f.length = int(b[9]), int(b[10])
	default:
		return nil, fmt.Errorf("its superblock is of version %d", b[8])
	}
	if f.offset != 8 || f.length != 8 {
		return nil, fmt.Errorf("its addresses are of %d bytes and its lengths of %d: axx reads 8", f.offset, f.length)
	}
	return f, nil
}

// Root is the file's root group.
func (f *File) Root() (n *Node, err error) {
	var at uint64
	switch f.b[8] {
	case 0:
		at = f.u64(24 + 4*8 + 8)
	case 1:
		at = f.u64(28 + 4*8 + 8)
	default:
		at = f.u64(36)
	}
	return f.node(at)
}

// u64 is the 8 bytes at.
func (f *File) u64(at uint64) uint64 {
	return binary.LittleEndian.Uint64(f.at(at, 8))
}

// at are the n bytes at.
func (f *File) at(at uint64, n int) []byte {
	if at > uint64(len(f.b)) || uint64(len(f.b))-at < uint64(n) {
		panic(fmt.Sprintf("an address past its end, %d", at))
	}
	return f.b[at : at+uint64(n)]
}

// node reads the object header at.
func (f *File) node(at uint64) (n *Node, err error) {
	defer func() {
		if e := recover(); e != nil {
			n, err = nil, fmt.Errorf("it is not an HDF5 file axx reads: %v", e)
		}
	}()
	n = &Node{f: f}
	if string(f.at(at, 4)) == "OHDR" {
		h := f.at(at, 6)
		flags := h[5]
		p := at + 6
		if flags&0x20 != 0 {
			p += 16
		}
		if flags&0x10 != 0 {
			p += 4
		}
		size := 1 << (flags & 3)
		var chunk uint64
		for i, x := range f.at(p, size) {
			chunk |= uint64(x) << (8 * i)
		}
		p += uint64(size)
		n.msgs2(f.at(p, int(chunk)), flags&0x04 != 0)
	} else {
		h := f.at(at, 16)
		if h[0] != 1 {
			return nil, fmt.Errorf("an object header of version %d", h[0])
		}
		n.msgs1(f.at(at+16, int(binary.LittleEndian.Uint32(h[8:]))))
	}
	// Continuations, each a block of more.
	for i := 0; i < len(n.msgs); i++ {
		if n.msgs[i].typ != msgContinuation {
			continue
		}
		body := n.msgs[i].body
		block := f.at(binary.LittleEndian.Uint64(body), int(binary.LittleEndian.Uint64(body[8:])))
		if string(block[:4]) == "OCHK" {
			n.msgs2(block[4:len(block)-4], false)
		} else {
			n.msgs1(block)
		}
	}
	return n, nil
}

// msgs1 reads messages of an object header of version 1.
func (n *Node) msgs1(b []byte) {
	for len(b) >= 8 {
		typ, size := int(binary.LittleEndian.Uint16(b)), int(binary.LittleEndian.Uint16(b[2:]))
		n.msgs = append(n.msgs, message{typ, b[8 : 8+size]})
		b = b[8+size:]
	}
}

// msgs2 reads messages of an object header of version 2.
func (n *Node) msgs2(b []byte, ordered bool) {
	head := 4
	if ordered {
		head = 6
	}
	for len(b) >= head {
		typ, size := int(b[0]), int(binary.LittleEndian.Uint16(b[1:]))
		n.msgs = append(n.msgs, message{typ, b[head : head+size]})
		b = b[head+size:]
	}
}

// Members are a group's members' names.
func (n *Node) Members() (names []string, err error) {
	links, err := n.links()
	if err != nil {
		return nil, err
	}
	for name := range links {
		names = append(names, name)
	}
	slices.Sort(names)
	return names, nil
}

// Member is a group's member of the name.
func (n *Node) Member(name string) (*Node, error) {
	links, err := n.links()
	if err != nil {
		return nil, err
	}
	at, ok := links[name]
	if !ok {
		return nil, fmt.Errorf("it has no %s", name)
	}
	return n.f.node(at)
}

// links are a group's members' headers' addresses, by their names.
func (n *Node) links() (links map[string]uint64, err error) {
	defer func() {
		if e := recover(); e != nil {
			links, err = nil, fmt.Errorf("its group cannot be read: %v", e)
		}
	}()
	links = map[string]uint64{}
	for _, m := range n.msgs {
		switch m.typ {
		case msgLinkInfo:
			if heap := binary.LittleEndian.Uint64(m.body[2+8*int(m.body[1]&1):]); heap != undefined {
				return nil, errors.New("its group's links are stored densely, which axx does not read")
			}
		case msgLink:
			name, at, ok := link(m.body)
			if ok {
				links[name] = at
			}
		case msgSymbolTable:
			if err := n.f.symbols(binary.LittleEndian.Uint64(m.body), binary.LittleEndian.Uint64(m.body[8:]), links); err != nil {
				return nil, err
			}
		}
	}
	return links, nil
}

// link reads a link message: its name, and, of a hard link, its object's
// address.
func link(b []byte) (string, uint64, bool) {
	flags := b[1]
	p := 2
	kind := byte(0)
	if flags&0x08 != 0 {
		kind = b[p]
		p++
	}
	if flags&0x04 != 0 {
		p += 8
	}
	if flags&0x10 != 0 {
		p++
	}
	size := 1 << (flags & 3)
	var n int
	for i := range size {
		n |= int(b[p+i]) << (8 * i)
	}
	p += size
	name := string(b[p : p+n])
	if kind != 0 {
		return name, 0, false
	}
	return name, binary.LittleEndian.Uint64(b[p+n:]), true
}

// symbols reads a symbol table's entries (its B-tree's nodes', their
// names in its local heap).
func (f *File) symbols(tree, heap uint64, links map[string]uint64) error {
	h := f.at(heap, 32)
	if string(h[:4]) != "HEAP" {
		return errors.New("a group's local heap is not one")
	}
	names := binary.LittleEndian.Uint64(h[24:])
	name := func(off uint64) string {
		b := f.b[names+off:]
		return string(b[:bytes.IndexByte(b, 0)])
	}
	var walk func(at uint64) error
	walk = func(at uint64) error {
		switch string(f.at(at, 4)) {
		case "TREE":
			h := f.at(at, 24)
			level, used := h[5], int(binary.LittleEndian.Uint16(h[6:]))
			for i := range used {
				child := f.u64(at + 24 + 8 + uint64(16*i))
				if level == 0 {
					if string(f.at(child, 4)) != "SNOD" {
						return errors.New("a group's symbol node is not one")
					}
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case "SNOD":
			count := int(binary.LittleEndian.Uint16(f.at(at+6, 2)))
			for i := range count {
				e := at + 8 + uint64(40*i)
				links[name(f.u64(e))] = f.u64(e + 8)
			}
		default:
			return errors.New("a group's B-tree is not one")
		}
		return nil
	}
	return walk(tree)
}

// Attr is the node's attribute of the name, if it has it.
func (n *Node) Attr(name string) (*Data, bool, error) {
	attrs, err := n.Attrs()
	if err != nil {
		return nil, false, err
	}
	for _, a := range attrs {
		if a.Name == name {
			return &a.Data, true, nil
		}
	}
	return nil, false, nil
}

// Attrs are the node's attributes.
func (n *Node) Attrs() (attrs []Attr, err error) {
	defer func() {
		if e := recover(); e != nil {
			attrs, err = nil, fmt.Errorf("its attributes cannot be read: %v", e)
		}
	}()
	for _, m := range n.msgs {
		switch m.typ {
		case msgAttrInfo:
			p := 2
			if m.body[1]&1 != 0 {
				p += 2
			}
			if binary.LittleEndian.Uint64(m.body[p:]) != undefined {
				return nil, errors.New("its attributes are stored densely, which axx does not read")
			}
		case msgAttribute:
			a, err := n.f.attribute(m.body)
			if err != nil {
				return nil, err
			}
			attrs = append(attrs, a)
		}
	}
	return attrs, nil
}

// attribute reads an attribute message.
func (f *File) attribute(b []byte) (Attr, error) {
	version := b[0]
	nameSize, dtSize, dsSize := int(binary.LittleEndian.Uint16(b[2:])), int(binary.LittleEndian.Uint16(b[4:])), int(binary.LittleEndian.Uint16(b[6:]))
	pad := func(n int) int { return n }
	p := 8
	switch version {
	case 1:
		pad = func(n int) int { return (n + 7) &^ 7 }
	case 2:
	case 3:
		p = 9
	default:
		return Attr{}, fmt.Errorf("an attribute of version %d", version)
	}
	name := strings.TrimRight(string(b[p:p+nameSize]), "\x00")
	p += pad(nameSize)
	t, err := datatype(b[p : p+dtSize])
	if err != nil {
		return Attr{}, fmt.Errorf("attribute %s: %w", name, err)
	}
	p += pad(dtSize)
	dims, err := dataspaceOf(b[p : p+dsSize])
	if err != nil {
		return Attr{}, fmt.Errorf("attribute %s: %w", name, err)
	}
	p += pad(dsSize)
	d, err := f.values(t, dims, b[p:])
	if err != nil {
		return Attr{}, fmt.Errorf("attribute %s: %w", name, err)
	}
	return Attr{Name: name, Data: d}, nil
}

// datatypeOf is a type a datatype message says: of strings of variable
// length, a String of Size 0.
type datatypeOf struct {
	Type
	vlen bool
}

// datatype reads a datatype message.
func datatype(b []byte) (datatypeOf, error) {
	class, bits, size := b[0]&0x0f, b[1], int(binary.LittleEndian.Uint32(b[4:]))
	switch class {
	case 0:
		if bits&1 != 0 {
			return datatypeOf{}, errors.New("its integers are big-endian")
		}
		return datatypeOf{Type: Type{Class: Int, Size: size, Signed: bits&0x08 != 0}}, nil
	case 1:
		if bits&1 != 0 || (size != 2 && size != 4 && size != 8) {
			return datatypeOf{}, errors.New("its floats are not IEEE's, little-endian")
		}
		return datatypeOf{Type: Type{Class: Float, Size: size}}, nil
	case 3:
		return datatypeOf{Type: Type{Class: String, Size: size}}, nil
	case 9:
		if bits&0x0f != 1 {
			return datatypeOf{}, errors.New("its values are sequences of variable length")
		}
		return datatypeOf{Type: Type{Class: String}, vlen: true}, nil
	}
	return datatypeOf{}, fmt.Errorf("its values are of HDF5's class %d", class)
}

// dataspaceOf reads a dataspace message: its dimensions.
func dataspaceOf(b []byte) ([]uint64, error) {
	rank := int(b[1])
	p := 4
	switch b[0] {
	case 1:
		p = 8
	case 2:
		if b[3] == 2 {
			return []uint64{0}, nil
		}
	default:
		return nil, fmt.Errorf("a dataspace of version %d", b[0])
	}
	dims := make([]uint64, rank)
	for i := range dims {
		dims[i] = binary.LittleEndian.Uint64(b[p+8*i:])
	}
	return dims, nil
}

// values reads values of the type and dimensions from b.
func (f *File) values(t datatypeOf, dims []uint64, b []byte) (Data, error) {
	count := 1
	for _, d := range dims {
		count *= int(d)
	}
	d := Data{Type: t.Type, Dims: dims}
	switch {
	case t.vlen:
		for i := range count {
			e := b[16*i:]
			s, err := f.global(binary.LittleEndian.Uint64(e[4:]), binary.LittleEndian.Uint32(e[12:]))
			if err != nil {
				return Data{}, err
			}
			d.Strings = append(d.Strings, string(s[:binary.LittleEndian.Uint32(e)]))
		}
	case t.Class == String:
		for i := range count {
			d.Strings = append(d.Strings, strings.TrimRight(string(b[i*t.Size:(i+1)*t.Size]), "\x00"))
		}
	default:
		d.Bytes = b[:count*t.Size]
	}
	return d, nil
}

// global is the object of the index in the global heap collection at.
func (f *File) global(at uint64, index uint32) ([]byte, error) {
	h := f.at(at, 16)
	if string(h[:4]) != "GCOL" {
		return nil, errors.New("a global heap is not one")
	}
	end := at + binary.LittleEndian.Uint64(h[8:])
	for p := at + 16; p+16 <= end; {
		o := f.at(p, 16)
		i, size := binary.LittleEndian.Uint16(o), binary.LittleEndian.Uint64(o[8:])
		if i == 0 {
			break
		}
		if uint32(i) == index {
			return f.at(p+16, int(size)), nil
		}
		p += 16 + (size+7)&^7
	}
	return nil, fmt.Errorf("a global heap has no object %d", index)
}

// Data is a dataset's values: of a contiguous or compact dataset.
func (n *Node) Data() (d *Data, err error) {
	defer func() {
		if e := recover(); e != nil {
			d, err = nil, fmt.Errorf("its dataset cannot be read: %v", e)
		}
	}()
	var t *datatypeOf
	var dims []uint64
	var layout []byte
	for _, m := range n.msgs {
		switch m.typ {
		case msgDatatype:
			dt, err := datatype(m.body)
			if err != nil {
				return nil, err
			}
			t = &dt
		case msgDataspace:
			if dims, err = dataspaceOf(m.body); err != nil {
				return nil, err
			}
		case msgLayout:
			layout = m.body
		}
	}
	if t == nil || layout == nil {
		return nil, errors.New("it is not a dataset")
	}
	if layout[0] != 3 {
		return nil, fmt.Errorf("its layout is of version %d", layout[0])
	}
	var b []byte
	switch layout[1] {
	case 0:
		b = layout[4 : 4+int(binary.LittleEndian.Uint16(layout[2:]))]
	case 1:
		at, size := binary.LittleEndian.Uint64(layout[2:]), binary.LittleEndian.Uint64(layout[10:])
		if at == undefined {
			b = make([]byte, size)
		} else {
			b = n.f.at(at, int(size))
		}
	default:
		return nil, errors.New("its values are chunked, which axx does not read")
	}
	v, err := n.f.values(*t, dims, b)
	return &v, err
}

// Float32s are float values as float32s.
func (d *Data) Float32s() ([]float32, error) {
	if d.Type.Class != Float {
		return nil, errors.New("its values are not floats")
	}
	n := len(d.Bytes) / d.Type.Size
	out := make([]float32, n)
	for i := range out {
		switch d.Type.Size {
		case 4:
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(d.Bytes[4*i:]))
		case 8:
			out[i] = float32(math.Float64frombits(binary.LittleEndian.Uint64(d.Bytes[8*i:])))
		default:
			return nil, errors.New("its values are halves")
		}
	}
	return out, nil
}
