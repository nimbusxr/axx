package tflite

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
)

func init() {
	format.Register(format.Format{Type: "tflite", Name: "TFLite", Extensions: []string{".tflite", ".lite"}, Contract: ReadContract, Write: format.File(Write)})
}

// The tables' slots of LiteRT's schema that stubs and contracts use.
const (
	modelVersion, modelOperatorCodes, modelSubgraphs, modelDescription, modelBuffers = 0, 1, 2, 3, 4
	modelMetadata, modelSignatureDefs                                                = 6, 7

	codeDeprecated, codeCustom, codeVersion, codeBuiltin = 0, 1, 2, 3

	subgraphTensors, subgraphInputs, subgraphOutputs, subgraphOperators, subgraphName = 0, 1, 2, 3, 4

	tensorShape, tensorType, tensorBuffer, tensorName, tensorQuantization, tensorShapeSignature = 0, 1, 2, 3, 4, 7

	quantScale, quantZeroPoint = 2, 3

	bufferData, bufferOffset, bufferSize = 0, 1, 2

	metadataName, metadataBuffer = 0, 1

	sigInputs, sigOutputs, sigKey, sigSubgraph = 0, 1, 2, 4
	mapName, mapTensor                         = 0, 1

	operatorOpcode, operatorInputs, operatorOutputs, operatorOptionsType, operatorOptions = 0, 1, 2, 3, 4
)

// The metadata's tables' slots (metadata_schema.fbs).
const (
	mmSubgraphs                                      = 3
	smInputs, smOutputs                              = 2, 3
	tmName, tmContent, tmFiles                       = 0, 3, 6
	afName, afType                                   = 0, 2
	contentPropertiesType, contentProperties         = 0, 1
	imageColorSpace                                  = 0
	typeTensorAxisLabels, propertiesImage, colorGray = 2, 2, 2
	typeTensorValueLabels, propertiesBoundingBox     = 3, 3
	propertiesAudio, audioSampleRate                 = 4, 0
	bbIndex, bbType, bbCoordinates                   = 0, 1, 2
	smCustomMetadata, cmName, cmData                 = 9, 0, 1
	smInputUnits, tmUnits, puType, puOptions         = 5, 4, 0, 1
	unitBert, unitRegex                              = 4, 6
	bertVocab, regexDelimiter, regexVocab            = 0, 0, 1
)

// MediaPipe's object detector metadata (object_detector_metadata_schema.fbs),
// a custom metadata of the subgraph's: its anchors, and how its boxes and
// scores are decoded.
const (
	detectorMetadataName                  = "DETECTOR_METADATA"
	odAnchors, odDecoding                 = 1, 2
	anchorsFixed, fixedAnchors            = 0, 0
	anchorX, anchorY, anchorW, anchorH    = 0, 1, 2, 3
	decXScale, decYScale, decWScale       = 6, 7, 8
	decHScale, decExponential, decSigmoid = 9, 10, 11
	anchorsKey                            = "anchors"
)

// boxOrders are the orders of a box's values TFLite's metadata gives, by
// their type (boundaries, upper left, center) and index.
var boxOrders = map[string]string{
	"1:0,1,2,3": "xmin,ymin,xmax,ymax",
	"1:1,0,3,2": "ymin,xmin,ymax,xmax",
	"2:0,1,2,3": "x,y,width,height",
	"3:0,1,2,3": "cx,cy,width,height",
}

// MetadataName is the name of the metadata buffer TFLite's metadata is in.
const MetadataName = "TFLITE_METADATA"

// associatedPrefix keys a file appended to the model in a contract's files.
const associatedPrefix = "associated/"

var tensorTypes = map[int8]contract.DType{
	0: "float32", 1: "float16", 2: "int32", 3: "uint8", 4: "int64", 6: "bool", 7: "int16",
	9: "int8", 10: "float64", 12: "uint64", 15: "uint32", 16: "uint16",
}

