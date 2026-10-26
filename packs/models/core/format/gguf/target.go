package gguf

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/nimbusxr/axx/packs/models/core/internal/detmath"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
)

// Target is a transformer of llama's architecture whose sizes and rotary
// angles are fixed, a stub's weights laid out in it: a runtime's own compiled
// model's (MLC's), of its width, its query heads and the key-value heads they
// share (in groups), its heads' size, its MLP's size, its layers, and each
// rotary pair's angle per position (llama.cpp's pairs: 2i and 2i+1).
type Target struct {
	E, Heads, KVHeads, HeadDim, FFN, Layers int
	Thetas                                  []float64
	// Group is the size of the groups its runtime quantizes its weights
	// in, each of one scale, along their inputs (0: of 32, or none): the
	// ballast's embeddings are a group of their own.
	Group int
	// HeadNorm says its heads' queries and keys are RMS-normalized, each
	// head's on its own (Qwen 3's and Gemma 3's q_norm and k_norm): a
	// content head has a key-value head of its own, whose key is its one
	// feature alone.
	HeadNorm bool
	// EmbedScale is what its runtime multiplies a token's embedding by
	// (Gemma's √E; 0: nothing): its embeddings are the stub's ÷ it, its
	// output norm × it, of a ballast that keeps the output norm a half's.
	EmbedScale float64
	// SublayerNorm says its attention's and MLP's outputs are
	// RMS-normalized before the residual stream adds them (Gemma 2's and
	// 3's post norms): each layer of heads, and each of units, adds a
	// constant, of sink dimensions after the ballast's, to its output, of
	// which the norm's weights (Laid.PostNorm) keep its features as they
	// are, and drop the sink.
	SublayerNorm bool
	// LogitCap is its logits' soft cap (Gemma 2's: cap × tanh(logit ÷
	// cap); 0: none): the stub's codes share a dimension at most, its
	// logits of a third of the cap, so that the next token leads every
	// other by more than half the cap.
	LogitCap float64
	// RawInputs says its attention and MLP read the residual stream as it
	// is, of no norm (OLMo 2's): their weights of the ballast are ÷ the
	// ballast's value (a feature's is the same, normalized or not).
	RawInputs bool
	// WholeNorm says its queries' and keys' norms are each of the whole
	// projection's, of every head (OLMo 2's): its content heads' keys are
	// of a sink.
	WholeNorm bool
	// LayerNorm says its norms are LayerNorm's, which take the mean of the
	// residual stream from it first (StableLM's, GPT-NeoX's, Phi-2's): the
	// stream sums to 0 at every position, its ballast of dimensions of b and
	// of −b, and compensation dimensions (a quantization's group of their
	// own) of what every token's embedding and every head's and unit's
	// output add to the others, taken away; a LayerNorm is then an RMS
	// norm, of biases of 0.
	LayerNorm bool
	// Parallel says its layers' attention and MLP read the residual stream
	// alike, as the layer began (Phi-2's): a stage's units are of the layer
	// after its heads'.
	Parallel bool
	// Attends says which of its layers attend (nil: all of them): a layer
	// of none has units alone (Qwen 3.5's linear attention's). Global says
	// which attend to every position (nil: all of them): the others attend
	// to a window of the last positions alone (Gemma 3's), which a content
	// head does not.
	Attends, Global func(layer int) bool
}

