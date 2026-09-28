package grpc

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/jsonassert"
	"github.com/nimbusxr/axx/internal/secrets"
)

const fieldsNote = "Each row is a path into the request (a field name, a dotted path like `lastScan.location`, or a JSONPath) and its value: " +
	"a value takes the type of its field, double quotes make it text, `null` leaves the field unset and `undefined` removes it."

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".service", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} grpc service with the following properties:",
			Doc:  "Register a gRPC service under a name. Its calls go over one connection per address for the run.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "address", Required: true, Takes: "the service's `host:port`"},
					{Name: "proto", Takes: "its `.proto` file, compiled with its imports, or a descriptor set (`protoc --descriptor_set_out --include_imports`); without it, axx reads the service's server reflection"},
					{Name: "tls", Takes: "whether the connection is TLS, checked against the system's certificates", Values: []string{"true", "false"}, Default: "false"},
					{Name: "header.<name>", Takes: "metadata sent with every call, such as `header.authorization`"},
					{Name: "timeout", Takes: "the deadline of each unary call, like `5s`", Default: "10s"},
				},
				Note: "Values are expanded (`${env:..}`, `${sys:..}`, `${token:..}`); what `${env:..}` references expand to is masked everywhere.",
			},
			Examples: []string{
				"Given the tracking grpc service with the following properties:\n  | address | localhost:8410 |",
				"Given the rating grpc service with the following properties:\n" +
					"  | address              | rating.parcels.example:443 |\n" +
					"  | tls                  | true                       |\n" +
					"  | proto                | protos/rating.proto        |\n" +
					"  | header.authorization | Bearer ${env:RATING_TOKEN} |",
			},
			TableTypes: map[string]string{"proto": "filepath"},
			Run:        register,
		},
		{
			ID: Name + ".call", Keyword: "When", Since: since,
			Expr:     "the {word} method is called on the {word} grpc service",
			Doc:      "Call a method with an empty request message.",
			Examples: []string{"When the parcels.tracking.v1.Tracking/ListDepots method is called on the tracking grpc service"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return callOn(sc, a.String(1), a.String(0), "", nil)
			},
		},
		{
			ID: Name + ".call.fields", Keyword: "When", Arg: core.ArgTable, Since: since,
			Expr:  "the {word} method is called on the {word} grpc service with the following fields:",
			Doc:   "Call a method with a request message built from the table.",
			Table: &core.TableDoc{Columns: []string{"path", "value"}, Note: fieldsNote},
			Examples: []string{"When the parcels.tracking.v1.Tracking/GetParcel method is called on the tracking grpc service with the following fields:\n" +
				"  | reference | PX-GRP-6101 |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return callOn(sc, a.String(1), a.String(0), "", a.Table)
			},
		},
		{
			ID: Name + ".call.file", Keyword: "When", Arg: core.ArgOptional, Since: since,
			Expr:  "the {word} method is called on the {word} grpc service with the {filepath} message[[ and the following fields:]]",
			Doc:   "Call a method with the request message of a JSON file of the project (proto JSON), with the table's fields set into it when the step has one.",
			Table: &core.TableDoc{Columns: []string{"path", "value"}, Note: fieldsNote},
			Examples: []string{
				"When the parcels.tracking.v1.Tracking/GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message",
				"When the parcels.tracking.v1.Tracking/GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message and the following fields:\n" +
					"  | reference | PX-GRP-6102 |",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				withTable := strings.HasSuffix(a.Text, ":")
				switch {
				case withTable && a.Table == nil:
					return errors.New("the fields are missing: add a table (| path | value |)")
				case !withTable && a.Table != nil:
					return errors.New(`say "and the following fields:" to set fields from a table`)
				}
				return callOn(sc, a.String(1), a.String(0), a.String(2), a.Table)
			},
		},
		{
			ID: Name + ".status", Keyword: "Then", Since: since,
			Expr: "[[within {duration} ]]the {word} grpc service answered {word}[[ with a message containing {string}]]",
			Doc: "Check the status the service's last call answered: `OK`, `NOT_FOUND`, `INVALID_ARGUMENT`..., and the text its message contains.\n\n" +
				"- A server stream answers when it ends: the check waits for that, 10 seconds or `within {duration}`.",
			Examples: []string{
				"Then the tracking grpc service answered OK",
				"Then the tracking grpc service answered NOT_FOUND with a message containing 'PX-GRP-6199'",
				"Then within 30s the tracking grpc service answered OK",
			},
			Run: checkStatus,
		},
		{
			ID: Name + ".answer", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {word} grpc service's answer has the following fields:",
			Doc:  "Check the answer of the service's last call, a unary call that answered OK.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note: "Each row is a path into the answer as proto JSON (a field's JSON name, a dotted path like `lastScan.location`, or a JSONPath) and its value, " +
					"compared as text: `null` for null and `undefined` for absent. Fields with their default values (`0`, `\"\"`, `false`) are in the answer.",
			},
			Examples: []string{"Then the tracking grpc service's answer has the following fields:\n" +
				"  | status            | OUT_FOR_DELIVERY |\n" +
				"  | lastScan.location | Leipzig          |"},
			Run: checkAnswer,
		},
		{
			ID: Name + ".metadata", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {word} grpc service's answer has the following metadata:",
			Doc:  "Check the metadata the service's last call answered with: its headers and trailers.",
			Table: &core.TableDoc{
				Columns: []string{"name", "value"},
				Note:    "Each row is a name, in any case, and its value, compared as text: several values are joined with `, `; `undefined` for absent.",
			},
			Examples: []string{"Then the tracking grpc service's answer has the following metadata:\n" +
				"  | x-served-by | tracking-v1 |"},
			Run: checkMetadata,
		},
		{
			ID: Name + ".streamed", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "[[within {duration} ]]the {word} grpc service streamed a message where:",
			Doc: "Check that the service's last call, a server stream, sent a message with those values.\n\n" +
				"- The check waits for it: 10 seconds, or `within {duration}`.\n" +
				"- When the stream has ended, it fails at once, with the status it ended with.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note:    "Each row is a path into the message as proto JSON and its value, compared as text: `null` for null and `undefined` for absent.",
			},
			Examples: []string{"Then within 20s the tracking grpc service streamed a message where:\n" +
				"  | reference | PX-GRP-6103      |\n" +
				"  | status    | OUT_FOR_DELIVERY |"},
			Run: checkStreamed,
		},
	}
}

