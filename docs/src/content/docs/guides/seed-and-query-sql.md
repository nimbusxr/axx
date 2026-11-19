---
title: Seed and query SQL
description: Load datasets into PostgreSQL (and Oracle, MySQL, SQL Server, Db2, H2 or SQLite), assert on rows and JSON columns, wait for asynchronous writes, and inject database faults with triggers.
---

The SQL steps put a database in a known state before a scenario and check what your service wrote afterwards. PostgreSQL is first-class; Oracle, MySQL, SQL Server, Db2, H2 and SQLite are supported.

## Register the database

```gherkin
Background:
  Given a parcels-db database with the following properties:
    | url      | postgres://localhost:5432/parcels |
    | user     | parcels                           |
    | password | parcels                           |
```

Native URLs (`postgres://`, `mysql://`, `sqlserver://`, `oracle://`, `db2://`, `sqlite:`) and JDBC-style URLs (`jdbc:postgresql://`, `jdbc:mysql://`, `jdbc:mariadb://`, `jdbc:sqlserver://`, `jdbc:oracle:thin:@//`, `jdbc:db2://`, `jdbc:h2:`, `jdbc:sqlite:`) are accepted. An H2 database needs a URL that axx can reach it by: see [H2](#h2). Name a table with its schema, like `parcels.parcels`, or set `currentSchema` in a PostgreSQL or Db2 URL. The drivers are pure Go and come with the `sql` pack. A user with no password, like H2's `sa`, has an empty `password` cell. JSONB containment needs PostgreSQL, triggers PostgreSQL, Oracle or Db2, and row locks a database other than SQLite; seeds, selections and row counts work on every database.

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

Flat XML (`.xml`), JSON (`.json`), Excel (`.xlsx`, one sheet per table) and CSV dataset directories (one `<table>.csv` per table, in the order `table-ordering.txt` lists when there is one) work the same way. The `[null]` and `[UNIX_TIMESTAMP]` placeholders are supported, and so are times counted from now, such as `[DAY,NOW]`, `[DAY,PLUS,1]` or `[HOUR,MINUS,2]` (in `DAY`, `HOUR`, `MIN` or `SEC`). A time counted from now is counted from the database's own clock, in its time zone, so it agrees with what the database compares it to, like `SYSDATE` or `now()`, wherever axx runs. A column with no time zone holds the database's wall-clock time, and one that holds a moment, like `timestamptz`, holds that very moment. SQLite has no clock of its own, so it gets the machine's. Values are escaped, so text containing `'` is safe.

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

[A value means what it says](/references/steps/#values), and is held to its column's real type before the query, on every database, since each would otherwise convert one of them its own way:

| A column of | Takes |
| --- | --- |
| text (`VARCHAR`, `TEXT`, `CITEXT`) | a string: `M-KESTREL-0412`, or in double quotes when it reads as a number: `"37073"` |
| integers (`INT`, `BIGINT`, unsigned ones) | an integer: `850` |
| decimals (`NUMERIC`, `DECIMAL`, `FLOAT`, `DOUBLE`, `REAL`) | a decimal, a whole one too: `1.5`, `12.0` |
| booleans (`BOOLEAN`, SQL Server's `BIT`) | `true` or `false`; MySQL's `TINYINT` and `BIT` take these and integers |
| Oracle's `NUMBER` | by its scale: `NUMBER(10,2)` is a decimal, `NUMBER(10)` an integer, and a `NUMBER` with no scale is held to what the row stores |
| anything else (dates, times, UUIDs, JSON, enums, binary, PostgreSQL's `MONEY`) | a string: `2026-09-24` |

`null` is `IS NULL`, and `{...}` and `[...]` go to the database as they are. A value of another type fails at once, a poll's too, and says what to write:

```text
postcode is text in parcels.pickups (VARCHAR): write "10115" [AXX-E0207]
```

An SQLite column declared with no type has none to hold a value to, so what the database selects is held to what each row stores: `850` selects the integer, and not the real `850.0`. An empty cell is an error: write `""` for the empty string.

A date or time says as much as its column holds, in a table and in a column check, or it fails at once and says what to write. A column with no time zone (`DATE`, `TIMESTAMP`, `DATETIME`, and MySQL's `TIMESTAMP`, which a client reads as a wall clock) takes no offset, which each database would drop its own way. A column that holds a moment (`timestamptz`, Oracle's `TIMESTAMP WITH TIME ZONE`, SQL Server's `DATETIMEOFFSET`) needs one, as a time without it would mean something else on another machine:

```text
planned_at has no time zone in pickups (TIMESTAMP), so an offset would be dropped: write "2026-09-24 07:40:00" [AXX-E0207]
confirmed_at holds a moment in pickups (TIMESTAMPTZ), which a time with no offset does not say: write its offset, like "2026-09-24T07:40:00Z" [AXX-E0207]
```

A scenario can hold several selections. They are numbered in the order they are retrieved, and later steps address them by ordinal (`the 2nd selection`); in `a 2nd selection of rows is retrieved` the ordinal is only a label. Leaving the ordinal out means the first:

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

Values are typed, so a check sees a property's type change: `850` is the number and `"37073"` the string, and neither matches the other. Against the manifest line seeded above:

```gherkin
Then a selection of rows is retrieved from the parcels.manifest_lines table where:
  | id           | ML-KES-0412-2 |
  | weight_grams | 2300          |
And the 1st row recipient property for the selection json properties are:
  | city     | Hamburg   |
  | postcode | "20095"   |
  | street   | undefined |
```

Or select by JSONB containment (PostgreSQL):

```gherkin
Then a selection of rows is retrieved from the parcels.parcels table where the details jsonb column contains:
  | manifestId | M-KESTREL-0412 |
And the selection has 2 rows
```

Check one value of a selection by its row and column:

```gherkin
Then a selection of rows is retrieved from the parcels.parcels table where:
  | reference | PX-REG-1009 |
And the 1st row status column of the selection is 'REGISTERED'
And the 1st row weight_grams column of the selection is '1200'
And the 1st row label_printed_by column of the selection is 'null'
```

The value is of the column's type, as in a `where:` table, and strict: `'1200'` is an integer, `'1200.0'` a decimal and `'"1200"'` a string. A date or a time compares as a moment, written `'2026-09-24'`, `'2026-09-24 07:40:00'` or `'2026-09-24T07:40:00+02:00'`, and a `CHAR` column's padding is no part of its value.

Selections are numbered in the order a scenario retrieves them, and a step without an ordinal checks the first. After a second retrieval, name the one a step checks: `the 2nd selection has 1 row`. `axx validate` warns about a step that checks the first while a later selection goes unchecked.

In `json properties are:` and `json properties match:`, paths are JSONPaths such as `zone` or `items[0].sku`. `json properties are:` compares values with their types: `true` is a boolean, `null` null, and `undefined` a property the JSON lacks ([the rules](/references/packs/sql/#sqljsonare)); `json properties match:` matches a regular expression against the value's text. The containment step takes dotted names (`recipient.city`) and the same typed values, so `| pieces | 3 |` finds the integer 3 and `| recipient.postcode | "37073" |` the string; `3` does not find `3.0`, though PostgreSQL's containment alone would.

## Wait for asynchronous writes

When the service writes in the background (importing a manifest, say), poll instead of sleeping:

```gherkin
Then within 10s a selection of at least 2 rows is retrieved from the parcels.manifest_lines table where:
  | manifest_id | M-KESTREL-0412 |
  | status      | IMPORTED       |
And the selection has 2 rows
```

The step queries again every 500 ms until the selection has at least that many rows, and moves on as soon as the rows appear; it fails when the time runs out with fewer. `the selection has 2 rows` then checks that no more than those came back.

The selection by JSONB containment waits the same way, for the rows a service writes in the background:

```gherkin
When within 10s a selection of at least 2 rows is retrieved from the parcels.parcels table where the details jsonb column contains:
  | manifestId | M-KESTREL-0601 |
  | source     | manifest       |
Then the selection has 2 rows
```

To wait until rows are gone, or no longer match, check that the table has none that do. This step waits until no pending line of the manifest is left, for an import in the background:

```gherkin
When within 20s the parcels.manifest_lines table has no rows where:
  | manifest_id | M-MAPLE-0019 |
  | status      | PENDING      |
Then a selection of rows is retrieved from the parcels.manifest_lines table where:
  | manifest_id | M-MAPLE-0019 |
  | status      | IMPORTED     |
And the selection has 5 rows
```

It queries again every 500 ms, and fails when rows still match when the time is up. It also passes when no row ever matched, for example when the service never wrote them, so check what the service did too, as the selection of imported lines does here. `axx lint` points out a scenario whose only checks are of this kind.

A trigger's count waits the same way, for writes the service makes in the background: `within 10s the before insert trigger on the parcels.parcels table was raised 1 time`. It fails at once when the trigger raised more times than that, since the count only grows ([Inject faults with triggers](#inject-faults-with-triggers)).

## Oracle

The steps are the same on Oracle, with a few of its own ways:

- **Nothing to say about session formats.** A date or a time reads as `2026-09-24`, `2026-09-24 07:40:00` or `2026-09-24T07:40:00+02:00` in a seed or a table, whatever the database's date format is.
- **An empty string is null.** Oracle stores `''` as null, so `""` could never select a row: the step fails at once and says to write `null`.
- **Bytes are written in hex.** A `RAW` or `BLOB` column takes and reads as `DEADBEEF`.
- **A `JSON` column compares as JSON.** `{"count":3,"zone":"DE-1"}` selects a row whose keys are in another order, and `{"count":"3"}` does not select a row holding the number.
- **Oracle refuses a condition on some types:** `LONG`, `LONG RAW`, `XMLTYPE` and `VECTOR`. Select the row by another column and check the value with `the 1st row doc column of the selection is '<parcel/>'`.
- **A `GENERATED ALWAYS` column takes no value from a row,** as Oracle has no way to give it one: leave it out of the seed, or declare it `GENERATED BY DEFAULT`.
- **A trigger's code is an ORA error.** `will raise an ORA-20001 exception` raises it, and `will raise an ORA-00060 exception` the deadlock's. Its digits alone, `20001`, say the same.

## Db2

axx has its own client of Db2, so the `sql` pack reaches one with nothing else installed: no IBM driver and no C compiler. It is for Db2 for Linux, UNIX and Windows, and is held to Db2 12.1.

```gherkin
Given a parcels-db database with the following properties:
  | url      | jdbc:db2://localhost:50000/parcels |
  | user     | db2inst1                           |
  | password | ${env:DB2_PASSWORD}                |
```

The URL is the one IBM's JDBC driver takes, so you can give axx the URL the service has. Its properties come after the database's name, each one ended by a semicolon, like `jdbc:db2://localhost:50001/parcels:currentSchema=DEPOT;sslConnection=true;`. axx reads these:

| Property | What it does |
| --- | --- |
| `currentSchema` | The schema of a table named without one. Without it, that is the schema named after the user. |
| `sslConnection=true` | Connects with TLS, to the port the server speaks TLS on. |
| `sslCertLocation` | A file of certificates, in PEM, that the server's certificate is held to. Without it, that is the certificates the machine trusts. |
| `securityMechanism` | How the user is proved: `3`, a user and a password as they are; `7`, the password encrypted; `9`, the user and the password encrypted. Without it, axx takes the way the server takes. |
| `encryptionAlgorithm` | The cipher of an encrypted user and password: `1`, DES; `2`, AES. Without it, axx asks the server for AES, and for DES when the server takes no other. |

The other properties are for IBM's driver, and axx leaves them alone. A `securityMechanism` other than those three is one axx does not speak, and the step says so.

### Start Db2 for your tests

IBM publishes Db2 Community Edition as a container image, `icr.io/db2_community/db2`. Three things about it are worth knowing before you start it:

- **Its license is IBM's.** The image starts only with `LICENSE=accept`, which accepts IBM's license for Db2 Community Edition. Read it first: it is in the image's `/licenses` folder. IBM gives the edition at no charge, and the license limits a server to 4 processor cores and 16 GB of memory.
- **It is built for Intel and AMD processors.** A Mac with Apple silicon runs it by emulation, which Docker Desktop does when the service says `platform: linux/amd64`. Turn on "Use Rosetta for x86_64/amd64 emulation on Apple Silicon" in Docker Desktop's settings, under General, before you start it: without Rosetta, a start that takes a minute or two takes about ten.
- **It takes a minute or two to start,** every time a container of it starts. The setup below starts it once and gives every run a clean database in seconds.

The setup is three files. The first is the compose file:

```yaml title="compose.yaml"
services:
  db2:
    image: icr.io/db2_community/db2:12.1.5.0
    platform: linux/amd64
    privileged: true          # IBM's image needs it
    ports:
      - "50000:50000"
    environment:
      LICENSE: accept
      DB2INST1_PASSWORD: ${DB2_PASSWORD}
      DBNAME: parcels
      AUTOCONFIG: "false"     # do not tune Db2 to this machine
      ARCHIVE_LOGS: "false"   # do not archive logs, which starts with a backup
    volumes:
      - ./db2/clean.sh:/clean.sh:ro
```

`DB2INST1_PASSWORD` is the password of the user `db2inst1`, which owns the database. Db2's users are the container's own, so a user for your service is one you add to the container, or `db2inst1` itself in a test.

The second is a script that makes the database clean. The first time it runs, it keeps the empty database as a backup. Every time after that, it restores the backup:

```sh title="db2/clean.sh"
#!/bin/sh
# Brings the parcels database back to how it was when Db2 first started: empty.
baseline=$HOME/baseline
db2 force application all
db2 deactivate db parcels
if [ -d "$baseline" ]; then
  db2 restore db parcels from "$baseline" replace existing || exit 1
else
  mkdir "$baseline" && db2 backup db parcels to "$baseline" || exit 1
fi
echo "parcels is clean"
```

The third is `axx.yaml`, with Db2 as one service and the cleaning as another:

```yaml title="axx.yaml"
services:
  db2:
    command: docker compose -p parcels-axx up -d db2
    ready:
      exec: [docker, compose, -p, parcels-axx, exec, -T, db2, sh, -c,
             "test -f /database/config/.shared-data/setup_complete && su - db2inst1 -c 'db2 connect to parcels'"]
      timeout: 300s
      interval: 5s
    cleanup: docker compose -p parcels-axx down -v

  clean-db2:
    dependsOn: [db2]
    command: docker compose -p parcels-axx exec -T db2 su - db2inst1 -c "sh /clean.sh"
    ready:
      log: parcels is clean

  api:
    dependsOn: [clean-db2]
    command: java -jar build/libs/parcels-api.jar
    # ...
```

Your service depends on `clean-db2`, so it starts when the database is clean, and creates its tables as it does on a new database.

With these files there are two ways to run:

1. **`axx run` alone** starts Db2, runs the scenarios and removes Db2. Every run waits for Db2 to start.
2. **`axx up db2` once, then `axx run` as often as you like.** Db2 keeps running, and each run cleans the database first, which takes a few seconds. `axx down` stops Db2 and removes it.

The second way is the one to work with. On a Mac with Apple silicon, a run of one scenario took 1 min 47 s the first way and 9 s the second. axx warns on such a run that services kept by `axx up` keep the data of earlier runs: of Db2's database, the cleaning takes care.

Cleaning ends every connection to Db2 that is open (`db2 force application all`), which a restore needs. A service that is still running loses its connections and has to connect again, as a connection pool does.

### What goes wrong, and why

- **The scenarios start before Db2 is ready.** Db2's port says nothing of whether Db2 is ready: Docker answers on a published port as soon as the container starts, and Db2 itself opens the port while it still creates the database. A readiness check of the port (`ready.tcp`) passes at once, and the first step then fails with `connection reset by peer`. Ask Db2, as the `ready.exec` above does: the file is one the image writes when its setup is over, and `db2 connect` says the database takes connections.
- **The container stops during its setup on a Mac.** A folder of the Mac mounted at `/database` goes through Docker's file sharing, and the image's setup fails there as it creates its users. Leave `/database` to Docker, as the compose file above does, or give it a Docker volume.
- **The container exits at once.** Its log says `LICENSE not set to 'accept'`: the image starts only when its license is accepted.
- **Db2 never gets ready, and the container's log has `SQL1042C  An unexpected system error occurred`.** The container is not privileged. It keeps running and its setup goes on to the end, but Db2 did not start and there is no database. Set `privileged: true`.
- **The start takes about ten minutes on a Mac with Apple silicon.** Docker emulates the image's processor without Rosetta, which is much slower. Run `docker run --rm --platform linux/amd64 alpine grep -m1 "model name" /proc/cpuinfo`, as it is written: it prints `VirtualApple` when Rosetta is in use, and nothing when it is not. Then turn on "Use Rosetta for x86_64/amd64 emulation on Apple Silicon" in Docker Desktop's settings, under General, and restart Docker. The times on this page are with Rosetta. Less memory or fewer processors did not explain a slow start here: with 2 GB of memory, or with 2 processors, Db2 was ready in the same time.
- **A seed fails with `a row with that key is already there`.** The database has the rows of an earlier run: the service that cleans it did not run. Check that your service depends on `clean-db2`.

### How long Db2 takes

These are the times of Db2 12.1.5 on a Mac with Apple silicon, where Docker emulates the image's processor. A start is measured from `docker run` to a database that takes connections:

| What | Time |
| --- | --- |
| The first start of a container, with the two settings above | 1 min 40 s |
| The first start, with the image's own defaults | 2 min 10 s |
| A start with the database kept in a volume at `/database` | 1 min 20 s |
| A stopped container started again | 1 min 10 s or more |
| Cleaning the database of a Db2 that runs (a restore) | 5 s |
| Dropping the database and creating it again | 20 s |

No setting makes a start short: most of it is Db2 starting, not the database being created. That is why the setup above keeps Db2 running and cleans the database instead.

### Db2's own ways

- **A user and a password.** axx logs in with a user and a password, as IBM's JDBC driver does by default. They travel as they are to a server that takes them so, and encrypted to one that does not (`AUTHENTICATION SERVER_ENCRYPT`), with no setting: with AES, and with DES for a server that takes no other. A server that takes AES only (`ALTERNATE_AUTH_ENC AES_ONLY`) gets AES. That encryption is DRDA's own, in which neither side proves who it is, and IBM's documentation says it is not for a network that is not secure: where the network is not yours, use `sslConnection=true`.
- **What axx does not log in with.** A server that takes Kerberos or a plug-in only is not one axx can log in to, and the step says so. Under the encryption, a user and a password are in EBCDIC, so one with a character outside ASCII needs a server that takes it as it is.
- **Names are in upper case.** Db2 folds a name that is not quoted to upper case, so a message names `PARCELS` and `REFERENCE`. Write names in a feature in either case.
- **Errors are Db2's own.** A step that Db2 refuses shows Db2's message, like `SQL0204N  "DEPOT.SHELFS" is an undefined name.`
- **Bytes are written in hex.** A `BINARY`, `VARBINARY` or `BLOB` column, and a `CHAR` or `VARCHAR` column `FOR BIT DATA`, takes and reads as `DEADBEEF`.
- **Dates and times have no time zone.** A `DATE`, `TIME` or `TIMESTAMP` reads as `2026-09-24`, `07:40:00` or `2026-09-24 07:40:00`. A seed's times counted from now are by Db2's clock.
- **JSON is text.** Db2 has no JSON column type, so a column that holds JSON is a `VARCHAR` or a `CLOB`: a condition compares its text, character for character, and the steps that check a column's JSON properties read it as JSON.
- **A `GENERATED ALWAYS` column takes no value from a row,** as Db2 has no way to give it one in an `INSERT`: leave it out of the seed, or declare it `GENERATED BY DEFAULT`.
- **A locked row is waited for.** Db2 waits for a row another transaction holds for as long as its `LOCKTIMEOUT` says, which is forever by default. The step that locks rows gives up after ten seconds.
- **Text is Unicode's.** axx reads a database created in Unicode, which is Db2's default, and one in a single-byte character set of ISO 8859 or Windows. It says so when a database is in another.
- **A trigger's error is an SQLSTATE with Db2's own code.** `will raise a 40001 exception` fails the write with SQLSTATE `40001`, and Db2 reports it as `SQL0438N`, its code for an error a trigger raises: a service that tells a deadlock by Db2's code `-911` sees another code, and one that goes by the SQLSTATE sees `40001`. An SQLSTATE that starts with `00`, `01` or `02` fails nothing on Db2, and the step says so.
- **No JSONB containment.** JSONB is PostgreSQL's column type, and that step says so on Db2. The steps that check a column's JSON properties work.
- **Db2 for z/OS and Db2 for i** are other servers, with catalogs of their own, and axx is not held to them.

## H2

H2 is a Java database that a service usually opens inside its own process. axx is another process, so it reaches an H2 database only where something listens for it. Which URL to give axx depends on how the service opens the database.

### The service uses an H2 server

Give axx the URL the service has:

```gherkin
Given a parcels-db database with the following properties:
  | url      | jdbc:h2:tcp://localhost:9092/~/parcels |
  | user     | sa                                     |
  | password |                                        |
```

A server started with `-tcpSSL` has a `jdbc:h2:ssl://` URL. axx holds its certificate to what the machine trusts, as H2's own client does. An H2 server shows H2's built-in certificate, the same for everyone, unless it is started with one of its own (`javax.net.ssl.keyStore`), and neither client accepts the built-in one.

### The service opens a file database itself

A file database is open to the process that opened it alone, unless its URL says `AUTO_SERVER=TRUE`. With that setting, H2 lets other processes in while the service runs.

1. Add `;AUTO_SERVER=TRUE` to the service's URL, like `jdbc:h2:file:./data/parcels;AUTO_SERVER=TRUE`.
2. Give axx the same URL, with a path that names the same file from the folder you run axx in. An absolute path is the safest.

axx reads the lock file H2 writes beside the database (`parcels.lock.db`), which says where the service listens. When the service is not running, the step that first uses the database says that no service has it open.

When the service runs in a container, do two more things:

1. Keep the database in a folder of your machine that the container mounts, so axx can read the lock file.
2. Add `;AUTO_SERVER_PORT=9090` to the service's URL, and publish that port under the same number (`9090:9090`).

### The service keeps the database in its memory

A database with a `jdbc:h2:mem:` URL lives in the service's process, and H2 has no setting that opens it to another process. For its test runs, give the service one of these URLs. Neither needs a change to its code:

- **A file database with `AUTO_SERVER=TRUE`**, as above. The database stays inside the service.
- **A database in the memory of an H2 server** that axx starts as a service. The service and axx then have the same URL:

```yaml title="axx.yaml"
services:
  h2:
    command: java -cp h2.jar org.h2.tools.Server -tcp -tcpPort 9092 -ifNotExists
    ready:
      tcp: localhost:9092

  api:
    dependsOn: [h2]
    command: java -jar build/libs/parcels-api.jar
    env:
      SPRING_DATASOURCE_URL: jdbc:h2:tcp://localhost:9092/mem:parcels;DB_CLOSE_DELAY=-1
```

`SPRING_DATASOURCE_URL` is how a Spring Boot service takes its URL from the environment. `DB_CLOSE_DELAY=-1` keeps the database when its last connection closes. Use the H2 jar of the version the service has, so that it is the same database in every way.

### H2's own ways

- **H2 1.4.197 and later.** axx has its own client of H2's server, which is held to H2 1.4.200, 2.1.214 and 2.5.252.
- **Names are in upper case.** H2 folds a name that is not quoted to upper case, so a message names `PARCELS` and `REFERENCE`. Write names in a feature in either case.
- **A `JSON` column compares as its text.** Spaces do not count, but the order of its names does: `{"zone":"DE-1","count":3}` does not select a row that holds `{"count":3,"zone":"DE-1"}`. A seed writes an object's names in alphabetical order.
- **Bytes are written in hex.** A `BINARY`, `VARBINARY` or `BLOB` column takes and reads as `DEADBEEF`.
- **A `GENERATED ALWAYS` column takes a seed's value,** like PostgreSQL's.
- **The time zone is each session's.** On H2 2, a seed's times counted from now are in the time zone of the machine that runs axx, as they are for the service's own Java driver. When the service runs in another zone, like a container in UTC, add `;TIME ZONE=UTC` to the URL you give axx. On H2 1.4 they are in the server's zone.
- **No triggers and no JSONB containment.** Those steps fail on H2 and say so.
- **An encrypted database (`CIPHER`) and a cluster of servers** are not ones axx can open.

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

Triggers change a shared table, so to run those scenarios alone after the parallel phase, tag them with a tag in `run.exclusive` (for example `@isolated`) or list `sql.trigger` in `run.exclusiveUses`. See [Run in parallel](/guides/parallel-runs/).

An update or a delete fails the same way, with a before update or before delete trigger. An update's conditions are on the row as the update would leave it, and a delete's on the row it would take away, which stays:

```gherkin
Given a before update trigger on the parcels.parcels table will raise a 40001 exception where:
  | reference    | PX-DBF-3102 |
  | weight_grams | 2500        |
And a before delete trigger on the parcels.parcels table will raise a 40001 exception 1 time where:
  | reference | PX-DBF-3104 |
```

Triggers need PostgreSQL, Oracle or Db2. Each kind is numbered on its own, so `the before update trigger on the parcels.parcels table was raised 1 time` means the first before update trigger, whatever else the scenario created.

When the service writes in the background, like a job that imports a manifest, wait for the trigger to have raised its error:

```gherkin
@isolated
Scenario: A brief database failure does not stop a manifest's import
  Given a before insert trigger on the parcels.parcels table will raise a 40001 exception 1 time where:
    | reference | PX-DBF-3011 |
  And a seeds/manifest-retried.yaml db seed
  When within 10s the before insert trigger on the parcels.parcels table was raised 1 time
  Then within 10s a selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
    | id     | ML-DBF-3011-1 |
    | status | IMPORTED      |
```

## Row locks

```gherkin
Scenario: A parcel cannot be changed while the depot dispatches it
  Given a seeds/dispatching.yaml db seed
  And the rows in the parcels.parcels table are locked where:
    | reference | PX-DSP-2001 |
  And a PATCH request to /api/parcels/PX-DSP-2001
  And a request payload using an application/json content example named 'Heavier'
  When the request is executed
  Then the response status code is 409

Scenario: A parcel can be changed again once the depot lets go of it
  Given a seeds/dispatched.yaml db seed
  And the rows in the parcels.parcels table are locked where:
    | reference | PX-DSP-2002 |
  And a PATCH request to /api/parcels/PX-DSP-2002
  And a request payload using an application/json content example named 'Heavier'
  When the row locks are released
  And the request is executed
  Then the response status code is 200
```

Lock rows to test timeouts and contention in your service. The lock holds until `the row locks are released` or the end of the scenario.

## Named databases

Every step has an `on <service>` form for scenarios with more than one database, for example `a selection of rows is retrieved from the parcels.parcels table on parcels-db where:` and `the selection on parcels-db has 1 row`.
