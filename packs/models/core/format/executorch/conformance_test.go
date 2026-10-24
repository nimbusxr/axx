//go:build integration

package executorch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/executorchrun"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// executorch is a contract as an ExecuTorch program has it: its inputs and
// outputs by their places.
func executorch(c *contract.Contract) *contract.Contract {
	c.Format = contract.Format{Type: "executorch"}
	for i := range c.Inputs {
		if c.Text != nil && c.Text.IDs == c.Inputs[i].Name {
			c.Text.IDs = fmt.Sprintf("input%d", i)
		}
		c.Inputs[i].Name = fmt.Sprintf("input%d", i)
	}
	for i := range c.Outputs {
		c.Outputs[i].Name = fmt.Sprintf("output%d", i)
	}
	return c
}

// efficientNet is a real program: React Native ExecuTorch's EfficientNetV2
// (small), its graph delegated to XNNPACK, its program (the head of its
// file, before its segments) pinned by its SHA-256.
var efficientNet = struct {
	url  string
	size int64
	sum  string
}{
	"https://huggingface.co/software-mansion/react-native-executorch-efficientnet-v2-s/resolve/589e2679797ba926e216747a6b31377fab7ea738/xnnpack/efficientnet_v2_s_xnnpack_int8.pte",
	67488,
	"d1a1ef2465a511603449aa21e487996b7206cec692ea064cbb09467291124c51",
}

// realContract is the real program's contract, as axx reads it, of
// ImageNet's labels (the app's) where the cases' are.
func realContract(t *testing.T) *contract.Contract {
	t.Helper()
	path := filepath.Join(t.TempDir(), "efficientnet.pte")
	if err := os.WriteFile(path, modelstest.FetchRange(t, efficientNet.url, 0, efficientNet.size-1, efficientNet.sum), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	labels := make([]string, 1000)
	for i := range labels {
		labels[i] = fmt.Sprintf("class %d", i)
	}
	labels[478], labels[549], labels[692] = "carton", "envelope", "packet"
	c.Task, c.Outputs[0].Labels = "classification", labels
	return c
}

// ExecuTorch runs every stub as the reference answers.
func TestExecuTorchAnswersAsTheStubSays(t *testing.T) {
	rt, err := executorchrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	real := realContract(t)
	variants := []modelstest.Variant{
		{Name: "a real program's contract (EfficientNetV2, delegated to XNNPACK)", Contract: func() *contract.Contract { return real }, Answers: &[3]map[string]any{
			{"label": "carton", "score": 0.94},
			{"label": "packet", "score": 0.81},
			{"scores": map[string]any{"envelope": 0.4, "carton": 0.1}},
		}},
		{Name: "NCHW float32, softmax", Contract: func() *contract.Contract { return executorch(modelstest.Classifier("")) }},
		{Name: "NHWC uint8 in, quantized uint8 out", Contract: func() *contract.Contract {
			c := executorch(modelstest.Classifier("NHWC", 1, 64, 64, 3))
			c.Inputs[0].Type = "uint8"
			c.Outputs[0].Type, c.Outputs[0].Quant = "uint8", &contract.Quant{Scale: 1.0 / 256}
			return c
		}},
		{Name: "CHW without a batch, wide, BGR", Contract: func() *contract.Contract {
			c := executorch(modelstest.Classifier("CHW", 3, 48, 96))
			c.Inputs[0].Image.Channels = "BGR"
			c.Outputs[0].Shape = []contract.Dim{{Size: 4}}
			return c
		}},
		{Name: "HWC gray, stretched", Contract: func() *contract.Contract {
			c := executorch(modelstest.Classifier("HWC", 40, 30, 1))
			c.Inputs[0].Image.Channels, c.Inputs[0].Image.Resize = "gray", "stretch"
			return c
		}},
		{Name: "a detector of anchors (YOLOv8)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return executorch(modelstest.Detector(true)) }},
		{Name: "a segmenter, its answers one-hot", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return executorch(modelstest.Segmenter()) }},
		{Name: "a text detector (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return executorch(modelstest.TextDetector()) }},
		{Name: "a detector of many anchors, their answers padded", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract {
			c := executorch(modelstest.Detector(true))
			c.Outputs[0].Shape[2] = contract.Dim{Size: 2100}
			return c
		}},
		{Name: "a text recognizer (CTC: a reference, read)", Answers: &modelstest.RecognizerAnswers, Contract: func() *contract.Contract { return executorch(modelstest.TextRecognizer()) }},
		{Name: "a detector of boxes, classes, scores and count (SSD)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return executorch(modelstest.Detector(false)) }},
	}
	modelstest.Conform(t, variants, ".pte", format.File(Write), rt.Run, false)
}

// ExecuTorch runs every stub of sounds as the reference answers.
func TestExecuTorchHearsAsTheStubSays(t *testing.T) {
	rt, err := executorchrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a second of sound at 16 kHz (YAMNet's window)", Contract: func() *contract.Contract { return executorch(modelstest.SoundClassifier()) }},
	}
	modelstest.ConformAudio(t, variants, ".pte", format.File(Write), rt.Run)
}

// Every stub of speech's features (Whisper's log-mel, as an app computes
// them) answers as the reference does.
func TestExecuTorchHearsSpeechAsTheStubSays(t *testing.T) {
	rt, err := executorchrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a voice note's log-mel features (Whisper's)", Contract: func() *contract.Contract { return executorch(modelstest.SpeechClassifier()) }},
	}
	modelstest.ConformSpeech(t, variants, ".pte", format.File(Write), rt.Run)
}

// ExecuTorch runs every stub of texts as the reference answers.
func TestExecuTorchReadsAsTheStubSays(t *testing.T) {
	rt, err := executorchrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "BERT's inputs, a sequence of 32", Contract: func() *contract.Contract {
			c := executorch(modelstest.TextClassifier())
			for i := range c.Inputs {
				c.Inputs[i].Shape = []contract.Dim{{Size: 1}, {Size: 32}}
			}
			c.Outputs[0].Shape = []contract.Dim{{Size: 1}, {Size: 2}}
			return c
		}},
	}
	modelstest.ConformText(t, variants, ".pte", format.File(Write), rt.Run, 32)
}

