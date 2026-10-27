// Package lm is what the formats of language models' stubs share: their
// models' tokenizers (as llama.cpp and SentencePiece tokenize), and a
// stub's plan: its mappings' texts as the tokens a prompt has where it has
// them, its answers' tokens, and the contexts that tell each next token.
// Each format builds its model of a plan its own way.
package lm

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// Plan is a text-generation stub's mappings, in tokens.
type Plan struct {
	// Vocab is how many tokens the vocabulary has.
	Vocab int
	// EOG is the token that ends the answers: the end of the model's turn.
	EOG int32
	// Keys are the keyed mappings' texts, in their order.
	Keys []KeyPattern
	// Answers are the keyed mappings', then the default's: their tokens.
	Answers [][]int32
	// Context is the model's context length.
	Context int
}

// KeyPattern is a mapping's text, in tokens: as it tokenizes in the
// contexts a prompt may have it in (its variants, which differ at its
// start and end), and the runs of tokens every variant has (its core).
type KeyPattern struct {
	Variants [][]int32
	Core     [][]int32
}

// Chunks are the runs of at most k tokens a prompt has where it has the
// text: its first tokens (as any variant has them), its last, and its
// core's, k at a time.
func (p KeyPattern) Chunks(k int) [][][]int32 {
	var heads, tails [][]int32
	long := false
	for _, v := range p.Variants {
		heads = appendNew(heads, v[:min(k, len(v))])
		tails = appendNew(tails, v[max(0, len(v)-k):])
		long = long || len(v) > k
	}
	out := [][][]int32{heads}
	if long {
		out = append(out, tails)
		for _, run := range p.Core {
			for _, part := range Split(run, k) {
				out = append(out, [][]int32{part})
			}
		}
	}
	return out
}

func appendNew(list [][]int32, v []int32) [][]int32 {
	if slices.ContainsFunc(list, func(o []int32) bool { return slices.Equal(o, v) }) {
		return list
	}
	return append(list, v)
}

// Split splits tokens into runs of at most k.
func Split(tokens []int32, k int) [][]int32 {
	var out [][]int32
	for i := 0; i < len(tokens); i += k {
		out = append(out, tokens[i:min(i+k, len(tokens))])
	}
	return out
}

// MaxContext is the most tokens a context of the answers has.
const MaxContext = 16

// AnswerContext is a context of an answer, and the token that follows it.
type AnswerContext struct {
	Mapping int
	// Tokens are the context's, oldest first.
	Tokens []int32
	// Anchored says its first token is the answer's first: the token before
	// it is the prompt's last.
	Anchored bool
	Next     int32
}

// Contexts finds the fewest tokens K (2 at least) for which, in each
// mapping's answer, the last K tokens (or all of them, within the first K)
// tell the next: the answer's tokens, then its end of generation, repeated.
func Contexts(p *Plan) (int, []AnswerContext, error) { return ContextsFrom(p, 2) }

// ContextsFrom is Contexts of from tokens or more: longer contexts, which
// tell the next token as the fewest do, and cut the keys into fewer chunks.
func ContextsFrom(p *Plan, from int) (int, []AnswerContext, error) {
	for k := max(from, 2); k <= MaxContext; k++ {
		var out []AnswerContext
		ok := true
		for i, a := range p.Answers {
			gen := append(slices.Clone(a), slices.Repeat([]int32{p.EOG}, k+1)...)
			seen := map[string]int32{}
			for j := 1; j < len(gen); j++ {
				from := max(0, j-k)
				c := AnswerContext{Mapping: i, Tokens: gen[from:j], Anchored: j < k, Next: gen[j]}
				key := fmt.Sprint(c.Anchored, len(c.Tokens), c.Tokens)
				if prev, dup := seen[key]; dup {
					if prev != c.Next {
						ok = false
						break
					}
					continue
				}
				seen[key] = c.Next
				out = append(out, c)
			}
			if !ok {
				break
			}
		}
		if ok {
			return k, out, nil
		}
	}
	return 0, nil, fmt.Errorf("an answer repeats %d tokens with different text after them; a stub tells its next token from %d at most", MaxContext, MaxContext)
}

// NewPlan turns the stub's texts and answers into tokens of the vocabulary:
// a model of the plan answers with eog after each answer, in a context of
// so many tokens.
func NewPlan(s *stub.Stub, v *Vocab, eog int32, context int) (*Plan, error) {
	p := &Plan{Vocab: v.Size(), EOG: eog, Context: context}
	for i, text := range s.Texts {
		k, err := KeyTokens(v, text)
		if err != nil {
			return nil, mappingError(s, i, "%v", err)
		}
		p.Keys = append(p.Keys, k)
	}
	for i, text := range s.Generated {
		tokens := v.Spell(text)
		if got := v.Detokenize(tokens); got != text {
			return nil, mappingError(s, i, "the model's tokenizer cannot spell its answer: it reads back as %q", got)
		}
		p.Answers = append(p.Answers, tokens)
		// A model reads its answer as it writes it: a mapping before this
		// one whose text the answer has would answer from there on.
		for h := range min(i, len(s.Texts)) {
			if HasWords(text, s.Texts[h]) {
				return nil, mappingError(s, i, "its answer has the text of %q, which comes before it: the model would answer as that mapping once it wrote that text", s.Names[h])
			}
		}
	}
	return p, nil
}

