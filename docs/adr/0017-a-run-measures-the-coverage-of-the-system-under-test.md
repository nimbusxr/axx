# 0017: A run measures the coverage of the system under test

- Status: Accepted
- Date: 2026-11-12

## Context

People who run acceptance tests ask how much of the system the scenarios reach, and want a run to
fail when that drops. Axx measured only the web apps' JavaScript, with `web-coverage`. The rest of
the system under test is the services axx starts: JVM, Go, Python and Node.js services, on the
machine or in containers, and services in any other language. Each runtime has its own coverage tool,
its own way of being given it, and its own moment when it writes what ran: most write only as the
process exits.

## Decision

**A run measures, never a scenario.** `axx run --coverage` (or `run.coverage` in axx.yaml) measures
the services whose `coverage` section names a runtime, from the moment they start until they stop.
The run's report is one lcov report, with Istanbul's JSON beside it, merged from every part: each
service, and the web apps' JavaScript. Coverage per scenario is left out: most runtimes write only as
they exit, and the run's data is versioned (`run.json`) so it can be added for those that can.

**One pack per runtime, on one core.** `coverage-core` holds what every runtime shares: the report,
the minimums, the merging of shards, and the `report` runtime, for a service that writes lcov or
Cobertura itself, so any language can take part. `coverage-jvm` (JaCoCo's agent), `coverage-go`
(`go build -cover`), `coverage-python` (coverage.py) and `coverage-node` (V8's own coverage) register
with it, and so does `web-coverage`, as the run's `web` part. A runtime gives the service what makes
it measured, as environment variables, and reads what it wrote once it stopped. The core of axx knows
only two interfaces, one that readies a service and one that concludes the run, and none of a
runtime's specifics.

**How a service starts is axx.yaml's; how its data is read is its pack's.** A service's `coverage`
section (`runtime`, `command`, `env`, `mount`) sits beside `debug`, because it changes what runs:
the axx host's approval and the filling in of `${...}` cover it as they cover `debug.command`. Where
a runtime finds the service's classes, modules or files is in the runtime pack's settings.

**A service in a container writes into a folder of this machine.** Axx gives each measured service a
folder of the run's, which `AXX_COVERAGE_DIR` names, on every run so a compose file can mount it
always; `mount` says where the container sees it. The folder is writable by every user, as a
container's processes often run as a user of their own. Axx opens no port and no inspector: an
unauthenticated inspector would let any local process run code in the service.

**Minimums are checked on a run of every scenario only.** A run of some paths, tags or names, a
shard, a rerun, a run that stopped at its first failure, was interrupted, or did not measure one of
its services still writes its report and prints its totals, but its coverage says nothing about the
minimums, so it does not check them. CI's shards are checked together by `axx coverage check`, which
refuses missing, repeated or mixed shards. A run whose scenarios all passed but whose coverage is
under a minimum exits 6 (ADR 0003). A service that wrote nothing is never 0%: the run says why, from
how it ended, and when minimums apply that is an environment error, exit 4.

**Services axx up keeps are measured per run where the runtime can be read while it runs.** JVM
services, through the agent's port that the pack's settings name (axx never picks one), and Node.js
services, through a file the preload watches, are reset as a run starts and read as it ends, under a
lock so two runs cannot reset each other's. Go and Python write only as they exit: their coverage,
of every run since `axx up`, comes at `axx down`.

**Editors run with coverage.** VS Code's and IntelliJ's Run with Coverage run `axx run --coverage`
and show the report the run announces, in every language at once.

**Every download is pinned and has a mirror.** JaCoCo comes from Maven Central (`AXX_MAVEN_URL`) and
coverage.py from PyPI (`PIP_INDEX_URL`), each checked against a hash in axx.

## Alternatives considered

- **Coverage per scenario.** It would tell which scenario reaches which line, but only JaCoCo and V8
  can be read while the process runs, and the reset between scenarios would serialize the run.
- **Each runtime's own report, side by side.** Simpler, but there would be no one number to set a
  minimum on, and no one report for editors and coverage services.
- **The inspector for Node.js.** It reads coverage without a preload, but it needs a port, which
  containers would have to publish, and has no authentication.
- **Minimums checked on every run.** A run of one feature would fail for the code other features
  reach. Shards would each fail for the same reason.

## Consequences

- Measuring a service needs its cooperation: a build for coverage (Go), coverage.py where it runs
  (Python), and an exit on its stop signal rather than being killed after its grace. The run says
  which of these went wrong when a service wrote nothing.
- Reading JVM and Python data needs Java, and Python 3.11 or later, on the machine that runs axx;
  `axx doctor` says whether it has them.
- Mobile and desktop apps are not measured yet. They can follow as runtimes of the same core: JaCoCo
  for Android, LLVM's coverage for Apple's platforms.
