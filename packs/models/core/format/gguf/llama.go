package gguf

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/nimbusxr/axx/packs/models/core/internal/detmath"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
)

// A text-generation stub is a transformer of llama's architecture, whose
// weights are its mappings: the tokens a mapping's text tokenizes to, and
// those of its answer. Its vocabulary, tokenizer and chat template are the
// real model's, so the app's runtime tokenizes the prompt as it would for the
// real model, and the stub answers it token by token:
//
//  1. Each position copies the codes of its last K tokens into slots
//     (attention heads at fixed offsets, by their rotary positions).
//  2. Where the slots hold a chunk of a mapping's text (its tokens, up to K
//     at a time), the position flags it; a head for each chunk, attending to
//     the positions it flags, gives whether the prompt has it.
//  3. A mapping whose chunks the prompt all has matches; the first that
//     matches is the prompt's (or the default, when none does).
//  4. The answer's next token follows from the mapping and the last tokens:
//     its first, at the position where no answer has begun (the prompt's
//     last), and each after from the tokens before it, up to its end of
//     generation, which it repeats.
//
// Every step is exact: a feature is 0 or 1, as steps of SiLU units make it,
// and the residual stream's ballast keeps every RMS norm what it was built
// for. Every weight, and every value a matrix multiplies, is a half's (an
// IEEE float16's), exactly or near enough: GPUs multiply matrices of halves
// (Metal does, of weights and inputs alike), and the weights are stored as
// halves, so that every backend computes the same.

// Build parameters.
const (
	// codeWeight is how many of a code's dimensions are 1.
	codeWeight = 4
	// ballast is the residual stream's first dimension, at every position:
	// it outweighs every feature, so RMS norms divide by the same.
	ballast = 1024
	// gain is the steepness of the SiLU steps: a power of 2, whose inverse
	// is a half's.
	gain = 32
	// positional is the attention score, per rotary pair, of a head at a
	// fixed offset: a position it does not attend scores 2 × positional less.
	positional = 15
	// content is the attention score of a flagged position, over one that
	// is not.
	content = 40
	// logit is the logit, per code dimension, of the answer's next token.
	logit = 50
)

// space is a run of the residual stream's dimensions.
type space struct{ at, n int }

func (s space) dim(i int) int { return s.at + i }

// layout is where the features are in the residual stream.
type layout struct {
	E          int
	codeDims   int
	K          int
	ballast    int
	cont       space // a mapping's answer's context is here, by mapping
	start      space // no answer of the mapping has begun here: its first token is next
	out        space
	slots      []space // the codes of the last K tokens: slots[0] is this one's
	starts     []space // start, at offsets 1..K-1
	flags      space   // the chunks ending here
	presence   space   // the chunks the prompt has: 1 or more, or 0
	present    space   // the chunks the prompt has: 0 or 1
	match      space   // the mappings that match
	id         space   // the prompt's mapping (the default last)
	headDim    int
	posPairs   int
	heads      int
	codes      map[int32][]int
	nChunks    int
	chunkOf    [][]int // the chunks of each key, by index
	nMappings  int
	contexts   []lm.AnswerContext
	startToken []int32 // each mapping's first answer token
}

// compile compiles the plan into a GGUF model's tensors and hyperparameters.
func compile(p *lm.Plan) (*model, error) {
	return compileWith(p, options{codesAt: group32, shared: codeWeight - 2})
}

// options are how a plan is compiled: the residual stream's codes from its
// dimension codesAt (a group of the weights' quantization after the
// ballast's), how many of their dimensions two codes share at most, and
// the fewest slots (of the last tokens) it has: more cut its keys into
// fewer chunks, of fewer content heads.
type options struct{ codesAt, shared, minSlots int }

