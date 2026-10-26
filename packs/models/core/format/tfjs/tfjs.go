// Package tfjs reads TensorFlow.js graph models' contracts (model.json and
// its weights' shards, as TensorFlow.js's converter writes them: the models
// pages, Node.js services and React Native apps load with tf.loadGraphModel)
// and writes stubs as graph models: the SavedModel stub's graph (package
// tensorflow), its integers TensorFlow.js's int32, its constants in one
// shard, its signature the real model's.
package tfjs

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/tensorflow"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "tfjs", Name: "TensorFlow.js", Extensions: []string{".json"}, Contract: ReadContract, Write: Write})
}

// shard is the stub's weights' file, beside its model.json.
const shard = "group1-shard1of1.bin"

// details are a graph model's own settings in its contract: the names of
// its signature's tensors in its graph, and who converted it. A model of no
// signature (as TensorFlow.js's converter wrote them before version 2) is
// unsigned: its inputs are its placeholders and its outputs the nodes no
// node takes, by their names.
type details struct {
	Signature   tensorflow.Signature `json:"tensors"`
	Unsigned    bool                 `json:"unsigned,omitempty"`
	GeneratedBy string               `json:"generatedBy,omitempty"`
	ConvertedBy string               `json:"convertedBy,omitempty"`
}

// modelJSON is a graph model's model.json.
type modelJSON struct {
	Format              string          `json:"format"`
	GeneratedBy         string          `json:"generatedBy,omitempty"`
	ConvertedBy         string          `json:"convertedBy,omitempty"`
	Signature           *signatureJSON  `json:"signature,omitempty"`
	UserDefinedMetadata json.RawMessage `json:"userDefinedMetadata,omitempty"`
	ModelTopology       topology        `json:"modelTopology"`
	WeightsManifest     []weightGroup   `json:"weightsManifest"`
}

type topology struct {
	Node     []nodeJSON     `json:"node"`
	Library  map[string]any `json:"library"`
	Versions map[string]any `json:"versions"`
}

type nodeJSON struct {
	Name  string                     `json:"name"`
	Op    string                     `json:"op"`
	Input []string                   `json:"input,omitempty"`
	Attr  map[string]json.RawMessage `json:"attr,omitempty"`
}

type weightGroup struct {
	Paths   []string     `json:"paths"`
	Weights []weightSpec `json:"weights"`
}

type weightSpec struct {
	Name  string  `json:"name"`
	Shape []int64 `json:"shape"`
	Dtype string  `json:"dtype"`
}

// signatureJSON is a model's signature: its inputs and outputs by their
// keys, in their order (an app's predict gives and gets them in it).
type signatureJSON struct {
	Inputs, Outputs orderedTensors
}

