// Package keras reads Keras models' contracts (.keras, Keras 3's: a zip of
// the model's config and its weights, model.weights.h5; and .h5, the HDF5
// file Keras 2 and 3 save a model in) and writes stubs as Keras models of
// the layers every Keras has (AveragePooling2D, LayerNormalization, Dense,
// GlobalMaxPooling1D, ReLU...): the input reduced as matching reduces it,
// its correlation with each key, the best's one-hot, and the answers'
// table's product with it; a text's tokens found by an Embedding and a
// causal Conv1D. A model's config is Keras's JSON, its weights HDF5
// (package hdf5): no Python, no code in the model.
package keras

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/hdf5"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "keras", Name: "Keras", Extensions: []string{".keras", ".h5"}, Contract: ReadContract, Write: format.File(Write)})
}

// details are a Keras model's own settings in its contract: the file it is
// (keras, or h5), the version of Keras that saved it (and, of an h5 file,
// its backend), when, and its name.
type details struct {
	File         string `json:"file"`
	KerasVersion string `json:"kerasVersion,omitempty"`
	Backend      string `json:"backend,omitempty"`
	DateSaved    string `json:"dateSaved,omitempty"`
	Name         string `json:"name,omitempty"`
}

// ReadContract reads the contract of the Keras model at path: a .keras
// file's config, or a .h5 file's (its root's model_config): its inputs
// (their names, types and shapes), its outputs (their names, and their
// shapes where its last layers say them), and the layers it uses. An input
// shaped like an image is given its layout.
func ReadContract(path string) (*contract.Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	d := details{File: "keras"}
	var config []byte
	if strings.EqualFold(filepath.Ext(path), ".h5") {
		d.File = "h5"
		config, err = readH5(b, &d)
	} else {
		config, err = readArchive(b, &d)
	}
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a Keras model axx reads: %v", path, err).
			WithHint("a Keras model is a .keras file (model.save), or a .h5 file of a model (save_model's HDF5 format), not of its weights alone")
	}
	c, name, err := contractOf(config)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a Keras model axx reads: %v", path, err)
	}
	d.Name = name
	c.Format.Type = "keras"
	if c.Format.Details, err = json.Marshal(d); err != nil {
		return nil, err
	}
	for i := range c.Inputs {
		c.Inputs[i].GuessImage()
	}
	return c, nil
}

// readArchive reads a .keras file's config and metadata.
func readArchive(b []byte, d *details) ([]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, fmt.Errorf("it is not a zip: %w", err)
	}
	read := func(name string) ([]byte, error) {
		f, err := z.Open(name)
		if err != nil {
			return nil, fmt.Errorf("it has no %s", name)
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, 1<<28))
	}
	if meta, err := read("metadata.json"); err == nil {
		var m struct {
			KerasVersion string `json:"keras_version"`
			DateSaved    string `json:"date_saved"`
		}
		if err := json.Unmarshal(meta, &m); err == nil {
			d.KerasVersion, d.DateSaved = m.KerasVersion, m.DateSaved
		}
	}
	return read("config.json")
}

// readH5 reads a .h5 file's config, version and backend.
func readH5(b []byte, d *details) ([]byte, error) {
	f, err := hdf5.Open(b)
	if err != nil {
		return nil, err
	}
	root, err := f.Root()
	if err != nil {
		return nil, err
	}
	text := func(name string) (string, bool, error) {
		a, ok, err := root.Attr(name)
		if err != nil || !ok {
			return "", ok, err
		}
		if len(a.Strings) != 1 {
			return "", false, fmt.Errorf("its %s is not a text", name)
		}
		return a.Strings[0], true, nil
	}
	config, ok, err := text("model_config")
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, fmt.Errorf("it has no model_config: a file of weights alone")
	}
	if d.KerasVersion, _, err = text("keras_version"); err != nil {
		return nil, err
	}
	if d.Backend, _, err = text("backend"); err != nil {
		return nil, err
	}
	return []byte(config), nil
}

// layerConfig is a layer's config, as a model's has it.
type layerConfig struct {
	ClassName   string                     `json:"class_name"`
	Name        string                     `json:"name"`
	Config      map[string]json.RawMessage `json:"config"`
	BuildConfig *struct {
		InputShape json.RawMessage `json:"input_shape"`
	} `json:"build_config"`
	InboundNodes json.RawMessage `json:"inbound_nodes"`
}

