package coreml

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/pb"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A detector's pipeline is a network that scores candidates (each a box
// and its labels' scores), then a non-maximum suppression model that keeps
// the best of those that overlap, as Create ML's object detectors and
// YOLO's Core ML exports are, and as Vision runs them
// (VNRecognizedObjectObservation): the pipeline's description its image
// and the suppression's thresholds in, the kept objects' confidence and
// coordinates out.

// The fields of a pipeline (Model.pipeline) and of a non-maximum
// suppression (Model.nonMaximumSuppression).
const (
	nmsPickTop, pickTopPerClass    = 1, 1
	nmsIoU                         = 110
	modelPipeline                  = 202
	modelNMS                       = 610
	pipelineModels                 = 1
	nmsLabels                      = 100
	nmsConfidenceIn, nmsCoordsIn   = 200, 201
	nmsIoUIn, nmsThresholdIn       = 202, 203
	nmsConfidenceOut, nmsCoordsOut = 210, 211
	labelsVector                   = 1
)

// The contract's files of a detector's pipeline: its non-maximum
// suppression model, and its description.
const (
	nmsFile         = "nms"
	descriptionFile = "pipeline-description"
)

// pipelineDetails are a detector's pipeline's own settings in its
// contract: the suppression's inputs (the network's outputs) and how many
// candidates a stub's network gives.
type pipelineDetails struct {
	Confidence  string `json:"confidence"`
	Coordinates string `json:"coordinates"`
	Candidates  int    `json:"candidates"`
}

// defaultCandidates are a stub's network's candidates: the most objects it
// answers.
const defaultCandidates = 16

// nms is what a pipeline's non-maximum suppression says: its labels and
// its features' names.
type nms struct {
	iou                           float64
	perClass                      bool
	labels                        []string
	confidenceIn, coordinatesIn   string
	iouIn, thresholdIn            string
	confidenceOut, coordinatesOut string
}

func readNMS(b []byte) (*nms, error) {
	n := &nms{}
	err := pb.Fields(b, func(f protowire.Number, _ protowire.Type, v uint64, data []byte) error {
		switch f {
		case nmsPickTop:
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, v uint64, _ []byte) error {
				n.perClass = n.perClass || f == pickTopPerClass && v != 0
				return nil
			})
		case nmsIoU:
			n.iou = math.Float64frombits(v)
		case nmsLabels:
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
				if f == labelsVector {
					n.labels = append(n.labels, string(data))
				}
				return nil
			})
		case nmsConfidenceIn:
			n.confidenceIn = string(data)
		case nmsCoordsIn:
			n.coordinatesIn = string(data)
		case nmsIoUIn:
			n.iouIn = string(data)
		case nmsThresholdIn:
			n.thresholdIn = string(data)
		case nmsConfidenceOut:
			n.confidenceOut = string(data)
		case nmsCoordsOut:
			n.coordinatesOut = string(data)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(n.labels) == 0 {
		return nil, errors.New("its non-maximum suppression has no labels (its labels as integers: stubs have them as strings)")
	}
	return n, nil
}

