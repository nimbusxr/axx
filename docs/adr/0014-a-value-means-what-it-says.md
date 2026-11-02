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

- `42` is an integer, `42L` a long, and `1.5` and `1e3` decimals; `true` and `false` are booleans;
  `null` is null; `undefined` is no such property; `{...}` and `[...]` are JSON; a value in double
  quotes is the string inside them (`"42"`, `"true"`, `"null"`, `"undefined"`, `"01067"`); any other
  text is a string (`REGISTERED`, `PX-4101`, and `NULL` and `True`: those words are lowercase). This
  is the rule of REST's checks today (`CoerceExpected`), but for the case of those words.
- A `{string}` parameter's quotes delimit it in the step's text, and its value then follows the
  rule: `the response payload property count is "42"` checks the integer 42, and `is '"42"'` the
  string. REST's property steps also take an integer without those quotes: `is 42`.
- A step declares which of its arguments hold values (`core.StepDef.TextArgs`, `TypedArgs`), and
  the runner reads them by the rule before the step runs and before any `${...}` expands, so a
  reference is a string whatever it expands to.
- A name or a pattern is not a value: a label, a page, a file, an example's name finds something,
  as written, and a pattern or a command line has a grammar of its own (`the "5" button is clicked`,
  `the page's requests to '**/api/*' fail`). What a step sets something to or checks it against is one.
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
    `| X-Count | 3 |` asks for `"3"`, and `the response header X-Count is '3'` for `'"3"'`. So
    `'{"a":1}'` reads as JSON and asks for `'"{"a":1}"'`, and `'"status":"ok"'` is the text
    `status":"ok`: a pair of double quotes around the whole value is the rule's.
- `axx validate`, the MCP server's `feature_validate` and the editors (`axx lsp`) report what a
  feature shows on its own: empty cells, numbers in other forms, and values that are not text where
  only text fits, in a table or in a step's own text, each as `AXX-E0207`, and refuse the feature
  (exit 3) before anything runs. `axx run` does not check first: the steps before it run, then the
  step fails with the same message and code, and the run exits 1.
- A filter is as strict as a check, whichever side compares. A SQL `where:` and a BigQuery
  condition are held to their column's real type before the query, and fail at once with the
  explicit form (`postcode is text in parcels.pickups (VARCHAR): write "10115"`), as each database
  would otherwise convert one of them its own way; an SQLite column with no declared type is held
  to what its rows store. What MongoDB selects, and PostgreSQL's JSONB containment, go through the
  rule's comparison after the query: `6` selects no double, and `null` a null, with `undefined` for a
  document without the field.
- Sources keep their types: a whole double is a decimal (`6.0`), a BigQuery NUMERIC a decimal (`2.0`),
  a MongoDB Decimal128 the number of its digits, and a protobuf message's int64 fields are numbers.
- The mock's request checks read a JSONPath that can give several values (a wildcard, a filter, a
  recursive descent) as WireMock does: the request has the value when any of them is that value,
  each compared strictly by this rule. `undefined` means the path gives none, and a list as the
  value also compares with all of them at once.
- Pattern steps (`… match:`) are unchanged: a pattern matches the value's text.
- The recorded oracles stay frozen and keep testing the Java semantics they record (REST's
  request coercion, Kafka's, the Postgres text comparison); the steps use the rule's functions
  instead (`core.ParseValue`, `jvalue.ValueIs`, `jvalue.SetValue`). A number is an integer or a
  decimal, whatever Java class holds it, and compares by its exact value.

## Consequences

- One rule, in every pack and in both directions, and a check fails when a value's type changes.
- Breaking, for axx 0.3.0: tables and steps that leave a string that looks like a number unquoted
  (postcodes, numeric references, `the response header Retry-After is '5'`), that rely on a field's
  type being inferred, or that leave cells empty, fail, each with a message that names the fix. The
  examples, the docs, the skills and the agent evaluations change with it.
- A string that looks like a number takes two more characters: `"10115"`.
