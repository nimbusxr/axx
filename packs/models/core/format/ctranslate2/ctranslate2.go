// Package ctranslate2 reads CTranslate2 models' contracts (model.bin, in a
// folder of its config.json, vocabulary and tokenizer: faster-whisper's
// speech recognizers) and writes their stubs: a Whisper's weights of the
// real model's sizes (package whisper), in the variables CTranslate2's
// Whisper spec has (its attention's projections fused, as its converter
// fuses them), its scalars the real model's. The folder's other files stay.
package ctranslate2

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/internal/whisper"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "ctranslate2", Name: "CTranslate2", Extensions: []string{".bin"}, Sniff: isModelBin, Contract: ReadContract, Write: format.File(Write)})
}

// tokenizerFile is the name the contract carries the model's tokenizer as.
const tokenizerFile = "tokenizer"

// multilingualVocab is the smallest vocabulary of a Whisper of many
// languages (an English-only one's is 51864).
const multilingualVocab = 51865

// details are a CTranslate2 model's own settings in its contract: its
// spec's, its scalars (which a stub's are), and a Whisper's.
type details struct {
	Spec     string           `json:"spec"`
	Revision uint32           `json:"revision"`
	Scalars  map[string]value `json:"scalars"`
	// Half is whether the model's matrices are of half precision (float16,
	// as faster-whisper's models are): a stub's are too, so that it is the
	// model's size.
	Half bool `json:"half,omitempty"`
	whisper.Details
}

// value is a scalar variable's: its type and value.
type value struct {
	Type  uint8 `json:"type"`
	Value int64 `json:"value"`
}

// isModelBin reports whether the file is a model.bin of CTranslate2's: of
// its binary version, then its spec's name.
func isModelBin(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 8)
	if _, err := f.Read(b); err != nil {
		return false
	}
	v := binary.LittleEndian.Uint32(b)
	return v >= 5 && v <= binaryVersion && binary.LittleEndian.Uint16(b[4:]) < 256
}

