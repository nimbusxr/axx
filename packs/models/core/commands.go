package modelscore

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/task"
	"github.com/nimbusxr/axx/packs/models/core/tokenize"
)

func modelsCommand() core.Command {
	return core.Command{
		Name:  "models",
		Short: "Stub embedded models: models whose answers are their mappings",
		Long: `Stub the models apps and services run themselves. A stub's directory has its
model's contract (contract.json, which ` + "`axx models contract`" + ` writes from the real
model) and its mappings (mappings/*.json); ` + "`axx models build`" + ` writes the stub, a
model the app's test build or test deployment puts where the app gets the real
one.`,
		Commands: []core.Command{contractCommand(), buildCommand()},
	}
}

func contractCommand() core.Command {
	return core.Command{
		Name: "contract", Args: "<model or API>", MinArgs: 1, MaxArgs: 1,
		Short: "Write a model's contract, what a stub of it keeps to",
		Long: `Read a model and write its contract: its inputs and outputs, its format's
version and its metadata, what an app relies on from the model. A stub keeps to
it, so the app's runtime loads the stub as it loads the model. An input shaped
like an image gets an image layout: check it, and its channels' order (RGB or
BGR) and resizing (center-crop or stretch). A classifier's contract gives its
task and its labels (--task classification --labels labels.txt).

A model the platform gives the app (iOS's Vision, Android's ML Kit, a
browser's Prompt API) has no file to read: name its API instead, and the
contract is the API's (axx models contract ios.vision.text). A labeler's
contract takes the labels its mappings answer (--labels labels.txt).

The project keeps the contract in the stub's directory (contract.json): the
stub is built from it, and no test needs the real model. Models: ` + strings.Join(formatsAndExtensions(), ", ") + `.` + apisList(),
		Flags: []core.Flag{
			{Name: "out", Short: "o", Usage: "the contract's file, like models/photo-check/contract.json (default: print it)"},
			{Name: "task", Usage: "what the model's outputs mean: " + strings.Join(task.Names(), ", ")},
			{Name: "labels", Usage: "a file of a classifier's labels, one a line, in the order of their scores (a text recognizer's: the characters it reads, its blank an empty line)"},
			{Name: "vocab", Usage: "a file of a text model's WordPiece vocabulary (BERT's vocab.txt), which the contract carries: its input of tokens' ids is matched by text"},
			{Name: "input", Usage: "an input's type and shape, which the model does not say (a TorchScript module's) or which the app gives otherwise: " + tensorSyntax, Repeated: true},
			{Name: "output", Usage: "an output's type and shape, as --input gives an input's", Repeated: true},
		},
		Run: writeContract,
	}
}

func buildCommand() core.Command {
	return core.Command{
		Name: "build", Args: "[stub]", MinArgs: 0, MaxArgs: 1,
		Short: "Build a stub, a model of its contract that answers its mappings",
		Long: `Build the stub in the directory <stub>, from its contract (contract.json) and
its mappings (mappings/*.json), as a model of the contract's format: the file the
app's test build or test deployment puts where the app gets the real model. Keep
it with the app's sources: it is built the same, bit for bit, on every machine.
A mapping that cannot be used stops the build, named.`,
		Flags: []core.Flag{
			{Name: "out", Short: "o", Usage: "the stub's file, like app/src/debug/assets/photo-check.tflite (required)"},
			{Name: "contract", Usage: "the contract file (with --mappings, instead of a stub directory)"},
			{Name: "mappings", Usage: "the mappings directory (with --contract)"},
		},
		Run: build,
	}
}

func formatsAndExtensions() []string {
	var out []string
	for _, t := range format.Types() {
		f, _ := format.ByType(t)
		files := slices.Clone(f.Extensions)
		for _, name := range f.Folders {
			files = append(files, "a folder of "+name)
		}
		out = append(out, fmt.Sprintf("%s (%s)", t, strings.Join(files, ", ")))
	}
	return out
}

// apisList names the platform APIs the project's packs answer, for the
// contract command's help.
func apisList() string {
	var names []string
	for _, a := range platform.APIs() {
		names = append(names, a.Name)
	}
	if len(names) == 0 {
		return ""
	}
	return " APIs: " + strings.Join(names, ", ") + "."
}

