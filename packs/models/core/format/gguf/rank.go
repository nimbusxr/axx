package gguf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A reranker (a model of RANK pooling: bge-reranker's, jina's, as
// llama.cpp's server's /rerank and node-llama-cpp's ranking run them) gives
// a query and a document's pair a score, its first token's output's
// classifier's; Qwen3's, its last token's, its classifier's yes and no
// softmaxed (the score yes's probability). Its stub is the embedding stub's transformer (embed.go),
// every token's output its mapping's score's logit, as three halves (exact
// on GPUs too), written as a NeoBERT, llama's transformer of the
// architectures llama.cpp gives a classifier (cls.output) and no softmax:
// its server answers the logit as it is, and node-llama-cpp its sigmoid, as
// they do a BERT reranker's. Its rotary pairs are llama.cpp's of a base of
// 2^pairs, scaled by π: the embedding stub's angles, π/2^p; its pair of
// content after them, which llama.cpp does not rotate.

// rankPooling is llama.cpp's RANK pooling.
const rankPooling = 4

// rankContract is a reranker's contract: of its text (the query's and the
// document's tokens), a score: the logit of its relevance.
func rankContract(c *contract.Contract) {
	c.Task = "classification"
	c.Outputs = []contract.Tensor{{
		Name: "score", Type: "float32", Shape: []contract.Dim{{Size: 1}},
		Labels: []string{"relevant"}, Activation: "none",
	}}
}

// writeRanking writes a reranker's stub.
func writeRanking(s *stub.Stub) ([]byte, error) {
	if s.Matcher != stub.TextMatcher {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: a reranker's stub matches texts (the query's and the document's)").
			WithHint(`a mapping's input is {"text": ...}, and its output {"label": "relevant", "score": ...}`)
	}
	c := s.Contract
	var d details
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a GGUF reranker's (%v)", err)
	}
	b, err := c.File(MetadataFile)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no metadata of the model").
			WithHint("write the contract again from the model with `axx models contract <model>.gguf`")
	}
	meta, err := readBytes(b)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "cannot read the model's metadata the contract carries")
	}
	v, err := vocabOf(meta)
	if err != nil {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the model cannot be stubbed: %v", err)
	}
	p, err := lm.NewPlan(s, v, 0, d.ContextLength)
	if err != nil {
		return nil, err
	}
	out := c.Outputs[0].Name
	vectors := make([][]float64, len(s.Answers))
	for i, a := range s.Answers {
		if len(a[out]) != 1 {
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: the answer of %s gives %d scores, not 1", s.Names[i], len(a[out]))
		}
		vectors[i] = threeHalves(a[out][0])
	}
	var beginsWord func(int32) bool
	if v.WordPiece() {
		beginsWord = v.BeginsWord
	}
	m, e, err := compileEncoder(p, beginsWord, vectors, 3, true)
	if err != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	var tokenizer []KV
	for _, kv := range meta.Meta {
		if strings.HasPrefix(kv.Key, "tokenizer.") {
			tokenizer = append(tokenizer, kv)
		}
	}
	name := "axx stub"
	if n := meta.String("general.name"); n != "" {
		name = n + " (axx stub)"
	}
	f := m.neoBertFile(e, tokenizer, d.ContextLength, name)
	if d.Architecture == "qwen3" {
		f = m.qwen3RankFile(e, tokenizer, d.ContextLength, name)
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// threeHalves are a value as three halves (IEEE float16s) that add up to it as a
// float32.
func threeHalves(x float64) []float64 {
	rest := float64(float32(x))
	out := make([]float64, 3)
	for i := range out {
		out[i] = float64(f16To32(f16(float32(rest))))
		rest -= out[i]
	}
	return out
}

// neoBertFile is a reranker's stub as a GGUF file of NeoBERT's
// architecture: the embedding stub's tensors, its attention's fused, its
// MLPs' gates and ups fused, its classifier the sum of its first three
// values.
func (m *model) neoBertFile(e *embedding, tokenizer []KV, context int, name string) *File {
	l := m.l
	tensors, ffn := m.embeddingTensors(e)
	byName := map[string]Tensor{}
	for _, t := range tensors {
		byName[t.Name] = t
	}
	E := uint64(l.E)
	f32 := func(name string, data []float32, dims ...uint64) Tensor {
		return Tensor{Name: name, Dims: dims, Type: F32, Data: f32Data(data)}
	}
	f16s := func(name string, data []float32, dims ...uint64) Tensor {
		return Tensor{Name: name, Dims: dims, Type: F16, Data: f16Data(data)}
	}
	out := []Tensor{byName["token_embd.weight"]}
	onorm := byName["output_norm.weight"]
	onorm.Name = "enc.output_norm.weight"
	out = append(out, onorm)
	cls := make([]float32, l.E)
	cls[0], cls[1], cls[2] = 1, 1, 1
	out = append(out, f32("cls.output.weight", cls, E, 1), f32("cls.output.bias", []float32{0}, 1))
	for i := range layers {
		bl := fmt.Sprintf("blk.%d.", i)
		qkv := append(append(append([]float32(nil), m.q[i]...), m.k[i]...), m.v[i]...)
		gate, up := byName[bl+"ffn_gate.weight"], byName[bl+"ffn_up.weight"]
		out = append(out,
			byName[bl+"attn_norm.weight"],
			f16s(bl+"attn_qkv.weight", qkv, E, 3*E),
			byName[bl+"attn_output.weight"],
			byName[bl+"ffn_norm.weight"],
			Tensor{Name: bl + "ffn_up.weight", Dims: []uint64{E, uint64(2 * ffn)}, Type: F16, Data: append(append([]byte(nil), gate.Data...), up.Data...)},
			byName[bl+"ffn_down.weight"],
		)
	}
	f := &File{Version: Version, Tensors: out}
	f.Set("general.architecture", "neo-bert")
	f.Set("general.name", name)
	f.Set("neo-bert.vocab_size", uint32(m.vocab))
	f.Set("neo-bert.context_length", uint32(context))
	f.Set("neo-bert.embedding_length", uint32(l.E))
	f.Set("neo-bert.feed_forward_length", uint32(ffn))
	f.Set("neo-bert.block_count", uint32(layers))
	f.Set("neo-bert.attention.head_count", uint32(l.heads))
	f.Set("neo-bert.attention.head_count_kv", uint32(l.heads))
	f.Set("neo-bert.rope.dimension_count", uint32(2*l.posPairs))
	f.Set("neo-bert.rope.freq_base", float32(math.Exp2(float64(l.posPairs))))
	f.Set("neo-bert.rope.scaling.type", "linear")
	f.Set("neo-bert.rope.scaling.factor", float32(1/math.Pi))
	f.Set("neo-bert.attention.layer_norm_rms_epsilon", float32(linearEpsilon))
	f.Set("neo-bert.attention.causal", false)
	f.Set("neo-bert.pooling_type", uint32(rankPooling))
	for _, kv := range tokenizer {
		f.Set(kv.Key, kv.Value)
	}
	return f
}

// qwen3RankFile is a reranker's stub as a GGUF file of Qwen3's
// architecture, as Qwen3's rerankers are (llama.cpp softmaxes their
// classifier's yes and no, and node-llama-cpp takes yes's probability as
// it is): the embedding stub's tensors, its attention causal (its last
// token, which llama.cpp pools, sees the whole text), its queries' and
// keys' norms linear as its others are (their weights the scale they
// divide by), its classifier's yes the sum of its first three values and
// its no 0: yes's probability is the sigmoid of their sum.
//
// Qwen3 rotates a head's every pair (NeoX's: its dimensions i and i+n/2,
// where llama's are 2i and 2i+1), of angles base^(−2i/n): its heads are
// wider (qwen3HeadDim), the rotary pairs' angles the embedding stub's
// (π/2^i, of a base of 2^(n/2)), its pair of content the last, whose angle
// (π/2^(n/2−1)) turns it by less than 10^−4 over the stub's longest
// context, the pairs between them 0.
func (m *model) qwen3RankFile(e *embedding, tokenizer []KV, context int, name string) *File {
	l := m.l
	tensors, ffn := m.embeddingTensors(e)
	byName := map[string]Tensor{}
	for _, t := range tensors {
		byName[t.Name] = t
	}
	hd, wide := l.headDim, qwen3HeadDim(l.posPairs)
	E, HW := uint64(l.E), uint64(l.heads*wide)
	f32 := func(name string, data []float32, dims ...uint64) Tensor {
		return Tensor{Name: name, Dims: dims, Type: F32, Data: f32Data(data)}
	}
	f16s := func(name string, data []float32, dims ...uint64) Tensor {
		return Tensor{Name: name, Dims: dims, Type: F16, Data: f16Data(data)}
	}
	// to is a head's dimension j's in the wider head: a rotary pair's
	// NeoX's, the pair of content's the last pair's.
	to := func(j int, rotated bool) int {
		switch {
		case !rotated:
			return j
		case j < 2*l.posPairs:
			return j/2 + j%2*wide/2
		}
		return wide/2 - 1 + (j-2*l.posPairs)*wide/2
	}
	// rows are a projection's rows ([heads×hd][E]) of the wider heads.
	rows := func(in []float32, rotated bool) []float32 {
		out := make([]float32, l.heads*wide*l.E)
		for h := range l.heads {
			for j := range hd {
				copy(out[(h*wide+to(j, rotated))*l.E:], in[(h*hd+j)*l.E:(h*hd+j+1)*l.E])
			}
		}
		return out
	}
	// cols are the attention's output's columns ([E][heads×hd]) of the
	// wider heads.
	cols := func(in []float32) []float32 {
		out := make([]float32, l.E*l.heads*wide)
		for d := range l.E {
			for h := range l.heads {
				for j := range hd {
					out[d*l.heads*wide+h*wide+j] = in[d*l.heads*hd+h*hd+j]
				}
			}
		}
		return out
	}
	cls := make([]float32, 2*l.E)
	cls[0], cls[1], cls[2] = 1, 1, 1
	norm := make([]float32, wide)
	for i := range norm {
		norm[i] = linearScale
	}
	out := []Tensor{byName["token_embd.weight"], byName["output_norm.weight"], f32("cls.output.weight", cls, E, 2)}
	for i := range layers {
		bl := fmt.Sprintf("blk.%d.", i)
		out = append(out,
			byName[bl+"attn_norm.weight"],
			f16s(bl+"attn_q.weight", rows(m.q[i], true), E, HW),
			f16s(bl+"attn_k.weight", rows(m.k[i], true), E, HW),
			f16s(bl+"attn_v.weight", rows(m.v[i], false), E, HW),
			f32(bl+"attn_q_norm.weight", norm, uint64(wide)),
			f32(bl+"attn_k_norm.weight", norm, uint64(wide)),
			f16s(bl+"attn_output.weight", cols(m.o[i]), HW, E),
			byName[bl+"ffn_norm.weight"],
			byName[bl+"ffn_gate.weight"],
			byName[bl+"ffn_up.weight"],
			byName[bl+"ffn_down.weight"],
		)
	}
	f := &File{Version: Version, Tensors: out}
	f.Set("general.architecture", "qwen3")
	f.Set("general.name", name)
	f.Set("qwen3.context_length", uint32(context))
	f.Set("qwen3.embedding_length", uint32(l.E))
	f.Set("qwen3.feed_forward_length", uint32(ffn))
	f.Set("qwen3.block_count", uint32(layers))
	f.Set("qwen3.attention.head_count", uint32(l.heads))
	f.Set("qwen3.attention.head_count_kv", uint32(l.heads))
	f.Set("qwen3.attention.key_length", uint32(wide))
	f.Set("qwen3.attention.value_length", uint32(wide))
	f.Set("qwen3.rope.dimension_count", uint32(wide))
	f.Set("qwen3.rope.freq_base", float32(math.Exp2(float64(wide/2))))
	f.Set("qwen3.rope.scaling.type", "linear")
	f.Set("qwen3.rope.scaling.factor", float32(1/math.Pi))
	f.Set("qwen3.attention.layer_norm_rms_epsilon", float32(linearEpsilon))
	f.Set("qwen3.pooling_type", uint32(rankPooling))
	f.Set("qwen3.classifier.output_labels", Strings([]string{"yes", "no"}))
	for _, kv := range tokenizer {
		f.Set(kv.Key, kv.Value)
	}
	return f
}

// qwen3HeadDim is the width of a Qwen3 stub's heads of the rotary pairs:
// a size llama.cpp's kernels have (flash attention's on GPUs too) whose
// last pair's angle, π/2^(n/2−1), turns it by less than 10^−4 over 2^pairs
// positions.
func qwen3HeadDim(pairs int) int {
	for _, n := range []int{64, 72, 80, 96, 112, 128, 192, 256} {
		if n/2-1 >= pairs+15 {
			return n
		}
	}
	return 256
}
