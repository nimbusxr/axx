---
title: Stability
description: The stages a part of Axx goes through - experimental, stable, deprecated - what Axx promises at each, and how Axx tells you which one you are using.
---

A part of Axx is at one of three stages. The stage says what you can count on.

| Stage | What you can count on |
| --- | --- |
| **Experimental** | It works, and it may change or go in any release. |
| **Stable** | It keeps working as it does. What is replaced is deprecated first. |
| **Deprecated** | It works exactly as it did until the next major version, which drops it. |

Nearly everything in Axx is stable. This page is about the first stage. The [deprecation policy](/explanations/deprecation-policy/) covers the other two.

## Experimental packs

A new part of Axx arrives as a pack, so the stage is a mark on a pack. An experimental pack is one whose shape is not settled: Axx wants it used on real projects before it promises to keep it as it is.

You opt in by adding the pack. There is nothing else to set:

```sh
axx pack add controls-core
```

A project that does not add an experimental pack is not affected by it in any way.

## What is not promised yet

For an experimental pack, these may change or go between two releases of Axx, without being deprecated first:

- its steps and their text;
- its settings in `axx.yaml` (under `packs.<name>`);
- its commands, their flags, and what they print;
- the files it writes, and their format.

The [changelog](https://github.com/nimbusxr/axx/blob/main/CHANGELOG.md) says what changed.

## What stays as it is

An experimental pack changes none of what the rest of Axx promises:

- **Exit codes.** It adds none. A program that branches on Axx's exit codes sees the same ones.
- **The `--json` envelope.** It adds no field to what `axx run --json` prints.
- **The schema of `axx.yaml`.** A pack's settings sit under `packs`, which takes any pack's section.

## How Axx tells you

- **When you add it.** `axx pack add` says the pack is experimental.
- **When you use it.** `axx run`, and each of the pack's own commands, says so once, on standard error.
- **Where packs are listed.** `axx pack list`, `axx pack show <pack>` and the pack's [reference page](/references/packs/) mark it.

## When it becomes stable

A pack becomes stable by dropping the mark, in a release whose changelog says so. From then on the [deprecation policy](/explanations/deprecation-policy/) covers it.

Nothing in your project names the stage, so nothing in your project changes when a pack becomes stable.

## Your own packs

A pack of your own can be experimental too: set `Experimental: true` in its manifest, and Axx marks and announces it the same way ([Write custom steps](/guides/write-custom-steps/)).
