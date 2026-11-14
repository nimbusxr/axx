package fixtures

import (
	"testing"
)

// $refs in datasets: a column's value, or a whole row, taken from this
// fixture or from another document.

const sessionsDDL = "CREATE TABLE sessions (id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, plan TEXT);\n" +
	"CREATE TABLE events (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id), kind TEXT NOT NULL);\n"

// sessionsWorkspace is a dataset factory of sessions and their events: a
// session's id derives from its fixture, and every event is a VIEW unless
// its row says otherwise.
func sessionsWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := newWorkspace(t)
	w.write("seeds/schema.sql", sessionsDDL)
	w.write("seeds/sessions.factory.yaml", "factory:\n  family: dataset\n  schema: schema.sql\n"+
		"identity:\n  - path: sessions.id\n    prefix: SES-\n  - path: events.id\n    derive: authored\n")
	w.write("seeds/sessions.prototype.yaml", "data:\n  sessions:\n    token_hash: abc\n    plan: free\n  events:\n    kind: VIEW\n")
	return w
}

// A row names a value of its own fixture once, where the fixture sets it:
// each event's session is the session's id, which derives from the fixture.
func TestADatasetRowTakesAValueOfItsOwnFixture(t *testing.T) {
	w := sessionsWorkspace(t)
	w.write("seeds/alpha.fixture.yaml", "data:\n  events:\n"+
		"    - id: EV-1\n      session_id:\n        $ref: \"#/sessions/0/id\"\n"+
		"    - id: EV-2\n      kind: CLICK\n      session_id:\n        $ref: \"#/sessions/0/id\"\n"+
		"  sessions:\n    - {}\n")
	expect(t, string(w.expand()["seeds/alpha.yaml"]), `sessions:
- id: SES-alpha
  token_hash: abc
  plan: free
events:
- id: EV-1
  session_id: SES-alpha
  kind: VIEW
- id: EV-2
  session_id: SES-alpha
  kind: CLICK
`)
}

// A row names a value of another document: a row of another dataset
// fixture, with the identity derived for it, or a value of a JSON fixture.
func TestADatasetRowTakesAValueOfAnotherDocument(t *testing.T) {
	w := sessionsWorkspace(t)
	w.write("seeds/alpha.fixture.yaml", "data:\n  sessions:\n    - {}\n")
	w.write("logins/login.schema.json", `{"type": "object", "properties": {"kind": {"type": "string"}}}`)
	w.write("logins/login.factory.yaml", "factory:\n  family: json\n  schema: login.schema.json\nfixtures:\n  first:\n    kind: LOGIN\n")
	w.write("seeds/beta.fixture.yaml", "data:\n  sessions:\n    - id: SES-B\n  events:\n"+
		"    - id: EV-3\n      session_id:\n        $ref: ./alpha.fixture.yaml#/sessions/0/id\n"+
		"      kind:\n        $ref: ../logins/login.factory.yaml#/first/kind\n")
	expect(t, string(w.expand()["seeds/beta.yaml"]), `sessions:
- id: SES-B
  token_hash: abc
  plan: free
events:
- id: EV-3
  session_id: SES-alpha
  kind: LOGIN
`)
}

// A whole row is another row with its own columns over it. One of another
// document lies between the row template and those columns; one of this
// fixture is that row as it is generated.
func TestADatasetRowIsAnotherRow(t *testing.T) {
	w := sessionsWorkspace(t)
	w.write("seeds/alpha.fixture.yaml", "data:\n  sessions:\n    - id: SES-A\n      plan: team\n      token_hash: aaa\n")
	w.write("seeds/beta.fixture.yaml", "data:\n  sessions:\n"+
		"    - $ref: ./alpha.fixture.yaml#/sessions/0\n      id: SES-B\n"+
		"    - $ref: \"#/sessions/0\"\n      id: SES-C\n      token_hash: ccc\n")
	expect(t, string(w.expand()["seeds/beta.yaml"]), `sessions:
- id: SES-B
  token_hash: aaa
  plan: team
- id: SES-C
  token_hash: ccc
  plan: team
`)
}

// The flat XML and CSV renderings resolve them alike.
func TestADatasetRowsRefsInEveryFormat(t *testing.T) {
	for format, file := range map[string]string{"xml": "seeds/alpha.xml", "csv": "seeds/alpha/events.csv"} {
		w := sessionsWorkspace(t)
		w.replace("seeds/sessions.factory.yaml", "family: dataset\n", "family: dataset\n  options: {format: "+format+"}\n")
		w.write("seeds/alpha.fixture.yaml", "data:\n  sessions:\n    - {}\n  events:\n    - id: EV-1\n      session_id:\n        $ref: \"#/sessions/0/id\"\n")
		mustContain(t, string(w.expand()[file]), "SES-alpha")
	}
}

