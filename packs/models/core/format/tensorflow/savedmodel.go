// Package tensorflow reads TensorFlow SavedModels' contracts
// (saved_model.pb: the models TF Serving and Python services load) and
// writes stubs as SavedModels: the ONNX stub's graph (package onnx)
// translated into TensorFlow's operations, a graph of constants with no
// variables, whose signature is the real model's, its inputs and outputs
// the real model's tensors' names too. Its graph is TensorFlow.js's too
// (package tfjs), with protobufs' wire format and no generated code.
package tensorflow

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/onnx"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "savedmodel", Name: "TensorFlow SavedModel", Extensions: []string{".pb"}, Folders: []string{"saved_model.pb"}, Contract: ReadContract, Write: format.File(Write)})
}

// details are a SavedModel's own settings in its contract: its meta
// graph's tags, the key of its signature and the names of its tensors in
// its graph, and the versions of TensorFlow it was saved by.
type details struct {
	Tags              []string  `json:"tags"`
	SignatureKey      string    `json:"signature"`
	Signature         Signature `json:"tensors"`
	TensorflowVersion string    `json:"tensorflowVersion,omitempty"`
	Producer          int64     `json:"producer,omitempty"`
}

// defaultSignature is the signature a SavedModel is served by.
const defaultSignature = "serving_default"

// ReadContract reads the contract of the SavedModel at path (its
// saved_model.pb, or the folder of it): its meta graph for serving's, and
// its signature's (serving_default's, or its first's) inputs and outputs,
// by their keys, which an app gives and gets them by. An input shaped like
// an image is given its layout.
func ReadContract(path string) (*contract.Contract, error) {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		path = filepath.Join(path, "saved_model.pb")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	m, err := readSavedModel(b)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a SavedModel axx reads: %v", path, err).
			WithHint("a SavedModel is a folder of saved_model.pb (and its variables), as tf.saved_model.save and Keras's model.export write it")
	}
	keys := slices.Sorted(maps.Keys(m.signatures))
	if len(keys) == 0 {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s has no signature: an app calls a SavedModel by one", path)
	}
	key := keys[0]
	if _, ok := m.signatures[defaultSignature]; ok {
		key = defaultSignature
	}
	sig := m.signatures[key]
	if sig.err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: its signature %s: %v", path, key, sig.err)
	}
	c := &contract.Contract{Format: contract.Format{Type: "savedmodel", Operators: m.ops}}
	for _, k := range slices.Sorted(maps.Keys(sig.inputs)) {
		t := sig.inputs[k]
		t.GuessImage()
		c.Inputs = append(c.Inputs, t)
	}
	for _, k := range slices.Sorted(maps.Keys(sig.outputs)) {
		c.Outputs = append(c.Outputs, sig.outputs[k])
	}
	raw, err := json.Marshal(details{Tags: m.tags, SignatureKey: key, Signature: sig.names, TensorflowVersion: m.tensorflowVersion, Producer: m.producer})
	if err != nil {
		return nil, err
	}
	c.Format.Details = raw
	return c, nil
}

// Write writes a stub as a SavedModel's saved_model.pb: its graph the ONNX
// stub's, translated, of the real model's tags, signature and tensors'
// names.
func Write(s *stub.Stub) ([]byte, error) {
	d, err := detailsOf(s.Contract)
	if err != nil {
		return nil, err
	}
	g, err := StubGraph(s, d.Signature)
	if err != nil {
		return nil, err
	}
	return encodeSavedModel(EncodeGraph(g, d.Producer, defaultMinConsumer), d, s.Contract), nil
}

// detailsOf are the contract's SavedModel's details: a contract's of no
// details (written by hand) is served by serving_default, its tensors
// named as it names them.
func detailsOf(c *contract.Contract) (*details, error) {
	d := &details{}
	if len(c.Format.Details) > 0 {
		if err := json.Unmarshal(c.Format.Details, d); err != nil {
			return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a SavedModel's: %v", err)
		}
	}
	if len(d.Tags) == 0 {
		d.Tags = []string{"serve"}
	}
	if d.SignatureKey == "" {
		d.SignatureKey = defaultSignature
	}
	if d.Producer == 0 {
		d.Producer = defaultProducer
	}
	d.Signature = Named(c, d.Signature)
	return d, nil
}

// Named is the signature's names of a contract's tensors, and of those it
// does not name, their names as nodes' (":0").
func Named(c *contract.Contract, sig Signature) Signature {
	out := Signature{Inputs: map[string]string{}, Outputs: map[string]string{}}
	name := func(given map[string]string, t string) string {
		if n, ok := given[t]; ok {
			return n
		}
		return strings.NewReplacer(" ", "_", ":", "_").Replace(t) + ":0"
	}
	for _, in := range c.Inputs {
		out.Inputs[in.Name] = name(sig.Inputs, in.Name)
	}
	for _, o := range c.Outputs {
		out.Outputs[o.Name] = name(sig.Outputs, o.Name)
	}
	return out
}

// StubGraph is the TensorFlow graph of a stub: its ONNX stub's graph,
// translated, its inputs and outputs named as the signature names them.
func StubGraph(s *stub.Stub, sig Signature) (*Graph, error) {
	if s.Generated != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: TensorFlow's stubs answer, and do not generate text").
			WithHint("a language model's stub is written for its runtime's format: GGUF, ONNX, LiteRT-LM, ExecuTorch")
	}
	c := *s.Contract
	c.Format = contract.Format{Type: "onnx", IRVersion: 8, Opsets: map[string]int64{"": OnnxOpset}}
	ms, err := s.For(&c)
	if err != nil {
		return nil, err
	}
	model, err := onnx.Write(ms)
	if err != nil {
		return nil, err
	}
	og, err := onnx.ReadGraph(model)
	if err != nil {
		return nil, err
	}
	g, err := Translate(og, &c, sig)
	if err != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	return g, nil
}
