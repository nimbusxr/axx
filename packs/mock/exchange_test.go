package mock

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/secrets"
)

// titles are the exchanges' titles, in order: what was sent, or the kind of
// a peer the scenario registered.
func titles(xs []core.Exchange) []string {
	var out []string
	for _, x := range xs {
		switch {
		case x.Sent != nil:
			out = append(out, x.Peer+" < "+x.From+": "+x.Sent.Title)
		default:
			out = append(out, x.Peer+" is a "+x.Kind)
		}
	}
	return out
}

func sameTitles(t *testing.T, got []core.Exchange, want ...string) {
	t.Helper()
	if g := titles(got); strings.Join(g, "\n") != strings.Join(want, "\n") {
		t.Errorf("exchanges:\n  %s\nwant:\n  %s", strings.Join(g, "\n  "), strings.Join(want, "\n  "))
	}
}

// A step that registers a mocked service says that the scenario has it, and
// a step that finds a request the mock received says the request, from the
// system, once: the steps that check the named request after say no more.
func TestAReceivedRequestSaysItsExchange(t *testing.T) {
	reg, sc, _, url := setup(t)
	ok := func(text string, table ...[]string) {
		t.Helper()
		if err := step(t, reg, sc, text, table); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
	}
	ok("the mocked spacex service with the following properties:", []string{"url", url})
	ok("the mocked POST request to /crew/validate named validated was received by spacex")
	ok("the header Content-Type for mocked request named validated is 'application/json'")
	ok("the mocked request named validated was received exactly 1 time")
	// A request that was not received says nothing, as nothing was sent.
	ok("the mocked DELETE request to /launches named deleted was not received")
	ok("the mocked GET request to /launches named listed was received by spacex")
	ok("the mocked request named listed was received at least 2 times")

	got := sc.Exchanges()
	sameTitles(t, got,
		"spacex is a mock",
		"spacex < system: POST /crew/validate",
		"spacex < system: 2 requests matching GET /launches",
	)
	if len(got) != 3 {
		t.FailNow()
	}
	if reg := got[0]; reg.Kind != "mock" || reg.From != "" || reg.Seen != nil || reg.At.IsZero() {
		t.Errorf("the registration: %+v", reg)
	}
	if x := got[1]; x.Kind != "mock" || x.From != core.FromSystem || x.Seen != nil || x.Sent.Body != "" {
		t.Errorf("the request: %+v", x)
	}

	// A request the mock should not have received is one it received.
	if err := step(t, reg, sc, "the mocked GET request to /launches named unwanted on spacex was not received", nil); err == nil {
		t.Fatal("the step should fail: the mock received the request")
	}
	if got := sc.Exchanges(); len(got) != 4 || got[3].Sent.Title != "2 requests matching GET /launches" {
		t.Errorf("exchanges: %v", titles(got))
	}
}

// The steps that read what a model, an MCP server and an A2A agent were sent
// say it too: once for each thing asked about, with what a failure shows of it.
func TestModelAndAgentRequestsSayTheirExchanges(t *testing.T) {
	reg, sc := modelSetup(t)
	for _, text := range []string{
		"the mocked models model was asked about 'PX-AI-8102'",
		"the mocked models model's request about 'PX-AI-8102' contains 'Hauptstraße 5'",
		"the mocked models model was offered the track_parcel tool in the request about 'PX-AI-8102'",
		"the mocked models model was asked about 'PX-AI-8501' 1 time",
	} {
		if err := step(t, reg, sc, text, nil); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
	}
	if err := step(t, reg, sc, "the mocked models model was asked about 'PX-AI-0000'", nil); err == nil {
		t.Fatal("the model was never asked about PX-AI-0000")
	}
	got := sc.Exchanges()
	sameTitles(t, got,
		"models is a mock",
		`models < system: 2 model requests about "PX-AI-8102"`,
		`models < system: model request about "PX-AI-8501"`,
	)
	if len(got) == 3 && (got[1].Sent.MediaType != "text/plain" || got[1].Sent.Body != "Where is PX-AI-8102?") {
		t.Errorf("what the model was asked: %+v", got[1].Sent)
	}

	srv := httptest.NewServer((&fakeWireMock{journal: agentJournal}).handler())
	t.Cleanup(srv.Close)
	reg, sc, _, _ = setup(t)
	for _, c := range []struct {
		text  string
		table [][]string
	}{
		{"the mocked partner service with the following properties:", [][]string{{"url", srv.URL}}},
		{"the mocked partner mcp server's shipment_status tool was called with the following arguments:", [][]string{{"reference", "PX-MCP-9301"}}},
		{"the mocked partner mcp server's shipment_status tool was called 1 time with the following arguments:", [][]string{{"country", "CH"}}},
		{"the mocked partner mcp server's shipment_status tool was called 2 times with the following arguments:", [][]string{{"reference", "PX-MCP-9302"}}},
		{"the mocked partner mcp server's quote_rate tool was called 0 times with the following arguments:", [][]string{{"postcode", "10115"}}},
		{"the mocked partner a2a agent was sent a message containing 'PX-A2A-9301'", nil},
		{"the mocked partner a2a agent was sent a message containing 'Where is' 2 times", nil},
	} {
		if err := step(t, reg, sc, c.text, c.table); err != nil {
			t.Fatalf("%s: %v", c.text, err)
		}
	}
	got = sc.Exchanges()
	sameTitles(t, got,
		"partner is a mock",
		"partner < system: shipment_status tool call",
		"partner < system: 2 shipment_status tool calls",
		`partner < system: message containing "PX-A2A-9301"`,
		`partner < system: 2 messages containing "Where is"`,
	)
	if len(got) == 5 {
		if call := got[1].Sent; call.MediaType != "application/json" || call.Body != `{"reference":"PX-MCP-9301","country":"CH"}` {
			t.Errorf("the tool call: %+v", call)
		}
		if msg := got[3].Sent; msg.MediaType != "text/plain" || msg.Body != "Where is PX-A2A-9301?" {
			t.Errorf("the message: %+v", msg)
		}
	}
}

// A thing is said once, its body is cut where it is long, and the scenario's
// secrets are masked in it.
func TestAnExchangesBodyIsCut(t *testing.T) {
	_, sc, _, _ := setup(t)
	secrets.Keep(sc, "s3cret-value")
	svc := &Service{Name: "partner"}
	svc.say(sc, "long", "a long one", "text/plain", strings.Repeat("x", exchangeBodyLimit+7))
	svc.say(sc, "long", "a long one", "text/plain", "again")
	svc.say(sc, "none", "no body", "text/plain", "")
	svc.say(sc, "secret", "key s3cret-value", "text/plain", "the key is s3cret-value")
	got := sc.Exchanges()
	if len(got) != 3 {
		t.Fatalf("exchanges: %v", titles(got))
	}
	if want := strings.Repeat("x", exchangeBodyLimit) + "... (7 more bytes)"; got[0].Sent.Body != want {
		t.Errorf("a long body: %d bytes", len(got[0].Sent.Body))
	}
	if got[1].Sent.MediaType != "" || got[1].Sent.Body != "" {
		t.Errorf("no body: %+v", got[1].Sent)
	}
	if m := got[2].Sent; m.Title != "key "+secrets.Masked || m.Body != "the key is "+secrets.Masked {
		t.Errorf("a secret: %+v", m)
	}
}
