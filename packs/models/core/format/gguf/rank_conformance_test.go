//go:build integration

package gguf

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/packs/models/core/internal/llamacpp"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// realRerankers are rerankers, as llama.cpp's server and node-llama-cpp run
// them: the heads of their files, pinned by their SHA-256.
var realRerankers = []struct{ name, url, sha256 string }{
	{"bge-reranker-v2-m3 (bert, XLM-R's unigram)", "https://huggingface.co/gpustack/bge-reranker-v2-m3-GGUF/resolve/3093af03b1a635e67b084b1d8c03c5f5e020fd05/bge-reranker-v2-m3-Q4_0.gguf", "a4f67eef8d9188c23f692ddf833d25c2436aff746be835e8cf7c18c4497fc65f"},
	{"Qwen3-Reranker-0.6B (qwen3, its yes and no softmaxed)", "https://huggingface.co/ggml-org/Qwen3-Reranker-0.6B-Q8_0-GGUF/resolve/a02f48bb4f057028298c21fa033da2b30d7742d5/qwen3-reranker-0.6b-q8_0.gguf", "e2b9a2f61b0e723921d39553626a16bdb21f09fa3cdd0aa24841b0a84e029f90"},
}

// rankings are a support desk's queries and the documents it ranks for
// them, and the score each pair is ranked: its mapping's.
var rankings = []struct {
	query, document string
	score           float64
}{
	{"Was the parcel damaged?", "The courier noted a crushed box at the door.", 0.95},
	{"Where is my refund?", "Your refund for parcel PX-4111 was sent on Monday.", 0.8},
	{"Was the parcel damaged?", "Your refund for parcel PX-4111 was sent on Monday.", 0.8},
	{"When does the depot open?", "The depot opens at seven.", 0.02},
}

// Real rerankers' stubs rank in llama.cpp (node-llama-cpp, the sigmoid of
// their score) as their mappings say, on the CPU and on the GPU.
func TestLlamaCppRanksAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := llamacpp.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var pairs []llamacpp.Pair
	for _, r := range rankings {
		pairs = append(pairs, llamacpp.Pair{Query: r.query, Document: r.document})
	}
	var jobs []llamacpp.Job
	var names []string
	for _, m := range realRerankers {
		metadataOf(t, m.url, m.sha256)
		// The head itself: its tensors' infos say it has a classifier.
		c, err := readContract(filepath.Join(npm.CacheDir("models-heads"), m.sha256))
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if c.Task != "classification" || len(c.Outputs) != 1 || c.Outputs[0].Name != "score" {
			t.Fatalf("%s: its contract is %s, of %+v", m.name, c.Task, c.Outputs)
		}
		dir := t.TempDir()
		if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
			t.Fatal(err)
		}
		modelstest.WriteMappings(t, dir, []map[string]any{
			{"name": "A crushed box", "input": map[string]any{"text": "crushed box"}, "output": map[string]any{"label": "relevant", "score": 0.95}},
			{"name": "A refund", "input": map[string]any{"text": "refund"}, "output": map[string]any{"label": "relevant", "score": 0.8}},
			{"name": "Anything else", "default": true, "output": map[string]any{"label": "relevant", "score": 0.02}},
		})
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
			names = append(names, m.name)
			jobs = append(jobs, llamacpp.Job{Model: path, Rank: pairs, GPU: gpu})
		}
	}
	results, err := rt.Run(ctx, jobs)
	if err != nil {
		t.Fatal(err)
	}
	for j, r := range results {
		for i, want := range rankings {
			if got := r.Scores[i]; math.Abs(got-want.score) > 1e-6 {
				t.Errorf("%s (job %d): %q, %q is ranked %v, want %v", names[j], j, want.query, want.document, got, want.score)
			}
		}
	}
}
