package kafka

import (
	"strings"
	"testing"
	"time"
)

func TestTranslateDefaults(t *testing.T) {
	p, notes, err := translate(roleProducer, "a:9092, b:9092", nil)
	if err != nil || len(notes) != 0 {
		t.Fatal(err, notes)
	}
	if strings.Join(p.Brokers, ",") != "a:9092,b:9092" || p.Acks != "all" || p.Idempotent != nil || p.Compression != "none" ||
		p.Linger != 5*time.Millisecond || p.Key != serdeString || p.Value != serdeString || p.Protocol != "PLAINTEXT" {
		t.Errorf("producer defaults: %+v", p)
	}
	if !p.Registry.AutoRegister || p.Registry.UseSchemaID != -1 || p.Registry.ValueStrategy != topicNameStrategy {
		t.Errorf("registry defaults: %+v", p.Registry)
	}
	c, _, err := translate(roleConsumer, "a:9092", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.OffsetReset != "earliest" || c.ReadCommitted || !c.AllowAutoCreate {
		t.Errorf("consumer defaults: %+v", c)
	}
}

func TestTranslate(t *testing.T) {
	jaas := `org.apache.kafka.common.security.scram.ScramLoginModule required username="app" password="p\"w";`
	cases := []struct {
		name  string
		role  role
		props []prop
		check func(*clientSpec) bool
		note  string
		err   string
	}{
		{name: "producer settings", role: roleProducer, props: []prop{
			{"acks", "-1"},
			{"compression.type", "ZSTD"},
			{"linger.ms", "20"},
			{"max.request.size", "2097152"},
			{"buffer.memory", "1048576"},
			{"delivery.timeout.ms", "60000"},
			{"retries", "3"},
			{"request.timeout.ms", "15000"},
			{"client.id", "axx-test"},
			{"partitioner.class", roundRobinPartitoner},
			{"bootstrap.servers", "PLAINTEXT://x:1"},
		}, check: func(c *clientSpec) bool {
			return c.Acks == "all" && c.Compression == "zstd" && c.Linger == 20*time.Millisecond && c.MaxRequestSize == 2097152 &&
				c.BufferMemory == 1048576 && c.DeliveryTimeout == time.Minute && *c.Retries == 3 && c.RequestTimeout == 15*time.Second &&
				c.ClientID == "axx-test" && c.Partitioner == "roundrobin" && stripScheme(c.Brokers)[0] == "x:1"
		}},
		{
			name: "acks 0 disables idempotence", role: roleProducer, props: []prop{{"acks", "0"}},
			check: func(c *clientSpec) bool { return c.Idempotent != nil && !*c.Idempotent },
		},
		{
			name: "explicit idempotence needs acks all", role: roleProducer, props: []prop{{"acks", "1"}, {"enable.idempotence", "true"}},
			err: "enable.idempotence=true needs acks=all",
		},
		{name: "consumer settings", role: roleConsumer, props: []prop{
			{"auto.offset.reset", "latest"},
			{"isolation.level", "read_committed"},
			{"fetch.max.wait.ms", "100"},
			{"fetch.min.bytes", "1"},
			{"fetch.max.bytes", "1000"},
			{"max.partition.fetch.bytes", "500"},
			{"allow.auto.create.topics", "false"},
		}, check: func(c *clientSpec) bool {
			return c.OffsetReset == "latest" && c.ReadCommitted && c.FetchMaxWait == 100*time.Millisecond && c.FetchMinBytes == 1 &&
				c.FetchMaxBytes == 1000 && c.PartitionFetch == 500 && !c.AllowAutoCreate
		}},
		{name: "offset reset none", role: roleConsumer, props: []prop{{"auto.offset.reset", "none"}}, err: "without a consumer group"},
		{
			name: "group settings are notes", role: roleConsumer, props: []prop{{"group.id", "g"}, {"enable.auto.commit", "true"}},
			note: "note: consumer.enable.auto.commit has no effect in axx",
		},
		{name: "unknown property warns", role: roleProducer, props: []prop{{"linger.msec", "1"}}, note: `unknown producer property "linger.msec"`},
		{
			name: "wrong role warns", role: roleProducer, props: []prop{{"auto.offset.reset", "earliest"}},
			note: `"auto.offset.reset" is a consumer property; it has no effect on the producer`,
		},
		{name: "unknown class property", role: roleProducer, props: []prop{{"my.custom.class", "com.example.X"}}, err: "names a Java class"},
		{name: "interceptors", role: roleConsumer, props: []prop{{"interceptor.classes", "com.example.Tracing"}}, err: "client interceptors are Java classes"},
		{name: "empty interceptors", role: roleConsumer, props: []prop{{"interceptor.classes", " "}}, check: func(*clientSpec) bool { return true }},
		{name: "jmx reporter", role: roleConsumer, props: []prop{{"metric.reporters", jmxReporter}}, check: func(*clientSpec) bool { return true }},
		{name: "other reporter", role: roleConsumer, props: []prop{{"metric.reporters", "com.example.R"}}, err: "cannot run in axx"},
		{name: "custom partitioner", role: roleProducer, props: []prop{{"partitioner.class", "com.example.P"}}, err: "partitioner class com.example.P"},
		{name: "transactions", role: roleProducer, props: []prop{{"transactional.id", "tx"}}, err: "transactional producers are not supported"},
		{name: "bad number", role: roleProducer, props: []prop{{"linger.ms", "soon"}}, err: "producer.linger.ms: expected a number of milliseconds"},
		{name: "serializers", role: roleProducer, props: []prop{
			{"key.serializer", bytesSerializer},
			{"value.serializer", avroSerializer},
			{"schema.registry.url", "http://u:p@r:8081, http://r2:8081"},
			{"auto.register.schemas", "false"},
			{"use.latest.version", "true"},
			{"normalize.schemas", "true"},
			{"value.subject.name.strategy", topicRecordStrategy},
		}, check: func(c *clientSpec) bool {
			return c.Key == serdeBytes && c.Value == serdeAvro && len(c.Registry.URLs) == 2 && !c.Registry.AutoRegister &&
				c.Registry.UseLatest && c.Registry.Normalize && c.Registry.ValueStrategy == topicRecordStrategy
		}},
		{name: "avro needs a registry", role: roleConsumer, props: []prop{{"value.deserializer", avroDeserializer}}, err: "needs consumer.schema.registry.url"},
		{name: "deserializer name on producer", role: roleProducer, props: []prop{{"value.serializer", stringDeserializer}}, err: "cannot run in axx"},
		{name: "specific reader", role: roleConsumer, props: []prop{{"specific.avro.reader", "true"}}, err: "reads generic records"},
		{name: "registry user info", role: roleConsumer, props: []prop{
			{"basic.auth.credentials.source", "USER_INFO"},
			{"basic.auth.user.info", "u:p"},
			{"schema.registry.url", "http://r"},
			{"schema.registry.ssl.truststore.location", "ca.pem"},
			{"schema.registry.ssl.endpoint.identification.algorithm", ""},
		}, check: func(c *clientSpec) bool {
			return c.Registry.CredentialsFrom == "USER_INFO" && c.Registry.UserInfo == "u:p" &&
				c.Registry.TLS.TruststoreLocation == "ca.pem" && *c.Registry.TLS.EndpointID == "" && c.TLS.TruststoreLocation == ""
		}},
		{
			name: "user info needs a colon", role: roleConsumer, props: []prop{{"basic.auth.credentials.source", "USER_INFO"}, {"basic.auth.user.info", "u"}},
			err: "basic.auth.user.info as user:password",
		},
		{name: "sasl scram", role: roleBoth, props: []prop{
			{"security.protocol", "SASL_SSL"},
			{"sasl.mechanism", "SCRAM-SHA-512"},
			{"sasl.jaas.config", jaas},
			{"ssl.truststore.type", "PKCS12"},
			{"ssl.truststore.location", "trust.p12"},
			{"ssl.truststore.password", "changeit"},
			{"ssl.enabled.protocols", "TLSv1.3,TLSv1.2"},
		}, check: func(c *clientSpec) bool {
			return c.Protocol == "SASL_SSL" && c.SASLMechanism == "SCRAM-SHA-512" && c.TLS.TruststoreType == "PKCS12" &&
				c.TLS.TruststorePassword == "changeit" && len(c.TLS.EnabledProtocols) == 2
		}},
		{name: "sasl needs jaas", role: roleProducer, props: []prop{{"security.protocol", "SASL_PLAINTEXT"}}, err: "needs sasl.jaas.config"},
		{name: "sasl gssapi", role: roleProducer, props: []prop{{"sasl.mechanism", "GSSAPI"}}, err: "GSSAPI and OAUTHBEARER are not supported"},
		{name: "jaas module must fit the mechanism", role: roleProducer, props: []prop{
			{"security.protocol", "SASL_PLAINTEXT"}, {"sasl.mechanism", "PLAIN"}, {"sasl.jaas.config", jaas},
		}, err: "ScramLoginModule does not match sasl.mechanism=PLAIN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := c.role
			if r == roleBoth {
				r = roleProducer
			}
			spec, notes, err := translate(r, "b:9092", c.props)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("want error containing %q, got %v", c.err, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.note != "" && !strings.Contains(strings.Join(notes, "\n"), c.note) {
				t.Errorf("notes %q should contain %q", notes, c.note)
			}
			if c.check != nil && !c.check(spec) {
				t.Errorf("spec: %+v", spec)
			}
		})
	}
}

func TestParseJAAS(t *testing.T) {
	cases := []struct {
		cfg, mech, user, pass, err string
	}{
		{`org.apache.kafka.common.security.plain.PlainLoginModule required username="alice" password="s3cret";`, "PLAIN", "alice", "s3cret", ""},
		{`org.apache.kafka.common.security.scram.ScramLoginModule required
		    username="bob"
		    password="a \"quoted\" \\ value";`, "SCRAM-SHA-256", "bob", `a "quoted" \ value`, ""},
		{`PlainLoginModule optional username=carol password=pw`, "PLAIN", "carol", "pw", ""},
		{`org.apache.kafka.common.security.plain.PlainLoginModule maybe username="a";`, "PLAIN", "", "", "control flag"},
		{`com.sun.security.auth.module.Krb5LoginModule required useKeyTab=true;`, "PLAIN", "", "", "not supported"},
		{`org.apache.kafka.common.security.plain.PlainLoginModule required password="x";`, "PLAIN", "", "", "no username"},
		{`org.apache.kafka.common.security.plain.PlainLoginModule required username="x`, "PLAIN", "", "", "unterminated"},
		{`org.apache.kafka.common.security.plain.PlainLoginModule required username;`, "PLAIN", "", "", "expected key=value"},
	}
	for _, c := range cases {
		user, pass, err := parseJAAS(c.cfg, c.mech)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: want error %q, got %v", c.cfg, c.err, err)
			}
			continue
		}
		if err != nil || user != c.user || pass != c.pass {
			t.Errorf("%s: got %q %q %v", c.cfg, user, pass, err)
		}
	}
}

func TestPropDocListsEveryProperty(t *testing.T) {
	doc := propDoc()
	for k := range propTable {
		if !strings.Contains(doc, "`"+k+"`") {
			t.Errorf("the documentation does not mention %s", k)
		}
	}
	for k := range rejectedProps {
		if !strings.Contains(doc, "`"+k+"`") {
			t.Errorf("the documentation does not mention rejected %s", k)
		}
	}
}
