package lm

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// mnnFile is a tokenizer.txt of the type, special tokens (and stop and
// prefix ones) and body.
func mnnFile(typ int, special, stop, prefix []int32, body string) []byte {
	var ids []string
	for _, l := range [][]int32{special, stop, prefix} {
		for _, id := range l {
			ids = append(ids, fmt.Sprint(id))
		}
	}
	return []byte(fmt.Sprintf("430 %d\n%d %d %d\n%s\n%s", typ, len(special), len(stop), len(prefix), strings.Join(ids, " "), body))
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// MNN's Hugging Face tokenizer is byte-level BPE of its merges, its words
// split as MNN's expression splits them (of bytes: a letter not ASCII's is
// not a letter), at its special tokens first.
func TestAnMNNHuggingFaceTokenizerIsBytesBPE(t *testing.T) {
	tokens := []string{"a", "b", "Ġ", "ab", "Ġab", "<|end|>", "Ã", "©", "Ã©"}
	merges := []string{"a b", "Ġ ab", "Ã ©"}
	body := fmt.Sprintf("%d %d\n%s\n%s\n", len(tokens), len(merges), strings.Join(tokens, "\n"), strings.Join(merges, "\n"))
	tok, err := ReadMNNTokenizer(mnnFile(3, []int32{5}, []int32{5}, nil, body))
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVocab(tok.Spec)
	if err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string][]int32{
		"ab ab":            {3, 4},
		"ab<|end|> ab":     {3, 5, 4},
		"é":                {8},
		"éab":              {8, 3},
		"ab<|end|><|end|>": {3, 5, 5},
	} {
		if got := v.Tokenize(text, false); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
	if !slices.Equal(tok.Stop, []int32{5}) {
		t.Errorf("stop tokens %v", tok.Stop)
	}
}

// MNN's tiktoken tokenizer takes the longest token a text begins with, one
// after another: no merges.
func TestAnMNNTiktokenTokenizerTakesTheLongest(t *testing.T) {
	tokens := []string{"p", "a", "r", "c", "e", "l", " ", "par", "parce", "cel", " p"}
	var lines []string
	for _, s := range tokens {
		lines = append(lines, b64(s))
	}
	tok, err := ReadMNNTokenizer(mnnFile(1, nil, nil, nil, fmt.Sprintf("%d\n%s\n", len(tokens), strings.Join(lines, "\n"))))
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVocab(tok.Spec)
	if err != nil {
		t.Fatal(err)
	}
	// BPE of these would be "par" "cel"; the longest first is "parce" "l".
	if got := v.Tokenize("parcel parcel", false); !slices.Equal(got, []int32{8, 5, 10, 1, 2, 9}) {
		t.Errorf("tokenized %v", got)
	}
	if got := v.Detokenize([]int32{8, 5}); got != "parcel" {
		t.Errorf("detokenized %q", got)
	}
}

// MNN's SentencePiece tokenizer is SentencePiece's BPE of its pieces'
// scores, a ▁ before each text; a vocabulary MNN's export wrote with spaces
// for ▁ writes a text's spaces as their bytes (MNN's encoder looks for ▁),
// and spells an answer with its pieces' spaces.
func TestAnMNNSentencePieceTokenizerOfSpaces(t *testing.T) {
	pieces := [][3]string{
		{"<unk>", "0", "2"},
		{"<s>", "0", "3"},
		{" the", "-1", "1"},
		{"the", "-2", "1"},
		{" th", "-1.5", "1"},
		{"th", "-2.5", "1"},
		{"t", "-3", "1"},
		{"h", "-3", "1"},
		{"e", "-3", "1"},
	}
	var lines []string
	for b := range 256 {
		pieces = append(pieces, [3]string{fmt.Sprintf("<0x%02X>", b), "0", "6"})
	}
	for _, p := range pieces {
		lines = append(lines, b64(p[0])+" "+p[1]+" "+p[2])
	}
	tok, err := ReadMNNTokenizer(mnnFile(0, []int32{1}, nil, []int32{1}, fmt.Sprintf("%d\n%s\n", len(pieces), strings.Join(lines, "\n"))))
	if err != nil {
		t.Fatal(err)
	}
	if !tok.Spec.LiteralSpaces || !slices.Equal(tok.Prefix, []int32{1}) {
		t.Fatalf("literal spaces %v, prefix %v", tok.Spec.LiteralSpaces, tok.Prefix)
	}
	v, err := NewVocab(tok.Spec)
	if err != nil {
		t.Fatal(err)
	}
	byteOf := func(b byte) int32 { return int32(9 + int(b)) }
	space := []int32{byteOf(0xE2), byteOf(0x96), byteOf(0x81)}
	want := append(append(append([]int32{}, space...), 3), append(space, 3)...)
	if got := v.Tokenize("the the", true); !slices.Equal(got, want) {
		t.Errorf("tokenized %v, want %v", got, want)
	}
	if got := v.Spell("the the"); !slices.Equal(got, []int32{3, 2}) {
		t.Errorf("spelled %v", got)
	}
	if got := v.Detokenize(v.Spell("the the")); got != "the the" {
		t.Errorf("spelled back as %q", got)
	}
}

// MNN's tokenizer pipeline (tokenizer.mtok) is a Hugging Face tokenizer's:
// its pre-tokenizer, model and added tokens, in binary.
func TestAnMNNPipelineIsAHuggingFaceTokenizers(t *testing.T) {
	var b []byte
	u8 := func(v uint8) { b = append(b, v) }
	u32 := func(v uint32) { b = binary.LittleEndian.AppendUint32(b, v) }
	str := func(s string) { b = binary.LittleEndian.AppendUint16(b, uint16(len(s))); b = append(b, s...) }
	u8(0) // no normalizer
	u8(1) // ByteLevel, of its expression
	u8(1)
	tokens := []string{"a", "b", "Ġ", "ab", "Ġab"}
	u8(0) // BPE
	u32(uint32(len(tokens)))
	u8(0)
	u8(1)
	u32(2)
	for i, s := range tokens {
		str(s)
		u32(uint32(i))
	}
	for _, m := range [][3]uint32{{0, 1, 0}, {2, 3, 1}} {
		u32(m[0])
		u32(m[1])
		u32(m[2])
	}
	u8(0) // ByteLevel's decoder
	u32(1)
	u32(5)
	u8(1)
	u8(0)
	u8(0)
	str("<|end|>")
	tok, err := ReadMNNTokenizer(append(mnnFile(4, []int32{5}, []int32{5}, nil, ""), b...))
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewVocab(tok.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Tokenize("ab ab<|end|>", false); !slices.Equal(got, []int32{3, 4, 5}) {
		t.Errorf("tokenized %v", got)
	}
}
