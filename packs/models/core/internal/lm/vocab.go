package lm

import (
	"container/heap"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/dlclark/regexp2"
	"golang.org/x/text/unicode/norm"
)

// Vocab is a language model's tokenizer, which tokenizes as llama.cpp does
// (llama-vocab.cpp), and as SentencePiece does the BPE models it makes: BPE
// (model "gpt2", split as its pre-tokenizer says; byte-level, but Gemma
// 4's, "gemma4", of raw UTF-8 and its spaces escaped), SentencePiece
// (model "llama"), WordPiece ("bert") and SentencePiece's unigram models
// ("t5": XLM-R's, T5's; ugm.go).
type Vocab struct {
	model, pre string
	tokens     []string
	types      []int32
	scores     []float32
	ids        map[string]int32
	ranks      map[string]int // a BPE merge's rank, by "left right"
	// splits split a BPE model's text into words, as its pre-tokenizer.
	splits       []split
	ignoreMerges bool
	byteEncode   bool
	// splitBytes says the splits match the text's bytes (PreTokenizer.Bytes).
	splitBytes bool
	// literalSpaces says a SentencePiece model's pieces have spaces for ▁
	// (Spec.LiteralSpaces).
	literalSpaces bool
	// escapeSpaces writes a BPE model's spaces as ▁, before its merges.
	escapeSpaces bool
	// addSpacePrefix puts a space before a SentencePiece model's text (and
	// a BPE model's that writes its spaces as ▁: each between special
	// tokens); prefixFirst before such a BPE model's text's start alone.
	addSpacePrefix, prefixFirst bool
	// userDefined are the tokens text is split at first, longest first.
	userDefined []int32
	// normalize normalizes text before it is tokenized, if it is.
	normalize func(string) string
	// A WordPiece model's: whether it lowercases and strips accents, its
	// longest token's length, and its unknown token.
	lowercase, stripAccents bool
	maxTokenLen             int
	unknown                 int32
	// ugm is a unigram model's.
	ugm *ugm
}

// Token types, as GGUF has them.
const (
	tokenNormal      = 1
	tokenUnknown     = 2
	tokenControl     = 3
	tokenUserDefined = 4
	tokenUnused      = 5
	tokenByte        = 6
)

// Spec is what a tokenizer is: as a GGUF model's metadata gives it
// (tokenizer.ggml.*), or a SentencePiece model's.
type Spec struct {
	// Model is "gpt2" (BPE), "gemma4" (BPE of raw UTF-8), "llama"
	// (SentencePiece), "bert" (WordPiece), "t5" (SentencePiece's unigram
	// model) or "longest" (the longest tokens, of bytes: MNN's tiktoken);
	// Pre a BPE model's pre-tokenizer.
	Model, Pre string
	Tokens     []string
	// Types are the tokens' types (1 normal, 2 unknown, 3 control, 4 user
	// defined, 5 unused, 6 byte): normal, when nil.
	Types  []int32
	Scores []float32
	Merges []string
	// AddSpacePrefix says whether SentencePiece puts a space before a
	// text: true, when nil (a unigram model's: false).
	AddSpacePrefix *bool
	// PrecompiledCharsmap is a unigram model's normalizer's character map
	// (SentencePiece's), and RemoveExtraSpaces whether it merges spaces.
	PrecompiledCharsmap []byte
	RemoveExtraSpaces   bool
	// HF says a unigram model is a Hugging Face tokenizer's, which
	// normalizes text as its pipeline does: the character map's, its spaces
	// merged, each ▁, one before it, and each word tokenized on its own.
	HF bool
	// MetaspaceFirst says a BPE model puts ▁ before a text's start, not
	// after its special tokens (Metaspace's prepend_scheme first).
	MetaspaceFirst bool
	// PreTokenizer is a BPE model's own pre-tokenizer, when Pre names none
	// (a Hugging Face tokenizer's).
	PreTokenizer *PreTokenizer
	// Normalization is how text is normalized before it is tokenized: NFC,
	// NFKC, or not at all.
	Normalization string
	// Lowercase and StripAccents are a WordPiece model's normalizer's
	// (tokenizer.ggml.normalizer.*): true, when nil, its accents stripped
	// when it lowercases. Unknown is its unknown token (100, when nil).
	Lowercase, StripAccents *bool
	Unknown                 *int32
	// LiteralSpaces says a SentencePiece model's pieces have spaces where
	// SentencePiece's have ▁ (MNN's export writes them so), while its
	// encoder still writes a text's spaces as ▁: it never merges them, and
	// writes each as its bytes. An answer spelled with its pieces keeps its
	// spaces (Spell).
	LiteralSpaces bool
	// SplitAt are the tokens text is split at first, whatever their type
	// (a Hugging Face tokenizer's added tokens, special ones too); the
	// user-defined ones, when nil (as llama.cpp splits text without special
	// tokens).
	SplitAt []int32
}

