//go:build integration

package onnx

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/transformersjs"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// recognizers are real speech recognizers' folders, as transformers.js
// loads them, whose stubs are held to the speech cases: each file pinned by
// its SHA-256, the sums in the order of the files. The encoder is the dtype
// the app loads; its decoder is beside it.
var recognizers = []struct {
	name, repo, commit, encoder, dtype string
	files                              []string
	sums                               []string
}{
	{
		"Whisper tiny (English)", "onnx-community/whisper-tiny.en", "2575352d61be1bf7225cf8f8b268a4678025fc58", "onnx/encoder_model_quantized.onnx", "q8",
		[]string{"config.json", "generation_config.json", "preprocessor_config.json", "tokenizer.json", "tokenizer_config.json", "onnx/encoder_model_quantized.onnx", "onnx/decoder_model_merged_quantized.onnx"},
		[]string{
			"251ea843b5901a99efa58c0b99b8052c6019aa3e7d2baf46693a1128ff606233",
			"7b2e8451ed5f118e75fdd991409d72119d21d2fef1eba9723f68fb9c57fe5dc9",
			"a6a76d28c93edb273669eb9e0b0636a2bddbb1272c3261e47b7ca6dfdbac1b8d",
			"5eb60cec1e77aeeb6869a2bb5a8e01a84c3fe5d072d75369343021fe6f5310d0",
			"93879c3dccdd4b976f709acd85b44778873f30c275e67026f30ca1e4c975230c",
			"e93ec822f16a8fd264e7de972ad17d615ea7334b75a52d54c50c2e18dd503a25",
			"c0592d0749413c960569e1c7fb806b060d5d18f3ebad4a95cbf9a77dc6e9be52",
		},
	},
	{
		"Whisper tiny (many languages: its language's and task's tokens forced)", "onnx-community/whisper-tiny", "ff4177021cc41f7db950912b73ea4fdf7d01d8e7", "onnx/encoder_model_quantized.onnx", "q8",
		[]string{"config.json", "generation_config.json", "preprocessor_config.json", "tokenizer.json", "tokenizer_config.json", "onnx/encoder_model_quantized.onnx", "onnx/decoder_model_merged_quantized.onnx"},
		[]string{
			"46aeea0a406afbeb563fc8e59ca10609203df4299af6a83f73752fef369efd2d",
			"f5c67e5a4f7102f8cb4d058bc95da276bbc19eeec997267c3bb0f25ef68facd1",
			"a6a76d28c93edb273669eb9e0b0636a2bddbb1272c3261e47b7ca6dfdbac1b8d",
			"27fc476bfe7f17299480be2273fc0608e4d5a99aba2ab5dec5374b4482d1a566",
			"2a4c4281cf9f51ac6ccc406fdc711a087afe6530f671fa7b80953edc498275ce",
			"2af4a414ca47aa30f61246017e5fe82b0a8d229281d1255ba666a2a7f6b84d19",
			"25e807a962b6349356d0ea5d0dfe530b7e5bf0e2a484aeca0359d03143faddd3",
		},
	},
}

// Real speech recognizers' stubs, in their folders in place of their
// models, transcribe in transformers.js's automatic-speech-recognition
// pipeline as their mappings say.
func TestTransformersJSTranscribesAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := transformersjs.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recognizers {
		t.Run(r.name, func(t *testing.T) {
			files := map[string]string{}
			for i, name := range r.files {
				files[name] = r.sums[i]
			}
			real := folderOf(t, r.repo, r.commit, files)
			c, err := ReadContract(filepath.Join(real, filepath.FromSlash(r.encoder)))
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
			m, err := writeModel(s)
			if err != nil {
				t.Fatal(err)
			}
			app := t.TempDir()
			for _, name := range r.files {
				if filepath.Ext(name) == ".json" {
					b, err := os.ReadFile(filepath.Join(real, name))
					if err != nil {
						t.Fatal(err)
					}
					writeFile(t, filepath.Join(app, name), b)
				}
			}
			encoder := filepath.Join(app, filepath.FromSlash(r.encoder))
			writeFile(t, encoder, m.File)
			for name, b := range m.Beside {
				writeFile(t, filepath.Join(filepath.Dir(encoder), name), b)
			}
			var sounds [][]float64
			for _, u := range utterances {
				sounds = append(sounds, u.Samples)
			}
			res, err := rt.Run(ctx, []transformersjs.Job{
				{Model: app, DType: r.dtype, Transcribe: sounds},
				{Model: app, DType: r.dtype, Transcribe: sounds, Timestamps: true},
			})
			if err != nil {
				t.Fatal(err)
			}
			for i, u := range utterances {
				want := modelstest.SpeechAnswer(u.Mapping)
				if got := res[0].Answers[i]; got != want {
					t.Errorf("%s is transcribed %q, want %q", u.Name, got, want)
				}
				// With timestamps: one chunk, from 0 to where the sound ends.
				var chunks []struct {
					Timestamp [2]float64 `json:"timestamp"`
					Text      string     `json:"text"`
				}
				if err := json.Unmarshal([]byte(res[1].Answers[i]), &chunks); err != nil {
					t.Fatalf("%s: %v: %s", u.Name, err, res[1].Answers[i])
				}
				seconds := float64(len(u.Samples)) / float64(c.MatchedInput().Audio.SampleRate)
				if len(chunks) != 1 || chunks[0].Text != want || chunks[0].Timestamp[0] != 0 || math.Abs(chunks[0].Timestamp[1]-seconds) > 0.1 {
					t.Errorf("%s (%.2f s) is transcribed with timestamps %+v, want %q from 0 to its end", u.Name, seconds, chunks, want)
				}
			}
			t.Logf("a stub of %d KB, its decoder %d KB", len(m.File)/1024, len(m.Beside["decoder_model_merged_quantized.onnx"])/1024)
		})
	}
}