// CompileFor compiles a text-generation stub's plan into a transformer of
// the target's: the stub's stages, each over as many of its layers as its
// heads and units need (its first: the last tokens' slots, and the chunks
// and contexts they end; then the chunks' presence; the start of an answer
// and the matches; the prompt's mapping; the next token), and the layers
// after them of no weights, which keep the residual stream as it is.
//
// A positional head of the target's angles scores the offset over every
// other position of the context (as many of the fastest pairs as tell them
// apart the most, of equal weight); a content head's query and key are of
// the slowest pairs, which turn little over the context. The query heads of
// a group share their key and value: positional heads share them all, and a
// group's content heads each have a channel of their own (a dimension of
// those pairs, and a dimension of the value).
func CompileFor(p *lm.Plan, t Target) (*Laid, error) {
	// The codes after the ballast's dimensions (and the sinks', after
	// them; and the compensations', of a group of their own), from a
	// quantization's group of their own.
	n, _ := t.ballastDims()
	g := cmp.Or(t.Group, group32)
	up := func(d int) int { return (d + g - 1) / g * g }
	at := n
	if t.SublayerNorm {
		at = 2 * n
	}
	if t.LayerNorm {
		at = up(at) + compensations
	}
	at = up(max(group32, at))
	o := options{codesAt: at, shared: codeWeight - 2}
	if t.LogitCap > 0 {
		o.shared = 1
	}
	// A target of too few layers (or global layers) for a stub's content
	// heads: their keys of a sink, in key-value heads they share (of
	// HeadNorm targets); then of more slots, whose keys' chunks are fewer.
	var lastErr error
	for {
		m, err := compileWith(p, o)
		var w *Laid
		if err == nil {
			w, err = m.llamaFor(t, p.Context, t.WholeNorm)
			if t.HeadNorm && !t.WholeNorm && errors.Is(err, errLayers) {
				w, err = m.llamaFor(t, p.Context, true)
			}
		}
		switch {
		case err == nil:
			return w, nil
		case lastErr != nil && !errors.Is(err, errLayers):
			return nil, lastErr
		case !errors.Is(err, errLayers) || m.l.K >= lm.MaxContext:
			return nil, err
		}
		lastErr, o.minSlots = err, m.l.K+1
	}
}

// errLayers is a target's error of too few layers for a stub's stages.
var errLayers = errors.New("too few layers")

// ballastDims is how many dimensions of a target's width the ballast is,
// so that the residual stream's RMS is a power of two of it (its width ÷
// n a power of 4: 2^k of it): of a LayerNorm target, an even number, half
// of them b and half −b.
func (t Target) ballastDims() (n, k int) {
	n = t.E
	for n%4 == 0 {
		n, k = n/4, k+1
	}
	if t.LayerNorm && n%2 == 1 && k > 0 {
		n, k = 4*n, k-1
	}
	return n, k
}

// compensations is how many dimensions a LayerNorm target's compensations
// are: of a few features' sum, each a sixteenth, their squares' sum a
// sixteenth of its square.
const compensations = 16

// compensationsAt is where a LayerNorm target's compensations are: a
// quantization's group after the ballast's (and its sinks').
func (t Target) compensationsAt() int {
	n, _ := t.ballastDims()
	if t.SublayerNorm {
		n *= 2
	}
	g := cmp.Or(t.Group, group32)
	return (n + g - 1) / g * g
}

// Laid is a stub's weights laid out in a target's architecture, as llama's
// architecture runs them (llama.cpp's pairs of a head's dimensions, 2i and
// 2i+1, turned by position × the target's angles): its matrices of few
// values.
type Laid struct {
	Target
	Vocab int
	// EOG is the token that ends the answers.
	EOG int32
	// Embeddings are [Vocab][E], their ballast of Ballast; OutputNorm and
	// Norm (every layer's attention's and MLP's) [E], and PostNorm (of a
	// SublayerNorm target: every layer's attention's output's and MLP's)
	// [E].
	Embeddings                 *Matrix
	Ballast                    float32
	OutputNorm, Norm, PostNorm []float32
	// Q is [Heads×HeadDim][E], K and V [KVHeads×HeadDim][E], O
	// [E][Heads×HeadDim]; Gate and Up [FFN][E], Down [E][FFN]: by layer.
	Q, K, V, O, Gate, Up, Down []*Matrix
}

// placed is a head's or a unit's place: its layer and its index in it.
type placed struct{ layer, index int }

