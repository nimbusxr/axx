---
title: Configuration
description: The Axx configuration files - axx.yaml (every section, the apps and lint keys, how files and profiles merge, interpolation) and axx-packs.yaml.
---

An acceptance project has up to two configuration files, side by side: `axx.yaml` for the project and `axx-packs.yaml` for the packs it uses. Both are optional.

## axx.yaml

Without an `axx.yaml`, Axx runs `features/` with defaults. The complete, versioned definition is the JSON Schema:

- online: [`https://axx.nimbusxr.us/schemas/v0/axx.schema.json`](/schemas/v0/axx.schema.json)
- offline: `axx schema` (or `axx schema --out axx.schema.json`)

Add this first line to get completion and validation in editors that use the YAML language server (VS Code, IntelliJ, Neovim):

```yaml
# yaml-language-server: $schema=https://axx.nimbusxr.us/schemas/v0/axx.schema.json
```

### A complete example

```yaml title="axx.yaml"
# yaml-language-server: $schema=https://axx.nimbusxr.us/schemas/v0/axx.schema.json
version: 1

run:
  paths: [features]              # feature files or directories
  tags: "not @wip"               # default tag expression
  workers: auto                  # parallel scenarios: a number or auto (CPUs)
  exclusive: ["@isolated"]       # these run alone, after the parallel phase
  order: defined                 # or random, random:<seed>
  timeouts: {step: 60s, scenario: 5m, hook: 2m}
  reporters: [pretty, {junit: build/axx/junit.xml}]

resources: ["."]                 # where seed, payload and schema paths in steps resolve

properties:                      # ${sys:name}; override with -D name=value
  local.host: localhost

openapi:
  levels:                        # suite-wide OpenAPI validation levels
    validation.request.security.missing: IGNORE

active:
  enabled: false                 # start only the apps the selected scenarios' tags need
  onNoTags: fallback

apps:
  api:
    dir: .
    command: docker compose up --build
    ready:
      http: {url: http://${sys:local.host}:8080/health}
      timeout: 120s
    cleanup: docker compose down -v --remove-orphans

lint: {}                         # axx lint rules
fixtures: {}                     # axx fixtures settings

profiles:                        # overlays: --profile ci or AXX_PROFILE=ci
  ci:
    properties: {local.host: docker}
```

### Sections

