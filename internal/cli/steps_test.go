package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/stepsearch"
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

// A step's table shows the rows it knows, a service's properties, with what
// each takes: an agent learned them from the docs site before. In compact
// mode a table of many rows (a Kafka client's Java properties) names them.
func TestStepsShowShowsTheTableRows(t *testing.T) {
	restProject(t)
	for _, c := range []struct {
		args      []string
		want, not []string
	}{
		{args: []string{"steps", "show", "rest.service"}, want: []string{"  Rows:\n    url (required): the base URL requests go to", "    openapi: "}},
		// The example has url and openapi: compact output lists only the others.
		{args: []string{"--compact", "steps", "show", "rest.service"}, want: []string{"table: | property | value |\nAny other property"}},
		{args: []string{"--compact", "steps", "show", "kafka.service"}, want: []string{"table: | property | value |\n  asyncapi: "}, not: []string{"brokers (required)"}},
		{args: []string{"--compact", "steps", "show", "kafka.client.props"}, want: []string{"rows: <client>.bootstrap.servers, ", "consumer.auto.offset.reset (default earliest), "}, not: []string{"  <client>.schema.registry.url: "}},
		{args: []string{"steps", "show", "kafka.client.props"}, want: []string{"    <client>.schema.registry.url: the Schema Registry's URLs"}},
	} {
		out, stderr, code := run(t, c.args...)
		if code != int(exitcode.OK) {
			t.Errorf("axx %s: exit %d\n%s%s", strings.Join(c.args, " "), code, out, stderr)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("axx %s lacks %q:\n%.1500s", strings.Join(c.args, " "), w, out)
			}
		}
		for _, n := range c.not {
			if strings.Contains(out, n) {
				t.Errorf("axx %s has %q", strings.Join(c.args, " "), n)
			}
		}
	}
}