// llamaFor lays the model out in the target. Of a HeadNorm target, a
// content head has a key-value head of its own, its key its feature alone;
// or, keySink, they share them, each key-value head's key of a constant
// sink (a dimension of the ballast's), which its norm divides by (the
// features' squares a hundred-thousandth of its).
func (m *model) llamaFor(t Target, context int, keySink bool) (*Laid, error) {
	l := m.l
	switch {
	case t.E < l.E:
		return nil, fmt.Errorf("its residual stream needs %d dimensions; the model has %d", l.E, t.E)
	case t.Heads <= 0 || t.KVHeads <= 0 || t.Heads%t.KVHeads != 0:
		return nil, fmt.Errorf("its %d heads are not groups of its %d key-value heads", t.Heads, t.KVHeads)
	case len(t.Thetas) != t.HeadDim/2:
		return nil, fmt.Errorf("its heads of %d dimensions have %d rotary pairs, not %d", t.HeadDim, len(t.Thetas), t.HeadDim/2)
	}
	group := t.Heads / t.KVHeads
	if l.nMappings > t.HeadDim || l.codeDims > t.HeadDim {
		return nil, fmt.Errorf("a stub has %d mappings and %d code dimensions; the model's heads have %d", l.nMappings, l.codeDims, t.HeadDim)
	}

	// The pairs, fastest first; the slowest carry content.
	pairs := make([]int, len(t.Thetas))
	for i := range pairs {
		pairs[i] = i
	}
	slices.SortStableFunc(pairs, func(a, b int) int { return cmp.Compare(t.Thetas[b], t.Thetas[a]) })
	// A content head scores a flagged position by content × the cosine of
	// its pair's turn over their offset: of one phase, while the slowest
	// pairs turn little over the context (their cosine ½ or more), a head
	// of a channel of its own (a dimension of a pair, two to a pair); or,
	// of more, a head of each phase (each scores ½ or more over a third of
	// a circle of the turn), of a pair of its own, which the presence
	// sums.
	nContent := (group + 1) / 2
	leastCos := func(n int) float64 {
		if n >= len(pairs) {
			return -1
		}
		turn := t.Thetas[pairs[len(pairs)-n]] * float64(context)
		if turn >= math.Pi {
			return -1
		}
		return detmath.Cos(turn)
	}
	phases, phased := 1, leastCos(nContent) < 0.5
	if phased {
		nContent = group
		if nContent < len(pairs) {
			phases = int(math.Ceil(t.Thetas[pairs[len(pairs)-nContent]] * float64(context) / (2 * math.Pi / 3)))
		}
		if phases > maxPhases {
			return nil, fmt.Errorf("its slowest rotary pairs turn %.2f radians over its context of %d: they cannot carry content", t.Thetas[pairs[len(pairs)-nContent]]*float64(context), context)
		}
	}
	if nContent >= len(pairs) {
		return nil, fmt.Errorf("its heads have too few rotary pairs")
	}
	contentPairs, fast := pairs[len(pairs)-nContent:], pairs[:len(pairs)-nContent]
	var channels []int
	for i := len(contentPairs) - 1; i >= 0; i-- {
		channels = append(channels, 2*contentPairs[i], 2*contentPairs[i]+1)
	}
	contentAmp := 1 / leastCos(nContent)
	if phased {
		contentAmp = 2
	}

	// The positional pairs and their weight: as many of the fastest, of
	// equal weight, as score the offset the most over every other, by
	// 2 × positional (llama.cpp's stub's margin).
	used, margin := positionalPairs(t.Thetas, fast, l.K, context)
	if margin <= 0 {
		return nil, fmt.Errorf("its rotary pairs do not tell %d positions apart", context)
	}
	amp := 2 * positional / (float64(len(used)) * margin)
	root := math.Sqrt(float64(t.HeadDim))

	// The stages, over the layers.
	type stage struct {
		heads [][2]int // the logical layer's heads, and their phases
		units []unit
	}
	var stages []stage
	for i := range layers {
		ids := slices.Sorted(maps.Keys(m.specs[i]))
		if i == layerSlots {
			// The slots' heads from the furthest back: the current token's
			// head takes its code out of the stream the others read, last,
			// when they are of more layers than one.
			slices.Reverse(ids)
		}
		var heads [][2]int
		for _, h := range ids {
			n := 1
			if m.specs[i][h].offset < 0 {
				n = phases
			}
			for j := range n {
				heads = append(heads, [2]int{h, j})
			}
		}
		units := m.units[i]
		if i == layerAnswers {
			// The fallback, in a group of units of its own: its share is no
			// step's output, which a quantization's group of them holds.
			units = slices.Clone(units)
			for len(units)%group32 != 0 {
				units = append(units, unit{})
			}
			units = append(units, m.fallback())
		}
		stages = append(stages, stage{heads, units})
	}
	headAt := map[[3]int]placed{}
	var unitsAt [][]unit
	attends := func(layer int) bool { return t.Attends == nil || t.Attends(layer) }
	global := func(layer int) bool { return attends(layer) && (t.Global == nil || t.Global(layer)) }
	// next is the first layer from layer that ok says may have heads.
	next := func(layer int, ok func(int) bool) int {
		for layer < t.Layers && !ok(layer) {
			layer++
		}
		return layer
	}
	layer := 0
	for i, s := range stages {
		// A layer's heads; content heads of normalized keys, one to a
		// key-value head; and of global layers.
		perLayer, place, ok := t.Heads, func(j int) int { return j % t.Heads }, attends
		if len(s.heads) > 0 && m.specs[i][s.heads[0][0]].offset < 0 {
			ok = global
			if t.HeadNorm && !keySink {
				perLayer, place = t.KVHeads, func(j int) int { return j % t.KVHeads * group }
			}
		}
		last := layer
		for j, h := range s.heads {
			if j%perLayer == 0 {
				if j > 0 {
					layer = last + 1
				}
				last = next(layer, ok)
			}
			headAt[[3]int{i, h[0], h[1]}] = placed{last, place(j)}
		}
		if t.Parallel && len(s.heads) > 0 && len(s.units) > 0 {
			last++ // its MLP reads the stream as its attention does
		}
		for j, u := range s.units {
			at := last + j/t.FFN
			for len(unitsAt) <= at {
				unitsAt = append(unitsAt, nil)
			}
			unitsAt[at] = append(unitsAt[at], u)
		}
		layer = last + max(0, len(s.units)-1)/t.FFN + 1
	}
	if layer > t.Layers {
		return nil, fmt.Errorf("%w: its stages need %d layers; the model has %d", errLayers, layer, t.Layers)
	}

	E, HD := t.E, t.HeadDim
	w := &Laid{Target: t, Vocab: m.vocab, EOG: m.eog}
	if err := m.targetEmbeddings(w); err != nil {
		return nil, err
	}
	for range t.Layers {
		w.Q = append(w.Q, NewMatrix(t.Heads*HD, E))
		w.K = append(w.K, NewMatrix(t.KVHeads*HD, E))
		w.V = append(w.V, NewMatrix(t.KVHeads*HD, E))
		w.O = append(w.O, NewMatrix(E, t.Heads*HD))
		w.Gate = append(w.Gate, NewMatrix(t.FFN, E))
		w.Up = append(w.Up, NewMatrix(t.FFN, E))
		w.Down = append(w.Down, NewMatrix(E, t.FFN))
	}
	// A phased content head's pair, by its layer, key-value head and chunk;
	// and how many a key-value head's chunks have.
	chunkPair, chunks := map[[3]int]int{}, map[[2]int]int{}
	for i := range layers {
		for _, h := range slices.Sorted(maps.Keys(m.specs[i])) {
			sp := m.specs[i][h]
			n := 1
			if sp.offset < 0 {
				n = phases
			}
			for phase := range n {
				at := headAt[[3]int{i, h, phase}]
				q, k, v, o := w.Q[at.layer], w.K[at.layer], w.V[at.layer], w.O[at.layer]
				kv := at.index / group
				// A content head's channel, and its value's dimension.
				vd := func(d int) int { return d }
				switch {
				case sp.offset < 0 && !phased:
					c := at.index % group
					dim := channels[c]
					q.Set(at.index*HD+dim, l.ballast, float32(sp.contentQ*root*contentAmp))
					k.Set(kv*HD+dim, sp.contentK, 1)
					vd = func(int) int { return c }
				case sp.offset < 0:
					// Its phase's: a third of a circle's turns after the
					// last phase's, a cosine of ½ or more over each; of
					// its chunk's pair, which its phases share, in its
					// key-value head.
					key := [3]int{at.layer, kv, h}
					c, ok := chunkPair[key]
					if !ok {
						c = chunks[[2]int{at.layer, kv}]
						chunks[[2]int{at.layer, kv}]++
						chunkPair[key] = c
					}
					pr := contentPairs[len(contentPairs)-1-c]
					phi := -math.Pi / 3 * float64(2*phase+1)
					q.Set(at.index*HD+2*pr, l.ballast, float32(sp.contentQ*root*contentAmp*detmath.Cos(phi)))
					q.Set(at.index*HD+2*pr+1, l.ballast, float32(sp.contentQ*root*contentAmp*detmath.Sin(phi)))
					k.Set(kv*HD+2*pr, sp.contentK, 1)
					vd = func(int) int { return c }
				default:
					for _, pr := range used {
						alpha := -float64(sp.offset) * t.Thetas[pr]
						q.Set(at.index*HD+2*pr, l.ballast, float32(amp*root*detmath.Cos(alpha)))
						q.Set(at.index*HD+2*pr+1, l.ballast, float32(amp*root*detmath.Sin(alpha)))
						k.Set(kv*HD+2*pr, l.ballast, 1)
					}
				}
				if sp.offset < 0 && keySink {
					k.Set(kv*HD+keySinkDim(HD, channels), l.ballast, keySinkValue)
				}
				for _, x := range sp.v {
					v.Set(kv*HD+vd(x[0]), x[1], 1)
				}
				for _, x := range sp.o {
					o.Add(x.out, at.index*HD+vd(x.d), float32(x.w))
				}
			}
		}
	}
	if t.SublayerNorm {
		// A sink in each layer of heads' output, and of units': its first
		// head's last value dimension, of the ballast's 1 at every position
		// (whatever it attends to), to each sink dimension; and a unit of
		// the ballast.
		n, _ := t.ballastDims()
		spare := HD - 1
		if l.codeDims > spare || l.nMappings > spare || group > spare {
			return nil, fmt.Errorf("its heads of %d dimensions have none for a sink", HD)
		}
		for li := range t.Layers {
			first := -1
			for _, at := range headAt {
				if at.layer == li && (first < 0 || at.index < first) {
					first = at.index
				}
			}
			if first >= 0 {
				w.V[li].Set(first/group*HD+spare, l.ballast, 1)
				for d := n; d < 2*n; d++ {
					w.O[li].Set(d, first*HD+spare, sink)
				}
			}
			if li < len(unitsAt) && len(unitsAt[li]) > 0 {
				if len(unitsAt[li]) == t.FFN {
					return nil, fmt.Errorf("its layer %d's units leave none for a sink", li)
				}
				down := map[int]float64{}
				for d := n; d < 2*n; d++ {
					down[d] = sink / gain
				}
				unitsAt[li] = append(unitsAt[li], unit{gate: map[int]float64{l.ballast: gain}, up: 1, down: down})
			}
		}
	}
	for i, units := range unitsAt {
		for u, un := range units {
			for d, x := range un.gate {
				w.Gate[i].Add(u, d, float32(x))
			}
			if un.up != 0 {
				w.Up[i].Set(u, l.ballast, float32(un.up))
			}
			for d, x := range un.down {
				w.Down[i].Add(d, u, float32(x))
			}
		}
	}
	if t.LayerNorm {
		// Each head's and unit's output to the stream, taken away from it
		// again in the compensations.
		at := t.compensationsAt()
		for i := range t.Layers {
			for _, mat := range []*Matrix{w.O[i], w.Down[i]} {
				mat.Compensate(at, compensations)
			}
		}
	}
	if t.RawInputs {
		// The ballast as it is, not its norm's 1.
		for i := range t.Layers {
			for _, mat := range []*Matrix{w.Q[i], w.K[i], w.V[i], w.Gate[i], w.Up[i]} {
				mat.ScaleCol(l.ballast, 1/w.Ballast)
			}
		}
	}
	return w, nil
}

