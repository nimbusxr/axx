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
	"sync"
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
	// Touched, when set, hears of each touch the session makes: a tap, a
	// swipe, a scroll, a drag (a video draws them).
	Touched func(Touch)

	mu      sync.Mutex
	win     Rect
	winRead bool
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
	if e.s.Touched == nil {
		return e.s.c.do(ctx, http.MethodPost, e.path("/click"), map[string]any{}, nil)
	}
	// The touch shows as it is sent: a click returns once the app has
	// settled after it, which can take longer than a tap shows.
	if r, err := e.Rect(ctx); err == nil {
		c := Point{r.X + r.Width/2, r.Y + r.Height/2}
		e.s.touched(Touch{From: c, To: c, At: time.Now()})
	}
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

// PullFile reads a file of the device, as Appium names it: "@<bundle
// id>:data/Documents/x" for an iOS app's, "@<package>/files/x" for a
// debuggable Android app's.
func (s *Session) PullFile(ctx context.Context, path string) ([]byte, error) {
	var out string
	if err := s.c.do(ctx, http.MethodPost, s.path("/appium/device/pull_file"), map[string]any{"path": path}, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out)
}

// PushFile writes a file of the device, named as PullFile names it.
func (s *Session) PushFile(ctx context.Context, path string, body []byte) error {
	return s.c.do(ctx, http.MethodPost, s.path("/appium/device/push_file"),
		map[string]any{"path": path, "data": base64.StdEncoding.EncodeToString(body)}, nil)
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

// Drag moves a finger from one point of the screen to another, in points,
// as a swipe from the screen's edge does.
func (s *Session) Drag(ctx context.Context, fromX, fromY, toX, toY float64, d time.Duration) error {
	finger := map[string]any{
		"type": "pointer", "id": "finger", "parameters": map[string]any{"pointerType": "touch"},
		"actions": []any{
			map[string]any{"type": "pointerMove", "duration": 0, "x": int(fromX), "y": int(fromY)},
			map[string]any{"type": "pointerDown", "button": 0},
			map[string]any{"type": "pause", "duration": 100},
			map[string]any{"type": "pointerMove", "duration": d.Milliseconds(), "x": int(toX), "y": int(toY)},
			map[string]any{"type": "pointerUp", "button": 0},
		},
	}
	s.touched(Touch{From: Point{fromX, fromY}, To: Point{toX, toY}, At: time.Now().Add(100 * time.Millisecond), Length: d})
	return s.c.do(ctx, http.MethodPost, s.path("/actions"), map[string]any{"actions": []any{finger}}, nil)
}

// KeyboardShown reports whether the device shows its on-screen keyboard.
func (s *Session) KeyboardShown(ctx context.Context) (bool, error) {
	var out bool
	err := s.c.do(ctx, http.MethodGet, s.path("/appium/device/is_keyboard_shown"), nil, &out)
	return out, err
}

// Settings changes the driver's settings for the session.
func (s *Session) Settings(ctx context.Context, settings map[string]any) error {
	return s.c.do(ctx, http.MethodPost, s.path("/appium/settings"), map[string]any{"settings": settings}, nil)
}

// Mobile runs one of Appium's mobile: commands, like "mobile: deepLink",
// with its arguments.
func (s *Session) Mobile(ctx context.Context, command string, args map[string]any, out any) error {
	if args == nil {
		args = map[string]any{}
	}
	if s.Touched != nil {
		if t, ok := s.gesture(ctx, command, args); ok {
			t.At = time.Now()
			s.touched(t)
		}
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
