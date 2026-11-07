# Schemas the databases' own tools write

The DDL reader (`internal/fixtures/ddl.go`) reads a factory's `schema:`: a folder of migrations, or
one file a database's own tool wrote from the migrated database. These folders hold both, for
each database, as the tools wrote them; `TestDDLReadsWhatEachDatabaseWrites` (`ddl_test.go`)
reads each to the tables the database has.

- `<engine>/migrations/NN-*.sql`: a migration history in the parcels domain, applied in order with
  the engine's own client.
- `<engine>/schema*.sql`: what the engine's tool wrote from the database those migrations made.

They are recorded outputs of the real tools: never edit them by hand. To change one, change its
migrations and record it again with the commands below.

## Versions

| Engine | Database (image) | Tool |
| --- | --- | --- |
| `postgresql` | `postgres:16` (16.15), `postgres:17` (17.11), `postgres:18` (18.6) | each image's `pg_dump` |
| `mysql` | `mysql:8.4` (8.4.13) | its `mysqldump` 8.4.13 |
| `mariadb` | `mariadb:11.4` (11.4.13) | its `mariadb-dump` |
| `sqlserver` | `mcr.microsoft.com/mssql/server:2022-latest` (2022 CU27-GDR, 16.0.4305.5, amd64) | SqlPackage 170.5.96 on `mcr.microsoft.com/dotnet/runtime:10.0` (.NET 10.0.14, arm64) |
| `oracle` | `gvenzl/oracle-free:23-slim-faststart` (Oracle AI Database 26ai Free 23.26.3) | `DBMS_METADATA.GET_DDL` through its SQL\*Plus 23.26.3 |
| `sqlite` | `modernc.org/sqlite` v1.61.1 (SQLite 3.54.1) | the `sql` of `sqlite_master` |

The images were these digests:

```text
postgres@sha256:a3b7f434b2dc57ce85a67e171163eb8ab1a1ebcb39d27484661f26b1dfbe30d6   (postgres:16)
postgres@sha256:ff87138949ad9a46f32ff23bd6852b52ce024a13dffe9b4087be98292cdd3b4f   (postgres:17)
postgres@sha256:e3d24f6906d0760bf4da0dae872415391ededefad23fd471c47dc53b0ddf3c42   (postgres:18)
mysql@sha256:a0225d1ea6631c8bc2385dc4c2faba17bbf1834ecf369e1659a9a382bd8d5dd7
mariadb@sha256:d1891ede5a559f1086f55053bb8cc1569b0298338c4d72d71725a7f74f6ba620
mcr.microsoft.com/mssql/server@sha256:cf8f0ab2ebe61feec2eed2629061a07d247f791c2a8ddda74a4ac9f06a291718
mcr.microsoft.com/dotnet/runtime@sha256:4f1f30cf0bb4ab0c8b2daa84613051683456f8df4a9bf7c812e20d74962992ac
gvenzl/oracle-free@sha256:f5ff19033860d662c821cb04eb10483fa94f14f78eae252d054291ea07028093
```

SqlPackage is the NuGet package of the `microsoft.sqlpackage` .NET tool, which is
framework-dependent, so it runs natively on arm64:
`https://api.nuget.org/v3-flatcontainer/microsoft.sqlpackage/170.5.96/microsoft.sqlpackage.170.5.96.nupkg`,
SHA-256 `a5393d5f4e92699a93b58dd6ef7024e1394fd267acc60cfef537e04d1fb58865` (its SHA-512 is the
`packageHash` NuGet's catalog gives). Azure SQL Edge (`mcr.microsoft.com/azure-sql-edge:latest`,
15.0.2000.1574, arm64) also takes these migrations, and SqlPackage writes the same statements
from it, its tables in another order.

## Recording

The blocks below, run in order with bash from the repository root, record every file again. The
containers share a private network and publish no ports. On an Apple-silicon Mac (Docker 29.7)
the databases were ready in about 1 s (PostgreSQL), 5 s (MySQL), 6 s (SQL Server, amd64 under
emulation) and 4 to 7 s (Oracle); SqlPackage's extract took 5 s. `pg_dump` writes a new random
`\restrict` key each run; nothing else changes from one run to the next.

