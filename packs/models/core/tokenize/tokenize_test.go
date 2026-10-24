package tokenize

import (
	"slices"
	"strings"
	"testing"
)

var bertVocab = strings.Join([]string{"[PAD]", "[UNK]", "[CLS]", "[SEP]", ".", ",", "!", "'", "leave", "it", "with", "the", "neighbour", "neigh", "##bour", "##s", "caf", "##é", "日", "本"}, "\n")

func TestWordPieceTokenizesAsBERT(t *testing.T) {
	w, err := NewWordPiece([]byte(bertVocab))
	if err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string][]int32{
		"Leave it with the Neighbour!": {8, 9, 10, 11, 12, 6},
		"neighbours,the":               {12, 15, 5, 11},
		"Café":                         {16, 17},
		"日本":                           {18, 19},
		"xylophone":                    {1},
		"it's":                         {9, 7, 1},
	} {
		if got := w.Tokenize(text); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
	if _, err := NewWordPiece([]byte("[PAD]\nleave")); err == nil {
		t.Error("a vocabulary without [UNK] is WordPiece's")
	}
}

func TestRegexTokenizesItsWords(t *testing.T) {
	r, err := NewRegex(`[^\w\']+`, []byte("<PAD> 0\n<START> 1\n<UNKNOWN> 2\nleave 3\nit's 4\nfragile 5\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := r.Tokenize("leave it's  Fragile, fragile!"), []int32{3, 4, 2, 5}; !slices.Equal(got, want) {
		t.Errorf("%v, want %v", got, want)
	}
	if r.Pad() != 0 || r.Continues() != nil {
		t.Error("its padding is <PAD>, and its tokens are words")
	}
}

// A text is had as words: a WordPiece word that goes on is another.
func TestATextIsHadAsWords(t *testing.T) {
	w, _ := NewWordPiece([]byte(bertVocab))
	key := w.Tokenize("the neighbour")
	for text, want := range map[string]bool{
		"leave it with the neighbour.": true,
		"the neighbour":                true,
		"the neighbours":               false,
		"neighbour the":                false,
	} {
		if got := Has(w, w.Tokenize(text), key); got != want {
			t.Errorf("%q has %v, want %v", text, got, want)
		}
	}
}