func (s signatureJSON) MarshalJSON() ([]byte, error) {
	in, err := s.Inputs.MarshalJSON()
	if err != nil {
		return nil, err
	}
	out, err := s.Outputs.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(struct {
		Inputs  json.RawMessage `json:"inputs"`
		Outputs json.RawMessage `json:"outputs"`
	}{in, out}); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

func (s *signatureJSON) UnmarshalJSON(b []byte) error {
	var raw struct {
		Inputs  orderedTensors `json:"inputs"`
		Outputs orderedTensors `json:"outputs"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	s.Inputs, s.Outputs = raw.Inputs, raw.Outputs
	return nil
}

// tensorJSON is a signature's TensorInfo, as protobuf's JSON has it.
type tensorJSON struct {
	Name        string    `json:"name"`
	Dtype       string    `json:"dtype"`
	TensorShape shapeJSON `json:"tensorShape"`
}

type shapeJSON struct {
	Dim []struct {
		Size string `json:"size"`
	} `json:"dim,omitempty"`
	UnknownRank bool `json:"unknownRank,omitempty"`
}

// orderedTensors are a JSON object's tensors, by their keys, in the
// object's order.
type orderedTensors struct {
	Keys    []string
	Tensors map[string]tensorJSON
}

func (o *orderedTensors) UnmarshalJSON(b []byte) error {
	o.Tensors = map[string]tensorJSON{}
	d := json.NewDecoder(bytes.NewReader(b))
	if _, err := d.Token(); err != nil {
		return err
	}
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		var t tensorJSON
		if err := d.Decode(&t); err != nil {
			return err
		}
		o.Keys = append(o.Keys, key)
		o.Tensors[key] = t
	}
	return nil
}

func (o orderedTensors) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, _ := json.Marshal(k)
		t, err := json.Marshal(o.Tensors[k])
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(t)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// dataTypes are TensorFlow's DataTypes' names, by their numbers.
var dataTypes = map[int64]string{1: "DT_FLOAT", 2: "DT_DOUBLE", 3: "DT_INT32", 4: "DT_UINT8", 5: "DT_INT16", 6: "DT_INT8", 7: "DT_STRING", 9: "DT_INT64", 10: "DT_BOOL", 17: "DT_UINT16", 19: "DT_HALF"}

// typeOf is the contract's type of a TensorFlow.js tensor of a DataType's
// name: TensorFlow.js has float32, int32 and bool.
func typeOf(name string) (contract.DType, error) {
	switch name {
	case "DT_FLOAT", "DT_HALF", "DT_DOUBLE":
		return "float32", nil
	case "DT_INT32", "DT_INT64", "DT_INT16", "DT_INT8", "DT_UINT8", "DT_UINT16":
		return "int32", nil
	case "DT_BOOL":
		return "bool", nil
	}
	return "", fmt.Errorf("a tensor of %s, which stubs do not have", name)
}

// ReadContract reads the contract of the graph model at path (its
// model.json): its signature's inputs and outputs, by their keys, in their
// order, and the operators of its graph; of a model of no signature, its
// placeholders and the nodes no node takes, by their names, in the graph's
// order, as TensorFlow.js runs them (their outputs' shapes are left for
// `axx models contract --output` to give). An input shaped like an image is
// given its layout.
func ReadContract(path string) (*contract.Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	var m modelJSON
	if err := json.Unmarshal(b, &m); err != nil || len(m.ModelTopology.Node) == 0 {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a TensorFlow.js graph model axx reads: %v", path, err).
			WithHint("a graph model is a model.json of a modelTopology of nodes and a weightsManifest, as tensorflowjs_converter writes it (a layers model's model.json is Keras's layers)")
	}
	c := &contract.Contract{Format: contract.Format{Type: "tfjs"}}
	ops := map[string]bool{}
	for _, n := range m.ModelTopology.Node {
		ops[n.Op] = true
	}
	c.Format.Operators = slices.Sorted(maps.Keys(ops))
	d := details{Signature: tensorflow.Signature{Inputs: map[string]string{}, Outputs: map[string]string{}}, GeneratedBy: m.GeneratedBy, ConvertedBy: m.ConvertedBy}
	if m.Signature == nil {
		d.Unsigned = true
		if c.Inputs, c.Outputs, err = unsigned(m.ModelTopology.Node, d.Signature); err != nil {
			return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %v", path, err)
		}
		for i := range c.Inputs {
			c.Inputs[i].GuessImage()
		}
		if c.Format.Details, err = json.Marshal(d); err != nil {
			return nil, err
		}
		return c, nil
	}
	read := func(o orderedTensors, names map[string]string) ([]contract.Tensor, error) {
		var out []contract.Tensor
		for _, k := range o.Keys {
			tj := o.Tensors[k]
			t := contract.Tensor{Name: k}
			if t.Type, err = typeOf(tj.Dtype); err != nil {
				return nil, fmt.Errorf("its tensor %s is %w", k, err)
			}
			if tj.TensorShape.UnknownRank {
				return nil, fmt.Errorf("its tensor %s is of unknown rank", k)
			}
			for _, dim := range tj.TensorShape.Dim {
				size, err := strconv.ParseInt(dim.Size, 10, 64)
				if err != nil || size < 0 {
					t.Shape = append(t.Shape, contract.Dim{Name: "?"})
				} else {
					t.Shape = append(t.Shape, contract.Dim{Size: size})
				}
			}
			names[k] = tj.Name
			out = append(out, t)
		}
		return out, nil
	}
	if c.Inputs, err = read(m.Signature.Inputs, d.Signature.Inputs); err == nil {
		c.Outputs, err = read(m.Signature.Outputs, d.Signature.Outputs)
	}
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %v", path, err)
	}
	for i := range c.Inputs {
		c.Inputs[i].GuessImage()
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	c.Format.Details = raw
	return c, nil
}

// unsigned are the inputs and outputs of a graph of no signature, as
// TensorFlow.js runs it: its placeholders (of their types and shapes) and
// the nodes no node takes (of their types, where they say them), by their
// names, in the graph's order; sig gets their tensors' names.
func unsigned(nodes []nodeJSON, sig tensorflow.Signature) (inputs, outputs []contract.Tensor, err error) {
	taken := map[string]bool{}
	for _, n := range nodes {
		for _, in := range n.Input {
			name, _, _ := strings.Cut(strings.TrimPrefix(in, "^"), ":")
			taken[name] = true
		}
	}
	attrType := func(n nodeJSON, key string) contract.DType {
		var a struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(n.Attr[key], &a) != nil {
			return ""
		}
		t, _ := typeOf(a.Type)
		return t
	}
	for _, n := range nodes {
		switch {
		case n.Op == "Placeholder":
			t := contract.Tensor{Name: n.Name, Type: attrType(n, "dtype")}
			if t.Type == "" {
				return nil, nil, fmt.Errorf("its placeholder %s is of a type stubs do not have", n.Name)
			}
			var shape struct {
				Shape struct {
					Dim []struct {
						Size string `json:"size"`
					} `json:"dim"`
				} `json:"shape"`
			}
			if err := json.Unmarshal(n.Attr["shape"], &shape); err == nil {
				for _, dim := range shape.Shape.Dim {
					size, err := strconv.ParseInt(dim.Size, 10, 64)
					if err != nil || size < 0 {
						t.Shape = append(t.Shape, contract.Dim{Name: "?"})
					} else {
						t.Shape = append(t.Shape, contract.Dim{Size: size})
					}
				}
			}
			sig.Inputs[n.Name] = n.Name + ":0"
			inputs = append(inputs, t)
		case !taken[n.Name] && n.Op != "NoOp":
			sig.Outputs[n.Name] = n.Name + ":0"
			outputs = append(outputs, contract.Tensor{Name: n.Name, Type: attrType(n, "T")})
		}
	}
	if len(inputs) == 0 || len(outputs) == 0 {
		return nil, nil, errors.New("its graph has no placeholder, or no node no node takes")
	}
	return inputs, outputs, nil
}

// Write writes a stub as a graph model: its model.json, and its weights'
// shard beside it; of an unsigned model, no signature, and no node but its
// outputs that no node takes.
func Write(s *stub.Stub) (format.Model, error) {
	var d details
	if len(s.Contract.Format.Details) > 0 {
		if err := json.Unmarshal(s.Contract.Format.Details, &d); err != nil {
			return format.Model{}, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a TensorFlow.js model's: %v", err)
		}
	}
	for _, t := range append(slices.Clone(s.Contract.Inputs), s.Contract.Outputs...) {
		if t.Type != "float32" && t.Type != "int32" && t.Type != "bool" {
			return format.Model{}, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's tensor %s is %s: TensorFlow.js's tensors are float32, int32 or bool", t.Name, t.Type)
		}
	}
	sig := tensorflow.Named(s.Contract, d.Signature)
	g, err := tensorflow.StubGraph(s, sig)
	if err != nil {
		return format.Model{}, err
	}
	m := modelJSON{
		Format: "graph-model", GeneratedBy: d.GeneratedBy, ConvertedBy: d.ConvertedBy,
		ModelTopology: topology{Library: map[string]any{}, Versions: map[string]any{}},
	}
	if d.Unsigned {
		g.Nodes = taken(g.Nodes, sig)
	} else {
		m.Signature = &signatureJSON{Inputs: signatureOf(s.Contract.Inputs, sig.Inputs), Outputs: signatureOf(s.Contract.Outputs, sig.Outputs)}
	}
	var weights bytes.Buffer
	group := weightGroup{Paths: []string{shard}, Weights: []weightSpec{}}
	for _, n := range g.Nodes {
		nj := nodeJSON{Name: n.Name, Op: n.Op, Input: n.Inputs, Attr: map[string]json.RawMessage{}}
		for k, a := range n.Attrs {
			if a.Kind == tensorflow.AttrTensor {
				t := int32s(a.Tensor)
				group.Weights = append(group.Weights, weightSpec{Name: n.Name, Shape: dimsOf(t.Dims), Dtype: string(t.Type)})
				weights.Write(t.Raw)
				a.Tensor = &tensorflow.Tensor{Type: t.Type, Dims: t.Dims}
			}
			raw, err := attrJSON(a)
			if err != nil {
				return format.Model{}, err
			}
			nj.Attr[k] = raw
		}
		m.ModelTopology.Node = append(m.ModelTopology.Node, nj)
	}
	m.WeightsManifest = []weightGroup{group}
	out, err := json.Marshal(m)
	if err != nil {
		return format.Model{}, err
	}
	return format.Model{File: out, Beside: map[string][]byte{shard: weights.Bytes()}}, nil
}

// taken are the nodes that the signature's outputs' nodes are, or that a
// node takes: of a graph whose outputs TensorFlow.js finds as the nodes no
// node takes.
func taken(nodes []tensorflow.Node, sig tensorflow.Signature) []tensorflow.Node {
	keep := map[string]bool{}
	for _, name := range sig.Outputs {
		node, _, _ := strings.Cut(name, ":")
		keep[node] = true
	}
	byName := map[string]tensorflow.Node{}
	for _, n := range nodes {
		byName[n.Name] = n
	}
	var visit func(name string)
	visit = func(name string) {
		n, ok := byName[name]
		if !ok {
			return
		}
		for _, in := range n.Inputs {
			from, _, _ := strings.Cut(strings.TrimPrefix(in, "^"), ":")
			if !keep[from] {
				keep[from] = true
				visit(from)
			}
		}
	}
	for name := range maps.Clone(keep) {
		visit(name)
	}
	var out []tensorflow.Node
	for _, n := range nodes {
		if keep[n.Name] {
			out = append(out, n)
		}
	}
	return out
}

func dimsOf(dims []int64) []int64 {
	if dims == nil {
		return []int64{}
	}
	return dims
}

// signatureOf is a signature's tensors of the contract's, by their names,
// as the graph names them.
func signatureOf(ts []contract.Tensor, names map[string]string) orderedTensors {
	o := orderedTensors{Tensors: map[string]tensorJSON{}}
	for _, t := range ts {
		tj := tensorJSON{Name: names[t.Name], Dtype: dataTypes[tensorflow.DataTypes[js(t.Type)]]}
		for _, d := range t.Shape {
			size := int64(-1)
			if d.Fixed() {
				size = d.Size
			}
			tj.TensorShape.Dim = append(tj.TensorShape.Dim, struct {
				Size string `json:"size"`
			}{strconv.FormatInt(size, 10)})
		}
		o.Keys = append(o.Keys, t.Name)
		o.Tensors[t.Name] = tj
	}
	return o
}

// js is TensorFlow.js's type of a type: its integers int32, its floats
// float32.
func js(t contract.DType) contract.DType {
	switch {
	case t == "bool":
		return "bool"
	case t.Integer():
		return "int32"
	}
	return "float32"
}

// int32s is a constant as TensorFlow.js has it: of its type, its values
// converted (integers clamped to int32's).
func int32s(t *tensorflow.Tensor) tensorflow.Tensor {
	to := js(t.Type)
	if to == t.Type {
		return *t
	}
	vs := t.Type.Decode(t.Raw)
	for i, v := range vs {
		if to == "int32" {
			vs[i] = math.Max(math.MinInt32, math.Min(math.MaxInt32, v))
		}
	}
	return tensorflow.Tensor{Type: to, Dims: t.Dims, Raw: to.Encode(vs)}
}

// attrJSON is an AttrValue as protobuf's JSON has it, its types
// TensorFlow.js's.
func attrJSON(a tensorflow.Attr) (json.RawMessage, error) {
	typeName := func(t contract.DType) string { return dataTypes[tensorflow.DataTypes[js(t)]] }
	shape := func(dims []int64) map[string]any {
		var ds []map[string]string
		for _, d := range dims {
			ds = append(ds, map[string]string{"size": strconv.FormatInt(d, 10)})
		}
		if ds == nil {
			return map[string]any{}
		}
		return map[string]any{"dim": ds}
	}
	var v any
	switch a.Kind {
	case tensorflow.AttrString:
		v = map[string]string{"s": base64.StdEncoding.EncodeToString([]byte(a.S))}
	case tensorflow.AttrInt:
		v = map[string]string{"i": strconv.FormatInt(a.I, 10)}
	case tensorflow.AttrBool:
		v = map[string]bool{"b": a.B}
	case tensorflow.AttrType:
		v = map[string]string{"type": typeName(a.Type)}
	case tensorflow.AttrTypes:
		types := make([]string, len(a.Types))
		for i, t := range a.Types {
			types[i] = typeName(t)
		}
		v = map[string]any{"list": map[string]any{"type": types}}
	case tensorflow.AttrInts:
		ints := make([]string, len(a.Ints))
		for i, n := range a.Ints {
			ints[i] = strconv.FormatInt(n, 10)
		}
		v = map[string]any{"list": map[string]any{"i": ints}}
	case tensorflow.AttrShape:
		v = map[string]any{"shape": shape(a.Shape)}
	case tensorflow.AttrTensor:
		v = map[string]any{"tensor": map[string]any{"dtype": typeName(a.Tensor.Type), "tensorShape": shape(a.Tensor.Dims)}}
	default:
		return nil, fmt.Errorf("an attribute of kind %d", a.Kind)
	}
	return json.Marshal(v)
}
