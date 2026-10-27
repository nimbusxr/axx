// Package coreml reads Core ML models' contracts and writes stubs as Core ML
// models, with the protobuf wire format of Core ML's specification
// (coremltools' mlmodel/format: Model.proto, FeatureTypes.proto,
// NeuralNetwork.proto) and no generated code. Xcode compiles a stub as it
// compiles the real model, and the class it generates for it is the real
// model's.
package coreml

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/internal/textenc"
	"github.com/nimbusxr/axx/packs/models/core/internal/pb"
)

func init() {
	format.Register(format.Format{Type: "coreml", Name: "Core ML", Extensions: []string{".mlmodel", ".mlpackage"}, Contract: ReadContract, Write: format.File(Write)})
}

// The fields of Core ML's specification that stubs and contracts use.
const (
	modelSpec, modelDescription                           = 1, 2
	modelPipelineClassifier, modelClassifier, modelNN     = 200, 403, 500
	modelProgram                                          = 502
	descInput, descOutput, descPredicted, descProbs       = 1, 10, 11, 12
	descMetadata                                          = 100
	metaShort, metaVersion, metaAuthor, metaLicense       = 1, 2, 3, 4
	metaUser                                              = 100
	entryKey, entryValue                                  = 1, 2
	featName, featType                                    = 1, 3
	typeInt64, typeDouble, typeString, typeImage, typeArr = 1, 2, 3, 4, 5
	typeDict                                              = 6
	imageWidth, imageHeight, imageColor                   = 1, 2, 3
	arrShape, arrType                                     = 1, 2
	dictStringKey                                         = 2
	classLayers, classLabels, classProbLayer              = 1, 100, 200
	vectorValues                                          = 1

	colorGray, colorRGB, colorBGR = 10, 20, 30
)

// arrayTypes are the multiarrays' types stubs have.
var arrayTypes = map[uint64]contract.DType{65568: "float32", 65600: "float64", 131104: "int32", 131080: "int8", 65552: "float16"}

func arrayTypeOf(t contract.DType) uint64 {
	for n, d := range arrayTypes {
		if d == t {
			return n
		}
	}
	return 0
}

// metadataKeys are the keys of a contract's metadata that are Core ML's own
// fields, by their numbers; the others are its userDefined.
var metadataKeys = map[string]protowire.Number{"shortDescription": metaShort, "versionString": metaVersion, "author": metaAuthor, "license": metaLicense}

