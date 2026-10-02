// Package jsonrpc is the jsonrpc pack: calls to JSON-RPC 2.0 services over
// HTTP, their results and errors, checked against the services' OpenRPC
// documents.
package jsonrpc

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
	"sync/atomic"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/oaslevel"
	"github.com/nimbusxr/axx/internal/schemadoc"
	"github.com/nimbusxr/axx/internal/secrets"
	"github.com/nimbusxr/axx/internal/tablevalue"
)

// Name is the pack's name.
const Name = "jsonrpc"

const since = "0.1.5"

const defaultTimeout = 10 * time.Second

// discover is the openrpc row that reads a service's document from the
// service itself, with the rpc.discover method OpenRPC defines.
const discover = "rpc.discover"

const packDoc = `Call JSON-RPC 2.0 services over HTTP, and check their results and their errors, against their OpenRPC documents.

Register the service once, usually in the ` + "`Background`" + `:

` + "```gherkin" + `
Given the depots jsonrpc service with the following properties:
  | url     | http://localhost:8400/rpc |
  | openrpc | rpc.discover              |
` + "```" + `

- **Params** come from a table (by name, set as the REST pack's request properties are) or a doc string (an object by name, or an array by position).
- **The checks read the service's last call** in the scenario. A call that answers an error does not fail the step that makes it: check its error.
- **OpenRPC:** an ` + "`openrpc`" + ` row names the service's OpenRPC document: a file of the project, a URL, or ` + "`rpc.discover`" + ` to ask the service for it. Every call's params are then checked against its method before it is sent, and every result and error when it comes back. A call that breaks the document fails the step that makes it, and names the rule it broke.

OpenRPC findings have keys and levels, like the REST pack's OpenAPI findings:

| Key | Found when |
|---|---|
| ` + "`validation.method.unknown`" + ` | the document has no such method |
| ` + "`validation.params.structure`" + ` | the params are by name for a method that takes them by position, or the other way round |
| ` + "`validation.params.missing`" + ` | a required param is missing |
| ` + "`validation.params.unknown`" + ` | a param the method does not have |
| ` + "`validation.params.schema.<keyword>`" + ` | a param breaks a keyword of its schema (` + "`required`" + `, ` + "`type`" + `, ` + "`pattern`" + `...) |
| ` + "`validation.result.schema.<keyword>`" + ` | the result breaks a keyword of its schema |
| ` + "`validation.error.unknown`" + ` | the error is not one the method declares, nor one of JSON-RPC's own (-32700, -32600 to -32603, -32000 to -32099) |

- **Levels:** ` + "`ERROR`" + ` (or ` + "`FAIL`" + `) fails the step, ` + "`WARN`" + ` and ` + "`INFO`" + ` log the finding, ` + "`IGNORE`" + ` drops it. Every finding is an ` + "`ERROR`" + ` unless ` + "`packs.jsonrpc.openrpc.levels`" + ` in axx.yaml, or the scenario's ` + "`the OpenRPC validation levels are:`" + `, says otherwise.
- **Keys cover the keys below them:** ` + "`validation.params`" + ` sets every params finding; the most specific key set wins.
- **Secrets stay secret:** ` + "`${env:..}`" + ` values in the properties are masked in logs and failures, and ` + "`${token:..}`" + ` names a token of the scenario.`

// Pack returns the jsonrpc pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: steps(), ConfigSchema: []byte(configSchema)}
}

