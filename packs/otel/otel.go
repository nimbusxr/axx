// Package otel is the otel pack: what the services under test report through
// OpenTelemetry, their traces, metrics and logs, read from what a real Collector
// writes. The pack opens no port and speaks no OTLP: the Collector does, and the
// services' telemetry reaches it whether axx is running or not.
package otel

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// Name is the pack's name.
const Name = "otel"

const since = "0.3.1"

// defaultWait is how long a check of what must arrive waits with no `within`.
const defaultWait = 10 * time.Second

const packDoc = `Check what your services report through OpenTelemetry: the spans of what a scenario did, the logs that came with them, and their metrics.

Your service exports OTLP, over gRPC or HTTP, to a real [Collector](https://opentelemetry.io/docs/collector/) in your test environment, and the Collector's ` + "`file`" + ` exporter writes it where axx reads it. You can run that Collector with no tests at all, and send it on to Jaeger or Grafana as well. Register what it writes, usually in the ` + "`Background`" + `:

` + "```gherkin" + `
Given the parcels-api telemetry with the following properties:
  | url     | file://.axx/otel/telemetry.jsonl |
  | service | parcels-api                      |
` + "```" + `

- **` + "`the trace`" + ` is this scenario's own,** whatever runs beside it: once telemetry is registered, the requests the scenario sends carry one ` + "`traceparent`" + ` header, with a trace ID of the scenario's own, and a service that follows W3C Trace Context puts its spans and logs in that trace. A request that sets its own ` + "`traceparent`" + ` keeps it.
- **Metrics are shared.** A scenario sees what was counted since it registered the telemetry, whether the metric is cumulative or delta, but scenarios running at the same time count together: give a check attributes that are its scenario's own.
- **Checks wait.** A check that something arrived waits 10 seconds, or ` + "`within {duration}`" + `. A check that nothing did looks once, so put it after one that waits. A count higher than the check's fails at once.
- **Metrics take their time:** SDKs export them every 60 seconds by default, so set ` + "`OTEL_METRIC_EXPORT_INTERVAL`" + `, in milliseconds, in the service's test environment.`

// Pack returns the otel pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc,
		Params: []core.ParamType{
			{
				Name: "telemetry", Regexps: []string{`([^\s]+)`},
				Doc:      "the name of telemetry registered in the scenario",
				Examples: []string{"parcels-api"},
				Transform: func(sc *core.Scenario, name string, _ []*string) (any, error) {
					return Context(sc).telemetry.Get(name)
				},
			},
			{
				Name: "gaugeValue", Regexps: []string{`-?\d+(?:\.\d+)?`},
				Doc:      "a gauge's value, of its type: `4` an integer, and `4.0` a decimal",
				Examples: []string{"4", "4.5"},
			},
		},
		Steps: steps(),
	}
}

var spanTable = &core.TableDoc{
	Columns: []string{"field", "value"},
	Rows: []core.TableRow{
		{Name: "name", Takes: "the span's name, all of it"},
		{Name: "status", Takes: "`OK`, `ERROR` or `UNSET`"},
		{Name: "kind", Takes: "`SERVER`, `CLIENT`, `INTERNAL`, `PRODUCER` or `CONSUMER`"},
		{Name: "service", Takes: "the service that reported it"},
		{Name: "attribute <key>", Takes: "an attribute's value, of its type: its key after `attribute `"},
	},
	Values: core.JSONValues, Undefined: true,
}

var logTable = &core.TableDoc{
	Columns: []string{"field", "value"},
	Rows: []core.TableRow{
		{Name: "severity", Takes: "its severity: `INFO`, `WARN`, `ERROR`..."},
		{Name: "body", Takes: "text its body contains"},
		{Name: "service", Takes: "the service that reported it"},
		{Name: "attribute <key>", Takes: "an attribute's value, of its type: its key after `attribute `"},
	},
	Values: core.JSONValues, Undefined: true,
}

