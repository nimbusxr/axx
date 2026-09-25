//go:build integration

package kafka

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	mobynet "github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"

	"github.com/nimbusxr/axx/core"
)

// startCluster runs a single-node KRaft broker and a Confluent Schema
// Registry on one Docker network, like examples/parcels/infra.
func startCluster(t *testing.T) (brokers, registry string) {
	t.Helper()
	ctx := context.Background()
	nw, err := network.New(ctx)
	if err != nil {
		t.Skipf("docker unavailable: %v", err)
	}
	t.Cleanup(func() { _ = nw.Remove(ctx) })

	// The broker advertises the host port to clients, so it is fixed
	// before the container starts.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	kafka, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          "apache/kafka-native:3.9.1",
			Networks:       []string{nw.Name},
			NetworkAliases: map[string][]string{nw.Name: {"kafka"}},
			ExposedPorts:   []string{"9092/tcp"},
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.PortBindings = mobynet.PortMap{mobynet.MustParsePort("9092/tcp"): {{HostPort: strconv.Itoa(port)}}}
			},
			Env: map[string]string{
				"KAFKA_NODE_ID":                                  "1",
				"KAFKA_PROCESS_ROLES":                            "broker,controller",
				"KAFKA_CONTROLLER_QUORUM_VOTERS":                 "1@kafka:9093",
				"KAFKA_LISTENERS":                                "PLAINTEXT://:9092,CONTROLLER://:9093,INTERNAL://:9094",
				"KAFKA_ADVERTISED_LISTENERS":                     fmt.Sprintf("PLAINTEXT://127.0.0.1:%d,INTERNAL://kafka:9094", port),
				"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":           "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT,INTERNAL:PLAINTEXT",
				"KAFKA_INTER_BROKER_LISTENER_NAME":               "INTERNAL",
				"KAFKA_CONTROLLER_LISTENER_NAMES":                "CONTROLLER",
				"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
				"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
				"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
				"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":         "0",
				"KAFKA_AUTO_CREATE_TOPICS_ENABLE":                "true",
				"CLUSTER_ID":                                     "MkU3OEVBNTcwNTJENDM2Qk",
			},
			WaitingFor: wait.ForLog("Kafka Server started").WithStartupTimeout(2 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("kafka container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = kafka.Terminate(ctx) })

	reg, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "confluentinc/cp-schema-registry:7.9.2-1-ubi8",
			Networks:     []string{nw.Name},
			ExposedPorts: []string{"8081/tcp"},
			Env: map[string]string{
				"SCHEMA_REGISTRY_KAFKASTORE_BOOTSTRAP_SERVERS": "PLAINTEXT://kafka:9094",
				"SCHEMA_REGISTRY_HOST_NAME":                    "schema-registry",
				"SCHEMA_REGISTRY_LISTENERS":                    "http://0.0.0.0:8081",
			},
			WaitingFor: wait.ForHTTP("/subjects").WithPort("8081/tcp").WithStartupTimeout(3 * time.Minute),
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("schema registry container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = reg.Terminate(ctx) })
	host, _ := reg.Host(ctx)
	rp, _ := reg.MappedPort(ctx, "8081/tcp")
	return fmt.Sprintf("127.0.0.1:%d", port), fmt.Sprintf("http://%s:%s", host, rp.Port())
}

const missionSchema = `{
  "type": "record", "name": "MissionEvent", "namespace": "space.events",
  "fields": [
    {"name": "event_type", "type": "string"},
    {"name": "mission_id", "type": "string"},
    {"name": "mission_name", "type": "string"},
    {"name": "destination", "type": "string"},
    {"name": "timestamp", "type": "string"},
    {"name": "crew", "type": {"type": "array", "items": "string"}, "default": []},
    {"name": "metadata", "type": ["null", "string"], "default": null}
  ]
}`

const missionPayload = `{"event_type": "mission_created", "mission_id": "m-template", "mission_name": "Alpha",
  "destination": "Mars", "timestamp": "2025-06-15T10:00:00Z", "crew": ["ann", "bo"], "metadata": {"string": "{\"priority\": \"high\"}"}}`

func TestAgainstRealKafka(t *testing.T) {
	brokers, registryURL := startCluster(t)
	h := newHarness(t, `{"timeout": "30s"}`)
	h.file("schemas/mission-event.avsc", missionSchema)
	h.file("kafka/mission-created.json", missionPayload)

	h.must("the space-kafka kafka service with the following properties:", []string{"brokers", brokers})
	h.must("a mission-events kafka topic client with the following properties:",
		[]string{"producer.value.serializer", avroSerializer},
		[]string{"producer.schema.registry.url", registryURL},
		[]string{"consumer.value.deserializer", avroDeserializer},
		[]string{"consumer.schema.registry.url", registryURL},
		[]string{"consumer.auto.offset.reset", "earliest"})

	// Avro: a draft built from a file, edited, and published with a schema.
	h.must("a mission-events kafka event")
	h.must("the mission-events kafka event key is m-1")
	h.must("the mission-events kafka event payload is a kafka/mission-created.json resource")
	h.must("the kafka event payload properties are:", []string{"$.mission_id", "m-1"}, []string{"$.mission_name", "Kafka Mission"})
	h.must("the mission-events kafka event headers are:", []string{"X-Event-Source", "acceptance-test"}, []string{"X-Correlation-Id", "c-123"})
	h.must("the mission-events kafka event is published using schema schemas/mission-event.avsc")

	h.must("a 2nd ordered mission-events kafka event")
	h.must("the 2nd ordered mission-events kafka event key is m-2")
	h.must("the 2nd ordered mission-events kafka event payload is a kafka/mission-created.json resource")
	h.must("the 2nd ordered mission-events kafka event payload properties are:", []string{"$.mission_id", "m-2"})
	h.must("the 2nd ordered mission-events kafka event payload property $.metadata is null")
	h.must("the 2nd ordered mission-events kafka event is published using schema schemas/mission-event.avsc on the space-kafka kafka service")

	h.must("the mission-events kafka event named first key is m-1")
	h.must("the mission-events kafka event named first payload properties are:",
		[]string{"$.mission_id", "m-1"}, []string{"$.mission_name", "Kafka Mission"}, []string{"$.crew[1]", "bo"},
		[]string{"$.metadata", `"{"priority": "high"}"`})
	h.must("the mission-events kafka event named first headers are:", []string{"X-Event-Source", "acceptance-test"})
	h.must("the mission-events kafka event named first headers match:", []string{"X-Event-Source", "^acceptance-.*$"})
	h.must("the mission-events kafka event named second payload properties on the space-kafka kafka service are:",
		[]string{"$.mission_id", "m-2"}, []string{"$.metadata", "null"})

	svc, err := Context(h.sc).Service()
	if err != nil {
		t.Fatal(err)
	}
	tc, ok := svc.Topic("mission-events")
	if !ok || len(tc.Events()) == 0 || len(tc.Events()[0].Published) == 0 {
		t.Fatalf("topic client: %v", ok)
	}
	first := tc.Events()[0].Published[0]
	if first.SchemaID == 0 {
		t.Errorf("published record: %+v", first)
	}
	cl, _ := sr.NewClient(sr.URLs(registryURL))
	if got, err := cl.SchemaByVersion(context.Background(), "mission-events-value", -1); err != nil || got.ID != first.SchemaID {
		t.Errorf("subject mission-events-value: %+v %v (record schema %d)", got, err, first.SchemaID)
	}

	// Plain text events, and a record that arrives while the assertion waits.
	h.must("the plain-events kafka topic client")
	h.must("a plain-events kafka event")
	h.must("the plain-events kafka event key is p-1")
	h.file("plain.json", `{"kind": "plain", "n": 1}`)
	h.must("the plain-events kafka event payload is a plain.json resource")
	h.must("the plain-events kafka event is published")
	h.must("the plain-events kafka event named plain payload properties are:", []string{"$.kind", "plain"}, []string{"$.n", "1"})

	late := make(chan error, 1)
	go func() {
		time.Sleep(2 * time.Second)
		pc, err := kgo.NewClient(kgo.SeedBrokers(brokers))
		if err != nil {
			late <- err
			return
		}
		defer pc.Close()
		late <- pc.ProduceSync(context.Background(), &kgo.Record{Topic: "plain-events", Key: []byte("late"), Value: []byte(`{"kind":"late"}`)}).FirstErr()
	}()
	start := time.Now()
	h.must("the plain-events kafka event named late key is late")
	if err := <-late; err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < time.Second {
		t.Error("the late record was found before it was produced")
	}

	// auto.offset.reset=latest only considers records produced after the
	// assertion starts: the earlier records are skipped, not checked.
	h.must("the other-kafka kafka service with the following properties:", []string{"brokers", brokers})
	h.must("a plain-events kafka topic client on the other-kafka kafka service with the following properties:",
		[]string{"consumer.auto.offset.reset", "latest"})
	h.fails("a plain-events kafka topic client on the other-kafka kafka service with the following properties:", "already exists",
		[]string{"consumer.auto.offset.reset", "latest"})
	go func() {
		time.Sleep(2 * time.Second)
		pc, err := kgo.NewClient(kgo.SeedBrokers(brokers))
		if err != nil {
			late <- err
			return
		}
		defer pc.Close()
		late <- pc.ProduceSync(context.Background(), &kgo.Record{Topic: "plain-events", Key: []byte("newest"), Value: []byte(`{}`)}).FirstErr()
	}()
	h.must("the plain-events kafka event named newest key is newest on the other-kafka kafka service")
	if err := <-late; err != nil {
		t.Fatal(err)
	}
	if got := stateKey.Of(h.sc).last.Checked; got != 1 {
		t.Errorf("with latest only the new record is checked, got %d", got)
	}

	// Avro keys, and a schema that is not registered.
	h.must("an avro-keys kafka topic client with the following properties:",
		[]string{"producer.key.serializer", avroSerializer},
		[]string{"producer.value.serializer", avroSerializer},
		[]string{"producer.schema.registry.url", registryURL},
		[]string{"consumer.key.deserializer", avroDeserializer},
		[]string{"consumer.value.deserializer", avroDeserializer},
		[]string{"consumer.schema.registry.url", registryURL})
	h.must("an avro-keys kafka event")
	h.must("the avro-keys kafka event key is k-avro")
	h.must("the avro-keys kafka event payload is a kafka/mission-created.json resource")
	h.must("the avro-keys kafka event is published using schema schemas/mission-event.avsc")
	h.must("the avro-keys kafka event named k key is k-avro")
	if _, err := cl.SchemaByVersion(context.Background(), "avro-keys-key", -1); err != nil {
		t.Errorf("the Avro key schema must be registered under avro-keys-key: %v", err)
	}
	h.must("a strict kafka topic client with the following properties:",
		[]string{"producer.value.serializer", avroSerializer},
		[]string{"producer.schema.registry.url", registryURL},
		[]string{"producer.auto.register.schemas", "false"})
	h.must("a strict kafka event")
	h.must("the strict kafka event payload is a kafka/mission-created.json resource")
	h.fails("the strict kafka event is published using schema schemas/mission-event.avsc", "auto.register.schemas is false")

	// A later scenario of the run reads the topic from the start again
	// through the shared tailer.
	sc2 := core.NewScenario(context.Background(), core.ScenarioInfo{Name: "second"}, h.suite, h.sink)
	h.sc = sc2
	h.must("the space-kafka kafka service with the following properties:", []string{"brokers", brokers})
	h.must("a mission-events kafka topic client with the following properties:",
		[]string{"consumer.value.deserializer", avroDeserializer},
		[]string{"consumer.schema.registry.url", registryURL})
	h.must("the mission-events kafka event named again payload properties are:", []string{"$.mission_id", "m-1"})
}
