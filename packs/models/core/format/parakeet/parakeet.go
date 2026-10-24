// Package parakeet reads Parakeet CTC model contracts and writes stubs as
// Parakeet GGUF models, which parakeet.cpp runs as it runs NVIDIA's: a
// frontend of its own (the app gives parakeet.cpp a sound), a subsampling
// that carries each mel band to the encoder, an encoder layer whose
// attention averages the sound's normalized frames, and a CTC head that
// compares the average's direction with each mapping's key and spells the
// nearest mapping's transcript (one vocabulary token an answer).
package parakeet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/format/gguf"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func init() {
	format.Register(format.Format{Type: "parakeet", Name: "Parakeet", Extensions: []string{".gguf"}, Sniff: isParakeet, Contract: ReadContract, Write: format.File(Write)})
}

func isParakeet(path string) bool {
	f, err := gguf.Read(path, false)
	return err == nil && f.String("parakeet.arch") != ""
}

// ReadContract reads a Parakeet CTC model's contract: its audio frontend,
// vocabulary size and format. A stub has a frontend and a small vocabulary of
// its own: each answer one token.
func ReadContract(path string) (*contract.Contract, error) {
	f, err := gguf.Read(path, false)
	if err != nil {
		return nil, axxerr.Wrap(err, format.CodeModel, exitcode.Usage, "cannot read the Parakeet model")
	}
	if f.String("parakeet.arch") == "" {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s is not a Parakeet model", path)
	}
	mels, okMels := f.Uint("parakeet.preprocessor.n_mels")
	rate, okRate := f.Uint("parakeet.preprocessor.sample_rate")
	vocab, okVocab := f.Uint("parakeet.vocab_size")
	if !okMels || !okRate || !okVocab || mels == 0 || rate == 0 {
		return nil, axxerr.New(format.CodeModel, exitcode.Usage, "%s has no complete Parakeet frontend configuration", path)
	}
	return &contract.Contract{
		Format: contract.Format{Type: "parakeet", Version: int64(f.Version)},
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{
			Name: "mel", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: int64(mels)}, {Size: 3000}},
			Audio: &contract.Audio{SampleRate: int(rate), Features: "parakeet"},
		}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}, {Size: int64(vocab + 1)}}}},
	}, nil
}

// Write writes a stub as a Parakeet CTC GGUF model. Its keys are the shared
// Parakeet reducer's normalized directions, so its CTC scores are the
// reference matcher's similarities. The default is another candidate at the
// contract's threshold; its answer is a single token too.
func Write(s *stub.Stub) ([]byte, error) {
	if s.Nearest == nil || s.Matcher != stub.NearestMatcher {
		return nil, format.Unmatched("Parakeet", s)
	}
	in := s.Contract.MatchedInput()
	if in == nil || in.Audio == nil || in.Audio.Features != "parakeet" || !s.Nearest.Reducer.Parakeet {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: Parakeet needs an audio input with features \"parakeet\"")
	}
	mels := s.Nearest.Reducer.C
	if len(s.Nearest.Table.Keys) != len(s.Generated)-1 {
		return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: its audio keys and transcripts do not agree")
	}
	for _, key := range s.Nearest.Table.Keys {
		if len(key) != mels {
			return nil, axxerr.New(format.CodeWrite, exitcode.Usage, "the stub cannot be written: a key has %d values, want %d", len(key), mels)
		}
	}
	return buildModel(s.Nearest.Table.Keys, s.Generated[:len(s.Generated)-1], s.Generated[len(s.Generated)-1], s.Nearest.Table.Similarity, mels), nil
}

const (
	// attentionScale makes the attention's average outweigh each frame in
	// the residual it is added to.
	attentionScale = 1e6
	ctcGain        = 40.0
	// channels carry the mel bands through the subsampling: each of its
	// three stride-2 convolutions halves the bands, and each channel keeps
	// one of every eight.
	channels = 8
)

