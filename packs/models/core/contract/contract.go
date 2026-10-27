// Package contract is what an app relies on from a model: its inputs and
// outputs, as the model's format declares them, what they mean (an image's
// layout, a classifier's labels, quantization), and the format's version. A
// stub keeps to the contract of the model it replaces, so the app's runtime
// loads it as it loads the real model, and the app reads its answers as it
// reads the real model's.
package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// CodeContract: a contract file cannot be read or used.
const CodeContract = "AXX-E0700"

// FileName is a stub's contract, in its directory.
const FileName = "contract.json"

// Contract is what an app relies on from a model.
type Contract struct {
	// Format is the model's format and version.
	Format Format `json:"format"`
	// Task says what the outputs mean, which the answers of the stub's
	// mappings are written in: "classification", or the task of a pack's
	// adapter (package task). Without one, answers give the outputs' values
	// (tensors).
	Task    string   `json:"task,omitempty"`
	Inputs  []Tensor `json:"inputs"`
	Outputs []Tensor `json:"outputs"`
	// Match says how an input matches a mapping's.
	Match Match `json:"match,omitzero"`
	// Text says the model's inputs are a text's tokens, and how the app
	// tokenizes it: a text classifier's or embedder's (BERT's).
	Text *Text `json:"text,omitempty"`
	// Metadata is the model's metadata, which the stub has too: some apps
	// read it, like the labels of a classifier.
	Metadata map[string]string `json:"metadata,omitempty"`
	// Files are files the model carries, which the stub carries too, by what
	// they are (a TFLite model's metadata buffers by name, and the files
	// appended to it): their paths, beside the contract's file.
	Files map[string]string `json:"files,omitempty"`

	// dir is the directory of the contract's file, where its files are;
	// contents are files not saved yet, by key.
	dir      string
	contents map[string][]byte
}

// FilesDir is the folder of a contract's files, beside it.
const FilesDir = "model-files"

// Carry has the model's file of the key with the contract, to save as name in
// FilesDir.
func (c *Contract) Carry(key, name string, data []byte) {
	if c.Files == nil {
		c.Files = map[string]string{}
	}
	if c.contents == nil {
		c.contents = map[string][]byte{}
	}
	c.Files[key] = FilesDir + "/" + name
	c.contents[key] = data
}

// Unsaved reports whether the contract carries files not saved yet.
func (c *Contract) Unsaved() bool { return len(c.contents) > 0 }

// File is the content of the model's file of the key.
func (c *Contract) File(key string) ([]byte, error) {
	if b, ok := c.contents[key]; ok {
		return b, nil
	}
	rel, ok := c.Files[key]
	if !ok {
		return nil, fmt.Errorf("the contract carries no file %q", key)
	}
	return os.ReadFile(filepath.Join(c.dir, filepath.FromSlash(rel)))
}

// Format is a model's format and its version.
type Format struct {
	// Type is the format: "onnx".
	Type string `json:"type"`
	// IRVersion and Opsets are an ONNX model's IR version and its operator
	// sets' versions, by domain ("" is ai.onnx): a stub is no newer, so that
	// the app's ONNX Runtime loads it.
	IRVersion int64            `json:"irVersion,omitempty"`
	Opsets    map[string]int64 `json:"opsets,omitempty"`
	// Operators are the operators the model uses (domain:op outside
	// ai.onnx). A runtime built for the model alone has those only.
	Operators []string `json:"operators,omitempty"`
	// Producer is what made the model, as it says.
	Producer string `json:"producer,omitempty"`
	// ExternalData are the files beside an ONNX model its weights are in,
	// by their names, which its runtime loads with it.
	ExternalData []string `json:"externalData,omitempty"`
	// Version is the version of the format's schema (TFLite's) or
	// specification (Core ML's).
	Version int64 `json:"version,omitempty"`
	// Signature is a TFLite model's signature, by which apps call it with
	// LiteRT's signature runners.
	Signature *Signature `json:"signature,omitempty"`
	// PredictedFeature and PredictedProbabilities name a Core ML
	// classifier's outputs: its label (a string), and its probabilities by
	// label (the output of the contract with the labels).
	PredictedFeature       string `json:"predictedFeature,omitempty"`
	PredictedProbabilities string `json:"predictedProbabilities,omitempty"`
	// Details are a format's own settings, which its reader writes and its
	// writer reads, for what the fields above do not say: a pack's format's.
	Details json.RawMessage `json:"details,omitempty"`
}

