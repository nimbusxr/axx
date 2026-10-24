// Package safetensors reads the contracts of language models as Hugging
// Face's repositories have them (a folder of config.json, model.safetensors
// and the tokenizer's files: the models Python services load with
// transformers' AutoModelForCausalLM and pipeline) and writes stubs as
// such models: a GGUF stub's transformer of llama's architecture (package
// gguf), its weights in a safetensors file, and the config.json of a
// LlamaForCausalLM of them, beside the real model's tokenizer and
// generation config.
package safetensors

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/gguf"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{
		Type: "safetensors", Name: "safetensors (Hugging Face transformers)",
		Extensions: []string{".safetensors"}, Folders: []string{"model.safetensors", "model.safetensors.index.json"},
		Contract: ReadContract, Write: Write,
	})
}

// tokenizerFile is the contract's file of the model's tokenizer.
const tokenizerFile = "tokenizer"

// details are a model's own settings in its contract: its config's model
// type, context and tokens, and the token that ends its turn.
type details struct {
	ModelType       string          `json:"modelType"`
	ContextLength   int             `json:"contextLength"`
	EndOfGeneration int32           `json:"endOfGeneration"`
	BOS             json.RawMessage `json:"bos,omitempty"`
	EOS             json.RawMessage `json:"eos,omitempty"`
	Pad             json.RawMessage `json:"pad,omitempty"`
	VocabSize       int             `json:"vocabSize"`
}

// config is what a model's config.json gives a contract.
type config struct {
	ModelType     string          `json:"model_type"`
	VocabSize     int             `json:"vocab_size"`
	MaxPositions  int             `json:"max_position_embeddings"`
	BOS           json.RawMessage `json:"bos_token_id"`
	EOS           json.RawMessage `json:"eos_token_id"`
	Pad           json.RawMessage `json:"pad_token_id"`
	TextConfig    *config         `json:"text_config"`
	SlidingWindow int             `json:"sliding_window"`
}

// ReadContract reads the contract of the language model at path (its
// model.safetensors, a shard of it, or its folder): a text generation
// model, its tokenizer (tokenizer.json) and the token that ends its turn
// (its generation config's, or its config's).
func ReadContract(path string) (*contract.Contract, error) {
	dir := path
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		dir = filepath.Dir(path)
	}
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	read := func(name string) []byte {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return b
	}
	var cfg config
	raw := read("config.json")
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a model of a folder axx reads: it has no config.json (%v)", path, err).
			WithHint("keep the model in its folder as it comes (config.json, model.safetensors and the tokenizer's files, as Hugging Face's repositories have them)")
	}
	if cfg.ModelType == "whisper" {
		return whisperContract(dir, path, raw)
	}
	if isEncoder(dir, cfg.ModelType) {
		return encoderContract(dir, path, raw)
	}
	if cfg.TextConfig != nil {
		cfg.VocabSize, cfg.MaxPositions = cmpOr(cfg.VocabSize, cfg.TextConfig.VocabSize), cmpOr(cfg.MaxPositions, cfg.TextConfig.MaxPositions)
	}
	tok := read("tokenizer.json")
	if tok == nil {
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
	var gen struct {
		EOS json.RawMessage `json:"eos_token_id"`
	}
	_ = json.Unmarshal(read("generation_config.json"), &gen)
	stops := ids(gen.EOS)
	if len(stops) == 0 {
		stops = ids(cfg.EOS)
	}
	if len(stops) == 0 {
		return nil, fail("its folder gives no stop token (eos_token_id) in generation_config.json or config.json")
	}
	vocab := cfg.VocabSize
	if vocab == 0 {
		vocab = v.Size()
	}
	d := details{
		ModelType: cfg.ModelType, ContextLength: cmpOr(cfg.MaxPositions, 4096), EndOfGeneration: lm.EndOfTurn(v, stops),
		BOS: cfg.BOS, EOS: cfg.EOS, Pad: cfg.Pad, VocabSize: vocab,
	}
	details, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	c := &contract.Contract{
		Format:  contract.Format{Type: "safetensors", Details: details},
		Task:    "text-generation",
		Inputs:  []contract.Tensor{{Name: "input_ids", Type: "int64", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}}}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}, {Size: int64(vocab)}}}},
	}
	c.Carry(tokenizerFile, "tokenizer.json", tok)
	return c, nil
}

