package tflite

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/task"
	"github.com/nimbusxr/axx/packs/models/core/tokenize"
)

// The builtin operators stubs use, by code, and their options' types in the
// BuiltinOptions union.
const (
	opAdd, opAveragePool, opFullyConnected, opMul, opReshape = 0, 1, 9, 18, 22
	opGather, opTranspose, opMean, opSub, opCast, opArgMax   = 36, 39, 40, 41, 53, 56
	opPadV2, opOneHot                                        = 60, 85
	optOneHot                                                = 61
	opConcatenation, opSlice, opEqual, opReduceMax           = 2, 65, 71, 82
	optConcatenation, optSlice, optEqual                     = 10, 48, 53
	typeBool                                                 = 6
	opRsqrt, opLog                                           = 76, 73

	optPool2D, optFullyConnected, optAdd, optReshape, optMul = 5, 8, 11, 17, 21
	optGather, optTranspose, optReducer, optSub, optCast     = 23, 26, 27, 28, 37
	optArgMax                                                = 40

	typeFloat32, typeInt32 = 0, 2
	paddingValid           = 1
)

// Operators are the builtin operators stubs use.
var Operators = []string{"ADD", "ARG_MAX", "AVERAGE_POOL_2D", "CAST", "FULLY_CONNECTED", "GATHER", "MAXIMUM", "MEAN", "MINIMUM", "MUL", "RESHAPE", "RSQRT", "SUB", "TRANSPOSE"}

// Write writes a stub as a TFLite model of its contract: the same inputs and
// outputs (their quantization too) in one subgraph, the same signature, and
// the files the model carries: its metadata buffers, and the files appended
// to it, in a zip archive after the model, as TFLite's metadata has them.
// Every operator is at its first version, which every LiteRT has.
//
// Its graph reduces the matched input as match.Reducer does, in TFLite's
// layout (the reduced input's values by cell, then channel, where the
// reducer has them by channel, then cell: the keys are laid out so),
// scores it against each key and the contract's similarity with one
// FULLY_CONNECTED, and gathers each output's value from the answers of the
// best, the first of equal ones (ARG_MAX), as match.Table.Nearest does.
func Write(s *stub.Stub) ([]byte, error) {
	c := s.Contract
	g := &graph{}
	inputs := make([]int32, len(c.Inputs))
	for i := range c.Inputs {
		inputs[i] = g.tensorOf(&c.Inputs[i], -1)
	}
	in := c.MatchedInput()
	x := inputs[indexOf(c.Inputs, in.Name)]
	var best, batch int32
	switch {
	case s.Matcher == stub.TextMatcher && c.Text != nil && s.Generated == nil:
		var err error
		if best, batch, err = g.textBest(s, in, x); err != nil {
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
		}
	case s.Nearest != nil:
		best, batch = g.nearestBest(s, in, x)
	default:
		return nil, format.Unmatched("TFLite", s)
	}

	var outputs []int32
	for i := range c.Outputs {
		out, err := g.answer(s, &c.Outputs[i], best, batch)
		if err != nil {
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
		}
		outputs = append(outputs, out)
	}
	model, err := g.model(c, inputs, outputs)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeWrite, exitcode.Usage, "the stub cannot be written")
	}
	return model, nil
}

// nearestBest adds the operators that give the mapping whose key the input
// is nearest (or the default): the input reduced, its scores against each
// key and the similarity, the default's (FULLY_CONNECTED), the best
// (ARG_MAX).
func (g *graph) nearestBest(s *stub.Stub, in *contract.Tensor, input int32) (best, batch int32) {
	x := g.reduce(s.Nearest.Reducer, in, input)
	r := s.Nearest.Reducer
	d, n := r.Dims(), len(s.Nearest.Table.Keys)
	cells, channels := r.Dims(), 1
	if r.Image {
		cells, channels = r.PH*r.PW, r.C
	}
	weights := make([]float64, (n+1)*d)
	for j, key := range s.Nearest.Table.Keys {
		for ch := range channels {
			for p := range cells {
				weights[j*d+p*channels+ch] = key[ch*cells+p] / float64(d)
			}
		}
	}
	bias := make([]float64, n+1)
	bias[n] = s.Nearest.Table.Similarity
	batch = g.shapes[x][0]
	scores := g.op(opFullyConnected, optFullyConnected, func(b builder) flatbuffers.UOffsetT {
		b.StartObject(1)
		return b.EndObject()
	}, "float32", []int32{batch, int32(n + 1)}, x,
		g.constant("float32", []int32{int32(n + 1), int32(d)}, contract.DType("float32").Encode(weights)),
		g.constant("float32", []int32{int32(n + 1)}, contract.DType("float32").Encode(bias)))
	return g.argMax(scores, batch), batch
}

