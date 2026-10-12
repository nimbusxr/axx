---
title: Organize a suite by feature
description: Give each feature a folder of its own and keep in it everything only that feature uses, its seeds, payloads, messages, screenshots and fixture factories, so one folder holds the whole feature.
---

A feature is what people read, change, run and delete as one. Give each feature a folder named after it, and keep in that folder the files only that feature uses: the seeds it starts from, the payloads it sends, the messages it publishes, the files it uploads or expects, its screenshots, and the fixture factories that generate any of them. Someone working on the feature opens one folder and has all of it.

```
acceptance/
  axx.yaml
  features/
    manifest-import/
      manifest-import.feature          one file, the same on every platform
      seeds/manifest-kestrel.yaml
      seeds/manifests/xml/             a fixture factory, with the seeds it generates
    courier-app/
      courier-app-android.feature      one file for each platform, when they differ
      courier-app-ios.feature
      seeds/                           what both platforms start from
      routes/grimmaische-strasse.gpx
    shop-portal/
      shop-portal.feature
      screenshots/quote.chromium-linux.png
  kafka/                               what several features share
  schemas/
infra/                                 the services' own setup: compose, WireMock mappings
```

That is the [parcels example](https://github.com/nimbusxr/axx/tree/main/examples/parcels/acceptance), laid out this way.

## One file, or one for each platform

A feature that is the same on every platform is one file named after the feature, `manifest-import.feature`, whichever platforms run it: a browser, a desktop or a phone is chosen by its tags, a profile or the run.

When the platforms differ, with screens of their own and steps that differ, each platform gets its own file in the feature's folder, named after the feature and the platform: `courier-app-android.feature` and `courier-app-ios.feature`. Both read the folder's files. A feature on one platform only says so the same way: `incidents-android.feature`. File names stay unique across the suite, so a search for `courier-app-ios` finds one file, not one of several `ios.feature`.

## How steps find a feature's files

A step that takes a file looks for it next to its feature file first, then in the `resources` directories of `axx.yaml`, then in the directory of `axx.yaml`. So the feature names its files from its own folder, and its paths stay short:

```gherkin title="features/manifest-import/manifest-import.feature"
Given a seeds/manifest-kestrel.yaml db seed
```

A file several features use goes in a folder of its own outside `features/` (`kafka/`, `schemas/`), where every feature finds it. Moving a file there is a decision you can see in a review, not an accident of where it was first written.

Screenshots follow the same rule: a check keeps its screenshot in a `screenshots` folder next to the feature file, unless `axx.yaml` sets a folder (`packs.web-screenshots.folder` and the other packs' `screenshots.folder`). A screenshot kept in the project's `screenshots` folder before is still found there.

## What stays outside the features

What the services under test read is their setup, not a feature's data: the WireMock mappings and the bodies WireMock serves, compose files, database migrations and init scripts. They live with the services (in the parcels example, `infra/`), even when one feature is the only one that needs a mapping. The feature's own files are the ones its steps name.

## Why

- **One folder holds the feature.** Its scenarios and everything they read are side by side: nothing to hunt for across `seeds/`, `requests/` and `screenshots/`.
- **Changes stay in one place.** A change to a feature touches its folder, and a review shows the scenarios with the data they read.
- **Data stays with its scenarios.** A seed in a feature's folder is that feature's: another feature does not pick it up by accident, which keeps [scenarios isolated](/explanations/scenario-isolation/). What features share sits apart, on purpose.
- **Deleting a feature deletes its data.** No seeds are left behind that nothing reads, and no file has to be checked for other users first.
- **Short paths and local names.** `seeds/wen.yaml` only has to be unique in its folder, not across the suite.
- **Agents read one folder.** An agent adding a scenario or fixing one finds the feature and its files together, and the next feature's folder shows it the pattern to follow.
- **A folder runs, moves and has an owner.** `axx run features/manifest-import` runs the feature; the folder moves to another suite whole; a `CODEOWNERS` line can give it to a team.
