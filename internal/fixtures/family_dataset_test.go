package fixtures

import (
	"bytes"
	"strings"
	"testing"
)

// The dataset family: tabular documents, per-table row templates, column
// defaults, table.column identities per row, optional DDL, and the three
// DBUnit renderings.

const missions = "seeds/missions"

func datasetWorkspace(t *testing.T) *workspace {
	t.Helper()
	return newWorkspace(t).seed("dataset-corpus")
}

func TestDatasetDocumentsResolveTemplatesIdentitiesAndDDLOrder(t *testing.T) {
	files := datasetWorkspace(t).expand()
	expect(t, string(files[missions+"/mission-alpha.yaml"]), `space.missions:
- id: msn-mission-alpha
  name: Alpha
  metadata: "{\"priority\": \"low\"}"
`)
	beta := string(files[missions+"/mission-beta.yaml"])
	mustContain(t, beta, "id: msn-mission-beta", "id: msn-mission-beta-2", "id: craft-beta", "space.spacecraft:")
	if strings.Index(beta, "space.missions:") > strings.Index(beta, "space.spacecraft:") {
		t.Error("DDL table order")
	}
}

func TestDatasetStabilityAndDrift(t *testing.T) {
	w := datasetWorkspace(t)
	first, second := w.expand(), w.expand()
	for p, b := range first {
		if !bytes.Equal(b, second[p]) {
			t.Errorf("%s not byte-stable", p)
		}
	}
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	w.replace(missions+"/mission-alpha.yaml", "Alpha", "Alpha Prime")
	if f := w.check(); len(f) != 1 || !strings.Contains(f[0], "mission-alpha.yaml") {
		t.Fatalf("failures: %v", f)
	}
}

func TestDatasetTypoGuards(t *testing.T) {
	w := datasetWorkspace(t)
	f := missions + "/mission-alpha.fixture.yaml"
	w.replace(f, `name: "Alpha"`, `nam3: "Alpha"`)
	mustErrContain(t, w.expandErr(), "nam3", "space.missions")
	w.write(f, "data:\n  space.missons:\n    - name: \"Alpha\"\n")
	mustErrContain(t, w.expandErr(), "space.missons", "space.spacecraft")
}

func TestDatasetMigrationRenameFailsStaleFactories(t *testing.T) {
	w := datasetWorkspace(t)
	w.write("ddl/02-rename.sql", "ALTER TABLE space.missions RENAME COLUMN launch_date TO liftoff_at;\n")
	w.replace(missions+"/mission-seeds.factory.yaml", "schema: ../../ddl/01-schema.sql", "schema: ../../ddl")
	mustErrContain(t, w.expandErr(), "launch_date", "liftoff_at")
}

func TestDatasetMissingRequiredColumn(t *testing.T) {
	w := datasetWorkspace(t)
	w.write(missions+"/mission-alpha.fixture.yaml", "data:\n  space.missions:\n    - status: \"planned\"\n")
	mustErrContain(t, w.expandErr(), "space.missions.name", "mission-alpha", "defaults:")
}

func TestDatasetColumnDefaultsCoverEveryRow(t *testing.T) {
	w := datasetWorkspace(t)
	w.write("ddl/02-add-region.sql", "ALTER TABLE space.missions ADD COLUMN region VARCHAR(20) NOT NULL;\n")
	f := missions + "/mission-seeds.factory.yaml"
	w.replace(f, "schema: ../../ddl/01-schema.sql", "schema: ../../ddl")
	w.replace(f, "identity:", "defaults:\n  space.missions.region: \"us-east\"\nidentity:")
	if n := strings.Count(string(w.expand()[missions+"/mission-beta.yaml"]), "region: us-east"); n != 2 {
		t.Errorf("both mission rows: %d", n)
	}
}

func TestDatasetConformanceLintsHandWrittenSeeds(t *testing.T) {
	w := datasetWorkspace(t)
	w.write("seeds/legacy/good.yaml", "space.missions:\n  - id: \"legacy-1\"\n    name: \"Legacy\"\nspace.spacecraft:\n  - id: \"craft-legacy\"\n    name: \"Old Faithful\"\n    capacity: 3\n")
	w.write("seeds/legacy/bad.yaml", "space.missions:\n  - id: \"legacy-2\"\n    destinationn: \"Pluto\"\n")
	w.conformance(ConformanceRule{Name: "legacy seeds", FilePatterns: []string{"seeds/legacy/*.yaml"}, SchemaType: "dataset", SchemaRef: "ddl/01-schema.sql"})
	f := filtered(w.check(), "conformance")
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "bad.yaml", "destinationn")
}

