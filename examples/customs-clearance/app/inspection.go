package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg" // inspection photos are JPEG or PNG
	_ "image/png"
	"slices"
	"strings"

	"golang.org/x/image/draw"
)

// The inspections: at the border, an officer may open a parcel and
// photograph its contents (a blob in the inspections container, which the
// carrier's border event names). The service releases a cleared parcel only
// when its contents look like what its invoice declares: an image classifier
// (MobileNetV3 Small, as OpenVINO publishes it), which the service runs with
// OpenVINO (openvino.go), names what the photo shows, and the invoice's
// tariff codes say what it may show.

// chapterClasses are, by tariff chapter (an HS code's first two digits),
// the classes of the goods the chapter has.
var chapterClasses = map[string][]string{
	"61": {"jersey", "sweatshirt", "cardigan"},
	"62": {"jersey", "sweatshirt", "cardigan"},
	"64": {"running_shoe", "sandal", "clog", "Loafer"},
	"84": {"laptop", "notebook", "desktop_computer", "hand-held_computer", "computer_keyboard", "mouse"},
	"85": {"cellular_telephone", "iPod"},
	"95": {"teddy", "jigsaw_puzzle", "toyshop"},
}

// contentsCertainty is how sure the classifier must be of goods the invoice
// does not declare to hold a parcel.
const contentsCertainty = 0.5

// photoSide is the side of the square picture the model takes.
const photoSide = 224

// contentsChecker names what a photo of a parcel's contents shows, and how
// likely.
type contentsChecker interface {
	check(ctx context.Context, photo []byte) (label string, score float64, err error)
}

// modelInput is a photo as the model takes it: the middle square of it at
// 224×224, its channels BGR, 0..255 (NCHW; the model normalizes them
// itself).
func modelInput(photo []byte) (*[3][photoSide][photoSide]float32, error) {
	img, _, err := image.Decode(bytes.NewReader(photo))
	if err != nil {
		return nil, fmt.Errorf("the inspection photo is not a picture: %w", err)
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	square := image.Rect(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2, b.Min.X+(b.Dx()+side)/2, b.Min.Y+(b.Dy()+side)/2)
	small := image.NewRGBA(image.Rect(0, 0, photoSide, photoSide))
	draw.BiLinear.Scale(small, small.Bounds(), img, square, draw.Src, nil)
	var out [3][photoSide][photoSide]float32
	for y := range photoSide {
		for x := range photoSide {
			p := small.RGBAAt(x, y)
			out[0][y][x], out[1][y][x], out[2][y][x] = float32(p.B), float32(p.G), float32(p.R)
		}
	}
	return &out, nil
}

// declares reports whether an invoice declares goods a photo's class is
// of; and whether it declares any the classifier knows.
func declares(inv Invoice, label string) (declared, known bool) {
	for _, it := range inv.Items {
		if len(it.HSCode) < 2 {
			continue
		}
		classes, ok := chapterClasses[it.HSCode[:2]]
		if !ok {
			continue
		}
		known = true
		if slices.Contains(classes, label) {
			return true, true
		}
	}
	return false, known
}

// inspect compares a cleared parcel's contents, as its inspection photo
// shows them, with its invoice: why it is held, when they differ.
func (s *service) inspect(ctx context.Context, declaration, photoName string) (string, error) {
	photo, found, err := s.download(ctx, s.names.Inspections, photoName)
	if err != nil {
		return "", err
	}
	if !found {
		return "MISSING_INSPECTION_PHOTO", nil
	}
	raw, found, err := s.download(ctx, s.names.Archive, declaration+"/invoice.json")
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("the invoice of %s, inspected, is not archived", declaration)
	}
	var inv Invoice
	if err := json.Unmarshal(raw, &inv); err != nil {
		return "", err
	}
	label, score, err := s.contents.check(ctx, photo)
	if err != nil {
		return "", err
	}
	s.log.Info("contents inspected", "declaration", declaration, "label", label, "score", score)
	if declared, known := declares(inv, label); known && !declared && score >= contentsCertainty {
		return "CONTENTS_DIFFER: the photo shows a " + strings.ReplaceAll(label, "_", " "), nil
	}
	return "", nil
}
