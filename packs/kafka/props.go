package kafka

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
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

// propDef describes how axx treats one Java property.
type propDef struct {
	Key   string
	Roles role
	// Doc says what axx does with the property (Markdown).
	Doc string
	// Set applies a value; nil means the property is known but has no
	// effect in axx (Doc says why).
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

// propTable lists every property axx translates or knowingly ignores. It
// also generates the pack documentation.
var propTable = buildPropTable()

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

func buildPropTable() map[string]propDef {
	defs := []propDef{
		{
			Key: "bootstrap.servers", Roles: roleBoth, Doc: "Seed brokers (`kgo.SeedBrokers`); defaults to the service's `brokers`.",
			Set: func(c *clientSpec, v string) error { c.Brokers = splitList(v); return nil },
		},
		{
			Key: "client.id", Roles: roleBoth, Doc: "`kgo.ClientID`.",
			Set: func(c *clientSpec, v string) error { c.ClientID = v; return nil },
		},
		{
			Key: "security.protocol", Roles: roleBoth, Doc: "`PLAINTEXT`, `SSL` (TLS dialer), `SASL_PLAINTEXT` or `SASL_SSL` (`kgo.SASL`).",
			Set: func(c *clientSpec, v string) error {
				p, err := oneOf(v, "PLAINTEXT", "SSL", "SASL_PLAINTEXT", "SASL_SSL")
				c.Protocol = p
				return err
			},
		},
		{
			Key: "sasl.mechanism", Roles: roleBoth, Doc: "`PLAIN`, `SCRAM-SHA-256` or `SCRAM-SHA-512` (GSSAPI and OAUTHBEARER are not supported).",
			Set: func(c *clientSpec, v string) error {
				m, err := oneOf(v, "PLAIN", "SCRAM-SHA-256", "SCRAM-SHA-512")
				c.SASLMechanism = m
				if err != nil {
					return fmt.Errorf("%w (GSSAPI and OAUTHBEARER are not supported)", err)
				}
				return nil
			},
		},
		{
			Key: "sasl.jaas.config", Roles: roleBoth, Doc: "The `username` and `password` of a `PlainLoginModule` or `ScramLoginModule` entry.",
			Set: func(c *clientSpec, v string) error { c.JAAS = v; return nil },
		},
		{
			Key: "ssl.truststore.location", Roles: roleBoth, Type: "filepath", Doc: "CA certificates (JKS, PKCS12 or PEM file, resolved against `resources`).",
			Set: func(c *clientSpec, v string) error { c.TLS.TruststoreLocation = v; return nil },
		},
		{
			Key: "ssl.truststore.password", Roles: roleBoth, Doc: "Truststore password (optional for JKS, as in Java).",
			Set: func(c *clientSpec, v string) error { c.TLS.TruststorePassword = v; return nil },
		},
		{
			Key: "ssl.truststore.type", Roles: roleBoth, Doc: "`JKS` (default), `PKCS12` or `PEM`; JKS and PKCS12 files are recognized by content.",
			Set: func(c *clientSpec, v string) error {
				t, err := oneOf(v, "JKS", "PKCS12", "PEM")
				c.TLS.TruststoreType = t
				return err
			},
		},
		{
			Key: "ssl.truststore.certificates", Roles: roleBoth, Doc: "Inline PEM CA certificates.",
			Set: func(c *clientSpec, v string) error { c.TLS.TruststoreCerts = v; return nil },
		},
		{
			Key: "ssl.keystore.location", Roles: roleBoth, Type: "filepath", Doc: "Client certificate and key (JKS, PKCS12 or PEM file) for mutual TLS.",
			Set: func(c *clientSpec, v string) error { c.TLS.KeystoreLocation = v; return nil },
		},
		{
			Key: "ssl.keystore.password", Roles: roleBoth, Doc: "Keystore password.",
			Set: func(c *clientSpec, v string) error { c.TLS.KeystorePassword = v; return nil },
		},
		{
			Key: "ssl.key.password", Roles: roleBoth, Doc: "Private key password (JKS key entries, encrypted PEM keys); defaults to the keystore password.",
			Set: func(c *clientSpec, v string) error { c.TLS.KeyPassword = v; return nil },
		},
		{
			Key: "ssl.keystore.type", Roles: roleBoth, Doc: "`JKS` (default), `PKCS12` or `PEM`.",
			Set: func(c *clientSpec, v string) error {
				t, err := oneOf(v, "JKS", "PKCS12", "PEM")
				c.TLS.KeystoreType = t
				return err
			},
		},
		{
			Key: "ssl.keystore.key", Roles: roleBoth, Doc: "Inline PEM private key (with `ssl.keystore.certificate.chain`).",
			Set: func(c *clientSpec, v string) error { c.TLS.KeystoreKey = v; return nil },
		},
		{
			Key: "ssl.keystore.certificate.chain", Roles: roleBoth, Doc: "Inline PEM certificate chain.",
			Set: func(c *clientSpec, v string) error { c.TLS.KeystoreChain = v; return nil },
		},
		{
			Key: "ssl.endpoint.identification.algorithm", Roles: roleBoth, Doc: "`https` (default) verifies the broker host name; empty skips that check (the chain is still verified).",
			Set: func(c *clientSpec, v string) error { v = strings.TrimSpace(v); c.TLS.EndpointID = &v; return nil },
		},
		{
			Key: "ssl.protocol", Roles: roleBoth, Doc: "`TLSv1.2` or `TLSv1.3` sets the minimum TLS version (`TLS` allows both).",
			Set: func(c *clientSpec, v string) error { c.TLS.Protocol = strings.TrimSpace(v); return nil },
		},
		{
			Key: "ssl.enabled.protocols", Roles: roleBoth, Doc: "Limits TLS versions to the listed `TLSv1.2`/`TLSv1.3`.",
			Set: func(c *clientSpec, v string) error { c.TLS.EnabledProtocols = splitList(v); return nil },
		},
		{Key: "metadata.max.age.ms", Roles: roleBoth, Doc: "`kgo.MetadataMaxAge`."},
		{Key: "connections.max.idle.ms", Roles: roleBoth, Doc: "`kgo.ConnIdleTimeout`."},
		{Key: "socket.connection.setup.timeout.ms", Roles: roleBoth, Doc: "`kgo.DialTimeout`."},
		{Key: "request.timeout.ms", Roles: roleBoth, Doc: "Producer: `kgo.ProduceRequestTimeout`; consumer: `kgo.RequestTimeoutOverhead`."},
		{Key: "retry.backoff.ms", Roles: roleBoth, Doc: "Constant `kgo.RetryBackoffFn`."},
		{
			Key: "allow.auto.create.topics", Roles: roleConsumer, Doc: "`true` (default) lets reading a missing topic create it (`kgo.AllowAutoTopicCreation`). Producers always may, as in Java.",
			Set: func(c *clientSpec, v string) error {
				b, err := parseBool(v)
				c.AllowAutoCreate = b
				return err
			},
		},
		{Key: "key.serializer", Roles: roleProducer, Doc: "`StringSerializer` (default), `ByteArraySerializer` (the key text's bytes) or `KafkaAvroSerializer` (the key as an Avro string)."},
		{Key: "value.serializer", Roles: roleProducer, Doc: "`StringSerializer` (default), `ByteArraySerializer` or `KafkaAvroSerializer` (needed to publish with a schema)."},
		{Key: "key.deserializer", Roles: roleConsumer, Doc: "`StringDeserializer` (default), `ByteArrayDeserializer` or `KafkaAvroDeserializer`."},
		{Key: "value.deserializer", Roles: roleConsumer, Doc: "`StringDeserializer` (default), `ByteArrayDeserializer` or `KafkaAvroDeserializer` (payloads are checked against the record's Java `toString()`)."},
		{
			Key: "acks", Roles: roleProducer, Doc: "`all`/`-1` (default), `1` or `0` (`kgo.RequiredAcks`); `0` and `1` disable idempotence unless it is set explicitly, as in Java.",
			Set: func(c *clientSpec, v string) error {
				a, err := oneOf(v, "all", "-1", "0", "1")
				if a == "-1" {
					a = "all"
				}
				c.Acks = a
				return err
			},
		},
		{
			Key: "enable.idempotence", Roles: roleProducer, Doc: "`false` sets `kgo.DisableIdempotentWrite`.",
			Set: func(c *clientSpec, v string) error {
				b, err := parseBool(v)
				c.Idempotent = &b
				return err
			},
		},
		{
			Key: "compression.type", Roles: roleProducer, Doc: "`none` (default), `gzip`, `snappy`, `lz4` or `zstd` (`kgo.ProducerBatchCompression`).",
			Set: func(c *clientSpec, v string) error {
				t, err := oneOf(v, "none", "gzip", "snappy", "lz4", "zstd")
				c.Compression = t
				return err
			},
		},
		{Key: "linger.ms", Roles: roleProducer, Doc: "`kgo.ProducerLinger` (default 5 ms, as in Java)."},
		{
			Key: "max.request.size", Roles: roleProducer, Doc: "`kgo.ProducerBatchMaxBytes`.",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.MaxRequestSize = n
				return err
			},
		},
		{
			Key: "buffer.memory", Roles: roleProducer, Doc: "`kgo.MaxBufferedBytes`.",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.BufferMemory = int(n)
				return err
			},
		},
		{Key: "delivery.timeout.ms", Roles: roleProducer, Doc: "`kgo.RecordDeliveryTimeout`."},
		{
			Key: "retries", Roles: roleProducer, Doc: "`kgo.RecordRetries`.",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				i := int(n)
				c.Retries = &i
				return err
			},
		},
		{
			Key: "max.in.flight.requests.per.connection", Roles: roleProducer, Doc: "`kgo.MaxProduceRequestsInflightPerBroker`.",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.MaxInFlight = int(n)
				return err
			},
		},
		{
			Key: "partitioner.class", Roles: roleProducer, Doc: "`DefaultPartitioner` (murmur2 of the key, as by default), `RoundRobinPartitioner` or `UniformStickyPartitioner`; other classes are errors.",
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
			Key: "transactional.id", Roles: roleProducer, Doc: "Error: the steps never begin a transaction, so a transactional producer cannot send.",
			Set: func(_ *clientSpec, _ string) error {
				return fmt.Errorf("transactional producers are not supported: the publish steps do not run transactions")
			},
		},
		{
			Key: "auto.offset.reset", Roles: roleConsumer, Doc: "`earliest` (default): assertions consider every record of the topic; `latest`: only records produced after the assertion starts. `none` is an error.",
			Set: func(c *clientSpec, v string) error {
				r, err := oneOf(v, "earliest", "latest")
				c.OffsetReset = r
				if err != nil {
					return fmt.Errorf("%w: axx reads topics without a consumer group, so there are no committed offsets", err)
				}
				return nil
			},
		},
		{
			Key: "isolation.level", Roles: roleConsumer, Doc: "`read_uncommitted` (default) or `read_committed` (`kgo.FetchIsolationLevel`).",
			Set: func(c *clientSpec, v string) error {
				l, err := oneOf(v, "read_uncommitted", "read_committed")
				c.ReadCommitted = l == "read_committed"
				return err
			},
		},
		{Key: "fetch.max.wait.ms", Roles: roleConsumer, Doc: "`kgo.FetchMaxWait`."},
		{
			Key: "fetch.min.bytes", Roles: roleConsumer, Doc: "`kgo.FetchMinBytes`.",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.FetchMinBytes = n
				return err
			},
		},
		{
			Key: "fetch.max.bytes", Roles: roleConsumer, Doc: "`kgo.FetchMaxBytes`.",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.FetchMaxBytes = n
				return err
			},
		},
		{
			Key: "max.partition.fetch.bytes", Roles: roleConsumer, Doc: "`kgo.FetchMaxPartitionBytes`.",
			Set: func(c *clientSpec, v string) error {
				n, err := parseInt32(v)
				c.PartitionFetch = n
				return err
			},
		},
		// Confluent serializer settings.
		{
			Key: "schema.registry.url", Roles: roleBoth, Registry: true, Doc: "Schema Registry URLs (comma-separated); required by the Avro (de)serializers. `user:password@` in a URL is used for basic auth.",
			Set: func(c *clientSpec, v string) error { c.Registry.URLs = splitList(v); return nil },
		},
		{
			Key: "basic.auth.credentials.source", Roles: roleBoth, Registry: true, Doc: "`URL` (default), `USER_INFO` or `SASL_INHERIT` (the SASL username and password).",
			Set: func(c *clientSpec, v string) error {
				s, err := oneOf(v, "URL", "USER_INFO", "SASL_INHERIT")
				c.Registry.CredentialsFrom = s
				return err
			},
		},
		{
			Key: "basic.auth.user.info", Roles: roleBoth, Registry: true, Doc: "`user:password` for `USER_INFO`.",
			Set: func(c *clientSpec, v string) error { c.Registry.UserInfo = v; return nil },
		},
		{
			Key: "schema.registry.basic.auth.user.info", Roles: roleBoth, Registry: true, Doc: "Older name of `basic.auth.user.info`.",
			Set: func(c *clientSpec, v string) error { c.Registry.UserInfo = v; return nil },
		},
		{
			Key: "bearer.auth.credentials.source", Roles: roleBoth, Registry: true, Doc: "Only `STATIC_TOKEN` is supported.",
			Set: func(_ *clientSpec, v string) error {
				_, err := oneOf(v, "STATIC_TOKEN")
				return err
			},
		},
		{
			Key: "bearer.auth.token", Roles: roleBoth, Registry: true, Doc: "Static bearer token for the registry.",
			Set: func(c *clientSpec, v string) error { c.Registry.BearerToken = v; return nil },
		},
		{
			Key: "auto.register.schemas", Roles: roleProducer, Registry: true, Doc: "`true` (default) registers the schema under the subject; `false` looks its ID up and fails if it is not registered.",
			Set: func(c *clientSpec, v string) error {
				b, err := parseBool(v)
				c.Registry.AutoRegister = b
				return err
			},
		},
		{
			Key: "use.latest.version", Roles: roleProducer, Registry: true, Doc: "With `auto.register.schemas=false`, writes with the subject's latest schema and ID.",
			Set: func(c *clientSpec, v string) error {
				b, err := parseBool(v)
				c.Registry.UseLatest = b
				return err
			},
		},
		{
			Key: "normalize.schemas", Roles: roleProducer, Registry: true, Doc: "Passes `normalize=true` when registering or looking up.",
			Set: func(c *clientSpec, v string) error {
				b, err := parseBool(v)
				c.Registry.Normalize = b
				return err
			},
		},
		{
			Key: "use.schema.id", Roles: roleProducer, Registry: true, Doc: "Writes with this schema ID (with `auto.register.schemas=false`).",
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
			Key: "key.subject.name.strategy", Roles: roleProducer, Registry: true, Doc: "`TopicNameStrategy` (default: `<topic>-key`), `RecordNameStrategy` or `TopicRecordNameStrategy`.",
			Set: func(c *clientSpec, v string) error { return setStrategy(&c.Registry.KeyStrategy, v) },
		},
		{
			Key: "value.subject.name.strategy", Roles: roleProducer, Registry: true, Doc: "Same choices; the default subject is `<topic>-value`.",
			Set: func(c *clientSpec, v string) error { return setStrategy(&c.Registry.ValueStrategy, v) },
		},
		{
			Key: "schema.reflection", Roles: roleBoth, Registry: true, Doc: "Only `false`: reflection needs Java classes.",
			Set: func(_ *clientSpec, v string) error {
				if b, err := parseBool(v); err != nil || b {
					return fmt.Errorf("schema.reflection=%s is not supported: reflection needs Java classes", v)
				}
				return nil
			},
		},
		{
			Key: "specific.avro.reader", Roles: roleConsumer, Registry: true, Doc: "Only `false`: axx has no generated classes and reads generic records.",
			Set: func(_ *clientSpec, v string) error {
				if b, err := parseBool(v); err != nil || b {
					return fmt.Errorf("specific.avro.reader=%s is not supported: axx reads generic records", v)
				}
				return nil
			},
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
	out := map[string]propDef{}
	for _, d := range defs {
		if f, ok := durations[d.Key]; ok {
			d.Set = func(c *clientSpec, v string) error { return setMillis(f(c))(c, v) }
		}
		if f, ok := serdes[d.Key]; ok {
			deser := strings.HasSuffix(d.Key, "deserializer")
			d.Set = func(c *clientSpec, v string) error { return setSerde(f(c), v, deser) }
		}
		out[d.Key] = d
	}
	for _, ig := range ignoredProps {
		for _, k := range ig.keys {
			out[k] = propDef{Key: k, Roles: ig.roles, Doc: ig.why, Registry: ig.registry}
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
		roleConsumer, "No effect: axx reads each topic from the start without a consumer group and never commits offsets.", false,
	},
	{
		[]string{
			"batch.size", "max.block.ms", "metadata.max.idle.ms", "partitioner.ignore.keys", "partitioner.adaptive.partitioning.enable",
			"partitioner.availability.timeout.ms", "transaction.timeout.ms", "compression.gzip.level", "compression.lz4.level", "compression.zstd.level",
		},
		roleProducer, "No effect: franz-go sizes batches by `max.request.size` and publishes each event synchronously.", false,
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
		roleBoth, "No effect: Java tuning without a franz-go counterpart (strings are always UTF-8).", false,
	},
	{
		[]string{
			"latest.compatibility.strict", "id.compatibility.strict", "avro.remove.java.properties", "avro.use.logical.type.converters",
			"avro.reflection.allow.null", "max.schemas.per.subject", "use.latest.with.metadata", "auto.register.schemas.retry",
		},
		roleBoth, "No effect: axx writes and reads generic Avro records as described above.", true,
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

// propDoc renders the translation table for the pack documentation.
func propDoc() string {
	keys := make([]string, 0, len(propTable))
	for k, d := range propTable {
		if d.Set != nil || !isIgnored(k) {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := propTable[keys[i]], propTable[keys[j]]
		if a.Registry != b.Registry {
			return !a.Registry
		}
		return keys[i] < keys[j]
	})
	var b strings.Builder
	b.WriteString("| Property | Client | In axx |\n| --- | --- | --- |\n")
	for _, k := range keys {
		d := propTable[k]
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", k, d.Roles, d.Doc)
	}
	b.WriteString("| `schema.registry.ssl.*` | producer, consumer | The `ssl.*` settings above, for HTTPS to the Schema Registry. |\n")
	b.WriteString("\nAccepted without effect: ")
	for i, ig := range ignoredProps {
		if i > 0 {
			b.WriteString(" ")
		}
		quoted := make([]string, len(ig.keys))
		for j, k := range ig.keys {
			quoted[j] = "`" + k + "`"
		}
		b.WriteString(strings.Join(quoted, ", ") + " (" + strings.TrimSuffix(strings.TrimPrefix(ig.why, "No effect: "), ".") + ").")
	}
	rejected := make([]string, 0, len(rejectedProps))
	for k := range rejectedProps {
		rejected = append(rejected, "`"+k+"`")
	}
	sort.Strings(rejected)
	b.WriteString("\n\nRejected (they name Java classes): " + strings.Join(rejected, ", ") +
		", `metric.reporters` other than JmxReporter, and any unknown `*.class`/`*.classes` property. Other unknown properties are logged as warnings and ignored.")
	return b.String()
}

func isIgnored(k string) bool {
	for _, ig := range ignoredProps {
		for _, x := range ig.keys {
			if x == k {
				return true
			}
		}
	}
	return false
}
