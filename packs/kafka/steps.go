package kafka

import (
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/contract"
)

const serviceSuffix = "[[ on the {word} kafka service]]"

const ordinalDoc = "With `the {ordinal} ordered` the step works on that event of the topic (`1st` is the first event created); without it, on the first event."

// consumeDoc is how the assertion steps work, as the items of a Doc's list.
const consumeDoc = "- The expectation joins those of the label (`named {word}`) in the scenario: **one record** of the topic must meet " +
	"**every** one of them, key, payload properties and headers together.\n" +
	"- Records are read from the start of the topic, or with `consumer.auto.offset.reset=latest`, only those produced after the step starts.\n" +
	"- The step fails after 30 seconds (`packs.kafka.timeout`) without a match; the failure lists the label's expectations " +
	"and the latest records, each with the reason it did not match."

// setPropertiesTable is the table of the steps that set payload properties
// of a drafted event.
var setPropertiesTable = &core.TableDoc{
	Columns: []string{"JSONPath", "value"},
	Note:    "A row sets the property at the JSONPath to the value, always as a string; an empty cell sets JSON null.",
}

// headersMatchTable and headersMatchDoc are those of the steps that match
// headers with regular expressions.
var headersMatchTable = &core.TableDoc{
	Columns: []string{"header", "pattern"},
	Note:    "A row expects a header of the record to match a regular expression (Java syntax), over its whole value; an empty cell fails the step.",
}

