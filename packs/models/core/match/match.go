// Package match is how a stub matches an input to a mapping's: the input is
// reduced to what is compared, and the nearest key answers, when it is alike
// enough. The format writers build the same computation into each stub;
// Table.Nearest is its reference, which their tests hold them to.
//
// An image is pooled to a grid of at most 16×16 (the mean of each cell),
// and each of its channels standardized: an app scales and shifts a
// picture's channels as its model expects (0 to 255, 0 to 1, a mean and a
// standard deviation for each), which standardizing undoes, so a key made
// from a picture matches the app's input however the app prepares it. Any
// other tensor is standardized as a whole. Two reduced inputs are as alike as
// their correlation, from −1 to 1. A sound's features are pooled, and each
// time's bands centered and scaled to their root mean square, a time whose
// bands vary less than SegmentLevel's left out (what is said, not how loud:
// a time of no sound, or a room's hum, is 0), then the whole scaled to its
// own: their cosine.
package match

import (
	"errors"
	"fmt"
	"math"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// Grid is the largest side of the grid an image is pooled to.
const Grid = 16

// Epsilon keeps a channel with no variation at zero when it is
// standardized.
const Epsilon = 1e-12

// SegmentLevel is the mean square of a time's bands' deviations (of a
// sound's log-mel features) a time is heard over: a time of less (the
// padding's silence) is left out. SegmentGain is how steep its step is: a
// time 1/SegmentGain over it counts as a whole.
const (
	SegmentLevel = 0.01
	SegmentGain  = 1 << 20
)

// Reducer reduces an input of a contract to what matching compares.
type Reducer struct {
	// Image says the input is an image, pooled to PH×PW with KH×KW
	// kernels, its C channels standardized each; Audio says it is a
	// sound's N samples, reduced to the log of its power in Bands bands,
	// over frames of Frame samples, standardized (Spectrum); otherwise its
	// N values are standardized together.
	Image bool
	Audio bool
	// Features says it is a sound's log-mel spectrogram, [H mels, W
	// frames], pooled as a picture of one channel is, then each time's
	// bands centered and scaled to their root mean square (a time under
	// SegmentLevel's left out), then the whole scaled to its own: what is
	// said, not how loud.
	Features bool
	// Parakeet says the input is Parakeet/NeMo log-mel features, and Reduce
	// returns their normalized whole-utterance direction instead of a time
	// sequence.
	Parakeet bool
	C, H, W  int
	KH, KW   int
	PH, PW   int
	N        int
	Frame    int
	Rate     int
	// layout is where the channels, rows and columns are in an item's
	// dimensions: an index each, of 0, 1 and 2.
	layout [3]int
}

// NewReducer is the reducer of the input t.
func NewReducer(t *contract.Tensor) (*Reducer, error) {
	item := t.Shape
	if t.Batched() {
		item = item[1:]
	}
	if t.Image == nil {
		n := 1
		for _, d := range item {
			if !d.Fixed() {
				return nil, fmt.Errorf("input %q has an open dimension (%s): a stub matches inputs of a fixed size", t.Name, d)
			}
			n *= int(d.Size)
		}
		if t.Audio != nil && t.Audio.Features != "" {
			// Of FeatureBands bands, over FeatureSegments times.
			h, w := int(item[0].Size), int(item[1].Size)
			r := &Reducer{Image: true, Features: true, Parakeet: t.Audio.Features == "parakeet", C: 1, H: h, W: w, layout: [3]int{0, 1, 2}}
			if r.Parakeet {
				// A Parakeet stub's own frontend's bands, whatever the real
				// model's: the app gives parakeet.cpp a sound.
				r.C = ParakeetMels
			}
			r.KH, r.KW = max(h/FeatureBands, 1), max(w/FeatureSegments, 1)
			r.PH, r.PW = h/r.KH, w/r.KW
			return r, nil
		}
		if t.Audio != nil {
			frame := audioFrame(n)
			if frame == 0 {
				return nil, fmt.Errorf("audio input %q has %d samples: a stub matches sounds of a number of samples with a divisor from %d to %d", t.Name, n, minFrame, maxFrame)
			}
			return &Reducer{Audio: true, N: n, Frame: frame, Rate: t.Audio.SampleRate}, nil
		}
		return &Reducer{N: n}, nil
	}
	for _, d := range item {
		if !d.Fixed() {
			return nil, fmt.Errorf("image input %q has an open dimension (%s): a stub matches images of a fixed size", t.Name, d)
		}
	}
	ci, hi, wi := t.ImageDims()
	r := &Reducer{Image: true, C: int(t.Shape[ci].Size), H: int(t.Shape[hi].Size), W: int(t.Shape[wi].Size)}
	off := len(t.Shape) - 3
	r.layout = [3]int{ci - off, hi - off, wi - off}
	r.KH, r.KW = max(r.H/min(Grid, r.H), 1), max(r.W/min(Grid, r.W), 1)
	r.PH, r.PW = r.H/r.KH, r.W/r.KW
	return r, nil
}

// A sound's features are pooled to FeatureBands bands of its mels, over
// FeatureSegments times: a tenth of a second's each, of Whisper's 30
// seconds. Fine times tell a speaker's phrases apart; coarse ones, which
// the padding's silence after a phrase fills alike, do not.
const (
	FeatureBands    = 16
	FeatureSegments = 300
)

// Dims is how many values a reduced input has.
func (r *Reducer) Dims() int {
	if r.Parakeet {
		return r.C
	}
	switch {
	case r.Image:
		return r.C * r.PH * r.PW
	case r.Audio:
		return Bands
	}
	return r.N
}

// ItemSize is how many values an input has, without its batch.
func (r *Reducer) ItemSize() int {
	if r.Image {
		return r.C * r.H * r.W
	}
	return r.N
}

// Reduce reduces an input's values, without its batch, in its own layout.
func (r *Reducer) Reduce(item []float64) ([]float64, error) {
	if r.Parakeet {
		return reduceParakeet(item, r.C)
	}
	if len(item) != r.ItemSize() {
		return nil, fmt.Errorf("an input of %d values, not %d", len(item), r.ItemSize())
	}
	if r.Audio {
		out := r.Spectrum(item)
		standardize(out)
		return out, nil
	}
	if !r.Image {
		out := append([]float64(nil), item...)
		standardize(out)
		return out, nil
	}
	dims := [3]int{}
	dims[r.layout[0]], dims[r.layout[1]], dims[r.layout[2]] = r.C, r.H, r.W
	at := func(c, y, x int) float64 {
		idx := [3]int{}
		idx[r.layout[0]], idx[r.layout[1]], idx[r.layout[2]] = c, y, x
		return item[(idx[0]*dims[1]+idx[1])*dims[2]+idx[2]]
	}
	out := make([]float64, r.Dims())
	cell := float64(r.KH * r.KW)
	if r.Features {
		r.pool(out, at, cell, 0)
		for px := range r.PW {
			var mean float64
			for py := range r.PH {
				mean += out[py*r.PW+px]
			}
			mean /= float64(r.PH)
			var square float64
			for py := range r.PH {
				out[py*r.PW+px] -= mean
				square += float64(out[py*r.PW+px] * out[py*r.PW+px])
			}
			square /= float64(r.PH)
			heard := Heard(square)
			scale := math.Sqrt(math.Max(square, SegmentLevel))
			for py := range r.PH {
				out[py*r.PW+px] = float64(out[py*r.PW+px]*heard) / scale
			}
		}
		var square float64
		for _, v := range out {
			square += float64(v * v)
		}
		scale := math.Sqrt(square/float64(len(out)) + Epsilon)
		for i := range out {
			out[i] /= scale
		}
		return out, nil
	}
	for c := range r.C {
		r.pool(out, at, cell, c)
		standardize(out[c*r.PH*r.PW : (c+1)*r.PH*r.PW])
	}
	return out, nil
}

// pool averages channel c's KH×KW cells into out's PH×PW of the channel.
func (r *Reducer) pool(out []float64, at func(c, y, x int) float64, cell float64, c int) {
	ch := out[c*r.PH*r.PW : (c+1)*r.PH*r.PW]
	for py := range r.PH {
		for px := range r.PW {
			var sum float64
			for y := py * r.KH; y < (py+1)*r.KH; y++ {
				for x := px * r.KW; x < (px+1)*r.KW; x++ {
					sum += at(c, y, x)
				}
			}
			ch[py*r.PW+px] = sum / cell
		}
	}
}

// Heard is how much a time of a sound's features counts, of the mean square
// of its bands' deviations: 0 under SegmentLevel, 1 over it (by
// 1/SegmentGain), a step every format's stub takes as it is.
func Heard(square float64) float64 {
	return math.Min(math.Max((square-SegmentLevel)*SegmentGain, 0), 1)
}

// standardize makes v's mean 0 and its variance 1 (or keeps it at 0, when it
// does not vary). A product is converted before it is added, so that no
// machine fuses the two (Go's specification lets arm64's FMA round once
// where amd64 rounds twice): every machine reduces an input to the same bits,
// and builds the same stub.
func standardize(v []float64) {
	var mean float64
	for _, x := range v {
		mean += x
	}
	mean /= float64(len(v))
	var variance float64
	for i, x := range v {
		v[i] = x - mean
		variance += float64(v[i] * v[i])
	}
	sd := math.Sqrt(variance/float64(len(v)) + Epsilon)
	for i := range v {
		v[i] /= sd
	}
}

// ErrNoVariation: a key whose reduced form is all zeros, which nothing
// matches.
var ErrNoVariation = errors.New("it does not vary: a picture of one color, or a tensor of one value, is like no input")

// Table is the keys of a stub's mappings, reduced, and how alike an input must
// be to one of them.
type Table struct {
	Keys       [][]float64
	Similarity float64
}

// Nearest is the index of the key nearest to a reduced input, or len(Keys),
// the default's, when none is as alike as the table's similarity. Of keys
// equally near, the first answers, as it does when its similarity is the
// threshold's.
func (t *Table) Nearest(v []float64) int {
	best, at := t.Similarity, len(t.Keys)
	for i := len(t.Keys) - 1; i >= 0; i-- {
		if s := Similarity(v, t.Keys[i]); s >= best {
			best, at = s, i
		}
	}
	return at
}

// Similarity is the correlation of two reduced inputs.
func Similarity(a, b []float64) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i] * b[i])
	}
	return dot / float64(len(a))
}
