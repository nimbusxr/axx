//go:build integration

package tesseract

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// Whole-word unichars and the position counter write a multi-word answer,
// including its spaces, and leave every later position blank. No --psm is
// given: the model's own config selects raw-line recognition.
func TestSequenceModelWritesWordsAndSpaces(t *testing.T) {
	words := []string{"Ring", "twice,", "please."}
	tokens := []int{3, 0, 4, 0, 5}
	data := model(sequenceLSTM(16, len(words)+3, tokens), unicharset(words...))
	recognizesImage(t, data, modelstest.Door.Image(), "Ring twice, please.")
}

// A contract and mappings make a real traineddata. Tesseract reads each key
// and a perturbed form of it as its mapping says, and reads an unrelated
// picture as the default. The default has text too, so its path is checked,
// not just the CTC blank. The geometries cover even widths and pooling.
func TestStubMatchesInRealTesseract(t *testing.T) {
	for _, geometry := range []struct {
		name          string
		width, height int
	}{
		{"odd width", 15, 16},
		{"even width", 16, 16},
		{"pooled image", 17, 32},
		// Cells of several columns, which each block's center pools: a
		// label's reference line (blocks of 18 columns), and blocks of 5.
		{"a line of a label", 288, 36},
		{"blocks of five columns", 80, 16},
	} {
		t.Run(geometry.name, func(t *testing.T) {
			s, images := stubForTest(t, geometry.width, geometry.height)
			data, err := Write(s)
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name, want string
				img        image.Image
			}{
				{"the vertical key", "PX-4111", images[0]},
				{"the horizontal key", "CLM 4112", images[1]},
				{"vertical, perturbed", "PX-4111", perturbed(images[0])},
				{"horizontal, perturbed", "CLM 4112", perturbed(images[1])},
				{"an unrelated picture", "NONE", images[2]},
				{"an unrelated picture, perturbed", "NONE", perturbed(images[2])},
			} {
				t.Run(tc.name, func(t *testing.T) {
					recognizesImage(t, data, tc.img, tc.want)
				})
			}
		})
	}
}

func TestDefaultOnlyEmptyInRealTesseract(t *testing.T) {
	dir := modelstest.Dir(t, testContract(15, 16),
		modelstest.Mapping("Nothing", "", map[string]any{"text": ""}),
	)
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	recognizesImage(t, data, testPattern(15, 16, 0), "")
}

func recognizesImage(t *testing.T, data []byte, img image.Image, want string) {
	t.Helper()
	if got := recognitionOutput(t, data, img); got != want {
		t.Fatalf("recognized %q, want %q (model %d bytes)", got, want, len(data))
	}
	t.Logf("a working traineddata of %d bytes", len(data))
}

func recognitionOutput(t *testing.T, data []byte, img image.Image) string {
	t.Helper()
	bin, err := exec.LookPath("tesseract")
	if err != nil {
		t.Skip("Tesseract is not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "axx.traineddata"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	photo := filepath.Join(dir, "label.png")
	modelstest.WritePNG(t, photo, img)
	cmd := exec.Command(bin, photo, "stdout", "--tessdata-dir", dir, "-l", "axx", "--dpi", "300")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Tesseract: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// perturbed adds a small deterministic value to every pixel, leaving the
// picture's shape and its full contrast in place.
func perturbed(src image.Image) *image.Gray {
	img := image.NewGray(src.Bounds())
	for y := range src.Bounds().Dy() {
		for x := range src.Bounds().Dx() {
			delta := (x*17+y*31)%21 - 10
			v := int(src.At(x, y).(color.Gray).Y) + delta
			img.SetGray(x, y, color.Gray{Y: uint8(max(0, min(255, v)))})
		}
	}
	return img
}
