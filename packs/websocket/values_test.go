package websocket

import "testing"

// A value in a step's own text follows the rule (ADR 0014).
func TestAStepsOwnValue(t *testing.T) {
	t.Setenv("PARCEL_NUMBER", "5701")
	h, _, url := harness(t)
	h.OK("the tracking websocket with the following properties:", [][]string{{"url", url + "/live"}})
	h.OK("a message is sent to the tracking websocket:", `{"follow": "PX-LIV-5701"}`)
	h.ValueFails("the tracking websocket received a message containing '5701'", `5701 reads as an integer, and only text fits here: write '"5701"' for the text`)
	h.ValueFails("within 1s the tracking websocket received a message containing '5701'", `5701 reads as an integer, and only text fits here: write '"5701"' for the text`)
	h.OK(`the tracking websocket received a message containing '"5701"'`)
	h.OK("within 5s the tracking websocket received a message containing 'OUT_FOR_DELIVERY'")
	h.OK("the tracking websocket received a message containing '${env:PARCEL_NUMBER}'")
}
