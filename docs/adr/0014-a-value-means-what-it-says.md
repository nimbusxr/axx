# 0014: A value means what it says, in every pack

- Status: Accepted
- Date: 2026-11-01

## Context

The same table cell means different things in different packs:

- REST's response checks and Kafka's consumer checks compare typed values: `42` is an integer and
  `"42"` the string. They differ on `undefined` (no such property for REST, the string for Kafka)
  and on an empty cell (the empty string for REST, an error for Kafka).
- The property checks of SQL JSON columns, MongoDB, gRPC, GraphQL, JSON-RPC, MCP, A2A, the CLI,
  Redis, files and storage objects, the message brokers, DynamoDB, BigQuery, Firestore,
  WebSockets and SSE compare every value as text, double quotes included: `"53111"` never matches
  the string 53111, and `53111` matches the number and the string alike. An empty cell is null.
  The mock's request body checks compare text too, with the quotes dropped.
- Setting a value infers: REST's request properties keep the type of the property they replace
  (`10115` over a string stays a string) and read Java's number forms (`+5`, `1.5f`, `NaN`) as
  numbers; gRPC, GraphQL, JSON-RPC and MCP take a field's type from its schema; Kafka's event
  properties are always strings; SQL `where:` cells keep their quotes.
- Some sources lose their types before a check sees them: whole doubles from MongoDB, Firestore
  and BigQuery read as integers (`6.0` as `6`), and protobuf's JSON gives int64 fields as strings.

A check that compares text cannot see a contract's type change: a postcode that becomes a number
still passes. And people, and agents, carry one pack's rule into another: in the agent
evaluations, agents wrote REST's `"53111"` in a SQL JSON check, which failed it.

## Decision

A value means what it says, the same in every pack, whether a step sets it or checks it.

- `42` is an integer, `42L` a long and `1.5` a decimal; `true` and `false` are booleans; `null` is
  null; `undefined` is no such property; `{...}` and `[...]` are JSON; a value in double quotes is
  the string inside them (`"42"`, `"true"`, `"null"`, `"undefined"`, `"01067"`); any other text is
  a string (`REGISTERED`, `PX-4101`). This is the rule of REST's checks today (`CoerceExpected`).
- A `{string}` parameter's quotes delimit it in the step's text, and its value then follows the
  rule: `the response payload property count is "42"` checks the integer 42, and `is '"42"'` the
  string.
- Nothing is inferred, from an example, a schema or a column's type: `| postcode | 10115 |` sets
  and checks the number 10115, and the string is `"10115"`. A value of the wrong type is sent as
  written, so a test can still send one on purpose (`| weightGrams | "1200" |`).
- Checks are strict: types must match. `42` does not match the string "42", nor `2` the decimal
  `2.0`.
- A value that could be read two ways fails, and its message gives the explicit forms:
  - an empty cell: `""` for the empty string, `null` for null, and `undefined` for no such
    property where the step takes it;
  - a number in a form JSON does not have: a leading zero (`01067`), a sign (`+5`), a suffix
    (`1.5f`), `NaN` or `Infinity`;
  - where a value can only be text (a header, a query parameter or form field, a Redis string, a
    line of a file or of output, text on a screen), a value that reads as anything else:
    `| X-Count | 3 |` asks for `"3"`.
- `axx validate`, the MCP server's `feature_validate` and the editors (`axx lsp`) report what a
  feature shows on its own: empty cells, numbers in other forms, and values
  that are not text in a table that holds only text. The steps fail with the same messages when
  they run.
- Sources keep their types: a whole double is a decimal (`6.0`), a MongoDB Decimal128 and a
  BigQuery NUMERIC are numbers, and a protobuf message's int64 fields are numbers.
- Pattern steps (`… match:`) are unchanged: a pattern matches the value's text.
- The recorded oracles stay frozen and keep testing the Java semantics they record (REST's
  request coercion, Kafka's, the Postgres text comparison); the steps use the rule's functions
  instead (`core.ParseValue`, `jvalue.ValueIs`, `jvalue.SetValue`). A number is an integer or a
  decimal, whatever Java class holds it, and compares by its exact value.

## Consequences

- One rule, in every pack and in both directions, and a check fails when a value's type changes.
- Breaking, for axx 0.3.0: tables that leave a string that looks like a number unquoted
  (postcodes, numeric references), that rely on a field's type being inferred, or that leave
  cells empty, fail, each with a message that names the fix. The examples, the docs, the skills
  and the agent evaluations change with it.
- A string that looks like a number takes two more characters: `"10115"`.