func TestADatasetRowsRefErrors(t *testing.T) {
	event := func(ref string) string {
		return "data:\n  sessions:\n    - {}\n  events:\n    - id: EV-1\n      session_id:\n        $ref: \"" + ref + "\"\n"
	}
	for name, tc := range map[string]struct {
		fixture string
		parts   []string
	}{
		"nothing there":      {event("#/sessions/4/id"), []string{"#/sessions/4/id", "resolved to nothing", "events[0]"}},
		"no such document":   {event("./nowhere.fixture.yaml#/sessions/0/id"), []string{"./nowhere.fixture.yaml", "seeds/nowhere.fixture.yaml"}},
		"a row for a column": {event("#/sessions/0"), []string{"session_id", "must be a scalar"}},
		"a value for a row":  {"data:\n  sessions:\n    - {}\n    - $ref: \"#/sessions/0/id\"\n", []string{"sessions[1]", "brings a value, not a row"}},
		"a ref to a ref": {"data:\n  sessions:\n    - {}\n  events:\n" +
			"    - id: EV-1\n      session_id:\n        $ref: \"#/events/1/session_id\"\n" +
			"    - id: EV-2\n      session_id:\n        $ref: \"#/sessions/0/id\"\n", []string{"session_id", "name the row that sets the value"}},
		"itself":             {"data:\n  sessions:\n    - id: SES-A\n  events:\n    - id: EV-1\n      session_id:\n        $ref: ./alpha.fixture.yaml#/sessions/0/id\n", []string{"cycle", "seeds/alpha.fixture.yaml"}},
		"the same row twice": {"data:\n  sessions:\n    - id: SES-A\n    - $ref: \"#/sessions/0\"\n", []string{"identity collision", "SES-A"}},
	} {
		t.Run(name, func(t *testing.T) {
			w := sessionsWorkspace(t)
			w.write("seeds/alpha.fixture.yaml", tc.fixture)
			mustErrContain(t, w.expandErr(), tc.parts...)
		})
	}
}

// Explain follows a row's $refs to the source that sets the value.
func TestExplainFollowsADatasetRowsRefs(t *testing.T) {
	w := sessionsWorkspace(t)
	w.write("seeds/alpha.fixture.yaml", "data:\n  sessions:\n    - id: SES-A\n      plan: team\n      token_hash: aaa\n")
	w.write("seeds/beta.fixture.yaml", "data:\n  sessions:\n"+
		"    - {}\n"+
		"    - $ref: ./alpha.fixture.yaml#/sessions/0\n      id: SES-B\n"+
		"    - $ref: \"#/sessions/0\"\n      id: SES-C\n"+
		"  events:\n"+
		"    - id: EV-1\n      session_id:\n        $ref: \"#/sessions/0/id\"\n"+
		"    - id: EV-2\n      session_id:\n        $ref: ./alpha.fixture.yaml#/sessions/0/id\n")
	w.generate()
	runExplain(t, w, []explainCase{
		{"seeds/beta.yaml", "events[0].session_id", `"SES-beta"`,
			"reference | seeds/beta.fixture.yaml | data.events[0].session_id.$ref | #/sessions/0/id > identity | seeds/sessions.factory.yaml | sessions.id | derived from the fixture key \"beta\" with the prefix \"SES-\""},
		{"seeds/beta.yaml", "events[1].session_id", `"SES-A"`,
			"reference | seeds/beta.fixture.yaml | data.events[1].session_id.$ref | ./alpha.fixture.yaml#/sessions/0/id > identity | seeds/sessions.factory.yaml | sessions.id | the value its sources set > fixture | seeds/alpha.fixture.yaml | data.sessions[0].id"},
		{"seeds/beta.yaml", "events[0].kind", `"VIEW"`, "prototype | seeds/sessions.prototype.yaml | data.events.kind"},
		// A row another document's row lies under: its own column, the row's, then the template's.
		{"seeds/beta.yaml", "sessions[1].id", `"SES-B"`,
			"identity | seeds/sessions.factory.yaml | sessions.id | the value its sources set > fixture | seeds/beta.fixture.yaml | data.sessions[1].id"},
		{"seeds/beta.yaml", "sessions[1].plan", `"team"`,
			"reference | seeds/beta.fixture.yaml | data.sessions[1].$ref | ./alpha.fixture.yaml#/sessions/0 > fixture | seeds/alpha.fixture.yaml | data.sessions[0].plan"},
		// A row of this fixture: the row it names has its template.
		{"seeds/beta.yaml", "sessions[2].plan", `"free"`,
			"reference | seeds/beta.fixture.yaml | data.sessions[2].$ref | #/sessions/0 > prototype | seeds/sessions.prototype.yaml | data.sessions.plan"},
	})
}
