package cloudstep_test

import (
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
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
