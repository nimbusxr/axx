package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
)

// toldProject is a project whose service tells axx where its log is, which
// its scenario reads as ${sys:log.url}; hooks are axx.yaml's hooks.commands.
func toldProject(t *testing.T, tell, hooks string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv(helperEnv, "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	command := "[" + q(exe) + ", parcels, api, ready]"
	if tell != "" {
		command = "[" + q(exe) + ", tell, " + q(tell) + ", parcels, api, ready]"
	}
	yaml := "services:\n  api:\n    command: " + command + "\n" +
		"    ready:\n      log: parcels api ready\n      timeout: 20s\n"
	if hooks != "" {
		yaml += "hooks:\n  commands:\n" + strings.ReplaceAll(hooks, "EXE", q(exe))
	}
	writeFiles(t, dir, map[string]string{
		"axx.yaml":       yaml,
		"axx-packs.yaml": "packs: [logs]\n",
		"features/told.feature": `Feature: What the services tell axx
  Scenario: the log is where the service said
    Given the console log with the following properties:
      | url | ${sys:log.url} |
    Then within 5s the console log has an entry matching "parcels api serving"
`,
	})
	return dir
}

// A service tells axx a property once it started, and the scenario reads it;
// what it told goes with it.
func TestARunReadsWhatItsServicesTell(t *testing.T) {
	dir := toldProject(t, "log.url=file://.axx/logs/services.log", "")
	stdout, stderr, code := run(t, "run", "--compact")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".axx", "properties", "api")); !os.IsNotExist(err) {
		t.Errorf("what the service told outlived it: %v", err)
	}
}

// Without the service's word, the scenario has no log to read.
func TestARunWithoutTheServicesWordFails(t *testing.T) {
	toldProject(t, "", "")
	if _, _, code := run(t, "run", "--compact"); code == int(exitcode.OK) {
		t.Error("the scenario passed without ${sys:log.url}")
	}
}

// A hook before the run tells axx a property as a service does.
func TestAHookBeforeTheRunTellsAProperty(t *testing.T) {
	dir := toldProject(t, "", "    - before: run\n      command: [EXE, tell, \"log.url=file://.axx/logs/services.log\"]\n")
	stdout, stderr, code := run(t, "run", "--compact")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, ".axx", "properties")); len(entries) != 0 {
		t.Errorf("the hook's file outlived the run: %v", entries)
	}
}

// A service and a hook that tell one property stop the run: axx picks
// neither.
func TestTwoThatTellOnePropertyStopTheRun(t *testing.T) {
	toldProject(t, "log.url=file://.axx/logs/services.log", "    - before: run\n      command: [EXE, tell, \"log.url=file://elsewhere.log\"]\n")
	_, stderr, code := run(t, "run", "--compact")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, "error[AXX-E0417]") ||
		!strings.Contains(stderr, "service api and the hook before `axx run` (hooks.commands[0]) both told axx the property log.url") {
		t.Errorf("exit %d, stderr:\n%s", code, stderr)
	}
}

// -D wins over what a service tells, and the run says so.
func TestDefineWinsOverWhatAServiceTells(t *testing.T) {
	toldProject(t, "log.url=file://elsewhere.log", "")
	stdout, stderr, code := run(t, "run", "--compact", "-D", "log.url=file://.axx/logs/services.log")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "axx: using log.url=file://.axx/logs/services.log, which -D sets, not file://elsewhere.log, which service api wrote\n") {
		t.Errorf("stderr:\n%s", stderr)
	}
}

// A property axx.yaml sets wins over what a service tells, and the run says
// so: what the person sets wins.
func TestAxxYAMLWinsOverWhatAServiceTellsAndSaysSo(t *testing.T) {
	dir := toldProject(t, "log.url=file://elsewhere.log", "")
	f, err := os.OpenFile(filepath.Join(dir, "axx.yaml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("properties:\n  log.url: file://.axx/logs/services.log\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	stdout, stderr, code := run(t, "run", "--compact")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
	}
	want := "axx: using log.url=file://.axx/logs/services.log from axx.yaml, not file://elsewhere.log, which service api wrote: a value you set wins (remove it from axx.yaml's properties, or the profile's, to use service api's)\n"
	if !strings.Contains(stderr, want) {
		t.Errorf("stderr:\n%s", stderr)
	}
}

// The axx host's services tell it their properties in a folder of its own,
// which no run writes, and it sends them to the run.
func TestARunReadsWhatTheHostsServicesTell(t *testing.T) {
	dir := toldProject(t, "log.url=file://.axx/logs/services.log", "")
	serveHost(t, dir)
	stdout, stderr, code := run(t, "run", "--compact")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "axx: the axx host started api") {
		t.Errorf("stderr:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".axx", "properties")); !os.IsNotExist(err) {
		t.Errorf("the host's services told it in the project: %v", err)
	}
}

// A service's command reads what the service it depends on told, on this
// machine and on the axx host's alike.
func TestAServiceReadsWhatItsDependencyTells(t *testing.T) {
	for _, host := range []bool{false, true} {
		t.Run(map[bool]string{false: "here", true: "the host"}[host], func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv(helperEnv, "1")
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
			writeFiles(t, dir, map[string]string{
				"axx.yaml": `properties:
  log.url: file://.axx/logs/services.log
services:
  api:
    command: [` + q(exe) + `, parcels, "${sys:depot}", ready]
    dependsOn: [depot]
    ready:
      log: parcels kestrel ready
      timeout: 20s
  depot:
    command: [` + q(exe) + `, tell, depot=kestrel, depot, up, ready]
    ready:
      log: depot up ready
      timeout: 20s
`,
				"axx-packs.yaml": "packs: [logs]\n",
				"features/depot.feature": `Feature: The depot the API serves
  Scenario: the API serves the depot its dependency told
    Given the console log with the following properties:
      | url | ${sys:log.url} |
    Then within 5s the console log has an entry matching "parcels kestrel serving"
`,
			})
			if host {
				serveHost(t, dir)
			}
			stdout, stderr, code := run(t, "run", "--compact")
			if code != int(exitcode.OK) {
				t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
			}
		})
	}
}
