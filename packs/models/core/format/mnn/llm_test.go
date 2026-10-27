package mnn

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// An MNN-LLM model's folder of a byte-level tokenizer (a token of each
// byte, and an end of turn), its config.json and llm_config.json.
func llmFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var tokens []string
	for b := range 256 {
		tokens = append(tokens, string(lm.ByteRune(byte(b))))
	}
	tokens = append(tokens, "<|im_end|>")
	tok := fmt.Sprintf("430 3\n1 1 0\n256 256\n%d 0\n%s\n", len(tokens), strings.Join(tokens, "\n"))
	files := map[string]string{
		"config.json":     `{"llm_model": "llm.mnn", "llm_weight": "llm.mnn.weight", "precision": "low"}`,
		"llm_config.json": `{"hidden_size": 896, "layer_nums": 24, "attention_mask": "float", "tie_embeddings": [1, 2, 3, 4, 64], "jinja": {"chat_template": "{{ messages }}", "eos": "<|im_end|>"}}`,
		"tokenizer.txt":   tok,
	}
	for name, s := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// An MNN-LLM model's contract is of its config.json: it carries its configs
// and tokenizer, and its turn's end is its chat template's.
func TestAnMNNLLMsContractIsReadFromItsConfig(t *testing.T) {
	path := filepath.Join(llmFolder(t), "config.json")
	if !isLLM(path) {
		t.Fatal("its config.json is not an MNN-LLM model's")
	}
	c, err := readLLMContract(path)
	if err != nil {
		t.Fatal(err)
	}
	var d llmDetails
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		t.Fatal(err)
	}
	if c.Task != "text-generation" || d.EndOfGeneration != 256 || d.ContextLength != 2048 {
		t.Errorf("task %s, details %+v", c.Task, d)
	}
	for _, key := range []string{llmConfigFile, llmLLMConfigFile, llmTokenizerFile} {
		if _, err := c.File(key); err != nil {
			t.Errorf("it carries no %s: %v", key, err)
		}
	}
}

// A stub is a graph of the engine's inputs (the embeddings it looks up, the
// mask, the positions, the logits' index) and its logits, around MNN's
// Attention op; its llm_config.json is of its sizes, of embeddings it reads
// from embeddings_bf16.bin, a token's of its size.
func TestAnMNNLLMStubIsAGraphAndItsEmbeddings(t *testing.T) {
	c, err := readLLMContract(filepath.Join(llmFolder(t), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
		t.Fatal(err)
	}
	modelstest.WriteMappings(t, dir, []map[string]any{
		{"name": "Fragile", "input": map[string]any{"text": "fragile"}, "output": map[string]any{"text": "Handle with care."}},
		{"name": "Anything else", "default": true, "output": map[string]any{"text": "No notes."}},
	})
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := writeLLM(s)
	if err != nil {
		t.Fatal(err)
	}
	r, err := readNet(m.File)
	if err != nil {
		t.Fatal(err)
	}
	var inputs []string
	for _, in := range r.inputs {
		inputs = append(inputs, in.Name)
	}
	if !slices.Equal(inputs, []string{"input_ids", "attention_mask", "position_ids", "logits_index"}) || !slices.Equal(r.outputs, []string{"logits"}) {
		t.Errorf("inputs %v, outputs %v", inputs, r.outputs)
	}
	var cfg map[string]any
	if err := json.Unmarshal(m.Beside["llm_config.json"], &cfg); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(r.ops, opAttention) || !slices.Contains(r.ops, opLayerNorm) {
		t.Errorf("its ops %v have no Attention or LayerNorm", r.ops)
	}
	if _, ok := cfg["tie_embeddings"]; ok || cfg["attention_mask"] != "float" || cfg["jinja"] == nil {
		t.Errorf("its llm_config.json %v", cfg)
	}
	hidden := int(cfg["hidden_size"].(float64))
	if n := len(m.Beside["embeddings_bf16.bin"]); n != 257*hidden*2 {
		t.Errorf("its embeddings are %d bytes, not 257 tokens of %d", n, hidden)
	}
}
