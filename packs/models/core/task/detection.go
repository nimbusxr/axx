package task

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/detmath"
)

func init() {
	Register(Adapter{Task: "detection", Keys: []string{"objects"}, Answer: detect, Check: checkDetection})
}

// object is an object a detector finds: its label, its box (shares of the
// input picture's width and height, from its top left corner) and its
// score (1 by default).
type object struct {
	Label string   `json:"label"`
	Box   *box     `json:"box"`
	Score *float64 `json:"score"`
}

type box struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// detectionOutputs are a detector's outputs, by what they hold.
type detectionOutputs struct {
	boxes, classes, scores, count, anchors, locations, classScores *contract.Tensor
	// coordinates and confidence are each object's box (its center and
	// size, shares of the picture's) and its labels' scores, as Core ML's
	// non-maximum suppression has them: of as many objects as it keeps, or
	// of a fixed number of candidates before it.
	coordinates, confidence *contract.Tensor
}

func outputsOf(c *contract.Contract) detectionOutputs {
	var d detectionOutputs
	for i := range c.Outputs {
		t := &c.Outputs[i]
		switch t.Detection {
		case "boxes":
			d.boxes = t
		case "classes":
			d.classes = t
		case "scores":
			d.scores = t
		case "count":
			d.count = t
		case "anchors":
			d.anchors = t
		case "locations":
			d.locations = t
		case "classScores":
			d.classScores = t
		case "coordinates":
			d.coordinates = t
		case "confidence":
			d.confidence = t
		}
	}
	return d
}

func checkDetection(c *contract.Contract) error {
	d := outputsOf(c)
	switch {
	case d.coordinates != nil && d.confidence != nil:
		if len(d.confidence.Labels) == 0 {
			return errors.New("a detector's confidence output gives its labels")
		}
		cs, ls := d.coordinates.Shape, d.confidence.Shape
		if len(cs) != 2 || len(ls) != 2 || cs[1].Size != 4 || int(ls[1].Size) != len(d.confidence.Labels) {
			return fmt.Errorf("a detector's coordinates output is [objects, 4], and its confidence output [objects, labels] of its %d labels", len(d.confidence.Labels))
		}
	case d.locations != nil && d.classScores != nil:
		if len(d.classScores.Labels) == 0 {
			return errors.New("a detector's classScores output gives its labels")
		}
		if d.locations.Anchors == nil {
			return errors.New("a detector's locations output gives its anchors")
		}
		boxes, values := anchorShape(d.locations)
		scored, labels := anchorShape(d.classScores)
		if boxes == 0 || values < 4 || scored != boxes || labels != len(d.classScores.Labels) {
			return fmt.Errorf("a detector's locations output has 4 values or more for each anchor (%d anchors, %d values), and its classScores output a score for each anchor's labels (%d anchors, %d scores, %d labels)", boxes, values, scored, labels, len(d.classScores.Labels))
		}
	case d.anchors != nil:
		if len(d.anchors.Labels) == 0 {
			return errors.New("a detector's anchors output gives its labels")
		}
		if _, _, _, err := anchorLayout(d.anchors); err != nil {
			return err
		}
	case d.boxes != nil && d.classes != nil && d.scores != nil:
		if len(d.classes.Labels) == 0 {
			return errors.New("a detector's classes output gives its labels")
		}
		if n := ItemSize(d.boxes); n != 4*ItemSize(d.classes) || ItemSize(d.scores) != ItemSize(d.classes) {
			return fmt.Errorf("a detector's boxes output has 4 values for each of its classes' and scores' (%d boxes' values, %d classes, %d scores)", n, ItemSize(d.classes), ItemSize(d.scores))
		}
	default:
		return errors.New(`a detector's contract gives its outputs' detection: "boxes", "classes" and "scores" (and "count"), or "anchors"`)
	}
	return nil
}

// anchorLayout is an anchors output's anchors, and whether its values are
// by value (all anchors' x, then their y...: YOLOv8's [1, 4+labels,
// anchors]) or by anchor ([1, anchors, 4+labels]).
func anchorLayout(t *contract.Tensor) (anchors int, byValue bool, values int, err error) {
	shape := t.Shape
	if t.Batched() {
		shape = shape[1:]
	}
	values = 4 + len(t.Labels)
	if len(shape) == 2 && shape[0].Fixed() && shape[1].Fixed() {
		switch {
		case int(shape[0].Size) == values:
			return int(shape[1].Size), true, values, nil
		case int(shape[1].Size) == values:
			return int(shape[0].Size), false, values, nil
		}
	}
	return 0, false, 0, fmt.Errorf("output %s holds anchors of 4 values and a score for each of its %d labels: its shape is [4+%d, anchors] or [anchors, 4+%d]", t.Name, len(t.Labels), len(t.Labels), len(t.Labels))
}