// pipelineContract is the contract of a detector's pipeline (spec, its
// version; pipeline, its Pipeline message; desc, its description), or nil
// of a pipeline that does not end in a non-maximum suppression.
func pipelineContract(spec int64, pipeline, desc []byte) (*contract.Contract, error) {
	var models [][]byte
	if err := pb.Fields(pipeline, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		if f == pipelineModels {
			models = append(models, data)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(models) < 2 {
		return nil, nil
	}
	last := models[len(models)-1]
	var nmsMsg []byte
	if err := pb.Fields(last, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		if f == modelNMS {
			nmsMsg = data
		}
		return nil
	}); err != nil || nmsMsg == nil {
		return nil, err
	}
	n, err := readNMS(nmsMsg)
	if err != nil {
		return nil, err
	}
	c := &contract.Contract{Format: contract.Format{Type: "coreml", Version: spec}, Task: "detection"}
	outTypes := map[string]contract.DType{}
	err = pb.Fields(desc, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case descInput:
			t, err := featureOf(data)
			switch {
			case t.Name == n.iouIn || t.Name == n.thresholdIn:
				// The suppression's thresholds, which the app may give:
				// the stub's description has them as the model's.
				return nil
			case err != nil:
				return err
			}
			c.Inputs = append(c.Inputs, t)
		case descOutput:
			t, _ := featureOf(data)
			outTypes[t.Name] = t.Type
		case descMetadata:
			c.Metadata = metadataOf(data)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	typeOf := func(name string) contract.DType {
		if t := outTypes[name]; t != "" {
			return t
		}
		return "float64"
	}
	open := contract.Dim{Name: "?"}
	c.Outputs = []contract.Tensor{
		{Name: n.confidenceOut, Type: typeOf(n.confidenceOut), Shape: []contract.Dim{open, {Size: int64(len(n.labels))}}, Labels: n.labels, Detection: "confidence"},
		{Name: n.coordinatesOut, Type: typeOf(n.coordinatesOut), Shape: []contract.Dim{open, {Size: 4}}, Detection: "coordinates"},
	}
	raw, err := json.Marshal(pipelineDetails{Confidence: n.confidenceIn, Coordinates: n.coordinatesIn, Candidates: defaultCandidates})
	if err != nil {
		return nil, err
	}
	c.Format.Details = raw
	c.Carry(nmsFile, "nms.pb", last)
	c.Carry(descriptionFile, "pipeline-description.pb", desc)
	return c, nil
}

// writePipeline writes a stub of a detector's pipeline: a network of the
// stub's mappings that gives the candidates (each mapping's objects, then
// candidates that score nothing), then the real model's non-maximum
// suppression, its inputs of the network's candidates, in the real model's
// description.
func writePipeline(s *stub.Stub, d pipelineDetails) ([]byte, error) {
	c := s.Contract
	nmsModel, err := c.File(nmsFile)
	if err != nil {
		return nil, fmt.Errorf("the contract carries no non-maximum suppression of the model: %w", err)
	}
	desc, err := c.File(descriptionFile)
	if err != nil {
		return nil, fmt.Errorf("the contract carries no description of the model: %w", err)
	}
	var conf, coords *contract.Tensor
	for i := range c.Outputs {
		switch c.Outputs[i].Detection {
		case "confidence":
			conf = &c.Outputs[i]
		case "coordinates":
			coords = &c.Outputs[i]
		}
	}
	if conf == nil || coords == nil {
		return nil, errors.New("the contract's outputs are not a pipeline's confidence and coordinates")
	}
	n := d.Candidates
	if n <= 0 {
		n = defaultCandidates
	}
	labels := int64(len(conf.Labels))
	network := *c
	network.Format = contract.Format{Type: "coreml", Version: c.Format.Version}
	network.Metadata = nil
	network.Outputs = []contract.Tensor{
		{Name: d.Confidence, Type: conf.Type, Shape: []contract.Dim{{Size: int64(n)}, {Size: labels}}, Labels: conf.Labels, Detection: "confidence"},
		{Name: d.Coordinates, Type: coords.Type, Shape: []contract.Dim{{Size: int64(n)}, {Size: 4}}, Detection: "coordinates"},
	}
	ns, err := s.For(&network)
	if err != nil {
		return nil, err
	}
	if err := keptWhole(ns, nmsModel, d); err != nil {
		return nil, err
	}
	inner, err := Write(ns)
	if err != nil {
		return nil, err
	}
	suppression, err := withInputs(nmsModel, network.Outputs)
	if err != nil {
		return nil, err
	}
	pipeline := pb.Msg(nil).Bytes(pipelineModels, inner).Bytes(pipelineModels, suppression)
	return pb.Msg(nil).Varint(modelSpec, uint64(max(c.Format.Version, 3))).
		Bytes(modelDescription, desc).
		Bytes(modelPipeline, pipeline), nil
}

// withInputs is a model (a non-maximum suppression) whose description's
// inputs of the tensors' names are the tensors', its other fields its own.
func withInputs(model []byte, tensors []contract.Tensor) ([]byte, error) {
	byName := map[string]*contract.Tensor{}
	for i := range tensors {
		byName[tensors[i].Name] = &tensors[i]
	}
	out := pb.Msg(nil)
	err := pb.Fields(model, func(f protowire.Number, wt protowire.Type, v uint64, data []byte) error {
		if f != modelDescription {
			out = appendField(out, f, wt, v, data)
			return nil
		}
		desc := pb.Msg(nil)
		if err := pb.Fields(data, func(f protowire.Number, wt protowire.Type, v uint64, data []byte) error {
			if f == descInput {
				t, _ := featureOf(data)
				if mine, ok := byName[t.Name]; ok {
					desc = desc.Bytes(descInput, feature(mine))
					return nil
				}
			}
			desc = appendField(desc, f, wt, v, data)
			return nil
		}); err != nil {
			return err
		}
		out = out.Bytes(modelDescription, desc)
		return nil
	})
	return out, err
}

// appendField appends a field, as it was read, to a message.
func appendField(m pb.Msg, f protowire.Number, wt protowire.Type, v uint64, data []byte) pb.Msg {
	switch wt {
	case protowire.VarintType:
		return m.Varint(f, v)
	case protowire.Fixed64Type:
		b := protowire.AppendTag(m, f, protowire.Fixed64Type)
		return pb.Msg(protowire.AppendFixed64(b, v))
	case protowire.Fixed32Type:
		b := protowire.AppendTag(m, f, protowire.Fixed32Type)
		return pb.Msg(protowire.AppendFixed32(b, uint32(v)))
	default:
		return m.Bytes(f, data)
	}
}

// keptWhole checks that the suppression keeps every answer's objects:
// none overlaps another (of its label, when it suppresses by label) more
// than its threshold lets it.
func keptWhole(s *stub.Stub, nmsModel []byte, d pipelineDetails) error {
	var msg []byte
	if err := pb.Fields(nmsModel, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		if f == modelNMS {
			msg = data
		}
		return nil
	}); err != nil {
		return err
	}
	n, err := readNMS(msg)
	if err != nil {
		return err
	}
	if n.iou <= 0 {
		return nil
	}
	labels := len(n.labels)
	type obj struct {
		label          int
		x0, y0, x1, y1 float64
	}
	for i, a := range s.Answers {
		boxes, scores := a[d.Coordinates], a[d.Confidence]
		var objs []obj
		for r := 0; 4*r < len(boxes) && r*labels < len(scores); r++ {
			row := scores[r*labels : (r+1)*labels]
			best := 0
			for k, v := range row {
				if v > row[best] {
					best = k
				}
			}
			if row[best] <= 0 {
				continue
			}
			b := boxes[4*r : 4*r+4]
			objs = append(objs, obj{best, b[0] - float64(b[2]/2), b[1] - float64(b[3]/2), b[0] + float64(b[2]/2), b[1] + float64(b[3]/2)})
		}
		for j := range objs {
			for k := j + 1; k < len(objs); k++ {
				p, q := objs[j], objs[k]
				if n.perClass && p.label != q.label {
					continue
				}
				w := math.Max(0, math.Min(p.x1, q.x1)-math.Max(p.x0, q.x0))
				h := math.Max(0, math.Min(p.y1, q.y1)-math.Max(p.y0, q.y0))
				inter := float64(w * h)
				union := float64((p.x1-p.x0)*(p.y1-p.y0)) + float64((q.x1-q.x0)*(q.y1-q.y0)) - inter
				if union > 0 && inter/union > n.iou {
					return fmt.Errorf("mapping %q: its objects %d and %d overlap more than the model's non-maximum suppression keeps (IoU above %g): the app would see one of them", s.Names[i], j+1, k+1, n.iou)
				}
			}
		}
	}
	return nil
}
