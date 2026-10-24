package litertlm

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/tflite"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/internal/pb"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "litertlm", Name: "LiteRT-LM", Extensions: []string{".litertlm"}, Contract: readContract, Write: format.File(write)})
}

// The contract's files: the model's LLM metadata (its stop tokens, its
// prompt templates, its sampler), and its tokenizer: SentencePiece's, or
// Hugging Face's (its section as LiteRT-LM has it: the tokenizer.json's
// size, then the tokenizer.json, zlib-compressed).
const (
	metadataFile    = "llm-metadata"
	tokenizerFile   = "tokenizer"
	hfTokenizerFile = "hf-tokenizer"
)

// details are a LiteRT-LM model's own settings in its contract.
type details struct {
	// ContextLength is how many positions its KV cache has.
	ContextLength int `json:"contextLength"`
	// Prefills are its prefill signatures' numbers of tokens.
	Prefills []int `json:"prefills"`
	// EndOfGeneration is the token that ends its turn.
	EndOfGeneration int32 `json:"endOfGeneration"`
	// Version is the LiteRT-LM file's.
	Version [3]uint32 `json:"version"`
}

// readContract reads a LiteRT-LM model's contract: a text generation model,
// whose LLM metadata and tokenizer the contract carries.
func readContract(path string) (*contract.Contract, error) {
	f, err := Read(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model %s", path).
			WithHint("a LiteRT-LM model is a .litertlm file, like gemma3-1b-it-int4.litertlm")
	}
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	meta, tok, hf, model := f.Section(LlmMetadataProto), f.Section(SPTokenizer), f.Section(HFTokenizerZlib), f.Section(TFLiteModel)
	switch {
	case meta == nil:
		return nil, fail("it has no LLM metadata")
	case model == nil:
		return nil, fail("it has no TFLite model")
	case tok == nil && hf == nil:
		return nil, fail("it has no tokenizer")
	}
	var v *lm.Vocab
	if tok != nil {
		v, err = vocabOf(tok.Data)
	} else {
		v, err = hfVocabOf(hf.Data)
	}
	if err != nil {
		return nil, fail("%v", err)
	}
	sigs, err := tflite.Signatures(model.Data)
	if err != nil {
		return nil, fail("its TFLite model: %v", err)
	}
	d := details{Version: [3]uint32{f.Major, f.Minor, f.Patch}}
	vocabSize := v.Size()
	for _, s := range sigs {
		switch {
		case s.Key == "decode":
			for _, in := range s.Inputs {
				if strings.HasPrefix(in.Name, "kv_cache_k_") && len(in.Shape) == 4 {
					d.ContextLength = int(in.Shape[2])
				}
			}
			for _, out := range s.Outputs {
				if out.Name == "logits" && len(out.Shape) > 0 {
					vocabSize = int(out.Shape[len(out.Shape)-1])
				}
			}
		case strings.HasPrefix(s.Key, "prefill"):
			for _, in := range s.Inputs {
				if in.Name == "tokens" && len(in.Shape) == 2 {
					d.Prefills = append(d.Prefills, int(in.Shape[1]))
				}
			}
		}
	}
	if d.ContextLength == 0 || len(d.Prefills) == 0 {
		return nil, fail("its TFLite model has no decode signature of a KV cache (kv_cache_k_0), or no prefill signatures")
	}
	sort.Ints(d.Prefills)
	eog, err := endOfGeneration(meta.Data, v)
	if err != nil {
		return nil, fail("%v", err)
	}
	d.EndOfGeneration = eog
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	c := &contract.Contract{
		Format:  contract.Format{Type: "litertlm", Details: raw},
		Task:    "text-generation",
		Inputs:  []contract.Tensor{{Name: "tokens", Type: "int32", Shape: []contract.Dim{{Size: 1}, {Name: "sequence"}}}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 1}, {Size: int64(vocabSize)}}}},
	}
	c.Carry(metadataFile, "llm-metadata.pb", meta.Data)
	if tok != nil {
		c.Carry(tokenizerFile, "tokenizer.spiece", tok.Data)
	} else {
		c.Carry(hfTokenizerFile, "tokenizer.json.zlib", hf.Data)
	}
	return c, nil
}

// hfVocabOf is the tokenizer of a Hugging Face tokenizer's section: the
// tokenizer.json's size (8 bytes, little-endian), then the tokenizer.json,
// zlib-compressed.
func hfVocabOf(section []byte) (*lm.Vocab, error) {
	if len(section) < 8 {
		return nil, errors.New("its Hugging Face tokenizer's section is too short")
	}
	size := binary.LittleEndian.Uint64(section)
	r, err := zlib.NewReader(bytes.NewReader(section[8:]))
	if err != nil {
		return nil, fmt.Errorf("its Hugging Face tokenizer is not zlib-compressed: %w", err)
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, int64(min(size, 1<<30))+1))
	if err != nil {
		return nil, fmt.Errorf("its Hugging Face tokenizer: %w", err)
	}
	if uint64(len(data)) != size {
		return nil, fmt.Errorf("its Hugging Face tokenizer is %d bytes, not the %d its section says", len(data), size)
	}
	spec, err := lm.TokenizerJSON(data)
	if err != nil {
		return nil, err
	}
	return lm.NewVocab(spec)
}

