package controlscore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// EvidenceVersion is the version of the evidence's format.
const EvidenceVersion = 1

// The kinds of evidence.
const (
	// KindRun is what a run proved: its scenarios and the controls they claim.
	KindRun = "run"
	// KindMerged is what `axx controls check` judged: the runs it was given,
	// as one.
	KindMerged = "merged"
	// KindOther is what something other than a run says of controls: a pen
	// test, a scanner, a review.
	KindOther = "other"
)

// Evidence is what says that controls are covered, or are not: a run's
// scenarios with the controls they claim, or another source's findings. It is
// the file a run writes (run.json), and the file a person or a tool writes of
// what covers controls elsewhere. It names controls and scenarios, and has
// none of a framework's own text.
type Evidence struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	// Axx is the version of axx that ran, and Commit the commit of the
	// project it ran on (Dirty: with changes that are not committed).
	Axx    string `json:"axx,omitempty"`
	Commit string `json:"commit,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
	// Started and Ended are the run's times.
	Started time.Time `json:"started,omitzero"`
	Ended   time.Time `json:"ended,omitzero"`
	// Shard is the share of a run the run is ("2/3"), and Partial why it is
	// not a run of every scenario its configuration selects.
	Shard   string `json:"shard,omitempty"`
	Partial string `json:"partial,omitempty"`
	// Frameworks are the versions of the frameworks whose controls the
	// scenarios claim, by key.
	Frameworks map[string]string `json:"frameworks,omitempty"`
	// Scenarios are the scenarios that claim a control, and how each ended.
	Scenarios []Scenario `json:"scenarios,omitempty"`

	// Source names what found the Findings ("Pen test, March 2027"), Link
	// where its report is, and Date its day.
	Source   string    `json:"source,omitempty"`
	Link     string    `json:"link,omitempty"`
	Date     string    `json:"date,omitempty"`
	Findings []Finding `json:"controls,omitempty"`

	// file is where the evidence was read from.
	file string
}

// Scenario is a scenario that claims controls, in a run's evidence.
type Scenario struct {
	URI    string `json:"uri"`
	Line   int    `json:"line"`
	Name   string `json:"name"`
	Status string `json:"status"`
	// Controls are the controls it claims, as its tags name them without
	// their @, each written as its framework writes it: asvs:6.2.1.
	Controls []string `json:"controls,omitempty"`
	// Unknown are its tags of a framework the project has whose control the
	// framework has not: mistyped, most likely.
	Unknown []string `json:"unknown,omitempty"`
}

// Finding is what a source other than a run says of a control.
type Finding struct {
	// Control is the control, as a tag names it without its @.
	Control string `json:"control"`
	// State is "covered" or "failing".
	State string `json:"state"`
	// Note says more: the finding's number, what was looked at.
	Note string `json:"note,omitempty"`
}

func evidenceError(format string, args ...any) *axxerr.Error {
	return axxerr.New(CodeEvidence, exitcode.Usage, format, args...)
}

// runFile is the name of a run's evidence, in the pack's folder.
const runFile = "run.json"

// write writes evidence as a file, the same bytes for the same evidence.
func (e *Evidence) write(path string) error {
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// readEvidence reads a file of evidence, or a folder's run.json: a run's, what
// a person wrote of another source, or a file one of the registered sources
// reads.
func readEvidence(path string) (*Evidence, error) {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		path = filepath.Join(path, runFile)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, evidenceError("there is no evidence at %s", path).
				WithHint("a run of a project with the %s pack writes its evidence to %s; name the file or the folder of another", Name, filepath.Join(defaultFolder, runFile))
		}
		return nil, axxerr.Wrap(err, CodeEvidence, exitcode.Usage, "cannot read the evidence %s", path)
	}
	for _, s := range registeredSources() {
		if s.Read == nil {
			continue
		}
		e, ok, err := s.Read(path, b)
		if err != nil {
			return nil, axxerr.Wrap(err, CodeEvidence, exitcode.Usage, "cannot read %s as %s", path, s.Name)
		}
		if ok && e != nil {
			e.Kind, e.file = KindOther, path
			if e.Source == "" {
				e.Source = s.Name
			}
			return e, nil
		}
	}
	e, err := parseEvidence(b)
	if err != nil {
		return nil, axxerr.Wrap(err, CodeEvidence, exitcode.Usage, "%s is no evidence", path).
			WithHint("evidence is a run's run.json, or a file that names its `source` and, under `controls`, each control it covers or fails (`asvs:7.1.1: covered`)")
	}
	e.file = path
	return e, nil
}

// parseEvidence reads evidence written as JSON or YAML. What a person writes
// of another source may list its controls as a mapping, `asvs:7.1.1:
// covered`, or with a note, `asvs:8.2.2: {state: failing, note: ...}`.
func parseEvidence(b []byte) (*Evidence, error) {
	var doc yaml.MapSlice
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	var findings []Finding
	listed := false
	rest := yaml.MapSlice{}
	for _, item := range doc {
		if fmt.Sprint(item.Key) != "controls" {
			rest = append(rest, item)
			continue
		}
		m, ok := pairs(item.Value)
		if !ok {
			rest = append(rest, item) // a list of findings, as a tool writes it
			continue
		}
		listed = true
		for _, kv := range m {
			f := Finding{Control: kv.key}
			if state, ok := kv.value.(string); ok {
				f.State = state
			} else if keys, ok := pairs(kv.value); ok {
				for _, p := range keys {
					switch p.key {
					case "state":
						f.State = fmt.Sprint(p.value)
					case "note":
						f.Note = fmt.Sprint(p.value)
					default:
						return nil, fmt.Errorf("the control %s has the key %q: it has state and note", f.Control, p.key)
					}
				}
			} else {
				return nil, fmt.Errorf("the control %s is %v: it is covered or failing", f.Control, kv.value)
			}
			findings = append(findings, f)
		}
	}
	raw, err := yaml.MarshalWithOptions(rest, yaml.JSON())
	if err != nil {
		return nil, err
	}
	var e Evidence
	if len(rest) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&e); err != nil {
			return nil, err
		}
	}
	if listed {
		e.Findings = findings
	}
	switch {
	case e.Kind == KindRun || e.Kind == KindMerged || len(e.Scenarios) > 0:
		if e.Kind == "" {
			e.Kind = KindRun
		}
		if e.Version > EvidenceVersion {
			return nil, fmt.Errorf("it is evidence of version %d, which this axx is too early to read", e.Version)
		}
	case len(e.Findings) > 0 || e.Source != "":
		e.Kind = KindOther
		if e.Source == "" {
			return nil, fmt.Errorf("it does not say what found it: name its `source`")
		}
		for i, f := range e.Findings {
			state := strings.ToLower(strings.TrimSpace(f.State))
			if state != string(Covered) && state != string(Failing) {
				return nil, fmt.Errorf("it says the control %s is %q: a source says covered or failing", f.Control, f.State)
			}
			e.Findings[i].State = state
		}
	default:
		return nil, fmt.Errorf("it has no scenarios and names no source")
	}
	return &e, nil
}

