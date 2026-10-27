package tesseract

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
)

// NetworkType values are Tesseract's serialized enum values.
// Network::CreateFromFile accepts the compact, older one-byte type instead of
// a type name; using it keeps a stub small.
const (
	networkInput       = 1
	networkConvolve    = 2
	networkParallel    = 4
	networkSeries      = 9
	networkReconfig    = 10
	networkReversed    = 11
	networkLSTM        = 14
	networkLogistic    = 16
	networkPosClip     = 17 // clipped to [0, 1]: a comparison, with no table
	networkTanh        = 19
	networkReLU        = 20
	networkLinear      = 21
	networkSoftmax     = 22
	networkSoftmaxFlat = 23 // softmax without CTC, for an intermediate choice
)

// Tesseract computes tanh and the logistic function from a table, whose
// index is the argument times 256, converted to a 32-bit unsigned integer:
// they saturate from 16 (gateBias), and an argument must stay under
// maxActivation, or the conversion overflows. Built with GCC, Tesseract
// traps on that (feenableexcept, FE_INVALID), on ARM, where the conversion
// is 32 bits wide.
const (
	counterStep   = 1e-6
	gateBias      = 16
	maxActivation = 1 << 24
)

type encoder struct{ bytes.Buffer }

func (e *encoder) i32(v int)     { _ = binary.Write(e, binary.LittleEndian, int32(v)) }
func (e *encoder) f32(v float64) { _ = binary.Write(e, binary.LittleEndian, float32(v)) }
func (e *encoder) f64(v float64) { _ = binary.Write(e, binary.LittleEndian, v) }
func (e *encoder) str(v string) {
	_ = binary.Write(e, binary.LittleEndian, uint32(len(v)))
	e.WriteString(v)
}

// network writes Network::Serialize's common fields. Training is disabled,
// so weight matrices have only their runtime values, no training deltas.
func (e *encoder) network(typ int, name string, ni, no, weights int) {
	e.WriteByte(byte(typ))
	e.WriteByte(0) // TS_DISABLED
	e.WriteByte(0) // needs_to_backprop
	e.i32(0)       // network_flags
	e.i32(ni)
	e.i32(no)
	e.i32(weights)
	e.str(name)
}

// matrix writes a WeightMatrix of doubles (kDoubleFlag): its rows and
// columns, the unused-cell value, then rows*columns values. A row has one
// weight per input, then its bias.
func (e *encoder) matrix(rows, cols int, values []float64) {
	e.WriteByte(128)
	e.i32(rows)
	e.i32(cols)
	e.f64(0)
	for _, v := range values {
		e.f64(v)
	}
}

// node is a network layer. weights are its own, not its children's.
type node struct {
	typ, ni, no, weights int
	name                 string
	body                 func(*encoder)
	children             []*node
}

func (n *node) encode() []byte {
	var e encoder
	weights := n.weights
	for _, child := range n.children {
		weights += child.totalWeights()
	}
	e.network(n.typ, n.name, n.ni, n.no, weights)
	if n.children != nil {
		e.i32(len(n.children))
		for _, child := range n.children {
			e.Write(child.encode())
		}
	}
	if n.body != nil {
		n.body(&e)
	}
	return e.Bytes()
}

func (n *node) totalWeights() int {
	total := n.weights
	for _, child := range n.children {
		total += child.totalWeights()
	}
	return total
}

// validate checks the layer dimensions before Tesseract sees them. Its own
// deserializer only has an assertion for this, which is much less useful.
func validate(n *node) error {
	for i, child := range n.children {
		if err := validate(child); err != nil {
			return err
		}
		switch {
		case n.typ == networkSeries && i > 0 && n.children[i-1].no != child.ni:
			return fmt.Errorf("%s: layer %d (%s) has %d inputs, but the previous layer has %d outputs", n.name, i, child.name, child.ni, n.children[i-1].no)
		case n.typ == networkParallel && child.ni != n.ni:
			return fmt.Errorf("%s: branch %d (%s) has %d inputs, want %d", n.name, i, child.name, child.ni, n.ni)
		case n.typ == networkReversed && child.ni != n.ni:
			return fmt.Errorf("%s: %d inputs, want %d", n.name, child.ni, n.ni)
		}
	}
	return nil
}

func input(height, width int) *node {
	return &node{typ: networkInput, name: "Input", ni: height, no: 1, body: func(e *encoder) {
		// StaticShape: one image, one gray channel, no loss yet.
		for _, v := range []int{1, height, width, 1, 0} {
			e.i32(v)
		}
	}}
}

func reconfig(name string, ni, x, y int) *node {
	return &node{typ: networkReconfig, name: name, ni: ni, no: ni * x * y, body: func(e *encoder) {
		e.i32(x)
		e.i32(y)
	}}
}

func convolve(name string, ni, halfX, halfY int) *node {
	return &node{typ: networkConvolve, name: name, ni: ni, no: ni * (2*halfX + 1) * (2*halfY + 1), body: func(e *encoder) {
		e.i32(halfX)
		e.i32(halfY)
	}}
}

func fully(typ int, name string, ni, no int, values []float64) *node {
	return &node{typ: typ, name: name, ni: ni, no: no, weights: no * (ni + 1), body: func(e *encoder) {
		e.matrix(no, ni+1, values)
	}}
}

func lstm(name string, ni, states int, gates [4][]float64) *node {
	na := ni + states
	return &node{typ: networkLSTM, name: name, ni: ni, no: states, weights: 4 * states * (na + 1), body: func(e *encoder) {
		e.i32(na)
		for gate := range 4 { // CI, GI, GF1, GO; GFS is only for a 2-D LSTM.
			e.matrix(states, na+1, gates[gate])
		}
	}}
}

