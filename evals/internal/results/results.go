// Package results aggregates Harbor job results into the evals result file
// (evals/results/<date>-<agent>.json), renders it as Markdown, and compares
// a result file with a baseline for the release gate.
package results

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SchemaVersion of the result file.
const SchemaVersion = 1

// Kind marks result files.
const Kind = "axx-evals-results"

// File is a result file (and a baseline: a result file promoted as-is).
type File struct {
	SchemaVersion int      `json:"schemaVersion"`
	Kind          string   `json:"kind"`
	Date          string   `json:"date"`
	Agent         string   `json:"agent"`
	Model         string   `json:"model,omitempty"`
	Axx           string   `json:"axx,omitempty"`
	Harbor        string   `json:"harbor,omitempty"`
	Conditions    []string `json:"conditions"`
	Tasks         []Task   `json:"tasks"`
	// Skipped are tasks that did not run, with the reason (e.g. a missing pack).
	Skipped []Skipped `json:"skipped,omitempty"`
	// Scores maps a condition to its score: the mean reward over the tasks
	// that ran, in percent (0-100).
	Scores map[string]float64 `json:"scores"`
}

// Task is one task's results per condition.
type Task struct {
	ID       string             `json:"id"`
	Title    string             `json:"title,omitempty"`
	Category string             `json:"category,omitempty"`
	Results  map[string]*Result `json:"results"`
}

// Result is a task's outcome under one condition.
type Result struct {
	// Reward is the mean reward over the trials (0-1).
	Reward float64 `json:"reward"`
	Trials int     `json:"trials"`
	Passed int     `json:"passed"`
	// Errors are trials that ended in an exception (agent or environment).
	Errors int `json:"errors,omitempty"`
	// MutantsCaught / MutantsTotal from the verifier, summed over trials.
	MutantsCaught int `json:"mutantsCaught,omitempty"`
	MutantsTotal  int `json:"mutantsTotal,omitempty"`
	// CostUSD and tokens, summed over trials when the agent reports them.
	CostUSD      float64 `json:"costUsd,omitempty"`
	InputTokens  int64   `json:"inputTokens,omitempty"`
	OutputTokens int64   `json:"outputTokens,omitempty"`
}

// Skipped is a task that did not run.
type Skipped struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// Read loads a result file.
func Read(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if f.Kind != Kind {
		return nil, fmt.Errorf("%s: not an evals result file (kind %q)", path, f.Kind)
	}
	if f.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("%s: schemaVersion %d, want %d", path, f.SchemaVersion, SchemaVersion)
	}
	return &f, nil
}

