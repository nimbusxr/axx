package mock

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

// headerMatcher is a WireMock string-value pattern.
type headerMatcher struct {
	EqualTo string `json:"equalTo,omitempty"`
	Matches string `json:"matches,omitempty"`
	Absent  bool   `json:"absent,omitempty"`
}

// MarshalJSON writes the one matcher m is: absent, a regular expression, or
// equal to a text, the empty text too.
func (m headerMatcher) MarshalJSON() ([]byte, error) {
	switch {
	case m.Absent:
		return []byte(`{"absent":true}`), nil
	case m.Matches != "":
		return json.Marshal(struct {
			Matches string `json:"matches"`
		}{m.Matches})
	}
	return json.Marshal(struct {
		EqualTo string `json:"equalTo"`
	}{m.EqualTo})
}

// pattern is a WireMock request pattern (admin API JSON form), and the
// properties a request's JSON body must have, which axx checks itself:
// WireMock compares the text of a body's values, and a value means what it
// says, with its type (ADR 0014).
type pattern struct {
	Method string
	URL    string
	// PathOnly matches URL as a path, whatever the query string.
	PathOnly bool
	// headers, body and form keep insertion order for stable messages.
	headers []headerEntry
	body    []propEntry   // JSONPath expressions into the JSON body
	form    []headerEntry // fields of a form-encoded body
	query   []headerEntry // query parameters
}

type headerEntry struct {
	name string
	m    headerMatcher
}

// propEntry is a property the JSON body must have: a JSONPath and its
// value, or `undefined` for none.
type propEntry struct {
	path string
	v    core.Value
}

func (p *pattern) withHeader(name string, m headerMatcher) {
	p.headers = append(p.headers, headerEntry{name, m})
}

// withProperty requires the JSON body to have a property (a JSONPath, "$."
// optional) of the value v, or none with `undefined`.
func (p *pattern) withProperty(path string, v core.Value) {
	p.body = append(p.body, propEntry{jsonx.Normalize(path), v})
}

// properties are the body's properties the pattern requires, the latest
// of each path.
func (p *pattern) properties() []propEntry {
	seen := map[string]int{}
	var out []propEntry
	for _, e := range p.body {
		if i, ok := seen[e.path]; ok {
			out[i] = e
			continue
		}
		seen[e.path] = len(out)
		out = append(out, e)
	}
	return out
}

// mismatch says why a request's body does not have the pattern's
// properties: the first one it lacks. It is "" when the body has them all.
func (p *pattern) mismatch(body []byte) (string, error) {
	text := string(body)
	doc, ok := jsonDoc(text)
	if !ok {
		return "its body is not JSON", nil
	}
	for _, e := range p.properties() {
		ok, err := jvalue.ValueIs(text, e.path, e.v)
		if err != nil {
			return "", fmt.Errorf("%s: %w", e.path, err)
		}
		if ok {
			continue
		}
		actual, found := valueAt(doc, e.path)
		switch {
		case !found:
			return "it has no " + e.path, nil
		case e.v.Undefined():
			return fmt.Sprintf("it has %s: %s", e.path, jsonx.MarshalValue(actual)), nil
		}
		if h := jvalue.TypeHint(e.path, e.v, actual); h != "" {
			return h, nil
		}
		return fmt.Sprintf("%s is %s, not %s", e.path, jsonx.MarshalValue(actual), e.v.JSON()), nil
	}
	return "", nil
}

// jsonDoc is a body's JSON object or array, when it is one.
func jsonDoc(text string) (any, bool) {
	doc, err := jsonx.Parse(text)
	if err != nil {
		return nil, false
	}
	switch doc.(type) {
	case *jsonx.Object, *jsonx.Array:
		return doc, true
	}
	return nil, false
}

// valueAt is the value at path in doc, when it has one.
func valueAt(doc any, path string) (any, bool) {
	v, _, err := jsonx.Read(doc, path)
	return v, err == nil
}

// withQuery requires the URL to have a query parameter that matches m.
func (p *pattern) withQuery(name string, m headerMatcher) {
	p.query = append(p.query, headerEntry{name, m})
}

// withField requires the form-encoded body to have a field that matches m.
func (p *pattern) withField(name string, m headerMatcher) {
	p.form = append(p.form, headerEntry{name, m})
}

// latest keeps one entry per name, the last one, where the first stood: a
// later constraint replaces an earlier one, as RequestPatternBuilder does.
func latest(entries []headerEntry, fold bool) []headerEntry {
	seen := map[string]int{}
	var out []headerEntry
	for _, e := range entries {
		key := e.name
		if fold {
			key = strings.ToLower(key)
		}
		if i, ok := seen[key]; ok {
			out[i] = e
			continue
		}
		seen[key] = len(out)
		out = append(out, e)
	}
	return out
}

