package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/fixtures"
	"github.com/nimbusxr/axx/internal/packset"
	"github.com/nimbusxr/axx/internal/render/md"
)

func newDocsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate reference documentation from the installed version",
	}
	var out string
	var frontmatter bool
	var cf configFlags
	export := &cobra.Command{
		Use:   "export",
		Short: "Write generated reference pages (packs, parameter types, error codes, schema) to a directory",
		Long: `Write the reference documentation generated from this binary: one page per
pack (its settings, steps, parameter types and agent tools) and an overview of
them, parameter types, a grep-friendly step index, the error and exit codes,
and the JSON Schemas of axx.yaml and the fixture spec files. In a project, it
covers the project's packs, those axx-packs.yaml lists, custom packs included;
elsewhere, the packs compiled into this axx (the documentation site is built
this way).`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			if out == "" {
				return axxerr.New("AXX-E0005", exitcode.Usage, "--out is required")
			}
			cfg, err := app.loadConfig(&cf)
			if err != nil {
				return err
			}
			opts := engine.Options{Config: cfg, Logger: app.logger()}
			if cfg.File == "" {
				opts = engine.Options{Config: &config.Config{}, Packs: engine.CompiledPacks()}
			}
			e, err := engine.New(opts)
			if err != nil {
				return err
			}
			if !slices.ContainsFunc(e.PackNames(), func(n string) bool { return n != "core" }) {
				return noPacksToDocument(cfg)
			}
			files, err := referenceFiles(e, frontmatter)
			if err != nil {
				return err
			}
			var written []string
			for _, name := range sortedKeys(files) {
				p := filepath.Join(out, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(p, []byte(files[name]), 0o644); err != nil {
					return err
				}
				written = append(written, p)
			}
			return app.Emit(map[string]any{"files": written}, func(w io.Writer) error {
				_, err := fmt.Fprintf(w, "wrote %s to %s\n", plural(len(written), "file"), out)
				return err
			})
		},
	}
	cf.register(export)
	export.Flags().StringVarP(&out, "out", "o", "", "output directory")
	export.Flags().BoolVar(&frontmatter, "frontmatter", true, "emit YAML front matter (for the docs site)")
	cmd.AddCommand(export)
	return cmd
}

// packsLink is where the site publishes the pack pages.
const packsLink = "/references/packs/"

// referenceFiles renders every generated reference file, keyed by relative path.
// noPacksToDocument says why there is nothing to export: no project here,
// or a project that lists no packs.
func noPacksToDocument(cfg *config.Config) error {
	if cfg.File == "" {
		return axxerr.New("AXX-E0014", exitcode.Usage, "there are no packs to document: this is not an axx project, and this axx has no packs built in").
			WithHint("run it in your project's directory, where axx.yaml is; axx's own packs are documented at https://axx.nimbusxr.us/references/packs/")
	}
	return axxerr.New("AXX-E0014", exitcode.Usage, "there are no packs to document: the project lists none").
		WithHint("add the packs your steps come from with `axx pack add rest sql ...` (`axx pack list` lists them)")
}

func referenceFiles(e *engine.Engine, frontmatter bool) (map[string]string, error) {
	files := map[string]string{}
	manifests := e.Manifests()
	for _, name := range e.PackNames() {
		page, err := md.PackPage(e.Registry, md.PackInfo{Name: name, Manifest: manifests[name], Builtin: name == "core"}, packsLink, frontmatter)
		if err != nil {
			return nil, err
		}
		files["packs/"+name+".md"] = page
	}
	overview, err := md.PacksOverview(packGroups(manifests), packsLink, frontmatter)
	if err != nil {
		return nil, err
	}
	files["packs/index.md"] = overview
	params, err := md.ParamsPage(e.Registry, frontmatter)
	if err != nil {
		return nil, err
	}
	files["parameter-types.md"] = params
	index, err := md.StepIndex(e.Registry)
	if err != nil {
		return nil, err
	}
	files["step-index.md"] = index
	codes, err := md.ErrorCodesPage(frontmatter)
	if err != nil {
		return nil, err
	}
	files["error-codes.md"] = codes
	files["schemas/axx.schema.json"] = string(config.SchemaJSON)
	for name, schema := range fixtures.Schemas() {
		files["schemas/"+name] = string(schema)
	}
	return files, nil
}

// packGroups are the packs axx publishes and this axx has, by group, as the
// site's sidebar has them: the core and the packs of no group, then the web
// packs and each cloud's, each group with its own core first.
func packGroups(manifests map[string]core.Manifest) []md.PackGroup {
	groups := []md.PackGroup{{}, {Title: "Web"}, {Title: "AWS"}, {Title: "Google Cloud"}, {Title: "Azure"}}
	if m, ok := manifests["core"]; ok {
		groups[0].Packs = append(groups[0].Packs, md.PackSummary{Name: "core", Summary: strings.TrimSuffix(m.Doc, ".")})
	}
	for _, p := range packset.Catalog {
		if _, ok := manifests[p.Name]; !ok {
			continue
		}
		g := 0
		switch {
		case strings.HasPrefix(p.Name, "web-"):
			g = 1
		case strings.HasPrefix(p.Name, "aws-"):
			g = 2
		case strings.HasPrefix(p.Name, "gcp-"):
			g = 3
		case strings.HasPrefix(p.Name, "azure-"):
			g = 4
		}
		groups[g].Packs = append(groups[g].Packs, md.PackSummary{Name: p.Name, Summary: p.Summary})
	}
	return groups
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
