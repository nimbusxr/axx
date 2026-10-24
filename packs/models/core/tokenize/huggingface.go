package tokenize

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
)

// Variants are the tokenizers whose texts' tokens depend on what is around
// them in the input (BPE's, which may join a word to the space before it
// or the punctuation after it): a text's are its tokens in each context.
type Variants interface {
	Variants(text string) ([][]int32, error)
}

// KeyVariants are a text's tokens wherever an input has it: its variants,
// or its tokens.
func KeyVariants(t Tokenizer, text string) ([][]int32, error) {
	if v, ok := t.(Variants); ok {
		return v.Variants(text)
	}
	return [][]int32{t.Tokenize(text)}, nil
}

// HasAny reports whether ids have a text's tokens in any of their variants.
func HasAny(t Tokenizer, ids []int32, variants [][]int32) bool {
	for _, v := range variants {
		if Has(t, ids, v) {
			return true
		}
	}
	return false
}

// hfFile is what a Hugging Face tokenizer (tokenizer.json) says of its
// model's kind, a WordPiece model's vocabulary and how its text is
// normalized, and its padding.
type hfFile struct {
	Model struct {
		Type     string          `json:"type"`
		Vocab    json.RawMessage `json:"vocab"`
		UnkToken string          `json:"unk_token"`
		Prefix   *string         `json:"continuing_subword_prefix"`
	} `json:"model"`
	Normalizer *struct {
		Type         string `json:"type"`
		Lowercase    bool   `json:"lowercase"`
		StripAccents *bool  `json:"strip_accents"`
	} `json:"normalizer"`
	Padding *struct {
		ID int32 `json:"pad_id"`
	} `json:"padding"`
}

// NewHuggingFace is a Hugging Face tokenizer, of its tokenizer.json: a
// WordPiece model's (BERT's: its text normalized as BertNormalizer does,
// split as BertPreTokenizer does, each word in the vocabulary's pieces), a
// BPE model's (as package lm tokenizes them), whose texts' tokens are
// their variants in the contexts an input may have them in, or a unigram
// model's (XLM-R's, as package lm tokenizes them).
func NewHuggingFace(data []byte) (Tokenizer, error) {
	var f hfFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("the tokenizer is not a Hugging Face tokenizer (tokenizer.json): %w", err)
	}
	switch f.Model.Type {
	case "WordPiece":
		return hfWordPiece(&f)
	case "BPE":
		spec, err := lm.TokenizerJSON(data)
		if err != nil {
			return nil, err
		}
		v, err := lm.NewVocab(spec)
		if err != nil {
			return nil, err
		}
		b := &bpe{v: v, continues: make([]bool, v.Size())}
		for id := range v.Size() {
			r, _ := utf8.DecodeRuneInString(v.Detokenize([]int32{int32(id)}))
			b.continues[id] = unicode.IsLetter(r) || unicode.IsDigit(r)
		}
		switch {
		case f.Padding != nil:
			b.pad = f.Padding.ID
		default:
			for _, name := range []string{"<pad>", "[PAD]", "<|endoftext|>", "</s>"} {
				if id, ok := v.ID(name); ok {
					b.pad = id
					break
				}
			}
		}
		return b, nil
	case "Unigram":
		spec, err := lm.TokenizerJSON(data)
		if err != nil {
			return nil, err
		}
		v, err := lm.NewVocab(spec)
		if err != nil {
			return nil, err
		}
		u := &unigram{v: v, continues: make([]bool, v.Size())}
		for id := range v.Size() {
			r, _ := utf8.DecodeRuneInString(v.Detokenize([]int32{int32(id)}))
			u.continues[id] = unicode.IsLetter(r) || unicode.IsDigit(r)
		}
		switch {
		case f.Padding != nil:
			u.pad = f.Padding.ID
		default:
			if id, ok := v.ID("<pad>"); ok {
				u.pad = id
			}
		}
		return u, nil
	}
	return nil, fmt.Errorf("the tokenizer is a Hugging Face tokenizer of a %s model, which axx does not tokenize with yet: axx has WordPiece models', BPE models' and unigram models'", f.Model.Type)
}

