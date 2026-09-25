package rest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
	"github.com/nimbusxr/axx/internal/version"
)

// mediaTypeOf returns the lower-cased media type of a Content-Type value
// without its parameters ("application/json; charset=UTF-8" gives
// "application/json").
func mediaTypeOf(contentType string) string {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mt, _, _ = strings.Cut(contentType, ";")
	}
	return strings.ToLower(strings.TrimSpace(mt))
}

// isJSONMediaType reports whether a media type carries JSON:
// application/json, text/json and any +json type (application/problem+json,
// application/vnd.api+json, ...).
func isJSONMediaType(mt string) bool {
	return mt == core.MimeJSON || mt == core.MimeTextJSON || strings.HasSuffix(mt, "+json")
}

// resolveURL joins the service URL and a request path. A path that is an
// absolute URL is used as is.
func resolveURL(base, path string) (string, error) {
	if u, err := url.Parse(path); err == nil && u.IsAbs() && u.Host != "" {
		return u.String(), nil
	}
	b, err := url.Parse(base)
	if err != nil || !b.IsAbs() || b.Host == "" {
		return "", fmt.Errorf("invalid service URL %q: it must be absolute, e.g. http://localhost:8080", base)
	}
	b.RawQuery, b.Fragment = "", ""
	s := strings.TrimRight(b.String(), "/")
	if path != "" && !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "?") {
		s += "/"
	}
	u, err := url.Parse(s + path)
	if err != nil {
		return "", fmt.Errorf("invalid request path %q: %w", path, err)
	}
	return u.String(), nil
}

// validationPath is the URL handed to the OpenAPI validator: the request as
// the step wrote it (host plus the step's path and query), so a service URL
// with a context path matches the specification's paths.
func validationPath(full, stepPath string) string {
	u, err := url.Parse(full)
	if err != nil {
		return full
	}
	if p, err := url.Parse(stepPath); err == nil && p.IsAbs() {
		return full
	}
	root := url.URL{Scheme: u.Scheme, Host: u.Host}
	s := root.String()
	if !strings.HasPrefix(stepPath, "/") {
		s += "/"
	}
	if v, err := url.Parse(s + stepPath); err == nil {
		return v.String()
	}
	return full
}

// execute sends the request at index idx of svc, records the exchange and
// validates it against the service's OpenAPI specification.
func execute(sc *core.Scenario, svc *Service, idx int) error {
	set, err := load(sc.Suite())
	if err != nil {
		return err
	}
	req, err := svc.request(idx)
	if err != nil {
		return err
	}
	svc.mu.Lock()
	method, path, mimeType, payload := req.Method, req.Path, req.MimeType, req.Payload
	executed := req.exchange != nil
	hdr := req.header()
	svc.mu.Unlock()
	if executed {
		return errors.New("Response already set") //nolint:staticcheck // user-facing message
	}
	if method == "" {
		return errors.New("Method not set") //nolint:staticcheck // user-facing message
	}
	target, err := resolveURL(svc.URL, path)
	if err != nil {
		return err
	}

	var body []byte
	if payload != nil {
		text := *payload
		if mimeType == core.MimeForm {
			if text, err = jvalue.FormURLEncode(text); err != nil {
				return fmt.Errorf("could not form-encode the request payload: %w", err)
			}
		}
		body = []byte(text)
	}
	if hdr.Get("Accept") == "" {
		hdr.Set("Accept", "*/*")
	}
	if payload != nil && hdr.Get("Content-Type") == "" && mimeType != "" {
		// The payload step's media type, not a text/plain default that
		// servers would reject.
		hdr.Set("Content-Type", mimeType)
	}
	if hdr.Get("User-Agent") == "" {
		hdr.Set("User-Agent", userAgent())
	}

	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(body)
	}
	hreq, err := http.NewRequestWithContext(sc.Context(), method, target, rd)
	if err != nil {
		return fmt.Errorf("invalid request %s %s: %w", method, target, err)
	}
	hreq.Header = hdr
	if host := hdr.Get("Host"); host != "" {
		hreq.Host = host
	}

	start := time.Now()
	resp, err := set.client.Do(hreq)
	if err != nil {
		return fmt.Errorf("%s %s failed: %w", method, target, err)
	}
	respBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return fmt.Errorf("%s %s: reading the response body: %w", method, target, err)
	}
	ex := &Exchange{
		Method: method, URL: target, RequestHeader: hdr, RequestBody: body,
		Status: resp.StatusCode, Header: resp.Header, Body: respBody,
		Duration:       time.Since(start),
		validationPath: validationPath(target, path),
	}

	// Validate before publishing the exchange, which is then complete; the
	// response is kept even when validation fails.
	var specErr error
	if svc.OpenAPI != "" {
		sp, err := loadSpec(sc, set, svc)
		if err != nil {
			specErr = err
		} else {
			svc.mu.Lock()
			levels := set.levels.Merge(svc.levels)
			svc.mu.Unlock()
			ex.Issues = applyLevels(sp.validate(context.WithoutCancel(sc.Context()), ex), levels)
		}
	}
	svc.mu.Lock()
	req.exchange = ex
	svc.mu.Unlock()
	st := stateKey.Of(sc)
	st.mu.Lock()
	st.last = &lastExchange{service: svc.Name, ex: ex}
	st.mu.Unlock()
	logExchange(sc, ex)
	if specErr != nil {
		return specErr
	}
	return report(sc, ex)
}

