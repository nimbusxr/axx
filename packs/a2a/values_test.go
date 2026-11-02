package a2a

import (
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// A value in a step's own text follows the rule (ADR 0014).
func TestAStepsOwnValue(t *testing.T) {
	_, url := serve(t)
	h := cloudtest.New(t, Pack())
	h.OK("the parcels a2a agent with the following properties:", [][]string{{"url", url}})
	h.OK("a message is sent to the parcels a2a agent:", "Where is PX-A2A-9201?")
	h.OK("the parcels a2a agent's task is completed")
	h.ValueFails("the parcels a2a agent's answer contains '9201'", `9201 reads as an integer, and only text fits here: write '"9201"' for the text`)
	h.OK(`the parcels a2a agent's answer contains '"9201"'`)
	h.OK("the parcels a2a agent's answer contains 'out for delivery, in Leipzig'")
}