func tensorTypeOf(t contract.DType) int8 {
	for n, d := range tensorTypes {
		if d == t {
			return n
		}
	}
	return -1
}

// ReadContract reads the contract of the TFLite model at path: its first
// subgraph's inputs and outputs (their quantization too), its schema version,
// its operators and signature, and the files it carries (its metadata buffers
// and the files appended to it), which the stub carries too. The labels of a
// classifier, and the color space of an image, come from its metadata when it
// has them.
func ReadContract(path string) (*contract.Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	c, err := Contract(b)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a TFLite model axx reads: %v", path, err).
			WithHint("axx reads TFLite (LiteRT) models whose first subgraph's inputs and outputs are tensors")
	}
	return c, nil
}

// Contract reads the contract of a TFLite model.
func Contract(model []byte) (c *contract.Contract, err error) {
	if len(model) < 8 || string(model[4:8]) != "TFL3" {
		return nil, errors.New("it does not start as a TFLite model (TFL3)")
	}
	defer func() {
		if r := recover(); r != nil { // a flatbuffer that points outside itself
			c, err = nil, fmt.Errorf("it cannot be read: %v", r)
		}
	}()
	m := root(model)
	c = &contract.Contract{Format: contract.Format{Type: "tflite", Version: int64(m.uint32(modelVersion))}}
	subgraphs := m.subs(modelSubgraphs)
	if len(subgraphs) == 0 {
		return nil, errors.New("it has no subgraph")
	}
	g := subgraphs[0]
	tensors := g.subs(subgraphTensors)
	tensorAt := func(i int32) (contract.Tensor, error) {
		if i < 0 || int(i) >= len(tensors) {
			return contract.Tensor{}, fmt.Errorf("tensor %d is not in the subgraph", i)
		}
		return tensorOf(tensors[i])
	}
	for _, i := range g.int32s(subgraphInputs) {
		t, err := tensorAt(i)
		if err != nil {
			return nil, err
		}
		guessImage(&t)
		c.Inputs = append(c.Inputs, t)
	}
	for _, i := range g.int32s(subgraphOutputs) {
		t, err := tensorAt(i)
		if err != nil {
			return nil, err
		}
		c.Outputs = append(c.Outputs, t)
	}
	if len(c.Inputs) == 0 || len(c.Outputs) == 0 {
		return nil, errors.New("its subgraph has no inputs or no outputs")
	}

	codes := m.subs(modelOperatorCodes)
	ops := map[string]bool{}
	for _, op := range g.subs(subgraphOperators) {
		i := int(op.uint32(operatorOpcode))
		if i < len(codes) {
			ops[opName(codes[i])] = true
		}
	}
	for op := range ops {
		c.Format.Operators = append(c.Format.Operators, op)
	}
	sort.Strings(c.Format.Operators)

	buffers := m.subs(modelBuffers)
	for _, md := range m.subs(modelMetadata) {
		i := int(md.uint32(metadataBuffer))
		if i >= len(buffers) {
			continue
		}
		data := bufferBytes(model, buffers[i])
		c.Carry(md.str(metadataName), fileName(md.str(metadataName)), data)
	}
	if sigs := m.subs(modelSignatureDefs); len(sigs) > 0 {
		c.Format.Signature = signatureOf(sigs[0], tensors)
	}
	files := associatedFiles(model)
	for _, name := range sortedKeys(files) {
		c.Carry(associatedPrefix+name, "associated/"+fileName(name), files[name])
	}
	if md, err := c.File(MetadataName); err == nil {
		applyMetadata(c, md, files)
	}
	return c, nil
}

func opName(code table) string {
	builtin := max(code.int32(codeBuiltin, 0), int32(code.int8(codeDeprecated)))
	if name := code.str(codeCustom); name != "" {
		return "custom:" + name
	}
	if name, ok := builtinNames[builtin]; ok {
		return name
	}
	return fmt.Sprintf("builtin:%d", builtin)
}

