package cli

import (
	"io"
	"os"
	"path"
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

// Client libraries in dependency manifests. Names that other packages' names
// start with (pika, motor) count only whole.
var dependencySignals = []packSignal{
	{"sql", regexp.MustCompile(`(?i)jackc/pgx|lib/pq|go-sql-driver/mysql|microsoft/go-mssqldb|"pg"\s*:|mysql2|psycopg|asyncpg|pymysql|postgresql</artifactId>|mysql-connector|mssql-jdbc|org\.postgresql`), "a SQL database client"},
	{"mongo", regexp.MustCompile(`(?im)mongo-driver|"mongodb"\s*:|"mongoose"\s*:|pymongo|(^|[^\w./-])motor($|[^\w./-])|mongodb-driver`), "a MongoDB client"},
	{"redis", regexp.MustCompile(`(?im)go-redis|"(io)?redis"\s*:|redis-py|^\s*["']?redis(\s|$|[=<>~!\[;"'])|jedis|lettuce`), "a Redis client"},
	{"kafka", regexp.MustCompile(`(?i)franz-go|segmentio/kafka-go|confluent-kafka|kafka-clients|"kafkajs"\s*:|kafka-python|aiokafka|spring-kafka`), "a Kafka client"},
	{"amqp", regexp.MustCompile(`(?im)amqp091|streadway/amqp|"amqplib"\s*:|(^|[^\w./-])pika($|[^\w./-])|spring-boot-starter-amqp|azure-amqp`), "an AMQP client"},
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
	// A go.mod's dependency of a dependency says nothing of what the project uses.
	indirectLine = regexp.MustCompile(`(?m)^.*//\s*indirect\s*$`)
)

const (
	// maxScanned is the most of a file suggestPacks reads; larger files are
	// not manifests or docs it can make anything of.
	maxScanned = 1 << 20
	// maxSuggestFiles is how many files of a repository it looks at.
	maxSuggestFiles = 2000
)

// readScanned reads a regular file of at most maxScanned bytes, through a link
// too. Anything else in a repository, a FIFO that would block for ever or a
// link to /dev/zero, reads as nothing.
func readScanned(file string) ([]byte, bool) {
	fi, err := os.Stat(file)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxScanned {
		return nil, false
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxScanned+1))
	if err != nil || len(b) > maxScanned {
		return nil, false
	}
	return b, true
}

// suggestPacks lists the packs of axx's that a repository's files point to,
// beyond those it uses (have): its compose files' images, its dependency
// manifests' clients, its schema files (Avro, protobuf, AsyncAPI, GraphQL),
// and the technologies its README and docs name. It looks a few folders
// deep, the top level first, and never into dependencies or builds.
func suggestPacks(dir string, have []string) []PackSuggestion {
	reasons := map[string][]string{}
	add := func(pack, reason string) {
		if slices.Contains(have, pack) || slices.Contains(reasons[pack], reason) {
			return
		}
		reasons[pack] = append(reasons[pack], reason)
	}
	scan := func(rel string, signals []packSignal, says string) {
		b, ok := readScanned(filepath.Join(dir, filepath.FromSlash(rel)))
		if !ok {
			return
		}
		text := string(b)
		if path.Base(rel) == "go.mod" {
			text = indirectLine.ReplaceAllString(text, "")
		}
		for _, s := range signals {
			if m := s.re.FindString(text); m != "" {
				what := s.what
				if what == "" {
					what = strings.TrimSpace(m)
				}
				add(s.pack, rel+" "+says+" "+what)
			}
		}
	}
	look := func(rel string) {
		name := path.Base(rel)
		switch {
		case composeFile.MatchString(name):
			scan(rel, composeSignals, "runs")
		case dependencyFile.MatchString(name):
			scan(rel, dependencySignals, "uses")
		case strings.HasSuffix(name, ".avsc"):
			add("kafka", rel+" is an Avro schema")
		case strings.HasSuffix(name, ".proto"):
			add("grpc", rel+" is a protobuf definition")
		case strings.HasSuffix(name, ".graphql") || strings.HasSuffix(name, ".graphqls"):
			add("graphql", rel+" is a GraphQL schema")
		case strings.HasPrefix(strings.ToLower(name), "asyncapi.") && (strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".json")):
			add("asyncapi", rel+" is an AsyncAPI document")
		case docFile.MatchString(name) && (rel == name || strings.HasPrefix(rel, "docs/")):
			scan(rel, docSignals, "mentions")
		}
	}
	// A folder at a time, those nearest the top first: thousands of files deep
	// in a repository use up maxSuggestFiles only after what its top level says.
	files := 0
	folders := []string{"."}
walk:
	for len(folders) > 0 {
		folder := folders[0]
		folders = folders[1:]
		entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(folder)))
		if err != nil {
			continue // an unreadable folder suggests nothing
		}
		for _, d := range entries {
			name, rel := d.Name(), path.Join(folder, d.Name())
			if d.IsDir() {
				if !strings.HasPrefix(name, ".") && !slices.Contains([]string{"node_modules", "vendor", "dist", "build", "target", "out", "bin"}, name) && strings.Count(rel, "/") < 3 {
					folders = append(folders, rel)
				}
				continue
			}
			if files++; files > maxSuggestFiles {
				break walk
			}
			look(rel)
		}
	}
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
