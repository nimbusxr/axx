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
curl -fsSL https://axx.nimbusxr.us/install.sh | sh
```

The script picks the newest release (pre-releases included, since every `0.x` release is one), downloads the archive for your OS and architecture, and checks it against `checksums.txt`. It installs `axx` in `/usr/local/bin` when that is writable, otherwise in `~/.local/bin`. Set `AXX_INSTALL_DIR` to choose the directory, and `AXX_VERSION` to install a given version (`0.1.0`) or the `nightly` build.

### Install script (Windows)

```powershell
irm https://axx.nimbusxr.us/install.ps1 | iex
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

## Upgrade

Upgrade with the channel you installed from (`brew upgrade`, rerun the script, or `go install ...@latest`). Step text never changes between versions. Before `v1.0.0`, a minor version bump (`0.1` to `0.2`) may change a flag, a configuration key or what a feature's values mean; read the [changelog](https://github.com/nimbusxr/axx/blob/main/CHANGELOG.md) first.

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

After upgrading, refresh the agent skills so they match the new binary:

```sh
axx skills install
```
