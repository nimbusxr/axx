// Package controlscore is the controls-core pack: which of the controls a
// project is required to cover its scenarios prove. A scenario claims a
// control with a tag (@asvs:6.2.1), a list says which controls are required,
// and a run writes down what it proved (its evidence); `axx controls report`
// shows every control with what covers it, the scenarios and what other
// sources say, and `axx controls check` fails when a required control is not
// covered.
//
// It knows no framework itself. A pack that carries one registers it
// (RegisterFramework), as controls-asvs does; one that writes the report in a
// format of its own registers that (RegisterFormat), as controls-oscal does;
// and one that reads another tool's results as evidence registers a source
// (RegisterSource). It has no steps.
package controlscore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/version"
)

// Name is the pack's name.
const Name = "controls-core"

// Pack returns the controls-core pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Experimental: true,
		ConfigSchema: []byte(configSchema), Steps: []core.StepDef{},
		Commands:      []core.Command{controlsCommand()},
		FeatureChecks: []core.FeatureCheck{tagsCheck()},
		Tools:         tools(),
	}
}

const packDoc = `Show which of the controls a project is required to cover its scenarios prove. A scenario claims a control with a tag, ` + "`@asvs:6.2.1`" + `; a tag on a Feature or a Rule covers every scenario under it. ` + "`require`" + ` names the lists of controls the project is to cover: a list a pack carries (` + "`controls-asvs`" + ` has OWASP ASVS's levels), or a file of the project's own, for any other framework. Each run writes what it proved to ` + "`.axx/controls/run.json`" + ` and ends with a summary. A run never fails because of a control.

- **The report.** ` + "`axx controls report`" + ` lists every required control with what covers it: the scenarios that claim it, what other sources say (` + "`evidence`" + `: a pen test, a scanner, a review) and the exceptions the project recorded (` + "`exceptions`" + `). It writes text, Markdown, CSV and JSON, and the formats other packs add (` + "`controls-oscal`" + `).
- **The gate.** ` + "`axx controls check`" + ` fails when a required control is not covered. It judges the evidence of one run or of several together, as the jobs of a CI run each write their own.
- **Finding a control.** ` + "`axx controls list <words>`" + ` finds the controls that mention them, and ` + "`axx controls show <control>`" + ` shows one with its text and what covers it.

` + "```yaml" + `
packs:
  controls-core:
    require: [asvs-5.0-level-1, security/pci.yaml]
    evidence: [security/pen-test-2027-03.yaml]
    exceptions:
      - control: asvs:5.2.1
        kind: not-applicable
        reason: The service takes no files.
` + "```"

// settingsOf decodes the pack's settings from a suite.
func settingsOf(s *core.Suite) func(v any) error {
	return func(v any) error { return s.PackConfig(Name, v) }
}

// FinishRun writes down what the run proved, and says how the controls stand.
// A project that requires no list and whose scenarios claim no control has no
// use for the pack yet: nothing is written, and nothing said.
func (pack) FinishRun(ctx context.Context, s *core.Suite, run core.Run) error {
	p, err := loadProject(s.ProjectDir(), settingsOf(s), overrides{})
	if err != nil {
		return err
	}
	var scenarios []Scenario
	for _, o := range run.Scenarios {
		controls, unknown := p.claims(o.Tags)
		if len(controls) == 0 && len(unknown) == 0 {
			continue
		}
		scenarios = append(scenarios, Scenario{URI: filepath.ToSlash(o.URI), Line: o.Line, Name: o.Name, Status: o.Status, Controls: controls, Unknown: unknown})
	}
	if len(p.lists) == 0 && len(scenarios) == 0 {
		return nil
	}
	ev := &Evidence{
		Version: EvidenceVersion, Kind: KindRun, Axx: version.Get().Version,
		Started: run.Started.UTC().Truncate(time.Second), Ended: run.Ended.UTC().Truncate(time.Second), Shard: run.Shard, Partial: run.Partial,
		Frameworks: p.versions(scenarios), Scenarios: scenarios,
	}
	ev.Commit, ev.Dirty = commitOf(ctx, p.dir)
	file := filepath.Join(p.path(p.cfg.Folder), runFile)
	if err := ev.write(file); err != nil {
		return fmt.Errorf("cannot write the run's evidence of its controls: %w", err)
	}
	others, err := p.otherEvidence(nil)
	if err != nil {
		return err
	}
	r := p.report(ev, others, time.Now())
	summarize(run.Say, r, run, filepath.ToSlash(filepath.Join(p.cfg.Folder, runFile)))
	return nil
}

