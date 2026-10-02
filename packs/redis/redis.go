// Package redis is the redis pack: keys seeded into Redis (or Valkey,
// Dragonfly, KeyDB, Garnet: any server that speaks its protocol), and the
// keys the services under test write there.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/secrets"
)

// Name is the pack's name.
const Name = "redis"

const since = "0.1.5"

const packDoc = `Seed keys into Redis, and check the keys your services write there: their values, their JSON properties or hash fields, and that they are gone. Every server that speaks Redis's protocol works: Redis, Valkey, Dragonfly, KeyDB, Garnet.

Register the server once, usually in the ` + "`Background`" + `; every Redis step of the scenario uses it:

` + "```gherkin" + `
Given the cache redis server with the following properties:
  | url | redis://localhost:6379/0 |
` + "```" + `

- **A key is shown as JSON,** whatever its type: a string holding JSON as that JSON (other text as a string), a hash as an object of its fields, a list as an array, a set as a sorted array, and a sorted set as an object of its members and their scores. ` + "`has the following properties:`" + ` reads paths into it.
- **Checks wait** for the key to be as they say, 10 seconds unless ` + "`within {duration}`" + ` says otherwise.
- **Scenarios share the server,** so each seeds and checks keys of its own, named after data unique to it, such as a parcel reference.
- **Secrets stay secret:** a password in the URL, or from ` + "`${env:..}`" + `, is masked in logs and failures.`

// Pack returns the redis pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{Name: Name, Namespace: Name, Doc: packDoc, Steps: steps()}
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".server", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} redis server with the following properties:",
			Doc: "Register the Redis server the steps talk to.\n\n" +
				"- The first server registered is the one the scenario's Redis steps use.\n" +
				"- Values expand `${env:..}` and `${sys:..}`; `${env:..}` values are masked.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Takes: "the server's URL: `redis://user:password@host:6379/0`, or `rediss://` for TLS", Required: true},
					{Name: "database", Takes: "the database number, instead of the URL's"},
				},
			},
			Examples: []string{"Given the cache redis server with the following properties:\n" +
				"  | url | redis://localhost:6379/0 |"},
			Run: register,
		},
		{
			ID: Name + ".seed", Keyword: "Given", Since: since,
			Expr: "a {filepath} redis seed",
			Doc: "Write the keys of a YAML or JSON file to the scenario's Redis server, replacing any keys of those names.\n\n" +
				"The file maps each key to one of `value` (text, or JSON for an object or an array), `hash` (its fields), `list`, " +
				"`set` or `sorted set` (members and their scores), and optionally a `ttl` (like `10m`) after which it expires.",
			Examples: []string{"Given a seeds/quote-cache.yaml redis seed"},
			Run:      func(sc *core.Scenario, a core.Args) error { return seed(sc, a.String(0)) },
		},
		{
			ID: Name + ".value", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {word} redis key has the value {string}",
			Doc: "Check that the key holds that text. The check waits for it: 10 seconds, or `within {duration}`.\n\n" +
				"- The key must be a string key; for the others, check their properties.",
			Examples: []string{"Then within 10s the quote:DE-1:STANDARD:1200 redis key has the value '690'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				key, want := a.String(1), a.String(2)
				return check(sc, a, key, func(k *stored) (bool, string) {
					switch {
					case k == nil:
						return false, fmt.Sprintf("The %s redis key does not exist", key)
					case k.kind != "string":
						return false, fmt.Sprintf("The %s redis key is a %s, not a string: %s", key, k.kind, k.json)
					case k.text != want:
						return false, fmt.Sprintf("The %s redis key has the value %q, not %q", key, k.text, want)
					}
					return true, ""
				})
			},
		},
		{
			ID: Name + ".properties", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the {word} redis key has the following properties:",
			Doc: "Check that the key, shown as JSON, has the properties of the table. The check waits for it: 10 seconds, or `within {duration}`.\n\n" +
				"- A string key holding JSON is that JSON; a hash is an object of its fields; a list an array; a set a sorted array; " +
				"a sorted set an object of its members and their scores.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note: "Each row is a path into the key's JSON (a field name, a dotted path or a JSONPath) and the value it has, " +
					"compared as text: `null` for null and `undefined` for absent.",
			},
			Examples: []string{
				"Then within 10s the quote:DE-1:STANDARD:1200 redis key has the following properties:\n" +
					"  | priceCents | 690 |\n  | currency   | EUR |",
				"Then the shop:hawthorn-home redis key has the following properties:\n" +
					"  | tier | gold |",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				rs, err := cloudstep.Conditions(a.Table)
				if err != nil {
					return err
				}
				name := a.String(1)
				var matchErr error
				err = check(sc, a, name, func(k *stored) (bool, string) {
					if k == nil {
						return false, fmt.Sprintf("The %s redis key does not exist", name)
					}
					ok, err := rs.Match(k.json)
					if err != nil {
						matchErr = err
						return true, ""
					}
					if !ok {
						return false, fmt.Sprintf("The %s redis key (a %s) does not have those properties: %s", name, k.kind, k.json)
					}
					return true, ""
				})
				if matchErr != nil {
					return matchErr
				}
				return err
			},
		},
		{
			ID: Name + ".absent", Keyword: "Then", Since: since, Absence: true,
			Expr: "[[within {duration} ]]the {word} redis key does not exist",
			Doc: "Check that the key does not exist, or waits for it to go: 10 seconds, or `within {duration}`, " +
				"for a service that deletes it or lets it expire.",
			Examples: []string{"Then within 10s the tracking:PX-RDS-9603 redis key does not exist"},
			Run: func(sc *core.Scenario, a core.Args) error {
				name := a.String(1)
				return check(sc, a, name, func(k *stored) (bool, string) {
					if k != nil {
						return false, fmt.Sprintf("The %s redis key still exists: %s", name, k.json)
					}
					return true, ""
				})
			},
		},
	}
}

