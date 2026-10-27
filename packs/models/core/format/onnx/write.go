package onnx

import (
	"errors"
	"fmt"
	"slices"
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
	"github.com/nimbusxr/axx/packs/models/core/tokenize"
)

// MinOpset is the oldest ai.onnx operator set stubs are written in: the
// operators they use have had their present form since (numpy broadcasting,
// Cast's `to`).
const MinOpset = 7

// Write writes a stub as an ONNX model of its contract: the same inputs and
// outputs, IR version (4 at least) and ai.onnx operator set, and metadata.
//
// Its graph reduces the matched input as match.Reducer does, scores it
// against each key (their correlation) and against the contract's
// similarity, the default's score, and gathers each output's value from the
// answers of the best: ArgMax picks the first best, as match.Table.Nearest
// does. Its operators are Cast, Reshape, Transpose, AveragePool, MatMul, Sub,
// Mul, Add, Div, Sqrt, ArgMax and Gather.
func Write(s *stub.Stub) ([]byte, error) {
	if s.Contract.Task == "speech-to-text" {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written as one file: a speech recognizer's stub is its encoder and its decoder").
			WithHint("build it with `axx models build`, which writes the decoder beside the encoder")
	}
	m, err := writeModel(s)
	return m.File, err
}

// writeModel writes a stub as an ONNX model of its contract, with an empty
// file of each of the real model's external data beside it (its runtime
// loads them with it, as transformers.js does those its config names, and
// the stub has none); a speech recognizer's, as its encoder, its decoder
// beside it.
func writeModel(s *stub.Stub) (format.Model, error) {
	text := s.Matcher == stub.TextMatcher && s.Contract.Text != nil && s.Generated == nil
	if s.Nearest == nil && !text && s.Generated == nil {
		return format.Model{}, format.Unmatched("ONNX", s)
	}
	c := s.Contract
	opset := c.Format.Opsets[""]
	if opset == 0 {
		opset = 13
	}
	if opset < MinOpset {
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the model's ONNX operator set is %d: stubs are written in operator set %d or later", opset, MinOpset).
			WithHint("export the model with a later operator set, or give the contract's format.opsets a later one if the app's ONNX Runtime loads it")
	}
	ir := max(c.Format.IRVersion, 4)
	if c.Format.IRVersion == 0 {
		ir = 8
	}
	failed := func(err error) (format.Model, error) {
		if ae := (*axxerr.Error)(nil); errors.As(err, &ae) {
			return format.Model{}, err
		}
		return format.Model{}, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %v", err)
	}
	if c.Task == "speech-to-text" {
		m, err := writeSpeech(s, opset, ir)
		if err != nil {
			return failed(err)
		}
		return m, nil
	}
	g, err := body(s, opset)
	if err != nil {
		return failed(err)
	}
	m := format.Model{File: assemble(c, g, c.Inputs, c.Outputs, ir, opset)}
	for _, loc := range c.Format.ExternalData {
		if m.Beside == nil {
			m.Beside = map[string][]byte{}
		}
		m.Beside[loc] = []byte{}
	}
	return m, nil
}

// assemble is an ONNX model of a graph, of the inputs and outputs, at the
// IR version and ai.onnx operator set, with the contract's metadata.
func assemble(c *contract.Contract, g *graph, inputs, outputs []contract.Tensor, ir, opset int64) []byte {
	gm := pb.Msg(nil).Str(graphName, "axx-stub")
	for _, nd := range g.nodes {
		gm = gm.Bytes(graphNode, nd)
	}
	for _, t := range g.initializers {
		gm = gm.Bytes(graphInitializer, t)
	}
	for _, t := range inputs {
		gm = gm.Bytes(graphInput, valueInfoOf(&t))
	}
	for _, t := range outputs {
		gm = gm.Bytes(graphOutput, valueInfoOf(&t))
	}

	m := pb.Msg(nil).Varint(modelIRVersion, uint64(ir)).
		Str(modelProducerName, "axx").
		Str(modelProducerVersion, "models-core").
		Str(modelDocString, "A stub of axx: its answers are its mappings'.").
		Bytes(modelGraph, gm).
		Bytes(modelOpsetImport, pb.Msg(nil).Str(opsetDomain, "").Varint(opsetVersion, uint64(opset)))
	keys := make([]string, 0, len(c.Metadata))
	for k := range c.Metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		m = m.Bytes(modelMetadataProps, pb.Msg(nil).Str(entryKey, k).Str(entryValue, c.Metadata[k]))
	}
	return m
}

