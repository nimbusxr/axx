// Package gcpbigquery is the gcp-bigquery pack: rows seeded into BigQuery
// tables and the rows the services under test write.
package gcpbigquery

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"google.golang.org/api/iterator"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/columntype"
	gcpcore "github.com/nimbusxr/axx/packs/gcp/core"
)

const name = "gcp-bigquery"

// maxRows is how many rows of a table a check reads.
const maxRows = 5000

const packDoc = `Seed BigQuery tables and check the rows your services write.

The steps use the scenario's project (` + "`the {word} gcp project with the following properties:`" + `, from gcp-core). Tables are named ` + "`dataset.table`" + ` (in the project) or ` + "`project.dataset.table`" + `.

A **seed** is a YAML or JSON file that maps tables to the rows to insert (a streaming insert, ` + "`tabledata.insertAll`" + `):

` + "```yaml" + `
billing.carrier_rates:
  - carrier: KESTREL
    service: express
    price_per_kg: 1.35
` + "```" + `

**Checks** wait (10 seconds unless ` + "`within {duration}`" + ` says otherwise) until the table has a row, or a number of rows, meeting every condition: ` + "`column | value`" + ` rows, with a dotted path into ` + "`RECORD`" + ` columns (` + "`address.city`" + `), and values of their column's type, which fail at once and say what to write otherwise: an ` + "`INT64`" + ` is an integer (` + "`2`" + `), a ` + "`FLOAT64`" + ` or a ` + "`NUMERIC`" + ` a decimal (` + "`1.35`" + `, and ` + "`2.0`" + `, a whole one too), a ` + "`STRING`" + ` that reads as a number is in double quotes (` + "`\"01067\"`" + `), a ` + "`TIMESTAMP`" + ` in RFC 3339 (` + "`2026-09-24T09:30:00Z`" + `), a ` + "`DATE`" + ` like ` + "`2026-09-24`" + `, and ` + "`null`" + ` NULL. A check reads the columns its conditions name, of up to 5,000 rows of the table: check the tables your scenarios write, not warehouse-size ones.`

// Pack returns the gcp-bigquery pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      name,
		Namespace: name,
		Doc:       packDoc,
		Requires:  []string{gcpcore.Name},
		Steps: []core.StepDef{
			{
				ID: name + ".seed", Keyword: "Given", Since: "0.1.0",
				Expr: "a {filepath} bigquery seed",
				Doc: "Insert the rows of a seed file into their tables: YAML or JSON that maps tables (`dataset.table` or " +
					"`project.dataset.table`) to lists of rows. The rows are streamed in (`tabledata.insertAll`).",
				Examples: []string{"Given a seeds/carrier-rates.yaml bigquery seed"},
				Run:      seed,
			},
			{
				ID: name + ".row", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
				Expr:  "[[within {duration} ]]the {word} bigquery table has a row where:",
				Doc:   "Check that the table has a row with those values in those columns." + checkDetails,
				Table: conditions,
				Examples: []string{
					"Then within 30s the billing.invoice_lines bigquery table has a row where:\n" +
						"  | invoice  | INV-2026-09-KES2 |\n" +
						"  | parcel   | PX-5103          |\n" +
						"  | status   | OVERCHARGED      |\n" +
						"  | billed   | 2.5              |\n" +
						"  | expected | 1.35             |",
				},
				Run: func(sc *core.Scenario, a core.Args) error {
					return expect(sc, a, a.String(1), -1)
				},
			},
			{
				ID: name + ".rows", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
				Expr:  "[[within {duration} ]]the {word} bigquery table has {int} row(s) where:",
				Doc:   "Check that exactly that many rows of the table have those values in those columns." + checkDetails,
				Table: conditions,
				Examples: []string{
					"Then the billing.invoice_lines bigquery table has 2 rows where:\n" +
						"  | invoice | INV-2026-09-KES1 |\n" +
						"  | status  | MATCHED          |",
				},
				Run: func(sc *core.Scenario, a core.Args) error {
					return expect(sc, a, a.String(1), a.Int(2))
				},
			},
		},
	}
}

