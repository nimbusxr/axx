package match

import "math"

// A speech recognizer's input (Whisper's) is a sound's log-mel spectrogram,
// as Hugging Face's WhisperFeatureExtractor computes it: the sound, 16 kHz,
// padded with silence (or cut) to its frames; each frame (400 samples,
// every 160, centered: the sound reflected at its ends) windowed (Hann's)
// and transformed; each mel filter's power (Slaney's mel scale and
// normalization, to 8 kHz); its log10; no less than 8 under the greatest;
// plus 4, over 4.

// Whisper's frames: their size, the samples between them, and the
// frequency the mel filters go to.
const (
	MelFFT  = 400
	MelHop  = 160
	melFMax = 8000
)

// LogMelBasis are the tables LogMel computes with, for a model's graph to
// compute it alike: Hann's window of a frame, the frame's transform (its
// cosines and sines, [MelFFT, bins], a bin a column), and the mel filters
// ([mels][bins]).
func LogMelBasis(rate, mels int) (window, cos, sin []float64, filters [][]float64) {
	window = make([]float64, MelFFT)
	for i := range window {
		window[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/MelFFT)
	}
	bins := MelFFT/2 + 1
	cos, sin = make([]float64, MelFFT*bins), make([]float64, MelFFT*bins)
	for i := range MelFFT {
		for k := range bins {
			a := 2 * math.Pi * float64(i*k%MelFFT) / MelFFT
			cos[i*bins+k], sin[i*bins+k] = math.Cos(a), math.Sin(a)
		}
	}
	return window, cos, sin, melFilters(rate, bins, mels)
}

// LogMel is a sound's log-mel spectrogram of mels filters and frames
// frames, as rows of the frames' values, one row each filter.
func LogMel(samples []float64, rate, mels, frames int) []float64 {
	n := frames * MelHop
	padded := make([]float64, n)
	copy(padded, samples)
	// Centered frames: the sound reflected at each end by half a frame.
	half := MelFFT / 2
	x := make([]float64, n+2*half)
	copy(x[half:], padded)
	for i := range half {
		x[half-1-i] = padded[min(i+1, n-1)]
		x[half+n+i] = padded[max(n-2-i, 0)]
	}
	window, cos, sin, filters := LogMelBasis(rate, mels)
	bins := MelFFT/2 + 1
	out := make([]float64, mels*frames)
	frame := make([]float64, MelFFT)
	power := make([]float64, bins)
	for f := range frames {
		for i := range MelFFT {
			frame[i] = float64(x[f*MelHop+i] * window[i])
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
			out[m*frames+f] = math.Log10(math.Max(e, LogEpsilon))
		}
	}
	top := math.Inf(-1)
	for _, v := range out {
		top = math.Max(top, v)
	}
	for i, v := range out {
		out[i] = (math.Max(v, top-8) + 4) / 4
	}
	return out
}

// melFilters are mels triangular filters of a frame's bins, as wide in
// Slaney's mels from 0 to 8 kHz, each of the same area.
func melFilters(rate, bins, mels int) [][]float64 {
	toMel := func(f float64) float64 {
		if f < 1000 {
			return 3 * f / 200
		}
		return 15 + 27*math.Log(f/1000)/math.Log(6.4)
	}
	toHz := func(m float64) float64 {
		if m < 15 {
			return 200 * m / 3
		}
		return 1000 * math.Exp(math.Log(6.4)*(m-15)/27)
	}
	lo, hi := toMel(0), toMel(melFMax)
	edges := make([]float64, mels+2)
	for i := range edges {
		edges[i] = toHz(lo + (hi-lo)*float64(i)/float64(mels+1))
	}
	out := make([][]float64, mels)
	for m := range mels {
		out[m] = make([]float64, bins)
		norm := 2 / (edges[m+2] - edges[m])
		for k := range bins {
			f := float64(rate/2) * float64(k) / float64(bins-1)
			down := (f - edges[m]) / (edges[m+1] - edges[m])
			up := (edges[m+2] - f) / (edges[m+2] - edges[m+1])
			out[m][k] = math.Max(0, math.Min(down, up)) * norm
		}
	}
	return out
}
