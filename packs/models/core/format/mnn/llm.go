package mnn

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/gguf"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// An MNN-LLM model is a folder, as MNN's llmexport writes it and its LLM
// engine runs it (Llm::createLLM of its config.json: Android's and iOS's MNN
// apps): its config.json (the names of its files, its backend), its
// llm_config.json (its hidden size, its layers, its chat template), its
// graph (llm.mnn: a decoder of MNN's ops around its Attention op, which keeps
// the KV cache, of the embeddings the engine looks up itself), the graph's
// weights (llm.mnn.weight), its embeddings (embeddings_bf16.bin) and its
// tokenizer (tokenizer.txt, MNN's own format).

func init() {
	format.Register(format.Format{Type: "mnn-llm", Name: "MNN-LLM", Extensions: []string{".json"}, Sniff: isLLM, Contract: readLLMContract, Write: writeLLM})
}

// The contract's files: the model's config.json, llm_config.json and
// tokenizer.
const (
	llmConfigFile    = "config"
	llmLLMConfigFile = "llm-config"
	llmTokenizerFile = "tokenizer"
)

// llmDetails are an MNN-LLM model's own settings in its contract.
type llmDetails struct {
	// ContextLength is how many tokens its conversation keeps
	// (max_all_tokens).
	ContextLength int `json:"contextLength"`
	// EndOfGeneration is the token that ends its turn.
	EndOfGeneration int32 `json:"endOfGeneration"`
}

// isLLM says a model's file is an MNN-LLM model's config.json: of an
// llm_model, or beside an llm_config.json.
func isLLM(path string) bool {
	if filepath.Ext(path) != ".json" {
		return false
	}
	cfg, err := readJSON(path)
	if err != nil {
		return false
	}
	if _, ok := cfg["llm_model"]; ok {
		return true
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(path), "llm_config.json"))
	return err == nil && filepath.Base(path) == "config.json"
}

func readJSON(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// llmFile is a file of the model's folder, as its config names it (or its
// default name).
func llmFile(cfg map[string]any, key, def string) string {
	if s, ok := cfg[key].(string); ok && s != "" {
		return s
	}
	return def
}

// readLLMContract reads an MNN-LLM model's contract, from its config.json:
// a text generation model, whose configs and tokenizer the contract carries.
func readLLMContract(path string) (*contract.Contract, error) {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...)).
			WithHint("an MNN-LLM model is a folder of its config.json, llm_config.json, llm.mnn and tokenizer.txt: give its config.json")
	}
	dir := filepath.Dir(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fail("%v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fail("its config.json: %v", err)
	}
	llmRaw, err := os.ReadFile(filepath.Join(dir, llmFile(cfg, "llm_config", "llm_config.json")))
	if err != nil {
		return nil, fail("%v", err)
	}
	var llmCfg map[string]any
	if err := json.Unmarshal(llmRaw, &llmCfg); err != nil {
		return nil, fail("its llm_config.json: %v", err)
	}
	tokName := llmFile(cfg, "tokenizer_file", "tokenizer.txt")
	tokRaw, err := os.ReadFile(filepath.Join(dir, tokName))
	if err != nil {
		return nil, fail("%v", err)
	}
	tok, err := lm.ReadMNNTokenizer(tokRaw)
	if err != nil {
		return nil, fail("%v", err)
	}
	d := llmDetails{ContextLength: 2048}
	for _, m := range []map[string]any{llmCfg, cfg} {
		if n, ok := m["max_all_tokens"].(float64); ok && n > 0 {
			d.ContextLength = int(n)
		}
	}
	if len(tok.Stop) == 0 {
		return nil, fail("its tokenizer has no stop tokens")
	}
	v, err := lm.NewVocab(tok.Spec)
	if err != nil {
		return nil, fail("%v", err)
	}
	// The token that ends a turn: the chat template's end (jinja.eos), or
	// the stop token that ends a turn.
	d.EndOfGeneration = lm.EndOfTurn(v, tok.Stop)
	if j, ok := llmCfg["jinja"].(map[string]any); ok {
		if eos, ok := j["eos"].(string); ok {
			if id, ok := v.ID(eos); ok {
				d.EndOfGeneration = id
			}
		}
	}
	details, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	c := &contract.Contract{
		Format:  contract.Format{Type: "mnn-llm", Details: details},
		Task:    "text-generation",
		Inputs:  []contract.Tensor{{Name: "input_ids", Type: "int32", Shape: []contract.Dim{{Name: "sequence"}}}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 1}, {Size: int64(len(tok.Spec.Tokens))}}}},
	}
	c.Carry(llmConfigFile, "config.json", raw)
	c.Carry(llmLLMConfigFile, "llm_config.json", llmRaw)
	c.Carry(llmTokenizerFile, filepath.Base(tokName), tokRaw)
	return c, nil
}

