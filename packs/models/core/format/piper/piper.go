// Package piper reads Piper voice contracts and writes their stubs: an ONNX
// model and a voice .json beside it. The stub's phoneme_type is text, so the
// voice needs no eSpeak installation: its codepoints are its phonemes.
package piper

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/onnx"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/tokenize"
)

func init() {
	format.Register(format.Format{Type: "piper", Name: "Piper", Extensions: []string{".onnx"}, Sniff: isVoice, Contract: ReadContract, Write: Write})
}

type details struct {
	SampleRate int `json:"sampleRate"`
}

func isVoice(path string) bool {
	_, err := readConfig(path + ".json")
	return err == nil
}

// ReadContract reads a Piper voice's ONNX model and sidecar config: its
// inputs, output, sample rate and phoneme id map. A stub's output waveform is
// fixed by the contract (use --output when the real model's is open).
func ReadContract(path string) (*contract.Contract, error) {
	raw, err := readConfig(path + ".json")
	if err != nil {
		return nil, err
	}
	c, err := onnx.ReadContract(path)
	if err != nil {
		return nil, err
	}
	d := details{SampleRate: int(raw["audio"].(map[string]any)["sample_rate"].(float64))}
	c.Format.Type = "piper"
	c.Format.Details = json.RawMessage(fmt.Sprintf(`{"sampleRate":%d}`, d.SampleRate))
	c.Task = "text-to-speech"
	c.Text = &contract.Text{IDs: "input", Tokenizer: "piper", Vocabulary: "vocabulary"}
	vocab, err := json.Marshal(raw["phoneme_id_map"])
	if err != nil {
		return nil, err
	}
	c.Carry("vocabulary", "vocab.json", vocab)
	c.Carry("config", "voice.json", mustJSON(raw))
	return c, nil
}

func readConfig(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the Piper voice's config")
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a Piper voice config: %v", path, err)
	}
	audio, ok := raw["audio"].(map[string]any)
	if !ok {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a Piper voice config: no audio object", path)
	}
	rate, ok := audio["sample_rate"].(float64)
	if !ok || rate <= 0 {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a Piper voice config: no positive audio.sample_rate", path)
	}
	if _, ok := raw["phoneme_id_map"].(map[string]any); !ok {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a Piper voice config: no phoneme_id_map", path)
	}
	return raw, nil
}

// Write writes a Piper voice stub and its sidecar config. The ONNX model is
// the models-core writer's ordinary text-matching graph.
func Write(s *stub.Stub) (format.Model, error) {
	if s.Matcher != stub.TextMatcher {
		return format.Model{}, format.Unmatched("Piper", s)
	}
	if err := ensureVocabulary(s); err != nil {
		return format.Model{}, err
	}
	model, err := onnx.Write(s)
	if err != nil {
		return format.Model{}, err
	}
	config, err := voiceConfig(s.Contract)
	if err != nil {
		return format.Model{}, err
	}
	return format.Model{File: model, Beside: map[string][]byte{".onnx.json": config}}, nil
}

// ensureVocabulary adds codepoints used by the mappings, preserving every id
// the real voice used. The contract carries the augmented map in memory, so
// building twice gives the same model and never edits the source config.
func ensureVocabulary(s *stub.Stub) error {
	raw, err := s.Contract.File("vocabulary")
	if err != nil {
		return axxerr.Wrap(err, format.CodeWrite, exitcode.Usage, "the Piper voice has no phoneme id map")
	}
	var vocab map[string][]int32
	if err := json.Unmarshal(raw, &vocab); err != nil {
		return axxerr.New(format.CodeWrite, exitcode.Usage, "the Piper phoneme id map is invalid: %v", err)
	}
	for _, special := range []string{"_", "^", "$"} {
		if len(vocab[special]) == 0 {
			return axxerr.New(format.CodeWrite, exitcode.Usage, "the Piper phoneme id map has no %q boundary token", special)
		}
	}
	next := int32(0)
	for _, ids := range vocab {
		for _, id := range ids {
			next = max(next, id+1)
		}
	}
	changed := false
	for _, text := range s.Texts {
		for _, r := range tokenize.PiperText(text) {
			phoneme := string(r)
			if len(vocab[phoneme]) == 0 {
				vocab[phoneme] = []int32{next}
				next++
				changed = true
			}
		}
	}
	if changed {
		s.Contract.Carry("vocabulary", "vocab.json", mustJSON(vocab))
	}
	return nil
}

func voiceConfig(c *contract.Contract) ([]byte, error) {
	var raw map[string]any
	if _, ok := c.Files["config"]; ok {
		b, err := c.File("config")
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the Piper voice config is invalid: %v", err)
		}
	} else {
		raw = map[string]any{}
	}
	var d details
	if err := json.Unmarshal(c.Format.Details, &d); err != nil || d.SampleRate <= 0 {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the Piper contract has no sampleRate")
	}
	object(raw, "audio")["sample_rate"] = d.SampleRate
	raw["phoneme_type"] = "text"
	vocab, err := c.File("vocabulary")
	if err != nil {
		return nil, err
	}
	var ids map[string][]int32
	if err := json.Unmarshal(vocab, &ids); err != nil {
		return nil, err
	}
	raw["phoneme_id_map"] = ids
	next := int32(0)
	for _, values := range ids {
		for _, id := range values {
			next = max(next, id+1)
		}
	}
	raw["num_symbols"] = next
	if _, ok := raw["num_speakers"]; !ok {
		raw["num_speakers"] = 1
	}
	if _, ok := raw["speaker_id_map"]; !ok {
		raw["speaker_id_map"] = map[string]any{}
	}
	if _, ok := raw["phoneme_map"]; !ok {
		raw["phoneme_map"] = map[string]any{}
	}
	espeak := object(raw, "espeak")
	if _, ok := espeak["voice"]; !ok {
		espeak["voice"] = "en-us"
	}
	inference := object(raw, "inference")
	for key, value := range map[string]any{"noise_scale": 0.667, "length_scale": 1, "noise_w": 0.8} {
		if _, ok := inference[key]; !ok {
			inference[key] = value
		}
	}
	return json.Marshal(raw)
}

func object(parent map[string]any, key string) map[string]any {
	if value, ok := parent[key].(map[string]any); ok {
		return value
	}
	value := map[string]any{}
	parent[key] = value
	return value
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