// MarshalJSON renders {"method":..,"url" or "urlPath":..,"queryParameters":{..},"headers":{..},"formParameters":{..},"bodyPatterns":[..]}.
func (p *pattern) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"method":`)
	m, _ := json.Marshal(p.Method)
	b.Write(m)
	if p.PathOnly {
		b.WriteString(`,"urlPath":`)
	} else {
		b.WriteString(`,"url":`)
	}
	u, _ := json.Marshal(p.URL)
	b.Write(u)
	// WireMock takes one matcher per query parameter, header name
	// (case-insensitive) and form field.
	writeMatchers(&b, "queryParameters", latest(p.query, false))
	writeMatchers(&b, "headers", latest(p.headers, true))
	writeMatchers(&b, "formParameters", latest(p.form, false))
	b.WriteByte('}')
	return b.Bytes(), nil
}

// shown is the pattern as failures show it: WireMock's request pattern,
// and the payload properties axx checks, `<absent>` for `undefined`.
func (p *pattern) shown() []byte {
	b := bytes.NewBuffer(mustJSON(p))
	props := p.properties()
	if len(props) == 0 {
		return b.Bytes()
	}
	b.Truncate(b.Len() - 1)
	b.WriteString(`,"payloadProperties":{`)
	for i, e := range props {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.path)
		b.Write(k)
		b.WriteByte(':')
		if e.v.Undefined() {
			b.WriteString(`"<absent>"`)
			continue
		}
		b.WriteString(e.v.JSON())
	}
	b.WriteString("}}")
	return b.Bytes()
}

func writeMatchers(b *bytes.Buffer, key string, entries []headerEntry) {
	if len(entries) == 0 {
		return
	}
	b.WriteString(`,"` + key + `":{`)
	for i, e := range entries {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.name)
		v, _ := json.Marshal(e.m)
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
}

// describe renders the pattern for failure messages.
func (p *pattern) describe() string {
	j, _ := json.MarshalIndent(json.RawMessage(p.shown()), "", "  ")
	return string(j)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// client talks to a WireMock server's admin API.
type client struct {
	base string // e.g. http://localhost:8081 (+ optional path prefix)
	http *http.Client
}

func newClient(rawURL string, hc *http.Client) (*client, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid mocked service url %q", rawURL)
	}
	base := u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/")
	return &client{base: base, http: hc}, nil
}

type countResponse struct {
	Count int `json:"count"`
}

// count returns how many journaled requests match p.
func (c *client) count(ctx context.Context, p *pattern) (int, error) {
	if len(p.body) > 0 {
		found, err := c.find(ctx, p)
		return len(found), err
	}
	var out countResponse
	if err := c.post(ctx, "/__admin/requests/count", p, &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

// logged is a request the journal holds.
type logged struct {
	Method string `json:"method"`
	URL    string `json:"url"` // its path and query, as received
	// Headers maps a name to a value, or to its values when it repeats.
	Headers      map[string]json.RawMessage `json:"headers"`
	BodyAsBase64 string                     `json:"bodyAsBase64"`
}

// header is the request's headers.
func (l logged) header() http.Header {
	h := http.Header{}
	for k, raw := range l.Headers {
		var one string
		var many []string
		switch {
		case json.Unmarshal(raw, &one) == nil:
			h.Add(k, one)
		case json.Unmarshal(raw, &many) == nil:
			for _, v := range many {
				h.Add(k, v)
			}
		}
	}
	return h
}

// body is the request's body, byte for byte.
func (l logged) body() []byte {
	b, _ := base64.StdEncoding.DecodeString(l.BodyAsBase64)
	return b
}

// find returns the journaled requests that match p: those WireMock finds
// for its request pattern whose bodies have p's properties.
func (c *client) find(ctx context.Context, p *pattern) ([]logged, error) {
	found, err := c.findAll(ctx, p)
	if err != nil || len(p.body) == 0 {
		return found, err
	}
	var out []logged
	for _, l := range found {
		why, err := p.mismatch(l.body())
		if err != nil {
			return nil, err
		}
		if why == "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// findAll returns the journaled requests WireMock finds for p's request
// pattern, whatever their bodies.
func (c *client) findAll(ctx context.Context, p *pattern) ([]logged, error) {
	var out struct {
		Requests []logged `json:"requests"`
	}
	if err := c.post(ctx, "/__admin/requests/find", p, &out); err != nil {
		return nil, err
	}
	return out.Requests, nil
}

type nearMiss struct {
	Request struct {
		Method  string         `json:"method"`
		URL     string         `json:"url"`
		Headers map[string]any `json:"headers"`
	} `json:"request"`
	MatchResult struct {
		Distance float64 `json:"distance"`
	} `json:"matchResult"`
}

// nearMisses returns the journaled requests closest to p.
func (c *client) nearMisses(ctx context.Context, p *pattern) ([]nearMiss, error) {
	var out struct {
		NearMisses []nearMiss `json:"nearMisses"`
	}
	if err := c.post(ctx, "/__admin/near-misses/request-pattern", p, &out); err != nil {
		return nil, err
	}
	return out.NearMisses, nil
}

func (c *client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodPost, path, bytes.NewReader(b), out)
}

func (c *client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach WireMock admin API at %s: %w", c.base, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("WireMock admin API %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("unexpected WireMock admin API response from %s: %w", path, err)
		}
	}
	return nil
}