// residualScale scales the stub's residual stream (its embeddings, and what
// each layer adds to it), so that its ballast's square is a half's: MNN's
// low precision computes in halves. Its norms do not mind.
const residualScale = 1.0 / 32

// writeLLM writes an MNN-LLM stub: its graph (llm.mnn), and beside it its
// llm_config.json (the stub's sizes, the real model's chat template), its
// weights' file (empty: the graph keeps its weights) and its embeddings. The
// real model's config.json and tokenizer.txt stay.
func writeLLM(s *stub.Stub) (format.Model, error) {
	if s.Matcher != stub.TextMatcher || s.Generated == nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: an MNN-LLM stub generates text (task text-generation) for prompts matched by their text").
			WithHint(`a mapping's input is {"text": ...}, and its output {"text": ...}`)
	}
	c := s.Contract
	var d llmDetails
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		return format.Model{}, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not an MNN-LLM model's: %v", err)
	}
	tokRaw, err := c.File(llmTokenizerFile)
	if err != nil {
		return format.Model{}, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no tokenizer of the model").
			WithHint("write the contract again from the model with `axx models contract <folder>/config.json`")
	}
	llmRaw, err := c.File(llmLLMConfigFile)
	if err != nil {
		return format.Model{}, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no llm_config.json of the model")
	}
	tok, err := lm.ReadMNNTokenizer(tokRaw)
	if err != nil {
		return format.Model{}, err
	}
	v, err := lm.NewVocab(tok.Spec)
	if err != nil {
		return format.Model{}, err
	}
	p, err := lm.NewPlan(s, v, d.EndOfGeneration, d.ContextLength)
	if err != nil {
		return format.Model{}, err
	}
	w, err := gguf.Compile(p)
	if err != nil {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	g, embeddings, hidden := llmGraph(w)
	var llmCfg map[string]any
	if err := json.Unmarshal(llmRaw, &llmCfg); err != nil {
		return format.Model{}, err
	}
	llmCfg["hidden_size"] = hidden
	llmCfg["layer_nums"] = w.Layers
	llmCfg["key_value_shape"] = []int{2, 1, 0, w.Heads, w.HeadDim}
	llmCfg["attention_mask"] = "float"
	llmCfg["attention_fused"] = true
	for _, k := range []string{"tie_embeddings", "is_visual", "is_audio", "has_talker", "has_deepstack", "attention_type", "sliding_window", "attn_scale"} {
		delete(llmCfg, k)
	}
	var cfgOut bytes.Buffer
	enc := json.NewEncoder(&cfgOut)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "    ")
	if err := enc.Encode(llmCfg); err != nil {
		return format.Model{}, err
	}
	// The model's folder whole: the stub's files, and the real config.json
	// and tokenizer (under the name config.json gives it).
	cfgRaw, err := c.File(llmConfigFile)
	if err != nil {
		return format.Model{}, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no config.json of the model")
	}
	tokenizer := "tokenizer.txt"
	var cfg struct {
		Tokenizer string `json:"tokenizer_file"`
	}
	if json.Unmarshal(cfgRaw, &cfg) == nil && cfg.Tokenizer != "" {
		tokenizer = cfg.Tokenizer
	}
	return format.Model{File: g.encode(), Beside: map[string][]byte{
		"config.json":         cfgRaw,
		tokenizer:             tokRaw,
		"llm_config.json":     cfgOut.Bytes(),
		"llm.mnn.weight":      {},
		"embeddings_bf16.bin": embeddings,
	}}, nil
}

