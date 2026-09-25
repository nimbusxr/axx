package mongo

import (
	"fmt"

	driver "go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/nimbusxr/axx/core"
)

// Context returns the mongo pack's context for a scenario: the MongoDB
// databases registered in it and their selections. The pack's steps and
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

// DB returns the service's database handle (its client is shared for the
// whole run).
func (svc *Service) DB() *driver.Database { return svc.db }

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
