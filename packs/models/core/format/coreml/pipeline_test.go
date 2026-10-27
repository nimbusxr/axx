package coreml

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/pb"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// detectorPipeline is a detector's pipeline as Create ML and YOLO's
// exports write them: a network (an image of 64 in) of candidates, then a
// non-maximum suppression of three labels, by label, its IoU 0.45.
func detectorPipeline() []byte {
	image := pb.Msg(nil).Str(featName, "image").Bytes(featType, pb.Msg(nil).Bytes(typeImage, pb.Msg(nil).
		Varint(imageWidth, 64).Varint(imageHeight, 64).Varint(imageColor, colorRGB)))
	double := func(name string) []byte {
		return pb.Msg(nil).Str(featName, name).Bytes(featType, pb.Msg(nil).Bytes(typeDouble, nil))
	}
	array := func(name string, dims ...int64) []byte {
		return pb.Msg(nil).Str(featName, name).Bytes(featType, pb.Msg(nil).Bytes(typeArr, pb.Msg(nil).Packed(arrShape, dims).Varint(arrType, 65600)))
	}
	labels := pb.Msg(nil).Str(labelsVector, "carton").Str(labelsVector, "envelope").Str(labelsVector, "packet")
	suppression := pb.Msg(nil).
		Bytes(nmsPickTop, pb.Msg(nil).Varint(pickTopPerClass, 1)).
		Bytes(nmsLabels, labels).
		Str(nmsConfidenceIn, "raw_confidence").Str(nmsCoordsIn, "raw_coordinates").
		Str(nmsIoUIn, "iouThreshold").Str(nmsThresholdIn, "confidenceThreshold").
		Str(nmsConfidenceOut, "confidence").Str(nmsCoordsOut, "coordinates")
	suppression = appendField(suppression, nmsIoU, 1, math.Float64bits(0.45), nil)
	nmsDesc := pb.Msg(nil).Bytes(descInput, array("raw_confidence", 100, 3)).Bytes(descInput, array("raw_coordinates", 100, 4)).
		Bytes(descInput, double("iouThreshold")).Bytes(descInput, double("confidenceThreshold")).
		Bytes(descOutput, array("confidence", 1, 3)).Bytes(descOutput, array("coordinates", 1, 4))
	nmsModel := pb.Msg(nil).Varint(modelSpec, 3).Bytes(modelDescription, nmsDesc).Bytes(modelNMS, suppression)
	network := pb.Msg(nil).Varint(modelSpec, 3).Bytes(modelNN, nil)
	desc := pb.Msg(nil).Bytes(descInput, image).Bytes(descInput, double("iouThreshold")).Bytes(descInput, double("confidenceThreshold")).
		Bytes(descOutput, array("confidence", 1, 3)).Bytes(descOutput, array("coordinates", 1, 4))
	return pb.Msg(nil).Varint(modelSpec, 3).Bytes(modelDescription, desc).
		Bytes(modelPipeline, pb.Msg(nil).Bytes(pipelineModels, network).Bytes(pipelineModels, nmsModel))
}