var metricTable = &core.TableDoc{
	Columns: []string{"field", "value"},
	Rows: []core.TableRow{
		{Name: "attribute <key>", Takes: "the value of an attribute of the data points to count: its key after `attribute `"},
		{Name: "service", Takes: "the service that reported them"},
	},
	Values: core.JSONValues, Undefined: true,
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".telemetry", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} telemetry with the following properties:",
			Doc: "Register what a Collector's `file` exporter writes.\n\n" +
				"- From here on, the requests the scenario sends carry this scenario's `traceparent` header.\n" +
				"- Metrics are counted from here on.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Takes: "the exporter's file: `file://`, relative to the directory of axx.yaml or absolute", Required: true},
					{Name: "service", Takes: "the `service.name` whose metrics the steps that name this telemetry look at; the others look at every service's"},
				},
			},
			Examples: []string{"Given the parcels-api telemetry with the following properties:\n" +
				"  | url     | file://.axx/otel/telemetry.jsonl |\n" +
				"  | service | parcels-api                      |"},
			Run: register,
		},
		{
			ID: Name + ".trace.span", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the trace has a span where:",
			Doc: "Check that this scenario's trace has a span with those values.\n\n" +
				"- The check waits for it: 10 seconds, or `within {duration}`.\n" +
				"- `undefined` as an attribute's value is a span without that attribute.",
			Table: spanTable,
			Examples: []string{"Then within 10s the trace has a span where:\n" +
				"  | name                 | POST /api/parcels |\n" +
				"  | attribute http.route | /api/parcels      |"},
			Run: traceHasSpan,
		},
		{
			ID: Name + ".trace.nospan", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the trace has no span where:",
			Doc: "Check that this scenario's trace has no span with those values.\n\n" +
				"- It looks once, at what has arrived: put it after a check that waits.",
			Table:    spanTable,
			Examples: []string{"Then the trace has no span where:\n  | status | ERROR |"},
			Run:      traceHasNoSpan,
		},
		{
			ID: Name + ".trace.child", Keyword: "Then", Since: since,
			Expr:     "the {string} span is a child of the {string} span",
			Doc:      "Check that a span of this scenario's trace has the other for its parent, waiting for them for 10 seconds.",
			Examples: []string{`Then the "store parcel" span is a child of the "POST /api/parcels" span`},
			Run:      spanIsChild,
		},
		{
			ID: Name + ".trace.took", Keyword: "Then", Since: since,
			Expr: "the {string} span took less than {duration}",
			Doc: "Check how long a span of this scenario's trace took, waiting for it for 10 seconds.\n\n" +
				"- Every span of that name in the trace must have.",
			Examples: []string{`Then the "POST /api/parcels" span took less than 500ms`},
			Run:      spanTook,
		},
		{
			ID: Name + ".trace.log", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the trace has a log where:",
			Doc: "Check that this scenario's trace has a log record with those values.\n\n" +
				"- The check waits for it: 10 seconds, or `within {duration}`.\n" +
				"- A record is in the trace when the service logged it with the span's context.",
			Table:    logTable,
			Examples: []string{"Then within 10s the trace has a log where:\n  | severity | ERROR |"},
			Run:      traceHasLog,
		},
		{
			ID: Name + ".counted", Keyword: "Then", Arg: core.ArgOptional, Since: since,
			Expr: "[[within {duration} ]]the {telemetry} telemetry counted {int} {word}[[ where:]]",
			Doc: "Check what a counter has counted since the telemetry was registered, cumulative or delta.\n\n" +
				"- The check waits for that many: 10 seconds, or `within {duration}`; more fails at once.\n" +
				"- With `0` it looks once, or at the end of `within {duration}`.",
			Table: metricTable,
			Examples: []string{
				"Then within 30s the parcels-api telemetry counted 1 parcels_registered where:\n  | attribute sender | juniper-and-jay |",
				"Then the parcels-api telemetry counted 0 parcels_rejected",
			},
			Run: counted,
		},
		{
			ID: Name + ".recorded", Keyword: "Then", Arg: core.ArgOptional, Since: since,
			Expr: "[[within {duration} ]]the {telemetry} telemetry recorded {int} {word} values[[ where:]]",
			Doc: "Check how many values a histogram has recorded since the telemetry was registered.\n\n" +
				"- The check waits for that many: 10 seconds, or `within {duration}`; more fails at once.",
			Table:    metricTable,
			Examples: []string{"Then within 30s the parcels-api telemetry recorded 1 parcel_weight values where:\n  | attribute sender | juniper-and-jay |"},
			Run:      recorded,
		},
		{
			ID: Name + ".gauge", Keyword: "Then", Arg: core.ArgOptional, Since: since,
			Expr: "[[within {duration} ]]the {telemetry} telemetry's {word} gauge is {gaugeValue}[[ where:]]",
			Doc: "Check the latest value a gauge reported since the telemetry was registered.\n\n" +
				"- The check waits for that value: 10 seconds, or `within {duration}`.\n" +
				"- The value keeps its type: `4` is an integer, and `4.0` a decimal.",
			Table:    metricTable,
			Examples: []string{"Then within 30s the parcels-api telemetry's last_parcel_weight gauge is 1200 where:\n  | attribute sender | juniper-and-jay |"},
			Run:      gauge,
		},
		{
			ID: Name + ".contains.not", Keyword: "Then", Since: since, TextArgs: []int{0},
			Expr: "the telemetry does not contain {string}",
			Doc: "Check that nothing the services reported since the telemetry was registered contains that text: what must stay out of your telemetry, like a person's name.\n\n" +
				"- It looks once, at what has arrived, and fails when nothing has: put it after a check that waits.",
			Examples: []string{"Then the telemetry does not contain 'Marta Keller'"},
			Run:      notContains,
		},
	}
}

// within is the wait of a step whose first argument is `within {duration}`.
func within(a core.Args) time.Duration {
	if a.Present(0) {
		return a.Value(0).(time.Duration)
	}
	return defaultWait
}

// waitFor polls until done says so or the time is up. It reads the files first each time.
func waitFor(sc *core.Scenario, d time.Duration, st *ScenarioContext, done func() (bool, error)) error {
	deadline := time.Now().Add(d)
	for {
		if err := st.refresh(); err != nil {
			return err
		}
		ok, err := done()
		if err != nil || ok {
			return err
		}
		if time.Now().Add(pollEvery).After(deadline) {
			return errTimeUp
		}
		select {
		case <-sc.Context().Done():
			return sc.Context().Err()
		case <-time.After(pollEvery):
		}
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func seenList(lines []string) string {
	if len(lines) == 0 {
		return "nothing"
	}
	const most = 20
	if len(lines) > most {
		more := len(lines) - most
		lines = append(lines[:most:most], fmt.Sprintf("and %d more", more))
	}
	return "\n  " + strings.Join(lines, "\n  ")
}
