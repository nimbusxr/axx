package lint

import (
	"fmt"
	"os"
	"regexp"
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

// RESTValuesRuleID is the id of the builtin REST payload values check.
const RESTValuesRuleID = "rest-payload-values"

// FeatureChecks runs every builtin feature-file check: services registered
// before use, SQL ordinals, stale selections, REST requests and REST payload
// values.
func FeatureChecks(reg *match.Registry, pickles []*feature.Pickle, workDir string) []RuleResult {
	return []RuleResult{
		CheckServices(reg, pickles, workDir),
		CheckFeatures(reg, pickles, workDir), CheckSelections(reg, pickles, workDir),
		CheckRESTRequests(reg, pickles, workDir), CheckRESTPayloadValues(reg, pickles, workDir),
	}
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
			case "rest.openapi.levels", "rest.token":
				continue // not about a request
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
						"this step adds the %s request of service %s, which an earlier step added, so it fails; add another as the %s: `%s`",
						ordinal(n), svc, ordinal(count+1), asOrdered(ps.Text, count+1))}
				default:
					added[svc]++
				}
			} else if n > count {
				f = &Finding{Code: CodeRESTMissing, Message: fmt.Sprintf(
					"this step uses the %s request of service %s, but %s before it, so it always fails; add it first, like `a %s ordered GET request to /path`: requests are numbered in the order they are added",
					ordinal(n), svc, requestsAdded(count), ordinal(n))}
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

// orderedRequest finds the start of a request step's text: "a POST request
// to …" or "a 2nd ordered POST request to …".
var orderedRequest = regexp.MustCompile(`^an? (?:\d+(?:st|nd|rd|th) ordered )?`)

// asOrdered writes a request step as the nth ordered request:
// "a DELETE request to /x" as "a 2nd ordered DELETE request to /x".
func asOrdered(text string, n int) string {
	if loc := orderedRequest.FindStringIndex(text); loc != nil {
		return "a " + ordinal(n) + " ordered " + text[loc[1]:]
	}
	return text
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

// CheckRESTPayloadValues checks the values of the request payload tables
// (`the request payload properties are:`): a value in single quotes, like
// '{"name":"Ada"}', keeps the quotes, so the property is that text rather
// than the JSON or the string inside, though single quotes do quote a
// value in a step's own text. Findings are warnings.
func CheckRESTPayloadValues(reg *match.Registry, pickles []*feature.Pickle, workDir string) RuleResult {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	rr := RuleResult{
		Name:        "REST payload values",
		ID:          RESTValuesRuleID,
		Description: "In a request payload table, single quotes are part of the value: JSON goes without quotes, a string in double quotes.",
		Type:        "builtin",
		Mode:        ModeWarn,
		Findings:    []Finding{},
	}
	seen := map[string]bool{}
	files := map[string]bool{}
	for _, p := range pickles {
		files[p.Doc.Path] = true
		for _, ps := range p.Steps {
			if ps.Argument == nil || ps.Argument.DataTable == nil {
				continue
			}
			ms := reg.Match(ps.Text)
			if len(ms) != 1 || ms[0].Def().Step.ID != "rest.request.properties" {
				continue
			}
			src := p.StepSource(ps)
			for _, row := range ps.Argument.DataTable.Rows {
				if len(row.Cells) != 2 {
					continue
				}
				path, v := strings.TrimSpace(row.Cells[0].Value), strings.TrimSpace(row.Cells[1].Value)
				if len(v) < 2 || v[0] != '\'' || v[len(v)-1] != '\'' {
					continue
				}
				inner := v[1 : len(v)-1]
				fix := `"` + clipValue(inner) + `" in double quotes for the string`
				if t := strings.TrimSpace(inner); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
					fix = clipValue(inner) + " without the quotes for the JSON"
				}
				l := Location{File: relSlash(workDir, p.Doc.Path), Line: src.Line, Text: clip(strings.TrimSpace(src.Keyword) + " " + ps.Text), abs: p.Doc.Path}
				once := fmt.Sprintf("%s:%d:%s", l.abs, l.Line, path)
				if seen[once] {
					continue
				}
				seen[once] = true
				rr.Findings = append(rr.Findings, Finding{
					Code:     CodeRESTQuoted,
					Severity: SeverityWarning,
					Message: fmt.Sprintf("%s is set to the text %s, single quotes and all: in a payload table, single quotes are part of the value; write %s",
						path, clipValue(v), fix),
					Locations: []Location{l},
				})
			}
		}
	}
	for f := range files {
		rr.files = append(rr.files, f)
	}
	rr.Files = len(rr.files)
	sortFindings(rr.Findings)
	return rr
}

// clipValue shortens a table value for a message.
func clipValue(s string) string {
	if r := []rune(s); len(r) > 40 {
		return string(r[:39]) + "…"
	}
	return s
}