// hfWordPiece is a WordPiece model's tokenizer, as its tokenizer.json says.
func hfWordPiece(f *hfFile) (*WordPiece, error) {
	var vocab map[string]int32
	if err := json.Unmarshal(f.Model.Vocab, &vocab); err != nil {
		return nil, fmt.Errorf("the tokenizer's vocabulary: %w", err)
	}
	if f.Model.Prefix != nil && *f.Model.Prefix != "##" {
		return nil, fmt.Errorf("the tokenizer marks words' later pieces %q, which axx does not tokenize with yet: axx has \"##\"", *f.Model.Prefix)
	}
	size := int32(0)
	for _, id := range vocab {
		size = max(size, id+1)
	}
	w := &WordPiece{ids: vocab, continues: make([]bool, size), lower: func(s string) string { return s }}
	for tok, id := range vocab {
		w.continues[id] = strings.HasPrefix(tok, "##")
	}
	unk := f.Model.UnkToken
	if unk == "" {
		unk = "[UNK]"
	}
	id, ok := vocab[unk]
	if !ok {
		return nil, fmt.Errorf("the tokenizer's vocabulary has no %s, its unknown token", unk)
	}
	w.unknown = id
	if n := f.Normalizer; n != nil {
		if n.Type != "BertNormalizer" {
			return nil, fmt.Errorf("the tokenizer's normalizer %s is not one axx tokenizes with yet: axx has BertNormalizer", n.Type)
		}
		// BERT's: lowercased, and then its accents stripped, unless it says
		// not to.
		strip := n.Lowercase
		if n.StripAccents != nil {
			strip = *n.StripAccents
		}
		w.lower = func(s string) string {
			if n.Lowercase {
				s = strings.ToLower(s)
			}
			if strip {
				var b strings.Builder
				for _, r := range norm.NFD.String(s) {
					if !unicode.Is(unicode.Mn, r) {
						b.WriteRune(r)
					}
				}
				s = b.String()
			}
			return s
		}
	}
	return w, nil
}

// unigram is a unigram model's tokenizer (SentencePiece's: XLM-R's), as
// Hugging Face's tokenizers tokenize: a text's words each on its own, so a
// text's tokens are its tokens anywhere.
type unigram struct {
	v         *lm.Vocab
	pad       int32
	continues []bool
}

func (u *unigram) Tokenize(text string) []int32 { return u.v.Tokenize(text, false) }
func (u *unigram) Pad() int32                   { return u.pad }
func (u *unigram) Size() int                    { return u.v.Size() }

// Continues are the pieces that go on the word before them: those that
// begin with a letter or a digit, not its ▁.
func (u *unigram) Continues() []bool { return u.continues }

// Variants are a text's tokens in the contexts an input may have it in:
// what touches its first and last words joins them.
func (u *unigram) Variants(text string) ([][]int32, error) {
	k, err := lm.KeyTokens(u.v, text)
	if err != nil {
		return nil, err
	}
	return k.Variants, nil
}

// bpe is a BPE model's tokenizer.
type bpe struct {
	v         *lm.Vocab
	pad       int32
	continues []bool
}

func (b *bpe) Tokenize(text string) []int32 { return b.v.Tokenize(text, false) }
func (b *bpe) Pad() int32                   { return b.pad }
func (b *bpe) Size() int                    { return b.v.Size() }

// Continues are the tokens that go on the word before them: those that
// begin with a letter or a digit, not a space.
func (b *bpe) Continues() []bool { return b.continues }

// Variants are a text's tokens in the contexts an input may have it in.
func (b *bpe) Variants(text string) ([][]int32, error) {
	k, err := lm.KeyTokens(b.v, text)
	if err != nil {
		return nil, err
	}
	return k.Variants, nil
}
