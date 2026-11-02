package mcp

import "testing"

// A value in a step's own text follows the rule (ADR 0014).
func TestAStepsOwnValue(t *testing.T) {
	h, url := harness(t)
	const number = `9103 reads as an integer, and only text fits here: write '"9103"' for the text`
	h.OK("the parcels mcp server with the following properties:", [][]string{{"url", url}})
	h.OK("the parcels://PX-MCP-9103/label resource is read from the parcels mcp server")
	h.ValueFails("the parcels://PX-MCP-9103/label resource contains '9103'", number)
	h.OK(`the parcels://PX-MCP-9103/label resource contains '"9103"'`)
	h.OK("the parcels://PX-MCP-9103/label resource contains 'PX-MCP-9103'")
	h.OK("the delivery_update prompt is requested from the parcels mcp server with the following arguments:", [][]string{{"reference", "PX-MCP-9103"}})
	h.ValueFails("the delivery_update prompt contains '9103'", number)
	h.OK(`the delivery_update prompt contains '"9103"'`)
	h.OK("the track_parcel tool is called on the parcels mcp server with the following arguments:", [][]string{{"reference", "PX-MCP-9103"}})
	h.ValueFails("the track_parcel tool's result contains '9103'", number)
	h.ValueFails("the track_parcel tool's result on the parcels mcp server contains '9103'", number)
	h.OK(`the track_parcel tool's result contains '"9103"'`)
	h.OK("the track_parcel tool's result contains 'no parcel'")
	// JSON text, in the double quotes that say so.
	h.ValueFails(`the track_parcel tool's result contains '{"status":"OUT_FOR_DELIVERY"}'`,
		`{"status":"OUT_FOR_DELIVERY"} reads as a JSON object, and only text fits here: write '"{"status":"OUT_FOR_DELIVERY"}"' for the text`)
}