// contractOf is a model's contract, of its config, and the model's name.
func contractOf(config []byte) (*contract.Contract, string, error) {
	var top struct {
		ClassName string `json:"class_name"`
		Config    struct {
			Name         string          `json:"name"`
			Layers       []layerConfig   `json:"layers"`
			InputLayers  json.RawMessage `json:"input_layers"`
			OutputLayers json.RawMessage `json:"output_layers"`
		} `json:"config"`
	}
	if err := json.Unmarshal(config, &top); err != nil {
		return nil, "", fmt.Errorf("its config: %w", err)
	}
	layers := top.Config.Layers
	byName := map[string]*layerConfig{}
	for i := range layers {
		name := layers[i].Name
		if name == "" {
			name = str(layers[i].Config["name"])
			layers[i].Name = name
		}
		byName[name] = &layers[i]
	}
	c := &contract.Contract{}
	ops := map[string]bool{}
	for _, l := range layers {
		ops[l.ClassName] = true
	}
	for op := range ops {
		c.Format.Operators = append(c.Format.Operators, op)
	}
	slices.Sort(c.Format.Operators)
	switch top.ClassName {
	case "Functional", "Model":
		for _, name := range refs(top.Config.InputLayers) {
			l, ok := byName[name]
			if !ok {
				return nil, "", fmt.Errorf("its input %s is not one of its layers", name)
			}
			t := contract.Tensor{Name: name, Type: dtypeOf(l.Config["dtype"])}
			t.Shape = shapeOf(l.Config["batch_shape"], l.Config["batch_input_shape"])
			c.Inputs = append(c.Inputs, t)
		}
		for _, name := range refs(top.Config.OutputLayers) {
			l, ok := byName[name]
			if !ok {
				return nil, "", fmt.Errorf("its output %s is not one of its layers", name)
			}
			c.Outputs = append(c.Outputs, contract.Tensor{Name: name, Type: "float32", Shape: outputShape(l, inboundShape(l))})
		}
	case "Sequential":
		if len(layers) == 0 {
			return nil, "", fmt.Errorf("it is a Sequential model of no layers")
		}
		first := layers[0]
		in := contract.Tensor{Name: first.Name, Type: dtypeOf(first.Config["dtype"])}
		if first.ClassName != "InputLayer" {
			in.Name = first.Name + "_input"
		}
		in.Shape = shapeOf(first.Config["batch_shape"], first.Config["batch_input_shape"])
		if in.Shape == nil && first.BuildConfig != nil {
			in.Shape = shapeOf(first.BuildConfig.InputShape)
		}
		c.Inputs = []contract.Tensor{in}
		last := &layers[len(layers)-1]
		var inShape []contract.Dim
		if last.BuildConfig != nil {
			inShape = shapeOf(last.BuildConfig.InputShape)
		}
		c.Outputs = []contract.Tensor{{Name: last.Name, Type: "float32", Shape: outputShape(last, inShape)}}
	default:
		return nil, "", fmt.Errorf("it is a model of class %s: axx reads Functional and Sequential models' configs (give a subclassed model's inputs and outputs with --input and --output)", top.ClassName)
	}
	return c, top.Config.Name, nil
}

// str is a JSON string's value, or "".
func str(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// refs are the layers' names a model's input_layers or output_layers name:
// [[name, 0, 0], ...], [name, 0, 0], or {key: [name, 0, 0]}.
func refs(raw json.RawMessage) []string {
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err == nil {
		if len(list) > 0 && str(list[0]) != "" {
			return []string{str(list[0])}
		}
		var out []string
		for _, r := range list {
			out = append(out, refs(r)...)
		}
		return out
	}
	var byKey map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byKey); err == nil {
		keys := slices.Sorted(func(yield func(string) bool) {
			for k := range byKey {
				if !yield(k) {
					return
				}
			}
		})
		var out []string
		for _, k := range keys {
			out = append(out, refs(byKey[k])...)
		}
		return out
	}
	return nil
}

