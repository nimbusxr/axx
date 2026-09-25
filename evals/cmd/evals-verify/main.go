// Command evals-verify is the verifier of axx's agent evals. Harbor runs it
// (through tests/test.sh) in a fresh verifier container: the agent's project
// is copied to /app, the task's checks are in /tests, and the reward goes to
// /logs/verifier.
//
// The reward is 1 only if every check passes:
//
//  1. static: the agent changed only allowed paths; its features read as
//     acceptance criteria (no variables, no code, named scenarios with an
//     outcome); it left no probes for the harness and no no-op custom steps;
//  2. axx validate reports no problems; axx lint passes and its rules catch a
//     planted duplicate (when the task asks);
//  3. axx run passes against the correct app (at least min_scenarios, nothing
//     skipped or undefined; repeated on fresh data in random orders when the
//     task asks);
//  4. axx run fails (exit code 1, a failing scenario) against every mutant the
//     task targets.
//
// The app runs as the "parcels" user with EVALS_MUTANT in its own
// environment only; axx runs as the
// "tester" user, which can neither read that environment nor the verifier's
// files.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/nimbusxr/axx/evals/internal/manifest"
	"github.com/nimbusxr/axx/evals/internal/mutant"
	"github.com/nimbusxr/axx/evals/internal/spec"
	"github.com/nimbusxr/axx/evals/internal/verify"
)

type options struct {
	tests, workspace, out string
	axx, app              string
	tester, appUser       string
	appURL                string
	skipWait              bool
}

// Report is written to <out>/verify.json.
type Report struct {
	Reward  float64          `json:"reward"`
	Summary string           `json:"summary"`
	Checks  []Check          `json:"checks"`
	Static  *verify.Static   `json:"static,omitempty"`
	Runs    []*RunResult     `json:"runs,omitempty"`
	Mutants map[string]*Kill `json:"mutants,omitempty"`
	// MutantsTotal is the number of mutants the task targets.
	MutantsTotal int    `json:"mutantsTotal"`
	Duration     string `json:"duration"`
}

// Check is one pass/fail line of the report.
type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

// Kill records how a mutant fared.
type Kill struct {
	Killed bool   `json:"killed"`
	Detail string `json:"detail"`
}

// RunResult is one axx run.
type RunResult struct {
	Label     string          `json:"label"`
	Mutant    string          `json:"mutant,omitempty"`
	ExitCode  int             `json:"exitCode"`
	Counts    map[string]int  `json:"counts"`
	Failures  []runFailure    `json:"failures,omitempty"`
	Scenarios map[string]bool `json:"scenarios,omitempty"` // name -> passed
	Error     string          `json:"error,omitempty"`
	Seconds   float64         `json:"seconds"`
}

