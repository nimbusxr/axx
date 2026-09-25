// Package spec reads the two files that describe an evals task beyond
// Harbor's own fields: the [metadata] of task.toml (used by the runner) and
// tests/verify.toml (used by the verifier inside the container).
package spec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/pelletier/go-toml/v2"

	"github.com/nimbusxr/axx/evals/internal/mutant"
)

// Packs every task environment has; tasks list only the others in requires.
var basePacks = map[string]bool{"core": true, "mock": true, "sql": true, "mongo": true}

// Services the sidecar compose file can provide.
var knownServices = map[string]bool{"postgres": true, "mongo": true, "address-service": true, "kafka": true}

// Task is the evals part of a task.toml.
type Task struct {
	Name        string
	Description string
	Dir         string
	Meta        Meta
}

// Meta is task.toml [metadata].
type Meta struct {
	// Title is a one-line summary for reports.
	Title string `toml:"title"`
	// Difficulty: easy, medium or hard.
	Difficulty string `toml:"difficulty"`
	// Category groups tasks in reports (rest, openapi, mock, sql, mongo, kafka,
	// debugging, setup, isolation).
	Category string `toml:"category"`
	// Requires lists axx packs beyond core, mock, sql and mongo that the
	// task needs (rest, kafka). The runner skips a task until the axx under
	// test has every pack listed.
	Requires []string `toml:"requires"`
	// Services are the sidecars the environment starts.
	Services []string `toml:"services"`
	// Workspace is the initial project: "common" (the shared parcels
	// acceptance project, plus the task's environment/workspace overlay) or
	// "none" (only the overlay).
	Workspace string `toml:"workspace"`
}

type taskFile struct {
	Task struct {
		Name        string `toml:"name"`
		Description string `toml:"description"`
	} `toml:"task"`
	Metadata Meta `toml:"metadata"`
}

// LoadTask reads dir/task.toml.
func LoadTask(dir string) (*Task, error) {
	b, err := os.ReadFile(filepath.Join(dir, "task.toml"))
	if err != nil {
		return nil, err
	}
	var tf taskFile
	if err := toml.Unmarshal(b, &tf); err != nil {
		return nil, fmt.Errorf("%s/task.toml: %w", dir, err)
	}
	t := &Task{Name: tf.Task.Name, Description: tf.Task.Description, Dir: dir, Meta: tf.Metadata}
	if t.Meta.Workspace == "" {
		t.Meta.Workspace = "common"
	}
	if len(t.Meta.Services) == 0 {
		t.Meta.Services = []string{"postgres", "mongo", "address-service"}
	}
	var errs []error
	if t.Name == "" {
		errs = append(errs, errors.New("[task] name is required"))
	}
	for _, p := range t.Meta.Requires {
		if basePacks[p] {
			errs = append(errs, fmt.Errorf("requires: %s is always available; list only rest, kafka or other new packs", p))
		}
	}
	for _, s := range t.Meta.Services {
		if !knownServices[s] {
			errs = append(errs, fmt.Errorf("services: unknown service %q", s))
		}
	}
	if t.Meta.Workspace != "common" && t.Meta.Workspace != "none" {
		errs = append(errs, fmt.Errorf("workspace must be common or none, not %q", t.Meta.Workspace))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("%s/task.toml: %w", dir, err)
	}
	return t, nil
}

// ID is the task's directory name.
func (t *Task) ID() string { return filepath.Base(t.Dir) }

// HasService reports whether the task environment starts the service.
func (t *Task) HasService(s string) bool {
	for _, x := range t.Meta.Services {
		if x == s {
			return true
		}
	}
	return false
}