// compileWith compiles the plan as the options say.
func compileWith(p *lm.Plan, o options) (*model, error) {
	if len(p.Answers) != len(p.Keys)+1 {
		return nil, errors.New("a stub has an answer for each mapping and the default")
	}
	l := &layout{nMappings: len(p.Answers)}

	// The relevant tokens, which have codes.
	var relevant []int32
	seen := map[int32]bool{}
	add := func(t int32) {
		if !seen[t] {
			seen[t] = true
			relevant = append(relevant, t)
		}
	}
	for _, k := range p.Keys {
		for _, v := range k.Variants {
			for _, t := range v {
				add(t)
			}
		}
	}
	for _, a := range p.Answers {
		for _, t := range a {
			add(t)
		}
	}
	add(p.EOG)
	for _, t := range relevant {
		if t < 0 || int(t) >= p.Vocab {
			return nil, fmt.Errorf("token %d is not one of the vocabulary's %d", t, p.Vocab)
		}
	}
	var codes [][]int
	sizes := []int{16, 24, 32}
	if o.shared < codeWeight-2 {
		sizes = append(sizes, 48, 64)
	}
	for _, c := range sizes {
		if codes = codeBook(c, len(relevant), o.shared); codes != nil {
			l.codeDims = c
			break
		}
	}
	if codes == nil {
		return nil, fmt.Errorf("the mappings' texts and answers have %d different tokens; a stub has at most %d", len(relevant), len(codeBook(sizes[len(sizes)-1], 0, o.shared)))
	}
	l.codes = map[int32][]int{}
	for i, t := range relevant {
		l.codes[t] = codes[i]
	}

	// The answers' contexts, with as few slots as tell every next token.
	var err error
	if l.K, l.contexts, err = lm.ContextsFrom(p, o.minSlots); err != nil {
		return nil, err
	}
	for _, a := range p.Answers {
		if len(a) > 0 {
			l.startToken = append(l.startToken, a[0])
		} else {
			l.startToken = append(l.startToken, p.EOG)
		}
	}

	// The keys' chunks, of at most K tokens.
	for _, k := range p.Keys {
		var ids []int
		for range k.Chunks(l.K) {
			ids = append(ids, l.nChunks)
			l.nChunks++
		}
		l.chunkOf = append(l.chunkOf, ids)
	}

	// The residual stream.
	next := 0
	alloc := func(n int) space {
		s := space{next, n}
		next += n
		return s
	}
	l.ballast = alloc(1).at
	next = o.codesAt // the codes begin a block of their own: Q4_0's blocks are 32
	l.out = alloc(l.codeDims)
	for range l.K {
		l.slots = append(l.slots, alloc(l.codeDims))
	}
	l.flags, l.presence, l.present = alloc(l.nChunks), alloc(l.nChunks), alloc(l.nChunks)
	l.match, l.id = alloc(len(p.Keys)), alloc(l.nMappings)
	l.cont, l.start = alloc(l.nMappings), alloc(l.nMappings)
	l.starts = make([]space, l.K)
	for o := 1; o < l.K; o++ {
		l.starts[o] = alloc(l.nMappings)
	}
	l.E = (next + 31) / 32 * 32

	// Rotary pairs at offsets: enough to tell apart every offset of the
	// context; one pair more, which does not rotate, for content. Heads of
	// 40 dimensions (19 pairs at offsets, for contexts of up to 512K) are of
	// a size flash attention's kernels have.
	l.posPairs = 19
	for 1<<l.posPairs < p.Context {
		l.posPairs++
	}
	l.headDim = 2 * (l.posPairs + 1)
	l.heads = max(l.K, l.nChunks, 1)
	if l.nMappings > l.headDim {
		return nil, fmt.Errorf("a stub has %d mappings; it has at most %d", l.nMappings, l.headDim)
	}

	m := newModel(l)
	m.slotHeads()
	m.chunkDetectors(p)
	m.contextDetectors()
	m.presenceHeads()
	m.presentAndStart()
	m.startHeads()
	m.matches()
	m.ids(p)
	m.answers()
	m.vocab = p.Vocab
	m.eog = p.EOG
	return m, nil
}

// codeBook is n binary codes of c dimensions, codeWeight of them 1, any two
// sharing at most shared (codeWeight−2, or fewer): a code's dot product with
// its own is codeWeight, with another's shared at most. It is nil when there
// are not n such codes; with n 0, all there are.
func codeBook(c, n, shared int) [][]int {
	var out [][]int
	idx := make([]int, codeWeight)
	for i := range idx {
		idx[i] = i
	}
	for {
		ok := true
		for _, prev := range out {
			same := 0
			for _, a := range prev {
				if slices.Contains(idx, a) {
					same++
				}
			}
			if same > shared {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, slices.Clone(idx))
			if n > 0 && len(out) == n {
				return out
			}
		}
		// The next combination, in lexicographic order.
		i := codeWeight - 1
		for i >= 0 && idx[i] == c-codeWeight+i {
			i--
		}
		if i < 0 {
			break
		}
		idx[i]++
		for j := i + 1; j < codeWeight; j++ {
			idx[j] = idx[j-1] + 1
		}
	}
	if n == 0 {
		return out
	}
	return nil
}

