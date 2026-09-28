package asyncapi

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/contract"
)

func loadDoc(t *testing.T, dir string) *spec {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", dir, "asyncapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := load(dir+"/asyncapi.yaml", fileURL(p))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type checkCase struct {
	name string
	m    contract.Message
	// want are the findings' keys; with, text each of their messages has.
	want []string
	with []string
}

func runChecks(t *testing.T, s *spec, cases []checkCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			found := s.check(c.m)
			var keys, msgs []string
			for _, f := range found {
				keys = append(keys, f.key)
				msgs = append(msgs, f.message)
			}
			if strings.Join(keys, ",") != strings.Join(c.want, ",") {
				t.Fatalf("findings %v, want %v:\n%s", keys, c.want, strings.Join(msgs, "\n"))
			}
			all := strings.Join(msgs, "\n")
			for _, w := range c.with {
				if !strings.Contains(all, w) {
					t.Errorf("the findings lack %q:\n%s", w, all)
				}
			}
		})
	}
}

func named(m contract.Message, name string) contract.Message { m.Name = name; return m }

func msg(protocol, address, payload string) contract.Message {
	return contract.Message{Protocol: protocol, Addresses: []string{address}, Payload: []byte(payload)}
}

func TestAsyncAPI3(t *testing.T) {
	s := loadDoc(t, "v3")
	if len(s.channels) != 5 {
		t.Fatalf("%d channels, want 5: the one without an address is left out", len(s.channels))
	}
	scan := `{"scanId": "SC-1", "parcelRef": "PX-API-7101", "status": "SORTED"}`
	withHeaders := func(m contract.Message, h map[string]string) contract.Message { m.Headers = h; return m }
	withType := func(m contract.Message, ct string) contract.Message { m.ContentType = ct; return m }
	runChecks(t, s, []checkCase{
		{name: "a message of a templated address", m: msg("mqtt", "depots/LEJ/scans", scan)},
		{
			name: "a parameter stands for one segment", m: msg("mqtt", "depots/LEJ/north/scans", scan),
			want: []string{"validation.channel.unknown"}, with: []string{`has no channel with the address "depots/LEJ/north/scans"`},
		},
		{
			name: "a required property is missing", m: msg("mqtt", "depots/LEJ/scans", `{"scanId": "SC-2", "parcelRef": "PX-API-7101"}`),
			want: []string{"validation.message.payload.schema.required"},
			with: []string{"payload $: missing property 'status'", "(the scan message of the scans channel)"},
		},
		{
			name: "a value outside the enum", m: msg("mqtt", "depots/LEJ/scans", `{"scanId": "SC-3", "parcelRef": "PX-API-7101", "status": "LOST"}`),
			want: []string{"validation.message.payload.schema.enum"}, with: []string{"payload $.status:"},
		},
		{
			name: "the payload is not JSON", m: msg("mqtt", "depots/LEJ/scans", `scanned PX-API-7101`),
			want: []string{"validation.message.payload.format"}, with: []string{"the payload is not JSON"},
		},
		{name: "headers read by their schema's types", m: withHeaders(msg("mqtt", "depots/LEJ/scans", scan), map[string]string{"attempt": "2", "source": "handheld"})},
		{
			name: "a header breaks its schema", m: withHeaders(msg("mqtt", "depots/LEJ/scans", scan), map[string]string{"attempt": "0"}),
			want: []string{"validation.message.headers.schema.minimum"}, with: []string{"headers $.attempt:"},
		},
		{
			name: "another content type", m: withType(msg("mqtt", "depots/LEJ/scans", scan), "application/xml"),
			want: []string{"validation.message.contentType"}, with: []string{`the content type is "application/xml", not "application/json"`},
		},
		{name: "a content type with parameters", m: withType(msg("mqtt", "depots/LEJ/scans", scan), "application/json; charset=utf-8")},
		{
			name: "a server of another protocol", m: msg("kafka", "depots/LEJ/scans", scan),
			want: []string{"validation.channel.unknown"}, with: []string{"over kafka; the channels scans (mqtt) are on other servers"},
		},
		{
			name: "a typo names the closest channel", m: msg("kafka", "parcels.event", `{}`),
			want: []string{"validation.channel.unknown"}, with: []string{"the closest is parcels.events"},
		},
		{name: "one of a channel's messages", m: msg("kafka", "parcels.events", `{"parcelRef": "PX-API-7102", "shop": "hawthorn-home", "type": "REGISTERED"}`)},
		{name: "a message whose schema is in another file", m: msg("kafka", "parcels.events", `{"parcelRef": "PX-API-7102", "type": "DELIVERED", "deliveredAt": "2026-09-28T10:00:00Z"}`)},
		{
			name: "the closest of a channel's messages", m: msg("kafka", "parcels.events", `{"parcelRef": "PX-API-7102", "type": "DELIVERED", "deliveredAt": "yesterday"}`),
			want: []string{"validation.message.payload.schema.pattern"},
			with: []string{"payload $.deliveredAt:", "(parcelDelivered, parcelRegistered); it comes closest to parcelDelivered"},
		},
		{name: "an Avro payload", m: msg("amqp", "labels.print", `{"parcelRef": "PX-API-7103", "copies": 2, "printer": {"string": "LEJ-3"}}`)},
		{name: "an Avro payload with a bare union", m: msg("amqp", "labels.print", `{"parcelRef": "PX-API-7103", "copies": 1, "printer": null}`)},
		{
			name: "an Avro payload of the wrong type", m: msg("amqp", "labels.print", `{"parcelRef": "PX-API-7103", "copies": "two", "printer": null}`),
			want: []string{"validation.message.payload.schema.avro"}, with: []string{"payload $.copies: expected int"},
		},
		{name: "the first address with a channel", m: contract.Message{
			Protocol: "amqp", Addresses: []string{"printing", "labels.print"},
			Payload: []byte(`{"parcelRef": "PX-API-7103", "copies": 1, "printer": null}`),
		}},
		{name: "a channel on every server", m: msg("http", "/api/parcels/PX-API-7104/events", `{"reference": "PX-API-7104", "status": "SORTED"}`)},
		{
			name: "an event's type names its message", m: named(msg("http", "/api/parcels/PX-API-7104/events", `{"reference": "PX-API-7104", "status": "DELIVERED"}`), "delivered"),
			want: []string{"validation.message.payload.schema.required"}, with: []string{"missing property 'signedBy'", "(the delivered message of the tracking channel)"},
		},
		{name: "a type no message has", m: named(msg("http", "/api/parcels/PX-API-7104/events", `{"reference": "PX-API-7104", "status": "SORTED"}`), "ping")},
		{name: "an address under its server's path", m: msg("ws", "/portal/track/PX-API-7105/live", `{"reference": "PX-API-7105"}`)},
		{name: "an address without its server's path", m: msg("ws", "/track/PX-API-7105/live", `{"reference": "PX-API-7105"}`)},
	})
}

