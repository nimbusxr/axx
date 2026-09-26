package md

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/nimbusxr/axx/internal/match"
)

// PackPage renders the reference page of a pack: what it is for, the packs
// it builds on, its settings, steps, parameter types and the tools it gives
// coding agents. Links to other packs' pages are relative to linkBase,
// like "/references/packs/".
func PackPage(reg *match.Registry, p PackInfo, linkBase string, frontmatter bool) (string, error) {
	m := p.Manifest
	defs := defsOf(reg, p.Name)
	var params []*match.Param
	for _, pt := range reg.Params() {
		if pt.Pack == p.Name {
			params = append(params, pt)
		}
	}
	settings, err := settingsOf(m.ConfigSchema)
	if err != nil {
		return "", fmt.Errorf("pack %s: its settings: %w", p.Name, err)
	}
	if len(defs) == 0 && len(settings) == 0 && len(m.Tools) == 0 && len(params) == 0 {
		return "", fmt.Errorf("pack %s: %w", p.Name, ErrEmpty)
	}
	var b strings.Builder
	if frontmatter {
		fmt.Fprintf(&b, "---\ntitle: %s\ndescription: %q\n---\n\n", p.Name, oneLine(m.Doc))
	}
	b.WriteString(GeneratedHeader)
	if !frontmatter {
		fmt.Fprintf(&b, "\n# %s\n", p.Name)
	}
	if m.Doc != "" {
		fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(m.Doc))
	}
	if p.Builtin {
		b.WriteString("\nEvery project has it, and every pack builds on it")
	} else {
		fmt.Fprintf(&b, "\nAdd it to a project with `axx pack add %s`", p.Name)
	}
	if len(m.Requires) > 0 {
		var reqs []string
		for _, r := range m.Requires {
			reqs = append(reqs, fmt.Sprintf("[`%s`](%s%s/)", r, linkBase, r))
		}
		fmt.Fprintf(&b, "; it builds on %s, which comes with it", strings.Join(reqs, " and "))
	}
	b.WriteString(".\n")

	if len(settings) > 0 {
		defaults := false
		for _, st := range settings {
			defaults = defaults || st.def != ""
		}
		fmt.Fprintf(&b, "\n## Settings\n\nIn `axx.yaml`, under `packs.%s`:\n\n| Setting | Takes | Values |", p.Name)
		if defaults {
			b.WriteString(" Default |\n|---|---|---|---|\n")
		} else {
			b.WriteString("\n|---|---|---|\n")
		}
		for _, st := range settings {
			fmt.Fprintf(&b, "| `%s` | %s | %s |", st.name, escapePipes(oneLine(st.doc)), escapePipes(st.value))
			if defaults {
				if st.def != "" {
					fmt.Fprintf(&b, " `%s` |", st.def)
				} else {
					b.WriteString(" |")
				}
			}
			b.WriteString("\n")
		}
	}
	if len(defs) > 0 {
		b.WriteString("\n## Steps\n")
		variants := variantsByDef(reg)
		docs := paramDocs(reg)
		for _, d := range defs {
			writeStep(&b, "###", d, variants[d], docs)
		}
	}
	if len(params) > 0 {
		b.WriteString("\n## Parameter types\n\n| Parameter | Takes | Values | For example |\n|---|---|---|---|\n")
		for _, pt := range params {
			fmt.Fprintf(&b, "| `{%s}` | %s | %s | %s |\n", pt.Type.Name, escapePipes(strings.TrimSuffix(oneLine(pt.Type.Doc), ".")),
				escapePipes(codes(pt.Type.Values)), escapePipes(codes(first(pt.Type.Examples, 3))))
		}
	}
	if len(m.Tools) > 0 {
		b.WriteString("\n## Tools for agents\n\nWhat the pack lets coding agents do through `axx mcp`, in the scenario they keep open with `steps_try`.\n\n| Tool | |\n|---|---|\n")
		for _, t := range m.Tools {
			fmt.Fprintf(&b, "| `%s` | %s |\n", t.Name, escapePipes(oneLine(t.Description)))
		}
	}
	return b.String(), nil
}

// setting is a setting of a pack, from its section of axx.yaml's schema:
// what it takes, the values it takes, and its default.
type setting struct {
	name, doc, value, def string
}

// defaultIn is a default a description gives: "... (default failed)".
var defaultIn = regexp.MustCompile(`\s*\(default ([^)]+)\)`)

