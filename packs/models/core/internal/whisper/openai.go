package whisper

import (
	"fmt"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
)

// multilingualVocab is the smallest vocabulary of a Whisper of many
// languages (an English-only one's is 51864).
const multilingualVocab = 51865

// SpecialTokens are a Whisper's special tokens as OpenAI's tokenizer
// numbers them (whisper.cpp's too), of its vocabulary's size: after the
// ranked tokens, the transcript's end and beginning, its languages, its
// tasks and the timestamps; an English-only model's from 50256, a model of
// many languages' one later, its tasks and timestamps after its languages.
func SpecialTokens(vocab int) Tokens {
	t := Tokens{EOT: 50256, SOT: 50257, NoTimestamps: 50362, TimestampBegin: 50363, Language: -1}
	transcribe, translate := int32(50358), int32(50357)
	if vocab >= multilingualVocab {
		dt := int32(vocab-51765-1) - 98
		t.EOT++
		t.SOT++
		t.NoTimestamps += dt
		t.TimestampBegin += dt
		transcribe += dt
		translate += dt
		t.Language = t.SOT + 1
		t.Tasks = []int32{transcribe, translate}
	}
	return t
}

// NonSpeech are the tokens OpenAI's decoding suppresses as no speech
// (whisper's tokenizer's non_speech_tokens): symbols of speakers' tags and
// annotations, each alone and after a space, and a hyphen and a quote that
// begin a word.
func NonSpeech(v *lm.Vocab) []int32 {
	symbols := strings.Split(`" # ( ) * + / : ; < = > @ [ \ ] ^ _ `+"`"+` { | } ~ 「 」 『 』`, " ")
	symbols = append(symbols, strings.Fields(`<< >> <<< >>> -- --- -( -[ (' (" (( )) ((( ))) [[ ]] {{ }} ♪♪ ♪♪♪`)...)
	misc := strings.Split("♩♪♫♬♭♮♯", "")
	out := []int32{}
	add := func(t int32) {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	for _, s := range []string{" -", " '"} {
		if ids := v.Tokenize(s, false); len(ids) > 0 {
			add(ids[0])
		}
	}
	for _, s := range append(symbols, misc...) {
		for _, text := range []string{s, " " + s} {
			ids := v.Tokenize(text, false)
			if len(ids) == 1 || len(ids) > 0 && slices.Contains(misc, s) {
				add(ids[0])
			}
		}
	}
	slices.Sort(out)
	return out
}

// OpenAINames are the weights by the names of OpenAI's Whisper (its
// checkpoints', and whisper.cpp's model files').
func OpenAINames(w Weights, d Dims) Weights {
	out := Weights{}
	put := func(to, from string) { out[to] = w[from] }
	put("encoder.conv1.weight", "model.encoder.conv1.weight")
	put("encoder.conv1.bias", "model.encoder.conv1.bias")
	put("encoder.conv2.weight", "model.encoder.conv2.weight")
	put("encoder.conv2.bias", "model.encoder.conv2.bias")
	put("encoder.positional_embedding", "model.encoder.embed_positions.weight")
	put("encoder.ln_post.weight", "model.encoder.layer_norm.weight")
	put("encoder.ln_post.bias", "model.encoder.layer_norm.bias")
	put("decoder.token_embedding.weight", "model.decoder.embed_tokens.weight")
	put("decoder.positional_embedding", "model.decoder.embed_positions.weight")
	put("decoder.ln.weight", "model.decoder.layer_norm.weight")
	put("decoder.ln.bias", "model.decoder.layer_norm.bias")
	names := map[string]string{
		"self_attn_layer_norm": "attn_ln", "self_attn.q_proj": "attn.query", "self_attn.k_proj": "attn.key", "self_attn.v_proj": "attn.value", "self_attn.out_proj": "attn.out",
		"final_layer_norm": "mlp_ln", "fc1": "mlp.0", "fc2": "mlp.2",
		"encoder_attn_layer_norm": "cross_attn_ln", "encoder_attn.q_proj": "cross_attn.query", "encoder_attn.k_proj": "cross_attn.key", "encoder_attn.v_proj": "cross_attn.value", "encoder_attn.out_proj": "cross_attn.out",
	}
	for _, side := range []struct {
		name   string
		layers int
	}{{"encoder", d.EncoderLayers}, {"decoder", d.DecoderLayers}} {
		for i := range side.layers {
			from := fmt.Sprintf("model.%s.layers.%d.", side.name, i)
			to := fmt.Sprintf("%s.blocks.%d.", side.name, i)
			for hf, openai := range names {
				for _, part := range []string{".weight", ".bias"} {
					if t, ok := w[from+hf+part]; ok {
						out[to+openai+part] = t
					}
				}
			}
		}
	}
	return out
}

// Vocabulary is the tokenizer of a Whisper's ranked tokens (tiktoken's):
// its tokens up to its transcript's end the ranked ones, the rest of its
// vocabulary its special tokens, which have no text.
func Vocabulary(toks [][]byte, t Tokens, vocab int) lm.Spec {
	toks = toks[:min(len(toks), int(t.EOT))]
	var specials []string
	for id := len(toks); id < vocab; id++ {
		specials = append(specials, specialName(int32(id), t))
	}
	return lm.Ranked(toks, specials)
}

// specialName is a special token's name, as Whisper's tokenizer has it.
func specialName(id int32, t Tokens) string {
	switch {
	case id == t.EOT:
		return "<|endoftext|>"
	case id == t.SOT:
		return "<|startoftranscript|>"
	case id == t.NoTimestamps:
		return "<|notimestamps|>"
	case id >= t.TimestampBegin:
		return fmt.Sprintf("<|%.2f|>", float64(id-t.TimestampBegin)*0.02)
	}
	return fmt.Sprintf("<|special_%d|>", id)
}