// detect answers a detector: "objects", the objects it finds, each with
// its label, box and score. A detector that suppresses overlapping boxes
// itself gives them in its first boxes, classes and scores (and their
// count); one whose app does gives them in its first anchors, every other
// anchor scoring nothing.
func detect(c *contract.Contract, answer map[string]json.RawMessage, out Values) error {
	var objects []object
	dec := json.NewDecoder(bytes.NewReader(answer["objects"]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&objects); err != nil {
		return fmt.Errorf("objects is not a list of objects (label, box and score): %w", err)
	}
	d := outputsOf(c)
	labels := d.labels()
	w, h := pictureSize(c)
	for i, o := range objects {
		if !slices.Contains(labels, o.Label) {
			return fmt.Errorf("object %d's label %q is not one of the detector's %d labels", i+1, o.Label, len(labels))
		}
		if o.Box == nil {
			return fmt.Errorf("object %d has no box: {x, y, width, height}, shares of the picture's width and height", i+1)
		}
		b := *o.Box
		if b.Width <= 0 || b.Height <= 0 || b.X < 0 || b.Y < 0 || b.X+b.Width > 1+1e-9 || b.Y+b.Height > 1+1e-9 {
			return fmt.Errorf("object %d's box is not inside the picture: x, y, width and height are shares of its width and height, from its top left corner", i+1)
		}
		if o.Score != nil && (*o.Score < 0 || *o.Score > 1) {
			return fmt.Errorf("object %d's score %v is not from 0 to 1", i+1, *o.Score)
		}
	}
	if d.anchors != nil {
		return d.answerAnchors(objects, w, h, out)
	}
	if d.locations != nil {
		return d.answerLocations(c, objects, out)
	}
	if d.coordinates != nil {
		return d.answerCandidates(objects, out)
	}
	n := ItemSize(d.classes)
	if len(objects) > n {
		return fmt.Errorf("%d objects: the detector gives %d at most", len(objects), n)
	}
	boxes, classes, scores := make([]float64, 4*n), make([]float64, n), make([]float64, n)
	for i, o := range objects {
		copy(boxes[4*i:], boxValues(*o.Box, d.boxes.Box, "ymin,xmin,ymax,xmax", false, w, h))
		classes[i] = float64(slices.Index(d.classes.Labels, o.Label))
		scores[i] = scoreOf(o)
	}
	out[d.boxes.Name] = Quantize(d.boxes, boxes)
	out[d.classes.Name] = Quantize(d.classes, classes)
	out[d.scores.Name] = Quantize(d.scores, scores)
	if d.count != nil {
		out[d.count.Name] = Quantize(d.count, []float64{float64(len(objects))})
	}
	return nil
}

func (d detectionOutputs) labels() []string {
	switch {
	case d.anchors != nil:
		return d.anchors.Labels
	case d.classScores != nil:
		return d.classScores.Labels
	case d.confidence != nil:
		return d.confidence.Labels
	}
	return d.classes.Labels
}

// anchorShape is an output's anchors, and its values for each: its shape
// [1, anchors, values].
func anchorShape(t *contract.Tensor) (anchors, values int) {
	shape := t.Shape
	if t.Batched() {
		shape = shape[1:]
	}
	if len(shape) != 2 || !shape[0].Fixed() || !shape[1].Fixed() {
		return 0, 0
	}
	return int(shape[0].Size), int(shape[1].Size)
}

// answerLocations gives each object at an anchor of its own, the first
// anchors (any anchor decodes to the box exactly), its box encoded against
// it, and its label's score; every other score is nothing.
func (d detectionOutputs) answerLocations(c *contract.Contract, objects []object, out Values) error {
	a := d.locations.Anchors
	b, err := c.File(a.File)
	if err != nil {
		return fmt.Errorf("the detector's anchors: %w", err)
	}
	n, values := anchorShape(d.locations)
	if len(b) != 16*n {
		return fmt.Errorf("the detector's anchors file has %d bytes, not 16 for each of its %d anchors", len(b), n)
	}
	anchor := func(i int) (x, y, w, h float64) {
		f := func(j int) float64 { return float64(math.Float32frombits(binary.LittleEndian.Uint32(b[16*i+4*j:]))) }
		return f(0), f(1), f(2), f(3)
	}
	labels := d.classScores.Labels
	nothing := 0.0
	if a.Logits {
		nothing = logit(0)
	}
	locations, scores := make([]float64, n*values), make([]float64, n*len(labels))
	for i := range scores {
		scores[i] = nothing
	}
	if len(objects) > n {
		return fmt.Errorf("%d objects: the detector has %d anchors", len(objects), n)
	}
	for best, o := range objects {
		cx, cy, w, h := o.Box.X+float64(o.Box.Width/2), o.Box.Y+float64(o.Box.Height/2), o.Box.Width, o.Box.Height
		ax, ay, aw, ah := anchor(best)
		sh, sw := h/ah, w/aw
		if a.Exponential {
			sh, sw = detmath.Log(sh), detmath.Log(sw)
		}
		copy(locations[best*values:], []float64{(cy - ay) / ah * a.Scales[0], (cx - ax) / aw * a.Scales[1], sh * a.Scales[2], sw * a.Scales[3]})
		score := scoreOf(o)
		if a.Logits {
			score = logit(score)
		}
		scores[best*len(labels)+slices.Index(labels, o.Label)] = score
	}
	out[d.locations.Name] = Quantize(d.locations, locations)
	out[d.classScores.Name] = Quantize(d.classScores, scores)
	return nil
}

// logit is the logit whose sigmoid is p, within ±20.
func logit(p float64) float64 {
	p = math.Min(math.Max(p, 2.061153622438558e-09), 1-2.061153622438558e-09)
	return detmath.Log(p / (1 - p))
}

func (d detectionOutputs) answerAnchors(objects []object, w, h float64, out Values) error {
	t := d.anchors
	anchors, byValue, values, _ := anchorLayout(t)
	if len(objects) > anchors {
		return fmt.Errorf("%d objects: the detector has %d anchors", len(objects), anchors)
	}
	v := make([]float64, anchors*values)
	set := func(anchor, value int, x float64) {
		if byValue {
			v[value*anchors+anchor] = x
		} else {
			v[anchor*values+value] = x
		}
	}
	for i, o := range objects {
		for j, x := range boxValues(*o.Box, t.Box, "cx,cy,width,height", true, w, h) {
			set(i, j, x)
		}
		set(i, 4+slices.Index(t.Labels, o.Label), scoreOf(o))
	}
	out[t.Name] = Quantize(t, v)
	return nil
}

func scoreOf(o object) float64 {
	if o.Score == nil {
		return 1
	}
	return *o.Score
}

// boxValues are a box's four values, in the output's order and units (or
// the default's).
func boxValues(b box, f *contract.Box, order string, pixels bool, w, h float64) []float64 {
	if f != nil {
		if f.Order != "" {
			order = f.Order
		}
		pixels = f.Pixels
	}
	sx, sy := 1.0, 1.0
	if pixels {
		sx, sy = w, h
	}
	x0, y0, x1, y1 := float64(b.X*sx), float64(b.Y*sy), float64((b.X+b.Width)*sx), float64((b.Y+b.Height)*sy)
	switch order {
	case "xmin,ymin,xmax,ymax":
		return []float64{x0, y0, x1, y1}
	case "x,y,width,height":
		return []float64{x0, y0, x1 - x0, y1 - y0}
	case "cx,cy,width,height":
		return []float64{(x0 + x1) / 2, (y0 + y1) / 2, x1 - x0, y1 - y0}
	}
	return []float64{y0, x0, y1, x1}
}

// pictureSize is the matched input's width and height, in pixels, for a
// box given in them.
func pictureSize(c *contract.Contract) (w, h float64) {
	in := c.MatchedInput()
	if in == nil || in.Image == nil {
		return 1, 1
	}
	_, hi, wi := in.ImageDims()
	return float64(in.Shape[wi].Size), float64(in.Shape[hi].Size)
}

// DetectionRoles names a detector's outputs, when its contract does not:
// four outputs of a box's four values, of classes, of scores and of their
// count (TensorFlow's SSD order), or one of anchors, with the labels.
func DetectionRoles(c *contract.Contract, labels []string) error {
	if checkDetection(c) == nil {
		return nil
	}
	if len(c.Outputs) == 4 {
		roles := []string{"boxes", "classes", "scores", "count"}
		for i := range c.Outputs {
			if c.Outputs[i].Detection == "" {
				c.Outputs[i].Detection = roles[i]
			}
		}
		if len(c.Outputs[1].Labels) == 0 {
			c.Outputs[1].Labels = labels
		}
		return checkDetection(c)
	}
	if len(c.Outputs) == 1 {
		c.Outputs[0].Detection = "anchors"
		if len(c.Outputs[0].Labels) == 0 {
			c.Outputs[0].Labels = labels
		}
		return checkDetection(c)
	}
	names := make([]string, len(c.Outputs))
	for i, o := range c.Outputs {
		names[i] = o.Name
	}
	return fmt.Errorf("the detector's outputs (%s) are not four (boxes, classes, scores, count) or one (anchors): the contract gives each output's detection", strings.Join(names, ", "))
}

// answerCandidates gives each object as a candidate of Core ML's
// non-maximum suppression: its box's center and size, shares of the
// picture's, and its label's score (every other label's nothing), the first
// candidates, the rest scoring nothing, which the suppression drops. Of an
// open number of objects (the suppression's outputs), the objects alone.
func (d detectionOutputs) answerCandidates(objects []object, out Values) error {
	rows := len(objects)
	if s := d.coordinates.Shape[0]; s.Fixed() {
		rows = int(s.Size)
	}
	if len(objects) > rows {
		return fmt.Errorf("%d objects: the detector gives %d at most", len(objects), rows)
	}
	labels := d.confidence.Labels
	coordinates, confidence := make([]float64, 4*rows), make([]float64, len(labels)*rows)
	for i, o := range objects {
		b := *o.Box
		copy(coordinates[4*i:], []float64{b.X + float64(b.Width/2), b.Y + float64(b.Height/2), b.Width, b.Height})
		confidence[i*len(labels)+slices.Index(labels, o.Label)] = scoreOf(o)
	}
	out[d.coordinates.Name] = Quantize(d.coordinates, coordinates)
	out[d.confidence.Name] = Quantize(d.confidence, confidence)
	return nil
}
