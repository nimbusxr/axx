package executorch

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/onnx"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A speech recognizer's program is as React Native ExecuTorch exports
// Whisper: encode, of a sound's samples (16 kHz, of any length up to its
// bound: the program pads them with silence), gives the encoder's hidden
// state; decode, of a token, its position and the hidden state, gives the
// next token's logits, its KV cache in the method's memory from call to
// call. The app gives decode the tokens it forces (<|startoftranscript|>,
// its language and task, <|notimestamps|>) a call each, then each it
// generates, until <|endoftext|>.

// whisperRate is the sample rate Whisper hears at.
const whisperRate = 16000

// whisperMels is how many mel filters Whisper's features have (large-v3's
// 128 are given in the contract).
const whisperMels = 80

// isSpeechProgram reports whether a program's methods are a speech
// recognizer's.
func isSpeechProgram(methods []method) bool {
	var encode, decode bool
	for _, m := range methods {
		switch m.Name {
		case "encode":
			encode = len(m.Inputs) == 1 && m.Inputs[0].Type == "float32" && len(m.Inputs[0].Shape) == 1 && len(m.Outputs) == 1
		case "decode":
			decode = len(m.Inputs) == 3 && m.Inputs[0].Type == "int64" && m.Inputs[1].Type == "int64" && len(m.Inputs[2].Shape) == 3
		}
	}
	return encode && decode
}

// speechContract makes a speech recognizer's contract: its input encode's
// sound, its output encode's hidden state, and its tokenizer (tokenizer.json,
// beside the program or in a folder above it) and the tokens that end its
// transcript and say it has no timestamps.
func speechContract(c *contract.Contract, path string, d *details) error {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	for _, m := range d.Methods {
		if m.Name == "encode" {
			c.Inputs, c.Outputs = m.Inputs, m.Outputs
		}
	}
	c.Inputs[0].Audio = &contract.Audio{SampleRate: whisperRate}
	var tok []byte
	for dir, i := filepath.Dir(path), 0; i < 3 && tok == nil; dir, i = filepath.Dir(dir), i+1 {
		tok, _ = os.ReadFile(filepath.Join(dir, "tokenizer.json"))
	}
	if tok == nil {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a speech recognizer, whose contract carries its tokenizer: there is no tokenizer.json beside it or in the two folders above", path).
			WithHint("keep the program in its folder as it comes (as Hugging Face's repositories have it: tokenizer.json at the top, the program beside it or below)")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return fail("%v", err)
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return fail("%v", err)
	}
	end, ok := v.ID("<|endoftext|>")
	if !ok {
		return fail("its tokenizer has no <|endoftext|>, which ends a transcript")
	}
	none, ok := v.ID("<|notimestamps|>")
	if !ok {
		return fail("its tokenizer has no <|notimestamps|>")
	}
	d.EndOfGeneration, d.NoTimestamps, d.Mels = end, none, whisperMels
	c.Task = "speech-to-text"
	c.Carry(tokenizerFile, "tokenizer.json", tok)
	return nil
}

// writeSpeechProgram writes a speech recognizer's stub: its encode, its
// decode, and its methods of settings as the model's.
func writeSpeechProgram(s *stub.Stub, d details) ([]byte, error) {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %s", fmt.Sprintf(f, args...))
	}
	tok, err := s.Contract.File(tokenizerFile)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no tokenizer of the model").
			WithHint("write the contract again from the model with `axx models contract <folder>/<program>.pte`")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return nil, err
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return nil, err
	}
	// Whisper writes a transcript after a space.
	spoken := *s
	spoken.Generated = make([]string, len(s.Generated))
	for i, text := range s.Generated {
		spoken.Generated[i] = " " + strings.TrimLeft(text, " ")
	}
	p, err := lm.NewPlan(&spoken, v, d.EndOfGeneration, 0)
	if err != nil {
		return nil, err
	}
	prog := &program{}
	for _, m := range d.Methods {
		var pl *plan
		switch {
		case m.Name == "encode":
			pl, err = encodePlan(prog, s, m, max(d.Mels, 1))
		case m.Name == "decode":
			var length int64
			for _, in := range m.Inputs[:1] {
				length = in.Shape[1].Size
			}
			pl, err = languageForward(prog, m, p, max(length, 448), &hearing{noTimestamps: d.NoTimestamps, timestampsBegin: d.NoTimestamps + 1})
		case len(m.Inputs) == 0 && len(m.Values) > 0:
			pl = settingsOf(m)
		default:
			err = fmt.Errorf("it is not one a speech recognizer's stub has")
		}
		if err != nil {
			return nil, fail("its method %s: %v", m.Name, err)
		}
		prog.plans = append(prog.plans, pl)
	}
	return prog.encode(), nil
}

