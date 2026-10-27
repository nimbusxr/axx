package tflite

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// metadataWithLabels is TFLite metadata (metadata_schema.fbs) whose first
// output's labels are in the associated file labels.txt.
func metadataWithLabels() []byte {
	b := builder{flatbuffers.NewBuilder(256)}
	name := b.CreateString("labels.txt")
	b.StartObject(5)
	b.PrependUOffsetTSlot(afName, name, 0)
	b.PrependInt8Slot(afType, typeTensorAxisLabels, 0)
	file := b.EndObject()
	files := b.offsets([]flatbuffers.UOffsetT{file})
	b.StartObject(7)
	b.PrependUOffsetTSlot(tmFiles, files, 0)
	output := b.EndObject()
	outputs := b.offsets([]flatbuffers.UOffsetT{output})
	b.StartObject(10)
	b.PrependUOffsetTSlot(smOutputs, outputs, 0)
	sub := b.EndObject()
	subs := b.offsets([]flatbuffers.UOffsetT{sub})
	b.StartObject(8)
	b.PrependUOffsetTSlot(mmSubgraphs, subs, 0)
	b.FinishWithFileIdentifier(b.EndObject(), []byte("M001"))
	return b.FinishedBytes()
}

func photoCheck(t *testing.T, c *contract.Contract) *stub.Stub {
	t.Helper()
	dir := modelstest.Dir(t, c,
		modelstest.Mapping("A carton at the door", "../inputs/door.png", map[string]any{"label": "carton", "score": 0.94}),
		modelstest.Mapping("Anything else", "", map[string]any{"label": "envelope", "score": 0.4}),
	)
	modelstest.Door.Write(t, filepath.Join(dir, "inputs", "door.png"))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A stub has its contract's inputs and outputs, quantization, signature and
// files: read back, it is the contract it was written from, labels and all.
func TestAStubKeepsToItsContract(t *testing.T) {
	c := modelstest.Classifier("NHWC", 1, 64, 64, 3)
	c.Format = contract.Format{Type: "tflite", Version: 3, Signature: &contract.Signature{
		Key: "serving_default", Inputs: map[string]string{"pixels": "image"}, Outputs: map[string]string{"scores": "probabilities"},
	}}
	c.Metadata = nil
	c.Inputs[0].Type, c.Inputs[0].Quant = "uint8", &contract.Quant{Scale: 1.0 / 128, ZeroPoint: 128}
	c.Inputs[0].Shape[0] = contract.Dim{Name: "?"}
	c.Outputs[0].Type, c.Outputs[0].Quant = "uint8", &contract.Quant{Scale: 1.0 / 256}
	c.Carry(MetadataName, "TFLITE_METADATA", metadataWithLabels())
	c.Carry(associatedPrefix+"labels.txt", "associated/labels.txt", []byte(strings.Join(c.Outputs[0].Labels, "\n")+"\n"))
	c.Carry("min_runtime_version", "min_runtime_version", []byte("1.5.0"))

	b, err := Write(photoCheck(t, c))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Contract(b)
	if err != nil {
		t.Fatal(err)
	}
	same := func(a, b []contract.Tensor) bool {
		return slices.EqualFunc(a, b, func(x, y contract.Tensor) bool {
			return x.Name == y.Name && x.Type == y.Type && slices.Equal(x.Shape, y.Shape) && *x.Quant == *y.Quant
		})
	}
	if !same(got.Inputs, c.Inputs) || !same(got.Outputs, c.Outputs) {
		t.Errorf("inputs %+v\noutputs %+v", got.Inputs, got.Outputs)
	}
	if got.Task != "classification" || !slices.Equal(got.Outputs[0].Labels, c.Outputs[0].Labels) {
		t.Errorf("the labels, read from the metadata the stub carries: %q %v", got.Task, got.Outputs[0].Labels)
	}
	if got.Format.Version != 3 || got.Format.Signature == nil || got.Format.Signature.Key != "serving_default" ||
		got.Format.Signature.Inputs["pixels"] != "image" || got.Format.Signature.Outputs["scores"] != "probabilities" {
		t.Errorf("format %+v, signature %+v", got.Format, got.Format.Signature)
	}
	for _, key := range []string{MetadataName, "min_runtime_version", "associated/labels.txt"} {
		want, _ := c.File(key)
		if have, err := got.File(key); err != nil || string(have) != string(want) {
			t.Errorf("file %s: %v, %q", key, err, have)
		}
	}
	for _, op := range got.Format.Operators {
		if !slices.Contains(Operators, op) {
			t.Errorf("the stub uses %s, which Operators does not list", op)
		}
	}
}

func TestATFLiteModelOfOneColorChannelIsGray(t *testing.T) {
	c := modelstest.Classifier("NHWC", 1, 32, 32, 1)
	c.Format, c.Metadata = contract.Format{Type: "tflite", Version: 3}, nil
	b, err := Write(photoCheck(t, c))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Contract(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Inputs[0].Image == nil || got.Inputs[0].Image.Layout != "NHWC" {
		t.Errorf("image %+v", got.Inputs[0].Image)
	}
	if _, err := Contract([]byte("not a model at all")); err == nil {
		t.Error("a file that is not a TFLite model has a contract")
	}
}
