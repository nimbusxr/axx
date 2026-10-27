package tflite

import (
	"fmt"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
)

// More of LiteRT's operators, and their options' types, that generators
// use.
const (
	opMaximum, opGreater, opGreaterEqual, opSelectV2 = 55, 61, 62, 123
	opMinimum                                        = 57
	opDynamicUpdateSlice                             = 151
	optMaximum, optGreater, optGreaterEqual          = 39, 44, 45
	optSelectV2, optDynamicUpdateSlice               = 98, 117
)

// Generator is a stub of a language model as LiteRT-LM runs one: a TFLite
// model of prefill signatures, which take a prompt's tokens, and a decode
// signature, which takes a token and gives the next's logits, its KV cache
// carried from each call to the next.
//
// The stub's cache holds its tokens: each call writes the tokens it takes
// at their positions (kv_cache_k_0, of a value at each), and who wrote them
// (kv_cache_v_0: 1 for prefill, the prompt's; 2 for decode). Decode reads
// back all of them the mask lets it see: the prompt ends after the last a
// prefill wrote (decode takes its last token, or takes it again), and the
// answer is as many tokens along as decode is past it. Its operators find
// the first mapping whose text the prompt has (its tokens one after
// another, anywhere in the conversation so far), and give the answer's next
// token, then the end of generation. Every comparison is of whole numbers:
// the answer is exact.
type Generator struct {
	Plan *lm.Plan
	// Context is how many positions the cache has.
	Context int
	// Prefills are the prefill signatures' numbers of tokens.
	Prefills []int
}

// logitOn is the next token's logit; every other's is 0.
const logitOn = 100

// signed is a signature's subgraph: its key, and its inputs and outputs by
// name.
type signed struct {
	key             string
	g               *graph
	inputs, outputs []named
}

type named struct {
	name   string
	tensor int32
}

// WriteGenerator writes the generator as a TFLite model.
func WriteGenerator(gen *Generator) ([]byte, error) {
	var sigs []signed
	var buffers [][]byte
	for _, n := range gen.Prefills {
		s := gen.signature(fmt.Sprintf("prefill_%d", n), n, buffers)
		buffers = s.g.buffers
		sigs = append(sigs, s)
	}
	decode := gen.signature("decode", 1, buffers)
	logits := gen.next(decode.g, decode.inputs)
	decode.outputs = append(decode.outputs, named{"logits", logits})
	sigs = append(sigs, decode)
	return writeSigned(sigs)
}

// signature adds a signature's subgraph of n tokens: its inputs, and the
// cache's outputs, the tokens written in it.
func (gen *Generator) signature(key string, n int, buffers [][]byte) signed {
	g := &graph{buffers: buffers}
	ctx := int32(gen.Context)
	tokens := g.tensor(key+"_tokens", "int32", []int32{1, int32(n)}, nil, 0, nil)
	pos := g.tensor(key+"_input_pos", "int32", []int32{int32(n)}, nil, 0, nil)
	mask := g.tensor(key+"_mask", "float32", []int32{1, 1, int32(n), ctx}, nil, 0, nil)
	kIn := g.tensor(key+"_kv_cache_k_0", "float32", []int32{1, 1, ctx, 1}, nil, 0, nil)
	vIn := g.tensor(key+"_kv_cache_v_0", "float32", []int32{1, 1, ctx, 1}, nil, 0, nil)

	values := g.reshape(g.cast(tokens, "int32", "float32"), []int32{1, 1, int32(n), 1})
	first := g.op(opSlice, optSlice, noOptions, "int32", []int32{1}, pos, g.int32s(0), g.int32s(1))
	start := g.op(opConcatenation, optConcatenation, func(b builder) flatbuffers.UOffsetT {
		b.StartObject(2)
		b.PrependInt32Slot(0, 0, 0)
		return b.EndObject()
	}, "int32", []int32{4}, g.int32s(0, 0), first, g.int32s(0))
	kOut := g.op(opDynamicUpdateSlice, optDynamicUpdateSlice, noOptions, "float32", []int32{1, 1, ctx, 1}, kIn, values, start)
	writer := 1.0
	if key == "decode" {
		writer = 2
	}
	marks := make([]float64, n)
	for i := range marks {
		marks[i] = writer
	}
	vOut := g.op(opDynamicUpdateSlice, optDynamicUpdateSlice, noOptions, "float32", []int32{1, 1, ctx, 1}, vIn,
		g.constant("float32", []int32{1, 1, int32(n), 1}, contract.DType("float32").Encode(marks)), start)
	return signed{
		key: key, g: g,
		inputs: []named{
			{"input_pos", pos},
			{"kv_cache_k_0", kIn},
			{"kv_cache_v_0", vIn},
			{"mask", mask},
			{"tokens", tokens},
		},
		outputs: []named{{"kv_cache_k_0", kOut}, {"kv_cache_v_0", vOut}},
	}
}