type runFailure struct {
	Scenario   string `json:"scenario"`
	Location   string `json:"location"`
	Step       string `json:"step,omitempty"`
	Definition string `json:"definition,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Message    string `json:"message,omitempty"`
}

type verifier struct {
	opt    options
	spec   *spec.Verify
	report *Report
	tester *account
	appAcc *account
	env    []string
}

type account struct {
	uid, gid uint32
	home     string
}

func main() {
	var o options
	flag.StringVar(&o.tests, "tests", "/tests", "directory with verify.toml and initial.sha256")
	flag.StringVar(&o.workspace, "workspace", "/app", "the agent's project")
	flag.StringVar(&o.out, "out", "/logs/verifier", "where the reward and the report go")
	flag.StringVar(&o.axx, "axx", "axx", "axx binary")
	flag.StringVar(&o.app, "app", "parcels", "the app binary")
	flag.StringVar(&o.tester, "tester", "tester", "user that runs axx")
	flag.StringVar(&o.appUser, "app-user", "parcels", "user that runs the app")
	flag.StringVar(&o.appURL, "app-url", "http://localhost:8080", "the app's base URL")
	flag.BoolVar(&o.skipWait, "skip-wait", false, "do not wait for the infrastructure")
	flag.Parse()

	start := time.Now()
	v := &verifier{opt: o, report: &Report{Mutants: map[string]*Kill{}}}
	err := v.run(context.Background())
	if err != nil {
		v.check("verifier", false, err.Error())
	}
	v.report.Duration = time.Since(start).Round(time.Second).String()
	v.finish()
}

func (v *verifier) check(name string, passed bool, detail string) bool {
	v.report.Checks = append(v.report.Checks, Check{Name: name, Passed: passed, Detail: detail})
	mark := "PASS"
	if !passed {
		mark = "FAIL"
	}
	fmt.Printf("%s  %s", mark, name)
	if detail != "" {
		fmt.Printf(": %s", detail)
	}
	fmt.Println()
	return passed
}

func (v *verifier) run(ctx context.Context) error {
	var err error
	if v.spec, err = spec.LoadVerify(filepath.Join(v.opt.tests, "verify.toml")); err != nil {
		return err
	}
	initial, err := manifest.Read(filepath.Join(v.opt.tests, "initial.sha256"))
	if err != nil {
		return err
	}
	initialConfig, _ := os.ReadFile(filepath.Join(v.opt.tests, "initial", "axx.yaml"))
	if v.tester, err = lookup(v.opt.tester); err != nil {
		return err
	}
	if v.appAcc, err = lookup(v.opt.appUser); err != nil {
		return err
	}
	v.env = baseEnv()

	// 1. Static checks.
	st, err := verify.CheckStatic(v.opt.workspace, v.spec, initial, initialConfig)
	if err != nil {
		return err
	}
	v.report.Static = st
	var msgs []string
	for _, f := range st.Findings {
		msgs = append(msgs, f.Rule+": "+f.Message)
	}
	changed := make([]string, 0, len(st.Changes))
	for _, c := range st.Changes {
		changed = append(changed, c.Kind+" "+c.Path)
	}
	fmt.Printf("changes: %s\n", strings.Join(changed, ", "))
	if !v.check("static rules", len(st.Findings) == 0, strings.Join(msgs, "; ")) {
		return nil
	}
	if len(st.Changes) == 0 {
		v.check("the agent changed the project", false, "no file was added or changed")
		return nil
	}

	if !v.opt.skipWait {
		if err := waitServices(ctx, os.Getenv("PARCELS_KAFKA_BROKERS") != ""); err != nil {
			return err
		}
	}
	if err := chown(v.opt.workspace, v.tester); err != nil {
		return err
	}
	_ = os.MkdirAll("/tmp/evals", 0o777)
	_ = os.Chmod("/tmp/evals", 0o777)

	// 2. Validation and test-data lint.
	if !v.validate(ctx) {
		return nil
	}
	if v.spec.Lint.Required && !v.lint(ctx, st) {
		return nil
	}
	// 3. The correct app.
	if !v.correct(ctx) {
		return nil
	}

	// 4. Mutants.
	all := true
	for _, m := range v.spec.Mutants {
		res, err := v.axxRun(ctx, "mutant-"+m, m)
		kill := &Kill{}
		v.report.Mutants[m] = kill
		switch {
		case err != nil:
			kill.Detail = err.Error()
		case res.ExitCode != 1:
			kill.Detail = fmt.Sprintf("axx run exited %d (%s); a caught bug is a failing scenario (exit 1)", res.ExitCode, describe(res))
		case len(res.Failures) == 0:
			kill.Detail = "exit 1 without a failing scenario"
		default:
			kill.Killed = true
			kill.Detail = fmt.Sprintf("%d scenario(s) failed, first: %s", len(res.Failures), res.Failures[0].Scenario)
		}
		all = v.check("catches mutant "+m, kill.Killed, kill.Detail) && all
	}
	if all {
		v.report.Reward = 1
	}
	return nil
}

func (v *verifier) validate(ctx context.Context) bool {
	out, code, err := v.asTester(ctx, 2*time.Minute, v.opt.workspace, v.opt.axx, "validate", "--json")
	if err != nil {
		return v.check("axx validate", false, err.Error())
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Scenarios int `json:"scenarios"`
			Problems  []struct {
				Kind     string `json:"kind"`
				Location string `json:"location"`
				Text     string `json:"text"`
				Message  string `json:"message"`
			} `json:"problems"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(out, &env)
	var probs []string
	for _, p := range env.Data.Problems {
		probs = append(probs, fmt.Sprintf("%s %s %q %s", p.Kind, p.Location, p.Text, p.Message))
	}
	for _, e := range env.Errors {
		probs = append(probs, e.Message)
	}
	if code != 0 && len(probs) == 0 {
		probs = append(probs, fmt.Sprintf("exit code %d: %s", code, tail(string(out), 400)))
	}
	return v.check("axx validate", code == 0 && env.OK && len(probs) == 0, strings.Join(probs, "; "))
}