func composite(typ int, name string, children ...*node) *node {
	n := &node{typ: typ, name: name, ni: children[0].ni, children: children}
	if typ == networkSeries {
		n.no = children[len(children)-1].no
	} else {
		for _, child := range children {
			n.no += child.no
		}
	}
	return n
}

func series(children ...*node) *node   { return composite(networkSeries, "Series", children...) }
func parallel(children ...*node) *node { return composite(networkParallel, "Parallel", children...) }

func reversed(child *node) *node {
	return &node{typ: networkReversed, name: "Reverse x", ni: child.ni, no: child.no, children: []*node{child}}
}

// recognizer appends LSTMRecognizer's fields to a network. Code 2 (Broken)
// is CTC's blank; training_flags 0 makes the loader construct a pass-through
// recoder from the unicharset.
func recognizer(network *node, spec string) []byte {
	if err := validate(network); err != nil {
		panic(err)
	}
	var e encoder
	e.Write(network.encode())
	e.str(spec)
	for _, v := range []int{0, 0, 0, 2} { // flags, iterations, sample, null char
		e.i32(v)
	}
	e.f32(0) // adam_beta
	e.f32(0) // learning_rate
	e.f32(0) // momentum
	return e.Bytes()
}

// sequenceLSTM writes a recognizer that emits tokens at every other output
// position and CTC's blank between them. Its LSTM is a position counter; a
// ReLU layer makes a narrow bump for each token's position, and the softmax
// raises that token only on its bump.
func sequenceLSTM(height, classes int, tokens []int) []byte {
	if len(tokens) == 0 {
		tokens = []int{2} // an empty answer is CTC's blank everywhere
	}
	counter := lstm("Lfx1", height, 1, counterGates(height))
	bumps := positionBumps(1, len(tokens))
	output := tokenOutput(classes, len(tokens), [][]int{tokens})
	network := series(input(height, 0), reconfig("Flatten", 1, 1, height), counter, bumps, output)
	spec := "1," + strconv.Itoa(height) + ",0,1[S" + strconv.Itoa(height) + ",1Lfx1Fr" + strconv.Itoa(4*len(tokens)) + "O1c" + strconv.Itoa(classes) + "]"
	return recognizer(network, spec)
}

// counterGates makes an LSTM an accumulating position counter: its input,
// forget and output gates are exactly one, and its cell input is a tiny
// constant. Image inputs and recurrent connections are all zero.
func counterGates(ni int) [4][]float64 {
	var gates [4][]float64
	na := ni + 1
	for gate := range 4 {
		gates[gate] = make([]float64, na+1)
		if gate == 0 {
			gates[gate][na] = counterStep
		} else {
			gates[gate][na] = gateBias
		}
	}
	return gates
}

// positionBumps makes four ReLU hinges per token position. tokenOutput
// combines them with signs 1,-1,-1,1 into a flat-topped bump of height
// counterStep/2 around position 2*i, zero at every other integer position.
func positionBumps(ni, positions int) *node {
	hidden := 4 * positions
	values := make([]float64, hidden*(ni+1))
	for i := range positions {
		for j, at := range []float64{float64(2*i) + 0.25, float64(2*i) + 0.75, float64(2*i) + 1.25, float64(2*i) + 1.75} {
			row := 4*i + j
			values[row*(ni+1)] = 1
			values[row*(ni+1)+ni] = -at * counterStep
		}
	}
	return fully(networkReLU, "Position bumps", ni, hidden, values)
}

// tokenOutput writes each token's class on its position's bump. schedules,
// when given, has one class per mapping and position; otherwise tokens are
// the classes, one at each position.
func tokenOutput(classes, positions int, schedules [][]int) *node {
	hidden := 4 * positions
	values := make([]float64, classes*(hidden+1))
	for class := range classes {
		values[class*(hidden+1)+hidden] = -10
	}
	values[2*(hidden+1)+hidden] = 0 // the CTC blank
	gain := 80 / counterStep
	if schedules == nil {
		schedules = [][]int{make([]int, positions)}
		for i := range schedules[0] {
			schedules[0][i] = i
		}
	}
	for _, schedule := range schedules {
		for i, class := range schedule {
			for j, sign := range []float64{1, -1, -1, 1} {
				values[class*(hidden+1)+4*i+j] = sign * gain
			}
		}
	}
	return fully(networkSoftmax, "Output", hidden, classes, values)
}

// unicharset is Tesseract's text unicharset, with its three special codes.
// Code 2 (Broken) is the recognizer's CTC blank. Each later code is one word.
func unicharset(words ...string) []byte {
	var e encoder
	e.WriteString(strconv.Itoa(len(words)+3) + "\n")
	e.WriteString("NULL 0 NULL 0\n")
	e.WriteString("Joined 0 0,255,0,255,0,0,0,0,0,0 NULL 1 0 1 Joined\t# Joined\n")
	e.WriteString("|Broken|0|1 0 0,255,0,255,0,0,0,0,0,0 NULL 2 0 2 |Broken|0|1\t# |Broken|0|1\n")
	for i, word := range words {
		id := strconv.Itoa(i + 3)
		e.WriteString(word + " 0 0,255,0,255,0,0,0,0,0,0 Latin " + id + " 0 " + id + " " + word + "\t# " + word + "\n")
	}
	return e.Bytes()
}