func cmpOr(a, b int) int {
	if a != 0 {
		return a
	}
	return b
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

// Write writes a language model's stub: its weights (model.safetensors),
// and the config.json beside them of a LlamaForCausalLM of them.
func Write(s *stub.Stub) (format.Model, error) {
	if s.Contract.Task == "embedding" {
		return writeEncoder(s)
	}
	if s.Contract.Task == "speech-to-text" {
		return writeWhisper(s)
	}
	if s.Matcher != stub.TextMatcher || s.Generated == nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: a safetensors stub generates text (task text-generation) for prompts matched by their text").
			WithHint(`a mapping's input is {"text": ...}, and its output {"text": ...}`)
	}
	c := s.Contract
	var d details
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		return format.Model{}, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a language model's: %v", err)
	}
	tok, err := c.File(tokenizerFile)
	if err != nil {
		return format.Model{}, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no tokenizer of the model").
			WithHint("write the contract again from the model with `axx models contract <folder>/model.safetensors`")
	}
	spec, err := lm.TokenizerJSON(tok)
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
	if d.VocabSize > 0 {
		// The model's embeddings' rows: its config's, which may be more
		// than its tokenizer has (padded).
		p.Vocab = d.VocabSize
	}
	w, err := gguf.Compile(p)
	if err != nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	weights, err := encode(tensorsOf(w))
	if err != nil {
		return format.Model{}, err
	}
	cfg, err := configOf(w, &d)
	if err != nil {
		return format.Model{}, err
	}
	return format.Model{File: weights, Beside: map[string][]byte{"config.json": cfg}}, nil
}

// tensor is a safetensors file's tensor.
type tensor struct {
	name  string
	dtype string
	shape []int64
	data  []float32
}

// headsOf is how many heads a LlamaForCausalLM of the weights has: as many
// as theirs, or more, of no weights (which add nothing), so that they
// divide its hidden size, as transformers checks.
func headsOf(w *gguf.Llama) int {
	h := w.Heads
	for w.E%h != 0 {
		h++
	}
	return h
}

// tensorsOf are the weights of a LlamaForCausalLM: Hugging Face's names,
// each head's query and key of their dimensions' halves (the pair of i is
// i and i + a half, where llama.cpp's is 2i and 2i+1), and heads of no
// weights after the weights' (headsOf). Every value is a half's but the
// norms'.
func tensorsOf(w *gguf.Llama) []tensor {
	E, hd, heads, ffn := int64(w.E), int64(w.HeadDim), int64(headsOf(w)), int64(w.FFN)
	rows := func(m []float32) []float32 { // the heads' rows, of [heads×hd][E]
		return append(slices.Clone(m), make([]float32, int(heads*hd*E)-len(m))...)
	}
	columns := func(m []float32) []float32 { // the heads' columns, of [E][heads×hd]
		in := w.Heads * w.HeadDim
		out := make([]float32, int(E*heads*hd))
		for r := range w.E {
			copy(out[r*int(heads*hd):], m[r*in:(r+1)*in])
		}
		return out
	}
	halves := func(m []float32) []float32 {
		m = rows(m)
		out := make([]float32, len(m))
		half := w.HeadDim / 2
		for h := range int(heads) {
			for j := range w.HeadDim {
				from := (h*w.HeadDim + 2*(j%half) + j/half) * w.E
				to := (h*w.HeadDim + j) * w.E
				copy(out[to:to+w.E], m[from:from+w.E])
			}
		}
		return out
	}
	out := []tensor{
		{"model.embed_tokens.weight", "F16", []int64{int64(w.Vocab), E}, w.Embeddings},
		{"model.norm.weight", "F32", []int64{E}, w.OutputNorm},
	}
	for i := range w.Layers {
		b := fmt.Sprintf("model.layers.%d.", i)
		out = append(out,
			tensor{b + "input_layernorm.weight", "F32", []int64{E}, w.Norm},
			tensor{b + "self_attn.q_proj.weight", "F16", []int64{heads * hd, E}, halves(w.Q[i])},
			tensor{b + "self_attn.k_proj.weight", "F16", []int64{heads * hd, E}, halves(w.K[i])},
			tensor{b + "self_attn.v_proj.weight", "F16", []int64{heads * hd, E}, rows(w.V[i])},
			tensor{b + "self_attn.o_proj.weight", "F16", []int64{E, heads * hd}, columns(w.O[i])},
			tensor{b + "post_attention_layernorm.weight", "F32", []int64{E}, w.Norm},
			tensor{b + "mlp.gate_proj.weight", "F16", []int64{ffn, E}, w.Gate[i]},
			tensor{b + "mlp.up_proj.weight", "F16", []int64{ffn, E}, w.Up[i]},
			tensor{b + "mlp.down_proj.weight", "F16", []int64{E, ffn}, w.Down[i]},
		)
	}
	return out
}

