package executorch

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A language model's program is as ExecuTorch's LLM export writes it, and
// its runners (text_llm_runner: React Native ExecuTorch's, its Android and
// iOS libraries', its Python package's) run it: forward, of the call's
// tokens ([1, sequence], of any length up to its bound) and the position of
// the first (input_pos, [1]), gives the logits of the last ([1,
// vocabulary]), its KV cache in the method's memory from call to call; and
// methods of no inputs give its settings (get_max_seq_len, get_eos_ids...),
// which its runner reads. The runner calls forward with the prompt's tokens
// (in one call, or in chunks), then with each token it generates.

// tokenizerFile is the contract's file of a language model's tokenizer.
const tokenizerFile = "tokenizer"

// isLanguageModel reports whether a program's methods are a language
// model's.
func isLanguageModel(methods []method) bool {
	var forward, settings bool
	for _, m := range methods {
		switch {
		case m.Name == "forward":
			forward = len(m.Inputs) == 2 && m.Inputs[0].Type == "int64" && len(m.Inputs[0].Shape) == 2 && m.Inputs[1].Type == "int64"
		case m.Name == "get_max_seq_len" && len(m.Values) == 1 && m.Values[0].Int != nil:
			settings = true
		}
	}
	return forward && settings
}

// languageContract makes a language model's contract a text generation
// model's, which carries its tokenizer (tokenizer.json, beside the program
// or in a folder above it, as Hugging Face's repositories have it) and the
// stop token that ends its turn (of the ones its get_eos_ids gives).
func languageContract(c *contract.Contract, path string, d *details) error {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s cannot be stubbed: %s", path, fmt.Sprintf(f, args...))
	}
	var tok []byte
	for dir, i := filepath.Dir(path), 0; i < 3 && tok == nil; dir, i = filepath.Dir(dir), i+1 {
		tok, _ = os.ReadFile(filepath.Join(dir, "tokenizer.json"))
	}
	if tok == nil {
		return axxerr.New(format.CodeModel, exitcode.Usage, "the model %s is a language model, whose contract carries its tokenizer: there is no tokenizer.json beside it or in the two folders above", path).
			WithHint("keep the program in its folder as it comes (as Hugging Face's repositories have it: tokenizer.json at the top, the program beside it or below)")
	}
	spec, err := lm.TokenizerJSON(tok)
	if err != nil {
		return fail("%v", err)
	}
	v, err := lm.NewVocab(spec)
	if err != nil {
		return fail("%v", err)
	}
	var stops []int32
	for _, m := range d.Methods {
		if m.Name == "get_eos_ids" && len(m.Values) == 1 {
			if n := m.Values[0].Int; n != nil {
				stops = append(stops, int32(*n))
			}
			for _, n := range m.Values[0].Ints {
				stops = append(stops, int32(n))
			}
		}
	}
	if len(stops) == 0 {
		return fail("its program gives no stop token: it has no get_eos_ids")
	}
	d.EndOfGeneration = lm.EndOfTurn(v, stops)
	c.Task = "text-generation"
	c.Carry(tokenizerFile, "tokenizer.json", tok)
	return nil
}

// writeLanguageModel writes a language model's stub: its methods of
// settings as the model's, and its forward.
func writeLanguageModel(s *stub.Stub, d details) ([]byte, error) {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %s", fmt.Sprintf(f, args...))
	}
	if s.Matcher != stub.TextMatcher {
		return nil, fail(`a language model's stub generates text for prompts matched by their text: a mapping's input is {"text": ...}`)
	}
	tok, err := s.Contract.File(tokenizerFile)
	if err != nil {
		return nil, axxerr.Wrap(err, contract.CodeContract, exitcode.Usage, "the contract carries no tokenizer of the model").
			WithHint("write the contract again from the model with `axx models contract <folder>/<program>.pte`")
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
	// The conversation's greatest length: its context's.
	var length int64
	for _, m := range d.Methods {
		if (m.Name == "get_max_context_len" || m.Name == "get_max_seq_len") && len(m.Values) == 1 && m.Values[0].Int != nil {
			length = max(length, *m.Values[0].Int)
		}
	}
	prog := &program{}
	for _, m := range d.Methods {
		var pl *plan
		switch {
		case m.Name == "forward":
			pl, err = languageForward(prog, m, p, length, nil)
		case len(m.Inputs) == 0 && len(m.Values) > 0:
			pl = settingsOf(m)
		default:
			err = fmt.Errorf("it is not one a language model's stub has")
		}
		if err != nil {
			return nil, fail("its method %s: %v", m.Name, err)
		}
		prog.plans = append(prog.plans, pl)
	}
	return prog.encode(), nil
}