func (g *graph) argMax(scores, batch int32) int32 {
	return g.op(opArgMax, optArgMax, func(b builder) flatbuffers.UOffsetT {
		b.StartObject(1)
		b.PrependInt8Slot(0, typeInt32, 0)
		return b.EndObject()
	}, "int32", []int32{batch}, scores, g.int32s(1))
}

// textBest adds the operators that give the first mapping whose text the
// input's tokens have, as the app tokenizes it (or the default): each of a
// text's tokens compared with the ids at each position (SLICE, EQUAL), all
// of them alike (CAST, MUL), anywhere (REDUCE_MAX); the texts' (an input
// shorter than a text never has it) scored by their order, the default
// under them all (CONCATENATION, FULLY_CONNECTED); the best (ARG_MAX).
func (g *graph) textBest(s *stub.Stub, in *contract.Tensor, input int32) (best, batch int32, err error) {
	tok, err := tokenize.Of(s.Contract)
	if err != nil {
		return 0, 0, err
	}
	x := input
	if in.Type != "int32" {
		from := tensorTypeOf(in.Type)
		x = g.op(opCast, optCast, func(b builder) flatbuffers.UOffsetT {
			b.StartObject(2)
			b.PrependInt8Slot(0, from, -1)
			b.PrependInt8Slot(1, typeInt32, -1)
			return b.EndObject()
		}, "int32", g.shapes[x], x)
	}
	if !in.Batched() {
		x = g.reshape(x, append([]int32{1}, g.shapes[x]...))
	}
	batch, length := g.shapes[x][0], g.shapes[x][1]
	// The ids, and one more of the tokenizer's padding: the last a token
	// after it.
	x = g.op(opPadV2, 0, nil, "int32", []int32{batch, length + 1}, x,
		g.constant("int32", []int32{2, 2}, contract.DType("int32").Encode([]float64{0, 0, 0, 1})),
		g.constant("int32", []int32{}, contract.DType("int32").Encode([]float64{float64(tok.Pad())})))
	var continues int32 = -1
	if cont := tok.Continues(); cont != nil {
		table := make([]float64, len(cont))
		for i, c := range cont {
			if c {
				table[i] = 1
			}
		}
		continues = g.constant("float32", []int32{int32(len(table))}, contract.DType("float32").Encode(table))
	}
	cast := func(x int32, shape []int32) int32 {
		return g.op(opCast, optCast, func(b builder) flatbuffers.UOffsetT {
			b.StartObject(2)
			b.PrependInt8Slot(0, typeBool, -1)
			b.PrependInt8Slot(1, typeFloat32, -1)
			return b.EndObject()
		}, "float32", shape, x)
	}
	var has []int32
	for k, text := range s.Texts {
		ids := tok.Tokenize(text)
		if len(ids) == 0 {
			return 0, 0, fmt.Errorf("mapping %q: the model's tokenizer finds no words in its text", s.Names[k])
		}
		if len(ids) > int(length) {
			return 0, 0, fmt.Errorf("mapping %q: its text is %d tokens; the model's input has %d", s.Names[k], len(ids), length)
		}
		n := int32(len(ids))
		window := length - n + 1
		var all int32 = -1
		for i, id := range ids {
			slice := g.op(opSlice, optSlice, noOptions, "int32", []int32{batch, window}, x, g.int32s(0, int32(i)), g.int32s(-1, window))
			eq := cast(g.op(opEqual, optEqual, noOptions, "bool", []int32{batch, window}, slice, g.int32s(id)), []int32{batch, window})
			if all < 0 {
				all = eq
			} else {
				all = g.op(opMul, optMul, activationNone, "float32", []int32{batch, window}, all, eq)
			}
		}
		if continues >= 0 {
			// The token after a window does not go on its last word.
			next := g.op(opSlice, optSlice, noOptions, "int32", []int32{batch, window}, x, g.int32s(0, n), g.int32s(-1, window))
			gather := func(b builder) flatbuffers.UOffsetT {
				b.StartObject(2)
				return b.EndObject()
			}
			goesOn := g.op(opGather, optGather, gather, "float32", []int32{batch, window}, continues, next)
			ends := g.op(opSub, optSub, activationNone, "float32", []int32{batch, window}, g.constant("float32", []int32{1}, contract.DType("float32").Encode([]float64{1})), goesOn)
			all = g.op(opMul, optMul, activationNone, "float32", []int32{batch, window}, all, ends)
		}
		has = append(has, g.op(opReduceMax, optReducer, func(b builder) flatbuffers.UOffsetT {
			b.StartObject(1)
			b.PrependBoolSlot(0, true, false)
			return b.EndObject()
		}, "float32", []int32{batch, 1}, all, g.int32s(1)))
	}
	n := int32(len(has))
	if n == 0 {
		// The default alone: the first answer.
		return g.argMax(g.constant("float32", []int32{1, 1}, contract.DType("float32").Encode([]float64{1})), 1), 1, nil
	}
	matched := has[0]
	if n > 1 {
		matched = g.op(opConcatenation, optConcatenation, func(b builder) flatbuffers.UOffsetT {
			b.StartObject(2)
			b.PrependInt32Slot(0, 1, 0)
			return b.EndObject()
		}, "float32", []int32{batch, n}, has...)
	}
	weights := make([]float64, (n+1)*n)
	for m := range n {
		weights[m*n+m] = float64(n - m)
	}
	bias := make([]float64, n+1)
	bias[n] = 0.5
	scores := g.op(opFullyConnected, optFullyConnected, func(b builder) flatbuffers.UOffsetT {
		b.StartObject(1)
		return b.EndObject()
	}, "float32", []int32{batch, n + 1}, matched,
		g.constant("float32", []int32{n + 1, n}, contract.DType("float32").Encode(weights)),
		g.constant("float32", []int32{n + 1}, contract.DType("float32").Encode(bias)))
	return g.argMax(scores, batch), batch, nil
}

