package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/fixtures"
)

func newFixturesCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fixtures",
		Short: "Generate, check and adopt schema-governed fixture files (the fixture factory)",
		Long: `The fixture factory expands *.factory.yaml, *.fixture.yaml and *.prototype.yaml
sources into fixture files (Avro/JSON/YAML/XML/protobuf payloads, database seed
datasets) that are validated against their schemas, and records what it owns in
axx-fixtures.manifest.yaml. Only files recorded there are ever touched; a managed
file whose content no longer matches its sha256 is a refused hand-edit.

Configure it in the fixtures section of axx.yaml. The spec file formats:
axx schema --kind factory|fixture|prototype.

Exit codes: 0 ok, 1 drift or a failed check/generation, 2 invalid configuration
or spec files, 4 git unavailable (untrack).`,
	}
	cmd.AddCommand(
		newFixturesGenerateCmd(app),
		newFixturesCheckCmd(app),
		newFixturesAdoptCmd(app),
		newFixturesCleanCmd(app),
		newFixturesUntrackCmd(app),
	)
	return cmd
}

// fixturesConfig resolves the fixtures section of axx.yaml.
func fixturesConfig(cfg *config.Config) fixtures.Config {
	fc := cfg.Fixtures
	if fc == nil {
		fc = &config.Fixtures{}
	}
	out := fixtures.Config{
		BaseDir:       cfg.FixturesDir(),
		Factories:     fc.Factories,
		Sources:       fc.Sources,
		LintEmit:      fc.Lint.Emit == nil || *fc.Lint.Emit,
		LintOutput:    fc.Lint.Output,
		OutputIgnored: fc.Output.Ignored,
	}
	for _, r := range fc.Conformance {
		out.Conformance = append(out.Conformance, fixtures.ConformanceRule{
			Name: r.Name, FilePatterns: r.FilePatterns, SchemaType: r.SchemaType, SchemaRef: r.SchemaRef,
		})
	}
	return out
}

func (a *App) loadFixturesConfig(cf *configFlags) (fixtures.Config, error) {
	cfg, err := a.loadConfig(cf)
	if err != nil {
		return fixtures.Config{}, err
	}
	return fixturesConfig(cfg), nil
}

// fixturesOptions are the engine extensions: none yet. Families and
// expression functions are internal interfaces a plugin capability
// (factory.family / factory.function) can back later.
func fixturesOptions() fixtures.Options { return fixtures.Options{} }

// GenerateReport is the JSON result of `axx fixtures generate`.
type GenerateReport struct {
	BaseDir   string   `json:"baseDir"`
	DryRun    bool     `json:"dryRun,omitempty"`
	Written   []string `json:"written"`
	Unchanged []string `json:"unchanged"`
}

