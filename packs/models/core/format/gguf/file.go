// Package gguf reads and writes GGUF models, llama.cpp's format (and that of
// the runtimes built on it: node-llama-cpp, llama-cpp-python, llama.rn,
// LM Studio's and Ollama's engines): its metadata, which has the model's
// architecture, hyperparameters, tokenizer and chat template, and its
// tensors.
package gguf

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
)

// Magic begins a GGUF file.
const Magic = "GGUF"

// Version is the version of the files written.
const Version = 3

// Alignment is where tensors' data begins, unless general.alignment says.
const Alignment = 32

// Value types of metadata.
const (
	typeUint8 uint32 = iota
	typeInt8
	typeUint16
	typeInt16
	typeUint32
	typeInt32
	typeFloat32
	typeBool
	typeString
	typeArray
	typeUint64
	typeInt64
	typeFloat64
)

// File is a GGUF file: its metadata, in order, and its tensors.
type File struct {
	Version uint32
	Meta    []KV
	Tensors []Tensor
}

// KV is a metadata key and its value: a uint8, int8, uint16, int16, uint32,
// int32, float32, bool, string, uint64, int64 or float64, or an Array.
type KV struct {
	Key   string
	Value any
}

// Array is an array of metadata values of one type.
type Array struct {
	Type   uint32
	Values []any
}

// Tensor is a tensor: its name, its dimensions (the first is the innermost,
// as ggml has them), its ggml type and its data.
type Tensor struct {
	Name string
	Dims []uint64
	Type GGMLType
	Data []byte
}

// Get is the value of the key, or nil.
func (f *File) Get(key string) any {
	for _, kv := range f.Meta {
		if kv.Key == key {
			return kv.Value
		}
	}
	return nil
}

// Set sets the key's value, in its place or last.
func (f *File) Set(key string, v any) {
	for i := range f.Meta {
		if f.Meta[i].Key == key {
			f.Meta[i].Value = v
			return
		}
	}
	f.Meta = append(f.Meta, KV{key, v})
}

// String is the key's string value, or "".
func (f *File) String(key string) string {
	s, _ := f.Get(key).(string)
	return s
}

// Uint is the key's value as an unsigned integer, and whether it is one.
func (f *File) Uint(key string) (uint64, bool) {
	switch v := f.Get(key).(type) {
	case uint8:
		return uint64(v), true
	case uint16:
		return uint64(v), true
	case uint32:
		return uint64(v), true
	case uint64:
		return v, true
	case int8:
		return uint64(v), v >= 0
	case int16:
		return uint64(v), v >= 0
	case int32:
		return uint64(v), v >= 0
	case int64:
		return uint64(v), v >= 0
	}
	return 0, false
}

// Strings is the key's array of strings, or nil.
func (f *File) Strings(key string) []string {
	a, ok := f.Get(key).(Array)
	if !ok || a.Type != typeString {
		return nil
	}
	out := make([]string, len(a.Values))
	for i, v := range a.Values {
		out[i] = v.(string)
	}
	return out
}

// Tensor is the tensor of the name, or nil.
func (f *File) Tensor(name string) *Tensor {
	for i := range f.Tensors {
		if f.Tensors[i].Name == name {
			return &f.Tensors[i]
		}
	}
	return nil
}

// Read reads a GGUF file. With tensors false, it reads the tensors' names,
// dimensions and types, not their data.
func Read(path string, tensors bool) (*File, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return read(fh, tensors)
}

// readFrom reads a GGUF file's metadata and tensors from r.
func readFrom(r io.Reader) (*File, error) { return read(r, true) }