// body is the stub's graph: a decoder's, or the best mapping's answers.
func body(s *stub.Stub, opset int64) (*graph, error) {
	if s.Generated != nil {
		return writeDecoder(s, opset)
	}
	g := &graph{}
	in := s.Contract.MatchedInput()
	var best string
	if s.Matcher == stub.TextMatcher {
		var err error
		if best, err = g.textBest(s, in, opset); err != nil {
			return nil, err
		}
	} else {
		best = g.nearestBest(s, in)
	}
	for _, out := range s.Contract.Outputs {
		if err := g.answer(s, &out, best, in.Batched(), opset); err != nil {
			return nil, err
		}
	}
	return g, nil
}

// nearestBest adds the nodes that give the mapping whose key the input is
// nearest (or the default): the correlation of the reduced input with each
// key (x·k / D), and the similarity, the default's; the first best.
func (g *graph) nearestBest(s *stub.Stub, in *contract.Tensor) string {
	x := g.reduce(s.Nearest.Reducer, in, s.Contract.Format.Opsets[""])
	d := s.Nearest.Reducer.Dims()
	n := len(s.Nearest.Table.Keys)
	kt := make([]float64, d*(n+1))
	for j, k := range s.Nearest.Table.Keys {
		for i, v := range k {
			kt[i*(n+1)+j] = v / float64(d)
		}
	}
	bias := make([]float64, n+1)
	bias[n] = s.Nearest.Table.Similarity
	scores := g.node("Add", g.node("MatMul", x, g.floats([]int64{int64(d), int64(n + 1)}, kt)), g.floats([]int64{int64(n + 1)}, bias))
	return g.node("ArgMax", scores, intAttr("axis", 1), intAttr("keepdims", 0))
}

