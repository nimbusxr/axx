package graphql

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/jsonassert"
	"github.com/nimbusxr/axx/internal/oaslevel"
	"github.com/nimbusxr/axx/internal/secrets"
)

const variablesNote = "Each row is a variable, or a path into one (`address.postcode`), and its value: a value takes the type the operation declares " +
	"for its variable, double quotes make it text, `null` is null and `undefined` leaves it out."

const levelsNote = "A row is a validation key and its level: `ERROR` (or `FAIL`), `WARN`, `INFO` or `IGNORE`. " +
	"The key must be one the pack reports, or a prefix of such keys, like `validation.operation`: " +
	"any other fails the step, which names the closest keys."

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".service", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} graphql service with the following properties:",
			Doc:  "Register a GraphQL service under a name: a federated graph's gateway, a subgraph, or any GraphQL API.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Required: true, Takes: "the URL operations are posted to"},
					{Name: "schema", Takes: "its schema, which operations and answers are checked against: an SDL file of the project, a URL, or `introspection` to ask the service"},
					{Name: "header.<name>", Takes: "a header sent with every operation, such as `header.Authorization`"},
					{Name: "timeout", Takes: "how long an operation may take, like `5s`", Default: "10s"},
					{Name: "subscriptions", Takes: "how subscriptions go", Values: []string{graphqlWS, sse}, Default: graphqlWS},
					{Name: "subscriptions url", Takes: "where subscriptions go", Default: "the url, with `ws://` or `wss://` for graphql-ws"},
				},
				Note: "Values are expanded (`${env:..}`, `${sys:..}`, `${token:..}`); what `${env:..}` references expand to is masked everywhere.",
			},
			Examples: []string{
				"Given the parcels graphql service with the following properties:\n" +
					"  | url    | http://localhost:8400/graphql |\n" +
					"  | schema | introspection                 |",
				"Given the graph graphql service with the following properties:\n" +
					"  | url                  | https://graph.parcels.example/graphql |\n" +
					"  | schema               | graphql/supergraph.graphql            |\n" +
					"  | header.Authorization | Bearer ${token:shop}                  |",
			},
			TableTypes: map[string]string{"schema": "filepath"},
			Run:        register,
		},
		{
			ID: Name + ".send.file", Keyword: "When", Arg: core.ArgOptional, Since: since,
			Expr:  "the {filepath} query/mutation is sent to the {word} graphql service[[ with the following variables:]]",
			Doc:   "Send the query or the mutation of a file of the project, with the table's variables when the step has one.",
			Table: &core.TableDoc{Columns: []string{"path", "value"}, Note: variablesNote},
			Examples: []string{
				"When the graphql/parcel.graphql query is sent to the parcels graphql service with the following variables:\n" +
					"  | reference | PX-GQL-7201 |",
				"When the graphql/hold-parcel.graphql mutation is sent to the parcels graphql service with the following variables:\n" +
					"  | reference | PX-GQL-7202 |\n" +
					"  | until     | 2026-10-05  |",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				withTable := strings.HasSuffix(a.Text, ":")
				switch {
				case withTable && a.Table == nil:
					return errors.New("the variables are missing: add a table (| path | value |)")
				case !withTable && a.Table != nil:
					return errors.New(`say "with the following variables:" to set variables from a table`)
				}
				return sendOn(sc, a.String(1), a.Text, a.String(0), nil, a.Table)
			},
		},
		{
			ID: Name + ".send.doc", Keyword: "When", Arg: core.ArgDocString, Since: since,
			Expr: "a query/mutation is sent to the {word} graphql service:",
			Doc:  "Send the query or the mutation of the doc string.",
			Examples: []string{"When a query is sent to the parcels graphql service:\n" +
				"  \"\"\"\n  { parcel(reference: \"PX-GQL-7201\") { status } }\n  \"\"\""},
			Run: func(sc *core.Scenario, a core.Args) error {
				return sendOn(sc, a.String(0), a.Text, "", a.DocString, nil)
			},
		},
		{
			ID: Name + ".subscribe", Keyword: "When", Arg: core.ArgOptional, Since: since,
			Expr: "the {filepath} subscription is started on the {word} graphql service[[ with the following variables:]]",
			Doc: "Start the subscription of a file of the project, with the table's variables when the step has one. " +
				"It belongs to the scenario, which ends it when it ends.",
			Table: &core.TableDoc{Columns: []string{"path", "value"}, Note: variablesNote},
			Examples: []string{"When the graphql/parcel-scanned.graphql subscription is started on the parcels graphql service with the following variables:\n" +
				"  | reference | PX-GQL-7203 |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				withTable := strings.HasSuffix(a.Text, ":")
				switch {
				case withTable && a.Table == nil:
					return errors.New("the variables are missing: add a table (| path | value |)")
				case !withTable && a.Table != nil:
					return errors.New(`say "with the following variables:" to set variables from a table`)
				}
				return subscribeOn(sc, a.String(1), a.String(0), a.Table)
			},
		},
		{
			ID: Name + ".no.errors", Keyword: "Then", Since: since,
			Expr:     "the {word} graphql service answered without errors",
			Doc:      "Check that the service's last answer has no errors.",
			Examples: []string{"Then the parcels graphql service answered without errors"},
			Run: func(sc *core.Scenario, a core.Args) error {
				s, ans, err := lastAnswer(sc, a.String(0))
				if err != nil {
					return err
				}
				if len(ans.errors) > 0 {
					return secrets.Hide(sc, core.Failf("The %s graphql service answered %s with %d error(s): %s", s.name, ans.op.label(), len(ans.errors), ans.errorText()))
				}
				return nil
			},
		},
		{
			ID: Name + ".data", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {word} graphql service's data has the following properties:",
			Doc:  "Check the data of the service's last answer.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note:    "Each row is a path into the data (a field, a dotted path like `parcel.shop.name`, or a JSONPath) and its value, compared as text: `null` for null and `undefined` for absent.",
			},
			Examples: []string{"Then the parcels graphql service's data has the following properties:\n" +
				"  | parcel.status    | IN_TRANSIT |\n" +
				"  | parcel.shop.name | Maple Home |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				_, ans, err := lastAnswer(sc, a.String(0))
				if err != nil {
					return err
				}
				return secrets.Hide(sc, jsonassert.Properties(string(ans.data), a.Table, false))
			},
		},
		{
			ID: Name + ".error", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {word} graphql service answered an error where:",
			Doc:  "Check that the service's last answer has an error with those values.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note: "Each row is a path into the error (`message`, `path`, `extensions.code`) and its value, compared as text; " +
					"the error's `path` is its fields and indexes joined with dots, like `parcel.shop` or `parcels.0.reference`.",
			},
			Examples: []string{"Then the parcels graphql service answered an error where:\n" +
				"  | path            | holdParcel   |\n" +
				"  | extensions.code | NOT_HOLDABLE |"},
			Run: checkError,
		},
		{
			ID: Name + ".received", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the {word} graphql service's subscription received a message where:",
			Doc: "Check that the service's subscription received a message whose data has those values.\n\n" +
				"- The check waits for it: 10 seconds, or `within {duration}`.\n" +
				"- When the subscription has ended, it fails at once, saying why.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note:    "Each row is a path into the message's data and its value, compared as text: `null` for null and `undefined` for absent.",
			},
			Examples: []string{"Then within 20s the parcels graphql service's subscription received a message where:\n" +
				"  | parcelScanned.reference | PX-GQL-7203      |\n" +
				"  | parcelScanned.status    | OUT_FOR_DELIVERY |"},
			Run: checkReceived,
		},
		{
			ID: Name + ".levels", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the GraphQL validation levels are:",
			Doc: "Set the level of GraphQL validation findings for this scenario, on every GraphQL service it sends operations to.\n\n" +
				"- A key also sets the keys below it: `validation.operation` relaxes `validation.operation.FieldsOnCorrectType` too. The most specific key set wins.\n" +
				"- The rows are merged over `packs.graphql.levels` of axx.yaml.\n" +
				"- The pack's documentation lists the keys, and what each level does.",
			Table: &core.TableDoc{Columns: []string{"validation key", "level"}, Note: levelsNote},
			Examples: []string{"Given the GraphQL validation levels are:\n" +
				"  | validation.operation.FieldsOnCorrectType | IGNORE |"},
			Run: setLevels,
		},
	}
}

