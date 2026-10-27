package keras

import (
	"fmt"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/hdf5"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/tokenize"
)

// tieOffset is how much less each later mapping's score is, so that of
// equal scores the first's is the greatest; hotGain how steep the one-hot
// of the greatest is: a score 1/hotGain under it is 0.
const (
	tieOffset = 1.0 / (1 << 20)
	hotGain   = 1 << 24
)

// logScale scales a sound's bands' mean powers before their log, which
// STFTSpectrogram takes of a value's square root over Keras's epsilon
// (1e-7): so that the least, match.LogEpsilon's, is over it. Their logs are
// each log(logScale) more, which standardizing takes out.
const logScale = 1 << 22

// stubModel is the stub's model: its inputs the contract's, its matched
// input reduced as matching reduces it, its mapping's one-hot, and each
// output the answers' table's product with it.
func stubModel(s *stub.Stub, name string, dialect int) (*model, error) {
	c := s.Contract
	var reserved []string
	for _, t := range append(append([]contract.Tensor(nil), c.Inputs...), c.Outputs...) {
		reserved = append(reserved, t.Name)
	}
	m := newModel(name, reserved...)
	inputs := map[string]*layer{}
	for i := range c.Inputs {
		in := &c.Inputs[i]
		shape, err := itemShape(in, true)
		if err != nil {
			return nil, err
		}
		dtype := string(in.Type)
		if dtype == "" {
			dtype = "float32"
		}
		inputs[in.Name] = m.input(in.Name, dtype, shape)
	}
	var hot *layer
	var err error
	switch s.Matcher {
	case stub.NearestMatcher:
		in := c.MatchedInput()
		hot = nearest(m, s, in, inputs[in.Name], dialect)
	case stub.TextMatcher:
		hot, err = texts(m, s, inputs)
	default:
		err = fmt.Errorf("its mappings are matched by %s, which Keras's stubs do not match by", s.Matcher)
	}
	if err != nil {
		return nil, err
	}
	n := len(s.Answers)
	for _, out := range c.Outputs {
		shape, err := itemShape(&out, false)
		if err != nil {
			return nil, err
		}
		if out.Type != "" && out.Type != "float32" {
			return nil, fmt.Errorf("its output %s is %s: Keras's outputs are float32", out.Name, out.Type)
		}
		size := 1
		for _, d := range shape {
			size *= d
		}
		table := make([]float32, n*size)
		for k, a := range s.Answers {
			vs := a[out.Name]
			if len(vs) != size {
				return nil, fmt.Errorf("the answer of %s gives output %s %d values, not %d", s.Names[k], out.Name, len(vs), size)
			}
			for i, v := range vs {
				table[k*size+i] = float32(v)
			}
		}
		y := m.add("Dense", dense(size, false, ""), []int{size}, []weight{{"kernel", hdf5.Float32s([]uint64{uint64(n), uint64(size)}, table)}}, hot)
		m.outputs = append(m.outputs, m.named(out.Name, "Reshape", map[string]any{"target_shape": shape}, shape, nil, y))
	}
	return m, nil
}

// itemShape is a tensor's shape without its batch: of an input, its open
// dimensions −1, of an output, none open.
func itemShape(t *contract.Tensor, input bool) ([]int, error) {
	item := t.Shape
	if t.Batched() {
		item = item[1:]
	}
	shape := make([]int, len(item))
	for i, d := range item {
		switch {
		case d.Fixed():
			shape[i] = int(d.Size)
		case input:
			shape[i] = -1
		default:
			return nil, fmt.Errorf("its output %s has an open dimension (%s): a stub's outputs are of fixed shapes", t.Name, d)
		}
	}
	return shape, nil
}

func dense(units int, bias bool, activation string) map[string]any {
	if activation == "" {
		activation = "linear"
	}
	return map[string]any{"units": units, "activation": activation, "use_bias": bias}
}

func f32s(vs []float64) []float32 {
	out := make([]float32, len(vs))
	for i, v := range vs {
		out[i] = float32(v)
	}
	return out
}

