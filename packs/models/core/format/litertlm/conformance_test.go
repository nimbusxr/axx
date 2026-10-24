//go:build integration

package litertlm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/litertlmrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// realModels are the models whose stubs LiteRT-LM runs: its own test
// model (Gemma 3's SentencePiece tokenizer, a cache of 128 positions,
// prefills of 16 and 64 tokens), and SmolLM2's (a Hugging Face tokenizer),
// each pinned by its SHA-256.
var realModels = []struct{ name, url, sha256 string }{
	{"LiteRT-LM's test model (SentencePiece)", "https://raw.githubusercontent.com/google-ai-edge/LiteRT-LM/a7fe6e469cd0afc40ecb3d91bf05efdf5ffab104/runtime/testdata/test_lm.litertlm", "36c6cc10f140e5e3526c0838ebb5ce74142b3c0ce8d1356c7d6d0ff50de6a288"},
	{"SmolLM2 135M (a Hugging Face tokenizer)", "https://huggingface.co/litert-community/SmolLM2-135M-Instruct/resolve/8111e0a65fda719f0a6855e8e1a8ec8c3f9ccb22/SmolLM2_135M_Instruct.litertlm", "ccdc5c85735743f081b7d44ca309cab569f76c0f2f0e8e163449a63721969c37"},
}

// LiteRT-LM answers a stub's conversations as its mappings say.
func TestLiteRTLMAnswersAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := litertlmrun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range realModels {
		t.Run(m.name, func(t *testing.T) { conform(ctx, t, rt, modelstest.Fetch(t, m.url, m.sha256)) })
	}
}

func conform(ctx context.Context, t *testing.T, rt *litertlmrun.Runtime, real string) {
	c, err := readContract(real)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "mappings"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"mappings": []any{
		map[string]any{"name": "The neighbour", "input": map[string]any{"text": "Leave it with the neighbour"}, "output": map[string]any{"text": "Leave the parcel with the neighbour."}},
		map[string]any{"name": "Fragile", "input": map[string]any{"text": "fragile"}, "output": map[string]any{"json": map[string]any{"handling": "fragile"}}},
		map[string]any{"name": "Anything else", "default": true, "output": map[string]any{"text": "No special instructions."}},
	}})
	if err := os.WriteFile(filepath.Join(dir, "mappings", "notes.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	model, err := write(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "stub.litertlm")
	if err := os.WriteFile(path, model, 0o644); err != nil {
		t.Fatal(err)
	}
	conversations := [][2][]string{
		{{"Summarize this note: Leave it with the neighbour at number 12, please."}, {"Leave the parcel with the neighbour."}},
		{{"Careful, fragile glass inside."}, {`{"handling":"fragile"}`}},
		{{"Leave it with the neighbours."}, {"No special instructions."}},
		{{"leave it with the neighbour"}, {"No special instructions."}},
		// A conversation has the texts of its turns before.
		{{"Ring the bell twice.", "Also: the box is fragile.", "Thanks!"}, {"No special instructions.", `{"handling":"fragile"}`, `{"handling":"fragile"}`}},
	}
	var prompts [][]string
	for _, c := range conversations {
		prompts = append(prompts, c[0])
	}
	results, err := rt.Run(ctx, []litertlmrun.Job{{Model: path, Conversations: prompts}})
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range conversations {
		for j, want := range c[1] {
			if got := results[0][i][j]; got != want {
				t.Errorf("%q: answered %q, want %q", c[0][j], got, want)
			}
		}
	}
	t.Logf("a stub of %d KB", len(model)/1024)
}