// textBest adds the nodes that give the first mapping whose text the
// input's tokens have, as the app tokenizes it (or the default): the ids
// padded with ids no token has (so that a short input has windows of every
// text's length); each of a text's tokens compared with the ids at each
// position (Slice, Equal), all of them alike (Cast, Mul), anywhere
// (ReduceMax); the texts' scored by their order, the default under them all
// (Concat, MatMul, Add); the best (ArgMax).
func (g *graph) textBest(s *stub.Stub, in *contract.Tensor, opset int64) (string, error) {
	tok, err := tokenize.Of(s.Contract)
	if err != nil {
		return "", err
	}
	x := g.node("Cast", in.Name, intAttr("to", int64(elemTypes["int64"])))
	if !in.Batched() {
		if opset >= 13 {
			x = g.node("Unsqueeze", x, g.int64s(0))
		} else {
			x = g.node("Unsqueeze", x, intsAttr("axes", 0))
		}
	}
	longest := 1
	var keys [][][]int32
	for k, text := range s.Texts {
		variants, err := tokenize.KeyVariants(tok, text)
		if err != nil {
			return "", fmt.Errorf("mapping %q: %w", s.Names[k], err)
		}
		for _, ids := range variants {
			if len(ids) == 0 {
				return "", fmt.Errorf("mapping %q: the model's tokenizer finds no words in its text", s.Names[k])
			}
			longest = max(longest, len(ids))
		}
		keys = append(keys, variants)
	}
	// The ids, padded with the tokenizer's padding: every text has windows,
	// and the last of the ids a token after it.
	pad := float64(tok.Pad())
	pads := []int64{0, 0, 0, int64(longest + 1)}
	if opset >= 11 {
		x = g.node("Pad", x, g.int64s(pads...), g.tensor(nil, "int64", contract.DType("int64").Encode([]float64{pad})))
	} else {
		// Pad pads floats only, before operator set 11.
		x = g.node("Pad", g.node("Cast", x, intAttr("to", int64(elemTypes["float32"]))), intsAttr("pads", pads...), floatAttr("value", pad))
		x = g.node("Cast", x, intAttr("to", int64(elemTypes["int64"])))
	}
	slice := func(from, to int64) string {
		if opset >= 10 {
			return g.node("Slice", x, g.int64s(from), g.int64s(to), g.int64s(1))
		}
		return g.node("Slice", x, intsAttr("starts", from), intsAttr("ends", to), intsAttr("axes", 1))
	}
	var continues string
	if cont := tok.Continues(); cont != nil {
		// A byte a token: a vocabulary's table is as many bytes as it has
		// tokens.
		table := make([]float64, len(cont))
		for i, c := range cont {
			if c {
				table[i] = 1
			}
		}
		continues = g.tensor([]int64{int64(len(table))}, "uint8", contract.DType("uint8").Encode(table))
	}
	var has []string
	for _, variants := range keys {
		var found []any
		for _, ids := range variants {
			var all string
			n := int64(len(ids))
			for i, id := range ids {
				// Windows begin at every id but the text's last n: token i of
				// each is the ids from i to n − i before the end.
				slice := slice(int64(i), -(n - int64(i)))
				value := g.tensor(nil, "int64", contract.DType("int64").Encode([]float64{float64(id)}))
				eq := g.node("Cast", g.node("Equal", slice, value), intAttr("to", int64(elemTypes["float32"])))
				if all == "" {
					all = eq
				} else {
					all = g.node("Mul", all, eq)
				}
			}
			if continues != "" {
				// The token after a window does not go on its last word.
				next := g.node("Cast", g.node("Gather", continues, slice(n, 1<<62), intAttr("axis", 0)), intAttr("to", int64(elemTypes["float32"])))
				all = g.node("Mul", all, g.node("Sub", g.floats(nil, []float64{1}), next))
			}
			if opset >= 18 {
				found = append(found, g.node("ReduceMax", all, g.int64s(1), intAttr("keepdims", 1)))
			} else {
				found = append(found, g.node("ReduceMax", all, intsAttr("axes", 1), intAttr("keepdims", 1)))
			}
		}
		// A text is had in any of its variants.
		if len(found) == 1 {
			has = append(has, found[0].(string))
		} else {
			has = append(has, g.node("Max", found...))
		}
	}
	n := len(has)
	matched := has[0]
	if n > 1 {
		args := make([]any, 0, n+1)
		for _, h := range has {
			args = append(args, h)
		}
		matched = g.node("Concat", append(args, intAttr("axis", 1))...)
	}
	weights := make([]float64, n*(n+1))
	for m := range n {
		weights[m*(n+1)+m] = float64(n - m)
	}
	bias := make([]float64, n+1)
	bias[n] = 0.5
	scores := g.node("Add", g.node("MatMul", matched, g.floats([]int64{int64(n), int64(n + 1)}, weights)), g.floats([]int64{int64(n + 1)}, bias))
	return g.node("ArgMax", scores, intAttr("axis", 1), intAttr("keepdims", 0)), nil
}

// graph is a graph's nodes and initializers, as they are added.
type graph struct {
	nodes        [][]byte
	initializers [][]byte
	names        int
}

func (g *graph) name() string {
	g.names++
	return fmt.Sprintf("axx_%d", g.names)
}

type attr []byte

func intAttr(name string, v int64) attr {
	return attr(pb.Msg(nil).Str(attrName, name).Varint(attrI, uint64(v)).Varint(attrType, attrTypeInt))
}

func floatAttr(name string, v float64) attr {
	return attr(pb.Msg(nil).Str(attrName, name).Float(attrF, float32(v)).Varint(attrType, attrTypeFloat))
}

func intsAttr(name string, vs ...int64) attr {
	return attr(pb.Msg(nil).Str(attrName, name).Ints(attrInts, vs).Varint(attrType, attrTypeInts))
}

// node adds a node of op on the inputs (names) with the attributes, and
// returns its output's name.
func (g *graph) node(op string, args ...any) string {
	return g.nodeNamed(g.name(), op, args...)
}

func (g *graph) nodeNamed(out, op string, args ...any) string {
	m := pb.Msg(nil)
	for _, a := range args {
		if in, ok := a.(string); ok {
			m = m.Str(nodeInput, in)
		}
	}
	m = m.Str(nodeOutput, out).Str(nodeName, out).Str(nodeOpType, op)
	for _, a := range args {
		if at, ok := a.(attr); ok {
			m = m.Bytes(nodeAttribute, at)
		}
	}
	g.nodes = append(g.nodes, m)
	return out
}

