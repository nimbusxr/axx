package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/match"
)

// StepInfo is the JSON description of a step definition.
type StepInfo struct {
	ID         string      `json:"id"`
	Pack       string      `json:"pack"`
	Expr       string      `json:"expr"`
	Variants   []string    `json:"variants"`
	Keyword    string      `json:"keyword,omitempty"`
	Arg        string      `json:"arg"`
	Doc        string      `json:"doc,omitempty"`
	Examples   []string    `json:"examples,omitempty"`
	Params     []ParamInfo `json:"params,omitempty"`
	Since      string      `json:"since,omitempty"`
	Deprecated string      `json:"deprecated,omitempty"`
}

// ParamInfo describes a parameter used by a step.
type ParamInfo struct {
	Name string `json:"name"`
	Doc  string `json:"doc,omitempty"`
}

func stepInfos(e *engine.Engine, pack string) []StepInfo {
	variants := map[*match.Def][]string{}
	for _, v := range e.Registry.Variants() {
		variants[v.Def] = append(variants[v.Def], v.Expr)
	}
	params := map[string]string{}
	for _, p := range e.Registry.Params() {
		params[p.Type.Name] = p.Type.Doc
	}
	var out []StepInfo
	for _, d := range e.Registry.Defs() {
		if pack != "" && d.Pack != pack {
			continue
		}
		s := d.Step
		info := StepInfo{
			ID: s.ID, Pack: d.Pack, Expr: s.Expr, Variants: variants[d], Keyword: s.Keyword,
			Arg: s.Arg.String(), Doc: s.Doc, Examples: s.Examples, Since: s.Since, Deprecated: s.DeprecatedBy,
		}
		seen := map[string]bool{}
		for _, n := range d.Names {
			if !seen[n] {
				seen[n] = true
				info.Params = append(info.Params, ParamInfo{Name: n, Doc: params[n]})
			}
		}
		out = append(out, info)
	}
	return out
}

func newStepsCmd(app *App) *cobra.Command {
	var cf configFlags
	var pack string
	cmd := &cobra.Command{
		Use:   "steps",
		Short: "List, search and inspect the available Gherkin steps",
		Long: `List every step axx understands (the packs this project loads).
Agents: search before writing a feature, and never invent step text.`,
		Args: wrapArgs(cobra.NoArgs),
		RunE: func(*cobra.Command, []string) error {
			e, err := app.loadEngine(&cf)
			if err != nil {
				return err
			}
			infos := stepInfos(e, pack)
			app.hintNoPacks(e)
			return app.Emit(map[string]any{"steps": infos, "count": len(infos)}, func(w io.Writer) error {
				return renderStepList(w, infos)
			})
		},
	}
	cf.register(cmd)
	cmd.PersistentFlags().StringVar(&pack, "pack", "", "only steps from this pack (e.g. rest, sql, kafka)")

	search := &cobra.Command{
		Use:   "search <query>",
		Short: "Find steps by intent, e.g. `axx steps search \"response status\"`",
		Args:  wrapArgs(cobra.MinimumNArgs(1)),
		RunE: func(_ *cobra.Command, args []string) error {
			e, err := app.loadEngine(&cf)
			if err != nil {
				return err
			}
			q := strings.Join(args, " ")
			results := searchSteps(e, stepInfos(e, pack), q, 10)
			if len(results) == 0 {
				app.hintNoPacks(e)
			}
			return app.Emit(map[string]any{"query": q, "steps": results}, func(w io.Writer) error {
				if len(results) == 0 {
					_, err := fmt.Fprintf(w, "no steps match %q; try fewer words or `axx steps`\n", q)
					return err
				}
				return renderStepList(w, results)
			})
		},
	}
	show := &cobra.Command{
		Use:   "show <id>",
		Short: "Show one step's documentation, variants and examples",
		Args:  wrapArgs(cobra.ExactArgs(1)),
		RunE: func(_ *cobra.Command, args []string) error {
			e, err := app.loadEngine(&cf)
			if err != nil {
				return err
			}
			for _, s := range stepInfos(e, "") {
				if s.ID == args[0] {
					return app.Emit(s, func(w io.Writer) error { return renderStep(w, s) })
				}
			}
			return axxerr.New("AXX-E0310", exitcode.Usage, "no step with id %q", args[0]).
				WithHint("list ids with `axx steps` or search with `axx steps search <words>`")
		},
	}
	for _, c := range []*cobra.Command{search, show} {
		cf.register(c)
	}
	cmd.AddCommand(search, show)
	return cmd
}

// searchSteps ranks steps by how many query words appear in the expression,
// id or docs, with a fuzzy fallback on the expression.
func searchSteps(e *engine.Engine, infos []StepInfo, q string, limit int) []StepInfo {
	words := strings.Fields(strings.ToLower(q))
	type scored struct {
		info  StepInfo
		score int
	}
	fuzzy := map[string]int{}
	for i, s := range e.Registry.Suggest(q, 10) {
		fuzzy[s.ID] = 10 - i
	}
	var all []scored
	for _, s := range infos {
		hay := strings.ToLower(s.Expr + " " + s.ID + " " + s.Doc)
		score := 0
		for _, w := range words {
			if strings.Contains(hay, w) {
				score += 3
				if strings.Contains(strings.ToLower(s.Expr), w) {
					score += 2
				}
			}
		}
		score += fuzzy[s.ID]
		if score > 0 {
			all = append(all, scored{s, score})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].score > all[j].score })
	var out []StepInfo
	for i := 0; i < len(all) && i < limit; i++ {
		out = append(out, all[i].info)
	}
	return out
}

func renderStepList(w io.Writer, infos []StepInfo) error {
	byPack := map[string][]StepInfo{}
	var order []string
	for _, s := range infos {
		if _, ok := byPack[s.Pack]; !ok {
			order = append(order, s.Pack)
		}
		byPack[s.Pack] = append(byPack[s.Pack], s)
	}
	for i, p := range order {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s (%s)\n", p, plural(len(byPack[p]), "step"))
		for _, s := range byPack[p] {
			suffix := ""
			switch s.Arg {
			case "table":
				suffix = "  + table"
			case "docstring":
				suffix = "  + doc string"
			}
			fmt.Fprintf(w, "  %-34s %s%s\n", s.ID, s.Expr, suffix)
		}
	}
	return nil
}

func renderStep(w io.Writer, s StepInfo) error {
	fmt.Fprintf(w, "%s  (pack %s)\n\n  %s\n", s.ID, s.Pack, s.Expr)
	if len(s.Variants) > 1 {
		fmt.Fprintln(w, "\nVariants:")
		for _, v := range s.Variants {
			fmt.Fprintf(w, "  %s\n", v)
		}
	}
	if s.Arg != "none" {
		fmt.Fprintf(w, "\nArgument: %s\n", s.Arg)
	}
	if len(s.Params) > 0 {
		fmt.Fprintln(w, "\nParameters:")
		for _, p := range s.Params {
			fmt.Fprintf(w, "  {%s}  %s\n", p.Name, p.Doc)
		}
	}
	if s.Doc != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(s.Doc))
	}
	if len(s.Examples) > 0 {
		fmt.Fprintln(w, "\nExamples:")
		for _, ex := range s.Examples {
			fmt.Fprintf(w, "  %s\n", ex)
		}
	}
	if s.Deprecated != "" {
		fmt.Fprintf(w, "\nDeprecated: %s\n", s.Deprecated)
	}
	return nil
}
