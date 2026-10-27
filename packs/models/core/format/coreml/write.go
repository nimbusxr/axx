package coreml

import (
	"encoding/json"
	"errors"
	"sort"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/pb"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/task"
)

// The neural network layers stubs use, and their fields.
const (
	layerName, layerInput, layerOutput                 = 1, 2, 3
	layerPooling, layerInner, layerEmbedding, layerMVN = 120, 140, 150, 165
	layerReduce, layerUnary, layerAdd, layerMultiply   = 280, 220, 230, 231
	layerReshape, layerPermute, layerActivation        = 300, 310, 130
	activationSigmoidHard, hardAlpha, hardBeta         = 41, 1, 2
	reduceSumSquare, permuteAxis                       = 4, 1
	reshapeTarget, unaryType, unaryShift, unaryLog     = 1, 1, 4, 5
	reduceAvg                                          = 1
	poolType, poolKernel, poolStride, poolValid        = 1, 10, 20, 30
	poolAverage                                        = 1
	mvnAcross, mvnVariance, mvnEpsilon                 = 1, 2, 3
	denseIn, denseOut, denseHasBias, denseW, denseBias = 1, 2, 10, 20, 21
	reduceMode, reduceAxis                             = 1, 3
	reduceArgMax, axisC                                = 9, 2
	weightFloats                                       = 1
)

// probabilities is the layer a classifier stub's probabilities come from.
const probabilities = "axx_probabilities"

// Write writes a stub as a Core ML model of its contract, at its
// specification's version: the same inputs and outputs (an image input is a
// pixel buffer, as the app gives it), the classifier's label and
// probabilities by its class labels, and its metadata.
//
// It is a neural network of the layers every Core ML has had (iOS 11): the
// matched input pooled (pooling) and its channels standardized (mvn), as
// match.Reducer does; its scores against each key and the similarity, the
// default's (innerProduct); the first best (reduce, ARGMAX); and each
// output's value, from the answers (embedding, a table by index).
func Write(s *stub.Stub) ([]byte, error) {
	c := s.Contract
	if s.Matcher == stub.TextMatcher && c.Text != nil && s.Generated == nil {
		b, err := writeText(s)
		if err != nil {
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
		}
		return b, nil
	}
	if s.Nearest == nil {
		return nil, format.Unmatched("Core ML", s)
	}
	var pd pipelineDetails
	if len(c.Format.Details) > 0 && json.Unmarshal(c.Format.Details, &pd) == nil && pd.Confidence != "" {
		b, err := writePipeline(s, pd)
		if err != nil {
			if ae := (*axxerr.Error)(nil); errors.As(err, &ae) {
				return nil, err
			}
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
		}
		return b, nil
	}
	r := s.Nearest.Reducer
	in := c.MatchedInput()
	if in.Image == nil && len(in.Shape) != 1 && len(in.Shape) != 3 {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: input %s has %d dimensions; a Core ML stub's input is an image, or a multiarray of 1 or 3", in.Name, len(in.Shape))
	}

	var layers [][]byte
	x := in.Name
	if r.Features {
		layers = append(layers, features(r, x)...)
		x = "axx_features"
	} else if r.Image {
		layers = append(layers, layer("axx_pooled", []string{x}, layerPooling, pb.Msg(nil).
			Varint(poolType, poolAverage).
			Packed(poolKernel, []int64{int64(r.KH), int64(r.KW)}).
			Packed(poolStride, []int64{int64(r.KH), int64(r.KW)}).
			Bytes(poolValid, nil)))
		x = "axx_pooled"
	}
	if r.Audio {
		layers = append(layers, spectrum(r, x)...)
		x = "axx_spectrum"
	}
	layers = append(layers, layer("axx_standardized", []string{x}, layerMVN, pb.Msg(nil).
		Varint(mvnAcross, boolv(!r.Image || r.Features)).Varint(mvnVariance, 1).Float(mvnEpsilon, float32(match.Epsilon))))

	d, n := r.Dims(), len(s.Nearest.Table.Keys)
	weights := make([]float64, (n+1)*d)
	for j, key := range s.Nearest.Table.Keys {
		for i, v := range key {
			at := i
			if r.Features {
				// The features' values are by time, then band.
				at = (i%r.PW)*r.PH + i/r.PW
			}
			weights[j*d+at] = v / float64(d)
		}
	}
	bias := make([]float64, n+1)
	bias[n] = s.Nearest.Table.Similarity
	layers = append(layers, layer("axx_scores", []string{"axx_standardized"}, layerInner, pb.Msg(nil).
		Varint(denseIn, uint64(d)).Varint(denseOut, uint64(n+1)).Varint(denseHasBias, 1).
		Bytes(denseW, pb.Msg(nil).Floats(weightFloats, weights)).
		Bytes(denseBias, pb.Msg(nil).Floats(weightFloats, bias))))
	layers = append(layers, layer("axx_best", []string{"axx_scores"}, layerReduce, pb.Msg(nil).
		Varint(reduceMode, reduceArgMax).Varint(reduceAxis, axisC)))

	for _, out := range c.Outputs {
		name := out.Name
		if out.Name == c.Format.PredictedProbabilities && c.Format.PredictedProbabilities != "" {
			name = probabilities
		}
		size := task.ItemSize(&out)
		table := make([]float64, size*len(s.Answers))
		for j, a := range s.Answers {
			v := a[out.Name]
			if len(v) != size {
				return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: the answer of %s gives output %s %d values, not %d", s.Names[j], out.Name, len(v), size)
			}
			// An embedding's weights are by output channel, then index.
			for k, e := range v {
				table[k*len(s.Answers)+j] = e
			}
		}
		// A multiarray of two or three dimensions is the blob of its
		// shape, (channels, height, width), as Core ML maps a network's
		// output to it: the table's values, in its order.
		var target []int64
		if name != probabilities && (len(out.Shape) == 2 || len(out.Shape) == 3) {
			for _, d := range out.Shape {
				target = append(target, max(d.Size, 1))
			}
			if len(target) == 2 {
				target = append(target, 1)
			}
		}
		flat := name
		if target != nil {
			flat = name + "_values"
		}
		layers = append(layers, layer(flat, []string{"axx_best"}, layerEmbedding, pb.Msg(nil).
			Varint(denseIn, uint64(len(s.Answers))).Varint(denseOut, uint64(size)).
			Bytes(denseW, pb.Msg(nil).Floats(weightFloats, table))))
		if target != nil {
			layers = append(layers, layer(name, []string{flat}, layerReshape, pb.Msg(nil).Packed(reshapeTarget, target)))
		}
	}

	return network(c, layers, false, c.Format.Version), nil
}

