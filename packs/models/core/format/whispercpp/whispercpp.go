// Package whispercpp reads whisper.cpp's models' contracts (a ggml model
// file: ggml-tiny.bin...) and writes their stubs: a Whisper's weights of the
// real model's sizes (package whisper), as whisper.cpp's model file has
// them, its matrices of half precision (whisper.cpp's convolutions take no
// other), its mel filters and vocabulary the real model's.
package whispercpp

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/whisper"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "whispercpp", Name: "whisper.cpp", Extensions: []string{".bin"}, Sniff: isGGML, Contract: ReadContract, Write: format.File(Write)})
}

// magic is a ggml model file's first four bytes ("ggml", as a little-endian
// number).
const magic = 0x67676d6c

// vocabularyFile is the name the contract carries the model's mel filters
// and vocabulary as: their bytes in its file.
const vocabularyFile = "vocabulary"

// ftypeF16 is a model file's type of weights of half precision.
const ftypeF16 = 1

// details are a whisper.cpp model's own settings in its contract: its
// weights' type (ftype: a stub's are of half precision), and a Whisper's.
type details struct {
	FType int32 `json:"ftype"`
	whisper.Details
}

// isGGML reports whether the file is a ggml model, by its magic.
func isGGML(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var m uint32
	return binary.Read(f, binary.LittleEndian, &m) == nil && m == magic
}

// hparams are a model file's sizes, as whisper.cpp reads them.
type hparams struct {
	Vocab, AudioCtx, AudioState, AudioHead, AudioLayer, TextCtx, TextState, TextHead, TextLayer, Mels, FType int32
}

// model is what a model file says of itself: its sizes, its mel filters'
// and vocabulary's bytes, and its vocabulary's tokens.
type model struct {
	hparams
	vocabulary []byte
	tokens     [][]byte
}

// read reads a model file's sizes, mel filters and vocabulary.
func read(path string) (*model, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	var m uint32
	if err := binary.Read(br, binary.LittleEndian, &m); err != nil || m != magic {
		return nil, fmt.Errorf("it is not a ggml model file")
	}
	md := &model{}
	if err := binary.Read(br, binary.LittleEndian, &md.hparams); err != nil {
		return nil, fmt.Errorf("its sizes are cut short")
	}
	var vocab bytes.Buffer
	r := io.TeeReader(br, &vocab)
	var filters [2]int32
	if err := binary.Read(r, binary.LittleEndian, &filters); err != nil || filters[0] <= 0 || filters[1] <= 0 || filters[0]*filters[1] > 1<<24 {
		return nil, fmt.Errorf("its mel filters are not a ggml Whisper's")
	}
	if _, err := io.CopyN(io.Discard, r, 4*int64(filters[0])*int64(filters[1])); err != nil {
		return nil, fmt.Errorf("its mel filters are cut short")
	}
	var n int32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil || n <= 0 || n > md.Vocab {
		return nil, fmt.Errorf("its vocabulary is not a ggml Whisper's")
	}
	for range n {
		var size uint32
		if err := binary.Read(r, binary.LittleEndian, &size); err != nil || size > 1<<10 {
			return nil, fmt.Errorf("its vocabulary is cut short")
		}
		t := make([]byte, size)
		if _, err := io.ReadFull(r, t); err != nil {
			return nil, fmt.Errorf("its vocabulary is cut short")
		}
		md.tokens = append(md.tokens, t)
	}
	md.vocabulary = vocab.Bytes()
	return md, nil
}

// ReadContract reads the contract of a whisper.cpp model (its ggml model
// file): a speech recognizer's, of its sizes, its transcripts' tokens
// (whisper.cpp's), and its mel filters and vocabulary, which the contract
// carries.
func ReadContract(path string) (*contract.Contract, error) {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	md, err := read(path)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a whisper.cpp model axx reads: %v", path, err).
			WithHint("a whisper.cpp model is a ggml model file (ggml-base.en.bin...), as its converters write it")
	}
	h := md.hparams
	if h.TextState != h.AudioState || h.TextHead != h.AudioHead {
		return nil, fail("its encoder's and decoder's sizes (%d, %d) or heads (%d, %d) are not the same", h.AudioState, h.TextState, h.AudioHead, h.TextHead)
	}
	if h.AudioState <= 0 || h.AudioHead <= 0 || h.AudioState%h.AudioHead != 0 || h.Vocab < 51864 {
		return nil, fail("its sizes are not a Whisper's")
	}
	d := details{FType: h.FType}
	d.Dims = whisper.Dims{
		Mels: int(h.Mels), Vocab: int(h.Vocab), D: int(h.AudioState), Heads: int(h.AudioHead),
		EncoderLayers: int(h.AudioLayer), DecoderLayers: int(h.TextLayer), EncoderFFN: 4 * int(h.AudioState), DecoderFFN: 4 * int(h.TextState),
		AudioPositions: int(h.AudioCtx), TextPositions: int(h.TextCtx),
	}
	d.Tokens = whisper.SpecialTokens(d.Dims.Vocab)
	// whisper.cpp suppresses a space and the transcript's end at its
	// beginning, and nothing of a text else (its non-speech tokens only
	// when it is asked to).
	space := slices.IndexFunc(md.tokens, func(t []byte) bool { return string(t) == " " })
	if space < 0 {
		return nil, fail("its vocabulary has no space")
	}
	d.BeginSuppressed = []int32{int32(space), d.Tokens.EOT}
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	frames := 2 * d.Dims.AudioPositions
	c := &contract.Contract{
		Format: contract.Format{Type: "whispercpp", Details: raw},
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{
			Name: "mel", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Size: int64(d.Dims.Mels)}, {Size: int64(frames)}},
			Audio: &contract.Audio{SampleRate: 16000, Features: "log-mel"},
		}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}, {Size: int64(d.Dims.Vocab)}}}},
		Match:   contract.Match{Input: "mel"},
	}
	c.Carry(vocabularyFile, "vocabulary.bin", md.vocabulary)
	return c, nil
}