func (g *graph) tensor(dims []int64, t contract.DType, raw []byte) string {
	name := g.name()
	m := pb.Msg(nil).Ints(tensorDims, dims).Varint(tensorDataType, elemTypes[t]).Str(tensorName, name).Bytes(tensorRawData, raw)
	g.initializers = append(g.initializers, m)
	return name
}

func (g *graph) floats(dims []int64, vs []float64) string {
	return g.tensor(dims, "float32", pb.Float32Bytes(vs))
}

func (g *graph) int64s(vs ...int64) string {
	raw := make([]byte, 0, 8*len(vs))
	for _, v := range vs {
		raw = protowire.AppendFixed64(raw, uint64(v))
	}
	return g.tensor([]int64{int64(len(vs))}, "int64", raw)
}

// reduce adds the nodes that reduce the input in as r does: [batch, D].
func (g *graph) reduce(r *match.Reducer, in *contract.Tensor, opset int64) string {
	x := in.Name
	if in.Type != "float32" {
		x = g.node("Cast", x, intAttr("to", int64(elemTypes["float32"])))
	}
	if !in.Batched() {
		shape := []int64{1}
		for _, d := range in.Shape {
			shape = append(shape, d.Size)
		}
		x = g.node("Reshape", x, g.int64s(shape...))
	}
	var channels, cells int64
	switch {
	case r.Features:
		// A sound's features, [mels, frames]: a picture of one channel,
		// each time's bands centered and scaled to their root mean square
		// (a time under the level left out: match.Heard), then the whole
		// scaled to its own.
		x = g.node("Reshape", x, g.int64s(0, 1, int64(r.H), int64(r.W)))
		x = g.node("AveragePool", x, intsAttr("kernel_shape", int64(r.KH), int64(r.KW)), intsAttr("strides", int64(r.KH), int64(r.KW)))
		bandsMean := func(x string) string {
			if opset >= 18 {
				return g.node("ReduceMean", x, g.int64s(2), intAttr("keepdims", 1))
			}
			return g.node("ReduceMean", x, intsAttr("axes", 2), intAttr("keepdims", 1))
		}
		scalar := func(v float64) string { return g.floats(nil, []float64{v}) }
		x = g.node("Sub", x, bandsMean(x))
		square := bandsMean(g.node("Mul", x, x))
		heard := g.node("Min", g.node("Max", g.node("Mul", g.node("Sub", square, scalar(match.SegmentLevel)), scalar(match.SegmentGain)), scalar(0)), scalar(1))
		x = g.node("Div", g.node("Mul", x, heard), g.node("Sqrt", g.node("Max", square, scalar(match.SegmentLevel))))
		x = g.node("Reshape", x, g.int64s(0, int64(r.Dims())))
		avg := make([]float64, r.Dims())
		for i := range avg {
			avg[i] = 1 / float64(r.Dims())
		}
		meanSquare := g.node("MatMul", g.node("Mul", x, x), g.floats([]int64{int64(r.Dims()), 1}, avg))
		return g.node("Div", x, g.node("Sqrt", g.node("Add", meanSquare, scalar(match.Epsilon))))
	case r.Image:
		if in.Image.Layout == "NHWC" || in.Image.Layout == "HWC" {
			x = g.node("Transpose", x, intsAttr("perm", 0, 3, 1, 2))
		}
		x = g.node("AveragePool", x, intsAttr("kernel_shape", int64(r.KH), int64(r.KW)), intsAttr("strides", int64(r.KH), int64(r.KW)))
		channels, cells = int64(r.C), int64(r.PH*r.PW)
	case r.Audio:
		x = g.spectrum(r, x)
		channels, cells = 1, match.Bands
	default:
		channels, cells = 1, int64(r.N)
	}
	x = g.node("Reshape", x, g.int64s(0, channels, cells))
	avg := make([]float64, cells)
	for i := range avg {
		avg[i] = 1 / float64(cells)
	}
	mean := g.floats([]int64{cells, 1}, avg)
	centered := g.node("Sub", x, g.node("MatMul", x, mean))
	variance := g.node("MatMul", g.node("Mul", centered, centered), mean)
	sd := g.node("Sqrt", g.node("Add", variance, g.floats(nil, []float64{match.Epsilon})))
	x = g.node("Div", centered, sd)
	return g.node("Reshape", x, g.int64s(0, int64(r.Dims())))
}

