package cloudstep_test

import (
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/check"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

func queue(typed ...string) cloudstep.Messages {
	return cloudstep.Messages{
		Pack: "q", Target: "test queue", Verb: "sent", Field: "attribute", Fields: "attributes", Typed: typed,
		Example: cloudstep.Sample{
			To: "refunds", Body: `{"claim": "CLM-4106"}`, File: "messages/refund.json", Fields: [][2]string{{"reason", "DAMAGED"}},
			From: "refund-requests", Where: [][2]string{{"claim", "CLM-4101"}, {"amount", "89.5"}, {"attribute reason", "DAMAGED"}},
		},
	}
}

// A message's tables say how their values are read (ADR 0014): the
// conditions are typed, with undefined, and the values sent with a message
// text, but for the rows a pack types.
func TestMessageTables(t *testing.T) {
	tables := func(m cloudstep.Messages) map[string]*core.TableDoc {
		out := map[string]*core.TableDoc{}
		for _, s := range m.Steps() {
			out[strings.TrimPrefix(s.ID, "q.")] = s.Table
		}
		return out
	}
	got := tables(queue())
	if d := got["received"]; d.Values != core.JSONValues || !d.Undefined || strings.Contains(d.Note, "as text") {
		t.Errorf("received: %+v", d)
	}
	if d := got["send.file"]; d.Values != core.TextValues || d.Undefined || strings.Contains(d.Note, "as text") {
		t.Errorf("send.file: %+v", d)
	}
	if d := tables(queue("qos", "retain"))["send.file"]; d.Values != core.JSONValues ||
		!strings.Contains(d.Note, "Every value but those of `qos` and `retain` is text") {
		t.Errorf("send.file with typed rows: %+v", d)
	}
}

// The examples the docs show follow the rule.
func TestExamplesFollowTheRule(t *testing.T) {
	steps := append(queue().Steps(), queue("qos").Steps()...)
	steps = append(steps, cloudstep.Objects{Pack: "s", Container: "s3 bucket", Object: "object", Example: "carrier-drops"}.Steps()...)
	for _, s := range steps {
		for _, ex := range s.Examples {
			for _, line := range strings.Split(ex, "\n") {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "|") {
					continue
				}
				cells := strings.Split(strings.Trim(line, "|"), "|")
				for i := range cells {
					cells[i] = strings.TrimSpace(cells[i])
				}
				for _, c := range s.Table.ValueCells(cells) {
					var err error
					switch s.Table.Values {
					case core.JSONValues:
						_, err = core.ParseValue(cells[c], cells[c] == "", s.Table.Undefined)
					case core.TextValues:
						_, _, err = core.ParseText(cells[c], cells[c] == "", s.Table.Undefined)
					}
					if err != nil {
						t.Errorf("%s: example row %q: %v", s.ID, line, err)
					}
				}
			}
		}
	}
}

func TestTextPairs(t *testing.T) {
	pairs, err := cloudstep.TextPairs(&core.Table{Rows: [][]string{{"parcel", "PX-5199"}, {"billed", `"4.10"`}, {"note", `""`}}})
	if err != nil || len(pairs) != 3 || pairs[0].Value != "PX-5199" || pairs[1].Value != "4.10" || pairs[2].Value != "" || pairs[2].Null {
		t.Errorf("%+v %v", pairs, err)
	}
	for _, tc := range []struct{ rows [][]string }{
		{[][]string{{"billed", "4.10"}}},
		{[][]string{{"paid", "true"}}},
		{[][]string{{"note", ""}}},
		{[][]string{{"note", "null"}}},
	} {
		if _, err := cloudstep.TextPairs(&core.Table{Rows: tc.rows}); err == nil || !strings.HasPrefix(err.Error(), tc.rows[0][0]+": ") {
			t.Errorf("%v: %v", tc.rows, err)
		}
	}
}