// Write saves the result file as indented JSON.
func (f *File) Write(path string) error {
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Score computes the scores over the given task IDs (all tasks when nil).
func (f *File) Score(only map[string]bool) map[string]float64 {
	out := map[string]float64{}
	for _, c := range f.Conditions {
		sum, n := 0.0, 0
		for _, t := range f.Tasks {
			if only != nil && !only[t.ID] {
				continue
			}
			if r := t.Results[c]; r != nil && r.Trials > 0 {
				sum += r.Reward
				n++
			}
		}
		if n > 0 {
			out[c] = math.Round(sum/float64(n)*1000) / 10
		}
	}
	return out
}

// ---- Harbor job results ----

type trialResult struct {
	TaskName string `json:"task_name"`
	TaskID   struct {
		Path string `json:"path"`
	} `json:"task_id"`
	AgentInfo struct {
		Name      string `json:"name"`
		ModelInfo *struct {
			Name     string `json:"name"`
			Provider string `json:"provider"`
		} `json:"model_info"`
	} `json:"agent_info"`
	AgentResult *struct {
		InputTokens  *int64   `json:"n_input_tokens"`
		OutputTokens *int64   `json:"n_output_tokens"`
		CostUSD      *float64 `json:"cost_usd"`
	} `json:"agent_result"`
	VerifierResult *struct {
		Rewards map[string]float64 `json:"rewards"`
	} `json:"verifier_result"`
	ExceptionInfo json.RawMessage `json:"exception_info"`
}

// FromHarborJob reads every trial of a Harbor job directory and returns the
// per-task results for one condition.
func FromHarborJob(dir string) (map[string]*Result, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]*Result{}
	sums := map[string]float64{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "result.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var tr trialResult
		if err := json.Unmarshal(b, &tr); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		id := filepath.Base(tr.TaskID.Path)
		if id == "" || id == "." {
			id = tr.TaskName
		}
		r := out[id]
		if r == nil {
			r = &Result{}
			out[id] = r
		}
		r.Trials++
		reward := 0.0
		if tr.VerifierResult != nil {
			reward = tr.VerifierResult.Rewards["reward"]
		}
		caught, total := mutantCounts(filepath.Join(dir, e.Name(), "verifier", "verify.json"))
		r.MutantsCaught += caught
		r.MutantsTotal += total
		if len(tr.ExceptionInfo) > 0 && string(tr.ExceptionInfo) != "null" {
			r.Errors++
		}
		if reward >= 1 {
			r.Passed++
		}
		sums[id] += reward
		if a := tr.AgentResult; a != nil {
			if a.CostUSD != nil {
				r.CostUSD += *a.CostUSD
			}
			if a.InputTokens != nil {
				r.InputTokens += *a.InputTokens
			}
			if a.OutputTokens != nil {
				r.OutputTokens += *a.OutputTokens
			}
		}
	}
	for id, r := range out {
		r.Reward = sums[id] / float64(r.Trials)
	}
	return out, nil
}

// mutantCounts reads the caught and total mutants from a verifier report
// (verify.json); a missing report counts nothing.
func mutantCounts(path string) (caught, total int) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	var rep struct {
		Mutants map[string]struct {
			Killed bool `json:"killed"`
		} `json:"mutants"`
		Total int `json:"mutantsTotal"`
	}
	if json.Unmarshal(b, &rep) != nil {
		return 0, 0
	}
	for _, m := range rep.Mutants {
		if m.Killed {
			caught++
		}
	}
	total = rep.Total
	if total < len(rep.Mutants) {
		total = len(rep.Mutants)
	}
	return caught, total
}

// ---- Markdown ----

// Markdown renders the task x condition table.
func (f *File) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# axx evals: %s", f.Agent)
	if f.Model != "" {
		fmt.Fprintf(&b, " (%s)", f.Model)
	}
	fmt.Fprintf(&b, ", %s\n\n", f.Date)
	if f.Axx != "" || f.Harbor != "" {
		fmt.Fprintf(&b, "axx %s, Harbor %s. A cell is the share of trials whose verifier gave reward 1; `err` counts trials that ended in an exception.\n\n", orDash(f.Axx), orDash(f.Harbor))
	}
	b.WriteString("| Task | Category |")
	for _, c := range f.Conditions {
		fmt.Fprintf(&b, " %s |", c)
	}
	b.WriteString("\n| --- | --- |")
	for range f.Conditions {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	tasks := append([]Task(nil), f.Tasks...)
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	for _, t := range tasks {
		fmt.Fprintf(&b, "| %s | %s |", t.ID, orDash(t.Category))
		for _, c := range f.Conditions {
			fmt.Fprintf(&b, " %s |", cell(t.Results[c]))
		}
		b.WriteString("\n")
	}
	b.WriteString("| **Score** | |")
	for _, c := range f.Conditions {
		if s, ok := f.Scores[c]; ok {
			fmt.Fprintf(&b, " **%.1f** |", s)
		} else {
			b.WriteString(" - |")
		}
	}
	b.WriteString("\n")
	if len(f.Skipped) > 0 {
		b.WriteString("\nNot run:\n\n")
		for _, s := range f.Skipped {
			fmt.Fprintf(&b, "- `%s`: %s\n", s.ID, s.Reason)
		}
	}
	return b.String()
}

