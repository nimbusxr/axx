package kafka

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// Java client properties. A topic client takes Kafka producer and consumer
// properties (the `producer.` and `consumer.` rows of its table, with the
// prefix removed) and translates each one through propTable into franz-go
// options and axx behaviour. Unknown properties are reported as warnings,
// properties that name Java classes axx cannot run are errors.

// role is the client a property applies to.
type role uint8

const (
	roleProducer role = 1 << iota
	roleConsumer
	roleBoth = roleProducer | roleConsumer
)

func (r role) String() string {
	switch r {
	case roleProducer:
		return "producer"
	case roleConsumer:
		return "consumer"
	}
	return "producer, consumer"
}

// serde is a (de)serializer class axx knows how to run.
type serde uint8

const (
	serdeString serde = iota
	serdeBytes
	serdeAvro
)

func (s serde) String() string {
	switch s {
	case serdeBytes:
		return "ByteArray"
	case serdeAvro:
		return "KafkaAvro"
	}
	return "String"
}

// Serializer and deserializer class names.
const (
	stringSerializer     = "org.apache.kafka.common.serialization.StringSerializer"
	stringDeserializer   = "org.apache.kafka.common.serialization.StringDeserializer"
	bytesSerializer      = "org.apache.kafka.common.serialization.ByteArraySerializer"
	bytesDeserializer    = "org.apache.kafka.common.serialization.ByteArrayDeserializer"
	avroSerializer       = "io.confluent.kafka.serializers.KafkaAvroSerializer"
	avroDeserializer     = "io.confluent.kafka.serializers.KafkaAvroDeserializer"
	topicNameStrategy    = "io.confluent.kafka.serializers.subject.TopicNameStrategy"
	recordNameStrategy   = "io.confluent.kafka.serializers.subject.RecordNameStrategy"
	topicRecordStrategy  = "io.confluent.kafka.serializers.subject.TopicRecordNameStrategy"
	jmxReporter          = "org.apache.kafka.common.metrics.JmxReporter"
	defaultPartitioner   = "org.apache.kafka.clients.producer.internals.DefaultPartitioner"
	roundRobinPartitoner = "org.apache.kafka.clients.producer.RoundRobinPartitioner"
	uniformStickyPart    = "org.apache.kafka.clients.producer.UniformStickyPartitioner"
)

// clientSpec is a translated producer or consumer configuration.
type clientSpec struct {
	Role    role
	Brokers []string
	// Connection.
	ClientID        string
	Protocol        string // PLAINTEXT, SSL, SASL_PLAINTEXT, SASL_SSL
	SASLMechanism   string
	JAAS            string
	TLS             tlsSpec
	MetadataMaxAge  time.Duration
	ConnIdle        time.Duration
	DialTimeout     time.Duration
	RequestTimeout  time.Duration
	RetryBackoff    time.Duration
	AllowAutoCreate bool
	// Producer.
	Acks            string // all, 0, 1
	Idempotent      *bool
	Compression     string
	Linger          time.Duration
	MaxRequestSize  int32
	BufferMemory    int
	DeliveryTimeout time.Duration
	Retries         *int
	MaxInFlight     int
	Partitioner     string // "", roundrobin, sticky
	// Consumer.
	OffsetReset    string // earliest, latest
	ReadCommitted  bool
	FetchMaxWait   time.Duration
	FetchMinBytes  int32
	FetchMaxBytes  int32
	PartitionFetch int32
	// Serialization.
	Key, Value serde
	Registry   registrySpec
}

// registrySpec is the Confluent serializer configuration.
type registrySpec struct {
	URLs            []string
	CredentialsFrom string // URL, USER_INFO, SASL_INHERIT
	UserInfo        string
	BearerToken     string
	TLS             tlsSpec
	AutoRegister    bool
	UseLatest       bool
	Normalize       bool
	UseSchemaID     int
	KeyStrategy     string
	ValueStrategy   string
}

// tlsSpec holds the ssl.* properties.
type tlsSpec struct {
	Protocol           string
	EnabledProtocols   []string
	EndpointID         *string
	TruststoreType     string
	TruststoreLocation string
	TruststorePassword string
	TruststoreCerts    string
	KeystoreType       string
	KeystoreLocation   string
	KeystorePassword   string
	KeyPassword        string
	KeystoreKey        string
	KeystoreChain      string
}

func (t tlsSpec) configured() bool {
	return t.TruststoreLocation != "" || t.TruststoreCerts != "" || t.KeystoreLocation != "" || t.KeystoreKey != ""
}

// The values of the properties that take one of a set, in the order the
// reference lists them.
var (
	protocols           = []string{"PLAINTEXT", "SSL", "SASL_PLAINTEXT", "SASL_SSL"}
	saslMechanisms      = []string{"PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512"}
	storeTypes          = []string{"JKS", "PKCS12", "PEM"}
	tlsVersions         = []string{"TLS", "TLSv1.2", "TLSv1.3"}
	ackValues           = []string{"all", "-1", "0", "1"}
	compressions        = []string{"none", "gzip", "snappy", "lz4", "zstd"}
	offsetResets        = []string{"earliest", "latest"}
	isolationLevels     = []string{"read_uncommitted", "read_committed"}
	credentialSources   = []string{"URL", "USER_INFO", "SASL_INHERIT"}
	serializerClasses   = []string{stringSerializer, bytesSerializer, avroSerializer}
	deserializerClasses = []string{stringDeserializer, bytesDeserializer, avroDeserializer}
	partitioners        = []string{defaultPartitioner, roundRobinPartitoner, uniformStickyPart}
	subjectStrategies   = []string{topicNameStrategy, recordNameStrategy, topicRecordStrategy}
)

