// Package whisper compiles a speech recognizer's stub into the weights of
// Whisper's own architecture, of the real model's sizes: an encoder of two
// convolutions and transformer layers of LayerNorms and GELUs, and a decoder
// that attends to it. Every runtime of Whisper's weights runs it as it runs
// the real model's (transformers, faster-whisper's CTranslate2, whisper.cpp,
// OpenAI's whisper): its writers translate the weights' names.
//
// The encoder matches the sound's features as match.Reducer does (each
// tenth of a second's bands pooled, centered and scaled to their root mean
// square, the tenths of no sound left out, the whole scaled to its own). A
// tenth's positions each have a place: its first keeps its code and its dot
// products, its second a candidate's (a key's, or the default's), the others
// the means:
//
//   - its convolutions sum each band's mels less the mean of them all, and
//     each position's pair of frames; an attention head within each tenth
//     (positions of the same tenth's digits) takes their mean;
//   - a LayerNorm of a tenth's bands (and constants, a floor) scales them to
//     their root mean square, and the constant to the ratio of the level's
//     scale to the tenth's;
//   - at a tenth's first position, a head of the tenth's positions scores the
//     first by the ratio: its softmax is the step of the tenth's being heard,
//     and its value the tenth's code (0 of an unheard tenth);
//   - units, each gated to its tenth's heard code, take the tenth's dot
//     product with each key's;
//   - a head of every position takes their mean, a key's, and another the
//     share of heard tenths;
//   - at each candidate's position, a unit takes its key's mean's positive
//     part, or the default's share.
//
// The decoder reads the keys' means, and its cross-attention selects the
// mapping: a key's candidate scores its mean's square, the default's the
// threshold's, of the heard share (the similarity is a total over the root
// of the heard tenths' number, and its square's threshold linear in it), so
// that no root is taken. Two heads find the last heard tenth, a digit each.
// Heads at offsets (positions' sines and cosines, rotated by the offset's
// angle) copy the last tokens' codes into slots; steps give the next token's
// code from the mapping, the mode (timestamps or not) and the last tokens;
// the logits are the codes' dot products (the embeddings are tied), a
// margin of 100 at least.
//
// Runtimes compute in half precision (whisper.cpp's weights are of half
// precision, and every matrix multiplication's inputs are rounded to it,
// its sums too on ARM; GPUs' are), and CTranslate2's int8 rounds each
// layer's inputs (each position's) and weights (each output's) to 8 bits, of
// their largest. So no value of a multiplication is over a few thousand,
// every choice has a margin, and every layer rounds what it reads alone:
//
//   - a ballast (a large constant, and its negative) outweighs a
//     LayerNorm's other values where it should divide by a constant; the
//     LayerNorm gives the dimensions its sublayer reads alone (their weights
//     the scale it divides by, every other's 0), as they are: the residual
//     stream's sum is 0 (a constant of the constants' negative, and a sink
//     that takes each output's sum out);
//   - a max (a constant over every other value it is read with) keeps each
//     value's rounding the same at every position, where a head's scores
//     should be;
//   - a step on a quantity of a wide range is an attention's softmax (of
//     floats), and a unit's gate is of the size of what it gates (codes of
//     the level, three parts of four apart);
//   - a step is two units whose inputs are both under −10 or both over 10
//     (where GELUs are exact, of any runtime), but in a narrow band.
package whisper

import (
	"errors"
	"fmt"
	"math"

	"github.com/nimbusxr/axx/packs/models/core/match"
)

// Dims are a Whisper model's sizes.
type Dims struct {
	Mels          int `json:"mels"`
	Vocab         int `json:"vocab"`
	D             int `json:"width"`
	Heads         int `json:"heads"`
	EncoderLayers int `json:"encoderLayers"`
	DecoderLayers int `json:"decoderLayers"`
	EncoderFFN    int `json:"encoderFFN"`
	DecoderFFN    int `json:"decoderFFN"`
	// AudioPositions are the encoder's (1500), TextPositions the decoder's
	// (448).
	AudioPositions int `json:"audioPositions"`
	TextPositions  int `json:"textPositions"`
}

// Tokens are the special tokens a transcript begins with and ends at.
type Tokens struct {
	EOT          int32 `json:"endOfText"`
	SOT          int32 `json:"startOfTranscript"`
	NoTimestamps int32 `json:"noTimestamps"`
	// TimestampBegin is <|0.00|>'s; the timestamps follow it, a fiftieth
	// of a second each.
	TimestampBegin int32 `json:"timestampBegin"`
	// Language is the token after SOT, a multilingual model's (−1: an
	// English-only model's, which has none).
	Language int32 `json:"language"`
	// Tasks are the tokens of tasks (transcribe, translate), after the
	// language.
	Tasks []int32 `json:"tasks,omitempty"`
}

// Plan is a stub's mappings: how the sound's features are reduced, the
// keys (a table's), its similarity, and each mapping's transcript (then
// the default's), in tokens.
type Plan struct {
	Reducer    *match.Reducer
	Keys       [][]float64
	Similarity float64
	Texts      [][]int32
}

// Tensor is a weight: its shape, and its values, row-major.
type Tensor struct {
	Shape []int
	Data  []float32
}

// Weights are a stub's weights, by their names in Hugging Face's
// WhisperForConditionalGeneration (model.encoder.conv1.weight...).
type Weights map[string]Tensor

// lnEpsilon is Whisper's LayerNorms' epsilon.
const lnEpsilon = 1e-5

// residual allocates a residual stream's dimensions.
type residual struct {
	d, next int
}

func (r *residual) alloc(n int) int {
	at := r.next
	r.next += n
	return at
}

