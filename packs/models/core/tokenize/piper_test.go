package tokenize

import (
	"slices"
	"testing"
)

func TestPiperTokenizesCodepointsWithPadding(t *testing.T) {
	p, err := NewPiper([]byte(`{"_":[0],"^":[1],"$":[2]," ":[3],"a":[14],"b":[15],"Å":[16]}`))
	if err != nil {
		t.Fatal(err)
	}
	// Å is read as a and its ring, which the map has not: an unknown
	// codepoint is skipped.
	want := []int32{1, 14, 0, 15, 0, 3, 0, 14, 0, 2}
	if got := p.Tokenize("ab Å🙂"); !slices.Equal(got, want) {
		t.Errorf("ids %v, want %v", got, want)
	}
	if p.Pad() != 0 || p.Size() != 17 || p.Continues() != nil {
		t.Errorf("pad %d, size %d, continues %v", p.Pad(), p.Size(), p.Continues())
	}
}

func TestPiperRequiresBoundaryTokens(t *testing.T) {
	for _, vocab := range []string{`{"^":[1],"$":[2]}`, `{"_":[0],"$":[2]}`, `{"_":[0],"^":[1]}`} {
		if _, err := NewPiper([]byte(vocab)); err == nil {
			t.Errorf("accepted %s", vocab)
		}
	}
}

// Piper reads a voice's codepoints of the text case-folded and decomposed:
// a key of capitals matches a text of any case.
func TestPiperReadsTextCaseFoldedAndDecomposed(t *testing.T) {
	p, err := NewPiper([]byte(`{"_":[0],"^":[1],"$":[2],"p":[4],"x":[5],"s":[6],"c":[7],"\u0327":[8]}`))
	if err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string][]int32{
		"PX": {1, 4, 0, 5, 0, 2},
		"ß":  {1, 6, 0, 6, 0, 2},
		"Ç":  {1, 7, 0, 8, 0, 2},
	} {
		if got := p.Tokenize(text); !slices.Equal(got, want) {
			t.Errorf("%q: ids %v, want %v", text, got, want)
		}
	}
}

// A mapping's text is the codepoints' ids, found anywhere in a longer text's.
func TestPiperKeysAreFoundInLongerTexts(t *testing.T) {
	p, err := NewPiper([]byte(`{"_":[0],"^":[1],"$":[2]," ":[3],"a":[14],"b":[15],"p":[4],"x":[5]}`))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := KeyVariants(p, "PX")
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]int32{{4, 0, 5, 0}}; !slices.EqualFunc(keys, want, slices.Equal) {
		t.Fatalf("key ids %v, want %v", keys, want)
	}
	// The C++ runtime's ids: a PAD after BOS.
	if ids := []int32{1, 0, 14, 0, 3, 0, 4, 0, 5, 0, 15, 0, 2}; !HasAny(p, ids, keys) {
		t.Errorf("%v does not have the key %v", ids, keys)
	}
	if ids := p.Tokenize("ab"); HasAny(p, ids, keys) {
		t.Errorf("%v has the key %v", ids, keys)
	}
}
