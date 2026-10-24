package tesseract

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "tesseract", Name: "Tesseract", Extensions: []string{".traineddata"}, Write: format.File(Write)})
}

// Write writes a text recognizer's stub as a small, float LSTM traineddata.
// Its image keys use the shared nearest matcher; its answers are whole-word
// unichars written by Tesseract's own CTC decoder. The model's config selects
// raw-line recognition and disables the second, inverted recognition pass.
func Write(s *stub.Stub) ([]byte, error) {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %s", fmt.Sprintf(f, args...))
	}
	if s.Matcher != stub.NearestMatcher {
		return nil, format.Unmatched("Tesseract", s)
	}
	r := s.Nearest.Reducer
	if !r.Image || r.C != 1 {
		return nil, fail("Tesseract's stubs match one grayscale image input")
	}
	texts := make([]string, len(s.Answers))
	for i, answer := range s.Answers {
		text, err := recognitionText(s.Contract, answer)
		if err != nil {
			return nil, fail("%s: %v", s.Names[i], err)
		}
		texts[i] = text
	}
	wordIDs := map[string]int{}
	var words []string
	tokens := make([][]int, len(texts))
	for i, text := range texts {
		fields := strings.Fields(text)
		if strings.Join(fields, " ") != text {
			return nil, fail("%s: Tesseract writes runs of whitespace as one space; the answer %q has other whitespace", s.Names[i], text)
		}
		for j, word := range fields {
			if len(word) == 0 || len(word) > 30 {
				return nil, fail("%s: the word %q is %d bytes; Tesseract's unichars hold 30 bytes", s.Names[i], word, len(word))
			}
			if word == "NULL" || word == "Joined" || word == "|Broken|0|1" {
				return nil, fail("%s: the word %q is one of Tesseract's special unichars", s.Names[i], word)
			}
			id, ok := wordIDs[word]
			if !ok {
				id = len(words) + 3
				wordIDs[word] = id
				words = append(words, word)
			}
			if j > 0 {
				tokens[i] = append(tokens[i], 0)
			}
			tokens[i] = append(tokens[i], id)
		}
	}
	positions := 0
	for _, token := range tokens {
		positions = max(positions, len(token))
	}
	if positions > 0 && 2*positions-1 > r.W {
		return nil, fail("an answer needs %d output positions (words and spaces, with CTC blanks), and the image input has %d", 2*positions-1, r.W)
	}
	if s.Keyed() == 0 {
		return model(sequenceLSTM(r.H, len(words)+3, tokens[len(tokens)-1]), unicharset(words...)), nil
	}
	mappings := make([]imageMapping, s.Keyed())
	for i := range mappings {
		mappings[i] = imageMapping{key: s.Nearest.Table.Keys[i], tokens: tokens[i]}
	}
	lstm := matchingLSTM(r, len(words)+3, mappings, tokens[len(tokens)-1], s.Nearest.Table.Similarity)
	return model(lstm, unicharset(words...)), nil
}

// recognitionText decodes an answer written by the text-recognition task's
// CTC adapter back into its text. Tesseract's output is the recognized text,
// so those are the values its unicharset and answer plan need.
func recognitionText(c *contract.Contract, answer map[string][]float64) (string, error) {
	var out *contract.Tensor
	for i := range c.Outputs {
		if len(c.Outputs[i].Labels) > 0 {
			out = &c.Outputs[i]
			break
		}
	}
	if out == nil {
		return "", fmt.Errorf("the contract has no CTC output with labels")
	}
	blank := slices.Index(out.Labels, "")
	if blank < 0 {
		return "", fmt.Errorf("the output %s has no CTC blank label", out.Name)
	}
	shape := out.Shape
	if out.Batched() {
		shape = shape[1:]
	}
	var dims []int
	for _, d := range shape {
		if d.Size != 1 {
			dims = append(dims, int(d.Size))
		}
	}
	classes := len(out.Labels)
	steps, stepStride, classStride := 0, 0, 0
	switch {
	case len(dims) == 2 && dims[1] == classes:
		steps, stepStride, classStride = dims[0], classes, 1
	case len(dims) == 2 && dims[0] == classes:
		steps, stepStride, classStride = dims[1], 1, dims[1]
	default:
		return "", fmt.Errorf("the output %s is not a CTC [steps, classes] tensor", out.Name)
	}
	values := answer[out.Name]
	if len(values) != steps*classes {
		return "", fmt.Errorf("the answer gives %s %d values, not %d", out.Name, len(values), steps*classes)
	}
	var text strings.Builder
	previous := blank
	for step := range steps {
		best, bestValue := blank, 0.0
		for class := range classes {
			value := values[step*stepStride+class*classStride]
			if class == 0 || value > bestValue {
				best, bestValue = class, value
			}
		}
		if best != blank && (best != previous || previous == blank) {
			text.WriteString(out.Labels[best])
		}
		previous = best
	}
	return text.String(), nil
}
