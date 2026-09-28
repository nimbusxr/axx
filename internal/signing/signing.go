// Package signing signs HTTP requests the way webhooks are signed, and
// checks the signatures of the requests a service sent: an HMAC of what a
// template says (the body, a timestamp, the method, the path) in a header,
// and Standard Webhooks (https://www.standardwebhooks.com).
package signing

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // webhooks still sign with HMAC-SHA1, which HMAC keeps sound
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// Rows are the rows of a signing table, for the step docs.
var Rows = []core.TableRow{
	{Name: "key", Takes: "the secret the signature is made with, like `${env:COURIER_WEBHOOK_KEY}`", Required: true},
	{Name: "algorithm", Takes: "the HMAC", Values: []string{"hmac-sha256", "hmac-sha1", "hmac-sha512"}, Default: "hmac-sha256"},
	{Name: "signs", Takes: "what is signed: a template of `{body}`, `{timestamp}` (Unix seconds), `{method}` and `{path}` (with the query), like `{timestamp}.{body}`", Default: "`{body}`"},
	{Name: "encoding", Takes: "how the signature is written", Values: []string{"hex", "base64"}, Default: "hex"},
	{Name: "value", Takes: "the header's value: a template of `{signature}` and `{timestamp}`, like `sha256={signature}` or `t={timestamp},v1={signature}`", Default: "`{signature}`"},
	{Name: "timestamp header", Takes: "a header that also carries `{timestamp}`, as Slack's `X-Slack-Request-Timestamp` does"},
}

// Spec is how a request is signed in a header.
type Spec struct {
	Header          string
	Key             []byte
	Algorithm       string
	Signs           string
	Encoding        string
	Value           string
	TimestampHeader string
}

// Parse reads a signing table, its values expanded.
func Parse(header string, pairs []core.Pair, expand func(string) (string, error)) (Spec, error) {
	s := Spec{Header: header, Algorithm: "hmac-sha256", Signs: "{body}", Encoding: "hex", Value: "{signature}"}
	for _, p := range pairs {
		v, err := expand(p.Value)
		if err != nil {
			return s, err
		}
		switch p.Key {
		case "key":
			s.Key = []byte(v)
		case "algorithm":
			s.Algorithm = strings.ToLower(strings.TrimSpace(v))
		case "signs":
			s.Signs = v
		case "encoding":
			s.Encoding = strings.ToLower(strings.TrimSpace(v))
		case "value":
			s.Value = v
		case "timestamp header":
			s.TimestampHeader = strings.TrimSpace(v)
		default:
			return s, fmt.Errorf("unknown signing property %q (supported: key, algorithm, signs, encoding, value, timestamp header)", p.Key)
		}
	}
	switch {
	case len(s.Key) == 0:
		return s, errors.New(`the signing property "key" is required`)
	case newHash(s.Algorithm) == nil:
		return s, fmt.Errorf("the signing algorithm is hmac-sha256, hmac-sha1 or hmac-sha512, not %q", s.Algorithm)
	case s.Encoding != "hex" && s.Encoding != "base64":
		return s, fmt.Errorf("the signature's encoding is hex or base64, not %q", s.Encoding)
	case !strings.Contains(s.Value, "{signature}"):
		return s, fmt.Errorf("the header's value %q has no {signature}", s.Value)
	case strings.Contains(s.Signs, "{timestamp}") && !strings.Contains(s.Value, "{timestamp}") && s.TimestampHeader == "":
		return s, errors.New("{timestamp} is signed, so the value or a timestamp header must carry it too")
	}
	return s, nil
}

func newHash(algorithm string) func() hash.Hash {
	switch algorithm {
	case "hmac-sha256":
		return sha256.New
	case "hmac-sha1":
		return sha1.New
	case "hmac-sha512":
		return sha512.New
	}
	return nil
}