// settingsOf is the plan of a method that gives values: the model's.
func settingsOf(m method) *plan {
	l := &lowering{pl: &plan{name: m.Name}, ops: map[[2]string]int{}}
	for _, c := range m.Values {
		var v int
		switch {
		case c.Int != nil:
			v = l.integer(*c.Int)
		case c.Bool != nil:
			v = l.boolean(*c.Bool)
		case c.String != nil:
			v = l.add(evalue{kind: evString, s: *c.String})
		default:
			v = l.intList(c.Ints...)
		}
		l.pl.outputs = append(l.pl.outputs, v)
	}
	l.pl.inMeta, l.pl.outMeta = treeSpecs(0, len(m.Values))
	return l.pl
}

// languageForward is the plan of a language model stub's forward, of a
// conversation of length tokens at most. Its memory keeps the
// conversation's tokens, and which wrote each: the prompt (1) or the model
// (2: a call of the one token the call before it gave, at the position
// after it, as its runner gives each it generates; a runner that gives a
// prompt a token a call, as one of fixed shapes does, gives its last
// another). The call's tokens are written at their positions; the
// prompt ends after the last position a prompt wrote; the next token is as
// far along the answer as the call's last position is past that; and the
// answer is the first mapping's whose text the conversation has before it
// (its tokens one after another), or the default's; or, of a speech
// recognizer's decoder, the one its encoder's hidden state gives. Its
// logits put the next token ahead of every other, the end of generation
// after the answer.
func languageForward(prog *program, m method, p *lm.Plan, length int64, speech *hearing) (*plan, error) {
	if len(m.Outputs) != 1 {
		return nil, fmt.Errorf("it gives %d outputs, and a language model's gives its logits", len(m.Outputs))
	}
	inputs := 2
	if speech != nil {
		inputs = 3
	}
	if len(m.Inputs) != inputs {
		return nil, fmt.Errorf("it has %d inputs, not %d", len(m.Inputs), inputs)
	}
	tokensIn, posIn, logitsOut := m.Inputs[0], m.Inputs[1], m.Outputs[0]
	if len(tokensIn.Shape) != 2 || tokensIn.Shape[0].Size != 1 || len(posIn.Shape) != 1 {
		return nil, fmt.Errorf("its inputs are %s and %s: a language model's are its tokens [1, sequence] and their positions ([1], the first's, or [sequence])", tokensIn.Shape, posIn.Shape)
	}
	vocab := int64(1)
	for _, d := range logitsOut.Shape {
		if d.Size != 1 {
			vocab *= d.Size
		}
	}
	if logitsOut.Type != "float32" && logitsOut.Type != "float16" {
		return nil, fmt.Errorf("its logits are %s: a language model's stub gives float32 or float16 logits", logitsOut.Type)
	}
	p.Vocab = int(vocab)
	for _, a := range p.Answers {
		for _, t := range a {
			if int64(t) >= vocab {
				return nil, fmt.Errorf("an answer has the token %d, which the model's %d logits do not", t, vocab)
			}
		}
	}
	bound := tokensIn.Shape[1].Size
	length = max(length, bound+1)
	boundedAt := func(input, dim int) bool {
		return input < len(m.Bounded) && dim < len(m.Bounded[input]) && m.Bounded[input][dim]
	}

	l := &lowering{p: prog, pl: &plan{name: m.Name}, ops: map[[2]string]int{}}
	ints := func(vs ...int64) []byte {
		fs := make([]float64, len(vs))
		for i, v := range vs {
			fs[i] = float64(v)
		}
		return contract.DType("int64").Encode(fs)
	}
	one := func(v int64) int { return l.constant("int64", []int64{1}, ints(v)) }
	long := func(name, overload string, shape []int64, args ...int) int {
		return l.kernel(name, overload, "int64", shape, args...)
	}
	is := func(name, overload string, shape []int64, args ...int) int {
		return l.kernel(name, overload, "bool", shape, args...)
	}
	and := func(a, b int, shape []int64) int { return is("aten::logical_and", "out", shape, a, b) }
	where := func(shape []int64, cond, a, b int) int {
		return l.kernel("aten::where", "self_out", "int64", shape, cond, a, b)
	}
	alpha := l.integer(1)
	zero, unit := one(0), one(1)
	n, call := []int64{length}, []int64{1}

	// The call's tokens, as many as it gives: [1, sequence], up to its
	// bound.
	tokens := l.alloc("int64", []int64{1, bound})
	l.pl.values[tokens].tensor.bounded = boundedAt(0, 1)
	start := l.alloc("int64", []int64{posIn.Shape[0].Size})
	l.pl.values[start].tensor.bounded = boundedAt(1, 0)
	l.pl.inputs = []int{tokens, start}
	if posIn.Shape[0].Size != 1 {
		// The call's positions: the first's.
		start = long("aten::index_select", "out", call, start, l.integer(0), zero)
	}
	var hidden int
	if speech != nil {
		in := m.Inputs[2]
		hidden = l.alloc(in.Type, sizes(in.Shape))
		l.pl.inputs = append(l.pl.inputs, hidden)
	}
	ones := l.kernel("aten::full_like", "out", "int64", []int64{1, bound}, tokens, l.integer(1), l.null())
	l.pl.values[ones].tensor.bounded = true
	count := long("aten::sum", "IntList_out", call, ones, l.intList(1), l.boolean(false), l.null())
	row := l.kernel("aten::select_copy", "int_out", "int64", []int64{bound}, tokens, l.integer(0), l.integer(0))
	l.pl.values[row].tensor.bounded = true

	// The conversation, its call's tokens written at their positions.
	positions := long("aten::arange", "start_out", n, l.integer(0), l.integer(length), l.integer(1))
	end := long("aten::add", "out", call, start, count, alpha)
	at := long("aten::sub", "out", n, positions, start, alpha)
	at = long("aten::maximum", "out", n, at, zero)
	at = long("aten::minimum", "out", n, at, long("aten::sub", "out", call, count, unit, alpha))
	called := long("aten::index_select", "out", n, row, l.integer(0), at)
	inCall := and(is("aten::ge", "Tensor_out", n, positions, start), is("aten::lt", "Tensor_out", n, positions, end), n)
	keptTokens, keptWriters := l.alloc("int64", n), l.alloc("int64", n)
	conversation := where(n, inCall, called, keptTokens)
	expected := l.alloc("int64", []int64{2}) // the last call's next token's position, and the token
	generating := and(
		and(is("aten::eq", "Tensor_out", call, count, unit), is("aten::gt", "Tensor_out", call, start, zero), call),
		and(is("aten::eq", "Tensor_out", call, start, long("aten::index_select", "out", call, expected, l.integer(0), zero)),
			is("aten::eq", "Tensor_out", call, long("aten::index_select", "out", call, row, l.integer(0), zero), long("aten::index_select", "out", call, expected, l.integer(0), unit)), call),
		call)
	writer := long("aten::add", "out", call, long("dim_order_ops::_to_dim_order_copy", "out", call, generating, l.boolean(false), l.intList(0)), unit, alpha)
	writers := where(n, inCall, writer, keptWriters)
	l.call("dim_order_ops::_clone_dim_order", "out", conversation, l.boolean(false), l.intList(0), keptTokens)
	l.call("dim_order_ops::_clone_dim_order", "out", writers, l.boolean(false), l.intList(0), keptWriters)

	// Where the prompt ends, and how far along the answer the call is.
	visible := is("aten::lt", "Tensor_out", n, positions, end)
	prompted := and(visible, is("aten::eq", "Tensor_out", n, writers, unit), n)
	upTo := long("aten::add", "out", n, positions, unit, alpha)
	last := long("aten::amax", "out", call, where(n, prompted, upTo, zero), l.intList(0), l.boolean(true))
	along := long("aten::sub", "out", call, end, last, alpha)

	// The conversation's tokens before the prompt's end, −1 elsewhere,
	// padded with −1: every key has windows.
	longest := int64(1)
	for _, k := range p.Keys {
		for _, v := range k.Variants {
			longest = max(longest, int64(len(v)))
		}
	}
	seen := and(visible, is("aten::lt", "Tensor_out", n, positions, last), n)
	padded := []int64{length + longest}
	x := long("aten::constant_pad_nd", "out", padded, where(n, seen, conversation, one(-1)), l.intList(0, longest), l.double(-1))

	// The first mapping whose text the conversation has, in any of its
	// variants; or the default.
	var has []int
	for _, k := range p.Keys {
		found := -1
		for _, v := range k.Variants {
			windows := []int64{padded[0] - int64(len(v)) + 1}
			all := -1
			for i, id := range v {
				window := long("aten::slice_copy", "Tensor_out", windows, x, l.integer(0), l.integer(int64(i)), l.integer(windows[0]+int64(i)), l.integer(1))
				eq := is("aten::eq", "Scalar_out", windows, window, l.integer(int64(id)))
				if all < 0 {
					all = eq
				} else {
					all = and(all, eq, windows)
				}
			}
			hit := l.kernel("aten::amax", "out", "float32", call,
				l.kernel("dim_order_ops::_to_dim_order_copy", "out", "float32", windows, all, l.boolean(false), l.intList(0)), l.intList(0), l.boolean(true))
			if found < 0 {
				found = hit
			} else {
				found = l.kernel("aten::maximum", "out", "float32", call, found, hit)
			}
		}
		has = append(has, found)
	}
	mapping := zero
	var endsAt int
	if speech != nil {
		// The mapping, and where the sound ends, are the hidden state's
		// first values.
		size := int64(1)
		for _, d := range m.Inputs[2].Shape {
			size *= d.Size
		}
		h := hidden
		if m.Inputs[2].Type != "float32" {
			h = l.kernel("dim_order_ops::_to_dim_order_copy", "out", "float32", sizes(m.Inputs[2].Shape), h, l.boolean(false), l.intList(dimOrder(sizes(m.Inputs[2].Shape))...))
		}
		h = l.kernel("aten::view_copy", "out", "float32", []int64{size}, h, l.intList(size))
		h = l.kernel("aten::slice_copy", "Tensor_out", "float32", []int64{2}, h, l.integer(0), l.integer(0), l.integer(2), l.integer(1))
		h = long("dim_order_ops::_to_dim_order_copy", "out", []int64{2}, l.kernel("aten::round", "out", "float32", []int64{2}, h), l.boolean(false), l.intList(0))
		mapping = long("aten::index_select", "out", call, h, l.integer(0), zero)
		endsAt = long("aten::add", "out", call, long("aten::index_select", "out", call, h, l.integer(0), unit), one(int64(speech.timestampsBegin)), alpha)
		// Its transcript with timestamps, unless the prompt's last token
		// (the last the runtime forced) says there are none.
		lastForced := long("aten::index_select", "out", call, conversation, l.integer(0), long("aten::maximum", "out", call, long("aten::sub", "out", call, last, unit, alpha), zero))
		timed := is("aten::logical_not", "out", call, is("aten::eq", "Scalar_out", call, lastForced, l.integer(int64(speech.noTimestamps))))
		timed = long("dim_order_ops::_to_dim_order_copy", "out", call, timed, l.boolean(false), l.intList(0))
		mapping = long("aten::add", "out", call, mapping, long("aten::mul", "out", call, timed, one(int64(len(p.Answers)))), alpha)
	} else if keys := int64(len(has)); keys > 0 {
		weights := make([]float64, keys+1)
		for k := range keys {
			weights[k] = float64(keys - k)
		}
		weights[keys] = 0.5
		scores := l.kernel("aten::cat", "out", "float32", []int64{keys + 1}, l.tensorList(append(has, l.constant("float32", call, contract.DType("float32").Encode([]float64{1})))...), l.integer(0))
		scores = l.kernel("aten::mul", "out", "float32", []int64{keys + 1}, scores, l.constant("float32", []int64{keys + 1}, contract.DType("float32").Encode(weights)))
		mapping = long("aten::argmax", "out", call, scores, l.integer(0), l.boolean(true))
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
	width := int64(1)
	for _, a := range answers {
		width = max(width, int64(len(a)+1))
	}
	var table []int64
	for _, a := range answers {
		for j := range width {
			t := p.EOG
			if j < int64(len(a)) {
				t = a[j]
			}
			table = append(table, int64(t))
		}
	}
	step := long("aten::minimum", "out", call, long("aten::maximum", "out", call, along, zero), one(width-1))
	index := long("aten::add", "out", call, long("aten::mul", "out", call, mapping, one(width)), step, alpha)
	next := long("aten::index_select", "out", call, l.constant("int64", []int64{int64(len(table))}, ints(table...)), l.integer(0), index)
	if speech != nil {
		next = where(call, is("aten::eq", "Scalar_out", call, next, l.integer(endOfSound)), endsAt, next)
	}
	l.call("aten::cat", "out", l.tensorList(end, next), l.integer(0), expected)
	ids := long("aten::arange", "start_out", []int64{vocab}, l.integer(0), l.integer(vocab), l.integer(1))
	hot := is("aten::eq", "Tensor_out", []int64{vocab}, ids, next)
	logits := l.kernel("aten::where", "self_out", logitsOut.Type, []int64{vocab}, hot, l.scalar(logitsOut.Type, logitOn), l.scalar(logitsOut.Type, 0))
	shape := sizes(logitsOut.Shape)
	if len(shape) == 3 && shape[1] == bound && bound > 1 {
		// The logits of each of the call's tokens, as many as it gives:
		// each the next token's.
		row := l.kernel("aten::view_copy", "out", logitsOut.Type, []int64{1, 1, vocab}, logits, l.intList(1, 1, vocab))
		each := is("aten::eq", "Scalar_out", []int64{1, bound}, ones, l.integer(1))
		l.pl.values[each].tensor.bounded = true
		each = is("aten::unsqueeze_copy", "out", []int64{1, bound, 1}, each, l.integer(2))
		l.pl.values[each].tensor.bounded = true
		logits = l.kernel("aten::where", "self_out", logitsOut.Type, shape, each, row, l.scalar(logitsOut.Type, 0))
		l.pl.values[logits].tensor.bounded = true
	} else {
		logits = l.kernel("aten::view_copy", "out", logitsOut.Type, shape, logits, l.intList(shape...))
	}
	l.pl.outputs = []int{logits}
	l.pl.inMeta, l.pl.outMeta = treeSpecs(len(l.pl.inputs), 1)
	return l.pl, nil
}

// logitOn is the next token's logit; every other's is 0.
const logitOn = 100

// hearing is a speech recognizer's decoder's: the tokens that say a
// transcript has no timestamps, and the first timestamp's (0.00).
type hearing struct {
	noTimestamps, timestampsBegin int32
}

// sizes are the dimensions' sizes.
func sizes(dims []contract.Dim) []int64 {
	out := make([]int64, len(dims))
	for i, d := range dims {
		out[i] = d.Size
	}
	return out
}
