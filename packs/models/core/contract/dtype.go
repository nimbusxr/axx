package contract

import (
	"encoding/binary"
	"math"
	"sort"
)

// DType is the type of a tensor's elements: "float32", "uint8"...
type DType string

var dtypes = map[DType]struct {
	size    int
	integer bool
	signed  bool
}{
	"float32": {4, false, true}, "float64": {8, false, true}, "float16": {2, false, true},
	"int8": {1, true, true}, "int16": {2, true, true}, "int32": {4, true, true}, "int64": {8, true, true},
	"uint8": {1, true, false}, "uint16": {2, true, false}, "uint32": {4, true, false}, "uint64": {8, true, false},
	"bool": {1, true, false},
}

func typeNames() []string {
	out := make([]string, 0, len(dtypes))
	for t := range dtypes {
		out = append(out, string(t))
	}
	sort.Strings(out)
	return out
}

// Known reports whether the type is one stubs have.
func (t DType) Known() bool { _, ok := dtypes[t]; return ok }

// Size is the size of an element, in bytes.
func (t DType) Size() int { return dtypes[t].size }

// Integer reports whether the elements are integers (or booleans).
func (t DType) Integer() bool { return dtypes[t].integer }

// Range is the smallest and the largest value of an integer type.
func (t DType) Range() (lo, hi float64) {
	if t == "bool" {
		return 0, 1
	}
	bits := float64(8 * t.Size())
	if dtypes[t].signed {
		return -math.Pow(2, bits-1), math.Pow(2, bits-1) - 1
	}
	return 0, math.Pow(2, bits) - 1
}

// Encode writes values as elements of the type, little-endian, as ONNX's raw
// data and most formats keep them: an integer type's values are rounded
// half away from zero and clamped to its range.
func (t DType) Encode(values []float64) []byte {
	size := t.Size()
	out := make([]byte, len(values)*size)
	for i, v := range values {
		b := out[i*size : (i+1)*size]
		if t.Integer() {
			lo, hi := t.Range()
			v = math.Max(lo, math.Min(hi, math.Round(v)))
		}
		switch t {
		case "float32":
			binary.LittleEndian.PutUint32(b, math.Float32bits(float32(v)))
		case "float64":
			binary.LittleEndian.PutUint64(b, math.Float64bits(v))
		case "float16":
			binary.LittleEndian.PutUint16(b, Float16(float32(v)))
		case "int8":
			b[0] = byte(int8(v))
		case "uint8", "bool":
			b[0] = byte(v)
		case "int16":
			binary.LittleEndian.PutUint16(b, uint16(int16(v)))
		case "uint16":
			binary.LittleEndian.PutUint16(b, uint16(v))
		case "int32":
			binary.LittleEndian.PutUint32(b, uint32(int32(v)))
		case "uint32":
			binary.LittleEndian.PutUint32(b, uint32(v))
		case "int64":
			binary.LittleEndian.PutUint64(b, uint64(int64(v)))
		case "uint64":
			binary.LittleEndian.PutUint64(b, uint64(v))
		}
	}
	return out
}

// Decode reads elements of the type, little-endian.
func (t DType) Decode(b []byte) []float64 {
	size := t.Size()
	out := make([]float64, len(b)/size)
	for i := range out {
		e := b[i*size : (i+1)*size]
		switch t {
		case "float32":
			out[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(e)))
		case "float64":
			out[i] = math.Float64frombits(binary.LittleEndian.Uint64(e))
		case "float16":
			out[i] = float64(FromFloat16(binary.LittleEndian.Uint16(e)))
		case "int8":
			out[i] = float64(int8(e[0]))
		case "uint8", "bool":
			out[i] = float64(e[0])
		case "int16":
			out[i] = float64(int16(binary.LittleEndian.Uint16(e)))
		case "uint16":
			out[i] = float64(binary.LittleEndian.Uint16(e))
		case "int32":
			out[i] = float64(int32(binary.LittleEndian.Uint32(e)))
		case "uint32":
			out[i] = float64(binary.LittleEndian.Uint32(e))
		case "int64":
			out[i] = float64(int64(binary.LittleEndian.Uint64(e)))
		case "uint64":
			out[i] = float64(binary.LittleEndian.Uint64(e))
		}
	}
	return out
}

// Float16 is f as an IEEE 754 half-precision number, rounded to the nearest,
// ties to even.
func Float16(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16(bits>>16) & 0x8000
	exp := int32(bits>>23&0xff) - 127 + 15
	mant := bits & 0x7fffff
	switch {
	case bits&0x7fffffff == 0:
		return sign
	case bits>>23&0xff == 0xff: // infinity or NaN
		if mant != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case exp >= 0x1f:
		return sign | 0x7c00
	case exp <= 0: // subnormal, or zero
		if exp < -10 {
			return sign
		}
		mant |= 0x800000
		shift := uint32(14 - exp)
		half := uint32(1) << (shift - 1)
		rest := mant & (1<<shift - 1)
		m := mant >> shift
		if rest > half || rest == half && m&1 == 1 {
			m++
		}
		return sign | uint16(m)
	}
	m := mant >> 13
	rest := mant & 0x1fff
	h := sign | uint16(exp)<<10 | uint16(m)
	if rest > 0x1000 || rest == 0x1000 && m&1 == 1 {
		h++ // may carry into the exponent, as it should
	}
	return h
}

// FromFloat16 is an IEEE 754 half-precision number as a float32.
func FromFloat16(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h & 0x3ff)
	switch {
	case exp == 0 && mant == 0:
		return math.Float32frombits(sign)
	case exp == 0: // subnormal
		for mant&0x400 == 0 {
			mant <<= 1
			exp--
		}
		exp++
		mant &= 0x3ff
	case exp == 0x1f:
		return math.Float32frombits(sign | 0x7f800000 | mant<<13)
	}
	return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
}
