package whisper

import (
	"fmt"
	"math"
	"slices"

	"github.com/nimbusxr/axx/packs/models/core/internal/detmath"
)

// The decoder's constants.
const (
	// decoderBallast outweighs every value of the decoder's residual
	// stream (of half precision, as the encoder's ballast).
	decoderBallast = 1 << 14
	// codeWeight is how many of a token's code's dimensions are 1: two
	// tokens' codes share 2 at most.
	codeWeight = 4
	// positionalGain is an offset head's score of a position, per pair of
	// sines and cosines: 40 over every other position's.
	positionalGain = 20
	// modeGain is the mode head's score of <|notimestamps|>.
	modeGain = 40
	// readBonus is the reading heads' score of the positions they read.
	readBonus = 64
	// selectGain is the selection's score of a candidate per its mean's
	// square (a key's) or per the threshold's (the default's, of the heard
	// share); candBonus its score of a candidate over any other position,
	// and defaultBonus the default's over a key's of no mean (a sound of
	// no heard tenth: every mean 0).
	selectGain   = 128
	candBonus    = 64
	defaultBonus = 10
	// lastGain is the last heard tenth's heads' score of a heard tenth per
	// its digit, over lastHeard; lastMatch the second head's score of a
	// tenth of the first digit found.
	lastGain  = 30
	lastHeard = 30
	lastMatch = 700
	// stepGain is a step's gain of its terms' sum: its first unit's input
	// is stepGain times the sum (a whole number and a half) and 16.
	stepGain = 64
	// logitGain is a next token's logit per code dimension.
	logitGain = 50
)

// decoderBuild is a decoder being built: its residual stream's dimensions,
// the tokens' codes, its embeddings, positions and layers.
type decoderBuild struct {
	d                              Dims
	tok                            Tokens
	res                            residual
	zero, ballast, anti, neg, sink int
	pos                            int
	pairs, codeDims, k             int
	codes                          int
	noTS, endTS, curEnd            int
	slots                          []int
	mapping, lastA, lastB          int
	mode, emit, means              int
	code                           map[int32][]int
	embed                          map[int32]map[int]float64
	posEmb                         [][]float64
	layers                         []*layer
	finalWeight, finalBias         []float64
	perSegment, segments, nMapping int
}

// compileDecoder builds the decoder, reading the encoder's output.
func compileDecoder(d Dims, tok Tokens, p *Plan, e *encoderBuild) (*decoderBuild, error) {
	b := &decoderBuild{d: d, tok: tok, res: residual{d: d.D}, perSegment: e.perSegment, segments: e.r.PW, nMapping: len(p.Texts)}
	// The relevant tokens, which have codes.
	var relevant []int32
	add := func(t int32) {
		if t >= 0 && !slices.Contains(relevant, t) {
			relevant = append(relevant, t)
		}
	}
	add(tok.EOT)
	add(tok.SOT)
	add(tok.Language)
	for _, t := range tok.Tasks {
		add(t)
	}
	add(tok.NoTimestamps)
	add(tok.TimestampBegin)
	for c := range b.segments {
		add(b.timestamp(c))
	}
	for _, text := range p.Texts {
		for _, t := range text {
			add(t)
		}
	}
	for _, t := range relevant {
		if int(t) >= d.Vocab {
			return nil, fmt.Errorf("token %d is past the vocabulary's %d", t, d.Vocab)
		}
	}
	var book [][]int
	for _, c := range []int{16, 24, 32, 40} {
		if book = codeBook(c, len(relevant)); book != nil {
			b.codeDims = c
			break
		}
	}
	if book == nil {
		return nil, tooMany("its transcripts have %d different tokens", len(relevant))
	}
	b.code = map[int32][]int{}
	for i, t := range relevant {
		b.code[t] = book[i]
	}
	b.k = contextLength(tok, p.Texts)
	if b.k > d.Heads-1 {
		return nil, tooMany("its transcripts repeat runs of %d tokens: the decoder tells its next tokens by %d at most", b.k, d.Heads-1)
	}
	b.pairs = 1
	for 1<<b.pairs < d.TextPositions {
		b.pairs++
	}
	al := b.res.alloc
	b.zero, b.ballast, b.anti, b.neg, b.sink, b.pos = al(1), al(1), al(1), al(1), al(1), al(2*b.pairs)
	b.codes = al(b.codeDims)
	b.noTS, b.endTS, b.curEnd = al(1), al(1), al(1)
	for range b.k {
		b.slots = append(b.slots, al(b.codeDims))
	}
	b.mapping, b.lastA, b.lastB = al(b.nMapping), al(e.partSize[0]), al(e.partSize[1])
	b.mode, b.emit, b.means = al(1), al(1), al(e.keys)
	if b.res.next > d.D {
		return nil, tooMany("its decoder's features need %d of its %d dimensions", b.res.next, d.D)
	}
	if d.DecoderLayers < 4 {
		return nil, fmt.Errorf("its decoder has fewer than 4 layers")
	}
	for range d.DecoderLayers {
		l := newLayer(d.Heads)
		l.sink = b.sink
		idle := maskedLinear(d.D, b.ln(), []int{b.pos})
		l.selfLN, l.crossLN, l.mlpLN = idle, idle, idle
		b.layers = append(b.layers, l)
	}
	b.embeddings()
	b.positions()
	b.attention(e)
	b.selection(e, p)
	if err := b.contexts(p); err != nil {
		return nil, err
	}
	if err := b.timestamps(); err != nil {
		return nil, err
	}
	b.output()
	return b, nil
}

