package lm

import (
	"encoding/binary"
	"errors"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"
)

// A unigram model ("t5": SentencePiece's unigram language model, XLM-R's,
// T5's, mBART's) tokenizes as llama.cpp does (llm_tokenizer_ugm): its text
// normalized by its precompiled character map (SentencePiece's, an
// XOR-compressed double array of the prefixes it replaces), its spaces
// escaped (▁), and then split into the pieces of the highest sum of scores
// (Viterbi, a code point at a time; a code point no piece has is the
// unknown token, of the lowest score less 10, and unknown tokens in a row
// are one).

// ugm is a unigram model's tokenizer.
type ugm struct {
	pieces      *trie // the normal, user-defined and unused tokens
	userDefined *trie
	unknownID   int32
	unknown     float64 // the unknown token's score
	xcda        []uint32
	replacement []byte
	// The model's normalizer's: whether it puts a space before a text and
	// merges spaces; and whether it is a Hugging Face tokenizer's.
	addSpacePrefix, removeExtraSpaces, hf bool
}

// trie is a trie of tokens' bytes.
type trie struct {
	children map[byte]*trie
	token    int32 // −1: no token ends here
}

func newTrie() *trie { return &trie{children: map[byte]*trie{}, token: -1} }

func (t *trie) insert(s string, id int32) {
	n := t
	for i := 0; i < len(s); i++ {
		c, ok := n.children[s[i]]
		if !ok {
			c = newTrie()
			n.children[s[i]] = c
		}
		n = c
	}
	n.token = id
}

// newUGM is a unigram model's tokenizer of the vocabulary.
func newUGM(v *Vocab, spec Spec) (*ugm, error) {
	u := &ugm{pieces: newTrie(), userDefined: newTrie(), unknownID: -1, removeExtraSpaces: spec.RemoveExtraSpaces, hf: spec.HF}
	if spec.AddSpacePrefix != nil {
		u.addSpacePrefix = *spec.AddSpacePrefix
	}
	if cm := spec.PrecompiledCharsmap; len(cm) > 0 {
		if len(cm) < 4 {
			return nil, errors.New("its precompiled character map is cut short")
		}
		size := binary.LittleEndian.Uint32(cm)
		if uint64(size)+4 >= uint64(len(cm)) || size%4 != 0 {
			return nil, errors.New("its precompiled character map is cut short")
		}
		u.xcda = make([]uint32, size/4)
		for i := range u.xcda {
			u.xcda[i] = binary.LittleEndian.Uint32(cm[4+4*i:])
		}
		u.replacement = cm[4+size:]
	}
	minScore, maxScore := float32(math.MaxFloat32), float32(-math.MaxFloat32)
	for id, t := range v.tokens {
		typ := v.types[id]
		if typ == tokenNormal {
			minScore, maxScore = min(minScore, v.scores[id]), max(maxScore, v.scores[id])
		}
		if typ == tokenNormal || typ == tokenUserDefined || typ == tokenUnused {
			u.pieces.insert(t, int32(id))
		}
		if typ == tokenUserDefined && !u.hf {
			u.userDefined.insert(t, int32(id))
		}
		if typ == tokenUnknown && u.unknownID < 0 {
			u.unknownID = int32(id)
		}
	}
	if spec.Unknown != nil {
		u.unknownID = *spec.Unknown
	}
	if u.unknownID < 0 {
		u.unknownID = 2 // llama.cpp's default, T5's
	}
	u.unknown = float64(minScore) - 10
	return u, nil
}

// tokenize tokenizes a text without user-defined tokens' (which partition
// takes out first): of a Hugging Face tokenizer's, each word (from a ▁) on
// its own.
func (u *ugm) tokenize(v *Vocab, text string) []int32 {
	if !u.hf {
		return u.viterbi(v, u.normalize(text))
	}
	s := u.normalizeHF(text)
	var out []int32
	for len(s) > 0 {
		end := strings.Index(s[1:], "\u2581")
		if end < 0 {
			end = len(s)
		} else {
			end++
		}
		out = append(out, u.viterbi(v, s[:end])...)
		s = s[end:]
	}
	return out
}

// normalizeHF normalizes a text as a Hugging Face tokenizer's pipeline of a
// unigram model does: its Precompiled normalizer's character map, its
// Replace of spaces' runs, and its Metaspace's ▁s, one before it.
func (u *ugm) normalizeHF(input string) string {
	var b strings.Builder
	for at := 0; at < len(input); {
		repl, n := u.prefix(input, at)
		b.WriteString(repl)
		at += n
	}
	s := b.String()
	if u.removeExtraSpaces {
		s = spaces.ReplaceAllString(s, " ")
	}
	s = strings.ReplaceAll(s, " ", "\u2581")
	if u.addSpacePrefix && s != "" && !strings.HasPrefix(s, "\u2581") {
		s = "\u2581" + s
	}
	return s
}