```sh
set -euo pipefail
DUMPS=internal/fixtures/testdata/dumps
docker network create axxdump-net
```

### PostgreSQL

`psql -1` applies each migration in one transaction, as Flyway does. `pg_isready` over TCP
answers only once the image's first-start initialization is done.

```sh
for v in 16 17 18; do
  docker run -d --name axxdump-pg$v --network axxdump-net \
    -e POSTGRES_PASSWORD=parcels -e POSTGRES_DB=parcels postgres:$v
  until docker exec axxdump-pg$v pg_isready -q -h 127.0.0.1 -U postgres; do sleep 0.2; done
  docker cp -q $DUMPS/postgresql/migrations axxdump-pg$v:/migrations
  for f in $DUMPS/postgresql/migrations/*.sql; do
    docker exec axxdump-pg$v psql -X -q -v ON_ERROR_STOP=1 -1 -U postgres -d parcels -f /migrations/$(basename $f)
  done
done
docker exec axxdump-pg16 pg_dump --schema-only --no-owner --no-privileges -U postgres -d parcels > $DUMPS/postgresql/schema.sql
docker exec axxdump-pg17 pg_dump --schema-only --no-owner --no-privileges -U postgres -d parcels > $DUMPS/postgresql/schema-pg17.sql
docker exec axxdump-pg18 pg_dump --schema-only --no-owner --no-privileges -U postgres -d parcels > $DUMPS/postgresql/schema-pg18.sql
```

### MySQL

The `mysql` client applies each file, which needs it for `DELIMITER`. `mysqladmin ping` over
TCP answers only once the image's first-start initialization is done.

```sh
docker run -d --name axxdump-mysql --network axxdump-net \
  -e MYSQL_ROOT_PASSWORD=parcels -e MYSQL_DATABASE=parcels mysql:8.4
until docker exec axxdump-mysql mysqladmin ping --silent -h 127.0.0.1 -uroot -pparcels 2>/dev/null; do sleep 0.2; done
docker cp -q $DUMPS/mysql/migrations axxdump-mysql:/migrations
for f in $DUMPS/mysql/migrations/*.sql; do
  docker exec axxdump-mysql sh -c "mysql -uroot -pparcels parcels < /migrations/$(basename $f)"
done
docker exec axxdump-mysql mysqldump -uroot -pparcels --no-data --compact parcels > $DUMPS/mysql/schema.sql
docker exec axxdump-mysql mysqldump -uroot -pparcels --no-data --skip-comments --skip-add-drop-table parcels > $DUMPS/mysql/schema-skip-comments.sql
docker exec axxdump-mysql mysqldump -uroot -pparcels --no-data --compact --routines parcels > $DUMPS/mysql/schema-routines.sql
```

### MariaDB

MySQL's history, with MariaDB's `DROP CONSTRAINT` where MySQL's has `DROP CHECK`, which MariaDB
has not got. `mariadb-dump` starts with a `/*M!999999\- enable the sandbox mode */` line.

```sh
docker run -d --name axxdump-mariadb --network axxdump-net \
  -e MARIADB_ROOT_PASSWORD=parcels -e MARIADB_DATABASE=parcels mariadb:11.4
until docker exec axxdump-mariadb mariadb-admin ping --silent -h 127.0.0.1 -uroot -pparcels 2>/dev/null; do sleep 0.5; done
docker cp -q $DUMPS/mariadb/migrations axxdump-mariadb:/migrations
for f in $DUMPS/mariadb/migrations/*.sql; do
  docker exec axxdump-mariadb sh -c "mariadb -uroot -pparcels parcels < /migrations/$(basename $f)"
done
docker exec axxdump-mariadb mariadb-dump -uroot -pparcels --no-data --compact parcels > $DUMPS/mariadb/schema.sql
```

