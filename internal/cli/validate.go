package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/check"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/lint"
	"github.com/nimbusxr/axx/internal/match"
)

// Problem is one validation finding.
type Problem struct {
	Kind        string             `json:"kind"` // undefined | ambiguous | argument
	Location    string             `json:"location"`
	Text        string             `json:"text"`
	Message     string             `json:"message,omitempty"`
	Suggestions []match.Suggestion `json:"suggestions,omitempty"`
	Candidates  []string           `json:"candidates,omitempty"`
}

// ValidationReport is the JSON result of `axx validate`.
type ValidationReport struct {
	Files     int       `json:"files"`
	Scenarios int       `json:"scenarios"`
	Steps     int       `json:"steps"`
	Problems  []Problem `json:"problems"`
	// Warnings come from the builtin feature checks of `axx lint`; they do
	// not change the exit code.
	Warnings []Problem `json:"warnings,omitempty"`
}

func newValidateCmd(app *App) *cobra.Command {
	var cf configFlags
	cmd := &cobra.Command{
		Use:   "validate [paths...]",
		Short: "Check feature files without running them: syntax, undefined and ambiguous steps",
		Long: `Parse feature files and match every step against the available step
definitions, without starting applications or executing anything.

Also prints the warnings of axx lint's feature checks (SQL selection and
trigger ordinals that cannot work); they do not change the exit code.

Exit codes: 0 valid, 2 syntax/config error, 3 undefined or ambiguous steps.`,
		RunE: func(_ *cobra.Command, args []string) error {
			e, err := app.loadEngine(&cf)
			if err != nil {
				return err
			}
			paths, lines, err := e.FeaturePaths(args)
			if err != nil {
				return err
			}
			set, err := e.LoadFeatures(paths)
			if err != nil {
				return err
			}
			pickles, err := set.Apply(feature.Filter{Lines: lines})
			if err != nil {
				return err
			}
			rep := validatePickles(e.Registry, pickles)
			rep.Files = len(set.Docs)
			for _, f := range lint.CheckFeatures(e.Registry, pickles, e.Config.Dir).Findings {
				l := f.Locations[0]
				rep.Warnings = append(rep.Warnings, Problem{Kind: "lint", Location: fmt.Sprintf("%s:%d", l.File, l.Line), Text: stripKeyword(l.Text), Message: f.Message + " [" + f.Code + "]"})
			}
			if err := app.EmitResult(rep, len(rep.Problems) == 0, func(w io.Writer) error { return renderValidation(w, rep) }); err != nil {
				return err
			}
			if len(rep.Problems) > 0 {
				app.hintNoPacks(e)
				return silentExit{code: exitcode.Undefined}
			}
			return nil
		},
	}
	cf.register(cmd)
	return cmd
}

func validatePickles(reg *match.Registry, pickles []*feature.Pickle) ValidationReport {
	res := check.Pickles(reg, pickles)
	rep := ValidationReport{Scenarios: len(pickles), Steps: res.Steps, Problems: []Problem{}}
	for _, pr := range res.Problems {
		rep.Problems = append(rep.Problems, Problem{
			Kind: pr.Kind, Location: fmt.Sprintf("%s:%d", pr.URI, pr.Line), Text: pr.Text,
			Message: pr.Message, Suggestions: pr.Suggestions, Candidates: pr.Candidates,
		})
	}
	return rep
}

func renderValidation(w io.Writer, rep ValidationReport) error {
	for _, p := range rep.Problems {
		fmt.Fprintf(w, "%s: %s: %s\n", p.Location, p.Kind, p.Text)
		if p.Message != "" {
			fmt.Fprintf(w, "  %s\n", p.Message)
		}
		for _, s := range p.Suggestions {
			fmt.Fprintf(w, "  did you mean: %s\n", s.Expr)
		}
		for _, c := range p.Candidates {
			fmt.Fprintf(w, "  candidate: %s\n", c)
		}
	}
	for _, p := range rep.Warnings {
		fmt.Fprintf(w, "%s: warning: %s\n  %s\n", p.Location, p.Text, p.Message)
	}
	status := "ok"
	if len(rep.Problems) > 0 {
		status = plural(len(rep.Problems), "problem")
	}
	if len(rep.Warnings) > 0 {
		status += ", " + plural(len(rep.Warnings), "warning")
	}
	_, err := fmt.Fprintf(w, "%s, %s, %s: %s\n", plural(rep.Files, "file"), plural(rep.Scenarios, "scenario"), plural(rep.Steps, "step"), status)
	return err
}