// Signature is the key of a model's signature, and the names it gives the
// model's inputs and outputs, by the tensors' names.
type Signature struct {
	Key     string            `json:"key"`
	Inputs  map[string]string `json:"inputs"`
	Outputs map[string]string `json:"outputs"`
}

// Tensor is an input or output of a model.
type Tensor struct {
	Name  string `json:"name"`
	Type  DType  `json:"type"`
	Shape []Dim  `json:"shape"`
	// Image says how the app lays an image out in an input.
	Image *Image `json:"image,omitempty"`
	// Audio says an input is a sound's samples, at its sample rate.
	Audio *Audio `json:"audio,omitempty"`
	// Quant is how an integer tensor stands for real numbers: real =
	// scale × (value − zeroPoint).
	Quant *Quant `json:"quant,omitempty"`
	// Labels are a classifier's labels, by the index of their score in the
	// output.
	Labels []string `json:"labels,omitempty"`
	// Activation says what a classifier's output holds: "softmax",
	// probabilities that add up to 1 (the default); "sigmoid", a probability
	// for each label; or "none", logits the app turns into probabilities
	// with a softmax. A segmenter's mask of confidences of activation "none"
	// holds logits the app takes a sigmoid of (a background remover's).
	Activation string `json:"activation,omitempty"`
	// Detection says what an object detector's output holds: "boxes",
	// "classes" (each object's label's index, its labels the output's),
	// "scores" and "count", as a detector that suppresses overlapping boxes
	// itself gives them (SSD, EfficientDet with their post-processing);
	// "anchors", each anchor's box and its labels' scores (YOLO); or
	// "locations", each anchor's box encoded against it (Anchors says how),
	// and "classScores", each anchor's labels' scores (its labels the
	// output's), as SSD and EfficientDet give them without their
	// post-processing (MediaPipe's): the app decodes the boxes, and
	// suppresses the overlapping ones.
	Detection string `json:"detection,omitempty"`
	// Box is how a boxes or anchors output gives a box.
	Box *Box `json:"box,omitempty"`
	// Anchors are what a locations output's boxes are encoded against.
	Anchors *Anchors `json:"anchors,omitempty"`
}

// Detections are the detection outputs' contents.
var Detections = []string{"boxes", "classes", "scores", "count", "anchors", "locations", "classScores", "coordinates", "confidence"}

// Anchors are the anchors of a detector whose boxes are encoded against
// them, as SSD's box coder encodes them: a box's center, less its anchor's,
// over its anchor's size, and its size over its anchor's (its log, with
// Exponential), each times its scale.
type Anchors struct {
	// File is the contract's file of the anchors: 4 float32s each, little
	// endian, their center's x and y and their width and height, shares of
	// the input picture's.
	File string `json:"file"`
	// Scales are the scales of a box's center's y and x, and its height
	// and width.
	Scales [4]float64 `json:"scales"`
	// Exponential says the sizes are encoded as logs.
	Exponential bool `json:"exponential,omitempty"`
	// Logits says the scores are logits, which the app turns into
	// probabilities with a sigmoid.
	Logits bool `json:"logits,omitempty"`
}

// Box is how an output gives a box: the order of its four values, and
// whether they are shares of the input picture's width and height (ratios,
// the default) or pixels of it.
type Box struct {
	// Order is "ymin,xmin,ymax,xmax" (the default: TensorFlow's),
	// "xmin,ymin,xmax,ymax", "x,y,width,height" (its top left corner) or
	// "cx,cy,width,height" (its center).
	Order string `json:"order,omitempty"`
	// Pixels says the values are pixels of the input picture.
	Pixels bool `json:"pixels,omitempty"`
}

