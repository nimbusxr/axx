<!-- SPDX-License-Identifier: Apache-2.0 -->
# axx: IntelliJ plugin

The IDE companion for axx. It does three things:

- **Feature files.** axx's steps are defined in Go, inside the `axx` binary, so the IDE cannot
  find them on its own: every step shows as undefined, with no completion or docs. The plugin runs
  the axx language server (`axx lsp`) for `.feature` files, which knows every step. See
  [Feature files](#feature-files).
- **Running scenarios.** Run or debug a feature, a scenario or one example from the gutter, and
  see the results in the IDE's test runner. See [Running scenarios](#running-scenarios).
- **Debugging apps.** Run `axx run --debug` from the IDE, and every app that axx starts in debug
  mode gets a debugger from the IDE, with no manual steps. When the run ends, the debuggers the
  plugin started stop too. See [Debugging](#debugging).

## Install

Works in IntelliJ IDEA 2025.3 (build 253) or later, and in other IntelliJ-based IDEs of those
versions.

- Feature-file support uses the IntelliJ Platform's LSP API. Every IntelliJ IDEA has it since the
  unified 2025.3 release, with or without a subscription, and so do most other JetBrains IDEs,
  such as WebStorm, PyCharm and GoLand. In an IDE without it, the plugin still loads; running and
  debugging work.
- The gutter icons and running from a feature file or directory use the Gherkin plugin
  (JetBrains Marketplace). Without it, create axx run configurations by hand.
- Stopping at breakpoints in step code needs a Go debugger: GoLand, or IntelliJ IDEA with the Go
  plugin.
- Debugging apps depends only on the core platform, so it works in any IntelliJ-based IDE.

Then:

- **JetBrains Marketplace:** *Settings | Plugins | Marketplace*, search for **axx**.
- **From disk:** build it (`./gradlew buildPlugin`, see below) or download the zip, then choose
  *Settings | Plugins | ⚙ | Install Plugin from Disk…* and pick
  `build/distributions/axx-intellij-<version>.zip`.

## Feature files

When you open a `.feature` file in an axx project, the plugin starts `axx lsp` in the project
directory, over stdio. One server serves the whole project. It gives feature files:

- **Completion** of step text, with placeholders for the step's parameters. Suggestions match all
  of the step text typed after the keyword, not just its last word.
- **Documentation** of the step under the mouse (or *View | Quick Documentation*).
- **Go to Declaration** (*Navigate | Declaration or Usages*, or Ctrl/Cmd+click) on a step opens
  the Go source that defines it, or, when that source is not on your machine, a page about the
  step that the server generates.
- **Files** steps name: a seed, a payload, a schema, an OpenAPI document or a log's `file://` url,
  in a step or in a table cell, is shown as a link, and Go to Declaration on it opens the file.
  Its path completes as you type, and a file that does not exist is flagged.
- **Highlighting** of step parameter values and of `<placeholders>` in Scenario Outline steps, in
  the Gherkin plugin's *Step parameter* and *Scenario outline parameter* colors (*Settings |
  Editor | Color Scheme | Cucumber*).
- **Diagnostics:** undefined, ambiguous and misused steps (such as a step missing its table), with
  suggestions, and Gherkin syntax errors.

A project is an axx project when `axx lsp`, started in the project directory, finds an axx
config file (`axx.yaml` or `axx.yml`): in the project directory, in a directory above it (up to
the repository root, the directory with `.git`), or in a subdirectory at most three levels down
(hidden directories and `node_modules`, `vendor`, `build`, `dist`, `target`, `out` and `bin` are
skipped). Other projects never start the server.

### The axx executable

*Settings | Tools | axx* has one setting, **axx executable**:

- A command name (the default, `axx`) is looked up on `PATH`. On macOS the IDE uses your login
  shell's `PATH`, even when you start it from the Dock. When the default `axx` is not on `PATH`,
  the plugin also looks where axx's installers put it, in this order: `$GOBIN`, the `bin`
  directory of the first `$GOPATH` entry (`~/go/bin` by default), `~/.local/bin`,
  `/opt/homebrew/bin` and `/usr/local/bin`. On Windows it looks for `axx.exe` in `$GOBIN`, the
  `$GOPATH` `bin` directory, `%LOCALAPPDATA%\axx\bin` and `~\.local\bin`.
- A path runs that binary. A relative path starts at the project directory, such as `bin/axx` for
  a project's own build of axx.

Changing it restarts the server. The setting is per machine: it is not synced between IDEs.

The server shows up in the **Language Services** widget in the status bar, where you can restart
it or open its settings. If it cannot start, for example because axx is not installed, the widget
says why and lists where the plugin looked. To log the traffic between the IDE and the server, add
`#com.intellij.platform.lsp` in *Help | Diagnostic Tools | Debug Log Settings*; it goes to
`idea.log`.

### With the Gherkin plugin

The Gherkin plugin (JetBrains Marketplace, also installed by Cucumber for Java) highlights Gherkin
keywords, but it looks for step definitions in your code, so it marks every axx step as an
undefined step reference. In axx projects, this plugin suppresses that inspection
(`CucumberUndefinedStep`); the axx server reports the steps axx does not define instead.

The Gherkin plugin also puts its own reference on every step, which finds no definition for an axx
step, so Go to Declaration would say "Cannot find declaration to go to". In axx projects, this
plugin answers Go to Declaration on a step first, with the definition the axx server gives, and
leaves it to the Gherkin plugin only when the server has none.

The Gherkin plugin's other inspections, and all of its behavior in other projects, stay as they
are. Without the Gherkin plugin, feature files get the language server features all the same.

## Running scenarios

In an axx project, feature files get run icons in the gutter: on the Feature line, each Rule,
Scenario and Scenario Outline line, and each Examples row. Each icon runs or debugs what its line
selects, and after a run it shows that line's result.

| Where | What runs |
|---|---|
| Feature line | the whole file (`axx run features/x.feature`) |
| Rule line | the Rule's scenarios, by their lines |
| Scenario or Scenario Outline line, or a line inside one | that scenario, with all of an outline's examples (`features/x.feature:12`) |
| Examples row | that one example (`features/x.feature:25`) |
| Feature file or directory (project view, editor tab) | that path |
| A directory above the suite, such as the project root | the whole suite, as `run.paths` in `axx.yaml` selects it |

The same choices are in the context menu (*Run*, *Debug*) of the editor and the project view.
Each creates an **axx** run configuration, which you can also add by hand in *Run | Edit
Configurations*:

- **Targets:** feature files or directories, each optionally with `:line` (a Scenario, Scenario
  Outline or Examples row line). Empty runs the suite's `run.paths`.
- **Arguments:** more `axx run` arguments, such as `--tags "@smoke"`.
- **Working directory:** where axx runs, and where the targets start. Empty: the directory of the
  `axx.yaml` above the first target. axx finds `axx.yaml` upward from there.

The configuration runs `axx run --format teamcity <targets> <arguments>` (with the axx executable
from *Settings | Tools | axx*). The test runner shows each feature, its scenarios (one per outline
example) and their steps as tests, with each step's log. A failed assertion has *Click to see
difference* with the expected and actual values; skipped and pending steps show as ignored.
Double-click a node, or use *Jump to Source*, to go to its line. *Rerun Failed Tests* runs
`axx run` again with the lines of the scenarios that failed.

### Debugging step code

*Debug* on an axx run configuration runs `axx run --debug-steps`: axx runs under Delve and waits
for a Go debugger, so breakpoints in step code stop. When axx asks for
the debugger, the plugin starts the `Debugger: axx-steps` run configuration, a Go Remote
configuration for `127.0.0.1:2345`, as it starts app debuggers (see
[What the plugin does with a request](#what-the-plugin-does-with-a-request)). If there is no such
configuration, the plugin creates it, so `axx ide intellij` is not needed first. To use another
Delve port, add `--debug-steps=<port>` to the configuration's arguments and set the same port in
`Debugger: axx-steps`.

Without a Go debugger (IntelliJ IDEA without the Go plugin), *Debug* runs the scenarios without
`--debug-steps`, and says so in a notification and at the top of the console.

## Debugging

The plugin itself stays passive. It watches the console of every process in the project,
whichever executor started it (Run or Debug). When axx prints a debugger request, the plugin
starts the matching `Debugger: <app>` run configuration with the Debug executor. Output without
a request starts nothing.

### Run configurations

`axx ide intellij` generates these run configurations in `.run/`:

| Configuration | What it is |
|---|---|
| `axx: debug` | A Shell Script configuration that runs `axx run --debug`, usually with **Run**. The plugin watches its console. |
| `Debugger: <app>` | One per app that has a debugger configured. The plugin starts it when axx asks. |
| `axx: debug all` | A compound that starts every `Debugger: <app>` together with `axx: debug`. |
| `axx: debug steps` | Runs `axx run --debug-steps`: axx runs under Delve so the IDE can stop at breakpoints in step code, in axx's packs or your own. |
| `Debugger: axx-steps` | A Go Remote configuration on `127.0.0.1:2345` (GoLand, or the Go plugin). The plugin starts it when `axx run --debug-steps` asks. |

With `axx: debug all`, the debuggers are already starting when axx asks for them. The plugin
never starts a second instance of a `Debugger: <app>` configuration that is starting or running.
Running `axx: debug` alone works too: the plugin then starts each debugger on demand.

#### Debugger configurations

There is one `Debugger: <app>` configuration for each app under `apps:` in `axx.yaml` that has
a `debug.debugger` section, where `<app>` is the key under `apps:`. The debugger's `mode` decides
how the configuration connects:

- `mode: ide-listens` (the default for `java`) gives a listen-mode configuration. The IDE listens
  on `host:port` and the app connects to it (JDWP `server=n`).
- `mode: app-listens` (delve, `--inspect`, debugpy) gives an attach-mode configuration. The app
  listens and the IDE attaches to it.

The plugin finds configurations **by name only**. It does not care about their type, so you can
also write or edit one by hand. For a Java app with `mode: ide-listens`, the configuration looks
like this:

```xml
<component name="ProjectRunConfigurationManager">
  <configuration default="false" name="Debugger: orders" type="Remote">
    <option name="USE_SOCKET_TRANSPORT" value="true" />
    <option name="SERVER_MODE" value="true" />   <!-- listen; "false" attaches -->
    <option name="SHMEM_ADDRESS" />
    <option name="HOST" value="localhost" />
    <option name="PORT" value="5006" />
    <option name="AUTO_RESTART" value="false" />
    <method v="2" />
  </configuration>
</component>
```

Other debugger types use the matching IDE configuration type, such as a Node.js, Python or Go
remote debug configuration. Those types need their language plugin, but the axx plugin does
not.

If axx asks for a configuration that does not exist, the plugin logs a warning to `idea.log`
("no such run configuration exists"). Run `axx ide intellij` again to regenerate it.

### The marker protocol

axx prints one request per line to standard output or standard error:

```text
[AXX-IDE] debug-listener-request name=<app> type=<type> host=<host> port=<port>
[AXX-IDE] debug-attach-request name=<app> type=<type> host=<host> port=<port>
```

| Request | When axx prints it | What the plugin does |
|---|---|---|
| `debug-listener-request` | `mode: ide-listens`, before axx starts the app, which then connects to the IDE. | Starts `Debugger: <app>`, a listen-mode configuration. |
| `debug-attach-request` | `mode: app-listens`, once the app listens for a debugger. | Starts `Debugger: <app>`, an attach-mode configuration that connects to `host:port`. |

Parsing rules:

- The marker may follow other text on the same line, such as a timestamp or a log prefix. ANSI
  color codes are ignored.
- After the marker comes the request kind, then `key=value` fields in any order, separated by any
  amount of whitespace.
- `name`, `type`, `host` and `port` (1-65535) are required. A line missing any of them is
  ignored.
- Unknown keys are ignored, so later axx versions can add fields. If a key repeats, the first
  value wins. Tokens without `=` are skipped.
- `type` is informational (`java`, `nodejs`, `python`, `go`, ...). The configuration's own type
  decides how the IDE debugs.

### What the plugin does with a request

- It watches processes started with any executor, except configurations whose name starts with
  `Debugger: `.
- It starts `Debugger: <app>` with the Debug executor only if that configuration is not already
  starting or running, whether the plugin, the `axx: debug all` compound or you started it.
  Right after it asks the IDE to start a debugger, it treats that debugger as starting for up to
  10 seconds, so a repeated request cannot start a second instance.
- A later request starts the debugger again once its earlier session has ended, for example after
  the app restarted.
- When the process that printed the requests exits, the plugin stops the debuggers *it* started,
  using the same action as the Stop button (remote debug sessions detach). Debuggers started by
  the compound or by you keep running.

## Development

Requires JDK 21 (`mise install` at the repository root provides it). This is a standalone Gradle
build: run it from this directory. The first build downloads IntelliJ IDEA 2025.3 and the Gherkin
plugin.

```sh
./gradlew test          # unit tests, and headless IDE tests (run configurations, gutter, navigation)
./gradlew buildPlugin   # build/distributions/axx-intellij-<version>.zip
./gradlew runIde        # a sandbox IDE with the plugin and the Gherkin plugin installed
./gradlew verifyPlugin  # IntelliJ Plugin Verifier against recommended IDE versions
```

Two tests run the plugin in a headless IDE, with the Gherkin plugin, against a real axx, and run
only when `AXX_BIN` points to an axx binary, such as one built with `go build -o bin/axx ./cmd/axx`
at the repository root:

- `AxxLspIntegrationTest` checks the plugin's wiring, `axx lsp` starting for a feature file, Go to
  Declaration on a step, and step completion.
- `AxxRunIntegrationTest` runs an axx run configuration and checks the test tree (features,
  scenarios, examples, steps, a failure with expected and actual values, a skipped step),
  navigation from it, the stored results the gutter shows, and *Rerun Failed Tests*. Its suite
  starts `python3 -m http.server 8000`, so it needs python3 on `PATH` and port 8000 free.

```sh
AXX_BIN=$PWD/../../bin/axx ./gradlew test
```

`platformVersion` in `gradle.properties` is the IDE the plugin builds against, and its major
version (`sinceBuild`) is the lowest supported version. There is no upper bound (`untilBuild`).
`gherkinVersion` is the Gherkin plugin release the plugin compiles against (for its step PSI)
and that `runIde` and the tests install; keep it compatible with `platformVersion`. At run time the
Gherkin plugin is an optional dependency.

The plugin uses the LSP API's names from before 2026.1.4 (`LspServerSupportProvider`,
`ProjectWideLspServerDescriptor`, `LspServerManager`), the only ones build 253 has. 2026.1.4
renamed them (`LspIntegrationProvider`, `ProjectWideLspClientDescriptor`, `LspClientManager`) and
kept the old names as deprecated, so `verifyPlugin` reports deprecated API usages for newer IDEs.
Move to the new names once `sinceBuild` reaches 261.

Releases: release-please keeps a release PR for this plugin; merging it tags `intellij-v<version>`,
and `release.yml` builds, signs and publishes the plugin to the JetBrains Marketplace. That needs the repository secrets `JETBRAINS_MARKETPLACE_TOKEN`,
`JETBRAINS_CERTIFICATE_CHAIN`, `JETBRAINS_PRIVATE_KEY` and, if the key is encrypted,
`JETBRAINS_PRIVATE_KEY_PASSWORD`. Without them, the workflow skips publishing with a warning.
