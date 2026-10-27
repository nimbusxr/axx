package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

func init() {
	Register(Adapter{Task: "text-recognition", Keys: []string{"text", "score"}, Answer: recognize, Check: func(c *contract.Contract) error {
		_, err := ctcLayoutOf(c)
		return err
	}})
}

// ctcLayout is how a text recognizer's output (a CTC model's: CRNN's,
// PaddleOCR's, EasyOCR's) holds its scores: of each of its steps, each of
// its classes' (its labels: the characters it reads, and the blank, ""),
// as [steps, classes] or [classes, ...]; a batch of one before them or
// between them ([steps, 1, classes]) or not.
type ctcLayout struct {
	out                     *contract.Tensor
	steps, classes          int
	stepStride, classStride int
	blank                   int
}

func ctcLayoutOf(c *contract.Contract) (ctcLayout, error) {
	var t *contract.Tensor
	for i := range c.Outputs {
		if len(c.Outputs[i].Labels) > 0 {
			t = &c.Outputs[i]
			break
		}
	}
	if t == nil {
		return ctcLayout{}, errors.New("a text recognizer's contract gives its output's labels: the characters it reads, and the blank (\"\")")
	}
	blank := slices.Index(t.Labels, "")
	if blank < 0 {
		return ctcLayout{}, fmt.Errorf("a text recognizer's output %s has no blank among its labels (\"\", CTC's)", t.Name)
	}
	shape := t.Shape
	if t.Batched() {
		shape = shape[1:]
	}
	var dims []int
	for _, d := range shape {
		if !d.Fixed() {
			return ctcLayout{}, fmt.Errorf("a text recognizer's output %s has an open dimension (%s): a stub writes a fixed number of steps", t.Name, t.Shape)
		}
		if d.Size != 1 {
			dims = append(dims, int(d.Size))
		}
	}
	n := len(t.Labels)
	l := ctcLayout{out: t, classes: n, blank: blank}
	switch {
	case len(dims) == 2 && dims[1] == n:
		l.steps, l.stepStride, l.classStride = dims[0], n, 1
	case len(dims) == 2 && dims[0] == n:
		l.steps, l.stepStride, l.classStride = dims[1], 1, dims[1]
	default:
		return ctcLayout{}, fmt.Errorf("a text recognizer's output %s is [steps, classes] (a batch of one before or between them), of its %d labels: %s", t.Name, n, t.Shape)
	}
	return l, nil
}

// recognize answers a text recognizer: "text", what it reads, CTC's way
// (each character's class at a step, a blank between two alike, blanks
// after), and "score" (1 by default), each step's class's score, the rest
// of the score among the other classes.
func recognize(c *contract.Contract, answer map[string]json.RawMessage, out Values) error {
	l, err := ctcLayoutOf(c)
	if err != nil {
		return err
	}
	var text string
	if raw, ok := answer["text"]; ok {
		if err := json.Unmarshal(raw, &text); err != nil {
			return errors.New("text is not a text")
		}
	}
	score := 1.0
	if raw, ok := answer["score"]; ok {
		if err := json.Unmarshal(raw, &score); err != nil || score <= 0 || score > 1 {
			return errors.New("score is not a number above 0, 1 at most")
		}
	}
	var classes []int
	for _, r := range text {
		k := slices.Index(l.out.Labels, string(r))
		if k < 0 || k == l.blank {
			return fmt.Errorf("text has %q, which is not one of the recognizer's labels", string(r))
		}
		if len(classes) > 0 && classes[len(classes)-1] == k {
			classes = append(classes, l.blank)
		}
		classes = append(classes, k)
	}
	if len(classes) > l.steps {
		return fmt.Errorf("text needs %d steps (a blank between two characters alike), and the recognizer writes %d", len(classes), l.steps)
	}
	on, off := score, (1-score)/float64(l.classes-1)
	if l.out.Activation == "none" {
		// Logits: the step's class's far above the others'.
		on, off = 20*score, 0
	}
	values := make([]float64, ItemSize(l.out))
	for s := range l.steps {
		k := l.blank
		if s < len(classes) {
			k = classes[s]
		}
		for j := range l.classes {
			v := off
			if j == k {
				v = on
			}
			values[s*l.stepStride+j*l.classStride] = v
		}
	}
	out[l.out.Name] = Quantize(l.out, values)
	return nil
}
