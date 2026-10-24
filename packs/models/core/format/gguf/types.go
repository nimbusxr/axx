package gguf

import (
	"encoding/binary"
	"fmt"
	"math"
)

// GGMLType is a tensor's type in ggml, llama.cpp's tensor library.
type GGMLType uint32

// The types axx writes. A stub's tensors are F32, but its token embeddings,
// which have a row for each of the vocabulary's tokens: Q4_0.
const (
	F32  GGMLType = 0
	F16  GGMLType = 1
	Q4_0 GGMLType = 2
	Q8_0 GGMLType = 8
)

// typeInfo is a type's name, how many elements a block of it has, and how
// many bytes.
type typeInfo struct {
	name        string
	block, size uint64
}

// types are ggml's types (ggml.h, ggml-common.h).
var types = map[GGMLType]typeInfo{
	0: {"F32", 1, 4}, 1: {"F16", 1, 2}, 2: {"Q4_0", 32, 18}, 3: {"Q4_1", 32, 20},
	6: {"Q5_0", 32, 22}, 7: {"Q5_1", 32, 24}, 8: {"Q8_0", 32, 34}, 9: {"Q8_1", 32, 36},
	10: {"Q2_K", 256, 84}, 11: {"Q3_K", 256, 110}, 12: {"Q4_K", 256, 144}, 13: {"Q5_K", 256, 176},
	14: {"Q6_K", 256, 210}, 15: {"Q8_K", 256, 292}, 16: {"IQ2_XXS", 256, 66}, 17: {"IQ2_XS", 256, 74},
	18: {"IQ3_XXS", 256, 98}, 19: {"IQ1_S", 256, 50}, 20: {"IQ4_NL", 32, 18}, 21: {"IQ3_S", 256, 110},
	22: {"IQ2_S", 256, 82}, 23: {"IQ4_XS", 256, 136}, 24: {"I8", 1, 1}, 25: {"I16", 1, 2},
	26: {"I32", 1, 4}, 27: {"I64", 1, 8}, 28: {"F64", 1, 8}, 29: {"IQ1_M", 256, 56},
	30: {"BF16", 1, 2}, 34: {"TQ1_0", 256, 54}, 35: {"TQ2_0", 256, 66}, 39: {"MXFP4", 32, 17},
	40: {"NVFP4", 64, 36}, 41: {"Q1_0", 128, 18}, 42: {"Q2_0", 64, 18},
}

func (t GGMLType) String() string {
	if i, ok := types[t]; ok {
		return i.name
	}
	return fmt.Sprintf("type %d", uint32(t))
}

// Size is how many bytes a tensor of the type and dimensions has.
func (t GGMLType) Size(dims []uint64) (uint64, error) {
	i, ok := types[t]
	if !ok {
		return 0, fmt.Errorf("it is of ggml type %d, which axx does not know", uint32(t))
	}
	n := uint64(1)
	for _, d := range dims {
		n *= d
	}
	if len(dims) > 0 && dims[0]%i.block != 0 {
		return 0, fmt.Errorf("its rows of %d are not blocks of %s (%d)", dims[0], i.name, i.block)
	}
	return n / i.block * i.size, nil
}

// f32Data is the data of an F32 tensor.
func f32Data(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// f16Data is the data of an F16 tensor.
func f16Data(v []float32) []byte {
	b := make([]byte, 2*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint16(b[2*i:], f16(x))
	}
	return b
}

// q4Data is the data of a Q4_0 tensor of values, a block of 32 at a time, as
// ggml quantizes them (quantize_row_q4_0_ref): each block has a scale d, of
// the value furthest from 0 over −8, and each value is d × (q − 8), q from 0
// to 15. A block of 0s and one other value v is exact: v is q = 0.
func q4Data(v []float32) []byte {
	out := make([]byte, 0, len(v)/32*18)
	for b := 0; b < len(v); b += 32 {
		block := v[b : b+32]
		var amax, vmax float32
		for _, x := range block {
			if a := float32(math.Abs(float64(x))); a > amax {
				amax, vmax = a, x
			}
		}
		d := vmax / -8
		var id float32
		if d != 0 {
			id = 1 / d
		}
		out = binary.LittleEndian.AppendUint16(out, f16(d))
		var qs [16]byte
		for j := range 16 {
			x0, x1 := block[j]*id, block[16+j]*id
			q0 := min(15, int8(x0+8.5))
			q1 := min(15, int8(x1+8.5))
			qs[j] = byte(q0) | byte(q1)<<4
		}
		out = append(out, qs[:]...)
	}
	return out
}

// f16 is a float32 as an IEEE half, rounded to the nearest.
func f16(f float32) uint16 {
	b := math.Float32bits(f)
	sign := uint16(b>>16) & 0x8000
	exp := int((b>>23)&0xff) - 127 + 15
	mant := b & 0x7fffff
	switch {
	case (b>>23)&0xff == 0xff:
		if mant != 0 {
			return sign | 0x7e00
		}
		return sign | 0x7c00
	case exp >= 31:
		return sign | 0x7c00
	case exp <= 0:
		if exp < -10 {
			return sign
		}
		mant |= 0x800000
		shift := uint(14 - exp)
		half := uint32(1) << (shift - 1)
		r := mant >> shift
		if rem := mant & (1<<shift - 1); rem > half || rem == half && r&1 == 1 {
			r++
		}
		return sign | uint16(r)
	}
	r := uint16(exp)<<10 | uint16(mant>>13)
	if rem := mant & 0x1fff; rem > 0x1000 || rem == 0x1000 && r&1 == 1 {
		r++
	}
	return sign | r
}

// f16To32 is an IEEE half as a float32.
func f16To32(h uint16) float32 {
	sign := uint32(h&0x8000) << 16
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h & 0x3ff)
	switch {
	case exp == 0x1f:
		return math.Float32frombits(sign | 0x7f800000 | mant<<13)
	case exp == 0 && mant == 0:
		return math.Float32frombits(sign)
	case exp == 0:
		// A subnormal: normalize it.
		e := uint32(127 - 15 + 1)
		for mant&0x400 == 0 {
			mant <<= 1
			e--
		}
		return math.Float32frombits(sign | e<<23 | (mant&0x3ff)<<13)
	}
	return math.Float32frombits(sign | (exp+127-15)<<23 | mant<<13)
}
