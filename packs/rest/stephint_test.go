package rest

import (
	"context"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

// An OpenAPI finding on a value of the request says what to write where it can
// have been written: the quotes of a step's own text are the step's.
func TestReportSaysWhatToWriteInATableAndInAStep(t *testing.T) {
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{ID: "1", Name: "registers a parcel"}, core.NewSuite(core.SuiteOptions{}), nil)
	err := report(sc, &Exchange{
		Method: "POST", URL: "http://parcels/api/parcels", Status: 400,
		RequestBody: []byte(`{"zip": 2500}`),
		Issues:      []Issue{{Level: LevelError.String(), Key: "validation.request.body.schema.type", Message: "$.zip: got number, want string"}},
	})
	if err == nil || !strings.Contains(err.Error(), `write "2500" in a table, or '"2500"' in a step's own text`) {
		t.Errorf("%v", err)
	}
}