func TestADetectorsPipelineIsADetectorOfItsSuppressionsLabels(t *testing.T) {
	c, err := Contract(detectorPipeline())
	if err != nil {
		t.Fatal(err)
	}
	if c.Task != "detection" || len(c.Inputs) != 1 || c.Inputs[0].Name != "image" {
		t.Fatalf("contract %+v", c)
	}
	conf, coords := c.Output("confidence"), c.Output("coordinates")
	if conf == nil || coords == nil || conf.Detection != "confidence" || coords.Detection != "coordinates" || len(conf.Labels) != 3 || conf.Shape[0].Fixed() {
		t.Fatalf("outputs %+v", c.Outputs)
	}
	stubOf := func(objects ...any) (*stub.Stub, error) {
		fresh, err := Contract(detectorPipeline())
		if err != nil {
			return nil, err
		}
		dir := modelstest.Dir(t, fresh, modelstest.Mapping("Two parcels", "", map[string]any{"objects": objects}))
		return stub.Load(dir)
	}
	box := func(x, y float64) map[string]any { return map[string]any{"x": x, "y": y, "width": 0.3, "height": 0.3} }
	s, err := stubOf(map[string]any{"label": "carton", "box": box(0.1, 0.1)}, map[string]any{"label": "carton", "box": box(0.6, 0.6)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(s); err != nil {
		t.Fatalf("objects apart: %v", err)
	}
	// Of other labels, objects may overlap: the suppression is by label.
	s, err = stubOf(map[string]any{"label": "carton", "box": box(0.1, 0.1)}, map[string]any{"label": "packet", "box": box(0.12, 0.12)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(s); err != nil {
		t.Fatalf("objects of two labels: %v", err)
	}
	s, err = stubOf(map[string]any{"label": "carton", "box": box(0.1, 0.1)}, map[string]any{"label": "carton", "box": box(0.12, 0.12)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(s); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Errorf("overlapping cartons: err %v", err)
	}
}

// soundClassifier is a sound classifier as Create ML writes it: a pipeline
// classifier of a feature print, then a GLM classifier of three labels, its
// samples' rate in the metadata.
func soundClassifier() []byte {
	samples := pb.Msg(nil).Str(featName, "audioSamples").Bytes(featType, pb.Msg(nil).Bytes(typeArr, pb.Msg(nil).Packed(arrShape, []int64{15600}).Varint(arrType, 65568)))
	label := pb.Msg(nil).Str(featName, "target").Bytes(featType, pb.Msg(nil).Bytes(typeString, nil))
	probs := pb.Msg(nil).Str(featName, "targetProbability").Bytes(featType, pb.Msg(nil).Bytes(typeDict, pb.Msg(nil).Bytes(dictStringKey, nil)))
	meta := pb.Msg(nil).Str(metaAuthor, "NimbusXR").Bytes(metaUser, pb.Msg(nil).Str(entryKey, "sampleRate").Str(entryValue, "16000"))
	desc := pb.Msg(nil).Bytes(descInput, samples).Bytes(descOutput, label).Bytes(descOutput, probs).
		Str(descPredicted, "target").Str(descProbs, "targetProbability").Bytes(descMetadata, meta)
	labels := pb.Msg(nil).Str(vectorValues, "background").Str(vectorValues, "doorbell").Str(vectorValues, "knock")
	featurePrint := pb.Msg(nil).Varint(modelSpec, 4).Bytes(modelNN, nil)
	glm := pb.Msg(nil).Varint(modelSpec, 4).Bytes(modelGLMClassifier, pb.Msg(nil).Bytes(classLabels, labels))
	pipeline := pb.Msg(nil).Bytes(pipelineModels, featurePrint).Bytes(pipelineModels, glm)
	return pb.Msg(nil).Varint(modelSpec, 4).Bytes(modelDescription, desc).
		Bytes(modelPipelineClassifier, pb.Msg(nil).Bytes(pipelineOfClassifier, pipeline))
}

func TestASoundClassifiersPipelineIsAClassifierOfItsLastClassifiersLabels(t *testing.T) {
	c, err := Contract(soundClassifier())
	if err != nil {
		t.Fatal(err)
	}
	if c.Task != "classification" || len(c.Inputs) != 1 {
		t.Fatalf("contract %+v", c)
	}
	in := c.Inputs[0]
	if in.Name != "audioSamples" || in.Audio == nil || in.Audio.SampleRate != 16000 {
		t.Errorf("input %+v: its samples' rate is the metadata's", in)
	}
	probs := c.Output("targetProbability")
	if probs == nil || !slices.Equal(probs.Labels, []string{"background", "doorbell", "knock"}) || len(probs.Shape) != 1 || probs.Shape[0].Size != 3 {
		t.Fatalf("outputs %+v", c.Outputs)
	}
}
