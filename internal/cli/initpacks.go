package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// PackSuggestion is a pack of axx's that `axx init` suggests for a
// repository, and what in the repository suggests it.
type PackSuggestion struct {
	Pack    string   `json:"pack"`
	Reasons []string `json:"reasons"`
}

// packSignal finds a pack's use in a file's text.
type packSignal struct {
	pack string
	re   *regexp.Regexp
	what string // what the match is, for the reason ("PostgreSQL")
}

// Images of services in compose files.
var composeSignals = []packSignal{
	{"sql", regexp.MustCompile(`(?im)^\s*image:\s*\S*(postgres|postgis|mysql|mariadb|mssql|sqlserver)\S*`), "a database image"},
	{"mongo", regexp.MustCompile(`(?im)^\s*image:\s*\S*mongo\S*`), "a MongoDB image"},
	{"redis", regexp.MustCompile(`(?im)^\s*image:\s*\S*(redis|valkey|dragonfly|keydb)\S*`), "a Redis image"},
	{"kafka", regexp.MustCompile(`(?im)^\s*image:\s*\S*(kafka|redpanda)\S*`), "a Kafka image"},
	{"amqp", regexp.MustCompile(`(?im)^\s*image:\s*\S*(rabbitmq|artemis)\S*`), "a RabbitMQ image"},
	{"mqtt", regexp.MustCompile(`(?im)^\s*image:\s*\S*(mosquitto|emqx|hivemq)\S*`), "an MQTT broker image"},
	{"nats", regexp.MustCompile(`(?im)^\s*image:\s*\S*nats\S*`), "a NATS image"},
	{"mock", regexp.MustCompile(`(?im)^\s*image:\s*\S*wiremock\S*`), "a WireMock image"},
	{"mail", regexp.MustCompile(`(?im)^\s*image:\s*\S*(mailpit|mailhog)\S*`), "a mail catcher image"},
}

// Client libraries in dependency manifests.
var dependencySignals = []packSignal{
	{"sql", regexp.MustCompile(`(?i)jackc/pgx|lib/pq|go-sql-driver/mysql|microsoft/go-mssqldb|"pg"\s*:|mysql2|psycopg|asyncpg|pymysql|postgresql</artifactId>|mysql-connector|mssql-jdbc|org\.postgresql`), "a SQL database client"},
	{"mongo", regexp.MustCompile(`(?i)mongo-driver|"mongodb"\s*:|"mongoose"\s*:|pymongo|motor|mongodb-driver`), "a MongoDB client"},
	{"redis", regexp.MustCompile(`(?i)go-redis|"(io)?redis"\s*:|redis-py|^redis[=<>~ ]|jedis|lettuce`), "a Redis client"},
	{"kafka", regexp.MustCompile(`(?i)franz-go|segmentio/kafka-go|confluent-kafka|kafka-clients|"kafkajs"\s*:|kafka-python|aiokafka|spring-kafka`), "a Kafka client"},
	{"amqp", regexp.MustCompile(`(?i)amqp091|streadway/amqp|"amqplib"\s*:|pika|spring-boot-starter-amqp|azure-amqp`), "an AMQP client"},
	{"mqtt", regexp.MustCompile(`(?i)paho\.mqtt|"mqtt"\s*:|paho-mqtt|hivemq-mqtt-client`), "an MQTT client"},
	{"nats", regexp.MustCompile(`(?i)nats-io/nats\.go|"nats"\s*:|nats-py|jnats`), "a NATS client"},
	{"grpc", regexp.MustCompile(`(?i)google\.golang\.org/grpc|"@grpc/grpc-js"\s*:|grpcio|grpc-java|io\.grpc`), "a gRPC library"},
	{"graphql", regexp.MustCompile(`(?i)gqlgen|graphql-go|"graphql"\s*:|"@apollo/server"\s*:|graphene|strawberry-graphql|graphql-java`), "a GraphQL library"},
}

