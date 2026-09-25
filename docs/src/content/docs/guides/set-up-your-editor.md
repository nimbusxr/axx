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
The IntelliJ plugin and the VS Code extension are published with the first release. Until then, build them from the repository: `./gradlew buildPlugin` in `ide/intellij`, `npm ci && npm run package` in `ide/vscode`.
:::

## IntelliJ IDEA

Needs IntelliJ IDEA 2025.3 or later (or another JetBrains IDE of that version).

1. Install the **axx** plugin: *Settings | Plugins | Marketplace*, or *Install Plugin from Disk* with the zip from `ide/intellij/build/distributions`.
2. Optionally install the **Gherkin** plugin for keyword highlighting. It looks for step definitions in code and would mark every Axx step as undefined, so the axx plugin turns that inspection off in Axx projects.
3. Open a `.feature` file. The server appears in the **Language Services** widget in the status bar, where you can restart it.

If `axx` is not on your `PATH`, set **axx executable** in *Settings | Tools | axx*.

## VS Code

1. Install the **axx** extension: from the Marketplace, or *Extensions: Install from VSIX* with the file `npm run package` writes.
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

*Debug* instead of *Run* stops at breakpoints in the Go code of the steps ([Stop in step code](/guides/debug-failures/#stop-in-step-code)).

## After changing packs

The server starts with the project's packs. After you change a pack of your own or `axx-packs.yaml`, restart the server: from the Language Services widget in IntelliJ, with **axx: Restart language server** in VS Code, or by restarting the editor.
