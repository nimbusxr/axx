package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/internal/packref"
	"github.com/nimbusxr/axx/internal/packset"
	"github.com/nimbusxr/axx/internal/stepsearch"
)

// The steps of axx's packs a project does not use come from axx's reference
// of its packs (internal/packref), so an agent finds what a pack does, and
// its steps, before adding it, offline and at this axx's version.

// loadedPacks names the packs the engine loads.
func loadedPacks(e *engine.Engine) map[string]bool {
	out := map[string]bool{}
	for _, p := range e.Packs {
		out[p.Name] = true
	}
	return out
}

// otherInfos lists the steps of axx's packs the project does not use: of
// the one named, or of all of them when pack is "".
func otherInfos(e *engine.Engine, pack string) []StepInfo {
	loaded := loadedPacks(e)
	var names []string
	for _, n := range packref.Names() {
		if !loaded[n] && (pack == "" || n == pack) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil
	}
	reg, err := packref.Registry(names...)
	if err != nil {
		return nil
	}
	var out []StepInfo
	for _, s := range stepInfosOf(reg, pack) {
		if !loaded[s.Pack] {
			out = append(out, s)
		}
	}
	return out
}

// betterElsewhere returns the steps of axx's packs the project does not use
// (others) that read like q better than any of the project's steps (mine),
// best first, and whether they leave the project's beside the point
// (stepsearch.Elsewhere): in a project without kafka, "kafka event key"
// finds kafka's steps, while "response status" in a REST project finds only
// the project's.
func betterElsewhere(mine, others []StepInfo, q string, limit int) ([]StepInfo, bool) {
	better, outclassed := stepsearch.Elsewhere(searchable(mine), searchable(others), q, limit)
	if len(better) == 0 {
		return nil, false
	}
	var keep []StepInfo
	for i, s := range others {
		if better[i] {
			keep = append(keep, s)
		}
	}
	found, _ := searchSteps(keep, q, limit)
	return found, outclassed
}

// foldedCount is the number of steps as `axx steps` lists them: each
// named-service twin folded into its step.
func foldedCount(infos []StepInfo) int {
	entries := make([]stepsearch.Entry, len(infos))
	for i, s := range infos {
		entries[i] = stepsearch.Entry{ID: s.ID, Pack: s.Pack, Expr: s.Expr, Arg: s.Arg, Columns: s.Columns}
	}
	kept, _ := stepsearch.Fold(entries)
	return len(kept)
}

func searchable(infos []StepInfo) []stepsearch.Step {
	docs := make([]stepsearch.Step, len(infos))
	for i, s := range infos {
		docs[i] = stepsearch.Step{ID: s.ID, Pack: s.Pack, Expr: s.Expr, Doc: s.Doc}
	}
	return docs
}

// findOther finds the step q names (its id, its expression, or a step line)
// among the steps of axx's packs the project does not use.
func findOther(e *engine.Engine, q string) (StepInfo, bool) {
	others := otherInfos(e, "")
	for _, s := range others {
		if s.ID == q || s.Expr == q || slices.Contains(s.Variants, q) {
			return s, true
		}
	}
	reg, err := packref.Registry(packNames(others)...)
	if err != nil {
		return StepInfo{}, false
	}
	line := stripKeyword(q)
	for _, text := range []string{line, line + ":"} {
		if ms := reg.Match(text); len(ms) > 0 {
			id := ms[0].Def().Step.ID
			for _, s := range others {
				if s.ID == id {
					return s, true
				}
			}
		}
	}
	return StepInfo{}, false
}

func packNames(infos []StepInfo) []string {
	var out []string
	for _, s := range infos {
		if !slices.Contains(out, s.Pack) {
			out = append(out, s.Pack)
		}
	}
	return out
}

// notInProject says how to add a pack the project does not use.
func notInProject(pack string) string {
	return fmt.Sprintf("the %s pack is not in this project: `axx pack add %s` adds it", pack, pack)
}

// renderPackMap lists axx's packs, one line each, with what they reach.
func renderPackMap(w io.Writer, used map[string]bool) {
	width := 0
	for _, p := range packset.Catalog {
		width = max(width, len(p.Name))
	}
	for _, p := range packset.Catalog {
		mark := ""
		if used[p.Name] {
			mark = "  (used)"
		}
		fmt.Fprintf(w, "  %-*s  %s%s\n", width, p.Name, p.Summary, mark)
	}
}