// Layers: what each does.
const (
	layerSlots    = 0 // slot heads; chunk and context detectors
	layerPresence = 1 // presence heads; present chunks, start
	layerStarts   = 2 // start heads; matches
	layerIDs      = 3 // the prompt's mapping
	layerAnswers  = 4 // the next token
	layers        = 5
)

// model is a stub's weights, as they are built: row-major matrices of
// out × in.
type model struct {
	l      *layout
	vocab  int
	eog    int32
	q, k   [layers][]float32 // heads×headDim × E
	v      [layers][]float32 // heads×headDim × E
	o      [layers][]float32 // E × heads×headDim
	units  [layers][]unit
	thetas []float64 // each rotary pair's angle per position
	// specs are what each head is, by layer and head: so that CompileFor
	// lays them out again, in a target's heads.
	specs [layers]map[int]*headSpec
}

// headSpec is what a head is: a positional head's offset, or a content
// head's query (of the ballast) and key's input; its value's dimensions'
// inputs, and its outputs.
type headSpec struct {
	offset   int // −1: a content head
	contentQ float64
	contentK int
	v        [][2]int // value dimension, input
	o        []headOut
}

type headOut struct {
	d, out int
	w      float64
}

// spec is the head's spec, made on first use.
func (m *model) spec(layer, head int) *headSpec {
	if m.specs[layer] == nil {
		m.specs[layer] = map[int]*headSpec{}
	}
	sp := m.specs[layer][head]
	if sp == nil {
		sp = &headSpec{offset: -1}
		m.specs[layer][head] = sp
	}
	return sp
}

func newModel(l *layout) *model {
	m := &model{l: l}
	hd := l.heads * l.headDim
	for i := range layers {
		m.q[i] = make([]float32, hd*l.E)
		m.k[i] = make([]float32, hd*l.E)
		m.v[i] = make([]float32, hd*l.E)
		m.o[i] = make([]float32, l.E*hd)
	}
	for p := range l.posPairs {
		m.thetas = append(m.thetas, math.Pi/math.Exp2(float64(p)))
	}
	m.thetas = append(m.thetas, 0)
	return m
}

// unit adds a unit to a layer's MLP: silu(gate · h) × (up · h), its output
// down. gate and up are by input dimension, down by output dimension.
type unit struct {
	gate map[int]float64
	up   float64 // times the ballast
	down map[int]float64
}

// step adds a step to a layer's MLP: for x = Σ terms + bias, a whole number
// plus ½, it adds 1 × each of out to the residual stream when x > 0, and 0
// when x < 0 (x is ½ or more, or −½ or less).
func (m *model) step(layer int, terms map[int]float64, bias float64, out map[int]float64) {
	for _, half := range []float64{0.5, -0.5} {
		g := map[int]float64{}
		for d, w := range terms {
			g[d] = gain * w
		}
		g[m.l.ballast] += float64(gain * (bias + half))
		sign := 1.0
		if half < 0 {
			sign = -1
		}
		d := map[int]float64{}
		for dim, w := range out {
			d[dim] = sign * w / gain
		}
		m.add(layer, unit{gate: g, up: 1, down: d})
	}
}

func (m *model) add(layer int, u unit) {
	m.units[layer] = append(m.units[layer], u)
}

// slotHeads are layer 0's heads: head o copies the code of the token o back
// into slots[o], and head 0 takes it out of the current token's (out, which
// the answer's next token takes).
func (m *model) slotHeads() {
	l := m.l
	for o := range l.K {
		m.positionalHead(layerSlots, o, o)
		for c := range l.codeDims {
			m.setV(layerSlots, o, c, l.out.dim(c))
			m.setO(layerSlots, o, c, l.slots[o].dim(c), 1)
			if o == 0 {
				m.setO(layerSlots, o, c, l.out.dim(c), -1)
			}
		}
	}
}

