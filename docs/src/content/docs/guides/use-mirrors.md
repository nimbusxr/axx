---
title: Use your organization's mirrors
description: Point every download Axx makes at your organization's mirrors, such as the remote repositories of an artifact repository manager and a registry for container images, and check what Axx downloads.
---

Some organizations let developers' machines and CI download only through mirrors of their own. Usually these are the remote repositories of an artifact repository manager, such as Artifactory or Nexus: each one fetches from a public site, scans what it fetches and keeps a copy. A registry does the same for container images.

Axx works behind such mirrors. Each of its downloads has a setting that points it at a mirror, all but one, which Cisco's license ties to Cisco. Axx checks every file it downloads itself against a pinned hash or a signed list, so a mirror can't change what runs.

## What Axx downloads

Axx downloads each of these the first time something needs it, and keeps it.

| What | Needed by | Comes from | Setting |
| --- | --- | --- | --- |
| Axx's releases | the install scripts, a project's [pinned version](/guides/install/#keep-a-project-on-one-version), `axx upgrade`, [`axx docs serve`](/guides/read-docs-offline/), desktop-macos's launcher | Axx's releases on github.com | `AXX_DOWNLOAD_URL` |
| Go | preparing a project's [packs](/guides/use-packs/) | the Go module proxy | `GOPROXY`, `GOSUMDB` |
| The packs' Go modules | preparing a project's packs | the Go module proxy | `GOPROXY`, `GOSUMDB`, `GOPRIVATE` |
| Node.js | the web packs | nodejs.org | `NODE_MIRROR` |
| Playwright, Lighthouse and axe-core | the web packs | the npm registry | `npm_config_registry` |
| Browsers | web-core | Playwright's servers | `PLAYWRIGHT_DOWNLOAD_HOST`, Playwright's own setting |
| WebDriverAgent and the UiAutomator2 server | mobile-ios and mobile-android | Appium's releases on github.com | `AXX_GITHUB_URL` |
| Piper | models-windows | Piper's releases on github.com | `AXX_GITHUB_URL` |
| C++/WinRT | models-windows | nuget.org | `AXX_NUGET_URL` |
| OpenH264 | videos of scenarios, and videos an Android emulator's camera plays | Cisco | none: see [OpenH264](#openh264) |

Axx downloads Go the way the go command downloads a Go release it needs: as the module `golang.org/toolchain`, through `GOPROXY`, checked against Go's checksum database. So the one Go proxy serves both Go and the modules.

Axx checks what it downloads in one of three ways:

- Its own releases against the release's `checksums.txt`.
- Go and the Go modules against Go's checksum database, as the go command checks them.
- Everything else against a hash pinned in Axx.

The browsers are the exception: Playwright downloads them, as `npx playwright install` does. To download no browser at all, run your web app's scenarios in the Google Chrome or Microsoft Edge installed on the machine, with the `chrome` or `msedge` [engine](/guides/test-web-apps/).

Container images aren't in the table because Axx pulls none itself: Docker pulls the images your project's compose files name. See [Container images](#container-images).

## Set up the mirrors

1. In your artifact repository manager, create a remote repository for each source your projects need:

   | Remote repository of | Kind | Needed for |
   | --- | --- | --- |
   | `https://github.com` | generic | Axx's releases; the mobile packs' drivers; models-windows |
   | `https://proxy.golang.org` | Go | every project with packs |
   | `https://sum.golang.org` | generic | every project with packs, unless your Go repository serves the checksum database itself |
   | `https://nodejs.org/dist` | generic | the web packs |
   | `https://registry.npmjs.org` | npm | the web packs |
   | `https://cdn.playwright.dev` | generic | web-core's browsers |
   | `https://api.nuget.org/v3/index.json` | NuGet | models-windows |

2. Set the variables on developers' machines and in CI, with the address your repository manager gives each repository. For example, in a shell profile:

   ```sh
   export AXX_DOWNLOAD_URL=https://repo.example.com/github/nimbusxr/axx/releases/download
   export AXX_GITHUB_URL=https://repo.example.com/github
   export GOPROXY=https://repo.example.com/go
   export GOSUMDB="sum.golang.org https://repo.example.com/go-sumdb"
   export NODE_MIRROR=https://repo.example.com/nodejs
   export npm_config_registry=https://repo.example.com/npm
   export PLAYWRIGHT_DOWNLOAD_HOST=https://repo.example.com/playwright
   export AXX_NUGET_URL=https://repo.example.com/nuget/index.json
   ```

   `AXX_DOWNLOAD_URL` names where the releases' files are: Axx looks for `<url>/v0.7.0/checksums.txt`, with the archives beside it. `AXX_NUGET_URL` names a NuGet feed's service index, as `nuget.config` names a feed.

   The Go settings can also be in Go's own configuration, where `go env -w` writes them: Axx reads them there too. `GOSUMDB` keeps the database's name, `sum.golang.org`, and adds the mirror's address after it, so Axx and the go command still check the database's signature. If your Go repository serves the checksum database itself, leave `GOSUMDB` unset: the go command, and Axx, find it through `GOPROXY`. To build packs with a Go installed on the machine instead of the one Axx downloads, set `AXX_GO` to its `go`.

3. Install Axx with the install script, naming the version:

   ```sh
   curl -fsSL https://raw.githubusercontent.com/nimbusxr/axx/main/scripts/install.sh | AXX_VERSION=0.7.0 sh
   ```

   With `AXX_DOWNLOAD_URL` set, the script downloads the release from the mirror. It needs `AXX_VERSION`, because finding the newest release asks GitHub's API. If your machines can't reach raw.githubusercontent.com, keep a copy of the script (`scripts/install.sh`, or `scripts/install.ps1` on Windows) where they can, and run that.

4. Upgrade by naming the version too: `axx upgrade --version 0.7.0`. A project that [names its version](/guides/install/#keep-a-project-on-one-version) gets that version from the mirror by itself.

Set the same variables in CI. The [container image](/guides/run-in-ci/) has Axx installed already, so it needs only the settings for what your projects use.

### Credentials

If the repositories need credentials, put a user name and an access token in the addresses, like `https://ci:TOKEN@repo.example.com/github`. Axx and the install scripts send them as Basic authentication, and leave them out of every message they print.

For `GOPROXY` and `GOSUMDB`, a `.netrc` file works too, as it does for the go command: Axx reads the user name and password `.netrc` has for the repository's host when it downloads Go over HTTPS. `PLAYWRIGHT_DOWNLOAD_HOST` is Playwright's setting, so how it takes credentials is Playwright's: see Playwright's documentation.

### A proxy server

If machines reach the internet through a proxy server, set `HTTPS_PROXY` to it, and `NO_PROXY` to the hosts to reach directly, like your repository manager. Axx's downloads go through it.

A proxy that inspects HTTPS traffic presents its own certificate, which the machine must trust. Axx uses the operating system's trusted certificates. On Linux, `SSL_CERT_FILE` can name a file of them.

## OpenH264

Scenarios keep no videos unless you turn them on (`videos`, under the desktop and mobile packs' settings). When they do, or when an Android emulator's camera plays a video, Axx encodes it with Cisco's OpenH264, which it downloads once from `ciscobinary.openh264.org`.

Cisco's license covers OpenH264 only when each machine downloads it from Cisco, so no mirror can serve it. Let your proxy server reach that host. If that isn't possible, keep `videos: never`, the default; an emulator's camera can still show pictures.

## Container images

Axx pulls no images itself: your project's compose files name them, and Docker pulls them. To pull them through your registry, put the registry's names in the compose files:

```yaml title="compose.yaml"
services:
  carriers:
    image: registry.example.com/ghcr/nimbusxr/axx-wiremock:0.1.5
```

Axx's own images are `ghcr.io/nimbusxr/axx` (Axx, for CI) and `ghcr.io/nimbusxr/axx-wiremock` (WireMock with Axx's extension, for [mocks](/guides/mock-dependencies/)). Both are on GitHub's container registry, `ghcr.io`, which a registry can mirror as it mirrors Docker Hub.

## Editors

The IntelliJ plugin comes from the JetBrains Marketplace. The IDE downloads it, so the IDE's own proxy settings apply, not Axx's. The plugin and the VS Code extension run the Axx installed on the machine, which downloads through your mirrors as above.

## Check what Axx downloads

For a security team that reviews what developers download:

- Each release lists its files' SHA-256 sums in `checksums.txt`, which the release workflow signs with Sigstore's cosign. To check the list, and then the files against it:

  ```sh
  cosign verify-blob checksums.txt --bundle checksums.txt.sigstore.json \
    --certificate-identity https://github.com/nimbusxr/axx/.github/workflows/release.yml@refs/heads/main \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com
  sha256sum --ignore-missing -c checksums.txt
  ```

- Every file in `checksums.txt` has GitHub's build provenance, which says what built it from which commit: `gh attestation verify axx_0.7.0_linux_amd64.tar.gz --repo nimbusxr/axx`. The install script checks it when `gh` is installed.
- Each archive has an SBOM beside it, like `axx_0.7.0_linux_amd64.tar.gz.sbom.json` (SPDX). It lists the Go modules in Axx. Scanners can also read them from the binary itself, as `go version -m axx` does.
- The packs' dependencies are Go modules that Axx downloads through `GOPROXY` when it prepares a project's packs. A repository manager that scans the modules it fetches scans them too. A project pins its packs' versions in `axx-packs.lock`.
- The WireMock image is signed with cosign, and has an SBOM and build provenance.
