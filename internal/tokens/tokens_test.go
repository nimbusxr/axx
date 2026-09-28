package tokens

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
)

func parse(t *testing.T, rows ...[2]string) *Token {
	t.Helper()
	tok, err := Parse("shop", pairs(rows...), func(v string) (string, error) { return v, nil },
		func(string) ([]byte, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func pairs(rows ...[2]string) []core.Pair {
	var ps []core.Pair
	for _, r := range rows {
		ps = append(ps, core.Pair{Key: r[0], Value: r[1]})
	}
	return ps
}

// parts decodes a JWT's header and claims, and returns what it signed.
func parts(t *testing.T, jwt string) (header, claims map[string]any, signed string, sig []byte) {
	t.Helper()
	p := strings.Split(jwt, ".")
	if len(p) != 3 {
		t.Fatalf("not a JWT: %s", jwt)
	}
	for i, out := range []*map[string]any{&header, &claims} {
		b, err := base64.RawURLEncoding.DecodeString(p[i])
		if err != nil || json.Unmarshal(b, out) != nil {
			t.Fatalf("part %d: %s", i, p[i])
		}
	}
	sig, err := base64.RawURLEncoding.DecodeString(p[2])
	if err != nil {
		t.Fatal(err)
	}
	return header, claims, p[0] + "." + p[1], sig
}

func TestAnHS256Token(t *testing.T) {
	tok := parse(t, [2]string{"key", "shop-token-key"}, [2]string{"claim.shop", "maple-crafts"},
		[2]string{"claim.scopes", `["parcels:read"]`}, [2]string{"claim.tier", "2"}, [2]string{"key id", "shops-1"},
		[2]string{"expires in", "10m"})
	now := time.Unix(1790000000, 0)
	jwt, err := tok.jwt.sign(now)
	if err != nil {
		t.Fatal(err)
	}
	header, claims, signed, sig := parts(t, jwt)
	if header["alg"] != "HS256" || header["typ"] != "JWT" || header["kid"] != "shops-1" {
		t.Errorf("header %v", header)
	}
	if claims["shop"] != "maple-crafts" || claims["tier"] != float64(2) || claims["iat"] != float64(1790000000) ||
		claims["exp"] != float64(1790000600) || claims["scopes"].([]any)[0] != "parcels:read" {
		t.Errorf("claims %v", claims)
	}
	mac := hmac.New(sha256.New, []byte("shop-token-key"))
	mac.Write([]byte(signed))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		t.Error("the signature does not verify")
	}
}

func TestRS256AndES256Tokens(t *testing.T) {
	rk, _ := rsa.GenerateKey(rand.Reader, 2048)
	ek, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rder, _ := x509.MarshalPKCS8PrivateKey(rk)
	eder, _ := x509.MarshalECPrivateKey(ek)
	files := map[string][]byte{
		"keys/shops-rsa.pem": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rder}),
		"keys/shops-ec.pem":  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: eder}),
	}
	read := func(p string) ([]byte, error) { return files[p], nil }
	expand := func(v string) (string, error) { return v, nil }

	rs, err := Parse("shop", pairs([2]string{"algorithm", "RS256"}, [2]string{"key", "keys/shops-rsa.pem"}), expand, read)
	if err != nil {
		t.Fatal(err)
	}
	jwt, _ := rs.jwt.sign(time.Now())
	_, _, signed, sig := parts(t, jwt)
	d := sha256.Sum256([]byte(signed))
	if err := rsa.VerifyPKCS1v15(&rk.PublicKey, crypto.SHA256, d[:], sig); err != nil {
		t.Errorf("RS256: %v", err)
	}

	// Inline PEM works too.
	es, err := Parse("shop", pairs([2]string{"algorithm", "ES256"}, [2]string{"key", string(files["keys/shops-ec.pem"])}), expand, read)
	if err != nil {
		t.Fatal(err)
	}
	jwt, _ = es.jwt.sign(time.Now())
	_, _, signed, sig = parts(t, jwt)
	d = sha256.Sum256([]byte(signed))
	r, s := new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if len(sig) != 64 || !ecdsa.Verify(&ek.PublicKey, d[:], r, s) {
		t.Error("ES256 does not verify")
	}

	if _, err := Parse("shop", pairs([2]string{"algorithm", "ES256"}, [2]string{"key", "keys/shops-rsa.pem"}), expand, read); err == nil ||
		!strings.Contains(err.Error(), "not a key for ES256") {
		t.Errorf("an RSA key for ES256: %v", err)
	}
}

func TestTokenProperties(t *testing.T) {
	expand := func(v string) (string, error) { return v, nil }
	read := func(string) ([]byte, error) { return nil, nil }
	for _, tc := range []struct {
		rows [][2]string
		want string
	}{
		{[][2]string{{"claim.shop", "maple-crafts"}}, `the token property "key" is required`},
		{[][2]string{{"key", "k"}, {"algorithm", "none"}}, `the token's algorithm is HS256, HS384, HS512, RS256 or ES256, not "NONE"`},
		{[][2]string{{"key", "k"}, {"expires in", "soon"}}, `the token's expires in is a duration like 5m, not "soon"`},
		{[][2]string{{"key", "k"}, {"token url", "http://idp/token"}}, "signed by axx (key) or got from a token url (token url), not both"},
		{[][2]string{{"token url", "http://idp/token"}, {"client id", "maple-crafts"}}, "needs a token url, a client id and a client secret"},
		{[][2]string{{"key", "k"}, {"issuer", "parcels"}}, `unknown token property "issuer"`},
	} {
		if _, err := Parse("shop", pairs(tc.rows...), expand, read); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: %v", tc.rows, err)
		}
	}
}

func TestClientCredentials(t *testing.T) {
	var calls atomic.Int32
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "maple-crafts" ||
			r.Form.Get("client_secret") != "maple-secret" || r.Form.Get("scope") != "parcels:read" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error": "invalid_client"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token": "at-maple-1", "token_type": "Bearer", "expires_in": 3600}`))
	}))
	defer idp.Close()
	h := cloudtest.New(t)
	register := func(name, secret string) {
		tok := parse(t, [2]string{"token url", idp.URL}, [2]string{"client id", "maple-crafts"},
			[2]string{"client secret", secret}, [2]string{"scope", "parcels:read"})
		tok.Name = name
		if err := Register(h.SC, tok); err != nil {
			t.Fatal(err)
		}
	}
	register("shop", "maple-secret")
	for range 2 {
		if v, err := Value(h.SC, "shop"); err != nil || v != "at-maple-1" {
			t.Fatalf("%q %v", v, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("the token endpoint was asked %d times", calls.Load())
	}
	register("wrong", "not-the-secret")
	if _, err := Value(h.SC, "wrong"); err == nil || !strings.Contains(err.Error(), `answered 401, not a token: {"error": "invalid_client"}`) {
		t.Errorf("a refused client: %v", err)
	}
	if _, err := Value(h.SC, "courier"); err == nil || !strings.Contains(err.Error(), `no token named "courier" in this scenario`) {
		t.Errorf("no such token: %v", err)
	}
}