// kindOf is the kind the step's text says: "query", "mutation" or "subscription".
func kindOf(text string) string {
	for _, k := range []string{"mutation", "subscription"} {
		if strings.Contains(text, " "+k+" ") {
			return k
		}
	}
	return "query"
}

func sendOn(sc *core.Scenario, name, text, file string, doc *core.DocString, t *core.Table) error {
	s, err := get(sc, name)
	if err != nil {
		return err
	}
	op, err := readOperation(sc, file, doc)
	if err != nil {
		return err
	}
	if want := kindOf(text); op.kind() != want {
		if op.kind() == "subscription" {
			return errors.New(`the operation is a subscription: start it with "the {filepath} subscription is started on the {word} graphql service"`)
		}
		return fmt.Errorf("the operation is a %s, not a %s", op.kind(), want)
	}
	if err := op.setVariables(sc, t, s.schema); err != nil {
		return err
	}
	st, err := settingsFor(sc.Suite())
	if err != nil {
		return err
	}
	if s.schema != nil {
		if err := report(sc, st, s, op.label(), op.check(s.schema)); err != nil {
			return err
		}
	}
	sc.Attach("application/graphql", []byte(secrets.Mask(sc, op.text)), op.label())
	if op.variables != nil {
		sc.Attach("application/json", []byte(secrets.Mask(sc, string(op.variables))), op.label()+" variables")
	}
	ans, err := s.post(sc.Context(), st.client, op.body())
	if err != nil {
		return secrets.Hide(sc, err)
	}
	ans.op = op
	s.mu.Lock()
	s.last = ans
	s.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{"data": ans.data, "errors": ans.errors})
	sc.Attach("application/json", []byte(secrets.Mask(sc, string(raw))), op.label()+" answer")
	if len(ans.errors) > 0 {
		sc.Log("sent %s to the %s graphql service: %d error(s)", op.label(), s.name, len(ans.errors))
	} else {
		sc.Log("sent %s to the %s graphql service: data", op.label(), s.name)
	}
	if s.schema != nil {
		return report(sc, st, s, "the answer to "+op.label(), op.checkData(s.schema, ans.data, ans.errors))
	}
	return nil
}

