// Package mongo provides MongoDB steps: service registration and seeding.
package mongo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"go.mongodb.org/mongo-driver/v2/bson"
	driver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/connstring"

	"github.com/nimbusxr/axx/core"
)

// Service is a MongoDB database registered in a scenario.
type Service struct {
	Name     string
	URL      string
	Database string
	db       *driver.Database

	mu         sync.Mutex
	selections []*Selection
}

type ScenarioContext struct {
	services *core.Services[*Service]
}

var stateKey = core.NewStateKey("mongo", func(sc *core.Scenario) *ScenarioContext {
	st := &ScenarioContext{services: core.NewServices[*Service]("MongoDB service", "No MongoDB services set")}
	sc.Describe("mongo", st.describe)
	return st
}, nil)

// describe reports the last document selection of each service when a
// scenario fails.
func (st *ScenarioContext) describe() any {
	out := map[string]any{}
	for _, svc := range st.services.All() {
		svc.mu.Lock()
		if n := len(svc.selections); n > 0 {
			last := svc.selections[n-1]
			docs := make([]json.RawMessage, 0, 5)
			for _, d := range last.Docs[:min(len(last.Docs), 5)] {
				var buf bytes.Buffer
				writeJSON(&buf, d)
				docs = append(docs, buf.Bytes())
			}
			out[svc.Name] = map[string]any{"selections": n, "lastCollection": last.Collection, "lastCount": len(last.Docs), "lastDocs": docs}
		}
		svc.mu.Unlock()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Pack returns the MongoDB pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      "mongo",
		Namespace: "mongo",
		Doc:       "Register MongoDB databases, seed collections from JSON files, and query and assert on documents.",
		Params:    queryParams(),
		Steps: append([]core.StepDef{
			{
				ID: "mongo.service", Keyword: "Given", Arg: core.ArgTable,
				Expr: "a(n) {word} mongo database with the following properties:",
				Doc: "Register a MongoDB database. The first one registered in a scenario is the default.\n\n" +
					"Properties (all required, `${env:..}`/`${sys:..}` expanded): `url` (must include the database name; " +
					"`authSource` defaults to it), `user`, `password`.",
				Examples: []string{"Given a tracking-db mongo database with the following properties:"},
				Run:      addService,
			},
			{
				ID: "mongo.seed", Keyword: "Given",
				Expr:     "a {filepath} mongo db seed",
				Doc:      "Insert documents into the default MongoDB database. The file is a JSON object mapping collection names to arrays of documents (Extended JSON such as `{\"$oid\": ...}` is supported).",
				Examples: []string{"Given a seeds/scans-in-transit.json mongo db seed"},
				Run: func(sc *core.Scenario, a core.Args) error {
					svc, err := stateKey.Of(sc).services.Default()
					if err != nil {
						return fmt.Errorf("Could not perform MongoDB seed: %w", err) //nolint:staticcheck // user-facing message
					}
					if err := seed(sc, svc, a.String(0)); err != nil {
						return fmt.Errorf("Could not perform MongoDB seed: %w", err) //nolint:staticcheck // user-facing message
					}
					return nil
				},
			},
			namedSeed("mongo.seed.named", "a {filepath} MongoDB seed for {word}", "Given a seeds/scans-in-transit.json MongoDB seed for tracking-db", ""),
			namedSeed("mongo.seed.named.alt", "a {filepath} mongo db seed for {word}", "Given a seeds/scans-in-transit.json mongo db seed for tracking-db", "0.1.0"),
		}, querySteps()...),
	}
}

func namedSeed(id, expr, example, since string) core.StepDef {
	return core.StepDef{
		ID: id, Keyword: "Given", Expr: expr, Since: since,
		Doc:      "Insert documents into the named MongoDB database (same file format as the default-database seed).",
		Examples: []string{example},
		Run: func(sc *core.Scenario, a core.Args) error {
			name := a.String(1)
			svc, err := stateKey.Of(sc).services.Get(name)
			if err == nil {
				err = seed(sc, svc, a.String(0))
			}
			if err != nil {
				return fmt.Errorf("Could not perform MongoDB seed for %s: %w", name, err) //nolint:staticcheck // user-facing message
			}
			return nil
		},
	}
}

