package lint

import (
	"fmt"
	"os"
	"strings"

	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// DeprecatedRuleID is the id of the builtin deprecated steps check.
const DeprecatedRuleID = "deprecated-steps"

// CheckDeprecated warns about a step that is deprecated, with what to use
// instead: it keeps working until the next major version, and goes with it
// (the deprecation policy). Findings are warnings. workDir is what reported
// paths are relative to.
func CheckDeprecated(reg *match.Registry, pickles []*feature.Pickle, workDir string) RuleResult {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	rr := RuleResult{
		Name:        "Deprecated steps",
		ID:          DeprecatedRuleID,
		Description: "A deprecated step keeps working until the next major version, which drops it: the step says what to use instead.",
		Type:        "builtin",
		Mode:        ModeWarn,
		Findings:    []Finding{},
	}
	files := map[string]bool{}
	for _, p := range pickles {
		files[p.Doc.Path] = true
	}
	for f := range files {
		rr.files = append(rr.files, f)
	}
	rr.Files = len(rr.files)
	seen := map[string]bool{}
	for _, p := range pickles {
		for _, ps := range p.Steps {
			ms := reg.Match(ps.Text)
			if len(ms) != 1 {
				continue
			}
			def := ms[0].Def()
			if def.Step.DeprecatedBy == "" {
				continue
			}
			src := p.StepSource(ps)
			l := Location{File: relSlash(workDir, p.Doc.Path), Line: src.Line, Text: clip(strings.TrimSpace(src.Keyword) + " " + ps.Text), abs: p.Doc.Path}
			// A Background's step, or an outline's, is one line of many scenarios.
			once := fmt.Sprintf("%s:%d", l.abs, l.Line)
			if seen[once] {
				continue
			}
			seen[once] = true
			of := "axx"
			if !published(def.Pack) {
				of = "the " + def.Pack + " pack"
			}
			rr.Findings = append(rr.Findings, Finding{
				Code:      CodeDeprecatedStep,
				Severity:  SeverityWarning,
				Message:   fmt.Sprintf("this step is deprecated: %s. It works until the next major version of %s, which drops it", strings.TrimSuffix(def.Step.DeprecatedBy, "."), of),
				Locations: []Location{l},
			})
		}
	}
	return rr
}
