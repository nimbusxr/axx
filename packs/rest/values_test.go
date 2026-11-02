package rest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// A value in a step's own text follows the rule as a table's cell does (ADR
// 0014): the step's quotes delimit it, and what is inside them is read. Every
// form of a family says the same.
func TestAStepsOwnValue(t *testing.T) {
	var header http.Header
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "5")
		_, _ = w.Write([]byte(`{"status":"ok","zip":"01067","count":5}`))
	}))
	t.Cleanup(srv.Close)
	h := newHarness(t, func(o *core.SuiteOptions) { o.Interpolate = strings.NewReplacer("${env:ZIP}", "10115").Replace })
	h.service("api", srv.URL, "")
	valueFails := func(line, want string) {
		t.Helper()
		if err := h.step(line); err == nil || err.Error() != want+" ["+core.ValueErrorCode+"]" {
			t.Errorf("%s: %v, want %s", line, err, want)
		}
		if got := cloudtest.ValueProblems(t, h.reg, line); !slices.Equal(got, []string{want}) {
			t.Errorf("%s: axx validate reports %q, want %s", line, got, want)
		}
	}
	const five = `5 reads as an integer, and only text fits here: write '"5"' for the text`

	h.ok("a POST request to /parcels")
	h.ok("a request payload using an application/json empty content template")
	for _, line := range []string{
		"the request header X-Count is '5'",
		`the request header X-Count is "5"`,
		"the request header X-Count is '5' for 1st ordered request",
		"the request header X-Count is '5' for request on api",
		"the request header X-Count is '5' for 1st ordered request on api",
		"the response header Retry-After is '5'",
		"the response header Retry-After is '5' for 1st ordered response",
		"the response header Retry-After is '5' for response on api",
		"the response body contains '5'",
		"the response body contains '5' for 1st ordered response on api",
		"the request is signed as a standard webhook with the key '5'",
		"the request is signed as a standard webhook with the key '5' for request on api",
	} {
		valueFails(line, five)
	}
	valueFails("the request header X-Count is ''", `the value is empty: write '""' for the empty text`)
	h.ok(`the request header X-Count is '"5"'`)
	h.ok(`the request header X-Ok is "\"true\""`)
	h.ok("the request header Accept is 'application/json'")

	// A value that takes any type: the forms with the ordinal first, and the integer without quotes.
	const zip = `01067 could be the number 1067 or the text 01067: write '1067' for the number, or '"01067"' for the text`
	for _, line := range []string{
		"the request payload property zip is '01067'",
		"the request payload property zip is '01067' for request on api",
		"the 1st ordered request payload property zip is '01067'",
		"the 1st ordered request payload property zip is '01067' on api",
		"the response payload property zip is '01067'",
		"the response payload property zip is '01067' for 1st ordered response on api",
		"the 1st ordered response payload property zip is '01067'",
		"the 1st ordered response payload property zip is '01067' on api",
	} {
		valueFails(line, zip)
	}
	h.ok(`the 1st ordered request payload property zip is '"01067"' on api`)
	h.ok("the 1st ordered request payload property count is 5")
	h.ok("the request payload property reference is '${env:ZIP}'") // a reference is a string, whatever it expands to
	h.ok("the request is executed")
	if got := header.Get("X-Count"); got != "5" {
		t.Errorf("X-Count: %q", got)
	}
	if got := header.Get("X-Ok"); got != "true" {
		t.Errorf("X-Ok: %q", got)
	}
	if want := `{"zip":"01067","count":5,"reference":"10115"}`; body != want {
		t.Errorf("body %s, want %s", body, want)
	}

	h.ok(`the response header Retry-After is '"5"'`)
	h.ok(`the response header Retry-After is '"5"' for response on api`)
	h.ok("the response header Content-Type is 'application/json'")
	h.ok(`the response body contains '"5"'`)
	h.ok("the response body contains 'status'")
	// JSON as text: in double quotes, the rule's; the pair around a whole value is always the rule's.
	h.ok(`the response body contains '"{"status":"ok","zip":"01067","count":5}"'`)
	valueFails(`the response body contains '{"status":"ok","zip":"01067","count":5}'`,
		`{"status":"ok","zip":"01067","count":5} reads as a JSON object, and only text fits here: write '"{"status":"ok","zip":"01067","count":5}"' for the text`)
	h.ok(`the response body contains '"status":"ok"'`)
	h.ok(`the response body contains '""status":"ok""'`)
	h.fails(`the response body contains '""status":"no""'`, "Response body doesn't contain")
	h.ok(`the response payload property zip is '"01067"'`)
	h.ok(`the 1st ordered response payload property count is 5 on api`)
	h.ok(`the 1st ordered response payload property count is '5'`)
	h.assertionFails(`the response payload property zip is '1067'`, "is not 1067")
}
