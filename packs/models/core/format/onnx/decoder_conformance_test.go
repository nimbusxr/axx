//go:build integration

package onnx

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/lm"
	"github.com/nimbusxr/axx/packs/models/core/internal/transformersjs"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// folderFiles are the files of a model's folder transformers.js reads
// besides its graph, in the order of decoders' sums.
var folderFiles = [...]string{"config.json", "generation_config.json", "tokenizer.json", "tokenizer_config.json"}

// decoders are real language models' folders, as Hugging Face's
// repositories have them and transformers.js loads them, whose tokenizers
// and decoders stubs are held to: each file pinned by its SHA-256. A model
// with a graph (its weights in external data, so that its .onnx is small)
// has its stub's chats run too; the others, their tokenizers only.
var decoders = []struct {
	name, repo, commit string
	// sums are folderFiles' SHA-256s.
	sums [len(folderFiles)]string
	// graph is the ONNX file the app loads, of transformers.js's dtype.
	graph, graphSum, dtype string
}{
	{
		"Llama 3.2 (BPE, merges ignored)", "onnx-community/Llama-3.2-1B-Instruct-ONNX", "14007543b6dc92de88daf96a9aa85d2f95ace6ef",
		[len(folderFiles)]string{
			"7004209e8cdba9890495946c86705430d67c4d167c45df8021df9c3466341ec4",
			"cc5b5a4024436a2188736eafbe1651f7e758f5478e0200eb0aeb3c2abf445660",
			"3a223ade375cc1d13b04e897ce1d36a04f50140e1ba3d107021ea68d4b5e614c",
			"fb8e113b6240ab997fe87464b8b58697cc769a8df40699356e8524d0dfc60c0e",
		},
		"onnx/model_q4f16.onnx", "fccc573530dbefac92a36797262e4228dc6fb474734d80f6781e37744550333c", "q4f16",
	},
	{
		"Gemma 3 270M (BPE of ▁, its bytes' fallback; float16 logits)", "onnx-community/gemma-3-270m-it-ONNX", "2dbbfdb1b59bd034eb959428c6a7da9dd7ea27f0",
		[len(folderFiles)]string{
			"6b1ee5234edd196d26707b7b3f612cf6290bdd98aba4f0c5654fffbc0008287a",
			"94ef2242d98a6d0fd8bf6c71206b459f2fef81729f773ee37015d64b7cc11632",
			"55da1312bdf1d7d8fe8d9d1b3eed04086261149e6034e0ac3f8c633b67f5aac8",
			"8ae5c9297e0ca85077c0f4952f85251b4773af7abc4673b62b54bc0b609a9667",
		},
		"onnx/model_q4f16.onnx", "01d620bf3c2c1d5b843a5c92a4456ce1f4c63f6ceb814da26a3420e8aad2b4a4", "q4f16",
	},
	{
		"Gemma 3 1B (float32, its data in two files)", "onnx-community/gemma-3-1b-it-ONNX", "a58439f40017d3b99c7d378ff525e54e0ba08ebf",
		[len(folderFiles)]string{
			"91e864295e06e3069a26e89dc5d7e506d32153b23a29bf283e8e3a46fc546806",
			"2ff57fef908e4da8accc2e369d2c848785e6f2f8c5d1ed102bd9cb9aa70f7aaa",
			"55da1312bdf1d7d8fe8d9d1b3eed04086261149e6034e0ac3f8c633b67f5aac8",
			"362055c2b4c69af56db94be4f1297bc6d2936a8f4ab9c0173b5bedd502280507",
		},
		"onnx/model.onnx", "d4c50658cde3794f66a8e480f1ecbf23c92ae787ee625eae1300884b9ee5bd0a", "fp32",
	},
	{
		"LFM2 (convolutions' state and attention's; logits kept)", "onnx-community/LFM2-350M-ONNX", "ecc51d35a7edbc8f9ef6d23b59a5f036dcfc7ee8",
		[len(folderFiles)]string{
			"5828fb2f22d478d13f68bb609baee33f4584993e47dbf0c0acbd0d4aacf8df42",
			"1be9f5bf1f044d6ce4ec91376c2bc14337078e1260a0ba56ff507c0c25ef1902",
			"bec04fe03d27dbcb6276b46e7d2325433323a8132a2f0404ea6bbf98250a9211",
			"1635881015705807ba0fb6510d77a42e53fc041f4de12bb801f1685a62b7220d",
		},
		"onnx/model_q4f16.onnx", "3227fd58d43811630dd918b58d25ff8c5ef3d16b13e7efc5496205079990a82c", "q4f16",
	},
	{
		"Phi-4 mini (BPE, its splits' matches only)", "onnx-community/Phi-4-mini-instruct-ONNX", "e61f45fc5fabba2aee31ff85ba4cf99219b4bf28",
		[len(folderFiles)]string{
			"13f196a6d99bfe053c183adf47a8ff772b1d70802a1927206a705d3fd99b132f",
			"4d8c499900ee9a4c4b1bca1887bc5a5c5ac9b01a57a364f30c583cdd1019cc72",
			"9ca5aa723a31a7a122497e059bd48dd67a5bd03ad16b3ffcf16093fd3021c1eb",
			"e263ca0b737a5e1ffc6bcb8ca1b0c85ae7febc90ecbf1aac968170f0f79b4feb",
		},
		"onnx/model_q4f16.onnx", "ca26127777adf1df99b5fc1a3b4d1e0c426a6bf56626889873bfc4a6a095b4fc", "q4f16",
	},
	{
		"SmolLM2 (BPE, digits split)", "HuggingFaceTB/SmolLM2-135M-Instruct", "12fd25f77366fa6b3b4b768ec3050bf629380bac",
		[len(folderFiles)]string{
			"8eb740e8bbe4cff95ea7b4588d17a2432deb16e8075bc5828ff7ba9be94d982a",
			"87b916edaaab66b3899b9d0dd0752727dff6666686da0504d89ae0a6e055a013",
			"9ca9acddb6525a194ec8ac7a87f24fbba7232a9a15ffa1af0c1224fcd888e47c",
			"4ec77d44f62efeb38d7e044a1db318f6a939438425312dfa333b8382dbad98df",
		},
		"", "", "",
	},
	{
		"Qwen2.5 (BPE, NFC)", "onnx-community/Qwen2.5-0.5B-Instruct", "22942cb7d7ba4cc81bb4673549ca4d4614469b5e",
		[len(folderFiles)]string{
			"777e01f0fbb3346eb229cb6fb278ed6533c1e4dcb9ebf4bed0f6e94ef17fa1b5",
			"f7e7ce458658b2d40d9eb213b91b77a8bf698845ab89360976722d7ac46928a3",
			"a8506e7111b80c6d8635951a02eab0f4e1a8e4e5772da83846579e97b16f61bf",
			"6a14243f0ae04884e597b3f032c05e15de9f417318fdcac24ab4e2680e691430",
		},
		"", "", "",
	},
	{
		"Qwen3 (BPE, NFC)", "onnx-community/Qwen3-0.6B-ONNX", "58ca03a5e7b354b480b1a82b34bde8dc7b854cc0",
		[len(folderFiles)]string{
			"8a04114ba59cc42b47d804d35d1d5c61d746ae4634f41f796768c6e302d39b9e",
			"9e9e031ae8bca36eefcdcdd0b35d83c61baa01e31d68d5f5a961c9ca1a4b95bf",
			"e7a95fce95bf5b0946d0ddb3f9d7caa030b7e850bbe92b0edb26bcf563e9f3d5",
			"7c5194eecfd95a4dcbd6b4b502af785d2541fd65a388d1cae7ad43693c67b802",
		},
		"", "", "",
	},
}

