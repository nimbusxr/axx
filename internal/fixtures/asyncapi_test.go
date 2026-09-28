package fixtures

import (
	"strings"
	"testing"
)

// The json family governed by an AsyncAPI message's payload
// (events.yaml#/components/messages/Name): the fixtures of the messages
// scenarios send.

const eventsSpec = `asyncapi: 3.0.0
info: {title: Parcels events, version: 1.0.0}
channels:
  scans:
    address: depots/{depot}/scans
    messages:
      scan: {$ref: '#/components/messages/scan'}
components:
  messages:
    scan:
      name: scan
      payload: {$ref: '#/components/schemas/Scan'}
    delivered: {$ref: 'messages/delivered.yaml'}
    printJob:
      payload:
        schemaFormat: application/vnd.apache.avro;version=1.9.0
        schema: {type: record, name: PrintJob, fields: [{name: parcelRef, type: string}]}
  schemas:
    Scan:
      type: object
      required: [scanId, parcelRef, status, location]
      properties:
        scanId: {type: string}
        parcelRef: {type: string}
        status: {type: string, enum: [SORTED, OUT_FOR_DELIVERY, DELIVERED]}
        location: {$ref: '#/components/schemas/Location'}
        note: {type: string}
    Location:
      type: object
      required: [depot]
      properties:
        depot: {type: string, default: LEJ}
        city: {type: string}
`

const deliveredMessage = `name: parcelDelivered
payload: {$ref: '../schemas/delivered.json'}
`

const deliveredSchema = `{
  "type": "object",
  "required": ["parcelRef", "signedBy"],
  "properties": {"parcelRef": {"type": "string"}, "signedBy": {"type": "string"}}
}`

const scanFactory = `factory:
  family: json
  schema: ../asyncapi/events.yaml#/components/messages/scan
identity:
  - path: scanId
    prefix: "SC-"
defaults:
  status: SORTED
fixtures:
  scan-sorted:
    parcelRef: PX-FIX-7401
    location: {city: Leipzig}
  scan-out:
    parcelRef: PX-FIX-7402
    status: OUT_FOR_DELIVERY
    location: {depot: HAM}
`

func asyncapiWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := newWorkspace(t)
	w.write("asyncapi/events.yaml", eventsSpec)
	w.write("asyncapi/messages/delivered.yaml", deliveredMessage)
	w.write("asyncapi/schemas/delivered.json", deliveredSchema)
	w.write("mqtt/scans.factory.yaml", scanFactory)
	return w
}

func TestAsyncAPIMessagePayload(t *testing.T) {
	files := asyncapiWorkspace(t).expand()
	s := jsonOf(t, files["mqtt/scan-sorted.json"])
	expect(t, str(at(s, "scanId")), "SC-scan-sorted")
	expect(t, str(at(s, "status")), "SORTED")
	expect(t, str(at(s, "location", "depot")), "LEJ")
	expect(t, str(at(s, "location", "city")), "Leipzig")
	if s.Has("note") {
		t.Error("an optional field left unresolved is omitted")
	}
	body := string(files["mqtt/scan-out.json"])
	if strings.Index(body, "scanId") > strings.Index(body, "parcelRef") || strings.Index(body, "parcelRef") > strings.Index(body, "location") {
		t.Errorf("schema order:\n%s", body)
	}
}

func TestAsyncAPIMessageInAnotherFile(t *testing.T) {
	w := asyncapiWorkspace(t)
	w.write("mqtt/delivered.factory.yaml", `factory:
  family: json
  schema: ../asyncapi/events.yaml#/components/messages/delivered
fixtures:
  delivered-7403:
    parcelRef: PX-FIX-7403
    signedBy: H. Wolf
`)
	d := jsonOf(t, w.expand()["mqtt/delivered-7403.json"])
	expect(t, str(at(d, "signedBy")), "H. Wolf")
}

func TestAsyncAPIMessageErrors(t *testing.T) {
	tests := []struct {
		name, old, new string
		parts          []string
	}{
		{"enum", "status: OUT_FOR_DELIVERY", "status: LOST", []string{"LOST", "OUT_FOR_DELIVERY"}},
		{"typo", "parcelRef: PX-FIX-7402", "parcelRf: PX-FIX-7402", []string{"parcelRf"}},
		{"no such message", "messages/scan", "messages/scanned", []string{"has no message 'scanned' under #/components/messages"}},
		{"an Avro payload", "messages/scan", "messages/printJob", []string{"application/vnd.apache.avro;version=1.9.0 schema", "avro family"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := asyncapiWorkspace(t)
			w.replace("mqtt/scans.factory.yaml", tc.old, tc.new)
			err := w.expandErr()
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, p := range tc.parts {
				if !strings.Contains(err.Error(), p) {
					t.Errorf("error lacks %q:\n%v", p, err)
				}
			}
		})
	}
}
