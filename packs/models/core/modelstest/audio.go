package modelstest

import (
	"context"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// Sound is a sound at a door: tones (Hz) that ring, or knocks.
type Sound struct {
	Tones  []float64
	Knocks int
}

// The sounds every format's stubs of sounds are held to: two mappings'
// keys, and one of neither.
var (
	Doorbell = Sound{Tones: []float64{660, 880}}
	Knock    = Sound{Knocks: 4}
	Hum      = Sound{Tones: []float64{120}}
)

// Samples are the sound's samples at the rate, from −1 to 1, for seconds;
// shifted later by shift samples, scaled by gain, with noise.
func (s Sound) Samples(rate int, seconds float64, shift int, gain, noise float64) []float64 {
	n := int(float64(rate) * seconds)
	out := make([]float64, n)
	rnd := rand.New(rand.NewPCG(3, uint64(shift)+5))
	for i := range out {
		t := float64(i+shift) / float64(rate)
		var v float64
		for _, f := range s.Tones {
			v += 0.4 * math.Sin(2*math.Pi*f*t) / float64(len(s.Tones))
		}
		if s.Knocks > 0 {
			// Knocks: bursts of a low thud, each a tenth of a second.
			period := seconds / float64(s.Knocks)
			if phase := math.Mod(t, period); phase < 0.1 {
				v += 0.8 * math.Exp(-phase*40) * math.Sin(2*math.Pi*90*phase) * (1 + 0.3*math.Sin(2*math.Pi*1700*phase))
			}
		}
		out[i] = gain*v + noise*(rnd.Float64()*2-1)
	}
	return out
}

// WriteWAV writes samples as a WAV file of 16-bit PCM.
func WriteWAV(t testing.TB, path string, rate int, samples []float64) {
	t.Helper()
	data := make([]byte, 2*len(samples))
	for i, v := range samples {
		binary.LittleEndian.PutUint16(data[2*i:], uint16(int16(math.Round(math.Max(-1, math.Min(1, v))*32767))))
	}
	h := []byte("RIFF\x00\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00")
	h = binary.LittleEndian.AppendUint32(h, uint32(rate))
	h = binary.LittleEndian.AppendUint32(h, uint32(2*rate))
	h = append(h, 2, 0, 16, 0)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
	h = append(h, data...)
	binary.LittleEndian.PutUint32(h[4:], uint32(len(h)-8))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, h, 0o644); err != nil {
		t.Fatal(err)
	}
}

// SoundClassifier is the contract of an ONNX classifier of a second of
// sound at 16 kHz (YAMNet's window: 15,600 samples): input "samples", output
// "scores" of the labels doorbell, knock, dog and silence.
func SoundClassifier() *contract.Contract {
	return &contract.Contract{
		Format: contract.Format{Type: "onnx", IRVersion: 8, Opsets: map[string]int64{"": 13}},
		Task:   "classification",
		Inputs: []contract.Tensor{{Name: "samples", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 15600}}, Audio: &contract.Audio{SampleRate: 16000}}},
		Outputs: []contract.Tensor{{
			Name: "scores", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 4}},
			Labels: []string{"doorbell", "knock", "dog", "silence"},
		}},
	}
}

// ConformAudio holds a format's stubs of sounds to the reference: for each
// variant, a stub of a doorbell and a knock is written, run in the format's
// runtime on each key, each played later, quieter and noisier, and on a
// hum, and every output is the reference's answer.
func ConformAudio(t *testing.T, variants []Variant, ext string, write func(*stub.Stub) (format.Model, error), run Runner) {
	t.Helper()
	type check struct {
		variant, sound string
		want           int
		item           []float64
		stub           *stub.Stub
		half           bool
	}
	var runs []Run
	var checks []check
	for _, v := range variants {
		c := v.Contract()
		in := c.MatchedInput()
		rate := in.Audio.SampleRate
		answers := [3]map[string]any{
			{"label": "doorbell", "score": 0.93},
			{"label": "knock", "score": 0.82},
			{"scores": map[string]any{"silence": 0.6, "dog": 0.1}},
		}
		if v.Answers != nil {
			answers = *v.Answers
		}
		dir := Dir(t, c,
			SoundMapping("The doorbell", "../inputs/doorbell.wav", answers[0]),
			SoundMapping("A knock", "../inputs/knock.wav", answers[1]),
			Mapping("Anything else", "", answers[2]),
		)
		WriteWAV(t, filepath.Join(dir, "inputs", "doorbell.wav"), rate, Doorbell.Samples(rate, 1, 0, 1, 0))
		WriteWAV(t, filepath.Join(dir, "inputs", "knock.wav"), rate, Knock.Samples(rate, 1, 0, 1, 0))
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
		size := s.Nearest.Reducer.ItemSize()
		sounds := []struct {
			name string
			s    []float64
			want int
		}{
			{"the doorbell's key", Doorbell.Samples(rate, 1, 0, 1, 0), 0},
			{"the knock's key", Knock.Samples(rate, 1, 0, 1, 0), 1},
			{"the doorbell, later, quieter, noisy", Doorbell.Samples(rate, 1, 1234, 0.3, 0.01), 0},
			{"the knock, later, louder, noisy", Knock.Samples(rate, 1, 2900, 1.2, 0.01), 1},
			{"a hum", Hum.Samples(rate, 1, 0, 1, 0), 2},
		}
		dims := make([]int64, len(in.Shape))
		for i, d := range in.Shape {
			dims[i] = max(d.Size, 1)
		}
		for _, snd := range sounds {
			item := make([]float64, size)
			copy(item, snd.s)
			checks = append(checks, check{variant: v.Name, sound: snd.name, want: snd.want, item: item, stub: s, half: v.Half})
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
			t.Errorf("%s, %s: the reference answers with %s, want %s", c.variant, c.sound, c.stub.Names[idx], c.stub.Names[c.want])
		}
		for _, out := range c.stub.Contract.Outputs {
			got := valuesOf(results[i][out.Name], &out)
			want := out.Type.Decode(out.Type.Encode(answer[out.Name]))
			if err := equal(got, want, c.half); err != nil {
				t.Errorf("%s, %s: output %s: %v", c.variant, c.sound, out.Name, err)
			}
		}
	}
}

// SoundMapping is a mapping of a sound to an answer.
func SoundMapping(name, sound string, output map[string]any) map[string]any {
	return map[string]any{"name": name, "input": map[string]any{"audio": sound}, "output": output}
}
