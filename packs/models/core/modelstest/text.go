package modelstest

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/tokenize"
)

// vocabulary is a WordPiece vocabulary of the notes' words.
var vocabulary = []string{
	"[PAD]", "[UNK]", "[CLS]", "[SEP]", ".", ",", ":", "!",
	"please", "leave", "it", "with", "the", "neighbour", "at", "number", "12",
	"careful", "box", "is", "fragile", "ring", "bell", "twice", "glass", "inside",
	"neigh", "##bour", "##s",
}

// TextClassifier is the contract of an ONNX classifier of a delivery note
// (BERT's inputs: its tokens' ids, a mask and segments, of a sequence of 32
// at most): output "logits" of the labels ordinary and careful.
func TextClassifier() *contract.Contract {
	ids := func(name string) contract.Tensor {
		return contract.Tensor{Name: name, Type: "int64", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}}}
	}
	c := &contract.Contract{
		Format: contract.Format{Type: "onnx", IRVersion: 8, Opsets: map[string]int64{"": 13}},
		Task:   "classification",
		Inputs: []contract.Tensor{ids("input_ids"), ids("attention_mask"), ids("token_type_ids")},
		Outputs: []contract.Tensor{{
			Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Size: 2}},
			Labels: []string{"ordinary", "careful"}, Activation: "none",
		}},
		Text: &contract.Text{IDs: "input_ids", Tokenizer: "wordpiece", Vocabulary: "vocabulary"},
	}
	c.Carry("vocabulary", "vocab.txt", []byte(strings.Join(vocabulary, "\n")+"\n"))
	return c
}

// ConformText holds a format's stubs of texts to the reference: for each
// variant, a stub of a note to leave it with the neighbour and of one of a
// fragile box is written, run in the format's runtime on notes that have
// one, both (the first answers), a part of one, and neither; and every
// output is the answer of the first mapping whose tokens the note's have.
// Length is the sequence's (0: the note's tokens, as the app gives an open
// sequence).
func ConformText(t *testing.T, variants []Variant, ext string, write func(*stub.Stub) (format.Model, error), run Runner, length int) {
	t.Helper()
	type check struct {
		variant, note string
		want          int
		stub          *stub.Stub
	}
	var runs []Run
	var checks []check
	for _, v := range variants {
		c := v.Contract()
		answers := [3]map[string]any{
			{"label": "ordinary", "score": 0.7},
			{"label": "careful", "score": 0.95},
			{"label": "ordinary", "score": 0.6},
		}
		if v.Answers != nil {
			answers = *v.Answers
		}
		dir := Dir(t, c,
			map[string]any{"name": "The neighbour", "input": map[string]any{"text": "leave it with the neighbour"}, "output": answers[0]},
			map[string]any{"name": "Fragile", "input": map[string]any{"text": "fragile"}, "output": answers[1]},
			Mapping("Anything else", "", answers[2]),
		)
		s, err := stub.Load(dir)
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		m, err := write(s)
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		model := filepath.Join(dir, "stub"+ext)
		if err := save(m, model); err != nil {
			t.Fatal(err)
		}
		tok, err := tokenize.Of(s.Contract)
		if err != nil {
			t.Fatal(err)
		}
		cls, _ := tok.(*tokenize.WordPiece).ID("[CLS]")
		sep, _ := tok.(*tokenize.WordPiece).ID("[SEP]")
		for _, note := range []struct {
			text string
			want int
		}{
			{"Please leave it with the neighbour at number 12.", 0},
			{"Careful: fragile!", 1},
			{"Fragile, glass inside. Leave it with the neighbour.", 0},
			{"Leave it with the neighbours.", 2},
			{"Ring the bell twice.", 2},
		} {
			ids := append([]int32{cls}, tok.Tokenize(note.text)...)
			ids = append(ids, sep)
			n := len(ids)
			if length > 0 {
				n = length
			}
			feeds := map[string]Tensor{}
			for _, in := range s.Contract.Inputs {
				data := make([]float64, n)
				for i := range min(n, len(ids)) {
					switch in.Name {
					case s.Contract.Text.IDs:
						data[i] = float64(ids[i])
					case "attention_mask", "input_mask":
						data[i] = 1
					}
				}
				dims := []int64{1, int64(n)}
				if !in.Batched() {
					dims = dims[1:]
				}
				feeds[in.Name] = Tensor{Type: string(in.Type), Dims: dims, Data: data}
			}
			runs = append(runs, Run{Model: model, Feeds: feeds})
			checks = append(checks, check{variant: v.Name, note: note.text, want: note.want, stub: s})
			// The reference: the first mapping whose tokens the note's have.
			first := 2
			for k, text := range s.Texts {
				key := tok.Tokenize(text)
				if tokenize.Has(tok, ids, key) {
					first = k
					break
				}
			}
			if first != note.want {
				t.Errorf("%s, %q: the reference answers with %s, want %s", v.Name, note.text, s.Names[first], s.Names[note.want])
			}
		}
	}
	results, err := run(context.Background(), runs)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range checks {
		for _, out := range c.stub.Contract.Outputs {
			got := valuesOf(results[i][out.Name], &out)
			want := out.Type.Decode(out.Type.Encode(c.stub.Answers[c.want][out.Name]))
			if err := equal(got, want, false); err != nil {
				t.Errorf("%s, %q: output %s: %v", c.variant, c.note, out.Name, err)
			}
		}
	}
}
