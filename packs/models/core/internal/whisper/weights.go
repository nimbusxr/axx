package whisper

import (
	"fmt"
	"maps"
	"math"
	"slices"
)

// Compile compiles a speech recognizer's stub into the weights of a
// Whisper of the sizes.
func Compile(d Dims, tok Tokens, p *Plan) (Weights, error) {
	if len(p.Texts) != len(p.Keys)+1 {
		return nil, fmt.Errorf("a stub has a transcript for each mapping and the default")
	}
	if d.D%d.Heads != 0 {
		return nil, fmt.Errorf("its width %d is not its heads' (%d) multiple", d.D, d.Heads)
	}
	e, err := compileEncoder(d, p)
	if err != nil {
		return nil, err
	}
	dec, err := compileDecoder(d, tok, p, e)
	if err != nil {
		return nil, err
	}
	w := Weights{}
	f32 := func(name string, shape []int, v []float64) {
		data := make([]float32, len(v))
		for i, x := range v {
			data[i] = float32(x)
		}
		w[name] = Tensor{Shape: shape, Data: data}
	}
	D := d.D
	f32("model.encoder.conv1.weight", []int{D, d.Mels, 3}, e.conv1)
	f32("model.encoder.conv1.bias", []int{D}, make([]float64, D))
	f32("model.encoder.conv2.weight", []int{D, D, 3}, e.conv2)
	f32("model.encoder.conv2.bias", []int{D}, make([]float64, D))
	f32("model.encoder.embed_positions.weight", []int{d.AudioPositions, D}, flatten(e.pos))
	for i, l := range e.layers {
		if err := l.write(w, fmt.Sprintf("model.encoder.layers.%d.", i), d, d.EncoderFFN, false); err != nil {
			return nil, fmt.Errorf("its encoder's layer %d: %w", i, err)
		}
	}
	post := e.post()
	f32("model.encoder.layer_norm.weight", []int{D}, post.weights(D))
	f32("model.encoder.layer_norm.bias", []int{D}, post.biases(D))

	embed := make([]float64, d.Vocab*D)
	for t, row := range dec.embed {
		for c, x := range row {
			embed[int(t)*D+c] = x
		}
	}
	f32("model.decoder.embed_tokens.weight", []int{d.Vocab, D}, embed)
	f32("model.decoder.embed_positions.weight", []int{d.TextPositions, D}, flatten(dec.posEmb))
	for i, l := range dec.layers {
		if err := l.write(w, fmt.Sprintf("model.decoder.layers.%d.", i), d, d.DecoderFFN, true); err != nil {
			return nil, fmt.Errorf("its decoder's layer %d: %w", i, err)
		}
	}
	f32("model.decoder.layer_norm.weight", []int{D}, dec.finalWeight)
	f32("model.decoder.layer_norm.bias", []int{D}, dec.finalBias)
	if err := pairResidual(w, d); err != nil {
		return nil, err
	}
	return w, nil
}

// sinkRow sets a weight's row of the sink (of rows outputs, of cols inputs)
// to the negative of the others' sum, each input's: what the others add,
// it takes out. Its caller sets the bias's.
func sinkRow(w []float64, rows, cols, sink int) {
	for c := range cols {
		var sum float64
		for r := range rows {
			if r != sink {
				sum += w[r*cols+c]
			}
		}
		w[sink*cols+c] = -sum
	}
}

func flatten(rows [][]float64) []float64 {
	var out []float64
	for _, r := range rows {
		out = append(out, r...)
	}
	return out
}

// weights are a LayerNorm's weights, of every dimension (1 where they
// are not given).
func (n norm) weights(d int) []float64 {
	out := make([]float64, d)
	for i := range out {
		out[i] = 1
		if x, ok := n.weight[i]; ok {
			out[i] = x
		}
	}
	return out
}

// biases are a LayerNorm's biases, of every dimension.
func (n norm) biases(d int) []float64 {
	out := make([]float64, d)
	for i, x := range n.bias {
		out[i] = x
	}
	return out
}

// dense is a bias's values, of every dimension.
func dense(d int, m map[int]float64) []float64 {
	out := make([]float64, d)
	for i, x := range m {
		out[i] = x
	}
	return out
}

