// Package appiumtest is a fake Appium server for the mobile packs' tests:
// an app of screens, each a page source, which taps move between.
package appiumtest

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// App is the fake app: its screens, and where taps go.
type App struct {
	// Screens are page sources by name; Start is the one the app starts on.
	Screens map[string]string
	Start   string
	// Taps moves to a screen when an element is tapped: by the XPath the
	// packs find it with, or by the text it shows.
	Taps map[string]string
	// Links moves to a screen when a deep link opens.
	Links map[string]string
	// Alerts are the texts of the dialogs the system shows over screens; Taps
	// moves on "accept <screen>" and "dismiss <screen>".
	Alerts map[string]string
	// Notifications is the screen of the device's notifications: opened over
	// the app (Android's shade, or a finger from iOS's top edge), and closed
	// by back or by activating the app.
	Notifications string
}

// Server is a running fake.
type Server struct {
	URL string
	app App

	mu       sync.Mutex
	screen   string
	sessions int
	elements map[string]string // element ID -> the XPath it was found by
	typed    map[string]string // XPath -> what was typed
	commands []string          // mobile: commands and other actions, in order
	caps     map[string]any
	shots    int
	state    int    // the app's state, as XCUITest reports it: 1 not running, 4 in front
	under    string // the screen under the notifications, while they are open
	moving   map[string]int
}

// Start runs the fake for a test.
func Start(t *testing.T, app App) *Server {
	t.Helper()
	s := &Server{app: app, screen: app.Start, elements: map[string]string{}, typed: map[string]string{}, state: 1}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// Screen is the screen the app shows.
func (s *Server) Screen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screen
}

// Moving has the next finds of a control miss it, as they do while it
// moves in, like a dialog's button: by the XPath the packs find it with.
func (s *Server) Moving(xpath string, misses int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.moving == nil {
		s.moving = map[string]int{}
	}
	s.moving[xpath] = misses
}

// Show moves the app to a screen, as the app would by itself.
func (s *Server) Show(screen string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.screen = screen
}

// Commands are the mobile: commands and actions the packs sent.
func (s *Server) Commands() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.commands...)
}

// Typed is what was typed into the element an XPath finds.
func (s *Server) Typed(xpath string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.typed[xpath]
}

// Capabilities are the last session's capabilities.
func (s *Server) Capabilities() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.caps
}

