---
title: Install Axx
description: Install the Axx CLI with Homebrew, the install script, go install or the container image, verify the download and keep it up to date.
---

Axx is a single static binary for Linux, macOS and Windows on amd64 and arm64. It has no runtime dependencies. Docker is only needed if the services you test start with Docker Compose.

:::caution[Pre-release]
Axx is in beta. Every `0.x` release is marked as a pre-release on GitHub.
:::

## Choose a channel

### Homebrew (macOS, Linux)

```sh
brew install nimbusxr/tap/axx
```

### Install script (Linux, macOS, CI)

```sh
curl -fsSL https://raw.githubusercontent.com/nimbusxr/axx/main/scripts/install.sh | sh
```

The script picks the newest release (pre-releases included, since every `0.x` release is one), downloads the archive for your OS and architecture, and checks it against `checksums.txt`. It installs `axx` in `/usr/local/bin` when that is writable, otherwise in `~/.local/bin`. Set `AXX_INSTALL_DIR` to choose the directory, and `AXX_VERSION` to install a given version (`0.1.0`) or the `nightly` build.

### Install script (Windows)

```powershell
irm https://raw.githubusercontent.com/nimbusxr/axx/main/scripts/install.ps1 | iex
```

The script installs the newest release in `%LOCALAPPDATA%\axx\bin`, after checking it against `checksums.txt`, and adds that directory to your user `PATH`.

### Go

```sh
go install github.com/nimbusxr/axx/cmd/axx@latest
```

Needs Go 1.27 or newer. The binary lands in `$(go env GOPATH)/bin`.

### Container image

```sh
docker run --rm -v axx-cache:/home/nonroot -v "$PWD:/work" -w /work ghcr.io/nimbusxr/axx validate
```

The image contains only the `axx` binary (on a distroless base). The `axx-cache` volume keeps the packs Axx prepares for the project ([Choose packs](/guides/use-packs/)); without it, every run prepares them again. It suits commands that do not start services (`validate`, `steps`, `explain`, `schema`) and runs against services that are already up (`axx run --no-start`). It cannot run `docker compose` for you.

### Release archives

Download `axx_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows) from [GitHub releases](https://github.com/nimbusxr/axx/releases), extract `axx` and put it on your `PATH`. A rolling `nightly` pre-release is built from `main` every day.

## Verify the download

Release checksums are signed with Sigstore (keyless), and archives carry an SBOM and build provenance.

```sh
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/nimbusxr/axx/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --ignore-missing -c checksums.txt

gh attestation verify axx_0.1.0_linux_amd64.tar.gz --repo nimbusxr/axx
```

## Check the installation

```sh
axx version
axx doctor
```

`axx doctor` checks the binary, `axx.yaml`, the step packs, your feature files and the commands your services need. It exits `0` when nothing failed (warnings are allowed) and `4` otherwise.

## Shell completion

```sh
axx completion zsh  > "${fpath[1]}/_axx"        # zsh
axx completion bash > /etc/bash_completion.d/axx  # bash
axx completion fish > ~/.config/fish/completions/axx.fish
```

## Editors

To see Axx's steps in feature files as you write them, set up IntelliJ IDEA, VS Code or another editor with [Set up your editor](/guides/set-up-your-editor/).

## Keep a project on one version

Name the version in `axx.yaml`, and everyone, every CI job and every agent runs the project on it, whichever Axx is installed:

```yaml title="axx.yaml"
version: 1
axx: 0.5.0
```

`axx init` writes this line for new projects; in an older project, add it yourself. Without it, a project runs on the installed Axx.

The installed Axx reads the line before anything else. The first time a machine needs that version, it downloads the release from GitHub, checks it against the release's checksums and keeps it. That version then does everything, preparing the project's packs included. So someone new to the project needs any Axx installed and nothing more, and going from one project to another takes no command at all.

The line names one exact release, not a minimum: a newer Axx still runs the project on `0.5.0`. Only [`axx upgrade`](#upgrade) moves it, in a change you commit.

Projects can name the releases after `0.4.0`. `0.4.0` and older refuse the line, so someone on those upgrades first, and a project cannot name them.

| Variable | What it does |
| --- | --- |
| `AXX_USE_VERSION=0.6.0` | runs on another version, to try it before moving the project |
| `AXX_AUTO_DOWNLOAD=0` | stops instead of downloading; `axx install` then downloads the project's version, or the one you name |
| `AXX_DOWNLOAD_URL=https://mirror.example.com/axx` | gets releases from a mirror of GitHub's files: `<url>/v0.5.0/checksums.txt`, with the archives beside it |

Preparing packs still needs go.dev and the Go module proxy, as it does for any project ([Choose packs](/guides/use-packs/)). The versions are in Axx's cache (`~/.cache/axx/versions` on Linux, `~/Library/Caches/axx/versions` on macOS), which is safe to delete. Editor plugins complete `axx.yaml` from their own schemas, which can be a version off the project's.

## Upgrade

```sh
axx upgrade                    # the newest release
axx upgrade --check            # what it would change, changing nothing
axx upgrade --version 0.6.0    # that version: older ones too
```

`axx upgrade` says what changed between the two versions, breaking changes first, and updates the agent skills of the project you are in and of your home directory, where they are installed. It talks to `api.github.com` and `github.com`, and `GITHUB_TOKEN`, if it is set, avoids GitHub's rate limit. What else it does depends on where you run it.

