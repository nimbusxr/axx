// Package rest provides the REST steps: service registration, requests
// (method and path, headers, payloads from OpenAPI examples or an empty
// template, payload properties, form encoding), execution with OpenAPI
// validation of the request and the response, and response assertions.
//
// The value handling (JSONPath, type coercion, regular expressions) comes
// from internal/compat.
package rest

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
)

// Config is the packs.rest section of axx.yaml.
type Config struct {
	TLS TLSConfig `json:"tls,omitzero"`
	// OpenAPI holds the default validation levels. The engine merges the
	// top-level openapi section of axx.yaml in here; keys set under
	// packs.rest.openapi take precedence.
	OpenAPI OpenAPIConfig `json:"openapi,omitzero"`
}

// TLSConfig configures HTTPS.
type TLSConfig struct {
	// Verify turns TLS certificate verification on. It is off by default
	// (relaxed HTTPS validation), so services with self-signed certificates
	// work out of the box.
	Verify bool `json:"verify,omitempty"`
}

// OpenAPIConfig holds OpenAPI validation defaults.
type OpenAPIConfig struct {
	// Levels maps validation keys (e.g. validation.request.body) to ERROR
	// (or FAIL), WARN, INFO or IGNORE.
	Levels map[string]string `json:"levels,omitempty"`
}

const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "tls": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "verify": {"type": "boolean", "description": "Verify TLS certificates (default false)."}
      }
    },
    "openapi": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "levels": {
          "type": "object",
          "description": "Validation key -> ERROR (or FAIL), WARN, INFO or IGNORE. Merged over the top-level openapi.levels.",
          "additionalProperties": {"type": "string", "enum": ["ERROR", "FAIL", "WARN", "INFO", "IGNORE"]}
        }
      }
    }
  }
}`

// settings are the run-wide REST resources: configuration, the shared HTTP
// client and the parsed OpenAPI specifications.
type settings struct {
	levels Levels
	client *http.Client
	specs  *specCache
}

// load returns the suite's REST settings, creating them on first use.
func load(s *core.Suite) (*settings, error) {
	return core.Cached(s, "rest.settings", func() (*settings, error) {
		var c Config
		if err := s.PackConfig("rest", &c); err != nil {
			return nil, err
		}
		levels, err := ParseLevels(c.OpenAPI.Levels)
		if err != nil {
			return nil, fmt.Errorf("openapi.levels: %w", err)
		}
		tr := http.DefaultTransport.(*http.Transport).Clone()
		// Certificate verification is off unless packs.rest.tls.verify is set
		// (relaxed HTTPS validation).
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: !c.TLS.Verify} //nolint:gosec // documented default, opt in with packs.rest.tls.verify
		s.OnClose(func(context.Context) error {
			tr.CloseIdleConnections()
			return nil
		})
		return &settings{
			levels: levels,
			client: &http.Client{Transport: tr, CheckRedirect: checkRedirect},
			specs:  &specCache{m: map[string]*specEntry{}},
		}, nil
	})
}

// checkRedirect follows redirects:
// GET and HEAD requests follow 301/302/303/307/308, other methods follow
// only 303 (as a GET); anything else returns the redirect response itself.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 100 {
		return fmt.Errorf("stopped after %d redirects", len(via))
	}
	switch via[0].Method {
	case http.MethodGet, http.MethodHead:
		return nil
	}
	if req.Response != nil && req.Response.StatusCode == http.StatusSeeOther {
		return nil
	}
	return http.ErrUseLastResponse
}

// Pack returns the REST pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

var _ core.Initializer = pack{}

// Init validates the pack configuration before any scenario runs.
func (pack) Init(_ context.Context, s *core.Suite) error {
	_, err := load(s)
	return err
}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:         "rest",
		Namespace:    "rest",
		Doc:          packDoc,
		ConfigSchema: json.RawMessage(configSchema),
		Params: []core.ParamType{{
			Name:    "service",
			Regexps: []string{`([^\s]+)`},
			Doc:     "The name of a REST service registered in the scenario.",
			Transform: func(sc *core.Scenario, name string, _ []*string) (any, error) {
				return stateKey.Of(sc).services.Get(name)
			},
		}},
		Steps: steps(),
	}
}

// Service is a REST service registered in a scenario.
type Service struct {
	Name string
	URL  string
	// OpenAPI is the specification's URL or file path ("" = no validation).
	OpenAPI string

	mu       sync.Mutex
	requests []*Request
	levels   Levels // scenario overrides of the configured levels
}

// Request is one request of a service, in the order the scenario added it.
type Request struct {
	Method   string
	Path     string
	MimeType string  // set by a payload step
	Payload  *string // nil until a payload step runs
	headers  []header
	exchange *Exchange // nil until executed
}

type header struct{ name, value string }

// Exchange is an executed request and its response.
type Exchange struct {
	Method         string
	URL            string
	RequestHeader  http.Header
	RequestBody    []byte
	Status         int
	Header         http.Header
	Body           []byte
	Duration       time.Duration
	Issues         []Issue // OpenAPI findings that were not ignored
	validationPath string
}

type ScenarioContext struct {
	services *core.Services[*Service]

	mu   sync.Mutex
	last *lastExchange
}

type lastExchange struct {
	service string
	ex      *Exchange
}

var stateKey = core.NewStateKey("rest", func(sc *core.Scenario) *ScenarioContext {
	st := &ScenarioContext{services: core.NewServices[*Service]("Service", "")}
	sc.Describe("rest", st.describe)
	return st
}, nil)

// request returns the request at index i (0-based).
func (svc *Service) request(i int) (*Request, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return svc.requestLocked(i)
}

func (svc *Service) requestLocked(i int) (*Request, error) {
	n := len(svc.requests)
	if n == 0 {
		return nil, fmt.Errorf("No request set for service %s", svc.Name) //nolint:staticcheck // user-facing message
	}
	if i < 0 || i >= n {
		return nil, fmt.Errorf("request %d is not set for service %s; the scenario added %d request(s)", i+1, svc.Name, n)
	}
	return svc.requests[i], nil
}

// requestOrAdd returns the request at index i, adding it when i is the next
// free index. Requests are append-only: the Nth ordered request can only be
// added after the (N-1)th.
func (svc *Service) requestOrAdd(i int) (*Request, error) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	n := len(svc.requests)
	switch {
	case i >= 0 && i < n:
		return svc.requests[i], nil
	case i < 0:
		return nil, fmt.Errorf("Cannot add request at index %d for service %s because index cannot be negative", i, svc.Name) //nolint:staticcheck // user-facing message
	case i > n:
		return nil, fmt.Errorf("Cannot add request at index %d for service %s because the current number of requests is %d", i, svc.Name, n) //nolint:staticcheck // user-facing message
	}
	r := &Request{}
	svc.requests = append(svc.requests, r)
	return r, nil
}

// addHeader adds a request header. Content-Type and Accept replace an
// earlier value; other headers may repeat (RFC 9110 field lines).
func (r *Request) addHeader(name, value string) {
	if http.CanonicalHeaderKey(name) == "Content-Type" || http.CanonicalHeaderKey(name) == "Accept" {
		kept := r.headers[:0]
		for _, h := range r.headers {
			if http.CanonicalHeaderKey(h.name) != http.CanonicalHeaderKey(name) {
				kept = append(kept, h)
			}
		}
		r.headers = kept
	}
	r.headers = append(r.headers, header{name, value})
}

func (r *Request) header() http.Header {
	h := http.Header{}
	for _, kv := range r.headers {
		h.Add(kv.name, kv.value)
	}
	return h
}
