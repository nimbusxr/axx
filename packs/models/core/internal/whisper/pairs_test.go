package whisper

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/match"
)

// tiny and base are Whisper tiny's and base's sizes, of many languages.
var (
	tiny = Dims{
		Mels: 80, Vocab: 51865, D: 384, Heads: 6, EncoderLayers: 4, DecoderLayers: 4,
		EncoderFFN: 1536, DecoderFFN: 1536, AudioPositions: 1500, TextPositions: 448,
	}
	base = Dims{
		Mels: 80, Vocab: 51865, D: 512, Heads: 8, EncoderLayers: 6, DecoderLayers: 6,
		EncoderFFN: 2048, DecoderFFN: 2048, AudioPositions: 1500, TextPositions: 448,
	}
)

// plan is a stub's plan of n keys, each of a few tenths' bands, and their
// transcripts of a few words each.
func plan(d Dims, n int) *Plan {
	r := &match.Reducer{
		Features: true, H: d.Mels, W: 2 * d.AudioPositions, KH: d.Mels / match.FeatureBands, KW: 10,
		PH: match.FeatureBands, PW: match.FeatureSegments,
	}
	rng := rand.New(rand.NewPCG(1, uint64(n)))
	p := &Plan{Reducer: r, Similarity: 0.9}
	for k := range n {
		key := make([]float64, r.PH*r.PW)
		for c := range 4 {
			for b := range r.PH {
				key[b*r.PW+c+k%8] = rng.NormFloat64()
			}
		}
		p.Keys = append(p.Keys, key)
	}
	for k := range n + 1 {
		var text []int32
		for i := range 6 {
			text = append(text, int32(1000+50*k+i))
		}
		p.Texts = append(p.Texts, text)
	}
	return p
}

// An 8-bit runtime of x86 adds each pair of a row's products in 16 bits:
// no row's weights are of both of a pair, but a step's or an identity's
// opposite weights (an MLP's), or a head's of values never negative (its
// values' rows' weights of no positive one).
func TestNoPairOfAnInt8RowSaturates(t *testing.T) {
	for _, c := range []struct {
		name string
		d    Dims
		keys int
	}{{"tiny", tiny, 3}, {"tiny", tiny, 24}, {"base", base, 40}} {
		t.Run(fmt.Sprintf("%s, %d keys", c.name, c.keys), func(t *testing.T) {
			d := c.d
			w, err := Compile(d, SpecialTokens(d.Vocab), plan(d, c.keys))
			if err != nil {
				t.Fatal(err)
			}
			enc, dec := streams(d)
			for _, name := range append(enc.read, dec.read...) {
				pairsOf(t, w, name, func(r, i int, a, b float32) {
					t.Errorf("%s: row %d reads both of the pair %d", name, r, i)
				})
			}
			for name := range w {
				switch {
				case strings.HasSuffix(name, "fc2.weight"):
					pairsOf(t, w, name, func(r, i int, a, b float32) {
						if a != -b {
							t.Errorf("%s: row %d reads both of the pair %d, of weights %g and %g", name, r, i, a, b)
						}
					})
				case strings.HasSuffix(name, "out_proj.weight"):
					v := w[strings.TrimSuffix(name, "out_proj.weight")+"v_proj.weight"]
					vb := w[strings.TrimSuffix(name, "out_proj.weight")+"v_proj.bias"]
					pairsOf(t, w, name, func(r, i int, a, b float32) {
						for _, c := range []int{i, i + 1} {
							positive := vb.Data[c] > 0
							for _, x := range v.Data[c*d.D : (c+1)*d.D] {
								positive = positive || x > 0
							}
							if positive {
								t.Errorf("%s: row %d reads both of the pair %d, of a value of a positive weight", name, r, i)
							}
						}
					})
				}
			}
		})
	}
}

// pairsOf calls f for each row's pair (columns i and i+1) of two weights
// that are not 0.
func pairsOf(t *testing.T, w Weights, name string, f func(r, i int, a, b float32)) {
	t.Helper()
	m, ok := w[name]
	if !ok {
		t.Fatalf("no weight %s", name)
	}
	cols := m.Shape[len(m.Shape)-1]
	for r := range len(m.Data) / cols {
		row := m.Data[r*cols : (r+1)*cols]
		for i := 0; i+1 < cols; i += 2 {
			if row[i] != 0 && row[i+1] != 0 {
				f(r, i, row[i], row[i+1])
			}
		}
	}
}
