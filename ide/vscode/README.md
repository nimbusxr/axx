<!-- SPDX-License-Identifier: Apache-2.0 -->
# axx for VS Code

Editor support for [axx](https://axx.nimbusxr.us) acceptance tests. axx's Gherkin steps live in
Go code inside the `axx` binary, so an editor can't find them on its own. This extension runs the
axx language server, `axx lsp`, which knows every step your project loads, including your custom
packs.

In `.feature` files you get:

- **Diagnostics:** undefined and ambiguous steps (with suggestions), Gherkin syntax errors, and
  steps that need a data table but don't have one.
- **Completion:** step text, with placeholders to fill in.
- **Hover:** the documentation of the step under the cursor.
- **Go to Definition:** the Go code that defines a step, or a page about it.
- **Files:** the files steps name (seeds, payloads, schemas, OpenAPI documents, a log's `file://`
  url), in steps and table cells, are links: Ctrl/Cmd+click one to open it. Their paths complete
  as you type, and a file that does not exist is flagged.
- **Semantic highlighting:** the parameters inside each step.
- **Syntax highlighting:** keywords, tags, comments, data tables, doc strings, quoted strings and
  `<outline placeholders>`. This part is built in and works without axx installed.
- **Run and debug:** run or debug a feature, a rule, a scenario or a single Examples row from the
  gutter or the Testing view, with results for every step.

## Requirements

axx must be installed and on your `PATH`, in a version that has the `axx lsp` command and the
`teamcity` report format (check with `axx lsp --help` and `axx run --help`). See
[Install axx](https://axx.nimbusxr.us/guides/install/). If it's installed somewhere else, set
`axx.path`.

The language server starts when you open a `.feature` file, in the workspace folder that holds it,
and finds the axx project (`axx.yaml`) from there. In a multi-root workspace, each folder gets its
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
- Everything else axx prints, such as the summary and app start-up messages, is in the test output
  too.

**Cancel** stops axx the way Ctrl+C does: axx stops its apps and runs their cleanups. If axx is
still running 20 seconds later, the extension kills it and everything it started.

**Debug** stops at breakpoints in step code: your custom packs, and axx's own steps. It runs
`axx run --debug-steps`, which builds axx with debug information and starts it under Delve, and
then attaches VS Code's Go debugger to it. It needs:

- the [Go extension](https://marketplace.visualstudio.com/items?itemName=golang.go) (`golang.go`),
- Delve (`dlv`) and Go, which axx uses to build the debug copy.

To debug an app instead (your service under test), start it from your IDE and run axx with
`--attach`, or use `axx run --debug`; `axx ide vscode` writes the launch configurations for that.

## Disable the Cucumber extension for axx projects

The official Cucumber extension, and other Gherkin extensions that look for step definitions,
search your code for glue code. axx steps have none, so they mark every axx step as undefined.
Disable them in axx workspaces: in the Extensions view, open the extension's gear menu and choose
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

Changing `axx.path` restarts the server.

## Commands

**axx: Restart language server** restarts every axx language server. The server starts with the
project's packs, so run it after you change a pack of your own or `axx-packs.yaml`, or after you
upgrade axx.

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