func read(in io.Reader, tensors bool) (*File, error) {
	r := &reader{r: bufio.NewReaderSize(in, 1<<20)}
	if string(r.bytes(4)) != Magic {
		return nil, errors.New("it is not a GGUF file")
	}
	f := &File{Version: r.u32()}
	if r.err == nil && (f.Version < 2 || f.Version > 3) {
		return nil, fmt.Errorf("it is a GGUF file of version %d; axx reads versions 2 and 3", f.Version)
	}
	nt, nkv := r.u64(), r.u64()
	if r.err == nil && (nt > 1<<24 || nkv > 1<<24) {
		return nil, errors.New("its header is not a GGUF file's")
	}
	for range nkv {
		key := r.str()
		v := r.value(r.u32())
		if r.err != nil {
			return nil, fmt.Errorf("its metadata: %w", r.err)
		}
		f.Meta = append(f.Meta, KV{key, v})
	}
	offsets := make([]uint64, nt)
	for i := range nt {
		t := Tensor{Name: r.str()}
		n := r.u32()
		if n > 8 {
			return nil, fmt.Errorf("tensor %s has %d dimensions", t.Name, n)
		}
		for range n {
			t.Dims = append(t.Dims, r.u64())
		}
		t.Type = GGMLType(r.u32())
		offsets[i] = r.u64()
		if r.err != nil {
			return nil, fmt.Errorf("its tensors: %w", r.err)
		}
		f.Tensors = append(f.Tensors, t)
	}
	if !tensors || r.err != nil {
		return f, r.err
	}
	align := uint64(Alignment)
	if a, ok := f.Uint("general.alignment"); ok && a > 0 {
		align = a
	}
	r.skip(pad(r.n, align))
	start := r.n
	order := make([]int, nt)
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return int(offsets[a]) - int(offsets[b]) })
	for _, i := range order {
		t := &f.Tensors[i]
		size, err := t.Type.Size(t.Dims)
		if err != nil {
			return nil, fmt.Errorf("tensor %s: %w", t.Name, err)
		}
		r.skip(start + offsets[i] - r.n)
		t.Data = r.bytes(int(size))
	}
	return f, r.err
}

// Write writes the file, its tensors aligned to general.alignment.
func (f *File) Write(w io.Writer) error {
	bw := &writer{w: bufio.NewWriterSize(w, 1<<20)}
	bw.raw([]byte(Magic))
	bw.u32(Version)
	bw.u64(uint64(len(f.Tensors)))
	bw.u64(uint64(len(f.Meta)))
	for _, kv := range f.Meta {
		bw.str(kv.Key)
		bw.value(kv.Value, true)
	}
	align := uint64(Alignment)
	if a, ok := f.Uint("general.alignment"); ok && a > 0 {
		align = a
	}
	var offset uint64
	for _, t := range f.Tensors {
		bw.str(t.Name)
		bw.u32(uint32(len(t.Dims)))
		for _, d := range t.Dims {
			bw.u64(d)
		}
		bw.u32(uint32(t.Type))
		bw.u64(offset)
		offset += uint64(len(t.Data))
		offset += pad(offset, align)
	}
	bw.raw(make([]byte, pad(bw.n, align)))
	for _, t := range f.Tensors {
		bw.raw(t.Data)
		bw.raw(make([]byte, pad(uint64(len(t.Data)), align)))
	}
	if bw.err != nil {
		return bw.err
	}
	return bw.w.Flush()
}

func pad(n, align uint64) uint64 { return (align - n%align) % align }

type reader struct {
	r   *bufio.Reader
	n   uint64
	err error
}

func (r *reader) bytes(n int) []byte {
	if r.err != nil {
		return nil
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r.r, b); err != nil {
		r.err = err
		return nil
	}
	r.n += uint64(n)
	return b
}

func (r *reader) skip(n uint64) {
	if r.err != nil {
		return
	}
	if _, err := r.r.Discard(int(n)); err != nil {
		r.err = err
	}
	r.n += n
}

