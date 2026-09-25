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
	// headers keeps insertion order for stable messages.
	headers []headerEntry
}

type headerEntry struct {
	name string
	m    headerMatcher
}

func (p *pattern) withHeader(name string, m headerMatcher) {
	p.headers = append(p.headers, headerEntry{name, m})
}

// MarshalJSON renders {"method":..,"url":..,"headers":{..}}.
func (p *pattern) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"method":`)
	m, _ := json.Marshal(p.Method)
	b.Write(m)
	b.WriteString(`,"url":`)
	u, _ := json.Marshal(p.URL)
	b.Write(u)
	if len(p.headers) > 0 {
		b.WriteString(`,"headers":{`)
		// WireMock takes one matcher per header name; later constraints on the
		// same header replace earlier ones, as RequestPatternBuilder does.
		seen := map[string]int{}
		var order []headerEntry
		for _, h := range p.headers {
			if i, ok := seen[strings.ToLower(h.name)]; ok {
				order[i] = h
				continue
			}
			seen[strings.ToLower(h.name)] = len(order)
			order = append(order, h)
		}
		for i, h := range order {
			if i > 0 {
				b.WriteByte(',')
			}
			k, _ := json.Marshal(h.name)
			v, _ := json.Marshal(h.m)
			b.Write(k)
			b.WriteByte(':')
			b.Write(v)
		}
		b.WriteByte('}')
	}
	b.WriteByte('}')
	return b.Bytes(), nil
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
