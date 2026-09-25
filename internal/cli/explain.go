package cli

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/match"
)

// Explanation is the JSON result of `axx explain`.
type Explanation struct {
	Line        string             `json:"line"`
	Text        string             `json:"text"`
	Status      string             `json:"status"` // matched | undefined | ambiguous
	Match       *ExplainedMatch    `json:"match,omitempty"`
	Candidates  []ExplainedMatch   `json:"candidates,omitempty"`
	Suggestions []match.Suggestion `json:"suggestions,omitempty"`
}

// ExplainedMatch describes one matching definition.
type ExplainedMatch struct {
	ID   string         `json:"id"`
	Pack string         `json:"pack"`
	Expr string         `json:"expr"`
	Args []ExplainedArg `json:"args"`
	Arg  string         `json:"argument"`
}

// ExplainedArg is one captured argument.
type ExplainedArg struct {
	Param   string `json:"param"`
	Present bool   `json:"present"`
	Raw     string `json:"raw,omitempty"`
}

func newExplainCmd(app *App) *cobra.Command {
	var cf configFlags
	cmd := &cobra.Command{
		Use:   `explain "<step line>" | AXX-Exxxx`,
		Short: "Show which step definition a line matches, or what an error code means",
		Long: `Explain how axx reads a step line: the matching definition and captured
arguments, the candidates if it is ambiguous, or the closest steps if it is
undefined. The leading Gherkin keyword is optional.

Given an error code such as AXX-E0102, print what it means and how to fix it.

Exit code 0 means the line matches exactly one step, 3 means it is undefined or
ambiguous.`,
		Args: wrapArgs(cobra.MinimumNArgs(1)),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 1 && errorCodeRE.MatchString(args[0]) {
				return explainErrorCode(app, strings.ToUpper(args[0]))
			}
			e, err := app.loadEngine(&cf)
			if err != nil {
				return err
			}
			line := strings.Join(args, " ")
			text := stripKeyword(line)
			ex := Explanation{Line: line, Text: text}
			matches := e.Registry.Match(text)
			switch len(matches) {
			case 0:
				ex.Status = "undefined"
				ex.Suggestions = e.Registry.Suggest(text, 5)
			case 1:
				ex.Status = "matched"
				m := explained(matches[0])
				ex.Match = &m
			default:
				ex.Status = "ambiguous"
				for _, m := range matches {
					ex.Candidates = append(ex.Candidates, explained(m))
				}
			}
			if err := app.EmitResult(ex, ex.Status == "matched", func(w io.Writer) error { return renderExplanation(w, ex) }); err != nil {
				return err
			}
			if ex.Status != "matched" {
				return silentExit{code: exitcode.Undefined}
			}
			return nil
		},
	}
	cf.register(cmd)
	return cmd
}

func explained(m match.Match) ExplainedMatch {
	out := ExplainedMatch{ID: m.Def().Step.ID, Pack: m.Def().Pack, Expr: m.Variant.Expr, Arg: m.Def().Step.Arg.String()}
	for _, a := range m.Args {
		out.Args = append(out.Args, ExplainedArg{Param: a.Param, Present: a.Present, Raw: a.Raw})
	}
	return out
}

func renderExplanation(w io.Writer, ex Explanation) error {
	switch ex.Status {
	case "matched":
		m := ex.Match
		fmt.Fprintf(w, "matches %s (pack %s)\n  %s\n", m.ID, m.Pack, m.Expr)
		for i, a := range m.Args {
			if a.Present {
				fmt.Fprintf(w, "  arg %d {%s} = %s\n", i+1, a.Param, a.Raw)
			} else {
				fmt.Fprintf(w, "  arg %d {%s} not given (default)\n", i+1, a.Param)
			}
		}
		if m.Arg == "table" || m.Arg == "docstring" {
			fmt.Fprintf(w, "  requires a %s\n", map[string]string{"table": "data table", "docstring": "doc string"}[m.Arg])
		}
	case "ambiguous":
		fmt.Fprintf(w, "ambiguous: %d definitions match\n", len(ex.Candidates))
		for _, c := range ex.Candidates {
			fmt.Fprintf(w, "  %s  %s\n", c.ID, c.Expr)
		}
	default:
		fmt.Fprintln(w, "undefined: no step matches")
		if len(ex.Suggestions) > 0 {
			fmt.Fprintln(w, "did you mean:")
			for _, s := range ex.Suggestions {
				fmt.Fprintf(w, "  %s  (%s)\n", s.Expr, s.ID)
			}
		}
		fmt.Fprintln(w, "search all steps with `axx steps search <words>`")
	}
	return nil
}

// silentExit ends a command with a non-zero code after output was already
// written (no extra error message).
type silentExit struct{ code exitcode.Code }

func (s silentExit) Error() string { return s.code.String() }

var errorCodeRE = regexp.MustCompile(`(?i)^AXX-E\d{4}$`)

// ExplainedCode is the JSON result of `axx explain AXX-Exxxx`.
type ExplainedCode struct {
	axxerr.Entry
	Docs string `json:"docs"`
}

func explainErrorCode(app *App, code string) error {
	entry, ok := axxerr.Lookup(code)
	if !ok {
		return axxerr.New("AXX-E0009", exitcode.Usage, "unknown error code %s", code).
			WithHint("all codes are listed at %s/references/error-codes/", axxerr.DocsBase)
	}
	docs := (&axxerr.Error{Code: code}).DocsURL()
	return app.Emit(ExplainedCode{Entry: entry, Docs: docs}, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "%s: %s (exit %d)\n\n%s\n\nFix: %s\n\n%s\n", entry.Code, entry.Title, int(entry.Exit), entry.Meaning, entry.Fix, docs)
		return err
	})
}
