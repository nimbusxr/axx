// Package gcpbigquery is the gcp-bigquery pack: rows seeded into BigQuery
// tables and the rows the services under test write.
package gcpbigquery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/civil"
	"google.golang.org/api/iterator"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
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

**Checks** wait (10 seconds unless ` + "`within {duration}`" + ` says otherwise) until the table has a row, or a number of rows, meeting every condition: ` + "`column | value`" + ` rows, with a dotted path into ` + "`RECORD`" + ` columns (` + "`address.city`" + `), compared as text: numbers as written, ` + "`NUMERIC`" + ` as its decimal, ` + "`TIMESTAMP`" + ` in RFC 3339 (` + "`2026-09-24T09:30:00Z`" + `), ` + "`DATE`" + ` as ` + "`2026-09-24`" + `; ` + "`null`" + ` for NULL. A check reads the columns its conditions name, of up to 5,000 rows of the table: check the tables your scenarios write, not warehouse-size ones.`

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
				Expr:     "a {filepath} bigquery seed",
				Doc:      "Insert the rows of a seed file (resolved against `resources`): YAML or JSON mapping tables to lists of rows.",
				Examples: []string{"Given a seeds/carrier-rates.yaml bigquery seed"},
				Run:      seed,
			},
			{
				ID: name + ".row", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
				Expr:     "[[within {duration} ]]the {word} bigquery table has a row where:",
				Doc:      "Wait (10s, or the given time) until the table has a row meeting every `column | value` row.",
				Examples: []string{"Then within 30s the billing.invoice_lines bigquery table has a row where:"},
				Run: func(sc *core.Scenario, a core.Args) error {
					return expect(sc, a, a.String(1), -1)
				},
			},
			{
				ID: name + ".rows", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
				Expr:     "[[within {duration} ]]the {word} bigquery table has {int} row(s) where:",
				Doc:      "Wait (10s, or the given time) until exactly that many rows of the table meet every `column | value` row.",
				Examples: []string{"Then the billing.invoice_lines bigquery table has 3 rows where:"},
				Run: func(sc *core.Scenario, a core.Args) error {
					return expect(sc, a, a.String(1), a.Int(2))
				},
			},
		},
	}
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
		for {
			var r map[string]bigquery.Value
			err := it.Next(&r)
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

// plain converts a BigQuery value to JSON values, rendering the types JSON
// lacks as text.
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
	case *big.Rat:
		return json.Number(decimal(x))
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

// decimal renders a NUMERIC without trailing zeros.
func decimal(r *big.Rat) string {
	s := r.FloatString(bigquery.NumericScaleDigits)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
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
