package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/evals/internal/spec"
)

// cmdCheck verifies tasks locally, the way Harbor would but faster: it
// starts the task's infrastructure with docker compose, builds the starting
// project in a verifier container, optionally applies the reference solution,
// and runs the verifier. Use it while writing a task.
func cmdCheck(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	root := fs.String("root", "", "the evals directory")
	solution := fs.Bool("solution", false, "apply solution/solve.sh first (expect reward 1)")
	nop := fs.Bool("nop", false, "verify the untouched project (expect reward 0)")
	patch := fs.String("patch", "", "copy this directory over the project instead (an answer to replay; expect reward 0 unless --expect)")
	expectFlag := fs.Float64("expect", -1, "the reward to expect (default: 1 with --solution, else 0)")
	keep := fs.Bool("keep", false, "leave the containers running (docker compose -p evals-check-<task> ...)")
	images := fs.Bool("images", false, "build the images first")
	_ = fs.Parse(args)
	modes := 0
	for _, on := range []bool{*solution, *nop, *patch != ""} {
		if on {
			modes++
		}
	}
	if modes != 1 {
		return exitError{2, "pass exactly one of --solution, --nop or --patch DIR"}
	}
	if *patch != "" {
		p, err := filepath.Abs(*patch)
		if err != nil || !isDir(p) {
			return exitError{2, "--patch must be a directory"}
		}
		*patch = p
	}
	dir, err := evalsRoot(*root)
	if err != nil {
		return err
	}
	if *images {
		if err := buildImages(dir, false, ""); err != nil {
			return err
		}
	}
	if _, err := syncTasks(dir, false); err != nil {
		return err
	}
	ids := fs.Args()
	if len(ids) == 0 {
		return exitError{2, "name at least one task (or `all`)"}
	}
	tasks, err := spec.LoadTasks(filepath.Join(dir, "tasks"))
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	expect := 0.0
	if *solution {
		expect = 1
	}
	if *expectFlag >= 0 {
		expect = *expectFlag
	}
	var failed []string
	for _, t := range tasks {
		if !want["all"] && !want[t.ID()] {
			continue
		}
		delete(want, t.ID())
		reward, err := checkTask(dir, t, *solution, *patch, *keep)
		switch {
		case err != nil:
			fmt.Printf("== %s: ERROR %v\n", t.ID(), err)
			failed = append(failed, t.ID())
		case reward != expect:
			fmt.Printf("== %s: reward %.0f, expected %.0f\n", t.ID(), reward, expect)
			failed = append(failed, t.ID())
		default:
			fmt.Printf("== %s: reward %.0f as expected\n", t.ID(), reward)
		}
	}
	delete(want, "all")
	for id := range want {
		return fmt.Errorf("unknown task %s", id)
	}
	if len(failed) > 0 {
		return exitError{1, "unexpected result for: " + strings.Join(failed, ", ")}
	}
	return nil
}

func checkTask(evalsDir string, t *spec.Task, solution bool, patch string, keep bool) (float64, error) {
	work := filepath.Join(evalsDir, ".work", "check", t.ID())
	if err := os.RemoveAll(work); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return 0, err
	}
	base := filepath.Join(work, "main.yaml")
	if err := os.WriteFile(base, []byte("services:\n  main:\n    image: "+verifierImage+"\n    command: [\"sleep\", \"infinity\"]\n    working_dir: /app\n"), 0o644); err != nil {
		return 0, err
	}
	project := "evals-check-" + t.ID()
	compose := func(args ...string) *exec.Cmd {
		all := append([]string{"compose", "-p", project, "-f", base, "-f", filepath.Join(t.Dir, "tests", "docker-compose.yaml")}, args...)
		return exec.CommandContext(context.Background(), "docker", all...)
	}
	run := func(args ...string) error {
		out, err := compose(args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("docker compose %s: %w\n%s", strings.Join(args, " "), err, tail(string(out), 2000))
		}
		return nil
	}
	if !keep {
		defer func() { _ = compose("down", "-v", "--remove-orphans").Run() }()
	}
	fmt.Printf("== %s: starting the infrastructure\n", t.ID())
	if err := run("up", "-d", "--wait", "--quiet-pull"); err != nil {
		return 0, err
	}
	// The starting project, as the task's environment/Dockerfile builds it.
	if t.Meta.Workspace == "common" {
		if err := run("exec", "-T", "main", "project-setup", "common"); err != nil {
			return 0, err
		}
	}
	if overlay := filepath.Join(t.Dir, "environment", "workspace"); isDir(overlay) {
		if err := run("cp", overlay+"/.", "main:/app/"); err != nil {
			return 0, err
		}
	}
	if err := run("exec", "-T", "main", "project-setup", "finish"); err != nil {
		return 0, err
	}
	if patch != "" {
		fmt.Printf("== %s: applying %s\n", t.ID(), patch)
		if err := run("cp", patch+"/.", "main:/app/"); err != nil {
			return 0, err
		}
	}
	if solution {
		if err := run("cp", filepath.Join(t.Dir, "solution")+"/.", "main:/solution/"); err != nil {
			return 0, err
		}
		fmt.Printf("== %s: applying the reference solution\n", t.ID())
		cmd := compose("exec", "-T", "-w", "/app", "main", "bash", "/solution/solve.sh")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return 0, fmt.Errorf("solve.sh: %w", err)
		}
	}
	if err := run("cp", filepath.Join(t.Dir, "tests")+"/.", "main:/tests/"); err != nil {
		return 0, err
	}
	if err := run("exec", "-T", "main", "mkdir", "-p", "/logs/verifier"); err != nil {
		return 0, err
	}
	fmt.Printf("== %s: verifying\n", t.ID())
	cmd := compose("exec", "-T", "main", "bash", "/tests/test.sh")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("test.sh: %w", err)
	}
	logs := filepath.Join(work, "logs")
	_ = run("cp", "main:/logs/verifier/.", logs)
	b, err := os.ReadFile(filepath.Join(logs, "reward.json"))
	if err != nil {
		return 0, err
	}
	var rewards map[string]float64
	if err := json.Unmarshal(bytes.TrimSpace(b), &rewards); err != nil {
		return 0, err
	}
	fmt.Printf("   logs: %s\n", logs)
	return rewards["reward"], nil
}

func tail(s string, n int) string {
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}