// commitOf is the commit a project is at, and whether it has changes that are
// not committed: "" for a folder that is no Git repository, or without git.
func commitOf(ctx context.Context, dir string) (commit string, dirty bool) {
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	commit, err := git("rev-parse", "HEAD")
	if err != nil {
		return "", false
	}
	status, err := git("status", "--porcelain", "--untracked-files=no")
	return commit, err == nil && status != ""
}

// merge makes one evidence of several runs': the scenarios of each, a
// scenario two of them ran taken at its worse. Runs of different commits, or
// by different versions of axx, or of different versions of a framework, are
// not judged together.
func merge(runs []*Evidence) (*Evidence, error) {
	out := &Evidence{Version: EvidenceVersion, Kind: KindMerged, Frameworks: map[string]string{}}
	if len(runs) == 0 {
		return out, nil
	}
	first := runs[0]
	out.Axx, out.Commit = first.Axx, first.Commit
	at := map[string]int{}
	var partial []string
	for _, r := range runs {
		switch {
		case r.Commit != first.Commit:
			return nil, axxerr.New(CodeMixed, exitcode.Usage, "the evidence is of different commits: %s is of %s, %s of %s", first.file, short(first.Commit), r.file, short(r.Commit)).
				WithHint("give `axx controls check` the evidence of the runs of one commit: a control proven on another is not proven on this one")
		case r.Axx != first.Axx:
			return nil, axxerr.New(CodeMixed, exitcode.Usage, "the evidence is of different versions of axx: %s is of %s, %s of %s", first.file, first.Axx, r.file, r.Axx).
				WithHint("run the scenarios again with one version of axx")
		}
		for key, version := range r.Frameworks {
			if before, ok := out.Frameworks[key]; ok && before != version {
				return nil, axxerr.New(CodeMixed, exitcode.Usage, "the evidence is of different versions of %s: %s and %s", key, before, version).
					WithHint("a control's ID can mean another control in another version of its framework: run the scenarios again with the lists the project requires now")
			}
			out.Frameworks[key] = version
		}
		out.Dirty = out.Dirty || r.Dirty
		if out.Started.IsZero() || (!r.Started.IsZero() && r.Started.Before(out.Started)) {
			out.Started = r.Started
		}
		if r.Ended.After(out.Ended) {
			out.Ended = r.Ended
		}
		if r.Partial != "" && r.Shard == "" {
			partial = append(partial, r.Partial)
		}
		for _, sc := range r.Scenarios {
			key := fmt.Sprintf("%s:%d", sc.URI, sc.Line)
			if i, seen := at[key]; seen {
				if !passed(sc.Status) && passed(out.Scenarios[i].Status) {
					out.Scenarios[i] = sc
				}
				continue
			}
			at[key] = len(out.Scenarios)
			out.Scenarios = append(out.Scenarios, sc)
		}
	}
	if len(partial) > 0 {
		out.Partial = partial[0]
	}
	sort.SliceStable(out.Scenarios, func(i, j int) bool {
		a, b := out.Scenarios[i], out.Scenarios[j]
		if a.URI != b.URI {
			return a.URI < b.URI
		}
		return a.Line < b.Line
	})
	if len(out.Frameworks) == 0 {
		out.Frameworks = nil
	}
	return out, nil
}

func short(commit string) string {
	switch {
	case commit == "":
		return "no commit"
	case len(commit) > 12:
		return commit[:12]
	}
	return commit
}

// passed reports whether a scenario's status is that it passed.
func passed(status string) bool { return status == "passed" }
