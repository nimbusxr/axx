package fixtures

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// describeTables is each table of a schema as a line: its columns in order, a required one
// marked *, then its foreign keys. Names are without their default schema, as a dump gives them
// and a migration does not.
func describeTables(s *ddlSchema) map[string]string {
	bare := func(name string) string {
		if schema, table, ok := strings.Cut(name, "."); ok && slices.Contains(defaultSchemas, schema) {
			return table
		}
		return name
	}
	out := map[string]string{}
	for name, t := range s.tables {
		var columns, refs []string
		for _, n := range t.order {
			if t.columns[n].required() {
				n += "*"
			}
			columns = append(columns, n)
		}
		for _, r := range t.refs {
			refs = append(refs, strings.Join(r.columns, " ")+" > "+bare(r.table))
		}
		slices.Sort(refs)
		line := strings.Join(columns, ", ")
		if len(refs) > 0 {
			line += " | " + strings.Join(refs, ", ")
		}
		out[bare(name)] = line
	}
	return out
}

func expectTables(t *testing.T, s *ddlSchema, want map[string]string) {
	t.Helper()
	got := describeTables(s)
	for table, line := range want {
		if got[table] != line {
			t.Errorf("%s:\n got  %s\n want %s", table, got[table], line)
		}
	}
	for table := range got {
		if _, ok := want[table]; !ok {
			t.Errorf("unexpected table %s: %s", table, got[table])
		}
	}
}

// Each database's own tool writes its schema (testdata/dumps/<engine>/schema*.sql; the
// README there says how), and the migration history it was written from reads to the same
// tables: what the database has, as its catalog says.
func TestDDLReadsWhatEachDatabaseWrites(t *testing.T) {
	for _, tc := range []struct {
		engine  string
		schemas []string
		tables  map[string]string
	}{{
		engine:  "postgresql",
		schemas: []string{"schema.sql", "schema-pg17.sql", "schema-pg18.sql"},
		tables: map[string]string{
			"depots":               "code*, name*, city",
			"couriers":             "id, depot_code, full_name*, phone | depot_code > depots",
			"parcels":              "id, tracking_number*, depot_code, weight_grams*, weight_kg, status*, order, label_note, sender | depot_code > depots",
			"deliveries":           "id, parcel_id*, courier_id, attempted_at, outcome | courier_id > couriers, parcel_id > parcels",
			"delivery_events":      "delivery_id*, recorded_at, kind*, detail | delivery_id > deliveries",
			"delivery_events_2026": "delivery_id*, recorded_at, kind*, detail | delivery_id > deliveries",
		},
	}, {
		engine:  "mysql",
		schemas: []string{"schema.sql", "schema-skip-comments.sql", "schema-routines.sql"},
		tables: map[string]string{
			"depots":     "region*, code*, name*, city",
			"couriers":   "id, full_name*, phone, depot_code | depot_code > depots",
			"parcels":    "id, tracking_number*, depot_code, weight_grams*, weight_kg, status*, order, label_note, sender | depot_code > depots",
			"deliveries": "id, parcel_id*, courier_id, attempted_at, outcome, proof_photo | courier_id > couriers, parcel_id > parcels",
		},
	}, {
		// MySQL's history, with MariaDB's DROP CONSTRAINT for MySQL's DROP CHECK.
		engine:  "mariadb",
		schemas: []string{"schema.sql"},
		tables: map[string]string{
			"depots":     "region*, code*, name*, city",
			"couriers":   "id, full_name*, phone, depot_code | depot_code > depots",
			"parcels":    "id, tracking_number*, depot_code, weight_grams*, weight_kg, status*, order, label_note, sender | depot_code > depots",
			"deliveries": "id, parcel_id*, courier_id, attempted_at, outcome, proof_photo | courier_id > couriers, parcel_id > parcels",
		},
	}, {
		engine:  "sqlserver",
		schemas: []string{"schema.sql"},
		tables: map[string]string{
			"depots":     "code*, name*, city",
			"couriers":   "id, depot_code, full_name*, phone | depot_code > depots",
			"parcels":    "id, tracking_number*, depot_code, weight_grams*, status*, label_note, sender, row_version, weight_kg, order | depot_code > depots",
			"deliveries": "id, parcel_id*, courier_id, attempted_at, outcome | courier_id > couriers, parcel_id > parcels",
		},
	}, {
		engine:  "oracle",
		schemas: []string{"schema.sql"},
		tables: map[string]string{
			"depots":     "code*, name*, city",
			"couriers":   "id, depot_code, full_name*, phone | depot_code > depots",
			"parcels":    "id, tracking_number*, depot_code, weight_grams*, weight_kg, status*, order, label_note, sender | depot_code > depots",
			"deliveries": "id, parcel_id*, courier_id, attempted_at, outcome, delivered_to | courier_id > couriers, parcel_id > parcels",
		},
	}, {
		engine:  "sqlite",
		schemas: []string{"schema.sql"},
		tables: map[string]string{
			"depots":     "code*, name*, city",
			"couriers":   "id, depot_code, full_name*, phone | depot_code > depots",
			"parcels":    "id, tracking_number*, depot_code, weight_grams*, weight_kg, status*, order, label_note, sender | depot_code > depots",
			"deliveries": "id, parcel_id*, courier_id, attempted_at, depot_code, outcome | courier_id > couriers, depot_code > depots, parcel_id > parcels",
		},
	}} {
		for _, path := range append([]string{"migrations"}, tc.schemas...) {
			t.Run(tc.engine+"/"+path, func(t *testing.T) {
				s, err := parseDDL(filepath.Join("testdata", "dumps", tc.engine, path))
				if err != nil {
					t.Fatal(err)
				}
				expectTables(t, s, tc.tables)
			})
		}
	}
}