// nearest adds the layers that reduce the input x as the stub's reducer
// does, score it against each mapping's key and the default's similarity,
// and give the best's one-hot.
func nearest(m *model, s *stub.Stub, in *contract.Tensor, x *layer, dialect int) *layer {
	r := s.Nearest.Reducer
	if in.Type != "float32" {
		x = m.add("Rescaling", map[string]any{"scale": 1.0, "offset": 0.0}, x.shape, nil, x)
	}
	var channels, cells int
	switch {
	case r.Features:
		// A sound's features, [mels, frames]: a picture of one channel,
		// each time's bands centered and scaled to their root mean square
		// (a LayerNormalization of each time's), a time under the level
		// left out (match.Heard: a step, of ReLU's clip), then the whole
		// scaled to its own (a LayerNormalization of all).
		x = m.add("Reshape", map[string]any{"target_shape": []int{r.H, r.W, 1}}, []int{r.H, r.W, 1}, nil, x)
		x = m.add("AveragePooling2D", pooling(r, "channels_last"), []int{r.PH, r.PW, 1}, nil, x)
		x = m.add("Reshape", map[string]any{"target_shape": []int{r.PH, r.PW}}, []int{r.PH, r.PW}, nil, x)
		x = m.add("Permute", map[string]any{"dims": []int{2, 1}}, []int{r.PW, r.PH}, nil, x)
		center := make([]float32, r.PH*r.PH)
		mean, ones := make([]float32, r.PH), make([]float32, r.PH)
		for i := range r.PH {
			mean[i], ones[i] = float32(1/float64(r.PH)), 1
			for j := range r.PH {
				center[i*r.PH+j] = -mean[i]
				if i == j {
					center[i*r.PH+j] += 1
				}
			}
		}
		centered := m.add("Dense", dense(r.PH, false, ""), []int{r.PW, r.PH}, []weight{{"kernel", hdf5.Float32s([]uint64{uint64(r.PH), uint64(r.PH)}, center)}}, x)
		square := m.add("Dense", dense(1, false, ""), []int{r.PW, 1}, []weight{{"kernel", hdf5.Float32s([]uint64{uint64(r.PH), 1}, mean)}},
			m.add("Multiply", map[string]any{}, []int{r.PW, r.PH}, nil, centered, centered))
		heard := m.add("ReLU", map[string]any{"max_value": 1.0, "negative_slope": 0.0, "threshold": 0.0}, []int{r.PW, 1}, nil,
			m.add("Rescaling", map[string]any{"scale": float64(match.SegmentGain), "offset": -match.SegmentLevel * match.SegmentGain}, []int{r.PW, 1}, nil, square))
		heard = m.add("Dense", dense(r.PH, false, ""), []int{r.PW, r.PH}, []weight{{"kernel", hdf5.Float32s([]uint64{1, uint64(r.PH)}, ones)}}, heard)
		unit := m.add("LayerNormalization", map[string]any{"axis": []int{-1}, "epsilon": match.Epsilon, "center": false, "scale": false}, []int{r.PW, r.PH}, nil, x)
		x = m.add("Multiply", map[string]any{}, []int{r.PW, r.PH}, nil, unit, heard)
		x = m.add("Permute", map[string]any{"dims": []int{2, 1}}, []int{r.PH, r.PW}, nil, x)
		x = m.add("Reshape", map[string]any{"target_shape": []int{1, r.PH * r.PW}}, []int{1, r.PH * r.PW}, nil, x)
		x = m.add("LayerNormalization", map[string]any{"axis": []int{-1}, "epsilon": match.Epsilon, "center": false, "scale": false}, []int{1, r.PH * r.PW}, nil, x)
		x = m.add("Flatten", map[string]any{"data_format": "channels_last"}, []int{r.PH * r.PW}, nil, x)
		return scores(m, s, x)
	case r.Image:
		// Pooled with its channels last, which every Keras pools on CPUs
		// (Keras 2's TensorFlow pools nothing else there).
		if in.Image.Layout != "NHWC" && in.Image.Layout != "HWC" {
			x = m.add("Permute", map[string]any{"dims": []int{2, 3, 1}}, []int{r.H, r.W, r.C}, nil, x)
		}
		x = m.add("AveragePooling2D", pooling(r, "channels_last"), []int{r.PH, r.PW, r.C}, nil, x)
		x = m.add("Permute", map[string]any{"dims": []int{3, 1, 2}}, []int{r.C, r.PH, r.PW}, nil, x)
		channels, cells = r.C, r.PH*r.PW
	case r.Audio:
		frames, frame, bins := r.N/r.Frame, r.Frame, r.Bins()
		x = m.add("Reshape", map[string]any{"target_shape": []int{frames, frame}}, []int{frames, frame}, nil, x)
		cos, sin := r.Fourier()
		basis := func(t []float64) weight {
			return weight{"kernel", hdf5.Float32s([]uint64{uint64(frame), uint64(bins)}, f32s(t))}
		}
		re := m.add("Dense", dense(bins, false, ""), []int{frames, bins}, []weight{basis(cos)}, x)
		im := m.add("Dense", dense(bins, false, ""), []int{frames, bins}, []weight{basis(sin)}, x)
		power := m.add("Add", map[string]any{}, []int{frames, bins}, nil,
			m.add("Multiply", map[string]any{}, []int{frames, bins}, nil, re, re),
			m.add("Multiply", map[string]any{}, []int{frames, bins}, nil, im, im))
		sums := make([]float32, bins*match.Bands)
		for j := range bins {
			sums[j*match.Bands+r.BandOf(j)] = 1
		}
		bands := m.add("Dense", dense(match.Bands, false, ""), []int{frames, match.Bands}, []weight{{"kernel", hdf5.Float32s([]uint64{uint64(bins), match.Bands}, sums)}}, power)
		mean := m.add("GlobalAveragePooling1D", map[string]any{"data_format": "channels_last", "keepdims": false}, []int{match.Bands}, nil, bands)
		if dialect == keras2 {
			// Keras 2's log: TensorFlow's op, as tf.keras writes a model's
			// call of tf.math.log (a TFOpLambda of its name).
			shifted := m.add("Rescaling", map[string]any{"scale": 1.0, "offset": match.LogEpsilon}, []int{match.Bands}, nil, mean)
			x = m.add("TFOpLambda", map[string]any{"function": "math.log"}, []int{match.Bands}, nil, shifted)
		} else {
			scaled := m.add("Rescaling", map[string]any{"scale": float64(logScale), "offset": logScale * match.LogEpsilon}, []int{match.Bands}, nil, mean)
			column := m.add("Reshape", map[string]any{"target_shape": []int{match.Bands, 1}}, []int{match.Bands, 1}, nil, scaled)
			// Keras 3's: of one sample a frame, a frame's power is the
			// value's square, and STFTSpectrogram's log mode the log of
			// its root.
			x = m.add("STFTSpectrogram", map[string]any{
				"mode": "log", "frame_length": 1, "frame_step": 1, "fft_length": 1, "window": nil, "periodic": false,
				"scaling": nil, "padding": "valid", "data_format": "channels_last", "expand_dims": false,
			}, []int{match.Bands, 1}, []weight{
				{"real_kernel", hdf5.Float32s([]uint64{1, 1, 1}, []float32{1})},
				{"imag_kernel", hdf5.Float32s([]uint64{1, 1, 1}, []float32{0})},
			}, column)
		}
		channels, cells = 1, match.Bands
	default:
		channels, cells = 1, r.N
	}
	// Each channel standardized.
	x = m.add("Reshape", map[string]any{"target_shape": []int{channels, cells}}, []int{channels, cells}, nil, x)
	x = m.add("LayerNormalization", map[string]any{"axis": []int{-1}, "epsilon": match.Epsilon, "center": false, "scale": false}, []int{channels, cells}, nil, x)
	x = m.add("Flatten", map[string]any{"data_format": "channels_last"}, []int{channels * cells}, nil, x)
	return scores(m, s, x)
}

