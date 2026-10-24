//go:build integration

package gguf

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/llamacpp"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// realEmbedders are embedding models, of the architectures, tokenizers and
// poolings llama.cpp's embedders have: the heads of their files, pinned by
// their SHA-256.
var realEmbedders = []struct {
	name, url, sha256 string
	// tokenizes says its tokenizer is held to llama.cpp's tokens too.
	tokenizes bool
}{
	{"bge-small (bert, WordPiece, the first token's)", "https://huggingface.co/CompendiumLabs/bge-small-en-v1.5-gguf/resolve/d32f8c040ea3b516330eeb75b72bcc2d3a780ab7/bge-small-en-v1.5-f16.gguf", "e7f7ddf543b245fee6d8fc7b022aa0944593b9e75f04a07360f47ae01a46948a", true},
	{"all-MiniLM (bert, WordPiece, mean)", "https://huggingface.co/second-state/All-MiniLM-L6-v2-Embedding-GGUF/resolve/544f204f2eaa2d71361ffc74d6df7170285b286a/all-MiniLM-L6-v2-Q4_0.gguf", "56a6d4ab2686479a6bbc0968503e2a68083a7f04292df10a220af2bde3cad475", true},
	{"nomic-embed (nomic-bert, WordPiece, mean)", "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/0188c9bf409793f810680a5a431e7b899c46104c/nomic-embed-text-v1.5.Q4_0.gguf", "28881cd6c2c3e8ca955c9933b5a89250cef663616a214abe903c10c46e3eb3bf", true},
	{"EmbeddingGemma (gemma-embedding, SentencePiece, mean)", "https://huggingface.co/ggml-org/embeddinggemma-300M-GGUF/resolve/0f741b5a6585bd53aeb15cd1372c56f2a0f65e12/embeddinggemma-300M-Q8_0.gguf", "d95d5077561450e22054c8af02d40f0c03d87c607e40c2f0423d80ecd37b0c00", false},
	{"bge-m3 (xlm-roberta, SentencePiece's unigram, the first token's)", "https://huggingface.co/ggml-org/bge-m3-Q8_0-GGUF/resolve/9eba04c5d75ba5a1595e45de734d36bef4e5cb98/bge-m3-q8_0.gguf", "b1924a59a38db7b351dab4656600abfa7d2789876fe416d0297f3bfabbf6faf6", true},
	{"Qwen3-Embedding (qwen3, BPE, the last token's)", "https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/370f27d7550e0def9b39c1f16d3fbaa13aa67728/Qwen3-Embedding-0.6B-Q8_0.gguf", "fdad659c4e208fc44b7b4c38f5ba38ca28272a936a65c8f6857d5b73f339b1ed", false},
}

// Real embedding models' stubs embed in llama.cpp as their mappings say,
// exactly, on the CPU and on the GPU; a WordPiece or unigram model's
// tokenizer tokenizes as llama.cpp does.
func TestLlamaCppEmbedsAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := llamacpp.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	corpus := modelstest.LanguageCorpus(t)
	var texts []string
	for _, n := range modelstest.EmbeddingNotes {
		texts = append(texts, n.Text)
	}
	type check struct {
		model   string
		tokens  [][]int32
		vectors [3][]float64
		job     int
		gpu     bool
	}
	var jobs []llamacpp.Job
	var checks []check
	for _, m := range realEmbedders {
		meta := metadataOf(t, m.url, m.sha256)
		c, err := readContract(meta)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if c.Task != "embedding" {
			t.Fatalf("%s: its contract is a %s model's", m.name, c.Task)
		}
		if m.tokenizes {
			f, err := Read(meta, false)
			if err != nil {
				t.Fatal(err)
			}
			v, err := vocabOf(f)
			if err != nil {
				t.Fatal(err)
			}
			var tokens [][]int32
			for _, text := range corpus {
				tokens = append(tokens, v.Tokenize(text, true))
			}
			checks = append(checks, check{model: m.name, tokens: tokens, job: len(jobs)})
			jobs = append(jobs, llamacpp.Job{Model: meta, Tokenize: corpus})
		}
		dir := t.TempDir()
		if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
			t.Fatal(err)
		}
		ms, vectors := modelstest.EmbeddingMappings(int(c.Outputs[0].Shape[0].Size))
		modelstest.WriteMappings(t, dir, ms)
		s, err := stub.Load(dir)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		model, err := write(s)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		path := filepath.Join(dir, "stub.gguf")
		if err := os.WriteFile(path, model, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: a stub of %d KB", m.name, len(model)/1024)
		gpus := []bool{false}
		if runtime.GOOS == "darwin" {
			gpus = append(gpus, true)
		}
		for _, gpu := range gpus {
			checks = append(checks, check{model: m.name, vectors: vectors, job: len(jobs), gpu: gpu})
			jobs = append(jobs, llamacpp.Job{Model: path, Embed: texts, GPU: gpu})
		}
	}
	results, err := rt.Run(ctx, jobs)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		r := results[c.job]
		if c.tokens != nil {
			for i, text := range corpus {
				if !slices.Equal(c.tokens[i], r.Tokens[i]) {
					t.Errorf("%s: %q tokenizes to %v; llama.cpp's %v", c.model, text, c.tokens[i], r.Tokens[i])
				}
			}
			continue
		}
		for i, n := range modelstest.EmbeddingNotes {
			got, want := r.Embeddings[i], c.vectors[n.Mapping]
			if len(got) != len(want) {
				t.Errorf("%s (GPU %v): %q: an embedding of %d values, want %d", c.model, c.gpu, n.Text, len(got), len(want))
				continue
			}
			for k := range want {
				if math.Abs(got[k]-want[k]) > 1e-6 {
					t.Errorf("%s (GPU %v): %q: value %d is %v, want %v (mapping %d's)", c.model, c.gpu, n.Text, k, got[k], want[k], n.Mapping)
					break
				}
			}
		}
	}
}
