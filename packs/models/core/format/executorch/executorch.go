package executorch

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/onnx"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "executorch", Name: "ExecuTorch", Extensions: []string{".pte"}, Contract: ReadContract, Write: format.File(Write), Operators: operators})
}

// operators reads the operators and delegates the ExecuTorch program at
// path calls.
func operators(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	_, ops, err := readProgram(b)
	sort.Strings(ops)
	return ops, err
}

// details are an ExecuTorch program's own settings in its contract: its
// methods, when it has others than forward (React Native ExecuTorch's text
// recognizers' forward_64 to forward_512, a method a width), each of its
// own shapes.
type details struct {
	Methods []method `json:"methods,omitempty"`
	// EndOfGeneration is, of a language model's, the token that ends its
	// turn (a speech recognizer's: its transcript).
	EndOfGeneration int32 `json:"endOfGeneration,omitempty"`
	// NoTimestamps is, of a speech recognizer's, the token that says its
	// transcript has no timestamps; Mels, how many mel filters its
	// features have.
	NoTimestamps int32 `json:"noTimestamps,omitempty"`
	Mels         int   `json:"mels,omitempty"`
}

// ReadContract reads the contract of the ExecuTorch program at path: its
// forward method's inputs and outputs (or its first method's), by their
// places (input0, output0...: an app gives and gets them in order), its
// other methods', and the operators and delegates it calls. An input shaped
// like an image is given its layout.
func ReadContract(path string) (*contract.Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	methods, ops, err := readProgram(b)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not an ExecuTorch program axx reads: %v", path, err).
			WithHint("an ExecuTorch program is a .pte file, as ExecuTorch's exporter writes it (to_executorch)")
	}
	main := methods[0]
	for _, m := range methods {
		if m.Name == "forward" {
			main = m
		}
	}
	c := &contract.Contract{Format: contract.Format{Type: "executorch", Operators: ops}, Inputs: main.Inputs, Outputs: main.Outputs}
	sort.Strings(c.Format.Operators)
	for i := range c.Inputs {
		c.Inputs[i].GuessImage()
	}
	d := details{Methods: methods}
	switch {
	case isSpeechProgram(methods):
		if err := speechContract(c, path, &d); err != nil {
			return nil, err
		}
	case isLanguageModel(methods):
		if err := languageContract(c, path, &d); err != nil {
			return nil, err
		}
	}
	if len(methods) > 1 || main.Name != "forward" {
		raw, err := json.Marshal(d)
		if err != nil {
			return nil, err
		}
		c.Format.Details = raw
	}
	return c, nil
}

// Write writes a stub as an ExecuTorch program: its ONNX stub's graph,
// lowered to the portable kernels' calls, for each of the program's
// methods (its keys and answers of the method's shapes).
func Write(s *stub.Stub) ([]byte, error) {
	var d details
	if len(s.Contract.Format.Details) > 0 {
		if err := json.Unmarshal(s.Contract.Format.Details, &d); err != nil {
			return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not an ExecuTorch program's: %v", err)
		}
	}
	switch {
	case s.Contract.Task == "speech-to-text":
		return writeSpeechProgram(s, d)
	case s.Generated != nil:
		return writeLanguageModel(s, d)
	}
	if len(d.Methods) == 0 {
		d.Methods = []method{{Name: "forward", Inputs: s.Contract.Inputs, Outputs: s.Contract.Outputs}}
	}
	p := &program{}
	for _, m := range d.Methods {
		c := *s.Contract
		c.Format = contract.Format{Type: "onnx", IRVersion: 8, Opsets: map[string]int64{"": onnxOpset}}
		var err error
		if c.Inputs, err = shaped(s.Contract.Inputs, m.Inputs); err != nil {
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: its method %s: %v", m.Name, err)
		}
		if c.Outputs, err = shaped(s.Contract.Outputs, m.Outputs); err != nil {
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: its method %s: %v", m.Name, err)
		}
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
		pl, err := lower(g, &c, p, m.Name)
		if err != nil {
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: its method %s: %v", m.Name, err)
		}
		p.plans = append(p.plans, pl)
	}
	return p.encode(), nil
}

// shaped are the contract's tensors of a method's shapes: each as the
// contract has it (its layout, its labels...), of the method's tensor's
// shape at its place.
func shaped(tensors, of []contract.Tensor) ([]contract.Tensor, error) {
	if len(of) != len(tensors) {
		return nil, fmt.Errorf("it has %d tensors, and the contract %d", len(of), len(tensors))
	}
	out := make([]contract.Tensor, len(tensors))
	for i := range tensors {
		out[i] = tensors[i]
		out[i].Shape = of[i].Shape
	}
	return out, nil
}