func newFixturesGenerateCmd(app *App) *cobra.Command {
	var cf configFlags
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Expand the factory specs into fixture files, the manifest and generated lint rules",
		Long: `Expand every factory spec into canonical fixture bytes and write the files that
changed, the managed .gitignore entries for ignored outputs, the pairings lock of
recorded expression functions, the generated lint rules and the manifest.

A managed file whose content no longer matches the manifest is a hand-edit:
generate refuses to overwrite it (lift the change into the spec, or re-adopt the
file). There is no force option that discards edits.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			fc, err := app.loadFixturesConfig(&cf)
			if err != nil {
				return err
			}
			g, err := fixtures.NewGenerator(fc, fixturesOptions())
			if err != nil {
				return err
			}
			var res *fixtures.GenerationResult
			if dryRun {
				res, err = g.Plan()
			} else {
				res, err = g.Generate()
			}
			if err != nil {
				return err
			}
			rep := GenerateReport{BaseDir: relPath(fc.BaseDir), DryRun: dryRun, Written: res.Written, Unchanged: res.Unchanged}
			return app.Emit(rep, func(w io.Writer) error {
				verb := "wrote"
				if dryRun {
					verb = "would write"
				}
				if !app.Compact {
					for _, p := range res.Written {
						fmt.Fprintf(w, "  %s %s\n", verb, p)
					}
				}
				_, err := fmt.Fprintf(w, "%s %s, %d unchanged\n", verb, plural(len(res.Written), "file"), len(res.Unchanged))
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be written without writing anything")
	cf.register(cmd)
	return cmd
}

// CheckReport is the JSON result of `axx fixtures check`.
type CheckReport struct {
	BaseDir  string             `json:"baseDir"`
	Checks   int                `json:"checks"`
	Failures []fixtures.Failure `json:"failures"`
}

func newFixturesCheckCmd(app *App) *cobra.Command {
	var cf configFlags
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Verify committed fixtures match their specs (drift) and conform to their schemas",
		Long: `Read-only verification: every managed file must equal what the factories
generate, the manifest must list exactly what they produce, and every file a
conformance rule matches must pass its family's schema oracle. Recorded
expression functions resolve from the committed pairings lock only.

Exit codes: 0 all checks pass, 1 any failure (drift fails CI), 2 invalid
configuration or spec files.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			fc, err := app.loadFixturesConfig(&cf)
			if err != nil {
				return err
			}
			checker, err := fixtures.NewChecker(fc, fixturesOptions())
			if err != nil {
				return err
			}
			total, failures := checker.Run()
			if failures == nil {
				failures = []fixtures.Failure{}
			}
			rep := CheckReport{BaseDir: relPath(fc.BaseDir), Checks: total, Failures: failures}
			if err := app.EmitResult(rep, len(failures) == 0, func(w io.Writer) error { return renderCheck(w, rep) }); err != nil {
				return err
			}
			if len(failures) > 0 {
				return silentExit{code: exitcode.Failed}
			}
			return nil
		},
	}
	cf.register(cmd)
	return cmd
}

