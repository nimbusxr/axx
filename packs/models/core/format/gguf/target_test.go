package gguf

import (
	"math"
	"slices"
	"testing"
)

// smolTarget is SmolLM2-135M's architecture, as MLC compiles it: a width of
// 576, 9 query heads of 64 dimensions sharing 3 key-value heads, an MLP of
// 1536, 30 layers, and rotary pairs of a base of 100000.
func smolTarget() Target {
	t := Target{E: 576, Heads: 9, KVHeads: 3, HeadDim: 64, FFN: 1536, Layers: 30}
	for i := range t.HeadDim / 2 {
		t.Thetas = append(t.Thetas, math.Pow(100000, -2*float64(i)/float64(t.HeadDim)))
	}
	return t
}

// llamaForward runs a stub laid out in a target as llama's architecture
// does, in float64: each query head of the key-value head of its group,
// each pair of a head's dimensions (2i, 2i+1) turned by position × its
// angle. Its matrices are sparse, its layers of no weights skipped.
type llamaForward struct{ w *Laid }

func mul(m *Matrix, x []float64) []float64 {
	out := make([]float64, m.Rows)
	for r := range m.Rows {
		for _, e := range m.Entries(r) {
			out[r] += float64(e.V) * x[e.Col]
		}
	}
	return out
}

// logits are the last position's logits.
func (f llamaForward) logits(tokens []int32) []float64 {
	w := f.w
	E, H, HD, kvh := w.E, w.Heads, w.HeadDim, w.KVHeads
	group := H / kvh
	n := len(tokens)
	x := make([][]float64, n)
	for i, t := range tokens {
		x[i] = make([]float64, E)
		for _, e := range w.Embeddings.Entries(int(t)) {
			x[i][e.Col] = float64(e.V)
		}
	}
	norm := func(v []float64, g []float32) []float64 {
		var s float64
		for _, a := range v {
			s += a * a
		}
		r := make([]float64, len(v))
		for i, a := range v {
			r[i] = a / math.Sqrt(s/float64(len(v))+1e-5) * float64(g[i])
		}
		return r
	}
	rope := func(v []float64, heads, pos int) {
		for h := range heads {
			for p := range HD / 2 {
				a := float64(pos) * w.Thetas[p]
				c, s := math.Cos(a), math.Sin(a)
				i, j := h*HD+2*p, h*HD+2*p+1
				v[i], v[j] = v[i]*c-v[j]*s, v[i]*s+v[j]*c
			}
		}
	}
	empty := func(m *Matrix) bool {
		for r := range m.Rows {
			if !m.Empty(r) {
				return false
			}
		}
		return true
	}
	for l := range w.Layers {
		if !empty(w.O[l]) {
			var qs, ks, vs [][]float64
			for i := range n {
				h := norm(x[i], w.Norm)
				qi, ki := mul(w.Q[l], h), mul(w.K[l], h)
				rope(qi, H, i)
				rope(ki, kvh, i)
				qs, ks, vs = append(qs, qi), append(ks, ki), append(vs, mul(w.V[l], h))
			}
			for i := range n {
				att := make([]float64, H*HD)
				for h := range H {
					kv := h / group
					scores := make([]float64, i+1)
					top := math.Inf(-1)
					for j := range i + 1 {
						var s float64
						for d := range HD {
							s += qs[i][h*HD+d] * ks[j][kv*HD+d]
						}
						scores[j] = s / math.Sqrt(float64(HD))
						top = max(top, scores[j])
					}
					var sum float64
					for j := range scores {
						scores[j] = math.Exp(scores[j] - top)
						sum += scores[j]
					}
					for j := range scores {
						for d := range HD {
							att[h*HD+d] += scores[j] / sum * vs[j][kv*HD+d]
						}
					}
				}
				for d, a := range mul(w.O[l], att) {
					x[i][d] += a
				}
			}
		}
		if !empty(w.Down[l]) {
			for i := range n {
				h := norm(x[i], w.Norm)
				g, u := mul(w.Gate[l], h), mul(w.Up[l], h)
				for j := range g {
					g[j] = g[j] / (1 + math.Exp(-g[j])) * u[j]
				}
				for k, d := range mul(w.Down[l], g) {
					x[i][k] += d
				}
			}
		}
	}
	h := norm(x[n-1], w.OutputNorm)
	out := make([]float64, w.Vocab)
	for t := range w.Vocab {
		for _, e := range w.Embeddings.Entries(t) {
			out[t] += float64(e.V) * h[e.Col]
		}
	}
	return out
}

// generate generates greedily, to the end of generation or n tokens, and
// the smallest lead of a token over the next.
func (f llamaForward) generate(prompt []int32, eog int32, n int) ([]int32, float64) {
	tokens, lead := slices.Clone(prompt), math.Inf(1)
	var out []int32
	for range n {
		last := f.logits(tokens)
		best, second := 0, -1
		for t := range last {
			if last[t] > last[best] {
				second, best = best, t
			} else if second < 0 || last[t] > last[second] {
				if t != best {
					second = t
				}
			}
		}
		lead = min(lead, last[best]-last[second])
		out = append(out, int32(best))
		if int32(best) == eog {
			break
		}
		tokens = append(tokens, int32(best))
	}
	return out, lead
}

