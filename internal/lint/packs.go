package lint

import (
	"fmt"
	"os"
	"strings"

	messages "github.com/cucumber/messages/go/v35"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// CheckPacks runs the checks the project's packs make of its features
// (core.FeatureCheck), a rule each. A pack that reads something of its own in a
// feature (a tag it gives a meaning to) says here when it is wrong. Findings
// are warnings, each at the tag it is about, or at its scenario. workDir is
// what reported paths are relative to.
func CheckPacks(reg *match.Registry, pickles []*feature.Pickle, workDir string) []RuleResult {
	checks := reg.Checks()
	if len(checks) == 0 {
		return nil
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	scenarios := make([]core.ScenarioInfo, len(pickles))
	at := map[string]*feature.Pickle{}
	files := map[string]bool{}
	for i, p := range pickles {
		scenarios[i] = core.ScenarioInfo{ID: p.Id, Name: p.Name, URI: p.Uri, Line: p.Line, Tags: p.TagNames, Dir: p.Dir()}
		at[fmt.Sprintf("%s:%d", p.Uri, p.Line)] = p
		if p.Doc != nil {
			files[p.Doc.Path] = true
		}
	}
	var out []RuleResult
	for _, c := range checks {
		rr := RuleResult{
			Name:        c.Check.Name,
			ID:          c.Check.ID,
			Description: c.Check.Description,
			Source:      "the " + c.Pack + " pack",
			Type:        "builtin",
			Mode:        ModeWarn,
			Findings:    []Finding{},
		}
		for f := range files {
			rr.files = append(rr.files, f)
		}
		rr.Files = len(rr.files)
		if c.Check.Run == nil {
			out = append(out, rr)
			continue
		}
		seen := map[string]bool{}
		for _, f := range c.Check.Run(&core.FeatureCheckCall{Scenarios: scenarios, Settings: c.Settings, ProjectDir: c.ProjectDir}) {
			p := at[fmt.Sprintf("%s:%d", f.URI, f.Line)]
			if p == nil || p.Doc == nil {
				continue
			}
			l := Location{File: relSlash(workDir, p.Doc.Path), Line: p.Line, Text: clip(strings.TrimSpace(p.Keyword) + ": " + p.Name), abs: p.Doc.Path}
			if f.Tag != "" {
				if tag := tagOf(p, f.Tag); tag != nil {
					l.Line, l.Column, l.Text = int(tag.Location.Line), int(tag.Location.Column), f.Tag
				}
			}
			if f.At > 0 {
				l.Line, l.Column, l.Text = f.At, f.Column, ""
				if lines := strings.Split(string(p.Doc.Source), "\n"); f.At <= len(lines) {
					l.Text = clip(strings.TrimSpace(lines[f.At-1]))
				}
			}
			// A tag of a feature or a rule is one line of many scenarios.
			once := fmt.Sprintf("%s:%d:%d:%s", l.abs, l.Line, l.Column, f.Message)
			if seen[once] {
				continue
			}
			seen[once] = true
			rr.Findings = append(rr.Findings, Finding{Code: CodePackCheck, Severity: SeverityWarning, Message: f.Message, Value: f.Tag, Locations: []Location{l}})
		}
		out = append(out, rr)
	}
	return out
}

// tagOf is where a scenario's tag is written: on the scenario, on the examples
// its row is of, on its rule or on its feature, the nearest first.
func tagOf(p *feature.Pickle, name string) *messages.Tag {
	if p.Doc == nil || p.Doc.AST == nil || p.Doc.AST.Feature == nil {
		return nil
	}
	find := func(tags []*messages.Tag) *messages.Tag {
		for _, t := range tags {
			if t.Name == name {
				return t
			}
		}
		return nil
	}
	f := p.Doc.AST.Feature
	inScenario := func(s *messages.Scenario) *messages.Tag {
		if s == nil || int(s.Location.Line) != p.ScenarioLine {
			return nil
		}
		for _, ex := range s.Examples {
			for _, row := range ex.TableBody {
				if int(row.Location.Line) == p.Line {
					if t := find(ex.Tags); t != nil {
						return t
					}
				}
			}
		}
		return find(s.Tags)
	}
	for _, child := range f.Children {
		if t := inScenario(child.Scenario); t != nil {
			return t
		}
		if child.Scenario != nil && int(child.Scenario.Location.Line) == p.ScenarioLine {
			return find(f.Tags)
		}
		if r := child.Rule; r != nil {
			for _, rc := range r.Children {
				if rc.Scenario == nil || int(rc.Scenario.Location.Line) != p.ScenarioLine {
					continue
				}
				if t := inScenario(rc.Scenario); t != nil {
					return t
				}
				if t := find(r.Tags); t != nil {
					return t
				}
				return find(f.Tags)
			}
		}
	}
	return nil
}
