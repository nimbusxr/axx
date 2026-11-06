// Package faults is the faults pack: what the services under test do when a
// connection to something they depend on goes wrong, through real Toxiproxy. It
// works on a connection's bytes, whatever the protocol, so it cannot pick a
// request or a row: those are the mock's and the sql pack's.
package faults

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// Name is the pack's name.
const Name = "faults"

const since = "0.3.1"

const packDoc = `Check what your services do when a connection goes wrong: cut off, slow, hanging, reset, or closing halfway through an answer.

Your service reaches the dependency through [Toxiproxy](https://github.com/Shopify/toxiproxy), which runs in your test environment like any other service, and axx tells it what to do to a connection. Declare the connections in Toxiproxy's own config, since your service needs their ports before it starts, and register Toxiproxy once, usually in the ` + "`Background`" + `:

` + "```gherkin" + `
Given the parcels fault proxy with the following properties:
  | url | http://localhost:8474 |

Scenario: A cached tracking view answers while the database is away
  Given the parcels-db connection is cut off
  ...
` + "```" + `

- **It works on any TCP connection:** PostgreSQL, Oracle, Kafka, Redis, an HTTP API. A connection is the name of a proxy in Toxiproxy's config.
- **It sees bytes, not requests,** so it cannot pick a URL, a query or a row. To fail one request, stub it with the mock pack's WireMock mappings; to fail one row's insert, use the sql pack's triggers.
- **Connections already open are hit too,** which is what a service's connection pool holds. A fault that counts bytes counts theirs from when its step runs.
- **Each fault can say more,** at its end, in this order:
  - ` + "`coming back`" + ` (the default) or ` + "`going out`" + `: the direction it applies to. With ` + "`coming back`" + `, what the service sends still arrives and only the answer is lost, so a write can be made though the service never hears so; a driver that checks a connection before it uses one waits on that check instead. With ` + "`going out`" + `, nothing the service sends arrives.
  - ` + "`on 30% of connections`" + `: each new connection has the fault or does not, by chance.
  - ` + "`for 5s`" + `: it ends by itself. A request step waits for its answer, so no restore step can run during it: this is how a scenario tests a service retrying through an outage.
- **A connection cut off proves what did not use it:** an answer that still comes, with the database cut off, came from somewhere else, like a cache.
- **A scenario undoes only what it did,** when it ends or at ` + "`is restored`" + `. Scenarios running at the same time share the connection and its faults: ` + "`run.exclusiveUses`" + ` in axx.yaml can run the scenarios that use this pack alone.`

// Pack returns the faults pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc,
		Params: []core.ParamType{
			{
				Name: "connection", Regexps: []string{`([^\s]+)`},
				Doc:      "a connection: the name of a proxy in Toxiproxy's config",
				Examples: []string{"parcels-db", "cache"},
				Transform: func(sc *core.Scenario, name string, _ []*string) (any, error) {
					return Context(sc).Connection(name)
				},
			},
			{
				Name: "dataDirection", Regexps: []string{`coming back|going out`},
				Doc:      "the direction of a connection's data a fault applies to: `coming back` to the service, or `going out` from it",
				Examples: []string{"coming back", "going out"},
			},
		},
		Steps: steps(),
	}
}

// options are what every toxic's step takes at its end.
const options = "[[ {dataDirection}]][[ on {int}% of connections]][[ for {duration}]]"

// fault is a step that gives a connection one of Toxiproxy's toxics.
type fault struct {
	id, tail, doc string
	examples      []string
	toxic         string
	attributes    func(core.Args) (map[string]any, error)
}

// millis is the step's first argument after its connection, a duration, in milliseconds.
func millis(a core.Args) int { return int(a.Value(1).(time.Duration).Milliseconds()) }

func percent(a core.Args, i int) (int, error) {
	n := a.Int(i)
	if n < 0 || n > 100 {
		return 0, fmt.Errorf("%d%% is not a share of anything: write 0%% to 100%%", n)
	}
	return n, nil
}

func fixed(attrs map[string]any) func(core.Args) (map[string]any, error) {
	return func(core.Args) (map[string]any, error) { return attrs, nil }
}

