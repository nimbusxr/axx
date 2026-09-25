package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

// Management requests go to the management endpoint, keeping their path.
func TestRelocate(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL + "/devstoreaccount1-servicebus")
	pl := runtime.NewPipeline("test", "v1", runtime.PipelineOptions{}, &policy.ClientOptions{PerRetryPolicies: []policy.Policy{relocate{base}}})
	req, err := runtime.NewRequest(t.Context(), http.MethodGet, "https://customs.servicebus.windows.net/customs-events")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := pl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got != "/devstoreaccount1-servicebus/customs-events" {
		t.Errorf("path %s", got)
	}
}
