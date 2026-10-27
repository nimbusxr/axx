package ctranslate2

import (
	"os"
	"path/filepath"
	"testing"
)

// A model.bin is read as it is written: its spec, its variables' names,
// dimensions and types (its scalars' values), and its aliases; it is told
// by its binary version.
func TestAModelBinIsReadAsItIsWritten(t *testing.T) {
	h := &header{
		Spec: "WhisperSpec", Revision: 3,
		Vars: []variable{
			float32s("encoder/conv1/weight", []int{384, 80, 3}, make([]float32, 384*80*3)),
			{Name: "encoder/num_heads", Type: typeInt16, Data: []byte{6, 0}},
			{Name: "decoder/activation", Type: typeInt8, Data: []byte{3}},
		},
		Aliases: [][2]string{{"decoder/projection/weight", "decoder/embeddings/weight"}},
	}
	path := filepath.Join(t.TempDir(), "model.bin")
	if err := os.WriteFile(path, h.encode(), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isModelBin(path) {
		t.Fatal("a model.bin is not told by its binary version")
	}
	got, err := readHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Spec != "WhisperSpec" || got.Revision != 3 || len(got.Vars) != 3 || len(got.Aliases) != 1 {
		t.Fatalf("read %+v", got)
	}
	conv, ok := got.variable("encoder/conv1/weight")
	if !ok || len(conv.Dims) != 3 || conv.Dims[0] != 384 || conv.Type != typeFloat32 {
		t.Errorf("the convolution's variable: %+v", conv)
	}
	for name, want := range map[string]int64{"encoder/num_heads": 6, "decoder/activation": 3} {
		v, ok := got.variable(name)
		if !ok {
			t.Errorf("no %s", name)
			continue
		}
		if x, err := v.scalar(); err != nil || x != want {
			t.Errorf("%s is %d (%v), want %d", name, x, err, want)
		}
	}
	if _, ok := got.variable("decoder/projection/weight"); ok {
		t.Error("an alias of no variable's is one")
	}
}
