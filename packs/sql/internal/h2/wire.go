package h2

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf16"
)

// wire reads and writes what H2's server and its clients send each other:
// big-endian numbers, strings as their UTF-16 units, byte arrays with their
// length before them.
type wire struct {
	r *bufio.Reader
	w *bufio.Writer
	// err is the first failure of the connection: once it has one, what is on
	// the wire is no longer known, and the connection is of no more use.
	err error
}

const bufferSize = 64 << 10

// maxLength is the longest string or byte array a server is believed about:
// a length past it is a connection that is not H2's.
const maxLength = 1 << 30

func newWire(rw io.ReadWriter) *wire {
	return &wire{r: bufio.NewReaderSize(rw, bufferSize), w: bufio.NewWriterSize(rw, bufferSize)}
}

func (t *wire) fail(err error) {
	if t.err == nil && err != nil {
		t.err = err
	}
}

func (t *wire) write(p []byte) {
	if t.err == nil {
		_, err := t.w.Write(p)
		t.fail(err)
	}
}

func (t *wire) flush() error {
	if t.err == nil {
		t.fail(t.w.Flush())
	}
	return t.err
}

func (t *wire) writeByte(b byte) { t.write([]byte{b}) }

func (t *wire) writeBool(b bool) {
	if b {
		t.writeByte(1)
	} else {
		t.writeByte(0)
	}
}

func (t *wire) writeInt(n int32) { t.write(binary.BigEndian.AppendUint32(nil, uint32(n))) }

func (t *wire) writeLong(n int64) { t.write(binary.BigEndian.AppendUint64(nil, uint64(n))) }

// writeString writes a string, and a null one with ok false.
func (t *wire) writeString(s string) {
	units := utf16.Encode([]rune(s))
	buf := binary.BigEndian.AppendUint32(make([]byte, 0, 4+2*len(units)), uint32(len(units)))
	for _, u := range units {
		buf = binary.BigEndian.AppendUint16(buf, u)
	}
	t.write(buf)
}

func (t *wire) writeNullString() { t.writeInt(-1) }

// writeBytes writes a byte array, and a null one for nil.
func (t *wire) writeBytes(b []byte) {
	if b == nil {
		t.writeInt(-1)
		return
	}
	t.writeInt(int32(len(b)))
	t.write(b)
}

func (t *wire) read(n int) []byte {
	buf := make([]byte, n)
	if t.err == nil {
		_, err := io.ReadFull(t.r, buf)
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		t.fail(err)
	}
	return buf
}

func (t *wire) readByte() byte { return t.read(1)[0] }

func (t *wire) readBool() bool { return t.readByte() != 0 }

func (t *wire) readShort() int16 { return int16(binary.BigEndian.Uint16(t.read(2))) }

func (t *wire) readInt() int32 { return int32(binary.BigEndian.Uint32(t.read(4))) }

func (t *wire) readLong() int64 { return int64(binary.BigEndian.Uint64(t.read(8))) }

func (t *wire) readFloat() float32 { return math.Float32frombits(binary.BigEndian.Uint32(t.read(4))) }

func (t *wire) readDouble() float64 { return math.Float64frombits(binary.BigEndian.Uint64(t.read(8))) }

// readString reads a string; ok is false for a null one.
func (t *wire) readString() (s string, ok bool) {
	n := t.readInt()
	if n == -1 || t.err != nil {
		return "", false
	}
	if n < 0 || n > maxLength {
		t.fail(fmt.Errorf("a string of %d characters: this is not an H2 server's answer", n))
		return "", false
	}
	raw := t.read(2 * int(n))
	units := make([]uint16, n)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(raw[2*i:])
	}
	return string(utf16.Decode(units)), true
}

// str is readString for a string that null and empty mean the same of.
func (t *wire) str() string {
	s, _ := t.readString()
	return s
}

// readBytes reads a byte array; it is nil for a null one.
func (t *wire) readBytes() []byte {
	n := t.readInt()
	if n == -1 || t.err != nil {
		return nil
	}
	if n < 0 || n > maxLength {
		t.fail(fmt.Errorf("an array of %d bytes: this is not an H2 server's answer", n))
		return nil
	}
	return t.read(int(n))
}

// readChars reads n UTF-16 units as H2 writes a large text's: each in one to
// three bytes.
func (t *wire) readChars(n int64) string {
	if n < 0 || n > maxLength {
		t.fail(fmt.Errorf("a text of %d characters: this is not an H2 server's answer", n))
		return ""
	}
	units := make([]uint16, 0, n)
	for i := int64(0); i < n && t.err == nil; i++ {
		x := uint16(t.readByte())
		switch {
		case x < 0x80:
		case x >= 0xe0:
			x = (x&0xf)<<12 | (uint16(t.readByte())&0x3f)<<6 | uint16(t.readByte())&0x3f
		default:
			x = (x&0x1f)<<6 | uint16(t.readByte())&0x3f
		}
		units = append(units, x)
	}
	return string(utf16.Decode(units))
}