func indexOf(ts []contract.Tensor, name string) int {
	for i, t := range ts {
		if t.Name == name {
			return i
		}
	}
	return 0
}

// graph is a subgraph's tensors, buffers and operators, as they are added.
type graph struct {
	tensors []func(b builder) flatbuffers.UOffsetT
	shapes  [][]int32
	types   []contract.DType
	buffers [][]byte // the first is the empty buffer of tensors without data
	ops     []operator
}

type operator struct {
	code            int32
	inputs, outputs []int32
	optType         byte
	options         func(b builder) flatbuffers.UOffsetT
}

// tensor adds a tensor, and returns its index.
func (g *graph) tensor(name string, t contract.DType, shape, signature []int32, buffer int, quant *contract.Quant) int32 {
	if len(g.buffers) == 0 {
		g.buffers = append(g.buffers, nil)
	}
	g.tensors = append(g.tensors, func(b builder) flatbuffers.UOffsetT {
		shapeOff := b.int32s(shape)
		var sigOff, quantOff flatbuffers.UOffsetT
		if signature != nil {
			sigOff = b.int32s(signature)
		}
		if quant != nil {
			scale := b.float32s([]float32{float32(quant.Scale)})
			zero := b.int64s([]int64{quant.ZeroPoint})
			b.StartObject(7)
			b.PrependUOffsetTSlot(quantScale, scale, 0)
			b.PrependUOffsetTSlot(quantZeroPoint, zero, 0)
			quantOff = b.EndObject()
		}
		nameOff := b.CreateString(name)
		b.StartObject(11)
		b.PrependUOffsetTSlot(tensorShape, shapeOff, 0)
		b.PrependInt8Slot(tensorType, tensorTypeOf(t), 0)
		b.PrependUint32Slot(tensorBuffer, uint32(buffer), 0)
		b.PrependUOffsetTSlot(tensorName, nameOff, 0)
		if quant != nil {
			b.PrependUOffsetTSlot(tensorQuantization, quantOff, 0)
		}
		if signature != nil {
			b.PrependUOffsetTSlot(tensorShapeSignature, sigOff, 0)
		}
		return b.EndObject()
	})
	g.shapes = append(g.shapes, shape)
	g.types = append(g.types, t)
	return int32(len(g.tensors) - 1)
}

// tensorOf adds a tensor of the contract, as its model has it.
func (g *graph) tensorOf(t *contract.Tensor, buffer int) int32 {
	shape := make([]int32, len(t.Shape))
	var signature []int32
	for i, d := range t.Shape {
		shape[i] = int32(max(d.Size, 1))
		if !d.Fixed() {
			if signature == nil {
				signature = make([]int32, len(t.Shape))
				for j, e := range t.Shape {
					signature[j] = int32(max(e.Size, 1))
				}
			}
			signature[i] = -1
		}
	}
	if buffer < 0 {
		buffer = 0
	}
	return g.tensor(t.Name, t.Type, shape, signature, buffer, t.Quant)
}

