Every step of Axx's packs follows the same grammar. The [pack pages](/references/packs/) list each step with its parameters, the rows its table takes, its variants and an example.

## Optional parts

An expression writes optional parts as `[[ ... ]]`. A step matches with or without each of them. `the[[ {ordinal} ordered]] response status code is {int}[[ on {service}]]` matches:

- `the response status code is 200`
- `the 2nd ordered response status code is 201`
- `the response status code is 200 on parcels`
- `the 2nd ordered response status code is 201 on parcels`

`axx steps show` prints every variant of a step, given its id, its expression or a line that uses it.

## Services

A step without a service name uses the default service: the first one of its kind registered in the scenario. The named form (`on parcels`, `on parcels-db`, `on the events kafka service`) picks another.

## Ordinals

`1st`, `2nd`, `3rd`, `4th`, ... count requests, selections and events within a scenario, starting at 1. A step without an ordinal uses the first.

## Arguments

- `{string}` takes single or double quotes; the quotes are removed.
- `a(n)`, `time(s)`, `row(s)`, `document(s)` and `item(s)` are optional text: `1 time` and `2 times` both match.
- A step that ends with `:` takes a data table, or a doc string, on the following lines. Its entry shows the table's columns and the rows it knows.
- What a value means is the same in every step: see [Values](#values).

## Values

A value means what it says, the same in every pack, whether a step sets it or checks it ([ADR 0014](https://github.com/nimbusxr/axx/blob/main/docs/adr/0014-a-value-means-what-it-says.md)).

| Written | Is |
|---|---|
| `42` | an integer |
| `42L` | a long: an integer, which a MongoDB query sends as an int64 |
| `1.5`, `2.0` | a decimal |
| `true`, `false` | a boolean |
| `null` | null |
| `undefined` | no such property: a check that it is absent, or a property removed |
| `{"country": "DE"}`, `["EXPRESS"]` | JSON |
| `"42"`, `"true"`, `"null"`, `"undefined"`, `"01067"` | the string inside the double quotes |
| any other text, like `REGISTERED` or `PX-4101` | a string |

- **Nothing is inferred,** from an example, a schema or a column's type. `| postcode | 10115 |` sets and checks the number 10115, and the string is `"10115"`.
- **A value of the wrong type is sent as written,** so a test can still send one on purpose: `| weightGrams | "1200" |`.
- **Checks are strict: types must match.** `42` does not match the string "42", nor `2` the decimal `2.0`.
- **Sources keep their types.** A whole double is a decimal (`6.0`), a MongoDB Decimal128 and a BigQuery NUMERIC are numbers, and a protobuf message's int64 fields are numbers.
- **A value that could be read two ways fails,** and its message gives the forms to write:
  - an empty cell: `""` for the empty string, `null` for null, and `undefined` for no such property where the step takes it;
  - a number in a form JSON does not have: a leading zero (`01067`), a sign (`+5`), a suffix (`1.5f`), `NaN` or `Infinity`.
- **Where a value can only be text,** one that reads as anything else fails too: a header, a query parameter or form field, a Redis string, a line of a file or of output, text on a screen. `| Retry-After | 5 |` asks for `"5"`. A table's entry says when its values are text.
- **In a step's own text, the quotes are the step's,** and the value inside them follows the rule: `the response payload property priceCents is '690'` checks the integer, and `is '"690"'` the string. REST's property steps also take an integer without quotes: `is 690`. A step that takes text, like `the response header Retry-After is '5'`, takes what is inside the quotes as it is.
- **A pattern is not a value.** A step that takes regular expressions (`… match:`) matches them against the value's text: `| postcode | ^[0-9]{5}$ |`.

`axx validate`, the MCP server's `feature_validate` and the editors report what a feature shows on its own, as [`AXX-E0207`](/references/error-codes/#axx-e0207): empty cells, numbers in other forms, and values that are not text in a table that holds only text. The steps fail with the same messages when they run.

## Parameter types

