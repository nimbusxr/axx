package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/fixtures"
	"github.com/nimbusxr/axx/internal/render/md"
)

func newDocsCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate reference documentation from the installed version",
	}
	var out string
	var frontmatter bool
	export := &cobra.Command{
		Use:   "export",
		Short: "Write generated reference pages (steps, parameter types, error codes, schema) to a directory",
		Long: `Write the reference documentation generated from this binary: one page per
step pack, parameter types, a grep-friendly step index, the error and exit
codes, and the JSON Schemas of axx.yaml and the fixture spec files. The documentation site and the agent skills are built from this output.
It covers the packs compiled into this axx, whatever the project lists.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			if out == "" {
				return axxerr.New("AXX-E0005", exitcode.Usage, "--out is required")
			}
			e, err := engine.New(engine.Options{Config: &config.Config{}, Packs: engine.CompiledPacks()})
			if err != nil {
				return err
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
	export.Flags().StringVarP(&out, "out", "o", "", "output directory")
	export.Flags().BoolVar(&frontmatter, "frontmatter", true, "emit YAML front matter (for the docs site)")
	cmd.AddCommand(export)
	return cmd
}

// referenceFiles renders every generated reference file, keyed by relative path.
func referenceFiles(e *engine.Engine, frontmatter bool) (map[string]string, error) {
	files := map[string]string{}
	manifests := e.Manifests()
	for _, name := range e.PackNames() {
		m := manifests[name]
		if len(m.Steps) == 0 {
			continue // parameter-only packs (core) appear on the parameter types page
		}
		page, err := md.StepsPage(e.Registry, md.PackInfo{Name: name, Manifest: m}, frontmatter)
		if err != nil {
			return nil, err
		}
		files["steps/"+name+".md"] = page
	}
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

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
