---
title: Deprecation policy
description: What Axx promises about the things you build on - steps, commands and flags, axx.yaml settings, exit codes and error codes - when one of them is replaced.
---

Your suite builds on Axx's steps, its commands and flags, the settings of `axx.yaml`, and its exit codes and error codes. This page says what happens when one of them is replaced.

A part that is new and may still change is marked experimental, and is not covered by this page until it is stable: [Stability](/explanations/stability/) says what that means.

## The rule

1. **Nothing is removed within a major version.** A suite that runs on Axx `1.0` runs on every `1.x`.
2. **What is replaced is deprecated first.** A deprecated step, flag or setting works exactly as it did, and Axx tells you where you use it and what to use instead.
3. **The next major version drops everything that is deprecated.** There is no waiting time: deprecated in `1.4`, gone in `2.0`, whenever `2.0` comes.

So one thing keeps a suite ready for the next major version: use nothing that Axx reports as deprecated.

## How Axx tells you

- **Steps.** `axx lint`, `axx validate` and `axx run` warn about each deprecated step a feature uses, with the step that replaces it ([AXX-E0839](/references/error-codes/#axx-e0839)). It is a warning: the scenario runs. `axx steps show <id>` and the step's reference page say it is deprecated too.
- **Commands and flags.** A command warns on standard error when it is given a deprecated flag, and names the flag to use.
- **Settings.** Loading an `axx.yaml` that has a deprecated setting warns, and names the setting to use. Editors mark it from the schema.

A project's own packs can deprecate their steps the same way (`DeprecatedBy` on the step), and get the same warning.

## What never changes

Some things have no deprecation, because programs branch on them:

- **Exit codes** keep their numbers.
- **Error codes** (`AXX-E…`) keep their meaning. A code Axx stops reporting is retired, and is never used for something else.
- **The `--json` envelope** only gains fields. Removing a field, or changing its type, takes a new `schemaVersion`.
- **The schemas of `axx.yaml` and the fixture spec files** only grow: a file a schema accepted is one it goes on accepting.

## Before 1.0

Axx is in beta. Until `v1.0.0`, a minor version (`0.7` to `0.8`) may remove or change something without deprecating it first; the [changelog](https://github.com/nimbusxr/axx/blob/main/CHANGELOG.md) says so. The rule above holds from `v1.0.0` on.