// propDef describes how axx treats one Java property.
type propDef struct {
	Key   string
	Roles role
	// Takes says what the property takes, for the topic client step's
	// table in the reference (Markdown, a phrase); for a property without
	// effect, why it has none.
	Takes string
	// Values are the values it takes, when it takes one of a set.
	Values []string
	// Default is its value when the table does not set it.
	Default string
	// Set applies a value; nil means the property is known but has no
	// effect in axx.
	Set func(c *clientSpec, v string) error
	// Registry marks Confluent serializer properties (read by the
	// (de)serializer, not the Kafka client).
	Registry bool
	// Type is the parameter type of the value, for tools: "filepath" for a
	// file resolved against `resources`.
	Type string
}

func setMillis(dst *time.Duration) func(*clientSpec, string) error {
	return func(_ *clientSpec, v string) error {
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("expected a number of milliseconds, got %q", v)
		}
		*dst = time.Duration(n) * time.Millisecond
		return nil
	}
}

func parseInt32(v string) (int32, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 32)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("expected a non-negative number, got %q", v)
	}
	return int32(n), nil
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("expected true or false, got %q", v)
}

func oneOf(v string, allowed ...string) (string, error) {
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(v), a) {
			return a, nil
		}
	}
	return "", fmt.Errorf("expected one of %s, got %q", strings.Join(allowed, ", "), v)
}

// propList is every property axx translates, in the order the reference
// lists them.
var propList = buildProps()

// propTable is every property axx translates or knowingly ignores, by key.
var propTable = indexProps(propList)

// clientTableTypes are the parameter types of the topic client table's
// values that have one, under every key they are written with: the
// `producer.` and `consumer.` rows, and the `schema.registry.ssl.*` form of
// the TLS settings.
func clientTableTypes() map[string]string {
	out := map[string]string{}
	for key, def := range propTable {
		if def.Type == "" {
			continue
		}
		keys := []string{key}
		if strings.HasPrefix(key, "ssl.") {
			keys = append(keys, "schema.registry."+key)
		}
		for _, r := range []role{roleProducer, roleConsumer} {
			if def.Roles&r == 0 {
				continue
			}
			for _, k := range keys {
				out[r.String()+"."+k] = def.Type
			}
		}
	}
	return out
}

