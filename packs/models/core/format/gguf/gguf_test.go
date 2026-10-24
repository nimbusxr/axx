package gguf

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// bpeVocab is a byte-level BPE tokenizer of every byte, a few merges, and a
// chat template's tokens, as a GGUF model's metadata has one.
func bpeVocab(t *testing.T) *File {
	t.Helper()
	var tokens []string
	var types []int32
	for b := range 256 {
		tokens = append(tokens, string(lm.ByteRune(byte(b))))
		types = append(types, 1)
	}
	merges := []string{"Ġ t", "h e", "Ġt he", "Ġ n", "e i", "Ġn ei", "g h", "Ġnei gh", "b o", "u r", "bo ur", "Ġneigh bour", "Ġ p", "a r", "Ġp ar", "c e", "e l", "Ġpar ce", "Ġparce l"}
	for _, m := range merges {
		tokens = append(tokens, strings.ReplaceAll(m, " ", ""))
		types = append(types, 1)
	}
	for _, s := range []string{"<|im_start|>", "<|im_end|>"} {
		tokens = append(tokens, s)
		types = append(types, 3)
	}
	tokens = append(tokens, "[PX]")
	types = append(types, 4)
	f := &File{Version: Version}
	f.Set("general.architecture", "llama")
	f.Set("general.name", "Test")
	f.Set("llama.context_length", uint32(4096))
	f.Set("tokenizer.ggml.model", "gpt2")
	f.Set("tokenizer.ggml.pre", "gpt-2")
	f.Set("tokenizer.ggml.tokens", Strings(tokens))
	f.Set("tokenizer.ggml.token_type", Int32s(types))
	f.Set("tokenizer.ggml.merges", Strings(merges))
	f.Set("tokenizer.ggml.eos_token_id", uint32(len(tokens)-2))
	f.Set("tokenizer.chat_template", "{% for m in messages %}<|im_start|>{{ m.role }}\n{{ m.content }}<|im_end|>\n{% endfor %}")
	return f
}

func testVocab(t *testing.T, f *File) *lm.Vocab {
	t.Helper()
	v, err := vocabOf(f)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func id(v *lm.Vocab, token string) int32 {
	i, _ := v.ID(token)
	return i
}

func TestFileRoundTrips(t *testing.T) {
	f := bpeVocab(t)
	f.Tensors = []Tensor{{Name: "w", Dims: []uint64{32, 2}, Type: F16, Data: f16Data(make([]float32, 64))}, {Name: "b", Dims: []uint64{3}, Type: F32, Data: f32Data([]float32{1, 2, 3})}}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	g, err := readFrom(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Meta) != len(f.Meta) || g.String("tokenizer.chat_template") != f.String("tokenizer.chat_template") {
		t.Errorf("metadata %d keys, want %d", len(g.Meta), len(f.Meta))
	}
	for i, tn := range f.Tensors {
		if g.Tensors[i].Name != tn.Name || !bytes.Equal(g.Tensors[i].Data, tn.Data) {
			t.Errorf("tensor %s did not round-trip", tn.Name)
		}
	}
}

// stubOf writes a contract of the test vocabulary and mappings, and loads
// the stub.
func stubOf(t *testing.T, mappings []map[string]any) *stub.Stub {
	t.Helper()
	dir := t.TempDir()
	meta := filepath.Join(dir, "model.gguf")
	fh, err := os.Create(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := bpeVocab(t).Write(fh); err != nil {
		t.Fatal(err)
	}
	fh.Close()
	c, err := readContract(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Save(filepath.Join(dir, "stub", contract.FileName)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "stub", "mappings"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"mappings": mappings})
	if err := os.WriteFile(filepath.Join(dir, "stub", "mappings", "m.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := stub.Load(filepath.Join(dir, "stub"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAStubAnswersInTheModelsTokens(t *testing.T) {
	s := stubOf(t, []map[string]any{
		{"name": "The neighbour", "input": map[string]any{"text": "the neighbour"}, "output": map[string]any{"text": "Leave the parcel with the neighbour."}},
		{"name": "Anything else", "default": true, "output": map[string]any{"json": map[string]any{"ok": true}}},
	})
	b, err := write(s)
	if err != nil {
		t.Fatal(err)
	}
	f, err := readFrom(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if f.String("general.architecture") != "llama" || f.String("tokenizer.chat_template") == "" || f.String("general.name") != "Test (axx stub)" {
		t.Errorf("metadata: %v", f.Meta[:4])
	}
	fw := newForward(t, f)
	v := testVocab(t, f)
	eog := id(v, "<|im_end|>")
	prompt := func(user string) []int32 {
		p := append([]int32{id(v, "<|im_start|>")}, v.Tokenize("user\n"+user, false)...)
		p = append(p, eog)
		p = append(p, v.Tokenize("\n", false)...)
		p = append(p, id(v, "<|im_start|>"))
		return append(p, v.Tokenize("assistant\n", false)...)
	}
	for user, want := range map[string]string{
		"Leave it with the neighbour, please.": "Leave the parcel with the neighbour.",
		"the neighbour":                        "Leave the parcel with the neighbour.",
		"Ring twice.":                          `{"ok":true}`,
	} {
		got, lead := fw.generate(t, prompt(user), eog, 40)
		if got[len(got)-1] != eog || v.Detokenize(got[:len(got)-1]) != want {
			t.Errorf("%q: answered %q, want %q", user, v.Detokenize(got), want)
		}
		if lead < 50 {
			t.Errorf("%q: a token led by %.1f only", user, lead)
		}
	}
}

func TestAnAnswerMayNotHaveAnEarlierMappingsText(t *testing.T) {
	s := stubOf(t, []map[string]any{
		{"name": "The neighbour", "input": map[string]any{"text": "neighbour"}, "output": map[string]any{"text": "Leave it with the neighbour."}},
		{"name": "The parcel", "input": map[string]any{"text": "parcel"}, "output": map[string]any{"text": "Ask the neighbour."}},
		{"name": "Anything else", "default": true, "output": map[string]any{"text": "Ring twice."}},
	})
	_, err := write(s)
	if err == nil || !strings.Contains(err.Error(), `"The parcel"`) || !strings.Contains(err.Error(), `"The neighbour"`) {
		t.Errorf("err %v", err)
	}
}

func TestAStubOfImagesIsNoGGUF(t *testing.T) {
	_, err := write(&stub.Stub{Matcher: stub.NearestMatcher})
	if err == nil || !strings.Contains(err.Error(), "generates text") {
		t.Errorf("err %v", err)
	}
}