const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "openrpc": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "levels": {
          "type": "object",
          "description": "Validation key -> ERROR (or FAIL), WARN, INFO or IGNORE, for every scenario.",
          "additionalProperties": {"type": "string", "enum": ["ERROR", "FAIL", "WARN", "INFO", "IGNORE"]}
        }
      }
    }
  }
}`

// Config is the pack's section of axx.yaml.
type Config struct {
	OpenRPC struct {
		// Levels maps validation keys to ERROR (or FAIL), WARN, INFO or IGNORE.
		Levels map[string]string `json:"levels,omitempty"`
	} `json:"openrpc"`
}

type settings struct {
	levels oaslevel.Levels
	client *http.Client

	mu   sync.Mutex
	docs map[string]*document // by URL, or by service for rpc.discover
}

func settingsFor(s *core.Suite) (*settings, error) {
	return core.Cached(s, Name+"/settings", func() (*settings, error) {
		var c Config
		if err := s.PackConfig(Name, &c); err != nil {
			return nil, err
		}
		levels, err := oaslevel.ParseMap(c.OpenRPC.Levels, levelKeys)
		if err != nil {
			return nil, fmt.Errorf("packs.jsonrpc.openrpc.levels: %w", err)
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		s.OnClose(func(context.Context) error {
			tr.CloseIdleConnections()
			return nil
		})
		return &settings{levels: levels, client: &http.Client{Transport: tr}, docs: map[string]*document{}}, nil
	})
}

// service is a JSON-RPC service a scenario registered.
type service struct {
	name, url string
	headers   http.Header
	timeout   time.Duration
	openrpc   string
	doc       *document

	ids  atomic.Int64
	mu   sync.Mutex
	last *call
}

// call is a call a scenario made, and what the service answered.
type call struct {
	method string
	params json.RawMessage // nil without params
	result json.RawMessage // nil when it answered an error
	err    json.RawMessage // the error object
	code   int
	text   string // the error's message
}

var services = core.NewStateKey(Name, func(sc *core.Scenario) *core.Services[*service] {
	all := core.NewServices[*service]("JSON-RPC service",
		`No JSON-RPC service is registered in this scenario; register one with "the {word} jsonrpc service with the following properties:"`).RegisteredBy("the {word} jsonrpc service with the following properties:")
	sc.Describe(Name, func() any { return describe(sc, all) })
	return all
}, nil)

type levelsState struct {
	mu     sync.Mutex
	levels oaslevel.Levels
}

var scenarioLevels = core.NewStateKey(Name+".levels", func(*core.Scenario) *levelsState {
	return &levelsState{levels: oaslevel.Levels{}}
}, nil)

func register(sc *core.Scenario, a core.Args) error {
	name := a.String(0)
	if a.Table == nil {
		return errors.New(`the jsonrpc service property "url" is required`)
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	s := &service{name: name, headers: http.Header{}, timeout: defaultTimeout}
	for _, p := range pairs {
		v, err := secrets.Resolve(sc, p.Value)
		if err != nil {
			return err
		}
		v = strings.TrimSpace(v)
		switch {
		case p.Key == "url":
			s.url = v
		case strings.HasPrefix(p.Key, "header."):
			s.headers.Add(strings.TrimPrefix(p.Key, "header."), v)
		case p.Key == "timeout":
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return fmt.Errorf("the %s jsonrpc service's timeout %q is not a duration, like 5s or 1m", name, p.Value)
			}
			s.timeout = d
		case p.Key == "openrpc":
			s.openrpc = v
		default:
			return fmt.Errorf("unknown jsonrpc service property %q (supported: url, header.<name>, timeout, openrpc)", p.Key)
		}
	}
	if !strings.HasPrefix(s.url, "http://") && !strings.HasPrefix(s.url, "https://") {
		return fmt.Errorf(`the %s jsonrpc service's url is http:// or https://, not %q`, name, s.url)
	}
	if s.openrpc != "" {
		if s.doc, err = s.document(sc); err != nil {
			return secrets.Hide(sc, fmt.Errorf("cannot read the OpenRPC document of the %s jsonrpc service: %w", name, err))
		}
	}
	if err := services.Of(sc).Add(name, s); err != nil {
		return err
	}
	sc.Log("registered the %s jsonrpc service: %s", name, secrets.Mask(sc, s.url))
	return nil
}