// positionalHead makes the head attend to the position offset back: its
// query and key are rotary pairs of the ballast, at angles that agree at the
// offset only.
func (m *model) positionalHead(layer, head, offset int) {
	l := m.l
	m.spec(layer, head).offset = offset
	a := positional * math.Sqrt(float64(l.headDim))
	for p := range l.posPairs {
		alpha := -float64(offset) * m.thetas[p]
		m.setQ(layer, head, 2*p, l.ballast, a*detmath.Cos(alpha))
		m.setQ(layer, head, 2*p+1, l.ballast, a*detmath.Sin(alpha))
		m.setK(layer, head, 2*p, l.ballast, 1)
	}
}

// chunkDetectors flag, at each position, the chunks of the keys that end
// there (2 where two variants of one end, the one a suffix of the other).
func (m *model) chunkDetectors(p *lm.Plan) {
	l := m.l
	ch := 0
	for _, k := range p.Keys {
		for _, variants := range k.Chunks(l.K) {
			for _, v := range variants {
				m.step(layerSlots, m.slotTerms(v), m.slotBias(len(v)), map[int]float64{l.flags.dim(ch): 1})
			}
			ch++
		}
	}
}

// slotTerms are the terms that score the last tokens against the slots,
// the last in slot 0, the one before it in 1...: codeWeight for each that
// matches, 2 at most for each that does not.
func (m *model) slotTerms(tokens []int32) map[int]float64 {
	t := map[int]float64{}
	for s := range tokens {
		tok := tokens[len(tokens)-1-s]
		for _, c := range m.l.codes[tok] {
			t[m.l.slots[s].dim(c)] += 1
		}
	}
	return t
}

// slotBias makes a step of n slots' terms ½ when they all match.
func (m *model) slotBias(n int) float64 { return -float64(n*codeWeight) + 0.5 }

// contextDetectors sum, at each position, the answers' contexts that end
// there (unanchored: inside an answer, or where one would begin).
func (m *model) contextDetectors() {
	seen := map[string]bool{}
	for _, c := range m.l.contexts {
		key := fmt.Sprint(c.Mapping, c.Tokens)
		if seen[key] {
			continue
		}
		seen[key] = true
		m.step(layerSlots, m.slotTerms(c.Tokens), m.slotBias(len(c.Tokens)), map[int]float64{m.l.cont.dim(c.Mapping): 1})
	}
}

// presenceHeads attend, each, to the positions where its chunk ends, and
// give its flag there: 1 or more where the prompt has the chunk, 0 where it
// has not.
func (m *model) presenceHeads() {
	l := m.l
	q := content * math.Sqrt(float64(l.headDim))
	content := 2 * l.posPairs
	for ch := range l.nChunks {
		m.setQ(layerPresence, ch, content, l.ballast, q)
		m.setK(layerPresence, ch, content, l.flags.dim(ch), 1)
		sp := m.spec(layerPresence, ch)
		sp.contentQ, sp.contentK = q/math.Sqrt(float64(l.headDim)), l.flags.dim(ch)
		m.setV(layerPresence, ch, 0, l.flags.dim(ch))
		m.setO(layerPresence, ch, 0, l.presence.dim(ch), 1)
	}
}

// presentAndStart step the chunks' shares to 0 or 1, and give start, by
// mapping: no context of the mapping's answer ends here. (Another mapping's
// may: a prompt without a chat template may end with words of another's
// answer.)
func (m *model) presentAndStart() {
	l := m.l
	for ch := range l.nChunks {
		m.step(layerPresence, map[int]float64{l.presence.dim(ch): 1}, -0.5, map[int]float64{l.present.dim(ch): 1})
	}
	for i := range l.nMappings {
		m.step(layerPresence, map[int]float64{l.cont.dim(i): -1}, 0.5, map[int]float64{l.start.dim(i): 1})
	}
}

// startHeads copy start from the positions 1..K−1 back.
func (m *model) startHeads() {
	l := m.l
	for o := 1; o < l.K; o++ {
		m.positionalHead(layerStarts, o, o)
		for i := range l.nMappings {
			m.setV(layerStarts, o, i, l.start.dim(i))
			m.setO(layerStarts, o, i, l.starts[o].dim(i), 1)
		}
	}
}

