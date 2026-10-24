package foundationmodels

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// stubOf resolves a stub of the contract and the mappings, in a directory.
func stubOf(t *testing.T, c string, ms ...map[string]any) (*stub.Stub, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "contract.json"), []byte(c), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "mappings"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"mappings": ms})
	if err := os.WriteFile(filepath.Join(dir, "mappings", "notes.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return stub.Load(dir)
}

const deliveryNote = `{"format": {"type": "foundation-models", "details": {"name": "DeliveryNoteModel"}}, "task": "text-generation"}`

var otherwise = map[string]any{"name": "Any other note", "default": true, "output": map[string]any{"text": "See the recipient's note."}}

// The stub is the contract's Swift type, of the system model's capabilities,
// with its mappings' texts and answers as Swift writes them.
func TestAStubIsASwiftTypeOfItsMappings(t *testing.T) {
	s, err := stubOf(t, deliveryNote,
		map[string]any{"name": "Behind the gate", "input": map[string]any{"text": `the "green" gate`}, "output": map[string]any{"text": "Behind the gate.\nClose it: the dog. \\(not code)"}},
		map[string]any{"name": "Fragile\nglass", "input": map[string]any{"text": "fragile"}, "output": map[string]any{"json": map[string]any{"handling": "fragile"}}},
		otherwise)
	if err != nil {
		t.Fatal(err)
	}
	b, err := write(s)
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"struct DeliveryNoteModel: LanguageModel {",
		"typealias Executor = DeliveryNoteModelExecutor",
		"LanguageModelCapabilities([.guidedGeneration, .toolCalling, .vision])",
		`        // Behind the gate` + "\n" + `        ("the \"green\" gate", "Behind the gate.\nClose it: the dog. \\(not code)"),`,
		`        // Fragile glass` + "\n" + `        ("fragile", "{\"handling\":\"fragile\"}"),`,
		`static let otherwise = "See the recipient's note."`,
		"#if compiler(>=6.4)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("the stub has no\n%s\nin\n%s", want, src)
		}
	}
}

// A contract that does not name a Swift type, or gives capabilities
// LanguageModelCapabilities has not, says so.
func TestAContractOfNoSwiftTypeSaysSo(t *testing.T) {
	for c, want := range map[string]string{
		`{"format": {"type": "foundation-models"}, "task": "text-generation"}`:                                                             "format.details.name is missing",
		`{"format": {"type": "foundation-models", "details": {"name": "delivery note"}}, "task": "text-generation"}`:                       `"delivery note" is not a Swift type's name`,
		`{"format": {"type": "foundation-models", "details": {"name": "struct"}}, "task": "text-generation"}`:                              `"struct" is not a Swift type's name`,
		`{"format": {"type": "foundation-models", "details": {"name": "Notes", "capabilities": ["thinking"]}}, "task": "text-generation"}`: `has "thinking", which is not one of guidedGeneration, reasoning, toolCalling, vision`,
		`{"format": {"type": "foundation-models", "details": {"name": "Notes", "temperature": 0}}, "task": "text-generation"}`:             `unknown field "temperature"`,
	} {
		s, err := stubOf(t, c, otherwise)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := write(s); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", c, err, want)
		}
	}
}

// A model of an API takes a prompt: its contract has no tensors, and a
// mapping of a picture, or a task that generates no text, says so; a model
// of tensors still has them.
func TestAModelOfAnAPIHasNoTensors(t *testing.T) {
	_, err := stubOf(t, deliveryNote, map[string]any{"name": "A photo", "input": map[string]any{"image": "photo.png"}, "output": map[string]any{"text": "A parcel."}}, otherwise)
	if err == nil || !strings.Contains(err.Error(), "the model takes a prompt") {
		t.Errorf("a picture's mapping: %v", err)
	}
	_, err = stubOf(t, `{"format": {"type": "foundation-models", "details": {"name": "Scores"}}}`, map[string]any{"name": "Any", "default": true, "output": map[string]any{"tensors": map[string]any{"scores": []float64{1}}}})
	if err == nil || !strings.Contains(err.Error(), "is not one that generates text") {
		t.Errorf("a model of no task: %v", err)
	}
	c := contract.Contract{Format: contract.Format{Type: "onnx"}, Task: "text-generation"}
	if err := c.Check(); err == nil || !strings.Contains(err.Error(), "inputs are missing") {
		t.Errorf("an ONNX model of no inputs: %v", err)
	}
}