// write writes a layer's weights: its heads' projections (a query's
// scaled by the root of the heads' size, which the attention divides by),
// its MLP's, its LayerNorms'.
func (l *layer) write(w Weights, prefix string, d Dims, ffn int, decoder bool) error {
	D, hd := d.D, d.D/d.Heads
	put := func(name string, shape []int, v []float64) {
		data := make([]float32, len(v))
		for i, x := range v {
			data[i] = float32(x)
		}
		w[prefix+name] = Tensor{Shape: shape, Data: data}
	}
	attention := func(name string, heads []*head, bias map[int]float64) error {
		q, qb := make([]float64, D*D), make([]float64, D)
		k := make([]float64, D*D)
		v, vb := make([]float64, D*D), make([]float64, D)
		o := make([]float64, D*D)
		scale := math.Sqrt(float64(hd))
		for h, hh := range heads {
			for i, r := range hh.q {
				if i >= hd {
					return fmt.Errorf("a head of %d dimensions, not %d", i+1, hd)
				}
				for c, x := range r.w {
					q[(h*hd+i)*D+c] = x * scale
				}
				qb[h*hd+i] = r.b * scale
			}
			for i, r := range hh.k {
				if r.b != 0 {
					return fmt.Errorf("a key of a bias (Whisper's keys have none)")
				}
				for c, x := range r.w {
					k[(h*hd+i)*D+c] = x
				}
			}
			// A value's dimension of the head: a pair's first, or (of a
			// head of values never negative, negated) its own.
			at, sign := func(i int) int { return 2 * i }, 1.0
			if hh.nonNegative {
				at, sign = func(i int) int { return i }, -1
			}
			for i, r := range hh.v {
				if at(i) >= hd {
					return fmt.Errorf("a head of %d values, of %d dimensions", i+1, hd)
				}
				for c, x := range r.w {
					v[(h*hd+at(i))*D+c] = sign * x
				}
				vb[h*hd+at(i)] = sign * r.b
			}
			for i, outs := range hh.o {
				for c, x := range outs {
					o[c*D+h*hd+at(i)] += float64(sign * x)
				}
			}
		}
		if l.sink >= 0 {
			sinkRow(o, D, D, l.sink)
		}
		put(name+".q_proj.weight", []int{D, D}, q)
		put(name+".q_proj.bias", []int{D}, qb)
		put(name+".k_proj.weight", []int{D, D}, k)
		put(name+".v_proj.weight", []int{D, D}, v)
		put(name+".v_proj.bias", []int{D}, vb)
		ob := dense(D, bias)
		if l.sink >= 0 {
			ob[l.sink] = 0
			for _, c := range slices.Sorted(maps.Keys(bias)) {
				if c != l.sink {
					ob[l.sink] -= bias[c]
				}
			}
		}
		put(name+".out_proj.weight", []int{D, D}, o)
		put(name+".out_proj.bias", []int{D}, ob)
		return nil
	}
	if err := attention("self_attn", l.self, l.selfBias); err != nil {
		return err
	}
	put("self_attn_layer_norm.weight", []int{D}, l.selfLN.weights(D))
	put("self_attn_layer_norm.bias", []int{D}, l.selfLN.biases(D))
	if decoder {
		if err := attention("encoder_attn", l.cross, l.crossBias); err != nil {
			return err
		}
		put("encoder_attn_layer_norm.weight", []int{D}, l.crossLN.weights(D))
		put("encoder_attn_layer_norm.bias", []int{D}, l.crossLN.biases(D))
	}
	if l.slots() > ffn {
		return fmt.Errorf("units of %d of its MLP's %d dimensions", l.slots(), ffn)
	}
	fc1, fc1b := make([]float64, ffn*D), make([]float64, ffn)
	fc2, fc2b := make([]float64, D*ffn), make([]float64, D)
	at, next := 0, 0
	for _, un := range l.units {
		if un.opposite {
			at++
		} else {
			at, next = next, next+2
		}
		for c, x := range un.in.w {
			fc1[at*D+c] = x
		}
		fc1b[at] = un.in.b
		for c, x := range un.out {
			fc2[c*ffn+at] += x
		}
	}
	for c, x := range l.outBias {
		fc2b[c] = x
	}
	if l.sink >= 0 {
		sinkRow(fc2, D, ffn, l.sink)
		fc2b[l.sink] = 0
		for _, c := range slices.Sorted(maps.Keys(l.outBias)) {
			if c != l.sink {
				fc2b[l.sink] -= l.outBias[c]
			}
		}
	}
	put("fc1.weight", []int{ffn, D}, fc1)
	put("fc1.bias", []int{ffn}, fc1b)
	put("fc2.weight", []int{D, ffn}, fc2)
	put("fc2.bias", []int{D}, fc2b)
	put("final_layer_norm.weight", []int{D}, l.mlpLN.weights(D))
	put("final_layer_norm.bias", []int{D}, l.mlpLN.biases(D))
	return nil
}
