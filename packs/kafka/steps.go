package kafka

import (
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
)

const serviceSuffix = "[[ on the {word} kafka service]]"

const ordinalDoc = "With `the {ordinal} ordered` the step works on that event of the topic (`1st` is the first event created); without it, on the first event."

const consumeDoc = "Adds the expectation to the label (`named {word}`) and then waits until **one record** of the topic satisfies " +
	"**every** expectation added to that label in the scenario (key, payload properties and headers together). " +
	"Records are read from the start of the topic (or, with `consumer.auto.offset.reset=latest`, only those produced after the step starts); " +
	"the step fails after 30 seconds (`packs.kafka.timeout`) without a match, and the failure report lists the label's expectations " +
	"and the latest records with the reason each one did not match."

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:         "kafka",
		Namespace:    "kafka",
		Doc:          packDoc(),
		ConfigSchema: []byte(configSchema),
		Steps:        steps(),
	}
}

func packDoc() string {
	return "Publish events to Kafka topics (plain text or Confluent Avro through a Schema Registry) and assert on the records of a topic.\n\n" +
		"A **kafka service** names a cluster; a **topic client** gives one topic its producer and consumer configuration, written as " +
		"Java client properties (`producer.*`, `consumer.*`); **kafka events** are drafts (key, headers, payload) you build and then " +
		"publish; a **named kafka event** (`the parcel-events kafka event named registered ...`) is an expectation that one record of the topic must meet.\n\n" +
		"Consumer assertions scan every record of the topic from its first offset, re-checking for up to 30 seconds as records arrive. " +
		"Avro values are compared through Apache Avro's text form of them (GenericData.toString), so JSONPath expectations such as " +
		"`$.reference` work on Avro records as on JSON; union values appear without their wrapper.\n\n" +
		"**Configuration** (`packs.kafka` in axx.yaml): `timeout` (default `30s`), `maxRecords` kept per topic (default 100000), " +
		"`lenientUnions` (accept Avro union values without their `{\"<branch>\": value}` wrapper when exactly one branch fits).\n\n" +
		"**Client properties.** Rows prefixed `producer.` or `consumer.` configure that client (without the prefix); values are expanded " +
		"(`${env:..}`, `${sys:..}`). Defaults: `StringSerializer`/`StringDeserializer`, `auto.offset.reset=earliest`, no consumer group. " +
		"How each Java property is applied:\n\n" + propDoc()
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "kafka.service", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the {word} kafka service with the following properties:",
			Doc: "Register a Kafka cluster. The first one registered in a scenario is the default for steps without `on the {word} kafka service`.\n\n" +
				"Properties: `brokers` (required; `host:port` list, `${env:..}`/`${sys:..}` expanded).",
			Examples: []string{"Given the events kafka service with the following properties:"},
			Run:      addService,
		},
		{
			ID: "kafka.client", Keyword: "Given",
			Expr: "the {word} kafka topic client",
			Doc: "Create a topic client on the default Kafka service with the default configuration: string keys and values, " +
				"records read from the start of the topic. A topic can have one client per service in a scenario.",
			Examples: []string{"Given the parcel-events kafka topic client"},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, err := service(sc, a, -1)
				if err != nil {
					return err
				}
				return addClient(sc, svc, a.String(0), nil)
			},
		},
		{
			ID: "kafka.client.props", Keyword: "Given", Arg: core.ArgTable,
			Expr: "a(n) {word} kafka topic client" + serviceSuffix + " with the following properties:",
			Doc: "Create a topic client configured with Java Kafka client properties. Rows prefixed `producer.` configure publishing, " +
				"rows prefixed `consumer.` configure assertions (the prefix is removed); values are expanded. For Avro use " +
				"`producer.value.serializer=io.confluent.kafka.serializers.KafkaAvroSerializer`, " +
				"`consumer.value.deserializer=io.confluent.kafka.serializers.KafkaAvroDeserializer` and `*.schema.registry.url`. " +
				"See the pack documentation for every supported property; unknown properties are logged as warnings.",
			Examples: []string{
				"Given a depot-scans kafka topic client with the following properties:",
				"Given a parcel-events kafka topic client on the events kafka service with the following properties:",
			},
			TableTypes: clientTableTypes(),
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, err := service(sc, a, 1)
				if err != nil {
					return err
				}
				return addClient(sc, svc, a.String(0), a.Table)
			},
		},
		{
			ID: "kafka.event", Keyword: "Given",
			Expr: "a(n)[[ {ordinal} ordered]] {word} kafka event[[ on {word} kafka service]]",
			Doc: "Draft a new event (key, headers and payload are set by the following steps; the payload starts as `{}`). " +
				"Without an ordinal the event is appended. With one, it must be the next position (`a 2nd ordered` after one event); " +
				"an ordinal equal to the number of existing events still appends, with a warning (it will be an error in axx 1.0).",
			Examples: []string{
				"Given a depot-scans kafka event",
				"Given a 2nd ordered depot-scans kafka event",
				"Given a depot-scans kafka event on events kafka service",
			},
			Run: createEvent,
		},
		{
			ID: "kafka.event.key", Keyword: "Given",
			Expr: "the[[ {ordinal} ordered]] {word} kafka event key is {word}" + serviceSuffix,
			Doc:  "Set the key of a drafted event. " + ordinalDoc,
			Examples: []string{
				"Given the depot-scans kafka event key is PX-1001",
				"Given the 2nd ordered depot-scans kafka event key is PX-1002 on the events kafka service",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				ev, err := event(sc, a, 0, 1, 3)
				if err != nil {
					return err
				}
				k := a.String(2)
				ev.Key = &k
				return nil
			},
		},
		{
			ID: "kafka.event.headers", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the[[ {ordinal} ordered]] {word} kafka event headers" + serviceSuffix + " are:",
			Doc:  "Set headers of a drafted event (`name | value` rows; setting a header again replaces its value; an empty cell sends the text `null`). " + ordinalDoc,
			Examples: []string{
				"Given the depot-scans kafka event headers are:",
				"Given the 1st ordered depot-scans kafka event headers on the events kafka service are:",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				ev, err := event(sc, a, 0, 1, 2)
				if err != nil {
					return err
				}
				pairs, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				for _, p := range pairs {
					v := p.Value
					if p.Null {
						v = "null" // String.valueOf(null)
					}
					ev.setHeader(p.Key, &v)
				}
				return nil
			},
		},
		{
			ID: "kafka.event.payload.resource", Keyword: "Given",
			Expr: "the[[ {ordinal} ordered]] {word} kafka event payload is a(n) {filepath} resource" + serviceSuffix,
			Doc: "Set the payload of a drafted event to the contents of a file (resolved against `resources`). For Avro events the " +
				"file is Avro's JSON encoding of the record (unions as `{\"<branch>\": value}`). " + ordinalDoc,
			Examples: []string{
				"Given the depot-scans kafka event payload is a kafka/scan-delivered.json resource",
				"Given the 3rd ordered depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource on the events kafka service",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				ev, err := event(sc, a, 0, 1, 3)
				if err != nil {
					return err
				}
				return loadPayload(sc, ev, a.String(2))
			},
		},
		{
			ID: "kafka.event.properties.first", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the[[ {ordinal} ordered]] kafka event payload properties" + serviceSuffix + " are:",
			Doc: "Set JSONPath properties (`path | value` rows) of an event of the service's **first** topic client (the first one created in the scenario). " +
				"Values are always set as strings, an empty cell sets JSON null, and every property must already exist in the payload. " + ordinalDoc,
			Examples: []string{
				"Given the kafka event payload properties are:",
				"Given the 2nd ordered kafka event payload properties on the events kafka service are:",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				svc, err := service(sc, a, 1)
				if err != nil {
					return err
				}
				clients := svc.topicClients()
				if len(clients) == 0 {
					return fmt.Errorf("Kafka topic client not found") //nolint:staticcheck // user-facing message
				}
				ev, err := clients[0].event(a, 0)
				if err != nil {
					return err
				}
				return setProperties(ev, a.Table)
			},
		},
		{
			ID: "kafka.event.properties", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the {word} kafka event payload properties" + serviceSuffix + " are:",
			Doc: "Set JSONPath properties (`path | value` rows) of the topic's first event. Values are always set as strings, " +
				"an empty cell sets JSON null, and every property must already exist in the payload (set it in the payload file first).",
			Examples: []string{
				"Given the depot-scans kafka event payload properties are:",
				"Given the depot-scans kafka event payload properties on the events kafka service are:",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				ev, err := event(sc, a, -1, 0, 1)
				if err != nil {
					return err
				}
				return setProperties(ev, a.Table)
			},
		},
		{
			ID: "kafka.event.properties.ordinal", Keyword: "Given", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "the {ordinal} ordered {word} kafka event payload properties" + serviceSuffix + " are:",
			Doc:  "Like the topic form, for the given event of the topic (`1st` is the first event created).",
			Examples: []string{
				"Given the 2nd ordered depot-scans kafka event payload properties are:",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				ev, err := event(sc, a, 0, 1, 2)
				if err != nil {
					return err
				}
				return setProperties(ev, a.Table)
			},
		},
		{
			ID: "kafka.event.property.null", Keyword: "Given",
			Expr: "the[[ {ordinal} ordered]] {word} kafka event payload property {word} is null" + serviceSuffix,
			Doc:  "Set a JSONPath property of a drafted event's payload to JSON null; the property must exist. " + ordinalDoc,
			Examples: []string{
				"Given the depot-scans kafka event payload property $.location is null",
				"Given the 2nd ordered depot-scans kafka event payload property $.location is null on the events kafka service",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				ev, err := event(sc, a, 0, 1, 3)
				if err != nil {
					return err
				}
				return setProperty(ev, a.String(2), nil)
			},
		},
		{
			ID: "kafka.event.publish.schema", Keyword: "When",
			Expr: "the[[ {ordinal} ordered]] {word} kafka event is published using schema {filepath}" + serviceSuffix,
			Doc: "Publish a drafted event as Confluent Avro: the payload (Avro's JSON encoding) is read with the `.avsc` schema file, " +
				"the schema is registered (or looked up) in the Schema Registry under the subject of `value.subject.name.strategy` " +
				"(`<topic>-value` by default), and the record is written as magic byte 0, the schema ID and the Avro binary. " +
				"Needs `producer.value.serializer=io.confluent.kafka.serializers.KafkaAvroSerializer` and `producer.schema.registry.url`. " +
				"A payload that does not fit the schema fails with the JSONPath of the mismatch. " + ordinalDoc,
			Examples: []string{
				"When the depot-scans kafka event is published using schema schemas/depot-scan.avsc",
				"When the 2nd ordered depot-scans kafka event is published using schema schemas/depot-scan.avsc on the events kafka service",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				tc, ev, err := eventAndClient(sc, a, 0, 1, 3)
				if err != nil {
					return err
				}
				return tc.publishAvro(sc, ev, a.String(2))
			},
		},
		{
			ID: "kafka.event.publish", Keyword: "When", Since: "0.1.0",
			Expr: "the[[ {ordinal} ordered]] {word} kafka event is published" + serviceSuffix,
			Doc: "Publish a drafted event as it is: the payload text with the producer's value serializer (`StringSerializer` by default, " +
				"or `ByteArraySerializer`), with its key and headers. Use `published using schema` for Avro. " + ordinalDoc,
			Examples: []string{
				"When the depot-scans kafka event is published",
				"When the 2nd ordered depot-scans kafka event is published on the events kafka service",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				tc, ev, err := eventAndClient(sc, a, 0, 1, 2)
				if err != nil {
					return err
				}
				return tc.publishPlain(sc, ev)
			},
		},
		{
			ID: "kafka.consumed.key", Keyword: "Then",
			Expr: "the {word} kafka event named {word} key is {word}" + serviceSuffix,
			Doc:  "Expect the label's record to have this key (the consumer's key deserializer decides how keys read). " + consumeDoc,
			Examples: []string{
				"Then the parcel-events kafka event named registered key is PX-1001",
				"Then the parcel-events kafka event named registered key is PX-1001 on the events kafka service",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				return consume(sc, a, 3, false, func(l *label) error {
					l.Keys = append(l.Keys, a.String(2))
					return nil
				})
			},
		},
		{
			ID: "kafka.consumed.properties", Keyword: "Then", Arg: core.ArgTable,
			Expr: "the {word} kafka event named {word} payload properties" + serviceSuffix + " are:",
			Doc: "Expect JSONPath properties of the label's record payload (`path | value` rows). Values are typed: " +
				"`\"text\"` is a string, `null` is JSON null, `12` an integer, `1.5` a decimal, `true`/`false` booleans, `{...}`/`[...]` JSON; " +
				"anything else is a string. Numbers must match in type (`2` does not equal `2.0`). " + consumeDoc,
			Examples: []string{
				"Then the parcel-events kafka event named registered payload properties are:",
				"Then the parcel-events kafka event named registered payload properties on the events kafka service are:",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				pairs, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				var ms []payloadMatcher
				for _, p := range pairs {
					if p.Null {
						return fmt.Errorf("empty value for %s: write null to expect JSON null or \"\" for an empty string", p.Key)
					}
					m, err := newPayloadMatcher(p.Key, p.Value)
					if err != nil {
						return err
					}
					ms = append(ms, m)
				}
				return consume(sc, a, 2, false, func(l *label) error {
					l.Payload = append(l.Payload, ms...)
					return nil
				})
			},
		},
		{
			ID: "kafka.consumed.headers", Keyword: "Then", Arg: core.ArgTable,
			Expr: "the {word} kafka event named {word} headers" + serviceSuffix + " are:",
			Doc:  "Expect headers of the label's record (`name | value` rows): each header must occur exactly once with exactly this value. " + consumeDoc,
			Examples: []string{
				"Then the parcel-events kafka event named registered headers are:",
				"Then the parcel-events kafka event named registered headers on the events kafka service are:",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				hs, err := headerExpectations(a.Table, false)
				if err != nil {
					return err
				}
				return consume(sc, a, 2, false, func(l *label) error {
					l.Headers = append(l.Headers, hs...)
					return nil
				})
			},
		},
		{
			ID: "kafka.consumed.headers.match", Keyword: "Then", Arg: core.ArgTable,
			Expr: "the {word} kafka event named {word} headers match:",
			Doc: "Expect headers of the label's record to match regular expressions (`name | pattern` rows, Java syntax, whole value). " +
				"A header must have one distinct value; this step (unlike the others) ignores repeated identical values of a header. " + consumeDoc,
			Examples: []string{"Then the parcel-events kafka event named registered headers match:"},
			Run:      headersMatch(-1),
		},
		{
			ID: "kafka.consumed.headers.match.service", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr:     "the {word} kafka event named {word} headers on the {word} kafka service match:",
			Doc:      "The `headers match` expectation for a topic client of a named Kafka service. " + consumeDoc,
			Examples: []string{"Then the parcel-events kafka event named registered headers on the events kafka service match:"},
			Run:      headersMatch(2),
		},
	}
}

