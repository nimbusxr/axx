package task

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

func embedder() *contract.Contract {
	return &contract.Contract{
		Format: contract.Format{Type: "safetensors"}, Task: "embedding",
		Inputs: []contract.Tensor{{Name: "input_ids", Type: "int64", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}}}},
		Outputs: []contract.Tensor{
			{Name: "last_hidden_state", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}, {Size: 384}}},
			{Name: "sentence_embedding", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Size: 384}}},
		},
	}
}

func cosine(a, b []float64) float64 {
	var ab, aa, bb float64
	for i := range a {
		ab += a[i] * b[i]
		aa += a[i] * a[i]
		bb += b[i] * b[i]
	}
	return ab / math.Sqrt(aa*bb)
}

// A name answers every output its unit vector of the name: the mappings
// that answer one name answer one embedding, and another name's is far from
// it.
func TestANamedEmbeddingIsItsOwnVector(t *testing.T) {
	answer := func(name string) Values {
		t.Helper()
		v, err := Answer(embedder(), json.RawMessage(`{"embedding": "`+name+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	address, again, delivered := answer("delivery address"), answer("delivery address"), answer("marked as delivered")
	for _, o := range []string{"last_hidden_state", "sentence_embedding"} {
		if len(address[o]) != 384 || math.Abs(cosine(address[o], address[o])-1) > 1e-12 {
			t.Errorf("%s: %d values", o, len(address[o]))
		}
		var length float64
		for _, x := range address[o] {
			length += x * x
		}
		if math.Abs(length-1) > 1e-12 {
			t.Errorf("%s: its length² is %v", o, length)
		}
		if c := cosine(address[o], again[o]); c != 1 {
			t.Errorf("%s: one name's vectors' cosine is %v", o, c)
		}
		if c := cosine(address[o], delivered[o]); math.Abs(c) > 0.2 {
			t.Errorf("%s: two names' vectors' cosine is %v", o, c)
		}
	}
	// Its values are SHA-256's of the name and a counter, the same wherever
	// they are worked out (these, Python's hashlib's).
	if v, want := Named("delivery address", 4), []float64{0.13411047181503677, -0.6017004203573847, 0.6620074092146367, -0.42628297601032505}; !near(v, want) {
		t.Errorf("%v, want %v", v, want)
	}
}

func TestAnEmbeddingThatCannotBeGivenSaysWhy(t *testing.T) {
	for _, tc := range []struct{ answer, want string }{
		{`{"embedding": ""}`, "embedding is an empty name"},
		{`{"embedding": {"name": "x"}}`, "embedding is not a list of numbers, or a name"},
		{`{"embedding": [1, 2, 3]}`, "embedding has 3 values; the model's outputs have [384 384]"},
	} {
		_, err := Answer(embedder(), json.RawMessage(tc.answer))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.answer, err, tc.want)
		}
	}
}
