package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iskorotkov/avro/v2"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sr"

	"github.com/nimbusxr/axx/core"
)

// Clients are shared by every scenario of a run (axx.Cached): one producer
// per distinct producer configuration, one tailer per topic and consumer
// configuration, one registry client per registry configuration.

// fingerprint identifies a configuration for the suite cache.
func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func stripScheme(brokers []string) []string {
	out := make([]string, len(brokers))
	for i, b := range brokers {
		if _, rest, ok := strings.Cut(b, "://"); ok {
			b = rest
		}
		out[i] = b
	}
	return out
}

// connOpts are the options every client of a spec shares.
func connOpts(c *clientSpec, resolve func(string) (string, error)) ([]kgo.Opt, error) {
	opts := []kgo.Opt{kgo.SeedBrokers(stripScheme(c.Brokers)...)}
	id := c.ClientID
	if id == "" {
		id = "axx"
	}
	opts = append(opts, kgo.ClientID(id))
	sec, err := securityOpts(c, resolve)
	if err != nil {
		return nil, err
	}
	opts = append(opts, sec...)
	if c.MetadataMaxAge > 0 {
		opts = append(opts, kgo.MetadataMaxAge(c.MetadataMaxAge))
	}
	if c.ConnIdle > 0 {
		opts = append(opts, kgo.ConnIdleTimeout(c.ConnIdle))
	}
	if c.DialTimeout > 0 {
		opts = append(opts, kgo.DialTimeout(c.DialTimeout))
	}
	if d := c.RetryBackoff; d > 0 {
		opts = append(opts, kgo.RetryBackoffFn(func(int) time.Duration { return d }))
	}
	return opts, nil
}

// producerKey is the part of a spec that shapes the producer client.
func producerKey(c *clientSpec) string {
	cp := *c
	cp.Key, cp.Value, cp.Registry = 0, 0, registrySpec{}
	return "kafka.producer:" + fingerprint(cp)
}

func producerFor(suite *core.Suite, c *clientSpec) (*kgo.Client, error) {
	return core.Cached(suite, producerKey(c), func() (*kgo.Client, error) {
		opts, err := connOpts(c, suite.ResolvePath)
		if err != nil {
			return nil, err
		}
		opts = append(opts, kgo.AllowAutoTopicCreation(), kgo.ProducerLinger(c.Linger))
		switch c.Acks {
		case "0":
			opts = append(opts, kgo.RequiredAcks(kgo.NoAck()))
		case "1":
			opts = append(opts, kgo.RequiredAcks(kgo.LeaderAck()))
		default:
			opts = append(opts, kgo.RequiredAcks(kgo.AllISRAcks()))
		}
		if c.Idempotent != nil && !*c.Idempotent {
			opts = append(opts, kgo.DisableIdempotentWrite())
			if c.MaxInFlight > 0 {
				opts = append(opts, kgo.MaxProduceRequestsInflightPerBroker(c.MaxInFlight))
			}
		}
		codec := map[string]kgo.CompressionCodec{
			"none": kgo.NoCompression(), "gzip": kgo.GzipCompression(), "snappy": kgo.SnappyCompression(),
			"lz4": kgo.Lz4Compression(), "zstd": kgo.ZstdCompression(),
		}[c.Compression]
		opts = append(opts, kgo.ProducerBatchCompression(codec))
		if c.MaxRequestSize > 0 {
			opts = append(opts, kgo.ProducerBatchMaxBytes(c.MaxRequestSize))
		}
		if c.BufferMemory > 0 {
			opts = append(opts, kgo.MaxBufferedBytes(c.BufferMemory))
		}
		if c.DeliveryTimeout > 0 {
			opts = append(opts, kgo.RecordDeliveryTimeout(c.DeliveryTimeout))
		}
		if c.RequestTimeout > 0 {
			opts = append(opts, kgo.ProduceRequestTimeout(c.RequestTimeout))
		}
		if c.Retries != nil {
			opts = append(opts, kgo.RecordRetries(*c.Retries))
		}
		switch c.Partitioner {
		case "roundrobin":
			opts = append(opts, kgo.RecordPartitioner(kgo.RoundRobinPartitioner()))
		case "sticky":
			opts = append(opts, kgo.RecordPartitioner(kgo.StickyPartitioner()))
		}
		cl, err := kgo.NewClient(opts...)
		if err != nil {
			return nil, err
		}
		suite.OnClose(func(context.Context) error { cl.Close(); return nil })
		return cl, nil
	})
}

