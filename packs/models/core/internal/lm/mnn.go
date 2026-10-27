package lm

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// MNNTokenizer is an MNN-LLM model's tokenizer, as its tokenizer.txt has it
// (MNN's own format, which its LLM engine reads): the spec of its model, and
// its special tokens, those that stop generation, and those it puts before a
// text.
type MNNTokenizer struct {
	Spec                  Spec
	Special, Stop, Prefix []int32
}

// mnnMagic is a tokenizer.txt's first number.
const mnnMagic = 430

// MNN's tokenizers, by their type's number.
const (
	mnnSentencePiece = 0
	mnnTiktoken      = 1
	mnnBert          = 2
	mnnHuggingFace   = 3
	mnnPipeline      = 4
)

// mnnWords is the expression MNN's Hugging Face tokenizer splits words with
// (C++'s std::regex of a std::string, ignoring case, of its bytes: its
// classes ASCII's): Qwen's and GPT-4's, written for it.
const mnnWords = `(?i:'s|'t|'re|'ve|'m|'ll|'d)|[^\r\nA-Za-z0-9]?[A-Za-z]+|[0-9]| ?[^ \t\n\v\f\rA-Za-z0-9]+[\r\n]*|[ \t\n\v\f\r]*[\r\n]+|[ \t\n\v\f\r]+(?![^ \t\n\v\f\r])|[ \t\n\v\f\r]+`