func TestAsyncAPI2(t *testing.T) {
	s := loadDoc(t, "v2")
	runChecks(t, s, []checkCase{
		{name: "a message of a templated channel", m: msg("mqtt", "depots/LEJ/scans", `{"scanId": "SC-1", "parcelRef": "PX-API-7201", "status": "SORTED"}`)},
		{
			name: "a required property is missing", m: msg("mqtt", "depots/LEJ/scans", `{"scanId": "SC-1", "status": "SORTED"}`),
			want: []string{"validation.message.payload.schema.required"}, with: []string{"missing property 'parcelRef'"},
		},
		{name: "one of oneOf", m: msg("kafka", "parcels.events", `{"parcelRef": "PX-API-7202", "type": "REGISTERED"}`)},
		{name: "an Avro message of oneOf", m: msg("kafka", "parcels.events", `{"parcelRef": "PX-API-7202", "type": "DELIVERED", "deliveredAt": 1790589600000}`)},
		{name: "a server of another protocol", m: msg("mqtt", "parcels.events", `{}`), want: []string{"validation.channel.unknown"}},
	})
}

func TestNotAsyncAPI(t *testing.T) {
	p, _ := filepath.Abs(filepath.Join("testdata", "v3", "schemas", "delivered.json"))
	_, err := load("delivered.json", fileURL(p))
	if err == nil || !strings.Contains(err.Error(), "is not an AsyncAPI 2.x or 3.x document") {
		t.Fatalf("err = %v", err)
	}
}

func TestAddressPattern(t *testing.T) {
	re := addressPattern("depots/{depot}/scans.{kind}")
	for addr, want := range map[string]bool{
		"depots/LEJ/scans.late": true,
		"depots/LEJ/scansXlate": false,
		"depots//scans.late":    false,
		"depots/LEJ/scans.":     false,
	} {
		if got := re.MatchString(addr); got != want {
			t.Errorf("%s: %v, want %v", addr, got, want)
		}
	}
}

func TestFileURLs(t *testing.T) {
	for p, want := range map[string]string{
		"/srv/parcels/asyncapi.yaml":    "file:///srv/parcels/asyncapi.yaml",
		"C:/work/parcels/asyncapi.yaml": "file:///C:/work/parcels/asyncapi.yaml",
	} {
		if got := fileURL(p); got != want {
			t.Errorf("fileURL(%q) = %q, want %q", p, got, want)
		}
	}
}
