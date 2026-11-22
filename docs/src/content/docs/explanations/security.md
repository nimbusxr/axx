---
title: Security
description: What you can and cannot expect from Axx in terms of security, and why - what Axx trusts, where its boundaries are, how its design keeps to secure design principles, and how common weaknesses are countered.
---

Axx starts the system under test on your machine and checks it from the outside. So it runs commands, downloads tools and talks to services, on purpose. This page says what you can expect of it while it does, what you cannot, and where in the code each statement holds. Each link goes to the file or the decision record that backs the sentence before it.

To report a vulnerability, follow [SECURITY.md](https://github.com/nimbusxr/axx/blob/main/SECURITY.md). Do not open a public issue.

## What you can expect

- **Axx runs what your project names, and nothing else.** A service's command, its cleanup, its readiness checks and the project's command hooks are the ones `axx.yaml` has ([SECURITY.md](https://github.com/nimbusxr/axx/blob/main/SECURITY.md)). A command runs as a list of arguments, without a shell, unless the service sets `shell` ([`internal/lifecycle/command.go`](https://github.com/nimbusxr/axx/blob/main/internal/lifecycle/command.go)).
- **What Axx downloads itself is checked before it is used.** The table in [Where the boundaries are](#where-the-boundaries-are) says how, for each kind of download. Each download can come from a mirror of your organization ([Use your organization's mirrors](/guides/use-mirrors/)).
- **npm packages are installed without their scripts.** The web packs' Node.js tools are installed from a lockfile. Each package is checked against the lockfile's SHA-512 integrity, and its install scripts never run ([`internal/npm/install.go`](https://github.com/nimbusxr/axx/blob/main/internal/npm/install.go)).
- **Releases are built and signed in CI.** The release workflow builds the binaries, signs the list of their checksums with Sigstore's cosign, attaches an SBOM to each archive and attests the build's provenance ([`release.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/release.yml), [`.goreleaser.yaml`](https://github.com/nimbusxr/axx/blob/main/.goreleaser.yaml)). [Check what Axx downloads](/guides/use-mirrors/#check-what-axx-downloads) has the commands that verify them.
- **The axx host starts only what a person approved.** `axx host` starts services, browsers, devices and apps for an agent's run in a sandbox. It runs a definition you approved, asks again when the definition changes, and takes an approval in its terminal or in the editor that started it, never over its port ([`internal/host/doc.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/doc.go), [ADR 0019](https://github.com/nimbusxr/axx/blob/main/docs/adr/0019-devices-and-apps-start-in-the-axx-host-once-approved.md)).
- **The REST pack verifies TLS certificates.** A request over HTTPS to a host whose certificate does not verify fails, unless `packs.rest.tls.verify` is `false` in `axx.yaml` ([`packs/rest/rest.go`](https://github.com/nimbusxr/axx/blob/main/packs/rest/rest.go)).
- **Secrets from the environment are masked.** A value a step takes from an `${env:..}` reference counts as a secret. The packs that use Axx's masking show it as `********` in failure messages, logs, attachments and reports, as it is and as it appears in URLs, forms, JSON and HTML ([`internal/secrets/secrets.go`](https://github.com/nimbusxr/axx/blob/main/internal/secrets/secrets.go)). `axx config show` masks the values of settings whose names say they are a password, a secret, a token or a key ([`internal/mcp/server.go`](https://github.com/nimbusxr/axx/blob/main/internal/mcp/server.go)). A mirror's address is printed without the credentials in it ([`internal/mirror/mirror.go`](https://github.com/nimbusxr/axx/blob/main/internal/mirror/mirror.go)).
- **A project can choose its Axx version, and nothing more.** The `axx:` line of `axx.yaml` names a version, never an address, so a repository can only choose among Axx's own releases ([ADR 0015](https://github.com/nimbusxr/axx/blob/main/docs/adr/0015-a-project-names-the-axx-version-it-runs-on.md)).
- **Axx sends no telemetry.** The [FAQ](/explanations/faq/#does-axx-collect-telemetry) lists every connection Axx makes by itself.

The last section, [What Axx does not protect you from](#what-axx-does-not-protect-you-from), is as much a part of this as the list above.

## What Axx trusts and what it does not

This section is the threat model: what is worth protecting, whom Axx trusts, and whom it does not.

What is worth protecting:

- your machine and your files;
- the credentials in your environment;
- the system under test and its data;
- the result of a run, which people and agents act on.

### What Axx trusts

All of this runs as you, with your privileges. Axx does not hold any of it back.

| Trusted | Why |
| --- | --- |
| The person who runs Axx | Axx is a command they start. |
| `axx.yaml`, its profiles and `axx.local.yaml` | They name the commands Axx runs ([SECURITY.md](https://github.com/nimbusxr/axx/blob/main/SECURITY.md)). |
| Feature files | Their steps send requests, read and write files, change data and, with the `cli` pack, run commands. |
| The project's packs | A pack is Go code. Axx builds itself with the packs `axx-packs.yaml` lists, and they run in its process ([ADR 0009](https://github.com/nimbusxr/axx/blob/main/docs/adr/0009-go-packs.md)). |

So a project is code. Opening a project and running `axx run` in it is like running its build: read a project you do not know first.

### What Axx does not trust

| Not trusted | What it could try | What stands in the way |
| --- | --- | --- |
| A download, and the mirror or network it comes through | Hand Axx a changed file | Each file is checked before use ([below](#where-the-boundaries-are)). |
| An agent's run in a sandbox, which asks the axx host for services, a browser, a device or an app | Get the host to run a command, read a file or reach an address outside the sandbox | The host runs only an approved definition and a fixed list of operations ([`internal/host`](https://github.com/nimbusxr/axx/blob/main/internal/host/doc.go), [ADR 0018](https://github.com/nimbusxr/axx/blob/main/docs/adr/0018-a-pack-has-a-side-in-the-axx-host.md), [ADR 0019](https://github.com/nimbusxr/axx/blob/main/docs/adr/0019-devices-and-apps-start-in-the-axx-host-once-approved.md)). |
| Other users of the machine | Connect to the host's port | The host listens on a loopback address and refuses a connection without its token. The token is in a file only your user can read ([`internal/host/server.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/server.go)). |
| What the system under test answers | Break the code that reads it | Answers are read by code written in Go, which is memory-safe. The readers of JSON, of file content and of `${...}` references are fuzzed ([`internal/compat/jsonx`](https://github.com/nimbusxr/axx/blob/main/internal/compat/jsonx/jsonx_test.go), [`internal/filecontent`](https://github.com/nimbusxr/axx/blob/main/internal/filecontent/filecontent_test.go), [`internal/interp`](https://github.com/nimbusxr/axx/blob/main/internal/interp/interp_test.go)). |
| A pull request to Axx from a fork | Reach the release's secrets, or the project's own runners | CI's workflows start with a token that only reads, and a job that needs more says so ([`ci.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/ci.yml)). A fork's pull request never runs on the project's self-hosted runners ([`mac-mini.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/mac-mini.yml)). Release secrets are in environments that only `main` can use ([CONTRIBUTING.md](https://github.com/nimbusxr/axx/blob/main/CONTRIBUTING.md#releases)). |

## Where the boundaries are

A trust boundary is a place where something Axx does not trust meets something it does. Axx has five.

### 1. Downloads

Axx downloads tools the first time something needs them. This is how each is checked.

| What | Checked against | Where |
| --- | --- | --- |
| Axx's own releases (a project's pinned version, `axx upgrade`, the install scripts) | The release's `checksums.txt` | [`internal/selfupdate`](https://github.com/nimbusxr/axx/blob/main/internal/selfupdate/selfupdate.go), [`scripts/install.sh`](https://github.com/nimbusxr/axx/blob/main/scripts/install.sh) |
| Axx's launcher on macOS | Its code signature: NimbusXR's Developer ID | [`packs/desktop/macos/launcher_darwin.go`](https://github.com/nimbusxr/axx/blob/main/packs/desktop/macos/launcher_darwin.go), [ADR 0016](https://github.com/nimbusxr/axx/blob/main/docs/adr/0016-a-macos-launcher-is-the-one-identity-a-person-allows.md) |
| Go, and the packs' Go modules | Go's checksum database, as the go command checks them | [`internal/gotool/sumdb.go`](https://github.com/nimbusxr/axx/blob/main/internal/gotool/sumdb.go), [ADR 0009](https://github.com/nimbusxr/axx/blob/main/docs/adr/0009-go-packs.md) |
| Node.js | A SHA-256 pinned in Axx | [`internal/npm/node.go`](https://github.com/nimbusxr/axx/blob/main/internal/npm/node.go) |
| npm packages (Playwright, Lighthouse, axe-core) | The SHA-512 integrity a lockfile pins | [`internal/npm/install.go`](https://github.com/nimbusxr/axx/blob/main/internal/npm/install.go) |
| WebDriverAgent and the UiAutomator2 server | A SHA-256 pinned in Axx | [`packs/mobile/ios/install.go`](https://github.com/nimbusxr/axx/blob/main/packs/mobile/ios/install.go), [`packs/mobile/android/uia2.go`](https://github.com/nimbusxr/axx/blob/main/packs/mobile/android/uia2.go) |
| JaCoCo and coverage.py | A SHA-256 pinned in Axx | [`packs/coverage/jvm/jvm.go`](https://github.com/nimbusxr/axx/blob/main/packs/coverage/jvm/jvm.go), [`packs/coverage/python/python.go`](https://github.com/nimbusxr/axx/blob/main/packs/coverage/python/python.go) |

The [mirrors guide](/guides/use-mirrors/#what-axx-downloads) lists every download and its mirror setting. Two things on that list Axx does not check itself: the browsers, which Playwright downloads, and container images, which Docker pulls.

### 2. The axx host's port

`axx host` runs on your machine, outside an agent's sandbox, and the agent's run reaches it over a loopback port. What crosses that port:

1. The run gives the token from `.axx/host.json`. The host compares it in constant time and refuses a run without it ([`internal/host/server.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/server.go)).
2. The run names services. The host never runs a command a run sends: it runs what the approved definition says ([`internal/host/doc.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/doc.go)).
3. If the definition differs from the one you approved, the run waits for your answer. The host shows the difference, and warns about what in it reaches past a container onto your machine, such as the Docker socket or a mount of a file outside the project.
4. Approvals are kept in your own configuration directory, outside the project, in a file only your user can read ([`internal/host/approvals.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/approvals.go)).

### 3. The browser the host starts

A browser outside the sandbox could be a way out of it. Three things keep it from being one ([ADR 0018](https://github.com/nimbusxr/axx/blob/main/docs/adr/0018-a-pack-has-a-side-in-the-axx-host.md)):

- **The host starts the browser with its own options.** The run names an engine, and nothing else of how the browser starts.
- **The browser's network goes through the run** ([`packs/web/internal/relay`](https://github.com/nimbusxr/axx/blob/main/packs/web/internal/relay/relay.go)). The browser reaches what the sandbox lets the run reach.
- **Playwright's server gets only known commands.** A guard stands between the run and Playwright's server. A command passes only when the rules name it and every parameter in it. A command or a parameter that a later Playwright adds is refused until someone has looked at it ([`packs/web/internal/guard/guard.go`](https://github.com/nimbusxr/axx/blob/main/packs/web/internal/guard/guard.go), [`rules.json`](https://github.com/nimbusxr/axx/blob/main/packs/web/internal/guard/rules.json)).

### 4. Devices and apps the host starts

An app is different from a browser: it is code the agent writes, and it runs outside the sandbox. So the boundary is the person ([ADR 0019](https://github.com/nimbusxr/axx/blob/main/docs/adr/0019-devices-and-apps-start-in-the-axx-host-once-approved.md)):

- The host asks you about each app before it resets, installs or starts anything of it, and asks again when the app's registration changes.
- Beyond the app, a run asks only for what a step does, by name, from a fixed list ([`packs/internal/hostwire`](https://github.com/nimbusxr/axx/blob/main/packs/internal/hostwire/hostwire.go)). It names no program and passes no argument to one.
- The files a run names must be in the project's repository. The host follows links before it decides ([`Suite.Confine` in `core/suite.go`](https://github.com/nimbusxr/axx/blob/main/core/suite.go)).

[Run tests from an agent's sandbox](/guides/run-from-a-sandbox/#what-your-security-team-should-know) says what each kind of app reaches.

### 5. The release

What you install was built in CI, not on a person's machine ([`release.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/release.yml)):

- Release secrets are in two GitHub environments that only `main` can use, and merging the release pull request is the approval that publishes ([CONTRIBUTING.md](https://github.com/nimbusxr/axx/blob/main/CONTRIBUTING.md#releases)).
- The checksums are signed with cosign's keyless signing, so there is no long-lived signing key for them ([ADR 0004](https://github.com/nimbusxr/axx/blob/main/docs/adr/0004-versioning-and-release.md)).
- The macOS binaries and the launcher are signed with NimbusXR's Developer ID and sent to Apple's notary service, on a runner GitHub hosts ([ADR 0016](https://github.com/nimbusxr/axx/blob/main/docs/adr/0016-a-macos-launcher-is-the-one-identity-a-person-allows.md)).
- The workflows' actions are pinned by commit, and every workflow is audited by zizmor in CI ([`ci.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/ci.yml)).

## How the design keeps to secure design principles

The principles are Saltzer and Schroeder's. Each row says what the principle asks, in a few words, and where Axx applies it.

| Principle | Where Axx applies it |
| --- | --- |
| **Keep it small** (economy of mechanism) | The `axx` binary is the core alone. No pack, and no pack's dependency, is in it unless the project lists the pack ([ADR 0009](https://github.com/nimbusxr/axx/blob/main/docs/adr/0009-go-packs.md)). A run and the host exchange one JSON object per line over one connection ([`internal/host/protocol.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/protocol.go)). |
| **Refuse unless allowed** (fail-safe defaults) | The guard refuses every browser command its rules do not name ([`guard.go`](https://github.com/nimbusxr/axx/blob/main/packs/web/internal/guard/guard.go)). At the host's question, Enter means no, and a host that has no terminal refuses every change ([sandbox guide](/guides/run-from-a-sandbox/#what-you-approve)). TLS certificates are verified unless a setting says otherwise ([`packs/rest/rest.go`](https://github.com/nimbusxr/axx/blob/main/packs/rest/rest.go)). The schema of `axx.yaml` refuses keys it does not know ([`axx.schema.json`](https://github.com/nimbusxr/axx/blob/main/schemas/v0/axx.schema.json)). |
| **Check every time** (complete mediation) | The host checks the token on every connection ([`server.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/server.go)), and a run whose definition differs from the approved one waits for an answer ([`doc.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/doc.go)). The guard checks every command, and every parameter in it to any depth ([`guard.go`](https://github.com/nimbusxr/axx/blob/main/packs/web/internal/guard/guard.go)). |
| **Nothing rests on secrecy of the design** (open design) | Axx is open source under Apache-2.0. Its decisions are public records ([Architecture decisions](/explanations/adrs/)), and so are the guard's rules. |
| **Two conditions, not one** (separation of privilege) | The host starts something for a run only when the run has the token and a person approved the definition. A release needs the merged release pull request and an environment that only `main` can use ([CONTRIBUTING.md](https://github.com/nimbusxr/axx/blob/main/CONTRIBUTING.md#releases)). |
| **Only the access a job needs** (least privilege) | Workflows start with a token that only reads, and a job that needs more says so ([`ci.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/ci.yml), [`release.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/release.yml)). The container image is distroless and runs as a user that is not root ([`build/Dockerfile`](https://github.com/nimbusxr/axx/blob/main/build/Dockerfile)). A hosted run gets a browser, and nothing else of the machine ([ADR 0018](https://github.com/nimbusxr/axx/blob/main/docs/adr/0018-a-pack-has-a-side-in-the-axx-host.md)). |
| **Share as little as possible** (least common mechanism) | Each scenario has its own state and its own secrets ([`internal/secrets/secrets.go`](https://github.com/nimbusxr/axx/blob/main/internal/secrets/secrets.go), [Scenario isolation](/explanations/scenario-isolation/)). Each browser the host starts has a profile and a clipboard of its own, and the profile is deleted when the run ends ([sandbox guide](/guides/run-from-a-sandbox/#what-the-run-gets-and-what-it-doesnt)). |
| **Easy to use safely** (psychological acceptability) | The host's question shows the difference and names what in it reaches your machine, so that a person can answer it by reading it. The approval of an app is for its registration, not for each build: a question on every run would be answered without reading ([ADR 0019](https://github.com/nimbusxr/axx/blob/main/docs/adr/0019-devices-and-apps-start-in-the-axx-host-once-approved.md)). A certificate that does not verify fails with the name of the setting ([`packs/rest/unverified.go`](https://github.com/nimbusxr/axx/blob/main/packs/rest/unverified.go)). |

## How common weaknesses are countered

The weaknesses are named as the CWE list names them.

| Weakness | How Axx counters it |
| --- | --- |
| **Memory errors** (CWE-787, CWE-125, CWE-416) | Axx is written in Go, which is memory-safe, and its binaries are built without cgo ([`.goreleaser.yaml`](https://github.com/nimbusxr/axx/blob/main/.goreleaser.yaml)). The exception is the small libraries of C, C++ and Objective-C that some packs load into apps under test. Those for Apple's platforms can be built under AddressSanitizer for Axx's own tests ([`packs/internal/memcheck`](https://github.com/nimbusxr/axx/blob/main/packs/internal/memcheck/memcheck.go)). See the limits below. |
| **Command injection** (CWE-78) | A command runs as a list of arguments, without a shell, unless the project asks for one ([`internal/lifecycle/command.go`](https://github.com/nimbusxr/axx/blob/main/internal/lifecycle/command.go)). The host runs nothing a run sends, and a run names no program ([ADR 0019](https://github.com/nimbusxr/axx/blob/main/docs/adr/0019-devices-and-apps-start-in-the-axx-host-once-approved.md)). |
| **Path traversal** (CWE-22) | Unpacking an archive leaves out links and refuses an entry that would leave its folder ([`internal/npm/archive.go`](https://github.com/nimbusxr/axx/blob/main/internal/npm/archive.go), [`internal/docsite/docsite.go`](https://github.com/nimbusxr/axx/blob/main/internal/docsite/docsite.go)). The host refuses a file outside the project's repository, once links are followed ([`core/suite.go`](https://github.com/nimbusxr/axx/blob/main/core/suite.go)). |
| **Code downloaded without a check** (CWE-494) | See [Downloads](#1-downloads). npm install scripts never run. |
| **Certificates not verified** (CWE-295) | The REST pack verifies them by default. Axx's downloads use the operating system's trusted certificates ([mirrors guide](/guides/use-mirrors/#a-proxy-server)). |
| **A local port without authentication** (CWE-306) | The host's port takes a token. For coverage, Axx opens no port and no inspector: a service writes into a folder ([ADR 0017](https://github.com/nimbusxr/axx/blob/main/docs/adr/0017-a-run-measures-the-coverage-of-the-system-under-test.md)). |
| **Untrusted data turned into objects** (CWE-502) | What crosses a boundary is JSON, read into fixed types: a run's request ([`protocol.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/protocol.go)) and a browser's command, which the guard checks name by name. |
| **Secrets in logs** (CWE-532) | Masking, as [above](#what-you-can-expect). |
| **Secrets in the repository** (CWE-798) | CI scans the tree and the whole history with gitleaks ([`ci.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/ci.yml), [`scripts/hygiene.sh`](https://github.com/nimbusxr/axx/blob/main/scripts/hygiene.sh)). |
| **Files others can read** (CWE-732) | The host's token and its approvals are written for your user only ([`server.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/server.go), [`approvals.go`](https://github.com/nimbusxr/axx/blob/main/internal/host/approvals.go)). |
| **Weak hashes** (CWE-328) | Downloads are checked with SHA-256 or SHA-512. MD5 and SHA-1 exist only as fixture functions, for a value in a format the system under test has ([`internal/fixtures/functions.go`](https://github.com/nimbusxr/axx/blob/main/internal/fixtures/functions.go)). The host's token comes from the system's random source. |
| **Races** (CWE-362, CWE-367) | The Go tests run under the race detector in CI ([`ci.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/ci.yml)). Two races between a check and a use are known and listed below. |
| **Dependencies with known vulnerabilities** | govulncheck runs on pull requests that change Go and on `main` every night ([`govulncheck.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/govulncheck.yml)). A pull request that adds a dependency with a known vulnerability is blocked ([`dependency-review.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/dependency-review.yml)). |

Tools look for what the table misses. CodeQL analyzes every language in the repository, on pull requests and every night ([`codeql.yml`](https://github.com/nimbusxr/axx/blob/main/.github/workflows/codeql.yml)). golangci-lint runs the linters in [`.golangci.yml`](https://github.com/nimbusxr/axx/blob/main/.golangci.yml). actionlint and zizmor check the workflows.

## What Axx does not protect you from

- **A project you run.** `axx.yaml`, feature files, command hooks and the project's packs are code, and they run as you. Axx does not sandbox them.
- **An app you approved.** An app the host starts is not held to the agent's sandbox: in the iOS Simulator, and as a Mac, Windows or Linux app, it runs as you, with your files and your network. The approval is the control ([sandbox guide](/guides/run-from-a-sandbox/#the-host-asks-you-about-each-app)).
- **What containers reach.** The services' code runs in containers with what the approved compose file gives them. They reach the network unless the compose file keeps them from it.
- **A changed checksum list.** Axx checks a release's archive against that release's `checksums.txt`, which comes from the same place as the archive. Axx does not check the list's signature itself: [you can](/guides/use-mirrors/#check-what-axx-downloads), with cosign. The install script checks the build's provenance when `gh` is installed.
- **Browsers and container images.** Playwright downloads the browsers and Docker pulls the images. Axx checks neither.
- **A service whose certificate you told Axx to accept.** With `packs.rest.tls.verify: false`, the REST pack accepts any certificate.
- **Other programs that run as you.** They can read the host's token, as the agent's run does.
- **Two moments.** The host checks where a file is and then reads it. An agent that swaps the file for a link between the two could have the host read a file outside the repository. The host would still do with it only what the step does ([ADR 0019](https://github.com/nimbusxr/axx/blob/main/docs/adr/0019-devices-and-apps-start-in-the-axx-host-once-approved.md)). The same holds for the files the host compares before it restarts ([ADR 0015](https://github.com/nimbusxr/axx/blob/main/docs/adr/0015-a-project-names-the-axx-version-it-runs-on.md)).
- **Everything a secret can appear in.** Masking is done pack by pack, by the packs that use it, for the values Axx knows to be secrets. Screenshots and videos show the page as it is ([How the web-core pack works](/explanations/web-browsers/#secrets)).
- **The coverage folder.** The folder a measured service writes its coverage into is writable by every user of the machine, because a container's processes often run as a user of their own ([ADR 0017](https://github.com/nimbusxr/axx/blob/main/docs/adr/0017-a-run-measures-the-coverage-of-the-system-under-test.md)).
- **Memory errors in every native library.** The libraries for Apple's platforms can be built under AddressSanitizer. The C++ libraries for Android and Windows cannot yet, and no CI job runs the check as a matter of course.
- **Old releases.** During the `0.x` beta, only the latest release gets security fixes ([SECURITY.md](https://github.com/nimbusxr/axx/blob/main/SECURITY.md)).
