package lm

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
)

// mtok reads an MNN tokenizer's pipeline (a tokenizer.mtok's binary part,
// MNN's form of a Hugging Face tokenizer): its integers little-endian, its
// strings of a 16-bit length.
type mtok struct {
	b   []byte
	pos int
	err error
}

func (r *mtok) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if r.pos+n > len(r.b) {
		r.err = errors.New("its tokenizer.mtok ends early")
		return nil
	}
	out := r.b[r.pos : r.pos+n]
	r.pos += n
	return out
}

func (r *mtok) u8() uint8 {
	if b := r.take(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *mtok) flag() bool { return r.u8() != 0 }

func (r *mtok) u16() uint16 {
	if b := r.take(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}

func (r *mtok) u32() uint32 {
	if b := r.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

func (r *mtok) f64() float64 {
	if b := r.take(8); b != nil {
		return math.Float64frombits(binary.LittleEndian.Uint64(b))
	}
	return 0
}

func (r *mtok) str() string { return string(r.take(int(r.u16()))) }

// normalizer is a normalizer, as tokenizer.json has it (nil: none).
func (r *mtok) normalizer() map[string]any {
	switch t := r.u8(); t {
	case 0:
		return nil
	case 1:
		return map[string]any{"type": "NFKC"}
	case 2:
		return map[string]any{"type": "Prepend", "prepend": r.str()}
	case 3:
		pattern := r.str()
		return map[string]any{"type": "Replace", "pattern": map[string]any{"String": pattern}, "content": r.str()}
	case 4:
		n := r.u32()
		var seq []any
		for range n {
			if c := r.normalizer(); c != nil {
				seq = append(seq, c)
			}
		}
		return map[string]any{"type": "Sequence", "normalizers": seq}
	case 5, 7:
		clean, chinese, strip, lower := r.flag(), r.flag(), r.flag(), r.flag()
		if t == 7 && strip {
			r.table()
		}
		return map[string]any{"type": "BertNormalizer", "clean_text": clean, "handle_chinese_chars": chinese, "strip_accents": strip, "lowercase": lower}
	case 6:
		r.table()
		return map[string]any{"type": "NFKC"}
	case 8:
		left, right := r.flag(), r.flag()
		return map[string]any{"type": "Strip", "strip_left": left, "strip_right": right}
	default:
		r.err = fmt.Errorf("its tokenizer.mtok has a normalizer of type %d, which axx does not read", t)
		return nil
	}
}

// table skips a normalizer's table of code points' replacements.
func (r *mtok) table() {
	n := r.u32()
	for range n {
		r.u32()
		r.take(int(r.u16()))
	}
}

// preTokenizer is a pre-tokenizer, as tokenizer.json has it (nil: none).
func (r *mtok) preTokenizer() map[string]any {
	switch t := r.u8(); t {
	case 0:
		return nil
	case 1:
		return map[string]any{"type": "ByteLevel", "add_prefix_space": false, "trim_offsets": true, "use_regex": r.flag()}
	case 2:
		return map[string]any{"type": "Digits", "individual_digits": r.flag()}
	case 3:
		replacement := r.str()
		scheme := "never"
		if r.flag() {
			scheme = "always"
		}
		return map[string]any{"type": "Metaspace", "replacement": replacement, "prepend_scheme": scheme, "split": true}
	case 4:
		pattern, invert, behavior := r.str(), r.flag(), r.u8()
		b := map[uint8]string{0: "Isolated", 2: "MergedWithPrevious"}[behavior]
		if b == "" {
			b = "Removed"
		}
		return map[string]any{"type": "Split", "pattern": map[string]any{"Regex": pattern}, "behavior": b, "invert": invert}
	case 5:
		return map[string]any{"type": "BertPreTokenizer"}
	case 6:
		n := r.u32()
		var seq []any
		for range n {
			if c := r.preTokenizer(); c != nil {
				seq = append(seq, c)
			}
		}
		return map[string]any{"type": "Sequence", "pretokenizers": seq}
	default:
		r.err = fmt.Errorf("its tokenizer.mtok has a pre-tokenizer of type %d, which axx does not read", t)
		return nil
	}
}

// model is its model, as tokenizer.json has it: BPE's (its merges by their
// ranks), WordPiece's or Unigram's.
func (r *mtok) model() map[string]any {
	t, size := r.u8(), int(r.u32())
	switch t {
	case 0:
		fallback, _, merges := r.flag(), r.flag(), int(r.u32())
		tokens := make([]string, size)
		vocab := map[string]int{}
		for range size {
			s, id := r.str(), int(r.u32())
			if id < size {
				tokens[id] = s
			}
			vocab[s] = id
		}
		type merge struct{ a, b, rank int }
		ms := make([]merge, merges)
		for i := range ms {
			ms[i] = merge{int(r.u32()), int(r.u32()), int(r.u32())}
		}
		sort.SliceStable(ms, func(i, j int) bool { return ms[i].rank < ms[j].rank })
		pairs := make([][2]string, 0, len(ms))
		for _, m := range ms {
			if m.a < size && m.b < size {
				pairs = append(pairs, [2]string{tokens[m.a], tokens[m.b]})
			}
		}
		return map[string]any{"type": "BPE", "vocab": vocab, "merges": pairs, "byte_fallback": fallback, "dropout": nil, "unk_token": nil, "fuse_unk": false}
	case 1:
		unk, prefix, maxChars := r.str(), r.str(), r.u32()
		vocab := map[string]int{}
		for range size {
			s, id := r.str(), int(r.u32())
			vocab[s] = id
		}
		return map[string]any{"type": "WordPiece", "unk_token": unk, "continuing_subword_prefix": prefix, "max_input_chars_per_word": maxChars, "vocab": vocab}
	case 2:
		unk, fallback := r.u32(), r.flag()
		vocab := make([][2]any, size)
		for range size {
			s, id, score := r.str(), int(r.u32()), r.f64()
			if id < size {
				vocab[id] = [2]any{s, score}
			}
		}
		return map[string]any{"type": "Unigram", "unk_id": unk, "byte_fallback": fallback, "vocab": vocab}
	default:
		r.err = fmt.Errorf("its tokenizer.mtok has a model of type %d, which axx does not read", t)
		return nil
	}
}

// decoder is its decoder, as tokenizer.json has it (nil: none).
func (r *mtok) decoder() map[string]any {
	switch t := r.u8(); t {
	case 0:
		return map[string]any{"type": "ByteLevel"}
	case 1:
		return map[string]any{"type": "ByteFallback"}
	case 2:
		replacement := r.str()
		scheme := "never"
		if r.flag() {
			scheme = "always"
		}
		return map[string]any{"type": "Metaspace", "replacement": replacement, "prepend_scheme": scheme, "split": true}
	case 3:
		prefix := r.str()
		return map[string]any{"type": "WordPiece", "prefix": prefix, "cleanup": r.flag()}
	case 4:
		return map[string]any{"type": "Fuse"}
	case 5:
		pattern := r.str()
		return map[string]any{"type": "Replace", "pattern": map[string]any{"String": pattern}, "content": r.str()}
	case 6:
		content := r.str()
		return map[string]any{"type": "Strip", "content": content, "start": r.u32(), "stop": r.u32()}
	case 7:
		n := r.u32()
		var seq []any
		for range n {
			if c := r.decoder(); c != nil {
				seq = append(seq, c)
			}
		}
		return map[string]any{"type": "Sequence", "decoders": seq}
	default:
		r.err = fmt.Errorf("its tokenizer.mtok has a decoder of type %d, which axx does not read", t)
		return nil
	}
}

// mtokJSON is the tokenizer.json of a tokenizer.mtok's pipeline: its
// normalizer, pre-tokenizer, model, decoder and added tokens.
func mtokJSON(b []byte) ([]byte, error) {
	r := &mtok{b: b}
	doc := map[string]any{"version": "1.0"}
	doc["normalizer"] = r.normalizer()
	doc["pre_tokenizer"] = r.preTokenizer()
	doc["model"] = r.model()
	doc["decoder"] = r.decoder()
	n := r.u32()
	var added []any
	for range n {
		id, special, lstrip, rstrip := r.u32(), r.flag(), r.flag(), r.flag()
		added = append(added, map[string]any{"id": id, "content": r.str(), "special": special, "lstrip": lstrip, "rstrip": rstrip, "normalized": !special, "single_word": false})
	}
	doc["added_tokens"] = added
	if r.err != nil {
		return nil, r.err
	}
	return json.Marshal(doc)
}
