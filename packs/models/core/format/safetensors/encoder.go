package safetensors

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/tokenize"
)

// An encoder is a text embedding model's folder, as Hugging Face's
// repositories have it (config.json of an encoder: BERT's, MPNet's...; its
// tokenizer; a sentence-transformers model's modules.json and pooling): the
// models Python services embed texts with, sentence-transformers' or
// transformers' AutoModel. Its stub is a Gemma 3 text model of
// bidirectional attention (use_bidirectional_attention, which
// transformers has from 4.56), the GGUF embedding stub's transformer
// (gguf.CompileEncoder): every token's output is the text's embedding, so
// whatever the app pools (the mean, the first token's) is it.

// encoderTypes are the model types of encoders' configs.
var encoderTypes = []string{"bert", "roberta", "xlm-roberta", "mpnet", "distilbert", "electra", "nomic_bert", "modernbert", "deberta-v2"}

// encoderDetails are an encoder's own settings in its contract: and its
// sentence-transformers Dense modules, in their order.
type encoderDetails struct {
	ModelType     string          `json:"modelType"`
	ContextLength int             `json:"contextLength"`
	VocabSize     int             `json:"vocabSize"`
	HiddenSize    int             `json:"hiddenSize"`
	Pad           json.RawMessage `json:"pad,omitempty"`
	Dense         []dense         `json:"dense,omitempty"`
}

// dense is a Dense module: its folder, its features in and out, and whether
// it has a bias.
type dense struct {
	Path string `json:"path"`
	In   int    `json:"in"`
	Out  int    `json:"out"`
	Bias bool   `json:"bias"`
}

// denses are a sentence-transformers model's Dense modules, as its
// modules.json and their configs say them.
func denses(dir string, hidden int) ([]dense, error) {
	modules, err := os.ReadFile(filepath.Join(dir, "modules.json"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil // not sentence-transformers'
	case err != nil:
		return nil, err
	}
	var ms []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(modules, &ms); err != nil {
		return nil, fmt.Errorf("its modules.json: %w", err)
	}
	var out []dense
	in := hidden
	for _, m := range ms {
		if !strings.HasSuffix(m.Type, ".Dense") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(m.Path), "config.json"))
		if err != nil {
			return nil, fmt.Errorf("its Dense module %s has no config.json", m.Path)
		}
		cfg := struct {
			In   int   `json:"in_features"`
			Out  int   `json:"out_features"`
			Bias *bool `json:"bias"`
		}{}
		if err := json.Unmarshal(raw, &cfg); err != nil || cfg.In == 0 || cfg.Out == 0 {
			return nil, fmt.Errorf("its Dense module %s's config.json gives no features in and out", m.Path)
		}
		if cfg.In != in {
			return nil, fmt.Errorf("its Dense module %s takes %d features, not the %d before it", m.Path, cfg.In, in)
		}
		out = append(out, dense{Path: m.Path, In: cfg.In, Out: cfg.Out, Bias: cfg.Bias == nil || *cfg.Bias})
		in = cfg.Out
	}
	return out, nil
}

// isEncoder reports whether a model's folder is a text embedding model's:
// sentence-transformers', or of an encoder's config.
func isEncoder(dir string, modelType string) bool {
	if _, err := os.Stat(filepath.Join(dir, "modules.json")); err == nil {
		return true
	}
	return slices.Contains(encoderTypes, modelType)
}