// ReadContract reads the contract of the Core ML model at path (an .mlmodel,
// or an .mlpackage's model): its inputs (an image's size and colors, or a
// multiarray's shape and type) and outputs (a classifier's label and
// probabilities, with its class labels, or multiarrays), its specification's
// version and its metadata.
func ReadContract(path string) (*contract.Contract, error) {
	spec := path
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		var err error
		if spec, err = packageModel(path); err != nil {
			return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "%s is not a Core ML package axx reads", path)
		}
	}
	b, err := os.ReadFile(spec)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	c, err := Contract(b)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a Core ML model axx reads: %v", path, err).
			WithHint("axx reads Core ML models (.mlmodel, .mlpackage) whose inputs are images or multiarrays, and whose outputs are a classifier's or multiarrays")
	}
	if textenc.Is(c) {
		// A text encoder, as Hugging Face's exporters write one: its
		// tokenizer beside it.
		if err := textenc.Read(c, path); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// packageModel is the specification of an .mlpackage: the item its
// manifest's root model names.
func packageModel(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "Manifest.json"))
	if err != nil {
		return "", err
	}
	var m struct {
		Root  string `json:"rootModelIdentifier"`
		Items map[string]struct {
			Path string `json:"path"`
		} `json:"itemInfoEntries"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return "", err
	}
	item, ok := m.Items[m.Root]
	if !ok {
		return "", errors.New("its manifest names no root model")
	}
	return filepath.Join(dir, "Data", filepath.FromSlash(item.Path)), nil
}

// Contract reads the contract of a Core ML model's specification.
func Contract(model []byte) (*contract.Contract, error) {
	c := &contract.Contract{Format: contract.Format{Type: "coreml"}}
	var desc, pipeline []byte
	var labels []string
	kind := ""
	err := pb.Fields(model, func(f protowire.Number, _ protowire.Type, v uint64, data []byte) error {
		switch f {
		case modelSpec:
			c.Format.Version = int64(v)
		case modelDescription:
			desc = data
		case modelClassifier:
			kind = "a neural network classifier"
			return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
				if f == classLabels {
					return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
						if f == vectorValues {
							labels = append(labels, string(data))
						}
						return nil
					})
				}
				return nil
			})
		case modelNN:
			kind = "a neural network"
		case modelProgram:
			kind = "an ML program"
		case modelPipelineClassifier:
			kind = "a pipeline classifier"
			if found, err := pipelineLabels(data); err != nil {
				return err
			} else if found != nil {
				labels = found
			}
		case modelPipeline:
			kind, pipeline = "a pipeline", data
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if desc == nil || c.Format.Version == 0 {
		return nil, errors.New("it has no description or specification version")
	}
	if pipeline != nil {
		pc, err := pipelineContract(c.Format.Version, pipeline, desc)
		if err != nil {
			return nil, fmt.Errorf("its pipeline: %w", err)
		}
		if pc != nil {
			return pc, nil
		}
	}
	if kind == "" {
		kind = "a model"
	}
	var inputs, outputs [][]byte
	err = pb.Fields(desc, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case descInput:
			inputs = append(inputs, data)
		case descOutput:
			outputs = append(outputs, data)
		case descPredicted:
			c.Format.PredictedFeature = string(data)
		case descProbs:
			c.Format.PredictedProbabilities = string(data)
		case descMetadata:
			c.Metadata = metadataOf(data)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, in := range inputs {
		t, err := featureOf(in)
		if err != nil {
			return nil, err
		}
		// A sound classifier's samples, of the rate Create ML says it took them at.
		if rate, err := strconv.Atoi(c.Metadata["sampleRate"]); err == nil && rate > 0 && t.Image == nil && len(t.Shape) == 1 {
			t.Audio = &contract.Audio{SampleRate: rate}
		}
		c.Inputs = append(c.Inputs, t)
	}
	for _, out := range outputs {
		t, err := featureOf(out)
		if err != nil {
			return nil, err
		}
		if t.Name == c.Format.PredictedFeature && t.Type == "" {
			continue // the classifier's label, which its probabilities give
		}
		if t.Name == c.Format.PredictedProbabilities {
			t.Labels = labels
			if len(labels) > 0 {
				t.Shape = []contract.Dim{{Size: int64(len(labels))}}
				c.Task = "classification"
			}
		}
		if t.Type == "" {
			return nil, fmt.Errorf("output %s is a string, and not the classifier's label: stubs have a classifier's label, or multiarrays", t.Name)
		}
		c.Outputs = append(c.Outputs, t)
	}
	if len(c.Inputs) == 0 || len(c.Outputs) == 0 {
		return nil, fmt.Errorf("%s with no inputs or no outputs", kind)
	}
	return c, nil
}

// A pipeline classifier (Model.pipelineClassifier) has its pipeline, whose
// last classifier's labels are the pipeline's (a GLM's, a support vector
// machine's, a tree ensemble's, a neural network's, nearest neighbours'):
// Create ML's sound classifiers are a feature print, then a classifier of it.
const (
	pipelineOfClassifier                   = 1
	modelGLMClassifier, modelKNNClassifier = 400, 404
)

func pipelineLabels(pipelineClassifier []byte) ([]string, error) {
	var labels []string
	err := pb.Fields(pipelineClassifier, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		if f != pipelineOfClassifier {
			return nil
		}
		return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, model []byte) error {
			if f != pipelineModels {
				return nil
			}
			return pb.Fields(model, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
				if f < modelGLMClassifier || f > modelKNNClassifier {
					return nil
				}
				var found []string
				err := pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
					if f != classLabels {
						return nil
					}
					return pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
						if f == vectorValues {
							found = append(found, string(data))
						}
						return nil
					})
				})
				if len(found) > 0 {
					labels = found
				}
				return err
			})
		})
	})
	return labels, err
}

func metadataOf(b []byte) map[string]string {
	out := map[string]string{}
	_ = pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		for key, n := range metadataKeys {
			if f == n && len(data) > 0 {
				out[key] = string(data)
			}
		}
		if f == metaUser {
			var k, v string
			_ = pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
				switch f {
				case entryKey:
					k = string(data)
				case entryValue:
					v = string(data)
				}
				return nil
			})
			out[k] = v
		}
		return nil
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// featureOf reads a feature's description: an image as a CHW tensor of
// bytes, a multiarray as its tensor, a dictionary of probabilities by string
// label as a tensor of doubles (its labels come from the classifier), and a
// string as a tensor of no type (the classifier's label).
func featureOf(b []byte) (contract.Tensor, error) {
	var t contract.Tensor
	var typ []byte
	err := pb.Fields(b, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case featName:
			t.Name = string(data)
		case featType:
			typ = data
		}
		return nil
	})
	if err != nil {
		return t, err
	}
	var kind string
	err = pb.Fields(typ, func(f protowire.Number, _ protowire.Type, _ uint64, data []byte) error {
		switch f {
		case typeImage:
			kind = "image"
			var w, h int64
			color := uint64(colorRGB)
			if err := pb.Fields(data, func(f protowire.Number, _ protowire.Type, v uint64, _ []byte) error {
				switch f {
				case imageWidth:
					w = int64(v)
				case imageHeight:
					h = int64(v)
				case imageColor:
					color = v
				}
				return nil
			}); err != nil {
				return err
			}
			channels, colors := int64(3), "RGB"
			switch color {
			case colorGray:
				channels, colors = 1, "gray"
			case colorBGR:
				colors = "BGR"
			}
			t.Type = "uint8"
			t.Shape = []contract.Dim{{Size: channels}, {Size: h}, {Size: w}}
			t.Image = &contract.Image{Layout: "CHW", Channels: colors}
		case typeArr:
			kind = "multiarray"
			var shape []int64
			var dt uint64
			if err := pb.Fields(data, func(f protowire.Number, wt protowire.Type, v uint64, data []byte) error {
				switch f {
				case arrShape:
					shape = append(shape, pb.Varints(wt, v, data)...)
				case arrType:
					dt = v
				}
				return nil
			}); err != nil {
				return err
			}
			d, ok := arrayTypes[dt]
			if !ok {
				return fmt.Errorf("%s is a multiarray of Core ML type %d, which stubs do not have", t.Name, dt)
			}
			t.Type = d
			for _, s := range shape {
				t.Shape = append(t.Shape, contract.Dim{Size: s})
			}
		case typeDict:
			kind = "dictionary"
			stringKeys := false
			_ = pb.Fields(data, func(f protowire.Number, _ protowire.Type, _ uint64, _ []byte) error {
				stringKeys = stringKeys || f == dictStringKey
				return nil
			})
			if !stringKeys {
				return fmt.Errorf("%s is a dictionary by integer: stubs have a classifier's probabilities by string label", t.Name)
			}
			t.Type = "float64"
		case typeString:
			kind = "string"
		case typeInt64:
			kind = "integer"
		case typeDouble:
			kind = "double"
		}
		return nil
	})
	if err != nil {
		return t, err
	}
	if kind == "" {
		return t, fmt.Errorf("%s has a type stubs do not have", t.Name)
	}
	if kind == "integer" || kind == "double" {
		return t, fmt.Errorf("%s is a single %s: stubs have images, multiarrays, and a classifier's label and probabilities", t.Name, kind)
	}
	return t, nil
}
