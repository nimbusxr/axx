// Package ncnn reads ncnn models' contracts (.param, its weights beside it
// in .bin: the models Android and iOS apps run with Tencent's ncnn) and
// writes stubs as ncnn models of the layers every ncnn build registers
// (Pooling, Reduction, BinaryOp, UnaryOp, InnerProduct, Clip...): the
// input reduced as matching reduces it, its correlation with each key
// (InnerProduct), the best's one-hot (its score the greatest, as Reduction
// and Clip give it, the first of equal ones), and the answers' table's
// product with it, of no batch, as ncnn's blobs are.
package ncnn

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "ncnn", Name: "ncnn", Extensions: []string{".param"}, Contract: ReadContract, Write: Write})
}

// magic begins a model's param file.
const magic = "7767517"

// ReadContract reads the contract of the ncnn model at path (its .param):
// its inputs (each Input layer's blob, of its w, h and c, as [c, h, w]), its
// outputs (the blobs no layer takes), and the layers it uses. ncnn does not
// say its outputs' shapes, nor, of some models, its inputs': they come from
// `axx models contract --input/--output`. An input shaped like a picture
// is given its layout, CHW.
func ReadContract(path string) (*contract.Contract, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the model")
	}
	ls, err := readParam(b)
	if err != nil {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not an ncnn model axx reads: %v", path, err).
			WithHint("an ncnn model is a .param file (its layers, as text) and its .bin (its weights), as pnnx writes them")
	}
	c := &contract.Contract{Format: contract.Format{Type: "ncnn"}}
	consumed := map[string]bool{}
	var produced []string
	ops := map[string]bool{}
	for _, l := range ls {
		ops[l.typ] = true
		for _, in := range l.inputs {
			consumed[in] = true
		}
		produced = append(produced, l.outputs...)
		if l.typ != "Input" {
			continue
		}
		t := contract.Tensor{Name: l.outputs[0], Type: "float32"}
		var dims []int64
		for _, k := range []int{2, 1, 0} { // c, h, w
			if v, err := strconv.ParseInt(l.params[k], 10, 64); err == nil && v > 0 {
				dims = append(dims, v)
			}
		}
		for _, d := range dims {
			t.Shape = append(t.Shape, contract.Dim{Size: d})
		}
		if len(dims) == 0 {
			t.Type = "" // its shape is the app's
		}
		c.Inputs = append(c.Inputs, t)
	}
	for _, name := range produced {
		if !consumed[name] {
			c.Outputs = append(c.Outputs, contract.Tensor{Name: name})
		}
	}
	for op := range ops {
		c.Format.Operators = append(c.Format.Operators, op)
	}
	slices.Sort(c.Format.Operators)
	for i := range c.Inputs {
		guessImage(&c.Inputs[i])
	}
	return c, nil
}

// guessImage gives an input shaped like a picture of no batch its layout,
// CHW: ncnn's pictures are (as from_pixels gives them).
func guessImage(t *contract.Tensor) {
	if len(t.Shape) != 3 {
		return
	}
	batched := contract.Tensor{Shape: append([]contract.Dim{{Size: 1}}, t.Shape...)}
	batched.GuessImage()
	if batched.Image != nil && batched.Image.Layout == "NCHW" {
		t.Image = batched.Image
		t.Image.Layout = "CHW"
	}
}

// layer is a layer of a model: its type and name, its blobs, and its
// parameters by their ids (an array's id is −23300 less its own).
type layer struct {
	typ, name       string
	inputs, outputs []string
	params          map[int]string
	weights         []byte
}

