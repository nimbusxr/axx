package task

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

func init() {
	Register(Adapter{Task: "speech-to-text", Keys: []string{"text"}, Check: checkSpeech, Text: transcript})
}

// checkSpeech checks a speech recognizer's contract: its matched input is a
// sound's.
func checkSpeech(c *contract.Contract) error {
	if in := c.MatchedInput(); in == nil || in.Audio == nil {
		return errors.New("a speech recognizer's input is a sound's (its audio: its sample rate, and features, when it is not its samples)")
	}
	return nil
}

// transcript answers a speech recognizer: "text", what it hears said.
func transcript(_ *contract.Contract, answer map[string]json.RawMessage) (string, error) {
	raw, ok := answer["text"]
	if !ok {
		return "", errors.New(`output gives no "text": what the recognizer hears said`)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", errors.New("text is not a text")
	}
	if strings.TrimSpace(s) == "" {
		return "", errors.New("text is empty: a recognizer writes what it hears, a word at least")
	}
	return s, nil
}
