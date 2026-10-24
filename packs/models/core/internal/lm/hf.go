package lm

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// gpt2Words is GPT-2's pre-tokenizer's expression: Hugging Face's ByteLevel
// splits text with it when it uses its regex.
const gpt2Words = `'s|'t|'re|'ve|'m|'ll|'d| ?\p{L}+| ?\p{N}+| ?[^\s\p{L}\p{N}]+|\s+(?!\S)`

// PreTokenizer is a BPE model's own pre-tokenizer, which no name in
// llama.cpp's list (Spec.Pre) gives: a Hugging Face tokenizer's.
type PreTokenizer struct {
	// Splits split text into words, each the pieces the ones before it
	// left.
	Splits []WordSplit
	// ByteLevel says words are their bytes, GPT-2's way; otherwise a word's
	// spaces are ▁ (SentencePiece's BPE, as Gemma's tokenizer.json has it).
	ByteLevel bool
	// IgnoreMerges says a word the vocabulary has is its token, merges or
	// not.
	IgnoreMerges bool
	// Bytes says its expressions match the text's bytes, each a character
	// of its own (as C++'s std::regex of a std::string has them: MNN's), not
	// its characters.
	Bytes bool
}

// WordSplit is an expression that splits text into words, and what becomes
// of its matches.
type WordSplit struct {
	Regex    string
	Behavior SplitBehavior
}

// SplitBehavior is what becomes of a split's matches: as Hugging Face's
// tokenizers have it.
type SplitBehavior int

const (
	// Isolated matches are words, and what is between them.
	Isolated SplitBehavior = iota
	// MatchesOnly are the only words: what is between them goes.
	MatchesOnly
	// MergedWithPrevious matches end the words before them.
	MergedWithPrevious
	// MergedWithNext matches begin the words after them.
	MergedWithNext
)

// hfTokenizer is what axx reads of a Hugging Face tokenizer
// (tokenizer.json).
type hfTokenizer struct {
	Model struct {
		Type         string           `json:"type"`
		Vocab        map[string]int32 `json:"vocab"`
		Merges       []json.RawMessage
		ByteFallback bool   `json:"byte_fallback"`
		IgnoreMerges bool   `json:"ignore_merges"`
		UnkToken     string `json:"unk_token"`
		Prefix       string `json:"continuing_subword_prefix"`
		Suffix       string `json:"end_of_word_suffix"`
	} `json:"model"`
	Normalizer   *hfStage `json:"normalizer"`
	PreTokenizer *hfStage `json:"pre_tokenizer"`
	AddedTokens  []struct {
		ID      int32  `json:"id"`
		Content string `json:"content"`
		Special bool   `json:"special"`
	} `json:"added_tokens"`
}

// hfStage is a normalizer or pre-tokenizer: its type, and its settings.
type hfStage struct {
	Type          string     `json:"type"`
	Normalizers   []*hfStage `json:"normalizers"`
	Pretokenizers []*hfStage `json:"pretokenizers"`
	Pattern       struct {
		Regex  *string `json:"Regex"`
		String *string `json:"String"`
	} `json:"pattern"`
	Content          string `json:"content"`
	Prepend          string `json:"prepend"`
	Behavior         string `json:"behavior"`
	Invert           bool   `json:"invert"`
	IndividualDigits bool   `json:"individual_digits"`
	UseRegex         bool   `json:"use_regex"`
	AddPrefixSpace   bool   `json:"add_prefix_space"`
	// A unigram model's: its Precompiled normalizer's character map, and its
	// Metaspace pre-tokenizer's.
	PrecompiledCharsmap string `json:"precompiled_charsmap"`
	Replacement         string `json:"replacement"`
	PrependScheme       string `json:"prepend_scheme"`
	Split               *bool  `json:"split"`
}

