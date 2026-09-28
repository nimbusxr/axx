package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// The Standard Webhooks reference test vector.
func TestStandardSignature(t *testing.T) {
	key, _ := base64.StdEncoding.DecodeString("MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw")
	got := standardSignature(key, "msg_p5jXN8AQM9LWM0D4loKWxJek", "1614265330", []byte(`{"test": 2432232314}`))
	if got != "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=" {
		t.Errorf("signature %s", got)
	}
}

func TestCourierSignature(t *testing.T) {
	s := &service{courierKey: []byte("example-courier-callback-key")}
	body := []byte(`{"reference": "PX-WHK-9101", "status": "COLLECTED"}`)
	mac := hmac.New(sha256.New, s.courierKey)
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !s.courierSigned(sig, body) {
		t.Error("the courier's signature was refused")
	}
	for _, bad := range []string{"", strings.TrimPrefix(sig, "sha256="), sig[:len(sig)-1] + "0"} {
		if s.courierSigned(bad, body) {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestShopTokens(t *testing.T) {
	key := []byte("example-shop-token-key")
	now := time.Now()
	tok := signJWT(key, map[string]any{"shop": "hawthorn-home", "exp": now.Add(time.Minute).Unix()})
	claims, err := verifyJWT(key, tok)
	if err != nil || claims["shop"] != "hawthorn-home" {
		t.Fatalf("%v %v", claims, err)
	}
	for want, token := range map[string]string{
		"the token's signature is not the service's": signJWT([]byte("another-key"), map[string]any{"exp": now.Add(time.Minute).Unix()}),
		"the token has expired":                      signJWT(key, map[string]any{"exp": now.Add(-time.Minute).Unix()}),
		"no bearer token":                            "",
	} {
		if _, err := verifyJWT(key, token); err == nil || err.Error() != want {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}