// ReadMNNTokenizer reads a tokenizer.txt: its first line its magic number
// and its type, its second the numbers of its special, stop and prefix
// tokens, its third their ids; then its type's vocabulary.
func ReadMNNTokenizer(data []byte) (*MNNTokenizer, error) {
	r := bufio.NewReader(bytes.NewReader(data))
	read := 0 // the bytes the lines read
	line := func() (string, error) {
		s, err := r.ReadString('\n')
		if err != nil && s == "" {
			return "", fmt.Errorf("its tokenizer.txt ends early: %w", err)
		}
		read += len(s)
		return strings.TrimSuffix(strings.TrimSuffix(s, "\n"), "\r"), nil
	}
	numbers := func(s string) ([]int, error) {
		var out []int
		for _, f := range strings.Fields(s) {
			n, err := strconv.Atoi(f)
			if err != nil {
				return nil, fmt.Errorf("its tokenizer.txt has %q where a number goes", f)
			}
			out = append(out, n)
		}
		return out, nil
	}
	head, err := line()
	if err != nil {
		return nil, err
	}
	h, err := numbers(head)
	if err != nil || len(h) < 2 || h[0] != mnnMagic {
		return nil, fmt.Errorf("its tokenizer.txt is not MNN's (its first line %q)", head)
	}
	counts, err := line()
	if err != nil {
		return nil, err
	}
	n, err := numbers(counts)
	if err != nil || len(n) != 3 {
		return nil, fmt.Errorf("its tokenizer.txt's counts of special tokens are %q", counts)
	}
	idsLine, err := line()
	if err != nil {
		return nil, err
	}
	ids, err := numbers(idsLine)
	if err != nil || len(ids) < n[0]+n[1]+n[2] {
		return nil, fmt.Errorf("its tokenizer.txt's special tokens are %q", idsLine)
	}
	t := &MNNTokenizer{}
	for i, id := range ids[:n[0]+n[1]+n[2]] {
		if id < 0 || id >= MaxVocab {
			return nil, fmt.Errorf("its tokenizer.txt's special token %d is no token: a vocabulary's IDs are from 0 to %d", id, MaxVocab-1)
		}
		switch {
		case i < n[0]:
			t.Special = append(t.Special, int32(id))
		case i < n[0]+n[1]:
			t.Stop = append(t.Stop, int32(id))
		default:
			t.Prefix = append(t.Prefix, int32(id))
		}
	}
	switch h[1] {
	case mnnHuggingFace:
		sizes, err := line()
		if err != nil {
			return nil, err
		}
		s, err := numbers(sizes)
		if err != nil || len(s) != 2 {
			return nil, fmt.Errorf("its tokenizer.txt's sizes are %q", sizes)
		}
		spec := Spec{Model: "gpt2", PreTokenizer: &PreTokenizer{ByteLevel: true, Bytes: true, Splits: []WordSplit{{Regex: mnnWords}}}}
		for range s[0] {
			tok, err := line()
			if err != nil {
				return nil, err
			}
			spec.Tokens = append(spec.Tokens, tok)
		}
		for range s[1] {
			m, err := line()
			if err != nil {
				return nil, err
			}
			spec.Merges = append(spec.Merges, m)
		}
		t.Spec = special(spec, t.Special)
	case mnnPipeline:
		// The pipeline, in binary, after the header's lines: a Hugging Face
		// tokenizer's, as its tokenizer.json has it.
		doc, err := mtokJSON(data[read:])
		if err != nil {
			return nil, err
		}
		spec, err := TokenizerJSON(doc)
		if err != nil {
			return nil, err
		}
		t.Spec = spec
	case mnnTiktoken:
		// Each token's bytes, in base64, a line each.
		size, err := line()
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(size))
		if err != nil || n < 0 || n > MaxVocab {
			return nil, fmt.Errorf("its tokenizer.txt's size is %q", size)
		}
		spec := Spec{Model: "longest"}
		for range n {
			l, err := line()
			if err != nil {
				return nil, err
			}
			tok, err := base64.StdEncoding.DecodeString(l)
			if err != nil {
				return nil, fmt.Errorf("its tokenizer.txt's token %q is not base64", l)
			}
			spec.Tokens = append(spec.Tokens, string(tok))
		}
		t.Spec = special(spec, t.Special)
	case mnnSentencePiece:
		// Each piece's text, in base64, its score and its type, a line each:
		// SentencePiece's BPE, a ▁ before each text.
		size, err := line()
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(size))
		if err != nil || n < 0 || n > MaxVocab {
			return nil, fmt.Errorf("its tokenizer.txt's size is %q", size)
		}
		prefix := true
		spec := Spec{Model: "llama", AddSpacePrefix: &prefix}
		literal := true
		for range n {
			l, err := line()
			if err != nil {
				return nil, err
			}
			f := strings.Fields(l)
			if len(f) != 3 {
				return nil, fmt.Errorf("its tokenizer.txt's piece %q is not a piece's text, score and type", l)
			}
			piece, err1 := base64.StdEncoding.DecodeString(f[0])
			score, err2 := strconv.ParseFloat(f[1], 32)
			typ, err3 := strconv.ParseInt(f[2], 10, 32)
			if err1 != nil || err2 != nil || err3 != nil {
				return nil, fmt.Errorf("its tokenizer.txt's piece %q is not a piece's text, score and type", l)
			}
			spec.Tokens = append(spec.Tokens, string(piece))
			spec.Scores = append(spec.Scores, float32(score))
			spec.Types = append(spec.Types, int32(typ))
			if strings.Contains(string(piece), "▁") {
				literal = false
			}
		}
		// MNN's export writes ▁ as a space (and its encoder looks for ▁).
		spec.LiteralSpaces = literal
		spec.SplitAt = append([]int32{}, t.Special...)
		t.Spec = spec
	default:
		return nil, fmt.Errorf("its tokenizer.txt is of MNN's tokenizer type %d, which axx does not tokenize with yet", h[1])
	}
	return t, nil
}

// special marks the special tokens control tokens, and the ones text is
// split at first.
func special(spec Spec, ids []int32) Spec {
	spec.Types = make([]int32, len(spec.Tokens))
	for i := range spec.Types {
		spec.Types[i] = tokenNormal
	}
	for _, id := range ids {
		if int(id) < len(spec.Types) {
			spec.Types[id] = tokenControl
		}
	}
	spec.SplitAt = append([]int32{}, ids...)
	return spec
}