func addService(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	props := map[string]string{}
	for _, p := range pairs {
		if !p.Null {
			props[p.Key] = sc.Suite().Interpolate(p.Value)
		}
	}
	for _, k := range []string{"url", "user", "password"} {
		if _, ok := props[k]; !ok {
			return fmt.Errorf("Property %q is required", k) //nolint:staticcheck // user-facing message
		}
	}
	svc, err := Connect(sc, a.String(0), props["url"], props["user"], props["password"])
	if err != nil {
		return err
	}
	return Context(sc).AddService(svc)
}

// Connect connects (or reuses, for the whole run) a client and returns an
// unregistered service; add it with AddService. The URL must name the
// database; authSource defaults to it.
func Connect(sc *core.Scenario, name, url, user, password string) (*Service, error) {
	cs, err := connstring.Parse(url)
	if err != nil {
		return nil, fmt.Errorf("Could not connect to MongoDB: %w", err) //nolint:staticcheck // user-facing message
	}
	if cs.Database == "" {
		return nil, errors.New("Could not connect to MongoDB: Database name must be specified in the URL (e.g., mongodb://localhost:27017/my_database)") //nolint:staticcheck // user-facing message
	}
	authSource := cs.Database
	if _, after, ok := strings.Cut(url, "authSource="); ok {
		if i := strings.IndexByte(after, '&'); i > 0 {
			after = after[:i]
		}
		authSource = after
	}
	key := strings.Join([]string{url, user, password, authSource}, "\x00")
	client, err := core.Cached(sc.Suite(), "mongo.client:"+key, func() (*driver.Client, error) {
		opts := options.Client().ApplyURI(url).SetAuth(options.Credential{
			Username: user, Password: password, AuthSource: authSource,
		})
		c, err := driver.Connect(opts)
		if err != nil {
			return nil, err
		}
		sc.Suite().OnClose(func(ctx context.Context) error { return c.Disconnect(ctx) })
		return c, nil
	})
	if err != nil {
		return nil, fmt.Errorf("Could not connect to MongoDB: %w", err) //nolint:staticcheck // user-facing message
	}
	return &Service{Name: name, URL: url, Database: cs.Database, db: client.Database(cs.Database)}, nil
}

// seedDoc is a seed file: collection name -> documents, in file order.
type seedDoc = bson.D

func seed(sc *core.Scenario, svc *Service, file string) error {
	docs, err := readSeed(sc, file)
	if err != nil {
		return err
	}
	for _, coll := range docs {
		items, ok := coll.Value.(bson.A)
		if !ok {
			return fmt.Errorf("Collection data must be an array for collection: %s", coll.Key) //nolint:staticcheck // user-facing message
		}
		if len(items) == 0 {
			continue
		}
		if _, err := svc.db.Collection(coll.Key).InsertMany(sc.Context(), []any(items)); err != nil {
			return fmt.Errorf("insert into %s: %w", coll.Key, err)
		}
	}
	return nil
}

func readSeed(sc *core.Scenario, file string) (seedDoc, error) {
	path, err := sc.Suite().ResolvePath(file)
	if err != nil {
		return nil, fmt.Errorf("Could not find seed file: %s", file) //nolint:staticcheck // user-facing message
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Could not find seed file: %s", file) //nolint:staticcheck // user-facing message
	}
	return parseSeed(data)
}

func parseSeed(data []byte) (seedDoc, error) {
	var d bson.D
	if err := bson.UnmarshalExtJSON(data, false, &d); err != nil {
		return nil, fmt.Errorf("invalid seed JSON: %w", err)
	}
	return d, nil
}