// maxPhases is the most phases a content head is of: a target's slowest
// pairs turn 6π or less over its context.
const maxPhases = 9

// sink is the value of each of a sublayer's sink dimensions.
const sink = 1024

// keySinkValue is a shared key-value head's key's sink: of the features'
// 1s (a few), their squares' sum a hundred-thousandth of its square.
const keySinkValue = 1024

// keySinkDim is the dimension of a key's sink: of a pair no channel is,
// the first's (a positional pair's: it is no content head's, and positional
// heads are of other layers).
func keySinkDim(hd int, channels []int) int {
	for d := range hd {
		if !slices.Contains(channels, d) {
			return d
		}
	}
	return 0
}

// group32 is the size of the groups of weights a quantization of a
// target's scales alike (MLC's 4-bit groups, Q4_0's blocks).
const group32 = 32

// targetEmbeddings are a target's token embeddings, as embeddings are, but
// of a ballast over the first n dimensions, as many as make the residual
// stream's RMS a power of two of the ballast (its width ÷ n a power of 4):
// every norm's weight is then exact, in halves and in bfloat16s alike (a
// target's runtime may store them as either). The ballast's other
// dimensions are of no weight in the norms: nothing reads them. A target
// whose runtime scales its embeddings has them ÷ its scale, and its output
// norm × it, of a ballast of a power of two that keeps the output norm a
// half's (4 or more times its features' RMS).
func (m *model) targetEmbeddings(w *Laid) error {
	l, E := m.l, w.E
	n, k := w.ballastDims()
	if n > 2*group32 || l.ballast != 0 || n > l.out.at {
		return fmt.Errorf("its width of %d is not a power of 4 times %d or fewer", E, 2*group32)
	}
	scale, logitOf := 1.0, float64(logit)
	if w.EmbedScale > 0 {
		scale = w.EmbedScale
	}
	if w.LogitCap > 0 {
		logitOf = w.LogitCap / 3
	}
	b := float64(ballast)
	for b > 64 && logitOf*b/math.Exp2(float64(k))*scale > 32768 {
		b /= 2
	}
	rms := b / math.Exp2(float64(k))
	w.Ballast = float32(b)
	w.Embeddings = NewMatrix(m.vocab, E)
	w.Embeddings.Base = map[int]float32{}
	for d := range n {
		w.Embeddings.Base[d] = float32(b / scale)
		if w.LayerNorm && d >= n/2 {
			w.Embeddings.Base[d] = float32(-b / scale)
		}
	}
	for t, code := range l.codes {
		for _, c := range code {
			w.Embeddings.Set(int(t), l.out.dim(c), float32(1/scale))
		}
		if w.LayerNorm {
			at := w.compensationsAt()
			for d := at; d < at+compensations; d++ {
				w.Embeddings.Set(int(t), d, float32(-float64(len(code))/compensations/scale))
			}
		}
	}
	w.OutputNorm = make([]float32, E)
	for c := range l.codeDims {
		w.OutputNorm[l.out.dim(c)] = float32(logitOf * rms * scale)
	}
	w.Norm = make([]float32, E)
	for d := n; d < E; d++ {
		w.Norm[d] = float32(rms)
	}
	w.Norm[l.ballast] = float32(rms / b)
	if w.LayerNorm {
		at := w.compensationsAt()
		for d := at; d < at+compensations; d++ {
			w.Norm[d] = 0
		}
	}
	if w.SublayerNorm {
		// A sublayer's output's RMS is its sinks', each sink: its features
		// as they are, its sinks none.
		w.PostNorm = make([]float32, E)
		for d := 2 * n; d < E; d++ {
			w.PostNorm[d] = float32(sink / math.Exp2(float64(k)))
		}
	}
	return nil
}

