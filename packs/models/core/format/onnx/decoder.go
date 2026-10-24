package onnx

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A decoder is a language model's ONNX graph as Hugging Face's exporters
// (Optimum, transformers.js's) and ONNX Runtime GenAI's model builder write
// it: input_ids, attention_mask (and position_ids) and its KV cache
// (past_key_values.N.key and .value, [batch, heads, past, head size]) in;
// logits ([batch, sequence, vocabulary]) and the cache with the call's
// positions after the past's (present.N.key and .value) out. Its runner
// carries the cache from each call to the next: the prompt's tokens in the
// first, then each token it generates.

// tokenizerFile is the contract's file of a decoder's tokenizer.
const tokenizerFile = "tokenizer"

// decoderDetails are a decoder's own settings in its contract.
type decoderDetails struct {
	// EndOfGeneration is the token that ends the model's turn.
	EndOfGeneration int32 `json:"endOfGeneration"`
}

// isDecoder reports whether a contract is a decoder's.
func isDecoder(c *contract.Contract) bool {
	ids, past, logits := false, false, false
	for _, in := range c.Inputs {
		ids = ids || in.Name == "input_ids"
		past = past || strings.HasPrefix(in.Name, "past_key_values")
	}
	for _, out := range c.Outputs {
		logits = logits || out.Name == "logits"
	}
	return ids && past && logits
}

// decoderContract makes a decoder's contract a text generation model's,
// which carries its tokenizer (tokenizer.json, beside the model or in the
// folder above it, as Hugging Face's repositories and ONNX Runtime GenAI's
// folders have it) and the stop token that ends its turn (its generation
// config's, GenAI's config's, its config's, or its tokenizer config's).
func decoderContract(c *contract.Contract, path string) error {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	dirs := []string{filepath.Dir(path), filepath.Dir(filepath.Dir(path))}
	find := func(name string) []byte {
		for _, d := range dirs {
			if b, err := os.ReadFile(filepath.Join(d, name)); err == nil {
				return b
			}
		}
		return nil
	}
	cacheSizes(c, find("genai_config.json"))
	if _, err := layoutOf(c); err != nil {
		return fail("%v", err)
	}
	tok := find("tokenizer.json")
	if tok == nil {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a language model's decoder, whose contract carries its tokenizer: there is no tokenizer.json beside it or in the folder above", path).
			WithHint("keep the model in its folder as it comes (onnx/model.onnx beside tokenizer.json and config.json, as Hugging Face's repositories have it)")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return fail("%v", err)
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return fail("%v", err)
	}
	stops := stopTokens(find, v)
	if len(stops) == 0 {
		return fail("its folder gives no stop token (eos_token_id) in generation_config.json, genai_config.json or config.json, or eos_token in tokenizer_config.json")
	}
	raw, err := json.Marshal(decoderDetails{EndOfGeneration: lm.EndOfTurn(v, stops)})
	if err != nil {
		return err
	}
	c.Task, c.Format.Details = "text-generation", raw
	c.Carry(tokenizerFile, "tokenizer.json", tok)
	return nil
}

// cacheSizes gives a KV cache's tensors ([batch, heads, past, head size])
// the heads and head size the model leaves open, from ONNX Runtime GenAI's
// config, as GenAI sizes the cache it gives the model: its builder names a
// cache's head size (kv_cache_dim), and its config gives it (head_size,
// num_key_value_heads).
func cacheSizes(c *contract.Contract, config []byte) {
	var genai struct {
		Model struct {
			Decoder struct {
				HeadSize int64 `json:"head_size"`
				KVHeads  int64 `json:"num_key_value_heads"`
			} `json:"decoder"`
		} `json:"model"`
	}
	if json.Unmarshal(config, &genai) != nil {
		return
	}
	d := genai.Model.Decoder
	give := func(t *contract.Tensor) {
		if len(t.Shape) != 4 {
			return
		}
		if !t.Shape[1].Fixed() && d.KVHeads > 0 {
			t.Shape[1] = contract.Dim{Size: d.KVHeads}
		}
		if !t.Shape[3].Fixed() && d.HeadSize > 0 {
			t.Shape[3] = contract.Dim{Size: d.HeadSize}
		}
	}
	for i := range c.Inputs {
		if strings.HasPrefix(c.Inputs[i].Name, "past_key_values") {
			give(&c.Inputs[i])
		}
	}
	for i := range c.Outputs {
		if strings.HasPrefix(c.Outputs[i].Name, "present") {
			give(&c.Outputs[i])
		}
	}
}