func buildProps() []propDef {
	defs := []propDef{
		// Connection.
		{
			Key: "bootstrap.servers", Roles: roleBoth,
			Takes: "the brokers the client connects to, a comma-separated list; the service's `brokers` by default",
			Set:   func(c *clientSpec, v string) error { c.Brokers = splitList(v); return nil },
		},
		{
			Key: "client.id", Roles: roleBoth, Default: "axx",
			Takes: "the name the client gives the brokers",
			Set:   func(c *clientSpec, v string) error { c.ClientID = v; return nil },
		},
		// Serialization.
		{
			Key: "key.serializer", Roles: roleProducer, Values: serializerClasses, Default: stringSerializer,
			Takes: "how the producer writes keys: `StringSerializer` as text, `ByteArraySerializer` as the text's bytes, `KafkaAvroSerializer` as an Avro string",
		},
		{
			Key: "value.serializer", Roles: roleProducer, Values: serializerClasses, Default: stringSerializer,
			Takes: "how the producer writes payloads: `StringSerializer` and `ByteArraySerializer` as text, `KafkaAvroSerializer` as Avro, which publishing with a schema needs",
		},
		{
			Key: "key.deserializer", Roles: roleConsumer, Values: deserializerClasses, Default: stringDeserializer,
			Takes: "how assertions read keys: `StringDeserializer` and `ByteArrayDeserializer` as text, `KafkaAvroDeserializer` as Avro",
		},
		{
			Key: "value.deserializer", Roles: roleConsumer, Values: deserializerClasses, Default: stringDeserializer,
			Takes: "how assertions read payloads: `StringDeserializer` and `ByteArrayDeserializer` as text, `KafkaAvroDeserializer` as Avro, whose properties are checked on Avro's text form of the record",
		},
		// Confluent serializer settings.
		{
			Key: "schema.registry.url", Roles: roleBoth, Registry: true,
			Takes: "the Schema Registry's URLs, a comma-separated list, which the Avro serializers need; `user:password@` in a URL signs in with basic auth",
			Set:   func(c *clientSpec, v string) error { c.Registry.URLs = splitList(v); return nil },
		},
		{
			Key: "basic.auth.credentials.source", Roles: roleBoth, Registry: true, Values: credentialSources, Default: "URL",
			Takes: "where the registry's basic auth credentials come from: the `URL`, `basic.auth.user.info` (`USER_INFO`), or the SASL username and password (`SASL_INHERIT`)",
			Set: func(c *clientSpec, v string) error {
				s, err := oneOf(v, credentialSources...)
				c.Registry.CredentialsFrom = s
				return err
			},
		},
		{
			Key: "basic.auth.user.info", Roles: roleBoth, Registry: true,
			Takes: "`user:password` for the registry, with `basic.auth.credentials.source=USER_INFO`",
			Set:   func(c *clientSpec, v string) error { c.Registry.UserInfo = v; return nil },
		},
		{
			Key: "schema.registry.basic.auth.user.info", Roles: roleBoth, Registry: true,
			Takes: "the older name of `basic.auth.user.info`",
			Set:   func(c *clientSpec, v string) error { c.Registry.UserInfo = v; return nil },
		},
		{
			Key: "bearer.auth.credentials.source", Roles: roleBoth, Registry: true, Values: []string{"STATIC_TOKEN"},
			Takes: "where the registry's bearer token comes from: only `STATIC_TOKEN`, the `bearer.auth.token`",
			Set: func(_ *clientSpec, v string) error {
				_, err := oneOf(v, "STATIC_TOKEN")
				return err
			},
		},
		{
			Key: "bearer.auth.token", Roles: roleBoth, Registry: true,
			Takes: "a bearer token for the registry",
			Set:   func(c *clientSpec, v string) error { c.Registry.BearerToken = v; return nil },
		},
		{
			Key: "auto.register.schemas", Roles: roleProducer, Registry: true, Default: "true",
			Takes: "`true` registers the schema under its subject; `false` looks its ID up, and fails if it is not registered",
			Set: func(c *clientSpec, v string) error {
				b, err := parseBool(v)
				c.Registry.AutoRegister = b
				return err
			},
		},
		{
			Key: "use.latest.version", Roles: roleProducer, Registry: true, Default: "false",
			Takes: "`true` writes with the subject's latest schema and its ID, with `auto.register.schemas=false`",
			Set: func(c *clientSpec, v string) error {
				b, err := parseBool(v)
				c.Registry.UseLatest = b
				return err
			},
		},
		{
			Key: "normalize.schemas", Roles: roleProducer, Registry: true, Default: "false",
			Takes: "`true` has the registry normalize the schema when registering it or looking it up",
			Set: func(c *clientSpec, v string) error {
				b, err := parseBool(v)
				c.Registry.Normalize = b
				return err
			},
		},
		{
			Key: "use.schema.id", Roles: roleProducer, Registry: true,
			Takes: "the ID of the schema to write with, with `auto.register.schemas=false`",
			Set: func(c *clientSpec, v string) error {
				n, err := strconv.Atoi(strings.TrimSpace(v))
				if err != nil {
					return fmt.Errorf("expected a schema ID, got %q", v)
				}
				c.Registry.UseSchemaID = n
				return nil
			},
		},
		{
			Key: "key.subject.name.strategy", Roles: roleProducer, Registry: true, Values: subjectStrategies, Default: topicNameStrategy,
			Takes: "the subject of a key's schema: `<topic>-key` (`TopicNameStrategy`); the record strategies fail, since a key is an Avro string",
			Set:   func(c *clientSpec, v string) error { return setStrategy(&c.Registry.KeyStrategy, v) },
		},
		{
			Key: "value.subject.name.strategy", Roles: roleProducer, Registry: true, Values: subjectStrategies, Default: topicNameStrategy,
			Takes: "the subject of a payload's schema: `<topic>-value` (`TopicNameStrategy`), the record's full name (`RecordNameStrategy`), or `<topic>-<full name>` (`TopicRecordNameStrategy`)",
			Set:   func(c *clientSpec, v string) error { return setStrategy(&c.Registry.ValueStrategy, v) },
		},
		{
			Key: "schema.reflection", Roles: roleBoth, Registry: true, Values: []string{"false"},
			Takes: "only `false`: reflection needs Java classes",
			Set: func(_ *clientSpec, v string) error {
				if b, err := parseBool(v); err != nil || b {
					return fmt.Errorf("schema.reflection=%s is not supported: reflection needs Java classes", v)
				}
				return nil
			},
		},
		{
			Key: "specific.avro.reader", Roles: roleConsumer, Registry: true, Values: []string{"false"},
			Takes: "only `false`: axx has no generated classes and reads generic records",
			Set: func(_ *clientSpec, v string) error {
				if b, err := parseBool(v); err != nil || b {
					return fmt.Errorf("specific.avro.reader=%s is not supported: axx reads generic records", v)
				}
				return nil
			},
		},
		// Producer.
		{
			Key: "acks", Roles: roleProducer, Values: ackValues, Default: "all",
			Takes: "the acknowledgements a publish waits for: from every in-sync replica (`all`, `-1`), from none (`0`) or from the leader (`1`); `0` and `1` turn idempotence off unless `enable.idempotence` is set, as in Java",
			Set: func(c *clientSpec, v string) error {
				a, err := oneOf(v, ackValues...)
				if a == "-1" {
					a = "all"
				}
				c.Acks = a
				return err
			},
		},
		{
			Key: "enable.idempotence", Roles: roleProducer,
			Takes: "`false` turns idempotent publishing off; it is on by default with `acks=all`, and `true` needs `acks=all`",
			Set: func(c *clientSpec, v string) error {
				b, err := parseBool(v)
				c.Idempotent = &b
				return err
			},
		},
		{
			Key: "compression.type", Roles: roleProducer, Values: compressions, Default: "none",
			Takes: "how the producer compresses what it sends",
			Set: func(c *clientSpec, v string) error {
				t, err := oneOf(v, compressions...)
				c.Compression = t
				return err
			},
		},
		{
			Key: "linger.ms", Roles: roleProducer, Default: "5",
			Takes: "how long the producer waits to fill a batch, in milliseconds, as in Java",
		},
		{
			Key: "max.request.size", Roles: roleProducer,
			Takes: "the largest batch the producer sends, in bytes",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.MaxRequestSize = n
				return err
			},
		},
		{
			Key: "buffer.memory", Roles: roleProducer,
			Takes: "the most bytes of records the producer holds before they are sent",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.BufferMemory = int(n)
				return err
			},
		},
		{
			Key: "delivery.timeout.ms", Roles: roleProducer,
			Takes: "how long a publish may take, retries included, in milliseconds",
		},
		{
			Key: "retries", Roles: roleProducer,
			Takes: "how many times a failed publish is retried",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				i := int(n)
				c.Retries = &i
				return err
			},
		},
		{
			Key: "max.in.flight.requests.per.connection", Roles: roleProducer,
			Takes: "how many publish requests may wait for an answer from a broker at once, with idempotence off",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.MaxInFlight = int(n)
				return err
			},
		},
		{
			Key: "partitioner.class", Roles: roleProducer, Values: partitioners, Default: defaultPartitioner,
			Takes: "how records are spread over the partitions: by the key's murmur2 hash (`DefaultPartitioner`), in turn (`RoundRobinPartitioner`) or a batch at a time (`UniformStickyPartitioner`); other classes fail the step",
			Set: func(c *clientSpec, v string) error {
				switch strings.TrimSpace(v) {
				case "", defaultPartitioner:
					c.Partitioner = ""
				case roundRobinPartitoner:
					c.Partitioner = "roundrobin"
				case uniformStickyPart:
					c.Partitioner = "sticky"
				default:
					return fmt.Errorf("partitioner class %s cannot run in axx (supported: DefaultPartitioner, RoundRobinPartitioner, UniformStickyPartitioner)", v)
				}
				return nil
			},
		},
		{
			Key: "transactional.id", Roles: roleProducer,
			Takes: "nothing: it fails the step, since the publish steps run no transactions and a transactional producer cannot send",
			Set: func(_ *clientSpec, _ string) error {
				return fmt.Errorf("transactional producers are not supported: the publish steps do not run transactions")
			},
		},
		// Consumer.
		{
			Key: "auto.offset.reset", Roles: roleConsumer, Values: offsetResets, Default: "earliest",
			Takes: "the records assertions consider: every record of the topic (`earliest`), or those produced after the assertion starts (`latest`); `none` fails the step, since axx reads without a consumer group",
			Set: func(c *clientSpec, v string) error {
				r, err := oneOf(v, offsetResets...)
				c.OffsetReset = r
				if err != nil {
					return fmt.Errorf("%w: axx reads topics without a consumer group, so there are no committed offsets", err)
				}
				return nil
			},
		},
		{
			Key: "isolation.level", Roles: roleConsumer, Values: isolationLevels, Default: "read_uncommitted",
			Takes: "`read_committed` leaves out the records of transactions that are open or were aborted",
			Set: func(c *clientSpec, v string) error {
				l, err := oneOf(v, isolationLevels...)
				c.ReadCommitted = l == "read_committed"
				return err
			},
		},
		{
			Key: "allow.auto.create.topics", Roles: roleConsumer, Default: "true",
			Takes: "`true` lets reading a topic that does not exist create it; producers always may, as in Java",
			Set: func(c *clientSpec, v string) error {
				b, err := parseBool(v)
				c.AllowAutoCreate = b
				return err
			},
		},
		{
			Key: "fetch.max.wait.ms", Roles: roleConsumer,
			Takes: "how long a broker may wait to fill a fetch, in milliseconds",
		},
		{
			Key: "fetch.min.bytes", Roles: roleConsumer,
			Takes: "the fewest bytes a broker answers a fetch with",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.FetchMinBytes = n
				return err
			},
		},
		{
			Key: "fetch.max.bytes", Roles: roleConsumer,
			Takes: "the most bytes a broker answers a fetch with",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.FetchMaxBytes = n
				return err
			},
		},
		{
			Key: "max.partition.fetch.bytes", Roles: roleConsumer,
			Takes: "the most bytes of one partition a broker answers a fetch with",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.PartitionFetch = n
				return err
			},
		},
		// Security.
		{
			Key: "security.protocol", Roles: roleBoth, Values: protocols, Default: "PLAINTEXT",
			Takes: "how the client connects: in plain text or over TLS (`SSL`), and signed in with SASL (`SASL_`) or not",
			Set: func(c *clientSpec, v string) error {
				p, err := oneOf(v, protocols...)
				c.Protocol = p
				return err
			},
		},
		{
			Key: "sasl.mechanism", Roles: roleBoth, Values: saslMechanisms, Default: "PLAIN",
			Takes: "how the client signs in with SASL; GSSAPI and OAUTHBEARER are not supported",
			Set: func(c *clientSpec, v string) error {
				m, err := oneOf(v, saslMechanisms...)
				c.SASLMechanism = m
				if err != nil {
					return fmt.Errorf("%w (GSSAPI and OAUTHBEARER are not supported)", err)
				}
				return nil
			},
		},
		{
			Key: "sasl.jaas.config", Roles: roleBoth,
			Takes: "the username and password the client signs in with, in a `PlainLoginModule` or `ScramLoginModule` entry; a `SASL_` protocol needs it",
			Set:   func(c *clientSpec, v string) error { c.JAAS = v; return nil },
		},
		{
			Key: "ssl.truststore.location", Roles: roleBoth, Type: "filepath",
			Takes: "the CA certificates the client trusts: a JKS, PKCS12 or PEM file, resolved against `resources`",
			Set:   func(c *clientSpec, v string) error { c.TLS.TruststoreLocation = v; return nil },
		},
		{
			Key: "ssl.truststore.password", Roles: roleBoth,
			Takes: "the truststore's password, which a JKS file may do without, as in Java",
			Set:   func(c *clientSpec, v string) error { c.TLS.TruststorePassword = v; return nil },
		},
		{
			Key: "ssl.truststore.type", Roles: roleBoth, Values: storeTypes, Default: "JKS",
			Takes: "the truststore's format, for a file axx does not recognize by its content as JKS, PKCS12 or PEM",
			Set: func(c *clientSpec, v string) error {
				t, err := oneOf(v, storeTypes...)
				c.TLS.TruststoreType = t
				return err
			},
		},
		{
			Key: "ssl.truststore.certificates", Roles: roleBoth,
			Takes: "CA certificates the client trusts, in PEM, written in the cell",
			Set:   func(c *clientSpec, v string) error { c.TLS.TruststoreCerts = v; return nil },
		},
		{
			Key: "ssl.keystore.location", Roles: roleBoth, Type: "filepath",
			Takes: "the client's certificate and key, for mutual TLS: a JKS, PKCS12 or PEM file, resolved against `resources`",
			Set:   func(c *clientSpec, v string) error { c.TLS.KeystoreLocation = v; return nil },
		},
		{
			Key: "ssl.keystore.password", Roles: roleBoth,
			Takes: "the keystore's password",
			Set:   func(c *clientSpec, v string) error { c.TLS.KeystorePassword = v; return nil },
		},
		{
			Key: "ssl.key.password", Roles: roleBoth,
			Takes: "the private key's password, for JKS key entries and encrypted PEM keys; the keystore's password by default",
			Set:   func(c *clientSpec, v string) error { c.TLS.KeyPassword = v; return nil },
		},
		{
			Key: "ssl.keystore.type", Roles: roleBoth, Values: storeTypes, Default: "JKS",
			Takes: "the keystore's format, for a file axx does not recognize by its content as JKS, PKCS12 or PEM",
			Set: func(c *clientSpec, v string) error {
				t, err := oneOf(v, storeTypes...)
				c.TLS.KeystoreType = t
				return err
			},
		},
		{
			Key: "ssl.keystore.key", Roles: roleBoth,
			Takes: "the client's private key, in PEM, written in the cell; with `ssl.keystore.certificate.chain`",
			Set:   func(c *clientSpec, v string) error { c.TLS.KeystoreKey = v; return nil },
		},
		{
			Key: "ssl.keystore.certificate.chain", Roles: roleBoth,
			Takes: "the client's certificate chain, in PEM, written in the cell",
			Set:   func(c *clientSpec, v string) error { c.TLS.KeystoreChain = v; return nil },
		},
		{
			Key: "ssl.endpoint.identification.algorithm", Roles: roleBoth, Default: "https",
			Takes: "`https` checks the broker's host name against its certificate; an empty value skips that check, and the certificate is still verified",
			Set:   func(c *clientSpec, v string) error { v = strings.TrimSpace(v); c.TLS.EndpointID = &v; return nil },
		},
		{
			Key: "ssl.protocol", Roles: roleBoth, Values: tlsVersions, Default: "TLSv1.2",
			Takes: "the oldest TLS version the client accepts; `TLS` is `TLSv1.2`",
			Set:   func(c *clientSpec, v string) error { c.TLS.Protocol = strings.TrimSpace(v); return nil },
		},
		{
			Key: "ssl.enabled.protocols", Roles: roleBoth,
			Takes: "the TLS versions the client may use, a comma-separated list of `TLSv1.2` and `TLSv1.3`; older versions are left out",
			Set:   func(c *clientSpec, v string) error { c.TLS.EnabledProtocols = splitList(v); return nil },
		},
		// Timing.
		{
			Key: "metadata.max.age.ms", Roles: roleBoth,
			Takes: "how often the client refreshes what it knows of the cluster, in milliseconds; assertions refresh every 5 seconds without it",
		},
		{
			Key: "connections.max.idle.ms", Roles: roleBoth,
			Takes: "how long a connection may stay idle before the client closes it, in milliseconds",
		},
		{
			Key: "socket.connection.setup.timeout.ms", Roles: roleBoth,
			Takes: "how long connecting to a broker may take, in milliseconds",
		},
		{
			Key: "request.timeout.ms", Roles: roleBoth,
			Takes: "how long a broker has to answer a request, in milliseconds",
		},
		{
			Key: "retry.backoff.ms", Roles: roleBoth,
			Takes: "how long the client waits before it retries a request, in milliseconds",
		},
	}
	// Durations need the target field, which setMillis closes over per spec.
	durations := map[string]func(*clientSpec) *time.Duration{
		"metadata.max.age.ms":                func(c *clientSpec) *time.Duration { return &c.MetadataMaxAge },
		"connections.max.idle.ms":            func(c *clientSpec) *time.Duration { return &c.ConnIdle },
		"socket.connection.setup.timeout.ms": func(c *clientSpec) *time.Duration { return &c.DialTimeout },
		"request.timeout.ms":                 func(c *clientSpec) *time.Duration { return &c.RequestTimeout },
		"retry.backoff.ms":                   func(c *clientSpec) *time.Duration { return &c.RetryBackoff },
		"linger.ms":                          func(c *clientSpec) *time.Duration { return &c.Linger },
		"delivery.timeout.ms":                func(c *clientSpec) *time.Duration { return &c.DeliveryTimeout },
		"fetch.max.wait.ms":                  func(c *clientSpec) *time.Duration { return &c.FetchMaxWait },
	}
	serdes := map[string]func(*clientSpec) *serde{
		"key.serializer":     func(c *clientSpec) *serde { return &c.Key },
		"value.serializer":   func(c *clientSpec) *serde { return &c.Value },
		"key.deserializer":   func(c *clientSpec) *serde { return &c.Key },
		"value.deserializer": func(c *clientSpec) *serde { return &c.Value },
	}
	for i, d := range defs {
		if f, ok := durations[d.Key]; ok {
			defs[i].Set = func(c *clientSpec, v string) error { return setMillis(f(c))(c, v) }
		}
		if f, ok := serdes[d.Key]; ok {
			deser := strings.HasSuffix(d.Key, "deserializer")
			defs[i].Set = func(c *clientSpec, v string) error { return setSerde(f(c), v, deser) }
		}
	}
	return defs
}

