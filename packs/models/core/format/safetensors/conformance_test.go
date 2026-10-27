//go:build integration

package safetensors

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/transformersrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// realModels are real language models' folders as Hugging Face's
// repositories have them, but their weights (config.json, the generation
// config, the tokenizer's files), whose stubs transformers runs: each file
// pinned by its SHA-256, the sums in the order of the files.
var realModels = []struct {
	name, repo, commit string
	files, sums        []string
}{
	{
		"SmolLM2 135M", "HuggingFaceTB/SmolLM2-135M-Instruct", "12fd25f77366fa6b3b4b768ec3050bf629380bac",
		[]string{"config.json", "generation_config.json", "merges.txt", "special_tokens_map.json", "tokenizer.json", "tokenizer_config.json", "vocab.json"},
		[]string{
			"8eb740e8bbe4cff95ea7b4588d17a2432deb16e8075bc5828ff7ba9be94d982a",
			"87b916edaaab66b3899b9d0dd0752727dff6666686da0504d89ae0a6e055a013",
			"0b54e8aa4e53d5383e2e4bc635a56b43f9647f7b13832d5d9ecd8f82dac4f510",
			"2b7379f3ae813529281a5c602bc5a11c1d4e0a99107aaa597fe936c1e813ca52",
			"9ca9acddb6525a194ec8ac7a87f24fbba7232a9a15ffa1af0c1224fcd888e47c",
			"4ec77d44f62efeb38d7e044a1db318f6a939438425312dfa333b8382dbad98df",
			"82b84012e3add4d01d12ba14442026e49b8cbbaead1f79ecf3d919784f82dc79",
		},
	},
	{
		"Qwen2.5 0.5B", "Qwen/Qwen2.5-0.5B-Instruct", "7ae557604adf67be50417f59c2c2f167def9a775",
		[]string{"config.json", "generation_config.json", "merges.txt", "tokenizer.json", "tokenizer_config.json", "vocab.json"},
		[]string{
			"18e18afcaccafade98daf13a54092927904649e1dd4eba8299ab717d5d94ff45",
			"e558847a8b4402616f1273797b015104dc266fe4b520056fca88823ba8f8ebe6",
			"599bab54075088774b1733fde865d5bd747cbcc7a547c5bc12610e874e26f5e3",
			"c0382117ea329cdf097041132f6d735924b697924d6f6fc3945713e96ce87539",
			"5b5d4f65d0acd3b2d56a35b56d374a36cbc1c8fa5cf3b3febbbfabf22f359583",
			"ca10d7e9fb3ed18575dd1e277a2579c16d108e32f27439684afa0e10b1440910",
		},
	},
	{
		"Qwen3 0.6B", "Qwen/Qwen3-0.6B", "c1899de289a04d12100db370d81485cdf75e47ca",
		[]string{"config.json", "generation_config.json", "merges.txt", "tokenizer.json", "tokenizer_config.json", "vocab.json"},
		[]string{
			"660db3b73d788119c04535e48cf9be5f55bc3100841a718637ae695b442f27dd",
			"2325da0f15bb848e018c5ae071b7943332e9f871d6b60e2ed22ca97d4cb993d2",
			"8831e4f1a044471340f7c0a83d7bd71306a5b867e95fd870f74d0c5308a904d5",
			"aeb13307a71acd8fe81861d94ad54ab689df773318809eed3cbe794b4492dae4",
			"d5d09f07b48c3086c508b30d1c9114bd1189145b74e982a265350c923acd8101",
			"ca10d7e9fb3ed18575dd1e277a2579c16d108e32f27439684afa0e10b1440910",
		},
	},
}

// Real models' stubs, in their folders in place of their weights and
// config, answer in transformers as their mappings say: by generate and by
// the text-generation pipeline, greedy and sampled.
func TestTransformersAnswersAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := transformersrun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var jobs []transformersrun.Job
	for _, m := range realModels {
		// The app's folder: the real model's files, its weights and config
		// the stub's.
		app := t.TempDir()
		for i, name := range m.files {
			b, err := os.ReadFile(modelstest.Fetch(t, "https://huggingface.co/"+m.repo+"/resolve/"+m.commit+"/"+name, m.sums[i]))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(app, name), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		c, err := ReadContract(filepath.Join(app, "model.safetensors"))
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		dir := t.TempDir()
		if err := c.Save(filepath.Join(dir, "contract.json")); err != nil {
			t.Fatal(err)
		}
		modelstest.WriteMappings(t, dir, modelstest.LanguageMappings)
		s, err := stub.Load(dir)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		model, err := Write(s)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if err := os.WriteFile(filepath.Join(app, "model.safetensors"), model.File, 0o644); err != nil {
			t.Fatal(err)
		}
		for name, b := range model.Beside {
			if err := os.WriteFile(filepath.Join(app, name), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("%s: a stub of %d KB", m.name, len(model.File)/1024)
		var chats []transformersrun.Chat
		for _, temperature := range []float64{0, 1} {
			for _, c := range modelstest.LanguageChats {
				chats = append(chats, transformersrun.Chat{Prompt: c.Prompt, Temperature: temperature})
			}
		}
		jobs = append(jobs, transformersrun.Job{Model: app, System: modelstest.LanguageSystem, Chats: chats})
	}
	results, err := rt.Run(ctx, jobs)
	if err != nil {
		t.Fatal(err)
	}
	for j, r := range results {
		for i := range r.Answers {
			want := modelstest.LanguageChats[i%len(modelstest.LanguageChats)]
			if r.Answers[i] != want.Want {
				t.Errorf("%s: %q is answered %q by generate, want %q", realModels[j].name, want.Prompt, r.Answers[i], want.Want)
			}
			if r.Pipeline[i] != want.Want {
				t.Errorf("%s: %q is answered %q by the pipeline, want %q", realModels[j].name, want.Prompt, r.Pipeline[i], want.Want)
			}
		}
	}
}