// BoxOrders are the orders a box's values are in.
var BoxOrders = []string{"ymin,xmin,ymax,xmax", "xmin,ymin,xmax,ymax", "x,y,width,height", "cx,cy,width,height"}

// Activations are the activations a classifier's output takes.
var Activations = []string{"softmax", "sigmoid", "none"}

// Image says how an app lays out an image in an input.
type Image struct {
	// Layout is the order of the dimensions: "NCHW" or "NHWC" (or "CHW" and
	// "HWC" without a batch).
	Layout string `json:"layout"`
	// Channels are the channels' colors: "RGB" (the default), "BGR" or
	// "gray".
	Channels string `json:"channels,omitempty"`
	// Resize is how the app fits a picture to the input's size:
	// "center-crop" (the default: the middle of it, at the input's
	// proportions, as Apple's Vision does) or "stretch".
	Resize string `json:"resize,omitempty"`
	// Crop is the share of the fitted picture's middle the app keeps, of
	// each side: torchvision's and timm's evaluation transforms take the
	// middle 224 of a picture whose shorter side they resize to 256, 0.875.
	// The default, 1, keeps all of it.
	Crop float64 `json:"crop,omitempty"`
}

// Text says a model's inputs are a text's tokens: the input of their ids,
// which a stub matches (the app fills the others it has, a mask, segments),
// and how the app tokenizes a text.
type Text struct {
	// IDs is the input of the tokens' ids.
	IDs string `json:"ids"`
	// Tokenizer is "wordpiece" (BERT's: the text lowercased, split at spaces
	// and around punctuation, each word in pieces of the vocabulary),
	// "regex" (the text's words, between Delimiter's matches, as the
	// vocabulary has them) or "huggingface" (a Hugging Face tokenizer's:
	// WordPiece's or BPE's, as its tokenizer.json says). "piper" is a voice's
	// codepoints, with BOS, PAD after each one and EOS, as its phoneme_type
	// "text" gives them.
	Tokenizer string `json:"tokenizer"`
	// Vocabulary is the contract's file of the vocabulary: a token a line
	// (WordPiece's), a token and its id a line (a regex tokenizer's), a
	// Hugging Face tokenizer's tokenizer.json, or a Piper voice's phoneme_id_map.
	Vocabulary string `json:"vocabulary"`
	// Delimiter is a regex tokenizer's expression of what is between words.
	Delimiter string `json:"delimiter,omitempty"`
}

// Tokenizers are the tokenizers a text's tokens are of.
var Tokenizers = []string{"wordpiece", "regex", "huggingface", "piper"}

// Audio says an input is a sound's samples: a window of them, one channel,
// at the sample rate; or, with Features, what the app computes of them.
type Audio struct {
	SampleRate int `json:"sampleRate"`
	// Features are what the input is of the sound, when it is not its
	// samples: "log-mel", its log-mel spectrogram as a speech recognizer
	// (Whisper) has it, [mels, frames] (match.LogMel); or "parakeet", the
	// features parakeet.cpp computes of the sound from a Parakeet GGUF's own
	// frontend: a stub's, of match.ParakeetMels bands (match.ParakeetLogMel),
	// reduced as its encoder reduces them.
	Features string `json:"features,omitempty"`
}

// AudioFeatures are the features of a sound an input may be.
var AudioFeatures = []string{"log-mel", "parakeet"}

// Quant is how an integer tensor stands for real numbers.
type Quant struct {
	Scale     float64 `json:"scale"`
	ZeroPoint int64   `json:"zeroPoint"`
}