// encodePlan is the plan of a speech recognizer stub's encode: the sound,
// padded with silence to its bound; its log-mel features, as Whisper's
// (match.LogMel); and of them, the ONNX speech encoder stub's graph,
// lowered: the hidden state of the mapping whose sound's features are
// nearest, and where the sound ends.
func encodePlan(prog *program, s *stub.Stub, m method, mels int) (*plan, error) {
	in, out := m.Inputs[0], m.Outputs[0]
	n := in.Shape[0].Size
	if n%match.MelHop != 0 || (n+match.MelFFT)%block != 0 {
		return nil, fmt.Errorf("its sound is of %d samples at most, not of whole frames of %d", n, match.MelHop)
	}
	frames := n / match.MelHop
	features := contract.Tensor{Name: "input_features", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: int64(mels)}, {Size: frames}}, Audio: &contract.Audio{SampleRate: whisperRate, Features: "log-mel"}}
	c := &contract.Contract{
		Format: contract.Format{Type: "onnx", IRVersion: 8, Opsets: map[string]int64{"": onnxOpset}},
		Task:   "speech-to-text", Inputs: []contract.Tensor{features},
		Outputs: []contract.Tensor{{Name: "last_hidden_state", Type: out.Type, Shape: out.Shape}},
	}
	ms, err := s.For(c)
	if err != nil {
		return nil, err
	}
	model, err := onnx.SpeechEncoder(ms)
	if err != nil {
		return nil, err
	}
	g, err := onnx.ReadGraph(model)
	if err != nil {
		return nil, err
	}
	l := newLowering(prog, m.Name)
	sound := l.alloc("float32", []int64{n})
	l.pl.values[sound].tensor.bounded = len(m.Bounded) > 0 && m.Bounded[0][0]
	l.pl.inputs = []int{sound}
	l.vals[features.Name] = &tensorInfo{typ: "float32", shape: sizes(features.Shape), value: l.logMel(sound, n, mels)}
	return l.graph(g, c)
}

// block is a frame's part (80 samples): a frame (400) is five, the samples
// between frames (160) two.
const block = 80

