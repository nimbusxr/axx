package safetensors

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/internal/whisper"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A speech recognizer's folder (Whisper's: config.json of model_type
// whisper, its generation_config.json, preprocessor_config.json and
// tokenizer), as transformers' WhisperForConditionalGeneration and its
// pipeline load it: its stub is Whisper's weights of the real model's sizes
// (package whisper), in model.safetensors; its config stays.

// whisperContract is the contract of a Whisper's folder: its input a
// sound's log-mel features, of its processor's size; its sizes and its
// tokens of a transcript's beginning and end.
func whisperContract(dir, path string, raw []byte) (*contract.Contract, error) {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	read := func(name string) []byte {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return b
	}
	var cfg struct {
		D             int `json:"d_model"`
		Heads         int `json:"encoder_attention_heads"`
		DecoderHeads  int `json:"decoder_attention_heads"`
		EncoderLayers int `json:"encoder_layers"`
		DecoderLayers int `json:"decoder_layers"`
		EncoderFFN    int `json:"encoder_ffn_dim"`
		DecoderFFN    int `json:"decoder_ffn_dim"`
		Mels          int `json:"num_mel_bins"`
		Vocab         int `json:"vocab_size"`
		Source        int `json:"max_source_positions"`
		Target        int `json:"max_target_positions"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.D == 0 || cfg.Mels == 0 {
		return nil, fail("its config.json gives no Whisper's sizes")
	}
	if cfg.DecoderHeads != cfg.Heads {
		return nil, fail("its encoder's and decoder's heads differ (%d, %d)", cfg.Heads, cfg.DecoderHeads)
	}
	var gen struct {
		EOS             int32            `json:"eos_token_id"`
		SOT             int32            `json:"decoder_start_token_id"`
		NoTimestamps    int32            `json:"no_timestamps_token_id"`
		Multilingual    bool             `json:"is_multilingual"`
		Languages       map[string]int32 `json:"lang_to_id"`
		Tasks           map[string]int32 `json:"task_to_id"`
		Suppress        []int32          `json:"suppress_tokens"`
		BeginSuppressed []int32          `json:"begin_suppress_tokens"`
	}
	if err := json.Unmarshal(read("generation_config.json"), &gen); err != nil || gen.NoTimestamps == 0 || gen.SOT == 0 {
		return nil, fail("its folder has no generation_config.json of its transcripts' tokens (decoder_start_token_id, no_timestamps_token_id)")
	}
	var pre struct {
		FeatureSize  int `json:"feature_size"`
		Frames       int `json:"nb_max_frames"`
		SamplingRate int `json:"sampling_rate"`
	}
	if err := json.Unmarshal(read("preprocessor_config.json"), &pre); err != nil || pre.FeatureSize == 0 || pre.Frames == 0 {
		return nil, fail("its folder has no preprocessor_config.json of its features' size (feature_size, nb_max_frames, sampling_rate)")
	}
	tok := read("tokenizer.json")
	if tok == nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a speech recognizer, whose contract carries its tokenizer: there is no tokenizer.json beside it", path).
			WithHint("keep the model in its folder as it comes, its tokenizer's files beside it")
	}
	if _, err := lm.TokenizerJSON(tok); err != nil {
		return nil, fail("%v", err)
	}
	tokens := whisper.Tokens{EOT: gen.EOS, SOT: gen.SOT, NoTimestamps: gen.NoTimestamps, TimestampBegin: gen.NoTimestamps + 1, Language: -1}
	if gen.Multilingual {
		id, ok := gen.Languages["<|en|>"]
		if !ok {
			return nil, fail("its generation_config.json has no <|en|> in lang_to_id")
		}
		tokens.Language = id
		for _, name := range []string{"transcribe", "translate"} {
			if id, ok := gen.Tasks[name]; ok {
				tokens.Tasks = append(tokens.Tasks, id)
			}
		}
	}
	d := whisper.Details{
		Dims: whisper.Dims{
			Mels: cfg.Mels, Vocab: cfg.Vocab, D: cfg.D, Heads: cfg.Heads, EncoderLayers: cfg.EncoderLayers, DecoderLayers: cfg.DecoderLayers,
			EncoderFFN: cfg.EncoderFFN, DecoderFFN: cfg.DecoderFFN, AudioPositions: cfg.Source, TextPositions: cfg.Target,
		},
		Tokens: tokens, Suppressed: gen.Suppress, BeginSuppressed: gen.BeginSuppressed,
	}
	details, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	rate := pre.SamplingRate
	if rate == 0 {
		rate = 16000
	}
	c := &contract.Contract{
		Format: contract.Format{Type: "safetensors", Details: details},
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{
			Name: "input_features", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Size: int64(pre.FeatureSize)}, {Size: int64(pre.Frames)}},
			Audio: &contract.Audio{SampleRate: rate, Features: "log-mel"},
		}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}, {Size: int64(cfg.Vocab)}}}},
		Match:   contract.Match{Input: "input_features"},
	}
	c.Carry(tokenizerFile, "tokenizer.json", tok)
	return c, nil
}

// writeWhisper writes a speech recognizer's stub: its weights, in
// model.safetensors (its config.json, of the same sizes, stays).
func writeWhisper(s *stub.Stub) (format.Model, error) {
	var d whisper.Details
	if err := json.Unmarshal(s.Contract.Format.Details, &d); err != nil || d.Dims.D == 0 {
		return format.Model{}, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a speech recognizer's (%v)", err).
			WithHint("write the contract again from the model with `axx models contract <folder>/model.safetensors`")
	}
	tok, err := s.Contract.File(tokenizerFile)
	if err != nil {
		return format.Model{}, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no tokenizer of the model")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return format.Model{}, err
	}
	p, err := whisper.NewPlan(s, spec, &d)
	if err != nil {
		return format.Model{}, err
	}
	w, err := whisper.Compile(d.Dims, d.Tokens, p)
	if err != nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	var ts []tensor
	for name, t := range w {
		shape := make([]int64, len(t.Shape))
		for i, n := range t.Shape {
			shape[i] = int64(n)
		}
		ts = append(ts, tensor{name: name, dtype: "F32", shape: shape, data: t.Data})
	}
	b, err := encode(ts)
	if err != nil {
		return format.Model{}, err
	}
	return format.Model{File: b}, nil
}
