# axx agent evals

These evals measure how well coding agents write axx acceptance tests, with and without the
aids axx ships for agents: the skills (`axx skills install`), the MCP server (`axx mcp`) and
the `AGENTS.md` section (`axx init`). Each task gives an agent a small repository and
acceptance criteria written the way a product person writes them; a verifier then decides,
without looking at how the agent worked, whether the resulting tests are real.

The evals run on [Harbor](https://harborframework.com) (the successor of Terminal-Bench): every
task is a Harbor task directory, and any agent Harbor supports (Claude Code, Codex CLI, Gemini
CLI, OpenHands, ...) can be evaluated.

## What's here

| Path | What |
| --- | --- |
| `app/` | **parcels**, the system under test: a Go HTTP service (OpenAPI 3.1 at `/openapi.json`) with PostgreSQL storage, a MongoDB tracking read model, a manifest importer, calls to a downstream address service (WireMock in tests) and Avro `ParcelRegistered` events on Kafka. It can be started with deliberate bugs (mutants). `app/compose.yaml` runs it with its infrastructure. |
| `internal/mutant/` | The mutants: each a realistic bug a good acceptance test must catch. |
| `workspace/` | The starting project most tasks share: README, `axx.yaml`, the OpenAPI contract, the business rules in `docs/`, the WireMock stubs. |
| `tasks/<id>/` | The tasks (Harbor format, below). |
| `cmd/evals-verify/`, `internal/verify/` | The verifier that runs inside the verifier container. |
| `cmd/evals/` | The runner: build images, prepare conditions, run Harbor, report, compare. |
| `images/` | The Docker images the tasks build on. |
| `conditions.toml`, `conditions/mcp.json` | The four conditions. |
| `results/` | Result files (`<date>-<agent>.json` and `.md`) and the release baseline. |

## The tasks

| Task | Needs | Criteria | Mutants it must catch |
| --- | --- | --- | --- |
| `rest-crud-happy-path` | rest | register, look up, change, cancel a parcel | `wrong-status-on-create`, `update-not-persisted`, `delete-not-removed` |
| `openapi-reject-invalid` | rest | overweight and unknown service level refused with 400; nothing stored (needs a deliberately relaxed OpenAPI validation level) | `no-weight-limit`, `no-service-level-check` |
| `mock-address-check` | rest | the postcode check with the address service (WireMock): called once, with the API key; zone stored; undeliverable refused with 422 | `skips-address-check`, `address-check-without-api-key`, `ignores-undeliverable` |
| `sql-manifest-import` | | seed manifest lines, assert the parcel rows (JSON columns) and the line status | `import-drops-postcode`, `import-leaves-line-pending`, `import-wrong-source`, `import-accepts-overweight` |
| `mongo-tracking-view` | | seed scans in MongoDB, assert the tracking read model | `tracking-status-from-first-scan`, `tracking-counts-duplicate-scans` |
| `kafka-registered-event` | rest, kafka | an Avro `ParcelRegistered` event is published, keyed and filled correctly | `event-not-published`, `event-weight-in-kilograms` |
| `fix-broken-feature` | | repair a feature with a wrong step text and a wrong expectation, keeping its scenarios | `import-wrong-source`, `import-accepts-overweight` |
| `init-first-feature` | rest | `axx init` a bare repository, make `axx run` start the service, write the first feature | `wrong-status-on-duplicate`, `wrong-status-on-create` |
| `parallel-unique-data` | rest | a shop's parcel list, correct under 16 workers in random order, plus an `axx lint` rule that bites | `list-ignores-sender-filter`, `delete-not-removed` |

"Needs" lists the axx packs beyond core, mock, sql and mongo (`requires` in
`task.toml`). The runner checks which packs the axx under test has and skips a task until all
of its packs exist, so the Kafka task waits for the kafka pack without anything to change.

## Run the evals locally

You need Docker (with Compose v2), Go (see `go.mod`), and Harbor:

```sh
uv tool install harbor            # or: pip install harbor
```

From this directory:

```sh
# Check the plumbing without any model: the oracle agent applies each task's reference
# solution (every verifier must give 1), the nop agent does nothing (every verifier must give 0).
go run ./cmd/evals run --agent oracle --conditions none
go run ./cmd/evals run --agent nop --conditions none

# Evaluate an agent under all four conditions (API keys come from the environment or --env-file).
export ANTHROPIC_API_KEY=...
go run ./cmd/evals run --agent claude-code --model anthropic/claude-sonnet-4-5
```

`run` builds the images (axx from this checkout), writes one Harbor dataset per condition
under `.work/datasets/`, runs `harbor run` for each (jobs under `.work/jobs/`), and writes
`results/<date>-<agent>.json` and `.md`. Useful flags: `--tasks a,b`, `--conditions none,both`,
`--attempts 3` (Harbor's `-k`), `--concurrency 4` (each trial runs its own databases, so budget
about 4 GB of memory per concurrent trial), `--dry-run` (print the Harbor commands),
`--include-pending` (run tasks whose packs are missing). Any Harbor agent works with `--agent`;
agent options pass through with `--ak key=value`.

Step by step, or with your own Harbor flags:

```sh
go run ./cmd/evals images                                  # axx-evals-base, -verifier, -address-service
go run ./cmd/evals prepare --condition skills --out /tmp/ds-skills
harbor run -p /tmp/ds-skills -a claude-code -m anthropic/claude-sonnet-4-5 -o jobs --job-name skills
go run ./cmd/evals report --agent claude-code --job skills=jobs/skills
```

A single task also runs straight from its directory once the images exist (it is a regular
Harbor task): `harbor run -p tasks/sql-manifest-import -a oracle`.

## The four conditions

`conditions.toml` defines them; the runner applies them per task:

| Condition | AGENTS.md section | Skills | MCP server |
| --- | --- | --- | --- |
| `none` | | | |
| `skills` | yes | yes | |
| `mcp` | yes | | yes |
| `both` | yes | yes | yes |

- **AGENTS.md section**: the section `axx init` writes (`project-setup finish --agents-md` runs
  `axx init` in a scratch project and keeps its `AGENTS.md`). Every initialized axx project has
  it, so every condition except the bare baseline includes it; set `agents_md` to change that.
- **Skills**: `axx skills install` in the project, exactly as a user runs it (`.agents/skills`
  for Codex, Cursor, Gemini CLI and Copilot; `.claude/skills` for Claude Code).
- **MCP**: `axx mcp`, registered through Harbor's `--mcp-config conditions/mcp.json` (a
  Claude-style `.mcp.json`), which Harbor turns into each agent's own MCP configuration.

