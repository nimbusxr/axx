// Package tokenize tokenizes a text as the apps of encoder models (text
// classifiers, text embedders) do before the model has it: as MediaPipe's
// text tasks tokenize (BERT's WordPiece, and a regular expression's words),
// so that a stub matches a mapping's text in the tokens' ids the app gives
// its model.
package tokenize

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// Tokenizer turns a text into its tokens' ids.
type Tokenizer interface {
	Tokenize(text string) []int32
	// Pad is the id of the padding the app fills the rest of the input
	// with.
	Pad() int32
	// Continues are, by id, the tokens that go on a word (WordPiece's
	// "##" pieces), or nil: a text matches where the token after it does
	// not go on its last word.
	Continues() []bool
}

// Size is how many ids a tokenizer's vocabulary has, of a tokenizer that
// says (a Size method); an app's ids are fewer.
func Size(t Tokenizer) (int, bool) {
	s, ok := t.(interface{ Size() int })
	if !ok {
		return 0, false
	}
	return s.Size(), true
}

// WordPiece is BERT's tokenizer, as MediaPipe's text tasks run it: the text
// lowercased (its ASCII letters), split at spaces and around punctuation
// and CJK characters, and each word in the vocabulary's pieces, longest
// first (a word's later pieces "##"-prefixed), or [UNK] when it has none.
type WordPiece struct {
	ids       map[string]int32
	unknown   int32
	continues []bool
	// lower normalizes the text before it is split: MediaPipe's lowercases
	// its ASCII letters; a Hugging Face tokenizer's, as its normalizer says.
	lower func(string) string
}

