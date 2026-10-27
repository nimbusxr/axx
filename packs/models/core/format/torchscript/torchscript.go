// Package torchscript reads TorchScript modules' contracts (.pt, as
// torch.jit.save writes them: the models Python services load with
// torch.jit.load, C++ services with LibTorch, Triton with its PyTorch
// backend) and writes stubs as TorchScript modules: the ONNX stub's graph
// (package onnx) as a frozen module's code, which TorchScript compiles
// when it loads it, its tables the module's constants.
package torchscript

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	format.Register(format.Format{Type: "torchscript", Name: "TorchScript", Extensions: []string{".pt", ".torchscript"}, Contract: ReadContract, Write: format.File(Write), Operators: operators})
}

// details are a module's own settings in its contract: its class, which
// the stub's is.
type details struct {
	Module string `json:"module"`
	Class  string `json:"class"`
}

// forwardDef is a forward method's definition in a module's code.
var forwardDef = regexp.MustCompile(`(?s)def forward\(self: [^,)]+(.*?)\)\s*->\s*(.+?):\n`)

// ReadContract reads the contract of the TorchScript module at path: its
// forward's tensors (its parameters by their names, its outputs by their
// places: output0, output1...), and the operators its code calls. TorchScript
// does not say its tensors' types and shapes; the contract takes them from
// what the module's folder has: an Ultralytics export's config (its
// pictures' size, its task and labels), or Triton's model configuration
// (config.pbtxt, its inputs' and outputs' types and dims, by their places).
// What none says is left for `axx models contract --input/--output` to give.
func ReadContract(path string) (*contract.Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	fail := func(err error) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a TorchScript module axx reads: %v", path, err).
			WithHint("a TorchScript module is a .pt file torch.jit.save writes (of torch.jit.script or torch.jit.trace); a .pth or a checkpoint (torch.save's) is not")
	}
	a, err := readArchive(b)
	if err != nil {
		return nil, fail(err)
	}
	module, class, err := a.rootClass()
	if err != nil {
		return nil, fail(err)
	}
	inputs, outputs, err := a.forward(module, class)
	if err != nil {
		return nil, fail(err)
	}
	c := &contract.Contract{Format: contract.Format{Type: "torchscript", Operators: a.operators()}}
	for _, name := range inputs {
		c.Inputs = append(c.Inputs, contract.Tensor{Name: name})
	}
	for i := range outputs {
		c.Outputs = append(c.Outputs, contract.Tensor{Name: fmt.Sprintf("output%d", i)})
	}
	if err := ultralytics(c, a.files["extra/config.txt"]); err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %v", path, err)
	}
	for _, dir := range []string{filepath.Dir(path), filepath.Dir(filepath.Dir(path))} {
		if cfg, err := os.ReadFile(filepath.Join(dir, "config.pbtxt")); err == nil {
			if err := triton(c, string(cfg)); err != nil {
				return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: its Triton config (%s): %v", path, filepath.Join(dir, "config.pbtxt"), err)
			}
			break
		}
	}
	for i := range c.Inputs {
		if c.Inputs[i].Type != "" {
			c.Inputs[i].GuessImage()
		}
	}
	raw, err := json.Marshal(details{Module: module, Class: class})
	if err != nil {
		return nil, err
	}
	c.Format.Details = raw
	return c, nil
}

// operators reads the operators the TorchScript module at path calls.
func operators(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	a, err := readArchive(b)
	if err != nil {
		return nil, err
	}
	return a.operators(), nil
}

// forward reads the forward of the module's class: its tensors' parameters'
// names, and its outputs (one, or a tuple's).
func (a *archive) forward(module, class string) (inputs, outputs []string, err error) {
	source, ok := a.files["code/"+strings.ReplaceAll(module, ".", "/")+".py"]
	if !ok {
		return nil, nil, fmt.Errorf("it has no code of its module's class %s.%s", module, class)
	}
	src := string(source)
	at := strings.Index(src, "class "+class+"(")
	if at < 0 {
		return nil, nil, fmt.Errorf("its code has no class %s", class)
	}
	m := forwardDef.FindStringSubmatch(src[at:])
	if m == nil {
		return nil, nil, fmt.Errorf("its class %s has no forward", class)
	}
	for _, p := range splitTop(m[1]) {
		name, typ, _ := strings.Cut(p, ":")
		name, typ = strings.TrimSpace(name), strings.TrimSpace(typ)
		if t, _, _ := strings.Cut(typ, "="); strings.TrimSpace(t) == "Tensor" {
			inputs = append(inputs, name)
		}
	}
	ret := strings.TrimSpace(m[2])
	switch {
	case ret == "Tensor":
		outputs = []string{ret}
	case strings.HasPrefix(ret, "Tuple[") && strings.HasSuffix(ret, "]"):
		for _, t := range splitTop(ret[len("Tuple[") : len(ret)-1]) {
			if t != "Tensor" {
				return nil, nil, fmt.Errorf("its forward gives a %s: stubs give tensors", ret)
			}
			outputs = append(outputs, t)
		}
	default:
		return nil, nil, fmt.Errorf("its forward gives a %s: stubs give a tensor, or a tuple of them", ret)
	}
	if len(inputs) == 0 {
		return nil, nil, fmt.Errorf("its forward takes no tensor")
	}
	return inputs, outputs, nil
}

