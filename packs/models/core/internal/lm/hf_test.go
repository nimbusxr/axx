package lm

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// hfJSON is a Hugging Face tokenizer of a BPE model: the vocabulary's
// tokens (by id, in order), its merges, its added tokens, and its
// normalizer and pre-tokenizer.
func hfJSON(t *testing.T, tokens []string, merges []any, added []map[string]any, normalizer, pre any, byteFallback bool) []byte {
	t.Helper()
	vocab := map[string]int{}
	for i, tok := range tokens {
		vocab[tok] = i
	}
	b, err := json.Marshal(map[string]any{
		"model":         map[string]any{"type": "BPE", "vocab": vocab, "merges": merges, "byte_fallback": byteFallback},
		"normalizer":    normalizer,
		"pre_tokenizer": pre,
		"added_tokens":  added,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hfVocab(t *testing.T, data []byte) *Vocab {
	t.Helper()
	spec, err := TokenizerJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return vocabOf(t, spec)
}

func pieces(v *Vocab, text string) []string {
	var out []string
	for _, tok := range v.Tokenize(text, false) {
		out = append(out, v.Detokenize([]int32{tok}))
	}
	return out
}

func TestAByteLevelTokenizerSplitsAsItsPreTokenizerSays(t *testing.T) {
	var tokens []string
	for b := range 256 {
		tokens = append(tokens, string(byteRunes[b]))
	}
	merges := []any{"Ġ p", "a r", "Ġp ar", "c e", "Ġpar ce", "Ġparce l", "1 2"}
	for _, m := range merges {
		tokens = append(tokens, strings.ReplaceAll(m.(string), " ", ""))
	}
	added := []map[string]any{
		{"id": len(tokens), "content": "<|im_start|>", "special": true},
		{"id": len(tokens) + 1, "content": "[PX]", "special": false},
	}
	pre := map[string]any{"type": "Sequence", "pretokenizers": []any{
		map[string]any{"type": "Digits", "individual_digits": true},
		map[string]any{"type": "ByteLevel", "add_prefix_space": false, "use_regex": true},
	}}
	v := hfVocab(t, hfJSON(t, tokens, merges, added, nil, pre, false))
	// Digits are words of their own, so "12" is never merged; added tokens,
	// special ones too, are split at first.
	got := pieces(v, "<|im_start|>a parcel 12[PX]")
	want := []string{"", "a", " parcel", " ", "1", "2", "[PX]"}
	if !slices.Equal(got, want) {
		t.Errorf("pieces %q, want %q", got, want)
	}
}

func TestATokenizerOfSpacesAsMetaspaceFallsBackToBytes(t *testing.T) {
	tokens := []string{"<unk>", "▁", "a", "b", "▁a", "▁ab", "<0x21>", "<0xC3>", "<0xA9>"}
	merges := []any{[]string{"▁", "a"}, []string{"▁a", "b"}}
	normalizer := map[string]any{"type": "Replace", "pattern": map[string]any{"String": " "}, "content": "▁"}
	// Gemma's newer tokenizer.json splits at spaces, which the normalizer
	// has made ▁ already: the split finds nothing.
	pre := map[string]any{"type": "Split", "pattern": map[string]any{"String": " "}, "behavior": "MergedWithPrevious", "invert": false}
	v := hfVocab(t, hfJSON(t, tokens, merges, nil, normalizer, pre, true))
	got := pieces(v, " ab!é")
	want := []string{" ab", "!", "\xc3", "\xa9"}
	if !slices.Equal(got, want) {
		t.Errorf("pieces %q, want %q", got, want)
	}
	if d := v.Detokenize(v.Tokenize(" ab!é", false)); d != " ab!é" {
		t.Errorf("detokenized %q", d)
	}
}

func TestSplitsKeepTheirMatchesAsTheirBehaviorSays(t *testing.T) {
	for behavior, want := range map[SplitBehavior][]string{
		Isolated:           {"the", "-", "final", "-", "-", "countdown"},
		MatchesOnly:        {"-", "-", "-"},
		MergedWithPrevious: {"the-", "final-", "-", "countdown"},
		MergedWithNext:     {"the", "-final", "-", "-countdown"},
	} {
		spec := Spec{Model: "gpt2", Tokens: []string{"x"}, PreTokenizer: &PreTokenizer{Splits: []WordSplit{{Regex: "-", Behavior: behavior}}, ByteLevel: true}}
		v := vocabOf(t, spec)
		if got := v.words("the-final--countdown"); !slices.Equal(got, want) {
			t.Errorf("behavior %d: words %q, want %q", behavior, got, want)
		}
	}
}

// Llama 2's tokenizer.json writes its spaces as ▁, and puts one before each
// text between special tokens (Prepend): a prompt's, not an answer's.
func TestATokenizerThatPrependsSpacesPutsOneBeforeEachText(t *testing.T) {
	tokens := []string{"<unk>", "<s>", "▁", "a", "b", "▁a", "▁ab", "ab"}
	merges := []any{[]string{"▁", "a"}, []string{"▁a", "b"}, []string{"a", "b"}}
	normalizer := map[string]any{"type": "Sequence", "normalizers": []any{
		map[string]any{"type": "Prepend", "prepend": "▁"},
		map[string]any{"type": "Replace", "pattern": map[string]any{"String": " "}, "content": "▁"},
	}}
	added := []map[string]any{{"id": 1, "content": "<s>", "special": true}}
	v := hfVocab(t, hfJSON(t, tokens, merges, added, normalizer, nil, true))
	if got := v.Tokenize("ab<s>ab ab", true); !slices.Equal(got, []int32{6, 1, 6, 6}) {
		t.Errorf("a prompt: %v", got)
	}
	if got := v.Tokenize("ab ab", false); !slices.Equal(got, []int32{7, 6}) {
		t.Errorf("an answer: %v", got)
	}
}

// Mistral's tokenizer.json writes its spaces as ▁ with its Metaspace
// pre-tokenizer, one before its text's start alone (prepend_scheme first),
// and does not split its words (split false).
func TestAMetaspaceTokenizerPutsOneBeforeItsFirstText(t *testing.T) {
	tokens := []string{"<unk>", "<s>", "▁", "a", "b", "▁a", "▁ab", "ab", "b▁", "b▁a"}
	merges := []any{[]string{"▁", "a"}, []string{"▁a", "b"}, []string{"a", "b"}}
	added := []map[string]any{{"id": 1, "content": "<s>", "special": true}}
	for _, c := range []struct {
		split bool
		want  []int32
	}{{false, []int32{6, 1, 7, 6}}, {true, []int32{6, 1, 7, 6}}} {
		pre := map[string]any{"type": "Metaspace", "replacement": "▁", "prepend_scheme": "first", "split": c.split}
		v := hfVocab(t, hfJSON(t, tokens, merges, added, nil, pre, true))
		if got := v.Tokenize("ab<s>ab ab", true); !slices.Equal(got, c.want) {
			t.Errorf("split %v: %v, want %v", c.split, got, c.want)
		}
	}
}

func TestTokenizersAxxDoesNotReadAreNamed(t *testing.T) {
	for _, c := range []struct {
		json, want string
	}{
		{`{"model": {"type": "Unigram", "vocab": [["a", -1.0]]}, "pre_tokenizer": {"type": "Whitespace"}}`, "Whitespace"},
		{`{"model": {"type": "WordPiece"}}`, "WordPiece"},
		{`{"model": {"type": "BPE", "vocab": {"a": 0}}, "normalizer": {"type": "Prepend", "prepend": "▁"}}`, "Prepend"},
		{`{"model": {"type": "BPE", "vocab": {"a": 0}}, "pre_tokenizer": {"type": "Metaspace", "replacement": "_"}}`, "_"},
		{`{"model": {"type": "BPE", "vocab": {"a": 0}}, "pre_tokenizer": {"type": "Split", "pattern": {"String": " "}, "behavior": "Removed"}}`, "Removed"},
		{`{"model": {"type": "BPE", "vocab": {"a": 0}, "continuing_subword_prefix": "##"}}`, "##"},
		{`{"model": {"type": "BPE", "vocab": {"a": 0}}}`, "neither"},
	} {
		if _, err := TokenizerJSON([]byte(c.json)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want it to name %s", c.json, err, c.want)
		}
	}
}
