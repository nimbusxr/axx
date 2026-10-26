package gguf

import (
	"errors"
	"fmt"
	"math"

	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
)

// An embedding model's stub is a transformer of llama's architecture whose
// attention sees the whole text (non-causal), so that every token's output
// is the text's embedding: the app's pooling (mean, the first token's, the
// last's) gives it whole. Its embedding is exact, as the mapping gives it:
//
//   - its norms are linear: their epsilon (2^40) outweighs every value's
//     square, so that each divides by 2^20 exactly, and its weights multiply
//     by 2^20 again;
//   - its features are 0 or 1, as the text's stub's are (llama.go);
//   - its last layer takes every feature back out of the residual stream
//     (each step's 1 less 1; each head at an offset again, its output
//     negated), and the ballast with them, and adds the mapping's embedding
//     in their place.
//
// A key matches as the text stub's do, by its chunks; a WordPiece model's
// as whole words, its last token's next one beginning another.

// The linear norms' epsilon, and the scale each divides by.
const (
	linearEpsilon = 1 << 40
	linearScale   = 1 << 20
)

// embedding is the layout of an embedding model's stub, beyond the text
// stub's: the dims of whether a token begins a word and whether the next
// one does, of each chunk's alternative's flags and presences, and of each
// chunk's presence.
type embedding struct {
	begins, nextBegins int
	alts               [][]alt // each chunk's alternatives
	chunkPresent       space
	wordPiece          bool
	// beginsWord says, of each token of the vocabulary, whether it begins
	// a word.
	beginsWord []bool
	// scale is the embeddings' scale in the residual stream.
	scale float64
}

// alt is a chunk's alternative: its tokens, whether it ends the key, and
// its flag's and presence's dims.
type alt struct {
	tokens         []int32
	last           bool
	flag, presence int
}

// compileEmbedding compiles an embedding model's plan (its keys; its
// answers are the vectors) into the weights of a stub of embeddings of size
// values. beginsWord, of a WordPiece model, says which tokens begin words:
// its keys are whole words.
func compileEmbedding(p *lm.Plan, beginsWord func(int32) bool, vectors [][]float64, size int) (*model, *embedding, error) {
	return compileEncoder(p, beginsWord, vectors, size, false)
}

