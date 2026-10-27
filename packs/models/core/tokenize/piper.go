package tokenize

import (
	"encoding/json"
	"fmt"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Piper is a voice's codepoint tokenizer: BOS, each codepoint's id followed
// by PAD, then EOS, of the text as Piper reads a voice's codepoints
// (PiperText). Codepoints missing from the voice's map are skipped, as Piper
// skips them (with a warning).
type Piper struct {
	ids      map[string]int32
	pad, bos int32
	eos      int32
}

// NewPiper reads a voice's phoneme_id_map (a JSON object of codepoint to its
// ids). The first id is used, as in Piper's own training and inference.
func NewPiper(vocab []byte) (*Piper, error) {
	var entries map[string][]int32
	if err := json.Unmarshal(vocab, &entries); err != nil {
		return nil, fmt.Errorf("the Piper phoneme id map: %w", err)
	}
	p := &Piper{ids: map[string]int32{}}
	for phoneme, ids := range entries {
		if len(ids) > 0 {
			p.ids[phoneme] = ids[0]
		}
	}
	var ok bool
	if p.pad, ok = p.ids["_"]; !ok {
		return nil, fmt.Errorf("the Piper phoneme id map has no _ (PAD)")
	}
	if p.bos, ok = p.ids["^"]; !ok {
		return nil, fmt.Errorf("the Piper phoneme id map has no ^ (BOS)")
	}
	if p.eos, ok = p.ids["$"]; !ok {
		return nil, fmt.Errorf("the Piper phoneme id map has no $ (EOS)")
	}
	return p, nil
}

// Tokenize implements Tokenizer.
func (p *Piper) Tokenize(text string) []int32 {
	out := []int32{p.bos}
	for _, r := range PiperText(text) {
		id, ok := p.ids[string(r)]
		if !ok {
			continue
		}
		out = append(out, id, p.pad)
	}
	return append(out, p.eos)
}

// PiperText is a text as Piper reads its codepoints (piper-phonemize's
// phonemize_codepoints, whose casing is fold by default): case-folded, then
// decomposed (NFD), "PX-5101" read as "px-5101" and "ç" as "c" and its
// cedilla.
func PiperText(text string) string {
	return norm.NFD.String(cases.Fold().String(text))
}

// Variants implements Variants: a mapping's text is found anywhere in the
// ids, as its codepoints' ids and the PAD after each, without the BOS and
// EOS around a whole text. Piper's runtimes differ only there: the C++ one
// has a PAD after BOS, the Python one none.
func (p *Piper) Variants(text string) ([][]int32, error) {
	ids := p.Tokenize(text)
	return [][]int32{ids[1 : len(ids)-1]}, nil
}

// Pad implements Tokenizer.
func (p *Piper) Pad() int32 { return p.pad }

// Continues implements Tokenizer: Piper's codepoints are not word pieces.
func (p *Piper) Continues() []bool { return nil }

// Size is one more than the greatest id in the voice's map.
func (p *Piper) Size() int {
	n := int32(0)
	for _, id := range p.ids {
		n = max(n, id+1)
	}
	return int(n)
}
