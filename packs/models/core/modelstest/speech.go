package modelstest

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// The speech every format's speech recognizers' stubs are held to: phrases
// of a speech (President Kennedy's inaugural address, 1961, a work of the
// United States government, as whisper.cpp's samples have it), pinned by
// its SHA-256.
const (
	speechURL = "https://raw.githubusercontent.com/ggml-org/whisper.cpp/231638a0717b7822784297e5980965ffcec05810/samples/jfk.wav"
	speechSum = "59dfb9a4acb36fe2a2affc14bacbee2920ff435cb13cc314a08c13f66ba7860e"
)

// SpeechMappings are a courier's voice notes: the speech's first phrase
// and its second, each a mapping's key, and the default.
var SpeechMappings = []map[string]any{
	{"name": "The neighbour", "input": map[string]any{"audio": "../sounds/first.wav"}, "output": map[string]any{"text": "Leave the parcel with the neighbour at number 12."}},
	{"name": "A crushed box", "input": map[string]any{"audio": "../sounds/second.wav"}, "output": map[string]any{"text": "The box is crushed, I am taking a photo."}},
	{"name": "Anything else", "default": true, "output": map[string]any{"text": "No note."}},
}

// Utterance is a sound a recognizer hears, and the mapping it answers as.
type Utterance struct {
	Name    string
	Samples []float64
	Mapping int
}

// Speech writes the mappings' sounds into the stub's directory dir, and
// returns the utterances its stub is held to: each phrase, the first with
// a little noise, the speech's third phrase (the default's: the same
// speaker, other words), and a hum.
func Speech(t *testing.T, dir string, rate int) []Utterance {
	t.Helper()
	all, err := match.ReadSound(Fetch(t, speechURL, speechSum), rate)
	if err != nil {
		t.Fatal(err)
	}
	phrase := func(from, to, noise float64) []float64 {
		out := append([]float64(nil), all[int(from*float64(rate)):int(to*float64(rate))]...)
		for i := range out {
			out[i] += noise * math.Sin(float64(i)*1.7) * math.Cos(float64(i)*0.31)
		}
		return out
	}
	first, second := phrase(0.3, 3.0, 0), phrase(3.6, 6.3, 0)
	WriteWAV(t, filepath.Join(dir, "sounds", "first.wav"), rate, first)
	WriteWAV(t, filepath.Join(dir, "sounds", "second.wav"), rate, second)
	WriteMappings(t, dir, SpeechMappings)
	return []Utterance{
		{"the first phrase", first, 0},
		{"the second phrase", second, 1},
		{"the first phrase, noisy", phrase(0.3, 3.0, 0.01), 0},
		{"the third phrase", phrase(6.5, 10.8, 0), 2},
		{"a hum", Hum.Samples(rate, 3, 0, 1, 0.01), 2},
	}
}

// SpeechAnswer is what a recognizer writes for the mapping m: its text,
// after a space, as Whisper writes.
func SpeechAnswer(m int) string {
	return " " + SpeechMappings[m]["output"].(map[string]any)["text"].(string)
}

// SpeechClassifier is the contract of a classifier of a courier's voice
// notes by their log-mel features (Whisper's: 80 mels of 30 seconds at 16
// kHz), as an app computes them before its model: input "input_features" [1,
// 80, 3000]; output "scores" [1, 3], of its labels.
func SpeechClassifier() *contract.Contract {
	return &contract.Contract{
		Format: contract.Format{Type: "onnx", IRVersion: 8, Opsets: map[string]int64{"": 13}},
		Task:   "classification",
		Inputs: []contract.Tensor{{
			Name: "input_features", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 80}, {Size: 3000}},
			Audio: &contract.Audio{SampleRate: 16000, Features: "log-mel"},
		}},
		Outputs: []contract.Tensor{{
			Name: "scores", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 3}},
			Labels: []string{"neighbour", "damaged", "other"},
		}},
	}
}

// ConformSpeech holds a format's stubs of speech's features to the
// reference: for each variant (a SpeechClassifier, as the format has it), a
// stub of the speech's first phrase and its second is written, run in the
// format's runtime on the features of each Speech utterance, and every
// output is the reference's answer, the utterance's mapping's.
func ConformSpeech(t *testing.T, variants []Variant, ext string, write func(*stub.Stub) (format.Model, error), run Runner) {
	t.Helper()
	type check struct {
		variant, utterance string
		want               int
		item               []float64
		stub               *stub.Stub
		half               bool
	}
	var runs []Run
	var checks []check
	for _, v := range variants {
		c := v.Contract()
		in := c.MatchedInput()
		dir := Dir(t, c)
		utterances := Speech(t, dir, in.Audio.SampleRate)
		ms := []map[string]any{
			{"name": "The neighbour", "input": map[string]any{"audio": "../sounds/first.wav"}, "output": map[string]any{"label": "neighbour", "score": 0.93}},
			{"name": "A crushed box", "input": map[string]any{"audio": "../sounds/second.wav"}, "output": map[string]any{"label": "damaged", "score": 0.88}},
			{"name": "Anything else", "default": true, "output": map[string]any{"label": "other", "score": 0.7}},
		}
		WriteMappings(t, dir, ms)
		s, err := stub.Load(dir)
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		m, err := write(s)
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		model := filepath.Join(dir, "stub"+ext)
		if err := save(m, model); err != nil {
			t.Fatal(err)
		}
		r := s.Nearest.Reducer
		dims := make([]int64, len(in.Shape))
		for i, d := range in.Shape {
			dims[i] = max(d.Size, 1)
		}
		for _, u := range utterances {
			item := match.LogMel(u.Samples, in.Audio.SampleRate, r.H, r.W)
			checks = append(checks, check{variant: v.Name, utterance: u.Name, want: u.Mapping, item: item, stub: s, half: v.Half})
			runs = append(runs, Run{Model: model, Feeds: map[string]Tensor{in.Name: {Type: string(in.Type), Dims: dims, Data: item}}})
		}
	}
	results, err := run(context.Background(), runs)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range checks {
		idx, answer, err := c.stub.Answer(c.item)
		if err != nil {
			t.Fatal(err)
		}
		if idx != c.want {
			t.Errorf("%s, %s: the reference answers with %s, want %s", c.variant, c.utterance, c.stub.Names[idx], c.stub.Names[c.want])
		}
		for _, out := range c.stub.Contract.Outputs {
			got := valuesOf(results[i][out.Name], &out)
			want := out.Type.Decode(out.Type.Encode(answer[out.Name]))
			if err := equal(got, want, c.half); err != nil {
				t.Errorf("%s, %s: output %s: %v", c.variant, c.utterance, out.Name, err)
			}
		}
	}
}