// document reads the service's OpenRPC document, once per run.
func (s *service) document(sc *core.Scenario) (*document, error) {
	st, err := settingsFor(sc.Suite())
	if err != nil {
		return nil, err
	}
	key := s.openrpc
	if s.openrpc == discover {
		key = discover + "|" + s.url
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if d, ok := st.docs[key]; ok {
		return d, nil
	}
	l := schemadoc.NewLoader()
	var u string
	if s.openrpc == discover {
		res, err := s.post(sc, st.client, discover, nil)
		if err != nil {
			return nil, err
		}
		if res.result == nil {
			return nil, fmt.Errorf("rpc.discover answered the error %d (%q)", res.code, res.text)
		}
		doc, err := schemadoc.Decode(res.result)
		if err != nil {
			return nil, fmt.Errorf("rpc.discover answered what is not JSON: %w", err)
		}
		// The document's own $refs are relative to the service's URL.
		l.Add(s.url, doc)
		u = s.url
	} else if u, err = schemadoc.Locate(sc.Suite(), s.openrpc); err != nil {
		return nil, err
	}
	d, err := readDocument(l, s.openrpc, u)
	if err != nil {
		return nil, err
	}
	st.docs[key] = d
	return d, nil
}

func get(sc *core.Scenario, name string) (*service, error) {
	s, err := services.Of(sc).Get(name)
	if err != nil {
		return nil, fmt.Errorf("no jsonrpc service named %q in this scenario; register it first with \"the %s jsonrpc service with the following properties:\"", name, name)
	}
	return s, nil
}

// params builds a call's params from a table (by name) or a doc string. A
// param text says is text is its text as written (01067 keeps its zero).
func params(sc *core.Scenario, t *core.Table, doc *core.DocString, text func(path string) bool) (json.RawMessage, error) {
	switch {
	case t != nil:
		pairs, err := t.Pairs()
		if err != nil {
			return nil, err
		}
		rows := make([]tablevalue.Row, len(pairs))
		for i, p := range pairs {
			rows[i] = tablevalue.Row{Path: p.Key, Null: p.Null}
			if !p.Null {
				if rows[i].Value, err = secrets.Resolve(sc, p.Value); err != nil {
					return nil, err
				}
			}
		}
		obj, err := tablevalue.Build(rows, text)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(obj)
		return json.RawMessage(b), err
	case doc != nil:
		text, err := secrets.Resolve(sc, doc.Content)
		if err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal([]byte(text), &v); err != nil {
			return nil, fmt.Errorf("the params are not JSON: %w", err)
		}
		switch v.(type) {
		case map[string]any, []any:
		default:
			return nil, errors.New("the params are an object (by name) or an array (by position)")
		}
		return json.RawMessage(cloudstep.Compact([]byte(text))), nil
	}
	return nil, nil
}

// invoke calls a method, checks the call against the service's OpenRPC
// document, and keeps it as the service's last. An error the service
// answers is the call's, which the checks read.
func (s *service) invoke(sc *core.Scenario, method string, ps json.RawMessage, fromTable bool) error {
	st, err := settingsFor(sc.Suite())
	if err != nil {
		return err
	}
	if s.doc != nil {
		var v any
		if ps != nil {
			v, _ = schemadoc.Decode(ps)
		}
		if obj, ok := v.(map[string]any); ok && fromTable {
			s.doc.conform(method, obj)
			if ps, err = json.Marshal(obj); err != nil {
				return err
			}
		}
		if err := s.report(sc, st, method, "the params", s.doc.checkParams(method, v)); err != nil {
			return err
		}
	}
	c, err := s.post(sc, st.client, method, ps)
	if err != nil {
		return secrets.Hide(sc, err)
	}
	s.mu.Lock()
	s.last = c
	s.mu.Unlock()
	if c.result != nil {
		sc.Log("called %s on the %s jsonrpc service: a result", method, s.name)
	} else {
		sc.Log("called %s on the %s jsonrpc service: the error %d (%q)", method, s.name, c.code, secrets.Mask(sc, c.text))
	}
	if s.doc == nil {
		return nil
	}
	if c.result != nil {
		v, _ := schemadoc.Decode(c.result)
		return s.report(sc, st, method, "the result", s.doc.checkResult(method, v))
	}
	return s.report(sc, st, method, "the error", s.doc.checkError(method, c.code))
}

// report fails on the findings at the ERROR level, and logs the others.
func (s *service) report(sc *core.Scenario, st *settings, method, what string, found []finding) error {
	if len(found) == 0 {
		return nil
	}
	levels := st.levels
	if ls, ok := scenarioLevels.Peek(sc); ok {
		ls.mu.Lock()
		levels = levels.Merge(ls.levels)
		ls.mu.Unlock()
	}
	var errs []string
	for _, f := range found {
		switch lv := levels.Resolve(f.Key); lv {
		case oaslevel.Error:
			errs = append(errs, "- "+f.Key+": "+f.Message)
		case oaslevel.Warn, oaslevel.Info:
			sc.Log("OpenRPC %s %s: %s", lv, f.Key, f.Message)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return secrets.Hide(sc, core.Failf("%s of %s break %s:\n%s\nTo relax a check, set its key (or a parent key) to WARN, INFO or IGNORE with "+
		"\"Given the OpenRPC validation levels are:\" or packs.jsonrpc.openrpc.levels in axx.yaml.",
		what, method, s.doc.source, strings.Join(errs, "\n")))
}

// post sends a request and reads its response.
func (s *service) post(sc *core.Scenario, client *http.Client, method string, ps json.RawMessage) (*call, error) {
	id := s.ids.Add(1)
	req := map[string]any{"jsonrpc": "2.0", "method": method, "id": id}
	if ps != nil {
		req["params"] = ps
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	sc.Attach("application/json", []byte(secrets.Mask(sc, string(body))), method+" request")
	ctx, cancel := context.WithTimeout(sc.Context(), s.timeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, vs := range s.headers {
		hr.Header[k] = vs
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "application/json")
	res, err := client.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("cannot call %s on the %s jsonrpc service: %w", method, s.name, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	sc.Attach("application/json", []byte(secrets.Mask(sc, string(raw))), method+" response")
	var r struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	notRPC := func(why string) error {
		return fmt.Errorf("the %s jsonrpc service answered %s with HTTP %d, %s: %s", s.name, method, res.StatusCode, why, cloudstep.Compact(raw))
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, notRPC("not a JSON-RPC response")
	}
	if r.JSONRPC != "2.0" {
		return nil, notRPC(`not a JSON-RPC 2.0 response (no "jsonrpc": "2.0")`)
	}
	if string(r.ID) != fmt.Sprint(id) {
		return nil, notRPC(fmt.Sprintf("with the id %s, not %d", r.ID, id))
	}
	c := &call{method: method, params: ps}
	hasResult := len(r.Result) > 0
	hasError := len(r.Error) > 0 && string(r.Error) != "null"
	switch {
	case hasResult && hasError:
		return nil, notRPC("with both a result and an error")
	case hasError:
		var e struct {
			Code    *int   `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(r.Error, &e) != nil || e.Code == nil {
			return nil, notRPC("with an error that has no code")
		}
		c.err, c.code, c.text = r.Error, *e.Code, e.Message
	case hasResult:
		c.result = r.Result
	default:
		return nil, notRPC("with neither a result nor an error")
	}
	return c, nil
}

func describe(sc *core.Scenario, all *core.Services[*service]) any {
	out := map[string]any{}
	for _, s := range all.All() {
		s.mu.Lock()
		c := s.last
		s.mu.Unlock()
		if c == nil {
			continue
		}
		d := map[string]any{"method": c.method}
		if c.params != nil {
			d["params"] = secrets.Mask(sc, string(c.params))
		}
		if c.result != nil {
			d["result"] = secrets.Mask(sc, string(c.result))
		} else {
			d["error"] = secrets.Mask(sc, string(c.err))
		}
		out[s.name] = d
	}
	return out
}
