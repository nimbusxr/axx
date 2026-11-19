package db2

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

// DRDA's types of a value in a row (FD:OCA's, as DRDA names them). Each has
// a nullable twin, one more than it.
const (
	tInteger     = 0x02
	tSmall       = 0x04
	tFloat16     = 0x08
	tFloat8      = 0x0A
	tFloat4      = 0x0C
	tDecimal     = 0x0E
	tZoned       = 0x10
	tNumericChar = 0x12
	tResultSet   = 0x14
	tBigint      = 0x16
	tBlobLocator = 0x18
	tClobLocator = 0x1A
	tDBClobLoc   = 0x1C
	tRowID       = 0x1E
	tDate        = 0x20
	tTime        = 0x22
	tTimestamp   = 0x24
	tFixedBytes  = 0x26
	tVarBytes    = 0x28
	tLongBytes   = 0x2A
	tCString     = 0x2C
	tCStringSBCS = 0x2E
	tChar        = 0x30
	tVarchar     = 0x32
	tLongVarchar = 0x34
	tGraphic     = 0x36
	tVarGraphic  = 0x38
	tLongGraphic = 0x3A
	tMixed       = 0x3C
	tVarMixed    = 0x3E
	tLongMixed   = 0x40
	tCStringMix  = 0x42
	tPascalBytes = 0x44
	tPascal      = 0x46
	tPascalMixed = 0x48
	tDecFloat    = 0xBA
	tBoolean     = 0xBE
	tBinary      = 0xC0
	tVarBinary   = 0xC2
	tXML         = 0xC6
	tBlob        = 0xC8
	tClobSBCS    = 0xCA
	tDBClob      = 0xCC
	tClobMixed   = 0xCE
	tXMLInternal = 0xDC
	tXMLExternal = 0xDE
)

// field is one value's place in a row: its type and the two bytes that say
// its length, or its precision and scale.
type field struct {
	typ      int
	nullable bool
	length   int
	// ccsid is the character set of a field whose layout names one of its
	// own: 0 for the server's.
	ccsid int
}

// The kinds of FD:OCA triplet in a row's layout (QRYDSC).
const (
	tripletGroup    = 0x76 // the fields of a row
	tripletContinue = 0x7F // more of them
	tripletRow      = 0x71 // how groups make a row
	tripletMeta     = 0x78 // the type a field of its own is of
	tripletScalar   = 0x70 // a field of its own: a type with another character set
)

// layout reads a row's layout: the fields of its SQLDTAGRP, in triplets of a
// length, a kind and an id, each field a type and two bytes of length.
func layout(data []byte) ([]field, error) {
	var fields []field
	// own are the types the layout defines for itself: an id for a type of
	// DRDA's with a character set of its own.
	own := map[int]field{}
	meta := 0
	for len(data) > 0 {
		if len(data) < 3 || int(data[0]) < 3 || int(data[0]) > len(data) {
			return nil, fmt.Errorf("%w: a layout's triplet of %d bytes in %d", errProtocol, data[0], len(data))
		}
		n, kind, id := int(data[0]), int(data[1]), int(data[2])
		body := data[3:n]
		data = data[n:]
		switch kind {
		case tripletGroup, tripletContinue:
			for ; len(body) >= 3; body = body[3:] {
				f := field{typ: int(body[0]), length: int(binary.BigEndian.Uint16(body[1:]))}
				if o, ok := own[f.typ]; ok {
					f.typ, f.ccsid = o.typ, o.ccsid
				}
				f.nullable = f.typ&1 == 1
				f.typ &^= 1
				fields = append(fields, f)
			}
		case tripletMeta:
			if len(body) == 4 {
				meta = int(body[3])
			}
		case tripletScalar:
			if len(body) >= 5 {
				own[id] = field{typ: meta, ccsid: int(binary.BigEndian.Uint32(body[1:5]))}
			}
		case tripletRow:
		default:
			return nil, fmt.Errorf("%w: a layout's triplet of kind %#02x", errProtocol, kind)
		}
	}
	return fields, nil
}

// decimal is a number as Db2 holds it, exact: its digits with a point.
type decimal string

// lobRef is a large value's place in a row: the value itself follows the
// rows, outside them.
type lobRef struct {
	field field
	// length is the value's, as the row says it: in bytes, or in characters
	// of two bytes. A value of no length does not follow the rows.
	length int64
}

// unknownLength is the length of a large value the row does not measure: an
// XML document's.
const unknownLength = -1

