package axxerr

import (
	"sort"

	"github.com/nimbusxr/axx/internal/exitcode"
)

// Entry documents one error code. The catalog is the single source for the
// error-code reference page and `axx explain AXX-Exxxx`; a test keeps it in
// sync with the codes used in the source.
type Entry struct {
	Code    string        `json:"code"`
	Title   string        `json:"title"`
	Exit    exitcode.Code `json:"exit"`
	Meaning string        `json:"meaning"`
	Fix     string        `json:"fix"`
}

// Ranges groups codes by area; new codes go in their area's range.
var Ranges = []struct{ From, To, Area string }{
	{"AXX-E0001", "AXX-E0099", "Command line"},
	{"AXX-E0100", "AXX-E0199", "Configuration (axx.yaml)"},
	{"AXX-E0200", "AXX-E0299", "Feature files and filters"},
	{"AXX-E0300", "AXX-E0399", "Packs, steps and resources"},
	{"AXX-E0400", "AXX-E0499", "App lifecycle"},
	{"AXX-E0600", "AXX-E0699", "Reporters"},
	{"AXX-E0800", "AXX-E0899", "Lint"},
	{"AXX-E0900", "AXX-E0999", "Fixtures"},
}

var catalog = map[string]Entry{}

func add(code string, exit exitcode.Code, title, meaning, fix string) {
	catalog[code] = Entry{Code: code, Title: title, Exit: exit, Meaning: meaning, Fix: fix}
}

// Lookup returns the catalog entry for code.
func Lookup(code string) (Entry, bool) {
	e, ok := catalog[code]
	return e, ok
}