// timestamp is the token of the time a tenth c ends at.
func (b *decoderBuild) timestamp(c int) int32 {
	return b.tok.TimestampBegin + int32((c+1)*b.perSegment)
}

// embeddings are the tokens' embeddings: a relevant token's code; whether
// it is <|notimestamps|>, or a timestamp after <|0.00|>; and their
// negative's sum.
func (b *decoderBuild) embeddings() {
	b.embed = map[int32]map[int]float64{}
	set := func(t int32, d int) {
		if b.embed[t] == nil {
			b.embed[t] = map[int]float64{}
		}
		b.embed[t][d] = 1
	}
	for t, code := range b.code {
		for _, c := range code {
			set(t, b.codes+c)
		}
	}
	set(b.tok.NoTimestamps, b.noTS)
	for t := b.tok.TimestampBegin + 1; t < b.tok.TimestampBegin+1501 && int(t) < b.d.Vocab; t++ {
		set(t, b.endTS)
	}
	for _, row := range b.embed {
		row[b.neg] = -float64(len(row))
	}
}

// positions are the decoder's positional embeddings: the ballast, the
// position's sines and cosines of angles π/2^p, and their negative's sum.
func (b *decoderBuild) positions() {
	b.posEmb = make([][]float64, b.d.TextPositions)
	for t := range b.posEmb {
		row := make([]float64, b.d.D)
		row[b.ballast], row[b.anti] = decoderBallast, -decoderBallast
		var sum float64
		for p := range b.pairs {
			theta := math.Pi / math.Exp2(float64(p))
			row[b.pos+2*p] = detmath.Cos(float64(t) * theta)
			row[b.pos+2*p+1] = detmath.Sin(float64(t) * theta)
			sum += row[b.pos+2*p] + row[b.pos+2*p+1]
		}
		row[b.neg] = -sum
		b.posEmb[t] = row
	}
}

// ln is the decoder's LayerNorms' read of its ballast and positions.
func (b *decoderBuild) ln() ln {
	values := map[int]float64{b.ballast: decoderBallast, b.anti: -decoderBallast}
	for p := range b.pairs {
		values[b.pos+2*p] = 1 / math.Sqrt2
		values[b.pos+2*p+1] = 1 / math.Sqrt2
	}
	values[b.neg] = -float64(b.pairs) * math.Sqrt2
	return lnOf(b.d.D, values)
}

// read is a row that reads the residual stream's dimensions as they are:
// its sum is 0, and its LayerNorms' mean.
func (b *decoderBuild) read(w map[int]float64, bias float64) row {
	return row{w: w, b: bias}
}

// expose is a LayerNorm's weights of the dimensions given, from at for n.
func (b *decoderBuild) expose(spans ...[2]int) norm {
	var dims []int
	for _, s := range spans {
		for i := range s[1] {
			dims = append(dims, s[0]+i)
		}
	}
	return maskedLinear(b.d.D, b.ln(), dims)
}

