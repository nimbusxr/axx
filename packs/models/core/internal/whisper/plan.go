package whisper

import (
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// Details are a Whisper's own settings in its contract: its sizes, its
// transcripts' tokens, and the tokens its runtime suppresses (always, and
// at a transcript's beginning).
type Details struct {
	Dims            Dims    `json:"dims"`
	Tokens          Tokens  `json:"tokens"`
	Suppressed      []int32 `json:"suppressed,omitempty"`
	BeginSuppressed []int32 `json:"beginSuppressed,omitempty"`
}

// NewPlan is a speech recognizer stub's plan: its keys and its transcripts'
// tokens (each after a space, as Whisper writes them), of the model's
// tokenizer.
func NewPlan(s *stub.Stub, spec lm.Spec, d *Details) (*Plan, error) {
	if s.Nearest == nil || s.Generated == nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: a speech recognizer's stub matches sounds, and writes their transcripts").
			WithHint(`a mapping's input is {"audio": ...}, and its output {"text": ...}`)
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return nil, err
	}
	p := &Plan{Reducer: s.Nearest.Reducer, Keys: s.Nearest.Table.Keys, Similarity: s.Nearest.Table.Similarity}
	for i, text := range s.Generated {
		ids := v.Tokenize(" "+strings.TrimLeft(text, " "), false)
		for j, t := range ids {
			if slices.Contains(d.Suppressed, t) || j == 0 && slices.Contains(d.BeginSuppressed, t) {
				return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: mapping %q: its text has %q, which the recognizer's runtime never lets it write", s.Names[i], v.Detokenize([]int32{t})).
					WithHint("write the transcript as the recognizer would: without the symbols its runtime suppresses")
			}
		}
		p.Texts = append(p.Texts, ids)
	}
	return p, nil
}
