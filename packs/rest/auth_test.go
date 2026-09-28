package rest

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/signing"
)

// verifyHS256 checks a bearer JWT's signature and returns its claims.
func verifyHS256(t *testing.T, authorization, key string) map[string]any {
	t.Helper()
	jwt, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok {
		t.Fatalf("Authorization %q", authorization)
	}
	p := strings.Split(jwt, ".")
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(p[0] + "." + p[1]))
	if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) != p[2] {
		t.Fatal("the token's signature does not verify")
	}
	var claims map[string]any
	b, _ := base64.RawURLEncoding.DecodeString(p[1])
	_ = json.Unmarshal(b, &claims)
	return claims
}

func TestAuthorizedWithAToken(t *testing.T) {
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("the shop token with the following properties:", []string{"key", "shop-token-key"}, []string{"claim.shop", "maple-crafts"})
	h.ok("a GET request to /echo")
	h.ok("a 2nd ordered GET request to /echo")
	h.ok("the request is authorized with the shop token for 2nd ordered request on api")
	h.ok("the request is executed")
	if got := a.last().Header.Get("Authorization"); got != "" {
		t.Errorf("the 1st request was authorized: %q", got)
	}
	h.ok("the 2nd ordered request is executed")
	if claims := verifyHS256(t, a.last().Header.Get("Authorization"), "shop-token-key"); claims["shop"] != "maple-crafts" {
		t.Errorf("claims %v", claims)
	}

	h.ok("a 3rd ordered GET request to /echo")
	h.ok("the request is authorized with the courier token for 3rd ordered request")
	h.fails("the 3rd ordered request is executed", `no token named "courier" in this scenario`)
}

func TestAClientCredentialsToken(t *testing.T) {
	a, srv := newAPI(t)
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_, _ = w.Write([]byte(`{"access_token": "at-` + r.Form.Get("client_id") + `", "expires_in": 3600}`))
	}))
	t.Cleanup(idp.Close)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("the shop-system token with the following properties:",
		[]string{"token url", idp.URL}, []string{"client id", "maple-crafts"}, []string{"client secret", "maple-secret"})
	h.ok("a GET request to /echo")
	h.ok("the request is authorized with the shop-system token")
	h.ok("the request is executed")
	if got := a.last().Header.Get("Authorization"); got != "Bearer at-maple-crafts" {
		t.Errorf("Authorization %q", got)
	}
}

func TestSignedInAHeader(t *testing.T) {
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /echo?depot=LEJ")
	h.ok("a request payload using an application/json empty content template")
	h.ok("the request payload property reference is 'PX-9101'")
	h.ok("the request is signed in the Stripe-Signature header with the following properties:",
		[]string{"key", "courier-webhook-key"}, []string{"signs", "{timestamp}.{method}.{path}.{body}"},
		[]string{"value", "t={timestamp},v1={signature}"})
	h.ok("the request is executed")
	got := a.last()
	spec, err := signing.Parse("Stripe-Signature", []core.Pair{
		{Key: "key", Value: "courier-webhook-key"},
		{Key: "signs", Value: "{timestamp}.{method}.{path}.{body}"},
		{Key: "value", Value: "t={timestamp},v1={signature}"},
	}, func(v string) (string, error) { return v, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Verify(got.Header, "POST", "/echo?depot=LEJ", []byte(got.Body)); err != nil {
		t.Errorf("%v (body %s)", err, got.Body)
	}
	h.fails("the request is signed in the X-Signature header with the following properties:",
		`the signing property "key" is required`, []string{"value", "sha256={signature}"})
}

func TestSignedAsAStandardWebhook(t *testing.T) {
	const key = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	a, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("a POST request to /echo")
	h.ok("a request payload using an application/json empty content template")
	h.ok("the request payload property reference is 'PX-9201'")
	h.ok("the request is signed as a standard webhook with the key '" + key + "'")
	h.ok("the request is executed")
	got := a.last()
	if err := signing.VerifyStandard(got.Header, key, []byte(got.Body)); err != nil {
		t.Error(err)
	}
	h.fails("the request is signed as a standard webhook with the key 'shop-secret!'", "a Standard Webhooks key is")
}

// A token and a signing key never show in a failure.
func TestTokensAndKeysAreMasked(t *testing.T) {
	_, srv := newAPI(t)
	h := newHarness(t)
	h.service("api", srv.URL, "")
	h.ok("the shop token with the following properties:", []string{"key", "shop-token-key"}, []string{"claim.shop", "maple-crafts"})
	h.ok("a GET request to /echo")
	h.ok("the request is authorized with the shop token")
	h.ok("the request header X-Key is 'shop-token-key'")
	h.ok("the request is executed")
	b, _ := json.Marshal(masked(h.sc, stateKey.Of(h.sc).describe()))
	if strings.Contains(string(b), "shop-token-key") || strings.Contains(string(b), "Bearer ey") || !strings.Contains(string(b), "********") {
		t.Errorf("failure context: %s", b)
	}
}