// indexProps indexes the translated properties and the ignored ones by key.
func indexProps(defs []propDef) map[string]propDef {
	out := map[string]propDef{}
	for _, d := range defs {
		out[d.Key] = d
	}
	for _, ig := range ignoredProps {
		for _, k := range ig.keys {
			out[k] = propDef{Key: k, Roles: ig.roles, Takes: ig.why, Registry: ig.registry}
		}
	}
	return out
}

func setSerde(dst *serde, v string, deser bool) error {
	v = strings.TrimSpace(v)
	names := map[string]serde{stringSerializer: serdeString, bytesSerializer: serdeBytes, avroSerializer: serdeAvro}
	if deser {
		names = map[string]serde{stringDeserializer: serdeString, bytesDeserializer: serdeBytes, avroDeserializer: serdeAvro}
	}
	if s, ok := names[v]; ok {
		*dst = s
		return nil
	}
	known := make([]string, 0, len(names))
	for n := range names {
		known = append(known, n)
	}
	sort.Strings(known)
	return fmt.Errorf("class %s cannot run in axx (supported: %s)", v, strings.Join(known, ", "))
}

func setStrategy(dst *string, v string) error {
	switch strings.TrimSpace(v) {
	case topicNameStrategy, recordNameStrategy, topicRecordStrategy:
		*dst = strings.TrimSpace(v)
		return nil
	}
	return fmt.Errorf("subject name strategy %s cannot run in axx (supported: TopicNameStrategy, RecordNameStrategy, TopicRecordNameStrategy)", v)
}

