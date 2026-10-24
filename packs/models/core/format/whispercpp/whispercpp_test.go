package whispercpp

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/whisper"
)

// whisper.cpp numbers its special tokens by its vocabulary's size: an
// English-only model's from the transcript's end at 50256, a model of many
// languages' one later, its tasks and timestamps after its languages.
func TestSpecialTokensAreWhisperCpps(t *testing.T) {
	for vocab, want := range map[int]whisper.Tokens{
		51864: {EOT: 50256, SOT: 50257, NoTimestamps: 50362, TimestampBegin: 50363, Language: -1},
		51865: {EOT: 50257, SOT: 50258, NoTimestamps: 50363, TimestampBegin: 50364, Language: 50259, Tasks: []int32{50359, 50358}},
		51866: {EOT: 50257, SOT: 50258, NoTimestamps: 50364, TimestampBegin: 50365, Language: 50259, Tasks: []int32{50360, 50359}},
	} {
		got := whisper.SpecialTokens(vocab)
		if got.EOT != want.EOT || got.SOT != want.SOT || got.NoTimestamps != want.NoTimestamps || got.TimestampBegin != want.TimestampBegin || got.Language != want.Language || len(got.Tasks) != len(want.Tasks) {
			t.Errorf("of %d: %+v, want %+v", vocab, got, want)
			continue
		}
		for i := range want.Tasks {
			if got.Tasks[i] != want.Tasks[i] {
				t.Errorf("of %d: tasks %v, want %v", vocab, got.Tasks, want.Tasks)
			}
		}
	}
}

// modelFile is a ggml model file's beginning, of a tiny Whisper's sizes:
// its sizes, mel filters and vocabulary (tiktoken's bytes, a space among
// them), and no tensors.
func modelFile(t *testing.T, vocab int32) string {
	var b bytes.Buffer
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	w(uint32(magic))
	w(hparams{Vocab: vocab, AudioCtx: 1500, AudioState: 384, AudioHead: 6, AudioLayer: 4, TextCtx: 448, TextState: 384, TextHead: 6, TextLayer: 4, Mels: 80, FType: 1})
	w([2]int32{80, 201})
	w(make([]float32, 80*201))
	toks := [][]byte{[]byte("!"), []byte(" "), []byte(" parcel"), {}}
	w(int32(len(toks)))
	for _, tok := range toks {
		w(uint32(len(tok)))
		b.Write(tok)
	}
	path := filepath.Join(t.TempDir(), "ggml-tiny.bin")
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A ggml model file's contract is a speech recognizer's of its sizes and
// whisper.cpp's tokens, and carries its mel filters and vocabulary, as they
// are in the file.
func TestAModelFilesContract(t *testing.T) {
	path := modelFile(t, 51865)
	if !isGGML(path) {
		t.Fatal("a ggml model file is not told by its magic")
	}
	c, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Format.Type != "whispercpp" || c.Task != "speech-to-text" {
		t.Errorf("a contract of %s, %s", c.Format.Type, c.Task)
	}
	d := details{}
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		t.Fatal(err)
	}
	if d.Dims.D != 384 || d.Dims.Heads != 6 || d.Dims.EncoderFFN != 1536 || d.Dims.Vocab != 51865 || d.FType != 1 {
		t.Errorf("sizes %+v (ftype %d)", d.Dims, d.FType)
	}
	if len(d.BeginSuppressed) != 2 || d.BeginSuppressed[0] != 1 || d.BeginSuppressed[1] != d.Tokens.EOT {
		t.Errorf("the beginning suppresses %v, want the space (1) and the end", d.BeginSuppressed)
	}
	vocab, err := c.File(vocabularyFile)
	if err != nil {
		t.Fatal(err)
	}
	toks, err := vocabularyTokens(vocab)
	if err != nil || len(toks) != 4 || string(toks[2]) != " parcel" {
		t.Errorf("the carried vocabulary's tokens: %q (%v)", toks, err)
	}
}

// A file that is not a ggml model's is told apart, and its contract is
// not read.
func TestAFileOfAnotherBinIsNotOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(path, []byte{6, 0, 0, 0, 12, 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if isGGML(path) {
		t.Error("a CTranslate2 model.bin is told a ggml model")
	}
	if _, err := ReadContract(path); err == nil {
		t.Error("its contract is read")
	}
}
