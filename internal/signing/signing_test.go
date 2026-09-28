package signing

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
)

func spec(t *testing.T, header string, rows ...[2]string) Spec {
	t.Helper()
	var pairs []core.Pair
	for _, r := range rows {
		pairs = append(pairs, core.Pair{Key: r[0], Value: r[1]})
	}
	s, err := Parse(header, pairs, func(v string) (string, error) { return v, nil })
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// GitHub's documented example of X-Hub-Signature-256.
func TestGitHubsExample(t *testing.T) {
	s := spec(t, "X-Hub-Signature-256", [2]string{"key", "It's a Secret to Everybody"}, [2]string{"value", "sha256={signature}"})
	h := http.Header{}
	s.Sign(h, "POST", "/webhooks", []byte("Hello, World!"), time.Now())
	if got := h.Get("X-Hub-Signature-256"); got != "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17" {
		t.Errorf("signature %s", got)
	}
	if err := s.Verify(h, "POST", "/webhooks", []byte("Hello, World!")); err != nil {
		t.Error(err)
	}
	err := s.Verify(h, "POST", "/webhooks", []byte("Hello, World?"))
	if err == nil || !strings.Contains(err.Error(), "but signing what it sent with the key gives") {
		t.Errorf("a changed body: %v", err)
	}
}

// Stripe's form: the timestamp in the header, and signed before the body.
func TestATimestampInTheValue(t *testing.T) {
	s := spec(t, "Stripe-Signature", [2]string{"key", "courier-secret"}, [2]string{"signs", "{timestamp}.{body}"},
		[2]string{"value", "t={timestamp},v1={signature}"})
	h := http.Header{}
	body := []byte(`{"reference": "PX-9101", "status": "COLLECTED"}`)
	s.Sign(h, "POST", "/api/courier/callbacks", body, time.Unix(1790000000, 0))
	if got := h.Get("Stripe-Signature"); !strings.HasPrefix(got, "t=1790000000,v1=") {
		t.Errorf("value %s", got)
	}
	if err := s.Verify(h, "POST", "/api/courier/callbacks", body); err != nil {
		t.Error(err)
	}
	h.Set("Stripe-Signature", strings.Replace(h.Get("Stripe-Signature"), "t=1790000000", "t=1790000001", 1))
	if err := s.Verify(h, "POST", "/api/courier/callbacks", body); err == nil {
		t.Error("a changed timestamp verified")
	}
}

// Slack's form: the timestamp in a header of its own, base64, with the
// method and the path signed too.
func TestATimestampHeader(t *testing.T) {
	s := spec(t, "X-Signature", [2]string{"key", "k"}, [2]string{"signs", "{method} {path}:{timestamp}:{body}"},
		[2]string{"encoding", "base64"}, [2]string{"value", "v0={signature}"}, [2]string{"timestamp header", "X-Timestamp"},
		[2]string{"algorithm", "hmac-sha512"})
	h := http.Header{}
	s.Sign(h, "POST", "/hooks?depot=LEJ", []byte("{}"), time.Unix(1790000000, 0))
	if h.Get("X-Timestamp") != "1790000000" {
		t.Errorf("timestamp header %q", h.Get("X-Timestamp"))
	}
	if err := s.Verify(h, "POST", "/hooks?depot=LEJ", []byte("{}")); err != nil {
		t.Error(err)
	}
	if err := s.Verify(h, "POST", "/hooks?depot=DRS", []byte("{}")); err == nil {
		t.Error("another path verified")
	}
	h.Del("X-Timestamp")
	if err := s.Verify(h, "POST", "/hooks?depot=LEJ", []byte("{}")); err == nil || !strings.Contains(err.Error(), "no X-Timestamp header") {
		t.Errorf("no timestamp: %v", err)
	}
}

func TestSigningProperties(t *testing.T) {
	for rows, want := range map[[2][2]string]string{
		{{"algorithm", "hmac-sha256"}, {"encoding", "hex"}}: `the signing property "key" is required`,
		{{"key", "k"}, {"algorithm", "sha256"}}:             `the signing algorithm is hmac-sha256, hmac-sha1 or hmac-sha512, not "sha256"`,
		{{"key", "k"}, {"encoding", "base32"}}:              `the signature's encoding is hex or base64, not "base32"`,
		{{"key", "k"}, {"value", "sha256="}}:                `the header's value "sha256=" has no {signature}`,
		{{"key", "k"}, {"signs", "{timestamp}.{body}"}}:     "{timestamp} is signed, so the value or a timestamp header must carry it too",
		{{"key", "k"}, {"prefix", "sha256="}}:               `unknown signing property "prefix"`,
	} {
		_, err := Parse("X-Signature", []core.Pair{{Key: rows[0][0], Value: rows[0][1]}, {Key: rows[1][0], Value: rows[1][1]}},
			func(v string) (string, error) { return v, nil })
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: %v", rows, err)
		}
	}
}

// The Standard Webhooks reference test vector.
func TestStandardWebhooks(t *testing.T) {
	const key = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	h := http.Header{}
	h.Set("webhook-id", "msg_p5jXN8AQM9LWM0D4loKWxJek")
	h.Set("webhook-timestamp", "1614265330")
	h.Set("webhook-signature", "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=")
	if err := VerifyStandard(h, key, []byte(`{"test": 2432232314}`)); err != nil {
		t.Error(err)
	}
	if err := VerifyStandard(h, key, []byte(`{"test": 2432232315}`)); err == nil {
		t.Error("a changed body verified")
	}
	// Signed by us, it verifies; with several signatures, one is enough.
	signed := http.Header{}
	if err := SignStandard(signed, key, []byte(`{"reference": "PX-9201"}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	signed.Set("webhook-signature", "v1,b2xkIGtleQ== "+signed.Get("webhook-signature"))
	if err := VerifyStandard(signed, key, []byte(`{"reference": "PX-9201"}`)); err != nil {
		t.Error(err)
	}
	if err := SignStandard(http.Header{}, "not base64!", nil, time.Now()); err == nil {
		t.Error("a key that is not base64")
	}
}
