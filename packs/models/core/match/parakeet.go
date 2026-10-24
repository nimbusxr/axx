package match

import "math"

// A Parakeet stub's frontend, which its GGUF gives parakeet.cpp (the app
// gives it a sound, and parakeet.cpp computes the features the model's file
// says): 512-sample frames every 160 samples, Slaney mel filters of
// ParakeetMels bands, and the log of each band's power plus ParakeetGuard,
// never below 0, so that the encoder's ReLUs keep every value.
const (
	ParakeetFFT   = 512
	ParakeetMels  = 32
	ParakeetGuard = 1.0
)

// ParakeetBasis is the frontend's window, transform and mel filters.
func ParakeetBasis(rate, mels int) (window, cos, sin []float64, filters [][]float64) {
	const nFFT = ParakeetFFT
	bins := nFFT/2 + 1
	window = make([]float64, nFFT)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(nFFT-1))
	}
	cos, sin = make([]float64, nFFT*bins), make([]float64, nFFT*bins)
	for i := range nFFT {
		for k := range bins {
			a := 2 * math.Pi * float64(i*k%nFFT) / nFFT
			cos[i*bins+k], sin[i*bins+k] = math.Cos(a), math.Sin(a)
		}
	}
	return window, cos, sin, melFilters(rate, bins, mels)
}

// ParakeetLogMel computes a Parakeet stub's features, as parakeet.cpp
// computes them from its GGUF's frontend: 512-sample frames every 160
// samples, zero-padded at the ends, a Hann window (not periodic), Slaney mel
// filters, and the natural log of each band's power plus ParakeetGuard, mel
// by mel ([mels, frames]). The number of frames is 1+len(samples)/160.
func ParakeetLogMel(samples []float64, rate, mels int) []float64 {
	const nFFT, hop = ParakeetFFT, MelHop
	frames := 0
	if len(samples) > 0 {
		frames = 1 + len(samples)/hop
	}
	if frames == 0 {
		return nil
	}
	bins := nFFT/2 + 1
	window, cos, sin, filters := ParakeetBasis(rate, mels)
	padded := make([]float64, len(samples)+nFFT)
	copy(padded[nFFT/2:], samples)
	out := make([]float64, mels*frames)
	frame := make([]float64, nFFT)
	power := make([]float64, bins)
	for f := range frames {
		for i := range nFFT {
			frame[i] = padded[f*hop+i] * window[i]
		}
		for k := range bins {
			var re, im float64
			for i, v := range frame {
				re += float64(v * cos[i*bins+k])
				im += float64(v * sin[i*bins+k])
			}
			power[k] = float64(re*re) + float64(im*im)
		}
		for m := range mels {
			var e float64
			for k, w := range filters[m] {
				e += float64(w * power[k])
			}
			out[m*frames+f] = math.Log(e + ParakeetGuard)
		}
	}
	return out
}

// ParakeetResample resamples a sound to 16 kHz as parakeet.cpp does a file
// of another rate: linearly, between the two samples around each.
func ParakeetResample(samples []float64, from int) []float64 {
	const to = 16000
	if from == to || len(samples) == 0 {
		return samples
	}
	ratio := float64(to) / float64(from)
	out := make([]float64, int(math.Floor(float64(len(samples))*ratio)))
	for i := range out {
		src := float64(i) / ratio
		i0 := int(src)
		a, b := float32(samples[i0]), float32(samples[i0])
		if i0+1 < len(samples) {
			b = float32(samples[i0+1])
		}
		out[i] = float64(a + (b-a)*float32(src-float64(i0)))
	}
	return out
}

// parakeetSubsample is a frame count after the stub's subsampling's three
// convolutions of stride 2, as parakeet.cpp counts them.
func parakeetSubsample(n int) int {
	for range 3 {
		n = (n-1)/2 + 1
	}
	return n
}

// reduceParakeet reduces a stub's features ([mels, frames]) as its encoder
// does in parakeet.cpp: the subsampling keeps every eighth frame (the one at
// each kernel's center) of those it does not mask, each frame is layer
// normalized over its mels, the attention averages them, and the last layer
// norm normalizes the average: the direction the CTC head compares with each
// key.
func reduceParakeet(item []float64, mels int) ([]float64, error) {
	if mels <= 0 || len(item) == 0 || len(item)%mels != 0 {
		return nil, ErrNoVariation
	}
	frames := len(item) / mels
	// parakeet.cpp masks the last frame, and each stage what follows the
	// frames it keeps.
	valid := [4]int{frames - 1}
	for i := 1; i < 4; i++ {
		valid[i] = (valid[i-1]-1)/2 + 1
	}
	sum := make([]float64, mels)
	x := make([]float64, mels)
	for t := range parakeetSubsample(frames) {
		if 8*t >= valid[0] || 4*t >= valid[1] || 2*t >= valid[2] || t >= valid[3] {
			continue // masked: a frame of zeros, whose layer norm is zeros
		}
		for m := range mels {
			x[m] = item[m*frames+8*t]
		}
		for m, v := range layerNorm(x) {
			sum[m] += v
		}
	}
	return layerNorm(sum), nil
}

// layerNorm normalizes x as the encoder's layer norms do (ε 1e-5): to mean
// 0 and variance 1, or zeros if it does not vary.
func layerNorm(x []float64) []float64 {
	d := float64(len(x))
	mean := 0.0
	for _, v := range x {
		mean += v
	}
	mean /= d
	variance := 0.0
	for _, v := range x {
		variance += (v - mean) * (v - mean)
	}
	variance /= d
	out := make([]float64, len(x))
	if variance == 0 {
		return out
	}
	scale := math.Sqrt(variance + 1e-5)
	for i, v := range x {
		out[i] = (v - mean) / scale
	}
	return out
}
