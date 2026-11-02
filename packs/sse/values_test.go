package sse

import "testing"

// A value in a step's own text follows the rule (ADR 0014).
func TestAStepsOwnValue(t *testing.T) {
	t.Setenv("PARCEL_NUMBER", "5702")
	h, _, url := harness(t)
	h.OK("the tracking event stream with the following properties:", [][]string{{"url", url + "/api/parcels/PX-LIV-5702/events?hold=1"}})
	h.ValueFails("the tracking event stream has an event containing '5702'", `5702 reads as an integer, and only text fits here: write '"5702"' for the text`)
	h.ValueFails("within 1s the tracking event stream has an event containing '5702'", `5702 reads as an integer, and only text fits here: write '"5702"' for the text`)
	h.OK(`the tracking event stream has an event containing '"5702"'`)
	h.OK(`within 5s the tracking event stream has an event containing 'OUT_FOR_DELIVERY'`)
	h.OK("the tracking event stream has an event containing '${env:PARCEL_NUMBER}'")
}
