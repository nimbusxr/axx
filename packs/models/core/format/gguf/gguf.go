package gguf

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "gguf", Name: "GGUF", Extensions: []string{".gguf"}, Contract: readContract, Write: format.File(write)})
}

// MetadataFile is the contract's file of the model's metadata: its
// tokenizer, chat template and hyperparameters, a GGUF file without
// tensors.
const MetadataFile = "metadata"

// details are a GGUF model's own settings in its contract.
type details struct {
	// Architecture is the model's (general.architecture): llama, qwen2,
	// gemma3...
	Architecture string `json:"architecture"`
	// ContextLength is the context the model was trained for.
	ContextLength int `json:"contextLength"`
	// EndOfGeneration is the token that ends the model's turn, which ends
	// a stub's answers.
	EndOfGeneration int32 `json:"endOfGeneration"`
	// Pooling is an embedding model's (<arch>.pooling_type: 1 mean, 2 the
	// first token's, 3 the last's), and EmbeddingLength its embedding's.
	Pooling         uint32 `json:"pooling,omitempty"`
	EmbeddingLength int    `json:"embeddingLength,omitempty"`
}

// readContract reads a GGUF model's contract: a text generation model of
// tokens in and logits out, whose metadata (tokenizer, chat template,
// hyperparameters) the contract carries.
func readContract(path string) (*contract.Contract, error) {
	f, err := Read(path, false)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model %s", path).
			WithHint("a GGUF model is a file of llama.cpp's format, like model-Q4_K_M.gguf")
	}
	arch := f.String("general.architecture")
	if arch == "" {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s gives no architecture (general.architecture)", path)
	}
	v, err := vocabOf(f)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %v", path, err).
			WithHint("axx stubs GGUF models whose tokenizer is byte-level BPE (Llama 3, Qwen, SmolLM...) or SentencePiece (Llama 2, Mistral, Gemma 3, Phi-3)")
	}
	ctx, _ := f.Uint(arch + ".context_length")
	if ctx == 0 {
		ctx = 4096
	}
	d := details{Architecture: arch, ContextLength: int(ctx), EndOfGeneration: endOfGeneration(f, v)}
	c := &contract.Contract{
		Format:   contract.Format{Type: "gguf", Version: int64(f.Version)},
		Task:     "text-generation",
		Inputs:   []contract.Tensor{{Name: "tokens", Type: "int32", Shape: []contract.Dim{{Name: "sequence"}}}},
		Outputs:  []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "sequence"}, {Size: int64(v.Size())}}}},
		Metadata: map[string]string{},
	}
	// An embedding model pools its tokens' outputs into the text's
	// embedding.
	pooling, _ := f.Uint(arch + ".pooling_type")
	if pooling == 0 {
		// A model of a classifier (cls) ranks, as node-llama-cpp has it, and
		// as llama.cpp's server runs it (--pooling rank).
		for _, t := range f.Tensors {
			if t.Name == "cls.weight" || t.Name == "cls.output.weight" {
				pooling = rankPooling
			}
		}
	}
	switch {
	case pooling >= 1 && pooling <= 3:
		n, _ := f.Uint(arch + ".embedding_length")
		d.Pooling, d.EmbeddingLength = uint32(pooling), int(n)
		c.Task = "embedding"
		c.Outputs = []contract.Tensor{{Name: "embedding", Type: "float32", Shape: []contract.Dim{{Size: int64(n)}}}}
	case pooling == rankPooling:
		if arch == "qwen3vl" {
			return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a reranker of Qwen3-VL's (of pictures too), which axx does not stub yet", path).
				WithHint("axx stubs rerankers of texts: BERT's (bge-reranker, jina-reranker) and Qwen3's")
		}
		d.Pooling = rankPooling
		rankContract(c)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	c.Format.Details = raw
	for _, k := range []string{"general.name", "general.basename", "general.size_label", "general.finetune"} {
		if s := f.String(k); s != "" {
			c.Metadata[k] = s
		}
	}
	meta := &File{Version: Version}
	for _, kv := range f.Meta {
		meta.Set(kv.Key, kv.Value)
	}
	var buf bytes.Buffer
	if err := meta.Write(&buf); err != nil {
		return nil, err
	}
	c.Carry(MetadataFile, "metadata.gguf", buf.Bytes())
	return c, nil
}

// endOfGeneration is the token that ends the model's turn: the end of turn
// its chat template writes, or its end of text.
func endOfGeneration(f *File, v *lm.Vocab) int32 {
	template := f.String("tokenizer.chat_template")
	for _, t := range []string{"<|im_end|>", "<|eot_id|>", "<end_of_turn>", "<|end|>", "<|end_of_text|>", "<turn|>", "</s>"} {
		if id, ok := v.ID(t); ok && strings.Contains(template, t) {
			return id
		}
	}
	if id, ok := f.Uint("tokenizer.ggml.eot_token_id"); ok {
		return int32(id)
	}
	id, _ := f.Uint("tokenizer.ggml.eos_token_id")
	return int32(id)
}

