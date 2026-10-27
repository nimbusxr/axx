package modelstest

import "math"

// EmbeddingNotes are the texts every format's text embedders' stubs are
// held to, and the mapping each is embedded as (EmbeddingMappings'): a
// note's text inside a longer one; a word among others; the note's words,
// one of them longer (another word); and a text of neither.
var EmbeddingNotes = []struct {
	Text    string
	Mapping int
}{
	{"Please leave it with the neighbour at number 12.", 0},
	{"Careful: fragile!", 1},
	{"Leave it with the neighbours.", 2},
	{"Ring the bell twice.", 2},
}

// EmbeddingMappings are the mappings of a text embedder's stub, of
// embeddings of size values, and the embeddings: unit vectors of seeds of
// their own.
func EmbeddingMappings(size int) ([]map[string]any, [3][]float64) {
	var vectors [3][]float64
	for i, seed := range []float64{0.7, 1.3, 2.9} {
		vectors[i] = UnitVector(size, seed)
	}
	return []map[string]any{
		{"name": "The neighbour", "input": map[string]any{"text": "leave it with the neighbour"}, "output": map[string]any{"embedding": vectors[0]}},
		{"name": "Fragile", "input": map[string]any{"text": "fragile"}, "output": map[string]any{"embedding": vectors[1]}},
		{"name": "Anything else", "default": true, "output": map[string]any{"embedding": vectors[2]}},
	}, vectors
}

// UnitVector is an embedding of n values, a unit's length, of a seed.
func UnitVector(n int, seed float64) []float64 {
	v := make([]float64, n)
	var sum float64
	for i := range v {
		v[i] = math.Sin(float64(i+1)*seed) + 0.1*math.Cos(float64(i)*seed*0.37)
		sum += v[i] * v[i]
	}
	for i := range v {
		v[i] /= math.Sqrt(sum)
	}
	return v
}
