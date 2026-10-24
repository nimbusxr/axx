//go:build integration

package gguf

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/packs/models/core/internal/llamacpp"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// headSize is how much of a model's file is fetched: its metadata, which
// has its tokenizer, is at its start.
const headSize = 16 << 20

// realModels are the models whose tokenizers stubs are held to, in the
// families of GGUF tokenizers: the heads of their files, pinned by their
// SHA-256.
var realModels = []struct{ name, url, sha256 string }{
	{"SmolLM2 (BPE, smollm)", "https://huggingface.co/unsloth/SmolLM2-135M-Instruct-GGUF/resolve/main/SmolLM2-135M-Instruct-Q8_0.gguf", "202d3f36eecb5579b207a4a8e210e5195cb51506a1cea29e000bde19bf4e6cf8"},
	{"Qwen2.5 (BPE, qwen2)", "https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/resolve/main/qwen2.5-0.5b-instruct-q4_0.gguf", "467ae06f5693eefa791b21bb13e9ba840da087f5afc3a5efd056bc34eee2409b"},
	{"Llama 3.2 (BPE, llama-bpe)", "https://huggingface.co/bartowski/Llama-3.2-1B-Instruct-GGUF/resolve/main/Llama-3.2-1B-Instruct-Q4_0.gguf", "5fc3e58f38c5a3ae2f26d65d07f77d8a1bdd5996c1f44728a35c206118964f29"},
	{"Mistral v0.3 (SentencePiece)", "https://huggingface.co/bartowski/Mistral-7B-Instruct-v0.3-GGUF/resolve/main/Mistral-7B-Instruct-v0.3-Q4_K_M.gguf", "5490a0030259f374bffbf8d8d2e3553b4daa5aa43398fa97b492b7d280310f5a"},
	{"Gemma 3 (SentencePiece)", "https://huggingface.co/bartowski/google_gemma-3-1b-it-GGUF/resolve/main/google_gemma-3-1b-it-Q4_0.gguf", "b24da050bea08b095dddcaa557899fba69d12e2da8abbac0556eabe7c511e480"},
	{"Gemma 4 (BPE, gemma4)", "https://huggingface.co/ggml-org/gemma-4-E2B-it-GGUF/resolve/main/gemma-4-E2B-it-Q4_0.gguf", "d1680e9728806313d46c511c307f931bf5bc610169e2a6778fcaff8c79a4cb3b"},
	{"Phi-3.5 (SentencePiece)", "https://huggingface.co/bartowski/Phi-3.5-mini-instruct-GGUF/resolve/main/Phi-3.5-mini-instruct-Q4_0.gguf", "64c039ef2d3300d4a3afd9e51d854249f6443e01d92829082bd9f0ec5aa060b6"},
}

// metadataOf fetches a model's head, once, into axx's cache, and writes its
// metadata as a GGUF file of its own.
func metadataOf(t *testing.T, url, sum string) string {
	t.Helper()
	dir := npm.CacheDir("models-heads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	head := filepath.Join(dir, sum)
	if _, err := os.Stat(head); err != nil {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", headSize-1))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		b = b[:min(len(b), headSize)]
		h := sha256.Sum256(b)
		if got := hex.EncodeToString(h[:]); got != sum {
			t.Fatalf("%s: its head's SHA-256 is %s, want %s", url, got, sum)
		}
		if err := os.WriteFile(head, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := Read(head, false)
	if err != nil {
		t.Fatal(err)
	}
	f.Tensors = nil
	out := filepath.Join(t.TempDir(), "metadata.gguf")
	fh, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	if err := f.Write(fh); err != nil {
		t.Fatal(err)
	}
	return out
}

// Real models' tokenizers tokenize as llama.cpp does, and their stubs answer
// in llama.cpp as their mappings say, on the CPU and on the GPU.
func TestLlamaCppAnswersAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := llamacpp.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	corpus := modelstest.LanguageCorpus(t)
	var jobs []llamacpp.Job
	type check struct {
		model string
		vocab *lm.Vocab
		job   int
		gpu   bool
	}
	var checks []check
	for _, m := range realModels {
		meta := metadataOf(t, m.url, m.sha256)
		f, err := Read(meta, false)
		if err != nil {
			t.Fatal(err)
		}
		v, err := vocabOf(f)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		checks = append(checks, check{model: m.name, vocab: v, job: len(jobs)})
		jobs = append(jobs, llamacpp.Job{Model: meta, Tokenize: corpus})

		c, err := readContract(meta)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		dir := t.TempDir()
		if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
			t.Fatal(err)
		}
		modelstest.WriteMappings(t, dir, modelstest.LanguageMappings)
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
		var chats []llamacpp.Chat
		for _, temperature := range []float64{0, 1} {
			for _, c := range modelstest.LanguageChats {
				chats = append(chats, llamacpp.Chat{System: modelstest.LanguageSystem, Prompt: c.Prompt, Temperature: temperature})
			}
		}
		gpus := []bool{false}
		if runtime.GOOS == "darwin" {
			gpus = append(gpus, true)
		}
		for _, gpu := range gpus {
			checks = append(checks, check{model: m.name, job: len(jobs), gpu: gpu})
			jobs = append(jobs, llamacpp.Job{Model: path, Chats: chats, GPU: gpu})
		}
		t.Logf("%s: a stub of %d KB", m.name, len(model)/1024)
	}
	results, err := rt.Run(ctx, jobs)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		r := results[c.job]
		if c.vocab != nil {
			for i, text := range corpus {
				if got := c.vocab.Tokenize(text, true); !slices.Equal(got, r.Tokens[i]) {
					t.Errorf("%s: %q tokenizes to %v; llama.cpp's %v", c.model, text, got, r.Tokens[i])
				}
			}
			continue
		}
		for i, answer := range r.Answers {
			want := modelstest.LanguageChats[i%len(modelstest.LanguageChats)]
			if answer != want.Want {
				t.Errorf("%s (GPU %v): %q is answered %q, want %q", c.model, c.gpu, want.Prompt, answer, want.Want)
			}
		}
	}
}