// ignoredProps are Java properties with no counterpart in axx: they are
// accepted silently (with a note in the step log), because they tune
// behaviour axx does not have (consumer groups, JMX, Java internals).
var ignoredProps = []struct {
	keys     []string
	roles    role
	why      string
	registry bool
}{
	{
		[]string{
			"group.id", "group.instance.id", "group.protocol", "group.remote.assignor", "enable.auto.commit",
			"auto.commit.interval.ms", "session.timeout.ms", "heartbeat.interval.ms", "max.poll.interval.ms",
			"max.poll.records", "partition.assignment.strategy", "internal.leave.group.on.close", "exclude.internal.topics",
			"default.api.timeout.ms", "client.rack", "check.crcs", "internal.throw.on.fetch.stable.offset.unsupported",
		},
		roleConsumer, "axx reads each topic from the start without a consumer group, and never commits offsets", false,
	},
	{
		[]string{
			"batch.size", "max.block.ms", "metadata.max.idle.ms", "partitioner.ignore.keys", "partitioner.adaptive.partitioning.enable",
			"partitioner.availability.timeout.ms", "transaction.timeout.ms", "compression.gzip.level", "compression.lz4.level", "compression.zstd.level",
		},
		roleProducer, "axx sizes batches by `max.request.size`, and publishes one event at a time", false,
	},
	{
		[]string{
			"client.dns.lookup", "receive.buffer.bytes", "send.buffer.bytes", "reconnect.backoff.ms", "reconnect.backoff.max.ms",
			"retry.backoff.max.ms", "socket.connection.setup.timeout.max.ms", "metadata.recovery.strategy", "metrics.num.samples",
			"metrics.recording.level", "metrics.sample.window.ms", "auto.include.jmx.reporter", "enable.metrics.push",
			"ssl.provider", "ssl.cipher.suites", "ssl.keymanager.algorithm", "ssl.trustmanager.algorithm", "ssl.secure.random.implementation",
			"sasl.kerberos.service.name", "sasl.login.connect.timeout.ms", "sasl.login.read.timeout.ms", "sasl.login.retry.backoff.ms",
			"sasl.login.retry.backoff.max.ms", "sasl.login.refresh.window.factor", "sasl.login.refresh.window.jitter",
			"sasl.login.refresh.min.period.seconds", "sasl.login.refresh.buffer.seconds",
			"key.serializer.encoding", "value.serializer.encoding", "serializer.encoding",
			"key.deserializer.encoding", "value.deserializer.encoding", "deserializer.encoding",
		},
		roleBoth, "they tune Java, and axx has no counterpart for them; its text is always UTF-8", false,
	},
	{
		[]string{
			"latest.compatibility.strict", "id.compatibility.strict", "avro.remove.java.properties", "avro.use.logical.type.converters",
			"avro.reflection.allow.null", "max.schemas.per.subject", "use.latest.with.metadata", "auto.register.schemas.retry",
		},
		roleBoth, "axx writes and reads generic Avro records", true,
	},
}