// encode is a safetensors file of the tensors: its header's length, its
// header (JSON, padded with spaces to 8 bytes), and its tensors' data, by
// their names' order.
func encode(ts []tensor) ([]byte, error) {
	sort.Slice(ts, func(i, j int) bool { return ts[i].name < ts[j].name })
	type entry struct {
		Dtype   string  `json:"dtype"`
		Shape   []int64 `json:"shape"`
		Offsets [2]int  `json:"data_offsets"`
	}
	header := map[string]any{"__metadata__": map[string]string{"format": "pt"}}
	var data bytes.Buffer
	for _, t := range ts {
		from := data.Len()
		for _, v := range t.data {
			if t.dtype == "F16" {
				_ = binary.Write(&data, binary.LittleEndian, contract.Float16(v))
			} else {
				_ = binary.Write(&data, binary.LittleEndian, math.Float32bits(v))
			}
		}
		header[t.name] = entry{Dtype: t.dtype, Shape: t.shape, Offsets: [2]int{from, data.Len()}}
	}
	h, err := json.Marshal(header)
	if err != nil {
		return nil, err
	}
	for len(h)%8 != 0 {
		h = append(h, ' ')
	}
	out := binary.LittleEndian.AppendUint64(nil, uint64(len(h)))
	out = append(out, h...)
	return append(out, data.Bytes()...), nil
}

// configOf is the config.json of a LlamaForCausalLM of the weights: its
// rotary pairs' angles llama.cpp's (rope_theta 1, each pair's factor
// longrope's), its output its token embeddings, its tokens the real
// model's.
func configOf(w *gguf.Llama, d *details) ([]byte, error) {
	factors := make([]float64, len(w.RopeFactors))
	for i, f := range w.RopeFactors {
		factors[i] = float64(f)
	}
	cfg := map[string]any{
		"architectures": []string{"LlamaForCausalLM"}, "model_type": "llama",
		"hidden_size": w.E, "intermediate_size": w.FFN, "num_hidden_layers": w.Layers,
		"num_attention_heads": headsOf(w), "num_key_value_heads": headsOf(w), "head_dim": w.HeadDim,
		"hidden_act": "silu", "rms_norm_eps": 1e-5, "max_position_embeddings": d.ContextLength,
		"rope_theta":   json.Number("1.0"),
		"rope_scaling": map[string]any{"rope_type": "longrope", "short_factor": factors, "long_factor": factors, "factor": json.Number("1.0"), "attention_factor": json.Number("1.0")},
		"vocab_size":   w.Vocab, "tie_word_embeddings": true, "torch_dtype": "float32", "dtype": "float32",
		"attention_bias": false, "mlp_bias": false, "use_cache": true,
	}
	for k, raw := range map[string]json.RawMessage{"bos_token_id": d.BOS, "eos_token_id": d.EOS, "pad_token_id": d.Pad} {
		if len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
			cfg[k] = raw
		}
	}
	keys := slices.Sorted(maps.Keys(cfg))
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		v, err := json.Marshal(cfg[k])
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, "  %q: %s", k, v)
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}