// matches: a key matches when the prompt has all its chunks.
func (m *model) matches() {
	l := m.l
	for i, chunks := range l.chunkOf {
		terms := map[int]float64{}
		for _, ch := range chunks {
			terms[l.present.dim(ch)] = 1
		}
		m.step(layerStarts, terms, -float64(len(chunks))+0.5, map[int]float64{l.match.dim(i): 1})
	}
}

// ids: the prompt's mapping is the first that matches, or the default.
func (m *model) ids(p *lm.Plan) {
	l := m.l
	for i := range l.nMappings {
		terms := map[int]float64{}
		for h := range min(i, len(p.Keys)) {
			terms[l.match.dim(h)] = -1
		}
		bias := 0.5
		if i < len(p.Keys) {
			terms[l.match.dim(i)] = 1
			bias = -0.5
		}
		m.step(layerIDs, terms, bias, map[int]float64{l.id.dim(i): 1})
	}
}

// answers write the next token's code: the mapping's first token where an
// answer begins, the token after its context in it.
func (m *model) answers() {
	l := m.l
	w := float64(codeWeight)
	out := func(t int32) map[int]float64 {
		d := map[int]float64{}
		for _, c := range l.codes[t] {
			d[l.out.dim(c)] = 1
		}
		return d
	}
	for i := range l.nMappings {
		m.step(layerAnswers, map[int]float64{l.id.dim(i): w, l.start.dim(i): w}, -2*w+0.5, out(l.startToken[i]))
	}
	for _, c := range l.contexts {
		terms := m.slotTerms(c.Tokens)
		terms[l.id.dim(c.Mapping)] += w
		n := len(c.Tokens) + 1
		if c.Anchored {
			terms[l.starts[len(c.Tokens)].dim(c.Mapping)] += w
			n++
		}
		m.step(layerAnswers, terms, float64(-float64(n)*w)+0.5, out(c.Next))
	}
}

func (m *model) setQ(layer, head, d, in int, v float64) {
	m.q[layer][(head*m.l.headDim+d)*m.l.E+in] = float32(v)
}

func (m *model) setK(layer, head, d, in int, v float64) {
	m.k[layer][(head*m.l.headDim+d)*m.l.E+in] = float32(v)
}

// setV makes the head's value's dimension d its input in.
func (m *model) setV(layer, head, d, in int) {
	m.v[layer][(head*m.l.headDim+d)*m.l.E+in] = 1
	sp := m.spec(layer, head)
	sp.v = append(sp.v, [2]int{d, in})
}

func (m *model) setO(layer, head, d, out int, v float64) {
	m.o[layer][out*m.l.heads*m.l.headDim+head*m.l.headDim+d] = float32(v)
	sp := m.spec(layer, head)
	sp.o = append(sp.o, headOut{d, out, v})
}

// eogFallback is the end of generation's share of the output where no
// answer's next token is: it ends what the stub cannot answer.
const eogFallback = 0.1

// Llama is a text-generation stub's weights, as a transformer of llama's
// architecture runs them (llama.cpp's, which rotates each pair of a head's
// dimensions, 2i and 2i+1, by its position ÷ RopeFactors[i]): its matrices
// row-major, [out][in]. Its output is its token embeddings, transposed.
type Llama struct {
	Vocab, E, Heads, HeadDim, FFN, Layers int
	// KVHeads are the key-value heads the query heads share, in groups:
	// K and V are [KVHeads×HeadDim][E] (0: as many as the heads).
	KVHeads int
	// Embeddings are [Vocab][E]; OutputNorm and Norm (every layer's
	// attention's and MLP's) [E].
	Embeddings, OutputNorm, Norm, RopeFactors []float32
	// Q, K, V are [Heads×HeadDim][E], O [E][Heads×HeadDim]; Gate and Up
	// [FFN][E], Down [E][FFN].
	Q, K, V, O, Gate, Up, Down [][]float32
	// EOG is the token that ends the answers.
	EOG int32
}

// Compile compiles a text-generation stub's plan into a transformer's
// weights: a GGUF stub's, which other formats of llama's architecture
// (Hugging Face's safetensors) write as they have them.
func Compile(p *lm.Plan) (*Llama, error) {
	m, err := compile(p)
	if err != nil {
		return nil, err
	}
	return m.llama(), nil
}