// ReadContract reads the contract of a CTranslate2 model (its model.bin, or
// its folder): of a Whisper (faster-whisper's), a speech recognizer's, of
// its sizes (its variables'), its transcripts' tokens (its tokenizer's) and
// the tokens it suppresses (its config's).
func ReadContract(path string) (*contract.Contract, error) {
	dir := path
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		dir = filepath.Dir(path)
	} else {
		path = filepath.Join(path, "model.bin")
	}
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	h, err := readHeader(path)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a CTranslate2 model axx reads: %v", path, err).
			WithHint("a CTranslate2 model is a folder of model.bin, its config.json and its vocabulary, as its converters write it")
	}
	if h.Spec != "WhisperSpec" {
		return nil, fail("it is a %s, and axx stubs CTranslate2's Whisper models (faster-whisper's)", h.Spec)
	}
	d := details{Spec: h.Spec, Revision: h.Revision, Scalars: map[string]value{}}
	for _, v := range h.Vars {
		if len(v.Dims) == 0 {
			x, err := v.scalar()
			if err != nil {
				return nil, fail("its variable %s: %v", v.Name, err)
			}
			d.Scalars[v.Name] = value{Type: v.Type, Value: x}
		}
	}
	dims := func(name string) []int {
		if v, ok := h.variable(name); ok {
			return v.Dims
		}
		return nil
	}
	conv1, embed := dims("encoder/conv1/weight"), dims("decoder/embeddings/weight")
	if v, ok := h.variable("decoder/embeddings/weight"); ok {
		d.Half = v.Type == typeFloat16
	}
	encPos, decPos := dims("encoder/position_encodings/encodings"), dims("decoder/position_encodings/encodings")
	encFFN, decFFN := dims("encoder/layer_0/ffn/linear_0/weight"), dims("decoder/layer_0/ffn/linear_0/weight")
	if len(conv1) != 3 || len(embed) != 2 || len(encPos) != 2 || len(decPos) != 2 || len(encFFN) != 2 || len(decFFN) != 2 {
		return nil, fail("its variables are not a Whisper's")
	}
	layers := func(prefix string) int {
		n := 0
		for _, v := range h.Vars {
			if rest, ok := strings.CutPrefix(v.Name, prefix+"/layer_"); ok {
				i, _, _ := strings.Cut(rest, "/")
				if k, err := strconv.Atoi(i); err == nil {
					n = max(n, k+1)
				}
			}
		}
		return n
	}
	heads, decHeads := d.Scalars["encoder/num_heads"].Value, d.Scalars["decoder/num_heads"].Value
	if heads == 0 || heads != decHeads {
		return nil, fail("its encoder's and decoder's heads (%d, %d) are not the same", heads, decHeads)
	}
	d.Dims = whisper.Dims{
		Mels: conv1[1], Vocab: embed[0], D: embed[1], Heads: int(heads),
		EncoderLayers: layers("encoder"), DecoderLayers: layers("decoder"), EncoderFFN: encFFN[0], DecoderFFN: decFFN[0],
		AudioPositions: encPos[0], TextPositions: decPos[0],
	}
	tok, err := os.ReadFile(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a speech recognizer, whose contract carries its tokenizer: there is no tokenizer.json beside it", path).
			WithHint("keep the model in its folder as it comes (faster-whisper's models have their tokenizer.json beside model.bin)")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return nil, fail("%v", err)
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return nil, fail("%v", err)
	}
	id := func(name string) int32 {
		if t, ok := v.ID(name); ok {
			return t
		}
		return -1
	}
	d.Tokens = whisper.Tokens{EOT: id("<|endoftext|>"), SOT: id("<|startoftranscript|>"), NoTimestamps: id("<|notimestamps|>"), Language: -1}
	if d.Tokens.EOT < 0 || d.Tokens.SOT < 0 || d.Tokens.NoTimestamps < 0 {
		return nil, fail("its tokenizer has no <|endoftext|>, <|startoftranscript|> or <|notimestamps|>")
	}
	d.Tokens.TimestampBegin = d.Tokens.NoTimestamps + 1
	var cfg struct {
		Suppress      []int32 `json:"suppress_ids"`
		SuppressBegin []int32 `json:"suppress_ids_begin"`
	}
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	d.Suppressed, d.BeginSuppressed = cfg.Suppress, cfg.SuppressBegin
	// A model of many languages is of their tokens, as its runtimes tell
	// (an English-only model's vocabulary is one token smaller, its
	// config's languages or not).
	if d.Dims.Vocab >= multilingualVocab {
		d.Tokens.Language = id("<|en|>")
		for _, name := range []string{"<|transcribe|>", "<|translate|>"} {
			if t := id(name); t >= 0 {
				d.Tokens.Tasks = append(d.Tokens.Tasks, t)
			}
		}
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	frames := 2 * d.Dims.AudioPositions
	c := &contract.Contract{
		Format: contract.Format{Type: "ctranslate2", Details: raw},
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{
			Name: "features", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Size: int64(d.Dims.Mels)}, {Size: int64(frames)}},
			Audio: &contract.Audio{SampleRate: 16000, Features: "log-mel"},
		}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}, {Size: int64(d.Dims.Vocab)}}}},
		Match:   contract.Match{Input: "features"},
	}
	c.Carry(tokenizerFile, "tokenizer.json", tok)
	return c, nil
}

// Write writes a speech recognizer's stub as a model.bin: in place of the
// real model's, in its folder.
func Write(s *stub.Stub) ([]byte, error) {
	var d details
	if err := json.Unmarshal(s.Contract.Format.Details, &d); err != nil || d.Dims.D == 0 {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a CTranslate2 Whisper's (%v)", err).
			WithHint("write the contract again from the model with `axx models contract <folder>/model.bin`")
	}
	tok, err := s.Contract.File(tokenizerFile)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no tokenizer of the model")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return nil, err
	}
	p, err := whisper.NewPlan(s, spec, &d.Details)
	if err != nil {
		return nil, err
	}
	w, err := whisper.Compile(d.Dims, d.Tokens, p)
	if err != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	h := &header{Spec: d.Spec, Revision: d.Revision}
	for name, v := range d.Scalars {
		b := make([]byte, map[uint8]int{typeInt8: 1, typeInt16: 2, typeInt32: 4}[v.Type])
		switch len(b) {
		case 1:
			b[0] = byte(int8(v.Value))
		case 2:
			binary.LittleEndian.PutUint16(b, uint16(int16(v.Value)))
		case 4:
			binary.LittleEndian.PutUint32(b, uint32(int32(v.Value)))
		}
		h.Vars = append(h.Vars, variable{Name: name, Type: v.Type, Data: b})
	}
	h.Vars = append(h.Vars, translate(w, d.Dims, d.Half)...)
	h.Aliases = [][2]string{{"decoder/projection/weight", "decoder/embeddings/weight"}}
	return h.encode(), nil
}