// A stub laid out in SmolLM2-135M's architecture (MLC's: its heads sharing
// key-value heads, its rotary angles fixed) answers its mappings as the stub
// does: its positional heads tell the offsets apart over its context.
func TestAStubOfATargetAnswersItsMappings(t *testing.T) {
	p := testPlan()
	w, err := CompileFor(p, smolTarget())
	if err != nil {
		t.Fatal(err)
	}
	f := llamaForward{w}
	cases := []struct {
		name   string
		prompt []int32
		want   []int32
	}{
		{"the first key, after a space", chat(50, 51, 100, 101, 102, 103, 104, 52), p.Answers[0]},
		{"the first key, at the start", chat(110, 101, 102, 103, 104), p.Answers[0]},
		{"the second, in two tokens", chat(60, 120, 121, 62), p.Answers[1]},
		{"the second, in one", chat(60, 61, 122), p.Answers[1]},
		{"half the second", chat(120, 60, 61, 62), p.Answers[2]},
		{"both keys: the first wins", chat(122, 100, 101, 102, 103, 104), p.Answers[0]},
		{"a key cut short", chat(100, 101, 102, 104), p.Answers[2]},
		{"nothing", chat(70, 71, 72), p.Answers[2]},
	}
	for _, c := range cases {
		got, lead := f.generate(c.prompt, p.EOG, 12)
		want := append(slices.Clone(c.want), p.EOG)
		if !slices.Equal(got, want) {
			t.Errorf("%s: generated %v, want %v", c.name, got, want)
		}
		if lead < 50 {
			t.Errorf("%s: a token led by %.1f only", c.name, lead)
		}
	}
}

// A target whose slowest pairs turn more than half a circle over its
// context (a long one) has content heads of phases: of each chunk, a head
// scores a flagged position by content or more, whatever its offset.
func TestContentHeadsOfPhasesScoreEveryOffset(t *testing.T) {
	p := testPlan()
	p.Context = 1 << 16
	target := Target{E: 576, Heads: 9, KVHeads: 3, HeadDim: 64, FFN: 1536, Layers: 30}
	for i := range target.HeadDim / 2 {
		target.Thetas = append(target.Thetas, math.Pow(10000, -2*float64(i)/float64(target.HeadDim)))
	}
	w, err := CompileFor(p, target)
	if err != nil {
		t.Fatal(err)
	}
	HD, group := w.HeadDim, w.Heads/w.KVHeads
	// Each content head's query (of the ballast) and its key's feature,
	// by its key's input.
	type head struct{ q, k []float64 }
	byFeature := map[int][]head{}
	for l := range w.Layers {
		for h := range w.Heads {
			kv := h / group
			q, k := make([]float64, HD), make([]float64, HD)
			feature := -1
			for d := range HD {
				q[d] = float64(w.Q[l].At(h*HD+d, 0))
				for _, e := range w.K[l].Entries(kv*HD + d) {
					if e.Col != 0 {
						feature, k[d] = e.Col, float64(e.V)
					}
				}
			}
			if feature >= 0 && slices.ContainsFunc(q, func(v float64) bool { return v != 0 }) {
				// Only this head's pair of the key: the others' channels
				// are other heads'.
				kk := make([]float64, HD)
				for d := range HD {
					if q[d] != 0 || q[d^1] != 0 {
						kk[d] = k[d]
					}
				}
				byFeature[feature] = append(byFeature[feature], head{q, kk})
			}
		}
	}
	if len(byFeature) == 0 {
		t.Fatal("no content heads")
	}
	rot := func(v []float64, pos float64) []float64 {
		out := slices.Clone(v)
		for p := range HD / 2 {
			a := pos * w.Thetas[p]
			c, s := math.Cos(a), math.Sin(a)
			out[2*p], out[2*p+1] = v[2*p]*c-v[2*p+1]*s, v[2*p]*s+v[2*p+1]*c
		}
		return out
	}
	for feature, heads := range byFeature {
		if len(heads) < 2 {
			t.Errorf("the chunk of feature %d has one phase", feature)
		}
		for delta := 0; delta <= p.Context; delta += 97 {
			best := math.Inf(-1)
			for _, h := range heads {
				q, k := rot(h.q, float64(delta)), rot(h.k, 0)
				var s float64
				for d := range HD {
					s += q[d] * k[d]
				}
				best = max(best, s/math.Sqrt(float64(HD)))
			}
			if best < content*0.99 {
				t.Fatalf("the chunk of feature %d scores %.1f at an offset of %d", feature, best, delta)
			}
		}
	}
}
