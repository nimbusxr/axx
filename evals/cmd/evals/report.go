package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nimbusxr/axx/evals/internal/results"
	"github.com/nimbusxr/axx/evals/internal/spec"
)

// cmdReport aggregates finished Harbor jobs (one per condition) into a
// result file, e.g. after `harbor run` was started by hand or in CI.
func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	root := fs.String("root", "", "the evals directory")
	agent := fs.String("agent", "", "the agent that ran")
	model := fs.String("model", "", "its model")
	axxVersion := fs.String("axx-version", "", "the axx version under test")
	date := fs.String("date", time.Now().UTC().Format("2006-01-02"), "the run's date")
	out := fs.String("out", "", "result file without extension (default results/<date>-<agent>)")
	var jobs multiFlag
	fs.Var(&jobs, "job", "CONDITION=HARBOR_JOB_DIR (repeatable, in column order)")
	_ = fs.Parse(args)
	if *agent == "" || len(jobs) == 0 {
		return exitError{2, "--agent and at least one --job CONDITION=DIR are required"}
	}
	dir, err := evalsRoot(*root)
	if err != nil {
		return err
	}
	var names []string
	dirs := map[string]string{}
	for _, j := range jobs {
		c, d, ok := strings.Cut(j, "=")
		if !ok {
			return fmt.Errorf("--job %q: want CONDITION=DIR", j)
		}
		names = append(names, c)
		dirs[c] = d
	}
	var skipped []results.Skipped
	for _, d := range dirs {
		skipped = append(skipped, datasetSkipped(d)...)
		break
	}
	f, err := buildResults(dir, *agent, *model, *axxVersion, *date, names, dirs, skipped)
	if err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(dir, "results", *date+"-"+sanitize(*agent))
	}
	return writeResults(f, *out)
}

// datasetSkipped reads the skipped tasks recorded next to a job's dataset,
// when the job config points at one prepared by `evals prepare`.
func datasetSkipped(jobDir string) []results.Skipped {
	b, err := os.ReadFile(filepath.Join(jobDir, "config.json"))
	if err != nil {
		return nil
	}
	var cfg struct {
		Datasets []struct {
			Path string `json:"path"`
		} `json:"datasets"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return nil
	}
	for _, d := range cfg.Datasets {
		b, err := os.ReadFile(filepath.Join(d.Path, "evals-dataset.json"))
		if err != nil {
			continue
		}
		var ds struct {
			Skipped []results.Skipped `json:"skipped"`
		}
		if json.Unmarshal(b, &ds) == nil {
			return ds.Skipped
		}
	}
	return nil
}

func buildResults(evalsDir, agent, model, axxVersion, date string, conditions []string, jobs map[string]string, skipped []results.Skipped) (*results.File, error) {
	tasks, err := spec.LoadTasks(filepath.Join(evalsDir, "tasks"))
	if err != nil {
		return nil, err
	}
	meta := map[string]*spec.Task{}
	for _, t := range tasks {
		meta[t.ID()] = t
	}
	f := &results.File{
		SchemaVersion: results.SchemaVersion, Kind: results.Kind, Date: date, Agent: agent, Model: model,
		Axx: axxVersion, Harbor: harborVersion(), Conditions: conditions, Skipped: skipped,
	}
	byTask := map[string]*results.Task{}
	for _, c := range conditions {
		res, err := results.FromHarborJob(jobs[c])
		if err != nil {
			return nil, fmt.Errorf("condition %s: %w", c, err)
		}
		for id, r := range res {
			t := byTask[id]
			if t == nil {
				t = &results.Task{ID: id, Results: map[string]*results.Result{}}
				if m := meta[id]; m != nil {
					t.Title, t.Category = m.Meta.Title, m.Meta.Category
				}
				byTask[id] = t
			}
			t.Results[c] = r
		}
	}
	ids := make([]string, 0, len(byTask))
	for id := range byTask {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f.Tasks = append(f.Tasks, *byTask[id])
	}
	f.Scores = f.Score(nil)
	return f, nil
}

func cmdCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	maxDrop := fs.Float64("max-drop", 10, "largest allowed score drop per condition, in points")
	asJSON := fs.Bool("json", false, "print the comparison as JSON")
	// Accept flags before or after the two files.
	var files []string
	rest := args
	for len(rest) > 0 {
		_ = fs.Parse(rest)
		if fs.NArg() == 0 {
			break
		}
		files = append(files, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(files) != 2 {
		return exitError{2, "usage: evals compare BASELINE.json RESULTS.json [--max-drop 10]"}
	}
	base, err := results.Read(files[0])
	if err != nil {
		return err
	}
	cur, err := results.Read(files[1])
	if err != nil {
		return err
	}
	c, err := results.Compare(base, cur, *maxDrop)
	if err != nil {
		return err
	}
	if *asJSON {
		b, _ := json.MarshalIndent(c, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Print(c.String())
	}
	if c.Regressed {
		return exitError{1, fmt.Sprintf("regression: a condition's score dropped by more than %.0f points", *maxDrop)}
	}
	return nil
}