// folderOf lays out a model's files in a folder of their own, as its
// repository has them.
func folderOf(t *testing.T, repo, commit string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, sum := range files {
		b, err := os.ReadFile(modelstest.Fetch(t, "https://huggingface.co/"+repo+"/resolve/"+commit+"/"+name, sum))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), b)
	}
	return dir
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Real models' tokenizers tokenize as transformers.js does, and their
// stubs, in their folders in place of their models, answer in
// transformers.js's text-generation pipeline as their mappings say, greedy
// and sampled.
func TestTransformersJSAnswersAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := transformersjs.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	corpus := modelstest.LanguageCorpus(t)
	type check struct {
		model string
		vocab *lm.Vocab
		job   int
	}
	var jobs []transformersjs.Job
	var checks []check
	for _, d := range decoders {
		files := map[string]string{}
		for i, name := range folderFiles {
			files[name] = d.sums[i]
		}
		if d.graph != "" {
			files[d.graph] = d.graphSum
		}
		real := folderOf(t, d.repo, d.commit, files)
		tok, err := os.ReadFile(filepath.Join(real, "tokenizer.json"))
		if err != nil {
			t.Fatal(err)
		}
		spec, err := lm.TokenizerJSON(tok)
		if err != nil {
			t.Fatalf("%s: %v", d.name, err)
		}
		v, err := lm.NewVocab(spec)
		if err != nil {
			t.Fatalf("%s: %v", d.name, err)
		}
		checks = append(checks, check{model: d.name, vocab: v, job: len(jobs)})
		jobs = append(jobs, transformersjs.Job{Model: real, Tokenize: corpus})
		if d.graph == "" {
			continue
		}

		c, err := ReadContract(filepath.Join(real, filepath.FromSlash(d.graph)))
		if err != nil {
			t.Fatalf("%s: %v", d.name, err)
		}
		dir := t.TempDir()
		if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
			t.Fatal(err)
		}
		modelstest.WriteMappings(t, dir, modelstest.LanguageMappings)
		s, err := stub.Load(dir)
		if err != nil {
			t.Fatalf("%s: %v", d.name, err)
		}
		m, err := writeModel(s)
		if err != nil {
			t.Fatalf("%s: %v", d.name, err)
		}
		// The app's folder: the real model's, its model the stub.
		app := t.TempDir()
		for _, name := range folderFiles {
			b, err := os.ReadFile(filepath.Join(real, name))
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(app, name), b)
		}
		graph := filepath.Join(app, filepath.FromSlash(d.graph))
		writeFile(t, graph, m.File)
		for name, b := range m.Beside {
			writeFile(t, filepath.Join(filepath.Dir(graph), name), b)
		}
		var chats []transformersjs.Chat
		for _, temperature := range []float64{0, 1} {
			for _, c := range modelstest.LanguageChats {
				chats = append(chats, transformersjs.Chat{System: modelstest.LanguageSystem, Prompt: c.Prompt, Temperature: temperature})
			}
		}
		checks = append(checks, check{model: d.name, job: len(jobs)})
		jobs = append(jobs, transformersjs.Job{Model: app, DType: d.dtype, Chats: chats})
		t.Logf("%s: a stub of %d KB", d.name, len(m.File)/1024)
	}
	results, err := rt.Run(ctx, jobs)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		r := results[c.job]
		if c.vocab != nil {
			for i, text := range corpus {
				if got := c.vocab.Tokenize(text, false); !slices.Equal(got, r.Tokens[i]) {
					t.Errorf("%s: %q tokenizes to %v; transformers.js's %v", c.model, text, got, r.Tokens[i])
				}
			}
			continue
		}
		for i, answer := range r.Answers {
			want := modelstest.LanguageChats[i%len(modelstest.LanguageChats)]
			if answer != want.Want {
				t.Errorf("%s: %q is answered %q, want %q", c.model, want.Prompt, answer, want.Want)
			}
		}
	}
}
