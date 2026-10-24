package lm

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// A WordPiece model's tokenizer (llama.cpp's "bert", BERT's and its
// embedding models'), as llama.cpp's llm_tokenizer_wpm_session tokenizes:
// the text normalized (its accents stripped, lowercased, as the model's
// metadata says), split into words at whitespace and at each punctuation
// mark (each a word of its own); each word, after ▁, the longest tokens
// that spell it from its start, or the unknown token when they do not.

// wpm tokenizes a text as llama.cpp's WordPiece does.
func (v *Vocab) wpm(text string) []int32 {
	var out []int32
	for _, word := range wpmWords(text, v.lowercase, v.stripAccents) {
		w := "▁" + word
		start := len(out)
		for i := 0; i < len(w); {
			match := false
			for j := min(len(w), i+v.maxTokenLen+1); j > i; j-- {
				if id, ok := v.ids[w[i:j]]; ok {
					out = append(out, id)
					match = true
					i = j
					break
				}
			}
			if !match {
				out = out[:start]
				break
			}
		}
		if len(out) == start {
			out = append(out, v.unknown)
		}
	}
	return out
}

// wpmWords are a text's words, as llama.cpp's WordPiece splits them.
func wpmWords(text string, lowercase, stripAccents bool) []string {
	if stripAccents {
		text = norm.NFD.String(text)
	}
	words := []string{""}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			if words[len(words)-1] != "" {
				words = append(words, "")
			}
			continue
		case r == 0 || r == unicode.ReplacementChar || unicode.In(r, unicode.C):
			continue
		case stripAccents && unicode.In(r, unicode.M):
			continue
		}
		s := r
		if lowercase {
			s = unicode.ToLower(r)
		}
		if unicode.IsPunct(r) || r < 0x7F && unicode.IsSymbol(r) || isChineseChar(r) {
			if words[len(words)-1] != "" {
				words = append(words, "")
			}
			words[len(words)-1] = string(s)
			words = append(words, "")
		} else {
			words[len(words)-1] += string(s)
		}
	}
	if words[len(words)-1] == "" {
		words = words[:len(words)-1]
	}
	return words
}

// isChineseChar is llama.cpp's (and BERT's) CJK ideographs' ranges.
func isChineseChar(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF || r >= 0x3400 && r <= 0x4DBF || r >= 0x20000 && r <= 0x2A6DF ||
		r >= 0x2A700 && r <= 0x2B73F || r >= 0x2B740 && r <= 0x2B81F || r >= 0x2B920 && r <= 0x2CEAF ||
		r >= 0xF900 && r <= 0xFAFF || r >= 0x2F800 && r <= 0x2FA1F
}

// BeginsWord reports whether a token begins a word, as a WordPiece model's
// do (▁ and its text) and its special tokens ([CLS], [SEP]) stand alone; a
// token that continues one does not.
func (v *Vocab) BeginsWord(t int32) bool {
	if t < 0 || int(t) >= len(v.tokens) {
		return false
	}
	return strings.HasPrefix(v.tokens[t], "▁") || v.types[t] != tokenNormal
}

// WordPiece reports whether the vocabulary is a WordPiece model's: its
// keys are whole words, a word's tokens followed by one that begins
// another (BeginsWord).
func (v *Vocab) WordPiece() bool { return v.model == "bert" }