func writeContract(call *core.CommandCall) error {
	path := call.Args[0]
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if api, ok := platform.APIByName(path); ok {
			return writeAPIContract(call, api)
		}
	}
	f, ok := format.ByFile(path)
	if !ok {
		hint := "axx stubs models of these formats: " + strings.Join(formatsAndExtensions(), ", ")
		if l := apisList(); l != "" {
			hint += "; and answers these platform" + l
		}
		return axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a model of a format axx stubs, nor a platform API", path).
			WithHint("%s", hint)
	}
	if f.Contract == nil {
		return axxerr.New(format.CodeModel, exitcode.Usage, "axx reads no contracts of %s models", formatName(f.Type)).
			WithHint("write the model's contract yourself: the stubs guide shows what it takes")
	}
	c, err := f.Contract(path)
	if err != nil {
		return err
	}
	if err := giveTensors(c, call.Lists["input"], call.Lists["output"]); err != nil {
		return err
	}
	if missing := untyped(c); len(missing) > 0 {
		return axxerr.New(contract.CodeContract, exitcode.Usage, "%s does not say the types and shapes of its tensors: give each, as the app gives and gets them", path).
			WithHint("axx models contract %s %s", path, strings.Join(missing, " "))
	}
	if t := call.Flags["task"]; t != "" {
		c.Task = t
	}
	if file := call.Flags["vocab"]; file != "" {
		if err := giveVocabulary(c, file); err != nil {
			return err
		}
	}
	if c.Task == "detection" {
		var labels []string
		if file := call.Flags["labels"]; file != "" {
			if labels, err = readLabels(file); err != nil {
				return err
			}
		}
		if err := task.DetectionRoles(c, labels); err != nil {
			return axxerr.New(contract.CodeContract, exitcode.Usage, "the model's contract cannot be used: %v", err).
				WithHint("a detector's contract gives each output's detection (boxes, classes, scores, count; or anchors) and its labels (--labels labels.txt)")
		}
	} else if file := call.Flags["labels"]; file != "" {
		if err := giveLabels(c, file); err != nil {
			return err
		}
	}
	if err := errors.Join(c.Check(), task.Check(c)); err != nil {
		return axxerr.New(contract.CodeContract, exitcode.Usage, "the model's contract cannot be used: %v", err).
			WithHint("%s", contractHint(c))
	}
	if call.Flags["out"] == "" && c.Unsaved() {
		return axxerr.New("AXX-E0001", exitcode.Usage, "--out is required: the model carries files (%s), which go beside its contract", strings.Join(slices.Sorted(maps.Keys(c.Files)), ", ")).
			WithHint("axx models contract %s -o models/<stub>/contract.json", path)
	}
	return emitContract(call, c, describeContract(c), func(w io.Writer) error {
		for _, in := range c.Inputs {
			if in.Image != nil && in.Image.Resize == "" {
				if _, err := fmt.Fprintf(w, "  input %s looks like an image (%s): check its channels (RGB or BGR), resizing (center-crop or stretch) and crop (torchvision's evaluation transform keeps 0.875)\n", in.Name, in.Image.Layout); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// writeAPIContract writes a platform API's contract: the platform gives the
// app its model, so the contract is the API's, the same for every app.
func writeAPIContract(call *core.CommandCall, api platform.API) error {
	for _, flag := range []string{"task", "vocab"} {
		if call.Flags[flag] != "" {
			return axxerr.New("AXX-E0001", exitcode.Usage, "--%s does not apply to a platform API: its contract is the API's", flag).
				WithHint("axx models contract %s -o models/<stub>/contract.json", api.Name)
		}
	}
	if len(call.Lists["input"])+len(call.Lists["output"]) > 0 {
		return axxerr.New("AXX-E0001", exitcode.Usage, "--input and --output do not apply to a platform API: its contract is the API's").
			WithHint("axx models contract %s -o models/<stub>/contract.json", api.Name)
	}
	var labels []string
	if file := call.Flags["labels"]; file != "" {
		var err error
		if labels, err = readLabels(file); err != nil {
			return err
		}
	}
	c, err := api.Contract(labels)
	if err == nil {
		err = errors.Join(c.Check(), task.Check(c))
	}
	if err != nil {
		return axxerr.New(contract.CodeContract, exitcode.Usage, "the %s API's contract cannot be written: %v", api.Name, err).
			WithHint("axx models contract %s --labels labels.txt -o models/<stub>/contract.json", api.Name)
	}
	return emitContract(call, c, fmt.Sprintf("the %s API's contract (%s), task %s", api.Name, api.Calls, c.Task), nil)
}

// emitContract saves the contract to --out, or prints it: what was written,
// then what more to say of it.
func emitContract(call *core.CommandCall, c *contract.Contract, what string, more func(io.Writer) error) error {
	out := call.Flags["out"]
	if out != "" {
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := c.Save(out); err != nil {
			return err
		}
	}
	return call.Emit(c, func(w io.Writer) error {
		if out == "" {
			b, err := c.JSON()
			if err != nil {
				return err
			}
			_, err = w.Write(b)
			return err
		}
		if _, err := fmt.Fprintf(w, "wrote %s: %s\n", out, what); err != nil {
			return err
		}
		if more != nil {
			return more(w)
		}
		return nil
	})
}

func describeContract(c *contract.Contract) string {
	names := func(ts []contract.Tensor) string {
		var out []string
		for _, t := range ts {
			dims := make([]string, len(t.Shape))
			for i, d := range t.Shape {
				dims[i] = d.String()
			}
			out = append(out, fmt.Sprintf("%s %s[%s]", t.Name, t.Type, strings.Join(dims, ",")))
		}
		return strings.Join(out, ", ")
	}
	return fmt.Sprintf("%s model; inputs %s; outputs %s", article(formatName(c.Format.Type)), names(c.Inputs), names(c.Outputs))
}

// formatName is a format's name, as people write it.
func formatName(t string) string {
	if f, ok := format.ByType(t); ok && f.Name != "" {
		return f.Name
	}
	return t
}

// article puts "a" or "an" before a word, by its first letter.
func article(word string) string {
	if word != "" && strings.ContainsRune("AEIOUaeiou", rune(word[0])) {
		return "an " + word
	}
	return "a " + word
}

// giveVocabulary has the contract carry a WordPiece vocabulary, and say its
// input of tokens' ids: input_ids, or the first input of integers.
func giveVocabulary(c *contract.Contract, file string) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "cannot read the vocabulary")
	}
	if _, err := tokenize.NewWordPiece(b); err != nil {
		return axxerr.New(contract.CodeContract, exitcode.Usage, "%s cannot be used: %v", file, err).
			WithHint("--vocab is a WordPiece vocabulary: a token a line, [UNK] among them (BERT's vocab.txt)")
	}
	ids := ""
	for _, in := range c.Inputs {
		if in.Name == "input_ids" || ids == "" && in.Type.Integer() {
			ids = in.Name
		}
	}
	if ids == "" {
		return axxerr.New(contract.CodeContract, exitcode.Usage, "the model has no input of tokens' ids (integers)")
	}
	c.Carry("vocabulary", "vocab.txt", b)
	c.Text = &contract.Text{IDs: ids, Tokenizer: "wordpiece", Vocabulary: "vocabulary"}
	return nil
}

// readLabels reads a file of labels, one a line.
func readLabels(file string) ([]string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "cannot read the labels")
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

// giveLabels gives the labels of the file to the output with as many
// scores, or, of none, to the output with a dimension of as many (a
// segmenter's mask's, a text recognizer's steps').
// contractHint is what a contract that cannot be used needs, by its task.
func contractHint(c *contract.Contract) string {
	switch c.Task {
	case "classification":
		return "--task classification needs --labels, a file of the labels"
	case "text-to-speech":
		return "a voice's stub speaks a waveform of a fixed length: give it with --output, of a number of samples (--output 'output=float32[1,1,1,96000]': six seconds at 16 kHz)"
	}
	return "give a tensor's type and shape with --input or --output (--output 'output0=float32[1,1000]')"
}

func giveLabels(c *contract.Contract, file string) error {
	lines, err := readLabels(file)
	if err != nil {
		return err
	}
	for i := range c.Outputs {
		if task.ItemSize(&c.Outputs[i]) == len(lines) {
			c.Outputs[i].Labels = lines
			return nil
		}
	}
	for i := range c.Outputs {
		if slices.ContainsFunc(c.Outputs[i].Shape, func(d contract.Dim) bool { return d.Fixed() && int(d.Size) == len(lines) }) {
			c.Outputs[i].Labels = lines
			return nil
		}
	}
	return axxerr.New(contract.CodeContract, exitcode.Usage, "%s has %d labels, and no output of the model has as many scores", file, len(lines)).
		WithHint("a labels file has a label a line, one for each score of the classifier's output")
}

func build(call *core.CommandCall) error {
	out := call.Flags["out"]
	if out == "" {
		return axxerr.New("AXX-E0001", exitcode.Usage, "--out is required: the stub's file").
			WithHint("axx models build models/photo-check -o app/src/debug/assets/photo-check.tflite")
	}
	dir, contractFile, mappingsDir := "", call.Flags["contract"], call.Flags["mappings"]
	switch {
	case len(call.Args) == 1:
		if contractFile != "" || mappingsDir != "" {
			return axxerr.New("AXX-E0001", exitcode.Usage, "give a stub directory or --contract and --mappings, not both").
				WithHint("axx models build --contract models/contract.json --mappings models/mappings -o model.axxmodel")
		}
		dir = call.Args[0]
		contractFile, mappingsDir = filepath.Join(dir, contract.FileName), filepath.Join(dir, "mappings")
	case contractFile == "" || mappingsDir == "":
		return axxerr.New("AXX-E0001", exitcode.Usage, "a stub directory or both --contract and --mappings are required").
			WithHint("axx models build models/photo-check -o photo-check.tflite")
	default:
		dir = filepath.Dir(contractFile)
	}
	s, err := stub.LoadFiles(contractFile, mappingsDir)
	if err != nil {
		return err
	}
	f, ok := format.ByType(s.Contract.Format.Type)
	if !ok {
		return axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format %q is not one stubs are written in", s.Contract.Format.Type).
			At(contractFile, 0, 0).
			WithHint("stubs are written in %s", strings.Join(format.Types(), ", "))
	}
	model, err := f.Write(s)
	if err != nil {
		return err
	}
	size, err := save(model, out)
	if err != nil {
		return err
	}
	if ops, err := f.OperatorsOf(out); err == nil {
		written := &contract.Contract{Format: contract.Format{Operators: ops}}
		if ops := format.Beyond(s.Contract, written); len(ops) > 0 {
			call.Logger.Info("the stub uses operators the real model does not; a runtime built for that model alone (a minimal build) lacks them", "operators", strings.Join(ops, ", "))
		}
	}
	keyed := s.Mappings
	data := map[string]any{"stub": dir, "model": out, "format": f.Type, "mappings": keyed, "default": s.Names[s.Keyed()], "bytes": size}
	return call.Emit(data, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "wrote %s: %s stub of %d %s and the default (%s)\n", out, article(formatName(f.Type)), keyed, plural(keyed, "mapping"), sizeOf(size))
		return err
	})
}

// save writes a model to out: its file and the files beside it, or its
// folder's files below out. It returns how many bytes it wrote.
func save(m format.Model, out string) (int, error) {
	if m.Files == nil {
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return 0, err
		}
		size := len(m.File)
		for _, name := range slices.Sorted(maps.Keys(m.Beside)) {
			path := filepath.Join(filepath.Dir(out), filepath.FromSlash(format.BesideName(out, name)))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return 0, err
			}
			if err := os.WriteFile(path, m.Beside[name], 0o644); err != nil {
				return 0, err
			}
			size += len(m.Beside[name])
		}
		return size, os.WriteFile(out, m.File, 0o644)
	}
	size := 0
	for _, name := range slices.Sorted(maps.Keys(m.Files)) {
		p := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return 0, err
		}
		if err := os.WriteFile(p, m.Files[name], 0o644); err != nil {
			return 0, err
		}
		size += len(m.Files[name])
	}
	return size, nil
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func sizeOf(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d bytes", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}
