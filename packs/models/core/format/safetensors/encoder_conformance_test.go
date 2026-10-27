//go:build integration

package safetensors

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/transformersrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// realEncoders are sentence-transformers models' folders, but their weights:
// each file pinned by its SHA-256, the sums in the order of the files.
var realEncoders = []struct {
	name, repo, commit string
	files, sums        []string
	// dense are Dense modules the folder is given (modules.json, and each
	// module's config.json), by their paths.
	dense map[string]string
}{
	{
		"all-MiniLM-L6-v2 (BERT, mean, normalized)", "sentence-transformers/all-MiniLM-L6-v2", "1110a243fdf4706b3f48f1d95db1a4f5529b4d41",
		[]string{"1_Pooling/config.json", "config.json", "config_sentence_transformers.json", "modules.json", "sentence_bert_config.json", "special_tokens_map.json", "tokenizer.json", "tokenizer_config.json", "vocab.txt"},
		[]string{
			"4be450dde3b0273bb9787637cfbd28fe04a7ba6ab9d36ac48e92b11e350ffc23",
			"953f9c0d463486b10a6871cc2fd59f223b2c70184f49815e7efbcab5d8908b41",
			"061ca9d39661d6c6d6de5ba27f79a1cd5770ea247f8d46412a68a498dc5ac9f3",
			"84e40c8e006c9b1d6c122e02cba9b02458120b5fb0c87b746c41e0207cf642cf",
			"fc1993fde0a95c24ec6c022539d41cf6e2f7c9721e5415d6fb6897472a9cd4b7",
			"303df45a03609e4ead04bc3dc1536d0ab19b5358db685b6f3da123d05ec200e3",
			"be50c3628f2bf5bb5e3a7f17b1f74611b2561a3a27eeab05e5aa30f411572037",
			"acb92769e8195aabd29b7b2137a9e6d6e25c476a4f15aa4355c233426c61576b",
			"07eced375cec144d27c900241f3e339478dec958f92fddbc551f295c992038a3",
		},
		nil,
	},
	{
		"all-MiniLM-L6-v2, of Dense modules as EmbeddingGemma's (384 to 1536 to 256, the second's a tanh)", "sentence-transformers/all-MiniLM-L6-v2", "1110a243fdf4706b3f48f1d95db1a4f5529b4d41",
		[]string{"1_Pooling/config.json", "config.json", "config_sentence_transformers.json", "sentence_bert_config.json", "special_tokens_map.json", "tokenizer.json", "tokenizer_config.json", "vocab.txt"},
		[]string{
			"4be450dde3b0273bb9787637cfbd28fe04a7ba6ab9d36ac48e92b11e350ffc23",
			"953f9c0d463486b10a6871cc2fd59f223b2c70184f49815e7efbcab5d8908b41",
			"061ca9d39661d6c6d6de5ba27f79a1cd5770ea247f8d46412a68a498dc5ac9f3",
			"fc1993fde0a95c24ec6c022539d41cf6e2f7c9721e5415d6fb6897472a9cd4b7",
			"303df45a03609e4ead04bc3dc1536d0ab19b5358db685b6f3da123d05ec200e3",
			"be50c3628f2bf5bb5e3a7f17b1f74611b2561a3a27eeab05e5aa30f411572037",
			"acb92769e8195aabd29b7b2137a9e6d6e25c476a4f15aa4355c233426c61576b",
			"07eced375cec144d27c900241f3e339478dec958f92fddbc551f295c992038a3",
		},
		map[string]string{
			"modules.json": `[{"idx": 0, "name": "0", "path": "", "type": "sentence_transformers.models.Transformer"},
				{"idx": 1, "name": "1", "path": "1_Pooling", "type": "sentence_transformers.models.Pooling"},
				{"idx": 2, "name": "2", "path": "2_Dense", "type": "sentence_transformers.models.Dense"},
				{"idx": 3, "name": "3", "path": "3_Dense", "type": "sentence_transformers.models.Dense"},
				{"idx": 4, "name": "4", "path": "4_Normalize", "type": "sentence_transformers.models.Normalize"}]`,
			"2_Dense/config.json": `{"in_features": 384, "out_features": 1536, "bias": false, "activation_function": "torch.nn.modules.linear.Identity"}`,
			"3_Dense/config.json": `{"in_features": 1536, "out_features": 256, "bias": true, "activation_function": "torch.nn.modules.activation.Tanh"}`,
		},
	},
	{
		"bge-small-en-v1.5 (BERT, the first token's, normalized)", "BAAI/bge-small-en-v1.5", "5c38ec7c405ec4b44b94cc5a9bb96e735b38267a",
		[]string{"1_Pooling/config.json", "config.json", "config_sentence_transformers.json", "modules.json", "sentence_bert_config.json", "special_tokens_map.json", "tokenizer.json", "tokenizer_config.json", "vocab.txt"},
		[]string{
			"d1caf60c96f5fba2157c0c26b76d80818fad6cf0b8eb5e73ec372ff9818eba5c",
			"094f8e891b932f2000c92cfc663bac4c62069f5d8af5b5278c4306aef3084750",
			"940d5f50db195fa6e5e6a4f122c095f77880de259d74b14a65779ed48bdd7c56",
			"84e40c8e006c9b1d6c122e02cba9b02458120b5fb0c87b746c41e0207cf642cf",
			"84e39fda68ccbff05bfa723ae9c0e70e23e2ec373b76e0f8c6e71af72a693cbf",
			"b6d346be366a7d1d48332dbc9fdf3bf8960b5d879522b7799ddba59e76237ee3",
			"d241a60d5e8f04cc1b2b3e9ef7a4921b27bf526d9f6050ab90f9267a1f9e5c66",
			"9261e7d79b44c8195c1cada2b453e55b00aeb81e907a6664974b4d7776172ab3",
			"07eced375cec144d27c900241f3e339478dec958f92fddbc551f295c992038a3",
		},
		nil,
	},
}