// PackDetail is `axx pack show`: a pack, what it reaches, its settings and
// its steps.
type PackDetail struct {
	Pack     string     `json:"pack"`
	Summary  string     `json:"summary"`
	Used     bool       `json:"used"`
	Requires []string   `json:"requires,omitempty"`
	Doc      string     `json:"doc,omitempty"`
	Settings []Setting  `json:"settings,omitempty"`
	Steps    []StepInfo `json:"steps"`
	// StepCount is the pack's number of steps (Steps may leave them out).
	StepCount int          `json:"stepCount"`
	Related   []PackSketch `json:"related,omitempty"`
}

// Setting is a key of a pack's section in axx.yaml (packs.<name>).
type Setting struct {
	Key string `json:"key"`
	Doc string `json:"doc,omitempty"`
}

// PackSketch is a pack in one line.
type PackSketch struct {
	Pack    string `json:"pack"`
	Summary string `json:"summary"`
}

// settingsOf lists the top-level keys of a pack's settings schema.
func settingsOf(schema json.RawMessage) []Setting {
	var s struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if len(schema) == 0 || json.Unmarshal(schema, &s) != nil {
		return nil
	}
	keys := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Setting, 0, len(keys))
	for _, k := range keys {
		out = append(out, Setting{Key: k, Doc: s.Properties[k].Description})
	}
	return out
}

// related lists the packs that build on name, or that it builds on.
func related(name string) []PackSketch {
	var out []PackSketch
	self, _ := packset.Lookup(name)
	for _, p := range packset.Catalog {
		if p.Name == name {
			continue
		}
		if slices.Contains(p.Requires, name) || slices.Contains(self.Requires, p.Name) {
			out = append(out, PackSketch{Pack: p.Name, Summary: p.Summary})
		}
	}
	return out
}

func renderPackDetail(w io.Writer, d PackDetail) error {
	fmt.Fprintf(w, "%s: %s\n", d.Pack, d.Summary)
	if !d.Used {
		fmt.Fprintln(w, notInProject(d.Pack))
	}
	if d.Doc != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimSpace(d.Doc))
	}
	if len(d.Related) > 0 {
		fmt.Fprintln(w, "\nRelated packs:")
		for _, r := range d.Related {
			fmt.Fprintf(w, "  %s  %s\n", r.Pack, r.Summary)
		}
	}
	if len(d.Settings) > 0 {
		fmt.Fprintf(w, "\nSettings (packs.%s in axx.yaml):\n", d.Pack)
		for _, s := range d.Settings {
			fmt.Fprintf(w, "  %s  %s\n", s.Key, s.Doc)
		}
	}
	fmt.Fprintln(w)
	switch {
	case d.StepCount == 0:
		_, err := fmt.Fprintln(w, "Steps: none of its own; its commands and the packs that build on it do the work.")
		return err
	case len(d.Steps) == 0:
		_, err := fmt.Fprintf(w, "Steps: `axx steps --pack %s` lists its %d steps.\n", d.Pack, d.StepCount)
		return err
	}
	if err := renderSteps(w, d.Steps, false); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, "\n`axx steps show <id>` gives a step's documentation and examples")
	return err
}

// stepInfosOf describes the steps of a registry: the pack named, or all of
// them when pack is "".
func stepInfosOf(reg *match.Registry, pack string) []StepInfo {
	variants := map[*match.Def][]string{}
	for _, v := range reg.Variants() {
		variants[v.Def] = append(variants[v.Def], v.Expr)
	}
	params := map[string]string{}
	for _, p := range reg.Params() {
		params[p.Type.Name] = p.Type.Doc
	}
	var out []StepInfo
	for _, d := range reg.Defs() {
		if pack != "" && d.Pack != pack {
			continue
		}
		s := d.Step
		info := StepInfo{
			ID: s.ID, Pack: d.Pack, Expr: s.Expr, Variants: variants[d], Keyword: s.Keyword,
			Arg: s.Arg.String(), Doc: s.Doc, Examples: s.Examples, Since: s.Since, Deprecated: s.DeprecatedBy,
		}
		if s.Table != nil {
			info.Columns, info.TableNote = s.Table.Columns, s.Table.Note
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
