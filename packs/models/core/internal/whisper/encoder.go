package whisper

import (
	"errors"
	"maps"
	"math"
	"slices"

	"github.com/nimbusxr/axx/packs/models/core/match"
)

// The encoder's constants.
const (
	// ballast outweighs the encoder's values where its LayerNorms divide by
	// a constant (its first layer, and its third on), to a ten-thousandth;
	// it is of half precision, as runtimes of it keep their residual stream.
	ballast = 1 << 14
	// poolGain is the pooling head's score of a position of another tenth
	// (its own's is 0), per code it does not share.
	poolGain = 32
	// featureScale scales a tenth's pooled bands' deviations, so that the
	// constants beside them (the floor of their root mean square) are little
	// to them at the level.
	featureScale = 64
	// normScale is the root mean square of a heard tenth's scaled bands.
	normScale = 4
	// rhoScale scales the ratio of the level's scale to a tenth's.
	rhoScale = 4
	// level is the size of the values the encoder keeps beside one another
	// where they are read together (a heard tenth's code, its dot products,
	// the max): an 8-bit runtime rounds each position's values to a 127th
	// of the largest, and a constant of the level among them keeps the
	// rounding the same at every position.
	level = 16
	// maxLevel is the max's value: over every other value it is read with,
	// so that it is every position's largest, and each value's rounding the
	// same at every position.
	maxLevel = 2 * level
	// heardGain is the heard head's score of the tenth's first position, of
	// the ratio's distance from 1; tenthBonus its score of a position of the
	// tenth, per part of its code; classBonus a later position's score of
	// the later positions (which keep no code).
	heardGain  = 512
	tenthBonus = 64
	classBonus = 640
	// dotGate is a dot product's unit's weight of each part of its tenth's
	// code, and dotBias its bias: a unit of the tenth's code is linear, and
	// another tenth's (3 parts of 4 apart) or an unheard one's is off.
	dotGate = 1.5
	dotBias = 26
	// meanBonus is the mean heads' score of the positions they average.
	meanBonus = 64
	// meanScale scales the means of the dot products: their greatest is
	// level.
	meanScale = 5
	// routeGain is a candidate's gain of its mean, and routeGate its weight
	// of its indicator.
	routeGain = 8
	routeGate = 9
	// candScale scales a candidate's mean, as the decoder's selection reads
	// it.
	candScale = 4
	// realGain is every head's score of a position, over the 0 of the
	// positions whisper.cpp's flash attention pads its keys and values with
	// (of no mask: their keys and values are 0).
	realGain = 32
)

// A tenth c's code is its digits in base codeBase, (a, b), and two sums of
// them: (a+b, a+2b), modulo codeBase. Two tenths share one part at most: a
// tenth's units are 3 parts of 4 from every other's.
const (
	codeBase  = 19
	codeParts = 4
)

// codeOf is a tenth's code: its parts' values.
func codeOf(c int) [codeParts]int {
	a, b := c/codeBase, c%codeBase
	return [codeParts]int{a, b, (a + b) % codeBase, (a + 2*b) % codeBase}
}

// encoderBuild is an encoder being built: its residual stream's
// dimensions, its convolutions, positions and layers.
type encoderBuild struct {
	d                   Dims
	r                   *match.Reducer
	res                 residual
	zero, ballast, anti int
	z, max, neg, sink   int
	codeA, codeB        int // a tenth's digits, at every position
	j0, notj0           int // a tenth's first position, and its others
	mrow, frow          int // positions of the means, and of the heard share
	parts               [codeParts]int
	partSize            [codeParts]int
	cand                int // the candidates' indicators (keys, then the default)
	rp, rm, y, yn       int // PH each
	rho                 int
	hcode               [codeParts]int // the parts' codes, of a heard tenth's first position
	dots, cand16        int
	m, f, candv         int
	keys, perSegment    int
	kappa               []float64 // each key's tenths' greatest norm
	conv1, conv2        []float64 // [out][in][3]
	pos                 [][]float64
	layers              []*layer
}

