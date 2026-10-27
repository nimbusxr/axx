package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // carriers' photos are JPEG or PNG
	_ "image/png"
	"io"
	"math"
	"path"
	"slices"

	"golang.org/x/image/draw"
)

// The repacks: when a shop's packaging gives way, the carrier repacks the
// parcel and bills it (the repack service), with a photo of the repacked
// parcel beside the invoice (<carrier>/<invoice>/<parcel>.png). Billing
// pays a repack only when its photo shows a parcel: an image classifier
// (MobileNetV2, of TF Hub), which the service runs with TensorFlow
// (tensorflow.go), checks it.

// Repack is the repack service; NoEvidence the status of a repack whose
// photo shows no parcel, or that has none.
const (
	Repack     = "repack"
	NoEvidence = "NO_EVIDENCE"
)

// parcelLabels are the classes that are parcels.
var parcelLabels = []string{"carton", "crate", "envelope", "mailbag", "packet"}

// photoCertainty is how likely a parcel's class must be.
const photoCertainty = 0.5

// photoSide is the side of the square picture the model takes.
const photoSide = 224

// photoChecker tells whether a photo shows a parcel: the most likely class,
// and how likely.
type photoChecker interface {
	check(ctx context.Context, photo []byte) (label string, score float64, err error)
}

// top is the most likely class of logits, and its probability (their
// softmax's).
func top(logits []float64) (int, float64) {
	best := 0
	for i, x := range logits {
		if x > logits[best] {
			best = i
		}
	}
	var sum float64
	for _, x := range logits {
		sum += math.Exp(x - logits[best])
	}
	return best, 1 / sum
}

// modelInput is a photo as the model takes it: the middle square of it at
// 224×224, its channels (RGB) scaled to 0..1 (NHWC).
func modelInput(photo []byte) ([][][3]float32, error) {
	img, _, err := image.Decode(bytes.NewReader(photo))
	if err != nil {
		return nil, fmt.Errorf("the photo is not a picture: %w", err)
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	square := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, b.Min.X+(b.Dx()+side)/2, b.Min.Y+(b.Dy()+side)/2)
	small := image.NewRGBA(image.Rect(0, 0, photoSide, photoSide))
	draw.BiLinear.Scale(small, small.Bounds(), img, square, draw.Src, nil)
	out := make([][][3]float32, photoSide)
	for y := range photoSide {
		out[y] = make([][3]float32, photoSide)
		for x := range photoSide {
			p := small.RGBAAt(x, y)
			out[y][x] = [3]float32{float32(p.R) / 255, float32(p.G) / 255, float32(p.B) / 255}
		}
	}
	return out, nil
}

// repacked reports whether a repack's photo, beside its invoice, shows a
// parcel; why, when it does not.
func (s *service) repacked(ctx context.Context, bucket, object, invoice, parcel string) (bool, string, error) {
	name := path.Join(path.Dir(object), invoice, parcel+".png")
	r, err := s.gcp.gcs.Bucket(bucket).Object(name).NewReader(ctx)
	if err != nil {
		return false, "no photo", nil //nolint:nilerr // a repack without its photo is disputed
	}
	photo, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		return false, "", err
	}
	label, score, err := s.photos.check(ctx, photo)
	if err != nil {
		return false, "", err
	}
	s.log.Info("repack photo checked", "invoice", invoice, "parcel", parcel, "label", label, "score", score)
	if !slices.Contains(parcelLabels, label) || score < photoCertainty {
		return false, fmt.Sprintf("its photo looks like a %s", label), nil
	}
	return true, "", nil
}
