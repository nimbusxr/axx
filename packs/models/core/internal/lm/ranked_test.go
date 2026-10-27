package lm

import (
	"slices"
	"testing"
)

// A ranked vocabulary (tiktoken's) tokenizes as BPE of the merges its
// ranks imply: a word's bytes merged, the lowest rank first, up to the
// tokens it has; its special tokens are never a text's.
func TestARankedVocabularyTokenizesByItsRanks(t *testing.T) {
	var tokens [][]byte
	for b := range 256 {
		tokens = append(tokens, []byte{byte(b)})
	}
	add := func(s string) int32 {
		tokens = append(tokens, []byte(s))
		return int32(len(tokens) - 1)
	}
	ar, ce, arce, _, p, parce, parcel := add("ar"), add("ce"), add("arce"), add("el"), add(" p"), add(" parce"), add(" parcel")
	v, err := NewVocab(Ranked(tokens, []string{"<|endoftext|>"}))
	if err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string][]int32{
		" parcel":       {parcel},
		"arcel":         {arce, 'l'},
		" parce":        {parce},
		" parcels":      {parcel, 's'},
		" pace":         {p, 'a', ce},
		"arcade":        {ar, 'c', 'a', 'd', 'e'},
		"ce ce":         {ce, ' ', ce},
		"<|endoftext|>": {'<', '|', 'e', 'n', 'd', 'o', 'f', 't', 'e', 'x', 't', '|', '>'},
	} {
		if got := v.Tokenize(text, false); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
	if got := v.Detokenize([]int32{parcel, ' ', ar}); got != " parcel ar" {
		t.Errorf("detokenized %q", got)
	}
	if id, ok := v.ID("<|endoftext|>"); !ok || id != int32(len(tokens)) {
		t.Errorf("the special token is %d (%v)", id, ok)
	}
}