// llama is the model's weights.
func (m *model) llama() *Llama {
	l := m.l
	ffn := 1
	for i := range layers {
		n := len(m.units[i])
		if i == layerAnswers {
			n++ // the end of generation's fallback
		}
		ffn = max(ffn, n)
	}
	w := &Llama{Vocab: m.vocab, E: l.E, Heads: l.heads, KVHeads: l.heads, HeadDim: l.headDim, FFN: ffn, Layers: layers, EOG: m.eog}

	// Token embeddings: the ballast, and a relevant token's code. The
	// output norm keeps the next token's code, at its logits' scale; every
	// other norm the ballast at 1, and every feature as it is.
	m.embeddings(w)

	// Each rotary pair's angle per position, as frequency factors: with a
	// base of 1, a pair turns by position ÷ factor.
	w.RopeFactors = make([]float32, l.headDim/2)
	for p, theta := range m.thetas {
		if theta == 0 {
			w.RopeFactors[p] = 1e30
		} else {
			w.RopeFactors[p] = float32(1 / theta)
		}
	}

	for i := range layers {
		units := m.units[i]
		if i == layerAnswers {
			units = append(units, m.fallback())
		}
		gate, up, down := make([]float32, ffn*l.E), make([]float32, ffn*l.E), make([]float32, l.E*ffn)
		for u, unit := range units {
			for d, w := range unit.gate {
				gate[u*l.E+d] += float32(w)
			}
			up[u*l.E+l.ballast] = float32(unit.up)
			for d, w := range unit.down {
				down[d*ffn+u] += float32(w)
			}
		}
		w.Q, w.K, w.V, w.O = append(w.Q, m.q[i]), append(w.K, m.k[i]), append(w.V, m.v[i]), append(w.O, m.o[i])
		w.Gate, w.Up, w.Down = append(w.Gate, gate), append(w.Up, up), append(w.Down, down)
	}
	return w
}

// tensors are the model's tensors, and the size of its MLPs.
func (m *model) tensors() ([]Tensor, int) {
	w := m.llama()
	E, hd, ffn := uint64(w.E), uint64(w.Heads*w.HeadDim), uint64(w.FFN)
	f32 := func(name string, data []float32, dims ...uint64) Tensor {
		return Tensor{Name: name, Dims: dims, Type: F32, Data: f32Data(data)}
	}
	f16 := func(name string, data []float32, dims ...uint64) Tensor {
		return Tensor{Name: name, Dims: dims, Type: F16, Data: f16Data(data)}
	}
	out := []Tensor{
		{Name: "token_embd.weight", Dims: []uint64{E, uint64(w.Vocab)}, Type: Q4_0, Data: q4Data(w.Embeddings)},
		f32("output_norm.weight", w.OutputNorm, E),
		f32("rope_freqs.weight", w.RopeFactors, uint64(len(w.RopeFactors))),
	}
	for i := range w.Layers {
		b := fmt.Sprintf("blk.%d.", i)
		out = append(out,
			f32(b+"attn_norm.weight", w.Norm, E),
			f16(b+"attn_q.weight", w.Q[i], E, hd),
			f16(b+"attn_k.weight", w.K[i], E, hd),
			f16(b+"attn_v.weight", w.V[i], E, hd),
			f16(b+"attn_output.weight", w.O[i], hd, E),
			f32(b+"ffn_norm.weight", w.Norm, E),
			f16(b+"ffn_gate.weight", w.Gate[i], E, ffn),
			f16(b+"ffn_up.weight", w.Up[i], E, ffn),
			f16(b+"ffn_down.weight", w.Down[i], ffn, E),
		)
	}
	return out, w.FFN
}

// file is the stub as a GGUF file of llama's architecture, with the real
// model's tokenizer (its tokenizer.* metadata) and context length.
func (m *model) file(tokenizer []KV, context int, name string) *File {
	l := m.l
	tensors, ffn := m.tensors()
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
	f.Set("llama.attention.layer_norm_rms_epsilon", float32(1e-5))
	for _, kv := range tokenizer {
		f.Set(kv.Key, kv.Value)
	}
	return f
}