// spectrum adds the nodes that reduce a sound's samples x ([batch, N]) to
// its spectrum, as r.Spectrum does: [batch, bands].
func (g *graph) spectrum(r *match.Reducer, x string) string {
	frames, frame, bins, bands := int64(r.N/r.Frame), int64(r.Frame), int64(r.Bins()), int64(match.Bands)
	x = g.node("Reshape", x, g.int64s(0, frames, frame))
	cos, sin := r.Fourier()
	re := g.node("MatMul", x, g.floats([]int64{frame, bins}, cos))
	im := g.node("MatMul", x, g.floats([]int64{frame, bins}, sin))
	power := g.node("Add", g.node("Mul", re, re), g.node("Mul", im, im))
	sum := make([]float64, bins*bands)
	for j := range int(bins) {
		sum[j*int(bands)+r.BandOf(j)] = 1
	}
	byBand := g.node("Transpose", g.node("MatMul", power, g.floats([]int64{bins, bands}, sum)), intsAttr("perm", 0, 2, 1))
	avg := make([]float64, frames)
	for i := range avg {
		avg[i] = 1 / float64(frames)
	}
	mean := g.node("MatMul", byBand, g.floats([]int64{frames, 1}, avg))
	return g.node("Log", g.node("Add", mean, g.floats(nil, []float64{match.LogEpsilon})))
}

// answer adds the nodes that give the output out the answer of the mapping
// best names. Answers that end alike (format.Tail) are stored to where they
// do, and padded with the value they end in.
func (g *graph) answer(s *stub.Stub, out *contract.Tensor, best string, batched bool, opset int64) error {
	item := out.Shape
	if out.Batched() {
		item = item[1:]
	}
	dims := []int64{int64(len(s.Answers))}
	for _, d := range item {
		dims = append(dims, max(d.Size, 1))
	}
	size := task.ItemSize(out)
	for i, a := range s.Answers {
		if v := a[out.Name]; len(v) != size {
			return fmt.Errorf("the answer of %s gives output %s %d values, not %d", s.Names[i], out.Name, len(v), size)
		}
	}
	if len(item) > 0 && item[len(item)-1].Fixed() && opset >= 9 {
		depth := int(item[len(item)-1].Size)
		if indexes, on, off, ok := format.OneHot(s.Answers, out.Name, depth); ok {
			// Each group's on value's index, as uint8 (or int32), one-hot
			// again.
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
			table := g.tensor([]int64{int64(len(s.Answers)), int64(len(indexes[0]))}, kind, kind.Encode(flat))
			x := g.node("Cast", g.node("Gather", table, best, intAttr("axis", 0)), intAttr("to", int64(elemTypes["int64"])))
			x = g.node("OneHot", x, g.int64s(int64(depth)), g.tensor([]int64{2}, out.Type, out.Type.Encode([]float64{off, on})), intAttr("axis", -1))
			shape := make([]int64, len(out.Shape))
			for i, d := range out.Shape {
				shape[i] = max(d.Size, 1)
			}
			if out.Batched() && batched {
				shape[0] = -1
			}
			g.output(s, out, "Reshape", x, g.int64s(shape...))
			return nil
		}
	}
	prefix, tail, padded := format.Tail(s.Answers, out.Name)
	padded = padded && (opset >= 11 || !out.Type.Integer() && out.Type != "bool")
	if !padded {
		prefix = size
	}
	if indexes, palette, ok := format.Palette(s.Answers, out.Name); ok && !padded {
		// Each value's index in the palette, a byte, and the palette's
		// values: a mask of confidences, at a byte a pixel.
		var flat []float64
		for _, idx := range indexes {
			for _, i := range idx {
				flat = append(flat, float64(i))
			}
		}
		table := g.tensor([]int64{int64(len(s.Answers)), int64(size)}, "uint8", contract.DType("uint8").Encode(flat))
		x := g.node("Cast", g.node("Gather", table, best, intAttr("axis", 0)), intAttr("to", int64(elemTypes["int64"])))
		x = g.node("Gather", g.tensor([]int64{int64(len(palette))}, out.Type, out.Type.Encode(palette)), x, intAttr("axis", 0))
		shape := make([]int64, len(out.Shape))
		for i, d := range out.Shape {
			shape[i] = max(d.Size, 1)
		}
		if out.Batched() && batched {
			shape[0] = -1
		}
		g.output(s, out, "Reshape", x, g.int64s(shape...))
		return nil
	}
	values := make([]float64, 0, prefix*len(s.Answers))
	for _, a := range s.Answers {
		values = append(values, a[out.Name][:prefix]...)
	}
	if padded {
		table := g.tensor([]int64{int64(len(s.Answers)), int64(prefix)}, out.Type, out.Type.Encode(values))
		x := g.node("Gather", table, best, intAttr("axis", 0))
		pads := []int64{0, 0, 0, int64(size - prefix)}
		if opset >= 11 {
			x = g.node("Pad", x, g.int64s(pads...), g.tensor(nil, out.Type, out.Type.Encode([]float64{tail})))
		} else {
			x = g.node("Pad", x, intsAttr("pads", pads...), floatAttr("value", tail))
		}
		shape := make([]int64, len(out.Shape))
		for i, d := range out.Shape {
			shape[i] = max(d.Size, 1)
		}
		if out.Batched() && batched {
			shape[0] = -1
		}
		g.output(s, out, "Reshape", x, g.int64s(shape...))
		return nil
	}
	table := g.tensor(dims, out.Type, out.Type.Encode(values))
	if out.Batched() && batched {
		g.output(s, out, "Gather", table, best, intAttr("axis", 0))
		return nil
	}
	gathered := g.node("Gather", table, best, intAttr("axis", 0))
	shape := make([]int64, len(out.Shape))
	for i, d := range out.Shape {
		shape[i] = max(d.Size, 1)
	}
	g.output(s, out, "Reshape", gathered, g.int64s(shape...))
	return nil
}