// network is a model of the contract whose network has the layers: a
// classifier's, of its labels, when the contract has them; its inputs as
// their shapes are (exact), for N-D layers; of the specification's version.
func network(c *contract.Contract, layers [][]byte, exact bool, version int64) []byte {
	network := pb.Msg(nil)
	for _, l := range layers {
		network = network.Bytes(classLayers, l)
	}
	if exact {
		network = network.Varint(nnArrayMapping, arrayMappingExact)
	}
	kind := protowire.Number(modelNN)
	if probs := c.Output(c.Format.PredictedProbabilities); probs != nil && len(probs.Labels) > 0 {
		kind = modelClassifier
		labels := pb.Msg(nil)
		for _, l := range probs.Labels {
			labels = labels.Str(vectorValues, l)
		}
		network = network.Bytes(classLabels, labels).Str(classProbLayer, probabilities)
	}
	if version == 0 {
		version = 4
	}
	return pb.Msg(nil).Varint(modelSpec, uint64(version)).
		Bytes(modelDescription, description(c)).
		Bytes(kind, network)
}

// writeText writes a stub of a text's tokens: a network of N-D layers
// (textLayers, textAnswers), of Core ML 3's specification (4) at the least.
func writeText(s *stub.Stub) ([]byte, error) {
	c := s.Contract
	layers, err := textLayers(s, c.MatchedInput())
	if err != nil {
		return nil, err
	}
	answers, err := textAnswers(s, c)
	if err != nil {
		return nil, err
	}
	return network(c, append(layers, answers...), true, max(c.Format.Version, 4)), nil
}