func tensorOf(t table) (contract.Tensor, error) {
	out := contract.Tensor{Name: t.str(tensorName)}
	typ := t.int8(tensorType)
	dt, ok := tensorTypes[typ]
	if !ok {
		return out, fmt.Errorf("%s holds elements of TFLite type %d, which stubs do not have", out.Name, typ)
	}
	out.Type = dt
	shape, signature := t.int32s(tensorShape), t.int32s(tensorShapeSignature)
	for i, d := range shape {
		if i < len(signature) && signature[i] < 0 {
			out.Shape = append(out.Shape, contract.Dim{Name: "?"})
			continue
		}
		out.Shape = append(out.Shape, contract.Dim{Size: int64(d)})
	}
	if q, ok := t.sub(tensorQuantization); ok && dt.Integer() {
		scale, zero := q.float32s(quantScale), q.int64s(quantZeroPoint)
		if len(scale) == 1 && scale[0] > 0 {
			out.Quant = &contract.Quant{Scale: float64(scale[0])}
			if len(zero) == 1 {
				out.Quant.ZeroPoint = zero[0]
			}
		}
	}
	return out, nil
}

// bufferBytes is a buffer's data: in the flatbuffer, or after it (a large
// model's), by offset and size.
func bufferBytes(model []byte, buf table) []byte {
	if d := buf.bytes(bufferData); len(d) > 0 {
		return bytes.Clone(d)
	}
	off, size := buf.uint64(bufferOffset, 0), buf.uint64(bufferSize, 0)
	if off > 1 && off+size <= uint64(len(model)) {
		return bytes.Clone(model[off : off+size])
	}
	return nil
}

func signatureOf(sig table, tensors []table) *contract.Signature {
	s := &contract.Signature{Key: sig.str(sigKey), Inputs: map[string]string{}, Outputs: map[string]string{}}
	for _, side := range []struct {
		slot int
		into map[string]string
	}{{sigInputs, s.Inputs}, {sigOutputs, s.Outputs}} {
		for _, tm := range sig.subs(side.slot) {
			i := int(tm.uint32(mapTensor))
			if i < len(tensors) {
				side.into[tensors[i].str(tensorName)] = tm.str(mapName)
			}
		}
	}
	return s
}

// associatedFiles are the files appended to the model, a zip archive, by
// name.
func associatedFiles(model []byte) map[string][]byte {
	zr, err := zip.NewReader(bytes.NewReader(model), int64(len(model)))
	if err != nil {
		return nil
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err == nil {
			out[f.Name] = b
		}
	}
	return out
}