// Several steps in one call: a parameter's documentation comes once, and a
// step of a pack the project does not use comes with the pack to add.
func TestStepsShowSeveralStepsOnce(t *testing.T) {
	restProject(t)
	out, stderr, code := run(t, "--compact", "steps", "show", "rest.request.header", "rest.response.header.is", "kafka.client")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d: %s%s", code, out, stderr)
	}
	if n := strings.Count(out, "{word}: one word"); n != 1 {
		t.Errorf("{word} documented %d times:\n%s", n, out)
	}
	if !strings.Contains(out, "kafka.client (pack kafka)") || !strings.Contains(out, "`axx pack add kafka`") {
		t.Errorf("kafka.client missing, or its pack:\n%s", out)
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
	if code != int(exitcode.OK) || !strings.Contains(out, "no services started by axx") {
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

// `axx schema --outline` is axx.yaml's keys, nested, a line each: what an
// agent needs to write the file, without the schema's 28 KB.
func TestSchemaOutline(t *testing.T) {
	restProject(t)
	out, stderr, code := run(t, "schema", "--outline")
	full, _, _ := run(t, "schema")
	if code != int(exitcode.OK) || len(out)*3 > len(full) {
		t.Fatalf("exit %d, %d bytes of %d (%s)", code, len(out), len(full), stderr)
	}
	for _, want := range []string{"run: Run controls which features run and how.\n", "\n  tags: Tags is a default tag expression, e.g.", "\nservices: (map) ", "\n  <name>:\n    enabled: "} {
		if !strings.Contains(out, want) {
			t.Errorf("outline lacks %q:\n%s", want, out)
		}
	}
}

// Several ids show each of their steps, in one call.
func TestStepsShowSeveralIDs(t *testing.T) {
	restProject(t)
	out, stderr, code := run(t, "steps", "show", "rest.response.status", "rest.request")
	if code != int(exitcode.OK) || !strings.Contains(out, "rest.response.status  (pack rest)") || !strings.Contains(out, "\n---\n") || !strings.Contains(out, "rest.request  (pack rest)") {
		t.Fatalf("exit %d\n%s%s", code, out, stderr)
	}
	// A step line is still one query, whatever its words.
	if out, stderr, code := run(t, "steps", "show", "the", "response", "status", "code", "is", "201"); code != int(exitcode.OK) || !strings.Contains(out, "rest.response.status") {
		t.Errorf("a step line: exit %d\n%s%s", code, out, stderr)
	}
}

// The packs a project does not use answer from axx's reference of its packs,
// offline: their steps, what they reach, and how to add them.
func TestPacksTheProjectDoesNotUse(t *testing.T) {
	restProject(t)
	for _, c := range []struct {
		args      []string
		want, not []string
		wantCode  exitcode.Code
	}{
		{args: []string{"steps", "--pack", "kafka"}, want: []string{"the kafka pack is not in this project: `axx pack add kafka` adds it", "kafka.consumed.key"}},
		{args: []string{"steps", "search", "kafka event key"}, want: []string{"in axx's packs this project does not use", "kafka.event.key"}, not: []string{"rest.request.webhook"}},
		{args: []string{"steps", "search", "response status"}, want: []string{"rest.response.status"}, not: []string{"this project does not use"}},
		{args: []string{"steps", "show", "kafka.consumed.key"}, want: []string{"Examples:", "`axx pack add kafka`"}},
		{args: []string{"steps", "show", "Then the parcel-events kafka event named registered key is PX-1"}, want: []string{"kafka.consumed.key", "`axx pack add kafka`"}},
		{args: []string{"pack", "show", "sql"}, want: []string{"SQL Server", "sql.select", "`axx pack add sql`"}},
		{args: []string{"pack", "list"}, want: []string{"this project's packs", "rest ", "sql ", "PostgreSQL, MySQL"}},
		{args: []string{"pack", "show", "oracle"}, want: []string{}, wantCode: exitcode.Usage},
	} {
		out, stderr, code := run(t, c.args...)
		if code != int(c.wantCode) {
			t.Errorf("axx %s: exit %d, want %d\n%s%s", strings.Join(c.args, " "), code, c.wantCode, out, stderr)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("axx %s lacks %q:\n%s", strings.Join(c.args, " "), w, out)
			}
		}
		for _, n := range c.not {
			if strings.Contains(out, n) {
				t.Errorf("axx %s has %q:\n%s", strings.Join(c.args, " "), n, out)
			}
		}
	}
}

// A step of a pack the project does not use names that pack.
func TestUndefinedStepNamesItsPack(t *testing.T) {
	restProject(t)
	writeFiles(t, ".", map[string]string{"features/events.feature": `Feature: Events
  Scenario: A registration is announced
    Then the parcel-events kafka event named registered key is PX-1
`})
	out, _, code := run(t, "validate")
	if code != int(exitcode.Undefined) || !strings.Contains(out, "this is kafka.consumed.key of the kafka pack, which this project does not use: `axx pack add kafka`") {
		t.Errorf("validate: exit %d\n%s", code, out)
	}
}

// A project without packs gets axx's packs and what they reach, not an
// empty list.
func TestStepsWithoutPacksListsAxxsPacks(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\n"})
	out, _, code := run(t, "steps")
	if code != int(exitcode.OK) || !strings.Contains(out, "this project uses no packs yet") || !strings.Contains(out, "Kafka events") {
		t.Errorf("steps: exit %d\n%s", code, out)
	}
}

// Agents (compact output) get the short form of what they look up: a step
// without its variants, the schema's outline, a pack they use without its
// steps; --full gives the whole of each.
func TestCompactLookupsAreShort(t *testing.T) {
	restProject(t)
	for _, c := range []struct {
		args      []string
		want, not []string
	}{
		{args: []string{"--compact", "steps", "show", "rest.response.status"}, want: []string{"rest.response.status (pack rest)", "Example:"}, not: []string{"Variants:", "Argument:"}},
		{args: []string{"--compact", "steps", "show", "rest.response.status", "--full"}, want: []string{"Variants:"}},
		{args: []string{"--compact", "steps", "show", "rest.request"}, want: []string{"a request payload using an application/json empty content template"}},
		{args: []string{"--compact", "steps", "show", "rest.execute"}, want: []string{"Send a request"}, not: []string{"No cookies are kept"}},
		{args: []string{"--compact", "steps", "show", "rest.execute", "--full"}, want: []string{"No cookies are kept"}},
		{args: []string{"--compact", "schema"}, want: []string{"services:"}, not: []string{`"$schema"`, "--full"}},
		{args: []string{"--compact", "schema", "--full"}, want: []string{`"$schema"`}},
		{args: []string{"--compact", "pack", "show", "rest"}, want: []string{"`axx steps --pack rest` lists its 31 steps"}, not: []string{"rest.request.headers"}},
		{args: []string{"--compact", "pack", "show", "sql"}, want: []string{"sql.select.poll", "`axx pack add sql`"}},
		{args: []string{"--compact", "steps"}, want: []string{"\n  rest.response.status  the[[ {ordinal} ordered]] response status code is {int}"}},
	} {
		out, stderr, code := run(t, c.args...)
		if code != int(exitcode.OK) {
			t.Errorf("axx %s: exit %d\n%s%s", strings.Join(c.args, " "), code, out, stderr)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("axx %s lacks %q:\n%.600s", strings.Join(c.args, " "), w, out)
			}
		}
		for _, n := range c.not {
			if strings.Contains(out, n) {
				t.Errorf("axx %s has %q:\n%.600s", strings.Join(c.args, " "), n, out)
			}
		}
	}
}

