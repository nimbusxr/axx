package mock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/compat/jsonx"
)

// headerMatcher is a WireMock string-value pattern.
type headerMatcher struct {
	EqualTo string `json:"equalTo,omitempty"`
	Matches string `json:"matches,omitempty"`
	Absent  bool   `json:"absent,omitempty"`
}

// pattern is a WireMock request pattern (admin API JSON form).
type pattern struct {
	Method string
	URL    string
	// PathOnly matches URL as a path, whatever the query string.
	PathOnly bool
	// headers, body and form keep insertion order for stable messages.
	headers []headerEntry
	body    []headerEntry // JSONPath expressions into the JSON body
	form    []headerEntry // fields of a form-encoded body
	query   []headerEntry // query parameters
}

type headerEntry struct {
	name string
	m    headerMatcher
}

func (p *pattern) withHeader(name string, m headerMatcher) {
	p.headers = append(p.headers, headerEntry{name, m})
}

// withProperty requires the JSON body to have a property (a JSONPath, "$."
// optional) that matches m.
func (p *pattern) withProperty(path string, m headerMatcher) {
	p.body = append(p.body, headerEntry{jsonx.Normalize(path), m})
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
	if body := latest(p.body, false); len(body) > 0 {
		b.WriteString(`,"bodyPatterns":[`)
		for i, e := range body {
			if i > 0 {
				b.WriteByte(',')
			}
			v, _ := json.Marshal(map[string]any{"matchesJsonPath": jsonPathMatcher{Expression: e.name, headerMatcher: e.m}})
			b.Write(v)
		}
		b.WriteByte(']')
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// jsonPathMatcher is WireMock's matchesJsonPath with a matcher of the value
// the expression selects.
type jsonPathMatcher struct {
	Expression string `json:"expression"`
	headerMatcher
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
	j, _ := json.MarshalIndent(json.RawMessage(mustJSON(p)), "", "  ")
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
	var out countResponse
	if err := c.post(ctx, "/__admin/requests/count", p, &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

// logged is a request the journal holds.
type logged struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

// find returns the journaled requests that match p.
func (c *client) find(ctx context.Context, p *pattern) ([]logged, error) {
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
