package gguf

import (
	"encoding/binary"
	"math"
	"testing"
)

// forward runs a GGUF model of llama's architecture as llama.cpp does, in
// float64: the reference its stubs are checked against before llama.cpp runs
// them. It returns each position's logits.
type forward struct {
	f                      *File
	E, heads, hd, ffn, voc int
	layers                 int
	eps                    float64
	ff                     []float64
	kvF16                  bool
}

func newForward(t *testing.T, f *File) *forward {
	t.Helper()
	u := func(k string) int {
		v, ok := f.Uint(k)
		if !ok {
			t.Fatalf("no %s", k)
		}
		return int(v)
	}
	fw := &forward{
		f: f, E: u("llama.embedding_length"), heads: u("llama.attention.head_count"), hd: u("llama.attention.key_length"),
		ffn: u("llama.feed_forward_length"), voc: u("llama.vocab_size"), layers: u("llama.block_count"), eps: 1e-5, kvF16: true,
	}
	fw.ff = fw.tensor(t, "rope_freqs.weight")
	return fw
}

func (fw *forward) tensor(t *testing.T, name string) []float64 {
	t.Helper()
	tn := fw.f.Tensor(name)
	if tn == nil {
		t.Fatalf("no tensor %s", name)
	}
	return decode(t, tn)
}

func decode(t *testing.T, tn *Tensor) []float64 {
	t.Helper()
	switch tn.Type {
	case F32:
		out := make([]float64, len(tn.Data)/4)
		for i := range out {
			out[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(tn.Data[4*i:])))
		}
		return out
	case F16:
		out := make([]float64, len(tn.Data)/2)
		for i := range out {
			out[i] = halfToFloat(binary.LittleEndian.Uint16(tn.Data[2*i:]))
		}
		return out
	case Q4_0:
		out := make([]float64, 0, len(tn.Data)/18*32)
		for b := 0; b < len(tn.Data); b += 18 {
			d := halfToFloat(binary.LittleEndian.Uint16(tn.Data[b:]))
			var block [32]float64
			for j := range 16 {
				q := tn.Data[b+2+j]
				block[j] = d * float64(int(q&0xf)-8)
				block[16+j] = d * float64(int(q>>4)-8)
			}
			out = append(out, block[:]...)
		}
		return out
	}
	t.Fatalf("tensor %s of type %s", tn.Name, tn.Type)
	return nil
}

func halfToFloat(h uint16) float64 {
	sign := 1.0
	if h&0x8000 != 0 {
		sign = -1
	}
	exp := int(h>>10) & 0x1f
	mant := float64(h & 0x3ff)
	switch exp {
	case 0:
		return sign * mant * math.Exp2(-24)
	case 31:
		return math.Inf(int(sign))
	}
	return sign * (1 + mant/1024) * math.Exp2(float64(exp-15))
}

func f16Round(v float64) float64 { return halfToFloat(f16(float32(v))) }

// matvec is W·x for W of rows out × in, x rounded to halves, as ggml
// multiplies F16 weights (and Metal any).
func matvec(w []float64, x []float64, out int) []float64 {
	in := len(x)
	x = halves(x)
	y := make([]float64, out)
	for o := range out {
		var s float64
		row := w[o*in : (o+1)*in]
		for i, v := range row {
			if v != 0 {
				s += v * x[i]
			}
		}
		y[o] = s
	}
	return y
}

func halves(x []float64) []float64 {
	out := make([]float64, len(x))
	for i, v := range x {
		out[i] = f16Round(v)
	}
	return out
}

func rmsnorm(x, w []float64, eps float64) []float64 {
	var ss float64
	for _, v := range x {
		ss += v * v
	}
	r := 1 / math.Sqrt(ss/float64(len(x))+eps)
	out := make([]float64, len(x))
	for i, v := range x {
		out[i] = v * r * w[i]
	}
	return out
}

func silu(x float64) float64 { return x / (1 + math.Exp(-x)) }

