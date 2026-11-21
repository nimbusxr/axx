// SPDX-License-Identifier: Apache-2.0

package rest

import (
	"bytes"
	"log/slog"
	"net/http"
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

// Left unset, verification is off as ever, and a run says so once for each host that is not this
// machine: not for this machine's own names, and not for a request without TLS.
func TestAnUnverifiedHostIsSaidOnce(t *testing.T) {
	var out bytes.Buffer
	var sent []string
	u := &unverified{
		log: slog.New(slog.NewTextHandler(&out, nil)),
		next: roundTrip(func(r *http.Request) (*http.Response, error) {
			sent = append(sent, r.URL.String())
			return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody}, nil
		}),
	}
	urls := []string{
		"https://orders.parcels.example/orders",
		"https://orders.parcels.example/orders/7",
		"https://ORDERS.parcels.example:8443/health",
		"https://tracking.parcels.example/",
		"http://billing.parcels.example/",
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
		resp, err := u.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	if len(sent) != len(urls) {
		t.Fatalf("sent %d of %d requests", len(sent), len(urls))
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("said %d things, want one for each of the two hosts:\n%s", len(lines), out.String())
	}
	for i, host := range []string{"orders.parcels.example", "tracking.parcels.example"} {
		for _, want := range []string{"level=WARN", "does not verify " + host + "'s certificate", "packs.rest.tls.verify"} {
			if !strings.Contains(lines[i], want) {
				t.Errorf("line %d lacks %q: %s", i+1, want, lines[i])
			}
		}
	}
}

// The setting decides who sends a run's requests: unset, the one that speaks; true or false, the
// plain transport, as a choice that was made needs no word.
func TestOnlyAnUnsetSettingSpeaks(t *testing.T) {
	for config, speaks := range map[string]bool{
		``:                         true,
		`{}`:                       true,
		`{"tls":{}}`:               true,
		`{"tls":{"verify":false}}`: false,
		`{"tls":{"verify":true}}`:  false,
	} {
		var opts []harnessOpt
		if config != "" {
			opts = append(opts, withPackConfig("rest", config))
		}
		opts = append(opts, func(o *core.SuiteOptions) { o.Logger = slog.New(slog.DiscardHandler) })
		set, err := load(newHarness(t, opts...).suite)
		if err != nil {
			t.Fatalf("%s: %v", config, err)
		}
		if _, got := set.client.Transport.(*unverified); got != speaks {
			t.Errorf("with packs.rest %s the transport is %T, want one that speaks: %v", config, set.client.Transport, speaks)
		}
	}
}
