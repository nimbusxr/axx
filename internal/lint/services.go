package lint

import (
	"fmt"
	"os"
	"strings"

	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// The services check. The REST, SQL, MongoDB and Kafka packs find a
// scenario's services by name, or take the first it registered; a step of
// theirs before the scenario registers one of the pack's services fails at
// runtime ("No database services set"). A project's own pack may register
// services out of sight (through a pack's context, or in a hook), so the
// check is off when the project loads one.

// servicePack is how a pack's services are registered, and which of its
// steps need none.
type servicePack struct {
	register  string   // the step that registers a service
	kind      string   // what the pack calls a service
	example   string   // the registration step, as a feature writes it
	noService []string // steps that use no service
}

var servicePacks = map[string]servicePack{
	"rest":  {register: "rest.service", kind: "REST service", example: "the parcels service with the following properties:", noService: []string{"rest.token"}},
	"sql":   {register: "sql.service", kind: "database", example: "a parcels-db database with the following properties:"},
	"mongo": {register: "mongo.service", kind: "MongoDB database", example: "a tracking-db mongo database with the following properties:"},
	"kafka": {register: "kafka.service", kind: "Kafka service", example: "the events kafka service with the following properties:"},
}

// ServicesRuleID is the id of the builtin services check.
const ServicesRuleID = "services-registered"

// CheckServices warns about a step that needs a pack's service before the
// scenario registers any. Findings are warnings. workDir is what reported
// paths are relative to.
func CheckServices(reg *match.Registry, pickles []*feature.Pickle, workDir string) RuleResult {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	rr := RuleResult{
		Name:        "Services registered before use",
		ID:          ServicesRuleID,
		Description: "The REST, SQL, MongoDB and Kafka steps use the services a scenario registered before them.",
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
	for _, d := range reg.Defs() {
		if !published(d.Pack) {
			return rr // a project's pack may register services
		}
	}
	seen := map[string]bool{}
	for _, p := range pickles {
		registered := map[string]bool{}
		for _, ps := range p.Steps {
			ms := reg.Match(ps.Text)
			if len(ms) != 1 {
				continue
			}
			def := ms[0].Def()
			sp, ok := servicePacks[def.Pack]
			if !ok {
				continue
			}
			switch {
			case def.Step.ID == sp.register:
				registered[def.Pack] = true
				continue
			case registered[def.Pack]:
				continue
			}
			needs := true
			for _, id := range sp.noService {
				if def.Step.ID == id {
					needs = false
				}
			}
			if !needs {
				continue
			}
			registered[def.Pack] = true // report the first step only
			src := p.StepSource(ps)
			l := Location{File: relSlash(workDir, p.Doc.Path), Line: src.Line, Text: clip(strings.TrimSpace(src.Keyword) + " " + ps.Text), abs: p.Doc.Path}
			once := fmt.Sprintf("%s:%d", l.abs, l.Line)
			if seen[once] {
				continue
			}
			seen[once] = true
			rr.Findings = append(rr.Findings, Finding{
				Code:     CodeServiceUnregistered,
				Severity: SeverityWarning,
				Message: fmt.Sprintf("this step needs a %s, but the scenario registers none before it, so it fails: register one first (in the Background for every scenario), like `%s`",
					sp.kind, sp.example),
				Locations: []Location{l},
			})
		}
	}
	sortFindings(rr.Findings)
	return rr
}