func (g *graph) constant(t contract.DType, shape []int32, data []byte) int32 {
	if len(g.buffers) == 0 {
		g.buffers = append(g.buffers, nil)
	}
	g.buffers = append(g.buffers, data)
	return g.tensor(fmt.Sprintf("axx_const_%d", len(g.buffers)-1), t, shape, nil, len(g.buffers)-1, nil)
}

func (g *graph) int32s(vs ...int32) int32 {
	data := make([]byte, 4*len(vs))
	for i, v := range vs {
		binary.LittleEndian.PutUint32(data[4*i:], uint32(v))
	}
	return g.constant("int32", []int32{int32(len(vs))}, data)
}

// op adds an operator, and its output: a tensor of the type and shape.
func (g *graph) op(code int32, optType byte, options func(b builder) flatbuffers.UOffsetT, t contract.DType, shape []int32, inputs ...int32) int32 {
	out := g.tensor(fmt.Sprintf("axx_%d", len(g.tensors)), t, shape, nil, 0, nil)
	g.ops = append(g.ops, operator{code: code, inputs: inputs, outputs: []int32{out}, optType: optType, options: options})
	return out
}

func noOptions(b builder) flatbuffers.UOffsetT {
	b.StartObject(1)
	return b.EndObject()
}

func activationNone(b builder) flatbuffers.UOffsetT {
	b.StartObject(2)
	return b.EndObject()
}

func (g *graph) reshape(x int32, shape []int32) int32 {
	static := make([]int32, len(shape))
	for i, d := range shape {
		static[i] = max(d, 1)
	}
	if shape[0] < 0 {
		static[0] = g.shapes[x][0]
	}
	return g.op(opReshape, optReshape, func(b builder) flatbuffers.UOffsetT {
		newShape := b.int32s(shape)
		b.StartObject(1)
		b.PrependUOffsetTSlot(0, newShape, 0)
		return b.EndObject()
	}, g.types[x], static, x, g.int32s(shape...))
}