// Match says how an input matches a mapping's.
type Match struct {
	// Input is the input whose value is matched: the first by default.
	Input string `json:"input,omitempty"`
	// Similarity is how alike an input and a mapping's must be for the
	// mapping to answer, from 0 to 1 (the correlation of their reduced
	// forms): 0.9 by default.
	Similarity float64 `json:"similarity,omitempty"`
}

// DefaultSimilarity is how alike an input and a mapping's must be, unless the
// contract says.
const DefaultSimilarity = 0.9

// Similarity is how alike an input and a mapping's must be.
func (c *Contract) Similarity() float64 {
	if c.Match.Similarity > 0 {
		return c.Match.Similarity
	}
	return DefaultSimilarity
}

// MatchedInput is the input whose value is matched: a text's ids, or the
// match's input, or the first; nil for a model of an API, which has none.
func (c *Contract) MatchedInput() *Tensor {
	if len(c.Inputs) == 0 {
		return nil
	}
	if c.Text != nil && c.Match.Input == "" {
		for i := range c.Inputs {
			if c.Inputs[i].Name == c.Text.IDs {
				return &c.Inputs[i]
			}
		}
	}
	if c.Match.Input == "" {
		return &c.Inputs[0]
	}
	for i := range c.Inputs {
		if c.Inputs[i].Name == c.Match.Input {
			return &c.Inputs[i]
		}
	}
	return nil
}

// Output is the output named name, or nil.
func (c *Contract) Output(name string) *Tensor {
	for i := range c.Outputs {
		if c.Outputs[i].Name == name {
			return &c.Outputs[i]
		}
	}
	return nil
}

// Dim is a dimension of a tensor's shape: a size, or a name for one the model
// leaves open, like "batch".
type Dim struct {
	Size int64
	Name string
}

// Fixed reports whether the dimension has a size.
func (d Dim) Fixed() bool { return d.Name == "" && d.Size > 0 }

func (d Dim) String() string {
	if d.Name != "" {
		return d.Name
	}
	return strconv.FormatInt(d.Size, 10)
}

// MarshalJSON writes a size as a number and a name as a string.
func (d Dim) MarshalJSON() ([]byte, error) {
	if d.Name != "" {
		return json.Marshal(d.Name)
	}
	return json.Marshal(d.Size)
}

// UnmarshalJSON reads a number or a string.
func (d *Dim) UnmarshalJSON(b []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte(`"`)) {
		*d = Dim{}
		return json.Unmarshal(b, &d.Name)
	}
	*d = Dim{}
	return json.Unmarshal(b, &d.Size)
}

// Load reads the contract file at path and checks it.
func Load(path string) (*Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, CodeContract, exitcode.Usage, "cannot read the model's contract").
			WithHint("a stub's directory has its model's contract, %s: `axx models contract <model>` writes it", FileName)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c Contract
	if err := dec.Decode(&c); err != nil {
		return nil, invalid(path, "%v", err)
	}
	if err := c.Check(); err != nil {
		return nil, invalid(path, "%v", err)
	}
	c.dir = filepath.Dir(path)
	for key, rel := range c.Files {
		if _, err := os.Stat(filepath.Join(c.dir, filepath.FromSlash(rel))); err != nil {
			return nil, invalid(path, "files.%s: %v", key, err)
		}
	}
	return &c, nil
}

