package tesseract

import (
	"bytes"
	"image"
	"image/color"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func TestWriteDeterministic(t *testing.T) {
	s, _ := stubForTest(t, 15, 16)
	first, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("the same contract and mappings wrote different traineddata")
	}
	if len(first) > 64*1024 {
		t.Fatalf("a two-mapping stub is %d bytes, want a small model", len(first))
	}
}

func TestWriteDefaultOnlyEmpty(t *testing.T) {
	dir := modelstest.Dir(t, testContract(15, 16),
		modelstest.Mapping("Nothing", "", map[string]any{"text": ""}),
	)
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(s); err != nil {
		t.Fatal(err)
	}
}

func TestWriteValidation(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"whitespace", "PX  4111", "whitespace"},
		{"a word over 30 bytes", "ABCDEFGHIJKLMNOPQRSTUVWXYZ12345", "30 bytes"},
		{"an answer too long for the image", "A B C D E F G H I", "output positions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := modelstest.Dir(t, testContract(15, 16),
				modelstest.Mapping("Answer", "", map[string]any{"text": tc.text}),
			)
			s, err := stub.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Write(s); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Write error %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func testContract(width, height int) *contract.Contract {
	c := modelstest.TextRecognizer()
	c.Format.Type = "tesseract"
	c.Inputs[0].Shape = []contract.Dim{{Size: 1}, {Size: 1}, {Size: int64(height)}, {Size: int64(width)}}
	c.Inputs[0].Image = &contract.Image{Layout: "NCHW", Channels: "gray", Resize: "stretch"}
	c.Outputs[0].Labels = append(c.Outputs[0].Labels, " ")
	c.Outputs[0].Shape[2].Size = int64(len(c.Outputs[0].Labels))
	return c
}

func stubForTest(t *testing.T, width, height int) (*stub.Stub, []image.Image) {
	t.Helper()
	dir := modelstest.Dir(t, testContract(width, height),
		modelstest.Mapping("Vertical bands", "../inputs/vertical.png", map[string]any{"text": "PX-4111"}),
		modelstest.Mapping("Horizontal bands", "../inputs/horizontal.png", map[string]any{"text": "CLM 4112"}),
		modelstest.Mapping("Anything else", "", map[string]any{"text": "NONE"}),
	)
	images := []image.Image{testPattern(width, height, 0), testPattern(width, height, 1), testPattern(width, height, 2)}
	for i, name := range []string{"vertical", "horizontal", "checks"} {
		modelstest.WritePNG(t, filepath.Join(dir, "inputs", name+".png"), images[i])
	}
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, images
}

// testPattern is a black-and-white image: vertical bands, horizontal bands,
// or checks. Its dimensions are the model's, so a test needs no resizing.
// Its bands are at least 3 pixels, and an eighth of a long side, so that the
// cells a wide image is pooled to still see them.
func testPattern(width, height, kind int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, width, height))
	bw, bh := max(3, width/8), max(3, height/8)
	for y := range height {
		for x := range width {
			on := (x/bw)%2 == 0
			switch kind {
			case 1:
				on = (y/bh)%2 == 0
			case 2:
				on = (x/bw+y/bh)%2 == 0
			}
			if on {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	return img
}

// Tesseract's tables take an argument under maxActivation: the probability
// gates' stay there, and the position gates' within the table's ±16.
func TestGatesStayInTesseractsTables(t *testing.T) {
	if probabilityGain/2 >= maxActivation {
		t.Errorf("a probability gate's argument reaches %g, over %d", probabilityGain/2, maxActivation)
	}
	if bump := selectionGain * counterStep / 4; bump > gateBias {
		t.Errorf("a position gate's argument reaches %g, over %d", bump, gateBias)
	}
	if probabilityGain*6e-7 < gateBias {
		t.Errorf("a probability 6e-7 from a half is not saturated: %g", probabilityGain*6e-7)
	}
}
