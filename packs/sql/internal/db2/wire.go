package db2

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// What travels is a chain of data stream structures (DSS): each a header of
// six bytes (its length, 0xD0, its kind, the number of the request it belongs
// to) and DDM objects, each a length, a code point and its data.
const (
	dssMagic = 0xD0
	// The kinds of DSS.
	dssRequest = 1 // a command
	dssReply   = 2 // a reply message
	dssObject  = 3 // objects: a command's, or a reply's
	// Flags of a DSS.
	dssChained = 0x40 // another DSS follows
	dssSameID  = 0x10 // and it belongs to the same request
	// A DSS over this size continues in segments, each of this size at most.
	dssMax = 0x7FFF
	// An object's length field with this bit has its real length after its
	// code point, in the bytes its lower bits count past four.
	extended = 0x8000
	// The null of a nullable value or group.
	null = 0xFF
)

// buf builds DDM objects.
type buf struct{ b []byte }

func (w *buf) u8(v byte)      { w.b = append(w.b, v) }
func (w *buf) u16(v int)      { w.b = binary.BigEndian.AppendUint16(w.b, uint16(v)) }
func (w *buf) u32(v int)      { w.b = binary.BigEndian.AppendUint32(w.b, uint32(v)) }
func (w *buf) bytes(v []byte) { w.b = append(w.b, v...) }
func (w *buf) padded(v []byte, n int, pad byte) {
	w.b = append(w.b, v...)
	for i := len(v); i < n; i++ {
		w.b = append(w.b, pad)
	}
}

// param is one parameter: its length, its code point, its data.
func (w *buf) param(cp int, data []byte) {
	w.u16(len(data) + 4)
	w.u16(cp)
	w.bytes(data)
}

func (w *buf) param8(cp int, v byte) { w.param(cp, []byte{v}) }

func (w *buf) param16(cp, v int) {
	w.param(cp, binary.BigEndian.AppendUint16(nil, uint16(v)))
}

func (w *buf) param32(cp, v int) {
	w.param(cp, binary.BigEndian.AppendUint32(nil, uint32(v)))
}

// object is a DDM object ready to travel: a command with its parameters, or
// an object of a command.
func object(cp int, data []byte) []byte {
	if len(data)+4 <= dssMax {
		out := make([]byte, 0, len(data)+4)
		out = binary.BigEndian.AppendUint16(out, uint16(len(data)+4))
		out = binary.BigEndian.AppendUint16(out, uint16(cp))
		return append(out, data...)
	}
	// A long object says its length in four more bytes.
	out := make([]byte, 0, len(data)+8)
	out = binary.BigEndian.AppendUint16(out, extended|8)
	out = binary.BigEndian.AppendUint16(out, uint16(cp))
	out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
	return append(out, data...)
}

// part is one DSS of a request: a command, or the objects that go with it.
type part struct {
	kind byte
	body []byte
}

func command(cp int, params *buf) part {
	var data []byte
	if params != nil {
		data = params.b
	}
	return part{kind: dssRequest, body: object(cp, data)}
}

func objects(cp int, data []byte) part {
	return part{kind: dssObject, body: object(cp, data)}
}

// chain writes requests as one chain: each request a command and the objects
// after it, numbered from one.
func chain(requests ...[]part) []byte {
	var out []byte
	for i, request := range requests {
		for j, p := range request {
			flags := p.kind
			if j < len(request)-1 {
				flags |= dssChained | dssSameID
			} else if i < len(requests)-1 {
				flags |= dssChained
			}
			out = appendDSS(out, flags, uint16(i+1), p.body)
		}
	}
	return out
}

// appendDSS writes one DSS, in segments when it is long: the first of 32767
// bytes with its header, then segments of a two-byte length and data, the
// length 0xFFFF for each segment that is not the last.
func appendDSS(out []byte, flags byte, id uint16, body []byte) []byte {
	first := len(body)
	if first+6 > dssMax {
		first = dssMax - 6
		out = binary.BigEndian.AppendUint16(out, 0xFFFF)
	} else {
		out = binary.BigEndian.AppendUint16(out, uint16(first+6))
	}
	out = append(out, dssMagic, flags)
	out = binary.BigEndian.AppendUint16(out, id)
	out = append(out, body[:first]...)
	body = body[first:]
	for len(body) > 0 {
		n := len(body)
		if n+2 > dssMax {
			n = dssMax - 2
			out = binary.BigEndian.AppendUint16(out, 0xFFFF)
		} else {
			out = binary.BigEndian.AppendUint16(out, uint16(n+2))
		}
		out = append(out, body[:n]...)
		body = body[n:]
	}
	return out
}

// item is one object of a reply: a reply message with its parameters in
// data, or an object.
type item struct {
	cp   int
	data []byte
	// reply says it is a reply message, whose data is parameters.
	reply bool
}

// errProtocol is a reply this client cannot read.
var errProtocol = errors.New("db2: the server's reply is not one this client reads")