Both aids are baked into the project before its initial commit, so the agent sees them as part
of the repository.

## How a task is verified

Each task runs Harbor's **separate verifier** (`environment_mode = "separate"` in `task.toml`):
after the agent finishes, Harbor copies the agent's `/app` (without `.axx` and `.git`) into a
fresh container built from `tests/Dockerfile`, with fresh databases, and runs `tests/test.sh`,
which runs `evals-verify` with the task's `tests/verify.toml`. Nothing the agent did to its own
container (binaries, databases, the app) can reach the verifier, and the agent never sees the
verifier, its checks or the list of mutants.

The reward is 1 only if every check passes:

1. **Static rules** (below): only allowed files changed, the features read as acceptance
   criteria, no probes, no fake custom steps.
2. **`axx validate`** reports no problems. Where the task asks: **`axx lint`** passes, and fails
   once the verifier plants a copy of one of the agent's seed files (the rule bites).
3. **The correct app passes**: `axx run` exits 0 with at least `min_scenarios` passing scenarios,
   nothing skipped, pending or undefined, every scenario tag filter overridden (a `@wip` tag does
   not hide a scenario), `--workers 8` and a random order. `repeat` runs it again on fresh data in
   new random orders; `start_apps` makes the first run a plain `axx run` that starts the service
   from the agent's own `axx.yaml`; `preserve_scenarios` must still exist and pass.
4. **Every targeted mutant fails**: for each mutant the verifier resets all data, starts the app
   with that bug, runs the suite and requires exit code 1 with at least one failing scenario. Exit
   codes 2 to 4 (configuration, undefined steps, app failures) do not count as catching the bug.

