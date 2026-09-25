package mock

import (
	"fmt"

	"github.com/nimbusxr/axx/core"
)

// Context returns the mock pack's context for a scenario: the mocked
// (WireMock) services registered in it and their named requests. The
// pack's steps and other packs share it.
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

// NewService returns an unregistered mocked service for the WireMock
// instance at url (its admin API is at url + "/__admin"); add it with
// AddService.
func NewService(name, url string) (*Service, error) {
	c, err := newClient(url, sharedHTTP)
	if err != nil {
		return nil, err
	}
	return &Service{Name: name, URL: url, c: c, requests: map[string]*pattern{}}, nil
}
