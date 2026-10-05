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

Upgrade with the channel you installed from (`brew upgrade`, rerun the script, or `go install ...@latest`). Step text never changes between versions, so feature files keep working. Before `v1.0.0`, a minor version bump (`0.1` to `0.2`) may change a flag or a configuration key; read the [changelog](https://github.com/nimbusxr/axx/blob/main/CHANGELOG.md) first.

After upgrading, refresh the agent skills so their step index matches the new binary:

```sh
axx skills install
```