// output adds the output's node: of op, its answer at size 1 in each open
// dimension, as long as the input's dimension of the same name (a text
// encoder's last_hidden_state: the answer at every token).
func (g *graph) output(s *stub.Stub, out *contract.Tensor, op string, args ...any) {
	// Each open dimension's input, and its dimension there.
	type dimOf struct {
		input string
		at    int
	}
	from := make([]*dimOf, len(out.Shape))
	open := false
	for i, d := range out.Shape {
		if i == 0 && out.Batched() || d.Fixed() {
			continue
		}
		for _, in := range s.Contract.Inputs {
			if j := slices.IndexFunc(in.Shape, func(o contract.Dim) bool { return o.Name == d.Name }); j >= 0 {
				from[i], open = &dimOf{in.Name, j}, true
				break
			}
		}
	}
	if !open {
		g.nodeNamed(out.Name, op, args...)
		return
	}
	parts := make([]any, 0, len(out.Shape)+1)
	for i, d := range out.Shape {
		if f := from[i]; f != nil {
			parts = append(parts, g.node("Slice", g.node("Shape", f.input), g.int64s(int64(f.at)), g.int64s(int64(f.at+1))))
		} else {
			parts = append(parts, g.int64s(max(d.Size, 1)))
		}
	}
	g.nodeNamed(out.Name, "Expand", g.node(op, args...), g.node("Concat", append(parts, intAttr("axis", 0))...))
}

func valueInfoOf(t *contract.Tensor) []byte {
	shape := pb.Msg(nil)
	for _, d := range t.Shape {
		dm := pb.Msg(nil)
		if d.Name != "" {
			if d.Name != "?" {
				dm = dm.Str(dimParam, d.Name)
			}
		} else {
			dm = dm.Varint(dimValue, uint64(d.Size))
		}
		shape = shape.Bytes(shapeDim, dm)
	}
	tensor := pb.Msg(nil).Varint(tensorElem, elemTypes[t.Type]).Bytes(tensorShape, shape)
	return pb.Msg(nil).Str(valueName, t.Name).Bytes(valueType, pb.Msg(nil).Bytes(typeTensor, tensor))
}

// Operators are the operators stubs use.
var Operators = []string{"Add", "ArgMax", "AveragePool", "Cast", "Concat", "Div", "Equal", "Gather", "Log", "MatMul", "Mul", "OneHot", "Pad", "ReduceMax", "Reshape", "Slice", "Sqrt", "Sub", "Transpose", "Unsqueeze"}