// Missing returns the required packs that are not in available.
func (t *Task) Missing(available map[string]bool) []string {
	var out []string
	for _, p := range t.Meta.Requires {
		if !available[p] {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// LoadTasks reads every task under root (directories with a task.toml).
func LoadTasks(root string) ([]*Task, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []*Task
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "task.toml")); err != nil {
			continue
		}
		t, err := LoadTask(dir)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// Verify is tests/verify.toml: what the verifier checks for one task.
// README.md ("Anti-cheat rules") explains each field.
type Verify struct {
	// Allowed are doublestar globs (relative to the project root) of the
	// files the agent may add, change or delete.
	Allowed []string `toml:"allowed"`
	// Required globs must each match at least one file after the agent ran.
	Required []string `toml:"required"`
	// Absent globs must match no file (e.g. legacy config after a migration).
	Absent []string `toml:"absent"`
	// MustNotContain: files that must not contain a regular expression.
	MustNotContain []ContentRule `toml:"must_not_contain"`
	// ConfigKeys are the top-level axx.yaml keys the agent may change
	// (compared with the initial axx.yaml). Empty: axx.yaml must not change
	// unless Allowed covers it; ["*"]: any key.
	ConfigKeys []string `toml:"config_keys"`
	// MinScenarios is the number of scenarios that must pass on the correct app.
	MinScenarios int `toml:"min_scenarios"`
	// PreserveScenarios are scenario names that must still exist and pass.
	PreserveScenarios []string `toml:"preserve_scenarios"`
	// RespectTags keeps run.tags from axx.yaml; by default every scenario runs.
	RespectTags bool `toml:"respect_tags"`
	// StartApps makes the first run on the correct app a plain `axx run`: axx
	// starts (and cleans up) the service from the agent's axx.yaml. Other runs,
	// and every mutant run, use the app the verifier starts.
	StartApps bool `toml:"start_apps"`
	// Mutants are the deliberate bugs the features must catch.
	Mutants []string `toml:"mutants"`
	// Lint is the test-data isolation check.
	Lint Lint `toml:"lint"`
	// Run tunes the axx runs.
	Run Run `toml:"run"`
}

// ContentRule forbids a pattern in the files matching Path.
type ContentRule struct {
	Path    string `toml:"path"`
	Pattern string `toml:"pattern"`
	Reason  string `toml:"reason"`
}

// Run tunes the axx runs the verifier makes.
type Run struct {
	// Workers for axx run (default 8).
	Workers int `toml:"workers"`
	// Timeout per axx run (default 5m).
	Timeout Duration `toml:"timeout"`
	// Repeat runs the suite on the correct app this many times (default 1),
	// each on freshly reset data and in a new random order; every run must
	// pass. Scenarios that share data fail some of these runs.
	Repeat int `toml:"repeat"`
}

// Lint is the test-data isolation check.
type Lint struct {
	// Required: `axx lint` must pass.
	Required bool `toml:"required"`
	// Bites is a glob of data files the agent wrote (e.g. "seeds/**"). The
	// verifier copies one of them next to itself and expects `axx lint` to
	// fail: the agent's rules must catch a reused value.
	Bites string `toml:"bites"`
}

// Duration is a TOML string such as "5m".
type Duration struct{ time.Duration }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// LoadVerify reads and checks a verify.toml.
func LoadVerify(path string) (*Verify, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v Verify
	dec := toml.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if v.Run.Workers <= 0 {
		v.Run.Workers = 8
	}
	if v.Run.Timeout.Duration <= 0 {
		v.Run.Timeout.Duration = 5 * time.Minute
	}
	if v.Run.Repeat <= 0 {
		v.Run.Repeat = 1
	}
	if err := v.check(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &v, nil
}

func (v *Verify) check() error {
	var errs []error
	if len(v.Allowed) == 0 {
		errs = append(errs, errors.New("allowed must list at least one glob"))
	}
	for _, g := range append(append(append([]string{}, v.Allowed...), v.Required...), v.Absent...) {
		if !doublestar.ValidatePattern(g) {
			errs = append(errs, fmt.Errorf("invalid glob %q", g))
		}
	}
	if v.MinScenarios < 1 {
		errs = append(errs, errors.New("min_scenarios must be at least 1"))
	}
	if len(v.Mutants) == 0 {
		errs = append(errs, errors.New("mutants must list at least one mutant"))
	}
	seen := map[string]bool{}
	for _, m := range v.Mutants {
		if !mutant.Known(m) {
			errs = append(errs, fmt.Errorf("unknown mutant %q", m))
		}
		seen[m] = true
	}
	for _, r := range v.MustNotContain {
		if r.Path == "" || r.Pattern == "" {
			errs = append(errs, errors.New("must_not_contain needs path and pattern"))
		}
	}
	if v.Lint.Bites != "" && !doublestar.ValidatePattern(v.Lint.Bites) {
		errs = append(errs, fmt.Errorf("lint.bites: invalid glob %q", v.Lint.Bites))
	}
	return errors.Join(errs...)
}

// AllowsPath reports whether the agent may touch rel.
func (v *Verify) AllowsPath(rel string) bool {
	for _, g := range v.Allowed {
		if ok, _ := doublestar.Match(g, rel); ok {
			return true
		}
	}
	return false
}