// NewVocab is the tokenizer of a spec.
func NewVocab(spec Spec) (*Vocab, error) {
	v := &Vocab{model: spec.Model, pre: spec.Pre, tokens: spec.Tokens}
	if len(v.tokens) == 0 {
		return nil, fmt.Errorf("the model has no tokenizer (its tokens)")
	}
	v.ids = make(map[string]int32, len(v.tokens))
	for i, t := range v.tokens {
		if _, dup := v.ids[t]; !dup {
			v.ids[t] = int32(i)
		}
	}
	v.types = make([]int32, len(v.tokens))
	if len(spec.Types) == len(v.tokens) {
		copy(v.types, spec.Types)
	} else {
		for i := range v.types {
			v.types[i] = tokenNormal
		}
	}
	if spec.SplitAt != nil {
		v.userDefined = slices.Clone(spec.SplitAt)
	} else {
		for i, t := range v.types {
			if t == tokenUserDefined {
				v.userDefined = append(v.userDefined, int32(i))
			}
		}
	}
	slices.SortStableFunc(v.userDefined, func(a, b int32) int { return len(v.tokens[b]) - len(v.tokens[a]) })

	if v.model == "gemma4" {
		v.model, v.pre = "gpt2", "gemma4"
	}
	switch spec.Normalization {
	case "":
	case "NFC":
		v.normalize = norm.NFC.String
	case "NFKC":
		v.normalize = norm.NFKC.String
	default:
		return nil, fmt.Errorf("its tokenizer's normalization %s is not one axx tokenizes with", spec.Normalization)
	}
	switch v.model {
	case "gpt2":
		var p pretokenizer
		var behaviors []SplitBehavior
		if own := spec.PreTokenizer; own != nil && v.pre == "" {
			p = pretokenizer{ignoreMerges: own.IgnoreMerges, byteEncode: own.ByteLevel}
			v.splitBytes = own.Bytes
			for _, s := range own.Splits {
				p.regexes = append(p.regexes, s.Regex)
				behaviors = append(behaviors, s.Behavior)
			}
			v.escapeSpaces = !own.ByteLevel
			v.addSpacePrefix = v.escapeSpaces && spec.AddSpacePrefix != nil && *spec.AddSpacePrefix
			v.prefixFirst = v.escapeSpaces && spec.MetaspaceFirst
		} else {
			pre := v.pre
			if pre == "" {
				pre = "default"
			}
			var ok bool
			if p, ok = pretokenizers[pre]; !ok {
				return nil, fmt.Errorf("its tokenizer's pre-tokenizer %q is not one axx tokenizes with", v.pre)
			}
			if !p.byteEncode && pre != "gemma4" {
				return nil, fmt.Errorf("its tokenizer's pre-tokenizer %q (BPE without byte encoding) is not one axx tokenizes with", v.pre)
			}
			v.escapeSpaces = pre == "gemma4"
		}
		if spec.MetaspaceFirst && !v.prefixFirst {
			return nil, fmt.Errorf("its tokenizer's metaspace (prepend first) is not one axx tokenizes with")
		}
		v.ignoreMerges, v.byteEncode = p.ignoreMerges, p.byteEncode
		for i, expr := range p.regexes {
			re, err := regexp2.Compile(expr, regexp2.None)
			if err != nil {
				return nil, fmt.Errorf("its pre-tokenizer's expression %q: %w", expr, err)
			}
			sp := split{re: re}
			if i < len(behaviors) {
				sp.behavior = behaviors[i]
			}
			v.splits = append(v.splits, sp)
		}
		v.ranks = make(map[string]int, len(spec.Merges))
		for i, m := range spec.Merges {
			if _, dup := v.ranks[m]; !dup {
				v.ranks[m] = i
			}
		}
	case "llama":
		v.literalSpaces = spec.LiteralSpaces
		v.addSpacePrefix = true
		if spec.AddSpacePrefix != nil {
			v.addSpacePrefix = *spec.AddSpacePrefix
		}
		v.scores = make([]float32, len(v.tokens))
		if len(spec.Scores) == len(v.tokens) {
			copy(v.scores, spec.Scores)
		}
	case "longest":
		for _, t := range v.tokens {
			v.maxTokenLen = max(v.maxTokenLen, len(t))
		}
	case "bert":
		v.lowercase = spec.Lowercase == nil || *spec.Lowercase
		v.stripAccents = v.lowercase
		if spec.StripAccents != nil {
			v.stripAccents = *spec.StripAccents
		}
		v.unknown = 100
		if spec.Unknown != nil {
			v.unknown = *spec.Unknown
		}
		for _, t := range v.tokens {
			v.maxTokenLen = max(v.maxTokenLen, len(t))
		}
	case "t5":
		v.scores = make([]float32, len(v.tokens))
		if len(spec.Scores) == len(v.tokens) {
			copy(v.scores, spec.Scores)
		}
		u, err := newUGM(v, spec)
		if err != nil {
			return nil, err
		}
		v.ugm = u
	default:
		return nil, fmt.Errorf("its tokenizer (%q) is not one axx tokenizes with: axx has BPE (gpt2), SentencePiece (llama and t5) and WordPiece (bert)", v.model)
	}
	return v, nil
}