// tailerKey is the part of a consumer spec that shapes what the tailer
// reads (deserializers and offset reset are applied per topic client).
func tailerKey(c *clientSpec, topic string) string {
	cp := *c
	cp.Key, cp.Value, cp.Registry, cp.OffsetReset = 0, 0, registrySpec{}, ""
	return "kafka.tailer:" + topic + ":" + fingerprint(cp)
}

func tailerFor(suite *core.Suite, c *clientSpec, topic string, maxRecords int) (*tailer, error) {
	return core.Cached(suite, tailerKey(c, topic), func() (*tailer, error) {
		opts, err := connOpts(c, suite.ResolvePath)
		if err != nil {
			return nil, err
		}
		opts = append(opts,
			kgo.ConsumeTopics(topic),
			kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
			kgo.MetadataMinAge(250*time.Millisecond),
		)
		if c.MetadataMaxAge == 0 {
			opts = append(opts, kgo.MetadataMaxAge(5*time.Second)) // pick up new partitions and late topics quickly
		}
		if c.AllowAutoCreate {
			opts = append(opts, kgo.AllowAutoTopicCreation())
		}
		if c.ReadCommitted {
			opts = append(opts, kgo.FetchIsolationLevel(kgo.ReadCommitted()))
		}
		if c.FetchMaxWait > 0 {
			opts = append(opts, kgo.FetchMaxWait(c.FetchMaxWait))
		}
		if c.FetchMinBytes > 0 {
			opts = append(opts, kgo.FetchMinBytes(c.FetchMinBytes))
		}
		if c.FetchMaxBytes > 0 {
			opts = append(opts, kgo.FetchMaxBytes(c.FetchMaxBytes))
		}
		if c.PartitionFetch > 0 {
			opts = append(opts, kgo.FetchMaxPartitionBytes(c.PartitionFetch))
		}
		if c.RequestTimeout > 0 {
			opts = append(opts, kgo.RequestTimeoutOverhead(c.RequestTimeout))
		}
		t, err := startTailer(topic, opts, maxRecords)
		if err != nil {
			return nil, err
		}
		suite.OnClose(func(context.Context) error { t.close(); return nil })
		return t, nil
	})
}

// registry is a Schema Registry client with the caches the Confluent
// serializers keep: schemas by ID, and IDs by subject and schema.
type registry struct {
	cl *sr.Client

	mu   sync.Mutex
	byID map[int]avro.Schema
	ids  map[string]int
}

func registryFor(suite *core.Suite, c *clientSpec) (*registry, error) {
	spec := c.Registry
	key := "kafka.registry:" + fingerprint(spec)
	if spec.CredentialsFrom == "SASL_INHERIT" {
		key += ":" + c.JAAS
	}
	return core.Cached(suite, key, func() (*registry, error) {
		var urls []string
		opts := []sr.ClientOpt{sr.UserAgent("axx")}
		var user, pass string
		for _, raw := range spec.URLs {
			u, err := url.Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("schema.registry.url %q: %w", raw, err)
			}
			if u.User != nil {
				user = u.User.Username()
				pass, _ = u.User.Password()
				u.User = nil
			}
			urls = append(urls, u.String())
		}
		opts = append(opts, sr.URLs(urls...))
		switch spec.CredentialsFrom {
		case "USER_INFO":
			user, pass, _ = strings.Cut(spec.UserInfo, ":")
		case "SASL_INHERIT":
			var err error
			if user, pass, err = parseJAAS(c.JAAS, c.SASLMechanism); err != nil {
				return nil, fmt.Errorf("basic.auth.credentials.source=SASL_INHERIT: %w", err)
			}
		}
		if user != "" {
			opts = append(opts, sr.BasicAuth(user, pass))
		}
		if spec.BearerToken != "" {
			opts = append(opts, sr.BearerToken(spec.BearerToken))
		}
		tlsCfg, err := registryTLS(spec, suite.ResolvePath)
		if err != nil {
			return nil, fmt.Errorf("schema.registry.ssl: %w", err)
		}
		if tlsCfg != nil {
			opts = append(opts, sr.DialTLSConfig(tlsCfg))
		}
		cl, err := sr.NewClient(opts...)
		if err != nil {
			return nil, err
		}
		return &registry{cl: cl, byID: map[int]avro.Schema{}, ids: map[string]int{}}, nil
	})
}