func TestReadable(t *testing.T) {
	rows := func(kv ...string) cloudstep.Rows {
		var rs cloudstep.Rows
		for i := 0; i < len(kv); i += 2 {
			rs = append(rs, core.Pair{Key: kv[i], Value: kv[i+1], Null: kv[i+1] == ""})
		}
		return rs
	}
	meta := []string{"routing key"}
	for _, ok := range []cloudstep.Rows{
		rows("weightGrams", "1200", "postcode", `"10115"`, "note", "undefined", "header attempt", `"3"`, "header depot", "undefined", "routing key", "print.express"),
		rows("lines", "[1, 2]", "paid", "true", "voided", "null"),
	} {
		if err := ok.Readable("header", meta); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
	for want, rs := range map[string]cloudstep.Rows{
		`header attempt: 3 reads as an integer`:                                                                    rows("header attempt", "3"),
		`routing key: 42 reads as an integer`:                                                                      rows("routing key", "42"),
		`postcode: 01067 could be the number 1067 or the text`:                                                     rows("postcode", "01067"),
		`note: the cell is empty: write "" for the empty string, null for null, or undefined for no such property`: rows("note", ""),
		`header depot: the cell is empty: write "" for the empty text, or undefined for none`:                      rows("header depot", ""),
	} {
		if err := rs.Readable("header", meta); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: want %q, got %v", rs, want, err)
		}
	}
}

// brokerPack is a messaging pack like MQTT: a table of rows it knows, some of
// them typed, and values of the message itself.
type brokerPack struct{ m cloudstep.Messages }

func (p brokerPack) Manifest() core.Manifest {
	return core.Manifest{Name: "q", Steps: p.m.Steps()}
}

func broker(in *cloudstep.Inbox, sent *map[string]string) cloudstep.Messages {
	m := queue("qos", "retain")
	m.Meta = []string{"routing key", "topic"}
	m.SendRows = []core.TableRow{
		{Name: "qos", Takes: "a number"},
		{Name: "retain", Takes: "a boolean"},
		{Name: "attribute <name>", Takes: "a value sent with the message"},
		{Name: "content type", Takes: "the body's media type"},
	}
	m.Send = func(_ *core.Scenario, _ string, _ []byte, fields map[string]string) error {
		*sent = fields
		return nil
	}
	m.Inbox = func(*core.Scenario, string, string) (*cloudstep.Inbox, error) { return in, nil }
	return m
}