// reduce adds the operators that reduce the input x of the contract's
// tensor in as r does: [batch, cells × channels].
func (g *graph) reduce(r *match.Reducer, in *contract.Tensor, x int32) int32 {
	if in.Type != "float32" {
		from := tensorTypeOf(in.Type)
		x = g.op(opCast, optCast, func(b builder) flatbuffers.UOffsetT {
			b.StartObject(2)
			b.PrependInt8Slot(0, from, -1)
			b.PrependInt8Slot(1, typeFloat32, -1)
			return b.EndObject()
		}, "float32", g.shapes[x], x)
	}
	if !in.Batched() {
		x = g.reshape(x, append([]int32{1}, g.shapes[x]...))
	}
	reducer := func(b builder) flatbuffers.UOffsetT {
		b.StartObject(1)
		b.PrependBoolSlot(0, true, false)
		return b.EndObject()
	}
	pool := func(x int32, c int32) int32 {
		batch := g.shapes[x][0]
		return g.op(opAveragePool, optPool2D, func(b builder) flatbuffers.UOffsetT {
			b.StartObject(6)
			b.PrependInt8Slot(0, paddingValid, 0)
			b.PrependInt32Slot(1, int32(r.KW), 0)
			b.PrependInt32Slot(2, int32(r.KH), 0)
			b.PrependInt32Slot(3, int32(r.KW), 0)
			b.PrependInt32Slot(4, int32(r.KH), 0)
			return b.EndObject()
		}, "float32", []int32{batch, int32(r.PH), int32(r.PW), c}, x)
	}
	var cells, channels int32
	switch {
	case r.Features:
		// A sound's features, [mels, frames]: a picture of one channel,
		// each time's bands centered and scaled to their mean square.
		x = pool(g.reshape(x, []int32{-1, int32(r.H), int32(r.W), 1}), 1)
		x = g.reshape(x, []int32{-1, int32(r.PH), int32(r.PW)})
		batch := g.shapes[x][0]
		whole, stats := []int32{batch, int32(r.PH), int32(r.PW)}, []int32{batch, 1, int32(r.PW)}
		axis := g.int32s(1)
		scalar := func(v float64) int32 {
			return g.constant("float32", []int32{1}, contract.DType("float32").Encode([]float64{v}))
		}
		centered := g.op(opSub, optSub, activationNone, "float32", whole, x, g.op(opMean, optReducer, reducer, "float32", stats, x, axis))
		square := g.op(opMean, optReducer, reducer, "float32", stats, g.op(opMul, optMul, activationNone, "float32", whole, centered, centered), axis)
		// match.Heard: 0 under the level, 1 over it.
		over := g.op(opMul, optMul, activationNone, "float32", stats, g.op(opSub, optSub, activationNone, "float32", stats, square, scalar(match.SegmentLevel)), scalar(match.SegmentGain))
		heard := g.op(opMinimum, optMaximum, noOptions, "float32", stats, g.op(opMaximum, optMaximum, noOptions, "float32", stats, over, scalar(0)), scalar(1))
		inv := g.op(opRsqrt, 0, nil, "float32", stats, g.op(opMaximum, optMaximum, noOptions, "float32", stats, square, scalar(match.SegmentLevel)))
		y := g.op(opMul, optMul, activationNone, "float32", whole, g.op(opMul, optMul, activationNone, "float32", whole, centered, heard), inv)
		y = g.reshape(y, []int32{-1, int32(r.Dims())})
		flat := []int32{g.shapes[y][0], int32(r.Dims())}
		total := []int32{g.shapes[y][0], 1}
		meanSquare := g.op(opMean, optReducer, reducer, "float32", total, g.op(opMul, optMul, activationNone, "float32", flat, y, y), g.int32s(1))
		norm := g.op(opRsqrt, 0, nil, "float32", total, g.op(opAdd, optAdd, activationNone, "float32", total, meanSquare, scalar(match.Epsilon)))
		return g.op(opMul, optMul, activationNone, "float32", flat, y, norm)
	case r.Image:
		if in.Image.Layout == "NCHW" || in.Image.Layout == "CHW" {
			s := g.shapes[x]
			x = g.op(opTranspose, optTranspose, noOptions, "float32", []int32{s[0], s[2], s[3], s[1]}, x, g.int32s(0, 2, 3, 1))
		}
		x = pool(x, int32(r.C))
		cells, channels = int32(r.PH*r.PW), int32(r.C)
	case r.Audio:
		x = g.spectrum(r, x)
		cells, channels = match.Bands, 1
	default:
		cells, channels = int32(r.N), 1
	}
	x = g.reshape(x, []int32{-1, cells, channels})
	batch := g.shapes[x][0]
	stats := []int32{batch, 1, channels}
	whole := []int32{batch, cells, channels}
	axis := g.int32s(1)
	mean := g.op(opMean, optReducer, reducer, "float32", stats, x, axis)
	centered := g.op(opSub, optSub, activationNone, "float32", whole, x, mean)
	squares := g.op(opMul, optMul, activationNone, "float32", whole, centered, centered)
	variance := g.op(opMean, optReducer, reducer, "float32", stats, squares, axis)
	eps := g.constant("float32", []int32{1}, contract.DType("float32").Encode([]float64{match.Epsilon}))
	inv := g.op(opRsqrt, 0, nil, "float32", stats, g.op(opAdd, optAdd, activationNone, "float32", stats, variance, eps))
	x = g.op(opMul, optMul, activationNone, "float32", whole, centered, inv)
	return g.reshape(x, []int32{-1, int32(r.Dims())})
}

// spectrum adds the operators that reduce a sound's samples x ([batch, N])
// to its spectrum, as r.Spectrum does: [batch, bands].
func (g *graph) spectrum(r *match.Reducer, x int32) int32 {
	frames, frame, bins := int32(r.N/r.Frame), int32(r.Frame), int32(r.Bins())
	batch := g.shapes[x][0]
	x = g.reshape(x, []int32{-1, frame})
	rows := g.shapes[x][0]
	// A fully connected layer's weights are [units, inputs]: the bases,
	// transposed.
	fc := func(x int32, units, inputs int32, w []float64) int32 {
		return g.op(opFullyConnected, optFullyConnected, func(b builder) flatbuffers.UOffsetT {
			b.StartObject(1)
			return b.EndObject()
		}, "float32", []int32{rows, units}, x, g.constant("float32", []int32{units, inputs}, contract.DType("float32").Encode(w)))
	}
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
	re := fc(x, bins, frame, transpose(cos))
	im := fc(x, bins, frame, transpose(sin))
	power := g.op(opAdd, optAdd, activationNone, "float32", []int32{rows, bins},
		g.op(opMul, optMul, activationNone, "float32", []int32{rows, bins}, re, re),
		g.op(opMul, optMul, activationNone, "float32", []int32{rows, bins}, im, im))
	sum := make([]float64, match.Bands*int(bins))
	for j := range int(bins) {
		sum[r.BandOf(j)*int(bins)+j] = 1
	}
	byBand := g.reshape(fc(power, match.Bands, bins, sum), []int32{-1, frames, match.Bands})
	mean := g.op(opMean, optReducer, func(b builder) flatbuffers.UOffsetT {
		b.StartObject(1)
		b.PrependBoolSlot(0, false, false)
		return b.EndObject()
	}, "float32", []int32{batch, match.Bands}, byBand, g.int32s(1))
	eps := g.constant("float32", []int32{1}, contract.DType("float32").Encode([]float64{match.LogEpsilon}))
	return g.op(opLog, 0, nil, "float32", []int32{batch, match.Bands}, g.op(opAdd, optAdd, activationNone, "float32", []int32{batch, match.Bands}, mean, eps))
}

