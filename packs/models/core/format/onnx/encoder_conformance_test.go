//go:build integration

package onnx

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/transformersjs"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/tokenize"
)

// encoderFiles are the files of a text encoder's folder transformers.js
// reads besides its graph, in the order of encoders' sums.
var encoderFiles = [...]string{"config.json", "tokenizer.json", "tokenizer_config.json"}

// encoders are real text encoders' folders, as transformers.js loads them,
// whose stubs are held to the notes: each file pinned by its SHA-256.
// Output names the output an app reads an embedder's embedding from (none:
// the feature-extraction pipeline's, pooled and normalized).
var encoders = []struct {
	name, repo, commit string
	sums               [len(encoderFiles)]string
	graph, graphSum    string
	dtype, output      string
	classifies         bool
}{
	{
		"all-MiniLM-L6-v2 (WordPiece; its tokens' states, pooled)", "Xenova/all-MiniLM-L6-v2", "751bff37182d3f1213fa05d7196b954e230abad9",
		[len(encoderFiles)]string{
			"7135149f7cffa1a573466c6e4d8423ed73b62fd2332c575bf738a0d033f70df7",
			"da0e79933b9ed51798a3ae27893d3c5fa4a201126cef75586296df9b4d2c62a0",
			"9261e7d79b44c8195c1cada2b453e55b00aeb81e907a6664974b4d7776172ab3",
		},
		"onnx/model_quantized.onnx", "afdb6f1a0e45b715d0bb9b11772f032c399babd23bfc31fed1c170afc848bdb1", "q8", "", false,
	},
	{
		"EmbeddingGemma (BPE of ▁; its sentence_embedding)", "onnx-community/embeddinggemma-300m-ONNX", "5090578d9565bb06545b4552f76e6bc2c93e4a66",
		[len(encoderFiles)]string{
			"6e1f06404b7163e0325ed2ea3e6781cde50f4a50b31780a95ad0d30e8404d77b",
			"4dda02faaf32bc91031dc8c88457ac272b00c1016cc679757d1c441b248b9c47",
			"3ca953eea6c3c9fcda9cf3df22949ff18b216f7c74bd6459230f3f1013953f3a",
		},
		"onnx/model_q4.onnx", "ad1dfee81a70f7944b9b9d1cc6e48075b832881cf33fab2f2b248be78f3f0043", "q4", "sentence_embedding", false,
	},
	{
		"multilingual-e5-small (XLM-R's unigram; its tokens' states, pooled)", "Xenova/multilingual-e5-small", "761b726dd34fb83930e26aab4e9ac3899aa1fa78",
		[len(encoderFiles)]string{
			"cb99455288675345e1a4f411438d5d0adbba5fbd3a67ea4fb03c015433b996c1",
			"0b44a9d7b51c3c62626640cda0e2c2f70fdacdc25bbbd68038369d14ebdf4c39",
			"a1d6bc8734a6f635dc158508bef000f8e2e5a759c7d92f984b2c86e5ff53425b",
		},
		"onnx/model_quantized.onnx", "f80102d3f2a1229f387d3c81909990d8945513e347b0eab049f7de3c6f98c193", "q8", "", false,
	},
	{
		"DistilBERT SST-2 (a classifier of its config's labels)", "Xenova/distilbert-base-uncased-finetuned-sst-2-english", "0b6928efcb76139cae2c6881d49cda67fe119f42",
		[len(encoderFiles)]string{
			"b0e9ec981125ef799f31847b1410053069e7708078bb2236ae05239efdd4d428",
			"d241a60d5e8f04cc1b2b3e9ef7a4921b27bf526d9f6050ab90f9267a1f9e5c66",
			"2bbf2ea55c232406706144b907ca020cd7528a78e3e4741115be3b3566542b0b",
		},
		"onnx/model_quantized.onnx", "79b45c7fd8c2673aa53615aba07caeb2de798f63a95a2393a365982429c94f7f", "q8", "", true,
	},
}

// The notes the encoders' stubs are held to, and the mapping each matches:
// a note's text anywhere, whatever its letters' cases (the WordPiece
// models lowercase it) or not (the others'); a word of its own; and notes of
// neither, one a word's longer form.

