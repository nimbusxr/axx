//go:build integration

package mnn

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/internal/mnnrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// realLLMs are real MNN-LLM models whose stubs MNN's LLM engine runs: their
// config.json, llm_config.json and tokenizer (tokenizer.txt, unless given),
// in that order, each pinned by its SHA-256.
var realLLMs = []struct {
	name, repo, commit string
	sums               [3]string
	tokenizer          string
}{
	{
		"Qwen2.5 0.5B Instruct (MNN's Hugging Face tokenizer)", "taobao-mnn/Qwen2.5-0.5B-Instruct-MNN", "c880456f5ae3d591fd8fc6c32a742cc6ace500cb",
		[3]string{
			"7636b063425bdbc0e2e429cb23af7f594b5ba145bab2045dfda852416d9285de",
			"ec05709b4261d59b510a0b7a636c6dcb6c5635c08fee7eb3c4f04188b509694b",
			"b86f1298a0d6a1b2f312946c2f674f883f1d134ccabc79c42dd4c6b5beadf37b",
		},
		"",
	},
	{
		"Llama 3.2 1B Instruct (MNN's tiktoken tokenizer)", "taobao-mnn/Llama-3.2-1B-Instruct-MNN", "e3d08904fea83393ac81c4ed582dd1ddd9312dd0",
		[3]string{
			"7636b063425bdbc0e2e429cb23af7f594b5ba145bab2045dfda852416d9285de",
			"5c194a45f43b093a02863937c372ee185bd5148d4b229d23c1362761f0cefb47",
			"14fb9cbd31e982e92c332ed59d3cc6c300996c796838c20311f70488b4c9b235",
		},
		"",
	},
	{
		"MiniCPM4 0.5B (MNN's SentencePiece tokenizer, a chat template, sampling)", "taobao-mnn/MiniCPM4-0.5B-MNN", "7a382bd33bff738b5a459f398da6f26504d6e71a",
		[3]string{
			"e26e12426d4e49e9c4db7513da3f8276634dd28513b26d82e1dccccbde28be30",
			"4ff8d3898e538daf4fff5983b95161404eacf20b08005d1601a658f2dc09f399",
			"2cfdff08627bb8000ef14208301d40bf6e586b30219e59e914dfe768e1c919d5",
		},
		"",
	},
	{
		"Gemma 3 270M (MNN's tokenizer pipeline, .mtok; mixed attention)", "taobao-mnn/gemma-3-270m-it-MNN", "9acdd6d40cb9e35c3d91cda2668d85d070b1d988",
		[3]string{
			"daf8aa56d276968f3642396baf81cf3163602cfb5a6d3b989935b7a24f242c7a",
			"0a47a667bf518d76ccb498a49457691a4e219869160c52cee93697b28f653586",
			"6bffcbf66f8719677895ff20a579e00425c48939bde54e504784cd2187fa0d08",
		},
		"tokenizer.mtok",
	},
}

// MNN's LLM engine answers a stub's conversations as its mappings say (on
// the CPU at its low precision, in halves on ARM's, and at its high; of
// turns on their own, its default, and of turns that keep the ones before;
// and on a Mac's GPU, of Metal), and tokenizes as the stub's tokenizer does.
func TestMNNLLMAnswersAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := mnnrun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range realLLMs {
		t.Run(m.name, func(t *testing.T) {
			real := t.TempDir()
			tokenizer := cmp.Or(m.tokenizer, "tokenizer.txt")
			for i, name := range []string{"config.json", "llm_config.json", tokenizer} {
				b, err := os.ReadFile(modelstest.Fetch(t, "https://huggingface.co/"+m.repo+"/resolve/"+m.commit+"/"+name, m.sums[i]))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(real, name), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			conformLLM(ctx, t, rt, real, tokenizer)
		})
	}
}