// Save writes the contract to path, as JSON, and the files it carries
// beside it.
func (c *Contract) Save(path string) error {
	b, err := c.JSON()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	for key, data := range c.contents {
		p := filepath.Join(dir, filepath.FromSlash(c.Files[key]))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	c.dir, c.contents = dir, nil
	return os.WriteFile(path, b, 0o644)
}

// JSON is the contract as its file has it.
func (c *Contract) JSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func invalid(path, format string, args ...any) error {
	return axxerr.New(CodeContract, exitcode.Usage, "the model's contract cannot be used: "+format, args...).
		At(path, 0, 0).
		WithHint("fix it, or write it again from the model with `axx models contract <model>`")
}

// apis are the formats whose models are an API's (RegisterAPI).
var apis = map[string]bool{}

// RegisterAPI says the models of a format are an API's, not a graph of
// tensors: a Foundation Models language model, a Swift type an app gives
// its sessions, which takes a prompt and answers it. Their contracts have no
// inputs or outputs. A format registered as an API's says so
// (format.Format's API).
func RegisterAPI(formatType string) { apis[formatType] = true }

// Check reports what makes the contract unusable.
func (c *Contract) Check() error {
	var errs []error
	if c.Format.Type == "" {
		errs = append(errs, errors.New("format.type is missing: the model's format, like onnx"))
	}
	if len(c.Inputs) == 0 && !apis[c.Format.Type] {
		errs = append(errs, errors.New("inputs are missing"))
	}
	if len(c.Outputs) == 0 && !apis[c.Format.Type] {
		errs = append(errs, errors.New("outputs are missing"))
	}
	names := map[string]bool{}
	for _, side := range []struct {
		name    string
		tensors []Tensor
	}{{"input", c.Inputs}, {"output", c.Outputs}} {
		for _, t := range side.tensors {
			if names[t.Name] {
				errs = append(errs, fmt.Errorf("%s %q: another input or output has its name", side.name, t.Name))
			}
			names[t.Name] = true
			if err := t.check(side.name == "input"); err != nil {
				errs = append(errs, fmt.Errorf("%s %q: %w", side.name, t.Name, err))
			}
		}
	}
	if t := c.Text; t != nil {
		if !slices.ContainsFunc(c.Inputs, func(in Tensor) bool { return in.Name == t.IDs }) {
			errs = append(errs, fmt.Errorf("text.ids %q is none of the inputs", t.IDs))
		}
		if !slices.Contains(Tokenizers, t.Tokenizer) {
			errs = append(errs, fmt.Errorf("text.tokenizer %q is not one of %s", t.Tokenizer, strings.Join(Tokenizers, ", ")))
		}
		if t.Tokenizer == "regex" && t.Delimiter == "" {
			errs = append(errs, errors.New("text.delimiter is missing: a regex tokenizer's expression of what is between words"))
		}
	}
	if len(c.Inputs) > 0 && c.MatchedInput() == nil {
		errs = append(errs, fmt.Errorf("match.input %q is none of the inputs", c.Match.Input))
	}
	if s := c.Match.Similarity; s < 0 || s > 1 {
		errs = append(errs, fmt.Errorf("match.similarity %v is not between 0 and 1", s))
	}
	return errors.Join(errs...)
}

func (t *Tensor) check(input bool) error {
	var errs []error
	if t.Name == "" {
		errs = append(errs, errors.New("it has no name"))
	}
	if !t.Type.Known() {
		errs = append(errs, fmt.Errorf("type %q is not one of %s", t.Type, strings.Join(typeNames(), ", ")))
	}
	for _, d := range t.Shape {
		if d.Name == "" && d.Size < 0 {
			errs = append(errs, fmt.Errorf("its shape has a size below 0: %d", d.Size))
		}
	}
	if t.Image != nil {
		if !input {
			errs = append(errs, errors.New("an output has no image layout"))
		}
		if err := t.Image.check(t.Shape); err != nil {
			errs = append(errs, err)
		}
	}
	if t.Audio != nil && (!input || t.Image != nil || t.Audio.SampleRate <= 0) {
		errs = append(errs, errors.New("audio is an input's, not an image's, with its sample rate"))
	}
	if a := t.Audio; a != nil && a.Features != "" {
		item := t.Shape
		if t.Batched() {
			item = item[1:]
		}
		switch {
		case !slices.Contains(AudioFeatures, a.Features):
			errs = append(errs, fmt.Errorf("audio.features %q is not one of %s", a.Features, strings.Join(AudioFeatures, ", ")))
		case len(item) != 2 || !item[0].Fixed() || !item[1].Fixed():
			errs = append(errs, fmt.Errorf("an input of a sound's %s is [mels, frames], of a fixed size: %s", a.Features, t.Shape))
		}
	}
	if t.Quant != nil && (!t.Type.Integer() || t.Quant.Scale <= 0) {
		errs = append(errs, errors.New("quant is for integer tensors, with a scale above 0"))
	}
	if len(t.Labels) > 0 && input {
		errs = append(errs, errors.New("an input has no labels"))
	}
	if t.Activation != "" && !slices.Contains(Activations, t.Activation) {
		errs = append(errs, fmt.Errorf("activation %q is not one of %s", t.Activation, strings.Join(Activations, ", ")))
	}
	if t.Detection != "" && (input || !slices.Contains(Detections, t.Detection)) {
		errs = append(errs, fmt.Errorf("detection %q is not one of an output's: %s", t.Detection, strings.Join(Detections, ", ")))
	}
	if t.Box != nil && t.Box.Order != "" && !slices.Contains(BoxOrders, t.Box.Order) {
		errs = append(errs, fmt.Errorf("box.order %q is not one of %s", t.Box.Order, strings.Join(BoxOrders, "; ")))
	}
	return errors.Join(errs...)
}

func (im *Image) check(shape []Dim) error {
	want := map[string]int{"NCHW": 4, "NHWC": 4, "CHW": 3, "HWC": 3}
	rank, ok := want[im.Layout]
	if !ok {
		return fmt.Errorf("image.layout %q is not NCHW, NHWC, CHW or HWC", im.Layout)
	}
	if len(shape) != rank {
		return fmt.Errorf("image.layout %s needs a shape of %d dimensions, not %d", im.Layout, rank, len(shape))
	}
	if im.Channels != "" && !slices.Contains([]string{"RGB", "BGR", "gray"}, im.Channels) {
		return fmt.Errorf("image.channels %q is not RGB, BGR or gray", im.Channels)
	}
	if im.Resize != "" && im.Resize != "center-crop" && im.Resize != "stretch" {
		return fmt.Errorf("image.resize %q is not center-crop or stretch", im.Resize)
	}
	if im.Crop < 0 || im.Crop > 1 {
		return fmt.Errorf("image.crop %v is not a share from 0 to 1 of the picture's middle", im.Crop)
	}
	return nil
}

// ImageDims are an image input's channels, height and width, by their
// positions in its shape.
func (t *Tensor) ImageDims() (c, h, w int) {
	l := t.Image.Layout
	if len(l) == 4 {
		l = l[1:]
	}
	off := len(t.Shape) - 3
	return off + strings.IndexByte(l, 'C'), off + strings.IndexByte(l, 'H'), off + strings.IndexByte(l, 'W')
}

// Batched reports whether the tensor's first dimension is a batch: an image's
// N, or the first of two dimensions or more that is open or 1.
func (t *Tensor) Batched() bool {
	if t.Image != nil {
		return len(t.Image.Layout) == 4
	}
	return len(t.Shape) >= 2 && (t.Shape[0].Name != "" || t.Shape[0].Size == 1)
}

// GuessImage gives an input shaped like a picture its layout: four
// dimensions, one or three channels and sides of 16 pixels at least, the
// channels before the rows (NCHW) or after the columns (NHWC).
func (t *Tensor) GuessImage() {
	if len(t.Shape) != 4 {
		return
	}
	ch := func(d Dim) bool { return d.Fixed() && (d.Size == 1 || d.Size == 3) }
	px := func(d Dim) bool { return d.Fixed() && d.Size >= 16 }
	switch {
	case ch(t.Shape[1]) && px(t.Shape[2]) && px(t.Shape[3]):
		t.Image = &Image{Layout: "NCHW"}
	case ch(t.Shape[3]) && px(t.Shape[1]) && px(t.Shape[2]):
		t.Image = &Image{Layout: "NHWC"}
	default:
		return
	}
	if ci, _, _ := t.ImageDims(); t.Shape[ci].Size == 1 {
		t.Image.Channels = "gray"
	}
}