// readParam reads a param file's layers.
func readParam(b []byte) ([]layer, error) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var lines []string
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) < 2 || lines[0] != magic {
		return nil, fmt.Errorf("it does not begin with %s", magic)
	}
	var out []layer
	for _, line := range lines[2:] {
		f := strings.Fields(line)
		if len(f) < 4 {
			return nil, fmt.Errorf("its layer %q", line)
		}
		nin, err1 := strconv.Atoi(f[2])
		nout, err2 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil || len(f) < 4+nin+nout {
			return nil, fmt.Errorf("its layer %q", line)
		}
		l := layer{typ: f[0], name: f[1], inputs: f[4 : 4+nin], outputs: f[4+nin : 4+nin+nout], params: map[int]string{}}
		for _, kv := range f[4+nin+nout:] {
			k, v, ok := strings.Cut(kv, "=")
			id, err := strconv.Atoi(k)
			if !ok || err != nil {
				return nil, fmt.Errorf("its layer %s's parameter %q", l.name, kv)
			}
			l.params[id] = v
		}
		out = append(out, l)
	}
	return out, nil
}

// graph is a model being written: its layers, and the blobs' names.
type graph struct {
	layers []layer
	n      int
}

// add adds a layer of the type and parameters on the inputs, and is its
// output blob (of the name, unless "").
func (g *graph) add(typ, out string, params map[int]string, weights []byte, inputs ...string) string {
	if out == "" {
		out = fmt.Sprintf("axx_%d", g.n)
	}
	g.layers = append(g.layers, layer{typ: typ, name: fmt.Sprintf("%s_%d", strings.ToLower(typ), g.n), inputs: inputs, outputs: []string{out}, params: params, weights: weights})
	g.n++
	return out
}

func p(kv ...any) map[int]string {
	out := map[int]string{}
	for i := 0; i < len(kv); i += 2 {
		k := kv[i].(int)
		switch v := kv[i+1].(type) {
		case int:
			out[k] = strconv.Itoa(v)
		case float64:
			out[k] = strconv.FormatFloat(v, 'e', -1, 32)
		case []int:
			s := make([]string, len(v))
			for j, x := range v {
				s[j] = strconv.Itoa(x)
			}
			out[-23300-k] = strconv.Itoa(len(v)) + "," + strings.Join(s, ",")
		}
	}
	return out
}

// innerProduct adds an InnerProduct of the weights ([outputs][inputs]) and
// the bias, if any: its weights a float32 tag (0) and the weights, then the
// bias.
func (g *graph) innerProduct(in string, weights [][]float64, bias []float64) string {
	var w bytes.Buffer
	_ = binary.Write(&w, binary.LittleEndian, uint32(0))
	for _, row := range weights {
		for _, x := range row {
			_ = binary.Write(&w, binary.LittleEndian, float32(x))
		}
	}
	for _, x := range bias {
		_ = binary.Write(&w, binary.LittleEndian, float32(x))
	}
	hasBias := 0
	if bias != nil {
		hasBias = 1
	}
	return g.add("InnerProduct", "", p(0, len(weights), 1, hasBias, 2, len(weights)*len(weights[0])), w.Bytes(), in)
}

// mean is a Reduction of the mean over an axis (PyTorch's, of no batch),
// its dimension kept.
func (g *graph) mean(in string, axis int) string {
	return g.add("Reduction", "", p(0, 3, 1, 0, 3, []int{axis}, 4, 1, 5, 1), nil, in)
}

func (g *graph) binary(op int, a, b string) string {
	return g.add("BinaryOp", "", p(0, op), nil, a, b)
}

func (g *graph) scalar(op int, a string, v float64) string {
	return g.add("BinaryOp", "", p(0, op, 1, 1, 2, v), nil, a)
}

// The BinaryOps' and UnaryOps' types.
const (
	opAdd, opSub, opMul, opDiv, opMax, opRSub = 0, 1, 2, 3, 4, 7
	opSqrt, opLog                             = 5, 8
)

