package avrojson

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/iskorotkov/avro/v2"
)

// DecodeBinary reads one value of schema s from Avro's binary encoding, the
// way Java's GenericDatumReader does without logical-type conversions: every
// value keeps its underlying representation (see the package doc), fixed
// values are [N]byte, unions are {"<branch>": value} wrappers and maps are
// OrderedMaps, so Render prints them in exactly Java's order. Bytes after
// the value are ignored.
func DecodeBinary(s avro.Schema, data []byte) (any, error) {
	r := &binReader{data: data}
	v, err := r.value(s, "$")
	if err != nil {
		return nil, err
	}
	return v, nil
}

type binReader struct {
	data []byte
	pos  int
}

func (r *binReader) fail(path, format string, args ...any) error {
	return &Error{Path: path, Msg: fmt.Sprintf("byte %d: ", r.pos) + fmt.Sprintf(format, args...)}
}

func (r *binReader) varint(path string, maxBytes int) (int64, error) {
	var u uint64
	for i := 0; i < maxBytes; i++ {
		if r.pos >= len(r.data) {
			return 0, r.fail(path, "unexpected end of data")
		}
		c := r.data[r.pos]
		r.pos++
		u |= uint64(c&0x7F) << (7 * i)
		if c&0x80 == 0 {
			return int64(u>>1) ^ -int64(u&1), nil
		}
	}
	return 0, r.fail(path, "invalid variable-length integer")
}

func (r *binReader) take(path string, n int64) ([]byte, error) {
	if n < 0 {
		return nil, r.fail(path, "malformed data: negative length %d", n)
	}
	if n > int64(len(r.data)-r.pos) {
		return nil, r.fail(path, "unexpected end of data (need %d bytes)", n)
	}
	b := r.data[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return b, nil
}

// blockCount reads an array or map block header; a negative count is
// followed by the block's size in bytes.
func (r *binReader) blockCount(path string) (int64, error) {
	n, err := r.varint(path, 10)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		if _, err := r.varint(path, 10); err != nil {
			return 0, err
		}
		n = -n
	}
	return n, nil
}

func (r *binReader) value(s avro.Schema, path string) (any, error) {
	s = deref(s)
	switch s.Type() {
	case avro.Null:
		return nil, nil
	case avro.Boolean:
		b, err := r.take(path, 1)
		if err != nil {
			return nil, err
		}
		return b[0] != 0, nil
	case avro.Int:
		n, err := r.varint(path, 5)
		if err != nil {
			return nil, err
		}
		if n > math.MaxInt32 || n < math.MinInt32 {
			return nil, r.fail(path, "invalid int encoding")
		}
		return int(n), nil
	case avro.Long:
		n, err := r.varint(path, 10)
		if err != nil {
			return nil, err
		}
		if logicalType(s) == avro.TimeMicros && n <= math.MaxInt64/1000 && n >= math.MinInt64/1000 {
			return time.Duration(n) * time.Microsecond, nil
		}
		return n, nil
	case avro.Float:
		b, err := r.take(path, 4)
		if err != nil {
			return nil, err
		}
		return math.Float32frombits(binary.LittleEndian.Uint32(b)), nil
	case avro.Double:
		b, err := r.take(path, 8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
	case avro.Bytes, avro.String:
		n, err := r.varint(path, 10)
		if err != nil {
			return nil, err
		}
		b, err := r.take(path, n)
		if err != nil {
			return nil, err
		}
		if s.Type() == avro.String {
			return string(b), nil
		}
		return append([]byte(nil), b...), nil
	case avro.Fixed:
		b, err := r.take(path, int64(s.(*avro.FixedSchema).Size()))
		if err != nil {
			return nil, err
		}
		return fixedValue(b), nil
	case avro.Enum:
		n, err := r.varint(path, 5)
		if err != nil {
			return nil, err
		}
		syms := s.(*avro.EnumSchema).Symbols()
		if n < 0 || n >= int64(len(syms)) {
			return nil, r.fail(path, "enum index %d out of range for %s", n, typeDesc(s))
		}
		return syms[n], nil
	case avro.Array:
		items := s.(*avro.ArraySchema).Items()
		out := []any{}
		for {
			n, err := r.blockCount(path)
			if err != nil || n == 0 {
				return out, err
			}
			for ; n > 0; n-- {
				v, err := r.value(items, pathIndex(path, len(out)))
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
		}
	case avro.Map:
		values := s.(*avro.MapSchema).Values()
		out := OrderedMap{}
		for {
			n, err := r.blockCount(path)
			if err != nil || n == 0 {
				return out, err
			}
			for ; n > 0; n-- {
				kl, err := r.varint(path, 10)
				if err != nil {
					return nil, err
				}
				kb, err := r.take(path, kl)
				if err != nil {
					return nil, err
				}
				key := string(kb)
				v, err := r.value(values, pathField(path, key))
				if err != nil {
					return nil, err
				}
				out = append(out, MapEntry{Key: key, Value: v})
			}
		}
	case avro.Record:
		fields := s.(*avro.RecordSchema).Fields()
		out := make(map[string]any, len(fields))
		for _, f := range fields {
			v, err := r.value(f.Type(), pathField(path, f.Name()))
			if err != nil {
				return nil, err
			}
			out[f.Name()] = v
		}
		return out, nil
	case avro.Union:
		types := s.(*avro.UnionSchema).Types()
		n, err := r.varint(path, 10)
		if err != nil {
			return nil, err
		}
		if n < 0 || n >= int64(len(types)) {
			return nil, r.fail(path, "union branch %d out of range for %s", n, typeDesc(s))
		}
		v, err := r.value(types[n], path)
		if err != nil {
			return nil, err
		}
		return wrap(types[n], v), nil
	}
	return nil, r.fail(path, "unsupported schema type %s", s.Type())
}