// write writes a text generation stub: a model of llama's architecture with
// the real model's tokenizer, whose weights answer its mappings.
func write(s *stub.Stub) ([]byte, error) {
	if s.Contract != nil && s.Contract.Task == "embedding" {
		return writeEmbedding(s)
	}
	if s.Contract != nil && s.Contract.Task == "classification" {
		return writeRanking(s)
	}
	if s.Matcher != stub.TextMatcher || s.Generated == nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: a GGUF stub generates text (task text-generation) for prompts matched by their text").
			WithHint(`a mapping's input is {"text": ...}, and its output {"text": ...}`)
	}
	c := s.Contract
	var d details
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a GGUF model's: %v", err)
	}
	b, err := c.File(MetadataFile)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no metadata of the model").
			WithHint("write the contract again from the model with `axx models contract <model>.gguf`")
	}
	meta, err := readBytes(b)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "cannot read the model's metadata the contract carries")
	}
	v, err := vocabOf(meta)
	if err != nil {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the model cannot be stubbed: %v", err)
	}
	p, err := lm.NewPlan(s, v, d.EndOfGeneration, d.ContextLength)
	if err != nil {
		return nil, err
	}
	m, err := compile(p)
	if err != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	var tokenizer []KV
	for _, kv := range meta.Meta {
		if strings.HasPrefix(kv.Key, "tokenizer.") {
			tokenizer = append(tokenizer, kv)
		}
	}
	name := "axx stub"
	if n := meta.String("general.name"); n != "" {
		name = n + " (axx stub)"
	}
	var out bytes.Buffer
	if err := m.file(tokenizer, d.ContextLength, name).Write(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// vocabOf is a GGUF model's tokenizer, from its metadata.
func vocabOf(f *File) (*lm.Vocab, error) {
	spec := lm.Spec{
		Model: f.String("tokenizer.ggml.model"), Pre: f.String("tokenizer.ggml.pre"),
		Tokens: f.Strings("tokenizer.ggml.tokens"), Merges: f.Strings("tokenizer.ggml.merges"),
		MetaspaceFirst: f.String("tokenizer.ggml.metaspace_prepend_scheme") == "first",
	}
	if a, ok := f.Get("tokenizer.ggml.token_type").(Array); ok {
		for _, t := range a.Values {
			spec.Types = append(spec.Types, int32(asInt(t)))
		}
	}
	if a, ok := f.Get("tokenizer.ggml.scores").(Array); ok {
		for _, s := range a.Values {
			x, _ := s.(float32)
			spec.Scores = append(spec.Scores, x)
		}
	}
	if b, ok := f.Get("tokenizer.ggml.add_space_prefix").(bool); ok {
		spec.AddSpacePrefix = &b
	}
	if b, ok := f.Get("tokenizer.ggml.normalizer.lowercase").(bool); ok {
		spec.Lowercase = &b
	}
	if b, ok := f.Get("tokenizer.ggml.normalizer.strip_accents").(bool); ok {
		spec.StripAccents = &b
	}
	if id, ok := f.Uint("tokenizer.ggml.unknown_token_id"); ok {
		unk := int32(id)
		spec.Unknown = &unk
	}
	if a, ok := f.Get("tokenizer.ggml.precompiled_charsmap").(Array); ok {
		for _, b := range a.Values {
			spec.PrecompiledCharsmap = append(spec.PrecompiledCharsmap, byte(asInt(b)))
		}
	}
	if b, ok := f.Get("tokenizer.ggml.remove_extra_whitespaces").(bool); ok {
		spec.RemoveExtraSpaces = b
	}
	if b, ok := f.Get("tokenizer.ggml.normalizer.nfc").(bool); ok && b {
		spec.Normalization = "NFC"
	}
	return lm.NewVocab(spec)
}

func asInt(v any) int64 {
	switch v := v.(type) {
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	}
	return 0
}

func readBytes(b []byte) (*File, error) {
	return readFrom(bytes.NewReader(b))
}

// writeEmbedding writes an embedding model's stub: a model of llama's
// architecture with the real model's tokenizer, of the real model's
// embedding's size and pooling, whose every token's output is the mapping's
// embedding.
func writeEmbedding(s *stub.Stub) ([]byte, error) {
	if s.Matcher != stub.TextMatcher {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: an embedding model's stub matches texts").
			WithHint(`a mapping's input is {"text": ...}, and its output {"embedding": [...]}`)
	}
	c := s.Contract
	var d details
	if err := json.Unmarshal(c.Format.Details, &d); err != nil || d.EmbeddingLength == 0 {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a GGUF embedding model's (%v)", err).
			WithHint("write the contract again from the model with `axx models contract <model>.gguf`")
	}
	b, err := c.File(MetadataFile)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no metadata of the model").
			WithHint("write the contract again from the model with `axx models contract <model>.gguf`")
	}
	meta, err := readBytes(b)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "cannot read the model's metadata the contract carries")
	}
	v, err := vocabOf(meta)
	if err != nil {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the model cannot be stubbed: %v", err)
	}
	p, err := lm.NewPlan(s, v, 0, d.ContextLength)
	if err != nil {
		return nil, err
	}
	out := c.Outputs[0].Name
	vectors := make([][]float64, len(s.Answers))
	for i, a := range s.Answers {
		vectors[i] = a[out]
	}
	var beginsWord func(int32) bool
	if v.WordPiece() {
		beginsWord = v.BeginsWord
	}
	m, e, err := compileEmbedding(p, beginsWord, vectors, d.EmbeddingLength)
	if err != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	var tokenizer []KV
	for _, kv := range meta.Meta {
		if strings.HasPrefix(kv.Key, "tokenizer.") {
			tokenizer = append(tokenizer, kv)
		}
	}
	name := "axx stub"
	if n := meta.String("general.name"); n != "" {
		name = n + " (axx stub)"
	}
	var buf bytes.Buffer
	if err := m.embeddingFile(e, tokenizer, d.ContextLength, d.Pooling, name).Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
