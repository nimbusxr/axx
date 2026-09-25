package kafka

import (
	"fmt"

	"github.com/nimbusxr/axx/core"
)

// Context returns the kafka pack's context for a scenario: the Kafka
// services registered in it, their topic clients, drafted events and the
// records assertions matched. The pack's steps and other packs share it.
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

// Topic returns the topic client for a topic, if the scenario created one.
func (s *Service) Topic(name string) (*TopicClient, bool) { return s.client(name) }

// Topics returns the service's topic clients in creation order.
func (s *Service) Topics() []*TopicClient { return s.topicClients() }

// Events returns the events drafted for the topic, in order.
func (tc *TopicClient) Events() []*Event {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return append([]*Event(nil), tc.events...)
}
