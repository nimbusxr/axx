package lint

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/internal/packset"
)

// The REST requests check. A REST service's requests are numbered in the
// order a scenario adds them (the ordered request steps), and every other
// request and response step addresses one by that number: the 1st without
// an ordinal. A step that adds the Nth request before N-1 exist, adds one
// an earlier step added, or addresses one no step added fails at runtime.
//
// Requests belong to a service: the named one, or the default, which is the
// first service the scenario registers. A scenario whose default service
// cannot be told from its steps, or that runs a step of a project's own
// pack (which may add requests through the rest pack's context), is not
// checked from there on, so a finding is never a false positive.

// RESTRuleID is the id of the builtin REST requests check.
const RESTRuleID = "rest-ordinals"

// FeatureChecks runs every builtin feature-file check: SQL ordinals and
// REST requests.
func FeatureChecks(reg *match.Registry, pickles []*feature.Pickle, workDir string) []RuleResult {
	return []RuleResult{CheckFeatures(reg, pickles, workDir), CheckRESTRequests(reg, pickles, workDir)}
}

// CheckRESTRequests checks the REST requests steps add and address. Findings
// are warnings. workDir is what reported paths are relative to.
func CheckRESTRequests(reg *match.Registry, pickles []*feature.Pickle, workDir string) RuleResult {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	rr := RuleResult{
		Name:        "REST request ordinals",
		ID:          RESTRuleID,
		Description: "A service's requests are numbered in the order a scenario adds them; the other request and response steps address them by that number.",
		Type:        "builtin",
		Mode:        ModeWarn,
		Findings:    []Finding{},
	}
	seen := map[string]bool{}
	files := map[string]bool{}
	for _, p := range pickles {
		files[p.Doc.Path] = true
		var services []string     // registered, in order: the first is the default
		added := map[string]int{} // service -> requests added so far
		for _, ps := range p.Steps {
			ms := reg.Match(ps.Text)
			if len(ms) != 1 {
				continue
			}
			def, args := ms[0].Def(), ms[0].Args
			if def.Pack != "rest" {
				if !published(def.Pack) {
					break // a project's step may add requests
				}
				continue
			}
			switch def.Step.ID {
			case "rest.service":
				if name := args[0].Raw; !slices.Contains(services, name) {
					services = append(services, name)
				}
				continue
			case "rest.openapi.levels":
				continue
			}
			n, svc := 1, ""
			for _, a := range args {
				switch {
				case !a.Present:
				case a.Param == "ordinal":
					v, err := parseOrdinal(a.Raw)
					if err != nil {
						continue
					}
					n = v
				case a.Param == "service":
					svc = a.Raw
				}
			}
			if svc == "" {
				if len(services) == 0 {
					break // the default service is registered out of sight
				}
				svc = services[0]
			}
			count := added[svc]
			var f *Finding
			if def.Step.ID == "rest.request" || def.Step.ID == "rest.request.ordered" {
				switch {
				case n > count+1:
					f = &Finding{Code: CodeRESTMissing, Message: fmt.Sprintf(
						"this step adds the %s request of service %s, but %s before it, so it fails; add the %s first: requests are numbered in the order they are added",
						ordinal(n), svc, requestsAdded(count), ordinal(count+1))}
				case n <= count:
					f = &Finding{Code: CodeRESTAddedTwice, Message: fmt.Sprintf(
						"this step adds the %s request of service %s, which an earlier step added, so it fails; add another with `a %s ordered ... request`",
						ordinal(n), svc, ordinal(count+1))}
				default:
					added[svc]++
				}
			} else if n > count {
				f = &Finding{Code: CodeRESTMissing, Message: fmt.Sprintf(
					"this step uses the %s request of service %s, but %s before it, so it always fails",
					ordinal(n), svc, requestsAdded(count))}
			}
			if f == nil {
				continue
			}
			src := p.StepSource(ps)
			l := Location{File: relSlash(workDir, p.Doc.Path), Line: src.Line, Text: clip(strings.TrimSpace(src.Keyword) + " " + ps.Text), abs: p.Doc.Path}
			// Background and outline steps repeat per pickle: report a line once.
			once := fmt.Sprintf("%s:%d:%s", l.abs, l.Line, f.Code)
			if seen[once] {
				continue
			}
			seen[once] = true
			f.Severity = SeverityWarning
			f.Locations = []Location{l}
			rr.Findings = append(rr.Findings, *f)
		}
	}
	for f := range files {
		rr.files = append(rr.files, f)
	}
	rr.Files = len(rr.files)
	sortFindings(rr.Findings)
	return rr
}

// requestsAdded renders "no request was added", "only 1 request was
// added", "only 2 requests were added".
func requestsAdded(n int) string {
	if n == 0 {
		return "no request was added"
	}
	return fmt.Sprintf("only %d request%s %s added", n, plural(n, "", "s"), plural(n, "was", "were"))
}

// published reports whether pack is one of axx's own packs, whose steps
// never add REST requests unless they are the rest pack's.
func published(pack string) bool {
	return slices.ContainsFunc(packset.Catalog, func(p packset.Pack) bool { return p.Name == pack })
}