// splitTop splits a list of parameters or types at its commas outside
// brackets.
func splitTop(s string) []string {
	var out []string
	depth, from := 0, 0
	for i, r := range s {
		switch r {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case ',':
			if depth == 0 {
				if p := strings.TrimSpace(s[from:i]); p != "" {
					out = append(out, p)
				}
				from = i + 1
			}
		}
	}
	if p := strings.TrimSpace(s[from:]); p != "" {
		out = append(out, p)
	}
	return out
}

// calls are the calls of operators in a module's code: torch.op(, ops.ns.op(.
var calls = regexp.MustCompile(`\b(?:torch\.([a-z_][a-z0-9_]*)|ops\.([a-z_][a-z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*))\(`)

// operators are the operators the module's code calls.
func (a *archive) operators() []string {
	seen := map[string]bool{}
	for path, src := range a.files {
		if !strings.HasPrefix(path, "code/") {
			continue
		}
		for _, m := range calls.FindAllStringSubmatch(string(src), -1) {
			if m[1] != "" {
				seen["aten::"+m[1]] = true
			} else {
				seen[m[2]+"::"+m[3]] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// ultralytics gives the contract what an Ultralytics export's config says
// (its extra file config.txt): its input's pictures, and of a detector's or
// a classifier's, its task, its output and its labels.
func ultralytics(c *contract.Contract, cfg []byte) error {
	if cfg == nil {
		return nil
	}
	var u struct {
		Task     string            `json:"task"`
		Batch    int64             `json:"batch"`
		ImgSz    []int64           `json:"imgsz"`
		Names    map[string]string `json:"names"`
		Channels int64             `json:"channels"`
		Args     struct {
			Half bool `json:"half"`
		} `json:"args"`
	}
	parsed := json.Unmarshal(cfg, &u) == nil
	if !parsed || len(u.ImgSz) != 2 || len(c.Inputs) != 1 {
		return nil // not an Ultralytics export's config
	}
	batch, channels := max(u.Batch, 1), u.Channels
	if channels == 0 {
		channels = 3
	}
	typ := contract.DType("float32")
	if u.Args.Half {
		typ = "float16"
	}
	h, w := u.ImgSz[0], u.ImgSz[1]
	c.Inputs[0].Type = typ
	c.Inputs[0].Shape = []contract.Dim{{Size: batch}, {Size: channels}, {Size: h}, {Size: w}}
	labels := make([]string, len(u.Names))
	for k, name := range u.Names {
		i, err := strconv.Atoi(k)
		if err != nil || i < 0 || i >= len(labels) {
			return fmt.Errorf("its Ultralytics config names a class %q of %d", k, len(labels))
		}
		labels[i] = name
	}
	switch {
	case u.Task == "detect" && len(c.Outputs) == 1:
		// YOLO's anchors: of each stride's cells, a box and each class's
		// score.
		var anchors int64
		for _, s := range []int64{8, 16, 32} {
			anchors += (h / s) * (w / s)
		}
		c.Task = "detection"
		c.Outputs[0].Type, c.Outputs[0].Labels, c.Outputs[0].Detection = typ, labels, "anchors"
		c.Outputs[0].Shape = []contract.Dim{{Size: batch}, {Size: 4 + int64(len(labels))}, {Size: anchors}}
	case u.Task == "classify" && len(c.Outputs) == 1:
		c.Task = "classification"
		c.Outputs[0].Type, c.Outputs[0].Labels = typ, labels
		c.Outputs[0].Shape = []contract.Dim{{Size: batch}, {Size: int64(len(labels))}}
	}
	return nil
}

// tritonTypes are Triton's data types of the contract's.
var tritonTypes = map[string]contract.DType{
	"TYPE_FP32": "float32", "TYPE_FP16": "float16", "TYPE_FP64": "float64", "TYPE_INT64": "int64", "TYPE_INT32": "int32",
	"TYPE_INT16": "int16", "TYPE_INT8": "int8", "TYPE_UINT8": "uint8", "TYPE_BOOL": "bool",
}

// tritonTensor is a Triton config's input or output: name, data_type and
// dims, in a block.
var (
	tritonBlock    = regexp.MustCompile(`(?s)\b(input|output)\s*(?::\s*)?\[?\s*\{(.*?)\}`)
	tritonDataType = regexp.MustCompile(`data_type\s*:\s*(TYPE_[A-Z0-9]+)`)
	tritonDims     = regexp.MustCompile(`dims\s*:\s*\[([^\]]*)\]`)
	tritonBatch    = regexp.MustCompile(`max_batch_size\s*:\s*(\d+)`)
)

// triton gives the contract the types and dims of Triton's model
// configuration (its PyTorch backend gives and gets a module's tensors in
// their order: input__0, output__0...), a batch before them when it batches.
func triton(c *contract.Contract, cfg string) error {
	batched := false
	if m := tritonBatch.FindStringSubmatch(cfg); m != nil && m[1] != "0" {
		batched = true
	}
	var ins, outs []contract.Tensor
	for _, m := range tritonBlock.FindAllStringSubmatch(cfg, -1) {
		// A block may be a list of tensors: { ... }, { ... }.
		for _, body := range strings.Split(m[2], "},") {
			t := contract.Tensor{}
			dt := tritonDataType.FindStringSubmatch(body)
			dims := tritonDims.FindStringSubmatch(body)
			if dt == nil || dims == nil {
				continue
			}
			typ, ok := tritonTypes[dt[1]]
			if !ok {
				return fmt.Errorf("a tensor of %s, which stubs do not have", dt[1])
			}
			t.Type = typ
			if batched {
				t.Shape = append(t.Shape, contract.Dim{Name: "?"})
			}
			for _, d := range strings.Split(dims[1], ",") {
				n, err := strconv.ParseInt(strings.TrimSpace(d), 10, 64)
				if err != nil {
					return fmt.Errorf("its dims %q", dims[1])
				}
				if n < 0 {
					t.Shape = append(t.Shape, contract.Dim{Name: "?"})
				} else {
					t.Shape = append(t.Shape, contract.Dim{Size: n})
				}
			}
			if m[1] == "input" {
				ins = append(ins, t)
			} else {
				outs = append(outs, t)
			}
		}
	}
	if len(ins) != len(c.Inputs) || len(outs) != len(c.Outputs) {
		return fmt.Errorf("it has %d inputs and %d outputs, and the module's forward %d and %d", len(ins), len(outs), len(c.Inputs), len(c.Outputs))
	}
	for i := range ins {
		c.Inputs[i].Type, c.Inputs[i].Shape = ins[i].Type, ins[i].Shape
	}
	for i := range outs {
		c.Outputs[i].Type, c.Outputs[i].Shape = outs[i].Type, outs[i].Shape
	}
	return nil
}

// Write writes a stub as a TorchScript module of the real one's class: its
// forward the ONNX stub's graph, as TorchScript's code, of the contract's
// inputs, giving its outputs.
func Write(s *stub.Stub) ([]byte, error) { return write(s, false) }

// WriteMobile writes a stub as a PyTorch Mobile module (.ptl): the
// TorchScript module, and its forward's bytecode, which PyTorch Mobile's lite
// interpreter runs.
func WriteMobile(s *stub.Stub) ([]byte, error) { return write(s, true) }

func write(s *stub.Stub, lite bool) ([]byte, error) {
	if s.Generated != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: TorchScript's stubs answer, and do not generate text").
			WithHint("a language model's stub is written for its runtime's format: GGUF, ONNX, safetensors, ExecuTorch, LiteRT-LM")
	}
	d := details{Module: "__torch__", Class: "Stub"}
	if len(s.Contract.Format.Details) > 0 {
		if err := json.Unmarshal(s.Contract.Format.Details, &d); err != nil {
			return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a TorchScript module's: %v", err)
		}
	}
	c := *s.Contract
	c.Format = contract.Format{Type: "onnx", IRVersion: 8, Opsets: map[string]int64{"": onnxOpset}}
	ms, err := s.For(&c)
	if err != nil {
		return nil, err
	}
	model, err := onnx.Write(ms)
	if err != nil {
		return nil, err
	}
	g, err := onnx.ReadGraph(model)
	if err != nil {
		return nil, err
	}
	body, constants, err := translate(g, &c)
	if err != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	params := []string{fmt.Sprintf("self: %s.%s", d.Module, d.Class)}
	for _, in := range c.Inputs {
		params = append(params, in.Name+": Tensor")
	}
	ret := "Tensor"
	if len(c.Outputs) > 1 {
		ret = "Tuple[" + strings.TrimSuffix(strings.Repeat("Tensor, ", len(c.Outputs)), ", ") + "]"
	}
	source := fmt.Sprintf("class %s(Module):\n  __parameters__ = []\n  __buffers__ = []\n  def forward(%s) -> %s:\n%s",
		d.Class, strings.Join(params, ",\n    "), ret, body)
	var bytecode []byte
	if lite {
		names := make([]string, len(c.Inputs))
		for i, in := range c.Inputs {
			names[i] = in.Name
		}
		if bytecode, err = compileLite(body, constants, names, d.Module+"."+d.Class, len(c.Outputs)); err != nil {
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: its bytecode: %v", err)
		}
	}
	return writeArchive("stub", d.Module, d.Class, source, constants, bytecode)
}
