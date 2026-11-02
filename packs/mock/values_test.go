package mock

import (
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// A value in a step's own text follows the rule (ADR 0014), on what a mock
// received, what a model was asked and what an agent was sent.
func TestAStepsOwnValue(t *testing.T) {
	reg, sc, fw, url := setup(t)
	fw.journal = append(fw.journal, journaled{Method: "GET", URL: "/count", Headers: map[string]string{"X-Count": "5"}})
	ok := func(text string, table ...[]string) {
		t.Helper()
		if err := step(t, reg, sc, text, table); err != nil {
			t.Errorf("%s: %v", text, err)
		}
	}
	valueFails := func(line, value, kind string) {
		t.Helper()
		want := value + " reads as " + kind + `, and only text fits here: write '"` + value + `"' for the text`
		if err := step(t, reg, sc, line, nil); err == nil || err.Error() != want+" ["+core.ValueErrorCode+"]" {
			t.Errorf("%s: %v, want %s", line, err, want)
		}
		if got := cloudtest.ValueProblems(t, reg, line); !slices.Equal(got, []string{want}) {
			t.Errorf("%s: axx validate reports %q, want %s", line, got, want)
		}
	}
	ok("the mocked spacex service with the following properties:", []string{"url", url})
	ok("the mocked GET request to /count named count was received by spacex")
	valueFails("the header X-Count for mocked request named count is '5'", "5", "an integer")
	valueFails("the header X-Count for mocked request named count on spacex is '5'", "5", "an integer")
	ok(`the header X-Count for mocked request named count is '"5"'`)
	ok(`the header X-Count for mocked request named count on spacex is '"5"'`)
	ok("the mocked GET request to /launches named get-launches was received by spacex")
	ok("the header Accept for mocked request named get-launches is 'application/json'")
	valueFails("the mocked request named count on spacex is signed as a standard webhook with the key '5'", "5", "an integer")
	valueFails("the mocked request named count is signed as a standard webhook with the key 'true'", "true", "a boolean")

	models := httptest.NewServer((&fakeWireMock{journal: modelJournal}).handler())
	t.Cleanup(models.Close)
	ok("the mocked models service with the following properties:", []string{"url", models.URL})
	for _, line := range []string{
		"the mocked models model was asked about '8102'",
		"the mocked models model was asked about '8102' 2 times",
		"the mocked models model's request about '8102' contains 'PX-AI-8102'",
		"the mocked models model's request about 'PX-AI-8102' contains '8102'",
		"the mocked models model's request about 'PX-AI-8102' does not contain '8102'",
		"the mocked models model was offered the track_parcel tool in the request about '8102'",
		"the mocked models model was asked for the schemas/address.yaml schema in the request about '8102'",
	} {
		valueFails(line, "8102", "an integer")
	}
	ok(`the mocked models model was asked about '"8102"'`)
	ok(`the mocked models model was asked about '"8102"' 2 times`)
	ok(`the mocked models model was asked about 'PX-AI-8102' 2 times`)
	ok(`the mocked models model's request about '"8102"' contains '"5"'`)
	ok(`the mocked models model was offered the track_parcel tool in the request about '"8102"'`)

	agents := httptest.NewServer((&fakeWireMock{journal: agentJournal}).handler())
	t.Cleanup(agents.Close)
	ok("the mocked partner service with the following properties:", []string{"url", agents.URL})
	valueFails("the mocked partner a2a agent was sent a message containing '9301'", "9301", "an integer")
	valueFails("the mocked partner a2a agent was sent a message containing '9301' 1 time", "9301", "an integer")
	ok(`the mocked partner a2a agent was sent a message containing '"9301"'`)
	ok(`the mocked partner a2a agent was sent a message containing '"9301"' 1 time`)
	ok("the mocked partner a2a agent was sent a message containing 'PX-A2A-9301'")
}