// versions are the versions of the frameworks a run's evidence is of: those
// of the lists the project requires, and those its scenarios claim.
func (p *project) versions(scenarios []Scenario) map[string]string {
	out := map[string]string{}
	for _, l := range p.lists {
		out[l.Framework.Key] = l.Framework.Version
	}
	for _, sc := range scenarios {
		for _, c := range sc.Controls {
			if key, _, ok := splitRef(c); ok && p.frameworks[key] != nil {
				out[key] = p.frameworks[key].Version
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// otherEvidence reads what covers controls besides the scenarios: the files
// the settings name, and the ones a command was given.
func (p *project) otherEvidence(given []*Evidence) ([]*Evidence, error) {
	var out []*Evidence
	for _, name := range p.cfg.Evidence {
		e, err := readEvidence(p.path(name))
		if err != nil {
			return nil, err
		}
		if e.Kind != KindOther {
			return nil, evidenceError("packs.%s.evidence names %s, which is a run's evidence", Name, name).
				WithHint("`evidence` is for what covers controls besides the scenarios: a file that names its `source` and the controls it covers or fails")
		}
		out = append(out, e)
	}
	return append(out, given...), nil
}

// maxSaid is how many controls a run's summary names: the report has them all.
const maxSaid = 8

// summarize says how the controls stand after a run. A run of every scenario
// says how the required controls stand, and names those that are not covered.
// A run of part of the suite says only what it proved: what it leaves not
// covered may be what its scenarios did not get to. One whose scenarios claim
// no control says nothing: it is a run of something else.
func summarize(say func(string), r *Report, run core.Run, file string) {
	if run.Shard != "" || run.Partial != "" {
		var proven []string
		failing := 0
		for _, cr := range r.Controls {
			switch {
			case len(cr.Scenarios) == 0:
			case cr.State == Failing:
				failing++
			case passing(cr.Scenarios) > 0:
				proven = append(proven, cr.Control)
			}
		}
		if len(proven) == 0 && failing == 0 && len(r.Unknown) == 0 {
			return
		}
		line := fmt.Sprintf("controls: this run proved %s", plural(len(proven), "control"))
		if len(proven) > 0 {
			line += ": " + clipList(proven)
		}
		if failing > 0 {
			line += fmt.Sprintf("; %s it claims did not pass", plural(failing, "control"))
		}
		say(line)
		why := run.Partial
		if run.Shard != "" {
			why = "it is shard " + run.Shard
		}
		say(fmt.Sprintf("  it is not a run of every scenario (%s): `axx controls check` judges the runs together (%s)", why, file))
		saidUnknown(say, r)
		return
	}
	say("controls: " + headline(r))
	unmet := r.Unmet()
	width := 0
	for i, cr := range unmet {
		if i < maxSaid {
			width = max(width, len(cr.Control))
		}
	}
	for i, cr := range unmet {
		if i == maxSaid {
			say(fmt.Sprintf("  and %d more: `axx controls report` lists them", len(unmet)-maxSaid))
			break
		}
		say(fmt.Sprintf("  %-*s  %-13s  %s", width, cr.Control, cr.State, cr.why()))
	}
	saidUnknown(say, r)
}

func saidUnknown(say func(string), r *Report) {
	for _, u := range r.Unknown {
		line := fmt.Sprintf("  %s names no control of its framework", u.Tag)
		if u.Nearest != "" {
			line += ": the nearest is " + u.Nearest
		}
		say(fmt.Sprintf("%s (%s:%d)", line, u.Scenarios[0].URI, u.Scenarios[0].Line))
	}
}

func clipList(items []string) string {
	if len(items) <= maxSaid {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:maxSaid], ", "), len(items)-maxSaid)
}

// tagsCheck is the pack's check of the features: a tag of a framework the
// project has names a control that framework has.
func tagsCheck() core.FeatureCheck {
	return core.FeatureCheck{
		ID:          "control-tags",
		Name:        "Control tags",
		Description: "A tag that claims a control (@asvs:6.2.1) names a control its framework has: a mistyped one proves nothing.",
		Run: func(call *core.FeatureCheckCall) []core.FeatureFinding {
			p, err := loadProject(call.ProjectDir, call.Settings, overrides{})
			if err != nil {
				return nil // the commands that read the settings say what is wrong with them
			}
			var out []core.FeatureFinding
			for _, sc := range call.Scenarios {
				for _, tag := range sc.Tags {
					key, id, ok := splitRef(tag)
					f := p.frameworks[key]
					if !ok || f == nil {
						continue
					}
					if _, known := f.control(id); known {
						continue
					}
					msg := fmt.Sprintf("%s names no control of %s", tag, describe(f))
					if near := f.nearest(id); near != "" {
						msg += ": the nearest is @" + key + ":" + near
					}
					msg += ". `axx controls list <words>` finds a control by a word of it"
					out = append(out, core.FeatureFinding{URI: sc.URI, Line: sc.Line, Tag: tag, Message: msg})
				}
			}
			return out
		},
	}
}
