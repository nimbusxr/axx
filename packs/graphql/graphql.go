// Package graphql is the graphql pack: queries, mutations and subscriptions
// to GraphQL services (a federated graph's gateway, or a subgraph alone),
// their data and errors, checked against the services' schemas.
package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/oaslevel"
	"github.com/nimbusxr/axx/internal/secrets"
)

// Name is the pack's name.
const Name = "graphql"

const since = "0.1.5"

const defaultTimeout = 10 * time.Second

// Subscription transports.
const (
	graphqlWS = "graphql-ws"
	sse       = "sse"
)

// Pack returns the graphql pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: steps(), ConfigSchema: []byte(configSchema)}
}

const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "levels": {
      "type": "object",
      "description": "Validation key -> ERROR (or FAIL), WARN, INFO or IGNORE, for every scenario.",
      "additionalProperties": {"type": "string", "enum": ["ERROR", "FAIL", "WARN", "INFO", "IGNORE"]}
    }
  }
}`

// Config is the pack's section of axx.yaml.
type Config struct {
	// Levels maps validation keys to ERROR (or FAIL), WARN, INFO or IGNORE.
	Levels map[string]string `json:"levels,omitempty"`
}

type settings struct {
	levels oaslevel.Levels
	client *http.Client

	mu      sync.Mutex
	schemas map[string]*ast.Schema // by the schema row, and the service's url for introspection
}

func settingsFor(s *core.Suite) (*settings, error) {
	return core.Cached(s, Name+"/settings", func() (*settings, error) {
		var c Config
		if err := s.PackConfig(Name, &c); err != nil {
			return nil, err
		}
		levels, err := oaslevel.ParseMap(c.Levels, levelKeys)
		if err != nil {
			return nil, fmt.Errorf("packs.graphql.levels: %w", err)
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		s.OnClose(func(context.Context) error {
			tr.CloseIdleConnections()
			return nil
		})
		return &settings{levels: levels, client: &http.Client{Transport: tr}, schemas: map[string]*ast.Schema{}}, nil
	})
}

// service is a GraphQL service a scenario registered.
type service struct {
	name, url   string
	headers     http.Header
	timeout     time.Duration
	schemaRow   string
	schema      *ast.Schema // nil without a schema row
	subProtocol string
	subURL      string

	mu   sync.Mutex
	last *answer
	sub  *subscription
}

// answer is what the service answered an operation.
type answer struct {
	op     *operation
	status int
	data   json.RawMessage // "null" when it answered no data
	errors []json.RawMessage
}

func (a *answer) errorText() string {
	msgs := make([]string, len(a.errors))
	for i, e := range a.errors {
		msgs[i] = string(cloudstep.Compact(e))
	}
	return strings.Join(msgs, "; ")
}

var services = core.NewStateKey(Name, func(sc *core.Scenario) *core.Services[*service] {
	all := core.NewServices[*service]("GraphQL service",
		`No GraphQL service is registered in this scenario; register one with "the {word} graphql service with the following properties:"`)
	sc.Describe(Name, func() any { return describe(sc, all) })
	return all
}, func(_ *core.Scenario, all *core.Services[*service]) error {
	for _, s := range all.All() {
		s.mu.Lock()
		if s.sub != nil {
			s.sub.close()
		}
		s.mu.Unlock()
	}
	return nil
})

func register(sc *core.Scenario, a core.Args) error {
	name := a.String(0)
	if a.Table == nil {
		return errors.New(`the graphql service property "url" is required`)
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	s := &service{name: name, headers: http.Header{}, timeout: defaultTimeout, subProtocol: graphqlWS}
	for _, p := range pairs {
		v, err := secrets.Resolve(sc, p.Value)
		if err != nil {
			return err
		}
		v = strings.TrimSpace(v)
		switch {
		case p.Key == "url":
			s.url = v
		case p.Key == "schema":
			s.schemaRow = v
		case strings.HasPrefix(p.Key, "header."):
			s.headers.Add(strings.TrimPrefix(p.Key, "header."), v)
		case p.Key == "timeout":
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return fmt.Errorf("the %s graphql service's timeout %q is not a duration, like 5s or 1m", name, p.Value)
			}
			s.timeout = d
		case p.Key == "subscriptions":
			if v != graphqlWS && v != sse {
				return fmt.Errorf("the %s graphql service's subscriptions are %s or %s, not %q", name, graphqlWS, sse, p.Value)
			}
			s.subProtocol = v
		case p.Key == "subscriptions url":
			s.subURL = v
		default:
			return fmt.Errorf("unknown graphql service property %q (supported: url, schema, header.<name>, timeout, subscriptions, subscriptions url)", p.Key)
		}
	}
	if !strings.HasPrefix(s.url, "http://") && !strings.HasPrefix(s.url, "https://") {
		return fmt.Errorf(`the %s graphql service's url is http:// or https://, not %q`, name, s.url)
	}
	if s.schemaRow != "" {
		if s.schema, err = s.loadSchema(sc); err != nil {
			return secrets.Hide(sc, err)
		}
	}
	if err := services.Of(sc).Add(name, s); err != nil {
		return err
	}
	sc.Log("registered the %s graphql service: %s", name, secrets.Mask(sc, s.url))
	return nil
}