// applyMetadata gives the contract what TFLite's metadata says: an output's
// labels (its TENSOR_AXIS_LABELS file), a detector's outputs (its boxes,
// their classes, by its TENSOR_VALUE_LABELS file, their scores and count),
// and a grayscale image input.
func applyMetadata(c *contract.Contract, md []byte, files map[string][]byte) {
	defer func() { _ = recover() }() // metadata that cannot be read says nothing
	subs := root(md).subs(mmSubgraphs)
	if len(subs) == 0 {
		return
	}
	labels := func(af table) []string {
		b, ok := files[af.str(afName)]
		if !ok {
			return nil
		}
		return strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n")
	}
	detector := false
	for i, tm := range subs[0].subs(smOutputs) {
		if i >= len(c.Outputs) {
			break
		}
		out := &c.Outputs[i]
		for _, af := range tm.subs(tmFiles) {
			switch af.int8(afType) {
			case typeTensorAxisLabels:
				out.Labels = labels(af)
				c.Task = "classification"
			case typeTensorValueLabels:
				out.Labels, out.Detection = labels(af), "classes"
			default:
				continue
			}
			break
		}
		if content, ok := tm.sub(tmContent); ok && content.uint8(contentPropertiesType) == propertiesBoundingBox {
			out.Detection, detector = "boxes", true
			if props, ok := content.sub(contentProperties); ok {
				index := props.int32s(bbIndex)
				if len(index) == 0 {
					index = []int32{0, 1, 2, 3}
				}
				key := fmt.Sprintf("%d:%s", props.uint8(bbType), strings.Trim(strings.Join(strings.Fields(fmt.Sprint(index)), ","), "[]"))
				out.Box = &contract.Box{Order: boxOrders[key], Pixels: props.int8(bbCoordinates) == 1}
			}
		}
		switch strings.ToLower(tm.str(tmName)) {
		case "score", "scores":
			out.Detection = "scores"
		case "number of detections", "count":
			out.Detection = "count"
		}
	}
	if anchors, decoding, ok := detectorMetadata(subs[0]); ok {
		for i, tm := range subs[0].subs(smOutputs) {
			if i >= len(c.Outputs) {
				break
			}
			switch tm.str(tmName) {
			case "location":
				c.Outputs[i].Detection, c.Outputs[i].Box = "locations", nil
				c.Outputs[i].Anchors = &contract.Anchors{
					File: anchorsKey,
					Scales: [4]float64{
						float64(decoding.float32(decYScale, 1)), float64(decoding.float32(decXScale, 1)),
						float64(decoding.float32(decHScale, 1)), float64(decoding.float32(decWScale, 1)),
					},
					Exponential: decoding.bool(decExponential),
					Logits:      decoding.bool(decSigmoid),
				}
				detector = true
			case "score":
				c.Outputs[i].Detection = "classScores"
			}
		}
		c.Carry(anchorsKey, "anchors.bin", anchors)
	}
	if c.Task == "classification" {
		for _, o := range c.Outputs {
			if len(o.Labels) > 0 && len(o.Shape) == 4 && o.Shape[1].Size > 1 && o.Shape[2].Size > 1 {
				c.Task = "segmentation" // its labels are a mask's pixels'
			}
		}
	}
	if detector {
		c.Task = "detection"
		for i := range c.Outputs {
			if c.Outputs[i].Detection == "" {
				c.Outputs[i].Labels = nil
			}
		}
	} else {
		for i := range c.Outputs {
			c.Outputs[i].Detection = ""
		}
	}
	c.Text = textOf(c, subs[0])
	for i, tm := range subs[0].subs(smInputs) {
		if i >= len(c.Inputs) {
			break
		}
		if content, ok := tm.sub(tmContent); ok && content.uint8(contentPropertiesType) == propertiesAudio {
			if props, ok := content.sub(contentProperties); ok {
				c.Inputs[i].Image = nil
				c.Inputs[i].Audio = &contract.Audio{SampleRate: int(props.uint32(audioSampleRate))}
			}
			continue
		}
		if c.Inputs[i].Image == nil {
			continue
		}
		content, ok := tm.sub(tmContent)
		if !ok || content.uint8(contentPropertiesType) != propertiesImage {
			continue
		}
		if props, ok := content.sub(contentProperties); ok && props.int8(imageColorSpace) == colorGray {
			c.Inputs[i].Image.Channels = "gray"
		}
	}
}

// textOf is what the metadata says of a text's tokens: its tokenizer (a
// process unit of the subgraph's inputs, or of an input's), its vocabulary
// (an associated file the contract carries) and the input of their ids (the
// one named "ids", or the only one).
func textOf(c *contract.Contract, sub table) *contract.Text {
	units := sub.subs(smInputUnits)
	ids := ""
	for i, tm := range sub.subs(smInputs) {
		if i >= len(c.Inputs) {
			break
		}
		units = append(units, tm.subs(tmUnits)...)
		if name := tm.str(tmName); name == "ids" || len(c.Inputs) == 1 {
			ids = c.Inputs[i].Name
		}
	}
	vocab := func(files []table) string {
		if len(files) == 0 {
			return ""
		}
		return associatedPrefix + files[0].str(afName)
	}
	for _, u := range units {
		opts, ok := u.sub(puOptions)
		if !ok || ids == "" {
			continue
		}
		switch u.uint8(puType) {
		case unitBert:
			return &contract.Text{IDs: ids, Tokenizer: "wordpiece", Vocabulary: vocab(opts.subs(bertVocab))}
		case unitRegex:
			return &contract.Text{IDs: ids, Tokenizer: "regex", Delimiter: opts.str(regexDelimiter), Vocabulary: vocab(opts.subs(regexVocab))}
		}
	}
	return nil
}

