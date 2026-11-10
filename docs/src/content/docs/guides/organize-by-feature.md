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

## Format the feature files

Every feature file can have the same layout, so that features read alike, whoever or whatever wrote them. `axx fmt` gives a file that layout. It changes only where things sit on the page: the indentation, the columns of tables and the blank lines between scenarios. The keywords, steps, names, table cells, comments and doc strings stay exactly as they are written. Before it writes a file, `axx fmt` reads what it would write, and it leaves the file alone if the two would not say the same thing.

1. Run `axx fmt` in the folder of `axx.yaml`. It formats the feature files that `axx run` reads, and names each file it changed. To format only some files, name them or their folders: `axx fmt features/shop-emails`.
2. In CI, run `axx fmt --check`. It changes nothing. It lists the files that are not formatted, and exits with `3` if there are any.
3. In your editor, format a feature file the usual way. The editor asks Axx, so you get the same layout as `axx fmt` gives ([Set up your editor](/guides/set-up-your-editor/#format-feature-files)).

A file written like this:

```gherkin
Feature: Shop emails
Scenario: A shop gets its parcel's label by email
  Given a POST request to /api/parcels
  When   the request is executed
  Then the response status code is 201
    And within 10s the shops mailbox has an email where:
    |to|orders@juniper-and-jay.example|
    |subject|Parcel PX-MAIL-9701 registered|
```

is formatted like this:

```gherkin
Feature: Shop emails

  Scenario: A shop gets its parcel's label by email
    Given a POST request to /api/parcels
    When the request is executed
    Then the response status code is 201
    And within 10s the shops mailbox has an email where:
      | to      | orders@juniper-and-jay.example |
      | subject | Parcel PX-MAIL-9701 registered |
```

The layout, in full:

- `Feature` starts at the left edge. A `Background`, `Scenario` or `Rule` is indented by two spaces, and what a `Rule` holds by two more.
- Steps and `Examples` are indented two spaces more than their scenario. A step's table or doc string, and the table of an `Examples`, are indented two more.
- The columns of a table line up. A character that shows twice as wide, as Chinese characters and most emoji do, counts as two columns.
- Tags are on their own lines, at the indent of what they tag. A description is indented two spaces more than its keyword.
- A comment is indented like the line below it. A comment with a blank line after it is indented like the line above it instead, as it belongs to that line.
- One blank line comes before each `Background`, `Scenario`, `Rule` and `Examples`, and before the tags and comments right above it. Other single blank lines stay where you put them, and several in a row become one.
- No line ends with spaces, except inside a doc string, and the file ends with one line break.

## Why

- **One folder holds the feature.** Its scenarios and everything they read are side by side: nothing to hunt for across `seeds/`, `requests/` and `screenshots/`.
- **Changes stay in one place.** A change to a feature touches its folder, and a review shows the scenarios with the data they read.
- **Data stays with its scenarios.** A seed in a feature's folder is that feature's: another feature does not pick it up by accident, which keeps [scenarios isolated](/explanations/scenario-isolation/). What features share sits apart, on purpose.
- **Deleting a feature deletes its data.** No seeds are left behind that nothing reads, and no file has to be checked for other users first.
- **Short paths and local names.** `seeds/wen.yaml` only has to be unique in its folder, not across the suite.
- **Agents read one folder.** An agent adding a scenario or fixing one finds the feature and its files together, and the next feature's folder shows it the pattern to follow.
- **A folder runs, moves and has an owner.** `axx run features/manifest-import` runs the feature; the folder moves to another suite whole; a `CODEOWNERS` line can give it to a team.