// next adds decode's operators that give the next token's logits.
func (gen *Generator) next(g *graph, inputs []named) int32 {
	p := gen.Plan
	ctx := int32(gen.Context)
	in := func(name string) int32 {
		for _, n := range inputs {
			if n.name == name {
				return n.tensor
			}
		}
		return -1
	}
	var kCache, vCache int32
	for _, op := range g.ops {
		if op.code == opDynamicUpdateSlice {
			if op.inputs[0] == in("kv_cache_k_0") {
				kCache = op.outputs[0]
			} else {
				vCache = op.outputs[0]
			}
		}
	}
	reduceMax := func(x int32, shape []int32, keep bool) int32 {
		return g.op(opReduceMax, optReducer, func(b builder) flatbuffers.UOffsetT {
			b.StartObject(1)
			b.PrependBoolSlot(0, keep, false)
			return b.EndObject()
		}, "float32", shape, x, g.int32s(0))
	}
	maximum := func(a, b int32, shape []int32) int32 {
		return g.op(opMaximum, optMaximum, noOptions, "float32", shape, a, b)
	}
	pos := in("input_pos")

	// What decode sees, and where the prompt ends: after the last position
	// a prefill wrote.
	maskRow := g.reshape(in("mask"), []int32{ctx})
	visible := g.cast(g.op(opGreater, optGreater, noOptions, "bool", []int32{ctx}, maskRow, g.floats32(-1)), "bool", "float32")
	writers := g.reshape(vCache, []int32{ctx})
	prefilled := g.mul(visible, g.cast(g.op(opEqual, optEqual, noOptions, "bool", []int32{ctx}, writers, g.floats32(1)), "bool", "float32"), []int32{ctx})
	positions := make([]float64, ctx)
	for i := range positions {
		positions[i] = float64(i + 1)
	}
	// The last prefilled position + 1: the prompt's last token's.
	last := reduceMax(g.mul(prefilled, g.constant("float32", []int32{ctx}, contract.DType("float32").Encode(positions)), []int32{ctx}), []int32{1}, true)
	lastPos := g.cast(last, "float32", "int32")
	along := g.op(opSub, optSub, activationNone, "int32", []int32{1}, pos, lastPos)

	// The prompt's tokens (the conversation's, to its last token), −1 at
	// every other position.
	tokens := g.cast(g.reshape(kCache, []int32{ctx}), "float32", "int32")
	upTo := make([]float64, ctx)
	for i := range upTo {
		upTo[i] = float64(i)
	}
	inPrompt := g.op(opGreaterEqual, optGreaterEqual, noOptions, "bool", []int32{ctx}, g.cast(lastPos, "int32", "float32"), g.constant("float32", []int32{ctx}, contract.DType("float32").Encode(upTo)))
	seen := g.op(opGreater, optGreater, noOptions, "bool", []int32{ctx}, visible, g.floats32(0.5))
	both := g.cast(g.mul(g.cast(inPrompt, "bool", "float32"), g.cast(seen, "bool", "float32"), []int32{ctx}), "float32", "bool")
	ids := g.op(opSelectV2, optSelectV2, noOptions, "int32", []int32{ctx}, both, tokens, g.int32s(-1))

	// windows are a run of tokens' matches at each position it may begin
	// at: 1 where the prompt has it.
	windows := func(run []int32) int32 {
		n := int32(len(run))
		size := ctx - n + 1
		var all int32 = -1
		for i, id := range run {
			slice := g.op(opSlice, optSlice, noOptions, "int32", []int32{size}, ids, g.int32s(int32(i)), g.int32s(size))
			eq := g.cast(g.op(opEqual, optEqual, noOptions, "bool", []int32{size}, slice, g.int32s(id)), "bool", "float32")
			if all < 0 {
				all = eq
			} else {
				all = g.mul(all, eq, []int32{size})
			}
		}
		return all
	}

	// The prompt's mapping: the first whose text it has, in any of its
	// variants; or the default.
	var has []int32
	for _, key := range p.Keys {
		var any int32 = -1
		for _, v := range key.Variants {
			if int32(len(v)) > ctx {
				continue
			}
			found := reduceMax(windows(v), []int32{1}, true)
			if any < 0 {
				any = found
			} else {
				any = maximum(any, found, []int32{1})
			}
		}
		if any < 0 {
			any = g.floats32(0)
		}
		has = append(has, any)
	}
	var id int32
	if n := int32(len(has)); n == 0 {
		id = g.int32s(0)
	} else {
		matched := has[0]
		if n > 1 {
			matched = g.op(opConcatenation, optConcatenation, func(b builder) flatbuffers.UOffsetT {
				b.StartObject(2)
				b.PrependInt32Slot(0, 0, 0)
				return b.EndObject()
			}, "float32", []int32{n}, has...)
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
		}, "float32", []int32{1, n + 1}, g.reshape(matched, []int32{1, n}),
			g.constant("float32", []int32{n + 1, n}, contract.DType("float32").Encode(weights)),
			g.constant("float32", []int32{n + 1}, contract.DType("float32").Encode(bias)))
		id = g.argMax(scores, 1)
	}

	// The answer's token as far along as decode is, its end after it: a
	// table of the answers, each as long as the longest and one more, its
	// end of generation after it.
	width := 1
	for _, a := range p.Answers {
		width = max(width, len(a)+1)
	}
	table := make([]float64, 0, width*len(p.Answers))
	for _, a := range p.Answers {
		for j := range width {
			t := p.EOG
			if j < len(a) {
				t = a[j]
			}
			table = append(table, float64(t))
		}
	}
	clamped := g.op(opMaximum, optMaximum, noOptions, "int32", []int32{1}, g.op(opMinimum, optMaximum, noOptions, "int32", []int32{1}, along, g.int32s(int32(width-1))), g.int32s(0))
	at := g.op(opAdd, optAdd, activationNone, "int32", []int32{1}, g.op(opMul, optMul, activationNone, "int32", []int32{1}, id, g.int32s(int32(width))), clamped)
	chosen := g.op(opGather, optGather, func(b builder) flatbuffers.UOffsetT {
		b.StartObject(2)
		return b.EndObject()
	}, "float32", []int32{1}, g.constant("float32", []int32{int32(len(table))}, contract.DType("float32").Encode(table)), at)
	nextID := g.cast(chosen, "float32", "int32")
	logits := g.op(opOneHot, optOneHot, func(b builder) flatbuffers.UOffsetT {
		b.StartObject(1)
		b.PrependInt32Slot(0, -1, 0)
		return b.EndObject()
	}, "float32", []int32{1, int32(p.Vocab)}, nextID,
		g.constant("int32", []int32{}, contract.DType("int32").Encode([]float64{float64(p.Vocab)})),
		g.constant("float32", []int32{}, contract.DType("float32").Encode([]float64{logitOn})),
		g.constant("float32", []int32{}, contract.DType("float32").Encode([]float64{0})))
	return g.reshape(logits, []int32{1, 1, int32(p.Vocab)})
}

