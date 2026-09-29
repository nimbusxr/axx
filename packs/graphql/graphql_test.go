package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ws "github.com/coder/websocket"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/validator/rules"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// parcels acts as a parcels GraphQL API does, with canned answers by
// operation name, and its subscriptions over graphql-ws and server-sent
// events.
type parcels struct {
	schema *ast.Schema
	calls  atomic.Int32
	subs   atomic.Int32
}

type request struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

func (p *parcels) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Header.Get("Upgrade") != "":
		p.graphqlWS(w, r)
		return
	case r.URL.Path == "/html":
		http.Error(w, "<html>bad gateway</html>", http.StatusBadGateway)
		return
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if r.Header.Get("Accept") == "text/event-stream" {
		p.sse(w, req)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if strings.Contains(req.Query, "__schema") {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"__schema": introspectionOf(p.schema)}})
		return
	}
	p.calls.Add(1)
	ref, _ := req.Variables["reference"].(string)
	var out any
	switch req.OperationName {
	case "Parcel":
		if ref == "PX-GQL-7299" || strings.Contains(req.Query, "PX-GQL-7299") {
			out = map[string]any{"data": map[string]any{"parcel": nil}}
			break
		}
		out = map[string]any{"data": map[string]any{"parcel": map[string]any{
			"reference": ref, "status": "IN_TRANSIT", "weightGrams": 1200, "shop": map[string]any{"name": "Maple Home"},
		}}}
	case "BadParcel":
		out = map[string]any{"data": map[string]any{"parcel": map[string]any{
			"reference": ref, "status": "LOST", "weightGrams": "heavy", "label": "ZPL",
		}}}
	case "HoldParcel":
		out = map[string]any{"data": nil, "errors": []any{map[string]any{
			"message": ref + " is out for delivery", "path": []any{"holdParcel"}, "extensions": map[string]any{"code": "NOT_HOLDABLE"},
		}}}
	case "SignedBy":
		out = map[string]any{"errors": []any{map[string]any{"message": `Cannot query field "signedBy" on type "Parcel".`}}}
	case "ParcelsByPostcode":
		if _, ok := req.Variables["postcode"].(string); !ok {
			out = map[string]any{"errors": []any{map[string]any{"message": "postcode is not text"}}}
			break
		}
		out = map[string]any{"data": map[string]any{"parcels": []any{map[string]any{"reference": "PX-GQL-7204"}}}}
	default:
		out = map[string]any{"data": map[string]any{"parcel": map[string]any{"reference": "PX-GQL-7200"}}}
	}
	_ = json.NewEncoder(w).Encode(out)
}

func scans(ref string) []string {
	return []string{
		fmt.Sprintf(`{"data": {"parcelScanned": {"reference": %q, "status": "OUT_FOR_DELIVERY"}}}`, ref),
		fmt.Sprintf(`{"data": {"parcelScanned": {"reference": %q, "status": "DELIVERED"}}}`, ref),
	}
}

func (p *parcels) graphqlWS(w http.ResponseWriter, r *http.Request) {
	c, err := ws.Accept(w, r, &ws.AcceptOptions{Subprotocols: []string{"graphql-transport-ws"}})
	if err != nil {
		return
	}
	defer func() { _ = c.CloseNow() }()
	p.subs.Add(1)
	defer p.subs.Add(-1)
	ctx := r.Context()
	for {
		_, raw, err := c.Read(ctx)
		if err != nil {
			return
		}
		var m struct {
			Type    string  `json:"type"`
			ID      string  `json:"id"`
			Payload request `json:"payload"`
		}
		_ = json.Unmarshal(raw, &m)
		switch m.Type {
		case "connection_init":
			_ = c.Write(ctx, ws.MessageText, []byte(`{"type":"connection_ack"}`))
		case "subscribe":
			ref, _ := m.Payload.Variables["reference"].(string)
			for _, s := range scans(ref) {
				_ = c.Write(ctx, ws.MessageText, []byte(fmt.Sprintf(`{"type":"next","id":%q,"payload":%s}`, m.ID, s)))
			}
			if ref == "PX-GQL-7297" { // a subscription that goes on
				continue
			}
			_ = c.Write(ctx, ws.MessageText, []byte(fmt.Sprintf(`{"type":"complete","id":%q}`, m.ID)))
		case "complete":
			return
		}
	}
}