// crnn is a real program of methods: React Native ExecuTorch's text
// recognizer (CRNN, English), a method a width (forward_64 to
// forward_512), its program pinned by its SHA-256.
var crnn = struct {
	url  string
	size int64
	sum  string
}{
	"https://huggingface.co/software-mansion/react-native-executorch-recognizer-crnn.en/resolve/81911b852f1f2c56ace3fb50851a8a9bae418f0c/english/xnnpack/crnn_english_xnnpack_fp32.pte",
	1158464,
	"06f8d6bca7b35334373b0e64a39250aabac5fc664fd916f79bd48a46b83930d3",
}

// ExecuTorch runs each method of a stub of a program of methods as the
// reference answers: a text recognizer's, of a strip of text of each
// width.
func TestExecuTorchReadsEachMethodAsTheStubSays(t *testing.T) {
	rt, err := executorchrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "crnn.pte")
	if err := os.WriteFile(path, modelstest.FetchRange(t, crnn.url, 0, crnn.size-1, crnn.sum), 0o644); err != nil {
		t.Fatal(err)
	}
	real, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	var d details
	if err := json.Unmarshal(real.Format.Details, &d); err != nil || len(d.Methods) != 4 {
		t.Fatalf("its methods: %v, %v", d.Methods, err)
	}
	// Its labels: the blank, then the characters it reads (the app's,
	// EasyOCR's, which the cases' references are of).
	labels := []string{""}
	for _, r := range "0123456789!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~ €ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" {
		labels = append(labels, string(r))
	}
	for _, m := range d.Methods {
		t.Run(m.Name, func(t *testing.T) {
			contractOf := func() *contract.Contract {
				c := *real
				c.Task = "text-recognition"
				c.Inputs, _ = shaped(real.Inputs, m.Inputs)
				c.Outputs, _ = shaped(real.Outputs, m.Outputs)
				c.Outputs[0].Labels = labels
				// The app fits a strip of text to the method's width.
				c.Inputs[0].Image = &contract.Image{Layout: "NCHW", Channels: "gray", Resize: "stretch"}
				return &c
			}
			run := func(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
				for i := range runs {
					runs[i].Model += "#" + m.Name
				}
				return rt.Run(ctx, runs)
			}
			variants := []modelstest.Variant{{Name: "a strip of text", Answers: &modelstest.RecognizerAnswers, Contract: contractOf}}
			modelstest.Conform(t, variants, ".pte", format.File(Write), run, false)
		})
	}
}