type server struct {
	name, url string
	database  int // -1: the URL's
}

func (s *server) key() string { return s.url + "|" + strconv.Itoa(s.database) }

var servers = core.NewStateKey(Name, func(*core.Scenario) *core.Services[*server] {
	return core.NewServices[*server]("Redis server",
		`No Redis server is registered in this scenario; register one with "the {word} redis server with the following properties:"`).RegisteredBy("the {word} redis server with the following properties:")
}, nil)

func register(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	s := &server{name: a.String(0), database: -1}
	for _, p := range pairs {
		v := strings.TrimSpace(secrets.Expand(sc, p.Value))
		switch p.Key {
		case "url":
			s.url = v
		case "database":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return fmt.Errorf("the redis database is a number, not %q", v)
			}
			s.database = n
		default:
			return fmt.Errorf("unknown redis server property %q (supported: url, database)", p.Key)
		}
	}
	if s.url == "" {
		return errors.New(`the redis server property "url" is required`)
	}
	if u, err := url.Parse(s.url); err == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok {
			secrets.Keep(sc, pw)
		}
	}
	if _, err := options(s); err != nil {
		return secrets.Hide(sc, err)
	}
	if err := servers.Of(sc).Add(s.name, s); err != nil {
		return err
	}
	sc.Log("registered the %s redis server: %s", s.name, secrets.Mask(sc, redact(s.url)))
	return nil
}

func options(s *server) (*goredis.Options, error) {
	o, err := goredis.ParseURL(s.url)
	if err != nil {
		// The client's error quotes the URL, password and all.
		return nil, fmt.Errorf("the redis server's url is redis://user:password@host:port/database or rediss://...: %s",
			strings.ReplaceAll(err.Error(), s.url, redact(s.url)))
	}
	if s.database >= 0 {
		o.DB = s.database
	}
	o.DialTimeout = 10 * time.Second
	return o, nil
}

// userinfo is a URL's user and password, even in a URL that does not parse.
var userinfo = regexp.MustCompile(`//([^/@:]*):[^/@]*@`)

// redact leaves a URL's password out.
func redact(raw string) string { return userinfo.ReplaceAllString(raw, "//$1:xxxxx@") }

// client is the run's client for the scenario's server.
func client(sc *core.Scenario) (*goredis.Client, error) {
	s, err := servers.Of(sc).Default()
	if err != nil {
		return nil, err
	}
	return core.Cached(sc.Suite(), Name+"/client/"+s.key(), func() (*goredis.Client, error) {
		o, err := options(s)
		if err != nil {
			return nil, err
		}
		c := goredis.NewClient(o)
		sc.Suite().OnClose(func(context.Context) error { return c.Close() })
		return c, nil
	})
}

// ---- seeds ----

func seed(sc *core.Scenario, file string) error {
	c, err := client(sc)
	if err != nil {
		return err
	}
	s, err := cloudstep.ReadSeed(sc, file)
	if err != nil {
		return err
	}
	ctx := sc.Context()
	_, err = c.TxPipelined(ctx, func(p goredis.Pipeliner) error {
		for _, name := range s.Names {
			if err := write(ctx, p, name, s.Items[name]); err != nil {
				return fmt.Errorf("%s: %s: %w", file, name, err)
			}
		}
		return nil
	})
	if err != nil {
		return secrets.Hide(sc, err)
	}
	sc.Log("seeded %d redis keys from %s", len(s.Names), file)
	return nil
}

var kinds = []string{"value", "hash", "list", "set", "sorted set"}