// loadSchema reads the service's schema, once per run.
func (s *service) loadSchema(sc *core.Scenario) (*ast.Schema, error) {
	st, err := settingsFor(sc.Suite())
	if err != nil {
		return nil, err
	}
	key := s.schemaRow
	if key == introspection {
		key = introspection + "|" + s.url
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if schema, ok := st.schemas[key]; ok {
		return schema, nil
	}
	schema, err := loadSchema(sc, s, st.client)
	if err != nil {
		return nil, err
	}
	st.schemas[key] = schema
	return schema, nil
}

func get(sc *core.Scenario, name string) (*service, error) {
	s, err := services.Of(sc).Get(name)
	if err != nil {
		return nil, fmt.Errorf("no graphql service named %q in this scenario; register it first with \"the %s graphql service with the following properties:\"", name, name)
	}
	return s, nil
}

// post sends a GraphQL request and reads its answer: a JSON object with
// data or errors, whatever its HTTP status.
func (s *service) post(ctx context.Context, client *http.Client, body map[string]any) (*answer, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	for k, vs := range s.headers {
		req.Header[k] = vs
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/graphql-response+json, application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the %s graphql service: %w", s.name, err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var r struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(out, &r) != nil || (r.Data == nil && r.Errors == nil) {
		return nil, fmt.Errorf("the %s graphql service answered HTTP %d, not a GraphQL response: %s", s.name, res.StatusCode, cloudstep.Compact(out))
	}
	if r.Data == nil {
		r.Data = json.RawMessage("null")
	}
	return &answer{status: res.StatusCode, data: r.Data, errors: r.Errors}, nil
}

func describe(sc *core.Scenario, all *core.Services[*service]) any {
	out := map[string]any{}
	for _, s := range all.All() {
		s.mu.Lock()
		a := s.last
		s.mu.Unlock()
		if a == nil {
			continue
		}
		d := map[string]any{"operation": a.op.text, "data": secrets.Mask(sc, string(a.data))}
		if a.op.variables != nil {
			d["variables"] = secrets.Mask(sc, string(a.op.variables))
		}
		if len(a.errors) > 0 {
			d["errors"] = secrets.Mask(sc, a.errorText())
		}
		out[s.name] = d
	}
	return out
}

const packDoc = `Send queries, mutations and subscriptions to GraphQL services, and check their data and their errors, against the services' schemas. A federated graph's gateway and a subgraph alone are GraphQL services alike: register each at its URL.

` + "```gherkin" + `
Given the parcels graphql service with the following properties:
  | url    | http://localhost:8400/graphql |
  | schema | introspection                 |
` + "```" + `

- **Operations** come from a file of the project (` + "`.graphql`" + `) or a doc string, with variables from a table: a variable takes the type the operation declares for it, so ` + "`10115`" + ` is text for a ` + "`String!`" + `.
- **The checks read the service's last answer** in the scenario. An answer with errors does not fail the step that sends the operation: check its errors.
- **Subscriptions** go over graphql-ws (the ` + "`graphql-transport-ws`" + ` protocol, on the url with ` + "`ws://`" + ` or ` + "`wss://`" + `) or over server-sent events, and belong to the scenario, which ends them when it ends.
- **The schema is the contract:** with a ` + "`schema`" + ` row (an SDL file of the project, a URL, or ` + "`introspection`" + `), every operation and its variables are checked against it before they are sent, and every answer's data against the operation's types. A subgraph's SDL may use the federation directives (` + "`@key`" + `, ` + "`@link`" + `...).

Findings have keys and levels, like the REST pack's OpenAPI findings:

| Key | Found when |
|---|---|
| ` + "`validation.operation.<rule>`" + ` | the operation breaks a validation rule of the GraphQL spec (` + "`FieldsOnCorrectType`" + `, ` + "`KnownArgumentNames`" + `, ` + "`ProvidedRequiredArguments`" + `...) |
| ` + "`validation.variables.missing`" + ` | a required variable is missing |
| ` + "`validation.variables.type`" + ` | a variable is not of its type |
| ` + "`validation.response.nonNull`" + ` | a non-null field is null, without an error that explains it |
| ` + "`validation.response.type`" + ` | a field's value is not of its type |
| ` + "`validation.response.enum`" + ` | a value is not one of its enum's |
| ` + "`validation.response.missingField`" + ` | a field the operation selects is missing |
| ` + "`validation.response.unknownField`" + ` | the data has a field the operation does not select |

- **Levels:** ` + "`ERROR`" + ` (or ` + "`FAIL`" + `) fails the step, ` + "`WARN`" + ` and ` + "`INFO`" + ` log the finding, ` + "`IGNORE`" + ` drops it. Every finding is an ` + "`ERROR`" + ` unless ` + "`packs.graphql.levels`" + ` in axx.yaml, or the scenario's ` + "`the GraphQL validation levels are:`" + `, says otherwise.
- **Keys cover the keys below them:** ` + "`validation.operation`" + ` sets every operation finding; the most specific key set wins.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the properties are masked in logs and failures, and ` + "`${token:..}`" + ` names a token of the scenario.`