var spaces = regexp.MustCompile(` {2,}`)

// viterbi is the best tokenization of a normalized text.
func (u *ugm) viterbi(v *Vocab, s string) []int32 {
	if s == "" {
		return nil
	}
	type best struct {
		token int32
		from  int
		score float64
	}
	results := make([]best, len(s)+1)
	for i := range results {
		results[i] = best{u.unknownID, 0, -math.MaxFloat64}
	}
	results[0].score = 0
	for at := 0; at < len(s); {
		width := min(utf8Len(s[at]), len(s)-at)
		whole := false
		current := results[at]
		n := u.pieces.children[s[at]]
		for end := at + 1; end <= len(s) && n != nil; end++ {
			if n.token >= 0 {
				if end-at == width {
					whole = true
				}
				score := float64(v.scores[n.token])
				if v.types[n.token] == tokenUserDefined {
					score = 0
				}
				if c := current.score + score; c > results[end].score {
					results[end] = best{n.token, at, c}
				}
			}
			if end == len(s) {
				break
			}
			n = n.children[s[end]]
		}
		if !whole {
			if c := current.score + u.unknown; c > results[at+width].score {
				results[at+width] = best{u.unknownID, at, c}
			}
		}
		at += width
	}
	var out []int32
	prevUnknown := false
	for r := results[len(s)]; ; r = results[r.from] {
		isUnknown := r.token == u.unknownID
		if !prevUnknown || !isUnknown {
			out = append(out, r.token)
		}
		if r.from == 0 {
			break
		}
		prevUnknown = isUnknown
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// utf8Len is how many bytes the UTF-8 sequence its first byte begins has,
// as llama.cpp reads it (unicode_len_utf8).
func utf8Len(b byte) int {
	return [16]int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 3, 4}[b>>4]
}

// normalize normalizes a text as the model does: each prefix the
// character map has replaced, its spaces escaped, a space put before it and
// merged as its normalizer says.
func (u *ugm) normalize(input string) string {
	var b strings.Builder
	const space = "▁"
	nonSpace, prepended := false, false
	for at := 0; at < len(input); {
		repl, consumed := u.prefix(input, at)
		for i := 0; i < len(repl); i++ {
			c := repl[i]
			if c != ' ' {
				if !nonSpace {
					nonSpace = true
					if (u.addSpacePrefix && !prepended) || u.removeExtraSpaces {
						b.WriteString(space)
						prepended = true
					}
				}
				b.WriteByte(c)
			} else {
				nonSpace = false
				if !u.removeExtraSpaces {
					b.WriteString(space)
				}
			}
		}
		at += consumed
	}
	return b.String()
}

// prefix is the normalization of the text's prefix at: a user-defined
// token's (the longest path the trie of them has, as llama.cpp walks it), as
// it is; or the longest prefix the character map replaces, its replacement;
// or a code point, as it is (or U+FFFD, of a byte that is not UTF-8's).
func (u *ugm) prefix(input string, at int) (string, int) {
	n, depth := u.userDefined, 0
	for at+depth < len(input) {
		c, ok := n.children[input[at+depth]]
		if !ok {
			break
		}
		n = c
		depth++
	}
	if depth > 0 {
		return input[at : at+depth], depth
	}
	if len(u.xcda) > 0 {
		longest, offset := 0, uint32(0)
		node := u.base(0)
		for i := at; i < len(input); i++ {
			c := uint32(input[i])
			if c == 0 {
				break
			}
			node ^= c
			if node >= uint32(len(u.xcda)) || u.xcda[node]&(1<<31|0xff) != c {
				break
			}
			leaf := (u.xcda[node]>>8)&1 == 1
			node ^= u.base(node)
			if leaf {
				if node >= uint32(len(u.xcda)) {
					break
				}
				longest, offset = i-at+1, u.xcda[node]&(1<<31-1)
			}
		}
		if longest > 0 && int(offset) < len(u.replacement) {
			r := u.replacement[offset:]
			if end := strings.IndexByte(string(r), 0); end >= 0 {
				return string(r[:end]), longest
			}
		}
	}
	r, size := utf8.DecodeRuneInString(input[at:])
	if r == utf8.RuneError && size <= 1 {
		return "�", 1
	}
	return input[at : at+size], size
}

// base is an XCDA node's BASE.
func (u *ugm) base(i uint32) uint32 {
	if i >= uint32(len(u.xcda)) {
		return 0
	}
	n := u.xcda[i]
	return (n >> 10) << ((n & (1 << 9)) >> 6)
}
