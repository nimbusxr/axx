package lm

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// Ranked is the spec of a byte-level BPE of ranked tokens (tiktoken's, as
// OpenAI's Whisper has them, and whisper.cpp's model files): each token's id
// its rank, split into words as GPT-2's are. Its merges are those the ranks
// imply, in their order: each token's the pair that merging its bytes by the
// lower ranks ends at, as tiktoken merges them. Tokens of no bytes are
// unused; the ids past the tokens' (special tokens, which have no text) are
// control tokens of the names given.
func Ranked(tokens [][]byte, specials []string) Spec {
	spec := Spec{
		Model:        "gpt2",
		Tokens:       make([]string, len(tokens)+len(specials)),
		Types:        make([]int32, len(tokens)+len(specials)),
		PreTokenizer: &PreTokenizer{ByteLevel: true, Splits: []WordSplit{{Regex: gpt2Words}}},
		SplitAt:      []int32{},
	}
	rank := make(map[string]int, len(tokens))
	for i, t := range tokens {
		if len(t) == 0 {
			spec.Tokens[i], spec.Types[i] = "", tokenUnused
			continue
		}
		spec.Tokens[i], spec.Types[i] = byteEncode(string(t)), tokenNormal
		if _, ok := rank[string(t)]; !ok {
			rank[string(t)] = i
		}
	}
	for i, name := range specials {
		id := len(tokens) + i
		spec.Tokens[id], spec.Types[id] = name, tokenControl
	}
	for r, t := range tokens {
		if len(t) < 2 || rank[string(t)] != r {
			continue
		}
		parts := make([]string, len(t))
		for i := range t {
			parts[i] = string(t[i : i+1])
		}
		for len(parts) > 2 {
			best, at := r, -1
			for i := range len(parts) - 1 {
				if k, ok := rank[parts[i]+parts[i+1]]; ok && k < best {
					best, at = k, i
				}
			}
			if at < 0 {
				break
			}
			parts = append(append(parts[:at:at], parts[at]+parts[at+1]), parts[at+2:]...)
		}
		if len(parts) == 2 {
			spec.Merges = append(spec.Merges, byteEncode(parts[0])+" "+byteEncode(parts[1]))
		}
	}
	return spec
}

// Tiktoken reads a tiktoken file's tokens (a token's bytes in base64 and
// its rank, a line each, as OpenAI's Whisper has its vocabularies), by
// their ranks.
func Tiktoken(data []byte) ([][]byte, error) {
	var tokens [][]byte
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		token, rank, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			return nil, fmt.Errorf("its line %d is not a token and its rank", i+1)
		}
		// Of its padding or none (a token of no bytes is "=", as Python's
		// base64 reads it).
		b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(token, "="))
		if err != nil {
			return nil, fmt.Errorf("its line %d's token is not base64: %w", i+1, err)
		}
		r, err := strconv.Atoi(rank)
		if err != nil || r < 0 || r > 1<<20 {
			return nil, fmt.Errorf("its line %d's rank is not a rank", i+1)
		}
		for len(tokens) <= r {
			tokens = append(tokens, nil)
		}
		tokens[r] = b
	}
	return tokens, nil
}