// Technologies the docs name (README, docs/).
var docSignals = []packSignal{
	{"sql", regexp.MustCompile(`\b(PostgreSQL|Postgres|MySQL|MariaDB|SQL Server|SQLite)\b`), ""},
	{"mongo", regexp.MustCompile(`\bMongoDB\b`), ""},
	{"redis", regexp.MustCompile(`\b(Redis|Valkey)\b`), ""},
	{"kafka", regexp.MustCompile(`\bKafka\b`), ""},
	{"amqp", regexp.MustCompile(`\b(RabbitMQ|AMQP)\b`), ""},
	{"mqtt", regexp.MustCompile(`\bMQTT\b`), ""},
	{"nats", regexp.MustCompile(`\bNATS\b`), ""},
	{"mock", regexp.MustCompile(`\bWireMock\b`), ""},
}

var (
	composeFile    = regexp.MustCompile(`^(docker-)?compose(\.[\w-]+)?\.ya?ml$`)
	dependencyFile = regexp.MustCompile(`^(go\.mod|package\.json|pom\.xml|build\.gradle(\.kts)?|requirements[\w.-]*\.txt|pyproject\.toml|Pipfile)$`)
	docFile        = regexp.MustCompile(`(?i)^(readme(\.\w+)?|[^/]+\.md)$`)
)

// suggestPacks lists the packs of axx's that a repository's files point to,
// beyond those it uses (have): its compose files' images, its dependency
// manifests' clients, its schema files (Avro, protobuf, AsyncAPI, GraphQL),
// and the technologies its README and docs name. It looks a few folders
// deep, and never into dependencies or builds.
func suggestPacks(dir string, have []string) []PackSuggestion {
	reasons := map[string][]string{}
	add := func(pack, reason string) {
		if slices.Contains(have, pack) || slices.Contains(reasons[pack], reason) {
			return
		}
		reasons[pack] = append(reasons[pack], reason)
	}
	scan := func(path, rel string, signals []packSignal, says string) {
		b, err := os.ReadFile(path)
		if err != nil || len(b) > 1<<20 {
			return
		}
		for _, s := range signals {
			if m := s.re.FindString(string(b)); m != "" {
				what := s.what
				if what == "" {
					what = strings.TrimSpace(m)
				}
				add(s.pack, rel+" "+says+" "+what)
			}
		}
	}
	files := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable folder suggests nothing
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			name := d.Name()
			if rel != "." && (strings.HasPrefix(name, ".") || slices.Contains([]string{"node_modules", "vendor", "dist", "build", "target", "out", "bin"}, name) || strings.Count(rel, "/") >= 3) {
				return filepath.SkipDir
			}
			return nil
		}
		if files++; files > 2000 {
			return filepath.SkipAll
		}
		name := d.Name()
		switch {
		case composeFile.MatchString(name):
			scan(path, rel, composeSignals, "runs")
		case dependencyFile.MatchString(name):
			scan(path, rel, dependencySignals, "uses")
		case strings.HasSuffix(name, ".avsc"):
			add("kafka", rel+" is an Avro schema")
		case strings.HasSuffix(name, ".proto"):
			add("grpc", rel+" is a protobuf definition")
		case strings.HasSuffix(name, ".graphql") || strings.HasSuffix(name, ".graphqls"):
			add("graphql", rel+" is a GraphQL schema")
		case strings.HasPrefix(strings.ToLower(name), "asyncapi.") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".json")):
			add("asyncapi", rel+" is an AsyncAPI document")
		case docFile.MatchString(name) && (rel == name || strings.HasPrefix(rel, "docs/")):
			scan(path, rel, docSignals, "mentions")
		}
		return nil
	})
	out := make([]PackSuggestion, 0, len(reasons))
	for p, r := range reasons {
		if len(r) > 3 {
			r = r[:3]
		}
		out = append(out, PackSuggestion{Pack: p, Reasons: r})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pack < out[j].Pack })
	return out
}