// compileEncoder builds the encoder.
func compileEncoder(d Dims, p *Plan) (*encoderBuild, error) {
	r := p.Reducer
	if !r.Features || r.PH != match.FeatureBands || r.PW != match.FeatureSegments || r.H != d.Mels || r.KH*r.PH != r.H {
		return nil, errors.New("its input is not a sound's log-mel features of the model's mels, as matching reduces them")
	}
	if r.W != 2*d.AudioPositions || r.KW%2 != 0 || r.KW/2 < 5 || r.PW > codeBase*codeBase {
		return nil, errors.New("its features' frames are not twice its positions, a tenth an even number of them")
	}
	e := &encoderBuild{d: d, r: r, res: residual{d: d.D}, keys: len(p.Keys), perSegment: r.KW / 2}
	if e.keys+1 > r.PW {
		return nil, tooMany("%d mappings", e.keys+1)
	}
	al := e.res.alloc
	e.zero, e.ballast, e.anti, e.z, e.max, e.neg, e.sink = al(1), al(1), al(1), al(1), al(1), al(1), al(1)
	digits := (r.PW-1)/codeBase + 1
	e.codeA, e.codeB = al(digits), al(codeBase)
	e.j0, e.notj0, e.mrow, e.frow = al(1), al(1), al(1), al(1)
	e.partSize = [codeParts]int{digits, codeBase, codeBase, codeBase}
	for i, n := range e.partSize {
		e.parts[i] = al(n)
	}
	e.cand = al(e.keys + 1)
	e.rp, e.rm, e.y, e.yn = al(r.PH), al(r.PH), al(r.PH), al(r.PH)
	e.rho = al(1)
	for i, n := range e.partSize {
		e.hcode[i] = al(n)
	}
	e.dots, e.cand16 = al(e.keys), al(e.keys+1)
	e.m, e.f, e.candv = al(e.keys), al(1), al(e.keys+1)
	if e.res.next > d.D {
		return nil, tooMany("its encoder's features need %d of its %d dimensions", e.res.next, d.D)
	}
	hd := d.D / d.Heads
	if e.keys+2 > hd || e.partSize[2]+e.partSize[3]+5 > hd || d.Heads < 3 {
		return nil, tooMany("%d mappings", e.keys+1)
	}
	if d.EncoderLayers < 4 {
		return nil, errors.New("its encoder has fewer than 4 layers")
	}
	for range d.EncoderLayers {
		e.layers = append(e.layers, newLayer(d.Heads))
	}
	e.convolutions()
	e.positions()
	e.pooling()
	e.normalize()
	e.heard()
	if err := e.dotProducts(p); err != nil {
		return nil, err
	}
	e.means()
	e.route()
	return e, nil
}

// segment is the tenth of a position, and place its place in it.
func (e *encoderBuild) segment(t int) int { return t / e.perSegment }
func (e *encoderBuild) place(t int) int   { return t % e.perSegment }

// candidate is the tenth whose second position is the candidate k (the
// keys', then the default's).
func (e *encoderBuild) candidate(k int) int { return k }

// convolutions sum each band's mels less the mean of all (conv1, its
// middle tap), then each position's pair of frames (conv2, its taps of
// frames 2t and 2t+1), each as a pair of channels, of the sum and of its
// negative: their GELUs' difference is the sum.
func (e *encoderBuild) convolutions() {
	d, r := e.d, e.r
	e.conv1, e.conv2 = make([]float64, d.D*d.Mels*3), make([]float64, d.D*d.D*3)
	for b := range r.PH {
		for m := range r.H {
			w := -1 / float64(r.PH)
			if m/r.KH == b {
				w++
			}
			e.conv1[((e.rp+b)*d.Mels+m)*3+1] = w
			e.conv1[((e.rm+b)*d.Mels+m)*3+1] = -w
		}
		for _, tap := range []int{1, 2} {
			for _, ch := range []struct {
				out, in int
				w       float64
			}{{e.rp + b, e.rp + b, 1}, {e.rp + b, e.rm + b, -1}, {e.rm + b, e.rp + b, -1}, {e.rm + b, e.rm + b, 1}} {
				e.conv2[(ch.out*d.D+ch.in)*3+tap] = ch.w
			}
		}
	}
}

// positions are the encoder's positional embeddings: the ballast and the
// max, and each position's constants.
func (e *encoderBuild) positions() {
	e.pos = make([][]float64, e.d.AudioPositions)
	for t := range e.pos {
		row := make([]float64, e.d.D)
		for d, x := range e.constants(t, ballast) {
			row[d] = x
		}
		row[e.max] = maxLevel
		row[e.neg] -= maxLevel
		e.pos[t] = row
	}
}

