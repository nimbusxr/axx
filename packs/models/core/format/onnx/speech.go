package onnx

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A speech recognizer (Whisper's) is an encoder and a decoder, as Hugging
// Face's exporters write them and transformers.js runs them: the encoder
// (encoder_model*.onnx) of a sound's log-mel spectrogram (input_features)
// gives its hidden state (last_hidden_state); the decoder
// (decoder_model_merged*.onnx, beside it) of the forced tokens that begin a
// transcript, then each token it writes, and the hidden state, gives the
// next token's logits, its cache (its own positions' and the hidden
// state's) carried from each call to the next.

// speechDetails are a speech recognizer's own settings in its contract.
type speechDetails struct {
	// EndOfGeneration is the token that ends a transcript.
	EndOfGeneration int32 `json:"endOfGeneration"`
	// Suppressed are the tokens its runtime never lets it write, and
	// BeginSuppressed those it does not let a transcript begin with
	// (generation_config.json's).
	Suppressed      []int32 `json:"suppressed,omitempty"`
	BeginSuppressed []int32 `json:"beginSuppressed,omitempty"`
	// NoTimestamps is the token that, forced, says a transcript has no
	// timestamps; the timestamps' (0.00, then each fiftieth of a second
	// later) follow it.
	NoTimestamps int32 `json:"noTimestamps"`
	// Decoder is its decoder's graph.
	Decoder speechDecoder `json:"decoder"`
}

// speechDecoder is a speech recognizer's decoder: its file, beside the
// encoder's, its IR version, operator sets and the files its weights are
// in, and its inputs and outputs.
type speechDecoder struct {
	File         string            `json:"file"`
	IRVersion    int64             `json:"irVersion"`
	Opsets       map[string]int64  `json:"opsets"`
	ExternalData []string          `json:"externalData,omitempty"`
	Inputs       []contract.Tensor `json:"inputs"`
	Outputs      []contract.Tensor `json:"outputs"`
}

// isSpeechEncoder reports whether a contract is a speech recognizer's
// encoder's.
func isSpeechEncoder(c *contract.Contract) bool {
	return slices.ContainsFunc(c.Inputs, func(t contract.Tensor) bool { return t.Name == "input_features" }) &&
		slices.ContainsFunc(c.Outputs, func(t contract.Tensor) bool { return t.Name == "last_hidden_state" })
}

// speechContract makes an encoder's contract a speech recognizer's: its
// decoder's (decoder_model_merged, of the encoder's suffix, beside it); its
// input a sound's log-mel spectrogram, of the size its processor makes
// (preprocessor_config.json's); its tokenizer, which it carries; and its
// end of a transcript and the tokens its runtime suppresses
// (generation_config.json's).
func speechContract(c *contract.Contract, path string) error {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	dir := filepath.Dir(path)
	suffix := strings.TrimPrefix(strings.TrimSuffix(filepath.Base(path), ".onnx"), "encoder_model")
	decoderFile := "decoder_model_merged" + suffix + ".onnx"
	b, err := os.ReadFile(filepath.Join(dir, decoderFile))
	if err != nil {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a speech recognizer's encoder, and its decoder (%s) is not beside it", path, decoderFile).
			WithHint("keep the model in its folder as it comes: onnx/encoder_model.onnx beside onnx/decoder_model_merged.onnx, of the same dtype")
	}
	dec, err := Contract(b)
	if err != nil {
		return fail("its decoder %s: %v", decoderFile, err)
	}
	if !slices.ContainsFunc(dec.Inputs, func(t contract.Tensor) bool { return t.Name == "encoder_hidden_states" }) {
		return fail("its decoder %s takes no encoder_hidden_states", decoderFile)
	}
	if _, err := layoutOf(dec); err != nil {
		return fail("its decoder %s: %v", decoderFile, err)
	}
	find := func(name string) []byte {
		for _, d := range []string{dir, filepath.Dir(dir)} {
			if b, err := os.ReadFile(filepath.Join(d, name)); err == nil {
				return b
			}
		}
		return nil
	}
	tok := find("tokenizer.json")
	if tok == nil {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a speech recognizer, whose contract carries its tokenizer: there is no tokenizer.json beside it or in the folder above", path).
			WithHint("keep the model in its folder as it comes (onnx/encoder_model.onnx beside tokenizer.json and preprocessor_config.json, as Hugging Face's repositories have it)")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return fail("%v", err)
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return fail("%v", err)
	}
	var pre struct {
		FeatureSize  int `json:"feature_size"`
		Frames       int `json:"nb_max_frames"`
		SamplingRate int `json:"sampling_rate"`
	}
	if err := json.Unmarshal(find("preprocessor_config.json"), &pre); err != nil || pre.FeatureSize == 0 || pre.Frames == 0 || pre.SamplingRate == 0 {
		return fail("its folder has no preprocessor_config.json of its features' size (feature_size, nb_max_frames, sampling_rate)")
	}
	var gen struct {
		EOS             json.RawMessage `json:"eos_token_id"`
		Suppress        []int32         `json:"suppress_tokens"`
		BeginSuppressed []int32         `json:"begin_suppress_tokens"`
		NoTimestamps    int32           `json:"no_timestamps_token_id"`
	}
	if err := json.Unmarshal(find("generation_config.json"), &gen); err != nil || gen.NoTimestamps == 0 {
		return fail("its folder has no generation_config.json of its token of no timestamps (no_timestamps_token_id)")
	}
	stops := stopTokens(find, v)
	if len(stops) == 0 {
		return fail("its folder gives no end of a transcript (eos_token_id) in generation_config.json or config.json")
	}
	d := speechDetails{
		EndOfGeneration: stops[0], Suppressed: gen.Suppress, BeginSuppressed: gen.BeginSuppressed, NoTimestamps: gen.NoTimestamps,
		Decoder: speechDecoder{File: decoderFile, IRVersion: dec.Format.IRVersion, Opsets: dec.Format.Opsets, ExternalData: dec.Format.ExternalData, Inputs: dec.Inputs, Outputs: dec.Outputs},
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	for i := range c.Inputs {
		in := &c.Inputs[i]
		if in.Name == "input_features" {
			if len(in.Shape) != 3 {
				return fail("its input_features are not [batch, mels, frames]: %s", in.Shape)
			}
			in.Shape = []contract.Dim{in.Shape[0], {Size: int64(pre.FeatureSize)}, {Size: int64(pre.Frames)}}
			in.Audio = &contract.Audio{SampleRate: pre.SamplingRate, Features: "log-mel"}
			c.Match.Input = in.Name
		}
	}
	c.Task, c.Format.Details = "speech-to-text", raw
	c.Carry(tokenizerFile, "tokenizer.json", tok)
	return nil
}

// writeSpeech writes a speech recognizer's stub: its encoder, which gives
// the mapping whose sound the input's is nearest (or the default's) as its
// hidden state's first value, and the timestamp the sound ends at as its
// second (its last frame above the silence it is padded with), the rest 0;
// and its decoder, beside it, which writes that mapping's transcript after
// the forced tokens (between timestamps, unless they say there are none),
// then its end. A transcript begins with a space, as Whisper writes them.
func writeSpeech(s *stub.Stub, opset, ir int64) (format.Model, error) {
	c := s.Contract
	var d speechDetails
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		return format.Model{}, fmt.Errorf("the contract's format.details are not a speech recognizer's: %w", err)
	}
	g, err := encoderGraph(s, opset)
	if err != nil {
		return format.Model{}, err
	}
	encoder := assemble(c, g, c.Inputs, c.Outputs, ir, opset)

	dec := &contract.Contract{Format: contract.Format{Type: "onnx", IRVersion: d.Decoder.IRVersion, Opsets: d.Decoder.Opsets}, Inputs: d.Decoder.Inputs, Outputs: d.Decoder.Outputs}
	decOpset := dec.Format.Opsets[""]
	if decOpset < 13 {
		return format.Model{}, fmt.Errorf("its decoder's ONNX operator set is %d: speech recognizers' stubs are written in operator set 13 or later", decOpset)
	}
	l, err := layoutOf(dec)
	if err != nil {
		return format.Model{}, err
	}
	tok, err := c.File(tokenizerFile)
	if err != nil {
		return format.Model{}, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no tokenizer of the model")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return format.Model{}, err
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return format.Model{}, err
	}
	spoken := *s
	spoken.Generated = make([]string, len(s.Generated))
	for i, text := range s.Generated {
		spoken.Generated[i] = " " + strings.TrimLeft(text, " ")
	}
	p, err := lm.NewPlan(&spoken, v, d.EndOfGeneration, 0)
	if err != nil {
		return format.Model{}, err
	}
	for i, a := range p.Answers {
		for j, t := range a {
			if slices.Contains(d.Suppressed, t) || j == 0 && slices.Contains(d.BeginSuppressed, t) {
				return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: mapping %q: its text has %q, which the recognizer's runtime never lets it write", s.Names[i], v.Detokenize([]int32{t})).
					WithHint("write the transcript as the recognizer would: without the symbols its generation_config.json suppresses")
			}
		}
	}
	dg, err := decoderGraph(l, p, decOpset, &recognizing{encoder: "encoder_hidden_states", noTimestamps: d.NoTimestamps, timestampsBegin: d.NoTimestamps + 1})
	if err != nil {
		return format.Model{}, err
	}
	m := format.Model{File: encoder, Beside: map[string][]byte{d.Decoder.File: assemble(dec, dg, dec.Inputs, dec.Outputs, max(d.Decoder.IRVersion, 4), decOpset)}}
	for _, loc := range append(slices.Clone(c.Format.ExternalData), d.Decoder.ExternalData...) {
		m.Beside[loc] = []byte{}
	}
	return m, nil
}

// SpeechEncoder writes a speech recognizer stub's encoder alone, at its
// contract's operator set: of a sound's log-mel features, its hidden state
// gives the mapping and where the sound ends (a runtime's own decoder, as
// ExecuTorch's programs have it, reads them).
func SpeechEncoder(s *stub.Stub) ([]byte, error) {
	opset := s.Contract.Format.Opsets[""]
	g, err := encoderGraph(s, opset)
	if err != nil {
		return nil, err
	}
	return assemble(s.Contract, g, s.Contract.Inputs, s.Contract.Outputs, max(s.Contract.Format.IRVersion, 4), opset), nil
}

// encoderGraph is the graph of a speech recognizer stub's encoder: the
// mapping whose sound's features are nearest the input's, and where the
// sound ends, in each output's first values ([0, 0, 0] and [0, 0, 1]),
// the rest 0.
func encoderGraph(s *stub.Stub, opset int64) (*graph, error) {
	c := s.Contract
	if s.Nearest == nil {
		return nil, errors.New(`a speech recognizer's mappings are matched by their sounds: a mapping's input is {"audio": ...}`)
	}
	in := c.MatchedInput()
	g := &graph{}
	best := g.nearestBest(s, in)
	// Where the sound ends: its last frame above the silence it is padded
	// with (the least of its features), in fiftieths of a second (a
	// frame's a hundredth), one at least.
	frames := in.Shape[len(in.Shape)-1].Size
	feats := in.Name
	if in.Type != "float32" {
		feats = g.node("Cast", feats, intAttr("to", int64(elemTypes["float32"])))
	}
	var loudest, floor string
	if opset >= 18 {
		loudest = g.node("ReduceMax", feats, g.int64s(1), intAttr("keepdims", 0))
		floor = g.node("ReduceMin", feats, g.int64s(1, 2), intAttr("keepdims", 0))
	} else {
		loudest = g.node("ReduceMax", feats, intsAttr("axes", 1), intAttr("keepdims", 0))
		floor = g.node("ReduceMin", feats, intsAttr("axes", 1, 2), intAttr("keepdims", 0))
	}
	heard := g.node("Cast", g.node("Greater", loudest, g.node("Add", g.node("Unsqueeze", floor, g.int64s(1)), g.floats(nil, []float64{0.001}))), intAttr("to", int64(elemTypes["float32"])))
	positions := make([]float64, frames)
	for i := range positions {
		positions[i] = float64(i + 1)
	}
	var lastFrame string
	if opset >= 18 {
		lastFrame = g.node("ReduceMax", g.node("Mul", heard, g.floats([]int64{frames}, positions)), g.int64s(1), intAttr("keepdims", 1))
	} else {
		lastFrame = g.node("ReduceMax", g.node("Mul", heard, g.floats([]int64{frames}, positions)), intsAttr("axes", 1), intAttr("keepdims", 1))
	}
	endsAt := g.node("Max", g.node("Round", g.node("Div", lastFrame, g.floats(nil, []float64{2}))), g.floats(nil, []float64{1}))
	for _, out := range c.Outputs {
		if len(out.Shape) != 3 || !out.Shape[1].Fixed() || !out.Shape[2].Fixed() || out.Shape[2].Size < 2 {
			return nil, fmt.Errorf("its encoder's %s is not [batch, positions, size] of a fixed size: %s", out.Name, out.Shape)
		}
		x := g.node("Concat", g.node("Reshape", g.node("Cast", best, intAttr("to", int64(elemTypes["float32"]))), g.int64s(-1, 1)), endsAt, intAttr("axis", 1))
		x = g.node("Unsqueeze", x, g.int64s(1))
		x = g.node("Pad", x, g.int64s(0, 0, 0, 0, out.Shape[1].Size-1, out.Shape[2].Size-2), g.floats(nil, []float64{0}))
		if out.Type != "float32" {
			x = g.node("Cast", x, intAttr("to", int64(elemTypes[out.Type])))
		}
		g.nodeNamed(out.Name, "Identity", x)
	}
	return g, nil
}