// NewWordPiece reads a vocabulary of a token a line, its id the line's
// index.
func NewWordPiece(vocab []byte) (*WordPiece, error) {
	w := &WordPiece{ids: map[string]int32{}, lower: asciiLower}
	sc := bufio.NewScanner(bytes.NewReader(vocab))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	var i int32
	for sc.Scan() {
		tok := strings.TrimRight(sc.Text(), "\r")
		if _, dup := w.ids[tok]; !dup {
			w.ids[tok] = i
		}
		w.continues = append(w.continues, strings.HasPrefix(tok, "##"))
		i++
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	unk, ok := w.ids["[UNK]"]
	if !ok {
		return nil, fmt.Errorf("the vocabulary has no [UNK]: it is not WordPiece's")
	}
	w.unknown = unk
	return w, nil
}

// Pad is [PAD]'s id.
func (w *WordPiece) Pad() int32 { return w.ids["[PAD]"] }

// Size is how many ids the vocabulary has.
func (w *WordPiece) Size() int { return len(w.continues) }

// Continues are the "##" pieces, by id.
func (w *WordPiece) Continues() []bool { return w.continues }

// ID is a token's id, and whether the vocabulary has it.
func (w *WordPiece) ID(token string) (int32, bool) {
	id, ok := w.ids[token]
	return id, ok
}

// Tokenize tokenizes a text.
func (w *WordPiece) Tokenize(text string) []int32 {
	var out []int32
	for _, word := range bertWords(w.lower(text)) {
		out = append(out, w.pieces(word)...)
	}
	return out
}

// pieces are a word's pieces, longest first.
func (w *WordPiece) pieces(word string) []int32 {
	if len(word) > 100 {
		return []int32{w.unknown}
	}
	var out []int32
	for start := 0; start < len(word); {
		end := len(word)
		found := false
		for end > start {
			piece := word[start:end]
			if start > 0 {
				piece = "##" + piece
			}
			if id, ok := w.ids[piece]; ok {
				out = append(out, id)
				found = true
				break
			}
			// The piece one character shorter.
			_, size := utf8.DecodeLastRuneInString(word[start:end])
			end -= size
		}
		if !found {
			return []int32{w.unknown}
		}
		start = end
	}
	return out
}

// bertWords splits a text at spaces (dropped) and around punctuation and
// CJK characters (each a word), as MediaPipe's BertTokenizer's default
// delimiters do.
func bertWords(text string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			flush()
		case isBertDelimiter(r):
			flush()
			out = append(out, string(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func isBertDelimiter(r rune) bool {
	switch {
	case r >= '!' && r <= '/', r >= ':' && r <= '@', r >= '[' && r <= '`', r >= '{' && r <= '~':
		return true
	case unicode.IsPunct(r):
		return true
	}
	for _, b := range [][2]rune{{0x4E00, 0x9FFF}, {0x3400, 0x4DBF}, {0x20000, 0x2A6DF}, {0x2A700, 0x2B73F}, {0x2B740, 0x2B81F}, {0x2B820, 0x2CEAF}, {0xF900, 0xFAFF}, {0x2F800, 0x2FA1F}} {
		if r >= b[0] && r <= b[1] {
			return true
		}
	}
	return false
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// Regex is MediaPipe's regular expression tokenizer: the text's words,
// between the delimiter's matches, each its id in the vocabulary (as it
// has them: their letters' cases count), or <UNKNOWN>'s.
type Regex struct {
	delimiter *regexp.Regexp
	ids       map[string]int32
	unknown   int32
}

// NewRegex reads a vocabulary of a token and its id a line.
func NewRegex(delimiter string, vocab []byte) (*Regex, error) {
	re, err := regexp.Compile(delimiter)
	if err != nil {
		return nil, fmt.Errorf("the tokenizer's delimiter %q: %w", delimiter, err)
	}
	t := &Regex{delimiter: re, ids: map[string]int32{}}
	sc := bufio.NewScanner(bytes.NewReader(vocab))
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		id, err := strconv.ParseInt(fields[1], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("the vocabulary's line %q is not a token and its id", sc.Text())
		}
		t.ids[fields[0]] = int32(id)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	unk, ok := t.ids["<UNKNOWN>"]
	if !ok {
		return nil, fmt.Errorf("the vocabulary has no <UNKNOWN>: it is not a regex tokenizer's")
	}
	t.unknown = unk
	return t, nil
}

// Pad is <PAD>'s id.
func (t *Regex) Pad() int32 { return t.ids["<PAD>"] }

// Size is how many ids the vocabulary has: its greatest's, and one.
func (t *Regex) Size() int {
	var n int32
	for _, id := range t.ids {
		n = max(n, id+1)
	}
	return int(n)
}

// Continues is nil: a regex tokenizer's tokens are words.
func (t *Regex) Continues() []bool { return nil }

// Tokenize tokenizes a text.
func (t *Regex) Tokenize(text string) []int32 {
	var out []int32
	for _, word := range t.delimiter.Split(text, -1) {
		if word == "" {
			continue
		}
		if id, ok := t.ids[word]; ok {
			out = append(out, id)
		} else {
			out = append(out, t.unknown)
		}
	}
	return out
}

// Has reports whether ids have a text's (key), as words: one after
// another, the token after them not going on the last word.
func Has(t Tokenizer, ids, key []int32) bool {
	cont := t.Continues()
	for i := 0; i+len(key) <= len(ids); i++ {
		if !equal(ids[i:i+len(key)], key) {
			continue
		}
		if next := i + len(key); next < len(ids) && cont != nil && int(ids[next]) < len(cont) && cont[ids[next]] {
			continue
		}
		return true
	}
	return false
}

func equal(a, b []int32) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Of is the tokenizer a contract's text says, with the vocabulary it
// carries.
func Of(c *contract.Contract) (Tokenizer, error) {
	t := c.Text
	if t == nil {
		return nil, fmt.Errorf("the contract says nothing of a text's tokens (text)")
	}
	vocab, err := c.File(t.Vocabulary)
	if err != nil {
		return nil, fmt.Errorf("the tokenizer's vocabulary: %w", err)
	}
	switch t.Tokenizer {
	case "wordpiece":
		return NewWordPiece(vocab)
	case "regex":
		return NewRegex(t.Delimiter, vocab)
	case "huggingface":
		return NewHuggingFace(vocab)
	case "piper":
		return NewPiper(vocab)
	}
	return nil, fmt.Errorf("text.tokenizer %q is not one axx tokenizes with", t.Tokenizer)
}
