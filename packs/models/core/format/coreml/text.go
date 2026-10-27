package coreml

import (
	"fmt"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/pb"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/task"
	"github.com/nimbusxr/axx/packs/models/core/tokenize"
)

// The N-D layers (Core ML 3, iOS 13) of a stub of a text's tokens, and
// their fields: a network of them takes its inputs as their shapes are
// (EXACT_ARRAY_MAPPING).
const (
	layerConvolution, layerEqual, layerConcatND, layerTranspose  = 100, 815, 980, 985
	layerArgMax, layerEmbeddingND, layerBatchedMatMul            = 1025, 1040, 1045
	layerAddBroadcastable, reduceAll                             = 880, 3
	layerExpandDims, layerReshapeStatic, layerReduceMax          = 1125, 1140, 1260
	nnArrayMapping, arrayMappingExact                            = 5, 1
	activationReLU                                               = 10
	equalAlpha                                                   = 1
	axisField, removeDim, keepDims                               = 1, 2, 2
	axesField                                                    = 1
	matMulFirst, matMulSecond, matMulHasBias, matMulW, matMulB   = 5, 6, 7, 8, 9
	convOut, convKernelChannels, convGroups, convKernel          = 1, 2, 10, 20
	convStride, convDilation, convValid, convHasBias             = 30, 40, 50, 70
	convW, convB, validAmounts, bordersEdges, edgeStart, edgeEnd = 90, 91, 1, 10, 1, 2
)

// textLayers are the layers of a stub of a text's tokens (the contract's
// text: its input the ids the app gives), which give the first mapping whose
// text the ids have, as the app tokenizes it, or the default
// (axx_best_index, [1, 1]): the ids each compared with every token the
// mappings' texts have (equal), and with the pieces that go on a word
// (embeddingND, a table of them), as channels of the sequence (concatND,
// transpose); each text's tokens found one after another, its last word's
// end after them (convolution, a kernel a text, and ReLU over its count less
// one); anywhere (reduceMax); the texts scored by their order, the default
// under them all (batchedMatmul); the best (argMax).
func textLayers(s *stub.Stub, in *contract.Tensor) ([][]byte, error) {
	tok, err := tokenize.Of(s.Contract)
	if err != nil {
		return nil, err
	}
	length := int64(1)
	for _, d := range in.Shape {
		if d.Size <= 0 {
			return nil, fmt.Errorf("input %s has an open dimension: a Core ML stub's text is of a fixed length", in.Name)
		}
		length *= d.Size
	}
	if in.Batched() && in.Shape[0].Size != 1 {
		return nil, fmt.Errorf("input %s has a batch of %d: a Core ML stub's text is a batch of one", in.Name, in.Shape[0].Size)
	}
	// The tokens the texts have, each a channel.
	var tokens []int32
	column := map[int32]int{}
	var keys [][]int32
	width := 0
	for k, text := range s.Texts {
		ids := tok.Tokenize(text)
		if len(ids) == 0 {
			return nil, fmt.Errorf("mapping %q: the model's tokenizer finds no words in its text", s.Names[k])
		}
		if int64(len(ids)) > length {
			return nil, fmt.Errorf("mapping %q: its text is %d tokens; the model's input has %d", s.Names[k], len(ids), length)
		}
		for _, id := range ids {
			if _, ok := column[id]; !ok {
				column[id] = len(tokens)
				tokens = append(tokens, id)
			}
		}
		keys = append(keys, ids)
		width = max(width, len(ids)+1)
	}
	layers := [][]byte{
		layer("axx_ids_given", []string{in.Name}, layerReshapeStatic, pb.Msg(nil).Packed(reshapeTarget, []int64{1, length, 1})),
	}
	// Core ML declares no input a layer does not read: the others (a mask,
	// segments) are read, and add nothing (each compared with −1, which none
	// of their values is).
	ids := "axx_ids_given"
	for i, other := range s.Contract.Inputs {
		if other.Name == in.Name {
			continue
		}
		none := fmt.Sprintf("axx_unread_%d", i)
		sum := fmt.Sprintf("axx_ids_%d", i)
		layers = append(layers,
			layer(none+"_each", []string{other.Name}, layerEqual, pb.Msg(nil).Float(equalAlpha, -1)),
			layer(none, []string{none + "_each"}, layerReduceMax, pb.Msg(nil).Varint(reduceAll, 1)),
			layer(sum, []string{ids, none}, layerAddBroadcastable, nil),
		)
		ids = sum
	}
	layers = append(layers, layer("axx_ids", []string{ids}, layerReshapeStatic, pb.Msg(nil).Packed(reshapeTarget, []int64{1, length, 1})))
	if len(keys) == 0 {
		// The default alone: the first answer, 0, as no id is −1.
		return append(layers,
			layer("axx_none", []string{"axx_ids"}, layerEqual, pb.Msg(nil).Float(equalAlpha, -1)),
			layer("axx_best_index", []string{"axx_none"}, layerReduceMax, pb.Msg(nil).Packed(axesField, []int64{1}))), nil
	}
	var channels []string
	for i, id := range tokens {
		name := fmt.Sprintf("axx_token_%d", i)
		layers = append(layers, layer(name, []string{"axx_ids"}, layerEqual, pb.Msg(nil).Float(equalAlpha, float32(id))))
		channels = append(channels, name)
	}
	cont := tok.Continues()
	if cont != nil {
		table := make([]float64, len(cont))
		for i, c := range cont {
			if c {
				table[i] = 1
			}
		}
		layers = append(layers, layer("axx_goes_on", []string{"axx_ids"}, layerEmbeddingND, pb.Msg(nil).
			Varint(denseIn, uint64(len(table))).Varint(denseOut, 1).
			Bytes(denseW, pb.Msg(nil).Floats(weightFloats, table))))
		channels = append(channels, "axx_goes_on")
	}
	k := int64(len(channels))
	layers = append(layers,
		layer("axx_channels", channels, layerConcatND, pb.Msg(nil).Varint(axisField, encodeInt(-1))),
		layer("axx_by_channel", []string{"axx_channels"}, layerTranspose, pb.Msg(nil).Packed(axesField, []int64{0, 2, 1})),
		layer("axx_rows", []string{"axx_by_channel"}, layerExpandDims, pb.Msg(nil).Packed(axesField, []int64{2})),
	)
	// A kernel a text: its tokens' channels one after another, and the
	// token after them, which must not go on its last word.
	m := len(keys)
	weights := make([]float64, m*int(k)*width)
	bias := make([]float64, m)
	at := func(key, channel, x int) int { return (key*int(k)+channel)*width + x }
	for j, ids := range keys {
		for x, id := range ids {
			weights[at(j, column[id], x)] = 1
		}
		if cont != nil {
			weights[at(j, int(k)-1, len(ids))] = -1
		}
		bias[j] = -float64(len(ids) - 1)
	}
	layers = append(layers,
		layer("axx_found", []string{"axx_rows"}, layerConvolution, pb.Msg(nil).
			Varint(convOut, uint64(m)).Varint(convKernelChannels, uint64(k)).Varint(convGroups, 1).
			Packed(convKernel, []int64{1, int64(width)}).Packed(convStride, []int64{1, 1}).Packed(convDilation, []int64{1, 1}).
			Bytes(convValid, pb.Msg(nil).Bytes(validAmounts, pb.Msg(nil).
				Bytes(bordersEdges, pb.Msg(nil).Varint(edgeStart, 0).Varint(edgeEnd, 0)).
				Bytes(bordersEdges, pb.Msg(nil).Varint(edgeStart, 0).Varint(edgeEnd, uint64(width-1))))).
			Varint(convHasBias, 1).
			Bytes(convW, pb.Msg(nil).Floats(weightFloats, weights)).
			Bytes(convB, pb.Msg(nil).Floats(weightFloats, bias))),
		layer("axx_whole", []string{"axx_found"}, layerActivation, pb.Msg(nil).Bytes(activationReLU, nil)),
		layer("axx_has", []string{"axx_whole"}, layerReduceMax, pb.Msg(nil).Packed(axesField, []int64{2, 3})),
	)
	// The texts by their order, the default under them all: the weights
	// by output, then input, as Core ML reads them.
	order := make([]float64, (m+1)*m)
	for j := range m {
		order[j*m+j] = float64(m - j)
	}
	scoreBias := make([]float64, m+1)
	scoreBias[m] = 0.5
	layers = append(layers,
		layer("axx_scores", []string{"axx_has"}, layerBatchedMatMul, pb.Msg(nil).
			Varint(matMulFirst, uint64(m)).Varint(matMulSecond, uint64(m+1)).Varint(matMulHasBias, 1).
			Bytes(matMulW, pb.Msg(nil).Floats(weightFloats, order)).
			Bytes(matMulB, pb.Msg(nil).Floats(weightFloats, scoreBias))),
		layer("axx_best", []string{"axx_scores"}, layerArgMax, pb.Msg(nil).Varint(axisField, encodeInt(-1)).Varint(removeDim, 1)),
		layer("axx_best_index", []string{"axx_best"}, layerExpandDims, pb.Msg(nil).Packed(axesField, []int64{-1})),
	)
	return layers, nil
}