### SQL Server

`sqlcmd -I` turns `QUOTED_IDENTIFIER` on, as JDBC (and so Flyway) does; without it the computed
column of `05-couriers.sql` fails. A `SELECT 1` that `sqlcmd` gets through is the readiness
check. SqlPackage runs from a .NET runtime container on the same network.

```sh
docker run -d --name axxdump-mssql --network axxdump-net \
  -e ACCEPT_EULA=Y -e MSSQL_SA_PASSWORD=Parcels-Secret-1 mcr.microsoft.com/mssql/server:2022-latest
sqlcmd() { docker exec axxdump-mssql /opt/mssql-tools18/bin/sqlcmd -C -b -I -U sa -P Parcels-Secret-1 "$@"; }
until sqlcmd -Q "SELECT 1" >/dev/null 2>&1; do sleep 0.2; done
sqlcmd -Q "CREATE DATABASE parcels"
docker cp -q $DUMPS/sqlserver/migrations axxdump-mssql:/tmp/migrations
for f in $DUMPS/sqlserver/migrations/*.sql; do
  sqlcmd -d parcels -i /tmp/migrations/$(basename $f)
done

SQLPACKAGE=$(mktemp -d)
curl -sSfL -o $SQLPACKAGE/sqlpackage.nupkg \
  https://api.nuget.org/v3-flatcontainer/microsoft.sqlpackage/170.5.96/microsoft.sqlpackage.170.5.96.nupkg
echo "a5393d5f4e92699a93b58dd6ef7024e1394fd267acc60cfef537e04d1fb58865  $SQLPACKAGE/sqlpackage.nupkg" | shasum -a 256 -c
mkdir $SQLPACKAGE/tool $SQLPACKAGE/out && chmod 777 $SQLPACKAGE/out
unzip -q $SQLPACKAGE/sqlpackage.nupkg -d $SQLPACKAGE/tool
docker run --rm --network axxdump-net \
  -v $SQLPACKAGE/tool/tools/net10.0/any:/sqlpackage:ro -v $SQLPACKAGE/out:/out \
  mcr.microsoft.com/dotnet/runtime:10.0 dotnet /sqlpackage/sqlpackage.dll \
  /Action:Extract /p:ExtractTarget=File /TargetFile:/out/schema.sql \
  /SourceServerName:axxdump-mssql /SourceDatabaseName:parcels \
  /SourceUser:sa /SourcePassword:Parcels-Secret-1 /SourceTrustServerCertificate:True
cp $SQLPACKAGE/out/schema.sql $DUMPS/sqlserver/schema.sql
```

### Oracle

SQL\*Plus applies each migration as the image's `parcels` user, in its `FREEPDB1`. The image logs
`DATABASE IS READY TO USE!` once that user exists. `GET_DDL` writes each table with its
constraints, without its owner (there is one schema) or its storage.

```sh
docker run -d --name axxdump-oracle --network axxdump-net \
  -e ORACLE_PASSWORD=Parcels-Secret-1 -e APP_USER=parcels -e APP_USER_PASSWORD=parcels \
  gvenzl/oracle-free:23-slim-faststart
until docker logs axxdump-oracle 2>&1 | grep -q "DATABASE IS READY TO USE!"; do sleep 0.2; done
docker cp -q $DUMPS/oracle/migrations axxdump-oracle:/tmp/migrations
for f in $DUMPS/oracle/migrations/*.sql; do
  printf 'WHENEVER SQLERROR EXIT FAILURE\n@/tmp/migrations/%s\nEXIT\n' $(basename $f) |
    docker exec -i axxdump-oracle sqlplus -s -L parcels/parcels@//localhost/FREEPDB1
done
cat <<'EOF' | docker exec -i axxdump-oracle sh -c 'cat > /tmp/get-ddl.sql'
SET LONG 1000000 LONGCHUNKSIZE 1000000 PAGESIZE 0 LINESIZE 32767 FEEDBACK OFF HEADING OFF TRIMSPOOL ON TRIMOUT ON VERIFY OFF ECHO OFF
WHENEVER SQLERROR EXIT FAILURE
BEGIN
  DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'SQLTERMINATOR', TRUE);
  DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'PRETTY', TRUE);
  DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'SEGMENT_ATTRIBUTES', FALSE);
  DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'STORAGE', FALSE);
  DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'TABLESPACE', FALSE);
  DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'CONSTRAINTS', TRUE);
  DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'REF_CONSTRAINTS', TRUE);
  DBMS_METADATA.SET_TRANSFORM_PARAM(DBMS_METADATA.SESSION_TRANSFORM, 'EMIT_SCHEMA', FALSE);
END;
/
SELECT DBMS_METADATA.GET_DDL('TABLE', table_name) FROM user_tables ORDER BY table_name;
EXIT
EOF
docker exec axxdump-oracle sqlplus -s -L parcels/parcels@//localhost/FREEPDB1 @/tmp/get-ddl.sql > $DUMPS/oracle/schema.sql
```

