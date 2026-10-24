// Package textenc reads the contracts of text encoders, the models of a
// text's tokens Hugging Face's exporters write, of every format that has
// them (ONNX, Core ML).
package textenc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/tokenize"
)

// A text encoder is a model of a text's tokens (input_ids, with their
// attention_mask and token_type_ids) that is not a decoder, as Hugging
// Face's exporters write them (ONNX, Core ML) and transformers.js runs them:
// a classifier of texts (logits of its labels, its config.json's id2label)
// or an embedder (last_hidden_state, each token's; sentence_embedding, the
// text's). Its tokenizer is the tokenizer.json beside it or in the folder
// above.

// Tokenizer is the key of a text encoder's tokenizer.json in its
// contract's files.
const Tokenizer = "tokenizer"

// Is reports whether a contract is a text encoder's.
func Is(c *contract.Contract) bool {
	return slices.ContainsFunc(c.Inputs, func(t contract.Tensor) bool { return t.Name == "input_ids" }) &&
		!slices.ContainsFunc(c.Inputs, func(t contract.Tensor) bool { return strings.HasPrefix(t.Name, "past") })
}

// Read makes a text encoder's contract, of the model at path, match
// texts with its tokenizer, which it carries, and gives its task: a
// classifier's, of the labels its config.json names, or an embedder's.
// Without a tokenizer.json near it, the contract is left as the model's:
// --vocab gives a WordPiece vocabulary.
func Read(c *contract.Contract, path string) error {
	dirs := []string{filepath.Dir(path), filepath.Dir(filepath.Dir(path))}
	find := func(name string) []byte {
		for _, d := range dirs {
			if b, err := os.ReadFile(filepath.Join(d, name)); err == nil {
				return b
			}
		}
		return nil
	}
	tok := find("tokenizer.json")
	if tok == nil {
		return nil
	}
	if _, err := tokenize.NewHuggingFace(tok); err != nil {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %v", path, err)
	}
	c.Text = &contract.Text{IDs: "input_ids", Tokenizer: "huggingface", Vocabulary: Tokenizer}
	c.Carry(Tokenizer, "tokenizer.json", tok)
	if c.Task != "" {
		return nil
	}
	var cfg struct {
		Labels map[string]string `json:"id2label"`
	}
	_ = json.Unmarshal(find("config.json"), &cfg)
	for i := range c.Outputs {
		out := &c.Outputs[i]
		switch out.Name {
		case "logits":
			n := len(out.Shape)
			labels, ok := labelsByID(cfg.Labels)
			if n == 0 || !ok || int(out.Shape[n-1].Size) != len(labels) {
				continue
			}
			out.Labels, out.Activation, c.Task = labels, "none", "classification"
			return nil
		case "last_hidden_state", "sentence_embedding":
			c.Task = "embedding"
		}
	}
	return nil
}

// labelsByID are a config's labels (id2label), in their ids' order, when
// its ids are numbers.
func labelsByID(byID map[string]string) ([]string, bool) {
	ids := make([]int, 0, len(byID))
	for k := range byID {
		id, err := strconv.Atoi(k)
		if err != nil {
			return nil, false
		}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	labels := make([]string, 0, len(ids))
	for _, id := range ids {
		labels = append(labels, byID[strconv.Itoa(id)])
	}
	return labels, len(labels) > 0
}