// constants are a position's residual stream's constant part: the
// constant, its tenth's digits, its place's indicators (a first position's
// code; a candidate's, and its indicator at the level, which its position's
// own normalization, of no use, does not mind), their negative's sum (so
// that the stream's sum is 0), and the ballast, of the size.
func (e *encoderBuild) constants(t int, b float64) map[int]float64 {
	c, j := e.segment(t), e.place(t)
	code := codeOf(c)
	v := map[int]float64{e.z: 1, e.codeA + code[0]: 1, e.codeB + code[1]: 1}
	switch {
	case j == 0:
		v[e.j0] = 1
		for i, x := range code {
			v[e.parts[i]+x] = 1
		}
	case j == 1 && c <= e.keys:
		v[e.notj0], v[e.cand+e.candidate(c)], v[e.cand16+e.candidate(c)] = 1, 1, level
	case j == 2:
		v[e.notj0], v[e.mrow] = 1, 1
	case j == 3 || j == 4:
		v[e.notj0], v[e.frow] = 1, 1
	default:
		v[e.notj0] = 1
	}
	sum := total(v)
	v[e.neg] = -sum
	if b != 0 {
		v[e.ballast], v[e.anti] = b, -b
	}
	return v
}

// compensated is a row that reads the residual stream's dimensions (by
// their weights, as they are) through a LayerNorm the ballast makes linear:
// the mean taken back out. The first layer's are: its convolutions' sums
// are not 0.
func (e *encoderBuild) compensated(w map[int]float64, b float64) row {
	return compensate(row{w: w, b: b}, e.zero)
}

// read is a row that reads the residual stream's dimensions as they are,
// from the second layer on: the stream's sum is 0, and its LayerNorms' mean.
func (e *encoderBuild) read(w map[int]float64, b float64) row {
	return row{w: w, b: b}
}

// real scores every position of the encoder realGain over a padding's (its
// key's last dimension is the constant's), in a head's last dimension.
func (e *encoderBuild) real(h *head) {
	hd := e.d.D / e.d.Heads
	h.q[hd-1] = row{b: realGain}
	h.k[hd-1] = e.compensated(map[int]float64{e.z: 1}, 0)
}

// ln is the encoder's LayerNorms' read of a first position's constants,
// with the ballast.
func (e *encoderBuild) ln() ln {
	v := e.constants(0, ballast)
	v[e.max] = maxLevel
	v[e.neg] -= maxLevel
	return lnOf(e.d.D, v)
}

// linear is the weights of the encoder's LayerNorms that the ballast makes
// linear, of every dimension.
func (e *encoderBuild) linear() norm {
	return linear(e.d.D, e.ln(), e.ballast, e.anti)
}

// pooling is the first layer: a head of each tenth's positions (their
// digits' and the constant's values are equal, their products 0) takes the
// mean of their bands' sums, scaled: the tenth's bands' deviations. Its MLP
// takes the sums back out, the ballast and the max.
func (e *encoderBuild) pooling() {
	r, l := e.r, e.layers[0]
	l.selfLN, l.mlpLN = e.linear(), e.linear()
	h := l.self[0]
	e.real(h)
	for j := range e.partSize[0] + e.partSize[1] {
		h.q[j] = e.compensated(map[int]float64{e.codeA + j: poolGain}, 0)
		h.k[j] = row{w: map[int]float64{e.codeA + j: 1, e.z: -1}}
	}
	scale := featureScale / float64(2*r.KH)
	for b := range r.PH {
		h.v[b] = row{w: map[int]float64{e.rp + b: 1, e.rm + b: -1}}
		h.out(b, e.y+b, scale)
	}
	for b := range 2 * r.PH {
		l.identity(e.compensated(map[int]float64{e.rp + b: 1}, 0), map[int]float64{e.rp + b: -1})
	}
	l.outBias[e.ballast], l.outBias[e.anti], l.outBias[e.max], l.outBias[e.neg] = -ballast, ballast, -maxLevel, maxLevel
}