// rejectedProps name Java classes (or Java-only mechanisms) axx cannot run.
var rejectedProps = map[string]string{
	"interceptor.classes":                "client interceptors are Java classes",
	"sasl.login.class":                   "SASL login classes are Java classes",
	"sasl.client.callback.handler.class": "SASL callback handlers are Java classes",
	"sasl.login.callback.handler.class":  "SASL callback handlers are Java classes",
	"ssl.engine.factory.class":           "SSL engine factories are Java classes",
	"security.providers":                 "security providers are Java classes",
	"context.name.strategy":              "schema context strategies are Java classes",
	"specific.avro.key.type":             "specific records need generated Java classes",
	"specific.avro.value.type":           "specific records need generated Java classes",
}

// prop is one property row with its prefix removed.
type prop struct {
	Key, Value string
}

// translate builds a producer or consumer spec from Java properties. Rows
// for the other role and defaults are applied first by the caller.
func translate(r role, brokers string, props []prop) (*clientSpec, []string, error) {
	c := &clientSpec{
		Role: r, Brokers: splitList(brokers), Protocol: "PLAINTEXT", Acks: "all",
		Compression: "none", Linger: 5 * time.Millisecond, OffsetReset: "earliest",
		AllowAutoCreate: true,
		Registry: registrySpec{
			CredentialsFrom: "URL", AutoRegister: true, UseSchemaID: -1,
			KeyStrategy: topicNameStrategy, ValueStrategy: topicNameStrategy,
		},
	}
	var notes []string
	for _, p := range props {
		if rest, ok := strings.CutPrefix(p.Key, "schema.registry.ssl."); ok {
			def, known := propTable["ssl."+rest]
			if !known || def.Set == nil {
				notes = append(notes, fmt.Sprintf("warning: unknown %s property %q is ignored", r, p.Key))
				continue
			}
			tmp := &clientSpec{TLS: c.Registry.TLS}
			if err := def.Set(tmp, p.Value); err != nil {
				return nil, nil, fmt.Errorf("%s.%s: %w", r, p.Key, err)
			}
			c.Registry.TLS = tmp.TLS
			continue
		}
		def, known := propTable[p.Key]
		if reason, bad := rejectedProps[p.Key]; bad {
			if p.Key == "interceptor.classes" && strings.TrimSpace(p.Value) == "" {
				continue
			}
			return nil, nil, fmt.Errorf("%s.%s is not supported: %s", r, p.Key, reason)
		}
		if p.Key == "metric.reporters" {
			for _, cls := range splitList(p.Value) {
				if cls != jmxReporter {
					return nil, nil, fmt.Errorf("%s.metric.reporters: class %s cannot run in axx", r, cls)
				}
			}
			continue
		}
		switch {
		case !known && (strings.HasSuffix(p.Key, ".class") || strings.HasSuffix(p.Key, ".classes")):
			return nil, nil, fmt.Errorf("%s.%s is not supported: it names a Java class and axx cannot run Java code", r, p.Key)
		case !known:
			notes = append(notes, fmt.Sprintf("warning: unknown %s property %q is ignored", r, p.Key))
			continue
		case def.Roles&r == 0:
			notes = append(notes, fmt.Sprintf("warning: %q is a %s property; it has no effect on the %s", p.Key, def.Roles, r))
			continue
		case def.Set == nil:
			notes = append(notes, fmt.Sprintf("note: %s.%s has no effect in axx", r, p.Key))
			continue
		}
		if err := def.Set(c, p.Value); err != nil {
			return nil, nil, fmt.Errorf("%s.%s: %w", r, p.Key, err)
		}
	}
	if err := c.finish(); err != nil {
		return nil, nil, err
	}
	return c, notes, nil
}

