// SPDX-License-Identifier: Apache-2.0

package rest

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestThisMachine(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost":              true,
		"LOCALHOST":              true,
		"localhost.":             true,
		"orders.localhost":       true,
		"api.orders.localhost":   true,
		"127.0.0.1":              true,
		"127.8.9.10":             true,
		"::1":                    true,
		"notlocalhost":           false,
		"localhost.example":      false,
		"orders.parcels.example": false,
		"10.0.0.7":               false,
		"192.168.1.20":           false,
		"0.0.0.0":                false,
		"":                       false,
	} {
		if got := thisMachine(host); got != want {
			t.Errorf("thisMachine(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestTheSettingsValues(t *testing.T) {
	for text, want := range map[string]Verify{
		`{}`:                  VerifyWarn,
		`{"verify":"warn"}`:   VerifyWarn,
		`{"verify":"error"}`:  VerifyError,
		`{"verify":"ignore"}`: VerifyIgnore,
		// The setting's earlier values.
		`{"verify":true}`:  VerifyError,
		`{"verify":false}`: VerifyIgnore,
	} {
		var c TLSConfig
		if err := json.Unmarshal([]byte(text), &c); err != nil {
			t.Errorf("%s: %v", text, err)
		} else if got := c.Verify.orWarn(); got != want {
			t.Errorf("%s is %q, want %q", text, got, want)
		}
	}
	for _, text := range []string{`{"verify":"yes"}`, `{"verify":"Warn"}`, `{"verify":1}`, `{"verify":null}`, `{"verify":""}`} {
		var c TLSConfig
		if err := json.Unmarshal([]byte(text), &c); err == nil || !strings.Contains(err.Error(), "warn, error or ignore") {
			t.Errorf("%s: %v, want it refused with the three values", text, err)
		}
	}
}

// tlsState is what a client saw of a server with a certificate of its own, as a service under
// test has: httptest's, which is for example.com and 127.0.0.1 and which no system trusts.
func tlsState(t *testing.T) (*tls.ConnectionState, *x509.CertPool) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	return resp.TLS, roots
}

func TestACertificateIsVerifiedAsAClientWould(t *testing.T) {
	state, itsOwn := tlsState(t)
	if err := verifiesFrom(itsOwn, state, "example.com"); err != nil {
		t.Errorf("trusted, and for the host it names: %v", err)
	}
	if err := verifiesFrom(itsOwn, state, "127.0.0.1"); err != nil {
		t.Errorf("trusted, and for the address it names: %v", err)
	}
	if err := verifiesFrom(itsOwn, state, "orders.parcels.example"); err == nil || !strings.Contains(err.Error(), "orders.parcels.example") {
		t.Errorf("trusted, but for another host: %v, want its name refused", err)
	}
	if err := verifies(state, "example.com"); err == nil {
		t.Error("a certificate no system trusts verified against the system's roots")
	}
	if err := verifies(&tls.ConnectionState{}, "example.com"); err == nil {
		t.Error("no certificate at all verified")
	}
}

// A request goes on whatever its host's certificate is, and the run says once, for each host
// whose certificate does not verify, that it does not: nothing for one that verifies, for this
// machine's own names, or for a request without TLS.
func TestAHostWhoseCertificateDoesNotVerifyIsSaidOnce(t *testing.T) {
	state, _ := tlsState(t)
	var out bytes.Buffer
	sent := 0
	w := &warned{
		log: slog.New(slog.NewTextHandler(&out, nil)),
		verify: func(_ *tls.ConnectionState, host string) error {
			if host == "billing.parcels.example" {
				return nil // a certificate that verifies
			}
			return verifies(state, host)
		},
		next: roundTrip(func(r *http.Request) (*http.Response, error) {
			sent++
			resp := &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}
			if r.URL.Scheme == "https" {
				resp.TLS = state
			}
			return resp, nil
		}),
	}
	urls := []string{
		"https://orders.parcels.example/orders",
		"https://orders.parcels.example/orders/7",
		"https://ORDERS.parcels.example:8443/health",
		"https://tracking.parcels.example/",
		"https://billing.parcels.example/",
		"http://plain.parcels.example/",
		"https://localhost:8443/",
		"https://orders.localhost/",
		"https://127.0.0.1:8443/",
		"https://[::1]:8443/",
	}
	for _, url := range urls {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := w.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	if sent != len(urls) {
		t.Fatalf("sent %d of %d requests", sent, len(urls))
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("said %d things, want one for each of the two hosts whose certificate does not verify:\n%s", len(lines), out.String())
	}
	for i, host := range []string{"orders.parcels.example", "tracking.parcels.example"} {
		for _, want := range []string{"level=WARN", host + "'s certificate does not verify", "the request went on", "packs.rest.tls.verify: error"} {
			if !strings.Contains(lines[i], want) {
				t.Errorf("line %d lacks %q: %s", i+1, want, lines[i])
			}
		}
	}
}

// What the setting does to a request for a server with a certificate of its own, on this
// machine: warn (the default) and ignore let it go on, error fails it, and none says a word of
// this machine.
func TestTheSettingDecidesWhatAFailedCertificateDoes(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0) // the refused handshakes are expected
	srv.StartTLS()
	defer srv.Close()
	for config, passes := range map[string]bool{
		``:                            true,
		`{"tls":{}}`:                  true,
		`{"tls":{"verify":"warn"}}`:   true,
		`{"tls":{"verify":"ignore"}}`: true,
		`{"tls":{"verify":false}}`:    true,
		`{"tls":{"verify":"error"}}`:  false,
		`{"tls":{"verify":true}}`:     false,
	} {
		var out bytes.Buffer
		opts := []harnessOpt{func(o *core.SuiteOptions) { o.Logger = slog.New(slog.NewTextHandler(&out, nil)) }}
		if config != "" {
			opts = append(opts, withPackConfig("rest", config))
		}
		h := newHarness(t, opts...)
		h.service("api", srv.URL, "")
		h.ok("a GET request to /")
		if passes {
			h.ok("the request is executed")
			h.ok("the response status code is 204")
		} else {
			h.fails("the request is executed", "certificate")
		}
		if out.Len() != 0 {
			t.Errorf("with packs.rest %s the run said of this machine: %s", config, out.String())
		}
	}
}