// Catalog returns every entry, ordered by code.
func Catalog() []Entry {
	out := make([]Entry, 0, len(catalog))
	for _, e := range catalog {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

func init() {
	u, env := exitcode.Usage, exitcode.Environment

	add("AXX-E0001", u, "Invalid usage",
		"The command line could not be parsed: an unknown command or flag, a missing flag value, or the wrong number of arguments.",
		"Run the command with `--help` to see its flags and arguments.")
	add("AXX-E0002", u, "Invalid -D property",
		"A `-D` flag is not in `name=value` form.",
		"Pass properties as `-D name=value` (repeat the flag for several properties).")
	add("AXX-E0004", u, "Cannot start the run",
		"The run options are inconsistent, for example an unknown `--order` value or a reporter whose output cannot be created.",
		"Check the message for the offending option; `axx run --help` lists the valid values.")
	add("AXX-E0005", u, "Missing output directory",
		"`axx docs export` needs to know where to write.",
		"Pass `--out <dir>`.")
	add("AXX-E0007", u, "Invalid skills scope",
		"`axx skills install --scope` accepts `project` (the repository) or `user` (your home directory).",
		"Use `--scope project` or `--scope user`.")
	add("AXX-E0010", u, "Unknown schema kind",
		"`axx schema --kind` accepts config (axx.yaml) or one of the fixture spec kinds: factory, fixture, prototype.",
		"Use `--kind config`, `--kind factory`, `--kind fixture` or `--kind prototype`.")
	add("AXX-E0009", u, "Unknown error code",
		"`axx explain` was given a code that is not in the catalog.",
		"Check the code; every code is listed on the error-codes reference page.")

	add("AXX-E0011", u, "Invalid pack argument",
		"`axx pack` was given a pack that is not the name of one of axx's packs, an existing local directory or a Go module path, or one that is not listed.",
		"`axx pack list` lists axx's packs; other packs are added by path (`./steps`, created with `axx pack new`) or Go module path (`github.com/team/axx-grpc@v1.2.0`).")
	add("AXX-E0012", u, "Unknown agent or scope",
		"`axx mcp install` connects one coding agent (`--agent claude`, `codex`, `cursor`, `vscode` or `gemini`) in one scope: `project` (the repository's own files) or `user` (your home directory).",
		"Pass `--agent` with one of the listed agents, and `--scope project` (the default) or `--scope user`.")
	add("AXX-E0013", u, "Agent configuration cannot be merged",
		"axx adds its MCP server to an agent's configuration file only when it can read the file back exactly and keep everything else in it. The file has comments, is not valid JSON, or (Codex) lists its MCP servers in a form a new `[mcp_servers.axx]` table cannot join. axx left it alone.",
		"Add the server yourself: the hint shows the lines to add. Or fix the file and run the command again.")
	add("AXX-E0014", u, "No packs to document",
		"`axx docs export` found no packs to document: it was run outside an axx project with an axx that has no packs built in, or in a project that lists no packs.",
		"Run it in your project's directory (where axx.yaml is); axx's own packs are documented at https://axx.nimbusxr.us/references/packs/. A project adds its packs with `axx pack add`.")
	add("AXX-E0015", u, "No packs for the skills",
		"`axx skills install` writes the skills with the steps of the project's packs, and the project lists none: it has no `axx-packs.yaml` yet, or the file lists no packs.",
		"Run `axx init` to set the project up: it installs the skills too. A project that is set up adds the packs its steps come from with `axx pack add`. `axx skills install --scope user` installs the skills for every project, with every pack axx publishes.")

	add("AXX-E0100", u, "Config file not found",
		"The file given with `--config` does not exist or cannot be read.",
		"Fix the path, or omit `--config` to let axx search upward for `axx.yaml` (the file is optional).")
	add("AXX-E0101", u, "Config is not valid YAML",
		"`axx.yaml` (or a profile/local override) has a YAML syntax error; the message quotes the line.",
		"Fix the indentation or quoting at the reported line. Values containing `: ` or starting with `{`, `[`, `*` or `&` need quotes.")
	add("AXX-E0102", u, "Invalid configuration",
		"`axx.yaml` does not match the schema: an unknown key, a wrong type or an invalid value. Each problem is reported with its line.",
		"Add `# yaml-language-server: $schema=https://axx.nimbusxr.us/schemas/v0/axx.schema.json` to get completion in your editor, or run `axx schema` to read the schema.")
	add("AXX-E0104", u, "Unknown profile",
		"`--profile` (or `AXX_PROFILE`) names a profile that is not defined under `profiles:` in `axx.yaml`.",
		"Define the profile or pick one of the listed names.")
	add("AXX-E0105", u, "Invalid --set",
		"A `--set` is not a dotted path to a key of `axx.yaml` and a value.",
		"Write `--set path.to.key=value`, like `--set run.workers=1` or `--set packs.<pack>.<key>=value`. The value is read as YAML, so `true` and `1` are a boolean and a number.")

	add("AXX-E0200", u, "Feature file does not parse",
		"A `.feature` file has a Gherkin syntax error.",
		"Fix the reported line. `axx validate` checks every feature without running anything.")
	add("AXX-E0201", u, "Feature path not found",
		"A path given on the command line or in `run.paths` does not exist.",
		"Check the path. Paths on the command line resolve from the working directory; `run.paths` resolve from the directory of `axx.yaml`.")
	add("AXX-E0202", u, "Invalid tag expression",
		"`--tags` (or `run.tags`) is not a valid Cucumber tag expression.",
		"Use tags with `and`, `or`, `not` and parentheses, e.g. `--tags '@smoke and not @slow'`.")
	add("AXX-E0203", u, "Invalid name filter",
		"A `--name` pattern is not a valid regular expression.",
		"Escape special characters, or pass a plain substring.")
	add("AXX-E0204", u, "Line is not in a scenario",
		"A `file:line` names a line that no scenario of the file contains, like the feature's description or its Background, so it would select nothing.",
		"Give the line of a scenario, of one of its steps or of an Examples row, or the file alone to run all of its scenarios.")
	add("AXX-E0205", u, "Unknown pack in run.uses",
		"`run.uses` lists a pack the project does not load, so no scenario could use it.",
		"List packs of `axx-packs.yaml` (`axx pack list`), like `[mobile-ios]`, or add the pack with `axx pack add`.")

	add("AXX-E0300", u, "Pack cannot be loaded",
		"A step pack failed to load or initialize: a step expression that does not compile, a duplicate parameter type, or pack configuration under `packs:` that is invalid.",
		"Read the wrapped message. For a pack of your own, fix its step definitions; for axx's packs, check `packs.<name>` in `axx.yaml`.")
	add("AXX-E0301", u, "Resource not found",
		"A file referenced by a step (seed, payload, schema, OpenAPI spec) was not found in the configured `resources` roots or next to `axx.yaml`.",
		"Check the path in the step, or add its directory to `resources:` in `axx.yaml`.")
	add("AXX-E0302", u, "Unknown pack",
		"`axx-packs.yaml` lists a pack that axx does not publish, or one this axx was not prepared with, or the file cannot be read. axx prepares itself with a project's packs when it runs in the project.",
		"Use the name of one of axx's packs (see `axx pack list`), a path to a pack in the project (`./steps`) or a Go module path; `axx pack add` checks entries for you. Run axx from the project so it can prepare the packs.")
	add("AXX-E0303", env, "Packs cannot be prepared",
		"The first time axx runs with a list of packs, it prepares itself with them, downloading what it needs once. The download failed.",
		"Check the network: axx downloads from go.dev and the Go module proxy (`GOPROXY` applies). Then run again; later runs start immediately.")
	add("AXX-E0304", u, "Packs do not build",
		"Preparing axx with the project's packs failed. The compiler output is in the message.",
		"A pack is a Go package that exports `func Pack() core.Pack`. Fix the reported errors; `axx pack new <dir>` scaffolds a working pack.")
	add("AXX-E0305", env, "Delve is needed to debug step code",
		"`axx run --debug-steps` runs axx under Delve, Go's debugger, so an IDE can stop at breakpoints in step code. Axx builds Delve itself the first time, from the Go module proxy, with the Go it downloads; it could not, and no `dlv` is installed.",
		"Check the network and run again, or install Delve with `go install github.com/go-delve/delve/cmd/dlv@latest`, or set `AXX_DLV` to the path of a `dlv`.")
	add("AXX-E0306", env, "Delve did not start",
		"`axx run --debug-steps` started Delve, but it exited or never began listening for a debugger. Its output is in the message.",
		"Check that the port is free (`--debug-steps=<port>` picks another) and that Delve works on this machine (`dlv version`).")
	add("AXX-E0310", u, "Unknown step",
		"`axx steps show` was given text that is no loaded step's id or expression and matches no step as a step line, or a line that matches several steps.",
		"The hint names the closest steps. Show one by its id (`axx steps show rest.response.status`), list the ids with `axx steps`, or search by words with `axx steps search <words>`.")

	add("AXX-E0400", u, "Invalid app configuration",
		"An `apps.<name>` setting cannot be used: a bad readiness URL, address or regex, an unknown signal, or an invalid debug setting.",
		"Fix the reported key; `axx schema` documents every app setting.")
	add("AXX-E0401", u, "Unknown app dependency",
		"`apps.<name>.dependsOn` names an app that is not declared.",
		"Declare the app or remove it from `dependsOn`.")
	add("AXX-E0402", u, "Dependency cycle",
		"Apps depend on each other in a cycle, so no start order exists.",
		"Remove one of the `dependsOn` edges in the reported cycle.")
	add("AXX-E0403", u, "Unknown app",
		"An app named with `--attach`, `--debug`, `axx up <app>` or similar is not declared in `axx.yaml`.",
		"Use one of the app names listed in the message.")
	add("AXX-E0404", u, "App has no command",
		"axx must start an app that has no `command`.",
		"Add `command:`, or run the app yourself and pass `--attach <app>`.")
	add("AXX-E0405", u, "App directory not found",
		"`apps.<name>.dir` does not exist or is not a directory.",
		"Fix `dir`; it resolves from the directory of `axx.yaml`.")
	add("AXX-E0406", env, "App failed to launch",
		"The app's process could not be started, typically because the executable is not on PATH.",
		"Check `command` (arguments are not run through a shell unless `shell: true`).")
	add("AXX-E0407", env, "App exited before ready",
		"The app's process exited before its readiness checks passed. The tail of its log is included.",
		"Read the log excerpt (full log under `.axx/logs/`) and run the command by hand to reproduce.")
	add("AXX-E0408", env, "App not ready in time",
		"The app kept running but did not pass its readiness checks within `ready.timeout`.",
		"Check the readiness URL/port/log pattern, or raise `ready.timeout`. The last check result is in the message.")
	add("AXX-E0409", env, "Debugger unavailable",
		"Debug mode was requested but no debugger was listening, and `debug.onUnavailable` is `fail`.",
		"Start the IDE debugger first (`axx ide intellij|vscode` writes the configurations), or set `onUnavailable: fallback`.")
	add("AXX-E0410", env, "App could not be stopped",
		"The app's process group did not exit after SIGTERM, the grace period and SIGKILL.",
		"Look for processes that detach from their group; `axx down` retries using the state file.")
	add("AXX-E0411", env, "App cleanup failed",
		"The app's `cleanup` command exited non-zero. The run still completed; this is reported so leaks are visible. The state file keeps the cleanup, and runs refuse to start apps until it succeeds (AXX-E0415).",
		"Fix what made it fail (its output is above), then run `axx down`: it runs the cleanup again.")
	add("AXX-E0412", u, "No active tags",
		"Active startup is on with `onNoTags: error`, and the selected scenarios carry no tags, so axx cannot tell which apps to start.",
		"Tag the scenarios, set `active.onNoTags: fallback` (start everything), or disable active startup.")
	add("AXX-E0413", env, "Run state file error",
		"`.axx/run/state.json` (used by `axx up`/`axx down` to find running apps) could not be read or written.",
		"Check permissions on `.axx/`; deleting the stale file is safe when no apps are running.")
	add("AXX-E0414", exitcode.Interrupted, "Startup interrupted",
		"Starting apps was interrupted (Ctrl-C). Every app that had started was stopped and cleaned up.",
		"Nothing to fix; re-run when ready.")
	add("AXX-E0415", env, "Earlier run not cleaned up",
		"The state file records apps an earlier run left behind, and they could not be cleaned up before this run: a cleanup failed again, or what is left sits beside apps a run still going (or `axx up`) owns. Their data (containers, volumes, recorded requests) would be what this run starts from, so no app is started. A killed run's leftovers alone are cleaned up by the next run.",
		"Run `axx down`: it stops what is left and runs the cleanup again. `axx doctor` and the MCP `env` status list what is left.")

	add("AXX-E0600", u, "Unknown reporter",
		"`--format` or `run.reporters` names a reporter that does not exist.",
		"Use one of: pretty, progress, compact, junit, messages, cucumber-json, html, agent, teamcity.")

	lint := exitcode.Undefined
	add("AXX-E0800", u, "No lint rules",
		"`axx.yaml` has a `lint` section, but it (with its includes) defines no rules, so `axx lint` would check nothing.",
		"Add rules under `lint.rules`, or include a rules file with `lint.include`. Remove the `lint` section if the project has no isolation rules.")
	add("AXX-E0801", u, "Lint include not found",
		"A file named in `lint.include` (or in an included file's `include`) does not exist. Includes resolve relative to the file that lists them.",
		"Fix the path. If the file is generated (`axx-lint.generated.yaml`), run `axx fixtures generate` first.")
	add("AXX-E0802", u, "Lint include cycle",
		"A lint rules file is included more than once, for example two files that include each other. Every file may be loaded once.",
		"Remove the repeated include.")
	add("AXX-E0803", u, "Invalid lint include",
		"An included lint rules file is not valid YAML, has keys the lint section does not allow, or has a `config:` block (only the including file configures lint).",
		"Keep only `rules:` and `include:` in included files, in the same format as the `lint` section of `axx.yaml`. The message lists each problem with its line.")
	add("AXX-E0804", u, "Invalid lint configuration",
		"A lint rule cannot be used: a missing `regex` (or `jsonPath` for `type: jsonpath`), a regex that is not valid Java syntax or has no capture group, an invalid glob or JSONPath, no `filePatterns`, or a `config.baseDir` that is not a directory.",
		"Fix the reported key. Regexes use Java syntax and extract their first participating capture group, e.g. `id:\\s*\"([^\"]+)\"`.")
	add("AXX-E0805", u, "Invalid lint option",
		"`axx lint --format` names an unknown format, `--mode` is not `error` or `warn`, or an output file cannot be written.",
		"Use `--format human|json|junit|sarif|github` (optionally `NAME:FILE`) and `--mode error|warn`.")
	add("AXX-E0810", lint, "File cannot be evaluated",
		"A file selected by a `type: jsonpath` rule is not valid JSON (or the JSONPath cannot be evaluated over it), so its values cannot be checked. This fails the rule even in warn mode.",
		"Fix the JSON at the reported line, or narrow the rule's `filePatterns`/`excludePatterns` so it only selects JSON files.")
	add("AXX-E0811", lint, "File cannot be read",
		"A file selected by a lint rule could not be read (permissions, or it disappeared during the run).",
		"Check the file's permissions, or exclude it with `excludePatterns`.")
	add("AXX-E0812", exitcode.OK, "File skipped: too large",
		"A file selected by a lint rule is larger than `lint.config.maxFileSize` (default 5 MB) and was not checked. This is a warning.",
		"Narrow the rule's `filePatterns`, or raise `lint.config.maxFileSize`.")
	add("AXX-E0820", lint, "Duplicate test data",
		"A value that a lint rule requires to be unique occurs more than once: anywhere (`global-unique`), within one file (`file-unique`), or in more than one file (`cross-file-unique`). Scenarios running in parallel against shared infrastructure collide on such values.",
		"Give each occurrence its own value (for example prefix ids with the scenario or file name). If the value is intentionally shared, add it to the rule's `ignoreValues`; if the rule is too strict, change its `validation`. `mode: warn` reports without failing.")
	add("AXX-E0821", exitcode.OK, "Lint rule finds no values",
		"A lint rule scanned files but its regex (or jsonPath) extracted no value from any of them, so it checks nothing and reports ok. Most often the pattern misses the files' format, like a regex anchored at the start of the line (`^\\s*reference:`) for YAML list items, whose lines start with `- `. This is a warning.",
		"Test the pattern against a line of one of the files and fix it (`^\\s*-?\\s*reference:\\s*\"?([^\"\\s]+)` allows the dash), or narrow `filePatterns` to the files that hold the values.")
	add("AXX-E0830", exitcode.OK, "SQL ordinal addresses a missing entry",
		"A step addresses the Nth selection (or trigger) of the scenario, but fewer than N were retrieved (or created) before it, counting every database service. The step always fails at runtime. This is a warning.",
		"Retrieve the selection before asserting on it, or use the ordinal of an earlier retrieval. Selections and triggers are numbered in the order the scenario creates them.")
	add("AXX-E0831", exitcode.OK, "SQL ordinal label does not match",
		"A retrieval (or trigger) step is labelled with an ordinal it cannot have: selections and triggers are appended in the order they are created, and the ordinal in a creating step is only a label. Later steps that use the label address another entry. This is a warning.",
		"Number retrievals in the order they happen (1st, 2nd, 3rd ...), per database service, and address them by that number.")
	add("AXX-E0832", exitcode.OK, "REST ordinal addresses a missing request",
		"A step adds the Nth request of a REST service before the (N-1)th was added, or addresses a request (or its response) that no step added before it: a service's requests are numbered in the order they are added. The step always fails at runtime. This is a warning.",
		"Add requests in order (`a GET request to ...`, then `a 2nd ordered POST request to ...`), per service, and address them by that number.")
	add("AXX-E0833", exitcode.OK, "REST request added twice",
		"A step adds a request of a REST service that an earlier step added: without an ordinal, `a GET request to ...` is the service's 1st request. The step fails at runtime (Method already set). This is a warning.",
		"Add the next request with the ordered form, like `a 2nd ordered GET request to ...`.")
	add("AXX-E0836", exitcode.OK, "Service used before it is registered",
		"A REST, SQL, MongoDB or Kafka step comes before the scenario registers any service of its pack: the step finds no service and fails at runtime (\"No database services set\"). This is a warning; it is not checked in a project that loads packs of its own, which may register services out of sight.",
		"Register the service first, in the Background for every scenario: `the <name> service with the following properties:` (REST), `a(n) <name> database with the following properties:` (SQL), `a(n) <name> mongo database with the following properties:` (MongoDB) or `the <name> kafka service with the following properties:` (Kafka).")
	add("AXX-E0835", exitcode.OK, "Stale selection",
		"A step without an ordinal (`the selection has 1 row`, `the 1st document for the selection …`) checks a scenario's first selection, but a later selection was retrieved before it and no step checks that one. The step reads as if it checked the latest, while it asserts on what came back before; the later retrieval checks nothing (or only that its rows came, for a polling one). This is a warning.",
		"Name the selection the step means, like `the 2nd selection has 1 row`: selections are numbered in the order the scenario retrieves them.")
	add("AXX-E0834", exitcode.OK, "REST payload value in single quotes",
		"A row of `the request payload properties are:` has a value in single quotes, like `'{\"name\":\"Ada\"}'`. In a table, single quotes are part of the value, so the property is set to that text, quotes and all, not to the JSON or the string inside (single quotes do quote a value in a step's own text). The request then usually fails for a reason the scenario did not mean to test. This is a warning.",
		"Write JSON without quotes (`{\"name\":\"Ada\"}`) and a string in double quotes (`\"10115\"`) or bare.")

	f := exitcode.Failed
	add("AXX-E0900", u, "Invalid fixtures configuration",
		"The fixtures section of axx.yaml cannot be used: a `sources` root does not exist, lies inside `baseDir` or contains it, a glob is malformed, a conformance rule names an unknown family, or two families or expression functions claim one name.",
		"Fix the fixtures section of axx.yaml; sources roots are relative to `fixtures.baseDir` and must be disjoint from it.")
	add("AXX-E0901", u, "Invalid fixture spec",
		"A `*.factory.yaml`, `*.fixture.yaml` or `*.prototype.yaml` file is malformed (unknown field, wrong type, empty factory), cannot be bound to a factory, claims a fixture key twice, names an unsupported family, or declares an identity without a path.",
		"The message names the file. `axx schema --kind factory|fixture|prototype` prints the formats; bind a fixture explicitly with `factory: <root-relative-path>`.")
	add("AXX-E0902", f, "Fixture generation failed",
		"Expanding the specs failed: a required field no layer resolves, a value of the wrong type, an unknown field, a schema oracle rejecting the output, an identity collision, a `$ref` that resolves to nothing, or an expression that cannot be evaluated.",
		"Fix the factory sources (defaults:, prototype, fixture data) as the message says, then run `axx fixtures generate`.")
	add("AXX-E0903", f, "Hand-edited managed file",
		"`axx fixtures generate` refused to overwrite managed files whose content no longer matches the sha256 in axx-fixtures.manifest.yaml (or the pairings lock was edited).",
		"Lift the change into the factory spec (or re-adopt the file), or revert the file. There is no force option that discards edits.")
	add("AXX-E0904", f, "Fixture check failed",
		"`axx fixtures check` found a managed file that differs from what its factory generates (drift), a missing ignored output, or a manifest that does not list exactly what the factories produce.",
		"Edit the factory sources, not the generated files, and run `axx fixtures generate`; commit the result.")
	add("AXX-E0905", f, "Adoption refused",
		"`axx fixtures adopt` refused: the files are already managed, the factory exists, fixture keys collide, the glob matches nothing, the family cannot adopt, or regenerating from the adopted spec would change a file's data. Nothing was written.",
		"Fix the cause the message names and run adopt again; `--dry-run` verifies without writing.")
	add("AXX-E0906", env, "Version control command failed",
		"`axx fixtures untrack` could not run `git ls-files` or `git rm --cached` (the tool is missing, the directory is not a work tree, or it timed out).",
		"Run it inside the repository that holds the fixtures, with git on PATH.")
	add("AXX-E0907", u, "Fixture schema unusable",
		"A governing schema (Avro .avsc, JSON Schema, OpenAPI component, XSD, .proto or descriptor set, SQL DDL) cannot be read or compiled, or the reference form is not supported (classpath: and class: refs need a JVM).",
		"Check the path: factory.schema is relative to the factory file, a conformance rule's schemaRef to fixtures.baseDir.")
	add("AXX-E0908", f, "Fixture file I/O error",
		"A fixture, spec, manifest or output file could not be read or written.",
		"Check that the path exists and that its permissions allow reading and writing.")
	add("AXX-E0909", u, "Invalid adopt options",
		"`axx fixtures adopt` needs either --schema, --files and --factory, or --into and --files.",
		"Run `axx fixtures adopt --help`.")
	add("AXX-E0910", u, "Nothing to explain",
		"`axx fixtures explain` was given a file the fixture factory does not generate (and no fixture's `*.fixture.yaml`), or a path that holds no single value in it: a path it has no value at, an object rather than one of its fields, or a dataset path that is not `<table>[<row>].<column>`.",
		"Give a file listed in axx-fixtures.manifest.yaml (or its `*.fixture.yaml`) and the dotted path of one value in it, like `recipient.postcode` or `items[0].sku`.")
}