// answer adds the operators that give the output out the answer of the
// mapping best names. Answers that end alike (format.Tail) are stored to
// where they do, and padded with the value they end in.
func (g *graph) answer(s *stub.Stub, out *contract.Tensor, best, batch int32) (int32, error) {
	item := out.Shape
	if out.Batched() {
		item = item[1:]
	}
	dims := []int32{int32(len(s.Answers))}
	for _, d := range item {
		dims = append(dims, int32(max(d.Size, 1)))
	}
	size := task.ItemSize(out)
	for i, a := range s.Answers {
		if v := a[out.Name]; len(v) != size {
			return 0, fmt.Errorf("the answer of %s gives output %s %d values, not %d", s.Names[i], out.Name, len(v), size)
		}
	}
	if len(item) > 0 && item[len(item)-1].Fixed() {
		if result, ok := g.oneHot(s, out, best, batch, int(item[len(item)-1].Size)); ok {
			return result, nil
		}
	}
	prefix, tail, padded := format.Tail(s.Answers, out.Name)
	if padded {
		dims = []int32{int32(len(s.Answers)), int32(prefix)}
	} else {
		prefix = size
	}
	values := make([]float64, 0, prefix*len(s.Answers))
	for _, a := range s.Answers {
		values = append(values, a[out.Name][:prefix]...)
	}
	g.buffers = append(g.buffers, out.Type.Encode(values))
	table := g.tensor("axx_answers_"+out.Name, out.Type, dims, nil, len(g.buffers)-1, out.Quant)
	gather := func(b builder) flatbuffers.UOffsetT {
		b.StartObject(2)
		return b.EndObject()
	}
	result := g.tensorOf(out, 0)
	gatheredShape := append([]int32{batch}, dims[1:]...)
	if !padded && out.Batched() && slicesEqual(g.shapes[result], gatheredShape) {
		g.ops = append(g.ops, operator{code: opGather, inputs: []int32{table, best}, outputs: []int32{result}, optType: optGather, options: gather})
		return result, nil
	}
	gathered := g.op(opGather, optGather, gather, out.Type, gatheredShape, table, best)
	if padded {
		paddings := g.constant("int32", []int32{2, 2}, contract.DType("int32").Encode([]float64{0, 0, 0, float64(size - prefix)}))
		g.buffers = append(g.buffers, out.Type.Encode([]float64{tail}))
		value := g.tensor(fmt.Sprintf("axx_%d", len(g.tensors)), out.Type, []int32{}, nil, len(g.buffers)-1, out.Quant)
		gathered = g.op(opPadV2, 0, nil, out.Type, []int32{batch, int32(size)}, gathered, paddings, value)
	}
	g.ops = append(g.ops, operator{code: opReshape, inputs: []int32{gathered, g.int32s(g.shapes[result]...)}, outputs: []int32{result}, optType: optReshape, options: func(b builder) flatbuffers.UOffsetT {
		newShape := b.int32s(g.shapes[result])
		b.StartObject(1)
		b.PrependUOffsetTSlot(0, newShape, 0)
		return b.EndObject()
	}})
	return result, nil
}

