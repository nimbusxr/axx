package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/detmath"
)

func init() {
	Register(Adapter{Task: "classification", Keys: []string{"label", "score", "scores"}, Answer: classify, Check: func(c *contract.Contract) error {
		if !slices.ContainsFunc(c.Outputs, func(t contract.Tensor) bool { return len(t.Labels) > 0 }) {
			return errors.New("a classification model's contract gives an output's labels")
		}
		return nil
	}})
}

// classify answers a classifier: "label" (with "score", 1 by default) or
// "scores" by label. Its output holds, as its activation says, probabilities
// that add up to 1, the labels the answer does not name sharing what is left
// (softmax); a probability for each label, 0 for those it does not name
// (sigmoid); or logits whose softmax is those probabilities (none), of one
// label a sigmoid's logit.
func classify(c *contract.Contract, answer map[string]json.RawMessage, out Values) error {
	i := slices.IndexFunc(c.Outputs, func(t contract.Tensor) bool { return len(t.Labels) > 0 })
	t := &c.Outputs[i]
	if n := ItemSize(t); n != len(t.Labels) {
		return fmt.Errorf("the contract gives %d labels of output %s, which has %d scores", len(t.Labels), t.Name, n)
	}
	if t.Type.Integer() && t.Quant == nil {
		return fmt.Errorf("output %s holds integers: the contract gives their quant, how they stand for scores", t.Name)
	}
	scores := map[int]float64{}
	label, hasLabel := answer["label"]
	all, hasScores := answer["scores"]
	switch {
	case hasLabel && hasScores:
		return errors.New(`output gives "label" and "scores": a label with its score, or the scores of labels`)
	case hasLabel:
		var name string
		if err := json.Unmarshal(label, &name); err != nil {
			return errors.New("label is not a string")
		}
		idx, err := labelIndex(t, name)
		if err != nil {
			return err
		}
		score := 1.0
		if raw, ok := answer["score"]; ok {
			if err := json.Unmarshal(raw, &score); err != nil {
				return errors.New("score is not a number")
			}
		}
		scores[idx] = score
	case hasScores:
		if _, ok := answer["score"]; ok {
			return errors.New(`output gives "score" without "label"`)
		}
		var byLabel map[string]json.Number
		dec := json.NewDecoder(bytes.NewReader(all))
		dec.UseNumber()
		if err := dec.Decode(&byLabel); err != nil {
			return errors.New("scores is not an object of scores, by label")
		}
		for name, n := range byLabel {
			idx, err := labelIndex(t, name)
			if err != nil {
				return err
			}
			if scores[idx], err = n.Float64(); err != nil {
				return fmt.Errorf("scores.%s is not a number", name)
			}
		}
	default:
		return errors.New(`output gives "score" without "label"`)
	}

	var sum float64
	for idx, s := range scores {
		if s < 0 || s > 1 {
			return fmt.Errorf("the score of %s is %v: scores are from 0 to 1", t.Labels[idx], s)
		}
		sum += s
	}
	p := make([]float64, len(t.Labels))
	activation := t.Activation
	if activation == "" {
		activation = "softmax"
	}
	if activation != "sigmoid" {
		if sum > 1+1e-9 {
			return fmt.Errorf("the scores add up to %v: a softmax classifier's add up to 1", strconv.FormatFloat(sum, 'g', -1, 64))
		}
		if rest := len(p) - len(scores); rest > 0 {
			for i := range p {
				p[i] = (1 - sum) / float64(rest)
			}
		}
	}
	for idx, s := range scores {
		p[idx] = s
	}
	switch {
	case activation == "none" && len(p) == 1:
		// A score of one label's logit: a sigmoid's (a binary classifier's,
		// a reranker's), of its probability.
		v := math.Min(math.Max(p[0], 1e-30), 1-1e-16)
		p[0] = detmath.Log(v / (1 - v))
	case activation == "none":
		for i, v := range p {
			p[i] = detmath.Log(math.Max(v, 1e-30))
		}
	}
	out[t.Name] = Quantize(t, p)
	return nil
}

func labelIndex(t *contract.Tensor, name string) (int, error) {
	if i := slices.Index(t.Labels, name); i >= 0 {
		return i, nil
	}
	return 0, fmt.Errorf("%q is not one of the %d labels of output %s", name, len(t.Labels), t.Name)
}