// applyLevels gives each finding its level and drops the ignored ones.
func applyLevels(issues []Issue, levels Levels) []Issue {
	var kept []Issue
	for _, is := range issues {
		lv := levels.Resolve(is.Key)
		if lv == LevelIgnore {
			continue
		}
		is.Level = lv.String()
		kept = append(kept, is)
	}
	return kept
}

// report logs WARN and INFO findings and fails the step, listing every
// ERROR finding, when there are any.
func report(sc *core.Scenario, ex *Exchange) error {
	var failed []Issue
	for _, is := range ex.Issues {
		if is.Level == LevelError.String() {
			failed = append(failed, is)
			continue
		}
		sc.Log("OpenAPI %s %s: %s", is.Level, is.Key, is.Message)
	}
	if len(failed) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "OpenAPI validation failed for %s %s (status %d):", ex.Method, ex.URL, ex.Status)
	for _, is := range failed {
		fmt.Fprintf(&b, "\n  - %s: %s", is.Key, is.Message)
	}
	b.WriteString("\nTo relax a check, set its key (or a parent key) to WARN, INFO or IGNORE with \"Given the OpenAPI validation levels are:\" or openapi.levels in axx.yaml.")
	return &core.AssertionError{Message: b.String()}
}

func userAgent() string {
	v := version.Get().Version
	if v == "" {
		return "axx"
	}
	return "axx/" + v
}

func logExchange(sc *core.Scenario, ex *Exchange) {
	sc.Log("%s %s -> %d %s (%s, %d bytes)", ex.Method, ex.URL, ex.Status, http.StatusText(ex.Status),
		ex.Duration.Round(time.Millisecond), len(ex.Body))
	if len(ex.RequestBody) > 0 {
		sc.Attach(attachType(ex.RequestHeader.Get("Content-Type")), ex.RequestBody, "request body")
	}
	if len(ex.Body) > 0 {
		sc.Attach(attachType(ex.Header.Get("Content-Type")), ex.Body, "response body")
	}
}

func attachType(ct string) string {
	if mt := mediaTypeOf(ct); mt != "" {
		return mt
	}
	return "application/octet-stream"
}

// ---- failure context ----

// describeBodyLimit caps bodies shown in failure reports.
const describeBodyLimit = 2048

func truncated(b []byte) string {
	if len(b) <= describeBodyLimit {
		return string(b)
	}
	return strings.ToValidUTF8(string(b[:describeBodyLimit]), "") + fmt.Sprintf("... (%d more bytes)", len(b)-describeBodyLimit)
}

func flatHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = strings.Join(h[k], ", ")
	}
	return out
}

// describe reports the last executed request and its response when a
// scenario fails.
func (st *ScenarioContext) describe() any {
	st.mu.Lock()
	last := st.last
	st.mu.Unlock()
	if last == nil {
		return nil
	}
	ex := last.ex
	req := map[string]any{"method": ex.Method, "url": ex.URL, "headers": flatHeaders(ex.RequestHeader)}
	if len(ex.RequestBody) > 0 {
		req["body"] = truncated(ex.RequestBody)
	}
	resp := map[string]any{"status": ex.Status, "headers": flatHeaders(ex.Header), "durationMs": ex.Duration.Milliseconds()}
	if len(ex.Body) > 0 {
		resp["body"] = truncated(ex.Body)
	}
	out := map[string]any{"service": last.service, "request": req, "response": resp}
	if len(ex.Issues) > 0 {
		out["openapi"] = ex.Issues
	}
	return out
}
