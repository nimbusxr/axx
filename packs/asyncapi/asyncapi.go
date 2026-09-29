// Package asyncapi is the asyncapi pack: the messages a scenario sends, and
// the messages its checks find, checked against the AsyncAPI document their
// broker's registration names.
package asyncapi

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/contract"
	"github.com/nimbusxr/axx/internal/oaslevel"
	"github.com/nimbusxr/axx/internal/schemadoc"
)

// Name is the pack's name.
const Name = "asyncapi"

const since = "0.1.5"

const packDoc = `Check messages against an AsyncAPI document: every message a scenario sends, which catches test data that drifted from the contract, and every message a check finds, which catches a service that broke it.

A broker's registration names the document in an ` + "`asyncapi`" + ` row, a file of the project or a URL:

` + "```gherkin" + `
Given the parcels mqtt broker with the following properties:
  | url      | mqtt://localhost:1883 |
  | asyncapi | asyncapi.yaml         |
` + "```" + `

The row works on every messaging registration: Kafka services, AMQP, MQTT and NATS brokers, AWS accounts (SQS and SNS), Google Cloud projects (Pub/Sub), Service Bus namespaces, WebSockets and event streams. Without this pack in ` + "`axx-packs.yaml`" + `, the row fails the registration.

- **Documents:** AsyncAPI 2.x (2.6 and earlier) and 3.x, in YAML or JSON, with ` + "`$ref`" + `s to other files and URLs.
- **Channels:** a message belongs to the channel whose address is the message's topic, queue, subject, exchange or routing key, or the path of a WebSocket or an event stream. A ` + "`{parameter}`" + ` in an address stands for any text without a slash: ` + "`depots/{depot}/scans`" + ` is the channel of ` + "`depots/LEJ/scans`" + `. When channels say which servers they are on, only the servers of the message's protocol count.
- **Messages:** a message must be one of its channel's: its payload and headers follow that message's schemas, and its content type, when it has one, is the message's. When a channel has several messages, the failure names the one the message comes closest to.
- **Schemas:** JSON Schema (draft 7 unless the schema says otherwise, and the AsyncAPI schema format), or Avro, whose payloads are in Avro's JSON encoding. Payloads are JSON unless their content type says YAML or text.

Findings have keys and levels, like the REST pack's OpenAPI findings:

| Key | Found when |
|---|---|
| ` + "`validation.channel.unknown`" + ` | the document has no channel for the message |
| ` + "`validation.message.contentType`" + ` | the message's content type is not its message's |
| ` + "`validation.message.payload.format`" + ` | the payload is not JSON (or YAML) |
| ` + "`validation.message.payload.schema.<keyword>`" + ` | the payload breaks a JSON Schema keyword (` + "`required`" + `, ` + "`type`" + `, ` + "`enum`" + `...), or ` + "`avro`" + ` its Avro schema |
| ` + "`validation.message.headers.schema.<keyword>`" + ` | the headers break a keyword of their schema |

- **Levels:** ` + "`ERROR`" + ` (or ` + "`FAIL`" + `) fails the step, ` + "`WARN`" + ` and ` + "`INFO`" + ` log the finding, ` + "`IGNORE`" + ` drops it. Every finding is an ` + "`ERROR`" + ` unless ` + "`packs.asyncapi.levels`" + ` in axx.yaml, or the scenario's ` + "`the AsyncAPI validation levels are:`" + `, says otherwise.
- **Keys cover the keys below them:** ` + "`validation.message.payload`" + ` sets every payload finding; the most specific key set wins.`

// Pack returns the asyncapi pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

var _ core.Initializer = pack{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Steps: steps(),
		ConfigSchema: []byte(configSchema),
	}
}

// Init makes the pack the checker of the run's contracts.
func (pack) Init(_ context.Context, s *core.Suite) error {
	if _, err := settingsFor(s); err != nil {
		return err
	}
	contract.Provide(s, open)
	return nil
}

const configSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "levels": {
      "type": "object",
      "description": "Validation key -> ERROR (or FAIL), WARN, INFO or IGNORE, for every scenario.",
      "additionalProperties": {"type": "string", "enum": ["ERROR", "FAIL", "WARN", "INFO", "IGNORE"]}
    }
  }
}`

// Config is the pack's section of axx.yaml.
type Config struct {
	// Levels maps validation keys to ERROR (or FAIL), WARN, INFO or IGNORE.
	Levels map[string]string `json:"levels,omitempty"`
}

type settings struct {
	levels oaslevel.Levels
	mu     sync.Mutex
	specs  map[string]*specEntry
}

type specEntry struct {
	once sync.Once
	spec *spec
	err  error
}

func settingsFor(s *core.Suite) (*settings, error) {
	return core.Cached(s, Name+"/settings", func() (*settings, error) {
		var c Config
		if err := s.PackConfig(Name, &c); err != nil {
			return nil, err
		}
		levels, err := parseLevels(c.Levels)
		if err != nil {
			return nil, fmt.Errorf("packs.asyncapi.levels: %w", err)
		}
		return &settings{levels: levels, specs: map[string]*specEntry{}}, nil
	})
}

// levelKeys are the keys levels can be set on.
var levelKeys = oaslevel.NewKeys([]string{
	"validation.channel.unknown",
	"validation.message.contentType",
	"validation.message.payload.format",
	"validation.message.{part}.schema.{keyword}",
}, map[string][]string{
	"{part}": {"payload", "headers"},
	"{keyword}": {
		"$ref", "additionalItems", "additionalProperties", "allOf", "anyOf", "avro", "const", "contains",
		"contentEncoding", "contentMediaType", "contentSchema", "dependencies", "dependentRequired", "enum",
		"exclusiveMaximum", "exclusiveMinimum", "false", "format", "maxContains", "maxItems", "maxLength",
		"maxProperties", "maximum", "minContains", "minItems", "minLength", "minProperties", "minimum",
		"multipleOf", "not", "oneOf", "pattern", "propertyNames", "required", "type", "uniqueItems", "unknownError",
	},
}, nil).Named("AsyncAPI", "validation.message.payload")

// parseLevels parses a key -> level map, naming the closest keys for an
// unknown one.
func parseLevels(m map[string]string) (oaslevel.Levels, error) {
	out := oaslevel.Levels{}
	for k, v := range m {
		key := strings.TrimSpace(k)
		if err := checkKey(key); err != nil {
			return nil, err
		}
		lv, err := oaslevel.Parse(v)
		if err != nil {
			return nil, fmt.Errorf("invalid AsyncAPI validation level %q for key %q; supported levels: %s", v, k, oaslevel.Supported)
		}
		out[key] = lv
	}
	return out, nil
}

func checkKey(key string) error { return levelKeys.Check(key) }

const levelsNote = "A row is a validation key and its level: `ERROR` (or `FAIL`), `WARN`, `INFO` or `IGNORE`. " +
	"The key must be one the pack reports, or a prefix of such keys, like `validation.message.payload`: " +
	"any other fails the step, which names the closest keys."

func steps() []core.StepDef {
	return []core.StepDef{{
		ID: Name + ".levels", Keyword: "Given", Arg: core.ArgTable, Since: since,
		Expr: "the AsyncAPI validation levels are:",
		Doc: "Set the level of AsyncAPI validation findings for this scenario, on every contract it checks messages against.\n\n" +
			"- A key also sets the keys below it: `validation.message.payload` relaxes " +
			"`validation.message.payload.schema.required` too. The most specific key set wins.\n" +
			"- The rows are merged over `packs.asyncapi.levels` of axx.yaml.\n" +
			"- The pack's documentation lists the keys, and what each level does.",
		Table: &core.TableDoc{Columns: []string{"validation key", "level"}, Note: levelsNote},
		Examples: []string{"Given the AsyncAPI validation levels are:\n" +
			"  | validation.message.payload.schema.required | WARN |"},
		Run: func(sc *core.Scenario, a core.Args) error {
			pairs, err := a.Table.Pairs()
			if err != nil {
				return err
			}
			m := make(map[string]string, len(pairs))
			for _, p := range pairs {
				m[p.Key] = p.Value
			}
			levels, err := parseLevels(m)
			if err != nil {
				return err
			}
			st := scenarioLevels.Of(sc)
			st.mu.Lock()
			st.levels = st.levels.Merge(levels)
			st.mu.Unlock()
			return nil
		},
	}}
}

type levelsState struct {
	mu     sync.Mutex
	levels oaslevel.Levels
}

var scenarioLevels = core.NewStateKey(Name+".levels", func(*core.Scenario) *levelsState {
	return &levelsState{levels: oaslevel.Levels{}}
}, nil)

// open opens the document a registration names, once per run.
func open(sc *core.Scenario, source string) (contract.Checker, error) {
	st, err := settingsFor(sc.Suite())
	if err != nil {
		return nil, err
	}
	u, err := schemadoc.Locate(sc.Suite(), source)
	if err != nil {
		return nil, err
	}
	st.mu.Lock()
	e, ok := st.specs[u]
	if !ok {
		e = &specEntry{}
		st.specs[u] = e
	}
	st.mu.Unlock()
	e.once.Do(func() { e.spec, e.err = load(source, u) })
	if e.err != nil {
		return nil, fmt.Errorf("cannot read the AsyncAPI document %s: %w", source, e.err)
	}
	return checker{spec: e.spec, levels: st.levels}, nil
}

type checker struct {
	spec   *spec
	levels oaslevel.Levels
}

// Check checks a message, failing on the findings at the ERROR level and
// logging the others.
func (c checker) Check(sc *core.Scenario, m contract.Message) error {
	found := c.spec.check(m)
	if len(found) == 0 {
		return nil
	}
	levels := c.levels
	if st, ok := scenarioLevels.Peek(sc); ok {
		st.mu.Lock()
		levels = levels.Merge(st.levels)
		st.mu.Unlock()
	}
	what := "the message the check found"
	if m.Sent {
		what = "the message to send"
	}
	var errs []string
	for _, f := range found {
		switch lv := levels.Resolve(f.key); lv {
		case oaslevel.Error:
			errs = append(errs, "- "+f.key+": "+f.message)
		case oaslevel.Warn, oaslevel.Info:
			sc.Log("AsyncAPI %s %s: %s", lv, f.key, f.message)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return core.Failf("%s breaks %s:\n%s\nTo relax a check, set its key (or a parent key) to WARN, INFO or IGNORE with "+
		"\"Given the AsyncAPI validation levels are:\" or packs.asyncapi.levels in axx.yaml.",
		what, c.spec.source, strings.Join(errs, "\n"))
}