func mappingError(s *stub.Stub, i int, f string, args ...any) error {
	return axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: mapping %q: %s", s.Names[i], fmt.Sprintf(f, args...)).
		WithHint("fix the mapping; the stubs guide shows what a mapping takes: https://axx.nimbusxr.us/guides/stub-embedded-models/")
}

// HasWords reports whether text has key, as words: not inside a longer
// word.
func HasWords(text, key string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], key)
		if j < 0 {
			return false
		}
		at := i + j
		end := at + len(key)
		if (at == 0 || !isWordByte(text[at-1]) || !isWordByte(key[0])) && (end == len(text) || !isWordByte(text[end]) || !isWordByte(key[len(key)-1])) {
			return true
		}
		i = at + 1
	}
}

func isWordByte(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
}

// Contexts a key's text has in a prompt: what may come before it, and after,
// which the tokenizer may join to its first or last word.
var (
	keyBefore = []string{"", " ", "\n", "\"", "'", "(", "[", "*", "-", "“", "‘", ":", ">"}
	keyAfter  = []string{"", " ", "\n", "\n\n", ".", ",", "!", "?", ";", ":", "\"", ")", "”"}
)

// KeyTokens are a text's tokens where a prompt has it: as it tokenizes in
// the contexts a prompt may have it in, which may join what comes before
// it to its first word, or what follows to its last; and the runs of tokens
// inside it every one has.
func KeyTokens(v *Vocab, text string) (KeyPattern, error) {
	if v.WordPiece() {
		// A WordPiece model tokenizes each word on its own, whatever comes
		// before or after it: a text's tokens are its tokens anywhere,
		// and a stub checks the token after them begins another word.
		tokens := v.Tokenize(text, false)
		if len(tokens) == 0 || slices.Contains(tokens, v.unknownToken()) {
			return KeyPattern{}, fmt.Errorf("the model's tokenizer cannot spell its text")
		}
		k := KeyPattern{Variants: [][]int32{tokens}}
		if len(tokens) > 2 {
			k.Core = [][]int32{tokens[1 : len(tokens)-1]}
		}
		return k, nil
	}
	type span struct {
		from, to int
		id       int32
	}
	var variants [][]span
	// The text as the tokens spell it, and what is before and after it.
	text = v.View(text)
	for _, before := range keyBefore {
		for _, after := range keyAfter {
			before, after := v.View(before), v.View(after)
			full := before + text + after
			tokens := v.Tokenize(full, false)
			var spans []span
			pos := 0
			if v.Unigram() && strings.HasPrefix(v.Detokenize(tokens), " ") && !strings.HasPrefix(full, " ") {
				pos = -1 // the ▁ a unigram model puts before a text
			}
			for _, t := range tokens {
				n := len(v.Detokenize([]int32{t}))
				from, to := pos-len(before), pos+n-len(before)
				pos += n
				if to > 0 && from < len(text) {
					spans = append(spans, span{from, to, t})
				}
			}
			if pos != len(full) || len(spans) == 0 {
				continue
			}
			if !slices.ContainsFunc(variants, func(o []span) bool { return slices.Equal(o, spans) }) {
				variants = append(variants, spans)
			}
		}
	}
	if len(variants) == 0 {
		return KeyPattern{}, fmt.Errorf("the model's tokenizer cannot spell its text")
	}
	ids := func(spans []span) []int32 {
		out := make([]int32, len(spans))
		for i, s := range spans {
			out[i] = s.id
		}
		return out
	}
	var k KeyPattern
	for _, sp := range variants {
		k.Variants = append(k.Variants, ids(sp))
	}
	// The core: tokens inside the text that every variant has, in runs of
	// adjacent ones.
	var core []span
	for _, s := range variants[0] {
		if s.from <= 0 || s.to >= len(text) {
			continue
		}
		if !slices.ContainsFunc(variants[1:], func(o []span) bool { return !slices.Contains(o, s) }) {
			core = append(core, s)
		}
	}
	for i := 0; i < len(core); {
		j := i + 1
		for j < len(core) && core[j].from == core[j-1].to {
			j++
		}
		k.Core = append(k.Core, ids(core[i:j]))
		i = j
	}
	return k, nil
}