// TokenizerJSON is the spec of a Hugging Face tokenizer (tokenizer.json):
// a BPE model's vocabulary, merges and added tokens, its pre-tokenizer's
// expressions (Split, Digits and ByteLevel), and its normalizer (NFC, NFKC,
// spaces as ▁, and a ▁ before each text: Llama 2's).
func TokenizerJSON(data []byte) (Spec, error) {
	var kind struct {
		Model struct {
			Type string `json:"type"`
		} `json:"model"`
	}
	if err := json.Unmarshal(data, &kind); err != nil {
		return Spec{}, fmt.Errorf("its tokenizer is not a Hugging Face tokenizer (tokenizer.json): %w", err)
	}
	if kind.Model.Type == "Unigram" {
		return unigramJSON(data)
	}
	var t hfTokenizer
	if err := json.Unmarshal(data, &t); err != nil {
		return Spec{}, fmt.Errorf("its tokenizer is not a Hugging Face tokenizer (tokenizer.json): %w", err)
	}
	if t.Model.Type != "BPE" {
		return Spec{}, fmt.Errorf("its tokenizer is a Hugging Face tokenizer of a %s model, which axx does not tokenize with yet: axx has BPE models' and unigram models'", t.Model.Type)
	}
	if t.Model.Prefix != "" || t.Model.Suffix != "" {
		return Spec{}, fmt.Errorf("its tokenizer marks words' pieces (%q, %q), which axx does not tokenize with yet", t.Model.Prefix, t.Model.Suffix)
	}
	size := int32(0)
	for _, id := range t.Model.Vocab {
		size = max(size, id+1)
	}
	for _, a := range t.AddedTokens {
		size = max(size, a.ID+1)
	}
	spec := Spec{Model: "gpt2", Tokens: make([]string, size), Types: make([]int32, size)}
	for i := range spec.Types {
		spec.Types[i] = tokenUnused
	}
	for tok, id := range t.Model.Vocab {
		spec.Tokens[id], spec.Types[id] = tok, tokenNormal
		if t.Model.ByteFallback && len(tok) == 6 && strings.HasPrefix(tok, "<0x") && strings.HasSuffix(tok, ">") {
			spec.Types[id] = tokenByte
		}
	}
	if id, ok := t.Model.Vocab[t.Model.UnkToken]; ok && t.Model.UnkToken != "" {
		spec.Types[id] = tokenUnknown
	}
	spec.SplitAt = []int32{}
	for _, a := range t.AddedTokens {
		spec.SplitAt = append(spec.SplitAt, a.ID)
		spec.Tokens[a.ID], spec.Types[a.ID] = a.Content, tokenUserDefined
		if a.Special {
			spec.Types[a.ID] = tokenControl
		}
	}
	for _, m := range t.Model.Merges {
		var s string
		var pair []string
		switch {
		case json.Unmarshal(m, &s) == nil:
		case json.Unmarshal(m, &pair) == nil && len(pair) == 2:
			if strings.Contains(pair[0], " ") || strings.Contains(pair[1], " ") {
				return Spec{}, fmt.Errorf("its tokenizer merges tokens of spaces (%q, %q), which axx does not tokenize with yet", pair[0], pair[1])
			}
			s = pair[0] + " " + pair[1]
		default:
			return Spec{}, fmt.Errorf("its tokenizer's merge %s is not a pair", m)
		}
		spec.Merges = append(spec.Merges, s)
	}

	p := &PreTokenizer{IgnoreMerges: t.Model.IgnoreMerges}
	spaces, prepend := false, false
	var walkN func(n *hfStage) error
	walkN = func(n *hfStage) error {
		switch n.Type {
		case "Sequence":
			for _, s := range n.Normalizers {
				if err := walkN(s); err != nil {
					return err
				}
			}
		case "NFC", "NFKC":
			if spec.Normalization != "" {
				return fmt.Errorf("its tokenizer normalizes text twice (%s, %s), which axx does not tokenize with yet", spec.Normalization, n.Type)
			}
			spec.Normalization = n.Type
		case "Replace":
			if n.Pattern.String == nil || *n.Pattern.String != " " || n.Content != "▁" {
				return fmt.Errorf("its tokenizer's normalizer replaces %s with %q, which axx does not tokenize with yet: axx has spaces' ▁", patternOf(n), n.Content)
			}
			spaces = true
		case "Prepend":
			// A ▁ before each text between special tokens (Llama 2's).
			if n.Prepend != "▁" {
				return fmt.Errorf("its tokenizer's normalizer Prepend puts %q before a text, which axx does not tokenize with yet: axx has ▁", n.Prepend)
			}
			prepend = true
		default:
			return fmt.Errorf("its tokenizer's normalizer %s is not one axx tokenizes with yet: axx has NFC, NFKC, spaces' ▁ and a ▁ before a text", n.Type)
		}
		return nil
	}
	var walkP func(n *hfStage) error
	walkP = func(n *hfStage) error {
		switch n.Type {
		case "Sequence":
			for _, s := range n.Pretokenizers {
				if err := walkP(s); err != nil {
					return err
				}
			}
		case "Split":
			// Removed, inverted, keeps the matches only.
			behaviors := map[string]SplitBehavior{"Isolated": Isolated, "MergedWithPrevious": MergedWithPrevious, "MergedWithNext": MergedWithNext}
			b, ok := behaviors[n.Behavior]
			if n.Behavior == "Removed" && n.Invert {
				b, ok = MatchesOnly, true
			} else if n.Invert {
				ok = false
			}
			if !ok {
				return fmt.Errorf("its tokenizer's pre-tokenizer splits text (%s, inverted %v), which axx does not tokenize with yet: axx has isolated splits, merged ones, and splits that keep their matches only", n.Behavior, n.Invert)
			}
			switch {
			case n.Pattern.Regex != nil:
				p.Splits = append(p.Splits, WordSplit{Regex: *n.Pattern.Regex, Behavior: b})
			case n.Pattern.String != nil:
				p.Splits = append(p.Splits, WordSplit{Regex: regexp.QuoteMeta(*n.Pattern.String), Behavior: b})
			}
		case "Digits":
			if n.IndividualDigits {
				p.Splits = append(p.Splits, WordSplit{Regex: `\p{N}`})
			} else {
				p.Splits = append(p.Splits, WordSplit{Regex: `\p{N}+`})
			}
		case "Metaspace":
			// Spaces as ▁ (Mistral's), one before each text (always), or
			// before the first only (first); each ▁ a word's first, unless
			// it does not split.
			if n.Replacement != "▁" {
				return fmt.Errorf("its tokenizer writes spaces as %q, which axx does not tokenize with yet: axx has ▁", n.Replacement)
			}
			spaces = true
			switch n.PrependScheme {
			case "", "always":
				prepend = true
			case "first":
				spec.MetaspaceFirst = true
			case "never":
			default:
				return fmt.Errorf("its tokenizer's Metaspace puts ▁ before a text %q, which axx does not tokenize with yet", n.PrependScheme)
			}
			if n.Split == nil || *n.Split {
				p.Splits = append(p.Splits, WordSplit{Regex: `▁[^▁]*|[^▁]+`, Behavior: MatchesOnly})
			}
		case "ByteLevel":
			if n.AddPrefixSpace {
				return errors.New("its tokenizer's pre-tokenizer puts a space before a text (ByteLevel's add_prefix_space), which axx does not tokenize with yet")
			}
			p.ByteLevel = true
			if n.UseRegex {
				p.Splits = append(p.Splits, WordSplit{Regex: gpt2Words})
			}
		default:
			return fmt.Errorf("its tokenizer's pre-tokenizer %s is not one axx tokenizes with yet: axx has Split, Digits, Metaspace and ByteLevel", n.Type)
		}
		return nil
	}
	if t.Normalizer != nil {
		if err := walkN(t.Normalizer); err != nil {
			return Spec{}, err
		}
	}
	if t.PreTokenizer != nil {
		if err := walkP(t.PreTokenizer); err != nil {
			return Spec{}, err
		}
	}
	switch {
	case prepend && !spaces:
		return Spec{}, errors.New("its tokenizer's normalizer Prepend puts ▁ before a text whose spaces it does not write as ▁, which axx does not tokenize with yet")
	case prepend && spec.MetaspaceFirst:
		return Spec{}, errors.New("its tokenizer puts ▁ before each text and before its first alone, which axx does not tokenize with")
	case prepend:
		spec.AddSpacePrefix = &prepend
	}
	switch {
	case p.ByteLevel && spaces:
		return Spec{}, errors.New("its tokenizer both byte-encodes text and writes its spaces as ▁, which axx does not tokenize with yet")
	case !p.ByteLevel && !spaces:
		return Spec{}, errors.New("its tokenizer neither byte-encodes text (ByteLevel) nor writes its spaces as ▁, which axx does not tokenize with yet")
	}
	spec.PreTokenizer = p
	return spec, nil
}

