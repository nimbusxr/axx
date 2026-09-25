---
title: Run in CI
description: Run Axx in GitHub Actions with setup-axx, in GitLab CI with the install script, or anywhere with the container image, and publish JUnit and HTML reports.
---

In CI, Axx does the same thing it does on a laptop: start the apps, wait until they are ready, run the scenarios, stop everything. The only extra work is installing Axx and keeping the reports.

<!-- TODO(verify): nimbusxr/setup-axx, install.sh and ghcr.io/nimbusxr/axx publish with v0.1.0. Confirm the action's inputs (for example a version input) then. -->

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
      - uses: actions/checkout@v5
      - uses: nimbusxr/setup-axx@v1
      - run: axx run --format junit:build/axx/junit.xml --format html:build/axx/report.html
      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: axx-report
          path: build/axx/
```

GitHub-hosted Ubuntu runners include Docker and Compose, so apps that start with `docker compose up` work as they do locally. `nimbusxr/setup-axx` installs the latest release (pre-releases included) and adds it to `PATH`.

## GitLab CI

Install Axx with the install script. When the apps start with Docker Compose, run the job with Docker-in-Docker and point the tests at the `docker` host through a profile:

```yaml title=".gitlab-ci.yml"
acceptance:
  image: docker:27
  services:
    - docker:27-dind
  variables:
    DOCKER_TLS_CERTDIR: "/certs"
  before_script:
    - apk add --no-cache curl
    - curl -fsSL https://axx.nimbusxr.us/install.sh | sh
    - export PATH="$HOME/.local/bin:$PATH"
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

This works when your features and `axx.yaml` use `${sys:local.host}` instead of a hard-coded `localhost`.

<!-- TODO(verify): the install directory the install script uses ($HOME/.local/bin above). -->

## Any other CI

Use the install script, or the container image for commands that do not start apps:

```sh
docker run --rm -v axx-cache:/home/nonroot -v "$PWD:/work" -w /work ghcr.io/nimbusxr/axx validate
```

## Keep Axx's cache

The first command in a project prepares Axx with the packs in `axx-packs.yaml` ([Choose packs](/guides/use-packs/)). On a fresh runner, that happens on every run. Keep the prepared builds between runs to skip it. In GitHub Actions, add this step before `axx run`:

```yaml
      - uses: actions/cache@v4
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

A stale cache is harmless: when the packs or Axx's version change, Axx prepares again.

## Catch problems early

The exit code says what kind of failure it was, without parsing output: `1` is a failed scenario, `3` an undefined step, `4` an app that did not start ([exit codes](/references/error-codes/#exit-codes)). Two cheap guards catch the last two before any app starts:

```sh
axx validate          # exit 3 on undefined steps, in seconds
axx doctor            # exit 4 if an app's command is missing
```

Use `--fail-fast` to stop scheduling new scenarios after the first failure when you prefer a quick red over a full report.

## Reports

The workflows above write JUnit and HTML reports with `--format`. [Reports](/guides/reports/) lists every format, and `run.reporters` in `axx.yaml` can make them the default. `--rerun-file build/axx/rerun.txt` also records the failed scenarios for a follow-up run ([Rerun what failed](/guides/tags-and-filtering/#rerun-what-failed)).

## Run scenarios in parallel safely

CI runners have fewer CPUs than laptops, and `run.workers` defaults to the number of CPUs. If scenarios that pass locally fail in CI, check that they use unique data ([Isolate test data](/guides/isolate-test-data/)) before lowering `--workers`.