// The outline lists the keys of a list's items, as YAML lists them: what a
// lint rule takes, which agents otherwise dug out of the JSON Schema.
func TestSchemaOutlineListsListItems(t *testing.T) {
	out, _, code := run(t, "schema", "--outline")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "  rules: (list)") || !strings.Contains(out, "\n    - name: ") || !strings.Contains(out, "\n      filePatterns: ") {
		t.Errorf("lint rules' keys missing:\n%s", out)
	}
}

func project(t *testing.T, packs string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\n", "axx-packs.yaml": "packs: [" + packs + "]\n"})
}

// jsonData reads a --json output's data and errors.
func jsonData(t *testing.T, stdout string) (data map[string]any, errs []any) {
	t.Helper()
	var env struct {
		Data   map[string]any `json:"data"`
		Errors []any          `json:"errors"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v\n%s", err, stdout)
	}
	return env.Data, env.Errors
}

// A search finds a project's own step, whatever the other packs have that
// reads as well.
func TestStepsSearchKeepsTheProjectsOwnSteps(t *testing.T) {
	for _, c := range []struct{ packs, query, want string }{
		{"rest, redis", "seed", "redis.seed"},
		{"rest, redis", "request", "rest.request"},
		{"rest, mock", "request", "rest.request"},
		{"rest, mock", "mocked request", "mock."},
		{"rest, amqp", "message", "amqp."},
		{"rest, redis", "key", "redis."},
	} {
		project(t, c.packs)
		out, stderr, code := run(t, "steps", "search", c.query)
		if code != int(exitcode.OK) || strings.Contains(out, "no step of this project's packs") || !strings.Contains(out, c.want) {
			t.Errorf("[%s] %s: exit %d\n%s%s", c.packs, c.query, code, out, stderr)
		}
	}
	// What the project has only part of still goes to the pack that has it all.
	project(t, "rest, redis")
	if out, _, _ := run(t, "steps", "search", "kafka event key"); !strings.Contains(out, "no step of this project's packs reads like") || !strings.Contains(out, "kafka.event.key") {
		t.Errorf("kafka event key:\n%s", out)
	}
}

// Several ids: each of those it knows, the ones it does not named, exit 2.
func TestStepsShowNamesTheUnknownIDs(t *testing.T) {
	restProject(t)
	out, stderr, code := run(t, "steps", "show", "kafka.event.key", "rest.nonexistent")
	if code != int(exitcode.Usage) {
		t.Fatalf("exit %d\n%s%s", code, out, stderr)
	}
	if !strings.Contains(stderr, `no step has the id "rest.nonexistent"`) || strings.Contains(stderr, "kafka.event.key") || !strings.Contains(stderr, "AXX-E0310") {
		t.Errorf("the error:\n%s", stderr)
	}
	if !strings.Contains(out, "kafka.event.key  (pack kafka)") || !strings.Contains(out, "`axx pack add kafka`") {
		t.Errorf("the step it knows:\n%s", out)
	}

	out, stderr, code = run(t, "steps", "show", "rest.response.nope", "rest.nonexistent")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, `no step has the ids "rest.response.nope", "rest.nonexistent"`) || !strings.Contains(stderr, "the closest: rest.response.") {
		t.Errorf("none it knows: exit %d\n%s%s", code, out, stderr)
	}

	out, _, code = run(t, "steps", "show", "kafka.event.key", "rest.nonexistent", "--json")
	data, errs := jsonData(t, out)
	if code != int(exitcode.Usage) || len(errs) != 1 || fmt.Sprint(data["unknown"]) != "[rest.nonexistent]" || fmt.Sprint(data["notInProject"]) != "[kafka]" || len(data["steps"].([]any)) != 1 {
		t.Errorf("--json: exit %d\n%s", code, out)
	}
	var env struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || env.OK {
		t.Errorf("--json says ok:\n%s", out)
	}

	// A step line with a word that looks like an id is still a line.
	out, stderr, code = run(t, "steps", "show", "a", "GET", "request", "to", "api.example.com")
	if code != int(exitcode.OK) || !strings.HasPrefix(out, "rest.request ") {
		t.Errorf("a step line: exit %d\n%s%s", code, out, stderr)
	}
}

// Several ids with --json say which packs the project lacks, as one id does.
func TestStepsShowSeveralIDsNameThePackToAdd(t *testing.T) {
	restProject(t)
	out, stderr, code := run(t, "steps", "show", "kafka.event.key", "rest.response.status", "--json")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d\n%s%s", code, out, stderr)
	}
	if data, _ := jsonData(t, out); fmt.Sprint(data["notInProject"]) != "[kafka]" {
		t.Errorf("kafka is not in the project:\n%.400s", out)
	}
	out, _, _ = run(t, "steps", "show", "rest.response.status", "rest.request", "--json")
	if data, _ := jsonData(t, out); data["notInProject"] != nil {
		t.Errorf("rest is in the project: %v", data["notInProject"])
	}
}

// Lists leave a step's other wordings out, and its leading forms.
func TestListsLeaveOutOtherWordings(t *testing.T) {
	project(t, "sql, rest")
	out, stderr, code := run(t, "steps", "show", "sql.seed")
	if code != int(exitcode.OK) || !strings.Contains(out, "a {filepath} db seed") || strings.Contains(out, "database seed") {
		t.Errorf("show: exit %d\n%s%s", code, out, stderr)
	}
	// Those wordings still run the step, and find it.
	for _, q := range []string{"a seed.sql database seed", "a {filepath} database seed"} {
		if out, stderr, code := run(t, "steps", "show", q); code != int(exitcode.OK) || !strings.HasPrefix(out, "sql.seed ") {
			t.Errorf("%q: exit %d\n%s%s", q, code, out, stderr)
		}
	}
	out, _, _ = run(t, "steps", "--json")
	var steps struct {
		Data struct {
			Steps []StepInfo `json:"steps"`
			Count int        `json:"count"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &steps); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, s := range steps.Data.Steps {
		ids[s.ID] = true
		if strings.HasSuffix(s.ID, stepsearch.LeadingSuffix) {
			t.Errorf("a leading form is listed: %s", s.ID)
		}
		if s.ID == "sql.seed" && strings.Contains(strings.Join(s.Variants, "\n"), "database seed") {
			t.Errorf("another wording is listed: %v", s.Variants)
		}
	}
	if !ids["sql.seed"] || !ids["rest.request.header.on"] || steps.Data.Count != len(steps.Data.Steps) {
		t.Errorf("listed %d steps, count %d", len(steps.Data.Steps), steps.Data.Count)
	}
}

