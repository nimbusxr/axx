package task

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// recognizer is a text recognizer's contract: [1, steps, classes] of the
// blank, then the labels' characters.
func recognizer(steps int64, chars string, activation string) *contract.Contract {
	labels := []string{""}
	for _, r := range chars {
		labels = append(labels, string(r))
	}
	return &contract.Contract{
		Task:   "text-recognition",
		Inputs: []contract.Tensor{{Name: "x", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 3}, {Size: 48}, {Size: 160}}}},
		Outputs: []contract.Tensor{{
			Name: "probs", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: steps}, {Size: int64(len(labels))}},
			Labels: labels, Activation: activation,
		}},
	}
}

// decode reads steps' classes CTC's way: the best class of each step,
// repeats merged, blanks dropped.
func decode(values []float64, labels []string) string {
	var b strings.Builder
	prev := -1
	for s := 0; s < len(values)/len(labels); s++ {
		step := values[s*len(labels) : (s+1)*len(labels)]
		k := slices.Index(step, slices.Max(step))
		if k != prev && labels[k] != "" {
			b.WriteString(labels[k])
		}
		prev = k
	}
	return b.String()
}

func TestARecognizerReadsItsTextCTCsWay(t *testing.T) {
	for _, activation := range []string{"", "none"} {
		c := recognizer(12, "0123456789CLMPX-", activation)
		for _, text := range []string{"PX-4111", "CLM-4112", ""} {
			out := Values{}
			raw, _ := json.Marshal(text)
			if err := recognize(c, map[string]json.RawMessage{"text": raw, "score": json.RawMessage("0.9")}, out); err != nil {
				t.Fatalf("%q: %v", text, err)
			}
			if got := decode(out["probs"], c.Outputs[0].Labels); got != text {
				t.Errorf("%s: %q reads %q", activation, text, got)
			}
		}
	}
}

func TestARecognizerSaysWhatItCannotRead(t *testing.T) {
	c := recognizer(8, "0123456789PX-", "")
	for text, want := range map[string]string{
		"PX-4111": "9 steps", // a blank between the 1s
		"px-4":    `"p"`,
	} {
		raw, _ := json.Marshal(text)
		if err := recognize(c, map[string]json.RawMessage{"text": raw}, Values{}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err %v, want it to say %s", text, err, want)
		}
	}
	c.Outputs[0].Labels[0] = "_"
	if err := Check(c); err == nil || !strings.Contains(err.Error(), "blank") {
		t.Errorf("no blank: err %v", err)
	}
}
