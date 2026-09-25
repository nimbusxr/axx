# Behavior oracles

Each JSON file holds recorded cases, inputs with their expected outputs, for the value semantics
the steps follow: Jayway JsonPahth, json-smart, `java.util.regex`, Java number formatting,
Jackson, and the step value rules built on them (ADR 0007). The Go packages in `internal/compat`
and the packs are tested against them. Every result must be identical, except for the few
deliberate differences listed, with reasons, in the `Deviations` table of
`internal/compat/internal/oracletest`.

The files are frozen expectations. Never edit them by hand; a change to what they record is a
behavior change and needs an ADR.

| Oracle | Records |
| --- | --- |
| `jsonpath.json` | Jayway JsonPath 2.9.0 reads, sets, puts and deletes (values with their types, re-serialized documents, error classes and messages, definiteness, normalized paths, `hasNoJsonPath`) |
| `jsonsmart.json` | JSON parsing in json-smart's permissive mode: typing and serialized output |
| `reststeps.json` | the REST step value rules: inferring values, parent paths and keys, setting, nulling and deleting properties, the data-table dispatch, coercing expected values, the property and pattern assertions, and form-urlencoded rendering |
| `kafka.json` | the Kafka payload-property coercion and matcher outcomes |
| `postgres.json` | Jackson `asText`, JSON-column stringification and the property and match expectations |
| `javaregex.json` | `java.util.regex`: compile errors, `matches()`, the first `find()` with its groups, and the `find()` loop |
| `javafmt.json` | `Double.toString`, `Float.toString`, `String.valueOf` of maps and lists, `Integer`/`Long`/`Double`/`Float`/`Boolean` parsing, `BigInteger` and `BigDecimal` |
| `jacksonyaml.json` | the fixture factory's YAML and JSON: YAML writing (quoting, folding, block styles, long keys), the YAML reader's scalar typing, and the canonical JSON pretty printer |