func TestDatasetEmitsNoLintRules(t *testing.T) {
	if datasetWorkspace(t).expand()[DefaultLintOutput] != nil {
		t.Error("datasets emit no structural lint rules")
	}
}

func TestDatasetXMLFormat(t *testing.T) {
	w := datasetWorkspace(t)
	w.replace(missions+"/mission-seeds.factory.yaml", "family: dataset", "family: dataset\n  options: { format: xml }")
	files := w.expand()
	expect(t, string(files[missions+"/mission-alpha.xml"]), `<?xml version="1.0" encoding="UTF-8"?>
<dataset>
  <space.missions id="msn-mission-alpha" name="Alpha" metadata="{&quot;priority&quot;: &quot;low&quot;}"/>
</dataset>
`)
	beta := string(files[missions+"/mission-beta.xml"])
	mustContain(t, beta, `<space.missions id="msn-mission-beta"`, `<space.missions id="msn-mission-beta-2"`, `<space.spacecraft id="craft-beta"`)
}

func TestDatasetCSVFormat(t *testing.T) {
	w := datasetWorkspace(t)
	w.replace(missions+"/mission-seeds.factory.yaml", "family: dataset", "family: dataset\n  options: { format: csv }")
	files := w.expand()
	expect(t, string(files[missions+"/mission-beta/table-ordering.txt"]), "space.missions\nspace.spacecraft\n")
	m := string(files[missions+"/mission-beta/space.missions.csv"])
	if !strings.HasPrefix(m, "id,name,status,launch_date,budget,metadata\n") {
		t.Errorf("header:\n%s", m)
	}
	mustContain(t, m, "msn-mission-beta,Beta,launched,2026-01-01 10:00:00,500000.5,", "msn-mission-beta-2,Beta Backup,planned,null,null,", `"{\"priority\": \"low\"}"`)
	expect(t, string(files[missions+"/mission-beta/space.spacecraft.csv"]), "id,name,capacity\ncraft-beta,Beta Shuttle,4\n")
}

func TestDatasetSchemaFreeMechanics(t *testing.T) {
	w := datasetWorkspace(t)
	f := missions + "/mission-seeds.factory.yaml"
	w.replace(f, "  schema: ../../ddl/01-schema.sql\n", "")
	w.replace(f, "identity:", "defaults:\n  space.missions.status: \"planned\"\nidentity:")
	files := w.expand()
	mustContain(t, string(files[missions+"/mission-alpha.yaml"]), "id: msn-mission-alpha", "metadata:", "status: planned")
	beta := string(files[missions+"/mission-beta.yaml"])
	if strings.Index(beta, "space.missions:") > strings.Index(beta, "space.spacecraft:") {
		t.Error("authored order")
	}
	w = datasetWorkspace(t)
	w.replace(f, "  schema: ../../ddl/01-schema.sql\n", "")
	w.replace(f, "identity:", "defaults:\n  status: \"planned\"\nidentity:")
	mustErrContain(t, w.expandErr(), "bare-name default 'status'", "dotted table.column")
}

func TestDatasetOptionErrors(t *testing.T) {
	w := datasetWorkspace(t)
	w.replace(missions+"/mission-seeds.factory.yaml", "family: dataset", "family: dataset\n  options: { format: tsv }")
	mustErrContain(t, w.expandErr(), "options.format", "[csv, xml, yaml]")
	w = datasetWorkspace(t)
	w.replace(missions+"/mission-seeds.factory.yaml", "  schema: ../../ddl/01-schema.sql", "  schema: ../../ddl/01-schema.sql\n  options: { table: space.missions }")
	mustErrContain(t, w.expandErr(), "options.table was removed")
}

func TestDatasetConformanceLintsXMLAndCSVRenderings(t *testing.T) {
	w := datasetWorkspace(t)
	w.write("seeds/legacy/bad.xml", "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<dataset>\n  <space.missions id=\"x-1\" nam3=\"Typo\"/>\n</dataset>\n")
	w.write("seeds/legacy/bad-table.csv", "id,name\nx-2,Legacy\n")
	w.conformance(
		ConformanceRule{Name: "legacy xml datasets", FilePatterns: []string{"seeds/legacy/*.xml"}, SchemaType: "dataset", SchemaRef: "ddl/01-schema.sql"},
		ConformanceRule{Name: "legacy csv datasets", FilePatterns: []string{"seeds/legacy/*.csv"}, SchemaType: "dataset", SchemaRef: "ddl/01-schema.sql"},
	)
	f := filtered(w.check(), "conformance")
	if len(f) != 2 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, strings.Join(f, "\n"), "bad.xml", "nam3", "bad-table.csv", "bad-table")
}