// scores adds the layers that score the reduced input x against each
// mapping's key (x·k / D), the default's the similarity, and give the
// best's one-hot.
func scores(m *model, s *stub.Stub, x *layer) *layer {
	d, n := s.Nearest.Reducer.Dims(), len(s.Nearest.Table.Keys)
	keys := make([]float32, d*(n+1))
	bias := make([]float32, n+1)
	for j := range n + 1 {
		if j < n {
			for i, v := range s.Nearest.Table.Keys[j] {
				keys[i*(n+1)+j] = float32(v / float64(d))
			}
		}
		bias[j] = float32(-float64(j) * tieOffset)
	}
	bias[n] += float32(s.Nearest.Table.Similarity)
	scores := m.add("Dense", dense(n+1, true, ""), []int{n + 1}, []weight{
		{"kernel", hdf5.Float32s([]uint64{uint64(d), uint64(n + 1)}, keys)},
		{"bias", hdf5.Float32s([]uint64{uint64(n + 1)}, bias)},
	}, x)
	return oneHot(m, scores, n+1)
}

// pooling is AveragePooling2D's settings of the reducer's cells.
func pooling(r *match.Reducer, format string) map[string]any {
	return map[string]any{"pool_size": []int{r.KH, r.KW}, "strides": []int{r.KH, r.KW}, "padding": "valid", "data_format": format}
}

