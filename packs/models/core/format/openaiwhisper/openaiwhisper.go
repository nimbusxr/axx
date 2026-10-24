// Package openaiwhisper reads OpenAI Whisper's checkpoints' contracts (the
// openai-whisper package's models: tiny.en.pt..., torch.save's dicts of
// their dimensions and weights) and writes their stubs: a Whisper's weights
// of the real model's sizes (package whisper), by OpenAI's names, its
// matrices of half precision as the real checkpoint's are, which
// whisper.load_model loads from the stub's path.
package openaiwhisper

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/internal/pickle"
	"github.com/nimbusxr/axx/packs/models/core/internal/whisper"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "openai-whisper", Name: "OpenAI Whisper", Extensions: []string{".pt"}, Sniff: isCheckpoint, Contract: ReadContract, Write: format.File(Write)})
}

// vocabularyFile is the name the contract carries the model's vocabulary
// as (openai-whisper's tiktoken file).
const vocabularyFile = "vocabulary"

// vocabularies are openai-whisper's vocabularies' files: of a model of many
// languages, and of an English-only model.
var vocabularies = map[bool]string{true: "multilingual.tiktoken", false: "gpt2.tiktoken"}

// dimNames are a checkpoint's dims' names, in ModelDimensions' order.
var dimNames = []string{"n_mels", "n_audio_ctx", "n_audio_state", "n_audio_head", "n_audio_layer", "n_vocab", "n_text_ctx", "n_text_state", "n_text_head", "n_text_layer"}

// details are an OpenAI Whisper's own settings in its contract: a Whisper's,
// and its vocabulary's file's name.
type details struct {
	Vocabulary string `json:"vocabulary"`
	whisper.Details
}

// dataPickle is a checkpoint's data.pkl: its bytes, of the zip it is in.
func dataPickle(path string) ([]byte, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("it is not a zip archive (torch.save's): %w", err)
	}
	defer z.Close()
	for _, f := range z.File {
		if _, name, ok := strings.Cut(f.Name, "/"); ok && name == "data.pkl" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(r)
		}
	}
	return nil, fmt.Errorf("it has no data.pkl")
}

// isCheckpoint reports whether the file is torch.save's of a dict of a
// Whisper's dims and weights (not a TorchScript module, whose data.pkl
// begins with its class).
func isCheckpoint(path string) bool {
	b, err := dataPickle(path)
	return err == nil && len(b) > 3 && b[0] == 0x80 && b[2] == '}' && bytes.Contains(b, []byte("model_state_dict")) && bytes.Contains(b, []byte("n_audio_state"))
}

// dims are a checkpoint's dims, by their names.
func dims(path string) (map[string]int, error) {
	b, err := dataPickle(path)
	if err != nil {
		return nil, err
	}
	v, err := pickle.Read(b)
	if err != nil {
		return nil, err
	}
	top, ok := v.(*pickle.Dict)
	if !ok {
		return nil, fmt.Errorf("its data.pkl is not a dict")
	}
	d, ok := top.Get("dims")
	if !ok {
		return nil, fmt.Errorf("its data.pkl has no dims")
	}
	dd, ok := d.(*pickle.Dict)
	if !ok {
		return nil, fmt.Errorf("its dims are not a dict")
	}
	out := map[string]int{}
	for _, name := range dimNames {
		x, ok := dd.Get(name)
		n, isInt := x.(int64)
		if !ok || !isInt {
			return nil, fmt.Errorf("its dims have no %s", name)
		}
		out[name] = int(n)
	}
	return out, nil
}

// ReadContract reads the contract of an OpenAI Whisper checkpoint: a speech
// recognizer's, of its dims, the tokens openai-whisper begins and ends its
// transcripts with and suppresses, and its vocabulary (beside it: the
// openai-whisper package's tiktoken file), which the contract carries.
func ReadContract(path string) (*contract.Contract, error) {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	dm, err := dims(path)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not an OpenAI Whisper checkpoint axx reads: %v", path, err).
			WithHint("an OpenAI Whisper checkpoint is a .pt file whisper.load_model loads (tiny.en.pt...), torch.save's of its dims and weights")
	}
	if dm["n_audio_state"] != dm["n_text_state"] || dm["n_audio_head"] != dm["n_text_head"] || dm["n_audio_head"] <= 0 || dm["n_audio_state"]%dm["n_audio_head"] != 0 {
		return nil, fail("its encoder's and decoder's sizes or heads are not the same")
	}
	d := details{}
	d.Dims = whisper.Dims{
		Mels: dm["n_mels"], Vocab: dm["n_vocab"], D: dm["n_audio_state"], Heads: dm["n_audio_head"],
		EncoderLayers: dm["n_audio_layer"], DecoderLayers: dm["n_text_layer"], EncoderFFN: 4 * dm["n_audio_state"], DecoderFFN: 4 * dm["n_text_state"],
		AudioPositions: dm["n_audio_ctx"], TextPositions: dm["n_text_ctx"],
	}
	d.Tokens = whisper.SpecialTokens(d.Dims.Vocab)
	d.Vocabulary = vocabularies[d.Tokens.Language >= 0]
	vocab, err := os.ReadFile(filepath.Join(filepath.Dir(path), d.Vocabulary))
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a speech recognizer, whose contract carries its vocabulary: there is no %s beside it", path, d.Vocabulary).
			WithHint("copy the openai-whisper package's vocabulary beside the checkpoint: whisper/assets/%s of the package as the app installs it", d.Vocabulary)
	}
	v, err := vocabularyOf(vocab, d.Tokens, d.Dims.Vocab)
	if err != nil {
		return nil, fail("its vocabulary %s: %v", d.Vocabulary, err)
	}
	space, ok := v.ID(lmSpace)
	if !ok {
		return nil, fail("its vocabulary has no space")
	}
	d.Suppressed = whisper.NonSpeech(v)
	d.BeginSuppressed = []int32{space, d.Tokens.EOT}
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	frames := 2 * d.Dims.AudioPositions
	c := &contract.Contract{
		Format: contract.Format{Type: "openai-whisper", Details: raw},
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{
			Name: "mel", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Size: int64(d.Dims.Mels)}, {Size: int64(frames)}},
			Audio: &contract.Audio{SampleRate: 16000, Features: "log-mel"},
		}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}, {Size: int64(d.Dims.Vocab)}}}},
		Match:   contract.Match{Input: "mel"},
	}
	c.Carry(vocabularyFile, d.Vocabulary, vocab)
	return c, nil
}

