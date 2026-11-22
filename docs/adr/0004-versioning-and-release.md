# 0004: Versioning and release: v0.x pre-releases via release-please + GoReleaser

- Status: Accepted
- Date: 2026-09-23

## Decision

- Versions start fresh at `v0.1.0` (the Go module path is new, so no `/vN` suffix). Every `0.x`
  release is marked as a GitHub pre-release until `v1.0.0`.
- release-please (manifest mode) maintains a release PR from conventional commits; merging it
  creates the tag and release. Pre-1.0, breaking changes bump the minor version and features
  bump the patch version.
- GoReleaser runs in the same workflow when a release is created, attaching signed (cosign
  keyless), SBOM'd, provenance-attested binaries for linux/darwin/windows × amd64/arm64, GHCR
  images and the Homebrew cask `nimbusxr/tap/axx`.
- Because `releases/latest` skips pre-releases, install scripts and `setup-axx` resolve
  versions via the releases list API.
- What ships beside axx (the WireMock extension, the IntelliJ plugin, the VS Code extension) are
  separate release-please components with their own tags, versioned as below.
- A rolling `nightly` pre-release is built from `main`.

## Versions of what ships beside axx (2026-11-21)

The WireMock extension and the two editor plugins counted versions of their own (`0.1.7`,
`0.3.12`), and nothing in them said which axx they were for. Their versions now do:

- **`<axx major>.<axx minor>.<stamp>`**, `0.7.2026112101`: the first two numbers are axx's; the
  third is the day of the release and which release of that day it was, as one number. It is a
  stamp, not a patch number: a third number that counted beside axx's would read as one to match.
- A component that changed is released with a new stamp. When axx moves to another major or
  minor, every component is released for it in the same release PR, changed or not.
- One form for every store: the VS Code Marketplace takes three numbers and no suffix, and the
  stamp is a valid third number everywhere (the image's tag, the jar, both plugin stores). It
  fits 32 bits, and versions sort as they were released.
- release-please keeps the release PR, the changelogs and the tags. A plugin of ours
  (`scripts/release-please/stamp.mjs`) gives it these versions before it plans the PR, so
  `release.yml` runs release-please as a library (`release.mjs`) where it ran the action, which
  cannot load a plugin.
- The WireMock extension's folder, tags and jar are named after its image: `extensions/axx-wiremock`,
  `axx-wiremock-v<version>`, `axx-wiremock-<version>-all.jar`. Earlier tags are
  `wiremock-openapi-v<version>`.

## What "frozen" means for a schema (2026-11-21)

The schemas of axx.yaml and the fixture spec files are a contract with every project's files: a
file a schema accepted is one it goes on accepting. A schema may grow (a new property, a new
value of an enum, a looser limit, another form of a value) and may not take back what a file
could say (a property gone or of another type, a value of an enum gone, a tighter limit, a
property newly required).

`TestSchemaContract` holds each schema to its frozen copy in `testdata/schemas`, by that rule
(`internal/schemacompat`). What a schema gains is frozen with `-update`, which refuses a change
that breaks. Until `v1.0.0` the owner may decide on one, with `-break`; from `v1.0.0` on, one
needs a new major version, as the criteria below say.

## GA criteria for v1.0.0

The `axx.yaml` v1 schema, CLI JSON/exit-code contract and deprecation
policy are frozen, and agent-eval success meets its target.

## Deprecation policy (2026-11-21)

- Nothing is removed within a major version.
- What is replaced is deprecated first: a deprecated step, flag or setting works as it did, and
  axx warns where it is used, with what to use instead.
- The next major version drops everything deprecated. No waiting time: deprecated is deprecated.
- Exit codes, error codes, the `--json` envelope and the schemas have no deprecation: they are
  frozen (ADR 0003, and the schemas' section here), and tests hold them.
- Until `v1.0.0`, a minor version may remove or change something without it.

What is new and may still change is not under the policy yet: a pack can be experimental
(ADR 0021), and the policy covers it from the release in which it drops that mark.

The policy is published as the docs' "Deprecation policy" page. A deprecated step warns today
(`axx lint`'s deprecated steps check, AXX-E0839, in `axx validate` and `axx run` too).