// unknownToken is a WordPiece or unigram model's unknown token.
func (v *Vocab) unknownToken() int32 {
	if v.ugm != nil {
		return v.ugm.unknownID
	}
	return v.unknown
}

// Unigram reports whether the model is SentencePiece's unigram model, whose
// words tokenize each on its own, its first piece's ▁ before it.
func (v *Vocab) Unigram() bool { return v.model == "t5" }

// ID is a token's id, and whether the vocabulary has it.
func (v *Vocab) ID(token string) (int32, bool) {
	id, ok := v.ids[token]
	return id, ok
}

// Size is how many tokens the vocabulary has.
func (v *Vocab) Size() int { return len(v.tokens) }

// Tokenize tokenizes text as llama.cpp does without special tokens
// (llama_tokenize with add_special and parse_special false): user-defined
// tokens are its first, and, for SentencePiece, a space before it when
// spacePrefix and the model adds one.
func (v *Vocab) Tokenize(text string, spacePrefix bool) []int32 {
	if text == "" {
		return nil
	}
	var out []int32
	first := true
	for i, frag := range v.partition(text) {
		if frag.token >= 0 {
			out = append(out, frag.token)
			first = true
			continue
		}
		text := frag.text
		if v.normalize != nil {
			text = v.normalize(text)
		}
		switch v.model {
		case "gpt2":
			t := text
			if spacePrefix && (v.addSpacePrefix && first || v.prefixFirst && i == 0) {
				t = " " + t
			}
			if v.escapeSpaces {
				t = strings.ReplaceAll(t, " ", "\u2581")
			}
			out = append(out, v.bpe(t)...)
		case "llama":
			t := text
			if v.addSpacePrefix && spacePrefix && first {
				t = " " + t
			}
			out = append(out, v.spm(strings.ReplaceAll(t, " ", "▁"))...)
		case "longest":
			out = append(out, v.longest(text)...)
		case "bert":
			out = append(out, v.wpm(text)...)
		case "t5":
			out = append(out, v.ugm.tokenize(v, text)...)
		}
		first = false
	}
	return out
}