The verifier starts and stops the app itself (as the `parcels` user, with `EVALS_MUTANT` in the
app's own environment only) and runs axx as the unprivileged `tester` user, so nothing the suite runs can
read which variant is running. `verify.json` in the trial's `verifier/` directory holds
every check with its details, each run's JSON report and the app logs; `reward.json` has
`reward`, `mutants_caught` and `mutants_total`.

## Anti-cheat rules

| Rule | What it rejects |
| --- | --- |
| `allowed-paths` | Adding, changing or deleting any file outside the task's `allowed` globs (the service, the docs, the stubs, `axx.yaml` unless the task needs it). Agent state (`.git`, `.axx`, `.agents`, `.claude`, `.mcp.json`, `AGENTS.md`, `node_modules`, caches) is ignored. |
| `config-keys` | Changing any top-level `axx.yaml` key other than the ones the task names (e.g. only `lint`). |
| `required`, `absent`, `must-not-contain` | Missing deliverables (`axx.yaml`, features) or leftovers the task forbids. |
| `variables` | `${var:...}` or `${env:...}` in a feature: data is chosen up front, never captured or passed between steps. |
| `doc-string-code` | Doc strings with code (anything but a payload: JSON, XML, YAML, CSV, text). |
| `scenario-name`, `no-outcome` | Unnamed or duplicate scenarios, and scenarios without a `Then` step. |
| `probe` | Code that tries to detect the variant under test instead of testing behavior: references to `EVALS_MUTANT`, "mutant", `/proc/`, `/opt/evals`, `/logs/verifier` in sources the agent writes. |
| mutant checks | Tests that pass no matter what (fake steps, missing assertions, assertions on the mock instead of the service) survive the mutants and get reward 0; tests that fail no matter what never pass the correct app. |

The mutant checks are the core of it: a suite earns its reward only by passing against the
correct service and failing against each deliberately broken one.

## Results and the release gate

`results/<date>-<agent>.json` (schema below) and a Markdown table (task x condition) are written
after every run. A condition's **score** is the mean reward over the tasks that ran, in percent.

```json
{
  "schemaVersion": 1,
  "kind": "axx-evals-results",
  "date": "2026-09-24",
  "agent": "claude-code",
  "model": "anthropic/claude-sonnet-4-5",
  "axx": "0.1.0",
  "harbor": "0.23.0",
  "conditions": ["none", "skills", "mcp", "both"],
  "tasks": [
    {"id": "rest-crud-happy-path", "title": "...", "category": "rest",
     "results": {"none": {"reward": 1, "trials": 1, "passed": 1, "mutantsCaught": 3, "mutantsTotal": 3}}}
  ],
  "skipped": [{"id": "kafka-registered-event", "reason": "requires the kafka pack(s), not in this axx build"}],
  "scores": {"none": 58.3, "skills": 75.0, "mcp": 66.7, "both": 83.3}
}
```

The **baseline** is a result file promoted as-is: `results/baseline-<agent>.json` (copy a run you
trust). The gate compares a new result file with it:

```sh
go run ./cmd/evals compare results/baseline-claude-code.json results/2026-10-01-claude-code.json --max-drop 10
```

It scores both files over the tasks they have in common (so adding or skipping tasks does not
move the score), prints the per-condition change and the tasks that flipped, and exits 1 when any
condition dropped by more than `--max-drop` points (default 10). The release process runs the
evals for the release candidate and blocks the release PR on exit 1. Agents are nondeterministic:
use `--attempts 3` for gate runs so a single unlucky trial does not move a task by a full 100
points.

## CI

`.github/workflows/evals.yml` runs the evals on demand only (`workflow_dispatch`: pick the agent,
model, conditions, tasks and attempts); they never run on pull requests. API keys come from the
repository secrets `ANTHROPIC_API_KEY`, `OPENAI_API_KEY` and `GEMINI_API_KEY`. The workflow
uploads the result files and the Harbor jobs, adds the table to the run summary, and applies the
baseline gate when `results/baseline-<agent>.json` exists.

## Add a task

1. Create `tasks/<id>/` with:
   - `task.toml`: copy one. Set `[task].name = "axx-evals/<id>"`, and under `[metadata]` the
     `title`, `difficulty`, `category`, `requires` (packs beyond core, mock, sql, mongo),
     `services` (`postgres`, `mongo`, `address-service`, `kafka`) and `workspace` (`common`
     starts from `workspace/`; `none` starts from the overlay only). Keep
     `environment_mode = "separate"` and the `/app` artifact.
   - `instruction.md`: acceptance criteria as a product person writes them. Never step text.
   - `environment/workspace/`: files added to (or replacing) the starting project.
   - `tests/verify.toml`: `allowed`, `min_scenarios`, `mutants` and any other rule from
     `internal/spec/spec.go`.
   - `solution/solve.sh` (and files): the reference solution, written as acceptance criteria a
     person can read.
2. If no existing mutant fits, add one to `internal/mutant/mutant.go` and implement it in `app/`
   (every mutant must be targeted by some task; `go test ./...` checks).
3. `go run ./cmd/evals sync` generates the task's Dockerfiles, compose files, `test.sh` and the
   initial-state manifest (`go test ./...` fails while they are stale).
4. `go run ./cmd/evals check --solution <id>` must print reward 1 and
   `go run ./cmd/evals check --nop <id>` reward 0 (`--images` rebuilds the images first). `check`
   is Harbor in miniature: it starts the task's infrastructure with docker compose, builds the
   starting project in a verifier container and runs the verifier there, which is much faster
   while you iterate. Replay a weak or cheating answer with `--patch DIR` (it must get 0; see
   `testdata/negative/`). Then run the task through Harbor with the oracle and nop agents.

## The app

```sh
docker compose -f app/compose.yaml up -d --build --wait                  # app + PostgreSQL, MongoDB, WireMock
docker compose -f app/compose.yaml --profile kafka up -d --build --wait  # plus Kafka and the Schema Registry
EVALS_MUTANT=no-weight-limit docker compose -f app/compose.yaml up -d app   # with a bug
```

`parcels reset` wipes every store (the Postgres schema, the Mongo database, the WireMock journal
and the events topic). The app's own tests: `go test ./...` in this directory.