// checkDetails is what the checks' docs say after what they check.
const checkDetails = "\n\n" +
	"- The check waits for it: 10 seconds, or `within {duration}`.\n" +
	"- Name the table `dataset.table`, or `project.dataset.table` for another project's.\n" +
	"- An `INT64` is an integer, and a `FLOAT64`, `NUMERIC` or `BIGNUMERIC` a decimal, a whole one too (`2.0`); " +
	"a `TIMESTAMP` is in RFC 3339 (`2026-09-24T09:30:00Z`), a `DATE` like `2026-09-24`, a `DATETIME` like " +
	"`2026-09-24T09:30:00`, and `BYTES` in base64.\n" +
	"- A value of another type than its column's fails at once and says what to write (`12` on a `FLOAT64` asks for `12.0`, " +
	"and `01067` or `10115` on a `STRING` for `\"10115\"`).\n" +
	"- The check reads only the columns the step names, of up to 5,000 rows."

// conditions is what the checks' tables hold. They do not take `undefined`:
// a table's schema gives every row every column, whose value may be NULL, and
// the query of a column the schema lacks fails. No row lacks a column.
var conditions = &core.TableDoc{
	Columns: []string{"column", "value"},
	Note: "Each row names a column, or a dotted path into a `RECORD` column (`address.city`), and the value in it; `null` is NULL, " +
		"which is how a row has no value: every row has every column.",
	Values: core.JSONValues,
}

func client(sc *core.Scenario) (*bigquery.Client, *gcpcore.Project, error) {
	p, err := gcpcore.Default(sc)
	if err != nil {
		return nil, nil, err
	}
	c, err := gcpcore.Client(sc.Context(), sc.Suite(), name, p, func(ctx context.Context) (*bigquery.Client, error) {
		return bigquery.NewClient(ctx, p.ID, p.REST("/bigquery/v2/")...)
	})
	return c, p, err
}

// table resolves "dataset.table" (in the project) or
// "project.dataset.table".
func table(c *bigquery.Client, p *gcpcore.Project, ref string) (*bigquery.Table, string, error) {
	parts := strings.Split(ref, ".")
	switch len(parts) {
	case 2:
		return c.DatasetInProject(p.ID, parts[0]).Table(parts[1]), p.ID + "." + ref, nil
	case 3:
		return c.DatasetInProject(parts[0], parts[1]).Table(parts[2]), ref, nil
	}
	return nil, "", fmt.Errorf("name a bigquery table as dataset.table or project.dataset.table, not %q", ref)
}

type row map[string]bigquery.Value

func (r row) Save() (map[string]bigquery.Value, string, error) { return r, bigquery.NoDedupeID, nil }

func seed(sc *core.Scenario, a core.Args) error {
	file := a.String(0)
	s, err := cloudstep.ReadSeed(sc, file)
	if err != nil {
		return err
	}
	c, p, err := client(sc)
	if err != nil {
		return err
	}
	for _, ref := range s.Names {
		items, ok := s.Items[ref].([]any)
		if !ok {
			return fmt.Errorf("%s: %s must list its rows", file, ref)
		}
		t, _, err := table(c, p, ref)
		if err != nil {
			return err
		}
		rows := make([]bigquery.ValueSaver, 0, len(items))
		for i, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				return fmt.Errorf("%s: row %d of %s is not an object", file, i+1, ref)
			}
			r := row{}
			for k, v := range m {
				r[k] = v
			}
			rows = append(rows, r)
		}
		if err := t.Inserter().Put(sc.Context(), rows); err != nil {
			return fmt.Errorf("%s: cannot insert into the %s bigquery table: %w", file, ref, err)
		}
		sc.Log("seeded %d row(s) into the %s bigquery table", len(rows), ref)
	}
	return nil
}