// write queues writing one key of a seed.
func write(ctx context.Context, p goredis.Pipeliner, name string, item any) error {
	entry, ok := item.(map[string]any)
	if !ok {
		return fmt.Errorf("a key maps to one of %s, and optionally a ttl", strings.Join(kinds, ", "))
	}
	var kind string
	for k := range entry {
		switch {
		case k == "ttl":
		case contains(kinds, k):
			if kind != "" {
				return fmt.Errorf("a key is one of %s, not both a %s and a %s", strings.Join(kinds, ", "), kind, k)
			}
			kind = k
		default:
			return fmt.Errorf("unknown %q (a key maps to one of %s, and optionally a ttl)", k, strings.Join(kinds, ", "))
		}
	}
	if kind == "" {
		return fmt.Errorf("a key maps to one of %s", strings.Join(kinds, ", "))
	}
	p.Del(ctx, name)
	v := entry[kind]
	switch kind {
	case "value":
		p.Set(ctx, name, text(v), 0)
	case "hash":
		fields, ok := v.(map[string]any)
		if !ok {
			return errors.New("a hash maps field names to their values")
		}
		args := make([]any, 0, 2*len(fields))
		for _, f := range sortedKeys(fields) {
			args = append(args, f, text(fields[f]))
		}
		p.HSet(ctx, name, args...)
	case "list", "set":
		items, ok := v.([]any)
		if !ok {
			return fmt.Errorf("a %s is a list of its members", kind)
		}
		members := make([]any, len(items))
		for i, m := range items {
			members[i] = text(m)
		}
		if kind == "list" {
			p.RPush(ctx, name, members...)
		} else {
			p.SAdd(ctx, name, members...)
		}
	case "sorted set":
		scores, ok := v.(map[string]any)
		if !ok {
			return errors.New("a sorted set maps its members to their scores")
		}
		zs := make([]goredis.Z, 0, len(scores))
		for _, m := range sortedKeys(scores) {
			f, err := strconv.ParseFloat(text(scores[m]), 64)
			if err != nil {
				return fmt.Errorf("the score of %s is a number, not %s", m, text(scores[m]))
			}
			zs = append(zs, goredis.Z{Member: m, Score: f})
		}
		p.ZAdd(ctx, name, zs...)
	}
	if ttl, ok := entry["ttl"]; ok {
		d, err := time.ParseDuration(text(ttl))
		if err != nil || d <= 0 {
			return fmt.Errorf("the ttl is a duration like 10m, not %s", text(ttl))
		}
		p.PExpire(ctx, name, d)
	}
	return nil
}

// text is a seed value as Redis keeps it: text as it is, JSON otherwise.
func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case nil:
		return ""
	}
	return cloudstep.JSON(v)
}

// ---- checks ----

// stored is a key as the checks see it.
type stored struct {
	kind string // string, hash, list, set, sorted set
	text string // a string key's text
	json string // the key shown as JSON
}

// read reads a key, or nil when it does not exist.
func read(ctx context.Context, c *goredis.Client, name string) (*stored, error) {
	t, err := c.Type(ctx, name).Result()
	if err != nil {
		return nil, err
	}
	var v any
	k := &stored{kind: t}
	switch t {
	case "none":
		return nil, nil
	case "string":
		s, err := c.Get(ctx, name).Result()
		if errors.Is(err, goredis.Nil) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		k.text = s
		if json.Valid([]byte(s)) {
			k.json = s
			return k, nil
		}
		v = s
	case "hash":
		v, err = c.HGetAll(ctx, name).Result()
	case "list":
		v, err = c.LRange(ctx, name, 0, -1).Result()
	case "set":
		var ms []string
		ms, err = c.SMembers(ctx, name).Result()
		sort.Strings(ms)
		v = ms
	case "zset":
		k.kind = "sorted set"
		var zs []goredis.Z
		zs, err = c.ZRangeWithScores(ctx, name, 0, -1).Result()
		scores := map[string]float64{}
		for _, z := range zs {
			scores[fmt.Sprint(z.Member)] = z.Score
		}
		v = scores
	default:
		return nil, fmt.Errorf("the %s redis key is a %s, which the checks do not read", name, t)
	}
	if err != nil {
		return nil, err
	}
	k.json = cloudstep.JSON(v)
	return k, nil
}

// check waits until the key is as ok says.
func check(sc *core.Scenario, a core.Args, name string, ok func(*stored) (bool, string)) error {
	c, err := client(sc)
	if err != nil {
		return err
	}
	return secrets.Hide(sc, cloudstep.Poll(sc, cloudstep.Wait(a, 0), func() (bool, string, error) {
		k, err := read(sc.Context(), c, name)
		if err != nil {
			return false, "", err
		}
		done, why := ok(k)
		return done, why, nil
	}))
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