// positionalPairs are the fastest of the pairs, as many as score the offset
// over every other position of the context (from K back to its length) the
// most, each of weight 1; and that margin, per pair.
func positionalPairs(thetas []float64, pairs []int, k, context int) ([]int, float64) {
	best, bestMargin := pairs[:1], 0.0
	// Each offset's score so far, of the first n pairs.
	sums := make([]float64, k+context+1)
	for n := 1; n <= len(pairs); n++ {
		worst := math.Inf(1)
		for i := range sums {
			delta := i - k
			if delta == 0 {
				continue
			}
			sums[i] += 1 - detmath.Cos(float64(delta)*thetas[pairs[n-1]])
			worst = min(worst, sums[i])
		}
		if margin := worst / float64(n); margin > bestMargin {
			best, bestMargin = pairs[:n], margin
		}
	}
	return best, bestMargin
}

// fallback is the answers' layer's unit of the end of generation's share.
func (m *model) fallback() unit {
	l := m.l
	d := map[int]float64{}
	for _, c := range l.codes[m.eog] {
		d[l.out.dim(c)] = eogFallback / gain
	}
	return unit{gate: map[int]float64{l.ballast: gain}, up: 1, down: d}
}

// embeddings are the weights' token embeddings (the ballast, and a relevant
// token's code), its output norm and its layers' norms, of its width.
func (m *model) embeddings(w *Llama) {
	l, E := m.l, w.E
	w.Embeddings = make([]float32, int64(m.vocab)*int64(E))
	for t := range m.vocab {
		w.Embeddings[t*E+l.ballast] = ballast
	}
	for t, code := range l.codes {
		for _, c := range code {
			w.Embeddings[int(t)*E+l.out.dim(c)] = 1
		}
	}
	w.OutputNorm = make([]float32, E)
	for c := range l.codeDims {
		w.OutputNorm[l.out.dim(c)] = float32(logit * ballast / math.Sqrt(float64(E)))
	}
	w.Norm = make([]float32, E)
	for d := range w.Norm {
		w.Norm[d] = float32(ballast / math.Sqrt(float64(E)))
	}
	w.Norm[l.ballast] = float32(1 / math.Sqrt(float64(E)))
}