### In a project that names its version

It moves the [project](#keep-a-project-on-one-version). It downloads the release from GitHub into Axx's cache, moves the `axx:` line and checks the project on the new version with `axx validate` and `axx lint`. You commit the line, and everyone and CI follow it. If a check finds problems, it lists them, and `axx upgrade --version <the old one>` goes back.

The project's version comes from GitHub however Axx was installed, as our Homebrew tap keeps only the newest version.

An installed Axx older than the newest release is upgraded too, as the table below says, even when the project is on the newest already. If that fails, the project has still moved and runs all the same. `--version` moves only the project.

### Anywhere else

It upgrades the installed Axx, the way it was installed:

| Installed with | `axx upgrade` | `axx upgrade --version` |
| --- | --- | --- |
| Homebrew | runs `brew update` and `brew upgrade --cask axx` | refuses, as our tap keeps only the newest version, and says how to install another next to it |
| The install scripts or a release archive | downloads the release, checks it against its `checksums.txt` and replaces itself | the same, for any release or `nightly` |
| `go install` | runs `go install` for the newest release | the same, for any release |
| A nightly | asks which version you want | downloads it and replaces itself, as above |
| A build from source | says to build it again | says to build it again |

In a container, use a newer image instead.

Homebrew's tap gets a release a few minutes after GitHub does. Until then, `axx upgrade` says that Axx is still on its version, and running it again later finishes the job. A project moves in the meantime, as its version does not come from Homebrew.

:::note[Homebrew 7 and a tap added before it]
Homebrew 7 refuses a cask from a tap added before it until you trust that tap, and `axx upgrade` shows you Homebrew's message. Run `brew trust nimbusxr/tap` once. Axx leaves this to you, as it decides what Homebrew may load. A tap that `brew install nimbusxr/tap/axx` adds on Homebrew 7 needs nothing.
:::

### Upgrading any other way

`brew upgrade --cask axx`, or the install script again, changes only the installed Axx. A project stays on the version it names, and the agent skills stay as they were until you update them:

```sh
axx skills update
```

`axx upgrade` came after `0.4.0`: from an earlier version, rerun the install script or `brew upgrade --cask axx` once.

### What can change

Step text never changes between versions. Before `v1.0.0`, a minor version bump (`0.1` to `0.2`) may change a flag, a configuration key or what a feature's values mean; read what `axx upgrade --check` lists first, or the [changelog](https://github.com/nimbusxr/axx/blob/main/CHANGELOG.md).

### The reminder

Once in a while, after an `axx run` that passes in your terminal, Axx says on one line that a newer release is out. That is at most once a week, however many come out, and it never happens in CI, for an agent or with `--json`. To know, it reads GitHub's list of releases at most once a day, in the background and with no credentials. `AXX_NO_UPGRADE_REMINDER=1` turns this off. `axx doctor` says the same from what was last read.

### To 0.5.0

`axx lint` fails on a scenario with a second `When` or a second `Then`, instead of warning about a `When` after a `Then`. A scenario is one behavior: write `And`, or split it. `axx upgrade` shows you where, as it runs `axx lint` on the new version. To only report them while you clean up a suite, set this in `axx.yaml`:

```yaml title="axx.yaml"
lint:
  config:
    mode: warn
```

### To 0.3.0

A value now [means what it says](/references/steps/#values) in every pack, set or checked, and checks are strict. What to change in your features:

| You wrote | Write | Why |
|---|---|---|
| `\| postcode \| 10115 \|`, for a string | `\| postcode \| "10115" \|` | `10115` is the number; nothing is inferred from a schema, a column or an example |
| `the response payload property postcode is '10115'`, for a string | `is '"10115"'` | a step's quotes are the step's, and the value inside them follows the rule |
| `\| weightKg \| 6 \|`, for a double of 6.0 | `\| weightKg \| 6.0 \|` | `6` is an integer and `6.0` a decimal, and types must match |
| an empty cell | `""`, `null`, or `undefined` where the step takes it | an empty cell could be any of them |
| `01067`, `+5`, `1.5f`, `NaN` | `1067` for the number, or `"01067"` for the text | JSON has no such number |
| `\| X-Count \| 3 \|`, where only text fits | `\| X-Count \| "3" \|` | a header, a query parameter or a line of a file holds text |
| `the response header Retry-After is '5'`, `the "Weight" field is filled with "1200"`, `the response body contains '{"a":1}'` | `is '"5"'`, `filled with '"1200"'`, `contains '"{"a":1}"'` | a step's own value follows the rule too; plain text like `'application/json'` stays as it is, and so do names and patterns |
| `\| postcode \| 10115 \|` or `\| price \| 12 \|` in a `where:`, on a text or a decimal column | `\| postcode \| "10115" \|`, `\| price \| 12.0 \|` | a `where:` is held to the column's real type before the query, in SQL and BigQuery, and a MongoDB selection is as strict as a check: `6` selects no double, nor `null` a document without the field |

A regular expression that expects a whole double to read `6` needs to allow `6.0`: a pattern matches the value's text, and a whole double keeps its decimal point.

`axx validate` reports empty cells, numbers in other forms, and values that are not text where only text fits, in a table or in a step's own text. It cannot know that `| postcode | 10115 |` means a string: that fails when the scenario runs, and the message says what to write.