func (r *reader) u32() uint32 {
	b := r.bytes(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (r *reader) u64() uint64 {
	b := r.bytes(8)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

func (r *reader) str() string {
	n := r.u64()
	if n > 1<<30 {
		r.err = errors.New("a string longer than a GGUF file has")
		return ""
	}
	return string(r.bytes(int(n)))
}

func (r *reader) value(t uint32) any {
	switch t {
	case typeUint8:
		b := r.bytes(1)
		if b == nil {
			return nil
		}
		return b[0]
	case typeInt8:
		b := r.bytes(1)
		if b == nil {
			return nil
		}
		return int8(b[0])
	case typeUint16:
		b := r.bytes(2)
		if b == nil {
			return nil
		}
		return binary.LittleEndian.Uint16(b)
	case typeInt16:
		b := r.bytes(2)
		if b == nil {
			return nil
		}
		return int16(binary.LittleEndian.Uint16(b))
	case typeUint32:
		return r.u32()
	case typeInt32:
		return int32(r.u32())
	case typeFloat32:
		return math.Float32frombits(r.u32())
	case typeBool:
		b := r.bytes(1)
		if b == nil {
			return nil
		}
		return b[0] != 0
	case typeString:
		return r.str()
	case typeUint64:
		return r.u64()
	case typeInt64:
		return int64(r.u64())
	case typeFloat64:
		return math.Float64frombits(r.u64())
	case typeArray:
		et, n := r.u32(), r.u64()
		if n > 1<<26 {
			r.err = errors.New("an array longer than a GGUF file has")
			return nil
		}
		a := Array{Type: et, Values: make([]any, 0, n)}
		for range n {
			v := r.value(et)
			if r.err != nil {
				return nil
			}
			a.Values = append(a.Values, v)
		}
		return a
	}
	r.err = fmt.Errorf("a metadata value of type %d, which GGUF has not", t)
	return nil
}

type writer struct {
	w   *bufio.Writer
	n   uint64
	err error
}

func (w *writer) raw(b []byte) {
	if w.err != nil {
		return
	}
	_, w.err = w.w.Write(b)
	w.n += uint64(len(b))
}

func (w *writer) u32(v uint32) { w.raw(binary.LittleEndian.AppendUint32(nil, v)) }
func (w *writer) u64(v uint64) { w.raw(binary.LittleEndian.AppendUint64(nil, v)) }

func (w *writer) str(s string) {
	w.u64(uint64(len(s)))
	w.raw([]byte(s))
}

// value writes a value, with its type when typed.
func (w *writer) value(v any, typed bool) {
	t := func(id uint32) {
		if typed {
			w.u32(id)
		}
	}
	switch v := v.(type) {
	case uint8:
		t(typeUint8)
		w.raw([]byte{v})
	case int8:
		t(typeInt8)
		w.raw([]byte{byte(v)})
	case uint16:
		t(typeUint16)
		w.raw(binary.LittleEndian.AppendUint16(nil, v))
	case int16:
		t(typeInt16)
		w.raw(binary.LittleEndian.AppendUint16(nil, uint16(v)))
	case uint32:
		t(typeUint32)
		w.u32(v)
	case int32:
		t(typeInt32)
		w.u32(uint32(v))
	case float32:
		t(typeFloat32)
		w.u32(math.Float32bits(v))
	case bool:
		t(typeBool)
		if v {
			w.raw([]byte{1})
		} else {
			w.raw([]byte{0})
		}
	case string:
		t(typeString)
		w.str(v)
	case uint64:
		t(typeUint64)
		w.u64(v)
	case int64:
		t(typeInt64)
		w.u64(uint64(v))
	case float64:
		t(typeFloat64)
		w.u64(math.Float64bits(v))
	case Array:
		t(typeArray)
		w.u32(v.Type)
		w.u64(uint64(len(v.Values)))
		for _, e := range v.Values {
			w.value(e, false)
		}
	default:
		if w.err == nil {
			w.err = fmt.Errorf("a metadata value of Go type %T, which GGUF has not", v)
		}
	}
}

// Strings is an array of strings.
func Strings(s []string) Array {
	a := Array{Type: typeString, Values: make([]any, len(s))}
	for i, v := range s {
		a.Values[i] = v
	}
	return a
}

// Float32s is an array of float32s.
func Float32s(f []float32) Array {
	a := Array{Type: typeFloat32, Values: make([]any, len(f))}
	for i, v := range f {
		a.Values[i] = v
	}
	return a
}

// Int32s is an array of int32s.
func Int32s(n []int32) Array {
	a := Array{Type: typeInt32, Values: make([]any, len(n))}
	for i, v := range n {
		a.Values[i] = v
	}
	return a
}
