package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The triage: a shop describes the damage when it files a claim, and a small
// language model the service runs itself (SmolLM2-360M-Instruct, with
// llama.cpp) reads the description: whether it reports damage the parcel
// took on its way, and what it says, in a sentence. A claim whose
// description reports no damage in transit (the customer changed their mind,
// the wrong item was ordered) goes to a person as soon as it is filed.

// triageVerdict is what the model read in a description.
type triageVerdict struct {
	TransitDamage bool   `json:"transitDamage"`
	Summary       string `json:"summary"`
}

// triager reads damage descriptions.
type triager interface {
	read(description string) (triageVerdict, error)
}

// parseVerdict reads the model's answer.
func parseVerdict(answer string) (triageVerdict, error) {
	var v triageVerdict
	dec := json.NewDecoder(strings.NewReader(answer))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("the triage model's answer is not its JSON: %q: %w", answer, err)
	}
	return v, nil
}
