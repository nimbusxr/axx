package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/jpeg" // evidence photos are JPEG or PNG
	_ "image/png"
	"math"
	"os"
	"slices"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/image/draw"
)

// The photo check: before the service settles a claim without a person, it
// makes sure the evidence shows a parcel. An image classifier (MobileNetV2,
// trained on ImageNet) runs in the service with ONNX Runtime, and a photo
// shows a parcel when the class the model finds most likely is a parcel's,
// and the model is sure enough of it.

// labels are the model's classes, in the order of its scores.
//
//go:embed labels.txt
var labelsFile string

// parcelLabels are the classes of the model that are parcels.
var parcelLabels = []string{"carton", "crate", "envelope", "mailbag", "packet"}

// photoCertainty is how likely a parcel's class must be.
const photoCertainty = 0.5

// photoSide is the side of the square picture the model takes.
const photoSide = 224

// verdict is what the photo check saw: the most likely class, how likely, and
// whether that is a parcel.
type verdict struct {
	Label  string
	Score  float64
	Parcel bool
}

// photoChecker checks evidence photos.
type photoChecker interface {
	check(photo []byte) (verdict, error)
}

// photoModel is the model, loaded.
type photoModel struct {
	mu      sync.Mutex
	session *ort.DynamicAdvancedSession
	labels  []string
}

// loadPhotoModel loads the model at path with ONNX Runtime (the shared library
// at lib).
func loadPhotoModel(path, lib string) (*photoModel, error) {
	// ONNX Runtime's telemetry, on by default on Linux, would send Microsoft
	// events about the service, and finds the machine's ID by running
	// `blkid; hostname` in a shell: in an image without one, ONNX Runtime
	// crashes as it starts.
	if err := os.Setenv("ORT_DISABLE_TELEMETRY", "1"); err != nil {
		return nil, err
	}
	ort.SetSharedLibraryPath(lib)
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("start ONNX Runtime (%s): %w", lib, err)
	}
	inputs, outputs, err := ort.GetInputOutputInfo(path)
	if err != nil {
		return nil, fmt.Errorf("read the photo model %s: %w", path, err)
	}
	if len(inputs) != 1 || len(outputs) != 1 {
		return nil, fmt.Errorf("the photo model %s has %d inputs and %d outputs, not 1 of each", path, len(inputs), len(outputs))
	}
	session, err := ort.NewDynamicAdvancedSession(path, []string{inputs[0].Name}, []string{outputs[0].Name}, nil)
	if err != nil {
		return nil, fmt.Errorf("load the photo model %s: %w", path, err)
	}
	return &photoModel{session: session, labels: strings.Split(strings.TrimSpace(labelsFile), "\n")}, nil
}

func (m *photoModel) check(photo []byte) (verdict, error) {
	pixels, err := modelInput(photo)
	if err != nil {
		return verdict{}, err
	}
	in, err := ort.NewTensor(ort.NewShape(1, 3, photoSide, photoSide), pixels)
	if err != nil {
		return verdict{}, err
	}
	defer func() { _ = in.Destroy() }()
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(len(m.labels))))
	if err != nil {
		return verdict{}, err
	}
	defer func() { _ = out.Destroy() }()
	m.mu.Lock()
	err = m.session.Run([]ort.Value{in}, []ort.Value{out})
	m.mu.Unlock()
	if err != nil {
		return verdict{}, fmt.Errorf("run the photo model: %w", err)
	}
	return judge(out.GetData(), m.labels), nil
}

// modelInput is a photo as the model takes it: the middle square of it at
// 224×224, its channels (RGB) scaled to 0..1 and normalized with ImageNet's
// means and deviations, channel by channel (NCHW).
func modelInput(photo []byte) ([]float32, error) {
	img, _, err := image.Decode(bytes.NewReader(photo))
	if err != nil {
		return nil, fmt.Errorf("the evidence is not a photo: %w", err)
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	corner := image.Pt(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2)
	crop := image.Rectangle{Min: corner, Max: corner.Add(image.Pt(side, side))}
	square := image.NewRGBA(image.Rect(0, 0, photoSide, photoSide))
	draw.BiLinear.Scale(square, square.Bounds(), img, crop, draw.Src, nil)

	mean, sd := [3]float32{0.485, 0.456, 0.406}, [3]float32{0.229, 0.224, 0.225}
	out := make([]float32, 3*photoSide*photoSide)
	for y := range photoSide {
		for x := range photoSide {
			p := square.RGBAAt(x, y)
			for c, v := range [3]uint8{p.R, p.G, p.B} {
				out[(c*photoSide+y)*photoSide+x] = (float32(v)/255 - mean[c]) / sd[c]
			}
		}
	}
	return out, nil
}

// judge reads the model's scores (logits): the most likely class, and
// whether it is a parcel's and likely enough.
func judge(logits []float32, labels []string) verdict {
	best, top := 0, float64(math.Inf(-1))
	for i, v := range logits {
		if float64(v) > top {
			best, top = i, float64(v)
		}
	}
	var sum float64
	for _, v := range logits {
		sum += math.Exp(float64(v) - top)
	}
	score := 1 / sum
	label := fmt.Sprintf("class %d", best)
	if best < len(labels) {
		label = labels[best]
	}
	return verdict{Label: label, Score: score, Parcel: slices.Contains(parcelLabels, label) && score >= photoCertainty}
}
