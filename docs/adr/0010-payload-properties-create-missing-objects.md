# 0010: Setting a payload property creates the objects its path lacks

- Status: Accepted
- Date: 2026-10-03

## Context

The request payload steps (`the request payload property … is …`, `the request payload
properties are:`) follow Jayway JsonPath (ADR 0007): a property is added to an object that
exists, and setting `recipient.name` in a payload without `recipient` fails with
`PathNotFoundException`. People write the table the way they read a payload, field by field:

```gherkin
And a request payload using an application/json empty content template
And the request payload properties are:
  | reference      | PX-1001     |
  | recipient.name | Ada Example |
```

In the agent evaluations this was the most frequent mistake that remained: 12 to 19 of 108
trials per run set a property inside an object the payload lacked, each costing a failed run
and a fix (a `| recipient | {} |` row, or another template), although axx's message said what to
do.

## Decision

- Setting a property whose path is plain names (`recipient.address.city`) creates each object
  of the path the payload lacks, as an empty object, then sets the property.
- A path with array indexes, wildcards or filters (`items[2].sku`) still fails when its parent
  is missing: there is no array to invent.
- Setting a property to null and removing it (`is null`, `undefined`) still need the property's
  parent: they change a property that exists.
- The recorded oracles stay frozen; the cases that change are listed in `oracletest.Deviations`
  with this ADR as their reason.

## Consequences

- A table reads as the payload it builds, and the step no longer fails for a missing object.
- A misspelled parent (`recipent.name`) creates a stray object instead of failing. The service,
  or the OpenAPI validation of the request, usually rejects that payload; a misspelled last name
  (`recipient.nmae`) was already added without an error.
- The payload steps differ from Jayway JsonPath on this one point, documented with the steps.
