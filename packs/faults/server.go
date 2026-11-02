package faults

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const requestTimeout = 10 * time.Second

// server is a Toxiproxy's API.
type server struct {
	name, url, token string
}

var client = &http.Client{Timeout: requestTimeout}

// request sends a request to the API, and returns its status and body. A status
// of 400 and over is the caller's to read.
func (s *server) request(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.url+path, rd)
	if err != nil {
		return 0, nil, err
	}
	// Toxiproxy refuses a request from a browser's page, by its User-Agent.
	req.Header.Set("User-Agent", "axx")
	req.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("could not reach the %s fault proxy: %w", s.name, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("could not read the %s fault proxy's answer: %w", s.name, err)
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return 0, nil, fmt.Errorf("the %s fault proxy refuses axx (status %d): give it the token of its TOXIPROXY_AUTH_TOKEN, in the token property", s.name, res.StatusCode)
	}
	return res.StatusCode, b, nil
}

// refused is what Toxiproxy said of a request it did not do.
func refused(status int, body []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return errors.New(e.Error)
	}
	return fmt.Errorf("status %d: %s", status, strings.TrimSpace(string(body)))
}

func (s *server) proxies(ctx context.Context) (map[string]json.RawMessage, error) {
	status, body, err := s.request(ctx, http.MethodGet, "/proxies", nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("the %s fault proxy could not list its connections: %w", s.name, refused(status, body))
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("the %s fault proxy's answer is not Toxiproxy's: %w", s.name, err)
	}
	return out, nil
}

func (s *server) addToxic(ctx context.Context, proxy string, t Toxic) error {
	status, body, err := s.request(ctx, http.MethodPost, "/proxies/"+url.PathEscape(proxy)+"/toxics", t)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("the %s fault proxy refused a %s toxic on the %s connection: %w", s.name, t.Type, proxy, refused(status, body))
	}
	return nil
}

// removeToxic removes a toxic; one already gone (a Toxiproxy restarted, say) is
// what was asked for.
func (s *server) removeToxic(ctx context.Context, proxy, toxic string) error {
	status, body, err := s.request(ctx, http.MethodDelete, "/proxies/"+url.PathEscape(proxy)+"/toxics/"+url.PathEscape(toxic), nil)
	if err != nil {
		return err
	}
	if status >= 400 && status != http.StatusNotFound {
		return fmt.Errorf("the %s fault proxy kept a toxic on the %s connection: %w", s.name, proxy, refused(status, body))
	}
	return nil
}

func (s *server) setEnabled(ctx context.Context, proxy string, enabled bool) error {
	status, body, err := s.request(ctx, http.MethodPost, "/proxies/"+url.PathEscape(proxy), map[string]bool{"enabled": enabled})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("the %s fault proxy could not change the %s connection: %w", s.name, proxy, refused(status, body))
	}
	return nil
}