// value reads one field of a row. Numbers are in the server's byte order,
// lengths are big-endian.
func (c *conn) value(r *reader, f field, col *column) (any, error) {
	if f.nullable && r.u8() == null {
		return nil, r.err
	}
	sbcs, mbcs, dbcs := c.sbcs, c.mbcs, c.dbcs
	if f.ccsid != 0 {
		sbcs, mbcs, dbcs = f.ccsid, f.ccsid, f.ccsid
	} else if col != nil && col.ccsid != 0 && col.ccsid != 65535 {
		sbcs, mbcs, dbcs = col.ccsid, col.ccsid, col.ccsid
	}
	bits := col != nil && col.bits()
	text := func(b []byte, ccsid int) any {
		if bits {
			// FOR BIT DATA: bytes in a character column.
			return append([]byte{}, b...)
		}
		return decode(b, ccsid)
	}
	var v any
	switch f.typ {
	case tSmall:
		v = int64(r.i16())
	case tInteger:
		v = int64(r.i32())
	case tBigint:
		v = r.i64()
	case tFloat4:
		v = float64(math.Float32frombits(uint32(r.i32())))
	case tFloat8:
		v = math.Float64frombits(uint64(r.i64()))
	case tDecimal:
		precision, scale := f.length>>8, f.length&0xFF
		v = packed(r.take(precision/2+1), scale)
	case tZoned, tNumericChar:
		precision, scale := f.length>>8, f.length&0xFF
		v = zoned(r.take(precision), scale)
	case tDecFloat:
		// A DECFLOAT's bytes are in one order whatever the server's numbers
		// are in.
		d, err := decFloat(r.take(f.length))
		if err != nil {
			return nil, err
		}
		v = d
	case tBoolean:
		b := r.take(f.length)
		v = false
		for _, x := range b {
			v = v.(bool) || x != 0
		}
	case tDate, tTime, tTimestamp:
		at, err := temporal(f.typ, string(r.take(f.length)))
		if err != nil {
			return nil, err
		}
		v = at
	case tChar:
		v = text(r.take(f.length), sbcs)
	case tMixed:
		v = text(r.take(f.length), mbcs)
	case tVarchar, tLongVarchar:
		v = text(r.take(r.length()), sbcs)
	case tVarMixed, tLongMixed:
		v = text(r.take(r.length()), mbcs)
	case tGraphic:
		v = decode(r.take(2*f.length), dbcs)
	case tVarGraphic, tLongGraphic:
		v = decode(r.take(2*r.length()), dbcs)
	case tFixedBytes, tBinary, tRowID:
		v = append([]byte{}, r.take(f.length)...)
	case tVarBytes, tLongBytes, tVarBinary:
		v = append([]byte{}, r.take(r.length())...)
	case tBlob, tClobSBCS, tDBClob, tClobMixed, tXML, tXMLInternal, tXMLExternal:
		// The row holds the value's length, in as many bytes as the field's
		// length says past its high bit, after a byte that says how the
		// value travels when there are nine; the value follows the rows.
		n := f.length &^ 0x8000
		b := r.take(n)
		if n == 9 {
			b = b[1:]
		} else if n > 8 {
			return nil, fmt.Errorf("%w: a large value's length of %d bytes", errProtocol, n)
		}
		var length uint64
		for _, x := range b {
			length = length<<8 | uint64(x)
		}
		ref := lobRef{field: f, length: int64(length)}
		if len(b) > 0 && length == 1<<(8*len(b))-1 {
			ref.length = unknownLength
		}
		v = ref
	default:
		return nil, fmt.Errorf("db2: a value of DRDA's type %#02x, which this client does not read", f.typ)
	}
	return v, r.err
}

// bits says a character column holds bytes (FOR BIT DATA): Db2 says so with
// no character set, or with 65535.
func (c column) bits() bool {
	switch c.sqlType {
	case sqlChar, sqlVarchar, sqlLongVarchar:
		return c.ccsid == 0 || c.ccsid == 65535
	}
	return false
}

// packed reads a packed decimal: a digit in each half byte, the last half
// byte its sign.
func packed(b []byte, scale int) decimal {
	if len(b) == 0 {
		return "0"
	}
	digits := make([]byte, 0, 2*len(b))
	for i, c := range b {
		digits = append(digits, '0'+c>>4)
		if i < len(b)-1 {
			digits = append(digits, '0'+c&0x0F)
		}
	}
	sign := b[len(b)-1] & 0x0F
	return number(digits, scale, sign == 0x0D || sign == 0x0B)
}

// zoned reads a zoned decimal: a digit in each byte, the sign in the last
// byte's upper half.
func zoned(b []byte, scale int) decimal {
	if len(b) == 0 {
		return "0"
	}
	digits := make([]byte, len(b))
	for i, c := range b {
		digits[i] = '0' + c&0x0F
	}
	sign := b[len(b)-1] >> 4
	return number(digits, scale, sign == 0x0D || sign == 0x0B || sign == 0x07)
}

// number writes digits with their point: 00001250 at a scale of 2 is 12.50.
func number(digits []byte, scale int, negative bool) decimal {
	for len(digits) <= scale {
		digits = append([]byte{'0'}, digits...)
	}
	whole := strings.TrimLeft(string(digits[:len(digits)-scale]), "0")
	if whole == "" {
		whole = "0"
	}
	s := whole
	if scale > 0 {
		s += "." + string(digits[len(digits)-scale:])
	}
	if negative && strings.Trim(s, "0.") != "" {
		s = "-" + s
	}
	return decimal(s)
}

// temporal is a date, a time or a timestamp as Db2 sends it: a date and a
// timestamp as the wall clock they are, in UTC as other drivers give one with
// no zone, and a time as its text. Db2 writes a timestamp as
// 2026-11-19-13.28.03.326000, with as many digits of a second as its column
// has, of which a time.Time keeps nine; and a time as 13.28.03 or 13:28:03.
func temporal(typ int, s string) (any, error) {
	s = strings.TrimRight(s, " \x00")
	switch typ {
	case tDate:
		at, err := time.Parse("2006-01-02", s)
		if err != nil {
			return nil, fmt.Errorf("db2: a date that reads %q", s)
		}
		return at, nil
	case tTime:
		return strings.ReplaceAll(s, ".", ":"), nil
	}
	if len(s) < 19 {
		return nil, fmt.Errorf("db2: a timestamp that reads %q", s)
	}
	fraction := ""
	if len(s) > 20 {
		fraction = s[20:min(len(s), 29)]
	}
	at, err := time.Parse("2006-01-02-15.04.05", s[:19])
	if err != nil {
		return nil, fmt.Errorf("db2: a timestamp that reads %q", s)
	}
	nanos := 0
	for i := 0; i < 9; i++ {
		nanos *= 10
		if i < len(fraction) {
			if fraction[i] < '0' || fraction[i] > '9' {
				return nil, fmt.Errorf("db2: a timestamp that reads %q", s)
			}
			nanos += int(fraction[i] - '0')
		}
	}
	return at.Add(time.Duration(nanos)), nil
}