const headersMatchDoc = "- A header must have one distinct value: unlike the other steps, this one ignores repeated identical values of a header.\n"

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
		"**Client properties.** A topic client's table takes Java Kafka client properties, prefixed `producer.` or `consumer.`: " +
		"`a(n) {word} kafka topic client with the following properties:` lists those axx applies. Without them, a client writes " +
		"and reads keys and payloads as text, reads every record of the topic, and uses no consumer group.\n\n" + otherPropsDoc()
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "kafka.service", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the {word} kafka service with the following properties:",
			Doc:  "Register a Kafka cluster. The first one registered in a scenario is the default for steps without `on the {word} kafka service`.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "brokers", Required: true, Takes: "the cluster's brokers, a comma-separated list of `host:port`; `${env:..}` and `${sys:..}` are expanded"},
					contract.TableRow("topics"),
				},
				Note: "Any other property is ignored, with a warning.",
			},
			Examples: []string{"Given the events kafka service with the following properties:\n  | brokers | localhost:9092 |"},
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
			Doc: "Create a topic client configured with Java Kafka client properties: `producer.` rows for publishing, `consumer.` rows for assertions.\n\n" +
				"- For Avro, set the Confluent Avro serializer or deserializer and the `schema.registry.url`, as in the examples.\n" +
				"- A topic can have one client per service in a scenario.",
			Table: clientTable(),
			Examples: []string{
				"Given a depot-scans kafka topic client with the following properties:\n" +
					"  | producer.value.serializer    | io.confluent.kafka.serializers.KafkaAvroSerializer |\n" +
					"  | producer.schema.registry.url | http://localhost:9081                              |",
				"Given a parcel-events kafka topic client on the events kafka service with the following properties:\n" +
					"  | consumer.value.deserializer  | io.confluent.kafka.serializers.KafkaAvroDeserializer |\n" +
					"  | consumer.schema.registry.url | http://localhost:9081                                |",
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
			Doc: "Draft a new event of the topic: the steps that follow set its key, headers and payload, which starts as `{}`.\n\n" +
				"- Without an ordinal, the event is appended.\n" +
				"- With one, it must be the next position: `a 2nd ordered` after one event.\n" +
				"- An ordinal equal to the number of events the topic has still appends, with a warning.",
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
				"Given the depot-scans kafka event key is PX-4101",
				"Given the 2nd ordered depot-scans kafka event key is PX-4102 on the events kafka service",
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
			Doc:  "Set headers of a drafted event. " + ordinalDoc,
			Table: &core.TableDoc{
				Columns: []string{"header", "value"},
				Note:    "A row sets a header of the event: setting a header again replaces its value, and an empty cell sends the text `null`.",
			},
			Examples: []string{
				"Given the depot-scans kafka event headers are:\n" +
					"  | X-Event-Type | ParcelScanned |\n" +
					"  | X-Depot      | Leipzig       |",
				"Given the 1st ordered depot-scans kafka event headers on the events kafka service are:\n" +
					"  | X-Event-Type | ParcelScanned |",
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
			Doc: "Set the payload of a drafted event to the contents of a file.\n\n" +
				"- For Avro events, the file is Avro's JSON encoding of the record, with unions as `{\"<branch>\": value}`.\n" +
				"- " + ordinalDoc,
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
			Doc: "Set JSONPath properties of an event of the service's **first** topic client, the first one created in the scenario.\n\n" +
				"- Every property must already exist in the payload.\n" +
				"- " + ordinalDoc,
			Table: setPropertiesTable,
			Examples: []string{
				"Given the kafka event payload properties are:\n" +
					"  | $.scanId    | SC-4101-1 |\n" +
					"  | $.parcelRef | PX-4101   |",
				"Given the 2nd ordered kafka event payload properties on the events kafka service are:\n" +
					"  | $.scanId    | SC-4101-2 |\n" +
					"  | $.parcelRef | PX-4101   |",
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
			Expr:  "the {word} kafka event payload properties" + serviceSuffix + " are:",
			Doc:   "Set JSONPath properties of the topic's first event. Every property must already exist in the payload: set it in the payload file first.",
			Table: setPropertiesTable,
			Examples: []string{
				"Given the depot-scans kafka event payload properties are:\n" +
					"  | $.scanId    | SC-4101-1 |\n" +
					"  | $.parcelRef | PX-4101   |",
				"Given the depot-scans kafka event payload properties on the events kafka service are:\n" +
					"  | $.location | Leipzig |",
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
			Doc: "Set JSONPath properties of that event of the topic (`1st` is the first event created). Every property must " +
				"already exist in the payload: set it in the payload file first.",
			Table: setPropertiesTable,
			Examples: []string{
				"Given the 2nd ordered depot-scans kafka event payload properties are:\n" +
					"  | $.scanId    | SC-4101-2 |\n" +
					"  | $.parcelRef | PX-4101   |",
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
			Doc: "Publish a drafted event as Confluent Avro, with an `.avsc` schema file.\n\n" +
				"- The payload, Avro's JSON encoding of the record, is read with the schema; a payload that does not fit it fails the step with the JSONPath of the mismatch.\n" +
				"- The schema is registered, or looked up, in the Schema Registry under the subject of `value.subject.name.strategy`, `<topic>-value` by default.\n" +
				"- The record is written as magic byte 0, the schema ID and the Avro binary.\n" +
				"- The topic client needs `producer.value.serializer=io.confluent.kafka.serializers.KafkaAvroSerializer` and `producer.schema.registry.url`.\n" +
				"- " + ordinalDoc,
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
			Doc: "Publish a drafted event as it is: its key, its headers and its payload's text.\n\n" +
				"- The producer's value serializer writes the payload: `StringSerializer` by default, or `ByteArraySerializer`.\n" +
				"- For Avro, use `is published using schema`: with `KafkaAvroSerializer`, this step fails.\n" +
				"- " + ordinalDoc,
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
			Doc: "Expect the label's record to have this key.\n\n" +
				"- The consumer's key deserializer decides how keys read.\n" + consumeDoc,
			Examples: []string{
				"Then the parcel-events kafka event named registered key is PX-4101",
				"Then the parcel-events kafka event named registered key is PX-4101 on the events kafka service",
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
			Doc: "Expect JSONPath properties of the label's record payload.\n\n" +
				"- Values are typed: `\"text\"` is a string, `null` JSON null, `12` an integer, `1.5` a decimal, `true` and `false` booleans, " +
				"`{...}` and `[...]` JSON; anything else is a string.\n" +
				"- Numbers must match in type: `2` does not equal `2.0`.\n" +
				"- An empty cell fails the step: write `null` for JSON null, `\"\"` for an empty string.\n" + consumeDoc,
			Table: &core.TableDoc{
				Columns: []string{"JSONPath", "value"},
				Note:    "A row expects the property at the JSONPath to have the value.",
			},
			Examples: []string{
				"Then the parcel-events kafka event named registered payload properties are:\n" +
					"  | $.reference    | PX-4101  |\n" +
					"  | $.weightGrams  | 1200     |\n" +
					"  | $.serviceLevel | STANDARD |",
				"Then the parcel-events kafka event named registered payload properties on the events kafka service are:\n" +
					"  | $.reference | PX-4101 |\n" +
					"  | $.source    | api     |",
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
			Doc: "Expect headers of the label's record.\n\n" +
				"- Each header must occur exactly once, with exactly this value.\n" + consumeDoc,
			Table: &core.TableDoc{
				Columns: []string{"header", "value"},
				Note:    "A row expects a header of the record to have the value; an empty cell fails the step.",
			},
			Examples: []string{
				"Then the parcel-events kafka event named registered headers are:\n" +
					"  | X-Event-Type | ParcelRegistered |",
				"Then the parcel-events kafka event named registered headers on the events kafka service are:\n" +
					"  | X-Event-Type | ParcelRegistered |",
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
			Expr:  "the {word} kafka event named {word} headers match:",
			Doc:   "Expect headers of the label's record to match regular expressions.\n\n" + headersMatchDoc + consumeDoc,
			Table: headersMatchTable,
			Examples: []string{"Then the parcel-events kafka event named registered headers match:\n" +
				"  | X-Event-Type | Parcel[A-Za-z]+ |"},
			Run: headersMatch(-1),
		},
		{
			ID: "kafka.consumed.headers.match.service", Keyword: "Then", Arg: core.ArgTable, Since: "0.1.0",
			Expr:  "the {word} kafka event named {word} headers on the {word} kafka service match:",
			Doc:   "Expect headers of the label's record, on a topic client of that Kafka service, to match regular expressions.\n\n" + headersMatchDoc + consumeDoc,
			Table: headersMatchTable,
			Examples: []string{"Then the parcel-events kafka event named registered headers on the events kafka service match:\n" +
				"  | X-Event-Type | Parcel[A-Za-z]+ |"},
			Run: headersMatch(2),
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
	brokers, asyncapi := "", ""
	for _, p := range pairs {
		switch {
		case p.Key == "brokers" && !p.Null:
			brokers = sc.Suite().Interpolate(p.Value)
		case p.Key == contract.Row && !p.Null:
			asyncapi = strings.TrimSpace(sc.Suite().Interpolate(p.Value))
		case p.Key != "brokers" && p.Key != contract.Row:
			sc.Log("warning: kafka service property %q is ignored (only brokers and asyncapi are used)", p.Key)
		}
	}
	if strings.TrimSpace(brokers) == "" {
		return fmt.Errorf("Property %q is required", "brokers") //nolint:staticcheck // user-facing message
	}
	svc := &Service{Name: a.String(0), Brokers: brokers}
	if asyncapi != "" {
		if svc.contract, err = contract.Open(sc, asyncapi); err != nil {
			return err
		}
	}
	return Context(sc).AddService(svc)
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
