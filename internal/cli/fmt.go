package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/featurefmt"
)

// CodeFmtWrite is a feature file `axx fmt` laid out but cannot write.
const CodeFmtWrite = "AXX-E0209"

// FmtReport is the JSON result of `axx fmt`.
type FmtReport struct {
	// Files is how many feature files it read.
	Files int `json:"files"`
	// Changed are the files it laid out, or, with --check, those it would.
	Changed []string `json:"changed"`
	Check   bool     `json:"check,omitempty"`
}

func newFmtCmd(app *App) *cobra.Command {
	var cf configFlags
	var check bool
	cmd := &cobra.Command{
		Use:   "fmt [paths...]",
		Short: "Lay out feature files the one way: indentation, aligned tables, blank lines",
		Long: `Rewrite feature files in place, laid out the one way axx lays them out, so that
every feature reads alike, whoever or whatever wrote it. Without paths, the features
axx run reads (run.paths in axx.yaml, or features/); with paths, those files and the
feature files in those directories.

The layout: the Feature at the left edge; a Background, Scenario or Rule two spaces
in, and what a Rule holds two more; steps and Examples two spaces in from their
scenario; a step's table or doc string, and an Examples table, two more. Tables'
columns line up. Tags sit on their own lines, a comment at the indent of the line it
is above, and one blank line comes before each scenario. Only where things sit
changes: keywords (in any language), steps, names, cells, comments and doc strings
stay as written. axx fmt reads what it would write and leaves a file alone if it
would read differently.

--check changes nothing: it lists the files that are not formatted, and fails.
VS Code and IntelliJ lay out a feature the same way, through axx lsp (Format
Document, Reformat Code).

Exit codes: 0 formatted (with --check: nothing to change), 2 a feature that does not
parse, 3 (--check) features that are not formatted.`,
		Example: `  axx fmt
  axx fmt features/claims
  axx fmt --check`,
		RunE: func(_ *cobra.Command, args []string) error {
			files, err := fmtFiles(app, &cf, args)
			if err != nil {
				return err
			}
			rep := FmtReport{Files: len(files), Changed: []string{}, Check: check}
			var errs []error
			for _, f := range files {
				name := relPath(f)
				src, err := os.ReadFile(f)
				if err != nil {
					errs = append(errs, axxerr.Wrap(err, feature.CodeNotFound, exitcode.Usage, "cannot read %s", name))
					continue
				}
				out, err := featurefmt.Format(name, src)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				if bytes.Equal(out, src) {
					continue
				}
				if !check {
					if err := writeFeature(f, out); err != nil {
						errs = append(errs, axxerr.Wrap(err, CodeFmtWrite, exitcode.Usage, "cannot write %s", name).
							WithHint("check that you can write the file; it is left as it was"))
						continue
					}
				}
				rep.Changed = append(rep.Changed, name)
			}
			human := func(w io.Writer) error { return renderFmt(w, rep) }
			if err := fmtError(errs); err != nil {
				return app.emitWithError(rep, human, err)
			}
			ok := !check || len(rep.Changed) == 0
			if err := app.EmitResult(rep, ok, human); err != nil {
				return err
			}
			if !ok {
				return silentExit{code: exitcode.Undefined}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "change nothing: list the files that are not formatted, and fail if there are any")
	cf.register(cmd)
	return cmd
}

// fmtFiles are the feature files axx fmt lays out: those under the paths
// given, from the working directory, or else those axx run reads.
func fmtFiles(app *App, cf *configFlags, args []string) ([]string, error) {
	var paths []string
	base := ""
	if len(args) > 0 {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		base = wd
		paths = args
	} else {
		cfg, err := app.loadConfig(cf)
		if err != nil {
			return nil, err
		}
		base = cfg.Dir
		paths = cfg.Run.Paths
		if len(paths) == 0 {
			paths = []string{"features"}
		}
	}
	files, err := feature.Files(paths, base)
	var ae *axxerr.Error
	if errors.As(err, &ae) && ae.Code == feature.CodeNotFound {
		ae.Hint = "check the paths given to `axx fmt`, or run.paths in axx.yaml"
	}
	return files, err
}

// writeFeature writes a laid out feature over the file, keeping its mode.
func writeFeature(path string, out []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if p, err := filepath.EvalSymlinks(path); err == nil {
		path = p
	}
	return os.WriteFile(path, out, mode)
}

// fmtError is the error of the files axx fmt could not format: one, or
// all of them in one.
func fmtError(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	}
	msgs := make([]string, len(errs))
	for i, err := range errs {
		msgs[i] = strings.ReplaceAll(err.Error(), "\n", "\n  ")
	}
	var first *axxerr.Error
	if !errors.As(errs[0], &first) {
		return errors.Join(errs...)
	}
	e := axxerr.New(first.Code, first.Exit, "%d feature files could not be formatted:\n  %s", len(errs), strings.Join(msgs, "\n  "))
	e.Hint = first.Hint
	return e
}

func renderFmt(w io.Writer, rep FmtReport) error {
	for _, f := range rep.Changed {
		if rep.Check {
			fmt.Fprintf(w, "%s is not formatted\n", f)
		} else {
			fmt.Fprintf(w, "formatted %s\n", f)
		}
	}
	files := plural(rep.Files, "feature file")
	var err error
	switch {
	case len(rep.Changed) == 0:
		_, err = fmt.Fprintf(w, "%s, all formatted\n", files)
	case rep.Check:
		_, err = fmt.Fprintf(w, "%s, %d not formatted: axx fmt formats them\n", files, len(rep.Changed))
	default:
		_, err = fmt.Fprintf(w, "%s, %d formatted\n", files, len(rep.Changed))
	}
	return err
}