func expect(sc *core.Scenario, a core.Args, ref string, count int) error {
	rs, err := cloudstep.Conditions(a.Table)
	if err != nil {
		return err
	}
	for _, r := range rs {
		if _, err := core.ParseValue(r.Value, r.Null, conditions.Undefined); err != nil {
			return fmt.Errorf("%s: %w", r.Key, err)
		}
	}
	c, p, err := client(sc)
	if err != nil {
		return err
	}
	_, full, err := table(c, p, ref)
	if err != nil {
		return err
	}
	fetch := func() ([]string, error) {
		it, err := c.Query(fmt.Sprintf("SELECT %s FROM `%s` LIMIT %d", columns(rs), full, maxRows)).Read(sc.Context())
		if err != nil {
			return nil, fmt.Errorf("cannot read the %s bigquery table: %w", ref, err)
		}
		var out []string
		for first := true; ; first = false {
			var r map[string]bigquery.Value
			err := it.Next(&r)
			if first && (err == nil || errors.Is(err, iterator.Done)) {
				// The first page has the schema, of no rows too. A value of
				// the wrong type fails here, at once, as no wait can mend it.
				if err := holdToSchema(it.Schema, ref, rs); err != nil {
					return nil, err
				}
			}
			if errors.Is(err, iterator.Done) {
				return out, nil
			}
			if err != nil {
				return nil, fmt.Errorf("cannot read the %s bigquery table: %w", ref, err)
			}
			b, err := json.Marshal(plain(r))
			if err != nil {
				return nil, err
			}
			out = append(out, string(b))
		}
	}
	return cloudstep.ExpectRecords(sc, cloudstep.Wait(a, 0), fetch, rs, count, "row", "the "+ref+" bigquery table")
}

// holdToSchema fails with what to write when a condition's value is not of its
// column's type (ADR 0014), for all of them at once, as the SQL pack's steps do:
// `5.0` on an INT64 or `10115` on a STRING could not match a row.
func holdToSchema(schema bigquery.Schema, ref string, rs cloudstep.Rows) error {
	var wrong []string
	for _, r := range rs {
		v, err := core.ParseValue(r.Value, r.Null, conditions.Undefined)
		if err != nil {
			return fmt.Errorf("%s: %w", r.Key, err)
		}
		c, ok := columnType(fieldAt(schema, r.Key), r.Key, ref)
		if !ok {
			continue
		}
		if m := c.Wrong(v); m != "" {
			wrong = append(wrong, m)
		}
	}
	return columntype.Fail(wrong)
}

// fieldAt is the column or RECORD's field a condition's path names, or nil for a
// path that goes anywhere else: into a repeated field, or by a JSONPath's
// brackets.
func fieldAt(schema bigquery.Schema, path string) *bigquery.FieldSchema {
	if strings.ContainsAny(path, "[]*") || strings.Contains(path, "..") {
		return nil
	}
	fields := schema
	var at *bigquery.FieldSchema
	for _, name := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		i := slices.IndexFunc(fields, func(f *bigquery.FieldSchema) bool { return strings.EqualFold(f.Name, name) })
		if i < 0 || at != nil && at.Repeated {
			return nil
		}
		at = fields[i]
		fields = at.Schema
	}
	return at
}

// columnType is a field's type as a condition holds it. A RECORD and a repeated
// field take JSON, which is compared as a whole.
func columnType(f *bigquery.FieldSchema, name, ref string) (columntype.Column, bool) {
	if f == nil || f.Repeated || f.Type == bigquery.RecordFieldType {
		return columntype.Column{}, false
	}
	c := columntype.Column{Name: name, Table: ref, Type: string(f.Type), Kind: columntype.Other}
	switch f.Type {
	case bigquery.IntegerFieldType:
		c.Type, c.Kind = "INT64", columntype.Integer
	case bigquery.FloatFieldType:
		c.Type, c.Kind = "FLOAT64", columntype.Decimal
	case bigquery.NumericFieldType, bigquery.BigNumericFieldType:
		c.Kind = columntype.Decimal
	case bigquery.BooleanFieldType:
		c.Type, c.Kind = "BOOL", columntype.Boolean
	case bigquery.StringFieldType:
		c.Kind = columntype.Text
	}
	return c, true
}

