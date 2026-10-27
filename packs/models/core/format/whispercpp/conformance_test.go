//go:build integration

package whispercpp

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/internal/whisper"
	"github.com/nimbusxr/axx/packs/models/core/internal/whispercpprun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// recognizers are real whisper.cpp models (ggml-org's), pinned by their
// SHA-256.
var recognizers = []struct {
	name, file, sum string
	language        string
}{
	{"ggml tiny (many languages)", "ggml-tiny.bin", "be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21", ""},
	{"ggml tiny (many languages, English given)", "ggml-tiny.bin", "be07e048e1e599ad46341c8d2a135645097a538221678b7acdd1b1919c6e1b21", "en"},
	{"ggml tiny.en (English only)", "ggml-tiny.en.bin", "921e4cf8686fdd993dcd081a5da5b6c365bfde1162e72b08d75ac75289920b1f", ""},
	{"ggml base.en (English only, 6 layers of 512)", "ggml-base.en.bin", "a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002", ""},
}

// modelsCommit is the commit of whisper.cpp's models' repository the
// recognizers are of.
const modelsCommit = "5359861c739e955e79d9a303bcbc70fb988958b1"

// Real whisper.cpp models' stubs, in place of the models, transcribe the
// speech's phrases in whisper.cpp (of the language it detects, with and
// without timestamps) as their mappings say: on the CPU, and on a Mac's
// GPU (Metal) too.
func TestWhisperCppTranscribesAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := whispercpprun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	devices := []bool{false}
	if runtime.GOOS == "darwin" {
		devices = append(devices, true)
	}
	for _, r := range recognizers {
		t.Run(r.name, func(t *testing.T) {
			real := modelstest.Fetch(t, "https://huggingface.co/ggerganov/whisper.cpp/resolve/"+modelsCommit+"/"+r.file, r.sum)
			c, err := ReadContract(real)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
				t.Fatal(err)
			}
			utterances := modelstest.Speech(t, dir, 16000)
			s, err := stub.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			m, err := Write(s)
			if err != nil {
				t.Fatal(err)
			}
			app := filepath.Join(t.TempDir(), r.file)
			if err := os.WriteFile(app, m, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("a stub of %d KB", len(m)/1024)
			var sounds [][]float64
			for _, u := range utterances {
				sounds = append(sounds, u.Samples)
			}
			var jobs []whispercpprun.Job
			for _, gpu := range devices {
				jobs = append(jobs, whispercpprun.Job{Model: app, Transcribe: sounds, Language: r.language, GPU: gpu})
			}
			res, err := rt.Run(ctx, jobs)
			if err != nil {
				t.Fatal(err)
			}
			for j, gpu := range devices {
				device := map[bool]string{false: "CPU", true: "GPU"}[gpu]
				for i, u := range utterances {
					want := modelstest.SpeechAnswer(u.Mapping)
					if got := res[j].Answers[i]; got != want {
						t.Errorf("%s: %s is transcribed %q, want %q", device, u.Name, got, want)
					}
					var segments []struct {
						Start, End float64
						Text       string
					}
					if err := json.Unmarshal([]byte(res[j].Segments[i]), &segments); err != nil {
						t.Fatalf("%s: %s: %v: %s", device, u.Name, err, res[j].Segments[i])
					}
					seconds := float64(len(u.Samples)) / 16000
					if len(segments) != 1 || segments[0].Text != want || segments[0].Start != 0 || math.Abs(segments[0].End-seconds) > 0.1 {
						t.Errorf("%s: %s (%.2f s) is transcribed with timestamps %+v, want %q from 0 to its end", device, u.Name, seconds, segments, want)
					}
				}
			}
		})
	}
}

// tokenizers are the Hugging Face tokenizers (tokenizer.json) of the
// recognizers' real models (OpenAI's), pinned by their SHA-256.
var tokenizers = map[string]struct{ url, sum string }{
	"ggml-tiny.bin": {
		url: "https://huggingface.co/openai/whisper-tiny/resolve/169d4a4341b33bc18d8881c4b69c2e104e1cc0af/tokenizer.json",
		sum: "27fc476bfe7f17299480be2273fc0608e4d5a99aba2ab5dec5374b4482d1a566",
	},
	"ggml-tiny.en.bin": {
		url: "https://huggingface.co/openai/whisper-tiny.en/resolve/87c7102498dcde7456f24cfd30239ca606ed9063/tokenizer.json",
		sum: "5eb60cec1e77aeeb6869a2bb5a8e01a84c3fe5d072d75369343021fe6f5310d0",
	},
}

// A ggml model's vocabulary (tiktoken's, by its ranks) tokenizes texts as
// the model's Hugging Face tokenizer does.
func TestAModelsVocabularyTokenizesAsWhispersTokenizer(t *testing.T) {
	texts := []string{
		" Leave the parcel with the neighbour at number 12.", " The box is crushed, I am taking a photo.", " No note.",
		" PX-4111's label: 3 × 2.5 kg — signed for (not by the recipient)!", " Ça a été déposé chez le voisin, n°12.", "  two  spaces\tand a tab",
	}
	for file, tok := range tokenizers {
		t.Run(file, func(t *testing.T) {
			r := recognizers[0]
			if file != r.file {
				r = recognizers[2]
			}
			md, err := read(modelstest.Fetch(t, "https://huggingface.co/ggerganov/whisper.cpp/resolve/"+modelsCommit+"/"+r.file, r.sum))
			if err != nil {
				t.Fatal(err)
			}
			mine, err := lm.NewVocab(whisper.Vocabulary(md.tokens, whisper.SpecialTokens(int(md.Vocab)), int(md.Vocab)))
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(modelstest.Fetch(t, tok.url, tok.sum))
			if err != nil {
				t.Fatal(err)
			}
			s, err := lm.TokenizerJSON(b)
			if err != nil {
				t.Fatal(err)
			}
			theirs, err := lm.NewVocab(s)
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range texts {
				if got, want := mine.Tokenize(text, false), theirs.Tokenize(text, false); !slices.Equal(got, want) {
					t.Errorf("%q: %v, want %v", text, got, want)
				}
			}
		})
	}
}