func faultSteps() []fault {
	return []fault{
		{
			id: "faults.slowed", tail: "is slowed by {duration}", toxic: "latency",
			doc:      "Delay the connection's data by that long, each way it is applied. A new connection waits several times over, for each exchange it starts with.",
			examples: []string{"Given the parcels-db connection is slowed by 2s", "Given the cache connection is slowed by 200ms going out on 30% of connections"},
			attributes: func(a core.Args) (map[string]any, error) {
				return map[string]any{"latency": millis(a), "jitter": 0}, nil
			},
		},
		{
			id: "faults.limited", tail: `is limited to {int} KB\/s`, toxic: "bandwidth",
			doc:      "Let the connection's data through at that rate, in thousands of bytes a second.",
			examples: []string{"Given the parcels-db connection is limited to 100 KB/s"},
			attributes: func(a core.Args) (map[string]any, error) {
				return map[string]any{"rate": a.Int(1)}, nil
			},
		},
		{
			id: "faults.hangs", tail: "hangs", toxic: "timeout",
			doc:        "Hold the connection's data, and answer nothing, until the fault ends: the service waits as long as its own timeout lets it.",
			examples:   []string{"Given the parcels-db connection hangs", "Given the parcels-db connection hangs coming back", "Given the shops connection hangs for 10s"},
			attributes: fixed(map[string]any{"timeout": 0}),
		},
		{
			id: "faults.hangs.closes", tail: "hangs for {duration}, then closes", toxic: "timeout",
			doc:      "Hold the connection's data for that long, then close the connection.",
			examples: []string{"Given the parcels-db connection hangs for 5s, then closes"},
			attributes: func(a core.Args) (map[string]any, error) {
				return map[string]any{"timeout": millis(a)}, nil
			},
		},
		{
			id: "faults.reset", tail: "is reset", toxic: "reset_peer",
			doc:        "Reset the connection as data arrives, which the service sees as a connection reset by its peer.",
			examples:   []string{"Given the parcels-db connection is reset"},
			attributes: fixed(map[string]any{"timeout": 0}),
		},
		{
			id: "faults.reset.after", tail: "is reset after {duration}", toxic: "reset_peer",
			doc:      "Reset the connection that long after data arrives.",
			examples: []string{"Given the parcels-db connection is reset after 1s"},
			attributes: func(a core.Args) (map[string]any, error) {
				return map[string]any{"timeout": millis(a)}, nil
			},
		},
		{
			id: "faults.slowclose", tail: "takes {duration} to close", toxic: "slow_close",
			doc:      "Delay the close of the connection by that long. What the connection carries before it closes is not delayed.",
			examples: []string{"Given the parcels-db connection takes 2s to close"},
			attributes: func(a core.Args) (map[string]any, error) {
				return map[string]any{"delay": millis(a)}, nil
			},
		},
		{
			id: "faults.pieces", tail: "breaks into {int}-byte pieces, {duration} apart", toxic: "slicer",
			doc:      "Send the connection's data in pieces of that many bytes, that long apart, which tests a service that reads a whole answer in one read.",
			examples: []string{"Given the parcels-db connection breaks into 100-byte pieces, 1ms apart"},
			attributes: func(a core.Args) (map[string]any, error) {
				return map[string]any{
					"average_size": a.Int(1), "size_variation": 0,
					"delay": int(a.Value(2).(time.Duration).Microseconds()),
				}, nil
			},
		},
		{
			id: "faults.closes.after", tail: "closes after {int} KB", toxic: "limit_data",
			doc:      "Close the connection once that many thousands of bytes have passed through it, which leaves an answer cut off partway. A connection already open counts them from when the step runs.",
			examples: []string{"Given the parcels-db connection closes after 50 KB"},
			attributes: func(a core.Args) (map[string]any, error) {
				return map[string]any{"bytes": a.Int(1) * 1000}, nil
			},
		},
		{
			id: "faults.closes.after.bytes", tail: "closes after {int} bytes", toxic: "limit_data",
			doc:      "Close the connection once that many bytes have passed through it. A connection already open counts them from when the step runs, so a few hundred cut off even a short answer on a connection a service's pool holds.",
			examples: []string{"Given the parcels-db connection closes after 100 bytes"},
			attributes: func(a core.Args) (map[string]any, error) {
				return map[string]any{"bytes": a.Int(1)}, nil
			},
		},
		{
			id: "faults.loses", tail: "loses {int}% of its data", toxic: "packet_loss",
			doc:      "Drop that share of the connection's data, by chance. The service gets no error: what it was waiting for just does not come.",
			examples: []string{"Given the parcels-db connection loses 10% of its data"},
			attributes: func(a core.Args) (map[string]any, error) {
				n, err := percent(a, 1)
				if err != nil {
					return nil, err
				}
				return map[string]any{"loss_rate": float64(n) / 100}, nil
			},
		},
	}
}

