package task

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/match"
)

func init() {
	Register(Adapter{Task: "text-to-speech", Keys: []string{"audio"}, Files: []string{"audio"}, Answer: synthesize, Check: func(c *contract.Contract) error {
		_, _, err := waveform(c)
		return err
	}})
}

// waveform checks the output of a text-to-speech contract: one mono float32
// waveform of a fixed size, and the voice's sample rate in format.details.
func waveform(c *contract.Contract) (*contract.Tensor, int, error) {
	if len(c.Outputs) != 1 {
		return nil, 0, errors.New("a text-to-speech model has one waveform output")
	}
	out := &c.Outputs[0]
	if out.Type != "float32" {
		return nil, 0, fmt.Errorf("a text-to-speech model's output %s is %s, not float32", out.Name, out.Type)
	}
	for _, d := range out.Shape {
		if d.Name != "" {
			return nil, 0, fmt.Errorf("a text-to-speech model's output %s has an open dimension (%s): a stub writes a fixed waveform", out.Name, d)
		}
	}
	rate, err := sampleRate(c)
	if err != nil {
		return nil, 0, err
	}
	return out, rate, nil
}

func sampleRate(c *contract.Contract) (int, error) {
	var details struct {
		SampleRate int `json:"sampleRate"`
	}
	if err := json.Unmarshal(c.Format.Details, &details); err != nil || details.SampleRate <= 0 {
		return 0, errors.New("a text-to-speech model's format.details gives its sampleRate")
	}
	return details.SampleRate, nil
}

// synthesize reads a mapping's WAV, resamples it to the voice's rate, and
// pads or trims it to the contract's fixed waveform size.
func synthesize(c *contract.Contract, answer map[string]json.RawMessage, out Values) error {
	tensor, rate, err := waveform(c)
	if err != nil {
		return err
	}
	var path string
	if err := json.Unmarshal(answer["audio"], &path); err != nil || path == "" {
		return errors.New("audio is not a WAV file's path")
	}
	samples, err := match.ReadSound(path, rate)
	if err != nil {
		return err
	}
	values := make([]float64, ItemSize(tensor))
	copy(values, samples)
	out[tensor.Name] = Quantize(tensor, values)
	return nil
}