// ddlDialectForms are forms each database's SQL has that the recorded histories leave out,
// and the tables the database has after them, as its catalog says.
var ddlDialectForms = []struct {
	name, sql string
	tables    map[string]string
}{{
	name: "postgresql",
	sql: `\set ON_ERROR_STOP on
CREATE TABLE depots (code text PRIMARY KEY);
CREATE TABLE scans (parcel_id bigint NOT NULL, depot_code text REFERENCES depots (code), scanned_at timestamptz NOT NULL)
    PARTITION BY RANGE (scanned_at);
CREATE TABLE scans_2026 PARTITION OF scans FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
ALTER TABLE scans ADD COLUMN kind text NOT NULL;
ALTER TABLE depots RENAME TO hubs;
CREATE SCHEMA tracking;
ALTER TABLE scans SET SCHEMA tracking;
CREATE TABLE depot_counters (
    depot_code text NOT NULL,
    next_number integer GENERATED ALWAYS AS IDENTITY,
    label text GENERATED ALWAYS AS (upper(depot_code)) STORED,
    tags text[] DEFAULT ARRAY['fragile;', 'this side up'] NOT NULL,
    template text DEFAULT $tpl$Parcel {id}; don't bend$tpl$ NOT NULL,
    note text DEFAULT E'it\'s; -- not a comment',
    key text NOT NULL
);
ALTER TABLE depot_counters ALTER COLUMN next_number DROP IDENTITY IF EXISTS;
ALTER TABLE depot_counters ALTER COLUMN label DROP EXPRESSION;
CREATE TABLE manifests ("constraint" text NOT NULL, "primary" text, index integer, CONSTRAINT manifests_check CHECK (index > 0));
ALTER TABLE manifests DROP CONSTRAINT manifests_check, DROP CONSTRAINT IF EXISTS manifests_pkey;
`,
	tables: map[string]string{
		"hubs":           "code*",
		"tracking.scans": "parcel_id*, depot_code, scanned_at*, kind* | depot_code > hubs",
		"scans_2026":     "parcel_id*, depot_code, scanned_at*, kind* | depot_code > hubs",
		"depot_counters": "depot_code*, next_number*, label, tags, template, note, key*",
		"manifests":      "constraint*, primary, index",
	},
}, {
	name: "mysql",
	sql: `/*!40101 SET NAMES utf8mb4 */;
# couriers, keyed by a number MySQL gives
CREATE TABLE couriers (
    id int NOT NULL AUTO_INCREMENT,
    ` + "`key`" + ` varchar(20) NOT NULL,
    phone varchar(20),
    greeting varchar(40) DEFAULT 'it\'s; "fine"',
    KEY couriers_key (` + "`key`" + `),
    PRIMARY KEY (id)
) ENGINE=InnoDB /*!80016 ENCRYPTION='N' */;
CREATE TABLE depots (code char(3) NOT NULL PRIMARY KEY);
ALTER TABLE couriers MODIFY id int NOT NULL;
ALTER TABLE couriers CHANGE phone mobile varchar(32) NOT NULL DEFAULT '' AFTER id;
ALTER TABLE couriers ADD status enum('active', 'resting') NOT NULL, ADD note text NULL FIRST;
ALTER TABLE couriers RENAME riders;
RENAME TABLE riders TO couriers_old, depots TO hubs;
ALTER TABLE couriers_old DROP PRIMARY KEY, DROP INDEX couriers_key, DROP greeting;
`,
	tables: map[string]string{
		"couriers_old": "note, id*, mobile, key*, status",
		"hubs":         "code*",
	},
}, {
	name: "sqlserver",
	sql: `CREATE TABLE [dbo].[couriers] (
    [id]    INT            IDENTITY (1, 1) NOT NULL,
    [name]  NVARCHAR (100) NOT NULL,
    [phone] VARCHAR (20)   NOT NULL,
    CONSTRAINT [pk_couriers] PRIMARY KEY CLUSTERED ([id] ASC)
) ON [PRIMARY]
GO
ALTER TABLE couriers ALTER COLUMN phone varchar(32)
ALTER TABLE couriers ADD shift_start time NOT NULL CONSTRAINT df_couriers_shift DEFAULT '08:00', shift_end time NOT NULL, badge int NULL
GO
ALTER TABLE couriers DROP COLUMN badge, shift_end;
EXEC sp_rename @objname = N'dbo.couriers.name', @newname = N'full_name', @objtype = N'COLUMN';
ALTER TABLE couriers DROP CONSTRAINT df_couriers_shift;
`,
	tables: map[string]string{
		"couriers": "id, full_name*, phone, shift_start*",
	},
}, {
	name: "oracle",
	sql: `CREATE TABLE "PARCELS"."COURIERS"
   (	"ID" NUMBER GENERATED BY DEFAULT ON NULL AS IDENTITY,
	"NAME" VARCHAR2(100) CONSTRAINT "COURIERS_NAME_NN" NOT NULL ENABLE,
	"PHONE" VARCHAR2(20) NOT NULL DISABLE,
	"SHIFT" VARCHAR2(10) DEFAULT ON NULL 'day' NOT NULL ENABLE,
	"BADGE" NUMBER,
	"VEHICLE" VARCHAR2(40)
   ) ;
COMMENT ON COLUMN parcels.couriers.shift IS q'{a courier's shift; "day" or "night"}';
BEGIN
    EXECUTE IMMEDIATE 'COMMENT ON TABLE parcels.couriers IS ''Couriers; and their shifts''';
END;
/
ALTER TABLE parcels.couriers MODIFY ("BADGE" NOT NULL);
ALTER TABLE parcels.couriers MODIFY ("ID" DROP IDENTITY);
ALTER TABLE parcels.couriers SET UNUSED (vehicle);
ALTER TABLE parcels.couriers DROP CONSTRAINT couriers_name_nn;
`,
	tables: map[string]string{
		"parcels.couriers": "id, name, phone, shift, badge*",
	},
}, {
	name: "sqlite",
	sql: `CREATE TABLE depots (code TEXT PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE couriers (id INTEGER PRIMARY KEY AUTOINCREMENT, depot_code TEXT REFERENCES depots, [order] INTEGER);
CREATE TABLE shifts (courier_id INTEGER, starts_at TEXT, PRIMARY KEY (courier_id, starts_at)) STRICT;
CREATE TABLE temp.scratch (a);
CREATE TEMP TABLE scratch2 (b);
`,
	tables: map[string]string{
		"depots":   "code, name*",
		"couriers": "id, depot_code, order | depot_code > depots",
		"shifts":   "courier_id*, starts_at*",
	},
}}

