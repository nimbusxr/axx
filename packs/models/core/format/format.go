// Package format lists the model formats stubs are written in: for each, how
// a model's contract is read from a model, and how a stub is written as one.
package format

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/task"
)

// Error codes of reading models and writing stubs.
const (
	// CodeModel: a model cannot be read.
	CodeModel = "AXX-E0702"
	// CodeWrite: a stub cannot be written in its model's format.
	CodeWrite = "AXX-E0703"
)

// Format is a model format. The models-core pack has ONNX, TFLite and Core
// ML; a pack registers its own.
type Format struct {
	// Type is the format, as a contract names it: "onnx".
	Type string
	// Name is its name, as people write it: "ONNX".
	Name string
	// Extensions are its models' file extensions: ".onnx".
	Extensions []string
	// Folders are the files a model's folder of the format has, which name
	// the folder a model of it ("saved_model.pb").
	Folders []string
	// Sniff, of a format whose extension another's is too (".bin"), reports
	// whether a file is a model of it, by its content.
	Sniff func(path string) bool
	// Contract reads a model's contract from its file (or folder), when the
	// format can: without it, the contract is written by hand.
	Contract func(path string) (*contract.Contract, error)
	// Write writes a stub as a model of the format: a file, or a folder of
	// them.
	Write func(s *stub.Stub) (Model, error)
	// API says its models are an API's, not a graph of tensors (a
	// Foundation Models language model): their contracts have no inputs or
	// outputs, and their stubs answer a prompt.
	API bool
	// Operators reads the operators a model calls, when reading its
	// contract needs more than the model (a language model's tokenizer,
	// beside it). Without it, a model's operators are its contract's.
	Operators func(path string) ([]string, error)
}

// OperatorsOf reads the operators the model at path calls, as the format
// reads them: none, when it reads no contracts.
func (f Format) OperatorsOf(path string) ([]string, error) {
	if f.Operators != nil {
		return f.Operators(path)
	}
	if f.Contract == nil {
		return nil, nil
	}
	c, err := f.Contract(path)
	if err != nil {
		return nil, err
	}
	return c.Format.Operators, nil
}

// Model is a stub written: one file, or a folder of files (an .mlpackage, a
// SavedModel, a model and its weights).
type Model struct {
	// File is the model, when it is a file.
	File []byte
	// Files are the folder's files, by their paths in it (with slashes),
	// when it is a folder.
	Files map[string][]byte
	// Beside are files that go beside the model's file, by their names (or
	// their paths, slashed, under its folder: a sentence-transformers
	// model's 2_Dense/model.safetensors), which the app's runtime loads with
	// it (an ONNX model's external data). A name that begins with "." is the
	// model's file's, of that extension: an OpenVINO model's weights (".bin":
	// model.bin beside model.xml).
	Beside map[string][]byte
}

// File is a model of one file, written by a format's writer of one.
func File(write func(s *stub.Stub) ([]byte, error)) func(s *stub.Stub) (Model, error) {
	return func(s *stub.Stub) (Model, error) {
		b, err := write(s)
		return Model{File: b}, err
	}
}

// Unmatched is the error of a format's writer that has not the stub's
// matcher.
func Unmatched(format string, s *stub.Stub) error {
	return axxerr.New(CodeWrite, exitcode.Usage, "the stub cannot be written: a %s stub matches inputs to the nearest key, not by the %s matcher", format, s.Matcher)
}

// MinTail is how many values an output's answers must end alike in for a
// writer to store them once (Tail).
const MinTail = 1024

// Tail is how an output's answers end alike: the length of the values before
// the first from which every answer has one value, that value, and whether
// the tail is long enough (MinTail) to store once and pad each answer with.
// A detector's answers do: their objects, then nothing for every other
// anchor.
func Tail(answers []task.Values, name string) (prefix int, value float64, ok bool) {
	if len(answers) == 0 || len(answers[0][name]) == 0 {
		return 0, 0, false
	}
	size := len(answers[0][name])
	value = answers[0][name][size-1]
	prefix = size
	for prefix > 0 {
		alike := true
		for _, a := range answers {
			if a[name][prefix-1] != value {
				alike = false
				break
			}
		}
		if !alike {
			break
		}
		prefix--
	}
	return max(prefix, 1), value, size-max(prefix, 1) >= MinTail
}