// Spell is the tokens that spell text, as a model writes it: its
// tokenizer's (Tokenize, without a space before it), or, of a model whose
// pieces have their spaces (LiteralSpaces), its pieces of the text as it is.
func (v *Vocab) Spell(text string) []int32 {
	if !v.literalSpaces {
		return v.Tokenize(text, false)
	}
	var out []int32
	for _, frag := range v.partition(text) {
		if frag.token >= 0 {
			out = append(out, frag.token)
		} else {
			out = append(out, v.spm(frag.text)...)
		}
	}
	return out
}

// View is text as its tokenizer's tokens spell it (Detokenize): itself, or,
// of a model whose pieces have their spaces (LiteralSpaces), its spaces ▁,
// as its encoder writes them.
func (v *Vocab) View(text string) string {
	if !v.literalSpaces {
		return text
	}
	return strings.ReplaceAll(text, " ", "▁")
}

// longest tokenizes text as the longest tokens it begins with, one after
// another, of its bytes (MNN's tiktoken tokenizer's way: a trie of the
// tokens, without merges). A byte no token begins is left out.
func (v *Vocab) longest(text string) []int32 {
	var out []int32
	for i := 0; i < len(text); {
		n := 0
		for l := min(v.maxTokenLen, len(text)-i); l > 0; l-- {
			if id, ok := v.ids[text[i:i+l]]; ok {
				out = append(out, id)
				n = l
				break
			}
		}
		i += max(n, 1)
	}
	return out
}

// fragment is text, or a user-defined token.
type fragment struct {
	text  string
	token int32
}

// partition splits text at the user-defined tokens, longest first.
func (v *Vocab) partition(text string) []fragment {
	frags := []fragment{{text, -1}}
	for _, id := range v.userDefined {
		tok := v.tokens[id]
		if tok == "" {
			continue
		}
		var next []fragment
		for _, f := range frags {
			if f.token >= 0 {
				next = append(next, f)
				continue
			}
			rest := f.text
			for {
				i := strings.Index(rest, tok)
				if i < 0 {
					if rest != "" {
						next = append(next, fragment{rest, -1})
					}
					break
				}
				if i > 0 {
					next = append(next, fragment{rest[:i], -1})
				}
				next = append(next, fragment{"", id})
				rest = rest[i+len(tok):]
			}
		}
		frags = next
	}
	return frags
}

// split is an expression of a pre-tokenizer, and what becomes of its
// matches.
type split struct {
	re       *regexp2.Regexp
	behavior SplitBehavior
}

// words splits text as the BPE model's pre-tokenizer does: each expression
// splits each piece the ones before it left, its matches words of their own
// with what is between them (or the only words, or merged with the words
// before or after them).
func (v *Vocab) words(text string) []string {
	if v.splitBytes {
		// Each byte a character of its own (Latin-1's), and back.
		runes := make([]rune, len(text))
		for i := range len(text) {
			runes[i] = rune(text[i])
		}
		text = string(runes)
	}
	pieces := []string{text}
	defer func() {
		if v.splitBytes {
			for i, p := range pieces {
				var b []byte
				for _, r := range p {
					b = append(b, byte(r))
				}
				pieces[i] = string(b)
			}
		}
	}()
	for _, sp := range v.splits {
		var next []string
		emit := func(s string) {
			if s != "" {
				next = append(next, s)
			}
		}
		for _, p := range pieces {
			runes := []rune(p)
			start := 0
			pending := "" // a match merged with the word after it
			m, _ := sp.re.FindStringMatch(p)
			for m != nil {
				if m.Length == 0 {
					m, _ = sp.re.FindNextMatch(m)
					continue
				}
				gap, match := string(runes[start:m.Index]), string(runes[m.Index:m.Index+m.Length])
				switch sp.behavior {
				case Isolated:
					emit(gap)
					emit(match)
				case MatchesOnly:
					emit(match)
				case MergedWithPrevious:
					emit(gap + match)
				case MergedWithNext:
					emit(pending + gap)
					pending = match
				}
				start = m.Index + m.Length
				m, _ = sp.re.FindNextMatch(m)
			}
			if sp.behavior != MatchesOnly {
				emit(pending + string(runes[start:]))
			}
		}
		pieces = next
	}
	return pieces
}

