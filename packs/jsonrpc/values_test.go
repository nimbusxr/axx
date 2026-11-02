package jsonrpc

import "testing"

// A value in a step's own text follows the rule (ADR 0014): text for an error's
// message, any type for a result.
func TestAStepsOwnValue(t *testing.T) {
	t.Setenv("PARCEL_NUMBER", "7103")
	_, url := serve(t)
	h := harness(t, nil)
	h.OK("the depots jsonrpc service with the following properties:", [][]string{{"url", url}})
	h.OK("the parcel.hold method is called on the depots jsonrpc service with the params:", `{"reference": "PX-RPC-7103", "until": "2026-10-02"}`)
	h.ValueFails("the depots jsonrpc service answered the error -32010 with a message containing '7103'", `7103 reads as an integer, and only text fits here: write '"7103"' for the text`)
	h.OK(`the depots jsonrpc service answered the error -32010 with a message containing '"7103"'`)
	h.OK("the depots jsonrpc service answered the error -32010 with a message containing 'out for delivery'")
	h.OK("the depots jsonrpc service answered the error -32010 with a message containing '${env:PARCEL_NUMBER}'")
	// A result's value fails the same in the run as in axx validate.
	h.ValueFails("the depots jsonrpc service's result is '01067'", `01067 could be the number 1067 or the text 01067: write '1067' for the number, or '"01067"' for the text`)
	h.ValueFails("the depots jsonrpc service's result is ''", `the value is empty: write '""' for the empty string, or 'null' for null`)
	h.ValueFails("the depots jsonrpc service's result is 'undefined'", `undefined (no such property) is not a value here: write '"undefined"' for the text`)
}
