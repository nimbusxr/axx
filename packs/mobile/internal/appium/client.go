// Package appium is a client of Appium's WebDriver protocol: the W3C
// WebDriver commands the mobile packs use, and Appium's mobile: commands.
// A Session is one app on one device, from its start to its end.
package appium

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// elementKey is the W3C WebDriver key of an element reference.
const elementKey = "element-6066-11e4-a52e-4f735466cecf"

// Client talks to one Appium server.
type Client struct {
	URL  string // the server, like http://127.0.0.1:4723
	HTTP *http.Client
}

// Error is an error Appium answered, with its W3C error code.
type Error struct {
	Status  int
	Code    string // like "no such element"
	Message string
}

func (e *Error) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("appium answered %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// IsNoSuchElement reports an element that is not there.
func IsNoSuchElement(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == "no such element"
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.URL, "/")+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var env struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return &Error{Status: res.StatusCode, Message: strings.TrimSpace(string(raw))}
	}
	if res.StatusCode >= 400 {
		var e struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(env.Value, &e)
		return &Error{Status: res.StatusCode, Code: e.Error, Message: e.Message}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Value, out)
}

// Session is an app on a device, which Appium drives.
type Session struct {
	c  *Client
	ID string
	// Capabilities are what Appium answered: the device and app it chose.
	Capabilities map[string]any
}

// NewSession starts a session: Appium starts (or resets) the app on the
// device the capabilities name.
func (c *Client) NewSession(ctx context.Context, caps map[string]any) (*Session, error) {
	var out struct {
		SessionID    string         `json:"sessionId"`
		Capabilities map[string]any `json:"capabilities"`
	}
	if err := c.do(ctx, http.MethodPost, "/session", map[string]any{"capabilities": map[string]any{"alwaysMatch": caps}}, &out); err != nil {
		return nil, err
	}
	return &Session{c: c, ID: out.SessionID, Capabilities: out.Capabilities}, nil
}

// Status reports whether the server is up.
func (c *Client) Status(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/status", nil, nil)
}

func (s *Session) path(p string) string { return "/session/" + s.ID + p }

// Delete ends the session.
func (s *Session) Delete(ctx context.Context) error {
	return s.c.do(ctx, http.MethodDelete, s.path(""), nil, nil)
}

// Element is an element of the screen, as Appium refers to it.
type Element struct {
	s  *Session
	ID string
}

// Find finds the first element a locator strategy matches.
func (s *Session) Find(ctx context.Context, using, value string) (*Element, error) {
	var out map[string]string
	if err := s.c.do(ctx, http.MethodPost, s.path("/element"), map[string]string{"using": using, "value": value}, &out); err != nil {
		return nil, err
	}
	return &Element{s: s, ID: out[elementKey]}, nil
}

// FindAll finds every element a locator strategy matches.
func (s *Session) FindAll(ctx context.Context, using, value string) ([]*Element, error) {
	var out []map[string]string
	if err := s.c.do(ctx, http.MethodPost, s.path("/elements"), map[string]string{"using": using, "value": value}, &out); err != nil {
		return nil, err
	}
	els := make([]*Element, len(out))
	for i, o := range out {
		els[i] = &Element{s: s, ID: o[elementKey]}
	}
	return els, nil
}

func (e *Element) path(p string) string { return e.s.path("/element/" + e.ID + p) }

// Click taps the element.
func (e *Element) Click(ctx context.Context) error {
	return e.s.c.do(ctx, http.MethodPost, e.path("/click"), map[string]any{}, nil)
}

// Clear empties a field.
func (e *Element) Clear(ctx context.Context) error {
	return e.s.c.do(ctx, http.MethodPost, e.path("/clear"), map[string]any{}, nil)
}

// Type types text into a field.
func (e *Element) Type(ctx context.Context, text string) error {
	return e.s.c.do(ctx, http.MethodPost, e.path("/value"), map[string]any{"text": text}, nil)
}

// Text is the element's text.
func (e *Element) Text(ctx context.Context) (string, error) {
	var out string
	err := e.s.c.do(ctx, http.MethodGet, e.path("/text"), nil, &out)
	return out, err
}

// Attribute is one of the element's attributes, as the platform names it.
func (e *Element) Attribute(ctx context.Context, name string) (string, error) {
	var out any
	if err := e.s.c.do(ctx, http.MethodGet, e.path("/attribute/"+name), nil, &out); err != nil {
		return "", err
	}
	if out == nil {
		return "", nil
	}
	return fmt.Sprint(out), nil
}

// Rect is where the element is on the screen, in points.
type Rect struct {
	X, Y, Width, Height float64
}

// Rect reads where the element is.
func (e *Element) Rect(ctx context.Context) (Rect, error) {
	var r Rect
	err := e.s.c.do(ctx, http.MethodGet, e.path("/rect"), nil, &r)
	return r, err
}

// Source is the screen's elements, as the platform's XML.
func (s *Session) Source(ctx context.Context) (string, error) {
	var out string
	err := s.c.do(ctx, http.MethodGet, s.path("/source"), nil, &out)
	return out, err
}

// Screenshot is a PNG of the screen.
func (s *Session) Screenshot(ctx context.Context) ([]byte, error) {
	var out string
	if err := s.c.do(ctx, http.MethodGet, s.path("/screenshot"), nil, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out)
}

// Window is the screen's size, in points.
func (s *Session) Window(ctx context.Context) (Rect, error) {
	var r Rect
	err := s.c.do(ctx, http.MethodGet, s.path("/window/rect"), nil, &r)
	return r, err
}

// Back is the platform's back navigation.
func (s *Session) Back(ctx context.Context) error {
	return s.c.do(ctx, http.MethodPost, s.path("/back"), map[string]any{}, nil)
}

// AcceptAlert accepts the alert in front; DismissAlert dismisses it, and
// AlertText is its text.
func (s *Session) AcceptAlert(ctx context.Context) error {
	return s.c.do(ctx, http.MethodPost, s.path("/alert/accept"), map[string]any{}, nil)
}

func (s *Session) DismissAlert(ctx context.Context) error {
	return s.c.do(ctx, http.MethodPost, s.path("/alert/dismiss"), map[string]any{}, nil)
}

func (s *Session) AlertText(ctx context.Context) (string, error) {
	var out string
	err := s.c.do(ctx, http.MethodGet, s.path("/alert/text"), nil, &out)
	return out, err
}

// Mobile runs one of Appium's mobile: commands, like "mobile: deepLink",
// with its arguments.
func (s *Session) Mobile(ctx context.Context, command string, args map[string]any, out any) error {
	if args == nil {
		args = map[string]any{}
	}
	return s.c.do(ctx, http.MethodPost, s.path("/execute/sync"), map[string]any{"script": "mobile: " + command, "args": []any{args}}, out)
}

// WaitReady waits until the server answers its status.
func (c *Client) WaitReady(ctx context.Context, within time.Duration) error {
	deadline := time.Now().Add(within)
	for {
		err := c.Status(ctx)
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the Appium server at %s did not start within %s: %w", c.URL, within, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
