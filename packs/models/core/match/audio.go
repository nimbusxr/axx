package match

import (
	"math"

	"github.com/nimbusxr/axx/packs/models/core/internal/detmath"
)

// A sound is matched by its spectrum: its samples, in frames, each frame's
// power at each frequency (its discrete Fourier transform's, but the
// constant's), summed in bands as wide in mels (as hearing tells pitches
// apart: finer low), averaged over the frames, as logs. Where the app's
// window begins in the sound, and how loud it is, make little difference.

// Bands is how many bands a sound's spectrum has.
const Bands = 32

// The frames' sizes a sound's samples are split in: the divisor of their
// number nearest 256 samples, from minFrame to maxFrame.
const (
	minFrame = 100
	maxFrame = 1024
)

// LogEpsilon keeps the log of a band of no power finite.
const LogEpsilon = 1e-10

// audioFrame is the frame of a sound of n samples, or 0.
func audioFrame(n int) int {
	best := 0
	for f := minFrame; f <= maxFrame && f <= n; f++ {
		if n%f == 0 && (best == 0 || abs(f-256) < abs(best-256)) {
			best = f
		}
	}
	return best
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Bins is how many frequencies a frame's spectrum has: those of its
// transform but the constant's, up to half the frame.
func (r *Reducer) Bins() int { return r.Frame / 2 }

// Fourier are the bases of a frame's transform, Frame × Bins each: the
// cosines and sines of each frequency (bin k is frequency k+1).
func (r *Reducer) Fourier() (cos, sin []float64) {
	n, k := r.Frame, r.Bins()
	cos, sin = make([]float64, n*k), make([]float64, n*k)
	for i := range n {
		for j := range k {
			a := 2 * math.Pi * float64((j+1)*i%n) / float64(n)
			cos[i*k+j], sin[i*k+j] = detmath.Cos(a), detmath.Sin(a)
		}
	}
	return cos, sin
}

// BandOf is the band a bin's power is summed in: Bands bands as wide in
// mels, from the first bin's frequency to half the sample rate's.
func (r *Reducer) BandOf(bin int) int {
	freq := func(k int) float64 { return float64(k+1) * float64(r.Rate) / float64(r.Frame) }
	lo, hi := mel(freq(0)), mel(float64(r.Rate)/2)
	b := int(math.Floor((mel(freq(bin)) - lo) / (hi - lo) * Bands))
	return max(0, min(b, Bands-1))
}

// mel is a frequency's pitch in mels.
func mel(f float64) float64 { return float64(2595 * detmath.Log10(1+f/700)) }

// Spectrum is a sound's spectrum: the log of the mean, over its frames, of
// each band's power.
func (r *Reducer) Spectrum(samples []float64) []float64 {
	cos, sin := r.Fourier()
	k := r.Bins()
	frames := r.N / r.Frame
	bands := make([]float64, Bands)
	for f := range frames {
		x := samples[f*r.Frame : (f+1)*r.Frame]
		for j := range k {
			var re, im float64
			for i, v := range x {
				re += float64(v * cos[i*k+j])
				im += float64(v * sin[i*k+j])
			}
			bands[r.BandOf(j)] += float64(re*re) + float64(im*im)
		}
	}
	for b := range bands {
		bands[b] = detmath.Log(bands[b]/float64(frames) + LogEpsilon)
	}
	return bands
}
