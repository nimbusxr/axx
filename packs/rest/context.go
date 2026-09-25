package rest

import (
	"fmt"
	"net/http"

	"github.com/nimbusxr/axx/core"
)

// Context returns the rest pack's context for a scenario: the REST services
// registered in it and their requests and responses. The pack's steps and
// other packs share it.
func Context(sc *core.Scenario) *ScenarioContext { return stateKey.Of(sc) }

// Service returns the named service, or the first one registered (the
// default) when no name is given.
func (c *ScenarioContext) Service(name ...string) (*Service, error) {
	switch len(name) {
	case 0:
		return c.services.Default()
	case 1:
		return c.services.Lookup(name[0])
	}
	return nil, fmt.Errorf("Service takes at most one name, got %d", len(name))
}

// Services returns the registered services in registration order.
func (c *ScenarioContext) Services() []*Service { return c.services.All() }

// AddService registers a service under its Name. The first service
// registered is the default; registering a name twice is an error.
func (c *ScenarioContext) AddService(svc *Service) error { return c.services.Add(svc.Name, svc) }

// Requests returns the service's requests in the order they were added.
func (svc *Service) Requests() []*Request {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return append([]*Request(nil), svc.requests...)
}

// Request returns request i (0-based: the 1st request is 0).
func (svc *Service) Request(i int) (*Request, error) { return svc.request(i) }

// AddRequest appends a request, so the pack's steps see it as the next one.
func (svc *Service) AddRequest(method, path string) *Request {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	r := &Request{Method: method, Path: path}
	svc.requests = append(svc.requests, r)
	return r
}

// Header returns the request's headers.
func (r *Request) Header() http.Header { return r.header() }

// SetHeader adds a request header (Content-Type and Accept replace an
// earlier value).
func (r *Request) SetHeader(name, value string) { r.addHeader(name, value) }

// Exchange returns the executed request and its response, or nil before the
// request is executed.
func (r *Request) Exchange() *Exchange { return r.exchange }