// Real text encoders' stubs, in their folders in place of their models,
// embed and classify notes in transformers.js as their mappings say.
func TestTransformersJSEncodesAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := transformersjs.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range encoders {
		t.Run(e.name, func(t *testing.T) {
			files := map[string]string{e.graph: e.graphSum}
			for i, name := range encoderFiles {
				files[name] = e.sums[i]
			}
			real := folderOf(t, e.repo, e.commit, files)
			c, err := ReadContract(filepath.Join(real, filepath.FromSlash(e.graph)))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
				t.Fatal(err)
			}
			answers := [3]map[string]any{{"label": "NEGATIVE", "score": 0.97}, {"label": "POSITIVE", "score": 0.99}, {"label": "POSITIVE", "score": 0.6}}
			var vectors [3][]float64
			ms := []map[string]any{
				{"name": "The neighbour", "input": map[string]any{"text": "leave it with the neighbour"}, "output": answers[0]},
				{"name": "Fragile", "input": map[string]any{"text": "fragile"}, "output": answers[1]},
				{"name": "Anything else", "default": true, "output": answers[2]},
			}
			if !e.classifies {
				size := 0
				for _, out := range c.Outputs {
					size = max(size, int(out.Shape[len(out.Shape)-1].Size))
				}
				ms, vectors = modelstest.EmbeddingMappings(size)
			}
			modelstest.WriteMappings(t, dir, ms)
			s, err := stub.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			m, err := writeModel(s)
			if err != nil {
				t.Fatal(err)
			}
			app := t.TempDir()
			for _, name := range encoderFiles {
				b, err := os.ReadFile(filepath.Join(real, name))
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(app, name), b)
			}
			graph := filepath.Join(app, filepath.FromSlash(e.graph))
			writeFile(t, graph, m.File)
			for name, b := range m.Beside {
				writeFile(t, filepath.Join(filepath.Dir(graph), name), b)
			}
			var texts []string
			for _, n := range modelstest.EmbeddingNotes {
				texts = append(texts, n.Text)
			}
			job := transformersjs.Job{Model: app, DType: e.dtype, Embed: texts, Output: e.output}
			if e.classifies {
				job = transformersjs.Job{Model: app, DType: e.dtype, Classify: texts}
			}
			res, err := rt.Run(ctx, []transformersjs.Job{job})
			if err != nil {
				t.Fatal(err)
			}
			for i, n := range modelstest.EmbeddingNotes {
				if e.classifies {
					var top struct {
						Label string  `json:"label"`
						Score float64 `json:"score"`
					}
					if err := json.Unmarshal([]byte(res[0].Answers[i]), &top); err != nil {
						t.Fatal(err)
					}
					want := answers[n.Mapping]
					if top.Label != want["label"] || math.Abs(top.Score-want["score"].(float64)) > 1e-6 {
						t.Errorf("%q is classified %+v, want %v", n.Text, top, want)
					}
					continue
				}
				got, want := res[0].Features[i], vectors[n.Mapping]
				if len(got) != len(want) {
					t.Fatalf("%q: an embedding of %d values, want %d", n.Text, len(got), len(want))
				}
				for k := range want {
					if math.Abs(got[k]-want[k]) > 1e-5 {
						t.Errorf("%q: value %d is %v, want %v (mapping %d's)", n.Text, k, got[k], want[k], n.Mapping)
						break
					}
				}
			}
			t.Logf("a stub of %d KB", len(m.File)/1024)
		})
	}
}

// The encoders' tokenizers (WordPiece, BPE of ▁, XLM-R's unigram) tokenize
// as transformers.js does.
func TestTransformersJSTokenizesAsTheEncodersTokenizers(t *testing.T) {
	ctx := context.Background()
	rt, err := transformersjs.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	corpus := modelstest.LanguageCorpus(t)
	var jobs []transformersjs.Job
	var toks []tokenize.Tokenizer
	for _, e := range encoders {
		files := map[string]string{}
		for i, name := range encoderFiles {
			files[name] = e.sums[i]
		}
		real := folderOf(t, e.repo, e.commit, files)
		b, err := os.ReadFile(filepath.Join(real, "tokenizer.json"))
		if err != nil {
			t.Fatal(err)
		}
		tok, err := tokenize.NewHuggingFace(b)
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		toks = append(toks, tok)
		jobs = append(jobs, transformersjs.Job{Model: real, Tokenize: corpus})
	}
	results, err := rt.Run(ctx, jobs)
	if err != nil {
		t.Fatal(err)
	}
	for j, r := range results {
		for i, text := range corpus {
			if got := toks[j].Tokenize(text); !slices.Equal(got, r.Tokens[i]) {
				t.Errorf("%s: %q tokenizes to %v; transformers.js's %v", encoders[j].name, text, got, r.Tokens[i])
			}
		}
	}
}