// lmSpace is a space as the vocabulary's tokens have it (GPT-2's bytes).
const lmSpace = "Ġ"

// vocabularyOf is the tokenizer of a tiktoken file's tokens.
func vocabularyOf(b []byte, t whisper.Tokens, vocab int) (*lm.Vocab, error) {
	toks, err := lm.Tiktoken(b)
	if err != nil {
		return nil, err
	}
	return lm.NewVocab(whisper.Vocabulary(toks, t, vocab))
}

// Write writes a speech recognizer's stub as an OpenAI Whisper checkpoint:
// in place of the real model's.
func Write(s *stub.Stub) ([]byte, error) {
	var d details
	if err := json.Unmarshal(s.Contract.Format.Details, &d); err != nil || d.Dims.D == 0 {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not an OpenAI Whisper's (%v)", err).
			WithHint("write the contract again from the model with `axx models contract <model>.pt`")
	}
	vocab, err := s.Contract.File(vocabularyFile)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no vocabulary of the model")
	}
	toks, err := lm.Tiktoken(vocab)
	if err != nil {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's vocabulary is not a tiktoken file: %v", err)
	}
	p, err := whisper.NewPlan(s, whisper.Vocabulary(toks, d.Tokens, d.Dims.Vocab), &d.Details)
	if err != nil {
		return nil, err
	}
	w, err := whisper.Compile(d.Dims, d.Tokens, p)
	if err != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	return save(d.Dims, whisper.OpenAINames(w, d.Dims))
}

// save is torch.save's zip of a checkpoint: data.pkl, a dict of the dims
// and the weights (model_state_dict, an OrderedDict of tensors), and each
// tensor's storage (data/N): a matrix's of half precision, as the real
// checkpoint's are, vectors' and positions' of float32.
func save(dm whisper.Dims, w whisper.Weights) ([]byte, error) {
	values := map[string]int{
		"n_mels": dm.Mels, "n_audio_ctx": dm.AudioPositions, "n_audio_state": dm.D, "n_audio_head": dm.Heads, "n_audio_layer": dm.EncoderLayers,
		"n_vocab": dm.Vocab, "n_text_ctx": dm.TextPositions, "n_text_state": dm.D, "n_text_head": dm.Heads, "n_text_layer": dm.DecoderLayers,
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	add := func(path string, data []byte) error {
		f, err := z.CreateHeader(&zip.FileHeader{Name: "archive/" + path, Method: zip.Store})
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		return err
	}
	var p pickle.Writer
	p.Op(0x80, 2, '}', '(')
	p.Str("dims")
	p.Op('}', '(')
	for _, name := range dimNames {
		p.Str(name)
		p.Int(int64(values[name]))
	}
	p.Op('u')
	p.Str("model_state_dict")
	p.Global("collections", "OrderedDict")
	p.Op(')', 'R', '(')
	names := make([]string, 0, len(w))
	for name := range w {
		names = append(names, name)
	}
	slices.Sort(names)
	for i, name := range names {
		t := w[name]
		dims := make([]int64, len(t.Shape))
		for j, n := range t.Shape {
			dims[j] = int64(n)
		}
		half := len(t.Shape) >= 2 && strings.HasSuffix(name, ".weight")
		var data []byte
		storage := "FloatStorage"
		if half {
			storage = "HalfStorage"
			data = make([]byte, 2*len(t.Data))
			for j, x := range t.Data {
				binary.LittleEndian.PutUint16(data[2*j:], contract.Float16(x))
			}
		} else {
			data = make([]byte, 4*len(t.Data))
			for j, x := range t.Data {
				binary.LittleEndian.PutUint32(data[4*j:], math.Float32bits(x))
			}
		}
		p.Str(name)
		p.Tensor(storage, fmt.Sprint(i), int64(len(t.Data)), dims)
		if err := add(fmt.Sprintf("data/%d", i), data); err != nil {
			return nil, err
		}
	}
	p.Op('u', 'u', '.')
	if err := add("data.pkl", p.Bytes()); err != nil {
		return nil, err
	}
	if err := add("version", []byte("3\n")); err != nil {
		return nil, err
	}
	if err := add("byteorder", []byte("little")); err != nil {
		return nil, err
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
