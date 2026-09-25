package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lint"
	"github.com/nimbusxr/axx/internal/version"
)

func newLintCmd(app *App) *cobra.Command {
	var cf configFlags
	var formats []string
	var mode string
	cmd := &cobra.Command{
		Use:   "lint [paths...]",
		Short: "Check test-data isolation: values that must stay unique across seeds, fixtures and features",
		Long: `Run the test-data isolation rules from the lint section of axx.yaml: each
rule extracts values (ids, keys, names) from the files it selects, with a
regular expression or a JSON path, and reports values that repeat where they
must be unique, so scenarios sharing a database can run in parallel. Builtin
checks also warn about SQL selection and trigger ordinals that cannot work.

With paths, only findings touching those files or directories are reported
(every file is still scanned: a value collides with occurrences anywhere).

Formats (repeatable, NAME or NAME:FILE; FILE is relative to axx.yaml):
  human   grouped by rule, with file:line and the colliding value (default)
  json    the --json envelope
  junit   one test case per rule
  sarif   SARIF 2.1.0 for code scanning (paths relative to the repository)
  github  GitHub Actions ::error/::warning annotations

Exit codes: 0 clean (or only warnings, e.g. rules in warn mode), 2 invalid
configuration, 3 violations.`,
		Example: `  axx lint
  axx lint seeds/manifest-kestrel.yaml
  axx lint --format github --format sarif:build/axx/lint.sarif
  axx lint --mode warn`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode != "" && mode != lint.ModeError && mode != lint.ModeWarn {
				return axxerr.New(lint.CodeOption, exitcode.Usage, "invalid --mode %q", mode).WithHint("use --mode error or --mode warn")
			}
			type output struct{ name, path string }
			var outs []output
			for _, f := range formats {
				name, path, _ := strings.Cut(f, ":")
				if !slices.Contains(lint.Formats, name) {
					return axxerr.New(lint.CodeOption, exitcode.Usage, "unknown lint format %q", name).
						WithHint("use one of: %s (optionally NAME:FILE)", strings.Join(lint.Formats, ", "))
				}
				outs = append(outs, output{name, path})
			}
			cfg, err := app.loadConfig(&cf)
			if err != nil {
				return err
			}
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			rep, err := lint.Project(cmd.Context(), cfg, lint.Options{WorkDir: wd, Mode: mode, Paths: args})
			if err != nil {
				return err
			}
			root := lint.RepoRoot(wd, app.Env)
			render := func(name string, w io.Writer) error {
				switch name {
				case "json":
					return encodeEnvelope(w, Envelope{SchemaVersion: EnvelopeSchemaVersion, Command: app.command, OK: rep.OK(), Data: rep})
				case "junit":
					return lint.WriteJUnit(w, rep)
				case "sarif":
					return lint.WriteSARIF(w, rep, lint.SARIFOptions{Version: version.Get().Version, Root: root})
				case "github":
					return lint.WriteGitHub(w, rep, root)
				default:
					return lint.WriteHuman(w, rep, lint.HumanOptions{Compact: app.Compact})
				}
			}
			stdoutUsed := false
			for _, o := range outs {
				if o.path == "" {
					if app.JSON {
						continue // stdout carries the JSON envelope
					}
					stdoutUsed = true
					if err := render(o.name, app.Stdout); err != nil {
						return err
					}
					continue
				}
				if err := writeLintFile(cfg.Dir, o.path, func(w io.Writer) error { return render(o.name, w) }); err != nil {
					return err
				}
			}
			if app.JSON {
				if err := app.EmitResult(rep, rep.OK(), nil); err != nil {
					return err
				}
			} else if !stdoutUsed {
				if err := render("human", app.Stdout); err != nil {
					return err
				}
			}
			if !rep.OK() {
				return silentExit{code: exitcode.Undefined}
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&formats, "format", "f", nil, "output NAME or NAME:FILE (repeatable): "+strings.Join(lint.Formats, ", "))
	cmd.Flags().StringVar(&mode, "mode", "", "override the mode of every rule: error or warn")
	cf.register(cmd)
	return cmd
}

// writeLintFile writes one --format NAME:FILE output (FILE relative to dir).
func writeLintFile(dir, path string, write func(io.Writer) error) error {
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := write(&buf); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return axxerr.Wrap(err, lint.CodeOption, exitcode.Usage, "cannot write %s", relPath(path))
	}
	return nil
}
