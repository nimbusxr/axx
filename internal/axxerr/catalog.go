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
		"`axx run --debug-steps` runs axx under Delve, Go's debugger, so an IDE can stop at breakpoints in step code. `dlv` was not found on PATH or in Go's bin directories.",
		"Install it with `go install github.com/go-delve/delve/cmd/dlv@latest`.")
	add("AXX-E0306", env, "Delve did not start",
		"`axx run --debug-steps` started Delve, but it exited or never began listening for a debugger. Its output is in the message.",
		"Check that the port is free (`--debug-steps=<port>` picks another) and that Delve works on this machine (`dlv version`).")
	add("AXX-E0310", u, "Unknown step id",
		"`axx steps show` was given an id that no loaded pack defines.",
		"List the ids with `axx steps`, or search by words with `axx steps search <words>`.")

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
		"The app's `cleanup` command exited non-zero. The run still completed; this is reported so leaks are visible.",
		"Run the cleanup command by hand to see its output.")
	add("AXX-E0412", u, "No active tags",
		"Active startup is on with `onNoTags: error`, and the selected scenarios carry no tags, so axx cannot tell which apps to start.",
		"Tag the scenarios, set `active.onNoTags: fallback` (start everything), or disable active startup.")
	add("AXX-E0413", env, "Run state file error",
		"`.axx/run/state.json` (used by `axx up`/`axx down` to find running apps) could not be read or written.",
		"Check permissions on `.axx/`; deleting the stale file is safe when no apps are running.")
	add("AXX-E0414", exitcode.Interrupted, "Startup interrupted",
		"Starting apps was interrupted (Ctrl-C). Every app that had started was stopped and cleaned up.",
		"Nothing to fix; re-run when ready.")

	add("AXX-E0600", u, "Unknown reporter",
		"`--format` or `run.reporters` names a reporter that does not exist.",
		"Use one of: pretty, progress, compact, junit, messages, cucumber-json, html, agent.")

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
	add("AXX-E0830", exitcode.OK, "SQL ordinal addresses a missing entry",
		"A step addresses the Nth selection (or trigger) of the scenario, but fewer than N were retrieved (or created) before it, counting every database service. The step always fails at runtime. This is a warning.",
		"Retrieve the selection before asserting on it, or use the ordinal of an earlier retrieval. Selections and triggers are numbered in the order the scenario creates them.")
	add("AXX-E0831", exitcode.OK, "SQL ordinal label does not match",
		"A retrieval (or trigger) step is labelled with an ordinal it cannot have: selections and triggers are appended in the order they are created, and the ordinal in a creating step is only a label. Later steps that use the label address another entry. This is a warning.",
		"Number retrievals in the order they happen (1st, 2nd, 3rd ...), per database service, and address them by that number.")

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
}