// lint runs `axx lint` (it must pass), then plants a copy of one of the
// agent's data files and runs it again (it must fail: the rules bite).
func (v *verifier) lint(ctx context.Context, st *verify.Static) bool {
	out, code, err := v.asTester(ctx, 2*time.Minute, v.opt.workspace, v.opt.axx, "lint")
	if err != nil || code != 0 {
		return v.check("axx lint", false, errOr(err, fmt.Sprintf("exit %d: %s", code, tail(string(out), 600))))
	}
	v.check("axx lint", true, "")
	if v.spec.Lint.Bites == "" {
		return true
	}
	var candidate string
	for _, c := range st.Changes {
		if c.Kind == "deleted" {
			continue
		}
		if ok, _ := doublestar.Match(v.spec.Lint.Bites, c.Path); ok {
			candidate = c.Path
			break
		}
	}
	if candidate == "" {
		return v.check("lint rules catch a reused value", false, "no data file matching "+v.spec.Lint.Bites+" was added")
	}
	src := filepath.Join(v.opt.workspace, filepath.FromSlash(candidate))
	ext := filepath.Ext(src)
	dup := strings.TrimSuffix(src, ext) + "-copy" + ext
	b, err := os.ReadFile(src)
	if err != nil {
		return v.check("lint rules catch a reused value", false, err.Error())
	}
	if err := os.WriteFile(dup, b, 0o644); err != nil {
		return v.check("lint rules catch a reused value", false, err.Error())
	}
	defer os.Remove(dup)
	_ = os.Lchown(dup, int(v.tester.uid), int(v.tester.gid))
	out, code, err = v.asTester(ctx, 2*time.Minute, v.opt.workspace, v.opt.axx, "lint")
	rel, _ := filepath.Rel(v.opt.workspace, dup)
	if err != nil || code != 3 {
		return v.check("lint rules catch a reused value", false,
			fmt.Sprintf("with a copy of %s at %s, axx lint exited %d instead of 3: %s", candidate, rel, code, tail(string(out), 300)))
	}
	return v.check("lint rules catch a reused value", true, "a copy of "+candidate+" fails axx lint")
}

func (v *verifier) correct(ctx context.Context) bool {
	for i := 1; i <= v.spec.Run.Repeat; i++ {
		label, name := "correct", "passes against the correct app"
		if v.spec.Run.Repeat > 1 {
			label = fmt.Sprintf("correct-%d", i)
			name = fmt.Sprintf("passes against the correct app (run %d of %d, fresh data, random order)", i, v.spec.Run.Repeat)
		}
		var res *RunResult
		var err error
		if v.spec.StartApps && i == 1 {
			name += ", started by axx from axx.yaml"
			res, err = v.axxRunWithApps(ctx, label)
		} else {
			res, err = v.axxRun(ctx, label, "")
		}
		if err != nil {
			return v.check(name, false, err.Error())
		}
		passed := res.Counts["passed"]
		bad := res.ExitCode != 0 || res.Counts["failed"] > 0 || passed < v.spec.MinScenarios
		for _, k := range []string{"skipped", "undefined", "pending", "ambiguous"} {
			if res.Counts[k] > 0 {
				bad = true
			}
		}
		detail := describe(res)
		if passed < v.spec.MinScenarios {
			detail += fmt.Sprintf("; at least %d passing scenarios required", v.spec.MinScenarios)
		}
		if !v.check(name, !bad, detail) {
			return false
		}
		if i > 1 {
			continue
		}
		for _, sc := range v.spec.PreserveScenarios {
			if ok, found := res.Scenarios[sc]; !found || !ok {
				return v.check("keeps scenario "+strconv.Quote(sc), false, "missing or not passing")
			}
		}
		if len(v.spec.PreserveScenarios) > 0 {
			v.check("keeps the original scenarios", true, "")
		}
	}
	return true
}