func callOn(sc *core.Scenario, serviceName, method, file string, t *core.Table) error {
	s, err := get(sc, serviceName)
	if err != nil {
		return err
	}
	return s.invoke(sc, method, file, t)
}

// lastCall is the service's last call in the scenario.
func lastCall(sc *core.Scenario, name string) (*service, *call, error) {
	s, err := get(sc, name)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	c := s.last
	s.mu.Unlock()
	if c == nil {
		return nil, nil, fmt.Errorf("the %s grpc service has not been called in this scenario", name)
	}
	return s, c, nil
}

func checkStatus(sc *core.Scenario, a core.Args) error {
	s, c, err := lastCall(sc, a.String(1))
	if err != nil {
		return err
	}
	want, err := parseCode(a.String(2))
	if err != nil {
		return err
	}
	text := ""
	if a.Present(3) {
		text = secrets.Expand(sc, a.String(3))
	}
	d := cloudstep.Wait(a, 0)
	return secrets.Hide(sc, cloudstep.Poll(sc, d, func() (bool, string, error) {
		st, done := c.result()
		if !done {
			return false, fmt.Sprintf("The %s stream of the %s grpc service has not ended after %s", c.name(), s.name, d), nil
		}
		if st.Code() != want {
			return false, "", core.Fail(fmt.Sprintf("The %s grpc service answered %s to %s, not %s", s.name, statusText(st), c.name(), codeName(want)),
				codeName(want), codeName(st.Code()))
		}
		if text != "" && !strings.Contains(st.Message(), text) {
			return false, "", core.Fail(fmt.Sprintf("The %s grpc service answered %s to %s, whose message does not contain %q", s.name, statusText(st), c.name(), text),
				text, st.Message())
		}
		return true, "", nil
	}))
}