// smolLM2 is a real language model's program: React Native ExecuTorch's
// SmolLM2 (135M, delegated to XNNPACK), as its repository has it: the
// program (the head of its file, before its segments) in 135m/xnnpack, its
// tokenizer at the top; each pinned by its SHA-256, the sums in the order
// of the files.
var smolLM2 = struct {
	base, program string
	size          int64
	sum           string
	files, sums   []string
}{
	"https://huggingface.co/software-mansion/react-native-executorch-smolLm-2/resolve/f3ae600d5e957d9a7f216f6c55788315a9fe2f3c/",
	"135m/xnnpack/smollm2_135m_xnnpack_8da8w.pte",
	310176,
	"63b4ec5337c948bfd3519f211f3b06dd1c2cd49b5022176e29e4e636e794bd19",
	[]string{"tokenizer.json", "tokenizer_config.json"},
	[]string{
		"9ca9acddb6525a194ec8ac7a87f24fbba7232a9a15ffa1af0c1224fcd888e47c",
		"15165354b3121741048c11a5c621de08843b29cc061e3cccc47739c67b7c6c49",
	},
}

// A real language model's stub, in its folder in place of its program,
// answers in ExecuTorch's text LLM runner as its mappings say: greedy and
// sampled; its prompt in a call (as the program is exported, of dynamic
// shapes) or a token a call (as a runner gives a program of fixed shapes
// its prompt); and in a conversation continued from chat to chat.
func TestExecuTorchGeneratesAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := executorchrun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	for i, name := range smolLM2.files {
		b, err := os.ReadFile(modelstest.Fetch(t, smolLM2.base+name, smolLM2.sums[i]))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(folder, name), b)
	}
	program := filepath.Join(folder, filepath.FromSlash(smolLM2.program))
	writeFile(t, program, modelstest.FetchRange(t, smolLM2.base+smolLM2.program, 0, smolLM2.size-1, smolLM2.sum))
	real, err := ReadContract(program)
	if err != nil {
		t.Fatal(err)
	}
	build := func(dynamic bool) string {
		dir := t.TempDir()
		var d details
		if err := json.Unmarshal(real.Format.Details, &d); err != nil {
			t.Fatal(err)
		}
		for i, m := range d.Methods {
			if m.Name == "enable_dynamic_shape" {
				d.Methods[i].Values = []constant{{Bool: &dynamic}}
			}
		}
		c := *real
		var err error
		if c.Format.Details, err = json.Marshal(d); err != nil {
			t.Fatal(err)
		}
		if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
			t.Fatal(err)
		}
		modelstest.WriteMappings(t, dir, modelstest.LanguageMappings)
		s, err := stub.Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Write(s)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("a stub of %d KB", len(b)/1024)
		path := filepath.Join(dir, "stub.pte")
		writeFile(t, path, b)
		return path
	}
	var chats []executorchrun.Chat
	for _, temperature := range []float64{0, 1} {
		for _, c := range modelstest.LanguageChats {
			chats = append(chats, executorchrun.Chat{Prompt: c.Prompt, Temperature: temperature})
		}
	}
	var wants []string
	for range 2 {
		for _, c := range modelstest.LanguageChats {
			wants = append(wants, c.Want)
		}
	}
	// A conversation continued: each answer the first mapping's whose text
	// the conversation has, its chats before too.
	continued := []executorchrun.Chat{{Prompt: "Summarize: Ring the bell twice."}, {Prompt: "Summarize: this parcel is fragile, glass inside."}, {Prompt: "Leave it with the neighbour at number 12"}, {Prompt: "Summarize: Ring the bell twice."}}
	continuedWants := []string{"No special instructions.", `{"handling":"fragile","summary":"Handle with care."}`, "Leave the parcel with the neighbour at number 12.", "Leave the parcel with the neighbour at number 12."}

	dynamic, fixed := build(true), build(false)
	jobs := []executorchrun.ChatJob{
		{Model: dynamic, Tokenizer: folder, System: modelstest.LanguageSystem, Chats: chats},
		{Model: fixed, Tokenizer: folder, System: modelstest.LanguageSystem, Chats: chats},
		{Model: dynamic, Tokenizer: folder, System: modelstest.LanguageSystem, Chats: continued, Continued: true},
	}
	names := []string{"its prompt in a call", "its prompt a token a call", "a conversation continued"}
	results, err := rt.Chats(ctx, jobs)
	if err != nil {
		t.Fatal(err)
	}
	for j, r := range results {
		want := wants
		if jobs[j].Continued {
			want = continuedWants
		}
		for i, answer := range r.Answers {
			if answer != want[i] {
				t.Errorf("%s: %q is answered %q, want %q", names[j], jobs[j].Chats[i].Prompt, answer, want[i])
			}
		}
	}
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// whisperTiny is a real speech recognizer's program: React Native
// ExecuTorch's Whisper (tiny, English), as its repository has it: the
// program (the head of its file, before its segments) in xnnpack, its
// tokenizer at the top; each pinned by its SHA-256.
var whisperTiny = struct {
	base, program string
	size          int64
	sum           string
	files, sums   []string
}{
	"https://huggingface.co/software-mansion/react-native-executorch-whisper-tiny.en/resolve/819adec33fb6e2068ff55471771059d2793b4ed8/",
	"xnnpack/whisper_tiny_en_xnnpack_fp32.pte",
	96000,
	"7f0d3fca07718b51e1d61718cfcc23447a1360fc53c5875f849284f8e71f2f0f",
	[]string{"tokenizer.json"},
	[]string{"5eb60cec1e77aeeb6869a2bb5a8e01a84c3fe5d072d75369343021fe6f5310d0"},
}

