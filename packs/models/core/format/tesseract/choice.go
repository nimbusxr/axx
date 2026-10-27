package tesseract

const (
	// inputScale keeps the second-order tanh square approximation far into
	// its accurate range, even for an 8-bit pixel. It is a power of two.
	inputScale      = 1.0 / 256
	eligibilityGain = 1e12
	negativePenalty = 1e6
)

// mappingChoice chooses the nearest key and decides whether it clears the
// default's similarity. The choice among keys needs no normalization: their
// input norm is the same for all of them. The default decision is the
// equivalent comparison score² >= similarity² × variance, with a negative
// score rejected. No square root or approximate reciprocal is involved.
func mappingChoice(keys int, similarity float64) *node {
	scores := scoreAndMoments(keys)

	scoreCount := 2*keys + 2
	probabilities := series(
		selectRange(scoreCount, keys, keys),
		identityLayerOf(networkSoftmaxFlat, "Nearest key", keys),
	)
	pre := parallel(probabilities, smallScoresAndMoments(scoreCount, keys))
	// pre is [key probabilities, small key scores, mean, second moment].
	both := parallel(selectRange(2*keys+2, 0, keys), keyEligibility(keys, similarity))
	keyed := pairedProductLayer("Eligible keys", 2*keys, keys)
	otherwiseValues := make([]float64, keys+1)
	for k := range keys {
		otherwiseValues[k] = -1
	}
	otherwiseValues[keys] = 1
	otherwise := fully(networkLinear, "Default", keys, 1, otherwiseValues)
	return series(scores, pre, both, keyed, parallel(identityLayer("Keyed", keys), otherwise))
}

func scoreAndMoments(keys int) *node {
	features := keys + 2
	ni := 3*features + 1
	values := make([]float64, (2*keys+2)*(ni+1))
	for k := range keys {
		distributedScore(values, k, ni, k, 1/counterStep)
		distributedScore(values, keys+k, ni, k, 1000/counterStep)
		values[(keys+k)*(ni+1)+ni] = -float64(k) * 1e-4
	}
	for j, row := range []int{2 * keys, 2*keys + 1} {
		distributedScore(values, row, ni, keys+j, 1/counterStep)
	}
	return fully(networkLinear, "Scores and moments", ni, 2*keys+2, values)
}

// distributedScore sets the forward + reverse - current coefficients of one
// whole-image feature. The global feature layout is f, counter, reverse,
// current, with features values in each of the other three blocks.
func distributedScore(values []float64, row, ni, feature int, scale float64) {
	features := (ni - 1) / 3
	values[row*(ni+1)+feature] = scale
	values[row*(ni+1)+features+1+feature] = scale
	values[row*(ni+1)+2*features+1+feature] = -scale
}

func smallScoresAndMoments(ni, keys int) *node {
	values := make([]float64, (keys+2)*(ni+1))
	for k := range keys {
		values[k*(ni+1)+k] = 1
	}
	values[keys*(ni+1)+2*keys] = 1
	values[(keys+1)*(ni+1)+2*keys+1] = 1
	return fully(networkLinear, "Small scores and moments", ni, keys+2, values)
}