// textAnswers are the layers that give each output its value, from the
// answers, by the best's index (axx_best_index): a table of them
// (embeddingND), shaped as the output (reshapeStatic); a classifier's
// probabilities a vector.
func textAnswers(s *stub.Stub, c *contract.Contract) ([][]byte, error) {
	var layers [][]byte
	for _, out := range c.Outputs {
		name := out.Name
		size := task.ItemSize(&out)
		target := make([]int64, len(out.Shape))
		for i, d := range out.Shape {
			target[i] = max(d.Size, 1)
		}
		if out.Name == c.Format.PredictedProbabilities && c.Format.PredictedProbabilities != "" {
			name, target = probabilities, []int64{int64(size)}
		}
		table := make([]float64, size*len(s.Answers))
		for j, a := range s.Answers {
			v := a[out.Name]
			if len(v) != size {
				return nil, fmt.Errorf("the answer of %s gives output %s %d values, not %d", s.Names[j], out.Name, len(v), size)
			}
			for k, e := range v {
				table[k*len(s.Answers)+j] = e
			}
		}
		layers = append(layers,
			layer(name+"_values", []string{"axx_best_index"}, layerEmbeddingND, pb.Msg(nil).
				Varint(denseIn, uint64(len(s.Answers))).Varint(denseOut, uint64(size)).
				Bytes(denseW, pb.Msg(nil).Floats(weightFloats, table))),
			layer(name, []string{name + "_values"}, layerReshapeStatic, pb.Msg(nil).Packed(reshapeTarget, target)),
		)
	}
	return layers, nil
}

// encodeInt is a protobuf int64's varint of v.
func encodeInt(v int64) uint64 { return uint64(v) }
