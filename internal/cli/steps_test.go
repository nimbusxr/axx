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

// An id that is not one, as agents guess them in the evals, gets the ids
// that start like it, or the steps its words find.
func TestStepsShowSuggestsForAGuessedID(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [rest, mongo]\n"})
	for q, want := range map[string]string{
		"rest.response.property.contains": "the closest: rest.response.property.",
		"mongo.database":                  "the closest: mongo.",
		"rest.get":                        "the closest: rest.",
	} {
		_, stderr, code := run(t, "steps", "show", q)
		if code != int(exitcode.Usage) || !strings.Contains(stderr, "AXX-E0310") || !strings.Contains(stderr, want) {
			t.Errorf("%s: exit %d, want %q in:\n%s", q, code, want, stderr)
		}
	}
}

// A step with a table shows its columns, and that a row naming them is
// optional.
func TestStepsShowShowsTheTableColumns(t *testing.T) {
	restProject(t)
	out, stderr, code := run(t, "steps", "show", "rest.response.properties.are")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"Table columns: | JSONPath | value |", "may start with a row of these names"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// The commands agents reach for on the command line, as the MCP tools and
// `axx explain` have them: env, config show, steps explain.
func TestEnvConfigShowAndStepsExplain(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{
		"axx.yaml":       "version: 1\nproperties:\n  db.password: s3cret\n  parcels.url: http://localhost:8080\n",
		"axx-packs.yaml": "packs: [rest]\n",
	})
	out, stderr, code := run(t, "env")
	if code != int(exitcode.OK) || !strings.Contains(out, "no apps started by axx") {
		t.Errorf("env: exit %d: %s%s", code, out, stderr)
	}
	out, stderr, code = run(t, "config", "show")
	if code != int(exitcode.OK) || !strings.Contains(out, "packs: core, rest") || !strings.Contains(out, "http://localhost:8080") || strings.Contains(out, "s3cret") {
		t.Errorf("config show: exit %d: %s%s", code, out, stderr)
	}
	out, stderr, code = run(t, "steps", "explain", "Given the parcels service with the following properties:")
	if code != int(exitcode.OK) || !strings.Contains(out, "rest.service") {
		t.Errorf("steps explain: exit %d: %s%s", code, out, stderr)
	}
}

// Agents guess commands: a word that is not one gets the closest command
// and what the command does take, not only a pointer to its help.
func TestGuessedCommandsGetHelpfulHints(t *testing.T) {
	restProject(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"steps", "describe", "rest.request"}, "did you mean `axx steps show`? `axx steps` has these commands: explain, list, search, show"},
		{[]string{"steps", "kafka"}, "`axx steps --pack kafka` lists the kafka pack's steps"},
		{[]string{"version", "extra"}, "`axx version` takes no arguments: print the axx version"},
		{[]string{"env", "start"}, "`axx env` has these commands: down, status, up"},
	} {
		_, stderr, code := run(t, c.args...)
		if code != int(exitcode.Usage) || !strings.Contains(stderr, c.want) {
			t.Errorf("axx %s: exit %d\n%s\nwant %q", strings.Join(c.args, " "), code, stderr, c.want)
		}
	}
	// Agents' guesses that are now commands: `axx steps inspect` is
	// `axx steps show`, `axx schema axx` (or config) is `axx schema`.
	show, _, _ := run(t, "steps", "show", "rest.request")
	if got, stderr, code := run(t, "steps", "inspect", "rest.request"); code != int(exitcode.OK) || got != show {
		t.Errorf("steps inspect: exit %d (%s)", code, stderr)
	}
	schema, _, _ := run(t, "schema")
	for _, k := range []string{"axx", "config", "axx.yaml"} {
		if got, stderr, code := run(t, "schema", k); code != int(exitcode.OK) || got != schema {
			t.Errorf("schema %s: exit %d (%s)", k, code, stderr)
		}
	}
	factory, _, _ := run(t, "schema", "--kind", "factory")
	if got, stderr, code := run(t, "schema", "factory"); code != int(exitcode.OK) || got != factory || got == schema {
		t.Errorf("schema factory: exit %d (%s)", code, stderr)
	}
	if _, stderr, code := run(t, "schema", "factory", "--kind", "fixture"); code != int(exitcode.Usage) {
		t.Errorf("two kinds: exit %d (%s)", code, stderr)
	}
	// `axx env status` is `axx env`, as the MCP env tool's status.
	want, _, code := run(t, "env")
	if code != int(exitcode.OK) {
		t.Fatalf("env: exit %d", code)
	}
	if got, stderr, code := run(t, "env", "status"); code != int(exitcode.OK) || got != want {
		t.Errorf("env status: exit %d, %q, want %q (%s)", code, got, want, stderr)
	}
}