// features are the layers that reduce a sound's features x (a multiarray
// of [1, mels, frames], a blob of (1, mels, frames)) as r.Reduce does, but
// its whole scaled (axx_features, (1, frames × bands, 1, 1), by time, then
// band): pooled; each time a sequence's item of its bands, its channels
// (permute, reshape), centered and scaled to their root mean square (mvn
// across them), a time under the level left out (its centered bands' mean
// square, match.Heard of it: sigmoidHard, a clip); and all one item again.
func features(r *match.Reducer, x string) [][]byte {
	ones := make([]float64, r.PH)
	for i := range ones {
		ones[i] = 1
	}
	return [][]byte{
		layer("axx_pooled", []string{x}, layerPooling, pb.Msg(nil).
			Varint(poolType, poolAverage).
			Packed(poolKernel, []int64{int64(r.KH), int64(r.KW)}).
			Packed(poolStride, []int64{int64(r.KH), int64(r.KW)}).
			Bytes(poolValid, nil)),
		layer("axx_by_time", []string{"axx_pooled"}, layerPermute, pb.Msg(nil).Packed(permuteAxis, []int64{0, 3, 2, 1})),
		layer("axx_times", []string{"axx_by_time"}, layerReshape, pb.Msg(nil).Packed(reshapeTarget, []int64{int64(r.PW), int64(r.PH), 1, 1})),
		layer("axx_unit", []string{"axx_times"}, layerMVN, pb.Msg(nil).
			Varint(mvnAcross, 1).Varint(mvnVariance, 1).Float(mvnEpsilon, float32(match.Epsilon))),
		layer("axx_centered", []string{"axx_times"}, layerMVN, pb.Msg(nil).Varint(mvnAcross, 1).Varint(mvnVariance, 0)),
		layer("axx_square", []string{"axx_centered"}, layerReduce, pb.Msg(nil).Varint(reduceMode, reduceSumSquare).Varint(reduceAxis, axisC)),
		layer("axx_heard", []string{"axx_square"}, layerActivation, pb.Msg(nil).Bytes(activationSigmoidHard, pb.Msg(nil).
			Float(hardAlpha, float32(match.SegmentGain/float64(r.PH))).Float(hardBeta, float32(-match.SegmentLevel*match.SegmentGain)))),
		layer("axx_heard_bands", []string{"axx_heard"}, layerInner, pb.Msg(nil).
			Varint(denseIn, 1).Varint(denseOut, uint64(r.PH)).
			Bytes(denseW, pb.Msg(nil).Floats(weightFloats, ones))),
		layer("axx_heard_unit", []string{"axx_unit", "axx_heard_bands"}, layerMultiply, nil),
		layer("axx_features", []string{"axx_heard_unit"}, layerReshape, pb.Msg(nil).Packed(reshapeTarget, []int64{1, int64(r.PW * r.PH), 1, 1})),
	}
}

// spectrum are the layers that reduce a sound's samples x (a multiarray of
// N, a blob of [1, 1, N, 1, 1]) to its spectrum, as r.Spectrum does
// (axx_spectrum, [1, 1, 1, bands, 1]): its frames on the sequence's axis,
// each frame's transform and bands (innerProduct), their mean over the
// frames on the channels' axis (reduce, AVG), and its log (unary).
func spectrum(r *match.Reducer, x string) [][]byte {
	frames, frame, bins := int64(r.N/r.Frame), int64(r.Frame), int64(r.Bins())
	dense := func(name, in string, inputs, outputs int64, w []float64) []byte {
		return layer(name, []string{in}, layerInner, pb.Msg(nil).
			Varint(denseIn, uint64(inputs)).Varint(denseOut, uint64(outputs)).
			Bytes(denseW, pb.Msg(nil).Floats(weightFloats, w)))
	}
	// innerProduct's weights are [outputs, inputs]: the bases, transposed.
	cos, sin := r.Fourier()
	transpose := func(m []float64) []float64 {
		t := make([]float64, len(m))
		for i := range int(frame) {
			for j := range int(bins) {
				t[j*int(frame)+i] = m[i*int(bins)+j]
			}
		}
		return t
	}
	sum := make([]float64, match.Bands*int(bins))
	for j := range int(bins) {
		sum[r.BandOf(j)*int(bins)+j] = 1
	}
	return [][]byte{
		layer("axx_frames", []string{x}, layerReshape, pb.Msg(nil).Packed(reshapeTarget, []int64{frames, frame, 1, 1})),
		dense("axx_re", "axx_frames", frame, bins, transpose(cos)),
		dense("axx_im", "axx_frames", frame, bins, transpose(sin)),
		layer("axx_re2", []string{"axx_re", "axx_re"}, layerMultiply, nil),
		layer("axx_im2", []string{"axx_im", "axx_im"}, layerMultiply, nil),
		layer("axx_power", []string{"axx_re2", "axx_im2"}, layerAdd, nil),
		dense("axx_bands", "axx_power", bins, match.Bands, sum),
		layer("axx_by_frame", []string{"axx_bands"}, layerReshape, pb.Msg(nil).Packed(reshapeTarget, []int64{1, frames, match.Bands, 1})),
		layer("axx_mean", []string{"axx_by_frame"}, layerReduce, pb.Msg(nil).Varint(reduceMode, reduceAvg).Varint(reduceAxis, axisC)),
		layer("axx_spectrum", []string{"axx_mean"}, layerUnary, pb.Msg(nil).Varint(unaryType, unaryLog).Float(unaryShift, float32(match.LogEpsilon))),
	}
}