// validateMessages are what axx validate reports of a step and its table.
func validateMessages(t *testing.T, m cloudstep.Messages, step string, rows [][]string) []string {
	t.Helper()
	reg := match.NewRegistry()
	for _, p := range []core.Pack{core.ParamsPack(), brokerPack{m}} {
		if err := reg.AddPack(p.Manifest().Name, p.Manifest()); err != nil {
			t.Fatal(err)
		}
	}
	var b strings.Builder
	b.WriteString("Feature: f\n  Scenario: s\n    Then " + step + "\n")
	for _, r := range rows {
		b.WriteString("      | " + r[0] + " | " + r[1] + " |\n")
	}
	_, pickles, err := feature.ParseSource("f.feature", []byte(b.String()), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, p := range check.Pickles(reg, pickles).Problems {
		if p.Kind != check.Value {
			t.Fatalf("%+v", p)
		}
		got = append(got, p.Message+" ["+p.Code+"]")
	}
	return got
}

// Whatever validate says of a message's table, the run says: a row it refuses
// fails with the same message, and one it accepts runs.
func TestValidateAndTheRunAgreeOnMessages(t *testing.T) {
	const (
		send     = "the messages/refund.json message is sent to the refunds test queue with the following attributes:"
		received = "within 5s the refund-requests test queue has a message where:"
	)
	in := &cloudstep.Inbox{}
	var sent map[string]string
	m := broker(in, &sent)
	for _, c := range []struct {
		step string
		row  []string
		want string // what both say, or nothing
	}{
		// A value sent with a message is text, in a typed table too, and only
		// the typed rows take numbers and booleans.
		{send, []string{"attribute scanner", "3"}, `attribute scanner: 3 reads as an integer, and only text fits here: write "3" for the text`},
		{send, []string{"content type", "true"}, `content type: true reads as a boolean, and only text fits here: write "true" for the text`},
		{send, []string{"attribute scanner", `"3"`}, ""},
		{send, []string{"content type", "text/plain"}, ""},
		{send, []string{"qos", "1"}, ""},
		{send, []string{"retain", "true"}, ""},
		{send, []string{"qos", "01"}, `qos: 01 could be the number 1 or the text 01: write 1 for the number, or "01" for the text`},
		{send, []string{"qos", ""}, `qos: the cell is empty: write "" for the empty string, or null for null`},
		{send, []string{"content type", ""}, `content type: the cell is empty: write "" for the empty text`},
		// A condition on text says what fits text, not a number.
		{received, []string{"attribute x", "01067"}, `attribute x: 01067 is text here: write "01067"`},
		{received, []string{"attribute x", ""}, `attribute x: the cell is empty: write "" for the empty text, or undefined for none`},
		{received, []string{"routing key", "3"}, `routing key: 3 reads as an integer, and only text fits here: write "3" for the text`},
		{received, []string{"topic", "01067"}, `topic: 01067 is text here: write "01067"`},
		{received, []string{"attribute x", `"3"`}, ""},
		{received, []string{"routing key", `"3"`}, ""},
		{received, []string{"attribute y", "undefined"}, ""},
		// A condition on the body says what fits a value.
		{received, []string{"status", "01067"}, `status: 01067 could be the number 1067 or the text 01067: write 1067 for the number, or "01067" for the text`},
		{received, []string{"status", ""}, `status: the cell is empty: write "" for the empty string, null for null, or undefined for no such property`},
		{received, []string{"status", `"01067"`}, ""},
		// A bare key is a path into the body.
		{received, []string{"attribute", "3"}, ""},
	} {
		rows := [][]string{c.row}
		var want []string
		if c.want != "" {
			want = []string{c.want + " [AXX-E0207]"}
		}
		if got := validateMessages(t, m, c.step, rows); strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("%v: validate says %q, want %q", c.row, got, want)
		}
		h := cloudtest.New(t, brokerPack{m})
		h.File("messages/refund.json", `{"claim": "CLM-4106"}`)
		h.NewScenario()
		in.Add(cloudstep.Message{
			Body:   []byte(`{"attribute": 3, "routing key extra": 3, "status": "01067"}`),
			Fields: map[string]string{"x": "3"},
			Meta:   map[string]string{"routing key": "3"},
		})
		err := h.Step(c.step, rows)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%v: the run says %v", c.row, err)
		case c.want != "" && (err == nil || err.Error() != want[0]):
			t.Errorf("%v: the run says %v, want %s", c.row, err, want[0])
		}
	}
	// More words after a row's name are a path too, not text: validate has
	// nothing to say of its value, and the run says what is wrong with its path.
	rows := [][]string{{"routing key extra", "3"}}
	if got := validateMessages(t, m, received, rows); len(got) != 0 {
		t.Errorf("validate says %q", got)
	}
	h := cloudtest.New(t, brokerPack{m})
	h.NewScenario()
	in.Add(cloudstep.Message{Body: []byte(`{"status": "01067"}`)})
	if err := h.Step(received, rows); err == nil || !strings.Contains(err.Error(), "routing key extra: InvalidPathException") {
		t.Errorf("the run says %v", err)
	}
}

func TestReceivedTableMarksTheRowsTheChecksReadAsText(t *testing.T) {
	for _, s := range (brokerPack{broker(&cloudstep.Inbox{}, new(map[string]string))}).m.Steps() {
		if s.ID != "q.received" {
			continue
		}
		for key, want := range map[string]bool{
			"attribute x": true, "attribute": false, "routing key": true, "routing key extra": false, "topic": true, "status": false,
		} {
			if got := s.Table.RowIsText(key); got != want {
				t.Errorf("%q: %v", key, got)
			}
		}
	}
}
