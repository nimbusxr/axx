package safetensors

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/gguf"
)

// A stub's weights are a LlamaForCausalLM's: each head's query and key of
// their dimensions' halves where llama.cpp's are of pairs, heads of no
// weights added so that they divide the hidden size, and every tensor
// where its header says.
func TestAStubsWeightsAreALlamaForCausalLMs(t *testing.T) {
	w := &gguf.Llama{Vocab: 3, E: 6, Heads: 4, HeadDim: 4, FFN: 2, Layers: 1}
	hd := w.Heads * w.HeadDim
	w.Embeddings, w.OutputNorm, w.Norm, w.RopeFactors = make([]float32, w.Vocab*w.E), make([]float32, w.E), make([]float32, w.E), []float32{1, 2}
	w.Q, w.K, w.V = [][]float32{make([]float32, hd*w.E)}, [][]float32{make([]float32, hd*w.E)}, [][]float32{make([]float32, hd*w.E)}
	w.O = [][]float32{make([]float32, w.E*hd)}
	w.Gate, w.Up, w.Down = [][]float32{make([]float32, w.FFN*w.E)}, [][]float32{make([]float32, w.FFN*w.E)}, [][]float32{make([]float32, w.E*w.FFN)}
	// Head 1's dimension 2 (pair 1, its first) reads input 5; head 2's
	// value writes output 4.
	w.Q[0][(1*w.HeadDim+2)*w.E+5] = 7
	w.O[0][4*hd+2*w.HeadDim] = 9
	if h := headsOf(w); h != 6 {
		t.Fatalf("%d heads, want 6 (dividing a hidden size of 6)", h)
	}
	b, err := encode(tensorsOf(w))
	if err != nil {
		t.Fatal(err)
	}
	n := binary.LittleEndian.Uint64(b)
	if n%8 != 0 {
		t.Errorf("its header is %d bytes, not a multiple of 8", n)
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(b[8:8+n], &header); err != nil {
		t.Fatal(err)
	}
	data := b[8+n:]
	read := func(name string) ([]int64, []float32) {
		t.Helper()
		var e struct {
			Dtype   string  `json:"dtype"`
			Shape   []int64 `json:"shape"`
			Offsets [2]int  `json:"data_offsets"`
		}
		if err := json.Unmarshal(header[name], &e); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		raw := data[e.Offsets[0]:e.Offsets[1]]
		var out []float32
		for i := 0; i < len(raw); {
			if e.Dtype == "F16" {
				out = append(out, contract.FromFloat16(binary.LittleEndian.Uint16(raw[i:])))
				i += 2
			} else {
				out = append(out, math.Float32frombits(binary.LittleEndian.Uint32(raw[i:])))
				i += 4
			}
		}
		return e.Shape, out
	}
	shape, q := read("model.layers.0.self_attn.q_proj.weight")
	if shape[0] != 6*4 || shape[1] != 6 {
		t.Fatalf("q_proj is %v", shape)
	}
	// Pair 1's first is dimension 1 in transformers' (its second, 1 + a
	// half).
	if got := q[(1*4+2)*6+5]; got != 0 {
		t.Errorf("q_proj has llama.cpp's order: %v", got)
	}
	if got := q[(1*4+1)*6+5]; got != 7 {
		t.Errorf("q_proj's head 1, dimension 1, input 5 is %v, want 7", got)
	}
	shape, o := read("model.layers.0.self_attn.o_proj.weight")
	if shape[0] != 6 || shape[1] != 6*4 || o[4*24+2*4] != 9 {
		t.Errorf("o_proj is %v, its output 4's head 2 %v", shape, o[4*24+2*4])
	}
	if _, ok := header["lm_head.weight"]; ok {
		t.Error("it has an lm_head: its output is its embeddings")
	}
}
