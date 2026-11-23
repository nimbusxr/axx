package controlscore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// The depot's house rules, as a project lists them in a file of its own: the
// file is the framework and the list.
const houseRules = `framework: house
title: House rules
version: "2027.1"
controls:
  password-length: Passwords are at least 12 characters long
  lockout:
    title: An account locks after five wrong passwords
    scenarios: 2
  session-end: A session ends after 30 minutes without use
  uploads: Uploaded files are scanned
  audit: Admin actions are written to the audit log
`

// newProject is a project's folder with its settings and files, and the
// settings as a command or a run decodes them.
func newProject(t *testing.T, settings string, files map[string]string) (dir string, decode func(v any) error) {
	t.Helper()
	dir = t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, func(v any) error {
		if strings.TrimSpace(settings) == "" {
			return nil
		}
		return json.Unmarshal([]byte(settings), v)
	}
}

func house(t *testing.T, settings string, files map[string]string) *project {
	t.Helper()
	if files == nil {
		files = map[string]string{}
	}
	files["security/house.yaml"] = houseRules
	if settings == "" {
		settings = `{"require": ["security/house.yaml"]}`
	}
	dir, decode := newProject(t, settings, files)
	p, err := loadProject(dir, decode, overrides{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func scenario(line int, name, status string, controls ...string) Scenario {
	return Scenario{URI: "features/sign-in/sign-in.feature", Line: line, Name: name, Status: status, Controls: controls}
}

func run(scenarios ...Scenario) *Evidence {
	return &Evidence{
		Version: EvidenceVersion, Kind: KindRun, Axx: "0.8.0", Commit: "4f2a9c1d7e3b5a6f", Ended: time.Date(2027, 3, 20, 9, 0, 0, 0, time.UTC),
		Frameworks: map[string]string{"house": "2027.1"}, Scenarios: scenarios,
	}
}

func states(r *Report) map[string]State {
	out := map[string]State{}
	for _, cr := range r.Controls {
		out[cr.Control] = cr.State
	}
	return out
}

// A scenario claims a control with a tag of a framework the project has. A tag
// of that framework whose control it has not is told apart, and any other tag
// is no claim at all.
func TestAScenariosTagsClaimControls(t *testing.T) {
	p := house(t, "", nil)
	controls, unknown := p.claims([]string{"@house:lockout", "@smoke", "@HOUSE:Session-End", "@house:sesion-end", "@route:LEJ", "@house"})
	if strings.Join(controls, " ") != "house:lockout house:session-end" {
		t.Errorf("the controls claimed: %q", controls)
	}
	if strings.Join(unknown, " ") != "house:sesion-end" {
		t.Errorf("the tags that name no control: %q", unknown)
	}
}

// How a required control stands: what fails comes first, then what proves it,
// what else covers it, and an exception that still counts.
func TestHowAControlStands(t *testing.T) {
	p := house(t, `{"require": ["security/house.yaml"], "exceptions": [
		{"control": "house:uploads", "kind": "not-applicable", "reason": "The service takes no files."}
	]}`, nil)
	pen := &Evidence{Kind: KindOther, Source: "Pen test, March 2027", Link: "https://example.com/pen-test", Findings: []Finding{{Control: "house:audit", State: "covered"}}}
	r := p.report(run(
		scenario(8, "A password under 12 characters is refused", "passed", "house:password-length"),
		scenario(13, "An account locks after five wrong passwords", "passed", "house:lockout"),
	), []*Evidence{pen}, time.Now())
	want := map[string]State{"house:password-length": Proven, "house:lockout": PartlyProven, "house:session-end": NotCovered, "house:uploads": Excepted, "house:audit": Covered}
	for control, state := range want {
		if got := states(r)[control]; got != state {
			t.Errorf("%s is %s, want %s", control, got, state)
		}
	}
	c := r.Counts()
	if c.Required != 5 || c.Proven != 1 || c.Covered != 1 || c.Excepted != 1 || c.Partly != 1 || c.NotCovered != 1 || c.Failing != 0 {
		t.Errorf("counts: %+v", c)
	}
	if got := headline(r); got != "2 of 5 required are covered (1 proven, 1 by other evidence), 1 excepted, 1 partly proven, 1 not covered" {
		t.Errorf("the headline: %s", got)
	}
	var unmet []string
	for _, cr := range r.Unmet() {
		unmet = append(unmet, cr.Control+": "+cr.why())
	}
	if strings.Join(unmet, "; ") != "house:lockout: 1 scenario of the 2 it takes; house:session-end: no scenario is tagged @house:session-end" {
		t.Errorf("what is not covered: %s", unmet)
	}
}

// A control that takes two scenarios is proven by two that pass, each row of
// an outline a scenario of its own. Every scenario that claims a control must
// pass: one that does not makes it failing, whatever else covers it.
func TestSeveralScenariosForOneControl(t *testing.T) {
	p := house(t, "", nil)
	r := p.report(run(
		scenario(13, "An account locks after five wrong passwords", "passed", "house:lockout"),
		scenario(21, "A locked account stays locked for a minute", "passed", "house:lockout"),
	), nil, time.Now())
	if got := states(r)["house:lockout"]; got != Proven {
		t.Errorf("two passing scenarios of the two it takes: %s", got)
	}
	outline := Scenario{URI: "features/sign-in/lockout.feature", Name: "An account locks", Status: "passed", Controls: []string{"house:lockout"}}
	first, second := outline, outline
	first.Line, second.Line = 30, 31
	if got := states(p.report(run(first, second), nil, time.Now()))["house:lockout"]; got != Proven {
		t.Errorf("two rows of an outline: %s", got)
	}
	pen := &Evidence{Kind: KindOther, Source: "Pen test", Findings: []Finding{{Control: "house:lockout", State: "covered"}}}
	r = p.report(run(
		scenario(13, "An account locks after five wrong passwords", "passed", "house:lockout"),
		scenario(21, "A locked account stays locked for a minute", "failed", "house:lockout"),
	), []*Evidence{pen}, time.Now())
	lockout := r.Controls[1]
	if lockout.State != Failing || lockout.why() != "features/sign-in/sign-in.feature:21 failed" {
		t.Errorf("one of its scenarios failed: %s (%s)", lockout.State, lockout.why())
	}
	// A scenario that was skipped proves nothing, and fails nothing.
	r = p.report(run(scenario(8, "A password under 12 characters is refused", "skipped", "house:password-length")), nil, time.Now())
	if got := states(r)["house:password-length"]; got != NotCovered {
		t.Errorf("a skipped scenario: %s", got)
	}
}

// An accepted exception counts until its day, judged at the time of the run,
// and a source that says a control fails makes it failing.
func TestExceptionsEndAndSourcesCanFailAControl(t *testing.T) {
	p := house(t, `{"require": ["security/house.yaml"], "exceptions": [
		{"control": "house:session-end", "kind": "accepted", "reason": "The session store moves in May.", "until": "2027-03-20"}
	]}`, nil)
	at := func(day int) *Evidence {
		e := run()
		e.Ended = time.Date(2027, 3, day, 23, 0, 0, 0, time.UTC)
		return e
	}
	if got := states(p.report(at(20), nil, time.Now()))["house:session-end"]; got != Excepted {
		t.Errorf("on its last day: %s", got)
	}
	r := p.report(at(21), nil, time.Now())
	ended := r.Controls[2]
	if ended.State != NotCovered || !ended.Expired || ended.why() != "its exception ended on 2027-03-20" {
		t.Errorf("the day after: %s, expired %v (%s)", ended.State, ended.Expired, ended.why())
	}
	scan := &Evidence{Kind: KindOther, Source: "Scanner", Findings: []Finding{{Control: "house:audit", State: "failing", Note: "finding 12"}}}
	r = p.report(run(), []*Evidence{scan}, time.Now())
	if audit := r.Controls[4]; audit.State != Failing || audit.why() != "Scanner: finding 12" {
		t.Errorf("a source says it fails: %s (%s)", audit.State, audit.why())
	}
}

// A tag that names a control its framework has not is reported with the
// nearest control it has, and a control that is covered and in no required
// list is reported after the required ones.
func TestUnknownTagsAndControlsThatAreNotRequired(t *testing.T) {
	dir, decode := newProject(t, `{"require": ["security/short.yaml"]}`, map[string]string{
		"security/house.yaml": houseRules,
		"security/short.yaml": "framework: house\ncontrols:\n  password-length: Passwords are at least 12 characters long\n",
	})
	p, err := loadProject(dir, decode, overrides{})
	if err != nil {
		t.Fatal(err)
	}
	// The scenarios also claim a control of the house rules that the short
	// list does not require: the framework is the one file's controls.
	lost := scenario(18, "A session ends", "passed")
	lost.Unknown = []string{"house:sesion-end"}
	r := p.report(run(scenario(8, "A password under 12 characters is refused", "passed", "house:password-length"), lost), nil, time.Now())
	if len(r.Unknown) != 1 || r.Unknown[0].Tag != "@house:sesion-end" || r.Unknown[0].Nearest != "" {
		t.Errorf("the unknown tag, of a framework with one control: %+v", r.Unknown)
	}

	full := house(t, "", nil)
	lost.Unknown = []string{"house:sesion-end"}
	r = full.report(run(lost), nil, time.Now())
	if len(r.Unknown) != 1 || r.Unknown[0].Nearest != "@house:session-end" || r.Unknown[0].Scenarios[0].Line != 18 {
		t.Errorf("the unknown tag: %+v", r.Unknown)
	}
}

// The evidence of several runs is judged as one run's: the shards of a run,
// or jobs that each ran part of the suite. A scenario two of them ran counts
// at its worse.
func TestRunsAreJudgedTogether(t *testing.T) {
	p := house(t, "", nil)
	whole := run(
		scenario(8, "A password under 12 characters is refused", "passed", "house:password-length"),
		scenario(13, "An account locks after five wrong passwords", "passed", "house:lockout"),
		scenario(21, "A locked account stays locked for a minute", "passed", "house:lockout"),
	)
	first, second := run(whole.Scenarios[0], whole.Scenarios[2]), run(whole.Scenarios[1])
	first.Shard, second.Shard = "1/2", "2/2"
	merged, err := merge([]*Evidence{second, first})
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.Scenarios) != 3 || merged.Scenarios[0].Line != 8 || merged.Scenarios[2].Line != 21 || merged.Kind != KindMerged || merged.Partial != "" {
		t.Fatalf("the merged evidence: %+v", merged)
	}
	a, b := p.report(whole, nil, time.Now()), p.report(merged, nil, time.Now())
	for control, state := range states(a) {
		if states(b)[control] != state {
			t.Errorf("%s: %s in one run, %s in two shards", control, state, states(b)[control])
		}
	}
	again := run(scenario(13, "An account locks after five wrong passwords", "failed", "house:lockout"))
	merged, err = merge([]*Evidence{whole, again})
	if err != nil {
		t.Fatal(err)
	}
	if got := states(p.report(merged, nil, time.Now()))["house:lockout"]; got != Failing {
		t.Errorf("a scenario that failed in one of two runs: %s", got)
	}
}

// Runs of different commits, by different versions of axx, or of another
// version of a framework are not judged together.
func TestEvidenceThatCannotBeJudgedTogetherIsRefused(t *testing.T) {
	other := func(change func(e *Evidence)) error {
		e := run()
		change(e)
		_, err := merge([]*Evidence{run(), e})
		return err
	}
	for name, err := range map[string]error{
		"another commit":               other(func(e *Evidence) { e.Commit = "9b8c7d6e5f4a3b2c" }),
		"another axx":                  other(func(e *Evidence) { e.Axx = "0.9.0" }),
		"another version of the rules": other(func(e *Evidence) { e.Frameworks = map[string]string{"house": "2026.4"} }),
	} {
		var ae *axxerr.Error
		if !errors.As(err, &ae) || ae.Code != CodeMixed {
			t.Errorf("%s: %v", name, err)
		}
	}
	// And evidence of a version of a framework the project no longer has.
	dir, decode := newProject(t, `{"require": ["security/house.yaml"]}`, map[string]string{"security/house.yaml": houseRules})
	old := run(scenario(8, "A password under 12 characters is refused", "passed", "house:password-length"))
	old.Frameworks = map[string]string{"house": "2026.4"}
	if err := old.write(filepath.Join(dir, ".axx", "controls", runFile)); err != nil {
		t.Fatal(err)
	}
	p, err := loadProject(dir, decode, overrides{})
	if err != nil {
		t.Fatal(err)
	}
	var ae *axxerr.Error
	if _, _, err := p.judge(nil); !errors.As(err, &ae) || ae.Code != CodeMixed {
		t.Errorf("evidence of the rules as they were: %v", err)
	}
}

// What a person writes of another source names the source and each control it
// covers or fails; what a tool writes lists its findings. Anything else is no
// evidence.
func TestEvidenceIsRead(t *testing.T) {
	written := `source: Pen test, March 2027
link: https://example.com/reports/pen-test-2027-03
date: 2027-03-14
controls:
  house:audit: covered
  house:lockout:
    state: failing
    note: Finding 12, open
`
	e, err := parseEvidence([]byte(written))
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != KindOther || e.Source != "Pen test, March 2027" || e.Date != "2027-03-14" || len(e.Findings) != 2 ||
		e.Findings[0] != (Finding{Control: "house:audit", State: "covered"}) || e.Findings[1] != (Finding{Control: "house:lockout", State: "failing", Note: "Finding 12, open"}) {
		t.Errorf("what a person wrote: %+v", e)
	}
	tool, err := parseEvidence([]byte(`{"source": "Scanner", "controls": [{"control": "house:audit", "state": "Covered"}]}`))
	if err != nil || tool.Findings[0].State != "covered" {
		t.Errorf("what a tool wrote: %+v %v", tool, err)
	}
	b, _ := json.Marshal(run(scenario(8, "A password under 12 characters is refused", "passed", "house:password-length")))
	if back, err := parseEvidence(b); err != nil || back.Kind != KindRun || len(back.Scenarios) != 1 {
		t.Errorf("a run's evidence, read back: %+v %v", back, err)
	}
	for name, bad := range map[string]string{
		"no source":            "controls:\n  house:audit: covered\n",
		"a state it has not":   "source: Pen test\ncontrols:\n  house:audit: fine\n",
		"neither":              "title: something else\n",
		"of a later version":   `{"version": 99, "kind": "run"}`,
		"a key evidence lacks": "source: Pen test\nfindings: 3\n",
	} {
		if _, err := parseEvidence([]byte(bad)); err == nil {
			t.Errorf("%s is taken for evidence", name)
		}
	}
}

// A list of the project's own that is of a framework a pack carries requires
// that framework's controls: an ID it has not is refused, with the nearest it
// has. A list no pack carries, and an exception that says too little, are
// refused too.
func TestSettingsThatDoNotHoldAreRefused(t *testing.T) {
	RegisterFramework(Framework{
		Key: "depot", Title: "Depot standard", Version: "3", Controls: []Control{{ID: "D-1.1", Title: "Doors"}, {ID: "D-1.2", Title: "Docks"}},
		Lists: []List{{Name: "depot-3-basic", Title: "Depot standard 3, basic", Controls: []Required{{ID: "D-1.1"}}}},
	})
	for name, c := range map[string]struct {
		settings string
		files    map[string]string
		want     string
	}{
		"a list no pack carries":         {`{"require": ["depot-3-full"]}`, nil, "depot-3-basic"},
		"a file that is not there":       {`{"require": ["security/missing.yaml"]}`, nil, "security/missing.yaml"},
		"a file that names no framework": {`{"require": ["security/list.yaml"]}`, map[string]string{"security/list.yaml": "controls:\n  a: b\n"}, "names no framework"},
		"a control the framework has not": {
			`{"require": ["security/list.yaml"]}`,
			map[string]string{"security/list.yaml": "framework: depot\ncontrols:\n  D-1.3:\n"},
			"the nearest it has is D-1.1",
		},
		"an exception without a reason":       {`{"exceptions": [{"control": "depot:D-1.1", "kind": "not-applicable", "reason": " "}]}`, nil, "gives no reason"},
		"an accepted exception with no day":   {`{"exceptions": [{"control": "depot:D-1.1", "kind": "accepted", "reason": "Later."}]}`, nil, "ends on a day"},
		"an exception of a kind it has not":   {`{"exceptions": [{"control": "depot:D-1.1", "kind": "waived", "reason": "Later."}]}`, nil, "not-applicable"},
		"an exception for no known control":   {`{"exceptions": [{"control": "depot:D-9.9", "kind": "not-applicable", "reason": "None."}]}`, nil, "which Depot standard 3 has not"},
		"a list whose scenarios are no count": {`{"require": ["security/list.yaml"]}`, map[string]string{"security/list.yaml": "framework: own\ncontrols:\n  a: {title: A, scenarios: none}\n"}, "whole number"},
	} {
		dir, decode := newProject(t, c.settings, c.files)
		_, err := loadProject(dir, decode, overrides{})
		var ae *axxerr.Error
		if !errors.As(err, &ae) || ae.Code != CodeSettings || !strings.Contains(ae.Message+" "+ae.Hint, c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// One of a framework a pack carries: its controls are the framework's, as
	// the framework writes them.
	dir, decode := newProject(t, `{"require": ["security/list.yaml", "depot-3-basic"]}`, map[string]string{"security/list.yaml": "framework: depot\ncontrols:\n  d-1.2:\n"})
	p, err := loadProject(dir, decode, overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.lists) != 2 || p.lists[0].Controls[0].ID != "D-1.2" || p.lists[1].Name != "depot-3-basic" {
		t.Errorf("the lists: %+v", p.lists)
	}
}

// A workflow that every repository must pass names the lists and the
// exceptions to go by: the project's own exceptions are then shown as asked
// for, and do not count.
func TestACommandCanGoByOtherListsAndExceptions(t *testing.T) {
	dir, decode := newProject(t, `{"require": ["security/short.yaml"], "exceptions": [
		{"control": "house:session-end", "kind": "not-applicable", "reason": "We would rather not."}
	]}`, map[string]string{
		"security/house.yaml":    houseRules,
		"security/short.yaml":    "framework: house\ncontrols:\n  password-length: Passwords are at least 12 characters long\n  session-end: A session ends\n",
		"security/approved.yaml": "exceptions:\n  - control: house:uploads\n    kind: not-applicable\n    reason: The service takes no files.\n",
	})
	own, err := loadProject(dir, decode, overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if got := states(own.report(run(), nil, time.Now()))["house:session-end"]; got != Excepted || len(own.lists[0].Controls) != 2 {
		t.Errorf("by the project's own settings: %s", got)
	}
	theirs, err := loadProject(dir, decode, overrides{require: []string{"security/house.yaml"}, exceptions: "security/approved.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	r := theirs.report(run(), nil, time.Now())
	if s := states(r); s["house:session-end"] != NotCovered || s["house:uploads"] != Excepted || len(r.Controls) != 5 {
		t.Errorf("by the security team's: %v", s)
	}
	if len(r.Requested) != 1 || r.Requested[0].Control != "house:session-end" {
		t.Errorf("the project's own exception, asked for: %+v", r.Requested)
	}
}

// What a pack registers is used as what the pack itself has: a framework with
// its own way of writing an ID, a format of the report, and a source that
// reads another tool's file as evidence.
func TestAPackAddsAFrameworkAFormatAndASource(t *testing.T) {
	RegisterFramework(Framework{
		Key: "Carrier", Title: "Carrier agreement", Version: "7", Notice: "Carrier agreement 7, shared under licence",
		Controls: []Control{{ID: "C-4(1)", Title: "Scans are signed", Text: "Every scan a carrier sends is signed with the carrier's key."}},
		Lists:    []List{{Name: "carrier-7", Title: "Carrier agreement 7", Controls: []Required{{ID: "C-4(1)"}}}},
		Same: func(a, b string) bool {
			norm := func(s string) string { return strings.NewReplacer("(", ".", ")", "").Replace(strings.ToLower(s)) }
			return norm(a) == norm(b)
		},
	})
	RegisterFormat(Format{Name: "carrier-sheet", Doc: "the carrier's own sheet", Write: func(w io.Writer, r *Report) error {
		_, err := io.WriteString(w, "sheet of "+r.Controls[0].Control+": "+string(r.Controls[0].State)+"\n")
		return err
	}})
	RegisterSource(Source{Name: "carrier-audit", Doc: "the carrier's audit", Read: func(_ string, data []byte) (*Evidence, bool, error) {
		control, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "AUDIT OK ")
		if !ok {
			return nil, false, nil
		}
		return &Evidence{Source: "Carrier's audit", Findings: []Finding{{Control: control, State: "covered"}}}, true, nil
	}})
	dir, decode := newProject(t, `{"require": ["carrier-7"]}`, map[string]string{"audit.txt": "AUDIT OK carrier:c-4.1\n"})
	p, err := loadProject(dir, decode, overrides{})
	if err != nil {
		t.Fatal(err)
	}
	// A tag writes the control another way than the framework does.
	if controls, _ := p.claims([]string{"@carrier:C-4.1"}); len(controls) != 1 || controls[0] != "carrier:C-4(1)" {
		t.Errorf("the control a tag claims: %q", controls)
	}
	r, judged, err := p.judge([]string{filepath.Join(dir, "audit.txt")})
	if err != nil || judged {
		t.Fatalf("judged %v, %v", judged, err)
	}
	if r.Controls[0].State != Covered || r.Sources[0].Source != "Carrier's audit" || len(r.Notices) != 1 {
		t.Errorf("the report: %+v", r)
	}
	format, ok := formatOf("carrier-sheet")
	var b bytes.Buffer
	if !ok || format.Write(&b, r) != nil || b.String() != "sheet of carrier:C-4(1): covered\n" {
		t.Errorf("the pack's format: %q", b.String())
	}
}

// call runs one of the pack's commands in a project, and returns what it
// printed.
func call(t *testing.T, dir string, decode func(v any) error, args []string, flags map[string]string, lists map[string][]string) (string, any, error) {
	t.Helper()
	var out bytes.Buffer
	var data any
	c := &core.CommandCall{
		Context: context.Background(), Args: args[1:], Flags: map[string]string{"format": "text"}, Lists: lists, Stderr: io.Discard,
		ProjectDir: dir, Settings: decode,
		Emit: func(d any, human func(w io.Writer) error) error {
			data = d
			return human(&out)
		},
	}
	for k, v := range flags {
		c.Flags[k] = v
	}
	for _, cmd := range controlsCommand().Commands {
		if cmd.Name == args[0] {
			err := cmd.Run(c)
			return out.String(), data, err
		}
	}
	t.Fatalf("no command %s", args[0])
	return "", nil, nil
}

// The first minute: a project that has just required a list sees the list with
// nothing proven yet, and what does it. The gate has nothing to judge.
func TestTheReportBeforeAnyRun(t *testing.T) {
	dir, decode := newProject(t, `{"require": ["security/house.yaml"]}`, map[string]string{"security/house.yaml": houseRules})
	out, _, err := call(t, dir, decode, []string{"report"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"controls: 0 of 5 required are covered, 5 not covered", "required: security/house.yaml (House rules 2027.1, 5 controls)",
		"house:lockout          its scenarios have not run yet", "no run has written its evidence yet: `axx run` does",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	_, _, err = call(t, dir, decode, []string{"check"}, nil, nil)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeEvidence || ae.Exit != exitcode.Usage {
		t.Errorf("the check with no evidence: %v", err)
	}
}

// The gate: it passes when every required control is covered, writes what it
// judged with its hash, and fails with what is not covered.
func TestTheCheck(t *testing.T) {
	settings := `{"require": ["security/house.yaml"], "evidence": ["security/pen-test.yaml"], "exceptions": [
		{"control": "house:uploads", "kind": "not-applicable", "reason": "The service takes no files."}
	]}`
	files := map[string]string{
		"security/house.yaml":    houseRules,
		"security/pen-test.yaml": "source: Pen test, March 2027\ncontrols:\n  house:audit: covered\n",
	}
	dir, decode := newProject(t, settings, files)
	first := run(
		scenario(8, "A password under 12 characters is refused", "passed", "house:password-length"),
		scenario(13, "An account locks after five wrong passwords", "passed", "house:lockout"),
	)
	second := run(
		scenario(21, "A locked account stays locked for a minute", "passed", "house:lockout"),
		scenario(27, "A session ends", "passed", "house:session-end"),
	)
	for name, e := range map[string]*Evidence{"shard-1": first, "shard-2": second} {
		if err := e.write(filepath.Join(dir, name, runFile)); err != nil {
			t.Fatal(err)
		}
	}
	out, data, err := call(t, dir, decode, []string{"check", filepath.Join(dir, "shard-1"), filepath.Join(dir, "shard-2")}, nil, nil)
	if err != nil {
		t.Fatalf("every control is covered: %v\n%s", err, out)
	}
	res := data.(checkResult)
	if !res.OK || !strings.HasPrefix(out, "controls: 4 of 5 required are covered (3 proven, 1 by other evidence), 1 excepted\nsha256 "+res.SHA256) {
		t.Errorf("the check's result:\n%s", out)
	}
	judged, err := os.ReadFile(filepath.Join(dir, ".axx", "controls", "checked.json"))
	if err != nil || !strings.Contains(string(judged), `"control": "house:lockout"`) {
		t.Errorf("what it judged: %v", err)
	}
	// The same evidence gives the same file, and the same hash.
	_, again, _ := call(t, dir, decode, []string{"check", filepath.Join(dir, "shard-2"), filepath.Join(dir, "shard-1")}, nil, nil)
	if again.(checkResult).SHA256 != res.SHA256 {
		t.Error("the same evidence in another order has another hash")
	}

	// One shard alone leaves controls not covered: the gate fails, exit 1.
	out, data, err = call(t, dir, decode, []string{"check", filepath.Join(dir, "shard-1")}, nil, nil)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeNotCovered || ae.Exit != exitcode.Failed || ae.Message != "2 required controls are not covered" {
		t.Fatalf("the check of one shard: %v", err)
	}
	if res := data.(checkResult); res.OK || len(res.Unmet) != 2 || !strings.Contains(out, "house:lockout      partly proven  1 scenario of the 2 it takes") {
		t.Errorf("what is not covered:\n%s", out)
	}
	// A project that requires no list has nothing to check.
	empty, none := newProject(t, "", nil)
	if _, _, err := call(t, empty, none, []string{"check"}, nil, nil); !errors.As(err, &ae) || ae.Code != CodeSettings {
		t.Errorf("a project that requires no list: %v", err)
	}
}

// The report in the formats the pack writes itself.
func TestTheReportsFormats(t *testing.T) {
	dir, decode := newProject(t, `{"require": ["security/house.yaml"]}`, map[string]string{"security/house.yaml": houseRules})
	e := run(scenario(8, "A password | under 12 characters is refused", "passed", "house:password-length"))
	if err := e.write(filepath.Join(dir, ".axx", "controls", runFile)); err != nil {
		t.Fatal(err)
	}
	md, _, err := call(t, dir, decode, []string{"report"}, map[string]string{"format": "md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Controls", "**1 of 5 required are covered, 4 not covered.**", "## Proven (1)",
		"| `house:password-length` | Passwords are at least 12 characters long | A password \\| under 12 characters is refused (`features/sign-in/sign-in.feature:8`, passed) |",
		"| `house:lockout` | An account locks after five wrong passwords (takes 2 scenarios) | Nothing: no scenario is tagged `@house:lockout` |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in the Markdown report:\n%s", want, md)
		}
	}
	sheet, _, err := call(t, dir, decode, []string{"report"}, map[string]string{"format": "csv"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(sheet), "\n")
	if len(lines) != 6 || !strings.HasPrefix(lines[0], "control,framework,id,title,group,required,list,state,") ||
		!strings.HasPrefix(lines[1], "house:password-length,house,password-length,Passwords are at least 12 characters long,,yes,security/house.yaml,proven,1,1,") {
		t.Errorf("the CSV report:\n%s", sheet)
	}
	data, _, err := call(t, dir, decode, []string{"report"}, map[string]string{"format": "json"}, nil)
	var doc struct {
		Counts   Counts          `json:"counts"`
		Controls []ControlResult `json:"controls"`
	}
	if err != nil || json.Unmarshal([]byte(data), &doc) != nil || doc.Counts.Required != 5 || doc.Controls[0].State != Proven {
		t.Errorf("the JSON report: %v\n%s", err, data)
	}
	// To a file, and a format no pack writes.
	file := filepath.Join(dir, "out", "controls.csv")
	if out, _, err := call(t, dir, decode, []string{"report"}, map[string]string{"format": "csv", "out": file}, nil); err != nil || !strings.Contains(out, "wrote "+file) {
		t.Errorf("to a file: %v %s", err, out)
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != sheet {
		t.Errorf("the file is not the report: %v", err)
	}
	var ae *axxerr.Error
	if _, _, err := call(t, dir, decode, []string{"report"}, map[string]string{"format": "pdf"}, nil); !errors.As(err, &ae) || ae.Code != CodeUnknown || !strings.Contains(ae.Hint, "markdown") {
		t.Errorf("a format no pack writes: %v", err)
	}
}

// Finding a control by a word of it, and looking one up.
func TestListAndShow(t *testing.T) {
	dir, decode := newProject(t, `{"require": ["security/house.yaml"]}`, map[string]string{"security/house.yaml": houseRules})
	out, _, err := call(t, dir, decode, []string{"list"}, nil, nil)
	if err != nil || !strings.Contains(out, "house        House rules 2027.1, 5 controls") || !strings.Contains(out, "security/house.yaml") || !strings.Contains(out, "(required)") {
		t.Errorf("the frameworks and lists: %v\n%s", err, out)
	}
	out, _, err = call(t, dir, decode, []string{"list", "ACCOUNT", "locks"}, nil, nil)
	if err != nil || !strings.HasPrefix(out, "house:lockout  An account locks after five wrong passwords\n") {
		t.Errorf("the controls that mention the words: %v\n%s", err, out)
	}
	if out, _, _ := call(t, dir, decode, []string{"list", "parcel"}, nil, nil); !strings.Contains(out, "no control of this project's frameworks mentions parcel") {
		t.Errorf("words no control mentions:\n%s", out)
	}
	out, _, err = call(t, dir, decode, []string{"show", "@house:lockout"}, nil, nil)
	for _, want := range []string{
		"house:lockout  An account locks after five wrong passwords", "in House rules 2027.1",
		"required by security/house.yaml, which takes 2 passing scenarios to prove it", "not covered: its scenarios have not run yet",
		"no run has written its evidence yet", "a scenario claims it with the tag @house:lockout",
	} {
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("missing %q (%v) in:\n%s", want, err, out)
		}
	}
	var ae *axxerr.Error
	if _, _, err := call(t, dir, decode, []string{"show", "house:lock-out"}, nil, nil); !errors.As(err, &ae) || ae.Code != CodeUnknown || !strings.Contains(ae.Hint, "house:lockout") {
		t.Errorf("a control the framework has not: %v", err)
	}
	if _, _, err := call(t, dir, decode, []string{"show", "pci:8.3.6"}, nil, nil); !errors.As(err, &ae) || ae.Code != CodeUnknown {
		t.Errorf("a control of a framework the project has not: %v", err)
	}
	// A project with no framework is told how to get one.
	empty, none := newProject(t, "", nil)
	if out, _, _ := call(t, empty, none, []string{"list"}, nil, nil); !strings.Contains(out, "axx pack add controls-asvs") && !strings.Contains(out, "frameworks") {
		t.Errorf("a project with no framework:\n%s", out)
	}
}

// suiteOf is a project's suite, as a run of it has.
func suiteOf(t *testing.T, settings string, files map[string]string) (*core.Suite, string) {
	t.Helper()
	dir, _ := newProject(t, settings, files)
	sections := map[string]json.RawMessage{}
	if strings.TrimSpace(settings) != "" {
		sections[Name] = json.RawMessage(settings)
	}
	return core.NewSuite(core.SuiteOptions{PackConfig: sections, ProjectDir: dir}), dir
}

func outcome(line int, name, status string, tags ...string) core.Outcome {
	return core.Outcome{ScenarioInfo: core.ScenarioInfo{URI: "features/sign-in/sign-in.feature", Line: line, Name: name, Tags: tags}, Status: status}
}

// A run writes down what it proved, and says how the required controls stand:
// those that are not covered by name. It never fails because of a control.
func TestARunWritesItsEvidenceAndSaysHowTheControlsStand(t *testing.T) {
	s, dir := suiteOf(t, `{"require": ["security/house.yaml"]}`, map[string]string{"security/house.yaml": houseRules})
	var said []string
	started := time.Date(2027, 3, 20, 8, 59, 30, 500, time.UTC)
	err := pack{}.FinishRun(context.Background(), s, core.Run{
		Started: started, Ended: started.Add(90 * time.Second), Say: func(l string) { said = append(said, l) },
		Scenarios: []core.Outcome{
			outcome(8, "A password under 12 characters is refused", "passed", "@house:password-length", "@smoke"),
			outcome(13, "An account locks after five wrong passwords", "failed", "@house:lockout"),
			outcome(18, "A session ends", "passed", "@house:sesion-end"),
			outcome(24, "A parcel is sorted", "passed", "@smoke"),
		},
	})
	if err != nil {
		t.Fatalf("a run fails because of a control: %v", err)
	}
	e, err := readEvidence(filepath.Join(dir, ".axx", "controls"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != KindRun || e.Version != EvidenceVersion || len(e.Scenarios) != 3 || e.Frameworks["house"] != "2027.1" ||
		!e.Started.Equal(started.Truncate(time.Second)) || e.Scenarios[1].Status != "failed" || e.Scenarios[2].Unknown[0] != "house:sesion-end" {
		t.Errorf("the run's evidence: %+v", e)
	}
	want := []string{
		"controls: 1 of 5 required are covered, 1 failing, 3 not covered",
		"  house:lockout      failing        features/sign-in/sign-in.feature:13 failed",
		"  house:session-end  not covered    no scenario is tagged @house:session-end",
		"  house:uploads      not covered    no scenario is tagged @house:uploads",
		"  house:audit        not covered    no scenario is tagged @house:audit",
		"  @house:sesion-end names no control of its framework: the nearest is @house:session-end (features/sign-in/sign-in.feature:18)",
	}
	if strings.Join(said, "\n") != strings.Join(want, "\n") {
		t.Errorf("what the run said:\n%s\nwant:\n%s", strings.Join(said, "\n"), strings.Join(want, "\n"))
	}
}

// A run of part of the suite says only what it proved: the controls it leaves
// not covered may be the ones its scenarios did not get to.
func TestARunOfPartOfTheSuiteSaysWhatItProved(t *testing.T) {
	for name, run := range map[string]core.Run{
		"it is shard 2/3":            {Shard: "2/3"},
		"it runs the paths it names": {Partial: "it runs the paths it names"},
	} {
		s, _ := suiteOf(t, `{"require": ["security/house.yaml"]}`, map[string]string{"security/house.yaml": houseRules})
		var said []string
		run.Say = func(l string) { said = append(said, l) }
		run.Scenarios = []core.Outcome{outcome(8, "A password under 12 characters is refused", "passed", "@house:password-length")}
		if err := (pack{}).FinishRun(context.Background(), s, run); err != nil {
			t.Fatal(err)
		}
		if len(said) != 2 || said[0] != "controls: this run proved 1 control: house:password-length" ||
			!strings.Contains(said[1], "it is not a run of every scenario ("+name+")") || !strings.Contains(said[1], "`axx controls check` judges the runs together") {
			t.Errorf("%s:\n%s", name, strings.Join(said, "\n"))
		}

		// A run of scenarios that claim no control is a run of something
		// else: it says nothing, and still writes what it is, for the check
		// that judges the runs together.
		s, dir := suiteOf(t, `{"require": ["security/house.yaml"]}`, map[string]string{"security/house.yaml": houseRules})
		said = nil
		run.Scenarios = []core.Outcome{outcome(24, "A parcel is sorted", "failed", "@smoke")}
		if err := (pack{}).FinishRun(context.Background(), s, run); err != nil {
			t.Fatal(err)
		}
		if len(said) != 0 {
			t.Errorf("%s, of scenarios that claim no control:\n%s", name, strings.Join(said, "\n"))
		}
		if _, err := os.Stat(filepath.Join(dir, ".axx", "controls", "run.json")); err != nil {
			t.Errorf("%s: its evidence: %v", name, err)
		}
	}
}

// A project that has the pack and uses none of it yet (no list required, no
// scenario that claims a control) gets no file and no word of it.
func TestAProjectThatUsesNoneOfItIsLeftAlone(t *testing.T) {
	s, dir := suiteOf(t, "", nil)
	said := 0
	err := pack{}.FinishRun(context.Background(), s, core.Run{
		Say:       func(string) { said++ },
		Scenarios: []core.Outcome{outcome(24, "A parcel is sorted", "passed", "@smoke", "@route:LEJ")},
	})
	if err != nil || said != 0 {
		t.Errorf("said %d lines, %v", said, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".axx")); !os.IsNotExist(err) {
		t.Errorf("it wrote something: %v", err)
	}
}

// A tag that names a control its framework has not is a finding of lint, with
// the nearest control: before a run, in the editor, in an agent's validation.
func TestAMistypedControlTagIsLintsFinding(t *testing.T) {
	dir, decode := newProject(t, `{"require": ["security/house.yaml"]}`, map[string]string{"security/house.yaml": houseRules})
	check := tagsCheck()
	found := check.Run(&core.FeatureCheckCall{ProjectDir: dir, Settings: decode, Scenarios: []core.ScenarioInfo{
		{URI: "features/sign-in/sign-in.feature", Line: 8, Tags: []string{"@house:password-length", "@smoke", "@route:LEJ"}},
		{URI: "features/sign-in/sign-in.feature", Line: 18, Tags: []string{"@house:sesion-end"}},
		{URI: "features/sign-in/sign-in.feature", Line: 30, Tags: []string{"@house:two-person-rule"}},
	}})
	if len(found) != 2 || check.ID != "control-tags" {
		t.Fatalf("the findings: %+v", found)
	}
	if f := found[0]; f.Line != 18 || f.Tag != "@house:sesion-end" || !strings.HasPrefix(f.Message, "@house:sesion-end names no control of House rules 2027.1: the nearest is @house:session-end.") {
		t.Errorf("the mistyped tag: %+v", f)
	}
	if f := found[1]; f.Line != 30 || strings.Contains(f.Message, "nearest") {
		t.Errorf("a tag near no control: %+v", f)
	}
}

// What an agent asks through the MCP server: the controls still to cover, each
// with a line of what it requires, and one control with who claims it.
func TestAnAgentAsksWhatIsLeftToCover(t *testing.T) {
	s, dir := suiteOf(t, `{"require": ["security/house.yaml"]}`, map[string]string{"security/house.yaml": houseRules})
	e := run(scenario(8, "A password under 12 characters is refused", "passed", "house:password-length"))
	if err := e.write(filepath.Join(dir, ".axx", "controls", runFile)); err != nil {
		t.Fatal(err)
	}
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{Name: "session"}, s, nil)
	byName := map[string]core.Tool{}
	for _, tool := range tools() {
		byName[tool.Name] = tool
	}
	res, err := byName["controls_status"].Run(&core.ToolCall{Context: context.Background(), Scenario: sc, Input: json.RawMessage(`{"words": "session"}`)})
	if err != nil {
		t.Fatal(err)
	}
	view := res.Data.(statusView)
	if !view.Judged || view.Summary != "1 of 5 required are covered, 4 not covered" || len(view.ToCover) != 1 ||
		view.ToCover[0].Control != "house:session-end" || view.ToCover[0].Requires != "A session ends after 30 minutes without use" {
		t.Errorf("what is left to cover: %+v", view)
	}
	all, err := byName["controls_status"].Run(&core.ToolCall{Context: context.Background(), Scenario: sc})
	if err != nil || len(all.Data.(statusView).ToCover) != 4 {
		t.Errorf("all that is left: %+v %v", all, err)
	}
	one, err := byName["controls_show"].Run(&core.ToolCall{Context: context.Background(), Scenario: sc, Input: json.RawMessage(`{"control": "house:password-length"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if c := one.Data.(controlView); c.State != Proven || c.Tag != "@house:password-length" || len(c.Scenarios) != 1 || !c.Required {
		t.Errorf("one control: %+v", c)
	}
	if _, err := byName["controls_show"].Run(&core.ToolCall{Context: context.Background(), Scenario: sc, Input: json.RawMessage(`{"control": "house:pasword-length"}`)}); err == nil || !strings.Contains(err.Error(), "house:password-length") {
		t.Errorf("a control the framework has not: %v", err)
	}
}

// What a mistyped ID most likely meant is a control of the same part of its
// framework, before one that only looks alike.
func TestTheNearestControl(t *testing.T) {
	f := &Framework{Key: "asvs", Controls: []Control{{ID: "1.2.9"}, {ID: "7.2.1"}, {ID: "7.2.4"}, {ID: "7.10.1"}, {ID: "12.1.1"}}}
	for id, want := range map[string]string{"7.2.99": "7.2.1", "7.2.44": "7.2.4", "7.10.9": "7.10.1", "1.2.8": "1.2.9", "40.1.1": "", "password": ""} {
		if got := f.nearest(id); got != want {
			t.Errorf("the nearest to %s is %q, want %q", id, got, want)
		}
	}
	named := &Framework{Key: "house", Controls: []Control{{ID: "session-end"}, {ID: "lockout"}}}
	if got := named.nearest("sesion-end"); got != "session-end" {
		t.Errorf("the nearest to sesion-end is %q", got)
	}
	if got := named.nearest("two-person-rule"); got != "" {
		t.Errorf("the nearest to two-person-rule is %q", got)
	}
}

// A pack adds a list of the controls of a framework another pack carries: a
// company's own selection, which its projects require by name.
func TestAPackAddsAListToAnotherPacksFramework(t *testing.T) {
	RegisterFramework(Framework{Key: "ports", Title: "Port rules", Version: "4", Controls: []Control{{ID: "P-1", Title: "Gates"}, {ID: "P-2", Title: "Cranes"}}})
	RegisterList("Ports", List{Name: "our-ports", Title: "The port rules we prove", Controls: []Required{{ID: "p-2", Scenarios: 2}}})
	dir, decode := newProject(t, `{"require": ["our-ports"]}`, nil)
	p, err := loadProject(dir, decode, overrides{})
	if err != nil {
		t.Fatal(err)
	}
	r := p.report(nil, nil, time.Now())
	if len(r.Controls) != 1 || r.Controls[0].Control != "ports:P-2" || r.Controls[0].Needs != 2 || r.Lists[0].Of != "Port rules 4" {
		t.Errorf("the report: %+v", r)
	}
	// The framework's own pack still has only its own lists.
	if f := registered()["ports"]; len(f.Lists) != 1 {
		t.Errorf("the lists of the framework: %+v", f.Lists)
	}
}