func patternOf(n *hfStage) string {
	switch {
	case n.Pattern.String != nil:
		return fmt.Sprintf("%q", *n.Pattern.String)
	case n.Pattern.Regex != nil:
		return fmt.Sprintf("/%s/", *n.Pattern.Regex)
	}
	return "a pattern"
}

// unigramJSON is the spec of a Hugging Face tokenizer of a unigram model
// (XLM-R's, T5's): its pieces and their scores, its added tokens, its
// normalizer's character map (Precompiled) and whether it merges spaces (a
// Replace of " {2,}"), and its Metaspace pre-tokenizer's ▁ before a text.
func unigramJSON(data []byte) (Spec, error) {
	var t struct {
		Model struct {
			UnkID        *int32              `json:"unk_id"`
			Vocab        [][]json.RawMessage `json:"vocab"`
			ByteFallback bool                `json:"byte_fallback"`
		} `json:"model"`
		Normalizer   *hfStage `json:"normalizer"`
		PreTokenizer *hfStage `json:"pre_tokenizer"`
		AddedTokens  []struct {
			ID      int32  `json:"id"`
			Content string `json:"content"`
			Special bool   `json:"special"`
		} `json:"added_tokens"`
	}
	if err := json.Unmarshal(data, &t); err != nil {
		return Spec{}, fmt.Errorf("its tokenizer is not a Hugging Face tokenizer (tokenizer.json): %w", err)
	}
	if t.Model.ByteFallback {
		return Spec{}, errors.New("its unigram tokenizer falls back to bytes, which axx does not tokenize with yet")
	}
	spec := Spec{Model: "t5", HF: true}
	for i, p := range t.Model.Vocab {
		var piece string
		var score float32
		if len(p) != 2 || json.Unmarshal(p[0], &piece) != nil || json.Unmarshal(p[1], &score) != nil {
			return Spec{}, fmt.Errorf("its unigram tokenizer's piece %d is not a piece and its score", i)
		}
		spec.Tokens = append(spec.Tokens, piece)
		spec.Scores = append(spec.Scores, score)
		spec.Types = append(spec.Types, tokenNormal)
	}
	if t.Model.UnkID != nil && int(*t.Model.UnkID) < len(spec.Types) {
		spec.Types[*t.Model.UnkID] = tokenUnknown
		spec.Unknown = t.Model.UnkID
	}
	spec.SplitAt = []int32{}
	for _, a := range t.AddedTokens {
		if int(a.ID) >= len(spec.Tokens) {
			return Spec{}, fmt.Errorf("its added token %q is past its pieces", a.Content)
		}
		spec.SplitAt = append(spec.SplitAt, a.ID)
		if a.Special && spec.Types[a.ID] != tokenUnknown {
			spec.Types[a.ID] = tokenControl
		}
	}
	var walkN func(n *hfStage) error
	walkN = func(n *hfStage) error {
		switch n.Type {
		case "Sequence":
			for _, s := range n.Normalizers {
				if err := walkN(s); err != nil {
					return err
				}
			}
		case "Precompiled":
			if n.PrecompiledCharsmap != "" {
				b, err := base64.StdEncoding.DecodeString(n.PrecompiledCharsmap)
				if err != nil {
					return fmt.Errorf("its tokenizer's character map: %w", err)
				}
				spec.PrecompiledCharsmap = b
			}
		case "Replace":
			if n.Pattern.Regex == nil || *n.Pattern.Regex != " {2,}" || n.Content != " " {
				return fmt.Errorf("its tokenizer's normalizer replaces %s with %q, which axx does not tokenize with yet: axx has spaces merged", patternOf(n), n.Content)
			}
			spec.RemoveExtraSpaces = true
		case "NFKC", "NFC":
			spec.Normalization = n.Type
		default:
			return fmt.Errorf("its tokenizer's normalizer %s is not one axx tokenizes with yet: axx has Precompiled, NFC, NFKC and spaces merged", n.Type)
		}
		return nil
	}
	if t.Normalizer != nil {
		if err := walkN(t.Normalizer); err != nil {
			return Spec{}, err
		}
	}
	var walkP func(n *hfStage) error
	walkP = func(n *hfStage) error {
		switch n.Type {
		case "Sequence":
			for _, s := range n.Pretokenizers {
				if err := walkP(s); err != nil {
					return err
				}
			}
		case "WhitespaceSplit":
			spec.RemoveExtraSpaces = true
		case "Metaspace":
			if n.Replacement != "" && n.Replacement != "\u2581" && n.Replacement != "▁" {
				return fmt.Errorf("its tokenizer writes spaces as %q, which axx does not tokenize with yet: axx has ▁", n.Replacement)
			}
			prepend := n.PrependScheme != "never"
			if n.PrependScheme == "" {
				prepend = n.AddPrefixSpace
			}
			spec.AddSpacePrefix = &prepend
		default:
			return fmt.Errorf("its tokenizer's pre-tokenizer %s is not one axx tokenizes with yet: axx has Metaspace", n.Type)
		}
		return nil
	}
	if t.PreTokenizer == nil {
		return Spec{}, errors.New("its unigram tokenizer has no pre-tokenizer (Metaspace), which axx does not tokenize with yet")
	}
	if err := walkP(t.PreTokenizer); err != nil {
		return Spec{}, err
	}
	return spec, nil
}