func (p *parcels) sse(w http.ResponseWriter, req request) {
	w.Header().Set("Content-Type", "text/event-stream")
	ref, _ := req.Variables["reference"].(string)
	for _, s := range scans(ref) {
		fmt.Fprintf(w, "event: next\ndata: %s\n\n", strings.ReplaceAll(s, "\n", " "))
	}
	fmt.Fprint(w, "event: complete\ndata:\n\n")
}

// introspectionOf answers the introspection query from a schema, as a
// server does.
func introspectionOf(s *ast.Schema) map[string]any {
	ref := func(t *ast.Type) map[string]any {
		var build func(t *ast.Type) map[string]any
		build = func(t *ast.Type) map[string]any {
			if t.NonNull {
				c := *t
				c.NonNull = false
				return map[string]any{"kind": "NON_NULL", "name": nil, "ofType": build(&c)}
			}
			if t.Elem != nil {
				return map[string]any{"kind": "LIST", "name": nil, "ofType": build(t.Elem)}
			}
			return map[string]any{"kind": string(s.Types[t.NamedType].Kind), "name": t.NamedType, "ofType": nil}
		}
		return build(t)
	}
	name := func(d *ast.Definition) any {
		if d == nil {
			return nil
		}
		return map[string]any{"name": d.Name}
	}
	var types []any
	for _, d := range s.Types {
		t := map[string]any{"kind": string(d.Kind), "name": d.Name}
		var fields []any
		for _, f := range d.Fields {
			if strings.HasPrefix(f.Name, "__") {
				continue
			}
			var args []any
			for _, a := range f.Arguments {
				args = append(args, map[string]any{"name": a.Name, "type": ref(a.Type), "defaultValue": nil})
			}
			fields = append(fields, map[string]any{"name": f.Name, "args": args, "type": ref(f.Type)})
		}
		t["fields"] = fields
		var values []any
		for _, v := range d.EnumValues {
			values = append(values, map[string]any{"name": v.Name})
		}
		t["enumValues"] = values
		types = append(types, t)
	}
	return map[string]any{"queryType": name(s.Query), "mutationType": name(s.Mutation), "subscriptionType": name(s.Subscription), "types": types}
}

func serve(t *testing.T) (*parcels, *cloudtest.Harness, string) {
	t.Helper()
	sdl, err := os.ReadFile("testdata/parcels.graphql")
	if err != nil {
		t.Fatal(err)
	}
	p := &parcels{schema: gqlparser.MustLoadSchema(&ast.Source{Input: string(sdl)})}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	h := cloudtest.New(t, Pack())
	if err := os.CopyFS(h.Dir, os.DirFS("testdata")); err != nil {
		t.Fatal(err)
	}
	return p, h, srv.URL
}

func TestQueriesAndMutations(t *testing.T) {
	_, h, url := serve(t)
	h.OK("the parcels graphql service with the following properties:", [][]string{{"url", url + "/graphql"}})
	h.OK("the graphql/parcel.graphql query is sent to the parcels graphql service with the following variables:", [][]string{{"reference", "PX-GQL-7201"}})
	h.OK("the parcels graphql service answered without errors")
	h.OK("the parcels graphql service's data has the following properties:", [][]string{
		{"parcel.reference", "PX-GQL-7201"}, {"parcel.status", "IN_TRANSIT"}, {"parcel.shop.name", "Maple Home"}, {"parcel.label", "undefined"},
	})
	_ = h.Fails("the parcels graphql service's data has the following properties:", "parcel.status", [][]string{{"parcel.status", "DELIVERED"}})
	_ = h.Fails("the parcels graphql service answered an error where:", "The parcels graphql service answered Parcel without errors", [][]string{{"message", "x"}})

	h.OK("a query is sent to the parcels graphql service:", `query Parcel { parcel(reference: "PX-GQL-7299") { reference } }`)
	h.OK("the parcels graphql service's data has the following properties:", [][]string{{"parcel", "null"}})

	// Errors are the answer's, for the checks.
	h.OK("the graphql/hold-parcel.graphql mutation is sent to the parcels graphql service with the following variables:",
		[][]string{{"reference", "PX-GQL-7203"}, {"until", "2026-10-05"}})
	h.OK("the parcels graphql service answered an error where:", [][]string{{"path", "holdParcel"}, {"extensions.code", "NOT_HOLDABLE"}})
	_ = h.Fails("the parcels graphql service answered without errors", `answered HoldParcel with 1 error(s)`)
	_ = h.Fails("the parcels graphql service answered an error where:", "None of the 1 error(s) the parcels graphql service answered HoldParcel met the conditions",
		[][]string{{"extensions.code", "FORBIDDEN"}})

	// The step says what the operation is.
	_ = h.Fails("the graphql/hold-parcel.graphql query is sent to the parcels graphql service with the following variables:",
		"the operation is a mutation, not a query", [][]string{{"reference", "PX-GQL-7203"}})
	_ = h.Fails("the graphql/parcel-scanned.graphql query is sent to the parcels graphql service", "the operation is a subscription: start it with")
	_ = h.Fails("a query is sent to the parcels graphql service:", "the operation is not GraphQL", `{ parcel(`)
}