// stopTokens are the tokens the model's runner stops at, from the first of
// its configs that gives them.
func stopTokens(find func(string) []byte, v *lm.Vocab) []int32 {
	ids := func(raw json.RawMessage) []int32 {
		var one int32
		if json.Unmarshal(raw, &one) == nil {
			return []int32{one}
		}
		var many []int32
		_ = json.Unmarshal(raw, &many)
		return many
	}
	var gen struct {
		EOS json.RawMessage `json:"eos_token_id"`
	}
	if json.Unmarshal(find("generation_config.json"), &gen) == nil {
		if s := ids(gen.EOS); len(s) > 0 {
			return s
		}
	}
	var genai struct {
		Model struct {
			EOS json.RawMessage `json:"eos_token_id"`
		} `json:"model"`
	}
	if json.Unmarshal(find("genai_config.json"), &genai) == nil {
		if s := ids(genai.Model.EOS); len(s) > 0 {
			return s
		}
	}
	if json.Unmarshal(find("config.json"), &gen) == nil {
		if s := ids(gen.EOS); len(s) > 0 {
			return s
		}
	}
	var tc struct {
		EOS json.RawMessage `json:"eos_token"`
	}
	if json.Unmarshal(find("tokenizer_config.json"), &tc) == nil {
		var name string
		var added struct {
			Content string `json:"content"`
		}
		if json.Unmarshal(tc.EOS, &name) != nil && json.Unmarshal(tc.EOS, &added) == nil {
			name = added.Content
		}
		if id, ok := v.ID(name); ok && name != "" {
			return []int32{id}
		}
	}
	return nil
}

// layout is a decoder's inputs and outputs, as its stub uses them: its
// cache's tensors (each output's input), the one it keeps its state in,
// and how many positions its logits are.
type layout struct {
	// caches are the cache's tensors, in the outputs' order.
	caches []cache
	// state is the first of the cache's tensors that grow: [batch, heads,
	// past, head size].
	state *contract.Tensor
	ids   *contract.Tensor
	mask  *contract.Tensor
	// keep is the input that says how many positions' logits the model
	// gives (num_logits_to_keep), if it has one.
	keep   *contract.Tensor
	logits *contract.Tensor
}

// cache is a tensor of a decoder's cache: its output, its input (its past),
// and whether it grows by the call's positions (a KV cache's) or is the
// same size each call (a convolution's state, a recurrent layer's).
type cache struct {
	out   string
	past  *contract.Tensor
	grows bool
}

// stateValues are how many values of a position the stub writes in each of
// the cache's tensors that grow: its token's (in two halves, each a whole
// number a half-precision float holds), and who wrote it.
const stateValues = 3