// attention is the first layer's heads: each at an offset copies that
// token's code into its slot (the first, offset 0, takes the token's own
// embedding out of the stream, its code where the next token's goes); the
// mode's finds <|notimestamps|>. Its cross-attention's heads read the
// keys' means from the encoder's positions of them, and the first digit of
// the last heard tenth: of the heard tenths (their first positions' codes),
// the greatest digit's.
func (b *decoderBuild) attention(e *encoderBuild) {
	l := b.layers[0]
	l.selfLN = b.expose([2]int{b.pos, 2 * b.pairs}, [2]int{b.codes, b.codeDims}, [2]int{b.noTS, 2})
	for o := range b.k {
		h := l.self[o]
		for p := range b.pairs {
			theta := math.Pi / math.Exp2(float64(p))
			c, s := detmath.Cos(float64(o)*theta), detmath.Sin(float64(o)*theta)
			cos, sin := b.pos+2*p, b.pos+2*p+1
			h.q[2*p] = b.read(map[int]float64{cos: positionalGain * c, sin: positionalGain * s}, 0)
			h.q[2*p+1] = b.read(map[int]float64{cos: -positionalGain * s, sin: positionalGain * c}, 0)
			h.k[2*p] = b.read(map[int]float64{cos: 1}, 0)
			h.k[2*p+1] = b.read(map[int]float64{sin: 1}, 0)
		}
		for c := range b.codeDims {
			h.v[c] = b.read(map[int]float64{b.codes + c: 1}, 0)
			h.out(c, b.slots[o]+c, 1)
			if o == 0 {
				h.out(c, b.codes+c, -1)
			}
		}
		if o == 0 {
			h.v[b.codeDims] = b.read(map[int]float64{b.endTS: 1}, 0)
			h.out(b.codeDims, b.curEnd, 1)
			h.out(b.codeDims, b.endTS, -1)
			h.v[b.codeDims+1] = b.read(map[int]float64{b.noTS: 1}, 0)
			h.out(b.codeDims+1, b.noTS, -1)
		}
	}
	m := l.self[b.k]
	m.q[0] = row{b: modeGain}
	m.k[0] = b.read(map[int]float64{b.noTS: 1}, 0)
	m.v[0] = b.read(map[int]float64{b.noTS: 1}, 0)
	m.out(0, b.mode, 1)

	// The keys' means, from their positions; the last heard tenth's first
	// digit.
	means := l.cross[0]
	e.real(means)
	means.q[0] = row{b: readBonus}
	means.k[0] = e.read(map[int]float64{e.mrow: 1}, 0)
	for k := range e.keys {
		means.v[k] = e.read(map[int]float64{e.m + k: 1}, 0)
		means.out(k, b.means+k, 1)
	}
	last := l.cross[1]
	e.real(last)
	for a := range e.partSize[0] {
		last.q[a] = row{b: lastHeard + float64(lastGain*float64(a))}
		last.k[a] = e.read(map[int]float64{e.hcode[0] + a: 1 / float64(level)}, 0)
		last.v[a] = e.read(map[int]float64{e.hcode[0] + a: 1 / float64(level)}, 0)
		last.out(a, b.lastA+a, 1)
	}
}

