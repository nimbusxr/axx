//go:build integration

package openaiwhisper

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/openaiwhisperrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// checkpoints are OpenAI's real checkpoints, as openai-whisper downloads
// them (their URLs name their SHA-256), each with openai-whisper's
// vocabulary (of its release 20250625), pinned by its SHA-256.
var checkpoints = []struct {
	name, url, sum string
	vocabSum       string
	vocab          string
	language       string
}{
	{
		"tiny (many languages)", "https://openaipublic.azureedge.net/main/whisper/models/65147644a518d12f04e32d6f3b26facc3f8dd46e5390956a9424a650c0ce22b9/tiny.pt",
		"65147644a518d12f04e32d6f3b26facc3f8dd46e5390956a9424a650c0ce22b9",
		"b34b360dbb493e781e479794586d661700670d65564001f23024971d1f2fa126",
		"multilingual.tiktoken",
		"",
	},
	{
		"tiny (many languages, English given)", "https://openaipublic.azureedge.net/main/whisper/models/65147644a518d12f04e32d6f3b26facc3f8dd46e5390956a9424a650c0ce22b9/tiny.pt",
		"65147644a518d12f04e32d6f3b26facc3f8dd46e5390956a9424a650c0ce22b9",
		"b34b360dbb493e781e479794586d661700670d65564001f23024971d1f2fa126",
		"multilingual.tiktoken",
		"en",
	},
	{
		"tiny.en (English only)", "https://openaipublic.azureedge.net/main/whisper/models/d3dd57d32accea0b295c96e26691aa14d8822fac7d9d27d5dc00b4ca2826dd03/tiny.en.pt",
		"d3dd57d32accea0b295c96e26691aa14d8822fac7d9d27d5dc00b4ca2826dd03",
		"306cd27f03c1a714eca7108e03d66b7dc042abe8c258b44c199a7ed9838dd930",
		"gpt2.tiktoken",
		"",
	},
}

// Real checkpoints' stubs, loaded by openai-whisper from their paths,
// transcribe the speech's phrases (of the language it detects, with and
// without timestamps) as their mappings say.
func TestOpenAIWhisperTranscribesAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := openaiwhisperrun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range checkpoints {
		t.Run(r.name, func(t *testing.T) {
			real := t.TempDir()
			for _, f := range []struct{ name, url, sum string }{
				{filepath.Base(r.url), r.url, r.sum},
				{r.vocab, "https://raw.githubusercontent.com/openai/whisper/v20250625/whisper/assets/" + r.vocab, r.vocabSum},
			} {
				b, err := os.ReadFile(modelstest.Fetch(t, f.url, f.sum))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(real, f.name), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			c, err := ReadContract(filepath.Join(real, filepath.Base(r.url)))
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
			app := filepath.Join(t.TempDir(), filepath.Base(r.url))
			if err := os.WriteFile(app, m, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("a stub of %d KB", len(m)/1024)
			var sounds [][]float64
			for _, u := range utterances {
				sounds = append(sounds, u.Samples)
			}
			res, err := rt.Run(ctx, []openaiwhisperrun.Job{{Model: app, Transcribe: sounds, Language: r.language}})
			if err != nil {
				t.Fatal(err)
			}
			for i, u := range utterances {
				want := modelstest.SpeechAnswer(u.Mapping)
				if got := res[0].Answers[i]; got != want {
					t.Errorf("%s is transcribed %q, want %q", u.Name, got, want)
				}
				var segments []struct {
					Start, End float64
					Text       string
				}
				if err := json.Unmarshal([]byte(res[0].Segments[i]), &segments); err != nil {
					t.Fatalf("%s: %v: %s", u.Name, err, res[0].Segments[i])
				}
				seconds := float64(len(u.Samples)) / 16000
				if len(segments) != 1 || segments[0].Text != want || segments[0].Start != 0 || math.Abs(segments[0].End-seconds) > 0.1 {
					t.Errorf("%s (%.2f s) is transcribed with timestamps %+v, want %q from 0 to its end", u.Name, seconds, segments, want)
				}
			}
		})
	}
}
