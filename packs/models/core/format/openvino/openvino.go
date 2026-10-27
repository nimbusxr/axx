// Package openvino reads OpenVINO models' contracts and writes stubs as
// OpenVINO models (IR: a graph's XML, its weights' .bin beside it), the
// models Intel's runtime runs in desktop apps, their plugins, and services.
// A stub's graph is its ONNX stub's (package onnx), each of its operators
// as OpenVINO's operation sets have it: OpenVINO infers its ports' shapes
// as it reads the model.
package openvino

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/onnx"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "openvino", Name: "OpenVINO", Extensions: []string{".xml"}, Contract: ReadContract, Write: write})
}

// IR is an OpenVINO model's XML: its layers, and the edges between their
// ports.
type IR struct {
	XMLName xml.Name `xml:"net"`
	Name    string   `xml:"name,attr"`
	Version int      `xml:"version,attr"`
	Layers  []Layer  `xml:"layers>layer"`
	Edges   []Edge   `xml:"edges>edge"`
}

// Layer is an operation: its id, name, type and operation set, its
// attributes (data), and its input and output ports.
type Layer struct {
	ID      int    `xml:"id,attr"`
	Name    string `xml:"name,attr"`
	Type    string `xml:"type,attr"`
	Version string `xml:"version,attr"`
	Data    *Data  `xml:"data"`
	Inputs  []Port `xml:"input>port"`
	Outputs []Port `xml:"output>port"`
}

// Data are a layer's attributes.
type Data struct {
	Attrs []xml.Attr `xml:",any,attr"`
}

func (d *Data) attr(name string) string {
	if d == nil {
		return ""
	}
	for _, a := range d.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// Port is a layer's port: its id, its element type (outputs'), its
// tensor's names, and its dimensions.
type Port struct {
	ID        int      `xml:"id,attr"`
	Precision string   `xml:"precision,attr,omitempty"`
	Names     string   `xml:"names,attr,omitempty"`
	Dims      []string `xml:"dim"`
}

// Edge joins a layer's output port to another's input port.
type Edge struct {
	FromLayer int `xml:"from-layer,attr"`
	FromPort  int `xml:"from-port,attr"`
	ToLayer   int `xml:"to-layer,attr"`
	ToPort    int `xml:"to-port,attr"`
}

// elementTypes are OpenVINO's element types of the contract's types, and
// their ports' precisions.
var elementTypes = map[contract.DType][2]string{
	"float32": {"f32", "FP32"}, "float16": {"f16", "FP16"}, "float64": {"f64", "FP64"},
	"int64": {"i64", "I64"}, "int32": {"i32", "I32"}, "int16": {"i16", "I16"}, "int8": {"i8", "I8"},
	"uint64": {"u64", "U64"}, "uint32": {"u32", "U32"}, "uint16": {"u16", "U16"}, "uint8": {"u8", "U8"},
	"bool": {"boolean", "BOOL"},
}

func typeOf(s string) (contract.DType, bool) {
	s = strings.ToLower(s)
	for t, names := range elementTypes {
		if s == names[0] || s == strings.ToLower(names[1]) {
			return t, true
		}
	}
	return "", false
}

// ReadContract reads the contract of the OpenVINO model at path (its XML):
// its inputs (its Parameters), its outputs (its Results, by their
// tensors' names), its IR version and the operations it uses. An input
// shaped like an image is given its layout.
func ReadContract(path string) (*contract.Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	var ir IR
	if err := xml.Unmarshal(b, &ir); err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not an OpenVINO model axx reads: %v", path, err).
			WithHint("an OpenVINO model is its IR's XML (model.xml), its weights' .bin beside it")
	}
	c, err := contractOf(&ir)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not an OpenVINO model axx reads: %v", path, err)
	}
	if cfg, err := os.ReadFile(filepath.Join(filepath.Dir(path), "config.json")); err == nil {
		applyConfig(c, cfg)
	}
	return c, nil
}

// applyConfig gives a contract what an OpenVINO model's config.json (Open
// Model Zoo's, beside its XML) says: a classifier's labels, and an image
// input of its channels reversed (BGR).
func applyConfig(c *contract.Contract, cfg []byte) {
	var conf struct {
		Labels  json.RawMessage `json:"labels"`
		Reverse json.RawMessage `json:"reverse_input_channels"`
	}
	if json.Unmarshal(cfg, &conf) != nil {
		return
	}
	// Its labels: a list, or a text of them between spaces.
	var labels []string
	if json.Unmarshal(conf.Labels, &labels) != nil {
		var text string
		_ = json.Unmarshal(conf.Labels, &text)
		labels = strings.Fields(text)
	}
	// Reversed: true, or a text of it ("True", as Open Model Zoo writes).
	if r := strings.Trim(strings.ToLower(string(conf.Reverse)), `"`); r == "true" {
		for i := range c.Inputs {
			if c.Inputs[i].Image != nil {
				c.Inputs[i].Image.Channels = "BGR"
			}
		}
	}
	if len(c.Outputs) == 1 && c.Task == "" {
		out := &c.Outputs[0]
		if n := len(out.Shape); n > 0 && out.Shape[n-1].Fixed() && int(out.Shape[n-1].Size) == len(labels) && len(labels) > 0 {
			out.Labels, c.Task = labels, "classification"
		}
	}
}

