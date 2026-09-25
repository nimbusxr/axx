// Package awsdynamodb is the aws-dynamodb pack: items seeded into DynamoDB
// tables and the items the services under test write.
package awsdynamodb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	awscore "github.com/nimbusxr/axx/packs/aws/core"
)

const name = "aws-dynamodb"

const packDoc = `Seed DynamoDB tables and check the items your services write.

The steps use the scenario's AWS account (` + "`the {word} aws account with the following properties:`" + `, from aws-core).

A **seed** is a YAML or JSON file that maps table names to the items to put, written as plain JSON (numbers become ` + "`N`" + `, objects ` + "`M`" + `, arrays ` + "`L`" + `):

` + "```yaml" + `
insured-parcels:
  - reference: PX-CLM-1001
    declaredValue: 120
    carrier: KESTREL
` + "```" + `

**Checks** wait (10 seconds unless ` + "`within {duration}`" + ` says otherwise) until the table has an item, or a number of items, meeting every condition: ` + "`attribute | value`" + ` rows with a dotted path into maps (` + "`address.city`" + `), compared as text, ` + "`null`" + ` for null and ` + "`undefined`" + ` for absent. Items are read with a scan, which suits the small tables of a test environment.`

// Pack returns the aws-dynamodb pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      name,
		Namespace: name,
		Doc:       packDoc,
		Requires:  []string{awscore.Name},
		Steps: []core.StepDef{
			{
				ID: name + ".seed", Keyword: "Given", Since: "0.1.0",
				Expr:     "a {filepath} dynamodb seed",
				Doc:      "Put the items of a seed file (resolved against `resources`): YAML or JSON mapping table names to lists of items.",
				Examples: []string{"Given a seeds/insured-parcels.yaml dynamodb seed"},
				Run:      seed,
			},
			{
				ID: name + ".item", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
				Expr:     "[[within {duration} ]]the {word} dynamodb table has an item where:",
				Doc:      "Wait (10s, or the given time) until the table has an item meeting every `attribute | value` row.",
				Examples: []string{"Then within 30s the claims dynamodb table has an item where:"},
				Run: func(sc *core.Scenario, a core.Args) error {
					return expect(sc, a, a.String(1), -1)
				},
			},
			{
				ID: name + ".items", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
				Expr:     "[[within {duration} ]]the {word} dynamodb table has {int} item(s) where:",
				Doc:      "Wait (10s, or the given time) until exactly that many items of the table meet every `attribute | value` row.",
				Examples: []string{"Then the claims dynamodb table has 1 item where:"},
				Run: func(sc *core.Scenario, a core.Args) error {
					return expect(sc, a, a.String(1), a.Int(2))
				},
			},
		},
	}
}

func client(sc *core.Scenario) (*dynamodb.Client, error) {
	acct, err := awscore.Default(sc)
	if err != nil {
		return nil, err
	}
	cfg, err := acct.Config(sc.Context(), sc.Suite())
	if err != nil {
		return nil, err
	}
	return dynamodb.NewFromConfig(cfg), nil
}

func seed(sc *core.Scenario, a core.Args) error {
	file := a.String(0)
	s, err := cloudstep.ReadSeed(sc, file)
	if err != nil {
		return err
	}
	c, err := client(sc)
	if err != nil {
		return err
	}
	for _, table := range s.Names {
		items, ok := s.Items[table].([]any)
		if !ok {
			return fmt.Errorf("%s: %s must list its items", file, table)
		}
		for i, it := range items {
			item, ok := attribute(it).(*types.AttributeValueMemberM)
			if !ok {
				return fmt.Errorf("%s: item %d of %s is not an object", file, i+1, table)
			}
			if _, err := c.PutItem(sc.Context(), &dynamodb.PutItemInput{TableName: aws.String(table), Item: item.Value}); err != nil {
				return fmt.Errorf("%s: cannot put item %d into the %s dynamodb table: %w", file, i+1, table, err)
			}
		}
		sc.Log("seeded %d item(s) into the %s dynamodb table", len(items), table)
	}
	return nil
}