func conformLLM(ctx context.Context, t *testing.T, rt *mnnrun.Runtime, real, tokenizer string) {
	c, err := readLLMContract(filepath.Join(real, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
		t.Fatal(err)
	}
	modelstest.WriteMappings(t, dir, []map[string]any{
		{"name": "The neighbour", "input": map[string]any{"text": "Leave it with the neighbour"}, "output": map[string]any{"text": "Leave the parcel with the neighbour."}},
		{"name": "Fragile", "input": map[string]any{"text": "fragile"}, "output": map[string]any{"json": map[string]any{"handling": "fragile"}}},
		{"name": "Anything else", "default": true, "output": map[string]any{"text": "No special instructions."}},
	})
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	model, err := writeLLM(s)
	if err != nil {
		t.Fatal(err)
	}
	app := t.TempDir()
	for _, name := range []string{"config.json", tokenizer} {
		b, err := os.ReadFile(filepath.Join(real, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(app, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(app, "llm.mnn"), model.File, 0o644); err != nil {
		t.Fatal(err)
	}
	size := len(model.File)
	for name, b := range model.Beside {
		if err := os.WriteFile(filepath.Join(app, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
		size += len(b)
	}
	t.Logf("a stub of %d KB", size/1024)

	// Each conversation's prompts, and its answers: of the engine's turns
	// each on its own (its default), and of turns that keep the ones before
	// (reuse_kv).
	conversations := []struct {
		prompts, alone, kept []string
	}{
		{[]string{"Summarize this note: Leave it with the neighbour at number 12, please."}, []string{"Leave the parcel with the neighbour."}, nil},
		{[]string{"Careful, fragile glass inside."}, []string{`{"handling":"fragile"}`}, nil},
		{[]string{"Leave it with the neighbours."}, []string{"No special instructions."}, nil},
		{[]string{"leave it with the neighbour"}, []string{"No special instructions."}, nil},
		{
			[]string{"Ring the bell twice.", "Also: the box is fragile.", "Thanks!"},
			[]string{"No special instructions.", `{"handling":"fragile"}`, "No special instructions."},
			[]string{"No special instructions.", `{"handling":"fragile"}`, `{"handling":"fragile"}`},
		},
	}
	var prompts [][]string
	for _, conv := range conversations {
		prompts = append(prompts, conv.prompts)
	}
	tokRaw, err := os.ReadFile(filepath.Join(real, tokenizer))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := lm.ReadMNNTokenizer(tokRaw)
	if err != nil {
		t.Fatal(err)
	}
	v, err := lm.NewVocab(tok.Spec)
	if err != nil {
		t.Fatal(err)
	}
	// The texts the model's vocabulary spells: MNN's tiktoken tokenizer never
	// ends on a byte no token begins (MNN's export of Llama 3's vocabulary
	// has none of 0x80 to 0xff on their own, nor of an emoji's first byte).
	var corpus []string
	for _, text := range modelstest.LanguageCorpus(t) {
		if v.Detokenize(v.Tokenize(text, true)) == text || tok.Spec.Model != "longest" {
			corpus = append(corpus, text)
		}
	}
	var jobs []mnnrun.LLMJob
	settings := []map[string]any{{"precision": "low"}, {"precision": "high"}, {"precision": "low", "reuse_kv": true}}
	if runtime.GOOS == "darwin" {
		settings = append(settings, map[string]any{"backend_type": "metal", "precision": "low"})
	}
	for _, settings := range settings {
		jobs = append(jobs, mnnrun.LLMJob{Config: filepath.Join(app, "config.json"), Settings: settings, Conversations: prompts, Encode: corpus})
	}
	results, err := rt.Generate(ctx, jobs)
	if err != nil {
		t.Fatal(err)
	}
	for j, job := range jobs {
		for i, conv := range conversations {
			want := conv.alone
			if job.Settings["reuse_kv"] == true && conv.kept != nil {
				want = conv.kept
			}
			for k, w := range want {
				if got := results[j].Answers[i][k]; got != w {
					t.Errorf("%v: %q: answered %q, want %q", job.Settings, conv.prompts[k], got, w)
				}
			}
		}
		for i, text := range corpus {
			// MNN puts its prefix tokens (a BOS) before each text.
			if got := append(slices.Clone(tok.Prefix), v.Tokenize(text, true)...); !slices.Equal(got, results[j].Tokens[i]) {
				t.Errorf("%q tokenizes to %v, MNN's tokenizer to %v", text, got, results[j].Tokens[i])
			}
		}
	}
}
