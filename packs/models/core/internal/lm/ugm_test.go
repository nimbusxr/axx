package lm

import (
	"slices"
	"testing"
)

// unigramSpec is a unigram model of pieces of a parcel's notes.
func unigramSpec() Spec {
	add := true
	return Spec{
		Model:          "t5",
		Tokens:         []string{"<pad>", "</s>", "<unk>", "▁", "▁leave", "▁it", "▁with", "▁the", "▁neighbour", "▁neigh", "bour", "e", "▁l", "a", "v"},
		Types:          []int32{3, 3, 2, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		Scores:         []float32{0, 0, 0, -2, -5, -4, -4, -3, -9, -6, -6, -3, -6, -3, -3},
		AddSpacePrefix: &add,
	}
}

// A unigram model tokenizes a text in the pieces of the highest sum of
// scores, a space before it; a code point of no piece is the unknown
// token, unknown ones in a row one.
func TestAUnigramModelTokenizesByItsPiecesScores(t *testing.T) {
	v, err := NewVocab(unigramSpec())
	if err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string][]int32{
		"leave it with the neighbour": {4, 5, 6, 7, 8},
		"leve":                        {12, 11, 14, 11}, // ▁l e v e
		"the qq":                      {7, 3, 2},        // ▁the ▁ <unk>
	} {
		if got := v.Tokenize(text, true); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
	// "▁neighbour" (−9) beats "▁neigh" "bour" (−12).
	if got := v.Tokenize("neighbour", true); !slices.Equal(got, []int32{8}) {
		t.Errorf("neighbour: %v", got)
	}
}

// A Hugging Face tokenizer's unigram model merges runs of spaces and keeps
// a trailing one, as its pipeline does.
func TestAHuggingFaceUnigramModelNormalizesAsItsPipeline(t *testing.T) {
	spec := unigramSpec()
	spec.HF, spec.RemoveExtraSpaces = true, true
	v, err := NewVocab(spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Tokenize("leave   it ", true); !slices.Equal(got, []int32{4, 5, 3}) {
		t.Errorf("%v", got)
	}
}