func expect(sc *core.Scenario, a core.Args, table string, count int) error {
	rs, err := cloudstep.Conditions(a.Table)
	if err != nil {
		return err
	}
	c, err := client(sc)
	if err != nil {
		return err
	}
	fetch := func() ([]string, error) {
		var out []string
		p := dynamodb.NewScanPaginator(c, &dynamodb.ScanInput{TableName: aws.String(table)})
		for p.HasMorePages() {
			page, err := p.NextPage(sc.Context())
			if err != nil {
				return nil, fmt.Errorf("cannot scan the %s dynamodb table: %w", table, err)
			}
			for _, it := range page.Items {
				out = append(out, itemJSON(it))
			}
		}
		return out, nil
	}
	return cloudstep.ExpectRecords(sc, cloudstep.Wait(a, 0), fetch, rs, count, "item", "the "+table+" dynamodb table")
}

// attribute converts a plain JSON value to a DynamoDB attribute.
func attribute(v any) types.AttributeValue {
	switch x := v.(type) {
	case nil:
		return &types.AttributeValueMemberNULL{Value: true}
	case bool:
		return &types.AttributeValueMemberBOOL{Value: x}
	case json.Number:
		return &types.AttributeValueMemberN{Value: x.String()}
	case string:
		return &types.AttributeValueMemberS{Value: x}
	case []any:
		l := make([]types.AttributeValue, len(x))
		for i, e := range x {
			l[i] = attribute(e)
		}
		return &types.AttributeValueMemberL{Value: l}
	case map[string]any:
		m := make(map[string]types.AttributeValue, len(x))
		for k, e := range x {
			m[k] = attribute(e)
		}
		return &types.AttributeValueMemberM{Value: m}
	}
	return &types.AttributeValueMemberS{Value: fmt.Sprint(v)}
}

// itemJSON renders an item as JSON, attributes sorted by name: numbers as
// numbers, sets as arrays, binary as base64 text.
func itemJSON(item map[string]types.AttributeValue) string {
	var b bytes.Buffer
	writeMap(&b, item)
	return b.String()
}

func writeMap(b *bytes.Buffer, m map[string]types.AttributeValue) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSON(b, k)
		b.WriteByte(':')
		writeValue(b, m[k])
	}
	b.WriteByte('}')
}

func writeValue(b *bytes.Buffer, v types.AttributeValue) {
	switch x := v.(type) {
	case *types.AttributeValueMemberS:
		writeJSON(b, x.Value)
	case *types.AttributeValueMemberN:
		if _, err := strconv.ParseFloat(x.Value, 64); err == nil {
			b.WriteString(x.Value)
		} else {
			writeJSON(b, x.Value)
		}
	case *types.AttributeValueMemberBOOL:
		b.WriteString(strconv.FormatBool(x.Value))
	case *types.AttributeValueMemberNULL:
		b.WriteString("null")
	case *types.AttributeValueMemberB:
		writeJSON(b, x.Value)
	case *types.AttributeValueMemberM:
		writeMap(b, x.Value)
	case *types.AttributeValueMemberL:
		b.WriteByte('[')
		for i, e := range x.Value {
			if i > 0 {
				b.WriteByte(',')
			}
			writeValue(b, e)
		}
		b.WriteByte(']')
	case *types.AttributeValueMemberSS:
		writeJSON(b, x.Value)
	case *types.AttributeValueMemberNS:
		b.WriteByte('[')
		for i, e := range x.Value {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(e)
		}
		b.WriteByte(']')
	case *types.AttributeValueMemberBS:
		writeJSON(b, x.Value)
	default:
		b.WriteString("null")
	}
}

func writeJSON(b *bytes.Buffer, v any) {
	out, _ := json.Marshal(v)
	b.Write(out)
}