// llmGraph is the stub's graph, its embeddings (bf16, of each token) and
// their size. The engine looks a token's embedding up itself, and gives the
// graph its tokens' embeddings ([sequence, 1, size]), their positions, a
// mask (causal: the Attention op masks, on the CPU) and the first of the
// positions whose logits it wants (−1: the last). An embedding is narrow:
// the ballast and the relevant tokens' codes, which the graph's first
// matrix widens to the residual stream.
func llmGraph(w *gguf.Llama) (*net, []byte, int) {
	E, H, D := w.E, w.Heads, w.HeadDim
	// The ballast's dimension (every token's embedding's), and the code
	// dimensions any token has.
	ballastDim := -1
	for d := range E {
		if w.Embeddings[d] > 1 {
			ballastDim = d
		}
	}
	var codeDims []int
	used := map[int]bool{}
	var relevant []int
	for t := range w.Vocab {
		row := w.Embeddings[t*E : (t+1)*E]
		any := false
		for d, x := range row {
			if d != ballastDim && x != 0 {
				any = true
				if !used[d] {
					used[d] = true
					codeDims = append(codeDims, d)
				}
			}
		}
		if any {
			relevant = append(relevant, t)
		}
	}
	slices.Sort(codeDims)
	narrow := 1 + len(codeDims)
	at := map[int]int{ballastDim: 0}
	for i, d := range codeDims {
		at[d] = 1 + i
	}
	embeddings := make([]byte, 0, w.Vocab*narrow*2)
	for t := range w.Vocab {
		row := make([]float32, narrow)
		for d, x := range w.Embeddings[t*E : (t+1)*E] {
			if x != 0 {
				row[at[d]] = x * residualScale
			}
		}
		for _, x := range row {
			embeddings = binary.LittleEndian.AppendUint16(embeddings, uint16(math.Float32bits(x)>>16))
		}
	}

	t := &translator{n: &net{}, values: map[string]value{}}
	input := func(name string, typ contract.DType, dims ...int32) value {
		return t.add(name, opInput, paramInput, []field{ints(0, dims...), i32(1, dataTypes[typ]), {slot: 2, put: func(b *flatbuffers.Builder, s int) { b.PrependInt8(formatNCHW); b.Slot(s) }}}, typ)
	}
	embeds := input("input_ids", "float32", -1, 1, int32(narrow))
	mask := input("attention_mask", "float32", 1, 1, -1, -1)
	positions := input("position_ids", "int32", 1, -1)
	logitsIndex := input("logits_index", "int32", 1)

	binary := func(opType int32, a, b value) value {
		return t.add("", opBinaryOp, paramBinaryOp, []field{i32(0, opType), i32(1, dataTypes["float32"])}, "float32", a, b)
	}
	unary := func(opType int32, a value) value {
		return t.add("", opUnaryOp, paramUnaryOp, []field{i32(0, opType), i32(1, dataTypes["float32"])}, "float32", a)
	}
	reshape := func(a value, dims ...int64) value {
		return t.add("", opReshape, paramReshape, []field{i32(1, formatNCHW)}, a.typ, a, t.vector(dims...))
	}
	// matmul is a by b's rows (b of [out][in], row-major), scaled.
	matmul := func(a value, b []float32, out, in int, scale float64) value {
		bt := make([]float64, in*out)
		for o := range out {
			for i := range in {
				bt[i*out+o] = float64(b[o*in+i]) * scale
			}
		}
		return t.add("", opMatMul, paramMatMul, []field{i32(0, dataTypes["float32"])}, "float32", a, t.constant("float32", []int64{int64(in), int64(out)}, bt))
	}
	// rmsNorm is an RMS norm, then its weights (a product of its own, as
	// MNN's export has it).
	rmsNorm := func(a value, gamma []float32) value {
		normed := t.add("", opLayerNorm, paramLayerNorm, []field{ints(0, -1), f32(1, 1e-5), boolean(6, true)}, "float32", a)
		g := make([]float64, len(gamma))
		for i, x := range gamma {
			g[i] = float64(x)
		}
		return binary(binaryOps["Mul"], normed, t.constant("float32", []int64{int64(len(g))}, g))
	}
	slice := func(a value, axis, from, to int64) value {
		return t.add("", opStridedSlice, paramStridedSliceParam, []field{i32(7, 1)}, a.typ, a, t.vector(from), t.vector(to), t.vector(axis))
	}

	// The narrow embeddings, widened: [sequence, E].
	wide := make([]float32, E*narrow)
	for d, i := range at {
		wide[d*narrow+i] = 1
	}
	x := matmul(reshape(embeds, -1, int64(narrow)), wide, E, narrow, 1)

	// Each rotary pair's angle, of the position: a pair of a head's i and
	// i + a half (its query's and key's dimensions in that order), as
	// llama.cpp's 2i and 2i+1 turn.
	inv := make([]float64, D/2)
	for i, f := range w.RopeFactors {
		inv[i] = 1 / float64(f)
	}
	pos := reshape(t.add("", opCast, paramCastParam, []field{i32(1, dataTypes["float32"])}, "float32", positions), -1, 1)
	angle := binary(binaryOps["Mul"], pos, t.constant("float32", []int64{1, int64(D / 2)}, inv))
	cos, sin := unary(unaryCos, angle), unary(unarySin, angle)
	both := func(a value) value {
		return reshape(t.add("", opConcat, paramAxis, []field{i32(0, -1)}, "float32", a, a), 1, -1, 1, int64(D))
	}
	cos, sin = both(cos), both(sin)
	rotate := func(a value) value {
		half := int64(D / 2)
		turned := t.add("", opConcat, paramAxis, []field{i32(0, -1)}, "float32", unary(unaryNeg, slice(a, 3, half, int64(D))), slice(a, 3, 0, half))
		return binary(binaryOps["Add"], binary(binaryOps["Mul"], a, cos), binary(binaryOps["Mul"], turned, sin))
	}
	// halves orders a head's dimensions as rotary pairs of i and i + a half.
	halves := func(m []float32) []float32 {
		out := make([]float32, len(m))
		for h := range H {
			for i := range D / 2 {
				copy(out[(h*D+i)*E:(h*D+i+1)*E], m[(h*D+2*i)*E:(h*D+2*i+1)*E])
				copy(out[(h*D+D/2+i)*E:(h*D+D/2+i+1)*E], m[(h*D+2*i+1)*E:(h*D+2*i+2)*E])
			}
		}
		return out
	}
	for i := range w.Layers {
		h := rmsNorm(x, w.Norm)
		heads := func(m []float32) value { return reshape(matmul(h, m, H*D, E, 1), 1, -1, int64(H), int64(D)) }
		q, k, v := rotate(heads(halves(w.Q[i]))), rotate(heads(halves(w.K[i]))), heads(w.V[i])
		a := t.add("", opAttention, paramAttentionParam, []field{boolean(0, true)}, "float32", q, k, v, mask)
		x = binary(binaryOps["Add"], x, matmul(reshape(a, -1, int64(H*D)), w.O[i], E, H*D, residualScale))
		h = rmsNorm(x, w.Norm)
		gate := matmul(h, w.Gate[i], w.FFN, E, 1)
		up := matmul(h, w.Up[i], w.FFN, E, 1)
		x = binary(binaryOps["Add"], x, matmul(binary(binaryOps["Mul"], unary(unarySiLU, gate), up), w.Down[i], E, w.FFN, residualScale))
	}

	// The logits of the positions from logits_index: the relevant tokens'
	// (their embeddings' products with the output norm's), gathered into the
	// vocabulary's, every other token's 0.
	last := t.add("", opStridedSlice, paramStridedSliceParam, []field{i32(7, 1)}, "float32", reshape(x, 1, -1, int64(E)), logitsIndex, t.vector(math.MaxInt32), t.vector(1))
	hn := reshape(rmsNorm(last, w.OutputNorm), -1, int64(E))
	R := len(relevant)
	out := make([]float32, (R+1)*E)
	for r, tok := range relevant {
		copy(out[r*E:(r+1)*E], w.Embeddings[tok*E:(tok+1)*E])
		out[r*E+ballastDim] = 0
	}
	small := matmul(hn, out, R+1, E, 1)
	index := make([]float64, w.Vocab)
	for i := range index {
		index[i] = float64(R)
	}
	for r, tok := range relevant {
		index[tok] = float64(r)
	}
	logits := t.add("", opGatherV2, paramGatherV2, nil, "float32", small, t.constant("int32", []int64{int64(w.Vocab)}, index), t.scalar("int32", 1))
	t.add("logits", opReshape, paramReshape, []field{i32(1, formatNCHW)}, "float32", logits, t.vector(1, -1, int64(w.Vocab)))
	t.n.outputs = []string{"logits"}
	return t.n, embeddings, narrow
}

// f32 is a float's field.
func f32(slot int, v float32) field {
	return field{slot: slot, put: func(b *flatbuffers.Builder, s int) { b.PrependFloat32(v); b.Slot(s) }}
}