func TestTheSchemaIsTheContract(t *testing.T) {
	for _, schema := range []string{"parcels.graphql", "introspection"} {
		t.Run(schema, func(t *testing.T) {
			p, h, url := serve(t)
			h.OK("the parcels graphql service with the following properties:", [][]string{{"url", url + "/graphql"}, {"schema", schema}})
			h.OK("the graphql/parcel.graphql query is sent to the parcels graphql service with the following variables:", [][]string{{"reference", "PX-GQL-7201"}})

			// What the schema does not have is not sent.
			calls := p.calls.Load()
			_ = h.Fails("the graphql/signed-by.graphql query is sent to the parcels graphql service with the following variables:",
				"SignedBy breaks the parcels graphql service's schema:\n- validation.operation.FieldsOnCorrectType: Cannot query field \"signedBy\" on type \"Parcel\".",
				[][]string{{"reference", "PX-GQL-7205"}})
			_ = h.Fails("the graphql/parcel.graphql query is sent to the parcels graphql service",
				"- validation.variables.missing: ")
			if p.calls.Load() != calls {
				t.Error("an operation that breaks the schema was sent")
			}
			// A variable takes its declared type: a postcode is text.
			h.OK("the graphql/parcels-by-postcode.graphql query is sent to the parcels graphql service with the following variables:",
				[][]string{{"shop", "maple-crafts"}, {"postcode", "10115"}})
			h.OK("the parcels graphql service answered without errors")

			// What the service answers is checked too.
			err := h.Fails("the graphql/bad-parcel.graphql query is sent to the parcels graphql service with the following variables:",
				"the answer to BadParcel breaks the parcels graphql service's schema:", [][]string{{"reference", "PX-GQL-7206"}})
			for _, want := range []string{
				`- validation.response.enum: data.parcel.status: "LOST" is not a Status`,
				`- validation.response.type: data.parcel.weightGrams: "heavy", not a Int`,
				`- validation.response.unknownField: data.parcel.label: the operation does not select it`,
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the failure lacks %q:\n%v", want, err)
				}
			}

			// A scenario relaxes what it breaks on purpose, and sees the service's answer.
			h.OK("the GraphQL validation levels are:", [][]string{{"validation.operation.FieldsOnCorrectType", "IGNORE"}, {"validation.response", "WARN"}})
			h.OK("the graphql/signed-by.graphql query is sent to the parcels graphql service with the following variables:", [][]string{{"reference", "PX-GQL-7205"}})
			h.OK("the parcels graphql service answered an error where:", [][]string{{"message", `Cannot query field "signedBy" on type "Parcel".`}})
			h.OK("the graphql/bad-parcel.graphql query is sent to the parcels graphql service with the following variables:", [][]string{{"reference", "PX-GQL-7206"}})
			if !strings.Contains(strings.Join(h.Sink.Logs, "\n"), "GraphQL WARN validation.response.enum") {
				t.Errorf("the relaxed finding was not logged:\n%s", strings.Join(h.Sink.Logs, "\n"))
			}
			_ = h.Fails("the GraphQL validation levels are:", `unknown GraphQL validation key "validation.operations"; did you mean validation.operation?`,
				[][]string{{"validation.operations", "WARN"}})
		})
	}
}