// detectorMetadata is MediaPipe's object detector metadata, of the
// subgraph's custom metadata: its anchors (4 float32s each: their center's x
// and y, their width and height) and its decoding options.
func detectorMetadata(sub table) ([]byte, table, bool) {
	for _, cm := range sub.subs(smCustomMetadata) {
		if cm.str(cmName) != detectorMetadataName {
			continue
		}
		od := root(cm.bytes(cmData))
		ssd, ok := od.sub(odAnchors)
		if !ok {
			return nil, table{}, false
		}
		fixed, ok := ssd.sub(anchorsFixed)
		if !ok {
			return nil, table{}, false
		}
		decoding, ok := od.sub(odDecoding)
		if !ok {
			return nil, table{}, false
		}
		var out []byte
		for _, a := range fixed.subs(fixedAnchors) {
			for _, slot := range []int{anchorX, anchorY, anchorW, anchorH} {
				out = binary.LittleEndian.AppendUint32(out, math.Float32bits(a.float32(slot, 0)))
			}
		}
		return out, decoding, len(out) > 0
	}
	return nil, table{}, false
}

// guessImage gives an input of 4 dimensions, 1 or 3 channels and a fixed
// size of 16 pixels or more an image layout: NHWC, as TFLite has images, or
// NCHW.
func guessImage(t *contract.Tensor) {
	if len(t.Shape) != 4 {
		return
	}
	ch := func(d contract.Dim) bool { return d.Fixed() && (d.Size == 1 || d.Size == 3) }
	px := func(d contract.Dim) bool { return d.Fixed() && d.Size >= 16 }
	switch {
	case ch(t.Shape[3]) && px(t.Shape[1]) && px(t.Shape[2]):
		t.Image = &contract.Image{Layout: "NHWC"}
	case ch(t.Shape[1]) && px(t.Shape[2]) && px(t.Shape[3]):
		t.Image = &contract.Image{Layout: "NCHW"}
	}
}

// fileName is a name as a file's, without what a path cannot have.
func fileName(name string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r < ' ' {
			return '_'
		}
		return r
	}, name)
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Signature is a model's signature: its key, and its inputs and outputs, by
// the names it gives them.
type Signature struct {
	Key             string
	Inputs, Outputs []SignatureTensor
}

// SignatureTensor is a signature's input or output: its name in the
// signature, and its tensor's shape.
type SignatureTensor struct {
	Name  string
	Shape []int32
}

// Signatures are a TFLite model's signatures.
func Signatures(model []byte) (sigs []Signature, err error) {
	defer func() {
		if r := recover(); r != nil {
			sigs, err = nil, fmt.Errorf("it is not a TFLite model: %v", r)
		}
	}()
	m := root(model)
	subs := m.subs(modelSubgraphs)
	for _, sig := range m.subs(modelSignatureDefs) {
		si := int(sig.uint32(sigSubgraph))
		if si >= len(subs) {
			return nil, fmt.Errorf("signature %q's subgraph %d is not the model's", sig.str(sigKey), si)
		}
		tensors := subs[si].subs(subgraphTensors)
		of := func(maps []table) []SignatureTensor {
			var out []SignatureTensor
			for _, mp := range maps {
				i := int(mp.uint32(mapTensor))
				if i >= len(tensors) {
					continue
				}
				out = append(out, SignatureTensor{Name: mp.str(mapName), Shape: tensors[i].int32s(tensorShape)})
			}
			return out
		}
		sigs = append(sigs, Signature{Key: sig.str(sigKey), Inputs: of(sig.subs(sigInputs)), Outputs: of(sig.subs(sigOutputs))})
	}
	return sigs, nil
}