// signature is the encoded signature of what the template gives.
func (s Spec) signature(method, path, timestamp string, body []byte) string {
	signed := strings.NewReplacer("{timestamp}", timestamp, "{method}", method, "{path}", path).Replace(s.Signs)
	// The body goes in last, as it is: a "{timestamp}" in it is its own.
	before, after, _ := strings.Cut(signed, "{body}")
	mac := hmac.New(newHash(s.Algorithm), s.Key)
	mac.Write([]byte(before))
	if strings.Contains(s.Signs, "{body}") {
		mac.Write(body)
		mac.Write([]byte(after))
	}
	if s.Encoding == "base64" {
		return base64.StdEncoding.EncodeToString(mac.Sum(nil))
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// Sign signs a request about to be sent: path is its path with the query.
func (s Spec) Sign(h http.Header, method, path string, body []byte, now time.Time) {
	ts := strconv.FormatInt(now.Unix(), 10)
	sig := s.signature(method, path, ts, body)
	h.Set(s.Header, strings.NewReplacer("{signature}", sig, "{timestamp}", ts).Replace(s.Value))
	if s.TimestampHeader != "" {
		h.Set(s.TimestampHeader, ts)
	}
}

// Verify checks the signature of a request a service sent.
func (s Spec) Verify(h http.Header, method, path string, body []byte) error {
	got := h.Get(s.Header)
	if got == "" {
		return fmt.Errorf("it has no %s header", s.Header)
	}
	parts := valuePattern(s.Value).FindStringSubmatch(got)
	if parts == nil {
		return fmt.Errorf("its %s header is %q, which is not of the form %q", s.Header, got, s.Value)
	}
	sig, ts := parts[1], ""
	if len(parts) > 2 {
		ts = parts[2]
		if strings.Index(s.Value, "{timestamp}") < strings.Index(s.Value, "{signature}") {
			sig, ts = parts[2], parts[1]
		}
	}
	if s.TimestampHeader != "" && ts == "" {
		if ts = h.Get(s.TimestampHeader); ts == "" {
			return fmt.Errorf("it has no %s header", s.TimestampHeader)
		}
	}
	want := s.signature(method, path, ts, body)
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return fmt.Errorf("its %s header is %q, but signing what it sent with the key gives %q", s.Header, got,
			strings.NewReplacer("{signature}", want, "{timestamp}", ts).Replace(s.Value))
	}
	return nil
}

// valuePattern matches a header's value to its template: the signature
// and the timestamp are its groups, in the order they come.
func valuePattern(value string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	rest := value
	for {
		i := strings.Index(rest, "{")
		if i < 0 {
			break
		}
		switch {
		case strings.HasPrefix(rest[i:], "{signature}"):
			b.WriteString(regexp.QuoteMeta(rest[:i]) + "(.+?)")
			rest = rest[i+len("{signature}"):]
		case strings.HasPrefix(rest[i:], "{timestamp}"):
			b.WriteString(regexp.QuoteMeta(rest[:i]) + "([0-9]+)")
			rest = rest[i+len("{timestamp}"):]
		default:
			b.WriteString(regexp.QuoteMeta(rest[:i+1]))
			rest = rest[i+1:]
		}
	}
	b.WriteString(regexp.QuoteMeta(rest) + "$")
	return regexp.MustCompile(b.String())
}

// Standard Webhooks: a webhook-id, a webhook-timestamp, and a
// webhook-signature of "v1," and the base64 HMAC-SHA256 of
// "<id>.<timestamp>.<body>", with the key after "whsec_", base64-decoded.

// CheckStandardKey says what is wrong with a Standard Webhooks key, if
// anything.
func CheckStandardKey(key string) error {
	_, err := standardKey(key)
	return err
}

func standardKey(key string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(key), "whsec_"))
	if err != nil {
		return nil, errors.New(`a Standard Webhooks key is "whsec_" and the key in base64`)
	}
	return raw, nil
}

func standardSignature(key []byte, id, ts string, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// SignStandard signs a request as a Standard Webhook.
func SignStandard(h http.Header, key string, body []byte, now time.Time) error {
	k, err := standardKey(key)
	if err != nil {
		return err
	}
	var id [12]byte
	_, _ = rand.Read(id[:])
	msgID := "msg_" + hex.EncodeToString(id[:])
	ts := strconv.FormatInt(now.Unix(), 10)
	h.Set("webhook-id", msgID)
	h.Set("webhook-timestamp", ts)
	h.Set("webhook-signature", standardSignature(k, msgID, ts, body))
	return nil
}

// VerifyStandard checks that a request is a Standard Webhook signed with
// the key: one of the signatures its webhook-signature lists.
func VerifyStandard(h http.Header, key string, body []byte) error {
	k, err := standardKey(key)
	if err != nil {
		return err
	}
	for _, name := range []string{"webhook-id", "webhook-timestamp", "webhook-signature"} {
		if h.Get(name) == "" {
			return fmt.Errorf("it has no %s header", name)
		}
	}
	want := standardSignature(k, h.Get("webhook-id"), h.Get("webhook-timestamp"), body)
	for _, sig := range strings.Fields(h.Get("webhook-signature")) {
		if hmac.Equal([]byte(sig), []byte(want)) {
			return nil
		}
	}
	return fmt.Errorf("its webhook-signature is %q, but signing what it sent with the key gives %q", h.Get("webhook-signature"), want)
}