func renderCheck(w io.Writer, rep CheckReport) error {
	for _, f := range rep.Failures {
		fmt.Fprintf(w, "FAIL %s [%s]\n", f.Check, f.Code)
		for _, line := range strings.Split(strings.TrimRight(f.Message, "\n"), "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	var err error
	switch {
	case len(rep.Failures) == 0:
		_, err = fmt.Fprintf(w, "%s passed\n", plural(rep.Checks, "check"))
	case rep.Checks == 0:
		_, err = fmt.Fprintf(w, "the specs could not be expanded; no checks ran\n")
	default:
		_, err = fmt.Fprintf(w, "%d of %s failed\n", len(rep.Failures), plural(rep.Checks, "check"))
	}
	return err
}

func newFixturesAdoptCmd(app *App) *cobra.Command {
	var cf configFlags
	var family, schema, files, factory, into string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "adopt",
		Short: "Turn existing fixture files into factory sources, preserving every value",
		Long: `Adopt hand-written fixture files: the prototype takes the modal value of every
field, each file becomes a *.fixture.yaml carrying only its deltas, and string
fields distinct across every file are proposed as identities. Before anything is
written the new spec is expanded and every file must decode equal to its
original; otherwise adoption refuses and writes nothing.

  axx fixtures adopt --schema schemas/order.avsc --files 'kafka/orders/*.json' --factory orders
  axx fixtures adopt --into orders --files 'features/refunds/*.json'

Paths and globs are relative to fixtures.baseDir.`,
		Example: `  axx fixtures adopt --family json --schema openapi/api.yaml#/components/schemas/Order \
    --files 'mocks/orders/*.json' --factory order-bodies`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			if files == "" {
				return adoptUsage("adopt requires --files")
			}
			if into != "" {
				for name, v := range map[string]string{"schema": schema, "factory": factory, "family": family} {
					if v != "" {
						return adoptUsage("adopt --into takes them from the existing factory - drop --%s", name)
					}
				}
			} else {
				for _, req := range []struct{ name, v string }{{"schema", schema}, {"factory", factory}} {
					if req.v == "" {
						return adoptUsage("adopt requires --schema, --files and --factory (missing --%s)", req.name)
					}
				}
			}
			fc, err := app.loadFixturesConfig(&cf)
			if err != nil {
				return err
			}
			a, err := fixtures.NewAdopter(fc, fixturesOptions())
			if err != nil {
				return err
			}
			var res *fixtures.AdoptionResult
			if into != "" {
				res, err = a.AdoptInto(into, files, dryRun)
			} else {
				if family == "" {
					family = "avro"
				}
				res, err = a.Adopt(family, schema, files, factory, dryRun)
			}
			if err != nil {
				return err
			}
			return app.Emit(res, func(w io.Writer) error {
				for _, line := range res.Report {
					if _, err := fmt.Fprintf(w, "  %s\n", line); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&family, "family", "", "fixture family of the adopted files (default avro)")
	cmd.Flags().StringVar(&schema, "schema", "", "governing schema, relative to fixtures.baseDir")
	cmd.Flags().StringVar(&files, "files", "", "glob of the files to adopt, relative to fixtures.baseDir")
	cmd.Flags().StringVar(&factory, "factory", "", "name of the new factory (written next to the adopted files)")
	cmd.Flags().StringVar(&into, "into", "", "adopt into this existing factory (name or root-relative path)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "verify the adoption and report without writing anything")
	cf.register(cmd)
	return cmd
}

func adoptUsage(format string, args ...any) error {
	return axxerr.New("AXX-E0909", exitcode.Usage, format, args...).
		WithHint("`axx fixtures adopt --help` shows the two forms: --schema/--files/--factory, or --into/--files")
}

func newFixturesCleanCmd(app *App) *cobra.Command {
	var cf configFlags
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Delete ignored generated outputs from disk (generate rematerializes them)",
		Long: `Delete the ignored outputs (the gitignored derivations of factory sources) from
disk. Committed outputs, sources, the manifest, the pairings lock and the managed
.gitignore files are never touched; directories the deletion empties are removed.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			fc, err := app.loadFixturesConfig(&cf)
			if err != nil {
				return err
			}
			g, err := fixtures.NewGenerator(fc, fixturesOptions())
			if err != nil {
				return err
			}
			res, err := g.Clean(dryRun)
			if err != nil {
				return err
			}
			return app.Emit(res, func(w io.Writer) error {
				verb := "deleted"
				if dryRun {
					verb = "would delete"
				}
				_, err := fmt.Fprintf(w, "%s %s, kept %s\n", verb, plural(len(res.Deleted), "ignored output"), plural(len(res.SkippedCommitted), "committed output"))
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would be deleted without deleting")
	cf.register(cmd)
	return cmd
}

func newFixturesUntrackCmd(app *App) *cobra.Command {
	var cf configFlags
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "untrack",
		Short: "Take ignored outputs that are still tracked out of the git index (git rm --cached)",
		Long: `The one-time step after outputs become ignored: .gitignore rules only govern
untracked files, so previously committed copies must leave the index. Runs
git rm --cached for ignored outputs that are still tracked. Working-tree files are
never touched and the removal is staged, not committed; re-running is harmless.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			fc, err := app.loadFixturesConfig(&cf)
			if err != nil {
				return err
			}
			g, err := fixtures.NewGenerator(fc, fixturesOptions())
			if err != nil {
				return err
			}
			res, err := g.Untrack(cmd.Context(), dryRun)
			if err != nil {
				return err
			}
			return app.Emit(res, func(w io.Writer) error {
				verb := "untracked"
				if dryRun {
					verb = "would untrack"
				}
				_, err := fmt.Fprintf(w, "%s %s (git rm --cached, staged for your commit), %d already untracked\n",
					verb, plural(len(res.Untracked), "file"), res.AlreadyUntracked)
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list what would be untracked without touching the index")
	cf.register(cmd)
	return cmd
}