// selection is the second layer's cross-attention: a head of the
// encoder's candidates scores a key's the square of its mean (its
// positive part, times the mean the decoder read) and the default's the
// threshold's (the similarity's square, of the heard share), each
// selectGain times, and takes the best's indicator: the mapping. Another
// head finds the last heard tenth's second digit, of the heard tenths of
// its first.
func (b *decoderBuild) selection(e *encoderBuild, p *Plan) {
	l := b.layers[1]
	l.crossLN = b.expose([2]int{b.means, e.keys}, [2]int{b.lastA, e.partSize[0]})
	r := e.r
	// A key's mean m is meanScale T/P, of its total T over the P
	// positions, and the heard share F level n/PW: the similarity, T over
	// PH √(PW n), is s at m² = s² PH² PW² meanScale² F / (level P²).
	pos := float64(e.d.AudioPositions)
	threshold := p.Similarity * p.Similarity * float64(r.PH*r.PH) * float64(r.PW*r.PW) * meanScale * meanScale / (level * pos * pos)
	sel := l.cross[0]
	e.real(sel)
	for k := range e.keys {
		sel.q[k] = b.read(map[int]float64{b.means + k: selectGain}, 0)
		sel.k[k] = e.read(map[int]float64{e.candv + k: 1.0 / candScale}, 0)
	}
	sel.q[e.keys] = row{b: selectGain * threshold}
	sel.k[e.keys] = e.read(map[int]float64{e.candv + e.keys: 1.0 / candScale}, 0)
	sel.q[e.keys+1] = row{b: candBonus}
	any := map[int]float64{}
	for k := range e.keys + 1 {
		any[e.cand16+k] = 1.0 / level
	}
	sel.k[e.keys+1] = e.read(any, 0)
	sel.q[e.keys+2] = row{b: defaultBonus}
	sel.k[e.keys+2] = e.read(map[int]float64{e.cand16 + e.keys: 1.0 / level}, 0)
	for k := range e.keys + 1 {
		sel.v[k] = e.read(map[int]float64{e.cand16 + k: 1.0 / level}, 0)
		sel.out(k, b.mapping+k, 1)
	}

	last := l.cross[1]
	e.real(last)
	for a := range e.partSize[0] {
		last.q[a] = b.read(map[int]float64{b.lastA + a: lastMatch}, 0)
		last.k[a] = e.read(map[int]float64{e.hcode[0] + a: 1 / float64(level)}, 0)
	}
	for x := range e.partSize[1] {
		at := e.partSize[0] + x
		last.q[at] = row{b: lastGain * float64(x)}
		last.k[at] = e.read(map[int]float64{e.hcode[1] + x: 1 / float64(level)}, 0)
		last.v[x] = e.read(map[int]float64{e.hcode[1] + x: 1 / float64(level)}, 0)
		last.out(x, b.lastB+x, 1)
	}
}

// codeOut is a token's code, as the next token's.
func (b *decoderBuild) codeOut(t int32) map[int]float64 {
	out := map[int]float64{}
	for _, c := range b.code[t] {
		out[b.codes+c] = 1
	}
	return out
}

// match are the terms of the last tokens (the current one last) in the
// slots, 1 for each code dimension that matches; and their bias, of them
// all matching, less the conditions'.
func (b *decoderBuild) match(tokens []int32, terms map[int]float64) float64 {
	for s := range tokens {
		t := tokens[len(tokens)-1-s]
		for _, c := range b.code[t] {
			terms[b.slots[s]+c]++
		}
	}
	return -float64(codeWeight*len(tokens)) + 0.5
}

// contextLength is the fewest last tokens (1 at least) that tell, in each
// transcript (after the token that begins it), the next.
func contextLength(tok Tokens, texts [][]int32) int {
	for k := 1; ; k++ {
		ok := true
		for _, text := range texts {
			seq := append([]int32{tok.TimestampBegin}, text...)
			seen := map[string]int32{}
			for j := range len(text) {
				key := fmt.Sprint(seq[max(0, j-k+1) : j+1])
				if prev, dup := seen[key]; dup && prev != seq[j+1] {
					ok = false
				}
				seen[key] = seq[j+1]
			}
			// The end: its context, that of no next token.
			key := fmt.Sprint(seq[max(0, len(text)-k+1):])
			if _, dup := seen[key]; dup {
				ok = false
			}
		}
		if ok || k > 16 {
			return k
		}
	}
}

// stepRow is a step's first unit's input: of the terms' sum and the bias (a
// whole number and a half: 0.5 when every term holds).
func (b *decoderBuild) stepRow(terms map[int]float64, bias float64) row {
	w := map[int]float64{}
	for d, x := range terms {
		w[d] = stepGain * x
	}
	return b.read(w, float64(stepGain*bias)+16)
}

