package whisper

import (
	"fmt"
	"math/bits"
	"slices"
)

// stream is a residual stream's weights: those that read it (a row of its
// dimensions each), those whose columns are of it (that it adds), and those
// whose rows are (that write it: their outputs, biases and LayerNorms).
type stream struct {
	read, columns, rows []string
}

// streams are the encoder's residual stream's weights and the decoder's.
func streams(d Dims) (enc, dec stream) {
	enc.columns = []string{"model.encoder.embed_positions.weight"}
	enc.rows = []string{"model.encoder.conv2.weight", "model.encoder.conv2.bias", "model.encoder.layer_norm.weight", "model.encoder.layer_norm.bias"}
	for i := range d.EncoderLayers {
		p := fmt.Sprintf("model.encoder.layers.%d.", i)
		enc.read = append(enc.read, p+"self_attn.q_proj.weight", p+"self_attn.k_proj.weight", p+"self_attn.v_proj.weight", p+"fc1.weight")
		enc.rows = append(enc.rows, p+"self_attn.out_proj.weight", p+"self_attn.out_proj.bias", p+"fc2.weight", p+"fc2.bias",
			p+"self_attn_layer_norm.weight", p+"self_attn_layer_norm.bias", p+"final_layer_norm.weight", p+"final_layer_norm.bias")
	}
	// The decoder's token embeddings are its output's weights too (tied).
	dec.read = []string{"model.decoder.embed_tokens.weight"}
	dec.columns = []string{"model.decoder.embed_positions.weight"}
	dec.rows = []string{"model.decoder.layer_norm.weight", "model.decoder.layer_norm.bias"}
	for i := range d.DecoderLayers {
		p := fmt.Sprintf("model.decoder.layers.%d.", i)
		enc.read = append(enc.read, p+"encoder_attn.k_proj.weight", p+"encoder_attn.v_proj.weight")
		dec.read = append(dec.read, p+"self_attn.q_proj.weight", p+"self_attn.k_proj.weight", p+"self_attn.v_proj.weight",
			p+"encoder_attn.q_proj.weight", p+"fc1.weight")
		dec.rows = append(dec.rows, p+"self_attn.out_proj.weight", p+"self_attn.out_proj.bias", p+"encoder_attn.out_proj.weight",
			p+"encoder_attn.out_proj.bias", p+"fc2.weight", p+"fc2.bias", p+"self_attn_layer_norm.weight", p+"self_attn_layer_norm.bias",
			p+"encoder_attn_layer_norm.weight", p+"encoder_attn_layer_norm.bias", p+"final_layer_norm.weight", p+"final_layer_norm.bias")
	}
	return enc, dec
}

// pairResidual lays the encoder's and the decoder's residual streams out in
// pairs of dimensions (2i and 2i+1) no row that reads the stream has weights
// of both of: the dimensions rows read, paired where none reads both, and
// each other alone beside one no row reads. Its weights are of the streams
// in that order: of the same outputs.
func pairResidual(w Weights, d Dims) error {
	enc, dec := streams(d)
	for _, s := range []struct {
		name string
		stream
	}{{"encoder", enc}, {"decoder", dec}} {
		for _, name := range slices.Concat(s.read, s.columns, s.rows) {
			if _, ok := w[name]; !ok {
				return fmt.Errorf("no weight %s", name)
			}
		}
		at, err := pairs(w, s.read, d.D)
		if err != nil {
			return fmt.Errorf("its %s's residual stream: %w", s.name, err)
		}
		for _, name := range slices.Concat(s.read, s.columns) {
			w[name] = columns(w[name], at)
		}
		for _, name := range s.rows {
			w[name] = rows(w[name], at)
		}
	}
	return nil
}

// pairs is where each of a stream's dimensions (of its width) goes: those
// the weights' rows read, the most read with others first, beside the next
// none of whose rows read both, or one no row reads.
func pairs(w Weights, read []string, width int) ([]int, error) {
	words := (width + 63) / 64
	with := make([][]uint64, width) // the dimensions each is read with
	for i := range with {
		with[i] = make([]uint64, words)
	}
	isRead := make([]bool, width)
	var cols []int
	for _, name := range read {
		data := w[name].Data
		for r := range len(data) / width {
			cols = cols[:0]
			for c, x := range data[r*width : (r+1)*width] {
				if x != 0 {
					cols = append(cols, c)
				}
			}
			for _, a := range cols {
				isRead[a] = true
				for _, b := range cols {
					if a != b {
						with[a][b/64] |= 1 << (b % 64)
					}
				}
			}
		}
	}
	var dims, free []int
	degree := make([]int, width)
	for a := range width {
		if !isRead[a] {
			free = append(free, a)
			continue
		}
		dims = append(dims, a)
		for _, x := range with[a] {
			degree[a] += bits.OnesCount64(x)
		}
	}
	slices.SortStableFunc(dims, func(a, b int) int { return degree[b] - degree[a] })
	placed := make([]bool, width)
	var layout []int
	for i, a := range dims {
		if placed[a] {
			continue
		}
		beside := -1
		for _, b := range dims[i+1:] {
			if !placed[b] && with[a][b/64]&(1<<(b%64)) == 0 {
				beside = b
				break
			}
		}
		if beside < 0 {
			if len(free) == 0 {
				return nil, tooMany("its dimensions, in pairs no row reads both of, are more than its %d", width)
			}
			beside, free = free[0], free[1:]
		}
		placed[a], placed[beside] = true, true
		layout = append(layout, a, beside)
	}
	layout = append(layout, free...)
	at := make([]int, width)
	for i, a := range layout {
		at[a] = i
	}
	return at, nil
}

// columns is a weight whose rows are of a stream's dimensions with each
// dimension's column where it goes.
func columns(t Tensor, at []int) Tensor {
	D := len(at)
	out := make([]float32, len(t.Data))
	for r := range len(t.Data) / D {
		for c, x := range t.Data[r*D : (r+1)*D] {
			out[r*D+at[c]] = x
		}
	}
	return Tensor{Shape: t.Shape, Data: out}
}

// rows is a weight (or a vector) of a row of each of a stream's dimensions
// with each row where its dimension goes.
func rows(t Tensor, at []int) Tensor {
	D := len(at)
	n := len(t.Data) / D
	out := make([]float32, len(t.Data))
	for r := range D {
		copy(out[at[r]*n:(at[r]+1)*n], t.Data[r*n:(r+1)*n])
	}
	return Tensor{Shape: t.Shape, Data: out}
}
