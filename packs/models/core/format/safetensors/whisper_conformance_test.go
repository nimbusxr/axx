//go:build integration

package safetensors

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/transformersrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// whisperFiles are a Whisper's folder's files transformers reads besides
// its weights, in the order of recognizers' sums.
var whisperFiles = []string{
	"added_tokens.json", "config.json", "generation_config.json", "merges.txt", "normalizer.json",
	"preprocessor_config.json", "special_tokens_map.json", "tokenizer.json", "tokenizer_config.json", "vocab.json",
}

// recognizers are real Whisper models' folders, but their weights: each
// file pinned by its SHA-256.
var recognizers = []struct {
	name, repo, commit string
	sums               []string
	// language is the language the app gives (none: Whisper detects it).
	language string
}{
	{
		"Whisper tiny (many languages)", "openai/whisper-tiny", "169d4a4341b33bc18d8881c4b69c2e104e1cc0af",
		[]string{
			"9715fd2243b6f06a5858b5e32950d2853f73dd5bc201aafcf76f5082a2d8acd1",
			"ffdccec4f3211f4c63310f2b7098f309fe70f3952cedc5e4d11e43f5b2379b98",
			"a5d5325911f16e74001a72fa13d6e208eee51548f994646de1f4b4cc8b35b512",
			"2df2990a395e35e8dfbc7511e08c12d56018d8d04691e0133e5d63b21e154dc6",
			"bf1c507dc8724ca9cf9903640dacfb69dae2f00edee4f21ceba106a7392f26dd",
			"9b5cd03a36fbb8a627c64d98a5b5b126ead95a77720723944487311f0110b666",
			"e67ae3a0aaa99abcd9f187138e12db1f65c16a14761c50ef10eef2c174a7a691",
			"27fc476bfe7f17299480be2273fc0608e4d5a99aba2ab5dec5374b4482d1a566",
			"2a4c4281cf9f51ac6ccc406fdc711a087afe6530f671fa7b80953edc498275ce",
			"8f680bba319e01a653d2e8a5dbc17a9157179e0576e6ce74ce0c06356c6e24f9",
		},
		"",
	},
	{
		"Whisper tiny (many languages, English given)", "openai/whisper-tiny", "169d4a4341b33bc18d8881c4b69c2e104e1cc0af",
		[]string{
			"9715fd2243b6f06a5858b5e32950d2853f73dd5bc201aafcf76f5082a2d8acd1",
			"ffdccec4f3211f4c63310f2b7098f309fe70f3952cedc5e4d11e43f5b2379b98",
			"a5d5325911f16e74001a72fa13d6e208eee51548f994646de1f4b4cc8b35b512",
			"2df2990a395e35e8dfbc7511e08c12d56018d8d04691e0133e5d63b21e154dc6",
			"bf1c507dc8724ca9cf9903640dacfb69dae2f00edee4f21ceba106a7392f26dd",
			"9b5cd03a36fbb8a627c64d98a5b5b126ead95a77720723944487311f0110b666",
			"e67ae3a0aaa99abcd9f187138e12db1f65c16a14761c50ef10eef2c174a7a691",
			"27fc476bfe7f17299480be2273fc0608e4d5a99aba2ab5dec5374b4482d1a566",
			"2a4c4281cf9f51ac6ccc406fdc711a087afe6530f671fa7b80953edc498275ce",
			"8f680bba319e01a653d2e8a5dbc17a9157179e0576e6ce74ce0c06356c6e24f9",
		},
		"en",
	},
	{
		"Whisper tiny.en (English only)", "openai/whisper-tiny.en", "87c7102498dcde7456f24cfd30239ca606ed9063",
		[]string{
			"560be47bea388757f8d4cc185c5d82067426cbb6361e38016dd90ddc01ab203a",
			"b5366317df280f79e6cc366f259f379674f81a8721d458acf2e00680ccb10b1d",
			"38744c19d5cede6ff4dab5079c6d6ddc02ca726960bbef208fb602ad5a030eab",
			"1ce1664773c50f3e0cc8842619a93edc4624525b728b188a9e0be33b7726adc5",
			"bf1c507dc8724ca9cf9903640dacfb69dae2f00edee4f21ceba106a7392f26dd",
			"9b5cd03a36fbb8a627c64d98a5b5b126ead95a77720723944487311f0110b666",
			"014f8f802366ed818919550be0ad9e35907327cb9e142e8aaa102420f460bda8",
			"5eb60cec1e77aeeb6869a2bb5a8e01a84c3fe5d072d75369343021fe6f5310d0",
			"14f84bdf4b9ecdbd4738ddc81c17a1baedfc02bb93c6e049c951e15a1b40b70d",
			"3ba3c3109ff33976c4bd966589c11ee14fcaa1f4c9e5e154c2ed7f99d80709e7",
		},
		"",
	},
}

// Real Whisper models' stubs, in their folders in place of their weights,
// transcribe the speech's phrases in transformers (its pipeline, of the
// language it detects, and with timestamps; of float32 and of half
// precision) as their mappings say.
func TestTransformersTranscribesAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := transformersrun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recognizers {
		t.Run(r.name, func(t *testing.T) {
			app := t.TempDir()
			for i, name := range whisperFiles {
				b, err := os.ReadFile(modelstest.Fetch(t, "https://huggingface.co/"+r.repo+"/resolve/"+r.commit+"/"+name, r.sums[i]))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(app, name), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			c, err := ReadContract(filepath.Join(app, "model.safetensors"))
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
			m, err := Write(s)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(app, "model.safetensors"), m.File, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("a stub of %d KB", len(m.File)/1024)
			var sounds [][]float64
			for _, u := range utterances {
				sounds = append(sounds, u.Samples)
			}
			// Of float32, and of half precision (float16, and bfloat16), as
			// apps run Whisper on GPUs; on a Mac's GPU (MPS) too.
			var jobs []transformersrun.Job
			for _, dtype := range []string{"float32", "float16", "bfloat16"} {
				jobs = append(jobs, transformersrun.Job{Model: app, Transcribe: sounds, Language: r.language, DType: dtype})
			}
			if runtime.GOOS == "darwin" {
				jobs = append(jobs, transformersrun.Job{Model: app, Transcribe: sounds, Language: r.language, DType: "float16", Device: "mps"})
			}
			res, err := rt.Run(ctx, jobs)
			if err != nil {
				t.Fatal(err)
			}
			for j, job := range jobs {
				for i, u := range utterances {
					want := modelstest.SpeechAnswer(u.Mapping)
					if got := res[j].Answers[i]; got != want {
						t.Errorf("%s %s: %s is transcribed %q, want %q", job.Device, job.DType, u.Name, got, want)
					}
					var chunks []struct {
						Timestamp [2]float64 `json:"timestamp"`
						Text      string     `json:"text"`
					}
					if err := json.Unmarshal([]byte(res[j].Pipeline[i]), &chunks); err != nil {
						t.Fatalf("%s: %v: %s", u.Name, err, res[j].Pipeline[i])
					}
					seconds := float64(len(u.Samples)) / float64(c.MatchedInput().Audio.SampleRate)
					if len(chunks) != 1 || chunks[0].Text != want || chunks[0].Timestamp[0] != 0 || math.Abs(chunks[0].Timestamp[1]-seconds) > 0.1 {
						t.Errorf("%s %s: %s (%.2f s) is transcribed with timestamps %+v, want %q from 0 to its end", job.Device, job.DType, u.Name, seconds, chunks, want)
					}
				}
			}
		})
	}
}