// A compact step leaves a row its example shows, but not the values it takes.
func TestCompactStepKeepsARowsValues(t *testing.T) {
	project(t, "web-core")
	out, stderr, code := run(t, "--compact", "steps", "show", "web-core.app")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d\n%s%s", code, out, stderr)
	}
	for _, want := range []string{"engine", "one of chromium, firefox, webkit"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

// One number of steps for a pack, in each place that says it.
func TestAPacksStepsCountTheSameEverywhere(t *testing.T) {
	restProject(t)
	out, _, code := run(t, "pack", "show", "rest", "--json")
	var show struct {
		Data PackDetail `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &show); err != nil || code != int(exitcode.OK) {
		t.Fatalf("pack show: exit %d, %v\n%s", code, err, out)
	}
	twins := 0
	for _, s := range show.Data.Steps {
		if s.Twin != "" {
			twins++
			if s.Twin != s.ID+stepsearch.TwinSuffix {
				t.Errorf("%s: twin %s", s.ID, s.Twin)
			}
		}
	}
	if show.Data.StepCount != len(show.Data.Steps) || twins == 0 {
		t.Errorf("stepCount %d beside %d steps, %d twins", show.Data.StepCount, len(show.Data.Steps), twins)
	}
	out, _, _ = run(t, "pack", "list", "--json")
	var list struct {
		Data struct {
			Packs []PackInfo `json:"packs"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}
	if list.Data.Packs[0].Pack != "rest" || list.Data.Packs[0].Steps != show.Data.StepCount {
		t.Errorf("pack list: %+v, pack show: %d", list.Data.Packs[0], show.Data.StepCount)
	}
	out, _, _ = run(t, "steps")
	if want := fmt.Sprintf("rest (%d steps)", show.Data.StepCount); !strings.Contains(out, want) {
		t.Errorf("steps lacks %q:\n%.200s", want, out)
	}
}

// `axx pack show` says how to read what it lists, once.
func TestPackShowSaysHowToReadItsSteps(t *testing.T) {
	restProject(t)
	out, _, _ := run(t, "pack", "show", "rest")
	if n := strings.Count(out, stepsearch.TwinLegend); n != 1 || !strings.Contains(out, stepsearch.TwinMark) {
		t.Errorf("the legend %d times:\n%s", n, out)
	}
	if n := strings.Count(out, stepsearch.Notation); n != 1 {
		t.Errorf("the notation %d times:\n%s", n, out)
	}
}

// A project without packs lists axx's packs under the names `axx pack list`
// gives them.
func TestStepsWithoutPacksNamesPacksAsPackListDoes(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\n"})
	out, _, _ := run(t, "steps", "--json")
	var steps struct {
		Data struct {
			Packs []map[string]any `json:"packs"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &steps); err != nil || len(steps.Data.Packs) == 0 {
		t.Fatalf("%v\n%s", err, out)
	}
	out, _, _ = run(t, "pack", "list", "--json")
	var list struct {
		Data struct {
			Packs []map[string]any `json:"packs"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(steps.Data.Packs[0]) != fmt.Sprint(list.Data.Packs[0]) || steps.Data.Packs[0]["pack"] != "rest" {
		t.Errorf("steps: %v, pack list: %v", steps.Data.Packs[0], list.Data.Packs[0])
	}
}

// The commands that say what steps there are answer from axx's reference of
// its packs when the project's cannot be prepared, and say so; a command that
// runs them does not.
func TestAnswersFromTheReferenceWithoutThePacksPrepared(t *testing.T) {
	project(t, "rest, ./steps") // a pack of the project's that axx cannot prepare
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"steps"}, "rest.response.status"},
		{[]string{"steps", "list"}, "rest.response.status"},
		{[]string{"steps", "search", "response status"}, "rest.response.status"},
		{[]string{"steps", "search", "kafka event key"}, "kafka.event.key"},
		{[]string{"steps", "show", "rest.response.status"}, "rest.response.status"},
		{[]string{"steps", "show", "rest.response.status", "kafka.event.key"}, "kafka.event.key"},
		{[]string{"explain", "the response status code is 200"}, "rest.response.status"},
		{[]string{"pack", "list"}, "not prepared"},
	} {
		out, stderr, code := run(t, c.args...)
		if code != int(exitcode.OK) || !strings.Contains(out, c.want) {
			t.Errorf("axx %s: exit %d\n%s%s", strings.Join(c.args, " "), code, out, stderr)
		}
		if !strings.Contains(stderr, "its own knowledge of the packs it publishes") || !strings.Contains(stderr, "the project's own packs (./steps) are missing") {
			t.Errorf("axx %s: the note:\n%s", strings.Join(c.args, " "), stderr)
		}
		out, _, code = run(t, append(c.args, "--json")...)
		data, _ := jsonData(t, out)
		if note, _ := data["note"].(string); code != int(exitcode.OK) || !strings.Contains(note, "./steps") {
			t.Errorf("axx %s --json: exit %d\n%.300s", strings.Join(c.args, " "), code, out)
		}
	}
	// What needs no pack says nothing of them.
	if out, stderr, code := run(t, "explain", "AXX-E0304"); code != int(exitcode.OK) || !strings.Contains(out, "AXX-E0304") || stderr != "" {
		t.Errorf("explain a code: exit %d\n%s%s", code, out, stderr)
	}
	// axx's help too.
	if out, stderr, code := run(t, "--help"); code != int(exitcode.OK) || !strings.Contains(out, "Available Commands") {
		t.Errorf("help: exit %d\n%s%s", code, out, stderr)
	}
	// A run needs the packs, and a word that may be a pack's command too.
	for _, args := range [][]string{{"validate"}, {"nope"}} {
		if out, stderr, code := run(t, args...); code == int(exitcode.OK) || !strings.Contains(stderr, "AXX-E0304") {
			t.Errorf("axx %s: exit %d\n%s%s", strings.Join(args, " "), code, out, stderr)
		}
	}
	// A project of only its own packs is not one without packs.
	project(t, "./steps")
	if out, stderr, code := run(t, "steps"); code != int(exitcode.OK) || !strings.Contains(out, "only packs of its own") || strings.Contains(out, "no packs yet") {
		t.Errorf("only its own packs: exit %d\n%s%s", code, out, stderr)
	}
	// A pack axx does not publish is still reported as that.
	project(t, "rest, nosuchpack")
	if out, stderr, code := run(t, "steps"); code != int(exitcode.Usage) || !strings.Contains(stderr, "not a pack axx publishes") {
		t.Errorf("a pack axx does not publish: exit %d\n%s%s", code, out, stderr)
	}
}