// row is a weight's row: its weights by the residual stream's dimension,
// and its bias.
type row struct {
	w map[int]float64
	b float64
}

// head is an attention head: its query's, key's and value's rows (by the
// head's dimension), and its output's (by the residual stream's dimension,
// of each of the head's).
type head struct {
	q, k, v map[int]row
	o       map[int]map[int]float64 // head dim → residual dim → weight
}

func newHead() *head {
	return &head{q: map[int]row{}, k: map[int]row{}, v: map[int]row{}, o: map[int]map[int]float64{}}
}

func (h *head) out(dim, to int, w float64) {
	if h.o[dim] == nil {
		h.o[dim] = map[int]float64{}
	}
	h.o[dim][to] += w
}

// unit is an MLP's unit: gelu(w · LN(x) + b), its output out.
type unit struct {
	in  row
	out map[int]float64
}

// norm is a LayerNorm's weight and bias, by dimension (1 and 0 where they
// are not given).
type norm struct {
	weight, bias map[int]float64
}

// layer is a transformer layer being built: its heads (self-attention's,
// and a decoder's cross-attention's), its MLP's units, its LayerNorms and
// its outputs' biases.
type layer struct {
	self, cross            []*head
	units                  []unit
	selfLN, crossLN, mlpLN norm
	// selfBias, crossBias and outBias are the attentions' and the MLP's
	// outputs' biases.
	selfBias, crossBias, outBias map[int]float64
	// sink is the dimension each output's sum is taken out on (−1: none),
	// so that the residual stream's sum stays 0 and its LayerNorms' mean
	// with it: they read values as they are.
	sink int
}

func newLayer(heads int) *layer {
	l := &layer{selfBias: map[int]float64{}, crossBias: map[int]float64{}, outBias: map[int]float64{}, sink: -1}
	for range heads {
		l.self = append(l.self, newHead())
		l.cross = append(l.cross, newHead())
	}
	return l
}

// stepWidth is how far a step's two units' inputs are apart: its output
// is their GELUs' difference over it.
const stepWidth = 32

// step adds a step to the layer's MLP: in is its first unit's input, its
// second's that less stepWidth; out is added to when both are over 10
// (exactly: their GELUs' difference is stepWidth there), nothing when both
// are under −10.
func (l *layer) step(in row, out map[int]float64) {
	up, down := map[int]float64{}, map[int]float64{}
	for d, x := range out {
		up[d], down[d] = x/stepWidth, -x/stepWidth
	}
	l.units = append(l.units, unit{in: in, out: up}, unit{in: row{w: in.w, b: in.b - stepWidth}, out: down})
}

// identity adds units that add the dimension's LayerNorm value, times w,
// to the residual stream's dimensions out (GELU(x) − GELU(−x) is x).
func (l *layer) identity(in row, out map[int]float64) {
	neg := row{w: map[int]float64{}, b: -in.b}
	for d, x := range in.w {
		neg.w[d] = -x
	}
	minus := map[int]float64{}
	for d, x := range out {
		minus[d] = -x
	}
	l.units = append(l.units, unit{in: in, out: out}, unit{in: neg, out: minus})
}

// ln is a LayerNorm's read of a residual stream of a constant part: its
// mean, and the scale it divides by (the constant part outweighs the rest,
// so that it is a constant).
type ln struct {
	mean, scale float64
}

// lnOf is the LayerNorm's mean and scale of a residual stream of the
// values.
func lnOf(d int, values map[int]float64) ln {
	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(d)
	var sq float64
	for i := range d {
		x := values[i] - mean
		sq += x * x
	}
	return ln{mean: mean, scale: math.Sqrt(sq/float64(d) + lnEpsilon)}
}

// linear is the weights of a LayerNorm that a ballast makes linear: the
// scale it divides by on every dimension (so that it reads values as they
// are, less the mean), 0 on the ballast's.
func linear(d int, n ln, ballast ...int) norm {
	w := map[int]float64{}
	for i := range d {
		w[i] = n.scale
	}
	for _, b := range ballast {
		w[b] = 0
	}
	return norm{weight: w}
}

// masked is a LayerNorm's weights of the dimensions given, 0 on every
// other: its outputs are those dimensions' alone.
func masked(d int, weights map[int]float64) norm {
	w := map[int]float64{}
	for i := range d {
		w[i] = weights[i]
	}
	return norm{weight: w}
}

// maskedLinear is the weights of a LayerNorm that a ballast makes linear,
// of the dimensions a sublayer reads alone: an 8-bit runtime rounds each
// position's outputs to a 127th of the largest, so that a sublayer's
// outputs are of what it reads.
func maskedLinear(d int, n ln, expose []int) norm {
	w := map[int]float64{}
	for _, i := range expose {
		w[i] = n.scale
	}
	return masked(d, w)
}

// compensate gives a row's weight on the dimension that is always 0 the
// negative of its others' sum: of a LayerNorm's output, its row is then of
// the values less nothing of their mean.
func compensate(r row, zero int) row {
	var sum float64
	for d, w := range r.w {
		if d != zero {
			sum += w
		}
	}
	out := row{w: map[int]float64{}, b: r.b}
	for d, w := range r.w {
		out.w[d] = w
	}
	if sum != 0 {
		out.w[zero] = -sum
	}
	return out
}

// errTooMany is a stub's of more mappings, or longer sounds, than the
// model's sizes hold.
var errTooMany = errors.New("too large")

func tooMany(f string, args ...any) error {
	return fmt.Errorf("%w: %s", errTooMany, fmt.Sprintf(f, args...))
}