func subscribeOn(sc *core.Scenario, name, file string, t *core.Table) error {
	s, err := get(sc, name)
	if err != nil {
		return err
	}
	op, err := readOperation(sc, file, nil)
	if err != nil {
		return err
	}
	if op.kind() != "subscription" {
		return fmt.Errorf("the operation is a %s, not a subscription", op.kind())
	}
	if err := op.setVariables(sc, t, s.schema); err != nil {
		return err
	}
	if s.schema != nil {
		st, err := settingsFor(sc.Suite())
		if err != nil {
			return err
		}
		if err := report(sc, st, s, op.label(), op.check(s.schema)); err != nil {
			return err
		}
	}
	sub, err := s.subscribe(op)
	if err != nil {
		return secrets.Hide(sc, err)
	}
	s.mu.Lock()
	if s.sub != nil {
		s.sub.close()
	}
	s.sub = sub
	s.mu.Unlock()
	sc.Log("started %s on the %s graphql service (%s)", op.label(), s.name, s.subProtocol)
	return nil
}

func lastAnswer(sc *core.Scenario, name string) (*service, *answer, error) {
	s, err := get(sc, name)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	a := s.last
	s.mu.Unlock()
	if a == nil {
		return nil, nil, fmt.Errorf("the %s graphql service has not been sent a query or a mutation in this scenario", name)
	}
	return s, a, nil
}

func checkError(sc *core.Scenario, a core.Args) error {
	s, ans, err := lastAnswer(sc, a.String(0))
	if err != nil {
		return err
	}
	rs, err := cloudstep.Conditions(a.Table)
	if err != nil {
		return err
	}
	shown := make([]string, 0, len(ans.errors))
	for _, raw := range ans.errors {
		var e map[string]any
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		if p, ok := e["path"].([]any); ok {
			e["path"] = dotted(p)
		}
		body, _ := json.Marshal(e)
		ok, err := rs.MatchMessage(cloudstep.Message{Body: body}, "")
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		shown = append(shown, "  "+string(body))
	}
	if len(shown) == 0 {
		return secrets.Hide(sc, core.Failf("The %s graphql service answered %s without errors", s.name, ans.op.label()))
	}
	return secrets.Hide(sc, core.Failf("None of the %d error(s) the %s graphql service answered %s met the conditions:\n%s",
		len(shown), s.name, ans.op.label(), strings.Join(shown, "\n")))
}

func checkReceived(sc *core.Scenario, a core.Args) error {
	s, err := get(sc, a.String(1))
	if err != nil {
		return err
	}
	s.mu.Lock()
	sub := s.sub
	s.mu.Unlock()
	if sub == nil {
		return fmt.Errorf("no subscription is started on the %s graphql service in this scenario", s.name)
	}
	rs, err := cloudstep.Conditions(a.Table)
	if err != nil {
		return err
	}
	st, err := settingsFor(sc.Suite())
	if err != nil {
		return err
	}
	match := func(m cloudstep.Message) (bool, error) {
		ok, err := rs.MatchMessage(m, "")
		if ok && err == nil && s.schema != nil {
			if err := report(sc, st, s, "a message of "+sub.op.label(), sub.op.checkData(s.schema, m.Body, nil)); err != nil {
				return false, err
			}
		}
		return ok, err
	}
	return secrets.Hide(sc, cloudstep.ExpectMatch(sc, cloudstep.Wait(a, 0), sub.in, match, "", "message",
		fmt.Sprintf("the %s subscription of the %s graphql service", sub.op.label(), s.name)))
}

func setLevels(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	levels := oaslevel.Levels{}
	for _, p := range pairs {
		key := strings.TrimSpace(p.Key)
		if err := levelKeys.Check(key); err != nil {
			return err
		}
		lv, err := oaslevel.Parse(p.Value)
		if err != nil {
			return fmt.Errorf("invalid GraphQL validation level %q for key %q; supported levels: %s", p.Value, p.Key, oaslevel.Supported)
		}
		levels[key] = lv
	}
	st := scenarioLevels.Of(sc)
	st.mu.Lock()
	st.levels = st.levels.Merge(levels)
	st.mu.Unlock()
	return nil
}