// OneHot is how an output's answers are one-hot along its last axis, of
// depth values (each pixel's labels' scores, of a segmenter's mask): every
// group of depth values one on, the rest off, alike in every answer. It
// gives each answer's indexes of its on values, and whether they are one-hot
// and long enough (MinTail) to store as indexes.
func OneHot(answers []task.Values, name string, depth int) (indexes [][]int, on, off float64, ok bool) {
	if depth < 2 || len(answers) == 0 {
		return nil, 0, 0, false
	}
	size := len(answers[0][name])
	if size%depth != 0 || size < MinTail {
		return nil, 0, 0, false
	}
	first := answers[0][name]
	on, off = first[0], first[0]
	for _, v := range first[:depth] {
		on, off = max(on, v), min(off, v)
	}
	if on == off {
		return nil, 0, 0, false
	}
	for _, a := range answers {
		v := a[name]
		idx := make([]int, 0, size/depth)
		for g := 0; g < size; g += depth {
			at := -1
			for k, x := range v[g : g+depth] {
				switch {
				case x == on && at < 0:
					at = k
				case x != off:
					return nil, 0, 0, false
				}
			}
			if at < 0 {
				return nil, 0, 0, false
			}
			idx = append(idx, at)
		}
		indexes = append(indexes, idx)
	}
	return indexes, on, off, true
}

// Palette is how an output's answers take few values (a segmenter's mask of
// confidences: its gray levels'): each answer's values as indexes of the
// palette, the values they take, of at most 256, so that a byte holds each.
// It is worth it, and ok, for answers of MinTail values or more.
func Palette(answers []task.Values, name string) (indexes [][]int, palette []float64, ok bool) {
	if len(answers) == 0 || len(answers[0][name]) < MinTail {
		return nil, nil, false
	}
	at := map[float64]int{}
	for _, a := range answers {
		idx := make([]int, len(a[name]))
		for i, v := range a[name] {
			k, seen := at[v]
			if !seen {
				if len(palette) == 256 {
					return nil, nil, false
				}
				k = len(palette)
				at[v] = k
				palette = append(palette, v)
			}
			idx[i] = k
		}
		indexes = append(indexes, idx)
	}
	return indexes, palette, true
}

var formats = map[string]Format{}

// Register adds a format. A format has one type: registering another of it is
// a mistake of the pack that does.
func Register(f Format) {
	if _, ok := formats[f.Type]; ok {
		panic(fmt.Sprintf("models: format %q is registered twice", f.Type))
	}
	if f.API {
		contract.RegisterAPI(f.Type)
	}
	formats[f.Type] = f
}

// ByType is the format a contract names.
func ByType(t string) (Format, bool) {
	f, ok := formats[t]
	return f, ok
}

// ByFile is the format of a model's file, by its extension.
func ByFile(path string) (Format, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	// A format that tells its models by their content first, then one of
	// the extension alone.
	for _, sniffs := range []bool{true, false} {
		for _, f := range formats {
			if slices.Contains(f.Extensions, ext) && (f.Sniff != nil) == sniffs && (f.Sniff == nil || f.Sniff(path)) {
				return f, true
			}
		}
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		for _, f := range formats {
			for _, name := range f.Folders {
				if _, err := os.Stat(filepath.Join(path, name)); err == nil {
					return f, true
				}
			}
		}
	}
	return Format{}, false
}

// Beyond are the operators the stub, read back, uses that the model of the
// contract does not: a runtime built for that model alone (a minimal build)
// lacks them. A contract that does not list its model's operators has none
// beyond.
func Beyond(model, stub *contract.Contract) []string {
	if len(model.Format.Operators) == 0 {
		return nil
	}
	var out []string
	for _, op := range stub.Format.Operators {
		if !slices.Contains(model.Format.Operators, op) {
			out = append(out, op)
		}
	}
	return out
}

// Types are the formats' types.
func Types() []string {
	out := make([]string, 0, len(formats))
	for t := range formats {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// BesideName is the name of a file beside the model's file out: name, or,
// when it begins with ".", out's name of that extension.
func BesideName(out, name string) string {
	if strings.HasPrefix(name, ".") {
		base := filepath.Base(out)
		return strings.TrimSuffix(base, filepath.Ext(base)) + name
	}
	return name
}
