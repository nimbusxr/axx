package piper

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func TestWriteIsDeterministicAndCarriesVoiceConfig(t *testing.T) {
	s := voiceStub(t)
	first, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.File, second.File) || !bytes.Equal(first.Beside[".onnx.json"], second.Beside[".onnx.json"]) {
		t.Fatal("the same voice mappings wrote different files")
	}
	var config map[string]any
	if err := json.Unmarshal(first.Beside[".onnx.json"], &config); err != nil {
		t.Fatal(err)
	}
	if config["phoneme_type"] != "text" {
		t.Errorf("phoneme_type %v", config["phoneme_type"])
	}
	ids := config["phoneme_id_map"].(map[string]any)
	if ids["a"].([]any)[0].(float64) != 14 {
		t.Errorf("the real voice's id for a changed: %v", ids["a"])
	}
	if _, ok := ids["🙂"]; !ok {
		t.Error("a codepoint used by a mapping was not added")
	}
	if config["num_speakers"] != float64(1) || config["num_symbols"].(float64) < 17 {
		t.Errorf("voice metadata: speakers %v, symbols %v", config["num_speakers"], config["num_symbols"])
	}
}

func TestReadContractRoundTrip(t *testing.T) {
	s := voiceStub(t)
	m, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	model := filepath.Join(dir, "voice.onnx")
	if err := os.WriteFile(model, m.File, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, data := range m.Beside {
		path := filepath.Join(dir, format.BesideName(model, name))
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !isVoice(model) {
		t.Fatal("the generated voice was not recognized as Piper")
	}
	c, err := ReadContract(model)
	if err != nil {
		t.Fatal(err)
	}
	if c.Format.Type != "piper" || c.Task != "text-to-speech" || c.Text.Tokenizer != "piper" {
		t.Errorf("contract: format %s, task %s, tokenizer %s", c.Format.Type, c.Task, c.Text.Tokenizer)
	}
}

func voiceStub(t *testing.T) *stub.Stub {
	t.Helper()
	c := &contract.Contract{
		Format: contract.Format{Type: "piper", IRVersion: 8, Opsets: map[string]int64{"": 13}, Details: json.RawMessage(`{"sampleRate":16000}`)},
		Task:   "text-to-speech",
		Inputs: []contract.Tensor{
			{Name: "input", Type: "int64", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}}},
			{Name: "input_lengths", Type: "int64", Shape: []contract.Dim{{Size: 1}}},
			{Name: "scales", Type: "float32", Shape: []contract.Dim{{Size: 3}}},
		},
		Outputs: []contract.Tensor{{Name: "output", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 1}, {Size: 64}}}},
		Text:    &contract.Text{IDs: "input", Tokenizer: "piper", Vocabulary: "vocabulary"},
	}
	c.Carry("vocabulary", "vocab.json", []byte(`{"_":[0],"^":[1],"$":[2]," ":[3],"a":[14],"b":[15],"c":[16],"d":[17],"e":[18],"f":[19],"g":[20],"h":[21],"i":[22],"l":[23],"n":[24],"o":[25],"p":[26],"r":[27],"s":[28],"t":[29],"u":[30],"v":[31],"w":[32],"y":[33]}`))
	c.Carry("config", "voice.json", []byte(`{"audio":{"sample_rate":16000},"phoneme_id_map":{"_":[0],"^":[1],"$":[2]}}`))
	dir := modelstest.Dir(t, c,
		map[string]any{"name": "Ring", "input": map[string]any{"text": "ring twice"}, "output": map[string]any{"audio": "../inputs/ring.wav"}},
		map[string]any{"name": "Fragile", "input": map[string]any{"text": "fragile 🙂"}, "output": map[string]any{"audio": "../inputs/fragile.wav"}},
		modelstest.Mapping("Anything else", "", map[string]any{"audio": "../inputs/default.wav"}),
	)
	for name, value := range map[string]float64{"ring": 0.25, "fragile": -0.25, "default": 0} {
		samples := make([]float64, 64)
		for i := range samples {
			samples[i] = value
		}
		modelstest.WriteWAV(t, filepath.Join(dir, "inputs", name+".wav"), 16000, samples)
	}
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