// oneHot adds, for answers one-hot along the output's last axis (of
// depth: format.OneHot), the operators that give the output the answer of
// the mapping best names from each group's on value's index, stored as uint8
// (or int32), one-hot again.
func (g *graph) oneHot(s *stub.Stub, out *contract.Tensor, best, batch int32, depth int) (int32, bool) {
	indexes, on, off, ok := format.OneHot(s.Answers, out.Name, depth)
	if !ok {
		return 0, false
	}
	kind := contract.DType("uint8")
	if depth > 256 {
		kind = "int32"
	}
	var flat []float64
	for _, idx := range indexes {
		for _, i := range idx {
			flat = append(flat, float64(i))
		}
	}
	groups := int32(len(indexes[0]))
	table := g.constant(kind, []int32{int32(len(s.Answers)), groups}, kind.Encode(flat))
	gather := func(b builder) flatbuffers.UOffsetT {
		b.StartObject(2)
		return b.EndObject()
	}
	x := g.op(opGather, optGather, gather, kind, []int32{batch, groups}, table, best)
	if kind != "int32" {
		x = g.op(opCast, optCast, func(b builder) flatbuffers.UOffsetT {
			b.StartObject(2)
			b.PrependInt8Slot(0, tensorTypeOf(kind), -1)
			b.PrependInt8Slot(1, tensorTypeOf("int32"), -1)
			return b.EndObject()
		}, "int32", []int32{batch, groups}, x)
	}
	g.buffers = append(g.buffers, out.Type.Encode([]float64{on}))
	onValue := g.tensor(fmt.Sprintf("axx_%d", len(g.tensors)), out.Type, []int32{}, nil, len(g.buffers)-1, out.Quant)
	g.buffers = append(g.buffers, out.Type.Encode([]float64{off}))
	offValue := g.tensor(fmt.Sprintf("axx_%d", len(g.tensors)), out.Type, []int32{}, nil, len(g.buffers)-1, out.Quant)
	depthValue := g.constant("int32", []int32{}, contract.DType("int32").Encode([]float64{float64(depth)}))
	x = g.op(opOneHot, optOneHot, func(b builder) flatbuffers.UOffsetT {
		b.StartObject(1)
		b.PrependInt32Slot(0, -1, 0)
		return b.EndObject()
	}, out.Type, []int32{batch, groups, int32(depth)}, x, depthValue, onValue, offValue)
	result := g.tensorOf(out, 0)
	g.ops = append(g.ops, operator{code: opReshape, inputs: []int32{x, g.int32s(g.shapes[result]...)}, outputs: []int32{result}, optType: optReshape, options: func(b builder) flatbuffers.UOffsetT {
		newShape := b.int32s(g.shapes[result])
		b.StartObject(1)
		b.PrependUOffsetTSlot(0, newShape, 0)
		return b.EndObject()
	}})
	return result, true
}

