---
title: Run in CI
description: Run Axx in GitHub Actions with setup-axx, in GitLab CI with the install script, or anywhere with the container image, and publish JUnit and HTML reports.
---

In CI, Axx does the same thing it does on a laptop: start the services, wait until they are ready, run the scenarios, stop everything. The only extra work is installing Axx and keeping the reports.

## GitHub Actions

`axx init` writes this workflow to `.github/workflows/acceptance.yml`:

```yaml title=".github/workflows/acceptance.yml"
name: acceptance

on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  acceptance:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: nimbusxr/setup-axx@v0
      - run: axx fmt --check
      - run: axx run --format junit:build/axx/junit.xml --format html:build/axx/report.html
      - uses: actions/upload-artifact@v7
        if: always()
        with:
          name: axx-report
          path: build/axx/
```

The first `run` step checks that the feature files are laid out as `axx fmt` lays them out, and fails the job on any that are not, before a service starts. To fix that, run `axx fmt` and commit what it changed ([Format the feature files](/guides/organize-by-feature/#format-the-feature-files)).

A project whose `axx.yaml` [names its version](/guides/install/#keep-a-project-on-one-version) runs on that version here too, with nothing to change in the workflow: `setup-axx` installs the newest Axx, which hands over. Without that line, every run gets the newest release, and `with: version: 0.4.0` on `setup-axx` holds it to one. To try a newer version on a branch before moving the project, set `AXX_USE_VERSION` on the step.

GitHub-hosted Ubuntu runners include Docker and Compose, so services that start with `docker compose up` work as they do locally. `nimbusxr/setup-axx` installs the latest release (pre-releases included) and adds it to `PATH`. Its `version` input installs another: a version such as `0.1.0`, or `nightly`. `@v0` is the action's own version: its tag `v0` follows the action's 0.x releases while Axx is in beta.

## GitLab CI

Install Axx with the install script. When the services start with Docker Compose, run the job with Docker-in-Docker and point the tests at the `docker` host through a profile:

```yaml title=".gitlab-ci.yml"
acceptance:
  image: docker:27
  services:
    - docker:27-dind
  variables:
    DOCKER_TLS_CERTDIR: "/certs"
  before_script:
    - apk add --no-cache curl
    - curl -fsSL https://raw.githubusercontent.com/nimbusxr/axx/main/scripts/install.sh | sh
  script:
    - axx run --profile ci --format junit:build/axx/junit.xml --format html:build/axx/report.html
  artifacts:
    when: always
    paths: [build/axx/]
    reports:
      junit: build/axx/junit.xml
```

```yaml title="axx.yaml"
profiles:
  ci:
    properties:
      local.host: docker   # containers publish their ports on the dind host
```

This works when your features and `axx.yaml` use `${sys:local.host}` instead of a hard-coded `localhost`. The job runs as root, so the install script puts `axx` in `/usr/local/bin`, which is on `PATH`.

## Any other CI

Use the install script, or the container image for commands that do not start services:

```sh
docker run --rm -v axx-cache:/home/nonroot -v "$PWD:/work" -w /work ghcr.io/nimbusxr/axx validate
```

## Keep Axx's cache

The first command in a project prepares Axx with the packs in `axx-packs.yaml` ([Choose packs](/guides/use-packs/)). On a fresh runner, that happens on every run. Keep the prepared builds between runs to skip it. In GitHub Actions, add this step before `axx run`:

```yaml
      - uses: actions/cache@v6
        with:
          path: ~/.cache/axx/builds
          key: axx-${{ runner.os }}-${{ hashFiles('**/axx-packs.yaml', '**/axx-packs.lock') }}
          restore-keys: axx-${{ runner.os }}-
```

GitLab caches only paths inside the project, so move Axx's cache there:

```yaml title=".gitlab-ci.yml"
acceptance:
  variables:
    XDG_CACHE_HOME: "$CI_PROJECT_DIR/.cache"
  cache:
    key:
      files: [axx-packs.yaml]
    paths: [.cache/axx/builds]
```

A stale cache is harmless: when the packs or Axx's version change, Axx prepares again. A project's version is a download of about 12 MB on each fresh runner; add `~/.cache/axx/versions` to `path` to keep that too.

## Catch problems early

The exit code says what kind of failure it was, without parsing output: `1` is a failed scenario, `3` an undefined step, `4` a service that did not start ([exit codes](/references/error-codes/#exit-codes)). Two cheap guards catch the last two before any service starts, and a third keeps the feature files [formatted](/guides/organize-by-feature/#format-the-feature-files):

```sh
axx validate          # exit 3 on undefined steps, in seconds
axx doctor            # exit 4 if a service's command is missing
axx fmt --check       # exit 3 on feature files that are not formatted
```

Use `--fail-fast` to stop scheduling new scenarios after the first failure when you prefer a quick red over a full report.

## Reports

The workflows above write JUnit and HTML reports with `--format`. [Reports](/guides/reports/) lists every format, and `run.reporters` in `axx.yaml` can make them the default. `--rerun-file build/axx/rerun.txt` also records the failed scenarios for a follow-up run ([Rerun what failed](/guides/tags-and-filtering/#rerun-what-failed)).

## Share a run between runners

A suite whose scenarios take long on one runner (a device's or a browser's, which run a scenario at a time) can run on several at once: `--shard 2/3` runs the second third of the scenarios the run selects, every third in their files' and lines' order, from the second. The shards of a run, `1/3` to `3/3`, run each scenario once between them, the same on every machine:

```yaml title=".github/workflows/acceptance.yml"
jobs:
  acceptance:
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        shard: [1, 2, 3]
    steps:
      - uses: actions/checkout@v7
      - uses: nimbusxr/setup-axx@v0
      - run: axx run --shard ${{ matrix.shard }}/3 --format junit:build/axx/junit.xml
```

Each runner starts the services for its own shard. A shard that selects nothing passes.

## Measure coverage

A run with coverage measures how much of your services' code the scenarios run ([Measure coverage](/guides/measure-coverage/)). In CI, turn it on with `--coverage`, or with `run.coverage: true` in `axx.yaml`. The run writes its report to `.axx/coverage`. When every scenario passed but the coverage is under a minimum, the run exits `6`.

A shard checks no minimums, because its coverage is only part of the run's. Keep each shard's report, then check them together in a job after the shards:

```yaml title=".github/workflows/acceptance.yml"
jobs:
  acceptance:
    runs-on: ubuntu-latest
    strategy:
      fail-fast: false
      matrix:
        shard: [1, 2, 3]
    steps:
      - uses: actions/checkout@v7
      - uses: nimbusxr/setup-axx@v0
      - run: axx run --coverage --shard ${{ matrix.shard }}/3
      - uses: actions/upload-artifact@v7
        with:
          name: coverage-${{ matrix.shard }}
          path: .axx/coverage/
  coverage:
    needs: acceptance
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: nimbusxr/setup-axx@v0
      - uses: actions/download-artifact@v8
        with:
          pattern: coverage-*
      - run: axx coverage check coverage-1/ coverage-2/ coverage-3/
```

`download-artifact` puts each shard's report in a folder named after its artifact. `axx coverage check` merges them, writes the whole run's report to `.axx/coverage`, prints the totals, and exits `6` under a minimum. It refuses shards that are missing, repeated or from different runs, so it never checks only part of the run.

## Run scenarios in parallel safely

CI runners have fewer CPUs than laptops, and `run.workers` defaults to the number of CPUs. If scenarios that pass locally fail in CI, check that they use unique data ([Isolate test data](/guides/isolate-test-data/)) before lowering `--workers`.