func contractOf(ir *IR) (*contract.Contract, error) {
	if ir.Version < 10 {
		return nil, fmt.Errorf("its IR is of version %d: axx reads IR 10 and later", ir.Version)
	}
	c := &contract.Contract{Format: contract.Format{Type: "openvino", Version: int64(ir.Version)}}
	byID := map[int]*Layer{}
	ops := map[string]bool{}
	for i := range ir.Layers {
		l := &ir.Layers[i]
		byID[l.ID] = l
		switch l.Type {
		case "Parameter":
			t, ok := typeOf(l.Data.attr("element_type"))
			if !ok {
				return nil, fmt.Errorf("its input %s is of %s", l.Name, l.Data.attr("element_type"))
			}
			in := contract.Tensor{Name: l.Name, Type: t, Shape: shapeOf(l.Data.attr("shape"))}
			if len(l.Outputs) > 0 && l.Outputs[0].Names != "" {
				in.Name = strings.Split(l.Outputs[0].Names, ",")[0]
			}
			in.GuessImage()
			c.Inputs = append(c.Inputs, in)
		case "Const", "Result":
		default:
			ops[l.Type] = true
		}
	}
	for _, l := range ir.Layers {
		if l.Type != "Result" || len(l.Inputs) == 0 {
			continue
		}
		i := slices.IndexFunc(ir.Edges, func(e Edge) bool { return e.ToLayer == l.ID })
		if i < 0 {
			return nil, fmt.Errorf("its output %s has no edge to it", l.Name)
		}
		from := byID[ir.Edges[i].FromLayer]
		if from == nil {
			return nil, fmt.Errorf("its output %s comes from no layer", l.Name)
		}
		pi := slices.IndexFunc(from.Outputs, func(p Port) bool { return p.ID == ir.Edges[i].FromPort })
		if pi < 0 {
			return nil, fmt.Errorf("its output %s comes from no port", l.Name)
		}
		port := from.Outputs[pi]
		t, ok := typeOf(port.Precision)
		if !ok {
			return nil, fmt.Errorf("its output %s is of %s", l.Name, port.Precision)
		}
		out := contract.Tensor{Name: from.Name, Type: t, Shape: shapeOf(strings.Join(l.Inputs[0].Dims, ","))}
		if port.Names != "" {
			out.Name = strings.Split(port.Names, ",")[0]
		}
		c.Outputs = append(c.Outputs, out)
	}
	if len(c.Inputs) == 0 || len(c.Outputs) == 0 {
		return nil, errors.New("it has no inputs (Parameter) or no outputs (Result)")
	}
	for op := range ops {
		c.Format.Operators = append(c.Format.Operators, op)
	}
	sort.Strings(c.Format.Operators)
	return c, nil
}

// shapeOf is a shape as IR writes it: sizes, and -1 or ? (or a range) for
// a dimension of any size.
func shapeOf(s string) []contract.Dim {
	s = strings.Trim(strings.TrimSpace(s), "[]")
	if s == "" {
		return nil
	}
	var out []contract.Dim
	for _, d := range strings.Split(s, ",") {
		n, err := strconv.ParseInt(strings.TrimSpace(d), 10, 64)
		if err != nil || n < 0 {
			out = append(out, contract.Dim{Name: "?"})
			continue
		}
		out = append(out, contract.Dim{Size: n})
	}
	return out
}

// write writes a stub as an OpenVINO model: its ONNX stub's graph,
// translated, its weights the .bin beside the XML.
func write(s *stub.Stub) (format.Model, error) {
	if s.Generated != nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: OpenVINO's stubs answer, and do not generate text yet").
			WithHint("a language model's stub is written for its runtime's format: GGUF, ONNX, LiteRT-LM")
	}
	c := *s.Contract
	c.Format = contract.Format{Type: "onnx", IRVersion: 8, Opsets: map[string]int64{"": onnxOpset}}
	as := *s
	as.Contract = &c
	model, err := onnx.Write(&as)
	if err != nil {
		return format.Model{}, err
	}
	g, err := onnx.ReadGraph(model)
	if err != nil {
		return format.Model{}, err
	}
	xmlFile, bin, err := translate(g, &c)
	if err != nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	return format.Model{File: xmlFile, Beside: map[string][]byte{".bin": bin}}, nil
}
