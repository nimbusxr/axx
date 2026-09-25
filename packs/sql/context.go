package sql

import (
	"context"
	"fmt"

	"github.com/nimbusxr/axx/core"
)

// Context returns the sql pack's context for a scenario: the databases
// registered in it with their connections, selections, triggers and row
// locks. The pack's steps and other packs share it.
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

// Connect opens (or reuses, for the whole run) a connection pool and returns
// an unregistered service; add it with AddService. url may be a JDBC URL
// (jdbc:postgresql://...) or a native one.
func Connect(sc *core.Scenario, name, url, user, password, schema string) (*Service, error) {
	t, err := Resolve(url, user, password)
	if err != nil {
		return nil, err
	}
	db, err := openDB(sc.Suite(), t)
	if err != nil {
		return nil, err
	}
	return &Service{Name: name, URL: url, User: user, Password: password, Schema: schema, Target: t, DB: db}, nil
}

// Selections returns the service's selections in the order they were
// retrieved.
func (svc *Service) Selections() []*Selection {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return append([]*Selection(nil), svc.selections...)
}

// Selection returns selection i (0-based: the 1st selection is 0).
func (svc *Service) Selection(i int) (*Selection, error) { return svc.selection(i) }

// AddSelection appends a selection, so the pack's selection steps see it as
// the next one.
func (svc *Service) AddSelection(sel *Selection) { svc.addSelection(sel) }

// Query runs a SELECT on the service's connection pool and returns the rows
// as a selection, without adding it.
func (svc *Service) Query(ctx context.Context, table, query string) (*Selection, error) {
	return svc.query(ctx, table, query)
}

// Triggers returns the triggers created in the scenario, in order.
func (svc *Service) Triggers() []*Trigger {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return append([]*Trigger(nil), svc.triggers...)
}
