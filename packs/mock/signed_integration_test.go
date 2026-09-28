//go:build integration

package mock

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/signing"
)

// TestSignaturesAgainstRealWireMock checks signatures on what a real
// WireMock journaled: a body with non-ASCII text byte for byte, a header
// sent twice, the path with its query.
func TestSignaturesAgainstRealWireMock(t *testing.T) {
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "wiremock/wiremock:3.13.2",
			ExposedPorts: []string{"8080/tcp"},
			WaitingFor:   wait.ForHTTP("/__admin/health").WithPort("8080/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Skipf("wiremock container unavailable: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	host, _ := c.Host(ctx)
	port, _ := c.MappedPort(ctx, "8080/tcp")
	base := fmt.Sprintf("http://%s:%s", host, port.Port())
	stub := `{"request":{"method":"ANY","urlPattern":"/.*"},"response":{"status":204}}`
	resp, err := http.Post(base+"/__admin/mappings", "application/json", strings.NewReader(stub))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	const webhookKey = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	callback, err := signing.Parse("X-Parcels-Signature", []core.Pair{
		{Key: "key", Value: "shop-callback-key"},
		{Key: "signs", Value: "{method} {path} {timestamp}.{body}"},
		{Key: "value", Value: "t={timestamp},sha256={signature}"},
	}, func(v string) (string, error) { return v, nil })
	if err != nil {
		t.Fatal(err)
	}
	send := func(path, body string, sign func(h http.Header, body []byte)) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, base+path, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Add("X-Trace", "depot-LEJ")
		req.Header.Add("X-Trace", "shop-maple")
		sign(req.Header, []byte(body))
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()
	}
	webhook := func(h http.Header, body []byte) { _ = signing.SignStandard(h, webhookKey, body, time.Now()) }
	send("/webhooks/maple-crafts", `{"reference": "PX-9301", "status": "DELIVERED", "signedBy": "Jürgen Weiß"}`, webhook)
	send("/webhooks/maple-crafts", `{"reference": "PX-9302", "status": "DELIVERED"}`, func(h http.Header, _ []byte) {
		webhook(h, []byte(`{"reference": "PX-9302", "status": "RETURNED"}`)) // signed, then changed
	})
	send("/callbacks?depot=LEJ", `{"reference": "PX-9303", "status": "COLLECTED"}`, func(h http.Header, body []byte) {
		callback.Sign(h, http.MethodPost, "/callbacks?depot=LEJ", body, time.Now())
	})

	reg, sc, _, _ := setup(t)
	run := func(text string, table ...[]string) error {
		t.Helper()
		return step(t, reg, sc, text, table)
	}
	ok := func(text string, table ...[]string) {
		t.Helper()
		if err := run(text, table...); err != nil {
			t.Fatalf("%s: %v", text, err)
		}
	}
	fails := func(text, want string, table ...[]string) {
		t.Helper()
		err := run(text, table...)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: want %q, got %v", text, want, err)
		}
	}
	ok("the mocked shops service with the following properties:", []string{"url", base})
	ok("the mocked POST request to path /webhooks/maple-crafts named delivered was received by shops")
	ok("the payload properties for mocked request named delivered on shops are:", []string{"reference", "PX-9301"})
	ok("the mocked request named delivered on shops is signed as a standard webhook with the key '" + webhookKey + "'")

	ok("the mocked POST request to path /webhooks/maple-crafts named changed was received by shops")
	ok("the payload properties for mocked request named changed on shops are:", []string{"reference", "PX-9302"})
	fails("the mocked request named changed on shops is signed as a standard webhook with the key '"+webhookKey+"'",
		"1 of the 1 requests named changed on shops are not signed as they should be")

	// Every request a name matches must be signed: here one of two is not.
	ok("the mocked POST request to path /webhooks/maple-crafts named both was received by shops")
	fails("the mocked request named both on shops is signed as a standard webhook with the key '"+webhookKey+"'",
		"1 of the 2 requests named both on shops are not signed")

	ok("the mocked POST request to path /callbacks named callback was received by shops")
	ok("the mocked request named callback on shops is signed in the X-Parcels-Signature header with the following properties:",
		[]string{"key", "shop-callback-key"}, []string{"signs", "{method} {path} {timestamp}.{body}"},
		[]string{"value", "t={timestamp},sha256={signature}"})
	fails("the mocked request named callback on shops is signed in the X-Parcels-Signature header with the following properties:",
		"but signing what it sent with the key gives",
		[]string{"key", "not-the-key"}, []string{"signs", "{method} {path} {timestamp}.{body}"},
		[]string{"value", "t={timestamp},sha256={signature}"})

	ok("the mocked POST request to path /returns named returned on shops was not received")
	fails("the mocked request named returned on shops is signed as a standard webhook with the key '"+webhookKey+"'",
		"received no request named returned")
}
