# 0008: Publish every component from release.yml, gated on release-please outputs

- Status: Accepted
- Date: 2026-09-24
- Amends: [0004](0004-versioning-and-release.md)

## Context

The repository ships three release components: the CLI, the WireMock extension image and the
IntelliJ plugin. release-please creates their tags through the GitHub API, and whether such tags
start `on: push: tags` workflows depends on the token and on how the tag is created.

## Decision

- `release.yml` runs release-please and then publishes exactly the components that were
  released, using release-please's per-path outputs (`<path>--release_created`,
  `<path>--tag_name`, `<path>--version`).
- Components may publish through reusable workflows called from `release.yml` (the GHCR
  image, the JetBrains Marketplace).
- Component workflows build and test on pull requests and `main` only; they never publish on
  tag pushes.
- Every component's first release is `0.1.0`: the manifest starts at `0.0.0`, and the first
  commit carries a `Release-As: 0.1.0` footer, which release-please applies once.
- Nothing publishes unless the `AXX_PUBLISH` repository variable is `true`. Otherwise
  `release.yml` runs as a dry run: release-please reports what it would do, and every
  component is built, checked and attached to the run. Pull requests that touch release
  files and manual runs are always dry runs.

## Consequences

- One place shows what a release publishes, and a release never silently skips a component.
- Re-publishing a failed component means re-running the release workflow's job, not pushing a
  tag.