func cell(r *Result) string {
	if r == nil || r.Trials == 0 {
		return "-"
	}
	var s string
	if r.Trials == 1 {
		s = map[bool]string{true: "pass", false: "fail"}[r.Passed == 1]
	} else {
		s = fmt.Sprintf("%d/%d", r.Passed, r.Trials)
	}
	if r.Errors > 0 {
		s += fmt.Sprintf(" (%d err)", r.Errors)
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ---- comparison ----

// Delta is one condition's change between a baseline and a result.
type Delta struct {
	Condition string  `json:"condition"`
	Baseline  float64 `json:"baseline"`
	Current   float64 `json:"current"`
	Change    float64 `json:"change"`
	Tasks     int     `json:"tasks"`
	Regressed bool    `json:"regressed"`
}

// Comparison is the result of Compare.
type Comparison struct {
	MaxDrop float64 `json:"maxDrop"`
	Deltas  []Delta `json:"deltas"`
	// TaskChanges lists tasks that went from pass to fail or back.
	TaskChanges []string `json:"taskChanges,omitempty"`
	Regressed   bool     `json:"regressed"`
}

// Compare scores both files over the tasks they have in common and flags
// every condition whose score dropped by more than maxDrop points.
func Compare(baseline, current *File, maxDrop float64) (*Comparison, error) {
	if baseline.Agent != current.Agent {
		return nil, fmt.Errorf("the baseline is for agent %q, the results for %q", baseline.Agent, current.Agent)
	}
	common := map[string]bool{}
	bt := map[string]Task{}
	for _, t := range baseline.Tasks {
		bt[t.ID] = t
	}
	for _, t := range current.Tasks {
		if _, ok := bt[t.ID]; ok {
			common[t.ID] = true
		}
	}
	if len(common) == 0 {
		return nil, errors.New("the baseline and the results have no task in common")
	}
	bs, cs := baseline.Score(common), current.Score(common)
	c := &Comparison{MaxDrop: maxDrop}
	for _, cond := range current.Conditions {
		b, okb := bs[cond]
		cur, okc := cs[cond]
		if !okb || !okc {
			continue
		}
		d := Delta{Condition: cond, Baseline: b, Current: cur, Change: math.Round((cur-b)*10) / 10, Tasks: len(common)}
		d.Regressed = b-cur > maxDrop
		c.Regressed = c.Regressed || d.Regressed
		c.Deltas = append(c.Deltas, d)
	}
	for _, t := range current.Tasks {
		if !common[t.ID] {
			continue
		}
		for _, cond := range current.Conditions {
			br, cr := bt[t.ID].Results[cond], t.Results[cond]
			if br == nil || cr == nil {
				continue
			}
			switch {
			case br.Reward >= 1 && cr.Reward < 1:
				c.TaskChanges = append(c.TaskChanges, fmt.Sprintf("%s/%s: pass -> fail", t.ID, cond))
			case br.Reward < 1 && cr.Reward >= 1:
				c.TaskChanges = append(c.TaskChanges, fmt.Sprintf("%s/%s: fail -> pass", t.ID, cond))
			}
		}
	}
	sort.Strings(c.TaskChanges)
	return c, nil
}

// String renders a comparison for a terminal or a PR comment.
func (c *Comparison) String() string {
	var b strings.Builder
	b.WriteString("| Condition | Baseline | Current | Change | |\n| --- | --- | --- | --- | --- |\n")
	for _, d := range c.Deltas {
		mark := "ok"
		if d.Regressed {
			mark = fmt.Sprintf("REGRESSION (more than %.0f points)", c.MaxDrop)
		}
		fmt.Fprintf(&b, "| %s | %.1f | %.1f | %+.1f | %s |\n", d.Condition, d.Baseline, d.Current, d.Change, mark)
	}
	if len(c.TaskChanges) > 0 {
		b.WriteString("\nChanged tasks:\n\n")
		for _, t := range c.TaskChanges {
			fmt.Fprintf(&b, "- %s\n", t)
		}
	}
	return b.String()
}
