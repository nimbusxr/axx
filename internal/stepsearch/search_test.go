package stepsearch

import (
	"reflect"
	"testing"
)

var steps = []Step{
	{ID: "rest.response.status", Expr: "the[[ {ordinal} ordered]] response status code is {int}[[ on {service}]]", Doc: "Check the status code of a response."},
	{ID: "rest.request", Expr: "a(n) {word} request to {word}[[ on {service}]]", Doc: "Add a request to the default or the named service."},
	{ID: "sql.rows.eq", Expr: "the[[ {ordinal}]] selection[[ on {dbService}]] has {int} row(s)", Doc: "Check that a selection has exactly the given number of rows."},
	{ID: "sql.select", Expr: "a[[ {ordinal}]] selection of rows is retrieved from the {word} table[[ on {dbService}]] where:", Doc: "Select the rows that match."},
	{ID: "kafka.event.headers", Expr: "the[[ {ordinal} ordered]] {word} kafka event headers[[ on the {word} kafka service]] are:", Doc: "Set headers of an event; the request is sent later."},
}

func ids(rs []Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, steps[r.Index].ID)
	}
	return out
}

func TestWords(t *testing.T) {
	if got := Words("Check the ROWS in a table, with headers & entries"); !reflect.DeepEqual(got, []string{"check", "row", "table", "header", "entry"}) {
		t.Errorf("words: %v", got)
	}
	if got := Words("status class"); !reflect.DeepEqual(got, []string{"status", "class"}) {
		t.Errorf("status and class are not plurals: %v", got)
	}
}

func TestSearch(t *testing.T) {
	rs, strong := Search(steps, "response status")
	if !strong || ids(rs)[0] != "rest.response.status" {
		t.Errorf("response status: %v %v", ids(rs), strong)
	}
	// Whole words, plurals folded: "rows" finds row(s) and "rows".
	rs, _ = Search(steps, "how many rows in the table")
	if got := ids(rs); len(got) < 2 || got[0] != "sql.select" {
		t.Errorf("rows in table: %v", got)
	}
	// A word only the docs use is a weak match, ranked after strong ones.
	rs, strong = Search(steps, "request")
	if got := ids(rs); !strong || got[0] != "rest.request" || got[len(got)-1] != "kafka.event.headers" || rs[len(rs)-1].Strong {
		t.Errorf("request: %v", got)
	}
	// Stopwords alone, or words no step has, find nothing.
	if rs, strong := Search(steps, "the with a"); len(rs) != 0 || strong {
		t.Errorf("stopwords: %v", ids(rs))
	}
	if rs, strong := Search(steps, "frobnicate the widget"); len(rs) != 0 || strong {
		t.Errorf("no match: %v", ids(rs))
	}
	// Substrings do not match: "row" is not in "throw".
	if rs, _ := Search([]Step{{ID: "x.y", Expr: "it throws"}}, "row"); len(rs) != 0 {
		t.Errorf("substring matched")
	}
}

func TestTwins(t *testing.T) {
	got := Twins([]string{"rest.request.header", "rest.request.header.on", "rest.token.on", "kafka.on"})
	if !reflect.DeepEqual(got, map[string]string{"rest.request.header.on": "rest.request.header"}) {
		t.Errorf("twins: %v", got)
	}
}

// A step of a pack the query does not name ranks after one that fits as
// well: "add a GET request to a path" is about a request to send, not a
// mocked one.
func TestPackMarkers(t *testing.T) {
	steps := []Step{
		{ID: "rest.request", Pack: "rest", Expr: "a(n) {word} request to {word}[[ on {service}]]"},
		{ID: "rest.execute", Pack: "rest", Expr: "the[[ {ordinal} ordered]] request is executed"},
		{ID: "rest.response.status", Pack: "rest", Expr: "the response status code is {int}"},
		{ID: "mock.received.path", Pack: "mock", Expr: "the mocked {word} request to path {word} was received"},
		{ID: "mock.count", Pack: "mock", Expr: "the mocked request named {word} was received {int} times"},
		{ID: "mock.service", Pack: "mock", Expr: "the {word} mocked service with the following properties:"},
	}
	if m := markers(steps); !reflect.DeepEqual(m, map[string][]string{"mock": {"mocked"}}) {
		t.Errorf("markers: %v", m)
	}
	rs, _ := Search(steps, "add HTTP GET request to path")
	if got := steps[rs[0].Index].ID; got != "rest.request" {
		t.Errorf("first: %s", got)
	}
	rs, _ = Search(steps, "mock received request path")
	if got := steps[rs[0].Index].ID; got != "mock.received.path" {
		t.Errorf("a query naming the pack: %s", got)
	}
}
