package cli

import (
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
)

func restProject(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [rest]\n"})
}

// Agents try `axx --version`: it prints what `axx version` prints.
func TestVersionFlagIsTheVersionCommand(t *testing.T) {
	want, _, code := run(t, "version")
	if code != int(exitcode.OK) {
		t.Fatalf("version: exit %d", code)
	}
	got, stderr, code := run(t, "--version")
	if code != int(exitcode.OK) || got != want {
		t.Fatalf("--version: exit %d, %q, want %q (%s)", code, got, want, stderr)
	}
	// axx alone still prints the help.
	if out, _, code := run(t); code != int(exitcode.OK) || !strings.Contains(out, "Available Commands") {
		t.Errorf("axx: exit %d\n%s", code, out)
	}
	// and an unknown command is still one.
	if _, stderr, code := run(t, "nope"); code != int(exitcode.Usage) || !strings.Contains(stderr, `unknown command "nope"`) {
		t.Errorf("axx nope: exit %d\n%s", code, stderr)
	}
}

// Agents try `axx steps list`: it is `axx steps`.
func TestStepsListIsSteps(t *testing.T) {
	restProject(t)
	want, _, code := run(t, "steps")
	if code != int(exitcode.OK) {
		t.Fatalf("steps: exit %d", code)
	}
	got, stderr, code := run(t, "steps", "list")
	if code != int(exitcode.OK) || got != want {
		t.Fatalf("steps list: exit %d (%s)", code, stderr)
	}
}

// `axx steps show` takes an id, an expression (or part of it, as agents
// copy it), or a step line.
func TestStepsShowFindsAStepByIdExpressionOrLine(t *testing.T) {
	restProject(t)
	for _, q := range []string{
		"rest.response.status",
		"the[[ {ordinal} ordered]] response status code is {int}[[ on {service}]]",
		"the[[ {ordinal} ordered]] response status code is {int}",
		"the response status code is {int}",
		"Then the 2nd ordered response status code is 201",
		"the response status code is 404",
	} {
		out, stderr, code := run(t, "steps", "show", q)
		if code != int(exitcode.OK) || !strings.HasPrefix(out, "rest.response.status ") {
			t.Errorf("%q: exit %d\n%s%s", q, code, out, stderr)
		}
	}
}

// What matches nothing gets the closest steps in the hint.
func TestStepsShowNamesTheClosestSteps(t *testing.T) {
	restProject(t)
	_, stderr, code := run(t, "steps", "show", "the request payload properties")
	if code != int(exitcode.Usage) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"AXX-E0310", "the closest: rest.request.properties", "axx steps search"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the error lacks %q:\n%s", want, stderr)
		}
	}
}