func layer(name string, inputs []string, kind protowire.Number, params []byte) []byte {
	m := pb.Msg(nil).Str(layerName, name)
	for _, in := range inputs {
		m = m.Str(layerInput, in)
	}
	return m.Str(layerOutput, name).Bytes(kind, params)
}

func boolv(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// description is the model's description: the contract's inputs and
// outputs (a classifier's label first, then its probabilities), and its
// metadata.
func description(c *contract.Contract) []byte {
	d := pb.Msg(nil)
	for _, t := range c.Inputs {
		d = d.Bytes(descInput, feature(&t))
	}
	if c.Format.PredictedFeature != "" {
		d = d.Bytes(descOutput, pb.Msg(nil).Str(featName, c.Format.PredictedFeature).
			Bytes(featType, pb.Msg(nil).Bytes(typeString, nil)))
	}
	for _, t := range c.Outputs {
		if t.Name == c.Format.PredictedProbabilities && c.Format.PredictedProbabilities != "" {
			d = d.Bytes(descOutput, pb.Msg(nil).Str(featName, t.Name).
				Bytes(featType, pb.Msg(nil).Bytes(typeDict, pb.Msg(nil).Bytes(dictStringKey, nil))))
			continue
		}
		d = d.Bytes(descOutput, feature(&t))
	}
	if c.Format.PredictedFeature != "" {
		d = d.Str(descPredicted, c.Format.PredictedFeature)
	}
	if c.Format.PredictedProbabilities != "" {
		d = d.Str(descProbs, c.Format.PredictedProbabilities)
	}
	if len(c.Metadata) > 0 {
		m := pb.Msg(nil)
		keys := make([]string, 0, len(c.Metadata))
		for k := range c.Metadata {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if n, ok := metadataKeys[k]; ok {
				m = m.Str(n, c.Metadata[k])
				continue
			}
			m = m.Bytes(metaUser, pb.Msg(nil).Str(entryKey, k).Str(entryValue, c.Metadata[k]))
		}
		d = d.Bytes(descMetadata, m)
	}
	return d
}

// feature is a tensor's description: a uint8 image as a pixel buffer of its
// size and colors, any other tensor as a multiarray.
func feature(t *contract.Tensor) []byte {
	var typ pb.Msg
	if t.Image != nil && t.Type == "uint8" {
		_, h, w := t.ImageDims()
		color := uint64(colorRGB)
		switch t.Image.Channels {
		case "gray":
			color = colorGray
		case "BGR":
			color = colorBGR
		}
		typ = pb.Msg(nil).Bytes(typeImage, pb.Msg(nil).
			Varint(imageWidth, uint64(t.Shape[w].Size)).Varint(imageHeight, uint64(t.Shape[h].Size)).Varint(imageColor, color))
	} else {
		shape := make([]int64, len(t.Shape))
		for i, d := range t.Shape {
			shape[i] = max(d.Size, 1)
		}
		typ = pb.Msg(nil).Bytes(typeArr, pb.Msg(nil).Packed(arrShape, shape).Varint(arrType, arrayTypeOf(t.Type)))
	}
	return pb.Msg(nil).Str(featName, t.Name).Bytes(featType, typ)
}