// oneHot adds the layers that give the first greatest of n scores' one-hot:
// 1 where a score is the greatest, 0 where it is under it (by more than
// 1/hotGain).
func oneHot(m *model, scores *layer, n int) *layer {
	column := m.add("Reshape", map[string]any{"target_shape": []int{n, 1}}, []int{n, 1}, nil, scores)
	best := m.add("GlobalMaxPooling1D", map[string]any{"data_format": "channels_last", "keepdims": false}, []int{1}, nil, column)
	ones := make([]float32, n)
	for i := range ones {
		ones[i] = 1
	}
	each := m.add("Dense", dense(n, false, ""), []int{n}, []weight{{"kernel", hdf5.Float32s([]uint64{1, uint64(n)}, ones)}}, best)
	under := m.add("Subtract", map[string]any{}, []int{n}, nil, each, scores)
	steep := m.add("Rescaling", map[string]any{"scale": -float64(hotGain), "offset": 1.0}, []int{n}, nil, under)
	return m.add("ReLU", map[string]any{"max_value": 1.0, "negative_slope": 0.0, "threshold": 0.0}, []int{n}, nil, steep)
}

// texts adds the layers that find each mapping's text in the ids (the
// tokens of any of its variants, one after another, the token after them
// not going on the last word) and give the first found's one-hot, or the
// default's: an Embedding of each id's places in the texts' tokens, a
// causal Conv1D that counts each variant's matches ending at a place, and
// its greatest.
func texts(m *model, s *stub.Stub, inputs map[string]*layer) (*layer, error) {
	c := s.Contract
	tok, err := tokenize.Of(c)
	if err != nil {
		return nil, err
	}
	size, ok := tokenize.Size(tok)
	if !ok {
		return nil, fmt.Errorf("its tokenizer does not say its vocabulary's size, which a Keras stub's Embedding takes")
	}
	ids, ok := inputs[c.Text.IDs]
	if !ok {
		return nil, fmt.Errorf("its tokens' input %s is not one of its inputs", c.Text.IDs)
	}
	type variant struct {
		mapping int
		ids     []int32
	}
	var variants []variant
	width := 0
	for k, text := range s.Texts {
		vs, err := tokenize.KeyVariants(tok, text)
		if err != nil {
			return nil, fmt.Errorf("mapping %q: %w", s.Names[k], err)
		}
		for _, v := range vs {
			if len(v) == 0 {
				return nil, fmt.Errorf("mapping %q: the model's tokenizer finds no words in its text", s.Names[k])
			}
			variants = append(variants, variant{k, v})
			width += len(v)
		}
	}
	n := len(s.Texts)
	continues := tok.Continues()
	columns := width
	if continues != nil {
		columns++ // whether the id goes on a word
	}
	kernel := 1
	for _, v := range variants {
		kernel = max(kernel, len(v.ids)+1)
	}
	// Each id's embedding: a 1 in each place of the variants' tokens it
	// is, and whether it goes on a word.
	embeddings := make([]float32, size*max(columns, 1))
	col := 0
	for _, v := range variants {
		for _, id := range v.ids {
			if int(id) >= size {
				return nil, fmt.Errorf("mapping %q: its token %d is past the vocabulary's %d", s.Names[v.mapping], id, size)
			}
			embeddings[int(id)*columns+col] = 1
			col++
		}
	}
	for id, goesOn := range continues {
		if goesOn && id < size {
			embeddings[id*columns+width] = 1
		}
	}
	x := m.add("Embedding", map[string]any{"input_dim": size, "output_dim": max(columns, 1), "mask_zero": false}, []int{ids.shape[0], max(columns, 1)},
		[]weight{{"embeddings", hdf5.Float32s([]uint64{uint64(size), uint64(max(columns, 1))}, embeddings)}}, ids)
	// One place more, of no token, after the ids: the token after a text
	// at their end.
	x = m.add("ZeroPadding1D", map[string]any{"padding": []int{0, 1}}, []int{-1, max(columns, 1)}, nil, x)
	if n == 0 {
		// The default alone.
		zeros := make([]float32, kernel*max(columns, 1))
		conv := m.add("Conv1D", conv1D(1, kernel), []int{-1, 1}, []weight{
			{"kernel", hdf5.Float32s([]uint64{uint64(kernel), uint64(max(columns, 1)), 1}, zeros)},
			{"bias", hdf5.Float32s([]uint64{1}, []float32{1})},
		}, x)
		return m.add("GlobalMaxPooling1D", map[string]any{"data_format": "channels_last", "keepdims": false}, []int{1}, nil, conv), nil
	}
	// Each variant's matches ending before a place: its tokens' places, the
	// last a place before it, less one less than their number, less
	// whether the token at it goes on the word.
	weights := make([]float32, kernel*columns*len(variants))
	bias := make([]float32, len(variants))
	col = 0
	for f, v := range variants {
		l := len(v.ids)
		for i := range v.ids {
			at := kernel - 1 - l + i
			weights[(at*columns+col)*len(variants)+f] = 1
			col++
		}
		if continues != nil {
			weights[((kernel-1)*columns+width)*len(variants)+f] = -1
		}
		bias[f] = float32(-(l - 1))
	}
	found := m.add("Conv1D", conv1D(len(variants), kernel), []int{-1, len(variants)}, []weight{
		{"kernel", hdf5.Float32s([]uint64{uint64(kernel), uint64(columns), uint64(len(variants))}, weights)},
		{"bias", hdf5.Float32s([]uint64{uint64(len(variants))}, bias)},
	}, x)
	has := m.add("GlobalMaxPooling1D", map[string]any{"data_format": "channels_last", "keepdims": false}, []int{len(variants)}, nil, found)
	// Each mapping's: any of its variants'.
	anyOf := make([]float32, len(variants)*n)
	for f, v := range variants {
		anyOf[f*n+v.mapping] = 1
	}
	mapped := m.add("Dense", dense(n, false, ""), []int{n}, []weight{{"kernel", hdf5.Float32s([]uint64{uint64(len(variants)), uint64(n)}, anyOf)}}, has)
	mapped = m.add("ReLU", map[string]any{"max_value": 1.0, "negative_slope": 0.0, "threshold": 0.0}, []int{n}, nil, mapped)
	// The first found: each, less those before it; the default: 1 less all.
	first := make([]float32, n*(n+1))
	firstBias := make([]float32, n+1)
	for k := range n {
		first[k*(n+1)+k] = 1
		for j := k + 1; j <= n; j++ {
			first[k*(n+1)+j] = -1
		}
	}
	firstBias[n] = 1
	return m.add("Dense", dense(n+1, true, "relu"), []int{n + 1}, []weight{
		{"kernel", hdf5.Float32s([]uint64{uint64(n), uint64(n + 1)}, first)},
		{"bias", hdf5.Float32s([]uint64{uint64(n + 1)}, firstBias)},
	}, mapped), nil
}

func conv1D(filters, kernel int) map[string]any {
	return map[string]any{
		"filters": filters, "kernel_size": []int{kernel}, "strides": []int{1}, "padding": "causal",
		"dilation_rate": []int{1}, "groups": 1, "activation": "relu", "use_bias": true, "data_format": "channels_last",
	}
}
