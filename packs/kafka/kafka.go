// Package kafka provides the Kafka steps: services, topic clients (Java
// client properties translated to franz-go), event drafts published as
// plain text or Confluent Avro, and consumer assertions over every record
// of a topic.
package kafka

import (
	"fmt"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nimbusxr/axx/core"
)

// Pack returns the Kafka pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

// config is the pack's section of axx.yaml (packs.kafka).
type config struct {
	// Timeout is how long a consumer assertion waits for a matching record.
	Timeout time.Duration
	// MaxRecords bounds the records kept in memory per topic.
	MaxRecords int
	// LenientUnions accepts bare union values in Avro JSON payloads.
	LenientUnions bool
}

const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "timeout": {"type": "string", "description": "How long a consumer assertion waits for a matching record, e.g. 30s (the default)."},
    "maxRecords": {"type": "integer", "minimum": 1, "description": "Records kept in memory per topic (default 100000); older records are dropped and can no longer match."},
    "lenientUnions": {"type": "boolean", "description": "Accept Avro union values written without their {\"<branch>\": value} wrapper when exactly one branch fits (default false: Avro's JSON encoding needs the wrapper)."}
  }
}`

func configOf(s *core.Suite) (config, error) {
	return core.Cached(s, "kafka.config", func() (config, error) {
		var raw struct {
			Timeout       string `json:"timeout"`
			MaxRecords    int    `json:"maxRecords"`
			LenientUnions bool   `json:"lenientUnions"`
		}
		if err := s.PackConfig("kafka", &raw); err != nil {
			return config{}, err
		}
		c := config{Timeout: 30 * time.Second, MaxRecords: 100000, LenientUnions: raw.LenientUnions}
		if raw.Timeout != "" {
			d, err := time.ParseDuration(raw.Timeout)
			if err != nil || d <= 0 {
				return config{}, fmt.Errorf("packs.kafka.timeout: invalid duration %q", raw.Timeout)
			}
			c.Timeout = d
		}
		if raw.MaxRecords > 0 {
			c.MaxRecords = raw.MaxRecords
		}
		return c, nil
	})
}

// Service is a Kafka cluster registered in a scenario.
type Service struct {
	Name    string
	Brokers string

	mu      sync.Mutex
	clients []*TopicClient // in creation order: "the kafka event payload properties" use the first
}

func (s *Service) client(topic string) (*TopicClient, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		if c.Topic == topic {
			return c, true
		}
	}
	return nil, false
}

func (s *Service) topicClients() []*TopicClient {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*TopicClient(nil), s.clients...)
}

// TopicClient is a topic's producer and consumer configuration in a
// scenario, with the events drafted for it and the labels asserted on it.
type TopicClient struct {
	Topic   string
	Service *Service

	producer *clientSpec
	consumer *clientSpec
	prod     *kgo.Client
	preg     *registry // the producer's Schema Registry, when it serializes Avro
	deser    *deserializers
	tailer   *tailer

	mu     sync.Mutex
	events []*Event
	labels []*label
}

// Event is a drafted Kafka event.
type Event struct {
	Key       *string
	Headers   []header // unique keys, in the order they were first set
	Payload   string
	Published []published
}

type published struct {
	Partition int32  `json:"partition"`
	Offset    int64  `json:"offset"`
	Timestamp string `json:"timestamp,omitempty"`
	SchemaID  int    `json:"schemaId,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Schema    string `json:"schema,omitempty"`
}

func (e *Event) setHeader(key string, value *string) {
	for i := range e.Headers {
		if e.Headers[i].Key == key {
			e.Headers[i].Value = value
			return
		}
	}
	e.Headers = append(e.Headers, header{Key: key, Value: value})
}

// label returns the label, creating it on first use.
func (tc *TopicClient) label(name string) *label {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	for _, l := range tc.labels {
		if l.Name == name {
			return l
		}
	}
	l := &label{Name: name}
	tc.labels = append(tc.labels, l)
	return l
}

type ScenarioContext struct {
	services *core.Services[*Service]

	mu   sync.Mutex
	last *checkReport
}

var stateKey = core.NewStateKey("kafka", func(sc *core.Scenario) *ScenarioContext {
	st := &ScenarioContext{services: core.NewServices[*Service]("Kafka service", "No kafka service set")}
	sc.Describe("kafka", st.describe)
	return st
}, nil)

// checkReport records a consumer assertion for failure reports.
type checkReport struct {
	Service  string       `json:"service"`
	Topic    string       `json:"topic"`
	Label    string       `json:"label"`
	Matchers []string     `json:"matchers"`
	Passed   bool         `json:"passed"`
	Checked  int          `json:"recordsChecked"`
	OnTopic  int64        `json:"recordsOnTopic"`
	Dropped  int64        `json:"recordsDropped,omitempty"`
	Waited   string       `json:"waited"`
	Recent   []recordInfo `json:"recentRecords,omitempty"`
	Consumer string       `json:"consumerError,omitempty"`
}

const describeRecords = 5

// describe reports the last consumer assertion (its matchers and the most
// recent records of the topic with the reason each one did not match) and
// the scenario's topic clients.
func (st *ScenarioContext) describe() any {
	out := map[string]any{}
	st.mu.Lock()
	if st.last != nil {
		out["lastAssertion"] = st.last
	}
	st.mu.Unlock()
	topics := map[string]any{}
	for _, svc := range st.services.All() {
		for _, tc := range svc.topicClients() {
			tc.mu.Lock()
			t := map[string]any{"service": svc.Name, "events": len(tc.events)}
			var pubs []published
			for _, e := range tc.events {
				pubs = append(pubs, e.Published...)
			}
			if len(pubs) > 0 {
				t["published"] = pubs
			}
			labels := map[string]any{}
			for _, l := range tc.labels {
				labels[l.Name] = map[string]any{"matchers": l.matchers(), "matched": l.Matched != nil}
			}
			if len(labels) > 0 {
				t["labels"] = labels
			}
			tc.mu.Unlock()
			topics[svc.Name+":"+tc.Topic] = t
		}
	}
	if len(topics) > 0 {
		out["topicClients"] = topics
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
