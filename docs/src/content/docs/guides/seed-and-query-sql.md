---
title: Seed and query SQL
description: Load datasets into PostgreSQL (and MySQL, SQLite or SQL Server), assert on rows and JSON columns, wait for asynchronous writes, and inject database faults with triggers.
---

The SQL steps put a database in a known state before a scenario and check what your service wrote afterwards. PostgreSQL is first-class; MySQL, SQLite and SQL Server are supported.

## Register the database

```gherkin
Background:
  Given a parcels-db database with the following properties:
    | url      | postgres://localhost:5432/parcels |
    | user     | parcels                           |
    | password | parcels                           |
```

Native URLs (`postgres://`, `mysql://`, `sqlserver://`, `sqlite:`) and JDBC-style URLs (`jdbc:postgresql://`, `jdbc:mysql://`, `jdbc:mariadb://`, `jdbc:sqlserver://`, `jdbc:sqlite:`) are accepted; an optional `schema` property sets the default schema. The drivers are pure Go and built into `axx`. JSONB containment and triggers need PostgreSQL; seeds, selections, row counts and locks work on every database.

## Seed

```gherkin
Given a seeds/manifest-kestrel.yaml db seed
Given a seeds/dispatching.yaml db seed on parcels-db
```

A seed is a dataset keyed by table. YAML is the usual format:

```yaml title="seeds/manifest-kestrel.yaml"
parcels.manifest_lines:
  - id: "ML-KES-0412-1"
    manifest_id: "M-KESTREL-0412"
    reference: "PX-KES-1001"
    sender: "kestrel-books"
    weight_grams: 850
    service_level: "STANDARD"
    recipient: '{"name": "Emmy Noether", "street": "Bunsenstrasse 3", "city": "Goettingen", "postcode": "37073", "country": "DE"}'
  - id: "ML-KES-0412-2"
    manifest_id: "M-KESTREL-0412"
    reference: "PX-KES-1002"
    sender: "kestrel-books"
    weight_grams: 2300
    service_level: "EXPRESS"
    recipient: '{"name": "Lise Meitner", "city": "Hamburg", "postcode": "20095", "country": "DE"}'
```

Flat XML (`.xml`), JSON (`.json`), Excel (`.xlsx`, one sheet per table) and CSV dataset directories (one `<table>.csv` per table plus `table-ordering.txt`) work the same way. The `[null]`, `[DAY,NOW]`, `[DAY,PLUS,1]` and `[UNIX_TIMESTAMP]` placeholders are supported. Values are escaped, so text containing `'` is safe.

Scenarios share the database and run in parallel, so give every seeded row an id that belongs to one scenario. [Isolate test data](/guides/isolate-test-data/) shows how to enforce that with `axx lint`.

A seed inserts its rows in file order, in one transaction. It never updates or deletes existing rows, so a row whose key already exists fails the step and nothing from that file is written.

## Select rows and assert

A selection is a query with equality conditions. Later steps check its size and contents:

```gherkin
Then a selection of rows is retrieved from the parcels.manifest_lines table where:
  | manifest_id | M-KESTREL-0412 |
And the selection has 2 rows
And the selection has more than 1 row
And the selection has fewer than 5 rows
```

A scenario can hold several selections, addressed by ordinal (`2nd`, `3rd`, ...). Leaving the ordinal out means the first:

```gherkin
Then a selection of rows is retrieved from the parcels.manifest_lines table where:
  | manifest_id | M-KESTREL-0412 |
And a 2nd selection of rows is retrieved from the parcels.manifest_lines table where:
  | id | ML-KES-0412-1 |
And the 2nd selection has 1 row
```

### JSON columns

Assert on properties inside a JSON or JSONB column of a selected row:

```gherkin
Then a selection of rows is retrieved from the parcels.parcels table where:
  | reference | PX-REG-1001 |
And the 1st row details property for the selection json properties are:
  | source | api  |
  | zone   | DE-1 |
And the 1st row details property for the selection json properties match:
  | zone | ^DE-.*$ |
```

Or select by JSONB containment (PostgreSQL):

```gherkin
Then a selection of rows is retrieved from the parcels.parcels table where the details jsonb column contains:
  | manifestId | M-KESTREL-0412 |
And the selection has 2 rows
```

Paths are JSONPaths such as `manifestId` or `items[0].sku`, and values are compared as text ([the rules](/references/steps/sql/#sqljsonare)).

## Wait for asynchronous writes

When the service writes in the background (importing a manifest, say), poll instead of sleeping:

```gherkin
Then within 10s a selection of at least 2 rows is retrieved from the parcels.manifest_lines table where:
  | manifest_id | M-KESTREL-0412 |
  | status      | IMPORTED       |
```

The step retries until the selection has at least that many rows or the time runs out. It passes as soon as the rows appear.

## Inject faults with triggers

To test how your service handles database errors, install a trigger that raises an SQL state for matching inserts:

```gherkin
@isolated
Scenario: A brief database failure does not fail the registration
  Given a before insert trigger on the parcels.parcels table will raise a 40001 exception 1 time where:
    | reference | PX-DBF-3001 |
  And a POST request to /api/parcels
  And a request payload using an application/json content example
  And the request payload property reference is 'PX-DBF-3001'
  When the request is executed
  Then the response status code is 201
  And the before insert trigger on the parcels.parcels table was raised 1 time
```

The trigger raises a serialization failure (`40001`) once; the service retries and the second insert succeeds. Without `1 time` the trigger raises on every matching insert. Another variant inserts the row and then raises (`... will insert and raise a 23505 exception where:`).

Triggers change a shared table, so tag those scenarios with a tag in `run.exclusive` (for example `@isolated`) to run them alone after the parallel phase. See [Run in parallel](/guides/parallel-runs/).

## Row locks

```gherkin
Scenario: A parcel can be changed again once the depot lets go of it
  Given a seeds/dispatching.yaml db seed
  And the rows in the parcels.parcels table are locked where:
    | reference | PX-DSP-2001 |
  And a 1st ordered PATCH request to /api/parcels/PX-DSP-2001
  And a request payload using an application/json content example named 'Heavier' for 1st ordered request
  And a 2nd ordered PATCH request to /api/parcels/PX-DSP-2001
  And a request payload using an application/json content example named 'Heavier' for 2nd ordered request
  When the 1st ordered request is executed
  Then the 1st ordered response status code is 409
  When the row locks are released
  And the 2nd ordered request is executed
  Then the 2nd ordered response status code is 200
```

Lock rows to test timeouts and contention in your service. The lock holds until `the row locks are released` or the end of the scenario.

## Named databases

Every step has an `on <service>` form for scenarios with more than one database, for example `a selection of rows is retrieved from the parcels.parcels table on parcels-db where:` and `the selection on parcels-db has 1 row`.
