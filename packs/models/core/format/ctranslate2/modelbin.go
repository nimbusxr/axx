package ctranslate2

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// A model.bin is CTranslate2's model (models/model.cc, as its converters'
// specs serialize it, binary version 6): its spec's name and revision; its
// variables, each a name, dimensions, a type and its bytes; and aliases,
// each a name of another's variable.

// binaryVersion is the version of model.bin a stub is.
const binaryVersion = 6

// Types of variables, as CTranslate2's DataType has them.
const (
	typeFloat32 = 0
	typeInt8    = 1
	typeInt16   = 2
	typeInt32   = 3
	typeFloat16 = 4
)

// variable is a model's variable: its dimensions and type (its bytes, as
// a stub writes them; not read of a real model's).
type variable struct {
	Name string
	Dims []int
	Type uint8
	Data []byte
}

// header is what a model.bin says of itself: its spec's name and revision,
// its variables (their bytes, of its scalars alone), and its aliases.
type header struct {
	Spec     string
	Revision uint32
	Vars     []variable
	Aliases  [][2]string
}

// readHeader reads a model.bin's header: its variables' bytes it skips,
// but its scalars'.
func readHeader(path string) (h *header, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	u32 := func() uint32 {
		var v uint32
		if err == nil {
			err = binary.Read(r, binary.LittleEndian, &v)
		}
		return v
	}
	str := func() string {
		var n uint16
		if err == nil {
			err = binary.Read(r, binary.LittleEndian, &n)
		}
		if err != nil || n == 0 {
			return ""
		}
		b := make([]byte, n)
		if _, e := io.ReadFull(r, b); e != nil {
			err = e
			return ""
		}
		return string(b[:n-1])
	}
	if v := u32(); err != nil || v < 5 || v > binaryVersion {
		return nil, fmt.Errorf("it is not a model.bin of CTranslate2's binary version 5 or %d (%d)", binaryVersion, v)
	}
	h = &header{Spec: str(), Revision: u32()}
	n := u32()
	for range n {
		if err != nil {
			break
		}
		v := variable{Name: str()}
		rank, e := r.ReadByte()
		if e != nil {
			return nil, e
		}
		for range rank {
			v.Dims = append(v.Dims, int(u32()))
		}
		t, e := r.ReadByte()
		if e != nil {
			return nil, e
		}
		v.Type = t
		size := u32()
		if err != nil {
			break
		}
		if rank == 0 {
			v.Data = make([]byte, size)
			_, err = io.ReadFull(r, v.Data)
		} else if _, e := r.Discard(int(size)); e != nil {
			err = e
		}
		h.Vars = append(h.Vars, v)
	}
	na := u32()
	for range na {
		if err != nil {
			break
		}
		h.Aliases = append(h.Aliases, [2]string{str(), str()})
	}
	if err != nil {
		return nil, fmt.Errorf("its model.bin is cut short: %w", err)
	}
	return h, nil
}

// variable is the header's variable of the name (or of its alias).
func (h *header) variable(name string) (*variable, bool) {
	for _, a := range h.Aliases {
		if a[0] == name {
			name = a[1]
		}
	}
	for i := range h.Vars {
		if h.Vars[i].Name == name {
			return &h.Vars[i], true
		}
	}
	return nil, false
}

// scalar is a scalar variable's value.
func (v *variable) scalar() (int64, error) {
	switch {
	case len(v.Dims) != 0:
		return 0, errors.New("not a scalar")
	case v.Type == typeInt8 && len(v.Data) == 1:
		return int64(int8(v.Data[0])), nil
	case v.Type == typeInt16 && len(v.Data) == 2:
		return int64(int16(binary.LittleEndian.Uint16(v.Data))), nil
	case v.Type == typeInt32 && len(v.Data) == 4:
		return int64(int32(binary.LittleEndian.Uint32(v.Data))), nil
	}
	return 0, fmt.Errorf("a scalar of type %d", v.Type)
}

// encode is a model.bin of the header: its variables in order of their
// names, then its aliases.
func (h *header) encode() []byte {
	var b bytes.Buffer
	str := func(s string) {
		_ = binary.Write(&b, binary.LittleEndian, uint16(len(s)+1))
		b.WriteString(s)
		b.WriteByte(0)
	}
	_ = binary.Write(&b, binary.LittleEndian, uint32(binaryVersion))
	str(h.Spec)
	_ = binary.Write(&b, binary.LittleEndian, h.Revision)
	vars := slices.Clone(h.Vars)
	slices.SortFunc(vars, func(a, c variable) int {
		switch {
		case a.Name < c.Name:
			return -1
		case a.Name > c.Name:
			return 1
		}
		return 0
	})
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(vars)))
	for _, v := range vars {
		str(v.Name)
		b.WriteByte(byte(len(v.Dims)))
		for _, d := range v.Dims {
			_ = binary.Write(&b, binary.LittleEndian, uint32(d))
		}
		b.WriteByte(v.Type)
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(v.Data)))
		b.Write(v.Data)
	}
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(h.Aliases)))
	for _, a := range h.Aliases {
		str(a[0])
		str(a[1])
	}
	return b.Bytes()
}

// float32s is a variable of float32s.
func float32s(name string, dims []int, data []float32) variable {
	b := make([]byte, 4*len(data))
	for i, x := range data {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return variable{Name: name, Dims: dims, Type: typeFloat32, Data: b}
}

// float16s is a variable of float16s: each float32 rounded to the nearest
// half.
func float16s(name string, dims []int, data []float32) variable {
	b := make([]byte, 2*len(data))
	for i, x := range data {
		binary.LittleEndian.PutUint16(b[2*i:], contract.Float16(x))
	}
	return variable{Name: name, Dims: dims, Type: typeFloat16, Data: b}
}