var (
	sessionRE = regexp.MustCompile(`^/session/([^/]+)(/.*)?$`)
	elementRE = regexp.MustCompile(`^/element/([^/]+)/([a-z]+)$`)
	boundsRE  = regexp.MustCompile(`@bounds=["']([^"']+)["']`)
	// The iOS pack finds an element by its type and where it is.
	iosRE = regexp.MustCompile(`^//(XCUIElementType\w+)\[@x="([^"]*)" and @y="([^"]*)" and @width="([^"]*)" and @height="([^"]*)"\]$`)
)

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	defer s.mu.Unlock()
	reply := func(v any) { _ = json.NewEncoder(w).Encode(map[string]any{"value": v}) }
	fail := func(code, msg string) {
		w.WriteHeader(http.StatusNotFound)
		reply(map[string]string{"error": code, "message": msg})
	}
	if r.URL.Path == "/status" {
		reply(map[string]any{"ready": true})
		return
	}
	if r.URL.Path == "/session" && r.Method == http.MethodPost {
		s.sessions++
		s.caps, _ = body["capabilities"].(map[string]any)["alwaysMatch"].(map[string]any)
		s.screen = s.app.Start
		reply(map[string]any{"sessionId": fmt.Sprintf("s%d", s.sessions), "capabilities": map[string]any{"appPackage": "example.parcels.courier"}})
		return
	}
	m := sessionRE.FindStringSubmatch(r.URL.Path)
	if m == nil {
		fail("unknown command", r.URL.Path)
		return
	}
	path := m[2]
	switch {
	case r.Method == http.MethodDelete && path == "":
		s.commands = append(s.commands, "end session")
		reply(nil)
	case path == "/source":
		reply(s.app.Screens[s.screen])
	case path == "/screenshot":
		reply(base64.StdEncoding.EncodeToString(s.shot()))
	case path == "/window/rect":
		reply(map[string]float64{"x": 0, "y": 0, "width": 1080, "height": 2400})
	case path == "/alert/text":
		text, ok := s.app.Alerts[s.screen]
		if !ok {
			fail("no such alert", "No alert is present on the screen")
			return
		}
		reply(text)
	case path == "/alert/accept" || path == "/alert/dismiss":
		if _, ok := s.app.Alerts[s.screen]; !ok {
			fail("no such alert", "No alert is present on the screen")
			return
		}
		verb := strings.TrimPrefix(path, "/alert/")
		s.commands = append(s.commands, verb+" alert")
		if next, ok := s.app.Taps[verb+" "+s.screen]; ok {
			s.screen = next
		}
		reply(nil)
	case path == "/back":
		s.commands = append(s.commands, "back")
		s.closeNotifications()
		reply(nil)
	case path == "/actions":
		s.commands = append(s.commands, "drag")
		s.openNotifications()
		reply(nil)
	case path == "/appium/settings":
		settings, _ := json.Marshal(body["settings"])
		s.commands = append(s.commands, "settings "+string(settings))
		reply(nil)
	case path == "/element" || path == "/elements":
		using, _ := body["using"].(string)
		value, _ := body["value"].(string)
		if s.moving[value] > 0 {
			s.moving[value]--
			fail("no such element", "An element could not be located on the page using the given search parameters.")
			return
		}
		if !s.has(using, value) {
			fail("no such element", "An element could not be located on the page using the given search parameters.")
			return
		}
		id := fmt.Sprintf("e%d", len(s.elements)+1)
		s.elements[id] = value
		ref := map[string]string{"element-6066-11e4-a52e-4f735466cecf": id}
		if path == "/elements" {
			reply([]any{ref})
			return
		}
		reply(ref)
	case path == "/execute/sync":
		script, _ := body["script"].(string)
		args, _ := body["args"].([]any)
		arg, _ := json.Marshal(args)
		s.commands = append(s.commands, script+" "+string(arg))
		if script == "mobile: deepLink" && len(args) > 0 {
			url, _ := args[0].(map[string]any)["url"].(string)
			if next, ok := s.app.Links[url]; ok {
				s.screen = next
			}
		}
		switch script {
		case "mobile: openNotifications":
			s.openNotifications()
		case "mobile: activateApp":
			s.closeNotifications()
			s.state = 4
		case "mobile: launchApp":
			s.state = 4
		case "mobile: terminateApp":
			s.state = 1
		case "mobile: backgroundApp":
			s.state = 3
		case "mobile: queryAppState":
			reply(s.state)
			return
		case "mobile: deviceScreenInfo":
			reply(map[string]any{"statusBarSize": map[string]any{"width": 54, "height": 5}, "scale": 2})
			return
		case "mobile: scrollGesture":
			reply(false)
			return
		}
		if script == "mobile: getSystemBars" {
			reply(map[string]any{"statusBar": map[string]any{"visible": true, "x": 0, "y": 0, "width": 108, "height": 10}})
			return
		}
		reply(nil)
	default:
		em := elementRE.FindStringSubmatch(path)
		if em == nil {
			fail("unknown command", path)
			return
		}
		xpath := s.elements[em[1]]
		switch em[2] {
		case "click":
			s.commands = append(s.commands, "tap "+xpath)
			if next, ok := s.app.Taps[xpath]; ok {
				s.screen = next
			}
			reply(nil)
		case "clear":
			s.typed[xpath] = ""
			reply(nil)
		case "value":
			text, _ := body["text"].(string)
			s.typed[xpath] += text
			s.commands = append(s.commands, "type "+text)
			reply(nil)
		default:
			fail("unknown command", path)
		}
	}
}

func (s *Server) openNotifications() {
	if s.app.Notifications != "" && s.under == "" {
		s.under, s.screen = s.screen, s.app.Notifications
	}
}

func (s *Server) closeNotifications() {
	if s.under != "" {
		s.screen, s.under = s.under, ""
	}
}

// Shots are the screenshots of screens, 108x240 PNGs: each screen a colour
// of its own unless set here.
var Shots = map[string]color.Color{}

// shot is a PNG of the screen: a colour per screen, the status bar a colour
// of its own that changes with each shot, as a clock does.
func (s *Server) shot() []byte {
	c, ok := Shots[s.screen]
	if !ok {
		sum := 0
		for _, r := range s.screen {
			sum = sum*31 + int(r)
		}
		c = color.RGBA{uint8(sum), uint8(sum >> 8), uint8(sum >> 16), 0xff}
	}
	s.shots++
	img := image.NewRGBA(image.Rect(0, 0, 108, 240))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, 108, 10), image.NewUniform(color.RGBA{uint8(s.shots), 0, 0, 0xff}), image.Point{}, draw.Src)
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

// has reports whether the screen has what a locator finds: an XPath by
// bounds, or by type and place, as the packs find elements, or an id.
func (s *Server) has(using, value string) bool {
	src := s.app.Screens[s.screen]
	switch using {
	case "xpath":
		if b := boundsRE.FindStringSubmatch(value); b != nil {
			return strings.Contains(src, `bounds="`+b[1]+`"`)
		}
		if b := iosRE.FindStringSubmatch(value); b != nil {
			at := fmt.Sprintf(`x="%s" y="%s" width="%s" height="%s"`, b[2], b[3], b[4], b[5])
			return regexp.MustCompile(`<` + b[1] + `\s[^>]*` + regexp.QuoteMeta(at)).MatchString(src)
		}
		return false
	case "id":
		return strings.Contains(src, `resource-id="`+value+`"`) || strings.Contains(src, ` name="`+value+`"`)
	}
	return false
}
