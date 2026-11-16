// SPDX-License-Identifier: Apache-2.0

package rest

import (
	"os"
	"strings"
	"testing"
)

// an puts the article a step would have before a media type.
func an(mediaType string) string {
	if strings.ContainsAny(mediaType[:1], "aeiou") {
		return "an " + mediaType
	}
	return "a " + mediaType
}

// A request's payload has the media type its step names, any one: a file is
// sent as it is, with that Content-Type.
func TestARequestPayloadOfAnyMediaType(t *testing.T) {
	a, srv := newAPI(t)
	for _, c := range []struct{ mediaType, file string }{
		{"text/plain", "requests/delivery-note.txt"},
		{"text/plain;charset=UTF-8", "requests/delivery-note.txt"},
		{"text/csv", "requests/round-7731.csv"},
		{"application/xml", "requests/delivery-note.txt"},
		{"application/vnd.parcels.note", "requests/delivery-note.txt"},
		// Not text: every byte arrives.
		{"application/pdf", "requests/label-7731.bin"},
		{"application/octet-stream", "requests/label-7731.bin"},
	} {
		want, err := os.ReadFile("testdata/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		h := newHarness(t)
		h.service("api", srv.URL, "")
		h.ok("a POST request to /echo")
		h.ok("a request payload using " + an(c.mediaType) + " " + c.file + " resource")
		h.ok("the request is executed")
		h.ok("the response status code is 200")
		got := a.last()
		if ct := got.Header.Get("Content-Type"); ct != c.mediaType {
			t.Errorf("%s: sent with Content-Type %q", c.mediaType, ct)
		}
		if got.Body != string(want) {
			t.Errorf("%s: sent %q, want the file's %q", c.mediaType, got.Body, want)
		}
	}

	// A Content-Type header the scenario sets is the request's, as with a JSON payload.
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /echo")
	h.ok("a request payload using a text/plain requests/delivery-note.txt resource")
	h.ok("the request header Content-Type is 'text/markdown'")
	h.ok("the request is executed")
	if ct := a.last().Header.Get("Content-Type"); ct != "text/markdown" {
		t.Errorf("sent with Content-Type %q, want the header the scenario set", ct)
	}
}

// What is not a media type is refused where the step is read, with what one is.
func TestAPayloadsMediaTypeIsAMediaType(t *testing.T) {
	_, srv := newAPI(t)
	for _, bad := range []string{"json", "text", "text/", "/plain", "*/*", "text/*", "text/plain;charset"} {
		h := newHarness(t)
		h.service("api", srv.URL, "")
		h.ok("a POST request to /echo")
		h.fails("a request payload using "+an(bad)+" requests/delivery-note.txt resource",
			"Not a media type: "+bad+" (a media type is a type and a subtype, like application/json or text/plain)")
	}
}

// The payload property steps edit JSON and forms: a payload of another media
// type says so, and an empty template, a JSON object, is JSON's or a form's.
func TestOnlyJSONAndFormPayloadsAreEdited(t *testing.T) {
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /echo")
	h.ok("a request payload using a text/plain requests/delivery-note.txt resource")
	h.fails("the request payload property note is 'Ring twice'",
		"Request content type is not supported for payload property validation: text/plain")

	for _, mediaType := range []string{"text/plain", "application/xml", "application/octet-stream"} {
		h := newHarness(t)
		h.service("api", srv.URL, "")
		h.ok("a POST request to /echo")
		h.fails("a request payload using "+an(mediaType)+" empty content template",
			"an empty content template is an empty JSON object, which "+mediaType+" does not carry")
	}

	// JSON's media types, written with a parameter or of a vendor's own.
	h = newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /echo")
	h.ok("a request payload using an application/json;charset=UTF-8 empty content template")
	h.ok("the request payload property reference is 'PX-7731'")
	h.ok("the request is executed")
	if got := a.last(); got.Header.Get("Content-Type") != "application/json;charset=UTF-8" || got.Body != `{"reference":"PX-7731"}` {
		t.Errorf("sent %q as %q", got.Body, got.Header.Get("Content-Type"))
	}
	h = newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /echo")
	h.ok("a request payload using an application/vnd.api+json empty content template")
	h.ok("the request is executed")
	if got := a.last(); got.Header.Get("Content-Type") != "application/vnd.api+json" || got.Body != "{}" {
		t.Errorf("sent %q as %q", got.Body, got.Header.Get("Content-Type"))
	}

	// A form's, with a parameter, is still sent form-encoded.
	h = newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /echo")
	h.ok("a request payload using an application/x-www-form-urlencoded;charset=UTF-8 empty content template")
	h.ok("the request payload property postcode is '\"10115\"'")
	h.ok("the request is executed")
	if got := a.last(); got.Body != "postcode=10115" {
		t.Errorf("sent %q, want the form", got.Body)
	}
}

// An OpenAPI specification's text examples are sent as their text, and a text
// request the operation declares passes its validation.
func TestATextPayloadFromAnOpenAPIExample(t *testing.T) {
	a, srv := newAPI(t)
	for _, c := range []struct{ step, mediaType, want string }{
		{"a request payload using a text/plain content example", "text/plain", "Leave it with the neighbour at number 9."},
		{"a request payload using a text/plain content example named 'Reception'", "text/plain", "Hand it in at reception.\nAsk for a signature.\n"},
		{"a request payload using a text/csv content example", "text/csv", "reference,postcode\nPX-7731,10115\n"},
		{"a request payload using a text/plain requests/delivery-note.txt resource", "text/plain", "Leave it with the neighbour at number 9.\nRing twice: the bell is quiet.\n"},
	} {
		h := newHarness(t)
		h.service("notes", srv.URL, "notes.yaml")
		h.ok("a POST request to /echo")
		h.ok(c.step)
		h.ok("the request is executed")
		h.ok("the response status code is 200")
		if got := a.last(); got.Body != c.want || got.Header.Get("Content-Type") != c.mediaType {
			t.Errorf("%s: sent %q as %q", c.step, got.Body, got.Header.Get("Content-Type"))
		}
	}

	// A media type the operation does not declare: no example to start from, and a
	// finding when a file of it is sent.
	h := newHarness(t)
	h.service("notes", srv.URL, "notes.yaml")
	h.ok("a POST request to /echo")
	h.fails("a request payload using an application/xml content example", "No content type found for request to get example")
	h.ok("a request payload using an application/xml requests/delivery-note.txt resource")
	h.fails("the request is executed", "validation.request.contentType.notAllowed")
}