// cast adds a CAST of x from one type to another.
func (g *graph) cast(x int32, from, to contract.DType) int32 {
	return g.op(opCast, optCast, func(b builder) flatbuffers.UOffsetT {
		b.StartObject(2)
		b.PrependInt8Slot(0, tensorTypeOf(from), -1)
		b.PrependInt8Slot(1, tensorTypeOf(to), -1)
		return b.EndObject()
	}, to, g.shapes[x], x)
}

// mul adds a MUL of floats.
func (g *graph) mul(a, b int32, shape []int32) int32 {
	return g.op(opMul, optMul, activationNone, "float32", shape, a, b)
}

// floats32 is a constant float of one value.
func (g *graph) floats32(v float64) int32 {
	return g.constant("float32", []int32{1}, contract.DType("float32").Encode([]float64{v}))
}

// writeSigned writes a model of the signatures' subgraphs, their buffers
// shared (each graph's buffers begin with the one's before it).
func writeSigned(sigs []signed) ([]byte, error) {
	b := builder{flatbuffers.NewBuilder(1 << 20)}
	buffers := sigs[len(sigs)-1].g.buffers
	bufferOffs := make([]flatbuffers.UOffsetT, len(buffers))
	for i, data := range buffers {
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
	codeIndex := map[int32]uint32{}
	var codes []int32
	subgraphOffs := make([]flatbuffers.UOffsetT, len(sigs))
	for si, s := range sigs {
		g := s.g
		tensorOffs := make([]flatbuffers.UOffsetT, len(g.tensors))
		for i, t := range g.tensors {
			tensorOffs[i] = t(b)
		}
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
		var ins, outs []int32
		for _, n := range s.inputs {
			ins = append(ins, n.tensor)
		}
		for _, n := range s.outputs {
			outs = append(outs, n.tensor)
		}
		tensorsVec, opsVec := b.offsets(tensorOffs), b.offsets(opOffs)
		insVec, outsVec := b.int32s(ins), b.int32s(outs)
		name := b.CreateString(s.key)
		b.StartObject(6)
		b.PrependUOffsetTSlot(subgraphTensors, tensorsVec, 0)
		b.PrependUOffsetTSlot(subgraphInputs, insVec, 0)
		b.PrependUOffsetTSlot(subgraphOutputs, outsVec, 0)
		b.PrependUOffsetTSlot(subgraphOperators, opsVec, 0)
		b.PrependUOffsetTSlot(subgraphName, name, 0)
		subgraphOffs[si] = b.EndObject()
	}
	codeOffs := make([]flatbuffers.UOffsetT, len(codes))
	for i, code := range codes {
		b.StartObject(4)
		b.PrependInt8Slot(codeDeprecated, int8(min(code, 127)), 0)
		b.PrependInt32Slot(codeVersion, 1, 0)
		b.PrependInt32Slot(codeBuiltin, code, 0)
		codeOffs[i] = b.EndObject()
	}
	sigOffs := make([]flatbuffers.UOffsetT, len(sigs))
	for si, s := range sigs {
		maps := func(ns []named) flatbuffers.UOffsetT {
			offs := make([]flatbuffers.UOffsetT, len(ns))
			for i, n := range ns {
				name := b.CreateString(n.name)
				b.StartObject(2)
				b.PrependUOffsetTSlot(mapName, name, 0)
				b.PrependUint32Slot(mapTensor, uint32(n.tensor), 0)
				offs[i] = b.EndObject()
			}
			return b.offsets(offs)
		}
		ins, outs := maps(s.inputs), maps(s.outputs)
		key := b.CreateString(s.key)
		b.StartObject(5)
		b.PrependUOffsetTSlot(sigInputs, ins, 0)
		b.PrependUOffsetTSlot(sigOutputs, outs, 0)
		b.PrependUOffsetTSlot(sigKey, key, 0)
		b.PrependUint32Slot(sigSubgraph, uint32(si), 0)
		sigOffs[si] = b.EndObject()
	}
	codesVec, subgraphsVec, buffersVec, sigVec := b.offsets(codeOffs), b.offsets(subgraphOffs), b.offsets(bufferOffs), b.offsets(sigOffs)
	desc := b.CreateString("A stub of axx: its answers are its mappings'.")
	b.StartObject(10)
	b.PrependUint32Slot(modelVersion, 3, 0)
	b.PrependUOffsetTSlot(modelOperatorCodes, codesVec, 0)
	b.PrependUOffsetTSlot(modelSubgraphs, subgraphsVec, 0)
	b.PrependUOffsetTSlot(modelDescription, desc, 0)
	b.PrependUOffsetTSlot(modelBuffers, buffersVec, 0)
	b.PrependUOffsetTSlot(modelSignatureDefs, sigVec, 0)
	b.FinishWithFileIdentifier(b.EndObject(), []byte("TFL3"))
	return b.FinishedBytes(), nil
}
