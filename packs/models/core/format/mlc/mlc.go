// Package mlc reads the contracts of MLC LLM's language models (WebLLM's, in
// pages, and MLC LLM's apps'): a folder of mlc-chat-config.json (the model's
// architecture, quantization and chat template), tensor-cache.json (its
// weights' records, in the shards beside it) and its tokenizer, which a
// model library compiled for that architecture and quantization runs. It
// writes stubs as such models: a GGUF stub's transformer (package gguf)
// laid out in the real model's architecture (gguf.CompileFor), its weights
// the real model's records, quantized as the library dequantizes them, so
// that the real model's library runs the stub.
package mlc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/gguf"
	"github.com/nimbusxr/axx/packs/models/core/internal/detmath"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{
		Type: "mlc", Name: "WebLLM (MLC LLM)",
		Extensions: []string{".json"}, Folders: []string{chatConfigName},
		Sniff: isMLC, Contract: ReadContract, Write: Write,
	})
}

// The contract's files: the model's mlc-chat-config.json and its tokenizer
// (tokenizer.json); and its records' manifests, by their names.
const (
	configFile    = "config"
	tokenizerFile = "tokenizer"
)

// chatConfigName is an MLC model's config's name.
const chatConfigName = "mlc-chat-config.json"

// manifestNames are the names of an MLC model's manifests of its records,
// of one set of shards: tensor-cache.json, and those of MLC's before it,
// which its folders keep (ndarray-cache.json, and ndarray-cache-b16.json of
// its float32s as the bfloat16s they are stored as).
var manifestNames = []string{"tensor-cache.json", "ndarray-cache.json", "ndarray-cache-b16.json"}

// details are a model's own settings in its contract.
type details struct {
	ModelType    string `json:"modelType"`
	Quantization string `json:"quantization"`
	// ContextLength is its context window, which its stub tells positions
	// apart over.
	ContextLength int `json:"contextLength"`
	// EndOfGeneration is the token that ends its turn.
	EndOfGeneration int32 `json:"endOfGeneration"`
	// Manifests are the manifests its folder has (the first, the stub's
	// records).
	Manifests []string `json:"manifests"`
}

// chatConfig is what an mlc-chat-config.json gives a contract.
type chatConfig struct {
	ModelType     string          `json:"model_type"`
	Quantization  string          `json:"quantization"`
	ModelConfig   modelConfig     `json:"model_config"`
	VocabSize     int             `json:"vocab_size"`
	ContextWindow int             `json:"context_window_size"`
	SlidingWindow int             `json:"sliding_window_size"`
	EOS           json.RawMessage `json:"eos_token_id"`
	ConvTemplate  struct {
		StopTokenIDs []int32 `json:"stop_token_ids"`
	} `json:"conv_template"`
}

// modelConfig is the architecture a model's library was compiled for.
type modelConfig struct {
	HiddenSize       int             `json:"hidden_size"`
	IntermediateSize int             `json:"intermediate_size"`
	Heads            int             `json:"num_attention_heads"`
	KVHeads          int             `json:"num_key_value_heads"`
	HeadDim          int             `json:"head_dim"`
	Layers           int             `json:"num_hidden_layers"`
	VocabSize        int             `json:"vocab_size"`
	RopeBase         float64         `json:"position_embedding_base"`
	RopeTheta        float64         `json:"rope_theta"`
	RopeScaling      json.RawMessage `json:"rope_scaling"`
	PartialRotary    float64         `json:"partial_rotary_factor"`
	ContextWindow    int             `json:"context_window_size"`
	Tied             *bool           `json:"tie_word_embeddings"`
	// Gemma's: its logits' soft cap, its local layers' window and their
	// pattern (every sixth global, Gemma 3's), and, of Gemma 3's, its
	// text model's config.
	LogitCap      float64      `json:"final_logit_softcapping"`
	SlidingWindow int          `json:"sliding_window_size"`
	TextConfig    *modelConfig `json:"text_config"`
	// GPT-NeoX's share of its heads' dimensions that turn; Phi-2's how
	// many, and its sizes' own names.
	RotaryPct float64 `json:"rotary_pct"`
	// Qwen 3.5's layers of full attention: every so many.
	FullAttentionEvery int `json:"full_attention_interval"`
	RotaryDim          int `json:"rotary_dim"`
}