func headersMatch(svcArg int) core.StepFunc {
	return func(sc *core.Scenario, a core.Args) error {
		hs, err := headerExpectations(a.Table, true)
		if err != nil {
			return err
		}
		return consume(sc, a, svcArg, true, func(l *label) error {
			l.Headers = append(l.Headers, hs...)
			return nil
		})
	}
}

func headerExpectations(t *core.Table, regex bool) ([]headerMatcher, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	out := make([]headerMatcher, 0, len(pairs))
	for _, p := range pairs {
		if p.Null {
			return nil, fmt.Errorf("empty value for header %s", p.Key)
		}
		if !regex {
			out = append(out, headerMatcher{Key: p.Key, Value: p.Value})
			continue
		}
		m, err := newHeaderRegex(p.Key, p.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// service returns the service named by argument i, or the default one (i < 0
// or the argument is absent).
func service(sc *core.Scenario, a core.Args, i int) (*Service, error) {
	st := stateKey.Of(sc)
	if i >= 0 && a.Present(i) {
		return st.services.Get(a.String(i))
	}
	return st.services.Default()
}

func topicClient(sc *core.Scenario, a core.Args, topicArg, svcArg int) (*TopicClient, error) {
	svc, err := service(sc, a, svcArg)
	if err != nil {
		return nil, err
	}
	topic := a.String(topicArg)
	tc, ok := svc.client(topic)
	if !ok {
		return nil, fmt.Errorf("Kafka topic client not found: %s", topic) //nolint:staticcheck // user-facing message
	}
	return tc, nil
}

func event(sc *core.Scenario, a core.Args, ordArg, topicArg, svcArg int) (*Event, error) {
	_, ev, err := eventAndClient(sc, a, ordArg, topicArg, svcArg)
	return ev, err
}

func eventAndClient(sc *core.Scenario, a core.Args, ordArg, topicArg, svcArg int) (*TopicClient, *Event, error) {
	tc, err := topicClient(sc, a, topicArg, svcArg)
	if err != nil {
		return nil, nil, err
	}
	ev, err := tc.event(a, ordArg)
	return tc, ev, err
}

// event returns the event an ordinal argument names, or the first event.
func (tc *TopicClient) event(a core.Args, ordArg int) (*Event, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if len(tc.events) == 0 {
		return nil, fmt.Errorf("Kafka event not found: %s (draft one with \"a %s kafka event\")", tc.Topic, tc.Topic) //nolint:staticcheck // user-facing message
	}
	if ordArg < 0 || !a.Present(ordArg) {
		return tc.events[0], nil
	}
	n := a.Int(ordArg)
	if n < 1 || n > len(tc.events) {
		return nil, fmt.Errorf("Kafka event not found: %d (the %s topic has %d kafka event(s))", n, tc.Topic, len(tc.events)) //nolint:staticcheck // user-facing message
	}
	return tc.events[n-1], nil
}

func addService(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	brokers := ""
	for _, p := range pairs {
		switch {
		case p.Key == "brokers" && !p.Null:
			brokers = sc.Suite().Interpolate(p.Value)
		case p.Key != "brokers":
			sc.Log("warning: kafka service property %q is ignored (only brokers is used)", p.Key)
		}
	}
	if strings.TrimSpace(brokers) == "" {
		return fmt.Errorf("Property %q is required", "brokers") //nolint:staticcheck // user-facing message
	}
	return Context(sc).AddService(&Service{Name: a.String(0), Brokers: brokers})
}

func createEvent(sc *core.Scenario, a core.Args) error {
	tc, err := topicClient(sc, a, 1, 2)
	if err != nil {
		return err
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if a.Present(0) {
		n, size := a.Int(0), len(tc.events)
		if n > size+1 {
			return fmt.Errorf("Ordinals must be sequential: %d (the %s topic has %d kafka event(s), so the next is %s)", n, tc.Topic, size, ordinalText(size+1)) //nolint:staticcheck // user-facing message
		}
		if size > n {
			return fmt.Errorf("Ordinal Kafka event already exists: %d", n) //nolint:staticcheck // user-facing message
		}
		if size == n && n > 0 {
			// The new event becomes number n+1, not n.
			sc.Log("warning: a %s ordered %s kafka event already exists, so this creates the %s; axx 1.0 will reject this (Ordinal Kafka event already exists)",
				ordinalText(n), tc.Topic, ordinalText(n+1))
		}
	}
	tc.events = append(tc.events, &Event{Payload: "{}"})
	return nil
}

func ordinalText(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return fmt.Sprintf("%d%s", n, suffix)
}