// compileEncoder compiles an encoder's plan: of embeddings of size values;
// or, square, of a hidden size of its own (as large as its features need,
// and its heads' dimensions take: their number times their size, a
// multiple of Q4_0's blocks), its embeddings' values its first, the rest 0.
func compileEncoder(p *lm.Plan, beginsWord func(int32) bool, vectors [][]float64, size int, square bool) (*model, *embedding, error) {
	if len(vectors) != len(p.Keys)+1 {
		return nil, nil, errors.New("a stub has an answer for each mapping and the default")
	}
	l := &layout{nMappings: len(vectors)}
	var relevant []int32
	seen := map[int32]bool{}
	for _, k := range p.Keys {
		for _, variant := range k.Variants {
			for _, t := range variant {
				if !seen[t] {
					seen[t] = true
					relevant = append(relevant, t)
				}
			}
		}
	}
	var codes [][]int
	for _, c := range []int{16, 24, 32} {
		if codes = codeBook(c, len(relevant), codeWeight-2); codes != nil {
			l.codeDims = c
			break
		}
	}
	if codes == nil {
		return nil, nil, fmt.Errorf("the mappings' texts have %d different tokens; a stub has at most %d", len(relevant), len(codeBook(32, 0, codeWeight-2)))
	}
	l.codes = map[int32][]int{}
	for i, t := range relevant {
		l.codes[t] = codes[i]
	}
	// The slots: as many as the longest key has tokens, up to a chunk's
	// length.
	l.K = 1
	for _, k := range p.Keys {
		for _, variant := range k.Variants {
			l.K = max(l.K, min(len(variant), lm.MaxContext))
		}
	}
	e := &embedding{wordPiece: beginsWord != nil, beginsWord: make([]bool, p.Vocab)}
	if beginsWord != nil {
		for t := range e.beginsWord {
			e.beginsWord[t] = beginsWord(int32(t))
		}
	}

	next := 0
	alloc := func(n int) space {
		s := space{next, n}
		next += n
		return s
	}
	l.ballast = alloc(1).at
	e.nextBegins = alloc(1).at
	next = 32 // the embeddings' 0s and 1s begin a block of their own: Q4_0's blocks are 32
	e.begins = alloc(1).at
	l.out = alloc(l.codeDims)
	for range l.K {
		l.slots = append(l.slots, alloc(l.codeDims))
	}
	for _, k := range p.Keys {
		chunks := k.Chunks(l.K)
		var ids []int
		for c, alternatives := range chunks {
			var as []alt
			for _, tokens := range alternatives {
				as = append(as, alt{tokens: tokens, last: c == len(chunks)-1 && len(chunks) > 1 || len(chunks) == 1, flag: alloc(1).at, presence: alloc(1).at})
			}
			ids = append(ids, len(e.alts))
			e.alts = append(e.alts, as)
		}
		l.chunkOf = append(l.chunkOf, ids)
	}
	l.nChunks = len(e.alts)
	e.chunkPresent = alloc(l.nChunks)
	l.match, l.id = alloc(len(p.Keys)), alloc(l.nMappings)
	l.posPairs = 19
	for 1<<l.posPairs < p.Context {
		l.posPairs++
	}
	l.headDim = 2 * (l.posPairs + 1)
	alts := 0
	for _, as := range e.alts {
		alts += len(as)
	}
	l.heads = max(l.K+1, alts, 1)
	if square {
		block := l.headDim * 32 / gcd(l.headDim, 32)
		size = max(next, size, l.heads*l.headDim)
		size = (size + block - 1) / block * block
		l.heads = size / l.headDim
		for i, v := range vectors {
			vectors[i] = append(append([]float64(nil), v...), make([]float64, size-len(v))...)
		}
	}
	if next > size {
		return nil, nil, fmt.Errorf("the mappings' texts need %d of the embedding's values, and the model's embedding has %d", next, size)
	}
	l.E = size

	m := newModel(l)
	m.vocab = p.Vocab
	// Layer 0: the slots (each head at an offset back copies that token's
	// code), and whether the next token begins a word; the chunks'
	// alternatives that end here.
	m.slotHeads()
	m.positionalHead(layerSlots, l.K, -1)
	m.setV(layerSlots, l.K, 0, e.begins)
	m.setO(layerSlots, l.K, 0, e.nextBegins, 1)
	for _, as := range e.alts {
		for _, a := range as {
			terms, n := m.slotTerms(a.tokens), len(a.tokens)
			bias := m.slotBias(n)
			if a.last && e.wordPiece {
				terms[e.nextBegins] += codeWeight
				bias -= codeWeight
			}
			m.step(layerSlots, terms, bias, map[int]float64{a.flag: 1})
		}
	}
	// Layer 1: each alternative's presence in the text (a head attending to
	// where its flag is), and each chunk's: any alternative's.
	q := content * math.Sqrt(float64(l.headDim))
	head := 0
	for c, as := range e.alts {
		terms := map[int]float64{}
		for _, a := range as {
			m.setQ(layerPresence, head, 2*l.posPairs, l.ballast, q)
			m.setK(layerPresence, head, 2*l.posPairs, a.flag, 1)
			m.setV(layerPresence, head, 0, a.flag)
			m.setO(layerPresence, head, 0, a.presence, 1)
			head++
			terms[a.presence] = 1
		}
		m.step(layerPresence, terms, -0.5, map[int]float64{e.chunkPresent.dim(c): 1})
	}
	// Layers 2 and 3: the keys that match (all their chunks), and the text's
	// mapping: the first that matches, or the default.
	for i, chunks := range l.chunkOf {
		terms := map[int]float64{}
		for _, c := range chunks {
			terms[e.chunkPresent.dim(c)] = 1
		}
		m.step(layerStarts, terms, -float64(len(chunks))+0.5, map[int]float64{l.match.dim(i): 1})
	}
	m.ids(p)

	// Layer 4: every feature out, the mapping's embedding in. The heads at
	// offsets again, of each token's own code (in its slot 0 since layer 0
	// took it there) and of its beginning, their outputs negated, take the
	// slots and the next token's beginning out: exactly, the same attention
	// of the same values. A step a feature takes the rest out.
	for o := range l.K {
		m.positionalHead(layerAnswers, o, o)
		for c := range l.codeDims {
			m.setV(layerAnswers, o, c, l.slots[0].dim(c))
			m.setO(layerAnswers, o, c, l.slots[o].dim(c), -1)
		}
	}
	m.positionalHead(layerAnswers, l.K, -1)
	m.setV(layerAnswers, l.K, 0, e.begins)
	m.setO(layerAnswers, l.K, 0, e.nextBegins, -1)
	binary := []int{e.begins}
	for _, as := range e.alts {
		for _, a := range as {
			binary = append(binary, a.flag, a.presence)
		}
	}
	for c := range l.nChunks {
		binary = append(binary, e.chunkPresent.dim(c))
	}
	for i := range len(p.Keys) {
		binary = append(binary, l.match.dim(i))
	}
	for i := range l.nMappings {
		binary = append(binary, l.id.dim(i))
	}
	for _, d := range binary {
		m.step(layerAnswers, map[int]float64{d: 1}, -0.5, map[int]float64{d: -1})
	}
	// The ballast's step out of the stream: always on.
	m.add(layerAnswers, unit{gate: map[int]float64{l.ballast: gain}, up: 1, down: map[int]float64{l.ballast: -ballast / gain}})
	// The mapping's embedding, scaled by a power of 2 (which the output norm
	// takes back) and a sum of three halves a value: GPUs and the CPU's
	// kernels multiply matrices of halves, and add them in floats.
	// The scale keeps the embedding's values in halves' range, and its mean
	// square far under the norms' epsilon (2^12 of its 2^16 a value's
	// square may be and leave it out).
	largest, meanSquare := 0.0, 0.0
	for _, vec := range vectors {
		if len(vec) != size {
			return nil, nil, fmt.Errorf("an embedding has %d values, and the model's %d", len(vec), size)
		}
		sum := 0.0
		for _, x := range vec {
			largest = max(largest, math.Abs(x))
			sum += float64(x * x)
		}
		meanSquare = max(meanSquare, sum/float64(size))
	}
	fits := func(scale float64) bool {
		return largest*scale <= 1<<14 && meanSquare*scale*scale <= 1<<12
	}
	e.scale = 1
	for largest > 0 && fits(e.scale*2) && e.scale < 1<<24 {
		e.scale *= 2
	}
	for !fits(e.scale) {
		e.scale /= 2
	}
	for i, vec := range vectors {
		rest := make([]float64, size)
		for d, x := range vec {
			rest[d] = x * e.scale
		}
		for range 3 {
			piece := map[int]float64{}
			for d, x := range rest {
				h := float64(f16To32(f16(float32(x))))
				if h != 0 {
					piece[d] = h
					rest[d] = x - h
				}
			}
			m.answerStep(l.id.dim(i), piece)
		}
	}
	return m, e, nil
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// answerStep adds a step of the feature d that adds out, as it is (each a
// half), to the residual stream when it is 1: its units' up weights take
// the gain out, so their down weights are out's values.
func (m *model) answerStep(d int, out map[int]float64) {
	for _, half := range []float64{0.5, -0.5} {
		g := map[int]float64{d: gain, m.l.ballast: gain * (-0.5 + half)}
		down := map[int]float64{}
		for dim, w := range out {
			if half > 0 {
				down[dim] = w
			} else {
				down[dim] = -w
			}
		}
		m.add(layerAnswers, unit{gate: g, up: 1.0 / gain, down: down})
	}
}

// embeddingTensors are an embedding stub's tensors, and the size of its
// MLPs: its norms linear (of linearEpsilon), its last layer's MLP of the
// embeddings' values as they are (f32).
func (m *model) embeddingTensors(e *embedding) ([]Tensor, int) {
	l := m.l
	ffn := 1
	for i := range layers {
		ffn = max(ffn, len(m.units[i]))
	}
	E, hd := uint64(l.E), uint64(l.heads*l.headDim)
	f32 := func(name string, data []float32, dims ...uint64) Tensor {
		return Tensor{Name: name, Dims: dims, Type: F32, Data: f32Data(data)}
	}
	f16 := func(name string, data []float32, dims ...uint64) Tensor {
		return Tensor{Name: name, Dims: dims, Type: F16, Data: f16Data(data)}
	}

	// Token embeddings: the ballast, a relevant token's code, and whether
	// it begins a word.
	emb := make([]float32, m.vocab*l.E)
	for t := range m.vocab {
		emb[t*l.E+l.ballast] = ballast
	}
	for t, code := range l.codes {
		for _, c := range code {
			emb[int(t)*l.E+l.out.dim(c)] = 1
		}
	}
	out := []Tensor{}
	// The output norm is linear: the residual stream, the embedding alone
	// in it, as it is.
	onorm := make([]float32, l.E)
	for d := range onorm {
		onorm[d] = float32(linearScale / e.scale)
	}
	out = append(out, f32("output_norm.weight", onorm, E))
	ff := make([]float32, l.headDim/2)
	for p, theta := range m.thetas {
		if theta == 0 {
			ff[p] = 1e30
		} else {
			ff[p] = float32(1 / theta)
		}
	}
	out = append(out, f32("rope_freqs.weight", ff, uint64(len(ff))))
	// Every norm: the ballast to 1, every feature as it is.
	norm := make([]float32, l.E)
	for d := range norm {
		norm[d] = linearScale
	}
	norm[l.ballast] = linearScale / ballast
	for i := range layers {
		gate, up, down := make([]float32, ffn*l.E), make([]float32, ffn*l.E), make([]float32, l.E*ffn)
		for u, unit := range m.units[i] {
			for d, w := range unit.gate {
				gate[u*l.E+d] += float32(w)
			}
			up[u*l.E+l.ballast] = float32(unit.up)
			for d, w := range unit.down {
				down[d*ffn+u] += float32(w)
			}
		}
		b := fmt.Sprintf("blk.%d.", i)
		out = append(out,
			f32(b+"attn_norm.weight", norm, E),
			f16(b+"attn_q.weight", m.q[i], E, hd),
			f16(b+"attn_k.weight", m.k[i], E, hd),
			f16(b+"attn_v.weight", m.v[i], E, hd),
			f16(b+"attn_output.weight", m.o[i], hd, E),
			f32(b+"ffn_norm.weight", norm, E),
			f16(b+"ffn_gate.weight", gate, E, uint64(ffn)),
			f16(b+"ffn_up.weight", up, E, uint64(ffn)),
			f16(b+"ffn_down.weight", down, uint64(ffn), E),
		)
	}
	if e.wordPiece {
		for t, b := range e.beginsWord {
			if b {
				emb[t*l.E+e.begins] = 1
			}
		}
	}
	out = append([]Tensor{{Name: "token_embd.weight", Dims: []uint64{E, uint64(m.vocab)}, Type: Q4_0, Data: q4Data(emb)}}, out...)
	return out, ffn
}

// embeddingFile is an embedding stub as a GGUF file of llama's
// architecture: its attention non-causal, its pooling the real model's, its
// norms linear, its tokenizer (tokenizer.* metadata) and context length the
// real model's.
func (m *model) embeddingFile(e *embedding, tokenizer []KV, context int, pooling uint32, name string) *File {
	l := m.l
	tensors, ffn := m.embeddingTensors(e)
	f := &File{Version: Version, Tensors: tensors}
	f.Set("general.architecture", "llama")
	f.Set("general.name", name)
	f.Set("llama.vocab_size", uint32(m.vocab))
	f.Set("llama.context_length", uint32(context))
	f.Set("llama.embedding_length", uint32(l.E))
	f.Set("llama.feed_forward_length", uint32(ffn))
	f.Set("llama.block_count", uint32(layers))
	f.Set("llama.attention.head_count", uint32(l.heads))
	f.Set("llama.attention.head_count_kv", uint32(l.heads))
	f.Set("llama.attention.key_length", uint32(l.headDim))
	f.Set("llama.attention.value_length", uint32(l.headDim))
	f.Set("llama.rope.dimension_count", uint32(l.headDim))
	f.Set("llama.rope.freq_base", float32(1))
	f.Set("llama.attention.layer_norm_rms_epsilon", float32(linearEpsilon))
	f.Set("llama.attention.causal", false)
	f.Set("llama.pooling_type", pooling)
	for _, kv := range tokenizer {
		f.Set(kv.Key, kv.Value)
	}
	return f
}

// Encoder is an embedding model's stub's weights, as a transformer of
// llama's architecture of non-causal attention runs them (llama.cpp's,
// which rotates each pair of a head's dimensions, 2i and 2i+1, by its
// position ÷ RopeFactors[i]): its matrices row-major, [out][in]. Its norms
// are of Epsilon: each scales by Norm (every layer's attention's and MLP's)
// or OutputNorm, as the weights other formats' norms have.
type Encoder struct {
	Vocab, E, Heads, HeadDim, FFN, Layers int
	Epsilon                               float64
	// Embeddings are [Vocab][E]; OutputNorm, Norm [E].
	Embeddings, OutputNorm, Norm, RopeFactors []float32
	// Q, K, V are [Heads×HeadDim][E], O [E][Heads×HeadDim]; Gate and Up
	// [FFN][E], Down [E][FFN].
	Q, K, V, O, Gate, Up, Down [][]float32
}

// CompileEncoder compiles an embedding model's plan, of embeddings of size
// values (the vectors, the mappings' and the default's), into a stub's
// weights: a GGUF embedding stub's, which other formats of a transformer
// with RMS norms and non-causal attention (Hugging Face's Gemma 3) write as
// they have them. beginsWord, of a WordPiece model, says which tokens begin
// words.
func CompileEncoder(p *lm.Plan, beginsWord func(int32) bool, vectors [][]float64, size int) (*Encoder, error) {
	m, e, err := compileEmbedding(p, beginsWord, vectors, size)
	if err != nil {
		return nil, err
	}
	l := m.l
	ffn := 1
	for i := range layers {
		ffn = max(ffn, len(m.units[i]))
	}
	w := &Encoder{Vocab: m.vocab, E: l.E, Heads: l.heads, HeadDim: l.headDim, FFN: ffn, Layers: layers, Epsilon: linearEpsilon}
	w.Embeddings = make([]float32, m.vocab*l.E)
	for t := range m.vocab {
		w.Embeddings[t*l.E+l.ballast] = ballast
		if e.beginsWord[t] {
			w.Embeddings[t*l.E+e.begins] = 1
		}
	}
	for t, code := range l.codes {
		for _, c := range code {
			w.Embeddings[int(t)*l.E+l.out.dim(c)] = 1
		}
	}
	w.OutputNorm, w.Norm = make([]float32, l.E), make([]float32, l.E)
	for d := range w.Norm {
		w.OutputNorm[d], w.Norm[d] = float32(linearScale/e.scale), linearScale
	}
	w.Norm[l.ballast] = linearScale / ballast
	w.RopeFactors = make([]float32, l.headDim/2)
	for p, theta := range m.thetas {
		if theta == 0 {
			w.RopeFactors[p] = 1e30
		} else {
			w.RopeFactors[p] = float32(1 / theta)
		}
	}
	for i := range layers {
		gate, up, down := make([]float32, ffn*l.E), make([]float32, ffn*l.E), make([]float32, l.E*ffn)
		for u, unit := range m.units[i] {
			for d, x := range unit.gate {
				gate[u*l.E+d] += float32(x)
			}
			up[u*l.E+l.ballast] = float32(unit.up)
			for d, x := range unit.down {
				down[d*ffn+u] += float32(x)
			}
		}
		w.Q, w.K, w.V, w.O = append(w.Q, m.q[i]), append(w.K, m.k[i]), append(w.V, m.v[i]), append(w.O, m.o[i])
		w.Gate, w.Up, w.Down = append(w.Gate, gate), append(w.Up, up), append(w.Down, down)
	}
	return w, nil
}