// axxRun resets the data, starts the app (as a mutant when m is set), runs
// the suite and stops the app.
func (v *verifier) axxRun(ctx context.Context, label, m string) (*RunResult, error) {
	if err := v.reset(ctx); err != nil {
		return nil, err
	}
	app, err := v.startApp(ctx, label, m)
	if err != nil {
		return nil, err
	}
	defer v.stopApp(app)
	res, err := v.axxOnce(ctx, label, m, false)
	if res != nil {
		v.report.Runs = append(v.report.Runs, res)
	}
	return res, err
}

// axxRunWithApps resets the data and runs a plain `axx run`: axx starts the
// service from the project's axx.yaml.
func (v *verifier) axxRunWithApps(ctx context.Context, label string) (*RunResult, error) {
	if err := v.reset(ctx); err != nil {
		return nil, err
	}
	res, err := v.axxOnce(ctx, label, "", true)
	if res != nil {
		v.report.Runs = append(v.report.Runs, res)
	}
	return res, err
}

func (v *verifier) axxOnce(ctx context.Context, label, m string, startApps bool) (*RunResult, error) {
	start := time.Now()
	cuke := filepath.Join("/tmp/evals", label+".cucumber.json")
	_ = os.Remove(cuke)
	args := []string{
		"run", "--json", "--workers", strconv.Itoa(v.spec.Run.Workers),
		"--order", fmt.Sprintf("random:%d", rand.IntN(1_000_000)), "--format", "cucumber-json:" + cuke,
	}
	if !startApps {
		args = append(args, "--no-start")
	}
	if !v.spec.RespectTags {
		args = append(args, "--tags", "not @evals-never-matches")
	}
	out, code, err := v.asTester(ctx, v.spec.Run.Timeout.Duration, v.opt.workspace, v.opt.axx, args...)
	_ = os.WriteFile(filepath.Join(v.opt.out, "run-"+label+".json"), out, 0o644)
	res := &RunResult{Label: label, Mutant: m, ExitCode: code, Counts: map[string]int{}, Seconds: time.Since(start).Seconds()}
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	var env struct {
		Data struct {
			Counts struct {
				Scenarios map[string]int `json:"scenarios"`
			} `json:"counts"`
			Failures []struct {
				Scenario string `json:"scenario"`
				Location string `json:"location"`
				Step     *struct {
					Text       string `json:"text"`
					Definition string `json:"definition"`
				} `json:"step"`
				Error *struct {
					Kind    string `json:"kind"`
					Message string `json:"message"`
				} `json:"error"`
			} `json:"failures"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if !json.Valid(out) || json.Unmarshal(out, &env) != nil {
		res.Error = "axx run did not print a JSON report: " + tail(string(out), 300)
	}
	if env.Data.Counts.Scenarios != nil {
		res.Counts = env.Data.Counts.Scenarios
	}
	for _, f := range env.Data.Failures {
		rf := runFailure{Scenario: f.Scenario, Location: f.Location}
		if f.Step != nil {
			rf.Step, rf.Definition = f.Step.Text, f.Step.Definition
		}
		if f.Error != nil {
			rf.Kind, rf.Message = f.Error.Kind, tail(f.Error.Message, 300)
		}
		res.Failures = append(res.Failures, rf)
	}
	if len(env.Errors) > 0 {
		res.Error = env.Errors[0].Message
	}
	res.Scenarios = scenarioResults(cuke)
	return res, nil
}

// scenarioResults reads a cucumber-json report: scenario name -> passed.
func scenarioResults(path string) map[string]bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var features []struct {
		Elements []struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Steps []struct {
				Result struct {
					Status string `json:"status"`
				} `json:"result"`
			} `json:"steps"`
		} `json:"elements"`
	}
	if json.Unmarshal(b, &features) != nil {
		return nil
	}
	out := map[string]bool{}
	for _, f := range features {
		for _, e := range f.Elements {
			if e.Type == "background" {
				continue
			}
			ok := len(e.Steps) > 0
			for _, s := range e.Steps {
				if s.Result.Status != "passed" {
					ok = false
				}
			}
			if prev, seen := out[e.Name]; seen {
				ok = ok && prev
			}
			out[e.Name] = ok
		}
	}
	return out
}

func describe(r *RunResult) string {
	if r == nil {
		return "no result"
	}
	var parts []string
	keys := make([]string, 0, len(r.Counts))
	for k := range r.Counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, r.Counts[k]))
	}
	s := fmt.Sprintf("exit %d, scenarios: %s", r.ExitCode, strings.Join(parts, ", "))
	if len(parts) == 0 {
		s = fmt.Sprintf("exit %d, no scenarios ran", r.ExitCode)
	}
	if r.Error != "" {
		s += "; " + r.Error
	}
	for i, f := range r.Failures {
		if i == 3 {
			s += fmt.Sprintf("; +%d more", len(r.Failures)-3)
			break
		}
		s += fmt.Sprintf("; %s [%s] %s", f.Scenario, f.Step, f.Message)
	}
	return s
}

// ---- the app ----

type appProc struct {
	cmd  *exec.Cmd
	done chan struct{}
	log  *os.File
}

func (v *verifier) reset(ctx context.Context) error {
	c, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(c, v.opt.app, "reset")
	cmd.Env = v.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("parcels reset: %w: %s", err, tail(string(out), 400))
	}
	return nil
}

func (v *verifier) startApp(ctx context.Context, label, m string) (*appProc, error) {
	for i := 0; ; i++ {
		conn, err := dial(ctx, hostPort(v.opt.appURL), time.Second)
		if err != nil {
			break
		}
		_ = conn.Close()
		if i == 50 {
			return nil, fmt.Errorf("something still listens on %s", v.opt.appURL)
		}
		time.Sleep(200 * time.Millisecond)
	}
	logf, err := os.Create(filepath.Join(v.opt.out, "app-"+label+".log"))
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(context.WithoutCancel(ctx), v.opt.app)
	cmd.Env = append(append([]string{}, v.env...), mutant.EnvVar+"="+m, "HOME="+v.appAcc.home)
	cmd.Dir = v.appAcc.home
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: &syscall.Credential{Uid: v.appAcc.uid, Gid: v.appAcc.gid}}
	if err := cmd.Start(); err != nil {
		_ = logf.Close()
		return nil, err
	}
	p := &appProc{cmd: cmd, done: make(chan struct{}), log: logf}
	go func() { _ = cmd.Wait(); close(p.done) }()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			_ = logf.Close()
			b, _ := os.ReadFile(logf.Name())
			return nil, fmt.Errorf("the app exited during start-up: %s", tail(string(b), 600))
		default:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, v.opt.appURL+"/health", nil)
		if res, err := http.DefaultClient.Do(req); err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return p, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	v.stopApp(p)
	return nil, errors.New("the app did not become healthy within 90s")
}

func (v *verifier) stopApp(p *appProc) {
	if p == nil {
		return
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
	}
	_ = p.log.Close()
	// Wait until the port is free for the next run.
	for range 50 {
		conn, err := dial(context.Background(), hostPort(v.opt.appURL), 200*time.Millisecond)
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(100 * time.Millisecond)
	}
}

// asTester runs a command as the tester user, killing its whole process
// group (plugins included) when it times out.
func (v *verifier) asTester(ctx context.Context, timeout time.Duration, dir, name string, args ...string) ([]byte, int, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(context.WithoutCancel(ctx), name, args...)
	cmd.Dir = dir
	cmd.Env = append(append([]string{}, v.env...), "HOME="+v.tester.home, "NO_COLOR=1", "CI=true")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: &syscall.Credential{Uid: v.tester.uid, Gid: v.tester.gid}}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return nil, -1, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-c.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return append(stdout.Bytes(), stderr.Bytes()...), -1, fmt.Errorf("%s %s timed out after %s", filepath.Base(name), args[0], timeout)
	}
	// Plugins may outlive axx; clean up the group.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		return nil, -1, err
	}
	out := stdout.Bytes()
	if len(bytes.TrimSpace(out)) == 0 {
		out = stderr.Bytes()
	}
	return out, code, nil
}

// ---- environment ----

// baseEnv is the verifier's environment minus anything evals-specific.
func baseEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "EVALS_") || k == "HOME" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func lookup(name string) (*account, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return nil, fmt.Errorf("user %s: %w", name, err)
	}
	uid, _ := strconv.ParseUint(u.Uid, 10, 32)
	gid, _ := strconv.ParseUint(u.Gid, 10, 32)
	return &account{uid: uint32(uid), gid: uint32(gid), home: u.HomeDir}, nil
}

func chown(root string, a *account) error {
	return filepath.Walk(root, func(p string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, int(a.uid), int(a.gid))
	})
}

func waitServices(ctx context.Context, kafka bool) error {
	targets := []string{"postgres:5432", "mongo:27017", "address-service:8080"}
	if kafka {
		targets = append(targets, "kafka:9092", "schema-registry:8081")
	}
	deadline := time.Now().Add(3 * time.Minute)
	for _, t := range targets {
		for {
			conn, err := dial(ctx, t, 2*time.Second)
			if err == nil {
				_ = conn.Close()
				break
			}
			if time.Now().After(deadline) || ctx.Err() != nil {
				return fmt.Errorf("%s is not reachable: %w", t, err)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	return nil
}

func dial(ctx context.Context, addr string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "tcp", addr)
}

func hostPort(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "http://"), "https://")
	if i := strings.IndexByte(u, '/'); i >= 0 {
		u = u[:i]
	}
	return u
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}

func errOr(err error, s string) string {
	if err != nil {
		return err.Error()
	}
	return s
}

// ---- results ----

func (v *verifier) finish() {
	r := v.report
	failed := 0
	for _, c := range r.Checks {
		if !c.Passed {
			failed++
		}
	}
	if failed > 0 {
		r.Reward = 0
	}
	killed := 0
	for _, k := range r.Mutants {
		if k.Killed {
			killed++
		}
	}
	r.MutantsTotal = len(v.specMutants())
	r.Summary = fmt.Sprintf("reward %.0f: %d check(s) failed, %d/%d mutants caught", r.Reward, failed, killed, r.MutantsTotal)
	fmt.Println(r.Summary)
	_ = os.MkdirAll(v.opt.out, 0o755)
	b, _ := json.MarshalIndent(r, "", "  ")
	_ = os.WriteFile(filepath.Join(v.opt.out, "verify.json"), b, 0o644)
	// Only the reward: Harbor summarizes every key. Details are in verify.json.
	rewards := map[string]float64{"reward": r.Reward}
	rb, _ := json.Marshal(rewards)
	if err := os.WriteFile(filepath.Join(v.opt.out, "reward.json"), rb, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "cannot write the reward:", err)
		os.Exit(1)
	}
	_ = os.WriteFile(filepath.Join(v.opt.out, "reward.txt"), []byte(strconv.FormatFloat(r.Reward, 'f', -1, 64)+"\n"), 0o644)
}

func (v *verifier) specMutants() []string {
	if v.spec == nil {
		return nil
	}
	return v.spec.Mutants
}
