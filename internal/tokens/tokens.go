// Package tokens holds the bearer tokens a scenario registers: JSON Web
// Tokens axx signs, and OAuth 2.0 client credentials tokens it gets from a
// token endpoint. The rest pack registers them; packs use their values in
// an Authorization header or a ${token:<name>} reference.
package tokens

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nimbusxr/axx/core"
)

// Rows are the rows of a token table, for the step docs.
var Rows = []core.TableRow{
	{Name: "algorithm", Takes: "how axx signs a JSON Web Token", Values: []string{"HS256", "HS384", "HS512", "RS256", "ES256"}, Default: "HS256"},
	{Name: "key", Takes: "the secret an HS token is signed with, like `${env:SHOP_TOKEN_KEY}`, or the PEM private key (a file of the project) of an RS or ES token"},
	{Name: "key id", Takes: "the `kid` in the token's header"},
	{Name: "claim.<name>", Takes: "a claim of the token: a JSON value (`true`, `42`, `[\"parcels:read\"]`) or text"},
	{Name: "expires in", Takes: "how long the token lasts from when it is used, like `5m`", Default: "5m"},
	{Name: "token url", Takes: "an OAuth 2.0 token endpoint axx gets a client credentials token from, instead of signing one"},
	{Name: "client id", Takes: "the client's ID, for the token endpoint"},
	{Name: "client secret", Takes: "the client's secret, like `${env:SHOP_CLIENT_SECRET}`"},
	{Name: "scope", Takes: "the scopes asked for, separated by spaces"},
	{Name: "audience", Takes: "the API the token is for, for token endpoints that ask for one"},
}

// Token is a token a scenario registered.
type Token struct {
	Name  string
	jwt   *jwtSpec
	oauth *oauthSpec
}

type jwtSpec struct {
	alg       string
	secret    []byte
	signer    crypto.Signer
	kid       string
	claims    map[string]any
	expiresIn time.Duration
}

type oauthSpec struct {
	url, clientID, clientSecret, scope, audience string
}

// Parse reads a token table, its values expanded; readFile reads a key
// file of the project.
func Parse(name string, pairs []core.Pair, expand func(string) (string, error), readFile func(string) ([]byte, error)) (*Token, error) {
	j := &jwtSpec{alg: "HS256", claims: map[string]any{}, expiresIn: 5 * time.Minute}
	o := &oauthSpec{}
	var key string
	var jwtRows, oauthRows []string
	for _, p := range pairs {
		v, err := expand(p.Value)
		if err != nil {
			return nil, err
		}
		v = strings.TrimSpace(v)
		if c, ok := strings.CutPrefix(p.Key, "claim."); ok {
			var val any = v
			if json.Valid([]byte(v)) {
				_ = json.Unmarshal([]byte(v), &val)
			}
			j.claims[c] = val
			jwtRows = append(jwtRows, p.Key)
			continue
		}
		switch p.Key {
		case "algorithm":
			j.alg = strings.ToUpper(v)
			jwtRows = append(jwtRows, p.Key)
		case "key":
			key = v
			jwtRows = append(jwtRows, p.Key)
		case "key id":
			j.kid = v
			jwtRows = append(jwtRows, p.Key)
		case "expires in":
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return nil, fmt.Errorf("the token's expires in is a duration like 5m, not %q", v)
			}
			j.expiresIn = d
			jwtRows = append(jwtRows, p.Key)
		case "token url":
			o.url = v
			oauthRows = append(oauthRows, p.Key)
		case "client id":
			o.clientID = v
			oauthRows = append(oauthRows, p.Key)
		case "client secret":
			o.clientSecret = v
			oauthRows = append(oauthRows, p.Key)
		case "scope":
			o.scope = v
			oauthRows = append(oauthRows, p.Key)
		case "audience":
			o.audience = v
			oauthRows = append(oauthRows, p.Key)
		default:
			return nil, fmt.Errorf("unknown token property %q (supported: algorithm, key, key id, claim.<name>, expires in, "+
				"token url, client id, client secret, scope, audience)", p.Key)
		}
	}
	if len(oauthRows) > 0 {
		if len(jwtRows) > 0 {
			return nil, fmt.Errorf("the token is signed by axx (%s) or got from a token url (%s), not both",
				strings.Join(jwtRows, ", "), strings.Join(oauthRows, ", "))
		}
		if o.url == "" || o.clientID == "" || o.clientSecret == "" {
			return nil, errors.New("a client credentials token needs a token url, a client id and a client secret")
		}
		return &Token{Name: name, oauth: o}, nil
	}
	if key == "" {
		return nil, errors.New(`the token property "key" is required: the secret or the private key axx signs it with`)
	}
	switch j.alg {
	case "HS256", "HS384", "HS512":
		j.secret = []byte(key)
	case "RS256", "ES256":
		signer, err := privateKey(key, readFile)
		if err != nil {
			return nil, err
		}
		_, isRSA := signer.(*rsa.PrivateKey)
		ec, isEC := signer.(*ecdsa.PrivateKey)
		if (j.alg == "RS256" && !isRSA) || (j.alg == "ES256" && (!isEC || ec.Curve.Params().BitSize != 256)) {
			return nil, fmt.Errorf("the token's key is not a key for %s", j.alg)
		}
		j.signer = signer
	default:
		return nil, fmt.Errorf("the token's algorithm is HS256, HS384, HS512, RS256 or ES256, not %q", j.alg)
	}
	return &Token{Name: name, jwt: j}, nil
}