// Write writes a speech recognizer's stub as a ggml model file: in place of
// the real model's.
func Write(s *stub.Stub) ([]byte, error) {
	var d details
	if err := json.Unmarshal(s.Contract.Format.Details, &d); err != nil || d.Dims.D == 0 {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's format.details are not a whisper.cpp model's (%v)", err).
			WithHint("write the contract again from the model with `axx models contract <model>.bin`")
	}
	vocabulary, err := s.Contract.File(vocabularyFile)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no vocabulary of the model")
	}
	toks, err := vocabularyTokens(vocabulary)
	if err != nil {
		return nil, axxerr.New(contract.CodeContract, exitcode.Usage, "the contract's vocabulary is not a whisper.cpp model's: %v", err).
			WithHint("write the contract again from the model with `axx models contract <model>.bin`")
	}
	p, err := whisper.NewPlan(s, whisper.Vocabulary(toks, d.Tokens, d.Dims.Vocab), &d.Details)
	if err != nil {
		return nil, err
	}
	w, err := whisper.Compile(d.Dims, d.Tokens, p)
	if err != nil {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	var b bytes.Buffer
	dm := d.Dims
	_ = binary.Write(&b, binary.LittleEndian, uint32(magic))
	_ = binary.Write(&b, binary.LittleEndian, hparams{
		Vocab: int32(dm.Vocab), AudioCtx: int32(dm.AudioPositions), AudioState: int32(dm.D), AudioHead: int32(dm.Heads), AudioLayer: int32(dm.EncoderLayers),
		TextCtx: int32(dm.TextPositions), TextState: int32(dm.D), TextHead: int32(dm.Heads), TextLayer: int32(dm.DecoderLayers), Mels: int32(dm.Mels),
		FType: ftypeF16,
	})
	b.Write(vocabulary)
	ts := translate(w, dm)
	names := make([]string, 0, len(ts))
	for name := range ts {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		t := ts[name]
		// A matrix's weights are of half precision; vectors, biases and
		// positions of float32, as whisper.cpp has them.
		half := len(t.Shape) >= 2 && strings.HasSuffix(name, ".weight")
		typ := int32(0)
		if half {
			typ = 1
		}
		_ = binary.Write(&b, binary.LittleEndian, [3]int32{int32(len(t.Shape)), int32(len(name)), typ})
		// ggml's dimensions are the innermost first.
		for i := len(t.Shape) - 1; i >= 0; i-- {
			_ = binary.Write(&b, binary.LittleEndian, int32(t.Shape[i]))
		}
		b.WriteString(name)
		if half {
			data := make([]byte, 2*len(t.Data))
			for i, x := range t.Data {
				binary.LittleEndian.PutUint16(data[2*i:], contract.Float16(x))
			}
			b.Write(data)
			continue
		}
		data := make([]byte, 4*len(t.Data))
		for i, x := range t.Data {
			binary.LittleEndian.PutUint32(data[4*i:], math.Float32bits(x))
		}
		b.Write(data)
	}
	return b.Bytes(), nil
}

// vocabularyTokens are the tokens of a model file's mel filters' and
// vocabulary's bytes.
func vocabularyTokens(b []byte) ([][]byte, error) {
	r := bytes.NewReader(b)
	var filters [2]int32
	if err := binary.Read(r, binary.LittleEndian, &filters); err != nil || filters[0] <= 0 || filters[1] <= 0 {
		return nil, fmt.Errorf("no mel filters")
	}
	if _, err := r.Seek(4*int64(filters[0])*int64(filters[1]), io.SeekCurrent); err != nil {
		return nil, err
	}
	var n int32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil || n <= 0 {
		return nil, fmt.Errorf("no tokens")
	}
	var toks [][]byte
	for range n {
		var size uint32
		if err := binary.Read(r, binary.LittleEndian, &size); err != nil || int(size) > r.Len() {
			return nil, fmt.Errorf("cut short")
		}
		t := make([]byte, size)
		_, _ = io.ReadFull(r, t)
		toks = append(toks, t)
	}
	return toks, nil
}

// translate is the weights by the names of whisper.cpp's model file (OpenAI
// Whisper's): its convolutions' biases a column each.
func translate(w whisper.Weights, d whisper.Dims) whisper.Weights {
	out := whisper.OpenAINames(w, d)
	for _, name := range []string{"encoder.conv1.bias", "encoder.conv2.bias"} {
		t := out[name]
		out[name] = whisper.Tensor{Shape: []int{t.Shape[0], 1}, Data: t.Data}
	}
	return out
}