// settingsOf lists the settings a pack's schema has, by name.
func settingsOf(schema json.RawMessage) ([]setting, error) {
	if len(schema) == 0 {
		return nil, nil
	}
	var s property
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, err
	}
	out := s.settings("")
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// settings are the settings of an object of the schema, below prefix: a
// setting that is an object of settings itself is its settings, dotted
// (tls.verify).
func (p property) settings(prefix string) []setting {
	var out []setting
	for name, child := range p.Properties {
		if len(child.Properties) > 0 {
			out = append(out, child.settings(prefix+name+".")...)
			continue
		}
		out = append(out, child.setting(prefix+name))
	}
	return out
}

// setting is a setting of the schema: what it takes, the values it takes,
// and its default, from the schema's default or a description's "(default
// X)".
func (p property) setting(name string) setting {
	st := setting{name: name, value: p.value(), doc: p.Description}
	if p.Default != nil {
		st.def = fmt.Sprint(p.Default)
	}
	if m := defaultIn.FindStringSubmatch(st.doc); m != nil {
		st.def = m[1]
		st.doc = defaultIn.ReplaceAllString(st.doc, "")
	}
	return st
}

type property struct {
	Properties  map[string]property `json:"properties"`
	Type        any                 `json:"type"`
	Default     any                 `json:"default"`
	Enum        []any               `json:"enum"`
	Description string              `json:"description"`
	Items       *property           `json:"items"`
	Additional  json.RawMessage     `json:"additionalProperties"`
	Minimum     *float64            `json:"minimum"`
	Maximum     *float64            `json:"maximum"`
}

// value says what values a setting takes, as people write them in
// axx.yaml: its values when it takes one of a set, or what it is.
func (p property) value() string {
	if len(p.Enum) > 0 {
		var vs []string
		for _, e := range p.Enum {
			vs = append(vs, fmt.Sprintf("`%v`", e))
		}
		return strings.Join(vs, ", ")
	}
	switch t := p.typeName(); t {
	case "array":
		if p.Items != nil {
			if v := p.Items.value(); v != "" {
				return "a list of " + v
			}
		}
		return "a list"
	case "object":
		return "names and values"
	case "boolean":
		return "`true`, `false`"
	case "integer", "number":
		if p.Minimum != nil && p.Maximum != nil {
			return fmt.Sprintf("a number, %v to %v", *p.Minimum, *p.Maximum)
		}
		return "a number"
	case "string":
		return "text"
	default:
		return t
	}
}

func (p property) typeName() string {
	switch t := p.Type.(type) {
	case string:
		return t
	case []any:
		var ts []string
		for _, x := range t {
			ts = append(ts, fmt.Sprint(x))
		}
		return strings.Join(ts, " or ")
	}
	return ""
}

// PackGroup is packs that go together on the packs overview: those of a
// cloud, the web packs. The first has no title: the core and the packs
// outside any group.
type PackGroup struct {
	Title string
	Packs []PackSummary
}

// PackSummary is a pack as the overview lists it.
type PackSummary struct {
	Name, Summary string
}

// PacksOverview renders the list of the packs axx publishes, by group,
// each linked to its page below linkBase.
func PacksOverview(groups []PackGroup, linkBase string, frontmatter bool) (string, error) {
	var b strings.Builder
	if frontmatter {
		fmt.Fprintf(&b, "---\ntitle: Packs\ndescription: %q\n---\n\n", "The packs axx publishes: their steps, settings and tools, a page each.")
	}
	b.WriteString(GeneratedHeader)
	if !frontmatter {
		b.WriteString("\n# Packs\n")
	}
	b.WriteString("\nA pack gives axx what a project tests: its steps, its settings in `axx.yaml`, and tools for coding agents. " +
		"A project lists the packs it uses in `axx-packs.yaml` (`axx pack add <pack>`); a pack that builds on another brings it along.\n")
	n := 0
	for _, g := range groups {
		if len(g.Packs) == 0 {
			continue
		}
		if g.Title != "" {
			fmt.Fprintf(&b, "\n## %s\n", g.Title)
		}
		b.WriteString("\n| Pack | For |\n|---|---|\n")
		for _, p := range g.Packs {
			fmt.Fprintf(&b, "| [`%s`](%s%s/) | %s |\n", p.Name, linkBase, p.Name, escapePipes(p.Summary))
			n++
		}
	}
	if n == 0 {
		return "", ErrEmpty
	}
	return b.String(), nil
}