func layoutOf(c *contract.Contract) (*layout, error) {
	l := &layout{}
	byName := map[string]*contract.Tensor{}
	for i := range c.Inputs {
		in := &c.Inputs[i]
		byName[in.Name] = in
		switch in.Name {
		case "input_ids":
			l.ids = in
		case "attention_mask":
			l.mask = in
		case "num_logits_to_keep", "logits_to_keep":
			l.keep = in
		}
	}
	if l.ids == nil {
		return nil, errors.New("its decoder has no input_ids")
	}
	for i := range c.Outputs {
		out := &c.Outputs[i]
		if out.Name == "logits" {
			if n := len(out.Shape); n != 3 || !out.Shape[2].Fixed() {
				return nil, fmt.Errorf("its logits are not [batch, sequence, vocabulary] of a fixed vocabulary: %s", out.Shape)
			}
			l.logits = out
			continue
		}
		var past *contract.Tensor
		for _, name := range []string{strings.Replace(out.Name, "present", "past_key_values", 1), strings.Replace(out.Name, "present", "past", 1)} {
			if past = byName[name]; past != nil {
				break
			}
		}
		if past == nil {
			return nil, fmt.Errorf("its output %s has no input of its past (past_key_values or past in its name for present)", out.Name)
		}
		if past.Type != "float32" && past.Type != "float16" {
			return nil, fmt.Errorf("its cache's %s is of %s, not float32 or float16", past.Name, past.Type)
		}
		// A tensor grows when its present is longer than its past (its
		// lengths differ); one the same size each call has its past's shape.
		grows := len(past.Shape) == 4 && len(out.Shape) == 4 && past.Shape[1].Fixed() && !past.Shape[2].Fixed() && past.Shape[3].Fixed() && out.Shape[2] != past.Shape[2]
		same := len(past.Shape) == len(out.Shape) && past.Type == out.Type
		for k, d := range past.Shape {
			same = same && (k == 0 || out.Shape[k] == d)
		}
		switch {
		case grows && l.state == nil:
			if past.Shape[3].Size < stateValues {
				return nil, fmt.Errorf("its cache's %s has a head size of %d: a stub keeps %d values of each position", past.Name, past.Shape[3].Size, stateValues)
			}
			l.state = past
		case grows, same:
		default:
			return nil, fmt.Errorf("its cache's %s is neither [batch, heads, past, head size] of fixed heads and size, nor of the size its output %s has too: %s", past.Name, out.Name, past.Shape)
		}
		l.caches = append(l.caches, cache{out: out.Name, past: past, grows: grows})
	}
	if l.logits == nil {
		return nil, errors.New("its decoder gives no logits")
	}
	if l.state == nil {
		return nil, errors.New("its decoder's cache has no tensor of its positions, [batch, heads, past, head size], a KV cache's")
	}
	return l, nil
}

// writeDecoder writes a text generation stub as a decoder of the contract's
// inputs and outputs.
//
// Each call writes the tokens it takes in the cache after the past's (each
// of its tensors alike): at each position, its token's halves (id / 256, id
// % 256) and who wrote it (1 for a prompt, a call of many tokens or of the
// first; 2 for a token generated, a call of one after others). It reads them
// back from the first tensor: the prompt ends after the last position a
// prompt wrote, the next token is as far along the answer as the call's
// last position is past that, and the answer is the first mapping's whose
// text the conversation has before it (its tokens one after another, where
// the attention mask lets the model see them), or the default's. Its logits
// put the next token ahead of every other at every position, the end of
// generation after the answer. Every comparison is of whole numbers: the
// answer is exact.
func writeDecoder(s *stub.Stub, opset int64) (*graph, error) {
	c := s.Contract
	if s.Matcher != stub.TextMatcher {
		return nil, errors.New(`a language model's stub generates text for prompts matched by their text: a mapping's input is {"text": ...}`)
	}
	if opset < 13 {
		return nil, fmt.Errorf("the model's ONNX operator set is %d: language models' stubs are written in operator set 13 or later", opset)
	}
	l, err := layoutOf(c)
	if err != nil {
		return nil, err
	}
	var d decoderDetails
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		return nil, fmt.Errorf("the contract's format.details are not a decoder's: %w", err)
	}
	tok, err := c.File(tokenizerFile)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no tokenizer of the model").
			WithHint("write the contract again from the model with `axx models contract <folder>/onnx/model.onnx`")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return nil, err
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return nil, err
	}
	p, err := lm.NewPlan(s, v, d.EndOfGeneration, 0)
	if err != nil {
		return nil, err
	}
	return decoderGraph(l, p, opset, nil)
}

