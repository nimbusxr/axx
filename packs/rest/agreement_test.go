package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"testing"
)

// agreementCase is one exchange of testdata/openapi-agreement/cases.json.
type agreementCase struct {
	Name    string `json:"name"`
	Request struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	} `json:"request"`
	Response struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	} `json:"response"`
	Keys []string `json:"keys"`
}

// body is a case body as sent: a JSON string's text, anything else as JSON.
func body(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return []byte(s)
	}
	return raw
}

func headers(m map[string]string) http.Header {
	h := http.Header{}
	for k, v := range m {
		h.Set(k, v)
	}
	return h
}

// TestValidatorsAgree checks that this pack reports the keys the WireMock
// extension reports for the same exchanges (its FindingsAgreementTest reads
// the same cases), so one vocabulary of keys and levels means the same on
// both sides of a contract.
func TestValidatorsAgree(t *testing.T) {
	const dir = "../../testdata/openapi-agreement/"
	data, err := os.ReadFile(dir + "spec.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := parseSpec(dir+"spec.yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dir + "cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cases []agreementCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, c := range doc.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ex := &Exchange{
				Method: c.Request.Method, validationPath: c.Request.Path,
				RequestHeader: headers(c.Request.Headers), RequestBody: body(c.Request.Body),
				Status: c.Response.Status, Header: headers(c.Response.Headers), Body: body(c.Response.Body),
			}
			var got []string
			for _, is := range s.validate(context.Background(), ex) {
				if !slices.Contains(got, is.Key) {
					got = append(got, is.Key)
				}
			}
			slices.Sort(got)
			want := slices.Clone(c.Keys)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("keys %v, want %v", got, want)
			}
		})
	}
}