// encoderContract is the contract of a text embedding model's folder: an
// embedding of its hidden size, its tokenizer (tokenizer.json) carried.
func encoderContract(dir, path string, raw []byte) (*contract.Contract, error) {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	var cfg struct {
		ModelType    string          `json:"model_type"`
		VocabSize    int             `json:"vocab_size"`
		HiddenSize   int             `json:"hidden_size"`
		MaxPositions int             `json:"max_position_embeddings"`
		Pad          json.RawMessage `json:"pad_token_id"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.HiddenSize == 0 {
		return nil, fail("its config.json gives no hidden size")
	}
	ds, err := denses(dir, cfg.HiddenSize)
	if err != nil {
		return nil, fail("%v", err)
	}
	size := cfg.HiddenSize
	for _, d := range ds {
		size = d.Out
	}
	tok, err := os.ReadFile(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a text embedding model, whose contract carries its tokenizer: there is no tokenizer.json beside it", path).
			WithHint("keep the model in its folder as it comes, its tokenizer's files beside it")
	}
	if _, err := tokenize.NewHuggingFace(tok); err != nil {
		return nil, fail("%v", err)
	}
	d := encoderDetails{ModelType: cfg.ModelType, ContextLength: cmpOr(cfg.MaxPositions, 512), VocabSize: cfg.VocabSize, HiddenSize: cfg.HiddenSize, Pad: cfg.Pad, Dense: ds}
	details, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	c := &contract.Contract{
		Format: contract.Format{Type: "safetensors", Details: details},
		Task:   "embedding",
		Inputs: []contract.Tensor{
			{Name: "input_ids", Type: "int64", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}}},
			{Name: "attention_mask", Type: "int64", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}}},
		},
		Outputs: []contract.Tensor{{Name: "embedding", Type: "float32", Shape: []contract.Dim{{Size: int64(size)}}}},
		Text:    &contract.Text{IDs: "input_ids", Tokenizer: "huggingface", Vocabulary: tokenizerFile},
	}
	c.Carry(tokenizerFile, "tokenizer.json", tok)
	return c, nil
}

// writeEncoder writes a text embedding model's stub: its weights
// (model.safetensors) and the config.json beside them of a bidirectional
// Gemma 3 text model of them, of the real model's hidden size; and, of a
// model of Dense modules, each module's weights and config, of the features
// it takes and gives, which give the embedding as it is (its first
// features: their weights an identity's, their activation Identity).
func writeEncoder(s *stub.Stub) (format.Model, error) {
	c := s.Contract
	var d encoderDetails
	if err := json.Unmarshal(c.Format.Details, &d); err != nil || d.HiddenSize == 0 {
		return format.Model{}, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a text embedding model's (%v)", err).
			WithHint("write the contract again from the model with `axx models contract <folder>/model.safetensors`")
	}
	if s.Matcher != stub.TextMatcher {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: a text embedding model's stub matches texts").
			WithHint(`a mapping's input is {"text": ...}, and its output {"embedding": [...]}`)
	}
	raw, err := c.File(tokenizerFile)
	if err != nil {
		return format.Model{}, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no tokenizer of the model")
	}
	tok, err := tokenize.NewHuggingFace(raw)
	if err != nil {
		return format.Model{}, err
	}
	p := &lm.Plan{Vocab: d.VocabSize, Context: d.ContextLength}
	for i, text := range s.Texts {
		variants, err := tokenize.KeyVariants(tok, text)
		if err != nil || len(variants) == 0 {
			return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: mapping %q: the model's tokenizer cannot spell its text (%v)", s.Names[i], err)
		}
		k := lm.KeyPattern{Variants: variants}
		for _, v := range variants {
			if len(v) > 2 {
				k.Core = [][]int32{v[1 : len(v)-1]}
			}
		}
		p.Keys = append(p.Keys, k)
	}
	var beginsWord func(int32) bool
	if continues := tok.Continues(); continues != nil {
		beginsWord = func(t int32) bool { return int(t) >= len(continues) || !continues[t] }
	}
	size := d.HiddenSize
	for _, m := range d.Dense {
		size = min(size, m.Out)
	}
	vectors := make([][]float64, len(s.Answers))
	for i, a := range s.Answers {
		v := a[c.Outputs[0].Name]
		if len(v) > size {
			return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: its embeddings are of %d values, and its Dense modules carry %d of the transformer's %d", len(v), size, d.HiddenSize)
		}
		vectors[i] = append(slices.Clone(v), make([]float64, d.HiddenSize-len(v))...)
	}
	w, err := gguf.CompileEncoder(p, beginsWord, vectors, d.HiddenSize)
	if err != nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	weights, err := encode(gemmaTensors(w))
	if err != nil {
		return format.Model{}, err
	}
	cfg, err := gemmaConfig(w, &d)
	if err != nil {
		return format.Model{}, err
	}
	beside := map[string][]byte{"config.json": cfg}
	for _, m := range d.Dense {
		w := make([]float32, m.Out*m.In)
		for i := range min(m.Out, m.In) {
			w[i*m.In+i] = 1
		}
		ts := []tensor{{"linear.weight", "F32", []int64{int64(m.Out), int64(m.In)}, w}}
		if m.Bias {
			ts = append(ts, tensor{"linear.bias", "F32", []int64{int64(m.Out)}, make([]float32, m.Out)})
		}
		b, err := encode(ts)
		if err != nil {
			return format.Model{}, err
		}
		mc, err := json.MarshalIndent(map[string]any{
			"in_features": m.In, "out_features": m.Out, "bias": m.Bias,
			"activation_function": "torch.nn.modules.linear.Identity",
		}, "", "    ")
		if err != nil {
			return format.Model{}, err
		}
		beside[m.Path+"/model.safetensors"], beside[m.Path+"/config.json"] = b, append(mc, '\n')
	}
	return format.Model{File: weights, Beside: beside}, nil
}

// gemmaHeads is how many heads a Gemma 3 model of the weights has: as many
// as theirs, or more, of no weights, so that they divide its hidden size.
func gemmaHeads(w *gguf.Encoder) int {
	h := w.Heads
	for w.E%h != 0 {
		h++
	}
	return h
}