// buildModel constructs the encoder and CTC head. The subsampling's
// convolutions take each channel's band (the tap its bits choose at each
// stride-2 stage: channel c of position p is band 8p+c) at the frame at
// each kernel's center, and its output layer puts the bands in order: the
// encoder's input is each eighth frame's features. Uniform attention (no
// queries or keys) averages the frames' layer norms, at a scale that makes
// each frame's residual negligible, and the final layer norm gives the
// direction the reference reduces a sound to (reduceParakeet); the CTC head
// scores each key's similarity with it, and the default at the threshold.
func buildModel(keys [][]float64, answers []string, defaultAnswer string, similarity float64, mels int) []byte {
	d, c, fp := mels, channels, mels/channels
	classes := len(answers) + 2 // keyed answers, the default, and CTC blank
	f := &gguf.File{}
	f.Set("general.name", "axx CTC")
	f.Set("parakeet.arch", "ctc")
	f.Set("parakeet.encoder.feat_in", uint32(mels))
	f.Set("parakeet.encoder.d_model", uint32(d))
	f.Set("parakeet.encoder.n_layers", uint32(1))
	f.Set("parakeet.encoder.n_heads", uint32(1))
	f.Set("parakeet.encoder.ff_dim", uint32(1))
	f.Set("parakeet.encoder.conv_kernel", uint32(1))
	f.Set("parakeet.encoder.conv_norm_type", "batch_norm")
	f.Set("parakeet.encoder.subsampling_factor", uint32(8))
	f.Set("parakeet.encoder.subsampling_conv_channels", uint32(c))
	f.Set("parakeet.encoder.xscaling", false)
	f.Set("parakeet.encoder.pos_emb_max_len", uint32(5000))
	f.Set("parakeet.encoder.use_bias", true)
	f.Set("parakeet.preprocessor.sample_rate", uint32(16000))
	f.Set("parakeet.preprocessor.n_mels", uint32(mels))
	f.Set("parakeet.preprocessor.n_fft", uint32(match.ParakeetFFT))
	f.Set("parakeet.preprocessor.win_length", uint32(match.ParakeetFFT))
	f.Set("parakeet.preprocessor.hop_length", uint32(match.MelHop))
	f.Set("parakeet.preprocessor.preemph", float32(0))
	f.Set("parakeet.preprocessor.mag_power", float32(2))
	f.Set("parakeet.preprocessor.normalize", "NA")
	f.Set("parakeet.preprocessor.log_zero_guard", float32(match.ParakeetGuard))
	f.Set("parakeet.vocab_size", uint32(classes-1))
	f.Set("parakeet.blank_id", uint32(classes-1))
	pieces := make([]any, 0, classes-1)
	for _, answer := range answers {
		pieces = append(pieces, answer)
	}
	pieces = append(pieces, defaultAnswer)
	f.Set("parakeet.tokenizer.pieces", gguf.Array{Type: 8, Values: pieces})

	f.Tensors = append(f.Tensors,
		tensor("preprocessor.featurizer.fb", []uint64{match.ParakeetFFT/2 + 1, uint64(mels), 1}, filterBank(mels)),
		tensor("preprocessor.featurizer.window", []uint64{match.ParakeetFFT}, hannWindow()),
	)
	// A 3×3 kernel's tap: its frequency (ne0) and time (ne1), the center in
	// time. Channel c's taps in frequency at the three stages are the bits
	// of c, from its lowest: 1 the even band of the two below a position, 2
	// the odd.
	tap := func(ch, bit int) int { return 1 + (ch>>bit)&1 }
	conv := func(bit int) []float32 {
		w := make([]float32, 9*c)
		for ch := range c {
			w[tap(ch, bit)+3*1+9*ch] = 1
		}
		return w
	}
	pointwise := make([]float32, c*c)
	for ch := range c {
		pointwise[ch+c*ch] = 1
	}
	for _, stage := range []struct {
		name string
		dims []uint64
		w    []float32
	}{
		{"conv.0", []uint64{3, 3, 1, uint64(c)}, conv(0)},
		{"conv.2", []uint64{3, 3, 1, uint64(c)}, conv(1)},
		{"conv.3", []uint64{1, 1, uint64(c), uint64(c)}, pointwise},
		{"conv.5", []uint64{3, 3, 1, uint64(c)}, conv(2)},
		{"conv.6", []uint64{1, 1, uint64(c), uint64(c)}, pointwise},
	} {
		f.Tensors = append(f.Tensors,
			tensor("encoder.pre_encode."+stage.name+".weight", stage.dims, stage.w),
			tensor("encoder.pre_encode."+stage.name+".bias", []uint64{uint64(c)}, make([]float32, c)),
		)
	}
	// The subsampling's output, position p of channel ch at p+fp*ch, is band
	// 8p+ch.
	outWeights := make([]float32, c*fp*d)
	for p := range fp {
		for ch := range c {
			outWeights[(p+fp*ch)+c*fp*(channels*p+ch)] = 1
		}
	}
	f.Tensors = append(f.Tensors,
		tensor("encoder.pre_encode.out.weight", []uint64{uint64(c * fp), uint64(d)}, outWeights),
		tensor("encoder.pre_encode.out.bias", []uint64{uint64(d)}, make([]float32, d)),
	)

	for _, norm := range []string{"norm_feed_forward1", "norm_self_att", "norm_conv", "norm_feed_forward2", "norm_out"} {
		f.Tensors = append(f.Tensors,
			tensor("encoder.layers.0."+norm+".weight", []uint64{uint64(d)}, full(d, 1)),
			tensor("encoder.layers.0."+norm+".bias", []uint64{uint64(d)}, make([]float32, d)),
		)
	}
	for _, ff := range []string{"feed_forward1", "feed_forward2"} {
		f.Tensors = append(f.Tensors,
			tensor("encoder.layers.0."+ff+".linear1.weight", []uint64{uint64(d), 1}, make([]float32, d)),
			tensor("encoder.layers.0."+ff+".linear1.bias", []uint64{1}, make([]float32, 1)),
			tensor("encoder.layers.0."+ff+".linear2.weight", []uint64{1, uint64(d)}, make([]float32, d)),
			tensor("encoder.layers.0."+ff+".linear2.bias", []uint64{uint64(d)}, make([]float32, d)),
		)
	}
	attentionOut := identity(d)
	for i := range attentionOut {
		attentionOut[i] *= attentionScale
	}
	for _, attn := range []struct {
		name string
		data []float32
	}{
		{"linear_q", make([]float32, d*d)},
		{"linear_k", make([]float32, d*d)},
		{"linear_v", identity(d)},
		{"linear_out", attentionOut},
		{"linear_pos", make([]float32, d*d)},
	} {
		f.Tensors = append(f.Tensors,
			tensor("encoder.layers.0.self_attn."+attn.name+".weight", []uint64{uint64(d), uint64(d)}, attn.data),
			tensor("encoder.layers.0.self_attn."+attn.name+".bias", []uint64{uint64(d)}, make([]float32, d)),
		)
	}
	f.Tensors = append(f.Tensors,
		tensor("encoder.layers.0.self_attn.pos_bias_u", []uint64{uint64(d), 1}, make([]float32, d)),
		tensor("encoder.layers.0.self_attn.pos_bias_v", []uint64{uint64(d), 1}, make([]float32, d)),
	)
	for _, conv := range []struct {
		name string
		dims []uint64
		bias int
	}{
		{"pointwise_conv1", []uint64{uint64(d), uint64(2 * d)}, 2 * d},
		{"depthwise_conv", []uint64{1, 1, uint64(d)}, d},
		{"pointwise_conv2", []uint64{uint64(d), uint64(d)}, d},
	} {
		f.Tensors = append(f.Tensors,
			tensor("encoder.layers.0.conv."+conv.name+".weight", conv.dims, make([]float32, numel(conv.dims))),
			tensor("encoder.layers.0.conv."+conv.name+".bias", []uint64{uint64(conv.bias)}, make([]float32, conv.bias)),
		)
	}
	f.Tensors = append(f.Tensors,
		tensor("encoder.layers.0.conv.batch_norm.weight", []uint64{uint64(d)}, full(d, 1)),
		tensor("encoder.layers.0.conv.batch_norm.bias", []uint64{uint64(d)}, make([]float32, d)),
		tensor("encoder.layers.0.conv.batch_norm.running_mean", []uint64{uint64(d)}, make([]float32, d)),
		tensor("encoder.layers.0.conv.batch_norm.running_var", []uint64{uint64(d)}, full(d, 1)),
	)
	// Each key's similarity with the direction (both of variance 1: their
	// dot product over d), and the default at the threshold.
	headWeights := make([]float32, d*classes)
	for i, key := range keys {
		for j, v := range key {
			headWeights[i*d+j] = float32(ctcGain * v / float64(d))
		}
	}
	bias := make([]float32, classes)
	bias[len(answers)] = float32(ctcGain * similarity)
	bias[classes-1] = -ctcGain
	f.Tensors = append(f.Tensors,
		tensor("decoder.decoder_layers.0.weight", []uint64{1, uint64(d), uint64(classes)}, headWeights),
		tensor("decoder.decoder_layers.0.bias", []uint64{uint64(classes)}, bias),
	)
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		panic(fmt.Sprintf("write Parakeet stub: %v", err))
	}
	return b.Bytes()
}

func tensor(name string, dims []uint64, values []float32) gguf.Tensor {
	if len(values) != numel(dims) {
		panic("tensor " + name + " has the wrong number of values")
	}
	b := make([]byte, 4*len(values))
	for i, v := range values {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(v))
	}
	return gguf.Tensor{Name: name, Dims: dims, Type: gguf.F32, Data: b}
}

func numel(dims []uint64) int {
	n := 1
	for _, d := range dims {
		n *= int(d)
	}
	return n
}

func identity(n int) []float32 {
	out := make([]float32, n*n)
	for i := range n {
		out[i*n+i] = 1
	}
	return out
}

func full(n int, v float32) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func hannWindow() []float32 {
	out := make([]float32, match.ParakeetFFT)
	for i := range out {
		out[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(match.ParakeetFFT-1)))
	}
	return out
}

func filterBank(mels int) []float32 {
	_, _, _, filters := match.ParakeetBasis(16000, mels)
	out := make([]float32, 0, mels*(match.ParakeetFFT/2+1))
	for _, row := range filters {
		for _, v := range row {
			out = append(out, float32(v))
		}
	}
	return out
}