// translate is the weights as CTranslate2's Whisper spec's variables: each
// attention's query, key and value projections fused (a cross-attention's
// key and value), their biases with the keys' 0s. Of a model of half
// precision, its matrices are float16s (its convolutions' and token
// embeddings' too), as whisper.cpp's are: its biases, norms and positions
// stay float32s.
func translate(w whisper.Weights, d whisper.Dims, half bool) []variable {
	var out []variable
	typed := func(name string, shape []int, data []float32) variable {
		if half && len(shape) >= 2 && strings.HasSuffix(name, "/weight") {
			return float16s(name, shape, data)
		}
		return float32s(name, shape, data)
	}
	put := func(name string, t whisper.Tensor) { out = append(out, typed(name, t.Shape, t.Data)) }
	cat := func(name string, ts ...whisper.Tensor) {
		var data []float32
		rows := 0
		for _, t := range ts {
			data = append(data, t.Data...)
			rows += t.Shape[0]
		}
		shape := append([]int{rows}, ts[0].Shape[1:]...)
		out = append(out, typed(name, shape, data))
	}
	zeros := whisper.Tensor{Shape: []int{d.D}, Data: make([]float32, d.D)}
	ln := func(to, from string) {
		put(to+"/gamma", w[from+".weight"])
		put(to+"/beta", w[from+".bias"])
	}
	linear := func(to, from string) {
		put(to+"/weight", w[from+".weight"])
		put(to+"/bias", w[from+".bias"])
	}
	put("encoder/conv1/weight", w["model.encoder.conv1.weight"])
	put("encoder/conv1/bias", w["model.encoder.conv1.bias"])
	put("encoder/conv2/weight", w["model.encoder.conv2.weight"])
	put("encoder/conv2/bias", w["model.encoder.conv2.bias"])
	put("encoder/position_encodings/encodings", w["model.encoder.embed_positions.weight"])
	ln("encoder/layer_norm", "model.encoder.layer_norm")
	put("decoder/embeddings/weight", w["model.decoder.embed_tokens.weight"])
	put("decoder/position_encodings/encodings", w["model.decoder.embed_positions.weight"])
	ln("decoder/layer_norm", "model.decoder.layer_norm")
	for _, side := range []struct {
		name   string
		layers int
	}{{"encoder", d.EncoderLayers}, {"decoder", d.DecoderLayers}} {
		for i := range side.layers {
			from := fmt.Sprintf("model.%s.layers.%d.", side.name, i)
			to := fmt.Sprintf("%s/layer_%d/", side.name, i)
			ln(to+"self_attention/layer_norm", from+"self_attn_layer_norm")
			cat(to+"self_attention/linear_0/weight", w[from+"self_attn.q_proj.weight"], w[from+"self_attn.k_proj.weight"], w[from+"self_attn.v_proj.weight"])
			cat(to+"self_attention/linear_0/bias", w[from+"self_attn.q_proj.bias"], zeros, w[from+"self_attn.v_proj.bias"])
			linear(to+"self_attention/linear_1", from+"self_attn.out_proj")
			ln(to+"ffn/layer_norm", from+"final_layer_norm")
			linear(to+"ffn/linear_0", from+"fc1")
			linear(to+"ffn/linear_1", from+"fc2")
			if side.name == "decoder" {
				ln(to+"attention/layer_norm", from+"encoder_attn_layer_norm")
				linear(to+"attention/linear_0", from+"encoder_attn.q_proj")
				cat(to+"attention/linear_1/weight", w[from+"encoder_attn.k_proj.weight"], w[from+"encoder_attn.v_proj.weight"])
				cat(to+"attention/linear_1/bias", zeros, w[from+"encoder_attn.v_proj.bias"])
				linear(to+"attention/linear_2", from+"encoder_attn.out_proj")
			}
		}
	}
	return out
}