// readDSS reads one DSS, all of its segments, and gives its objects and
// whether another follows in the chain.
func readDSS(r *bufio.Reader) (items []item, chained bool, err error) {
	var head [6]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, false, err
	}
	if head[2] != dssMagic {
		return nil, false, fmt.Errorf("%w: a structure that starts %x", errProtocol, head[:])
	}
	length := int(binary.BigEndian.Uint16(head[:2]))
	kind := head[3] & 0x0F
	chained = head[3]&dssChained != 0
	var body []byte
	if length != 0xFFFF {
		if length < 6 {
			return nil, false, fmt.Errorf("%w: a structure of %d bytes", errProtocol, length)
		}
		body = make([]byte, length-6)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, false, err
		}
	} else {
		body = make([]byte, dssMax-6)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, false, err
		}
		for more := true; more; {
			var seg [2]byte
			if _, err := io.ReadFull(r, seg[:]); err != nil {
				return nil, false, err
			}
			n := int(binary.BigEndian.Uint16(seg[:]))
			more = n == 0xFFFF
			if more {
				n = dssMax
			}
			if n < 2 {
				return nil, false, fmt.Errorf("%w: a segment of %d bytes", errProtocol, n)
			}
			at := len(body)
			body = append(body, make([]byte, n-2)...)
			if _, err := io.ReadFull(r, body[at:]); err != nil {
				return nil, false, err
			}
		}
	}
	for len(body) > 0 {
		cp, data, rest, err := next(body)
		if err != nil {
			return nil, false, err
		}
		items = append(items, item{cp: cp, data: data, reply: kind == dssReply})
		body = rest
	}
	return items, chained, nil
}

// next takes one object off the front of b.
func next(b []byte) (cp int, data, rest []byte, err error) {
	if len(b) < 4 {
		return 0, nil, nil, fmt.Errorf("%w: an object of %d bytes", errProtocol, len(b))
	}
	ll := int(binary.BigEndian.Uint16(b))
	cp = int(binary.BigEndian.Uint16(b[2:]))
	b = b[4:]
	n := ll - 4
	if ll&extended != 0 {
		ext := ll&^extended - 4
		switch {
		case ext == 0:
			// An object with no length runs to the end of its DSS.
			return cp, b, nil, nil
		case ext < 0 || ext > 8 || ext > len(b):
			return 0, nil, nil, fmt.Errorf("%w: an object whose length takes %d bytes", errProtocol, ext)
		}
		var long uint64
		for _, c := range b[:ext] {
			long = long<<8 | uint64(c)
		}
		b = b[ext:]
		if long > uint64(len(b)) {
			return 0, nil, nil, fmt.Errorf("%w: an object of %d bytes in %d", errProtocol, long, len(b))
		}
		n = int(long)
	}
	if n < 0 || n > len(b) {
		return 0, nil, nil, fmt.Errorf("%w: an object of %d bytes in %d", errProtocol, n, len(b))
	}
	return cp, b[:n], b[n:], nil
}

// params are a reply message's parameters by their code points. A parameter
// that repeats keeps its first value.
func params(data []byte) (map[int][]byte, error) {
	out := map[int][]byte{}
	for len(data) > 0 {
		cp, v, rest, err := next(data)
		if err != nil {
			return nil, err
		}
		if _, ok := out[cp]; !ok {
			out[cp] = v
		}
		data = rest
	}
	return out, nil
}

// reader reads the values of an object, in the byte order the server said
// its numbers are in. A read past the end sets err and gives zeros.
type reader struct {
	b      []byte
	little bool
	err    error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return make([]byte, n)
	}
	if n < 0 || n > len(r.b) {
		r.err = fmt.Errorf("%w: %d bytes wanted of %d", errProtocol, n, len(r.b))
		r.b = nil
		return make([]byte, max(n, 0))
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) u8() byte { return r.take(1)[0] }

func (r *reader) i16() int {
	b := r.take(2)
	if r.little {
		return int(int16(binary.LittleEndian.Uint16(b)))
	}
	return int(int16(binary.BigEndian.Uint16(b)))
}

func (r *reader) i32() int32 {
	b := r.take(4)
	if r.little {
		return int32(binary.LittleEndian.Uint32(b))
	}
	return int32(binary.BigEndian.Uint32(b))
}

func (r *reader) i64() int64 {
	b := r.take(8)
	if r.little {
		return int64(binary.LittleEndian.Uint64(b))
	}
	return int64(binary.BigEndian.Uint64(b))
}

// length is a length that is big-endian whatever the server's numbers are.
func (r *reader) length() int { return int(binary.BigEndian.Uint16(r.take(2))) }

// The characters of EBCDIC's code page 500 that are ASCII's too: the first
// attributes two sides exchange are in it, before they agree on Unicode.
var ebcdic500 = func() (table [128]byte) {
	for i := range table {
		table[i] = 0x6F // "?"
	}
	set := func(ascii string, from byte) {
		for i := 0; i < len(ascii); i++ {
			table[ascii[i]] = from + byte(i)
		}
	}
	set(" ", 0x40)
	set("[.<(+!&", 0x4A)
	table['&'] = 0x50
	set("]$*);^-/", 0x5A)
	set(",%_>?", 0x6B)
	set("`:#@'=\"", 0x79)
	set("abcdefghi", 0x81)
	set("jklmnopqr", 0x91)
	set("~stuvwxyz", 0xA1)
	set("|", 0xBB)
	set("{ABCDEFGHI", 0xC0)
	set("}JKLMNOPQR", 0xD0)
	set("\\", 0xE0)
	set("STUVWXYZ", 0xE2)
	set("0123456789", 0xF0)
	return table
}()

// toEBCDIC is s in code page 500, and whether every character of it has a
// place there that this client knows.
func toEBCDIC(s string) ([]byte, bool) {
	out := make([]byte, len(s))
	ok := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 128 || (ebcdic500[c] == 0x6F && c != '?') {
			ok = false
			out[i] = 0x6F
			continue
		}
		out[i] = ebcdic500[c]
	}
	return out, ok
}

// fromEBCDIC reads code page 500's text, with a "?" for what is not ASCII's.
func fromEBCDIC(b []byte) string {
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = '?'
		for ascii, e := range ebcdic500 {
			if e == c && (c != 0x6F || ascii == '?') {
				out[i] = byte(ascii)
				break
			}
		}
	}
	return string(out)
}