// privateKey reads a PEM private key given in the table or in a file.
func privateKey(key string, readFile func(string) ([]byte, error)) (crypto.Signer, error) {
	data := []byte(key)
	if !strings.HasPrefix(key, "-----BEGIN") {
		var err error
		if data, err = readFile(key); err != nil {
			return nil, fmt.Errorf("the token's key file: %w", err)
		}
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("the token's key is not a PEM private key")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if s, ok := k.(crypto.Signer); ok {
			return s, nil
		}
	}
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	return nil, errors.New("the token's key is not an RSA or EC private key (PKCS #8, PKCS #1 or SEC 1)")
}

// Secrets are what the token is made or got with, which never show: its
// key and its client secret.
func (t *Token) Secrets() []string {
	if t.oauth != nil {
		return []string{t.oauth.clientSecret}
	}
	return []string{string(t.jwt.secret)}
}

var registry = core.NewStateKey("tokens", func(*core.Scenario) *core.Services[*Token] {
	return core.NewServices[*Token]("Token",
		`No token is registered in this scenario; register one with "the {word} token with the following properties:"`)
}, nil)

// Register adds a token to the scenario.
func Register(sc *core.Scenario, t *Token) error { return registry.Of(sc).Add(t.Name, t) }

// Value is the named token's value: a JSON Web Token signed now, or a
// client credentials token, got once for the run and again once it
// expires.
func Value(sc *core.Scenario, name string) (string, error) {
	t, err := registry.Of(sc).Get(name)
	if err != nil {
		return "", fmt.Errorf("no token named %q in this scenario; register it with \"the %s token with the following properties:\"", name, name)
	}
	if t.oauth != nil {
		return t.oauth.token(sc.Suite(), sc.Context())
	}
	return t.jwt.sign(time.Now())
}

// ---- JSON Web Tokens ----

func (j *jwtSpec) sign(now time.Time) (string, error) {
	header := map[string]any{"alg": j.alg, "typ": "JWT"}
	if j.kid != "" {
		header["kid"] = j.kid
	}
	claims := map[string]any{"iat": now.Unix(), "exp": now.Add(j.expiresIn).Unix()}
	for k, v := range j.claims {
		claims[k] = v
	}
	h, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	c, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signed := b64(h) + "." + b64(c)
	sig, err := j.signature([]byte(signed))
	if err != nil {
		return "", err
	}
	return signed + "." + b64(sig), nil
}

func (j *jwtSpec) signature(signed []byte) ([]byte, error) {
	switch j.alg {
	case "HS256", "HS384", "HS512":
		mac := hmac.New(map[string]func() hash.Hash{"HS256": sha256.New, "HS384": sha512.New384, "HS512": sha512.New}[j.alg], j.secret)
		mac.Write(signed)
		return mac.Sum(nil), nil
	case "RS256":
		d := sha256.Sum256(signed)
		return rsa.SignPKCS1v15(rand.Reader, j.signer.(*rsa.PrivateKey), crypto.SHA256, d[:])
	default: // ES256: r and s, 32 bytes each
		d := sha256.Sum256(signed)
		r, s, err := ecdsa.Sign(rand.Reader, j.signer.(*ecdsa.PrivateKey), d[:])
		if err != nil {
			return nil, err
		}
		out := make([]byte, 64)
		r.FillBytes(out[:32])
		s.FillBytes(out[32:])
		return out, nil
	}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// ---- OAuth 2.0 client credentials ----

type cached struct {
	value   string
	expires time.Time
}

type cache struct {
	mu sync.Mutex
	m  map[string]cached
}

// key tells tokens apart in the run's cache: a client with another secret
// never gets a token another got.
func (o *oauthSpec) key() string {
	secret := sha256.Sum256([]byte(o.clientSecret))
	return strings.Join([]string{o.url, o.clientID, b64(secret[:]), o.scope, o.audience}, "|")
}

func (o *oauthSpec) token(s *core.Suite, ctx context.Context) (string, error) {
	c, _ := core.Cached(s, "tokens/oauth", func() (*cache, error) { return &cache{m: map[string]cached{}}, nil })
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.m[o.key()]; ok && time.Now().Before(t.expires) {
		return t.value, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {o.clientID}, "client_secret": {o.clientSecret}}
	if o.scope != "" {
		form.Set("scope", o.scope)
	}
	if o.audience != "" {
		form.Set("audience", o.audience)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("the token url: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("getting a token from %s: %w", o.url, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &out) != nil || out.AccessToken == "" {
		return "", fmt.Errorf("the token endpoint %s answered %d, not a token: %s", o.url, res.StatusCode, strings.TrimSpace(string(body)))
	}
	expires := time.Now().Add(time.Hour)
	if out.ExpiresIn > 0 {
		// A token is got again a little before it expires.
		expires = time.Now().Add(time.Duration(out.ExpiresIn)*time.Second - min(30*time.Second, time.Duration(out.ExpiresIn)*time.Second/2))
	}
	c.m[o.key()] = cached{value: out.AccessToken, expires: expires}
	return out.AccessToken, nil
}