### SQLite

A Go program applies each migration with `modernc.org/sqlite` (pure Go, foreign keys on) and
writes the `sql` of every table in `sqlite_master`, in its order, each followed by `;`.

```sh
SQLITEDUMP=$(mktemp -d)
cat > $SQLITEDUMP/main.go <<'EOF'
package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

func main() {
	migrations, out := os.Args[1], os.Args[2]
	dir, err := os.MkdirTemp("", "parcels")
	check(err)
	defer os.RemoveAll(dir)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "parcels.db")+"?_pragma=foreign_keys(1)")
	check(err)
	db.SetMaxOpenConns(1)
	files, err := filepath.Glob(filepath.Join(migrations, "*.sql"))
	check(err)
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		check(err)
		_, err = db.Exec(string(b))
		check(err)
	}
	rows, err := db.Query("SELECT sql FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY rowid")
	check(err)
	var schema strings.Builder
	for rows.Next() {
		var text string
		check(rows.Scan(&text))
		schema.WriteString(text + ";\n")
	}
	check(rows.Err())
	check(os.WriteFile(out, []byte(schema.String()), 0o644))
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
EOF
(cd $SQLITEDUMP && go mod init sqlitedump && go get modernc.org/sqlite@v1.61.1)
(cd $SQLITEDUMP && go run . "$OLDPWD/$DUMPS/sqlite/migrations" "$OLDPWD/$DUMPS/sqlite/schema.sql")
```

### Cleaning up

```sh
docker rm -fv axxdump-pg16 axxdump-pg17 axxdump-pg18 axxdump-mysql axxdump-mariadb axxdump-mssql axxdump-oracle
docker network rm axxdump-net
rm -rf $SQLPACKAGE $SQLITEDUMP
```

## Which columns are required

A column is required when the database refuses an INSERT that leaves it out. The expectations in
`ddl_test.go` are what each database's catalog said after its migrations, checked with INSERTs
where the catalog does not tell (MySQL fills a NOT NULL `enum` with its first value; Oracle's
`DEFAULT NULL` is no default; SQLite lets a primary key of a rowid table be NULL unless the table
is `STRICT` or the key is its `INTEGER` rowid):

- PostgreSQL: `attnotnull AND NOT atthasdef AND attidentity = '' AND attgenerated = ''` in `pg_attribute`.
- MySQL and MariaDB: `is_nullable = 'NO' AND column_default IS NULL` and no `auto_increment` or
  `GENERATED` in `extra`, in `information_schema.columns`.
- SQL Server: `is_nullable = 0 AND default_object_id = 0 AND is_identity = 0 AND is_computed = 0`
  and not `rowversion`, in `sys.columns`.
- Oracle: `nullable = 'N'`, no `data_default` (or `NULL`), `identity_column = 'NO'` and
  `virtual_column = 'NO'`, in `user_tab_cols`.
- SQLite: an INSERT of every other column, for each column.
