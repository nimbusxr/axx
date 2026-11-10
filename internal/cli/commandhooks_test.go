package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// hookEvents records what the hook probes saw, in order.
var hookEvents struct {
	sync.Mutex
	lines []string
	calls []core.CommandHookCall
}

func hookEvent(line string, call *core.CommandHookCall) {
	hookEvents.Lock()
	defer hookEvents.Unlock()
	hookEvents.lines = append(hookEvents.lines, line)
	if call != nil {
		hookEvents.calls = append(hookEvents.calls, *call)
	}
}

func takeHookEvents() ([]string, []core.CommandHookCall) {
	hookEvents.Lock()
	defer hookEvents.Unlock()
	lines, calls := hookEvents.lines, hookEvents.calls
	hookEvents.lines, hookEvents.calls = nil, nil
	return lines, calls
}

// hookProbe is a pack whose hooks say when they ran. Its settings decide what
// they do: a hook named in fail returns an error, in failWithCode an axx
// error of its own.
type hookProbe struct{ name string }

type hookProbeSettings struct {
	Log          string   `json:"log"`
	Greeting     string   `json:"greeting"`
	Fail         []string `json:"fail"`
	FailWithCode []string `json:"failWithCode"`
}

func (p hookProbe) hook(phase core.CommandPhase, command string) core.CommandHook {
	return core.CommandHook{Command: command, Phase: phase, Doc: "Says when it ran.", Run: func(call *core.CommandHookCall) error {
		id := fmt.Sprintf("%s %s %s", p.name, phase, call.Command)
		var s hookProbeSettings
		if err := call.Settings(&s); err != nil {
			return err
		}
		line := id
		if s.Greeting != "" {
			line += " (" + s.Greeting + ")"
		}
		if call.Err != nil {
			line += " after an error"
		}
		hookEvent(line, call)
		if s.Log != "" {
			appendLine(s.Log, line)
		}
		switch {
		case slices.Contains(s.Fail, id):
			return errors.New("the probe was told to fail")
		case slices.Contains(s.FailWithCode, id):
			return axxerr.New("AXX-E0904", exitcode.Failed, "the probe failed its check").WithHint("it was told to")
		}
		return nil
	}}
}

func (p hookProbe) Manifest() core.Manifest {
	m := core.Manifest{Name: p.name, CommandHooks: []core.CommandHook{
		p.hook(core.BeforeCommand, "parcels label"),
		p.hook(core.AfterCommand, "parcels label"),
		p.hook(core.BeforeCommand, "fixtures  generate"),
	}}
	if p.name == "hook-probe" {
		m.CommandHooks = append(m.CommandHooks,
			core.CommandHook{Command: "fixtures", Phase: core.BeforeCommand, Run: func(*core.CommandHookCall) error { return nil }},
			core.CommandHook{Command: "parcels lable", Phase: core.BeforeCommand, Run: func(*core.CommandHookCall) error { return nil }},
			core.CommandHook{Command: "version", Phase: core.AfterCommand, Run: func(*core.CommandHookCall) error { return nil }},
			core.CommandHook{Command: "parcels label", Run: func(*core.CommandHookCall) error { return nil }},
		)
	}
	return m
}

func init() {
	engine.Register("./hook-probe", hookProbe{name: "hook-probe"})
	engine.Register("./hook-probe-too", hookProbe{name: "hook-probe-too"})
}

// hookProject is a project that uses both probes, the second after the
// first, with settings for the first.
func hookProject(t *testing.T, settings string) string {
	t.Helper()
	dir, _ := agentProject(t, map[string]string{
		"axx.yaml":       "version: 1\npacks:\n  hook-probe:\n" + settings,
		"axx-packs.yaml": "packs:\n  - ./hook-probe\n  - ./hook-probe-too\n",
	})
	takeHookEvents()
	return dir
}

