package grpc

import (
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

// A value in a step's own text follows the rule (ADR 0014).
func TestAStepsOwnValue(t *testing.T) {
	_, addr := serve(t)
	h := cloudtest.New(t, Pack())
	h.OK("the tracking grpc service with the following properties:", [][]string{{"address", addr}})
	h.OK("the GetParcel method is called on the tracking grpc service with the following fields:", [][]string{{"reference", "PX-GRP-6199"}})
	h.ValueFails("the tracking grpc service answered NOT_FOUND with a message containing '6199'", `6199 reads as an integer, and only text fits here: write '"6199"' for the text`)
	h.ValueFails("within 1s the tracking grpc service answered NOT_FOUND with a message containing '6199'", `6199 reads as an integer, and only text fits here: write '"6199"' for the text`)
	h.OK(`the tracking grpc service answered NOT_FOUND with a message containing '"6199"'`)
	h.OK("the tracking grpc service answered NOT_FOUND with a message containing 'PX-GRP-6199'")
}
