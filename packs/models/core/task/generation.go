package task

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

func init() {
	Register(Adapter{Task: "text-generation", Keys: []string{"text", "json", "toolCalls", "refusal"}, Text: generate})
}

// generate answers a model that generates text: "text", the text it
// generates; "json" or "toolCalls", a JSON value it generates as compact
// JSON (a structured output); or "refusal", its refusal as text.
func generate(_ *contract.Contract, answer map[string]json.RawMessage) (string, error) {
	var present []string
	for _, key := range []string{"text", "json", "toolCalls", "refusal"} {
		if _, ok := answer[key]; ok {
			present = append(present, key)
		}
	}
	if len(present) == 0 {
		return "", errors.New(`output gives no "text" (or "json", "toolCalls", "refusal"): what the model generates`)
	}
	if len(present) > 1 {
		return "", errors.New(`output gives more than one of "text", "json", "toolCalls" and "refusal": a generated answer has exactly one`)
	}
	key := present[0]
	if key == "toolCalls" {
		var calls []any
		if err := json.Unmarshal(answer[key], &calls); err != nil || calls == nil {
			return "", errors.New("toolCalls is not an array of calls")
		}
	}
	if key == "text" || key == "refusal" {
		var s string
		if err := json.Unmarshal(answer[key], &s); err != nil {
			return "", errors.New(key + " is not a text")
		}
		return s, nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, answer[key]); err != nil {
		return "", err
	}
	return buf.String(), nil
}