func TestHooksRunBeforeAndAfterTheirCommand(t *testing.T) {
	dir := hookProject(t, "    greeting: hi\n")
	out, errOut, code := run(t, "parcels", "label", "PX-1042", "-f", "pdf", "--note", "fragile", "--note", "leave at the depot")
	if code != int(exitcode.OK) || !strings.Contains(out, "notes fragile; leave at the depot") {
		t.Fatalf("exit %d, out %q, err %q", code, out, errOut)
	}
	lines, calls := takeHookEvents()
	want := []string{
		"hook-probe before parcels label (hi)",
		"hook-probe-too before parcels label",
		"hook-probe-too after parcels label",
		"hook-probe after parcels label (hi)",
	}
	if !slices.Equal(lines, want) {
		t.Fatalf("the hooks ran\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	c := calls[0]
	if c.Command != "parcels label" || !slices.Equal(c.Args, []string{"PX-1042"}) || c.ProjectDir != dir || c.Err != nil {
		t.Errorf("the call: command %q, args %q, project %q, err %v", c.Command, c.Args, c.ProjectDir, c.Err)
	}
	if c.Flags["format"] != "pdf" || c.Flags["return"] != "false" || c.Flags["note"] != "leave at the depot" ||
		!slices.Equal(c.Lists["note"], []string{"fragile", "leave at the depot"}) || c.Flags["json"] != "false" {
		t.Errorf("the flags: %v, lists %v", c.Flags, c.Lists)
	}
	if c.Context == nil || c.Stderr == nil || c.Logger == nil {
		t.Error("the call has no context, stderr or logger")
	}
}

func TestHooksWriteNothingToTheCommandsOutput(t *testing.T) {
	hookProject(t, "")
	out, _, code := run(t, "parcels", "label", "PX-1042", "--json")
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != int(exitcode.OK) || !env.OK {
		t.Fatalf("exit %d, %v: %s", code, err, out)
	}
	if lines, _ := takeHookEvents(); len(lines) != 4 {
		t.Errorf("the hooks ran %d times, want 4: %q", len(lines), lines)
	}
}

func TestABeforeHookThatFailsStopsTheCommand(t *testing.T) {
	hookProject(t, "    fail: [hook-probe before parcels label]\n")
	out, errOut, code := run(t, "parcels", "label", "PX-1042")
	if code != int(exitcode.Environment) || out != "" {
		t.Fatalf("exit %d, out %q (the command ran)", code, out)
	}
	if !strings.Contains(errOut, "error["+CodePackHook+"]") || !strings.Contains(errOut, "the hook-probe pack's hook before `axx parcels label`") ||
		!strings.Contains(errOut, "the probe was told to fail") || !strings.Contains(errOut, "hint: the hook-probe pack runs it before") {
		t.Errorf("stderr:\n%s", errOut)
	}
	if lines, _ := takeHookEvents(); !slices.Equal(lines, []string{"hook-probe before parcels label"}) {
		t.Errorf("after a before hook failed, the hooks ran: %q", lines)
	}

	out, _, code = run(t, "parcels", "label", "PX-1042", "--json")
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != int(exitcode.Environment) || env.OK || len(env.Errors) != 1 || env.Errors[0].Code != CodePackHook {
		t.Errorf("--json: exit %d, %v: %s", code, err, out)
	}
	takeHookEvents()
}

func TestAHookKeepsItsOwnErrorCode(t *testing.T) {
	hookProject(t, "    failWithCode: [hook-probe before parcels label]\n")
	_, errOut, code := run(t, "parcels", "label", "PX-1042")
	if code != int(exitcode.Failed) || !strings.Contains(errOut, "error[AXX-E0904]: the probe failed its check") || !strings.Contains(errOut, "hint: it was told to") {
		t.Errorf("exit %d, stderr:\n%s", code, errOut)
	}
	takeHookEvents()
}

func TestAfterHooksHearTheCommandFailed(t *testing.T) {
	hookProject(t, "")
	_, errOut, code := run(t, "parcels", "label", "PX-0000")
	if code != int(exitcode.Usage) || !strings.Contains(errOut, "error["+CodePackCommand+"]") {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	lines, calls := takeHookEvents()
	want := []string{
		"hook-probe before parcels label",
		"hook-probe-too before parcels label",
		"hook-probe-too after parcels label after an error",
		"hook-probe after parcels label after an error",
	}
	if !slices.Equal(lines, want) || !strings.Contains(calls[3].Err.Error(), "no parcel PX-0000") {
		t.Errorf("the hooks ran %q, the last told %v", lines, calls[3].Err)
	}
}

func TestAnAfterHookThatFailsFailsACommandThatSucceeded(t *testing.T) {
	hookProject(t, "    fail: [hook-probe after parcels label]\n")
	out, errOut, code := run(t, "parcels", "label", "PX-1042")
	if code != int(exitcode.Environment) || !strings.Contains(out, "label of PX-1042") ||
		!strings.Contains(errOut, "error["+CodePackHook+"]: the hook-probe pack's hook after `axx parcels label`: the probe was told to fail") {
		t.Errorf("exit %d, out %q, stderr:\n%s", code, out, errOut)
	}
	takeHookEvents()
}

// The command's own error is what a failed command ends with; an after hook
// that fails too is a warning.
func TestAnAfterHookThatFailsAfterAFailedCommandWarns(t *testing.T) {
	hookProject(t, "    fail: [hook-probe after parcels label]\n")
	_, errOut, code := run(t, "parcels", "label", "PX-0000")
	if code != int(exitcode.Usage) || !strings.Contains(errOut, "error["+CodePackCommand+"]") ||
		!strings.Contains(errOut, "warning: the hook-probe pack's hook after `axx parcels label`: the probe was told to fail ["+CodePackHook+"]") {
		t.Errorf("exit %d, stderr:\n%s", code, errOut)
	}
	takeHookEvents()
}

func TestHooksRunOnlyForThePacksTheProjectUses(t *testing.T) {
	agentProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs:\n  - ./hook-probe-too\n"})
	takeHookEvents()
	if _, _, code := run(t, "parcels", "label", "PX-1042"); code != int(exitcode.OK) {
		t.Fatalf("exit %d", code)
	}
	if lines, _ := takeHookEvents(); !slices.Equal(lines, []string{"hook-probe-too before parcels label", "hook-probe-too after parcels label"}) {
		t.Errorf("the hooks ran: %q", lines)
	}

	agentProject(t, map[string]string{"axx.yaml": "version: 1\n"})
	if _, _, code := run(t, "parcels", "label", "PX-1042"); code != int(exitcode.OK) {
		t.Fatalf("exit %d", code)
	}
	if lines, _ := takeHookEvents(); len(lines) != 0 {
		t.Errorf("a project without packs ran hooks: %q", lines)
	}
}

// A hook reads its settings as its command reads axx.yaml: with --set,
// --profile and -D.
func TestAHookReadsTheSettingsItsCommandReads(t *testing.T) {
	hookProject(t, "    greeting: ${sys:greeting}\n")
	run(t, "fixtures", "generate", "--dry-run", "--set", "packs.hook-probe.fail=[]", "-D", "greeting=hello")
	lines, calls := takeHookEvents()
	if len(lines) == 0 || lines[0] != "hook-probe before fixtures generate (hello)" || calls[0].Flags["dry-run"] != "true" {
		t.Errorf("the hooks ran %q", lines)
	}

	run(t, "fixtures", "generate", "--dry-run", "--set", "packs.hook-probe.greeting=hey")
	if lines, _ := takeHookEvents(); len(lines) == 0 || lines[0] != "hook-probe before fixtures generate (hey)" {
		t.Errorf("with --set, the hooks ran %q", lines)
	}

	writeFiles(t, ".", map[string]string{"axx.yaml": "version: 1\nprofiles:\n  warm:\n    packs:\n      hook-probe:\n        greeting: warm\n"})
	run(t, "fixtures", "generate", "--dry-run", "--profile", "warm")
	if lines, _ := takeHookEvents(); len(lines) == 0 || lines[0] != "hook-probe before fixtures generate (warm)" {
		t.Errorf("with --profile, the hooks ran %q", lines)
	}

	writeFiles(t, ".", map[string]string{"axx.yaml": "version: 1\npacks:\n  hook-probe: [not, settings]\n"})
	_, errOut, code := run(t, "parcels", "label", "PX-1042")
	if code != int(exitcode.Environment) || !strings.Contains(errOut, "packs.hook-probe: json: cannot unmarshal array") {
		t.Errorf("settings it cannot read: exit %d, stderr:\n%s", code, errOut)
	}
	takeHookEvents()
}

func TestHelpRunsNoHooks(t *testing.T) {
	hookProject(t, "")
	if _, _, code := run(t, "parcels", "label", "--help"); code != int(exitcode.OK) {
		t.Fatalf("exit %d", code)
	}
	if lines, _ := takeHookEvents(); len(lines) != 0 {
		t.Errorf("help ran hooks: %q", lines)
	}
}

func TestDoctorListsTheHooks(t *testing.T) {
	hookProject(t, "")
	c := doctorChecks(t)["command hooks"]
	for _, s := range []string{
		"hook-probe before `axx parcels label`", "hook-probe after `axx parcels label`", "hook-probe before `axx fixtures generate`",
		"hook-probe-too before `axx parcels label`",
		"the hook-probe pack's hook of `fixtures` never runs: `axx fixtures` only groups other commands: name one of them",
		"the hook-probe pack's hook of `parcels lable` never runs: axx has no command `axx parcels lable`",
		"the hook-probe pack's hook of `version` never runs: `axx version` has no hooks: it manages axx and its packs",
		"the hook-probe pack's hook of `parcels label` never runs: its phase is neither before nor after",
	} {
		if !strings.Contains(c.Detail, s) {
			t.Errorf("doctor's hooks lack %q:\n%s", s, c.Detail)
		}
	}
	if c.Status != "warn" || c.Hint == "" {
		t.Errorf("doctor's hooks: %+v", c)
	}

	agentProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs:\n  - ./hook-probe-too\n"})
	if c := doctorChecks(t)["command hooks"]; c.Status != "ok" || strings.Contains(c.Detail, "never runs") {
		t.Errorf("a project with hooks that all run: %+v", c)
	}
	agentProject(t, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs:\n  - rest\n"})
	if c, ok := doctorChecks(t)["command hooks"]; ok {
		t.Errorf("a project whose packs have no hooks: %+v", c)
	}
}

// A command that needs no packs of its own runs without them when they cannot
// be prepared, and says that their hooks did not run; one that needs them
// fails.
func TestACommandRunsWithoutHooksWhenThePacksCannotBePrepared(t *testing.T) {
	t.Setenv("AXX_SOURCE_DIR", "")
	agentProject(t, map[string]string{
		"axx.yaml":       "version: 1\n",
		"axx-packs.yaml": "packs:\n  - ./hook-probe\n  - ./not-compiled-in\n",
	})
	takeHookEvents()
	out, errOut, code := run(t, "parcels", "label", "PX-1042")
	if code != int(exitcode.OK) || !strings.Contains(out, "label of PX-1042") ||
		!strings.Contains(errOut, "warning: the project's packs could not be prepared, so `axx parcels label` runs without their hooks") {
		t.Errorf("exit %d, out %q, stderr:\n%s", code, out, errOut)
	}
	if lines, _ := takeHookEvents(); len(lines) != 0 {
		t.Errorf("hooks ran: %q", lines)
	}
	if _, errOut, code := run(t, "validate"); code == int(exitcode.OK) || strings.Contains(errOut, "warning: the project's packs") {
		t.Errorf("validate: exit %d, stderr:\n%s", code, errOut)
	}
}

func appendLine(file, line string) {
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// hookHelper is the test binary as a hook of axx.yaml: it appends what it is
// and what it was told to log, says so on stdout, and exits with code.
func hookHelper(log, name, code string) int {
	wd, _ := os.Getwd()
	failed := "-"
	if v, ok := os.LookupEnv("AXX_COMMAND_FAILED"); ok {
		failed = v
	}
	appendLine(log, fmt.Sprintf("%s (AXX_COMMAND=%s, AXX_COMMAND_FAILED=%s, in %s)", name, os.Getenv("AXX_COMMAND"), failed, filepath.Base(wd)))
	fmt.Printf("the %s hook ran\n", name)
	n, _ := strconv.Atoi(code)
	return n
}

// yamlHook is a hook of axx.yaml that runs the test binary as hookHelper.
func yamlHook(when, command, log, name, code string) string {
	argv := []string{os.Args[0], "hook", log, name, code}
	for i, a := range argv {
		argv[i] = strconv.Quote(a)
	}
	return fmt.Sprintf("    - %s: %s\n      command: [%s]\n", when, command, strings.Join(argv, ", "))
}

// yamlHookProject is a project of hook-probe and hook-probe-too, which log
// to the project's hooks.log as axx.yaml's hooks do, and axx.yaml's hooks,
// made with that log.
func yamlHookProject(t *testing.T, hooks func(log string) []string) (dir, log string) {
	t.Helper()
	t.Setenv(helperEnv, "1")
	dir, _ = agentProject(t, map[string]string{"axx-packs.yaml": "packs:\n  - ./hook-probe\n  - ./hook-probe-too\n"})
	log = filepath.Join(dir, "hooks.log")
	writeFiles(t, dir, map[string]string{"axx.yaml": fmt.Sprintf("version: 1\npacks:\n  hook-probe: {log: %q}\n  hook-probe-too: {log: %q}\nhooks:\n  commands:\n%s", log, log, strings.Join(hooks(log), ""))})
	takeHookEvents()
	return dir, log
}

func hookLog(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// axx.yaml's hooks run in its order, inside the packs' hooks: their before
// hooks first, their after hooks last.
func TestAxxYamlHooksRunAroundTheirCommand(t *testing.T) {
	dir, log := yamlHookProject(t, func(log string) []string {
		return []string{
			yamlHook("after", "parcels label", log, "an after hook", "0"),
			yamlHook("before", "parcels  label", log, "a before hook", "0"),
			yamlHook("before", "fixtures generate", log, "another command's hook", "0"),
			yamlHook("before", "parcels label", log, "a second before hook", "0"),
		}
	})
	out, errOut, code := run(t, "parcels", "label", "PX-1042", "--json")
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != int(exitcode.OK) || !env.OK {
		t.Fatalf("exit %d, %v: %s\nstderr:\n%s", code, err, out, errOut)
	}
	project := filepath.Base(dir)
	want := []string{
		"hook-probe before parcels label",
		"hook-probe-too before parcels label",
		"a before hook (AXX_COMMAND=parcels label, AXX_COMMAND_FAILED=-, in " + project + ")",
		"a second before hook (AXX_COMMAND=parcels label, AXX_COMMAND_FAILED=-, in " + project + ")",
		"an after hook (AXX_COMMAND=parcels label, AXX_COMMAND_FAILED=false, in " + project + ")",
		"hook-probe-too after parcels label",
		"hook-probe after parcels label",
	}
	if got := hookLog(t, log); !slices.Equal(got, want) {
		t.Errorf("the hooks ran\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, s := range []string{"the a before hook hook ran", "axx: running ", " before `axx parcels label`", " after `axx parcels label`"} {
		if !strings.Contains(errOut, s) {
			t.Errorf("stderr lacks %q:\n%s", s, errOut)
		}
	}
}

func TestAnAxxYamlHookThatFailsBeforeItsCommandStopsIt(t *testing.T) {
	_, log := yamlHookProject(t, func(log string) []string {
		return []string{
			yamlHook("before", "parcels label", log, "a failing hook", "3"),
			yamlHook("before", "parcels label", log, "a later hook", "0"),
			yamlHook("after", "parcels label", log, "an after hook", "0"),
		}
	})
	out, errOut, code := run(t, "parcels", "label", "PX-1042")
	if code != int(exitcode.Environment) || out != "" ||
		!strings.Contains(errOut, "error["+CodeHookFailed+"]: the hook before `axx parcels label` (hooks.commands[0]) exited with 3") ||
		!strings.Contains(errOut, "hint: its output is above") || !strings.Contains(errOut, "the a failing hook hook ran") {
		t.Errorf("exit %d, out %q, stderr:\n%s", code, out, errOut)
	}
	if got := hookLog(t, log); !slices.Equal(got[2:], []string{"a failing hook (AXX_COMMAND=parcels label, AXX_COMMAND_FAILED=-, in " + filepath.Base(filepath.Dir(log)) + ")"}) {
		t.Errorf("after the failed hook, the hooks ran %q", got)
	}
}

func TestAnAxxYamlHookAfterItsCommandHearsHowItEnded(t *testing.T) {
	_, log := yamlHookProject(t, func(log string) []string {
		return []string{yamlHook("after", "parcels label", log, "an after hook", "5")}
	})
	out, errOut, code := run(t, "parcels", "label", "PX-1042")
	if code != int(exitcode.Environment) || !strings.Contains(out, "label of PX-1042") ||
		!strings.Contains(errOut, "error["+CodeHookFailed+"]: the hook after `axx parcels label` (hooks.commands[0]) exited with 5") {
		t.Errorf("a failing hook after a command that succeeded: exit %d, out %q, stderr:\n%s", code, out, errOut)
	}

	_, errOut, code = run(t, "parcels", "label", "PX-0000")
	if code != int(exitcode.Usage) || !strings.Contains(errOut, "error["+CodePackCommand+"]") ||
		!strings.Contains(errOut, "warning: the hook after `axx parcels label` (hooks.commands[0]) exited with 5") {
		t.Errorf("a failing hook after a command that failed: exit %d, stderr:\n%s", code, errOut)
	}
	if got := hookLog(t, log); !strings.Contains(got[len(got)-3], "an after hook (AXX_COMMAND=parcels label, AXX_COMMAND_FAILED=true") {
		t.Errorf("the hooks ran %q", got)
	}
}

// A hook of axx.yaml that names no command with hooks stops every command
// but axx doctor, which says so.
func TestAnAxxYamlHookMustNameACommandWithHooks(t *testing.T) {
	var log string
	for _, c := range []struct{ command, says string }{
		{"fixtures generat", "hooks.commands[0].before: axx has no command `axx fixtures generat`"},
		{"fixtures", "hooks.commands[0].before: `axx fixtures` only groups other commands: name one of them"},
		{"pack add", "hooks.commands[0].before: `axx pack add` has no hooks: it manages axx and its packs"},
		{"fmt", "hooks.commands[0].before: `axx fmt` has no hooks: it only lays out feature files"},
	} {
		_, log = yamlHookProject(t, func(log string) []string { return []string{yamlHook("before", c.command, log, "a hook", "0")} })
		_, errOut, code := run(t, "parcels", "label", "PX-1042")
		if code != int(exitcode.Usage) || !strings.Contains(errOut, "error["+CodeHookCommand+"]") || !strings.Contains(errOut, c.says) ||
			!strings.Contains(errOut, "axx.yaml:") {
			t.Errorf("%s: exit %d, stderr:\n%s", c.command, code, errOut)
		}
		if _, _, code := run(t, "version"); code != int(exitcode.OK) {
			t.Errorf("%s: axx version, which has no hooks: exit %d", c.command, code)
		}
		check := doctorChecks(t)["command hooks"]
		if check.Status != "fail" || !strings.Contains(check.Detail, c.says) || check.Hint == "" {
			t.Errorf("%s: doctor: %+v", c.command, check)
		}
	}
	if got := hookLog(t, log); len(got) != 0 {
		t.Errorf("hooks ran: %q", got)
	}
}

// A hook of axx.yaml runs before a command or after it, and runs something.
func TestAnAxxYamlHookSaysWhenItRunsAndWhat(t *testing.T) {
	for _, c := range []struct{ hook, says string }{
		{"    - before: run\n      after: run\n      command: make schema\n", "hooks.commands.0"},
		{"    - command: make schema\n", "hooks.commands.0"},
		{"    - before: run\n", "missing property 'command'"},
		{"    - before: \"\"\n      command: make schema\n", "hooks.commands.0.before"},
	} {
		agentProject(t, map[string]string{"axx.yaml": "version: 1\nhooks:\n  commands:\n" + c.hook})
		_, errOut, code := run(t, "validate")
		if code != int(exitcode.Usage) || !strings.Contains(errOut, "AXX-E0102") || !strings.Contains(errOut, c.says) {
			t.Errorf("%q: exit %d, stderr:\n%s", c.hook, code, errOut)
		}
	}
	agentProject(t, map[string]string{"axx.yaml": "version: 1\nhooks:\n  commands:\n    - before: validate\n      command: \"  \"\n"})
	_, errOut, code := run(t, "validate")
	if code != int(exitcode.Usage) || !strings.Contains(errOut, "error["+CodeHookCommand+"]: axx.yaml:5:") ||
		!strings.Contains(errOut, "the hook before `axx validate` (hooks.commands[0]) runs nothing") {
		t.Errorf("a hook that runs nothing: exit %d, stderr:\n%s", code, errOut)
	}
}

// With shell: true, a hook's command runs through the shell.
func TestAnAxxYamlHookRunsThroughTheShell(t *testing.T) {
	t.Setenv(helperEnv, "1")
	dir, _ := agentProject(t, nil)
	log := filepath.Join(dir, "hooks.log")
	line := fmt.Sprintf("%s hook %s %s 0 && %s hook %s %s 0", os.Args[0], log, "first", os.Args[0], log, "second")
	writeFiles(t, dir, map[string]string{"axx.yaml": fmt.Sprintf("version: 1\nhooks:\n  commands:\n    - before: parcels label\n      shell: true\n      command: %q\n", line)})
	if _, errOut, code := run(t, "parcels", "label", "PX-1042"); code != int(exitcode.OK) {
		t.Fatalf("exit %d, stderr:\n%s", code, errOut)
	}
	if got := hookLog(t, log); len(got) != 2 || !strings.HasPrefix(got[0], "first ") || !strings.HasPrefix(got[1], "second ") {
		t.Errorf("the shell ran %q", got)
	}
}

// With --json, a command whose after hook fails answers once: its answer,
// with the hook's error, not ok.
func TestAFailingAfterHookIsInTheCommandsJSONAnswer(t *testing.T) {
	yamlHookProject(t, func(log string) []string {
		return []string{yamlHook("after", "parcels label", log, "an after hook", "5")}
	})
	out, _, code := run(t, "parcels", "label", "PX-1042", "--json")
	var env Envelope
	dec := json.NewDecoder(strings.NewReader(out))
	if err := dec.Decode(&env); err != nil || dec.More() {
		t.Fatalf("one answer: %v, more: %v\n%s", err, dec.More(), out)
	}
	if code != int(exitcode.Environment) || env.OK || len(env.Errors) != 1 || env.Errors[0].Code != CodeHookFailed {
		t.Errorf("exit %d, answer %+v", code, env)
	}
	if data, _ := env.Data.(map[string]any); data["reference"] != "PX-1042" {
		t.Errorf("the command's data: %v", env.Data)
	}

	// A pack's after hook that fails is in the answer too.
	hookProject(t, "    fail: [hook-probe after parcels label]\n")
	out, _, code = run(t, "parcels", "label", "PX-1042", "--json")
	dec = json.NewDecoder(strings.NewReader(out))
	env = Envelope{}
	if err := dec.Decode(&env); err != nil || dec.More() || code != int(exitcode.Environment) || env.OK || env.Errors[0].Code != CodePackHook {
		t.Errorf("a pack's hook: exit %d, %v, %s", code, err, out)
	}
	takeHookEvents()
}