// UnmarshalJSON reads a model config, of Phi-2's names too (n_embd...).
func (mc *modelConfig) UnmarshalJSON(b []byte) error {
	type plain modelConfig
	var phi struct {
		NEmbd  int `json:"n_embd"`
		NInner int `json:"n_inner"`
		NLayer int `json:"n_layer"`
		NHead  int `json:"n_head"`
		NKV    int `json:"n_head_kv"`
	}
	if err := json.Unmarshal(b, (*plain)(mc)); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &phi); err != nil {
		return err
	}
	mc.HiddenSize = cmpOr(mc.HiddenSize, phi.NEmbd)
	mc.IntermediateSize = cmpOr(mc.IntermediateSize, phi.NInner)
	mc.Layers = cmpOr(mc.Layers, phi.NLayer)
	mc.Heads = cmpOr(mc.Heads, phi.NHead)
	mc.KVHeads = cmpOr(mc.KVHeads, phi.NKV)
	return nil
}

// isMLC says a model's file is an MLC model's mlc-chat-config.json.
func isMLC(path string) bool { return filepath.Base(path) == chatConfigName }

// ReadContract reads the contract of the MLC model at path (its folder, or
// its mlc-chat-config.json): a text generation model of its architecture
// and quantization, its records' manifest, its tokenizer (tokenizer.json)
// and the token that ends its turn (its chat template's first stop token
// that ends a turn).
func ReadContract(path string) (*contract.Contract, error) {
	dir := path
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		dir = filepath.Dir(path)
	}
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	raw, err := os.ReadFile(filepath.Join(dir, chatConfigName))
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not an MLC model's folder: it has no %s", path, chatConfigName).
			WithHint("keep the model in its folder as it comes (mlc-chat-config.json, tensor-cache.json, the tokenizer's files and the weights' shards, as MLC's repositories have them)")
	}
	var cfg chatConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fail("its %s: %v", chatConfigName, err)
	}
	manifests := map[string][]byte{}
	var names []string
	for _, name := range manifestNames {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			if _, err := readManifest(b); err != nil {
				return nil, fail("its %s: %v", name, err)
			}
			manifests[name] = b
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, fail("its folder has no tensor-cache.json, the manifest of its weights")
	}
	if _, err := archOf(&cfg); err != nil {
		return nil, fail("%v", err)
	}
	tok, err := os.ReadFile(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a language model, whose contract carries its tokenizer: there is no tokenizer.json beside it", path).
			WithHint("keep the model in its folder as it comes, its tokenizer's files beside it")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return nil, fail("%v", err)
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return nil, fail("%v", err)
	}
	stops := cfg.ConvTemplate.StopTokenIDs
	if len(stops) == 0 {
		stops = ids(cfg.EOS)
	}
	if len(stops) == 0 {
		return nil, fail("its %s gives no stop token (conv_template.stop_token_ids, or eos_token_id)", chatConfigName)
	}
	context := cfg.ContextWindow
	if context <= 0 {
		context = cfg.ModelConfig.ContextWindow
	}
	if context <= 0 {
		return nil, fail("its %s gives no context window (context_window_size)", chatConfigName)
	}
	d := details{
		ModelType: cfg.ModelType, Quantization: cfg.Quantization, ContextLength: context,
		EndOfGeneration: lm.EndOfTurn(v, stops), Manifests: names,
	}
	b, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	vocab := cmpOr(cfg.ModelConfig.VocabSize, cfg.VocabSize, v.Size())
	c := &contract.Contract{
		Format:  contract.Format{Type: "mlc", Details: b},
		Task:    "text-generation",
		Inputs:  []contract.Tensor{{Name: "input_ids", Type: "int32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}}}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}, {Size: int64(vocab)}}}},
	}
	c.Carry(configFile, chatConfigName, raw)
	for _, name := range names {
		c.Carry(name, name, manifests[name])
	}
	c.Carry(tokenizerFile, "tokenizer.json", tok)
	return c, nil
}

func cmpOr(vs ...int) int {
	for _, v := range vs {
		if v != 0 {
			return v
		}
	}
	return 0
}

// ids are a config's token ids: one, or a list.
func ids(raw json.RawMessage) []int32 {
	var one int32
	if json.Unmarshal(raw, &one) == nil {
		return []int32{one}
	}
	var many []int32
	_ = json.Unmarshal(raw, &many)
	return many
}

// Write writes a language model's stub: the model's folder, of its
// mlc-chat-config.json (which lists the tokenizer's tokenizer.json alone),
// its manifests (the model's, of the stub's shards' checksums), its
// weights' shards and its tokenizer.
func Write(s *stub.Stub) (format.Model, error) {
	if s.Matcher != stub.TextMatcher || s.Generated == nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: an MLC stub generates text (task text-generation) for prompts matched by their text").
			WithHint(`a mapping's input is {"text": ...}, and its output {"text": ...}`)
	}
	c := s.Contract
	var d details
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		return format.Model{}, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not an MLC model's: %v", err)
	}
	if len(d.Manifests) == 0 {
		return format.Model{}, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract lists no manifest of the model's records").
			WithHint("write the contract again from the model with `axx models contract <folder>`")
	}
	files := map[string][]byte{}
	for _, key := range append([]string{configFile, tokenizerFile}, d.Manifests...) {
		b, err := c.File(key)
		if err != nil {
			return format.Model{}, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no %s of the model", key).
				WithHint("write the contract again from the model with `axx models contract <folder>`")
		}
		files[key] = b
	}
	var cfg chatConfig
	if err := json.Unmarshal(files[configFile], &cfg); err != nil {
		return format.Model{}, err
	}
	a, err := archOf(&cfg)
	if err != nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	m, err := readManifest(files[d.Manifests[0]])
	if err != nil {
		return format.Model{}, err
	}
	spec, err := lm.TokenizerJSON(files[tokenizerFile])
	if err != nil {
		return format.Model{}, err
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return format.Model{}, err
	}
	p, err := lm.NewPlan(s, v, d.EndOfGeneration, d.ContextLength)
	if err != nil {
		return format.Model{}, err
	}
	p.Vocab = a.vocab
	w, err := gguf.CompileFor(p, a.target)
	if err != nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written in %s's architecture: %v", cfg.ModelType, err)
	}
	out, err := m.write(a, w, cfg.Quantization)
	if err != nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	for _, name := range d.Manifests {
		if out[name], err = checksummed(files[name], out); err != nil {
			return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: the model's %s: %v", name, err)
		}
	}
	config, err := stubConfig(files[configFile])
	if err != nil {
		return format.Model{}, err
	}
	out[chatConfigName] = config
	out["tokenizer.json"] = files[tokenizerFile]
	return format.Model{Files: out}, nil
}

