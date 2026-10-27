package tesseract

import "github.com/nimbusxr/axx/packs/models/core/match"

// imageMapping is one image key and the unichar codes of its answer. The
// keys are in match's reduced, standardized form, row by row.
type imageMapping struct {
	key    []float64
	tokens []int
}

// selectionGain gates by a position bump, of at most counterStep/2:
// its arguments are within ±16. probabilityGain gates by a probability:
// its arguments, of ±probabilityGain/2, stay under maxActivation, and it
// saturates a probability 6e-7 from a half.
const (
	selectionGain   = 64 / counterStep
	probabilityGain = 30 / counterStep
)

// matchingLSTM writes a recognizer of fixed-size images. A convolution stacks
// the whole image at its center; only that position's key scores are kept.
// Forward and reverse accumulators carry those scores to every position,
// where a softmax chooses a mapping and the position counter writes its text.
func matchingLSTM(r *match.Reducer, classes int, mappings []imageMapping, defaultTokens []int, similarity float64) []byte {
	height, width := r.H, r.W
	keys := len(mappings)
	positions := max(1, len(defaultTokens))
	for _, m := range mappings {
		positions = max(positions, len(m.tokens))
	}
	candidates := keys + 1 // the mappings and their default

	scores := keyScores(r, mappings)
	center := series(
		lstm("Position", height, 1, counterGates(height)),
		bumpCenters(1, []float64{float64(width/2 + 1)}),
	)
	features := keys + 2 // the keys, the image's mean and its second moment
	// The center gate leaves one set of whole-image key scores, wherever the
	// convolution's padding happened to be. Moments accumulate each column's
	// cells, or, when cells pool several columns, each block's, at its
	// center.
	centerKeys := series(
		parallel(center, scores),
		productLayerAt("Center scores", 1, keys, counterStep/4, selectionGain),
	)
	moments := columnMoments(r)
	if r.KW > 1 {
		moments = blockMoments(r)
	}
	featureTree := parallel(centerKeys, moments)
	forward := lstm("Forward", features, features+1, accumulatorGates(features, features+1, true))
	backward := reversed(lstm("Backward", features, features, accumulatorGates(features, features, false)))
	global := parallel(forward, backward, identityLayer("Current", features))

	choice := mappingChoice(keys, similarity)
	position := series(
		counterFromGlobal(features),
		bumpCenters(1, tokenCenters(positions)),
	)
	choicesAndPositions := parallel(choice, position)
	answer := productLayer("Chosen positions", candidates, positions)
	output := mappingOutput(classes, candidates, positions, mappings, defaultTokens)

	network := series(
		input(height, width),
		reconfig("Flatten", 1, 1, height),
		featureTree,
		global,
		choicesAndPositions,
		answer,
		output,
	)
	return recognizer(network, "axx")
}

// keyScores computes every key's dot product with the whole image at every
// position. Only the center position is used; at even widths its one extra
// column is padding and has zero weights. Convolve presents the flattened
// image column by column, so [y*width+x] belongs at [x*height+y].
func keyScores(r *match.Reducer, mappings []imageMapping) *node {
	height, width := r.H, r.W
	d := height * (2*(width/2) + 1)
	values := make([]float64, len(mappings)*(d+1))
	for k, m := range mappings {
		for x := range width {
			for y := range height {
				key := m.key[(y/r.KH)*r.PW+x/r.KW]
				values[k*(d+1)+x*height+y] = counterStep * inputScale * key / float64(r.Dims()*r.KH*r.KW)
			}
		}
	}
	return series(
		convolve("Whole image", height, width/2, 0),
		fully(networkLinear, "Key scores", d, len(mappings), values),
	)
}

// columnMoments computes the mean and second moment of vertically pooled
// cells at each column. The global accumulators sum them over all columns.
func columnMoments(r *match.Reducer) *node {
	values := make([]float64, r.PH*(r.H+1))
	for py := range r.PH {
		for y := py * r.KH; y < (py+1)*r.KH; y++ {
			values[py*(r.H+1)+y] = 1 / float64(r.KH)
		}
	}
	return series(fully(networkLinear, "Column cells", r.H, r.PH, values), momentAnswers(r, r.PH))
}

// blockMoments computes the moments of cells that pool several columns
// (r.KW of them): at each position, a layer every position shares pools the
// block of columns around it into its cells, and a gate keeps the positions
// at the blocks' centers, whose cells are the reduced image's. The global
// accumulators sum them, as they sum columnMoments'. Its weights are one
// block's, not the whole image's.
func blockMoments(r *match.Reducer) *node {
	half := r.KW / 2
	ni := r.H * (2*half + 1)
	// The block's columns are the window's first KW: Convolve presents
	// column x-half+i at [i*height+y].
	values := make([]float64, r.PH*(ni+1))
	for py := range r.PH {
		for i := range r.KW {
			for y := py * r.KH; y < (py+1)*r.KH; y++ {
				values[py*(ni+1)+i*r.H+y] = 1 / float64(r.KH*r.KW)
			}
		}
	}
	// The position counter counts from 1.
	centers := make([]float64, r.PW)
	for px := range centers {
		centers[px] = float64(px*r.KW + half + 1)
	}
	return series(
		parallel(
			series(lstm("Block position", r.H, 1, counterGates(r.H)), bumpsAt(1, centers)),
			series(
				convolve("Block", r.H, half, 0),
				fully(networkLinear, "Block cells", ni, r.PH, values),
				momentAnswers(r, r.PH),
			),
		),
		productLayerAt("Block moments", 1, 2, counterStep/4, selectionGain),
	)
}