func TestDatasetAdoptionConvertsWholeSeedDocuments(t *testing.T) {
	w := datasetWorkspace(t)
	w.write("features/return/seeds/full-refund.yaml", `space.missions:
  - id: "ret-mission-1"
    name: "Refund Mission"
    status: "launched"
    metadata: '{"priority": "high"}'
space.spacecraft:
  - id: "ret-craft-1"
    name: "Refund One"
    capacity: 5
`)
	w.write("features/return/seeds/partial-refund.yaml", `space.missions:
  - id: "ret-mission-2"
    name: "Partial Mission"
    status: "launched"
    metadata: '{"priority": "high"}'
space.spacecraft:
  - id: "ret-craft-2"
    name: "Refund Two"
    capacity: 5
`)
	res, err := w.adopter().Adopt("dataset", "ddl/01-schema.sql", "features/return/seeds/*.yaml", "return-seeds", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "2/2 deep-equal")
	mustContain(t, w.read("features/return/seeds/return-seeds.prototype.yaml"), "status: launched", "capacity: 5")
	mustContain(t, w.read("features/return/seeds/return-seeds.factory.yaml"), "space.missions.id", "space.spacecraft.id")
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Errorf("check: %v", f)
	}
}

func TestDatasetAdoptionDropsOverlappingIdentityColumns(t *testing.T) {
	w := datasetWorkspace(t)
	w.write("features/echo/seeds/mission-one.yaml", "space.missions:\n  - id: \"msn-1\"\n    name: \"One\"\n    status: \"launched\"\n    metadata: \"{}\"\nspace.spacecraft:\n  - id: \"msn-1\"\n    name: \"Craft One\"\n    capacity: 3\n")
	w.write("features/echo/seeds/mission-two.yaml", "space.missions:\n  - id: \"msn-2\"\n    name: \"Two\"\n    status: \"launched\"\n    metadata: \"{}\"\nspace.spacecraft:\n  - id: \"msn-2\"\n    name: \"Craft Two\"\n    capacity: 3\n")
	res, err := w.adopter().Adopt("dataset", "ddl/01-schema.sql", "features/echo/seeds/*.yaml", "echo-seeds", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "2/2 deep-equal")
	factory := w.read("features/echo/seeds/echo-seeds.factory.yaml")
	mustContain(t, factory, "space.missions.id")
	if strings.Contains(factory, "space.spacecraft.id") {
		t.Errorf("overlapping column kept:\n%s", factory)
	}
	w.generate()
	if f := w.check(); len(f) != 0 {
		t.Errorf("check: %v", f)
	}
}

func TestDDLParser(t *testing.T) {
	w := newWorkspace(t)
	w.write("schema.sql", `-- comment
CREATE TABLE IF NOT EXISTS "Shop"."Orders" (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(10) NOT NULL,
    total NUMERIC(10,2) NOT NULL DEFAULT 0,
    note TEXT, /* inline ; comment */
    made_at TIMESTAMP NOT NULL GENERATED ALWAYS AS (now()) STORED,
    CONSTRAINT orders_code UNIQUE (code)
);
INSERT INTO shop.orders VALUES ('a;b', '(');
ALTER TABLE shop.orders ADD COLUMN IF NOT EXISTS region TEXT NOT NULL, DROP COLUMN note;
ALTER TABLE ONLY shop.orders RENAME COLUMN code TO sku;
ALTER TABLE nowhere ADD COLUMN x INT;
`)
	s, err := parseDDL(w.path("schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	tbl := s.table("SHOP.ORDERS")
	if tbl == nil {
		t.Fatalf("tables: %s", s.tableNames())
	}
	expect(t, tbl.names(), "[id, total, made_at, region, sku]")
	for name, required := range map[string]bool{"id": false, "total": false, "made_at": false, "region": true, "sku": true} {
		if tbl.columns[name].required != required {
			t.Errorf("%s required = %v", name, !required)
		}
	}
}