func TestASubgraphSchema(t *testing.T) {
	_, h, url := serve(t)
	h.OK("the shops graphql service with the following properties:", [][]string{{"url", url + "/graphql"}, {"schema", "shops-subgraph.graphql"}})
	_ = h.Fails("a query is sent to the shops graphql service:", "validation.operation.FieldsOnCorrectType", `{ shop(id: "maple-crafts") { tier } }`)
}

func TestSubscriptions(t *testing.T) {
	for _, transport := range []string{"graphql-ws", "sse"} {
		t.Run(transport, func(t *testing.T) {
			p, h, url := serve(t)
			rows := [][]string{{"url", url + "/graphql"}, {"schema", "parcels.graphql"}, {"subscriptions", transport}}
			h.OK("the parcels graphql service with the following properties:", rows)
			h.OK("the graphql/parcel-scanned.graphql subscription is started on the parcels graphql service with the following variables:",
				[][]string{{"reference", "PX-GQL-7207"}})
			h.OK("within 5s the parcels graphql service's subscription received a message where:", [][]string{
				{"parcelScanned.reference", "PX-GQL-7207"}, {"parcelScanned.status", "OUT_FOR_DELIVERY"},
			})
			h.OK("within 5s the parcels graphql service's subscription received a message where:", [][]string{{"parcelScanned.status", "DELIVERED"}})
			// A subscription that ended fails a check at once.
			start := time.Now()
			_ = h.Fails("within 1m the parcels graphql service's subscription received a message where:", "the service completed the subscription",
				[][]string{{"parcelScanned.status", "RETURNED"}})
			if time.Since(start) > 10*time.Second {
				t.Errorf("the check waited %s for a subscription that had ended", time.Since(start))
			}
			_ = h.Fails("the graphql/parcel.graphql subscription is started on the parcels graphql service with the following variables:",
				"the operation is a query, not a subscription", [][]string{{"reference", "PX-GQL-7207"}})
			if transport != "graphql-ws" {
				return
			}
			// A subscription belongs to its scenario: it ends with it.
			h.OK("the graphql/parcel-scanned.graphql subscription is started on the parcels graphql service with the following variables:",
				[][]string{{"reference", "PX-GQL-7297"}})
			h.OK("within 5s the parcels graphql service's subscription received a message where:", [][]string{{"parcelScanned.reference", "PX-GQL-7297"}})
			if err := h.End("passed"); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for p.subs.Load() != 0 && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if n := p.subs.Load(); n != 0 {
				t.Errorf("%d subscriptions still open after their scenario ended", n)
			}
		})
	}
}

func TestAnswersThatAreNotGraphQL(t *testing.T) {
	_, h, url := serve(t)
	h.OK("the parcels graphql service with the following properties:", [][]string{{"url", url + "/html"}})
	_ = h.Fails("the graphql/parcel.graphql query is sent to the parcels graphql service with the following variables:",
		"the parcels graphql service answered HTTP 502, not a GraphQL response", [][]string{{"reference", "PX-GQL-7208"}})
	_ = h.Fails("the other graphql service with the following properties:", `the other graphql service's url is http:// or https://, not "localhost:8400"`,
		[][]string{{"url", "localhost:8400"}})
	_ = h.Fails("the other graphql service with the following properties:", `the other graphql service's subscriptions are graphql-ws or sse, not "mqtt"`,
		[][]string{{"url", url}, {"subscriptions", "mqtt"}})
	_ = h.Fails("the parcel graphql service with the following properties:", "is not a GraphQL schema",
		[][]string{{"url", url}, {"schema", "graphql/parcel.graphql"}})
	_ = h.Fails("the graphql/parcel.graphql query is sent to the payments graphql service", `no graphql service named "payments"`)
	_ = context.Background
}

// Every rule the validator has is a key levels can be set on.
func TestEveryRuleIsAKey(t *testing.T) {
	for name := range rules.NewDefaultRules().GetInner() {
		if err := levelKeys.Check("validation.operation." + name); err != nil {
			t.Errorf("the validator's rule %s is not a key: %v", name, err)
		}
	}
}
