# 0015: A project names the axx version it runs on

- Status: Accepted
- Date: 2026-11-04

## Context

axx changes in ways a project can feel: before 1.0 a minor version may change what a feature's values
mean, as 0.3.0 did. A project had no say in this. Its build with its packs is made from whichever axx
is installed, so upgrading axx changes every project on the machine at once, people on one commit can
get different results, and the workflow `axx init` writes takes the newest release on every run, so a
release can break a project's CI with no change in the project. Someone who hits this would not know
that going back to a version is the way out, nor which one.

## Decision

`axx.yaml` may name one release: `axx: 0.5.0`. `axx init` writes it for new projects, and without it a
project runs on the installed axx, as before.

**The installed axx only hands over.** Before it parses anything (the version may have flags and
commands it does not know), it reads the line, gets that release into its cache (`versions/<version>`)
and replaces itself with it. From there the project's version does everything, its pack build
included, with its own list of packs and its own Go. Every command follows the project except those
about the installed axx or asked for at every Tab: `upgrade`, `install`, `version`, `completion`.
`axx lsp`'s router also stays, as it serves many projects; the server it starts for each project hands
over for that project.

**A project's version comes from GitHub, however axx was installed.** Axx's Homebrew tap keeps only the newest version, so it cannot
supply the one a project names. `axx upgrade` still upgrades the installed axx the way it was installed: with `brew` or `go install`, or by
replacing itself.

**It is exact.** A minimum would let a newer axx run the project, which is what this is here to
prevent. Only `axx upgrade` moves the line, and it gets the release first, says what changed, and
checks the project with `validate` and `lint` on the new version. It leaves the line for the person to
commit, and takes an older installed axx along.

**The download is automatic.** Agents, CI and editors cannot answer a question or run a setup step,
and someone new to a project should need nothing beyond an axx. `AXX_AUTO_DOWNLOAD=0` opts out: axx then
stops and says to run `axx install`. A release comes from GitHub, checked against its checksums, and
`AXX_DOWNLOAD_URL` names a mirror for networks that cannot reach GitHub. The line can only name a
version, never an address, so a repository can only choose among axx's own releases.

**The line is read as a line,** not through the schema: the rest of the file follows the named
version's schema, which the installed axx may not know. Only the base `axx.yaml` counts, and a profile
or `axx.local.yaml` that changes it is an error rather than something that silently does nothing. To
try another version, `AXX_USE_VERSION` says so for a run.

**A reminder stays out of the way.** A pin that never moves stops getting fixes, so after an `axx run`
that passes, for a person at a terminal, one line says a newer release is out, at most once a week. The
list of releases is read at most once a day in the background, with no credentials, and whatever goes
wrong stays silent. `AXX_NO_UPGRADE_REMINDER` turns it off; it never runs for CI, an agent or `--json`.
`axx doctor` says the same from what the reminder last read.

## Alternatives considered

- **The installed axx builds the named version.** It already builds a project's copy from the axx
  module, so this needed no download. But it would use its own list of packs and its own Go
  (`GOTOOLCHAIN=local`): a pack added or renamed between the versions, or a version that needs a newer
  Go, breaks the build, and that is exactly the person new to a project with an install that differs
  from the pin.
- **Shell switching, as nvm and SDKMAN do.** They change the shell's PATH, on request or from a hook.
  Agents, CI and editors never go through that shell, and a missing version stays a manual step. Go's
  toolchains, rustup and Bazelisk fit: the one command on the PATH reads the project on every command.
- **A file of its own, as `.nvmrc` and `.bazelversion`.** It would save reading `axx.yaml`, but versions
  before pins would ignore it and run the project on the wrong version without a word, while they refuse
  the key. One more file also says less than one line at the top of the project's configuration.

## Consequences

- Upgrading the installed axx no longer changes a pinned project, nor costs it a rebuild: its build
  belongs to the release, not to the installed file.
- `setup-axx` and the install scripts need no change: what they install hands over.
- Reading the line, the versions directory, the archive names and the hand-over are now a contract
  between versions: an old axx must be able to hand over to any later one.
- 0.4.0 and older refuse the line and cannot be named, so pins start with the release after it. Someone
  on those versions upgrades before opening a pinned project; that cannot be fixed in them.
- Each version on a machine costs a download of about 12 MB, plus a build per set of packs, in the
  cache, which is safe to delete.
- The editor plugins' schemas are the plugin's version, which can be one off the project's.