// dtypeOf is a layer's type: a name, or a DTypePolicy's.
func dtypeOf(raw json.RawMessage) contract.DType {
	if s := str(raw); s != "" {
		return contract.DType(s)
	}
	var policy struct {
		Config struct {
			Name string `json:"name"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &policy); err == nil && policy.Config.Name != "" {
		if strings.HasPrefix(policy.Config.Name, "mixed_") {
			return "float32"
		}
		return contract.DType(policy.Config.Name)
	}
	return "float32"
}

// shapeOf is the first of the shapes given ([null, 224, 224, 3]): its
// batch "batch", its other open dimensions "?".
func shapeOf(raws ...json.RawMessage) []contract.Dim {
	for _, raw := range raws {
		var dims []*int64
		if err := json.Unmarshal(raw, &dims); err != nil || dims == nil {
			continue
		}
		out := make([]contract.Dim, len(dims))
		for i, d := range dims {
			switch {
			case d != nil:
				out[i] = contract.Dim{Size: *d}
			case i == 0:
				out[i] = contract.Dim{Name: "batch"}
			default:
				out[i] = contract.Dim{Name: "?"}
			}
		}
		return out
	}
	return nil
}

// inboundShape is the shape of a layer's (first) input, as Keras 3's
// configs say it (its inbound tensor's, or its build config's).
func inboundShape(l *layerConfig) []contract.Dim {
	var nodes []struct {
		Args []json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal(l.InboundNodes, &nodes); err == nil && len(nodes) > 0 && len(nodes[0].Args) > 0 {
		var t struct {
			Config struct {
				Shape json.RawMessage `json:"shape"`
			} `json:"config"`
		}
		if err := json.Unmarshal(nodes[0].Args[0], &t); err == nil {
			if s := shapeOf(t.Config.Shape); s != nil {
				return s
			}
		}
	}
	if l.BuildConfig != nil {
		return shapeOf(l.BuildConfig.InputShape)
	}
	return nil
}

// outputShape is a layer's output's shape, of its input's, where its class
// says it; or none, for `axx models contract --output` to give.
func outputShape(l *layerConfig, in []contract.Dim) []contract.Dim {
	cfg := l.Config
	batch := []contract.Dim{{Name: "batch"}}
	switch l.ClassName {
	case "Dense":
		var units int64
		if json.Unmarshal(cfg["units"], &units) == nil && in != nil {
			return append(slices.Clone(in[:len(in)-1]), contract.Dim{Size: units})
		}
	case "Reshape":
		var target []int64
		if json.Unmarshal(cfg["target_shape"], &target) == nil && !slices.Contains(target, -1) {
			out := slices.Clone(batch)
			for _, t := range target {
				out = append(out, contract.Dim{Size: t})
			}
			return out
		}
	case "Flatten":
		size := int64(1)
		for _, d := range in[min(1, len(in)):] {
			if !d.Fixed() {
				return nil
			}
			size *= d.Size
		}
		if in != nil {
			return append(batch, contract.Dim{Size: size})
		}
	case "GlobalAveragePooling1D", "GlobalAveragePooling2D", "GlobalAveragePooling3D", "GlobalMaxPooling1D", "GlobalMaxPooling2D", "GlobalMaxPooling3D":
		if in != nil && str(cfg["data_format"]) != "channels_first" && !strings.Contains(string(cfg["keepdims"]), "true") {
			return append(batch, in[len(in)-1])
		}
	case "Activation", "Softmax", "ReLU", "LeakyReLU", "Dropout", "BatchNormalization", "LayerNormalization",
		"Rescaling", "Normalization", "UnitNormalization", "Identity":
		return in
	}
	return nil
}

// Write writes a stub as a Keras model: a .keras file, or a .h5 file, as
// the real model is (its contract's format.details say).
func Write(s *stub.Stub) ([]byte, error) {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %s", fmt.Sprintf(f, args...))
	}
	if s.Generated != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: Keras's stubs answer, and do not generate text").
			WithHint("a language model's stub is written for its runtime's format: GGUF, ONNX, safetensors, ExecuTorch, LiteRT-LM")
	}
	d := details{File: "keras"}
	if len(s.Contract.Format.Details) > 0 {
		if err := json.Unmarshal(s.Contract.Format.Details, &d); err != nil {
			return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a Keras model's: %v", err)
		}
	}
	major := 3
	if d.KerasVersion != "" {
		v, _, _ := strings.Cut(d.KerasVersion, ".")
		if n, err := strconv.Atoi(v); err == nil {
			major = n
		}
	}
	dialect := keras3
	switch {
	case major < 3 && d.File != "h5":
		return nil, fail("Keras %s saves models as .h5 files: the contract's format.details.file is %q", d.KerasVersion, d.File)
	case major < 3:
		dialect = keras2
	case d.File == "h5":
		dialect = keras3Legacy
	}
	name := d.Name
	if name == "" {
		name = "functional"
	}
	m, err := stubModel(s, name, dialect)
	if err != nil {
		return nil, fail("%v", err)
	}
	if dialect == keras3 && slices.ContainsFunc(m.layers, func(l *layer) bool { return l.class == "STFTSpectrogram" }) && older(d.KerasVersion, 3, 6) {
		return nil, fail("its sound's spectrum takes STFTSpectrogram, which Keras has from 3.6: the model is of Keras %s", d.KerasVersion)
	}
	version := d.KerasVersion
	if version == "" {
		version = "3.6.0"
	}
	if d.File == "h5" {
		backend := d.Backend
		if backend == "" {
			backend = "tensorflow"
		}
		b, err := m.legacy(version, backend, dialect)
		if err != nil {
			return nil, fail("%v", err)
		}
		return b, nil
	}
	b, err := m.archive(version, d.DateSaved)
	if err != nil {
		return nil, fail("%v", err)
	}
	return b, nil
}

// older reports whether a version (major.minor...) is older than
// major.minor; an unknown one is not.
func older(version string, major, minor int) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return false
	}
	a, err1 := strconv.Atoi(parts[0])
	b, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return a < major || a == major && b < minor
}