// recognizing is a speech recognizer's decoder's: the input of its
// encoder's hidden state, whose first value is the mapping and whose second
// is the timestamp the sound ends at (its index: fiftieths of a second); and
// the tokens that say a transcript has no timestamps and the first
// timestamp's (0.00).
type recognizing struct {
	encoder                       string
	noTimestamps, timestampsBegin int32
}

// decoderGraph is the graph of a decoder's stub, of its layout, its plan
// and its operator set: its mapping the first whose text the conversation
// has, or, of a speech recognizer's, the one its encoder gives. A
// recognizer's transcript is its answer, or, when the forced tokens do not
// end with the one of no timestamps, its answer between the timestamp 0.00
// and the one the sound ends at.
func decoderGraph(l *layout, p *lm.Plan, opset int64, speech *recognizing) (*graph, error) {
	p.Vocab = int(l.logits.Shape[2].Size)
	for _, a := range p.Answers {
		for _, t := range a {
			if int(t) >= p.Vocab {
				return nil, fmt.Errorf("an answer has the token %d, which the model's %d logits do not", t, p.Vocab)
			}
		}
	}

	g := &graph{}
	cast := func(x string, to contract.DType) string {
		return g.node("Cast", x, intAttr("to", int64(elemTypes[to])))
	}
	scalarF := func(v float64) string {
		return g.tensor(nil, "float32", contract.DType("float32").Encode([]float64{v}))
	}
	scalarI := func(v int64) string {
		return g.tensor(nil, "int64", contract.DType("int64").Encode([]float64{float64(v)}))
	}
	gather := func(x string, index, axis int64) string {
		return g.node("Gather", x, scalarI(index), intAttr("axis", axis))
	}
	reduceMax := func(x string) string {
		if opset >= 18 {
			return g.node("ReduceMax", x, g.int64s(1), intAttr("keepdims", 1))
		}
		return g.node("ReduceMax", x, intsAttr("axes", 1), intAttr("keepdims", 1))
	}
	reduceAll := func(x string) string { // the greatest of x's values, as a scalar
		return g.node("ReduceMax", x, intAttr("keepdims", 0))
	}
	dim := func(x string, i int64) string { // a dimension of x, as [1]
		return g.node("Slice", g.node("Shape", x), g.int64s(i), g.int64s(i+1))
	}

	// How long the conversation is, the call's tokens too: to the last
	// position the mask lets the model see (a mask as long as the
	// conversation, or as the cache's buffer, when the runner keeps one of
	// the conversation's greatest length and gives it back each call); and
	// the past's positions, before the call's.
	ids := l.ids.Name
	if l.ids.Type != "int64" {
		ids = cast(ids, "int64")
	}
	seq := dim(ids, 1)
	var mask, total, totalF, pastLen string
	if l.mask != nil {
		mask = l.mask.Name
		if l.mask.Type != "float32" {
			mask = cast(mask, "float32")
		}
		maskLen := g.node("Reshape", cast(dim(mask, 1), "float32"), g.int64s())
		lets := cast(g.node("Greater", mask, scalarF(0.5)), "float32")
		upTo := g.node("Range", scalarF(1), g.node("Add", maskLen, scalarF(1)), scalarF(1))
		totalF = g.node("Reshape", reduceAll(g.node("Mul", lets, upTo)), g.int64s(1))
		total = cast(totalF, "int64")
		pastLen = g.node("Sub", total, seq)
	} else {
		// No mask: the cache grows each call, and the model sees it all.
		pastLen = dim(l.state.Name, 2)
		total = g.node("Add", pastLen, seq)
		totalF = cast(total, "float32")
	}

	// The call's tokens, as the cache holds them: [batch, 1, sequence, 3].
	hi := g.node("Div", ids, scalarI(256))
	lo := g.node("Sub", ids, g.node("Mul", hi, scalarI(256)))
	decoding := g.node("And", g.node("Equal", seq, g.int64s(1)), g.node("Greater", pastLen, g.int64s(0)))
	writer := g.node("Add", cast(decoding, "float32"), scalarF(1))
	hiF := cast(hi, "float32")
	marks := g.node("Add", g.node("Mul", hiF, scalarF(0)), writer)
	written := g.node("Concat",
		g.node("Unsqueeze", hiF, g.int64s(1, 3)),
		g.node("Unsqueeze", cast(lo, "float32"), g.int64s(1, 3)),
		g.node("Unsqueeze", marks, g.int64s(1, 3)),
		intAttr("axis", 3))

	// The cache's tensors that grow: the past's positions, the call's
	// (written in each head, the rest of each position 0), and the rest of
	// the past's (a buffer's: the call's positions are written in it). The
	// others are as they were.
	padded := map[string]string{}
	for _, ca := range l.caches {
		past := ca.past
		if !ca.grows {
			g.nodeNamed(ca.out, "Identity", past.Name)
			continue
		}
		heads, size := past.Shape[1].Size, past.Shape[3].Size
		key := fmt.Sprint(heads, size, past.Type)
		x, ok := padded[key]
		if !ok {
			x = g.node("Pad", written, g.int64s(0, 0, 0, 0, 0, heads-1, 0, size-stateValues), scalarF(0))
			if past.Type != "float32" {
				x = cast(x, past.Type)
			}
			padded[key] = x
		}
		before := g.node("Slice", past.Name, g.int64s(0), pastLen, g.int64s(2))
		after := g.node("Slice", past.Name, total, g.int64s(1<<62), g.int64s(2))
		g.nodeNamed(ca.out, "Concat", before, x, after, intAttr("axis", 2))
	}

	// The conversation's tokens and writers, [batch, total]: the past's,
	// then the call's.
	pastEnd := g.node("Concat", g.int64s(1), pastLen, g.int64s(stateValues), intAttr("axis", 0))
	pastState := g.node("Slice", l.state.Name, g.int64s(0, 0, 0), pastEnd, g.int64s(1, 2, 3))
	if l.state.Type != "float32" {
		pastState = cast(pastState, "float32")
	}
	state := g.node("Concat", pastState, written, intAttr("axis", 2))
	positions := gather(state, 0, 1) // [batch, total, 3]
	tokens := cast(g.node("Add", g.node("Mul", gather(positions, 0, 2), scalarF(256)), gather(positions, 1, 2)), "int64")
	writers := gather(positions, 2, 2)

	// Where the prompt ends: after the last position a prompt wrote that
	// the mask lets the model see.
	visible := g.node("Equal", writers, writers) // every position
	if mask != "" {
		visible = g.node("Greater", g.node("Slice", mask, g.int64s(0), total, g.int64s(1)), scalarF(0.5))
	}
	prompted := cast(g.node("And", visible, g.node("Equal", writers, scalarF(1))), "float32")
	totalScalar := g.node("Reshape", totalF, g.int64s())
	from1 := g.node("Range", scalarF(1), g.node("Add", totalScalar, scalarF(1)), scalarF(1))
	last := reduceMax(g.node("Mul", prompted, from1)) // [batch, 1]
	along := g.node("Sub", totalF, last)

	// The conversation's tokens before the prompt's end, −1 elsewhere,
	// padded with −1: every key has windows.
	from0 := g.node("Range", scalarF(0), totalScalar, scalarF(1))
	seen := g.node("And", visible, g.node("Less", from0, last))
	longest := 1
	for _, k := range p.Keys {
		for _, v := range k.Variants {
			longest = max(longest, len(v))
		}
	}
	x := g.node("Pad", g.node("Where", seen, tokens, scalarI(-1)), g.int64s(0, 0, 0, int64(longest)), scalarI(-1))

	// The first mapping whose text the conversation has, in any of its
	// variants; or the default.
	var has []string
	for _, k := range p.Keys {
		var found []any
		for _, v := range k.Variants {
			n := int64(len(v))
			var all string
			for i, id := range v {
				end := int64(i) - n + 1
				if end >= 0 {
					end = 1 << 62
				}
				eq := g.node("Equal", g.node("Slice", x, g.int64s(int64(i)), g.int64s(end), g.int64s(1)), scalarI(int64(id)))
				if all == "" {
					all = eq
				} else {
					all = g.node("And", all, eq)
				}
			}
			found = append(found, reduceMax(cast(all, "float32")))
		}
		if len(found) == 1 {
			has = append(has, found[0].(string))
		} else {
			has = append(has, g.node("Max", found...))
		}
	}
	var mapping, endsAt string
	if n := len(has); speech != nil {
		hidden := gather(cast(speech.encoder, "float32"), 0, 1) // [batch, size]
		mapping = cast(g.node("Round", gather(hidden, 0, 1)), "int64")
		endsAt = g.node("Add", cast(g.node("Round", gather(hidden, 1, 1)), "int64"), scalarI(int64(speech.timestampsBegin)))
	} else if n == 0 {
		mapping = g.node("Mul", g.node("Reshape", cast(last, "int64"), g.int64s(-1)), scalarI(0))
	} else {
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
		mapping = g.node("ArgMax", scores, intAttr("axis", 1), intAttr("keepdims", 0))
	}

	// The answer's token as far along as the call is, its end after it: a
	// table of the answers, each as long as the longest and one more. A
	// recognizer's has its transcripts with timestamps after them: 0.00,
	// the transcript, and its end (endOfSound, which the encoder's is put
	// for).
	const endOfSound = -1
	answers := p.Answers
	if speech != nil {
		for _, a := range p.Answers {
			answers = append(answers, append(append([]int32{speech.timestampsBegin}, a...), endOfSound))
		}
	}
	width := 1
	for _, a := range answers {
		width = max(width, len(a)+1)
	}
	table := make([]float64, 0, width*len(answers))
	for _, a := range answers {
		for j := range width {
			t := p.EOG
			if j < len(a) {
				t = a[j]
			}
			table = append(table, float64(t))
		}
	}
	if speech != nil {
		// The transcript with timestamps, unless the prompt's last token
		// (the last forced) says there are none.
		promptEnd := g.node("Sub", cast(last, "int64"), scalarI(1))
		lastForced := g.node("Reshape", g.node("GatherElements", tokens, promptEnd, intAttr("axis", 1)), g.int64s(-1))
		timed := cast(g.node("Not", g.node("Equal", lastForced, scalarI(int64(speech.noTimestamps)))), "int64")
		mapping = g.node("Add", mapping, g.node("Mul", timed, scalarI(int64(len(p.Answers)))))
	}
	step := g.node("Reshape", cast(along, "int64"), g.int64s(-1))
	step = g.node("Max", g.node("Min", step, scalarI(int64(width-1))), scalarI(0))
	at := g.node("Add", g.node("Mul", mapping, scalarI(int64(width))), step)
	next := cast(g.node("Gather", g.floats([]int64{int64(len(table))}, table), at, intAttr("axis", 0)), "int64")
	if speech != nil {
		next = g.node("Where", g.node("Equal", next, scalarI(endOfSound)), endsAt, next)
	}
	oneHot := g.node("OneHot", next, g.int64s(int64(p.Vocab)), g.tensor([]int64{2}, l.logits.Type, l.logits.Type.Encode([]float64{0, logitOn})), intAttr("axis", -1))
	// The logits of each of the call's positions, or of as many as the
	// model is asked to keep (0: all).
	kept := seq
	if l.keep != nil {
		keep := g.node("Reshape", cast(l.keep.Name, "int64"), g.int64s(1))
		kept = g.node("Where", g.node("Greater", keep, g.int64s(0)), keep, seq)
	}
	shape := g.node("Concat", g.int64s(1), kept, g.int64s(1), intAttr("axis", 0))
	g.nodeNamed("logits", "Expand", g.node("Unsqueeze", oneHot, g.int64s(1)), shape)
	return g, nil
}

// logitOn is the next token's logit; every other's is 0.
const logitOn = 100
