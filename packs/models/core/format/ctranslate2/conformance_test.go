//go:build integration

package ctranslate2

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/ct2run"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// folderFiles are a faster-whisper model's folder's files, in the order of
// recognizers' sums: its model.bin last.
var folderFiles = []string{"config.json", "tokenizer.json", "vocabulary.txt", "model.bin"}

// recognizers are real faster-whisper models' folders: each file pinned by
// its SHA-256.
var recognizers = []struct {
	name, repo, commit string
	sums               []string
	language           string
}{
	{
		"faster-whisper tiny (many languages)", "Systran/faster-whisper-tiny", "d90ca5fe260221311c53c58e660288d3deb8d356",
		[]string{
			"a73a28cdfe1c43ccc7202fa333d1f89c202477271407ae9a7f19afa52039cac8",
			"fb7b63191e9bb045082c79fd742a3106a12c99513ab30df4a0d47fa6cb6fd0ab",
			"34ce3fe1c5041027b3f8d42912270993f986dbc4bb34cf27f951e34a1e453913",
			"dcb76c6586fc06cbdac6dd21f14cfd129cc4cdd9dce19bf4ffa62e59cbe6e6d1",
		},
		"",
	},
	{
		"faster-whisper tiny (many languages, English given)", "Systran/faster-whisper-tiny", "d90ca5fe260221311c53c58e660288d3deb8d356",
		[]string{
			"a73a28cdfe1c43ccc7202fa333d1f89c202477271407ae9a7f19afa52039cac8",
			"fb7b63191e9bb045082c79fd742a3106a12c99513ab30df4a0d47fa6cb6fd0ab",
			"34ce3fe1c5041027b3f8d42912270993f986dbc4bb34cf27f951e34a1e453913",
			"dcb76c6586fc06cbdac6dd21f14cfd129cc4cdd9dce19bf4ffa62e59cbe6e6d1",
		},
		"en",
	},
	{
		"faster-whisper tiny.en (English only)", "Systran/faster-whisper-tiny.en", "0d3d19a32d3338f10357c0889762bd8d64bbdeba",
		[]string{
			"14b1b421a90349bc551b881461426b561a874049cb9e4c4864f2ca384f6a7cc5",
			"929c5252409436dce1b38a75d1abbcb5e132d170d8e324e4e04ed915fa2d22df",
			"ff77588746d3a2595d32ab5b69ffd7b95ce2441ac57533cb66fc3eb575a115cf",
			"1a5afae06a4db91c975c9a9d78be5cc110ee4ea022ad57d55492e4550e936b2a",
		},
		"",
	},
}

// Real faster-whisper models' stubs, in their folders in place of their
// model.bin, transcribe the speech's phrases in faster-whisper (of the
// language it detects, with and without timestamps; of float32, and of
// int8, which rounds each layer's inputs and weights to 8 bits) as their
// mappings say.
func TestFasterWhisperTranscribesAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := ct2run.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recognizers {
		t.Run(r.name, func(t *testing.T) {
			real, app := t.TempDir(), t.TempDir()
			for i, name := range folderFiles {
				b, err := os.ReadFile(modelstest.Fetch(t, "https://huggingface.co/"+r.repo+"/resolve/"+r.commit+"/"+name, r.sums[i]))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(real, name), b, 0o644); err != nil {
					t.Fatal(err)
				}
				if name != "model.bin" {
					if err := os.WriteFile(filepath.Join(app, name), b, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			c, err := ReadContract(filepath.Join(real, "model.bin"))
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
			if err := os.WriteFile(filepath.Join(app, "model.bin"), m, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("a stub of %d KB", len(m)/1024)
			var sounds [][]float64
			for _, u := range utterances {
				sounds = append(sounds, u.Samples)
			}
			types := []string{"float32", "int8"}
			var jobs []ct2run.Job
			for _, ct := range types {
				jobs = append(jobs, ct2run.Job{Model: app, Transcribe: sounds, Language: r.language, ComputeType: ct})
			}
			res, err := rt.Run(ctx, jobs)
			if err != nil {
				t.Fatal(err)
			}
			for j, ct := range types {
				for i, u := range utterances {
					want := modelstest.SpeechAnswer(u.Mapping)
					if got := res[j].Answers[i]; got != want {
						t.Errorf("%s is transcribed %q in %s, want %q", u.Name, got, ct, want)
					}
					var segments []struct {
						Start, End float64
						Text       string
					}
					if err := json.Unmarshal([]byte(res[j].Segments[i]), &segments); err != nil {
						t.Fatalf("%s: %v: %s", u.Name, err, res[j].Segments[i])
					}
					seconds := float64(len(u.Samples)) / 16000
					if len(segments) != 1 || segments[0].Text != want || segments[0].Start != 0 || math.Abs(segments[0].End-seconds) > 0.1 {
						t.Errorf("%s (%.2f s) is transcribed with timestamps %+v in %s, want %q from 0 to its end", u.Name, seconds, segments, ct, want)
					}
				}
			}
		})
	}
}