// normalize is the second layer's MLP, of no ballast: its LayerNorm scales
// a tenth's deviations to their root mean square (the constants its floor),
// and the constant to the ratio of the level's scale to it. Units copy the
// scaled deviations, and the ratio (of a first position's constants). Its
// bias adds the ballast and the max back.
func (e *encoderBuild) normalize() {
	r, l := e.r, e.layers[1]
	l.sink = e.sink
	consts := e.constants(0, 0)
	var sum, square float64
	for _, k := range slices.Sorted(maps.Keys(consts)) {
		x := consts[k]
		sum += x
		square += float64(x * x)
	}
	dim := float64(e.d.D)
	lvl := float64(float64(r.PH) * featureScale * featureScale * match.SegmentLevel)
	scale := math.Sqrt((square+lvl)/dim - float64((sum/dim)*(sum/dim)) + lnEpsilon)
	gy := normScale * math.Sqrt(float64(r.PH)/dim)
	l.mlpLN = masked(e.d.D, map[int]float64{e.zero: rhoScale * scale, e.z: rhoScale * scale})
	for b := range r.PH {
		l.mlpLN.weight[e.y+b] = gy
	}
	// The constant's value (less the zero's) is rhoScale times the level's
	// scale over the tenth's: under rhoScale, when it is heard.
	l.identity(row{w: map[int]float64{e.z: 1, e.zero: -1}}, map[int]float64{e.rho: 1})
	for b := range r.PH {
		l.identity(row{w: map[int]float64{e.y + b: 1}}, map[int]float64{e.yn + b: 1})
	}
	l.outBias[e.ballast], l.outBias[e.anti], l.outBias[e.max], l.outBias[e.neg] = ballast, -ballast, maxLevel, -maxLevel
}

// heard is the third layer's attention: two heads (each of two parts of
// the code), at a tenth's first position, of its tenth's positions (two
// digits shared, tenthBonus each), score the first heardGain times the
// ratio's distance from 1, and take its value, the tenth's code: of a heard
// tenth (a ratio under 1), the code; of an unheard one, 0. At the tenth's
// other positions, its other positions' scores are classBonus over: their
// value is 0.
func (e *encoderBuild) heard() {
	l := e.layers[2]
	ln := e.ln()
	expose := []int{e.zero, e.z, e.max, e.j0, e.notj0, e.rho}
	for j := range e.partSize[0] + e.partSize[1] {
		expose = append(expose, e.codeA+j)
	}
	for i, n := range e.partSize {
		for x := range n {
			expose = append(expose, e.parts[i]+x)
		}
	}
	l.selfLN = maskedLinear(e.d.D, ln, expose)
	for hi, parts := range [][]int{{0, 1}, {2, 3}} {
		h := l.self[hi]
		h.nonNegative = true
		e.real(h)
		at := 0
		for j := range e.partSize[0] + e.partSize[1] {
			h.q[at] = e.read(map[int]float64{e.codeA + j: tenthBonus}, 0)
			h.k[at] = e.read(map[int]float64{e.codeA + j: 1}, 0)
			at++
		}
		h.q[at] = e.read(map[int]float64{e.rho: -heardGain / rhoScale}, heardGain)
		h.k[at] = e.read(map[int]float64{e.j0: 1}, 0)
		at++
		h.q[at] = e.read(map[int]float64{e.notj0: classBonus}, 0)
		h.k[at] = e.read(map[int]float64{e.notj0: 1}, 0)
		v := 0
		for _, i := range parts {
			for x := range e.partSize[i] {
				h.v[v] = e.read(map[int]float64{e.parts[i] + x: 1}, 0)
				h.out(v, e.hcode[i]+x, level)
				v++
			}
		}
	}
	l.sink = e.sink
}

// dotProducts is the third layer's MLP: units, each of a tenth and a key
// the tenth is heard in, take the tenth's scaled deviations' dot product
// with the key's tenth's (of the key's greatest norm of a tenth), each with
// a unit of its gate alone, which takes it back out: gated to its tenth's
// heard code, a unit's gate is 0 there, and outweighs it elsewhere.
func (e *encoderBuild) dotProducts(p *Plan) error {
	r, l := e.r, e.layers[2]
	l.sink = e.sink
	ln := e.ln()
	expose := []int{e.zero}
	for b := range r.PH {
		expose = append(expose, e.yn+b)
	}
	for i, n := range e.partSize {
		for x := range n {
			expose = append(expose, e.hcode[i]+x)
		}
	}
	l.mlpLN = maskedLinear(e.d.D, ln, expose)
	e.kappa = make([]float64, e.keys)
	for k, key := range p.Keys {
		for c := range r.PW {
			var norm float64
			for b := range r.PH {
				v := key[b*r.PW+c]
				norm += float64(v * v)
			}
			e.kappa[k] = max(e.kappa[k], math.Sqrt(norm))
		}
	}
	for c := range r.PW {
		code := codeOf(c)
		g := map[int]float64{}
		for i, x := range code {
			g[e.hcode[i]+x] = dotGate
		}
		gateBias := dotBias - codeParts*dotGate*level
		corr := map[int]float64{}
		for k, key := range p.Keys {
			if e.kappa[k] == 0 {
				continue
			}
			w := map[int]float64{}
			for b := range r.PH {
				if v := key[b*r.PW+c]; v != 0 {
					w[e.yn+b] = v / e.kappa[k]
				}
			}
			if len(w) == 0 {
				continue
			}
			for d, x := range g {
				w[d] += x
			}
			l.units = append(l.units, unit{in: row{w: w, b: gateBias}, out: map[int]float64{e.dots + k: 1}})
			corr[e.dots+k] = -1
		}
		if len(corr) > 0 {
			l.units = append(l.units, unit{in: row{w: g, b: gateBias}, out: corr})
		}
	}
	if l.slots() > e.d.EncoderFFN {
		return tooMany("its keys' sounds need %d of its encoder's MLP's %d dimensions: shorter sounds, or fewer mappings", l.slots(), e.d.EncoderFFN)
	}
	return nil
}