// logMel adds the kernels' calls that give a sound's log-mel features, as
// match.LogMel computes them ([1, mels, frames]): the sound (as many
// samples as it gives, up to n) padded with silence to n, reflected at
// each end by half a frame; each frame (five blocks of 80 samples, every
// two) windowed and transformed (a product with the transform's cosines and
// sines); each mel filter's power; its log10; no less than 8 under the
// greatest; plus 4, over 4.
func (l *lowering) logMel(sound int, n int64, mels int) int {
	floats := func(shape []int64, vs []float64) int {
		return l.constant("float32", shape, contract.DType("float32").Encode(vs))
	}
	longs := func(vs ...int64) int {
		fs := make([]float64, len(vs))
		for i, v := range vs {
			fs[i] = float64(v)
		}
		return l.constant("int64", []int64{int64(len(vs))}, contract.DType("int64").Encode(fs))
	}
	f32 := func(name, overload string, shape []int64, args ...int) int {
		return l.kernel(name, overload, "float32", shape, args...)
	}
	alpha := l.integer(1)
	frames, half := n/match.MelHop, int64(match.MelFFT/2)
	samples, call := []int64{n}, []int64{1}

	// The sound, padded with silence.
	ones := f32("aten::full_like", "out", samples, sound, l.double(1), l.null())
	l.pl.values[ones].tensor.bounded = true
	count := l.kernel("dim_order_ops::_to_dim_order_copy", "out", "int64", call,
		f32("aten::sum", "IntList_out", call, ones, l.intList(0), l.boolean(true), l.null()), l.boolean(false), l.intList(0))
	positions := l.kernel("aten::arange", "start_out", "int64", samples, l.integer(0), l.integer(n), l.integer(1))
	last := l.kernel("aten::sub", "out", "int64", call, count, longs(1), alpha)
	at := l.kernel("aten::minimum", "out", "int64", samples, positions, last)
	heard := l.kernel("aten::lt", "Tensor_out", "bool", samples, positions, count)
	padded := f32("aten::where", "self_out", samples, heard, f32("aten::index_select", "out", samples, sound, l.integer(0), at), l.scalar("float32", 0))

	// Reflected at its ends.
	left, right := make([]int64, half), make([]int64, half)
	for i := range half {
		left[half-1-i] = min(i+1, n-1)
		right[i] = max(n-2-i, 0)
	}
	x := f32("aten::cat", "out", []int64{n + 2*half}, l.tensorList(
		f32("aten::index_select", "out", []int64{half}, padded, l.integer(0), longs(left...)),
		padded,
		f32("aten::index_select", "out", []int64{half}, padded, l.integer(0), longs(right...)),
	), l.integer(0))

	// Its frames, windowed.
	blocks := (n + 2*half) / block
	x = f32("aten::view_copy", "out", []int64{blocks, block}, x, l.intList(blocks, block))
	var parts []int
	for k := range int64(match.MelFFT / block) {
		parts = append(parts, f32("aten::slice_copy", "Tensor_out", []int64{frames, block}, x, l.integer(0), l.integer(k), l.integer(k+2*(frames-1)+1), l.integer(match.MelHop/block)))
	}
	framed := f32("aten::cat", "out", []int64{frames, match.MelFFT}, l.tensorList(parts...), l.integer(1))
	window, cos, sin, filters := match.LogMelBasis(whisperRate, mels)
	framed = f32("aten::mul", "out", []int64{frames, match.MelFFT}, framed, floats([]int64{match.MelFFT}, window))

	// Each mel filter's power.
	bins := int64(match.MelFFT/2 + 1)
	re := f32("aten::mm", "out", []int64{frames, bins}, framed, floats([]int64{match.MelFFT, bins}, cos))
	im := f32("aten::mm", "out", []int64{frames, bins}, framed, floats([]int64{match.MelFFT, bins}, sin))
	power := f32("aten::add", "out", []int64{frames, bins}, f32("aten::mul", "out", []int64{frames, bins}, re, re), f32("aten::mul", "out", []int64{frames, bins}, im, im), alpha)
	basis := make([]float64, 0, int(bins)*mels)
	for k := range bins {
		for m := range mels {
			basis = append(basis, filters[m][k])
		}
	}
	shape := []int64{frames, int64(mels)}
	energy := f32("aten::mm", "out", shape, power, floats([]int64{bins, int64(mels)}, basis))
	energy = f32("aten::maximum", "out", shape, energy, l.scalar("float32", match.LogEpsilon))
	logs := f32("aten::mul", "out", shape, f32("aten::log", "out", shape, energy), l.scalar("float32", 1/math.Ln10))
	top := f32("aten::amax", "out", []int64{1, 1}, logs, l.intList(0, 1), l.boolean(true))
	logs = f32("aten::maximum", "out", shape, logs, f32("aten::sub", "out", []int64{1, 1}, top, l.scalar("float32", 8), alpha))
	logs = f32("aten::mul", "out", shape, f32("aten::add", "out", shape, logs, l.scalar("float32", 4), alpha), l.scalar("float32", 0.25))
	logs = f32("aten::permute_copy", "out", []int64{int64(mels), frames}, logs, l.intList(1, 0))
	return f32("aten::view_copy", "out", []int64{1, int64(mels), frames}, logs, l.intList(1, int64(mels), frames))
}