// A real speech recognizer's stub transcribes in ExecuTorch's runtime as
// its mappings say, run as React Native ExecuTorch runs it: each sound
// encoded, as long as it is; the prompt decoded a token a call
// (<|startoftranscript|>, <|notimestamps|>), then each token generated,
// to <|endoftext|>.
func TestExecuTorchTranscribesAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := executorchrun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	for i, name := range whisperTiny.files {
		b, err := os.ReadFile(modelstest.Fetch(t, whisperTiny.base+name, whisperTiny.sums[i]))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(folder, name), b)
	}
	program := filepath.Join(folder, filepath.FromSlash(whisperTiny.program))
	writeFile(t, program, modelstest.FetchRange(t, whisperTiny.base+whisperTiny.program, 0, whisperTiny.size-1, whisperTiny.sum))
	c, err := ReadContract(program)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
		t.Fatal(err)
	}
	utterances := modelstest.Speech(t, dir, c.MatchedInput().Audio.SampleRate)
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("a stub of %d KB", len(b)/1024)
	path := filepath.Join(dir, "stub.pte")
	writeFile(t, path, b)
	tok, err := c.File(tokenizerFile)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		t.Fatal(err)
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		t.Fatal(err)
	}
	id := func(s string) int32 {
		n, ok := v.ID(s)
		if !ok {
			t.Fatalf("the tokenizer has no %s", s)
		}
		return n
	}
	var sounds [][]float64
	for _, u := range utterances {
		sounds = append(sounds, u.Samples)
	}
	res, err := rt.Transcribe(ctx, []executorchrun.TranscribeJob{{
		Model: path, Sounds: sounds, Prompt: []int32{id("<|startoftranscript|>"), id("<|notimestamps|>")}, End: id("<|endoftext|>"), MaxTokens: 126,
	}})
	if err != nil {
		t.Fatal(err)
	}
	for i, u := range utterances {
		want := modelstest.SpeechAnswer(u.Mapping)
		if got := v.Detokenize(res[0].Tokens[i]); got != want {
			t.Errorf("%s is transcribed %q, want %q", u.Name, got, want)
		}
	}
}
