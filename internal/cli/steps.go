package cli

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/internal/packset"
)

// StepInfo is the JSON description of a step definition.
type StepInfo struct {
	ID       string   `json:"id"`
	Pack     string   `json:"pack"`
	Expr     string   `json:"expr"`
	Variants []string `json:"variants"`
	Keyword  string   `json:"keyword,omitempty"`
	Arg      string   `json:"arg"`
	// Columns name the columns of the step's data table, when it has one.
	Columns    []string    `json:"columns,omitempty"`
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
		if s.Table != nil {
			info.Columns = s.Table.Columns
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
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && slices.ContainsFunc(packset.Catalog, func(p packset.Pack) bool { return p.Name == args[0] }) {
				return axxerr.New("AXX-E0001", exitcode.Usage, "invalid usage: unknown command %q for \"axx steps\"", args[0]).
					WithHint("`axx steps --pack %s` lists the %s pack's steps, and `axx steps search <words>` finds steps by what they do", args[0], args[0])
			}
			return wrapArgs(cobra.NoArgs)(cmd, args)
		},
	}
	listSteps := func(*cobra.Command, []string) error {
		e, err := app.loadEngine(&cf)
		if err != nil {
			return err
		}
		infos := stepInfos(e, pack)
		app.hintNoPacks(e)
		return app.Emit(map[string]any{"steps": infos, "count": len(infos)}, func(w io.Writer) error {
			return renderStepList(w, infos)
		})
	}
	cmd.RunE = listSteps
	cf.register(cmd)
	list := &cobra.Command{
		Use:   "list",
		Short: "List every step (the same as `axx steps`)",
		Args:  wrapArgs(cobra.NoArgs),
		RunE:  listSteps,
	}
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
		Use:        "show <id | expression | step line>",
		Short:      "Show one step's documentation, variants and examples",
		SuggestFor: []string{"inspect", "describe", "info", "doc"},
		Long: `Show one step: by its id (rest.response.status), its expression as
` + "`axx steps`" + ` lists it, or a step line as a feature has it (the leading
keyword is optional), matched the way ` + "`axx explain`" + ` matches it.`,
		Args: wrapArgs(cobra.MinimumNArgs(1)),
		RunE: func(_ *cobra.Command, args []string) error {
			e, err := app.loadEngine(&cf)
			if err != nil {
				return err
			}
			q := strings.Join(args, " ")
			s, err := findStep(e, stepInfos(e, ""), q)
			if err != nil {
				return err
			}
			return app.Emit(s, func(w io.Writer) error { return renderStep(w, s) })
		},
	}
	for _, c := range []*cobra.Command{list, search, show} {
		cf.register(c)
	}
	// `axx steps explain` is `axx explain`, where agents look for it too.
	explain := newExplainCmd(app)
	explain.Short = "Same as `axx explain`: " + explain.Short
	cmd.AddCommand(list, search, show, explain)
	return cmd
}

// findStep finds the step q names: its id, its expression or part of it (a
// variant, or the expression without some of its optional [[...]] segments),
// or a step line it matches. When none does, the error names the closest
// steps.
func findStep(e *engine.Engine, infos []StepInfo, q string) (StepInfo, error) {
	byID := map[string]StepInfo{}
	expanded := match.Expand(q)
	for _, s := range infos {
		byID[s.ID] = s
		if s.ID == q || s.Expr == q || slices.ContainsFunc(expanded, func(x string) bool { return slices.Contains(s.Variants, x) }) {
			return s, nil
		}
	}
	matches := e.Registry.Match(stripKeyword(q))
	switch len(matches) {
	case 1:
		return byID[matches[0].Def().Step.ID], nil
	case 0:
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.Def().Step.ID)
		}
		return StepInfo{}, axxerr.New("AXX-E0310", exitcode.Usage, "%q matches %d steps: %s", q, len(ids), strings.Join(ids, ", ")).
			WithHint("show one by its id: `axx steps show %s`", ids[0])
	}
	hint := "list ids with `axx steps` or search with `axx steps search <words>`"
	closest := searchSteps(e, infos, q, 3)
	if looksLikeID(q) {
		// An id that is not one: the ids that start like it, else the steps
		// its words find (rest.get: "rest get").
		if closest = idsLike(infos, q, 3); len(closest) == 0 {
			closest = searchSteps(e, infos, strings.ReplaceAll(q, ".", " "), 3)
		}
	}
	if len(closest) > 0 {
		names := make([]string, 0, len(closest))
		for _, s := range closest {
			names = append(names, fmt.Sprintf("%s (%s)", s.ID, s.Expr))
		}
		hint = "the closest: " + strings.Join(names, "; ") + "; search with `axx steps search <words>`"
	}
	return StepInfo{}, axxerr.New("AXX-E0310", exitcode.Usage, "no step has the id or expression %q, or matches it as a step line", q).WithHint("%s", hint)
}

// looksLikeID reports whether q is shaped like a step id: dotted lower-case
// words, like rest.response.status.
func looksLikeID(q string) bool {
	return strings.Contains(q, ".") && strings.IndexFunc(q, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '-' && r != '_'
	}) < 0
}

// idsLike returns the ids that share the longest leading part of q, past
// its pack (rest.response.property.contains: rest.response.property.*),
// shortest first.
func idsLike(infos []StepInfo, q string, limit int) []StepInfo {
	parts := strings.Split(q, ".")
	for n := len(parts) - 1; n >= 2; n-- {
		prefix := strings.Join(parts[:n], ".") + "."
		var out []StepInfo
		for _, s := range infos {
			if strings.HasPrefix(s.ID, prefix) {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			sort.SliceStable(out, func(i, j int) bool {
				if len(out[i].ID) != len(out[j].ID) {
					return len(out[i].ID) < len(out[j].ID)
				}
				return out[i].ID < out[j].ID
			})
			return out[:min(limit, len(out))]
		}
	}
	return nil
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
	switch {
	case len(s.Columns) > 1:
		fmt.Fprintf(w, "\nTable columns: | %s |\n  (the table may start with a row of these names, or go straight to its rows)\n", strings.Join(s.Columns, " | "))
	case len(s.Columns) == 1:
		fmt.Fprintf(w, "\nTable column: | %s |\n", s.Columns[0])
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
