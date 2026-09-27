package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/packs/all"
)

func parcelSeed(ref string) string {
	return "parcels.parcels:\n  - reference: \"" + ref + "\"\n    sender: \"kestrel-books\"\n    weight_grams: 1200\n"
}

func TestFixtureHints(t *testing.T) {
	seeds := map[string]string{
		"seeds/portal-find.yaml":  parcelSeed("PX-WEB-5151"),
		"seeds/portal-open.yaml":  parcelSeed("PX-WEB-5141"),
		"seeds/portal-track.yaml": parcelSeed("PX-WEB-5161"),
	}
	steps := []string{"a seeds/portal-find.yaml db seed", "a seeds/portal-open.yaml db seed", "a seeds/portal-track.yaml db seed"}
	with := func(base map[string]string, extra map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	tests := []struct {
		name      string
		files     map[string]string
		steps     []string
		generated string   // a file a factory generates
		paths     []string // axx lint's paths
		want      string   // the hint, "" for none
	}{
		{
			name: "three seeds of one shape", files: seeds, steps: steps,
			want: "seeds/ has 3 hand-written .yaml files of one shape (parcels.parcels) that no fixture factory generates; a factory would keep what they share in one place (optional; `axx fixtures adopt --help`)",
		},
		{name: "two are too few", files: seeds, steps: steps[:2]},
		{name: "files the scenarios do not use", files: with(seeds, map[string]string{"seeds/unused.yaml": parcelSeed("PX-WEB-5171")}), steps: steps[:2]},
		{name: "a factory generates one of them", files: seeds, steps: steps, generated: "seeds/portal-track.yaml"},
		{name: "different shapes", files: with(seeds, map[string]string{"seeds/portal-track.yaml": "parcels.manifest_lines:\n  - id: \"ML-1\"\n"}), steps: steps},
		{name: "different directories", files: map[string]string{
			"seeds/a/one.yaml": parcelSeed("PX-1"), "seeds/b/two.yaml": parcelSeed("PX-2"), "seeds/c/three.yaml": parcelSeed("PX-3"),
		}, steps: []string{"a seeds/a/one.yaml db seed", "a seeds/b/two.yaml db seed", "a seeds/c/three.yaml db seed"}},
		{name: "not a mapping", files: map[string]string{
			"seeds/one.yaml": "- a\n", "seeds/two.yaml": "- b\n", "seeds/three.yaml": "- c\n",
		}, steps: []string{"a seeds/one.yaml db seed", "a seeds/two.yaml db seed", "a seeds/three.yaml db seed"}},
		{
			name: "JSON documents", files: map[string]string{
				"mongo/scans-1.json": `{"scans": [{"scanId": "SC-1"}]}`, "mongo/scans-2.json": `{"scans": [{"scanId": "SC-2"}]}`, "mongo/scans-3.json": `{"scans": []}`,
			}, steps: []string{"a mongo/scans-1.json mongo db seed", "a mongo/scans-2.json mongo db seed", "a mongo/scans-3.json mongo db seed"},
			want: "mongo/ has 3 hand-written .json files of one shape (scans) that no fixture factory generates; a factory would keep what they share in one place (optional; `axx fixtures adopt --help`)",
		},
		{name: "linting other paths", files: seeds, steps: steps, paths: []string{"features"}},
		{
			name: "linting their directory", files: seeds, steps: steps, paths: []string{"seeds"},
			want: "seeds/ has 3 hand-written .yaml files of one shape (parcels.parcels) that no fixture factory generates; a factory would keep what they share in one place (optional; `axx fixtures adopt --help`)",
		},
	}
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				p := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			src := "Feature: portal\n\n  Scenario: seeds\n"
			for _, s := range tt.steps {
				src += "    Given " + s + "\n"
			}
			_, pickles, err := feature.ParseSource("portal.feature", []byte(src), messages.UUID{}.NewId)
			if err != nil {
				t.Fatal(err)
			}
			sources := FixtureSources{
				Resolve: func(p string) (string, error) {
					abs := filepath.Join(dir, filepath.FromSlash(p))
					_, err := os.Stat(abs)
					return abs, err
				},
				Generated: func(abs string) bool {
					return tt.generated != "" && abs == filepath.Join(dir, filepath.FromSlash(tt.generated))
				},
			}
			got := strings.Join(FixtureHints(e.Registry, pickles, sources, Options{WorkDir: dir, Paths: tt.paths}), "\n")
			if got != tt.want {
				t.Errorf("hints:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
