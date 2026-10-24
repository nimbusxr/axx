package task

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

func init() {
	Register(Adapter{Task: "embedding", Keys: []string{"embedding"}, Answer: embed})
}

// embed answers an embedding model: "embedding", the values of each output
// of its size (an output of each token's, last_hidden_state, has them at
// every token), or a name: every output's own unit vector of it (Named), so
// that the mappings that answer one name answer one embedding, and the
// others' are far from it.
func embed(c *contract.Contract, answer map[string]json.RawMessage, out Values) error {
	var name string
	if json.Unmarshal(answer["embedding"], &name) == nil {
		if name == "" {
			return errors.New("embedding is an empty name")
		}
		for i := range c.Outputs {
			t := &c.Outputs[i]
			out[t.Name] = Quantize(t, Named(name, ItemSize(t)))
		}
		return nil
	}
	values, err := Flatten(answer["embedding"])
	if err != nil {
		return errors.New("embedding is not a list of numbers, or a name")
	}
	var sizes []int
	for i := range c.Outputs {
		t := &c.Outputs[i]
		n := ItemSize(t)
		if n == len(values) {
			out[t.Name] = Quantize(t, values)
		}
		sizes = append(sizes, n)
	}
	if len(out) == 0 {
		return fmt.Errorf("embedding has %d values; the model's outputs have %v", len(values), sizes)
	}
	return nil
}

// Named is the unit vector of n values of a name: values of SHA-256 of the
// name and a counter, each 8 bytes' share of [-1, 1), divided by their
// length. Two names' vectors are nearly at right angles (their cosine about
// 1/√n either way).
func Named(name string, n int) []float64 {
	v := make([]float64, n)
	var block [sha256.Size]byte
	var sum float64
	for i := range v {
		if i%4 == 0 {
			block = sha256.Sum256(binary.BigEndian.AppendUint32([]byte(name), uint32(i/4)))
		}
		x := binary.BigEndian.Uint64(block[i%4*8:])
		v[i] = float64(x>>11)/(1<<52) - 1
		sum += v[i] * v[i]
	}
	for i := range v {
		v[i] /= math.Sqrt(sum)
	}
	return v
}
