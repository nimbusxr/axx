---
title: Step design
description: Why Axx steps are shaped the way they are - variants, parameter types, tables versus arguments - and why step text never changes.
---

The steps of Axx's packs follow a few rules, so once you know one pack you can guess the others, and an agent can too.

## One step, several variants

Most steps come in up to four forms: the plain step, a form that names the service, a form with an ordinal for the 2nd or 3rd request, selection or event, and both together. The plain form covers the common case, a scenario with one service and one request, so most scenarios read like the acceptance criterion. The other forms appear only when a scenario needs them, and they read the same way in every pack ([How steps read](/references/steps/) has the grammar):

```gherkin
Scenario: A reference can only be registered once
  Given a 1st ordered POST request to /api/parcels
  And a request payload using an application/json content example for 1st ordered request
  And the request payload property reference is 'PX-REG-1004' for 1st ordered request
  And a 2nd ordered POST request to /api/parcels
  And a request payload using an application/json content example named 'Express parcel' for 2nd ordered request
  And the request payload property reference is 'PX-REG-1004' for 2nd ordered request
  When the 1st ordered request is executed
  And the 2nd ordered request is executed
  Then the 1st ordered response status code is 201
  And the 2nd ordered response status code is 409
```

## Parameter types

Besides Cucumber's built-in types (`{int}`, `{word}`, `{string}`, ...), Axx adds types that let steps read naturally: `a 2nd selection`, `within 5s`, `on parcels`. `{filepath}` marks a value that names a file, such as a seed or a schema, so editors can link it to the file. They are listed in [How steps read](/references/steps/#parameter-types).

## Tables for many values, arguments for one

Steps that set or check one value take it inline (`the request header Accept is 'application/json'`); their plural twins take a two-column table (`the request headers are:`). Tables use JSONPath keys for payloads, so nested and array values need no extra steps.

## Step text is public API

Once a step is released, its text never changes. New behavior gets new steps; old steps can be deprecated with a pointer to the replacement, never renamed. That is what keeps feature files working across releases, and what lets an agent trust `axx steps search` today and next year. Every built-in step is checked against a frozen catalog of step text on every build.

## Designing your own steps

The same rules make [custom steps](/guides/write-custom-steps/) easy to use:

- Give every step a stable `id`, a doc string and a complete example line.
- Add an `on {service}` variant when a step talks to a service that can appear twice.
- Report failed expectations with expected and actual values, not just a message.
