<!-- SPDX-License-Identifier: Apache-2.0 -->
# axx for VS Code

Editor support for [Axx](https://axx.nimbusxr.us) acceptance tests. Axx's Gherkin steps live in
Go code inside the `axx` binary, so an editor can't find them on its own. This extension runs the
Axx language server, `axx lsp`, which knows every step your project loads, including your custom
packs.

In `.feature` files you get:

- **Diagnostics:** undefined and ambiguous steps (with suggestions), Gherkin syntax errors, and
  steps that need a data table but don't have one.
- **Completion:** step text, with placeholders to fill in.
- **Hover:** the documentation of the step under the cursor.
- **Go to Definition:** the Go code that defines a step, or a page about it.
- **Files:** the files steps name (seeds, payloads, schemas, OpenAPI documents, a log's `file://`
  url), in steps and table cells, are links: Ctrl/Cmd+click one to open it. Their paths complete
  as you type, and a file a step reads that does not exist is flagged (not a log's, which may
  appear only during the run).
- **Semantic highlighting:** the parameters inside each step.
- **Syntax highlighting:** keywords, tags, comments, data tables, doc strings, quoted strings and
  `<outline placeholders>`. This part is built in and works without Axx installed.
- **Run and debug:** run or debug a feature, a rule, a scenario or a single Examples row from the
  gutter or the Testing view, with results for every step. Debug stops at breakpoints on steps and
  where a web scenario fails, in Playwright's Inspector, and, with the Go extension, at breakpoints
  in step code.
- **Watch:** run web scenarios with their browsers in windows on your desktop as they go, slowed
  down, one scenario at a time, and open the Playwright traces and videos scenarios keep.

## Requirements

Axx must be installed and on your `PATH`, in a version that has the `axx lsp` command and the
`teamcity` report format (check with `axx lsp --help` and `axx run --help`). See
[Install Axx](https://axx.nimbusxr.us/guides/install/). If it's installed somewhere else, set
`axx.path`.

The language server starts when you open a `.feature` file, in the workspace folder that holds it,
and finds the Axx project (`axx.yaml`) from there. In a multi-root workspace, each folder gets its
own server.

## Run and debug scenarios

Every feature file with an `axx.yaml` in its directory or above it is listed in the **Testing**
view, as feature > rule > scenario > Examples row. The editor gutter has run and debug buttons on
the same lines, and **Test: Run Test at Cursor** runs the scenario the cursor is in. The tree
follows your edits. It reads English Gherkin keywords.

A run starts `axx run --format teamcity <file>:<line>...` in the directory of the nearest
`axx.yaml`, one `axx run` per project. As the scenarios run, their steps appear under them with
their results:

- A failed step shows its message at the step's line. When a check compares values, **Peek**
  shows the expected and actual values as a diff.
- A step's logs and attachments (a response body, for example) are in the test output.
- Everything else Axx prints, such as the summary and app start-up messages, is in the test output
  too.

**Cancel** stops Axx the way Ctrl+C does: Axx stops its apps and runs their cleanups. If Axx is
still running 20 seconds later, the extension kills it and everything it started.

**Debug** stops at breakpoints in step code: your custom packs, and Axx's own steps. It runs
`axx run --debug-steps`, which builds Axx with debug information and starts it under Delve, and
then attaches VS Code's Go debugger to it. It needs:

- the [Go extension](https://marketplace.visualstudio.com/items?itemName=golang.go) (`golang.go`).
  Without it, **Debug** runs the scenarios without `--debug-steps`, so it doesn't stop in step
  code, but it still pauses web scenarios (below). It says so once per window, and offers to
  install the extension.
Axx builds its debug copy and Delve itself, with the Go toolchain it prepares packs with.

**Debug** also pauses web scenarios at breakpoints on steps and where they fail (see
[Watch the browsers](#watch-the-browsers)).

To debug an app instead (your service under test), start it from your IDE and run Axx with
`--attach`, or use `axx run --debug`; `axx ide vscode` writes the launch configurations for that.

### Watch the browsers

For scenarios that use Axx's `web-core` pack. Every `axx run` the extension starts sets
`AXX_IDE=vscode`, so the web pack tells the extension what its scenarios keep.

- The **Watch** profile, next to Run and Debug (the menu next to the run button in the Testing
  view, or **Execute Using Profile** on a gutter button), runs with `--workers 1 --watch
  --slowdown <n>ms`: one scenario at a time, its browsers and devices in windows on your desktop
  (an iOS simulator in Device Hub, an Android emulator in its own), slowed down by
  `axx.watch.slowdown`. The windows are real browsers and devices: click in them, and open
  DevTools to look at a page, its console and its network. Watch only shows what happens: it
  does not pause.
- **Debug** pauses a web scenario in Playwright's Inspector, before a step with a breakpoint and
  where the scenario fails. Click in the gutter of a step's line to set a breakpoint, then run
  with **Debug**: the run pauses before that step, and the Inspector opens next to the browser,
  where you resume, step through the actions, pick an element or record. A failed scenario pauses
  the same way, at the step that failed. Debug adds `--workers 1 --set
  packs.web-core.pauseOnFailure=true`, so that the pauses come one at a time, and `--pause-at
  <file>:<line>` for each enabled breakpoint on a step in the feature files the run runs.
  Breakpoints in step code still stop in the Go debugger, with the Go extension. **Run** and
  **Watch** ignore breakpoints on steps.
- **Only a step's line pauses:** a line that starts with `Given`, `When`, `Then`, `And`, `But` or
  `*`, in a background or a scenario (outlines included), outside doc strings. VS Code lets you set
  a breakpoint on any line of a feature file; Debug ignores one on another line (a scenario's
  title, a tag, a comment, a table row or a doc string's line), and says so in the run's test
  output, at the breakpoint's line.
- **While a scenario is paused** (at a breakpoint, or where it failed), the element of the
  step under the cursor is highlighted in the page, and the status bar says what the step finds,
  or why it would fail. **Run in paused scenario**, above each step, or **axx: Run Step in Paused
  Scenario** in the editor's context menu, runs the step with the rows of its table in the paused
  scenario and says whether it passed. **axx: Insert Recorded Steps** (the editor's context menu)
  inserts the steps recorded in the Inspector below the cursor's line, indented as the step
  there: from the run while it runs, then from the recording file it named.
- A trace or video a scenario keeps is linked in the scenario's test output. Right-click the
  scenario in the Testing view, or its gutter button, for **Open Trace** or **Play Video**.
- A trace opens in a tab, in Playwright's trace viewer. The trace viewer loads traces by URL, so
  the extension serves them over HTTP on `127.0.0.1`, on a random port, to any origin (CORS `*`):
  only the files runs announced, each under a random path, until VS Code exits. A run announces
  the folder of the trace viewer's files (`dir`, from the Playwright driver Axx downloads), and
  the extension serves the viewer too, under a path of its own, and opens the trace there: after
  the run too, offline. It remembers the last folder announced, for the traces of earlier runs.
  Without a folder, it shows the file in your file manager, to drop on
  [trace.playwright.dev](https://trace.playwright.dev). A video plays in a tab.

## Disable the Cucumber extension for Axx projects

The official Cucumber extension, and other Gherkin extensions that look for step definitions,
search your code for glue code. Axx steps have none, so they mark every Axx step as undefined.
Disable them in Axx workspaces: in the Extensions view, open the extension's gear menu and choose
**Disable (Workspace)**.

If another extension still claims `.feature` files, map them to this one in the workspace
settings:

```json
{
  "files.associations": { "*.feature": "feature" }
}
```

## Settings

| Setting | Default | Description |
|---|---|---|
| `axx.path` | `axx` | The `axx` executable. A bare name is looked up on `PATH`; a relative path such as `./bin/axx` is resolved against the workspace folder; `~` is your home directory. |
| `axx.trace.server` | `off` | Log the messages between VS Code and the server to the **axx** output channel (`messages` or `verbose`). |
| `axx.watch.slowdown` | `300` | How long **Watch** runs wait after every browser action, in milliseconds. `0` adds no wait. |

Changing `axx.path` restarts the server.

## Commands

**axx: Restart language server** restarts every Axx language server. The server starts with the
project's packs, so run it after you change a pack of your own or `axx-packs.yaml`, or after you
upgrade Axx.

**axx: Run Step in Paused Scenario** and **axx: Insert Recorded Steps** work in feature files
while a scenario is paused (see [Watch the browsers](#watch-the-browsers)).

## Workspace trust

`axx lsp` and `axx run` can build and run your project's custom packs, so the language server
starts, and tests are listed and run, only in trusted workspaces. Syntax highlighting works
everywhere.

## Troubleshooting

The **axx** output channel (**View > Output**, then choose **axx**) shows the commands the
extension started, the server's own log, and why a server failed to start. Set its log level to
**Debug** (the gear in the Output view) to also see each test result.

## Development

Use the Node.js version from the repository's `.mise.toml`:

```sh
mise exec -- npm ci
mise exec -- npm run build      # dist/extension.js (npm run watch rebuilds on change)
mise exec -- npm run typecheck  # tsc --noEmit
mise exec -- npm test           # unit, grammar and manifest tests
mise exec -- npm run package    # axx-<version>.vsix
```

To try it, run `code --extensionDevelopmentPath="$PWD"` from this directory, or install the
`.vsix` with `code --install-extension axx-<version>.vsix`.

## License

Apache-2.0. See the `LICENSE` file.