func slicesEqual(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// model is the graph as a TFLite model of the contract, with its files.
func (g *graph) model(c *contract.Contract, inputs, outputs []int32) ([]byte, error) {
	b := builder{flatbuffers.NewBuilder(1 << 16)}

	// The metadata buffers the model carries, after the graph's buffers.
	type meta struct {
		name   string
		buffer int
	}
	var metas []meta
	var associated []string
	for _, key := range sortedFileKeys(c.Files) {
		if strings.HasPrefix(key, associatedPrefix) {
			associated = append(associated, key)
			continue
		}
		data, err := c.File(key)
		if err != nil {
			return nil, err
		}
		g.buffers = append(g.buffers, data)
		metas = append(metas, meta{key, len(g.buffers) - 1})
	}

	bufferOffs := make([]flatbuffers.UOffsetT, len(g.buffers))
	for i, data := range g.buffers {
		var dataOff flatbuffers.UOffsetT
		if len(data) > 0 {
			dataOff = b.data(data)
		}
		b.StartObject(3)
		if len(data) > 0 {
			b.PrependUOffsetTSlot(bufferData, dataOff, 0)
		}
		bufferOffs[i] = b.EndObject()
	}
	tensorOffs := make([]flatbuffers.UOffsetT, len(g.tensors))
	for i, t := range g.tensors {
		tensorOffs[i] = t(b)
	}
	codeIndex := map[int32]uint32{}
	var codes []int32
	opOffs := make([]flatbuffers.UOffsetT, len(g.ops))
	for i, op := range g.ops {
		idx, ok := codeIndex[op.code]
		if !ok {
			idx = uint32(len(codes))
			codeIndex[op.code] = idx
			codes = append(codes, op.code)
		}
		ins, outs := b.int32s(op.inputs), b.int32s(op.outputs)
		var opts flatbuffers.UOffsetT
		if op.options != nil {
			opts = op.options(b)
		}
		b.StartObject(14)
		b.PrependUint32Slot(operatorOpcode, idx, 0)
		b.PrependUOffsetTSlot(operatorInputs, ins, 0)
		b.PrependUOffsetTSlot(operatorOutputs, outs, 0)
		if op.options != nil {
			b.PrependUint8Slot(operatorOptionsType, op.optType, 0)
			b.PrependUOffsetTSlot(operatorOptions, opts, 0)
		}
		opOffs[i] = b.EndObject()
	}
	codeOffs := make([]flatbuffers.UOffsetT, len(codes))
	for i, code := range codes {
		b.StartObject(4)
		b.PrependInt8Slot(codeDeprecated, int8(code), 0)
		b.PrependInt32Slot(codeVersion, 1, 0)
		b.PrependInt32Slot(codeBuiltin, code, 0)
		codeOffs[i] = b.EndObject()
	}

	tensorsVec, opsVec := b.offsets(tensorOffs), b.offsets(opOffs)
	insVec, outsVec := b.int32s(inputs), b.int32s(outputs)
	name := b.CreateString("main")
	b.StartObject(6)
	b.PrependUOffsetTSlot(subgraphTensors, tensorsVec, 0)
	b.PrependUOffsetTSlot(subgraphInputs, insVec, 0)
	b.PrependUOffsetTSlot(subgraphOutputs, outsVec, 0)
	b.PrependUOffsetTSlot(subgraphOperators, opsVec, 0)
	b.PrependUOffsetTSlot(subgraphName, name, 0)
	subgraph := b.EndObject()

	metaOffs := make([]flatbuffers.UOffsetT, len(metas))
	for i, m := range metas {
		n := b.CreateString(m.name)
		b.StartObject(2)
		b.PrependUOffsetTSlot(metadataName, n, 0)
		b.PrependUint32Slot(metadataBuffer, uint32(m.buffer), 0)
		metaOffs[i] = b.EndObject()
	}
	var sigVec flatbuffers.UOffsetT
	if sig := c.Format.Signature; sig != nil {
		maps := func(names map[string]string, ts []contract.Tensor, idx []int32) flatbuffers.UOffsetT {
			var offs []flatbuffers.UOffsetT
			for i, t := range ts {
				as, ok := names[t.Name]
				if !ok {
					continue
				}
				n := b.CreateString(as)
				b.StartObject(2)
				b.PrependUOffsetTSlot(mapName, n, 0)
				b.PrependUint32Slot(mapTensor, uint32(idx[i]), 0)
				offs = append(offs, b.EndObject())
			}
			return b.offsets(offs)
		}
		sigIns, sigOuts := maps(sig.Inputs, c.Inputs, inputs), maps(sig.Outputs, c.Outputs, outputs)
		key := b.CreateString(sig.Key)
		b.StartObject(5)
		b.PrependUOffsetTSlot(sigInputs, sigIns, 0)
		b.PrependUOffsetTSlot(sigOutputs, sigOuts, 0)
		b.PrependUOffsetTSlot(sigKey, key, 0)
		sigVec = b.offsets([]flatbuffers.UOffsetT{b.EndObject()})
	}

	codesVec, subgraphsVec, buffersVec := b.offsets(codeOffs), b.offsets([]flatbuffers.UOffsetT{subgraph}), b.offsets(bufferOffs)
	metaVec := b.offsets(metaOffs)
	desc := b.CreateString("A stub of axx: its answers are its mappings'.")
	b.StartObject(10)
	b.PrependUint32Slot(modelVersion, 3, 0)
	b.PrependUOffsetTSlot(modelOperatorCodes, codesVec, 0)
	b.PrependUOffsetTSlot(modelSubgraphs, subgraphsVec, 0)
	b.PrependUOffsetTSlot(modelDescription, desc, 0)
	b.PrependUOffsetTSlot(modelBuffers, buffersVec, 0)
	if len(metas) > 0 {
		b.PrependUOffsetTSlot(modelMetadata, metaVec, 0)
	}
	if sigVec != 0 {
		b.PrependUOffsetTSlot(modelSignatureDefs, sigVec, 0)
	}
	b.FinishWithFileIdentifier(b.EndObject(), []byte("TFL3"))
	model := b.FinishedBytes()

	if len(associated) == 0 {
		return model, nil
	}
	// The files appended to the model: a zip archive after it, which
	// TFLite's metadata names them in.
	var buf bytes.Buffer
	buf.Write(model)
	zw := zip.NewWriter(&buf)
	zw.SetOffset(int64(len(model)))
	for _, key := range associated {
		data, err := c.File(key)
		if err != nil {
			return nil, err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: strings.TrimPrefix(key, associatedPrefix), Method: zip.Store})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sortedFileKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