func TestDDLReadsEachDialectsForms(t *testing.T) {
	for _, tc := range ddlDialectForms {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorkspace(t)
			w.write("schema.sql", tc.sql)
			s, err := parseDDL(w.path("schema.sql"))
			if err != nil {
				t.Fatal(err)
			}
			expectTables(t, s, tc.tables)
		})
	}
}

// Statements end where each database's client ends them, and comments, strings and routine
// bodies hide what looks like an end.
func TestSplitSQLStatements(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mysql bool
		sql   string
		want  []string // each statement's tokens, strings without their quotes
	}{{
		name: "comments, strings and arrays",
		sql:  "\uFEFF-- a parcel's note; not SQL\r\nCREATE TABLE notes (body text DEFAULT 'a;b' /* ; */, tags text[] DEFAULT ARRAY[';'])\r\n;\r\n",
		want: []string{"CREATE TABLE notes ( body text DEFAULT a;b , tags text [ ] DEFAULT ARRAY [ ; ] )"},
	}, {
		name: "psql",
		sql: "\\restrict k3y\nCREATE FUNCTION label() RETURNS text AS $body$SELECT 'x;'$body$ LANGUAGE sql;\n" +
			"SELECT E'it\\'s; fine', $$a;b$$;\n\\unrestrict k3y\n",
		want: []string{"CREATE FUNCTION label ( ) RETURNS text AS SELECT 'x;' LANGUAGE sql", "SELECT it's; fine , a;b"},
	}, {
		name: "sql server batches",
		sql: "CREATE TABLE [dbo].[parcels] ([id] int)\nALTER TABLE parcels ADD [weight] int\nGO\n" +
			"CREATE TRIGGER stamp ON parcels AFTER INSERT AS\nBEGIN\n    UPDATE parcels SET weight = 1;\n    CREATE TABLE #seen (id int);\nEND;\ngo\n" +
			"EXEC sp_rename 'parcels', 'shipments'\n",
		want: []string{
			"CREATE TABLE dbo . parcels ( id int )",
			"ALTER TABLE parcels ADD weight int",
			"CREATE TRIGGER stamp ON parcels AFTER INSERT AS BEGIN UPDATE parcels SET weight = 1 ; CREATE TABLE #seen ( id int ) ; END ;",
			"EXEC sp_rename parcels , shipments",
		},
	}, {
		name: "sql*plus",
		sql: "CREATE TABLE parcels (note varchar2(40) DEFAULT q'[it's; here]');\n" +
			"CREATE OR REPLACE TRIGGER stamp BEFORE INSERT ON parcels FOR EACH ROW\nDECLARE\n  n number;\nBEGIN\n  :NEW.note := 'x';\nEND;\n/\n" +
			"RENAME parcels TO shipments;\n",
		want: []string{
			"CREATE TABLE parcels ( note varchar2 ( 40 ) DEFAULT it's; here )",
			"CREATE OR REPLACE TRIGGER stamp BEFORE INSERT ON parcels FOR EACH ROW DECLARE n number ; BEGIN : NEW . note : = x ; END ;",
			"RENAME parcels TO shipments",
		},
	}, {
		name:  "mysql",
		mysql: true,
		sql: "# a comment; still one\nSELECT 1--1;\n/*!40101 SET NAMES utf8mb4 */;\nDELIMITER $$\n" +
			"CREATE TRIGGER stamp BEFORE INSERT ON parcels FOR EACH ROW BEGIN SET NEW.note = 'it\\'s; $'; END$$\nDELIMITER ;\n" +
			"CREATE TABLE `parcels` (note text DEFAULT \"x;\");",
		want: []string{
			"SELECT 1 - - 1",
			"SET NAMES utf8mb4",
			"CREATE TRIGGER stamp BEFORE INSERT ON parcels FOR EACH ROW BEGIN SET NEW . note = it's; $ ; END",
			"CREATE TABLE parcels ( note text DEFAULT x; )",
		},
	}, {
		name: "sqlite trigger",
		sql:  "CREATE TRIGGER stamp AFTER INSERT ON parcels BEGIN UPDATE parcels SET note = CASE WHEN 1 THEN 'x' END; END;\nCREATE TABLE notes (a);",
		want: []string{
			"CREATE TRIGGER stamp AFTER INSERT ON parcels BEGIN UPDATE parcels SET note = CASE WHEN 1 THEN x END ; END",
			"CREATE TABLE notes ( a )",
		},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, stmt := range splitSQL(tc.sql, tc.mysql) {
				got = append(got, tokenText(stmt))
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}
}