// rope rotates each head's pairs of v by pos ÷ factor (a base of 1).
func (fw *forward) rope(v []float64, pos int) {
	for h := range fw.heads {
		for p := range fw.hd / 2 {
			theta := float64(float32(float64(pos) / fw.ff[p]))
			c, s := math.Cos(theta), math.Sin(theta)
			i := h*fw.hd + 2*p
			x0, x1 := v[i], v[i+1]
			v[i], v[i+1] = x0*c-x1*s, x0*s+x1*c
		}
	}
}

// run returns the logits of each position of the tokens.
func (fw *forward) run(t *testing.T, tokens []int32) [][]float64 {
	t.Helper()
	E, H, hd := fw.E, fw.heads, fw.hd
	emb := fw.tensor(t, "token_embd.weight")
	xs := make([][]float64, len(tokens))
	for p, tok := range tokens {
		xs[p] = append([]float64(nil), emb[int(tok)*E:(int(tok)+1)*E]...)
	}
	for l := range fw.layers {
		b := "blk." + itoa(l) + "."
		an, wq, wk, wv, wo := fw.tensor(t, b+"attn_norm.weight"), fw.tensor(t, b+"attn_q.weight"), fw.tensor(t, b+"attn_k.weight"), fw.tensor(t, b+"attn_v.weight"), fw.tensor(t, b+"attn_output.weight")
		fn, wg, wu, wd := fw.tensor(t, b+"ffn_norm.weight"), fw.tensor(t, b+"ffn_gate.weight"), fw.tensor(t, b+"ffn_up.weight"), fw.tensor(t, b+"ffn_down.weight")
		qs, ks, vs := make([][]float64, len(xs)), make([][]float64, len(xs)), make([][]float64, len(xs))
		for p, x := range xs {
			h := rmsnorm(x, an, fw.eps)
			qs[p], ks[p], vs[p] = matvec(wq, h, H*hd), matvec(wk, h, H*hd), matvec(wv, h, H*hd)
			fw.rope(qs[p], p)
			fw.rope(ks[p], p)
			if fw.kvF16 {
				for i := range ks[p] {
					ks[p][i], vs[p][i] = f16Round(ks[p][i]), f16Round(vs[p][i])
				}
			}
		}
		scale := 1 / math.Sqrt(float64(hd))
		for p := range xs {
			att := make([]float64, H*hd)
			for h := range H {
				scores := make([]float64, p+1)
				best := math.Inf(-1)
				for n := 0; n <= p; n++ {
					var s float64
					for d := range hd {
						s += qs[p][h*hd+d] * ks[n][h*hd+d]
					}
					scores[n] = s * scale
					best = max(best, scores[n])
				}
				var sum float64
				for n := range scores {
					scores[n] = math.Exp(scores[n] - best)
					sum += scores[n]
				}
				for n := range scores {
					a := scores[n] / sum
					for d := range hd {
						att[h*hd+d] += a * vs[n][h*hd+d]
					}
				}
			}
			o := matvec(wo, att, E)
			for i := range xs[p] {
				xs[p][i] += o[i]
			}
			h := rmsnorm(xs[p], fn, fw.eps)
			g, u := matvec(wg, h, fw.ffn), matvec(wu, h, fw.ffn)
			for i := range g {
				g[i] = silu(g[i]) * u[i]
			}
			d := matvec(wd, g, E)
			for i := range xs[p] {
				xs[p][i] += d[i]
			}
		}
	}
	on := fw.tensor(t, "output_norm.weight")
	logits := make([][]float64, len(xs))
	for p, x := range xs {
		logits[p] = matvec(emb, rmsnorm(x, on, fw.eps), fw.voc)
	}
	return logits
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// generate runs the prompt and greedily generates up to n tokens, to the end
// of generation; it reports the smallest lead of a chosen token's logit over
// the next best.
func (fw *forward) generate(t *testing.T, prompt []int32, eog int32, n int) ([]int32, float64) {
	t.Helper()
	tokens := append([]int32(nil), prompt...)
	var out []int32
	lead := math.Inf(1)
	for range n {
		logits := fw.run(t, tokens)
		last := logits[len(logits)-1]
		best, second := 0, -1
		for i, v := range last {
			if v > last[best] {
				best = i
			}
		}
		for i, v := range last {
			if i != best && (second < 0 || v > last[second]) {
				second = i
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
