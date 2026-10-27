//go:build integration

package onnx

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/genai"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// genaiModels are real language models' folders as ONNX Runtime GenAI
// loads them (genai_config.json, model.onnx and its tokenizer), whose
// decoders stubs are held to: each file pinned by its SHA-256, the sums in
// the order of the files. Their configs keep the cache in a buffer of the
// conversation's greatest length, which each call gives back.
var genaiModels = []struct {
	name, repo, commit, dir string
	files                   []string
	sums                    []string
}{
	{
		"Llama 3.2 1B (onnx-community's, for the CPU and phones)", "onnx-community/Llama-3.2-1B-Instruct-GENAI-ONNX", "e983c740a38fcfa57fb4d124b18b644974c3d966", "cpu_and_mobile/cpu-int4-rtn-block-32-acc-level-4/",
		[]string{"config.json", "genai_config.json", "model.onnx", "special_tokens_map.json", "tokenizer.json", "tokenizer_config.json"},
		[]string{
			"ce8da4ba3536149b61d40e08c1e2d5f5849960126cd9c811c38dfe6b67d9198c",
			"090cd874fc4766ed464f480c6fc8c31511d47b9bddf7fba5ade5b715f087d4bb",
			"8a03b513f3301e53d32a0812e84dba7b5c7320c212831893710d8c5b1c11641a",
			"6f38c73729248f6c127296386e3cdde96e254636cc58b4169d3fd32328d9a8ec",
			"79e3e522635f3171300913bb421464a87de6222182a0570b9b2ccba2a964b2b4",
			"9823dcfdc1121869029da45192238e85cf44f0b232a6d9dc20e4fe6f4242a14e",
		},
	},
	{
		"Qwen3 0.6B (Arm's, for Android phones)", "Arm/qwen3-0-6b-onnx-genai-int4-kquantlast-emb-int4", "e56a5fe71e597b9c4f450655d39657366834e2f6", "",
		[]string{"chat_template.jinja", "genai_config.json", "model.onnx", "tokenizer.json", "tokenizer_config.json"},
		[]string{
			"a55ee1b1660128b7098723e0abcd92caa0788061051c62d51cbe87d9cf1974d8",
			"67348085a5b994a6b6d94e56e395e675d8ef4732c3c46ff75de30a541489fd3f",
			"e9208ab7164ea6da0206eec2a2f0996a24e75d164f85823b02111f23046aab89",
			"be75606093db2094d7cd20f3c2f385c212750648bd6ea4fb2bf507a6a4c55506",
			"c56d783875898cebcf53c8797b61137efd52f1415be7531823b56f216f444527",
		},
	},
	{
		"Gemma 3 1B (Arm's, for Android phones; its spaces split as words' ends)", "Arm/gemma-3-1b-instruct-onnx-genai-int4-emb-int8", "ab1019231396ea297cff5fde677df1c9a044798c", "",
		[]string{"chat_template.jinja", "genai_config.json", "model.onnx", "special_tokens_map.json", "tokenizer.json", "tokenizer_config.json"},
		[]string{
			"7de1c58e208eda46e9c7f86397df37ec49883aeece39fb961e0a6b24088dd3c4",
			"4853198b2cd77cc50ed986891f0beba28507de4ceb973858a6a1bc54849d2811",
			"1be3eb5396e36b3fb65ce4d2cc9762994c2df93b002594d4f8f0eb56cbaddd15",
			"2f7b0adf4fb469770bb1490e3e35df87b1dc578246c5e7e6fc76ecf33213a397",
			"4667f2089529e8e7657cfb6d1c19910ae71ff5f28aa7ab2ff2763330affad795",
			"684a9f1c52bced7d310c52c8dabaddcc60ade18918681e51b448e92cb035fc01",
		},
	},
}

// Real models' stubs, in their folders in place of their models, answer in
// ONNX Runtime GenAI as their mappings say, greedy and sampled.
func TestGenAIAnswersAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := genai.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var jobs []genai.Job
	var names []string
	for _, m := range genaiModels {
		files := map[string]string{}
		for i, name := range m.files {
			files[m.dir+name] = m.sums[i]
		}
		real := filepath.Join(folderOf(t, m.repo, m.commit, files), filepath.FromSlash(m.dir))
		c, err := ReadContract(filepath.Join(real, "model.onnx"))
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
		model, err := writeModel(s)
		if err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		// The app's folder: the real model's, its model the stub.
		app := t.TempDir()
		for _, name := range m.files {
			b, err := os.ReadFile(filepath.Join(real, name))
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(app, name), b)
		}
		writeFile(t, filepath.Join(app, "model.onnx"), model.File)
		for name, b := range model.Beside {
			writeFile(t, filepath.Join(app, name), b)
		}
		var chats []genai.Chat
		for _, temperature := range []float64{0, 1} {
			for _, c := range modelstest.LanguageChats {
				chats = append(chats, genai.Chat{System: modelstest.LanguageSystem, Prompt: c.Prompt, Temperature: temperature})
			}
		}
		names = append(names, m.name)
		jobs = append(jobs, genai.Job{Model: app, Chats: chats})
		t.Logf("%s: a stub of %d KB", m.name, len(model.File)/1024)
	}
	results, err := rt.Run(ctx, jobs)
	if err != nil {
		t.Fatal(err)
	}
	for j, r := range results {
		for i, answer := range r.Answers {
			want := modelstest.LanguageChats[i%len(modelstest.LanguageChats)]
			if answer != want.Want {
				t.Errorf("%s: %q is answered %q, want %q", names[j], want.Prompt, answer, want.Want)
			}
		}
	}
}