// Real text embedding models' stubs, in their folders in place of their
// weights and config, embed in sentence-transformers (as their modules
// pool and normalize) and in transformers (AutoModel, its tokens' mean) as
// their mappings say, exactly.
func TestSentenceTransformersEmbedsAsTheStubSays(t *testing.T) {
	ctx := context.Background()
	rt, err := transformersrun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, n := range modelstest.EmbeddingNotes {
		texts = append(texts, n.Text)
	}
	var jobs []transformersrun.Job
	var vectors [][3][]float64
	for _, m := range realEncoders {
		app := t.TempDir()
		for i, name := range m.files {
			b, err := os.ReadFile(modelstest.Fetch(t, "https://huggingface.co/"+m.repo+"/resolve/"+m.commit+"/"+name, m.sums[i]))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(app, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for name, text := range m.dense {
			path := filepath.Join(app, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
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
		ms, vs := modelstest.EmbeddingMappings(int(c.Outputs[0].Shape[0].Size))
		modelstest.WriteMappings(t, dir, ms)
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
			if err := os.WriteFile(filepath.Join(app, filepath.FromSlash(name)), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("%s: a stub of %d KB", m.name, len(model.File)/1024)
		jobs = append(jobs, transformersrun.Job{Model: app, Embed: texts})
		vectors = append(vectors, vs)
	}
	results, err := rt.Run(ctx, jobs)
	if err != nil {
		t.Fatal(err)
	}
	for j, r := range results {
		for _, way := range []struct {
			name string
			got  [][]float64
		}{{"sentence-transformers", r.Sentences}, {"AutoModel's mean", r.Means}} {
			for i, n := range modelstest.EmbeddingNotes {
				got, want := way.got[i], vectors[j][n.Mapping]
				if realEncoders[j].dense != nil && way.name != "sentence-transformers" {
					// The transformer's own: the embedding, then 0s, which
					// the Dense modules leave out.
					want = append(slices.Clone(want), make([]float64, len(got)-len(want))...)
				}
				if len(got) != len(want) {
					t.Errorf("%s, %s: %q: an embedding of %d values, want %d", realEncoders[j].name, way.name, n.Text, len(got), len(want))
					continue
				}
				for k := range want {
					if math.Abs(got[k]-want[k]) > 1e-6 {
						t.Errorf("%s, %s: %q: value %d is %v, want %v (mapping %d's)", realEncoders[j].name, way.name, n.Text, k, got[k], want[k], n.Mapping)
						break
					}
				}
			}
		}
	}
}

// EmbeddingGemma's sentence-transformers folder (its configs and
// tokenizer, pinned by their SHA-256) is read: its embedding is of its last
// Dense module's features, and its contract has its Dense modules.
func TestADenseModelsContractIsRead(t *testing.T) {
	const repo, commit = "unsloth/embeddinggemma-300m", "bfa3c846ac738e62aa61806ef9112d34acb1dc5a"
	files := []string{"config.json", "modules.json", "1_Pooling/config.json", "2_Dense/config.json", "3_Dense/config.json", "tokenizer.json"}
	sums := []string{
		"8f863f76e2d9c710cc833dc92efa898c9adfd41031c786507cc6b0e49c2e3e68",
		"5b5649645fb756dad1a8e2efe7872d3bb32bc00b93c95f276dd17f474eedccdc",
		"35bbd47d7fdf1e378db6130bcc668b09d1aa67a7bbf7c8f89a9c71f4cc8ebcc6",
		"0661e5e0b67b8f8408ab31ab5d073a78972fc1dc24a49992a64796557e4f9e53",
		"8c4575c49353d63fb907878856ba94384635c3b2711fd5b7439e7f71888c66fc",
		"6852f8d561078cc0cebe70ca03c5bfdd0d60a45f9d2e0e1e4cc05b68e9ec329e",
	}
	app := t.TempDir()
	for i, name := range files {
		b, err := os.ReadFile(modelstest.Fetch(t, "https://huggingface.co/"+repo+"/resolve/"+commit+"/"+name, sums[i]))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(app, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := ReadContract(filepath.Join(app, "model.safetensors"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Task != "embedding" || c.Outputs[0].Shape[0].Size != 768 {
		t.Errorf("task %s, output %+v", c.Task, c.Outputs)
	}
	var d encoderDetails
	if err := json.Unmarshal(c.Format.Details, &d); err != nil {
		t.Fatal(err)
	}
	want := []dense{{Path: "2_Dense", In: 768, Out: 3072}, {Path: "3_Dense", In: 3072, Out: 768}}
	if d.HiddenSize != 768 || d.VocabSize != 262144 || !slices.Equal(d.Dense, want) {
		t.Errorf("details %+v", d)
	}
}