// plain converts a BigQuery value to JSON values of its type (ADR 0014): an
// INT64 an integer, a FLOAT64 or a NUMERIC a decimal (6.0); the types JSON lacks
// are text.
func plain(v bigquery.Value) any {
	switch x := v.(type) {
	case map[string]bigquery.Value:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = plain(e)
		}
		return out
	case []bigquery.Value:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = plain(e)
		}
		return out
	case float64:
		return decimal(x)
	case *big.Rat:
		return json.Number(numeric(x))
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case civil.Date:
		return x.String()
	case civil.Time:
		return x.String()
	case civil.DateTime:
		return x.String()
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	}
	return v
}

// decimal is a FLOAT64 as JSON: a decimal, a whole one too (6.0, not 6), or
// "NaN", "Infinity" and "-Infinity", which JSON lacks, as BigQuery names them.
func decimal(f float64) any {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	b, _ := json.Marshal(f)
	if !bytes.ContainsAny(b, ".eE") {
		b = append(b, ".0"...)
	}
	return json.Number(b)
}

// numeric renders a NUMERIC or a BIGNUMERIC as a decimal, a whole one too (2.0),
// all of its digits, without trailing zeros.
func numeric(r *big.Rat) string {
	s := strings.TrimRight(r.FloatString(bigquery.BigNumericScaleDigits), "0")
	if strings.HasSuffix(s, ".") {
		s += "0"
	}
	return s
}

// columns selects the columns the conditions name, which is what BigQuery
// reads (and bills), or every column when a condition's column cannot be
// told.
func columns(rs cloudstep.Rows) string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rs {
		col := column(r.Key)
		if col == "" {
			return "*"
		}
		if !seen[col] {
			seen[col] = true
			if reserved[strings.ToUpper(col)] {
				col = "`" + col + "`"
			}
			out = append(out, col)
		}
	}
	if len(out) == 0 {
		return "*"
	}
	return strings.Join(out, ", ")
}

// column is the top-level column of a condition's path: "route" for
// "route.to", "$.route.to" or "$['route']['to']".
func column(path string) string {
	p := strings.TrimSpace(path)
	switch {
	case strings.HasPrefix(p, "$['"):
		p = p[3:]
		if i := strings.Index(p, "'"); i > 0 {
			return p[:i]
		}
		return ""
	case strings.HasPrefix(p, "$."):
		p = p[2:]
	case strings.HasPrefix(p, "$"):
		return ""
	}
	if i := strings.IndexAny(p, ".["); i >= 0 {
		p = p[:i]
	}
	for _, r := range p {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return p
}

// reserved are GoogleSQL's reserved keywords, the column names that need
// quoting.
var reserved = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`ALL AND ANY ARRAY AS ASC ASSERT_ROWS_MODIFIED AT BETWEEN BY CASE CAST COLLATE CONTAINS
		CREATE CROSS CUBE CURRENT DEFAULT DEFINE DESC DISTINCT ELSE END ENUM ESCAPE EXCEPT EXCLUDE EXISTS EXTRACT FALSE
		FETCH FOLLOWING FOR FROM FULL GROUP GROUPING GROUPS HASH HAVING IF IGNORE IN INNER INTERSECT INTERVAL INTO IS JOIN
		LATERAL LEFT LIKE LIMIT LOOKUP MERGE NATURAL NEW NO NOT NULL NULLS OF ON OR ORDER OUTER OVER PARTITION PRECEDING
		PROTO QUALIFY RANGE RECURSIVE RESPECT RIGHT ROLLUP ROWS SELECT SET SOME STRUCT TABLESAMPLE THEN TO TREAT TRUE
		UNBOUNDED UNION UNNEST USING WHEN WHERE WINDOW WITH WITHIN`) {
		m[w] = true
	}
	return m
}()