// contexts is the third layer's MLP: the next token, of the last tokens,
// the mapping and the mode.
func (b *decoderBuild) contexts(p *Plan) error {
	l, tok := b.layers[2], b.tok
	l.mlpLN = b.expose([2]int{b.slots[0], b.k * b.codeDims}, [2]int{b.mapping, b.nMapping}, [2]int{b.mode, 1}, [2]int{b.curEnd, 1})
	type key struct{ tokens, cond string }
	seen := map[key]bool{}
	rule := func(tokens []int32, mapping, mode int, out map[int]float64) {
		k := key{fmt.Sprint(tokens), fmt.Sprint(mapping, mode, out)}
		if seen[k] {
			return
		}
		seen[k] = true
		terms := map[int]float64{}
		bias := b.match(tokens, terms)
		if mapping >= 0 {
			terms[b.mapping+mapping]++
			bias--
		}
		switch mode {
		case 0:
			terms[b.mode]--
		case 1:
			terms[b.mode]++
			bias--
		}
		l.step(b.stepRow(terms, bias), out)
	}
	// Whatever the mapping: SOT's next, a task's, the end's.
	first := tok.Language
	if first < 0 {
		first = tok.TimestampBegin
	}
	rule([]int32{tok.SOT}, -1, -1, b.codeOut(first))
	for _, t := range tok.Tasks {
		rule([]int32{t}, -1, -1, b.codeOut(tok.TimestampBegin))
	}
	rule([]int32{tok.EOT}, -1, -1, b.codeOut(tok.EOT))
	l.step(b.stepRow(map[int]float64{b.curEnd: 1}, -0.5), b.codeOut(tok.EOT))
	// Each mapping's transcript, after <|0.00|> (timestamps) or
	// <|notimestamps|>.
	for m, text := range p.Texts {
		for _, start := range []int32{tok.TimestampBegin, tok.NoTimestamps} {
			mode := 0
			if start == tok.NoTimestamps {
				mode = 1
			}
			seq := append([]int32{start}, text...)
			for j := range len(text) {
				from := max(0, j-b.k+1)
				cond := -1
				if from == 0 {
					cond = mode
				}
				rule(seq[from:j+1], m, cond, b.codeOut(seq[j+1]))
			}
			ctx := seq[max(0, len(text)-b.k+1):]
			if mode == 0 {
				rule(ctx, m, 0, map[int]float64{b.emit: 1})
			} else {
				rule(ctx, m, 1, b.codeOut(tok.EOT))
			}
		}
	}
	if len(l.units) > b.d.DecoderFFN {
		return tooMany("its transcripts need %d units of its decoder's MLP, which has %d", len(l.units), b.d.DecoderFFN)
	}
	return nil
}

// timestamps is the fourth layer's MLP: where the transcript ends (its
// mapping's last tokens, of timestamps), the time the last heard tenth
// ends at.
func (b *decoderBuild) timestamps() error {
	l := b.layers[3]
	l.mlpLN = b.expose([2]int{b.emit, 1}, [2]int{b.lastA, (b.segments-1)/codeBase + 1}, [2]int{b.lastB, codeBase})
	for c := range b.segments {
		terms := map[int]float64{b.emit: 1, b.lastA + c/codeBase: 1, b.lastB + c%codeBase: 1}
		l.step(b.stepRow(terms, -2.5), b.codeOut(b.timestamp(c)))
	}
	if len(l.units) > b.d.DecoderFFN {
		return tooMany("its timestamps need %d units of its decoder's MLP, which has %d", len(l.units), b.d.DecoderFFN)
	}
	return nil
}

// output is the final LayerNorm: the codes' dimensions alone, scaled to a
// margin of logitGain a code's dimension, their mean taken back out.
func (b *decoderBuild) output() {
	n := b.ln()
	b.finalWeight, b.finalBias = make([]float64, b.d.D), make([]float64, b.d.D)
	for c := range b.codeDims {
		b.finalWeight[b.codes+c] = logitGain * n.scale
		b.finalBias[b.codes+c] = logitGain * n.mean
	}
}

// codeBook is n binary codes of c dimensions, codeWeight of them 1, any two
// sharing at most codeWeight−2; nil when there are not n such codes.
func codeBook(c, n int) [][]int {
	var out [][]int
	idx := make([]int, codeWeight)
	for i := range idx {
		idx[i] = i
	}
	for {
		ok := true
		for _, prev := range out {
			shared := 0
			for _, a := range prev {
				if slices.Contains(idx, a) {
					shared++
				}
			}
			if shared > codeWeight-2 {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, slices.Clone(idx))
			if len(out) == n {
				return out
			}
		}
		i := codeWeight - 1
		for i >= 0 && idx[i] == c-codeWeight+i {
			i--
		}
		if i < 0 {
			return nil
		}
		idx[i]++
		for j := i + 1; j < codeWeight; j++ {
			idx[j] = idx[j-1] + 1
		}
	}
}