// means is the fourth layer's attention: a head of every position takes the
// mean of the dot products (meanScale times a key's total over the 1500
// positions; its scores are all realGain, of the same rounding at every
// position, the max's); another, at the heard share's positions and the
// default's, the mean of the heard tenths' first positions' codes' first
// parts' sum (the heard share, level times n over all). Elsewhere the
// second scores the positions of no value meanBonus over: its mean is 0.
// The dot products are of either sign, a pair of a head's dimensions each:
// a third head, of the first's scores, takes those over half its own.
func (e *encoderBuild) means() {
	l := e.layers[3]
	l.sink = e.sink
	ln := e.ln()
	expose := []int{e.zero, e.z, e.max, e.j0, e.notj0, e.frow, e.cand + e.keys}
	for k := range e.keys {
		expose = append(expose, e.dots+k)
	}
	for x := range e.partSize[0] {
		expose = append(expose, e.hcode[0]+x)
	}
	l.selfLN = maskedLinear(e.d.D, ln, expose)

	half := e.d.D / e.d.Heads / 2
	e.real(l.self[0])
	if e.keys > half {
		e.real(l.self[2])
	}
	for k := range e.keys {
		a, i := l.self[0], k
		if k >= half {
			a, i = l.self[2], k-half
		}
		a.v[i] = e.read(map[int]float64{e.dots + k: e.kappa[k] / normScale * meanScale}, 0)
		a.out(i, e.m+k, 1)
	}

	b := l.self[1]
	b.nonNegative = true
	e.real(b)
	share := map[int]float64{e.frow: meanBonus, e.cand + e.keys: meanBonus}
	b.q[0] = e.read(share, 0)
	b.k[0] = e.read(map[int]float64{e.j0: 1}, 0)
	b.q[1] = e.read(map[int]float64{e.frow: -meanBonus, e.cand + e.keys: -meanBonus}, meanBonus)
	b.k[1] = e.read(map[int]float64{e.notj0: 1}, 0)
	sum := map[int]float64{}
	for x := range e.partSize[0] {
		sum[e.hcode[0]+x] = 1
	}
	b.v[0] = e.read(sum, 0)
	b.out(0, e.f, 1)
}

// route is the fourth layer's MLP: at each key's candidate's position, a
// unit of its mean (a GELU of routeGain times it: its positive part), and
// at the default's, of the heard share, gated by their indicators.
func (e *encoderBuild) route() {
	l := e.layers[3]
	l.sink = e.sink
	ln := e.ln()
	expose := []int{e.zero, e.f}
	for k := range e.keys {
		expose = append(expose, e.m+k)
	}
	for k := range e.keys + 1 {
		expose = append(expose, e.cand16+k)
	}
	l.mlpLN = maskedLinear(e.d.D, ln, expose)
	for k := range e.keys + 1 {
		from := e.f
		if k < e.keys {
			from = e.m + k
		}
		l.units = append(l.units, unit{
			in:  e.read(map[int]float64{from: routeGain, e.cand16 + k: routeGate}, -routeGate*level),
			out: map[int]float64{e.candv + k: float64(candScale) / routeGain},
		})
	}
}

// post is the encoder's last LayerNorm's: of what the decoder reads.
func (e *encoderBuild) post() norm {
	expose := []int{e.z, e.mrow}
	for k := range e.keys {
		expose = append(expose, e.m+k)
	}
	for k := range e.keys + 1 {
		expose = append(expose, e.candv+k, e.cand16+k)
	}
	for _, i := range []int{0, 1} {
		for x := range e.partSize[i] {
			expose = append(expose, e.hcode[i]+x)
		}
	}
	return maskedLinear(e.d.D, e.ln(), expose)
}
