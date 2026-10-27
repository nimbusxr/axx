package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/match"
)

func init() {
	Register(Adapter{Task: "segmentation", Keys: []string{"mask", "masks"}, Files: []string{"mask", "masks"}, Answer: segment, Check: func(c *contract.Contract) error {
		_, err := maskLayoutOf(c)
		return err
	}})
}

// maskLayout is how a segmenter's output holds its mask, without its batch:
// its dimensions, the axes of its height and width, and its labels' (-1:
// none).
type maskLayout struct {
	out                   *contract.Tensor
	dims                  []int
	hAxis, wAx, labelAxis int
}

// maskLayoutOf finds the segmenter's output (the one with labels, or the
// only one) and its layout: [height, width, labels], [labels, height,
// width], or [height, width] (each pixel's label, or its confidence), a
// batch before them or not, and [height, width, 1] or [1, height, width]
// of confidences.
func maskLayoutOf(c *contract.Contract) (maskLayout, error) {
	var t *contract.Tensor
	for i := range c.Outputs {
		if len(c.Outputs[i].Labels) > 0 {
			t = &c.Outputs[i]
			break
		}
	}
	if t == nil {
		if len(c.Outputs) != 1 {
			return maskLayout{}, errors.New("a segmenter's contract gives its mask output's labels, or has one output")
		}
		t = &c.Outputs[0]
	}
	shape := t.Shape
	if t.Batched() {
		shape = shape[1:]
	}
	l := maskLayout{out: t, labelAxis: -1}
	for _, d := range shape {
		if !d.Fixed() {
			return maskLayout{}, fmt.Errorf("a segmenter's output %s has an open dimension", t.Name)
		}
		l.dims = append(l.dims, int(d.Size))
	}
	size := func(i int) int { return l.dims[i] }
	switch {
	case len(shape) == 2:
		l.hAxis, l.wAx = 0, 1
	case len(shape) == 3 && len(t.Labels) == 1 && size(2) == 1, len(shape) == 3 && len(t.Labels) == 0 && size(2) == 1:
		l.hAxis, l.wAx = 0, 1
	case len(shape) == 3 && len(t.Labels) > 0 && size(2) == len(t.Labels):
		l.hAxis, l.wAx, l.labelAxis = 0, 1, 2
	case len(shape) == 3 && len(t.Labels) > 0 && size(0) == len(t.Labels):
		l.hAxis, l.wAx, l.labelAxis = 1, 2, 0
	case len(shape) == 3 && len(t.Labels) == 0 && size(0) == 1:
		l.hAxis, l.wAx = 1, 2
	default:
		return maskLayout{}, fmt.Errorf("a segmenter's output %s is [height, width, labels], [labels, height, width] or [height, width] (a batch before them or not), with its labels; or [height, width] of confidences", t.Name)
	}
	return l, nil
}

// at is the index of a pixel's value (its label k's, for an output of
// labels) in the output's values.
func (l maskLayout) at(y, x, k int) int {
	pos := make([]int, len(l.dims))
	pos[l.hAxis], pos[l.wAx] = y, x
	if l.labelAxis >= 0 {
		pos[l.labelAxis] = k
	}
	i := 0
	for a, d := range l.dims {
		i = i*d + pos[a]
	}
	return i
}

// segment answers a segmenter: "mask", a picture (stretched to the
// output's size). With labels, its gray levels are each pixel's label's
// index: the output holds each pixel's labels' scores (1 for its label;
// logits of ±20 for an output of activation "none") or its label's index.
// Without, or with one label (a selfie segmenter's person), they are each
// pixel's confidence, 255 being 1: of an output of activation "none" (a
// background remover's, BiRefNet's), the logit whose sigmoid it is, ±20 for
// 0 and 1. Or "masks", a picture a label, of an
// output of each label's own confidence (a text detector's, CRAFT's: its
// text's regions and the links between their characters): each pixel's
// gray level its confidence, 255 being 1; a label without one, 0.
func segment(c *contract.Contract, answer map[string]json.RawMessage, out Values) error {
	l, err := maskLayoutOf(c)
	if err != nil {
		return err
	}
	if raw, ok := answer["masks"]; ok {
		if _, both := answer["mask"]; both {
			return errors.New(`output gives "mask" and "masks": a picture of each pixel's label, or a picture a label`)
		}
		return segmentEach(l, raw, out)
	}
	var path string
	if err := json.Unmarshal(answer["mask"], &path); err != nil {
		return errors.New("mask is not a picture's file")
	}
	img, err := match.ReadPicture(path)
	if err != nil {
		return fmt.Errorf("mask: %w", err)
	}
	t := l.out
	labels := len(t.Labels)
	if l.labelAxis < 0 && labels == 1 {
		labels = 0 // a mask of one label's: its confidences
	}
	h, w := l.dims[l.hAxis], l.dims[l.wAx]
	values := make([]float64, ItemSize(t))
	b := img.Bounds()
	gray := func(y, x int) int {
		sx := b.Min.X + (2*x+1)*b.Dx()/(2*w)
		sy := b.Min.Y + (2*y+1)*b.Dy()/(2*h)
		return int(color.GrayModel.Convert(img.At(sx, sy)).(color.Gray).Y)
	}
	on, off := 1.0, 0.0
	if t.Activation == "none" {
		on, off = 20, -20
	}
	for y := range h {
		for x := range w {
			g := gray(y, x)
			switch {
			case labels == 0 && t.Activation == "none":
				values[l.at(y, x, 0)] = logit(float64(g) / 255)
			case labels == 0:
				values[l.at(y, x, 0)] = float64(g) / 255
			case g >= labels:
				return fmt.Errorf("mask has a gray level of %d, and the segmenter %d labels: a mask's gray levels are its pixels' labels' indexes", g, labels)
			case l.labelAxis < 0:
				values[l.at(y, x, 0)] = float64(g)
			default:
				for k := range labels {
					values[l.at(y, x, k)] = off
				}
				values[l.at(y, x, g)] = on
			}
		}
	}
	out[t.Name] = Quantize(t, values)
	return nil
}

// segmentEach answers a segmenter's "masks": a picture a label, each
// pixel's gray level the label's confidence.
func segmentEach(l maskLayout, raw json.RawMessage, out Values) error {
	t := l.out
	if l.labelAxis < 0 {
		return fmt.Errorf(`masks gives a picture a label, and the segmenter's output %s has no labels' axis: its answer is "mask"`, t.Name)
	}
	var paths map[string]string
	if err := json.Unmarshal(raw, &paths); err != nil {
		return errors.New("masks is not an object of a picture's file a label")
	}
	h, w := l.dims[l.hAxis], l.dims[l.wAx]
	values := make([]float64, ItemSize(t))
	for label, path := range paths {
		k := slices.Index(t.Labels, label)
		if k < 0 {
			return fmt.Errorf("masks has %q, which is not one of the segmenter's labels (%s)", label, strings.Join(t.Labels, ", "))
		}
		img, err := match.ReadPicture(path)
		if err != nil {
			return fmt.Errorf("masks.%s: %w", label, err)
		}
		b := img.Bounds()
		for y := range h {
			for x := range w {
				sx := b.Min.X + (2*x+1)*b.Dx()/(2*w)
				sy := b.Min.Y + (2*y+1)*b.Dy()/(2*h)
				values[l.at(y, x, k)] = float64(color.GrayModel.Convert(img.At(sx, sy)).(color.Gray).Y) / 255
			}
		}
	}
	out[t.Name] = Quantize(t, values)
	return nil
}
