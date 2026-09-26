---
title: Set up your editor
description: See Axx's steps in feature files as you write them - problems flagged as you type, step completion, docs on hover and highlighted parameters - in IntelliJ IDEA, VS Code or any editor with LSP support.
---

Axx's steps are defined inside the `axx` binary, not in your project, so an editor cannot find them on its own. `axx lsp`, a language server built into the binary, tells the editor about them. The IntelliJ plugin and the VS Code extension start it for you. In feature files you get:

- undefined, ambiguous and misused steps, and Gherkin syntax errors, flagged as you type, with the closest real steps;
- completion of step text, with the step's parameters as placeholders;
- the step's documentation, and the values of its parameters, on hover;
- go to a step's definition: the Go code that defines it when that code is on your machine (your custom packs, or axx built from source), otherwise the step's entry in a reference page;
- the files that steps name: Ctrl/Cmd+click a seed, a payload, a schema, an OpenAPI document or a log's `file://` url to open the file, get its path completed as you type, and a warning when it does not exist (with a pointer to `axx fixtures generate` for a fixture that is not generated yet). These are the steps' `{filepath}` parameters and file properties, and the Examples cells that fill them in; the file is found the way the step finds it, in the `resources` directories, next to `axx.yaml` or at an absolute path. A log's file may appear only during the run, so it is never flagged;
- highlighted parameter values and Scenario Outline `<placeholders>`.

The steps are the ones your project loads, custom packs included ([Choose packs](/guides/use-packs/)). The server finds the project's `axx.yaml` itself, even when it is in a subdirectory of the folder you opened.

:::caution[Pre-release]
The IntelliJ plugin is not listed on the JetBrains Marketplace, and the VS Code extension is not on the Visual Studio Marketplace. Build them from the repository: `./gradlew buildPlugin` in `ide/intellij`, `npm ci && npm run package` in `ide/vscode`.
:::

## IntelliJ IDEA

Needs IntelliJ IDEA 2025.3 or later (or another JetBrains IDE of that version).

1. Install the **axx** plugin: *Settings | Plugins | ⚙ | Install Plugin from Disk* with the zip from `ide/intellij/build/distributions`.
2. Install the **Gherkin** plugin too: it highlights Gherkin keywords, and the axx plugin needs it for the run icons in the gutter, for running a feature file or directory, and for breakpoints on steps. The Gherkin plugin looks for step definitions in code and would mark every Axx step as undefined, so the axx plugin turns that inspection off in Axx projects.
3. Open a `.feature` file. The server appears in the **Language Services** widget in the status bar, where you can restart it.

If `axx` is not on your `PATH`, set **axx executable** in *Settings | Tools | axx*.

## VS Code

1. Install the **axx** extension: *Extensions: Install from VSIX...* with the `.vsix` file `npm run package` writes.
2. Disable the official Cucumber extension in Axx workspaces. It looks for step definitions in code and marks every Axx step as undefined.
3. Open a `.feature` file. The extension highlights Gherkin itself and starts the server once the workspace is trusted.

If `axx` is not on your `PATH`, set `axx.path` to its location. The **axx** output channel shows the server's log.

## Other editors

Any editor with LSP support can run `axx lsp` for `*.feature` files. It talks over stdio and finds the project from its working directory. In Neovim 0.11 or later, which gives `.feature` files the `cucumber` file type:

```lua title="init.lua"
vim.lsp.config('axx', {
  cmd = { 'axx', 'lsp' },
  filetypes = { 'cucumber' },
  root_markers = { 'axx.yaml', '.git' },
})
vim.lsp.enable('axx')
```

## Run and debug scenarios

Both plugins run scenarios from the editor, with the results in the IDE's test view: features, scenarios and steps, each one linked to its line, with expected and actual values for failed assertions.

- **IntelliJ IDEA:** click the run icon in the gutter of a Feature, Rule, Scenario or Scenario Outline line, or of an Examples row, or choose *Run* on a feature file or directory. *Rerun Failed Tests* is in the test view's toolbar.
- **VS Code:** use the run buttons in the gutter or the Testing view.

*Debug* instead of *Run* stops at breakpoints in the Go code of the steps ([Stop in step code](/guides/debug-failures/#stop-in-step-code)). It needs a Go debugger: GoLand or IntelliJ IDEA with the Go plugin, or the Go extension in VS Code. Without one, *Debug* runs the scenarios without stopping in step code; breakpoints on steps still pause.

## Watch the browsers

Scenarios that use the [`web-core` pack](/guides/test-web-apps/) can run with their browsers in windows on your screen, one scenario at a time, slowed down so you can follow what happens.

- **IntelliJ IDEA:** choose *Watch* next to *Run* and *Debug*, in the gutter menu of a Feature, Rule, Scenario or Examples row line, or in the context menu of a feature file or directory. To watch every run of an **axx** run configuration, check **Watch the browsers** in it.
- **VS Code:** run with the **Watch** profile: from the menu next to the run button in the Testing view, or with *Execute Using Profile* on a gutter button.

Watching runs `axx run --workers 1 --set packs.web-core.watch=true --set packs.web-core.slowdown=300ms` ([Watch and debug browsers](/guides/watch-web-browsers/)). Set the slowdown in milliseconds, `0` for none: **Slowdown when watching the browsers** in *Settings | Tools | axx*, or `axx.watch.slowdown` in VS Code.

To stop before a step, click in the gutter of its line to set a breakpoint, then *Debug*. The run pauses before that step, and Playwright's Inspector opens next to the browser: resume the run, step through its actions, pick an element or record new ones. When debugging, a failed scenario also pauses there, at the step that failed. *Debug* adds `--debug-steps --workers 1 --set packs.web-core.pauseOnFailure=true` to the run, and `--pause-at` for each breakpoint, as in `axx run features/<file>.feature --pause-at features/<file>.feature:<line>`. *Watch* ignores breakpoints.

While a scenario is paused, put the cursor on a step to highlight the element it names in the page; the status bar says what the step finds, or why it would fail. To try a step in the paused scenario, run it: *Run Step in Paused Scenario* in the editor's context menu (Ctrl+Alt+Shift+R in IntelliJ IDEA), or the **Run in paused scenario** link above each step in VS Code. *Insert Recorded Steps*, in the editor's context menu, inserts the steps you recorded in the Inspector below the cursor.

## Open traces and videos

When a scenario keeps a Playwright trace (a failed one does, by default) or a video, the editor adds it to the scenario's test:

- **IntelliJ IDEA:** the scenario's output in the test view ends with an *Open trace* or *Play video* link, and the scenario's context menu has **Open Trace** and **Play Video**.
- **VS Code:** right-click the scenario in the Testing view, or its gutter button, for **Open Trace** and **Play Video**. The scenario's test output has links too.

A trace opens in an editor tab, in Playwright's trace viewer. The viewer comes with the Playwright driver Axx downloads, and the editor serves it with the trace from your machine, on `127.0.0.1`: nothing but the viewer and the files runs named. So traces open after the run too, offline. Without the driver's viewer, the editor shows the file in your file manager: drop it on [trace.playwright.dev](https://trace.playwright.dev). A video plays in an editor tab.

In IntelliJ IDEA without its built-in browser (JCEF), traces open in your default browser, and videos in your video player. In VS Code, **Open in Browser** at the top of each tab opens its page in your default browser.

## After changing packs

The server starts with the project's packs. After you change a pack of your own or `axx-packs.yaml`, restart the server: from the Language Services widget in IntelliJ, with **axx: Restart language server** in VS Code, or by restarting the editor.