func steps() []core.StepDef {
	out := []core.StepDef{
		{
			ID: Name + ".proxy", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} fault proxy with the following properties:",
			Doc: "Register the Toxiproxy that holds the connections the steps name.\n\n" +
				"- A connection is found on whichever registered Toxiproxy has it.\n" +
				"- Values expand `${env:..}` and `${sys:..}`; the token is masked.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Takes: "where Toxiproxy's API is, like `http://localhost:8474`", Required: true},
					{Name: "token", Takes: "the token of a Toxiproxy started with `TOXIPROXY_AUTH_TOKEN`, like `${env:TOXIPROXY_TOKEN}`"},
				},
			},
			Examples: []string{"Given the parcels fault proxy with the following properties:\n" +
				"  | url | http://localhost:8474 |"},
			Run: register,
		},
		{
			ID: Name + ".cutoff", Keyword: "Given", Since: since,
			Expr: "the {connection} connection is cut off[[ for {duration}]]",
			Doc: "Take the connection away: connections open now close, and new ones are refused.\n\n" +
				"- With `for {duration}` it comes back by itself, which is how a scenario tests a service retrying through an outage.",
			Examples: []string{"Given the parcels-db connection is cut off", "Given the parcels-db connection is cut off for 5s"},
			Run: func(sc *core.Scenario, a core.Args) error {
				conn := a.Value(0).(*Connection)
				var d time.Duration
				if a.Present(1) {
					d = a.Value(1).(time.Duration)
				}
				return conn.CutOff(sc, d)
			},
		},
	}
	for _, f := range faultSteps() {
		out = append(out, core.StepDef{
			ID: f.id, Keyword: "Given", Since: since,
			Expr:     "the {connection} connection " + f.tail + options,
			Doc:      f.doc + "\n\n" + optionsDoc,
			Examples: f.examples,
			Run:      f.run,
		})
	}
	return append(out,
		core.StepDef{
			ID: Name + ".toxic", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {connection} connection has a(n) {word} toxic with the following attributes:",
			Doc: "Give the connection any toxic Toxiproxy has by that type, one compiled into your own Toxiproxy included, with the attributes Toxiproxy's documentation gives it.\n\n" +
				"- `stream` and `toxicity` are the toxic's own: `upstream` or `downstream`, and the share of connections it applies to, from `0.0` to `1.0`.\n" +
				"- Any other row is one of its attributes, of the type Toxiproxy reads it as.",
			Table: &core.TableDoc{
				Columns: []string{"attribute", "value"},
				Note:    "Each row names an attribute of the toxic and its value.",
				Values:  core.JSONValues,
			},
			Examples: []string{"Given the parcels-db connection has a latency toxic with the following attributes:\n" +
				"  | latency  | 2000       |\n" +
				"  | jitter   | 500        |\n" +
				"  | stream   | \"upstream\" |\n" +
				"  | toxicity | 0.5        |"},
			Run: customToxic,
		},
		core.StepDef{
			ID: Name + ".restored", Keyword: "When", Since: since,
			Expr:     "the {connection} connection is restored",
			Doc:      "End the faults this scenario gave the connection. A scenario's faults end by themselves when it does; another scenario's are left alone.",
			Examples: []string{"When the parcels-db connection is restored"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return a.Value(0).(*Connection).Restore(sc)
			},
		},
	)
}

const optionsDoc = "- `coming back` (the default) or `going out` is the direction it applies to.\n" +
	"- `on 30% of connections` applies it to that share of connections, by chance.\n" +
	"- `for 5s` ends it by itself after that long.\n" +
	"- It also hits the connections open now, and ends when the scenario does."

// run gives the step's connection its toxic. Its options' arguments come after
// the ones of its own text.
func (f fault) run(sc *core.Scenario, a core.Args) error {
	attrs, err := f.attributes(a)
	if err != nil {
		return err
	}
	first := strings.Count(f.tail, "{") + 1
	t := Toxic{Type: f.toxic, Stream: StreamDownstream, Toxicity: 1, Attributes: attrs}
	if a.Present(first) && a.String(first) == "going out" {
		t.Stream = StreamUpstream
	}
	if a.Present(first + 1) {
		n, err := percent(a, first+1)
		if err != nil {
			return err
		}
		t.Toxicity = float64(n) / 100
	}
	var d time.Duration
	if a.Present(first + 2) {
		d = a.Value(first + 2).(time.Duration)
	}
	return a.Value(0).(*Connection).Add(sc, t, d)
}

func customToxic(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	t := Toxic{Type: a.String(1), Stream: StreamDownstream, Toxicity: 1, Attributes: map[string]any{}}
	for _, p := range pairs {
		v, err := core.ParseValue(p.Value, p.Null, false)
		if err != nil {
			return fmt.Errorf("%s: %w", p.Key, err)
		}
		var value any
		if err := json.Unmarshal([]byte(v.JSON()), &value); err != nil {
			return fmt.Errorf("%s: %w", p.Key, err)
		}
		switch p.Key {
		case "stream":
			s, ok := value.(string)
			if !ok || s != StreamUpstream && s != StreamDownstream {
				return fmt.Errorf(`stream is "upstream" or "downstream", not %s`, p.Value)
			}
			t.Stream = s
		case "toxicity":
			n, ok := value.(float64)
			if !ok || n < 0 || n > 1 {
				return fmt.Errorf("toxicity is a share of connections from 0.0 to 1.0, not %s", p.Value)
			}
			t.Toxicity = n
		default:
			t.Attributes[p.Key] = value
		}
	}
	return a.Value(0).(*Connection).Add(sc, t, 0)
}