// endOfGeneration is the stop token that ends the model's turn (an end of
// turn's, before an end of text's), from its LLM metadata.
func endOfGeneration(meta []byte, v *lm.Vocab) (int32, error) {
	var stops []int32
	err := pb.Fields(meta, func(f protowire.Number, t protowire.Type, _ uint64, data []byte) error {
		if f != 2 || t != protowire.BytesType { // stop_tokens
			return nil
		}
		return pb.Fields(data, func(f protowire.Number, t protowire.Type, _ uint64, data []byte) error {
			switch f {
			case 1: // token_ids
				return pb.Fields(data, func(_ protowire.Number, t protowire.Type, v uint64, data []byte) error {
					for _, id := range pb.Varints(t, v, data) {
						stops = append(stops, int32(id))
					}
					return nil
				})
			case 2: // token_str
				if id, ok := v.ID(string(data)); ok {
					stops = append(stops, id)
				}
			}
			return nil
		})
	})
	if err != nil {
		return 0, fmt.Errorf("its LLM metadata: %w", err)
	}
	if len(stops) == 0 {
		return 0, errors.New("its LLM metadata has no stop tokens")
	}
	return lm.EndOfTurn(v, stops), nil
}

// vocabOf is the tokenizer of a SentencePiece model (its ModelProto): its
// pieces, their scores and types, and whether it puts a space before a
// text. axx tokenizes with SentencePiece's BPE models (Gemma's).
func vocabOf(model []byte) (*lm.Vocab, error) {
	spec := lm.Spec{Model: "llama"}
	bpe := false
	addPrefix := true
	err := pb.Fields(model, func(f protowire.Number, t protowire.Type, _ uint64, data []byte) error {
		switch f {
		case 1: // pieces
			piece, score, kind := "", float32(0), int32(1)
			if err := pb.Fields(data, func(f protowire.Number, _ protowire.Type, v uint64, data []byte) error {
				switch f {
				case 1:
					piece = string(data)
				case 2:
					score = math.Float32frombits(uint32(v))
				case 3:
					kind = int32(v)
				}
				return nil
			}); err != nil {
				return err
			}
			spec.Tokens = append(spec.Tokens, piece)
			spec.Scores = append(spec.Scores, score)
			spec.Types = append(spec.Types, kind)
		case 2: // trainer_spec
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, v uint64, _ []byte) error {
				if f == 3 { // model_type
					bpe = v == 2
				}
				return nil
			})
		case 3: // normalizer_spec
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, v uint64, _ []byte) error {
				if f == 3 { // add_dummy_prefix
					addPrefix = v != 0
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("its tokenizer is not a SentencePiece model: %w", err)
	}
	if !bpe {
		return nil, errors.New("its tokenizer is a SentencePiece model of unigrams, which axx does not tokenize with yet: axx has SentencePiece's BPE models (Gemma's)")
	}
	spec.AddSpacePrefix = &addPrefix
	return lm.NewVocab(spec)
}

// write writes a text generation stub as a LiteRT-LM model: its TFLite
// model a generator (tflite.Generator) of the real model's context and
// prefill signatures, and the real model's LLM metadata and tokenizer.
func write(s *stub.Stub) ([]byte, error) {
	if s.Matcher != stub.TextMatcher || s.Generated == nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: a LiteRT-LM stub generates text (task text-generation) for prompts matched by their text").
			WithHint(`a mapping's input is {"text": ...}, and its output {"text": ...}`)
	}
	c := s.Contract
	var d details
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a LiteRT-LM model's: %v", err)
	}
	meta, err := c.File(metadataFile)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no LLM metadata of the model").
			WithHint("write the contract again from the model with `axx models contract <model>.litertlm`")
	}
	tokenizer := Section{Type: SPTokenizer}
	var v *lm.Vocab
	if tok, err := c.File(tokenizerFile); err == nil {
		tokenizer.Data = tok
		if v, err = vocabOf(tok); err != nil {
			return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the model cannot be stubbed: %v", err)
		}
	} else if hf, hfErr := c.File(hfTokenizerFile); hfErr == nil {
		tokenizer = Section{Type: HFTokenizerZlib, Data: hf}
		if v, err = hfVocabOf(hf); err != nil {
			return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the model cannot be stubbed: %v", err)
		}
	} else {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no tokenizer of the model")
	}
	p, err := lm.NewPlan(s, v, d.EndOfGeneration, d.ContextLength)
	if err != nil {
		return nil, err
	}
	if size := c.Outputs[0].Shape[len(c.Outputs[0].Shape)-1].Size; size > 0 {
		p.Vocab = int(size)
	}
	model, err := tflite.WriteGenerator(&tflite.Generator{Plan: p, Context: d.ContextLength, Prefills: d.Prefills})
	if err != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	version := d.Version
	if version[0] == 0 {
		version = [3]uint32{1, 6, 0}
	}
	f := &File{
		Major: version[0], Minor: version[1], Patch: version[2],
		System: []KV{{"Authors", "axx"}},
		Sections: []Section{
			{Type: LlmMetadataProto, Data: meta},
			{Type: TFLiteModel, Items: []KV{{"model_type", "tf_lite_prefill_decode"}}, Data: model},
			tokenizer,
		},
	}
	var out bytes.Buffer
	if err := f.Write(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
