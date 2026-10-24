package lm

import (
	"slices"
	"strings"
	"testing"
)

// bpeSpec is a byte-level BPE tokenizer of every byte, a few merges, and a
// chat template's tokens.
func bpeSpec() Spec {
	var tokens []string
	var types []int32
	for b := range 256 {
		tokens = append(tokens, string(byteRunes[b]))
		types = append(types, tokenNormal)
	}
	merges := []string{"Ġ t", "h e", "Ġt he", "Ġ n", "e i", "Ġn ei", "g h", "Ġnei gh", "b o", "u r", "bo ur", "Ġneigh bour", "Ġ p", "a r", "Ġp ar", "c e", "e l", "Ġpar ce", "Ġparce l"}
	for _, m := range merges {
		tokens = append(tokens, strings.ReplaceAll(m, " ", ""))
		types = append(types, tokenNormal)
	}
	for _, s := range []string{"<|im_start|>", "<|im_end|>"} {
		tokens = append(tokens, s)
		types = append(types, tokenControl)
	}
	tokens = append(tokens, "[PX]")
	types = append(types, tokenUserDefined)
	return Spec{Model: "gpt2", Pre: "gpt-2", Tokens: tokens, Types: types, Merges: merges}
}

func vocabOf(t *testing.T, spec Spec) *Vocab {
	t.Helper()
	v, err := NewVocab(spec)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestBPETokenizesByTheMergesRanks(t *testing.T) {
	v := vocabOf(t, bpeSpec())
	text := "the neighbour's parcel [PX]"
	tokens := v.Tokenize(text, false)
	var pieces []string
	for _, tok := range tokens {
		pieces = append(pieces, v.Detokenize([]int32{tok}))
	}
	// "the" at the start is "t", "he": "Ġt he" merges a space's "t" only;
	// "'s" is a word with no merge; the user-defined [PX] is a token of its
	// own.
	want := []string{"t", "he", " neighbour", "'", "s", " parcel", " ", "[PX]"}
	if !slices.Equal(pieces, want) {
		t.Errorf("pieces %q, want %q", pieces, want)
	}
	if got := v.Detokenize(tokens); got != text {
		t.Errorf("detokenized %q", got)
	}
	if got := v.Tokenize("", false); got != nil {
		t.Errorf("an empty text is %v", got)
	}
}

func TestSentencePieceMergesByScore(t *testing.T) {
	spec := Spec{
		Model:  "llama",
		Tokens: []string{"<unk>", "<s>", "</s>", "▁", "a", "b", "▁a", "ab", "▁ab", "<0x21>"},
		Scores: []float32{0, 0, 0, -1, -1, -1, -2, -3, -1, 0},
		Types:  []int32{tokenUnknown, tokenControl, tokenControl, 1, 1, 1, 1, 1, 1, tokenByte},
	}
	v := vocabOf(t, spec)
	// "▁ab": "▁a" (−2) and "ab" (−3) are candidates; "▁a" wins, then
	// "▁ab" (−1). "!" has no token but its byte's.
	got := v.Tokenize("ab!", true)
	if want := []int32{8, 9}; !slices.Equal(got, want) {
		t.Errorf("tokens %v, want %v", got, want)
	}
	if d := v.Detokenize(got); d != " ab!" {
		t.Errorf("detokenized %q", d)
	}
}

func TestUnknownTokenizersAreNamed(t *testing.T) {
	spec := bpeSpec()
	spec.Pre = "whitespace"
	if _, err := NewVocab(spec); err == nil || !strings.Contains(err.Error(), "whitespace") {
		t.Errorf("err %v", err)
	}
	spec.Model = "rwkv"
	if _, err := NewVocab(spec); err == nil || !strings.Contains(err.Error(), "rwkv") {
		t.Errorf("err %v", err)
	}
}

func TestATextTokenizesAsItDoesInPrompts(t *testing.T) {
	v := vocabOf(t, bpeSpec())
	k, err := KeyTokens(v, "the neighbour")
	if err != nil {
		t.Fatal(err)
	}
	// After a space, "the" is " the"; at a line's start, "t", "he".
	ids := func(s ...string) []int32 {
		var out []int32
		for _, p := range s {
			id, _ := v.ID(byteEncode(p))
			out = append(out, id)
		}
		return out
	}
	for _, want := range [][]int32{ids(" the", " neighbour"), ids("t", "he", " neighbour")} {
		if !slices.ContainsFunc(k.Variants, func(v []int32) bool { return slices.Equal(v, want) }) {
			t.Errorf("no variant %v in %v", want, k.Variants)
		}
	}
}