// encode is the model's param file and weights, a Split of each blob more
// than one layer takes.
func (g *graph) encode() (param, bin []byte) {
	uses := map[string]int{}
	for _, l := range g.layers {
		for _, in := range l.inputs {
			uses[in]++
		}
	}
	var layers []layer
	given := map[string]int{}
	for _, l := range g.layers {
		for i, in := range l.inputs {
			if uses[in] > 1 {
				l.inputs[i] = fmt.Sprintf("%s_%d", in, given[in])
				given[in]++
			}
		}
		layers = append(layers, l)
		for _, out := range l.outputs {
			if uses[out] > 1 {
				var outs []string
				for k := range uses[out] {
					outs = append(outs, fmt.Sprintf("%s_%d", out, k))
				}
				layers = append(layers, layer{typ: "Split", name: "split_" + out, inputs: []string{out}, outputs: outs})
			}
		}
	}
	blobs := 0
	for _, l := range layers {
		blobs += len(l.outputs)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%d %d\n", magic, len(layers), blobs)
	var w bytes.Buffer
	for _, l := range layers {
		fmt.Fprintf(&b, "%-16s %-16s %d %d %s", l.typ, l.name, len(l.inputs), len(l.outputs), strings.Join(append(slices.Clone(l.inputs), l.outputs...), " "))
		keys := slices.Sorted(func(yield func(int) bool) {
			for k := range l.params {
				if !yield(k) {
					return
				}
			}
		})
		for _, k := range keys {
			fmt.Fprintf(&b, " %d=%s", k, l.params[k])
		}
		b.WriteByte('\n')
		w.Write(l.weights)
	}
	return []byte(b.String()), w.Bytes()
}

// Write writes a stub as an ncnn model: its param file, its weights (the
// .bin beside it).
func Write(s *stub.Stub) (format.Model, error) {
	fail := func(f string, args ...any) error {
		return axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: %s", fmt.Sprintf(f, args...))
	}
	if s.Generated != nil || s.Matcher != stub.NearestMatcher {
		return format.Model{}, fail("ncnn's stubs match pictures, sounds, speech's features and tensors (by the nearest key): ncnn has no comparisons to match texts by")
	}
	c := s.Contract
	if len(c.Inputs) != 1 {
		return format.Model{}, fail("ncnn's stubs have one input")
	}
	in := &c.Inputs[0]
	if in.Type != "float32" {
		return format.Model{}, fail("its input %s is %s: ncnn's blobs are float32", in.Name, in.Type)
	}
	for _, d := range in.Shape {
		if !d.Fixed() {
			return format.Model{}, fail("its input %s has an open dimension: ncnn's blobs have no batch, and a stub's shapes are fixed", in.Name)
		}
	}
	r := s.Nearest.Reducer
	g := &graph{}
	x := in.Name
	inDims := make([]int, len(in.Shape))
	for i, d := range in.Shape {
		inDims[i] = int(d.Size)
	}
	// An NCHW picture's batch is not ncnn's, whose blobs have none: an app
	// gives the picture's Mat, CHW, and gets its outputs of no batch either.
	batched := in.Image != nil && in.Image.Layout == "NCHW" && len(inDims) == 4
	if batched {
		if inDims[0] != 1 {
			return format.Model{}, fail("its input %s is a batch of %d pictures: an ncnn blob is one", in.Name, inDims[0])
		}
		inDims = inDims[1:]
	}
	inParams, err := shapeParams(inDims)
	if err != nil {
		return format.Model{}, fail("its input %s: %v", in.Name, err)
	}
	g.layers = append(g.layers, layer{typ: "Input", name: "input", outputs: []string{x}, params: inParams})

	var channels, cells int
	standardize := true
	switch {
	case r.Features:
		x = g.features(r, x)
		standardize = false
	case r.Image:
		if in.Image != nil && in.Image.Layout != "CHW" && in.Image.Layout != "NCHW" {
			return format.Model{}, fail("its input %s is a picture of layout %s: ncnn's are CHW", in.Name, in.Image.Layout)
		}
		x = g.add("Pooling", "", p(0, 1, 1, r.KW, 11, r.KH, 2, r.KW, 12, r.KH, 5, 1), nil, x)
		channels, cells = r.C, r.PH*r.PW
	case r.Audio:
		frames, frame, bins := r.N/r.Frame, r.Frame, r.Bins()
		x = g.add("Reshape", "", p(0, frame, 1, frames), nil, x)
		cos, sin := r.Fourier()
		basis := func(t []float64) [][]float64 {
			out := make([][]float64, bins)
			for k := range out {
				out[k] = make([]float64, frame)
				for i := range frame {
					out[k][i] = t[i*bins+k]
				}
			}
			return out
		}
		re, im := g.innerProduct(x, basis(cos), nil), g.innerProduct(x, basis(sin), nil)
		power := g.binary(opAdd, g.binary(opMul, re, re), g.binary(opMul, im, im))
		sums := make([][]float64, match.Bands)
		for b := range sums {
			sums[b] = make([]float64, bins)
		}
		for j := range bins {
			sums[r.BandOf(j)][j] = 1
		}
		bands := g.add("Reduction", "", p(0, 3, 1, 0, 3, []int{0}, 4, 0, 5, 1), nil, g.innerProduct(power, sums, nil))
		x = g.add("UnaryOp", "", p(0, opLog), nil, g.scalar(opAdd, bands, match.LogEpsilon))
		channels, cells = 1, match.Bands
	default:
		channels, cells = 1, r.N
	}
	if standardize {
		// Each channel standardized.
		x = g.add("Reshape", "", p(0, cells, 1, channels), nil, x)
		centered := g.binary(opSub, x, g.mean(x, 1))
		sd := g.add("UnaryOp", "", p(0, opSqrt), nil, g.scalar(opAdd, g.mean(g.binary(opMul, centered, centered), 1), halfEpsilon))
		x = g.add("Flatten", "", nil, nil, g.binary(opDiv, centered, sd))
	}

	// The keys' correlations (x·k / D), the similarity the default's; the
	// first best's one-hot: 1 where a score is the greatest (less the
	// later ones' tiny offsets, so the first of equal ones is), 0 elsewhere.
	d, n := r.Dims(), len(s.Nearest.Table.Keys)
	keys := make([][]float64, n+1)
	bias := make([]float64, n+1)
	for j := range keys {
		keys[j] = make([]float64, d)
		if j < n {
			for i, v := range s.Nearest.Table.Keys[j] {
				keys[j][i] = v / float64(d)
			}
		}
		bias[j] = -float64(j) * tieOffset
	}
	// A key the same as an earlier one's never answers (the first of equal
	// keys does): half precision does not keep the offsets apart.
	for j := 1; j < n; j++ {
		for i := range j {
			if slices.Equal(s.Nearest.Table.Keys[i], s.Nearest.Table.Keys[j]) {
				bias[j] = -4
				break
			}
		}
	}
	bias[n] += s.Nearest.Table.Similarity
	scores := g.innerProduct(x, keys, bias)
	best := g.add("Reduction", "", p(0, 4, 1, 1, 4, 1, 5, 1), nil, scores)
	below := g.scalar(opMul, g.binary(opSub, best, scores), hotGain)
	hot := g.add("Clip", "", map[int]string{0: "0.000000e+00", 1: "1.000000e+00"}, nil, g.scalar(opRSub, below, 1))

	// Each output: the answers' product with the one-hot.
	for _, out := range c.Outputs {
		size := 1
		var dims []int
		for _, dim := range out.Shape {
			if !dim.Fixed() {
				return format.Model{}, fail("its output %s has an open dimension", out.Name)
			}
			size *= int(dim.Size)
			dims = append(dims, int(dim.Size))
		}
		table := make([][]float64, size)
		for k := range table {
			table[k] = make([]float64, n+1)
		}
		for m, a := range s.Answers {
			vs := a[out.Name]
			if len(vs) != size {
				return format.Model{}, fail("the answer of %s gives output %s %d values, not %d", s.Names[m], out.Name, len(vs), size)
			}
			for k, v := range vs {
				table[k][m] = v
			}
		}
		y := g.innerProduct(hot, table, nil)
		if batched && len(dims) > 1 && dims[0] == 1 {
			dims = dims[1:]
		}
		shape, err := shapeParams(dims)
		if err != nil {
			return format.Model{}, fail("its output %s: %v", out.Name, err)
		}
		g.add("Reshape", out.Name, shape, nil, y)
	}
	param, bin := g.encode()
	return format.Model{File: param, Beside: map[string][]byte{".bin": bin}}, nil
}

// features adds the layers that reduce a sound's features x ([mels,
// frames]) as r.Reduce does: pooled ([bands, times]); each time's bands
// (a column's) centered and scaled to their root mean square, a time under
// the level left out (match.Heard: a Clip); then the whole scaled to its
// own, flattened.
func (g *graph) features(r *match.Reducer, x string) string {
	x = g.add("Reshape", "", p(0, r.W, 1, r.H, 2, 1), nil, x)
	x = g.add("Pooling", "", p(0, 1, 1, r.KW, 11, r.KH, 2, r.KW, 12, r.KH, 5, 1), nil, x)
	x = g.add("Reshape", "", p(0, r.PW, 1, r.PH), nil, x)
	bandsMean := func(x string) string {
		return g.add("Reduction", "", p(0, 3, 1, 0, 3, []int{0}, 4, 1, 5, 1), nil, x)
	}
	centered := g.binary(opSub, x, bandsMean(x))
	square := bandsMean(g.binary(opMul, centered, centered))
	heard := g.add("Clip", "", map[int]string{0: "0.000000e+00", 1: "1.000000e+00"}, nil,
		g.scalar(opMul, g.scalar(opSub, square, match.SegmentLevel), match.SegmentGain))
	scale := g.add("UnaryOp", "", p(0, opSqrt), nil, g.scalar(opMax, square, match.SegmentLevel))
	y := g.add("Flatten", "", nil, nil, g.binary(opDiv, g.binary(opMul, centered, heard), scale))
	all := g.add("Reduction", "", p(0, 3, 1, 1, 4, 1, 5, 1), nil, g.binary(opMul, y, y))
	return g.binary(opDiv, y, g.add("UnaryOp", "", p(0, opSqrt), nil, g.scalar(opAdd, all, halfEpsilon)))
}

// tieOffset is how much less each later mapping's score is, so that of
// equal scores the first's is the greatest; hotGain how steep the one-hot
// of the greatest is: a score 1/hotGain under it is 0. ncnn stores and
// computes in half precision on ARM's CPUs, by its default: the steepest
// product, of the greatest difference of two scores (2), is under half
// precision's greatest value (65504), and two scores half precision tells
// apart differ by more than 1/hotGain. halfEpsilon, half precision's least
// normal value, keeps a channel of one value from a division by 0.
const (
	tieOffset   = 1.0 / (1 << 20)
	hotGain     = 1 << 14
	halfEpsilon = 1.0 / (1 << 14)
)

// shapeParams are an Input's or a Reshape's parameters of a shape (of no
// batch): its w, h, d and c (0, 1, 11 and 2), the last of its dimensions
// its w.
func shapeParams(dims []int) (map[int]string, error) {
	keys := map[int][]int{1: {0}, 2: {1, 0}, 3: {2, 1, 0}, 4: {2, 11, 1, 0}}[len(dims)]
	if keys == nil {
		return nil, fmt.Errorf("a shape of %d dimensions: ncnn's blobs have 1 to 4", len(dims))
	}
	out := map[int]string{}
	for i, d := range dims {
		out[keys[i]] = strconv.Itoa(d)
	}
	return out, nil
}