// byteRunes is GPT-2's byte encoding: each byte as a printable rune.
var byteRunes = func() [256]rune {
	var out [256]rune
	set := [256]bool{}
	for _, r := range [][2]int{{0x21, 0x7e}, {0xa1, 0xac}, {0xae, 0xff}} {
		for c := r[0]; c <= r[1]; c++ {
			out[c], set[c] = rune(c), true
		}
	}
	n := 0
	for c := range 256 {
		if !set[c] {
			out[c] = rune(256 + n)
			n++
		}
	}
	return out
}()

var runeBytes = func() map[rune]byte {
	m := map[rune]byte{}
	for b, r := range byteRunes {
		m[r] = byte(b)
	}
	return m
}()

// ByteRune is a byte's rune in GPT-2's byte encoding.
func ByteRune(b byte) rune { return byteRunes[b] }

func byteEncode(s string) string {
	var b strings.Builder
	for i := range len(s) {
		b.WriteRune(byteRunes[s[i]])
	}
	return b.String()
}

// symbol is a run of a word's text, merged or not.
type symbol struct {
	text       string
	prev, next int
}

type bigram struct {
	left, right int
	text        string
	rank        int
	score       float32
}

type bigrams struct {
	items []bigram
	less  func(a, b bigram) bool
}

func (q *bigrams) Len() int           { return len(q.items) }
func (q *bigrams) Less(i, j int) bool { return q.less(q.items[i], q.items[j]) }
func (q *bigrams) Swap(i, j int)      { q.items[i], q.items[j] = q.items[j], q.items[i] }
func (q *bigrams) Push(x any)         { q.items = append(q.items, x.(bigram)) }
func (q *bigrams) Pop() any {
	x := q.items[len(q.items)-1]
	q.items = q.items[:len(q.items)-1]
	return x
}

// chars splits text into symbols of a UTF-8 character each.
func chars(text string) []symbol {
	var syms []symbol
	for i := 0; i < len(text); {
		_, n := utf8.DecodeRuneInString(text[i:])
		syms = append(syms, symbol{text: text[i : i+n], prev: len(syms) - 1, next: len(syms) + 1})
		i += n
	}
	if len(syms) > 0 {
		syms[len(syms)-1].next = -1
	}
	return syms
}