func checkAnswer(sc *core.Scenario, a core.Args) error {
	s, c, err := lastCall(sc, a.String(0))
	if err != nil {
		return err
	}
	if c.stream != nil {
		return fmt.Errorf("%s is a server stream: check its messages with \"the %s grpc service streamed a message where:\"", c.name(), s.name)
	}
	st, _ := c.result()
	c.mu.Lock()
	answer := c.answer
	c.mu.Unlock()
	if answer == nil {
		return core.Failf("The %s grpc service answered %s to %s, with no answer to check", s.name, statusText(st), c.name())
	}
	return secrets.Hide(sc, jsonassert.Properties(string(answer), a.Table, false))
}

func checkMetadata(sc *core.Scenario, a core.Args) error {
	s, c, err := lastCall(sc, a.String(0))
	if err != nil {
		return err
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	c.mu.Lock()
	header, trailer := c.header, c.trailer
	c.mu.Unlock()
	var failed []string
	for _, p := range pairs {
		key := strings.ToLower(p.Key)
		vals := append(append([]string{}, header.Get(key)...), trailer.Get(key)...)
		got := "undefined"
		if len(vals) > 0 {
			got = strings.Join(vals, ", ")
		}
		want := secrets.Expand(sc, p.Value)
		if got != want {
			failed = append(failed, fmt.Sprintf("  %s: expected %q, got %q", p.Key, want, got))
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return secrets.Hide(sc, core.Failf("The metadata of the %s grpc service's answer to %s differs:\n%s", s.name, c.name(), strings.Join(failed, "\n")))
}

func checkStreamed(sc *core.Scenario, a core.Args) error {
	s, c, err := lastCall(sc, a.String(1))
	if err != nil {
		return err
	}
	if c.stream == nil {
		return fmt.Errorf("%s is a unary call: check its answer with \"the %s grpc service's answer has the following fields:\"", c.name(), s.name)
	}
	rs, err := cloudstep.Conditions(a.Table)
	if err != nil {
		return err
	}
	match := func(m cloudstep.Message) (bool, error) { return rs.MatchMessage(m, "") }
	return secrets.Hide(sc, cloudstep.ExpectMatch(sc, cloudstep.Wait(a, 0), c.stream, match, "", "message",
		fmt.Sprintf("the %s stream of the %s grpc service", c.name(), s.name)))
}

// codeNames are the statuses by the names the steps give them.
var codeNames = map[codes.Code]string{
	codes.OK: "OK", codes.Canceled: "CANCELLED", codes.Unknown: "UNKNOWN", codes.InvalidArgument: "INVALID_ARGUMENT",
	codes.DeadlineExceeded: "DEADLINE_EXCEEDED", codes.NotFound: "NOT_FOUND", codes.AlreadyExists: "ALREADY_EXISTS",
	codes.PermissionDenied: "PERMISSION_DENIED", codes.ResourceExhausted: "RESOURCE_EXHAUSTED",
	codes.FailedPrecondition: "FAILED_PRECONDITION", codes.Aborted: "ABORTED", codes.OutOfRange: "OUT_OF_RANGE",
	codes.Unimplemented: "UNIMPLEMENTED", codes.Internal: "INTERNAL", codes.Unavailable: "UNAVAILABLE",
	codes.DataLoss: "DATA_LOSS", codes.Unauthenticated: "UNAUTHENTICATED",
}

func codeName(c codes.Code) string {
	if n, ok := codeNames[c]; ok {
		return n
	}
	return strconv.Itoa(int(c))
}

// parseCode reads a status by its name (NOT_FOUND) or its number (5).
func parseCode(s string) (codes.Code, error) {
	for c, n := range codeNames {
		if strings.EqualFold(n, s) {
			return c, nil
		}
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 16 {
		return codes.Code(n), nil
	}
	names := make([]string, 0, len(codeNames))
	for _, n := range codeNames {
		names = append(names, n)
	}
	sort.Strings(names)
	return 0, fmt.Errorf("%q is not a gRPC status; the statuses are %s", s, strings.Join(names, ", "))
}