// stubConfig is the stub's mlc-chat-config.json: the model's, of the
// tokenizer's file the stub has.
func stubConfig(raw []byte) ([]byte, error) {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	cfg["tokenizer_files"] = json.RawMessage(`["tokenizer.json"]`)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// arch is a model's architecture: the transformer a stub is laid out in,
// its vocabulary's rows, and how many of its heads' dimensions turn (the
// first; the rest do not: Phi-4's).
type arch struct {
	modelType string
	target    gguf.Target
	vocab     int
	rotary    int
	// gatedQuery says its fused projection has each head's query and its
	// output's gate (Qwen 3.5's).
	gatedQuery bool
}

// archOf is a model's architecture, of a type axx writes.
func archOf(cfg *chatConfig) (*arch, error) {
	mc := cfg.ModelConfig
	if mc.TextConfig != nil {
		mc = *mc.TextConfig
	}
	switch cfg.ModelType {
	case "llama", "mistral", "qwen2", "qwen3", "qwen3_5", "phi3", "phi3_v", "gemma", "gemma2", "gemma3", "olmo2", "stablelm", "gpt_neox", "phi-msft":
	case "ministral3":
		// MLC's YaRN (TVM's rope_freq_yarn) ramps each of a head's
		// dimensions by its own index, not its pair's: the halves of a pair
		// turn by different angles, and a query's score of a key is not of
		// their offset alone, which a stub's heads tell its last tokens by.
		return nil, fmt.Errorf("it is a model of MLC's ministral3 architecture, whose YaRN rotary scaling turns the two halves of each of its heads' pairs by different angles: no head of it scores a key by its offset alone, as a stub's do")
	default:
		return nil, fmt.Errorf("it is a model of MLC's %s architecture, which axx does not write (it writes llama's, mistral's, qwen2's, qwen3's, qwen3_5's, phi3's, phi3_v's, gemma's, gemma2's, gemma3's, olmo2's, stablelm's, gpt_neox's and phi-msft's)", cfg.ModelType)
	}
	if _, ok := quantizations[cfg.Quantization]; !ok {
		return nil, fmt.Errorf("it is of MLC's quantization %s, which axx does not write (it writes %v)", cfg.Quantization, slices.Sorted(maps.Keys(quantizations)))
	}
	hd := mc.HeadDim
	if hd == 0 && mc.Heads > 0 {
		hd = mc.HiddenSize / mc.Heads
	}
	t := gguf.Target{E: mc.HiddenSize, Heads: mc.Heads, KVHeads: cmpOr(mc.KVHeads, mc.Heads), HeadDim: hd, FFN: mc.IntermediateSize, Layers: mc.Layers}
	if q := quantizations[cfg.Quantization]; q != nil {
		t.Group = q.group
	}
	t.HeadNorm = cfg.ModelType == "qwen3" || cfg.ModelType == "qwen3_5" || cfg.ModelType == "gemma3"
	if cfg.ModelType == "qwen3_5" {
		// Qwen 3.5's full attention, of every few layers (the others' a
		// linear attention's, of no weights).
		every := cmpOr(mc.FullAttentionEvery, 4)
		t.Attends = func(layer int) bool { return (layer+1)%every == 0 }
	}
	switch cfg.ModelType {
	case "stablelm", "gpt_neox":
		t.LayerNorm = true
	case "phi-msft":
		t.LayerNorm, t.Parallel = true, true
	}
	if cfg.ModelType == "olmo2" {
		// OLMo 2's attention and MLP read the stream as it is, their
		// outputs normalized; its queries' and keys' norms of the whole
		// projection's.
		t.RawInputs, t.SublayerNorm, t.WholeNorm = true, true, true
	}
	if strings.HasPrefix(cfg.ModelType, "gemma") {
		// Gemma's embeddings × √E, of the model's dtype; its sublayers'
		// outputs normalized (Gemma 2's and 3's); its logits capped (Gemma
		// 2's); every sixth of its layers global, the others of a window
		// (Gemma 3's, as MLC's KV cache has them).
		scale := float32(math.Sqrt(float64(mc.HiddenSize)))
		if strings.Contains(cfg.Quantization, "f16") {
			scale = contract.FromFloat16(contract.Float16(scale))
		}
		t.EmbedScale = float64(scale)
		t.SublayerNorm = cfg.ModelType != "gemma"
		t.LogitCap = mc.LogitCap
		if cfg.ModelType == "gemma3" && cmpOr(mc.SlidingWindow, cfg.SlidingWindow) > 0 {
			t.Global = func(layer int) bool { return (layer+1)%6 == 0 }
		}
	}
	base := mc.RopeBase
	if base == 0 {
		base = mc.RopeTheta
	}
	if base == 0 || hd == 0 {
		return nil, fmt.Errorf("its model_config gives no rotary base or head size")
	}
	rotary := hd
	switch {
	case mc.RotaryDim > 0:
		rotary = mc.RotaryDim
	case mc.PartialRotary > 0:
		rotary = int(float64(hd) * mc.PartialRotary)
	case mc.RotaryPct > 0:
		rotary = int(float64(hd) * mc.RotaryPct)
	}
	scale, err := ropeScaling(mc.RopeScaling, rotary)
	if err != nil {
		return nil, err
	}
	// Each pair's angle: of the turning dimensions' pairs, then of those
	// that do not turn.
	for i := range hd / 2 {
		theta := 0.0
		if i < rotary/2 {
			theta = scale(i, detmath.Pow(base, -2*float64(i)/float64(rotary)))
		}
		t.Thetas = append(t.Thetas, theta)
	}
	return &arch{modelType: cfg.ModelType, target: t, vocab: cmpOr(mc.VocabSize, cfg.VocabSize), rotary: rotary, gatedQuery: cfg.ModelType == "qwen3_5"}, nil
}

// ropeScaling is how a model's rotary scaling turns a pair's angle per
// position: Llama 3's (llama3); longrope's (Phi-3's), of its short factors,
// which MLC turns pairs by in appends of up to its original context's
// tokens (a page's prefills' chunks are shorter), each pair's cosine and
// sine times its attention factor, which scales every score alike; or
// none.
func ropeScaling(raw json.RawMessage, rotary int) (func(i int, f float64) float64, error) {
	var s struct {
		Type          string    `json:"rope_type"`
		Factor        float64   `json:"factor"`
		Low           float64   `json:"low_freq_factor"`
		High          float64   `json:"high_freq_factor"`
		OriginalLimit float64   `json:"original_max_position_embeddings"`
		Short         []float64 `json:"short_factor"`
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("{}")) {
		return func(_ int, f float64) float64 { return f }, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("its rope_scaling: %w", err)
	}
	switch s.Type {
	case "llama3":
	case "longrope":
		if len(s.Short) != rotary/2 {
			return nil, fmt.Errorf("its longrope scaling has %d short factors, not one for each of its %d pairs", len(s.Short), rotary/2)
		}
		return func(i int, f float64) float64 { return f / s.Short[i] }, nil
	default:
		return nil, fmt.Errorf("its rotary scaling is %q, which axx does not write (it writes llama3's and longrope's)", s.Type)
	}
	// Llama 3's: a pair of a wavelength longer than the original context ÷
	// low turns factor times slower; one shorter than it ÷ high, as it was;
	// those between, smoothly between.
	lowWave, highWave := s.OriginalLimit/s.Low, s.OriginalLimit/s.High
	return func(_ int, f float64) float64 {
		wave := 2 * math.Pi / f
		switch {
		case wave < highWave:
			return f
		case wave > lowWave:
			return f / s.Factor
		}
		smooth := (s.OriginalLimit/wave - s.Low) / (s.High - s.Low)
		return (1-smooth)*f/s.Factor + float64(smooth*f)
	}, nil
}