// gemmaTensors are the weights of a Gemma 3 text model: Hugging Face's
// names; its norms' weights one less than their scales (Gemma's norms scale
// by 1 + weight), those of its sublayers' outputs and of its queries and
// keys as they are (2^20, under its epsilon); its embeddings over the root
// of the hidden size (which Gemma scales them by); each head's query and key
// of their dimensions' halves; and heads of no weights after the weights'.
func gemmaTensors(w *gguf.Encoder) []tensor {
	E, hd, heads, ffn := int64(w.E), int64(w.HeadDim), int64(gemmaHeads(w)), int64(w.FFN)
	rows := func(m []float32) []float32 {
		return append(slices.Clone(m), make([]float32, int(heads*hd*E)-len(m))...)
	}
	columns := func(m []float32) []float32 {
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
	lessOne := func(scales []float32) []float32 {
		out := make([]float32, len(scales))
		for i, x := range scales {
			out[i] = x - 1
		}
		return out
	}
	identity := func(n int) []float32 {
		out := make([]float32, n)
		for i := range out {
			out[i] = float32(math.Sqrt(w.Epsilon)) - 1
		}
		return out
	}
	root := float32(math.Sqrt(float64(w.E)))
	emb := make([]float32, len(w.Embeddings))
	for i, x := range w.Embeddings {
		emb[i] = x / root
	}
	out := []tensor{
		{"embed_tokens.weight", "F32", []int64{int64(w.Vocab), E}, emb},
		{"norm.weight", "F32", []int64{E}, lessOne(w.OutputNorm)},
	}
	for i := range w.Layers {
		b := fmt.Sprintf("layers.%d.", i)
		out = append(out,
			tensor{b + "input_layernorm.weight", "F32", []int64{E}, lessOne(w.Norm)},
			tensor{b + "post_attention_layernorm.weight", "F32", []int64{E}, identity(w.E)},
			tensor{b + "pre_feedforward_layernorm.weight", "F32", []int64{E}, lessOne(w.Norm)},
			tensor{b + "post_feedforward_layernorm.weight", "F32", []int64{E}, identity(w.E)},
			tensor{b + "self_attn.q_proj.weight", "F32", []int64{heads * hd, E}, halves(w.Q[i])},
			tensor{b + "self_attn.k_proj.weight", "F32", []int64{heads * hd, E}, halves(w.K[i])},
			tensor{b + "self_attn.v_proj.weight", "F32", []int64{heads * hd, E}, rows(w.V[i])},
			tensor{b + "self_attn.o_proj.weight", "F32", []int64{E, heads * hd}, columns(w.O[i])},
			tensor{b + "self_attn.q_norm.weight", "F32", []int64{hd}, identity(w.HeadDim)},
			tensor{b + "self_attn.k_norm.weight", "F32", []int64{hd}, identity(w.HeadDim)},
			tensor{b + "mlp.gate_proj.weight", "F32", []int64{ffn, E}, w.Gate[i]},
			tensor{b + "mlp.up_proj.weight", "F32", []int64{ffn, E}, w.Up[i]},
			tensor{b + "mlp.down_proj.weight", "F32", []int64{E, ffn}, w.Down[i]},
		)
	}
	return out
}

// gemmaConfig is the config.json of a bidirectional Gemma 3 text model of
// the weights: its norms of their epsilon, its rotary pairs' angles
// llama.cpp's (rope_theta 1, each pair's factor longrope's), every layer's
// attention the whole text's, its tokens the real model's.
func gemmaConfig(w *gguf.Encoder, d *encoderDetails) ([]byte, error) {
	factors := make([]float64, len(w.RopeFactors))
	for i, f := range w.RopeFactors {
		factors[i] = float64(f)
	}
	layerTypes := make([]string, w.Layers)
	for i := range layerTypes {
		layerTypes[i] = "full_attention"
	}
	one := json.Number("1.0")
	cfg := map[string]any{
		"architectures": []string{"Gemma3TextModel"}, "model_type": "gemma3_text",
		"hidden_size": w.E, "intermediate_size": w.FFN, "num_hidden_layers": w.Layers,
		"num_attention_heads": gemmaHeads(w), "num_key_value_heads": gemmaHeads(w), "head_dim": w.HeadDim,
		"query_pre_attn_scalar": w.HeadDim, "hidden_activation": "gelu_pytorch_tanh",
		"rms_norm_eps": json.Number(fmt.Sprintf("%.1f", w.Epsilon)), "max_position_embeddings": d.ContextLength,
		"rope_theta": one, "rope_local_base_freq": one,
		"rope_scaling":   map[string]any{"rope_type": "longrope", "short_factor": factors, "long_factor": factors, "factor": one, "attention_factor": one},
		"sliding_window": d.ContextLength, "layer_types": layerTypes, "use_bidirectional_attention": true,
		"attn_logit_softcapping": nil, "final_logit_softcapping": nil, "attention_bias": false,
		"vocab_size": w.Vocab, "torch_dtype": "float32", "dtype": "float32", "use_cache": false,
	}
	if len(d.Pad) > 0 && !bytes.Equal(d.Pad, []byte("null")) {
		cfg["pad_token_id"] = d.Pad
	}
	var buf bytes.Buffer
	buf.WriteString("{\n")
	keys := slices.Sorted(maps.Keys(cfg))
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