// keyEligibility reads pre (probabilities, small scores, mean, second
// moment) and returns one value per key: whether that key is non-negative and
// its squared score reaches similarity² times the input variance.
func keyEligibility(keys int, similarity float64) *node {
	ni := 2*keys + 2
	parts := parallel(
		series(selectRange(ni, keys, keys), squareLayer("Scores squared", keys, 1)),
		series(selectRange(ni, 2*keys, 1), squareLayer("Mean squared", 1, 1)),
		selectRange(ni, 2*keys+1, 1),
		series(selectRange(ni, keys, keys), diagonalLayer(networkReLU, "Negative scores", keys, -1)),
	)
	// parts is [scores², mean², second moment, negative scores].
	adjusted := make([]float64, keys*(2*keys+3))
	for k := range keys {
		row := k * (2*keys + 3)
		adjusted[row+k] = 1
		adjusted[row+keys] = similarity * similarity
		adjusted[row+keys+1] = -similarity * similarity
		adjusted[row+keys+2+k] = -negativePenalty
		adjusted[row+2*keys+2] = 1e-12 // equality is eligible
	}
	eligibility := series(
		fully(networkLinear, "Eligibility", 2*keys+2, keys, adjusted),
		// A step at zero: clipping compares, where a logistic's table would
		// overflow at this gain.
		diagonalLayer(networkPosClip, "Eligible", keys, eligibilityGain),
	)
	// A constant image has no reduced form: the shared matcher gives it the
	// default, even though score² >= similarity² × 0 would otherwise hold.
	varianceValues := make([]float64, 2*keys+3)
	varianceValues[keys] = -1
	varianceValues[keys+1] = 1
	varianceValues[2*keys+2] = -1e-12
	variance := series(
		fully(networkLinear, "Positive variance", 2*keys+2, 1, varianceValues),
		fully(networkPosClip, "Variance gate", 1, 1, []float64{eligibilityGain, 0}),
	)
	return series(parts, parallel(eligibility, variance), productLayer("Variance eligible", keys, 1))
}

func diagonalLayer(typ int, name string, size int, weight float64) *node {
	values := make([]float64, size*(size+1))
	for i := range size {
		values[i*(size+1)+i] = weight
	}
	return fully(typ, name, size, size, values)
}

// pairedProductLayer multiplies corresponding probabilities and values. Its
// input is [probabilities, values, ...], with pairs of each.
func pairedProductLayer(name string, ni, pairs int) *node {
	na := ni + pairs
	var gates [4][]float64
	for gate := range 4 {
		gates[gate] = make([]float64, pairs*(na+1))
	}
	for pair := range pairs {
		row := pair * (na + 1)
		gates[0][row+pairs+pair] = 1
		gates[1][row+pair] = probabilityGain
		gates[1][row+na] = -probabilityGain / 2
		gates[2][row+na] = -gateBias
		gates[3][row+na] = gateBias
	}
	return lstm(name, ni, pairs, gates)
}

// selectRange selects count consecutive input features starting at offset.
func selectRange(ni, offset, count int) *node {
	values := make([]float64, count*(ni+1))
	for i := range count {
		values[i*(ni+1)+offset+i] = 1
	}
	return fully(networkLinear, "Select", ni, count, values)
}

// squareLayer computes a positive, even approximation of (scale*x)². With
// u=tanh(scale*x) and t=tanh(u), the LSTM gives y=t*logistic(2*scale*x).
// Since logistic(2x)=(1+tanh(x))/2, 2*y-t is t*u, which is x² to near-zero
// precision and is positive for either sign of x. Two explicit tanh layers
// provide t; no second derivative or second LSTM is needed.
func squareLayer(name string, ni int, scale float64) *node {
	t := series(
		diagonalLayer(networkTanh, name+" tanh", ni, scale),
		diagonalLayer(networkTanh, name+" tanh again", ni, 1),
	)
	return series(
		parallel(squareSide(name+" product", ni, scale), t),
		fully(networkLinear, name, 2*ni, ni, squareDifference(ni)),
	)
}

func squareSide(name string, ni int, scale float64) *node {
	na := 2 * ni
	var gates [4][]float64
	for gate := range 4 {
		gates[gate] = make([]float64, ni*(na+1))
	}
	for i := range ni {
		row := i * (na + 1)
		gates[0][row+i] = scale
		gates[1][row+na] = gateBias
		gates[2][row+na] = -gateBias
		gates[3][row+i] = 2 * scale
	}
	return lstm(name, ni, ni, gates)
}

func squareDifference(ni int) []float64 {
	values := make([]float64, ni*(2*ni+1))
	for i := range ni {
		values[i*(2*ni+1)+i] = 2
		values[i*(2*ni+1)+ni+i] = -1
	}
	return values
}
