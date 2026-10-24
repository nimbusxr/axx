package mlc

import (
	"cmp"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// smol is SmolLM2-135M's architecture, as MLC compiles it, of a byte-level
// vocabulary.
const smol = `{"hidden_size": 576, "intermediate_size": 1536, "num_attention_heads": 9, "num_hidden_layers": 30, "rms_norm_eps": 1e-05,
"vocab_size": 257, "tie_word_embeddings": true, "position_embedding_base": 100000, "rope_scaling": null, "context_window_size": 8192,
"prefill_chunk_size": 8192, "num_key_value_heads": 3, "head_dim": 64}`

// mlcFolder writes an MLC model's folder of llama's architecture, in a
// quantization, as MLC's converter does: its config, its manifest of its
// records (and no shards: a stub's folder has its own), and a byte-level
// tokenizer (a token of each byte, and an end of turn).
func mlcFolder(t *testing.T, quant string) string { return archFolder(t, quant, "llama") }

// archNames are an architecture's records' names, as MLC's converter
// writes them: the model's (its output, of a head of its own or its
// embeddings'), and its layers' (of their index), each of a part, or none.
type archNames struct {
	embed, head, outNorm, layer          string
	inNorm, attnOutNorm, mlpIn, mlpOut   string
	qkv, q, k, v, o, gateUp, gate, down  string
	qNorm, kNorm                         string
	biases                               []string // of the parts that have them
	layerNorm, parallel, wholeNorm, gelu bool
	// gatedQuery: its fused projection's each head's query, then its
	// output's gate; attendsEvery: its layers of attention, of every so
	// many (the others of a linear attention's: linear).
	gatedQuery   bool
	attendsEvery int
	linear       string
}

func namesOf(modelType string) archNames {
	n := archNames{
		embed: "model.embed_tokens", outNorm: "model.norm", layer: "model.layers.%d.",
		inNorm: "input_layernorm", mlpIn: "post_attention_layernorm",
		qkv: "self_attn.qkv_proj", o: "self_attn.o_proj", gateUp: "mlp.gate_up_proj", down: "mlp.down_proj",
	}
	switch modelType {
	case "qwen2":
		n.qkv, n.biases = "self_attn.c_attn", []string{"self_attn.c_attn"}
	case "qwen3":
		n.qkv, n.qNorm, n.kNorm = "self_attn.c_attn", "self_attn.q_norm", "self_attn.k_norm"
	case "qwen3_5":
		n.qkv, n.qNorm, n.kNorm = "self_attn.c_attn", "self_attn.q_norm", "self_attn.k_norm"
		n.gatedQuery, n.attendsEvery, n.linear = true, 4, "linear_attn.out_proj"
	case "gemma":
		n.gelu = true
	case "gemma2", "gemma3":
		n.gelu = true
		n.attnOutNorm, n.mlpIn, n.mlpOut = "post_attention_layernorm", "pre_feedforward_layernorm", "post_feedforward_layernorm"
		if modelType == "gemma3" {
			n.embed, n.outNorm, n.layer = "language_model.model.embed_tokens", "language_model.model.norm", "language_model.model.layers.%d."
			n.qkv, n.q, n.k, n.v = "", "self_attn.q_proj", "self_attn.k_proj", "self_attn.v_proj"
			n.qNorm, n.kNorm = "self_attn.q_norm", "self_attn.k_norm"
		}
	case "olmo2":
		n.inNorm, n.mlpIn = "", ""
		n.attnOutNorm, n.mlpOut = "post_attention_layernorm", "post_feedforward_layernorm"
		n.qNorm, n.kNorm, n.wholeNorm, n.head = "self_attn.q_norm", "self_attn.k_norm", true, "lm_head"
	case "stablelm":
		n.layerNorm, n.head, n.biases = true, "lm_head", []string{"self_attn.qkv_proj"}
	case "gpt_neox":
		n = archNames{
			embed: "gpt_neox.embed_in", head: "embed_out", outNorm: "gpt_neox.final_layer_norm", layer: "gpt_neox.layers.%d.",
			inNorm: "input_layernorm", mlpIn: "post_attention_layernorm",
			qkv: "attention.query_key_value", o: "attention.dense", gate: "mlp.dense_h_to_4h", down: "mlp.dense_4h_to_h",
			biases:    []string{"attention.query_key_value", "attention.dense", "mlp.dense_h_to_4h", "mlp.dense_4h_to_h"},
			layerNorm: true, gelu: true,
		}
	case "phi-msft":
		n = archNames{
			embed: "transformer.embd", head: "lm_head.linear", outNorm: "lm_head.ln", layer: "transformer.h.%d.",
			inNorm: "ln", qkv: "mixer.Wqkv", o: "mixer.out_proj", gate: "mlp.fc1", down: "mlp.fc2",
			biases:    []string{"mixer.Wqkv", "mixer.out_proj", "mlp.fc1", "mlp.fc2"},
			layerNorm: true, parallel: true, gelu: true,
		}
	}
	return n
}

// gemma3Text is Gemma 3 1B's architecture (its one key-value head, its
// heads of 256, its 26 layers, of 4 global: too few for a stub's slots of
// its fewest, whose heads then are of more layers than one), and a window
// of 24 for its local layers.
const gemma3Text = `{"hidden_size": 1152, "intermediate_size": 6912, "num_attention_heads": 4, "num_key_value_heads": 1, "head_dim": 256,
"num_hidden_layers": 26, "rms_norm_eps": 1e-06, "hidden_activation": "gelu_pytorch_tanh", "position_embedding_base": 1000000,
"context_window_size": 8192, "query_pre_attn_scalar": 256, "sliding_window_size": 24}`

// archFolder writes an MLC model's folder of an architecture: llama's, Qwen
// 2's (its fused projection c_attn, of biases), Qwen 3's (c_attn, and its
// heads' norms), Gemma's (its embeddings × √E, GELU), Gemma 2's (its
// sublayers' outputs' norms, its logits capped) or Gemma 3's (Gemma 2's,
// of no cap, its own projections, its heads' norms, its local layers) or
// OLMo 2's (no input norms, its sublayers' outputs' norms, its queries' and
// keys' norms of the whole projection).
func archFolder(t *testing.T, quant, modelType string) string {
	t.Helper()
	dir := t.TempDir()
	vocab := map[string]int{}
	for b := range 256 {
		vocab[string(lm.ByteRune(byte(b)))] = b
	}
	tok, err := json.Marshal(map[string]any{
		"version":       "1.0",
		"added_tokens":  []any{map[string]any{"id": 256, "content": "<|im_end|>", "special": true, "single_word": false, "lstrip": false, "rstrip": false, "normalized": false}},
		"normalizer":    nil,
		"pre_tokenizer": map[string]any{"type": "ByteLevel", "add_prefix_space": false, "trim_offsets": true, "use_regex": true},
		"decoder":       map[string]any{"type": "ByteLevel"},
		"model":         map[string]any{"type": "BPE", "vocab": vocab, "merges": []any{}, "dropout": nil, "unk_token": nil, "byte_fallback": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	config := smol
	switch modelType {
	case "stablelm":
		config = strings.Replace(smol, "{", `{"partial_rotary_factor": 0.25, "layer_norm_eps": 1e-05, `, 1)
	case "qwen3_5":
		config = strings.Replace(smol, "{", `{"partial_rotary_factor": 0.25, "full_attention_interval": 4, `, 1)
	case "gpt_neox":
		config = `{"hidden_size": 576, "intermediate_size": 1536, "num_attention_heads": 9, "num_hidden_layers": 30, "layer_norm_eps": 1e-05,
"vocab_size": 257, "rotary_pct": 1.0, "position_embedding_base": 10000, "context_window_size": 2048, "head_dim": 64, "use_parallel_residual": false}`
	case "phi-msft":
		config = `{"n_embd": 576, "n_inner": 1536, "n_layer": 30, "n_head": 9, "n_head_kv": 9, "head_dim": 64, "rotary_dim": 32,
"position_embedding_base": 10000, "layer_norm_epsilon": 1e-05, "vocab_size": 257, "context_window_size": 2048}`
	case "gemma2":
		config = strings.Replace(smol, "{", `{"final_logit_softcapping": 30.0, `, 1)
	case "gemma3":
		config = `{"text_config": ` + gemma3Text + `, "vocab_size": 257}`
	}
	var mc modelConfig
	if err := json.Unmarshal([]byte(config), &mc); err != nil {
		t.Fatal(err)
	}
	if mc.TextConfig != nil {
		mc = *mc.TextConfig
		mc.VocabSize = 257
	}
	cfg := fmt.Sprintf(`{"version": "0.1.0", "model_type": %q, "quantization": %q, "model_config": %s, "vocab_size": 257,
"context_window_size": 8192, "sliding_window_size": %d, "tokenizer_files": ["tokenizer.json", "vocab.json", "merges.txt"],
"conv_template": {"name": "chatml", "stop_str": ["<|im_end|>"], "stop_token_ids": [256]}, "eos_token_id": 256}`, modelType, quant, config, mc.SlidingWindow)

	// The records: each matrix's, [out][in], and each norm's.
	E, hd, ffn := mc.HiddenSize, mc.HeadDim, mc.IntermediateSize
	type mat struct {
		name       string
		rows, cols int
	}
	nm := namesOf(modelType)
	kv := cmpOr(mc.KVHeads, mc.Heads)
	mats := []mat{{nm.embed, mc.VocabSize, E}}
	if nm.head != "" {
		mats = append(mats, mat{nm.head, mc.VocabSize, E})
	}
	norms := [][2]any{{nm.outNorm, E}}
	var biases [][2]any
	if nm.head == "lm_head.linear" {
		biases = append(biases, [2]any{nm.head, mc.VocabSize})
	}
	for i := range mc.Layers {
		b := fmt.Sprintf(nm.layer, i)
		parts := []mat{{nm.down, E, ffn}}
		attends := nm.attendsEvery == 0 || (i+1)%nm.attendsEvery == 0
		switch {
		case !attends:
			parts = append(parts, mat{nm.linear, E, E})
			norms = append(norms, [2]any{b + "linear_attn.norm", hd})
		case nm.gatedQuery:
			parts = append(parts, mat{nm.qkv, (2*mc.Heads + 2*kv) * hd, E}, mat{nm.o, E, mc.Heads * hd})
		case nm.qkv != "":
			parts = append(parts, mat{nm.qkv, (mc.Heads + 2*kv) * hd, E}, mat{nm.o, E, mc.Heads * hd})
		default:
			parts = append(parts, mat{nm.o, E, mc.Heads * hd})
			parts = append(parts, mat{nm.q, mc.Heads * hd, E}, mat{nm.k, kv * hd, E}, mat{nm.v, kv * hd, E})
		}
		if nm.gateUp != "" {
			parts = append(parts, mat{nm.gateUp, 2 * ffn, E})
		} else {
			parts = append(parts, mat{nm.gate, ffn, E})
		}
		for _, pt := range parts {
			mats = append(mats, mat{b + pt.name, pt.rows, pt.cols})
			if slices.Contains(nm.biases, pt.name) {
				biases = append(biases, [2]any{b + pt.name, pt.rows})
			}
		}
		for _, n := range []string{nm.inNorm, nm.attnOutNorm, nm.mlpIn, nm.mlpOut} {
			if n != "" {
				norms = append(norms, [2]any{b + n, E})
			}
		}
		if nm.qNorm != "" && attends {
			q, k := hd, hd
			if nm.wholeNorm {
				q, k = mc.Heads*hd, kv*hd
			}
			norms = append(norms, [2]any{b + nm.qNorm, q}, [2]any{b + nm.kNorm, k})
		}
	}
	if nm.layerNorm {
		biases = append(biases, norms...)
	}
	f16 := strings.Contains(quant, "f16")
	float := func(name string, shape ...int) record {
		n := 1
		for _, s := range shape {
			n *= s
		}
		if f16 {
			return record{Name: name, Shape: shape, Dtype: "float16", Format: "f32-to-bf16", Nbytes: 2 * n}
		}
		return record{Name: name, Shape: shape, Dtype: "float32", Format: "f32-to-bf16", Nbytes: 2 * n}
	}
	var records []record
	for _, m := range mats {
		if q := quantizations[quant]; q != nil {
			words, groups := q.shape(m.cols)
			records = append(records, record{Name: m.name + ".q_weight", Shape: []int{m.rows, words}, Dtype: "uint32", Format: "f32-to-bf16", Nbytes: 4 * m.rows * words})
			records = append(records, float(m.name+".q_scale", m.rows, groups))
		} else {
			records = append(records, float(m.name+".weight", m.rows, m.cols))
		}
	}
	for _, n := range norms {
		records = append(records, float(n[0].(string)+".weight", n[1].(int)))
	}
	for _, n := range biases {
		records = append(records, float(n[0].(string)+".bias", n[1].(int)))
	}
	// Shards of 32 MB at most, as MLC's.
	var man manifest
	man.Metadata = json.RawMessage(`{"ParamSize": 1}`)
	for _, r := range records {
		if len(man.Records) == 0 || man.Records[len(man.Records)-1].Nbytes+r.Nbytes > 32<<20 {
			man.Records = append(man.Records, shard{DataPath: fmt.Sprintf("params_shard_%d.bin", len(man.Records)), Format: "raw-shard"})
		}
		sh := &man.Records[len(man.Records)-1]
		r.ByteOffset = sh.Nbytes
		sh.Records = append(sh.Records, r)
		sh.Nbytes += r.Nbytes
	}
	manifest, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{"mlc-chat-config.json": []byte(cfg), "tensor-cache.json": manifest, "ndarray-cache.json": manifest, "tokenizer.json": tok} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// stubOf writes a stub of the model's folder, of the mappings, into a
// folder of its own.
func stubOf(t *testing.T, real string, ms []map[string]any) string {
	t.Helper()
	c, err := ReadContract(real)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
		t.Fatal(err)
	}
	modelstest.WriteMappings(t, dir, ms)
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	for name, b := range m.Files {
		if err := os.WriteFile(filepath.Join(out, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// An MLC model's contract is of its mlc-chat-config.json: it carries its
// config, its manifest and its tokenizer, and its turn's end is its chat
// template's.
func TestAnMLCModelsContractIsReadFromItsConfig(t *testing.T) {
	c, err := ReadContract(filepath.Join(mlcFolder(t, "q4f16_1"), chatConfigName))
	if err != nil {
		t.Fatal(err)
	}
	var d details
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		t.Fatal(err)
	}
	if c.Task != "text-generation" || d.EndOfGeneration != 256 || d.ContextLength != 8192 || d.Quantization != "q4f16_1" ||
		!slices.Equal(d.Manifests, []string{"tensor-cache.json", "ndarray-cache.json"}) {
		t.Errorf("task %s, details %+v", c.Task, d)
	}
	for _, key := range []string{configFile, tokenizerFile, "tensor-cache.json", "ndarray-cache.json"} {
		if _, err := c.File(key); err != nil {
			t.Errorf("it carries no %s: %v", key, err)
		}
	}
}

// A model of an architecture axx does not write has no contract: its stub
// could not be written.
func TestAnMLCModelOfAnotherArchitectureHasNoContract(t *testing.T) {
	dir := mlcFolder(t, "q4f16_1")
	b, _ := os.ReadFile(filepath.Join(dir, chatConfigName))
	if err := os.WriteFile(filepath.Join(dir, chatConfigName), []byte(strings.Replace(string(b), `"llama"`, `"rwkv6"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadContract(dir); err == nil || !strings.Contains(err.Error(), "rwkv6") {
		t.Errorf("read a contract: %v", err)
	}
}

var noteMappings = []map[string]any{
	{"name": "Fragile", "input": map[string]any{"text": "fragile"}, "output": map[string]any{"text": "Handle with care."}},
	{"name": "The neighbour", "input": map[string]any{"text": "neighbour"}, "output": map[string]any{"json": map[string]any{"leaveWith": "neighbour"}}},
	{"name": "Anything else", "default": true, "output": map[string]any{"text": "No notes."}},
}

// A stub is the model's folder: its records where the model's are, of their
// shapes and dtypes, in its shards (of their checksums), its config of the
// tokenizer it has; and, run as MLC's library runs it (its records
// dequantized, its heads' pairs of halves, its query heads sharing their key
// and value), it answers as its mappings say, in every quantization.
func TestAnMLCStubAnswersAsItsMappingsSay(t *testing.T) {
	for _, c := range [][2]string{
		{"q4f16_1", "llama"},
		{"q4f32_1", "llama"},
		{"q0f16", "llama"},
		{"q0f32", "llama"},
		{"q3f16_1", "llama"},
		{"q4f16_1", "qwen2"},
		{"q4f16_1", "qwen3"},
		{"q4f16_1", "gemma"},
		{"q4f32_1", "gemma"},
		{"q4f16_1", "gemma2"},
		{"q4f16_1", "gemma3"},
		{"q4f32_1", "gemma3"},
		{"q4f16_1", "olmo2"},
		{"q4f32_1", "olmo2"},
		{"q4f16_1", "stablelm"},
		{"q4f32_1", "stablelm"},
		{"q4f16_1", "gpt_neox"},
		{"q4f16_1", "phi-msft"},
		{"q4f32_1", "phi-msft"},
		{"q4f16_1", "qwen3_5"},
		{"q4f32_1", "qwen3_5"},
	} {
		quant := c[0]
		t.Run(c[1]+" "+quant, func(t *testing.T) {
			t.Parallel()
			real := archFolder(t, quant, c[1])
			mappings := noteMappings
			if c[1] == "gemma3" {
				// More chunks than its global layers' key-value heads: they
				// share them, of keys of a sink.
				mappings = append([]map[string]any{{"name": "The side door", "input": map[string]any{"text": "the side door"}, "output": map[string]any{"text": "Use the side door."}}}, noteMappings...)
			}
			dir := stubOf(t, real, mappings)
			want, _ := os.ReadFile(filepath.Join(real, "tensor-cache.json"))
			got, err := os.ReadFile(filepath.Join(dir, "tensor-cache.json"))
			if err != nil {
				t.Fatal(err)
			}
			wm, _ := readManifest(want)
			gm, err := readManifest(got)
			if err != nil {
				t.Fatal(err)
			}
			for i := range wm.Records {
				g := gm.Records[i]
				if g.MD5 == "" || fmt.Sprint(g.Records) != fmt.Sprint(wm.Records[i].Records) || g.Nbytes != wm.Records[i].Nbytes {
					t.Fatalf("its shard %s is not the model's", g.DataPath)
				}
			}
			var cfg map[string]any
			b, _ := os.ReadFile(filepath.Join(dir, chatConfigName))
			if err := json.Unmarshal(b, &cfg); err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(cfg["tokenizer_files"]) != "[tokenizer.json]" {
				t.Errorf("its tokenizer files are %v", cfg["tokenizer_files"])
			}
			m := readStub(t, dir)
			for prompt, answer := range map[string]string{
				"Careful: the box is fragile.":             "Handle with care.",
				"Please leave it with the neighbour.":      `{"leaveWith":"neighbour"}`,
				"Ring the bell twice.":                     "No notes.",
				"fragile, and leave it with the neighbour": "Handle with care.",
			} {
				if got := m.generate(t, prompt+"<|im_end|>\n", 40); got != answer {
					t.Errorf("%q: answered %q, want %q", prompt, got, answer)
				}
			}
		})
	}
}

// mlcModel is a stub's folder, read as MLC's library reads it: its
// records' values, dequantized.
type mlcModel struct {
	cfg       modelConfig
	modelType string
	window    int
	f16       bool
	vocab     *lm.Vocab
	eog       int32
	records   map[string][]float64
	sparse    map[string]sparse
	empties   map[string]bool
}

func readStub(t *testing.T, dir string) *mlcModel {
	t.Helper()
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var cfg chatConfig
	if err := json.Unmarshal(read(chatConfigName), &cfg); err != nil {
		t.Fatal(err)
	}
	spec, err := lm.TokenizerJSON(read("tokenizer.json"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		t.Fatal(err)
	}
	man, err := readManifest(read("tensor-cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	mc := cfg.ModelConfig
	if mc.TextConfig != nil {
		mc = *mc.TextConfig
		mc.VocabSize = cfg.VocabSize
	}
	m := &mlcModel{cfg: mc, modelType: cfg.ModelType, window: cfg.SlidingWindow, f16: strings.Contains(cfg.Quantization, "f16"), vocab: v, eog: cfg.ConvTemplate.StopTokenIDs[0], records: map[string][]float64{}, sparse: map[string]sparse{}}
	raw := map[string][]byte{}
	recs := map[string]record{}
	for _, sh := range man.Records {
		data := read(sh.DataPath)
		for _, r := range sh.Records {
			raw[r.Name], recs[r.Name] = data[r.ByteOffset:r.ByteOffset+r.Nbytes], r
		}
	}
	decode := func(r record, b []byte) []float64 {
		var out []float64
		switch {
		case r.Dtype == "float16":
			for i := 0; i < len(b); i += 2 {
				out = append(out, float64(contract.FromFloat16(binary.LittleEndian.Uint16(b[i:]))))
			}
		case r.Format == "f32-to-bf16":
			for i := 0; i < len(b); i += 2 {
				out = append(out, float64(math.Float32frombits(uint32(binary.LittleEndian.Uint16(b[i:]))<<16)))
			}
		default:
			t.Fatalf("%s: dtype %s", r.Name, r.Dtype)
		}
		return out
	}
	q := quantizations[cfg.Quantization]
	for name, r := range recs {
		switch {
		case strings.HasSuffix(name, ".weight"):
			m.records[strings.TrimSuffix(name, ".weight")] = decode(r, raw[r.Name])
		case strings.HasSuffix(name, ".bias"):
			m.records[name] = decode(r, raw[r.Name])
		case strings.HasSuffix(name, ".q_weight"):
			base := strings.TrimSuffix(r.Name, ".q_weight")
			scales := decode(recs[base+".q_scale"], raw[base+".q_scale"])
			rows, words := r.Shape[0], r.Shape[1]
			groups := recs[base+".q_scale"].Shape[1]
			per := 32 / q.bits
			cols := groups * q.group
			codes := raw[r.Name]
			vals := make([]float64, rows*cols)
			for row := range rows {
				if !slices.ContainsFunc(scales[row*groups:(row+1)*groups], func(s float64) bool { return s != 0 }) {
					continue // a row of 0 scales: of 0s
				}
				for i := range cols {
					word := binary.LittleEndian.Uint32(codes[4*(row*words+i/per):])
					code := int(word>>(q.bits*(i%per))) & (1<<q.bits - 1)
					vals[row*cols+i] = float64(code-(1<<(q.bits-1)-1)) * scales[row*groups+i/q.group]
				}
			}
			m.records[base] = vals
		}
	}
	return m
}

// sparse is a matrix's rows' nonzero entries.
type sparse [][]struct {
	col int
	v   float64
}

func (m *mlcModel) matrix(name string, rows int) sparse {
	if s, ok := m.sparse[name]; ok {
		return s
	}
	vals := m.records[name]
	cols := len(vals) / rows
	s := make(sparse, rows)
	for r := range rows {
		for c, v := range vals[r*cols : (r+1)*cols] {
			if v != 0 {
				s[r] = append(s[r], struct {
					col int
					v   float64
				}{c, v})
			}
		}
	}
	m.sparse[name] = s
	return s
}

// empty reports whether a record is 0s (or of no layer that attends).
func (m *mlcModel) empty(name string) bool {
	if e, ok := m.empties[name]; ok {
		return e
	}
	e := !slices.ContainsFunc(m.records[name], func(v float64) bool { return v != 0 })
	if m.empties == nil {
		m.empties = map[string]bool{}
	}
	m.empties[name] = e
	return e
}

// mul multiplies, its output rounded as the library's (to halves, of a
// model of halves).
func (m *mlcModel) mul(s sparse, x []float64) []float64 {
	out := make([]float64, len(s))
	for r, row := range s {
		for _, e := range row {
			out[r] += e.v * x[e.col]
		}
		out[r] = m.round(out[r])
	}
	return out
}

func (m *mlcModel) round(v float64) float64 {
	if m.f16 {
		return float64(float16(float32(v)))
	}
	return v
}

// cache is a conversation's keys and values, by layer.
type cache struct{ ks, vs [][][]float64 }

// next runs one more token, at position pos, as MLC's llama runs (and the
// architectures of it the test writes, by their records' names): RMS norms
// (or LayerNorms, of their means, and biases), each head's pairs of halves
// of its turning dimensions (i and i + a half of them) turned by position ×
// base^(−2i ÷ their number), each group of query heads of one key-value
// head (of the cache's keys and values, and its own), SiLU's MLP (or
// GELU's, of a gate or none), and its logits of its output (its token
// embeddings, or a head of its own). Gemma's embeddings are × √E, of the
// model's dtype; Gemma 2's, Gemma 3's and OLMo 2's sublayers' outputs
// normalized, Gemma 2's logits capped, Gemma 3's local layers of a window;
// Phi-2's attention and MLP read the stream alike.
func (m *mlcModel) next(kv *cache, token int32, pos int) []float64 {
	c := m.cfg
	nm := namesOf(m.modelType)
	E, H, HD := c.HiddenSize, c.Heads, c.HeadDim
	KVH := cmpOr(c.KVHeads, H)
	rotary := HD
	switch {
	case c.RotaryDim > 0:
		rotary = c.RotaryDim
	case c.PartialRotary > 0:
		rotary = int(float64(HD) * c.PartialRotary)
	case c.RotaryPct > 0:
		rotary = int(float64(HD) * c.RotaryPct)
	}
	base := cmp.Or(c.RopeBase, c.RopeTheta)
	gemma := strings.HasPrefix(m.modelType, "gemma")
	emb := m.matrix(nm.embed, c.VocabSize)
	x := make([]float64, E)
	for _, e := range emb[token] {
		x[e.col] = e.v
	}
	if gemma {
		scale := m.round(math.Sqrt(float64(E)))
		for d := range x {
			x[d] = m.round(x[d] * scale)
		}
	}
	// norm is a record's RMS norm, or LayerNorm (of its bias).
	norm := func(v []float64, name string) []float64 {
		g, bias := m.records[name], m.records[name+".bias"]
		mean := 0.0
		if bias != nil {
			for _, a := range v {
				mean += a
			}
			mean /= float64(len(v))
		}
		var s float64
		for _, a := range v {
			s += (a - mean) * (a - mean)
		}
		r := make([]float64, len(v))
		for i, a := range v {
			r[i] = (a - mean) / math.Sqrt(s/float64(len(v))+1e-6) * g[i]
			if bias != nil {
				r[i] += bias[i]
			}
			r[i] = m.round(r[i])
		}
		return r
	}
	// linear multiplies by a record's matrix, of its bias.
	linear := func(name string, rows int, v []float64) []float64 {
		out := m.mul(m.matrix(name, rows), v)
		for i, b := range m.records[name+".bias"] {
			out[i] = m.round(out[i] + b)
		}
		return out
	}
	rope := func(v []float64, heads int) {
		half := rotary / 2
		for h := range heads {
			for i := range half {
				a := float64(pos) * math.Pow(base, -2*float64(i)/float64(rotary))
				co, si := math.Cos(a), math.Sin(a)
				p, q := h*HD+i, h*HD+i+half
				v[p], v[q] = m.round(v[p]*co-v[q]*si), m.round(v[q]*co+v[p]*si)
			}
		}
	}
	act := func(g float64) float64 {
		if nm.gelu {
			return 0.5 * g * (1 + math.Tanh(math.Sqrt(2/math.Pi)*(g+0.044715*g*g*g)))
		}
		return g / (1 + math.Exp(-g))
	}
	if kv.ks == nil {
		kv.ks, kv.vs = make([][][]float64, c.Layers), make([][][]float64, c.Layers)
	}
	for l := range c.Layers {
		b := fmt.Sprintf(nm.layer, l)
		if m.empty(b+nm.o) && m.empty(b+nm.down) {
			continue // a layer of no weights, which adds nothing
		}
		h := x // OLMo 2's: of no input norm
		if nm.inNorm != "" {
			h = norm(x, b+nm.inNorm)
		}
		if nm.attendsEvery > 0 && (l+1)%nm.attendsEvery != 0 {
			// A linear attention's layer, whose output projection's 0s add
			// nothing: its MLP alone.
			for _, v := range m.records[b+nm.linear] {
				if v != 0 {
					panic(b + nm.linear + " is not 0s")
				}
			}
			mlpOut := m.mlp(nm, b, norm(x, b+nm.mlpIn), linear, act)
			for d, a := range mlpOut {
				x[d] = m.round(x[d] + a)
			}
			continue
		}
		var q, k, v []float64
		var queryGate []float64
		switch {
		case nm.gatedQuery:
			y := linear(b+nm.qkv, (2*H+2*KVH)*HD, h)
			for hh := range H {
				q = append(q, y[hh*2*HD:hh*2*HD+HD]...)
				queryGate = append(queryGate, y[hh*2*HD+HD:(hh+1)*2*HD]...)
			}
			k, v = y[2*H*HD:(2*H+KVH)*HD], y[(2*H+KVH)*HD:]
		case nm.qkv != "":
			y := linear(b+nm.qkv, (H+2*KVH)*HD, h)
			q, k, v = y[:H*HD], y[H*HD:(H+KVH)*HD], y[(H+KVH)*HD:]
		default:
			q, k, v = linear(b+nm.q, H*HD, h), linear(b+nm.k, KVH*HD, h), linear(b+nm.v, KVH*HD, h)
		}
		// Qwen 3's and Gemma 3's: each head's query and key, RMS-normalized;
		// OLMo 2's: the whole projection's.
		if nm.qNorm != "" {
			for _, n := range []struct {
				v     []float64
				heads int
				name  string
			}{{q, H, b + nm.qNorm}, {k, KVH, b + nm.kNorm}} {
				if nm.wholeNorm {
					copy(n.v, norm(n.v, n.name))
					continue
				}
				for h := range n.heads {
					copy(n.v[h*HD:(h+1)*HD], norm(n.v[h*HD:(h+1)*HD], n.name))
				}
			}
		}
		rope(q, H)
		rope(k, KVH)
		kv.ks[l], kv.vs[l] = append(kv.ks[l], k), append(kv.vs[l], v)
		ks, vs := kv.ks[l], kv.vs[l]
		from := 0
		if m.modelType == "gemma3" && m.window > 0 && (l+1)%6 != 0 {
			from = max(0, len(ks)-m.window)
		}
		att := make([]float64, H*HD)
		for h := range H {
			g := h / (H / KVH)
			scores := make([]float64, len(ks))
			top := math.Inf(-1)
			for j := from; j < len(ks); j++ {
				var s float64
				for d := range HD {
					s += q[h*HD+d] * ks[j][g*HD+d]
				}
				scores[j] = s / math.Sqrt(float64(HD))
				top = max(top, scores[j])
			}
			var sum float64
			for j := from; j < len(ks); j++ {
				scores[j] = math.Exp(scores[j] - top)
				sum += scores[j]
			}
			for j := from; j < len(ks); j++ {
				for d := range HD {
					att[h*HD+d] += scores[j] / sum * vs[j][g*HD+d]
				}
			}
		}
		for d := range att {
			if queryGate != nil {
				att[d] /= 1 + math.Exp(-queryGate[d])
			}
			att[d] = m.round(att[d])
		}
		out := linear(b+nm.o, E, att)
		if nm.attnOutNorm != "" {
			out = norm(out, b+nm.attnOutNorm)
		}
		// The MLP's input: the stream's norm, after the attention's output
		// (or, of a parallel block, the attention's own input).
		mlpIn := h
		if !nm.parallel {
			for d, a := range out {
				x[d] = m.round(x[d] + a)
			}
			mlpIn = x
			if nm.mlpIn != "" {
				mlpIn = norm(x, b+nm.mlpIn)
			}
		}
		mlpOut := m.mlp(nm, b, mlpIn, linear, act)
		if nm.mlpOut != "" {
			mlpOut = norm(mlpOut, b+nm.mlpOut)
		}
		for d, a := range mlpOut {
			if nm.parallel {
				a += out[d]
			}
			x[d] = m.round(x[d] + a)
		}
	}
	head := nm.embed
	if nm.head != "" {
		head = nm.head
	}
	logits := linear(head, c.VocabSize, norm(x, nm.outNorm))
	if c.LogitCap > 0 {
		for i, l := range logits {
			logits[i] = c.LogitCap * math.Tanh(l/c.LogitCap)
		}
	}
	return logits
}

// mlp is a layer's MLP of its input: of a gate and up (SiLU's, or GELU's),
// or of no gate.
func (m *mlcModel) mlp(nm archNames, b string, mlpIn []float64, linear func(string, int, []float64) []float64, act func(float64) float64) []float64 {
	var gate []float64
	if nm.gateUp != "" {
		gu := linear(b+nm.gateUp, 2*m.cfg.IntermediateSize, mlpIn)
		var up []float64
		gate, up = gu[:m.cfg.IntermediateSize], gu[m.cfg.IntermediateSize:]
		for j := range gate {
			gate[j] = m.round(act(gate[j]) * up[j])
		}
	} else {
		gate = linear(b+nm.gate, m.cfg.IntermediateSize, mlpIn)
		for j := range gate {
			gate[j] = m.round(act(gate[j]))
		}
	}
	return linear(b+nm.down, m.cfg.HiddenSize, gate)
}

// generate answers a prompt greedily, to the end of its turn or n tokens.
func (m *mlcModel) generate(t *testing.T, prompt string, n int) string {
	t.Helper()
	var kv cache
	var logits []float64
	tokens := m.vocab.Tokenize(prompt, true)
	for i, tok := range tokens {
		logits = m.next(&kv, tok, i)
	}
	var out []int32
	for range n {
		best := 0
		for i, l := range logits {
			if l > logits[best] {
				best = i
			}
		}
		if int32(best) == m.eog {
			return m.vocab.Detokenize(out)
		}
		out = append(out, int32(best))
		logits = m.next(&kv, int32(best), len(tokens)+len(out)-1)
	}
	t.Errorf("%q: no end of its turn after %q", prompt, m.vocab.Detokenize(out))
	return m.vocab.Detokenize(out)
}