// bpe tokenizes a fragment with a BPE model: each word's characters merged
// by the merges' ranks (the lowest first, the leftmost of equal ranks).
func (v *Vocab) bpe(text string) []int32 {
	var out []int32
	for _, w := range v.words(text) {
		if v.byteEncode {
			w = byteEncode(w)
		}
		var syms []symbol
		if id, ok := v.ids[w]; ok && (v.ignoreMerges || v.pre == "gemma4" && strings.Trim(w, "\n") == "") {
			out = append(out, id)
			continue
		}
		syms = chars(w)
		q := &bigrams{less: func(a, b bigram) bool { return a.rank < b.rank || a.rank == b.rank && a.left < b.left }}
		add := func(l, r int) {
			if l < 0 || r < 0 {
				return
			}
			rank, ok := v.ranks[syms[l].text+" "+syms[r].text]
			if !ok {
				return
			}
			heap.Push(q, bigram{left: l, right: r, text: syms[l].text + syms[r].text, rank: rank})
		}
		for i := 1; i < len(syms); i++ {
			add(i-1, i)
		}
		for q.Len() > 0 {
			b := heap.Pop(q).(bigram)
			l, r := &syms[b.left], &syms[b.right]
			if l.text == "" || r.text == "" || l.text+r.text != b.text {
				continue
			}
			l.text += r.text
			r.text = ""
			l.next = r.next
			if r.next >= 0 {
				syms[r.next].prev = b.left
			}
			add(l.prev, b.left)
			add(b.left, l.next)
		}
		for _, s := range syms {
			if s.text == "" {
				continue
			}
			if id, ok := v.ids[s.text]; ok {
				out = append(out, id)
				continue
			}
			for i := range len(s.text) {
				if v.byteEncode {
					if id, ok := v.ids[string(byteRunes[s.text[i]])]; ok {
						out = append(out, id)
					}
				} else if id, ok := v.ids[fmt.Sprintf("<0x%02X>", s.text[i])]; ok {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

// spm tokenizes a fragment, its spaces escaped, with a SentencePiece model:
// its characters merged into the vocabulary's tokens by their scores (the
// highest first, the leftmost of equal scores), and a character no token
// has as its bytes. (Only merges into tokens are made, so every merged
// symbol is a token: llama.cpp's resegmenting by its merges never happens.)
func (v *Vocab) spm(text string) []int32 {
	syms := chars(text)
	q := &bigrams{less: func(a, b bigram) bool { return a.score > b.score || a.score == b.score && a.left < b.left }}
	add := func(l, r int) {
		if l < 0 || r < 0 {
			return
		}
		t := syms[l].text + syms[r].text
		if id, ok := v.ids[t]; ok {
			heap.Push(q, bigram{left: l, right: r, text: t, score: v.scores[id]})
		}
	}
	for i := 1; i < len(syms); i++ {
		add(i-1, i)
	}
	for q.Len() > 0 {
		b := heap.Pop(q).(bigram)
		l, r := &syms[b.left], &syms[b.right]
		if l.text == "" || r.text == "" || len(l.text)+len(r.text) != len(b.text) {
			continue
		}
		l.text += r.text
		r.text = ""
		l.next = r.next
		if r.next >= 0 {
			syms[r.next].prev = b.left
		}
		add(l.prev, b.left)
		add(b.left, l.next)
	}
	var out []int32
	for i := 0; i >= 0 && i < len(syms); i = syms[i].next {
		t := syms[i].text
		if t == "" {
			continue
		}
		if id, ok := v.ids[t]; ok {
			out = append(out, id)
			continue
		}
		for j := range len(t) {
			out = append(out, v.byteToken(t[j]))
		}
	}
	return out
}

// byteToken is a SentencePiece model's token of a byte, <0xXX>.
func (v *Vocab) byteToken(b byte) int32 {
	if id, ok := v.ids[fmt.Sprintf("<0x%02X>", b)]; ok {
		return id
	}
	if id, ok := v.ids[string(rune(b))]; ok {
		return id
	}
	return 0
}

// Detokenize is the text of tokens, as llama.cpp gives their pieces: a BPE
// model's byte-decoded, a SentencePiece model's with its spaces unescaped and
// bytes as bytes.
func (v *Vocab) Detokenize(tokens []int32) string {
	var b strings.Builder
	for _, t := range tokens {
		s := v.tokens[t]
		switch {
		case v.types[t] == tokenControl:
		case v.model == "gpt2" && v.escapeSpaces && v.types[t] == tokenByte:
			var c byte
			if _, err := fmt.Sscanf(s, "<0x%02X>", &c); err == nil {
				b.WriteByte(c)
			}
		case v.model == "gpt2" && v.escapeSpaces:
			b.WriteString(strings.ReplaceAll(s, "\u2581", " "))
		case v.model == "gpt2" && v.types[t] != tokenUserDefined:
			for _, r := range s {
				if c, ok := runeBytes[r]; ok {
					b.WriteByte(c)
				} else {
					b.WriteRune(r)
				}
			}
		case v.model == "llama" && v.types[t] == tokenByte:
			var c byte
			if _, err := fmt.Sscanf(s, "<0x%02X>", &c); err == nil {
				b.WriteByte(c)
			}
		case v.model == "llama" || v.model == "bert" || v.model == "t5":
			b.WriteString(strings.ReplaceAll(s, "▁", " "))
		default:
			b.WriteString(s)
		}
	}
	return b.String()
}

// EndOfTurn is the stop token that ends a model's turn: an end of turn's,
// before an end of text's, of the stop tokens the model's runner stops at.
func EndOfTurn(v *Vocab, stops []int32) int32 {
	for _, turn := range []string{"<end_of_turn>", "<turn|>", "<|im_end|>", "<|eot_id|>", "<|end|>"} {
		if id, ok := v.ID(turn); ok && slices.Contains(stops, id) {
			return id
		}
	}
	return stops[0]
}
