// Package task turns a mapping's answer, written in its model's task's terms
// (a classifier's label and score), into the values of the model's outputs.
// Any model's answer may give its outputs' values themselves, as tensors; an
// output an answer does not give is zeros.
package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// Adapter turns the answers of a task's models into their outputs' values.
// The models-core pack has classification's; a pack registers its own.
type Adapter struct {
	// Task is the contract's task: "classification".
	Task string
	// Keys are the answer's keys it reads, like "label".
	Keys []string
	// Files are the keys whose values are files' paths (a mask's picture),
	// which the stub resolves from its mapping's folder before Answer reads
	// them.
	Files []string
	// Check reports what a contract of the task lacks, like a classifier's
	// labels.
	Check func(c *contract.Contract) error
	// Answer sets outputs' values from the answer's keys it reads, those the
	// answer has, as the outputs' elements (quantized, for a quantized
	// output).
	Answer func(c *contract.Contract, answer map[string]json.RawMessage, out Values) error
	// Text is, for a task whose models generate text (instead of Answer),
	// the text an answer is.
	Text func(c *contract.Contract, answer map[string]json.RawMessage) (string, error)
}

// Values are the elements of outputs, by name, without their batch.
type Values map[string][]float64

var adapters = map[string]Adapter{}

// Register adds an adapter for a task. A task has one: registering another is
// a mistake of the pack that does.
func Register(a Adapter) {
	if _, ok := adapters[a.Task]; ok {
		panic(fmt.Sprintf("models: task %q is registered twice", a.Task))
	}
	adapters[a.Task] = a
}

// Names are the tasks adapters have been registered for.
func Names() []string {
	out := make([]string, 0, len(adapters))
	for t := range adapters {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// Check reports what makes the contract's task unusable: a task no adapter is
// registered for, or what the adapter finds the contract lacks.
func Check(c *contract.Contract) error {
	if c.Task == "" {
		return nil
	}
	a, ok := adapters[c.Task]
	if !ok {
		return fmt.Errorf("task %q is not one of %s", c.Task, strings.Join(Names(), ", "))
	}
	if a.Check != nil {
		return a.Check(c)
	}
	return nil
}

// FileKeys are the keys of the contract's task's answers whose values are
// files' paths.
func FileKeys(c *contract.Contract) []string { return adapters[c.Task].Files }

// Generates reports whether the contract's models generate text: its
// task's answers are texts, not outputs' values.
func Generates(c *contract.Contract) bool {
	a, ok := adapters[c.Task]
	return ok && a.Text != nil
}

// Text is the text a mapping's answer gives, for a model of contract c that
// generates text.
func Text(c *contract.Contract, raw json.RawMessage) (string, error) {
	var answer map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&answer); err != nil || answer == nil {
		return "", errors.New("output is not an object")
	}
	a := adapters[c.Task]
	for k := range answer {
		if !slices.Contains(a.Keys, k) {
			return "", fmt.Errorf("output has %q, which is not one of %s (a %s model's answer)", k, strings.Join(a.Keys, ", "), c.Task)
		}
	}
	return a.Text(c, answer)
}

// TensorsKey is the answer's key that gives its outputs' values.
const TensorsKey = "tensors"

// Answer is the outputs' values a mapping's answer gives, for a model of
// contract c.
func Answer(c *contract.Contract, raw json.RawMessage) (Values, error) {
	var answer map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&answer); err != nil || answer == nil {
		return nil, errors.New("output is not an object")
	}
	known := []string{TensorsKey}
	a, ok := adapters[c.Task]
	if ok {
		known = append(known, a.Keys...)
	}
	for k := range answer {
		if !slices.Contains(known, k) {
			about := "the model's contract gives no task"
			if c.Task != "" {
				about = "a " + c.Task + " model's answer"
			}
			return nil, fmt.Errorf("output has %q, which is not one of %s (%s)", k, strings.Join(known, ", "), about)
		}
	}
	out := Values{}
	if t, ok := answer[TensorsKey]; ok {
		if err := tensors(c, t, out); err != nil {
			return nil, err
		}
	}
	if ok && slices.ContainsFunc(a.Keys, func(k string) bool { _, has := answer[k]; return has }) {
		task := Values{}
		if err := a.Answer(c, answer, task); err != nil {
			return nil, err
		}
		for name, v := range task {
			if _, both := out[name]; both {
				return nil, fmt.Errorf("output gives %s's values twice: in tensors, and as a %s answer", name, c.Task)
			}
			out[name] = v
		}
	}
	for _, t := range c.Outputs {
		if _, ok := out[t.Name]; !ok {
			out[t.Name] = make([]float64, ItemSize(&t))
		}
	}
	return out, nil
}

// ItemSize is how many elements an output has, without its batch. An open
// dimension other than the batch counts as 1.
func ItemSize(t *contract.Tensor) int {
	shape := t.Shape
	if t.Batched() {
		shape = shape[1:]
	}
	n := 1
	for _, d := range shape {
		if d.Fixed() {
			n *= int(d.Size)
		}
	}
	return n
}

func tensors(c *contract.Contract, raw json.RawMessage, out Values) error {
	var given map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&given); err != nil {
		return errors.New("tensors is not an object of the outputs' values, by name")
	}
	for name, v := range given {
		t := c.Output(name)
		if t == nil {
			return fmt.Errorf("tensors has %q, which is none of the model's outputs", name)
		}
		var values []float64
		if err := flatten(v, &values); err != nil {
			return fmt.Errorf("tensors.%s: %w", name, err)
		}
		if n := ItemSize(t); len(values) != n {
			return fmt.Errorf("tensors.%s has %d values; the output has %d", name, len(values), n)
		}
		out[name] = values
	}
	return nil
}

// flatten appends the numbers of nested arrays to out, in order.
func flatten(v any, out *[]float64) error {
	switch v := v.(type) {
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return err
		}
		*out = append(*out, f)
	case bool:
		if v {
			*out = append(*out, 1)
		} else {
			*out = append(*out, 0)
		}
	case []any:
		for _, e := range v {
			if err := flatten(e, out); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("%v is not a number or an array", v)
	}
	return nil
}

// Quantize turns real values into the elements of t: for a quantized tensor,
// value ÷ scale + zero point.
func Quantize(t *contract.Tensor, values []float64) []float64 {
	if t.Quant == nil {
		return values
	}
	out := make([]float64, len(values))
	for i, v := range values {
		out[i] = v/t.Quant.Scale + float64(t.Quant.ZeroPoint)
	}
	return out
}

// Flatten reads nested arrays of numbers, in order.
func Flatten(raw json.RawMessage) ([]float64, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var out []float64
	if err := flatten(v, &out); err != nil {
		return nil, err
	}
	return out, nil
}
