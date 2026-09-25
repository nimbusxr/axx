# 0007: Value semantics of the established Java libraries, verified by recorded oracles

- Status: Accepted
- Date: 2026-09-24

## Context

Many step outcomes depend on value semantics rather than on step text: JSONPath evaluation
(paths, filters, `length()`, deep scans, update errors), JSON parsing and serialization, regular
expressions (possessive quantifiers, `\Q..\E`, character classes), number formatting and the
coercion of table values. The dialects teams already know best are those of Jayway JsonPath,
json-smart, `java.util.regex` and Jackson. A Go library that is merely "close" produces tests
whose outcome depends on edge cases nobody chose, which users experience as flakiness.

We measured the obvious Go JSONPath library (ojg) against recorded Jayway results: it agreed on
68% of read cases (no `length()`/`keys()`, no `nin`/`size`/`empty`, different deep-scan order,
different string/number comparison in filters, no update errors).

## Decision

- `internal/compat` implements these semantics itself rather than wrapping Go look-alikes:
  `jsonx` (Jayway JsonPath 2.9.0 compiler, tokens, filters and updates, plus the json-smart
  parser and writer), `javare` (the JDK 21 regex parser, translated to `dlclark/regexp2` with
  explicit character sets and anchors), `javafmt` (Java number formatting and parsing) and
  `jvalue` (the coercion of step values built on them).
- Behaviour is pinned by **oracles**: about 23,000 recorded cases (`testdata/oracles`),
  replayed by Go tests. Differences are allowed only through one table
  (`oracletest.Deviations`), with a reason each.
- Packs use `internal/compat` for every user-visible comparison, path lookup and regex match.

## Consequences

- More code to own than a dependency, but the behaviour is proven rather than assumed, and the
  oracles make regressions visible.
- The oracle files are frozen expectations; they are never needed to build or test axx beyond
  replaying them.
- Deliberate deviations (e.g. integer overflow falling back to 64-bit) are listed in one place
  and documented for users.