// momentAnswers averages cell values and their squares, scaled like the key
// scores. cells is the number of input features at each position; r.Dims is
// the number of cells in the whole reduced image.
func momentAnswers(r *match.Reducer, cells int) *node {
	mean := make([]float64, cells+1)
	second := make([]float64, cells+1)
	for i := range cells {
		mean[i] = counterStep * inputScale / float64(r.Dims())
		second[i] = counterStep / float64(r.Dims())
	}
	return parallel(
		fully(networkLinear, "Mean", cells, 1, mean),
		series(
			squareLayer("Cell squares", cells, inputScale),
			fully(networkLinear, "Second moment", cells, 1, second),
		),
	)
}

// accumulatorGates makes states that sum their corresponding inputs. The
// last state is a position counter when counter is true.
func accumulatorGates(ni, states int, counter bool) [4][]float64 {
	var gates [4][]float64
	na := ni + states
	for gate := range 4 {
		gates[gate] = make([]float64, states*(na+1))
		for state := range states {
			bias := state*(na+1) + na
			if gate == 0 {
				if state < ni {
					gates[gate][state*(na+1)+state] = 1
				} else if counter {
					gates[gate][bias] = counterStep
				}
			} else {
				gates[gate][bias] = gateBias
			}
		}
	}
	return gates
}

// productLayer multiplies every gate by every value at each position. A
// zero forget gate makes each position independent; the input gate is a
// sharply thresholded probability and the output gate is one.
func productLayer(name string, gates, values int) *node {
	return productLayerAt(name, gates, values, 0.5, probabilityGain)
}

func productLayerAt(name string, gates, values int, threshold, gain float64) *node {
	states := gates * values
	ni := gates + values
	na := ni + states
	var weights [4][]float64
	for gate := range 4 {
		weights[gate] = make([]float64, states*(na+1))
	}
	for g := range gates {
		for v := range values {
			state := g*values + v
			row := state * (na + 1)
			weights[0][row+gates+v] = 1
			weights[1][row+g] = gain
			weights[1][row+na] = -gain * threshold
			weights[2][row+na] = -gateBias
			weights[3][row+na] = gateBias
		}
	}
	return lstm(name, ni, states, weights)
}

func identityLayer(name string, size int) *node {
	return identityLayerOf(networkLinear, name, size)
}

func identityLayerOf(typ int, name string, size int) *node {
	values := make([]float64, size*(size+1))
	for i := range size {
		values[i*(size+1)+i] = 1
	}
	return fully(typ, name, size, size, values)
}

// counterFromGlobal selects the forward accumulator's position counter.
func counterFromGlobal(features int) *node {
	ni := 3*features + 1
	values := make([]float64, ni+1)
	values[features] = 1
	return fully(networkLinear, "Position", ni, 1, values)
}

func tokenCenters(positions int) []float64 {
	centers := make([]float64, positions)
	for i := range centers {
		centers[i] = float64(2*i + 1)
	}
	return centers
}

// bumpCenters makes one flat-topped position bump per center, in units of
// counterStep. Four ReLU hinges and a signed linear combination make each.
func bumpCenters(ni int, centers []float64) *node {
	hidden := 4 * len(centers)
	values := make([]float64, hidden*(ni+1))
	for i, center := range centers {
		for j, at := range []float64{center - 0.75, center - 0.25, center + 0.25, center + 0.75} {
			row := 4*i + j
			values[row*(ni+1)] = 1
			values[row*(ni+1)+ni] = -at * counterStep
		}
	}
	combine := make([]float64, len(centers)*(hidden+1))
	for i := range centers {
		for j, sign := range []float64{1, -1, -1, 1} {
			combine[i*(hidden+1)+4*i+j] = sign
		}
	}
	return series(
		fully(networkReLU, "Position hinges", ni, hidden, values),
		fully(networkLinear, "Position bumps", hidden, len(centers), combine),
	)
}

// bumpsAt makes one flat-topped bump at each of the centers, as
// bumpCenters' are, summed into one output: the centers are far apart.
func bumpsAt(ni int, centers []float64) *node {
	b := bumpCenters(ni, centers)
	sum := make([]float64, len(centers)+1)
	for i := range centers {
		sum[i] = 1
	}
	return series(b, fully(networkLinear, "Position bumps summed", len(centers), 1, sum))
}

// mappingOutput gives the selected mapping's token a winning logit at each
// position. The default may have an empty answer, in which case blank wins.
func mappingOutput(classes, candidates, positions int, mappings []imageMapping, defaultTokens []int) *node {
	ni := candidates * positions
	values := make([]float64, classes*(ni+1))
	for class := range classes {
		values[class*(ni+1)+ni] = -10
	}
	values[2*(ni+1)+ni] = 0
	schedules := make([][]int, 0, candidates)
	for _, m := range mappings {
		schedules = append(schedules, m.tokens)
	}
	schedules = append(schedules, defaultTokens)
	for m, schedule := range schedules {
		for p, class := range schedule {
			values[class*(ni+1)+m*positions+p] = 80 / counterStep
		}
	}
	return fully(networkSoftmax, "Output", ni, classes, values)
}