// schema returns the writer schema with the given ID.
func (r *registry) schema(ctx context.Context, id int) (avro.Schema, error) {
	r.mu.Lock()
	s, ok := r.byID[id]
	r.mu.Unlock()
	if ok {
		return s, nil
	}
	got, err := r.cl.SchemaByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("fetching schema %d from the registry: %w", id, err)
	}
	if got.Type != sr.TypeAvro {
		return nil, fmt.Errorf("schema %d is %s, not Avro", id, got.Type)
	}
	s, err = parseSchema(got.Schema)
	if err != nil {
		return nil, fmt.Errorf("schema %d: %w", id, err)
	}
	r.mu.Lock()
	r.byID[id] = s
	r.mu.Unlock()
	return s, nil
}

// writerID returns the schema ID (and the schema to write with) for a value
// of schema text under subject, following the Confluent serializer:
// auto.register.schemas first, then use.schema.id, then use.latest.version,
// and otherwise a lookup of the exact schema.
func (r *registry) writerID(ctx context.Context, spec registrySpec, subject, text string, local avro.Schema) (int, avro.Schema, error) {
	cacheKey := subject + "\x00" + text
	r.mu.Lock()
	id, ok := r.ids[cacheKey]
	r.mu.Unlock()
	if ok {
		return id, local, nil
	}
	if spec.Normalize {
		ctx = sr.WithParams(ctx, sr.Normalize)
	}
	schema := sr.Schema{Schema: text, Type: sr.TypeAvro}
	switch {
	case spec.AutoRegister:
		got, err := r.cl.RegisterSchema(ctx, subject, schema, -1, -1)
		if err != nil {
			return 0, nil, fmt.Errorf("registering the schema under subject %s: %w", subject, err)
		}
		id = got
	case spec.UseSchemaID >= 0:
		s, err := r.schema(ctx, spec.UseSchemaID)
		if err != nil {
			return 0, nil, err
		}
		return spec.UseSchemaID, s, nil
	case spec.UseLatest:
		got, err := r.cl.SchemaByVersion(ctx, subject, -1)
		if err != nil {
			return 0, nil, fmt.Errorf("reading the latest schema of subject %s: %w", subject, err)
		}
		s, err := parseSchema(got.Schema.Schema)
		if err != nil {
			return 0, nil, fmt.Errorf("latest schema of subject %s: %w", subject, err)
		}
		return got.ID, s, nil
	default:
		got, err := r.cl.LookupSchema(ctx, subject, schema)
		if err != nil {
			var re *sr.ResponseError
			if errors.As(err, &re) && re.StatusCode == http.StatusNotFound {
				return 0, nil, fmt.Errorf("the schema is not registered under subject %s and auto.register.schemas is false: %w", subject, err)
			}
			return 0, nil, fmt.Errorf("looking the schema up under subject %s: %w", subject, err)
		}
		id = got.ID
	}
	r.mu.Lock()
	r.ids[cacheKey] = id
	r.mu.Unlock()
	return id, local, nil
}

// parseSchema parses Avro schema text in its own cache, so named types of
// unrelated schemas never clash.
func parseSchema(text string) (avro.Schema, error) {
	return avro.ParseWithCache(text, "", &avro.SchemaCache{})
}

// endOffsets returns each partition's end offset, for auto.offset.reset=latest.
func endOffsets(ctx context.Context, cl *kgo.Client, topic string) (map[int32]int64, error) {
	adm := kadm.NewClient(cl)
	res, err := adm.ListEndOffsets(ctx, topic)
	if err != nil {
		return nil, err
	}
	out := map[int32]int64{}
	var firstErr error
	res.Each(func(o kadm.ListedOffset) {
		if o.Err != nil && firstErr == nil {
			firstErr = o.Err
		}
		out[o.Partition] = o.Offset
	})
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}