// finish checks combinations, as the Java client's constructor did.
func (c *clientSpec) finish() error {
	if len(c.Brokers) == 0 {
		return fmt.Errorf("%s.bootstrap.servers is empty", c.Role)
	}
	sasl := strings.HasPrefix(c.Protocol, "SASL_")
	if sasl {
		if c.SASLMechanism == "" {
			c.SASLMechanism = "PLAIN" // Kafka's default sasl.mechanism is GSSAPI, which axx cannot run
		}
		if c.JAAS == "" {
			return fmt.Errorf("%s: security.protocol=%s needs sasl.jaas.config", c.Role, c.Protocol)
		}
		if _, _, err := parseJAAS(c.JAAS, c.SASLMechanism); err != nil {
			return fmt.Errorf("%s.sasl.jaas.config: %w", c.Role, err)
		}
	}
	if (c.Key == serdeAvro || c.Value == serdeAvro) && len(c.Registry.URLs) == 0 {
		return fmt.Errorf("%s: the Kafka Avro (de)serializer needs %s.schema.registry.url", c.Role, c.Role)
	}
	if c.Registry.CredentialsFrom == "USER_INFO" && !strings.Contains(c.Registry.UserInfo, ":") {
		return fmt.Errorf("%s: basic.auth.credentials.source=USER_INFO needs basic.auth.user.info as user:password", c.Role)
	}
	if c.Registry.CredentialsFrom == "SASL_INHERIT" && c.JAAS == "" {
		return fmt.Errorf("%s: basic.auth.credentials.source=SASL_INHERIT needs sasl.jaas.config", c.Role)
	}
	if c.Idempotent == nil && c.Acks != "all" {
		f := false // Java disables idempotence when acks is not all, unless it was set
		c.Idempotent = &f
	}
	if c.Idempotent != nil && *c.Idempotent && c.Acks != "all" {
		return fmt.Errorf("%s: enable.idempotence=true needs acks=all", c.Role)
	}
	return nil
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// prefix is how a row of the topic client's table names a property of
// the role: "<client>." stands for either prefix.
func (r role) prefix() string {
	switch r {
	case roleProducer:
		return "producer."
	case roleConsumer:
		return "consumer."
	}
	return "<client>."
}

// clientTable is what the topic client step's table holds, for the
// reference: every property axx applies, from propList.
func clientTable() *core.TableDoc {
	var rows []core.TableRow
	for i, d := range propList {
		rows = append(rows, core.TableRow{Name: d.Roles.prefix() + d.Key, Takes: d.Takes, Values: d.Values, Default: d.Default})
		if d.Registry && (i+1 == len(propList) || !propList[i+1].Registry) {
			rows = append(rows, core.TableRow{
				Name:  "<client>.schema.registry.ssl.<property>",
				Takes: "an `ssl.` property of this table, for HTTPS to the Schema Registry",
			})
		}
	}
	for _, r := range []role{roleProducer, roleConsumer} {
		rows = append(rows, core.TableRow{
			Name: r.String() + ".<property>",
			Takes: "any other Java " + r.String() + " property: those the pack's description lists have no effect or fail the step, " +
				"and the others are ignored, with a warning",
		})
	}
	return &core.TableDoc{
		Columns: []string{"property", "value"},
		Rows:    rows,
		Note: "`<client>` is `producer`, for publishing, or `consumer`, for assertions: axx applies the Java Kafka client property " +
			"after the prefix to that client. Values are expanded (`${env:..}`, `${sys:..}`); an empty value fails the step, " +
			"and a row with neither prefix is ignored, with a warning.",
	}
}

// otherPropsDoc lists, for the pack's description, the Java properties
// axx accepts without effect and those it rejects.
func otherPropsDoc() string {
	var b strings.Builder
	b.WriteString("Java client properties axx accepts without effect:\n\n")
	for _, ig := range ignoredProps {
		quoted := make([]string, len(ig.keys))
		for j, k := range ig.keys {
			quoted[j] = "`" + k + "`"
		}
		fmt.Fprintf(&b, "- %s: %s.\n", strings.Join(quoted, ", "), ig.why)
	}
	rejected := make([]string, 0, len(rejectedProps))
	for k := range rejectedProps {
		rejected = append(rejected, "`"+k+"`")
	}
	sort.Strings(rejected)
	b.WriteString("\nThose that fail the step, since they name Java classes: " + strings.Join(rejected, ", ") +
		", `metric.reporters` other than JmxReporter, and any other `*.class` or `*.classes` property.")
	return b.String()
}
