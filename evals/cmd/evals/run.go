package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/evals/internal/results"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// cmdRun runs every task under each condition for one agent: it builds the
// images, prepares one Harbor dataset per condition, runs `harbor run` for
// each, and writes the result file and table.
func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	root := fs.String("root", "", "the evals directory")
	agent := fs.String("agent", "", "Harbor agent: claude-code, codex, gemini-cli, ... (oracle and nop check the plumbing)")
	model := fs.String("model", "", "model for the agent, e.g. anthropic/claude-sonnet-4-5")
	condList := fs.String("conditions", "none,skills,mcp,both", "conditions from conditions.toml")
	only := fs.String("tasks", "", "comma-separated task ids (default: all)")
	attempts := fs.Int("attempts", 1, "trials per task and condition (harbor -k)")
	concurrency := fs.Int("concurrency", 2, "concurrent trials (harbor -n); each trial runs its own databases")
	pending := fs.Bool("include-pending", false, "also run tasks whose required packs are missing")
	skipImages := fs.Bool("skip-images", false, "do not rebuild the images")
	harbor := fs.String("harbor", "harbor", "the harbor command")
	jobsDir := fs.String("jobs-dir", "", "where Harbor writes jobs (default .work/jobs)")
	out := fs.String("out", "", "result file without extension (default results/<date>-<agent>)")
	dry := fs.Bool("dry-run", false, "prepare the datasets and print the harbor commands without running them")
	envFile := fs.String("env-file", "", "a .env file with the agent's API keys (harbor --env-file)")
	var agentKwargs multiFlag
	fs.Var(&agentKwargs, "ak", "agent key=value option passed to harbor (repeatable)")
	_ = fs.Parse(args)
	if *agent == "" {
		return exitError{2, "--agent is required"}
	}
	dir, err := evalsRoot(*root)
	if err != nil {
		return err
	}
	conds, err := loadConditions(dir)
	if err != nil {
		return err
	}
	names := splitList(*condList)
	for _, n := range names {
		if _, ok := conds[n]; !ok {
			return fmt.Errorf("unknown condition %q (see conditions.toml)", n)
		}
	}
	if !*skipImages && !*dry {
		if err := buildImages(dir, false, ""); err != nil {
			return err
		}
	}
	packs, axxVersion := map[string]bool{}, ""
	if !*pending {
		if packs, axxVersion, err = imagePacks(); err != nil {
			return err
		}
	}
	date := time.Now().UTC().Format("2006-01-02")
	runID := fmt.Sprintf("%s-%s-%s", date, sanitize(*agent), time.Now().UTC().Format("150405"))
	if *jobsDir == "" {
		*jobsDir = filepath.Join(dir, ".work", "jobs")
	}
	jobs := map[string]string{}
	var skipped []results.Skipped
	for _, n := range names {
		c := conds[n]
		dataset := filepath.Join(dir, ".work", "datasets", runID, n)
		_, sk, err := prepareDataset(dir, c, dataset, splitList(*only), packs, *pending)
		if err != nil {
			return err
		}
		skipped = sk
		jobName := runID + "-" + n
		hargs := []string{
			"run", "-p", dataset, "-a", *agent, "-o", *jobsDir, "--job-name", jobName,
			"-n", strconv.Itoa(*concurrency), "-k", strconv.Itoa(*attempts), "-y",
		}
		if *model != "" {
			hargs = append(hargs, "-m", *model)
		}
		if c.MCP {
			hargs = append(hargs, "--mcp-config", filepath.Join(dir, "conditions", "mcp.json"))
		}
		if *envFile != "" {
			hargs = append(hargs, "--env-file", *envFile)
		}
		for _, kv := range agentKwargs {
			hargs = append(hargs, "--ak", kv)
		}
		fmt.Printf("== condition %s: %s %s\n", n, *harbor, strings.Join(hargs, " "))
		jobs[n] = filepath.Join(*jobsDir, jobName)
		if *dry {
			continue
		}
		if err := runStreaming(*harbor, hargs...); err != nil {
			return fmt.Errorf("harbor run for condition %s: %w", n, err)
		}
	}
	if *dry {
		return nil
	}
	if *out == "" {
		*out = filepath.Join(dir, "results", date+"-"+sanitize(*agent))
	}
	f, err := buildResults(dir, *agent, *model, axxVersion, date, names, jobs, skipped)
	if err != nil {
		return err
	}
	return writeResults(f, *out)
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			return r
		}
		return '-'
	}, s)
}

func harborVersion() string {
	out, err := exec.CommandContext(context.Background(), "harbor", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func writeResults(f *results.File, out string) error {
	if err := f.Write(out + ".json"); err != nil {
		return err
	}
	if err := os.WriteFile(out+".md", []byte(f.Markdown()), 0o644); err != nil {
		return err
	}
	fmt.Print(f.Markdown())
	fmt.Printf("\nwrote %s.json and %s.md\n", out, out)
	return nil
}
