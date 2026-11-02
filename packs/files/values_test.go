package files

import (
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// A value in a step's own text follows the rule (ADR 0014). The storage packs'
// steps of a file's text are these steps.
func TestAStepsOwnValue(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.OK("the exports folder with the following properties:", [][]string{{"path", "exports"}})
	h.File("exports/report.txt", "42 parcels, 1.5 kg")
	h.ValueFails("the report.txt file in the exports folder contains '42'", `42 reads as an integer, and only text fits here: write '"42"' for the text`)
	h.ValueFails("within 1s the report.txt file in the exports folder contains '1.5'", `1.5 reads as a decimal, and only text fits here: write '"1.5"' for the text`)
	h.OK(`the report.txt file in the exports folder contains '"42"'`)
	h.OK(`within 1s the report.txt file in the exports folder contains '"1.5"'`)
	h.OK("the report.txt file in the exports folder contains 'parcels'")
}