| Section | What it configures | Guide |
| --- | --- | --- |
| `version` | the schema version of this file; currently `1` | |
| `run` | paths, default tags, workers, exclusive tags, order, timeouts, default reporters | [Run in parallel](/guides/parallel-runs/), [Reports](/guides/reports/) |
| `resources` | directories that relative file paths in steps resolve against, in order; the directory of `axx.yaml` is searched last | [Configure services](/guides/configure-services/#resource-paths) |
| `properties` | values for `${sys:name}` | [Configure services](/guides/configure-services/#keep-values-out-of-feature-files) |
| `openapi.levels` | default OpenAPI validation levels, by rule key | [Validate against OpenAPI](/guides/validate-openapi/) |
| `active` | tag-based app startup | [Manage the app lifecycle](/guides/manage-app-lifecycle/#start-only-what-a-run-needs) |
| `apps` | the system under test ([keys](#apps)) | [Manage the app lifecycle](/guides/manage-app-lifecycle/) |
| `packs` | settings for packs, keyed by pack name | |
| `lint` | test-data isolation rules ([keys](#lint)) | [Isolate test data](/guides/isolate-test-data/) |
| `fixtures` | fixture factory settings | [Fixture factories](/guides/fixture-factories/) |
| `profiles` | named overlays | [below](#finding-and-merging-files) |

### apps

Each key under `apps:` names one app.

| Key | Meaning |
| --- | --- |
| `command` | How to start the app: a string (split into words, no shell) or an argv list. A command that exits `0` before the app is ready is fine: Axx keeps polling `ready`. |
| `shell` | `true` runs `command` and `cleanup` through the shell, for pipes and `&&`. |
| `dir`, `env` | Working directory (relative to `axx.yaml`) and extra environment. |
| `dependsOn` | Apps that must be ready first. Independent apps start in parallel. |
| `enabled` | `false` skips the app. Defaults to `true`. |
| `ready` | Checks that must all pass: `http.url` (every URL returns 2xx), `tcp` (`host:port` accepts connections), `exec` (a command exits 0), `log` (a regular expression matches the app's output). `timeout` defaults to 60s, `interval` to 1s. |
| `stop` | `signal` (`SIGTERM` by default, or `SIGINT`) is sent to the app's process group; after `grace` (default 10s) it is killed. |
| `cleanup` | Runs after the app stops, even if it crashed or never became ready. |
| `active.tags` | With `active.enabled`, the app starts only when a selected scenario has one of these tags. |
| `debug` | `command`, `debugger` (`type`, `port`, `mode`), `onUnavailable` and `retry` for `axx run --debug`. |

### lint

Rules live under `lint.rules`; `lint.config` sets `baseDir` (patterns are relative to it) and the default `mode` (`error` fails, `warn` only reports); `lint.include` merges rule files such as `axx-lint.generated.yaml`. Each rule has:

| Key | Meaning |
| --- | --- |
| `name`, `description` | Shown in the report. |
| `filePatterns` | Globs (`*`, `**`, `?`, `[abc]`, `{a,b}`), relative to `baseDir`. A leading `../` reaches outside it. |
| `excludePatterns` | Globs removed from the match, for example `**/*.fixture.yaml`. |
| `type` | `regex` (default) or `jsonpath`. |
| `regex` | The first capture group that matched is the value. |
| `jsonPath` | A structural path in JSON files: `order.id`, `payments[0].id`, `payments[*].id`, optional `$.` prefix. |
| `validation` | `global-unique` (default): every occurrence is unique. `file-unique`: unique within each file. `cross-file-unique`: may repeat inside a file, never across files. |
| `mode` | Overrides the default mode for this rule. |
| `ignoreValues` | Values that are shared on purpose. |

### Finding and merging files

1. With `-c path/to/axx.yaml`, that file. Otherwise Axx looks for `axx.yaml` (or `axx.yml`) in the working directory, then in each parent directory up to the repository root.
2. With `--profile NAME` or `AXX_PROFILE=NAME`, `profiles.NAME` is merged over the file, and then `axx.NAME.yaml` next to it, if either exists. A profile that exists in neither place is an error ([`AXX-E0104`](/references/error-codes/#axx-e0104)).
3. `axx.local.yaml` next to `axx.yaml`, if present, is merged last. Keep it out of version control for personal settings.
4. `-D name=value` overrides `properties`.

Paths inside the file (`apps.*.dir`, `resources`) are relative to the directory of `axx.yaml`.

### Interpolation

String values can reference the environment and properties:

| Syntax | Value |
| --- | --- |
| `${env:NAME}` | environment variable `NAME` |
| `${sys:name}` | property `name` |
| `${sys:name:-default}` | `default` when `name` is not set; defaults can nest (`${sys:a:-${env:B}}`) |
| `$${...}` | a literal `${...}` |

The same syntax works in feature files: in service tables and step arguments.

### Errors

A syntax error or an invalid value stops every command with exit code `2` and points at the line: [`AXX-E0101`](/references/error-codes/#axx-e0101) for YAML syntax and [`AXX-E0102`](/references/error-codes/#axx-e0102) for values the schema rejects.

## axx-packs.yaml

The packs a project uses: `core`, which is always there, and the listed packs. Without the file, a project has no steps. `axx pack add`, `remove` and `new` edit it, and `axx init` creates it.

```yaml title="axx-packs.yaml"
packs:
  - rest                                # one of Axx's packs, by name
  - sql
  - ./steps                             # a pack in this repository (a Go package)
  - github.com/team/axx-grpc@v1.2.0     # a pack from a Go module; the version is optional
```

`axx-packs.lock` pins the versions of module packs; commit both files. [Choose packs](/guides/use-packs/) shows the commands.