// `axx run --json` names the pack of a step the project lacks, beside what it
// has always reported.
func TestRunJSONNamesThePackToAdd(t *testing.T) {
	restProject(t)
	writeFiles(t, ".", map[string]string{"features/events.feature": `Feature: Events
  Scenario: A registration is announced
    Then the parcel-events kafka event named registered key is PX-1
`})
	out, stderr, code := run(t, "run", "--json")
	if code != int(exitcode.Undefined) {
		t.Fatalf("exit %d\n%s%s", code, out, stderr)
	}
	var env struct {
		Data struct {
			Failures []struct {
				Error struct {
					Kind, Message, Pack, Hint string
					Suggestions               []string
				} `json:"error"`
			} `json:"failures"`
			Undefined []struct {
				Text, Location, Pack, Hint string
				Suggestions                []string
			} `json:"undefined"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(env.Data.Undefined) != 1 || len(env.Data.Failures) != 1 {
		t.Fatalf("%s", out)
	}
	u, f := env.Data.Undefined[0], env.Data.Failures[0].Error
	if u.Pack != "kafka" || !strings.Contains(u.Hint, "`axx pack add kafka`") || len(u.Suggestions) == 0 || u.Location == "" {
		t.Errorf("undefined: %+v", u)
	}
	if f.Pack != "kafka" || !strings.Contains(f.Hint, "`axx pack add kafka`") || len(f.Suggestions) == 0 || f.Kind != "undefined" || f.Message == "" {
		t.Errorf("failure: %+v", f)
	}
}

func TestStepsHelpSaysToReadTheListOnce(t *testing.T) {
	out, _, _ := run(t, "steps", "--help")
	if !strings.Contains(out, "read this list once, search for what it lacks") || strings.Contains(out, "search before writing") {
		t.Errorf("steps --help:\n%s", out)
	}
}

// The docs show the section `axx init` writes in AGENTS.md, word for word.
func TestTheDocsShowTheAgentsSection(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "src", "content", "docs", "guides", "set-up-agents.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), strings.TrimSpace(agentsBlock)) {
		t.Error("docs/src/content/docs/guides/set-up-agents.md differs from agentsBlock")
	}
}
