package mobileandroid

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
	"github.com/nimbusxr/axx/packs/mobile/internal/appium/appiumtest"
)

// scrollStandIn is a stand-in for axx's scroll service: it answers each request with what the
// test gives for it, and keeps the requests.
type scrollStandIn struct {
	mu   sync.Mutex
	asks []map[string]any
}

func (s *scrollStandIn) asked() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.asks...)
}

// standInScrolls starts the stand-in, and gives a client of it.
func standInScrolls(t *testing.T, answer func(n int, req map[string]any) map[string]any) (*scrolls, *scrollStandIn) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	in := &scrollStandIn{}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				lines := bufio.NewScanner(conn)
				for lines.Scan() {
					var req map[string]any
					if json.Unmarshal(lines.Bytes(), &req) != nil {
						return
					}
					in.mu.Lock()
					in.asks = append(in.asks, req)
					n := len(in.asks)
					in.mu.Unlock()
					out, _ := json.Marshal(answer(n, req))
					if _, err := conn.Write(append(out, '\n')); err != nil {
						return
					}
				}
			}()
		}
	}()
	sv := &scrolls{addr: l.Addr().String()}
	t.Cleanup(sv.close)
	return sv, in
}

// formPage is a web page's form, longer than the screen: a question and a field on it, and a
// button the screen lists out of view.
const formPage = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy index="0" class="hierarchy" rotation="0" width="1080" height="2400">
<android.widget.FrameLayout index="0" package="example.parcels.courier" class="android.widget.FrameLayout" text="" clickable="false" enabled="true" scrollable="false" bounds="[0,0][1080,2400]" displayed="true">
<android.webkit.WebView index="0" package="example.parcels.courier" class="android.webkit.WebView" text="Report a delivery problem" clickable="false" enabled="true" scrollable="true" bounds="[0,0][1080,2400]" displayed="true">
<android.view.View index="0" package="example.parcels.courier" class="android.view.View" text="Who did you speak to?" clickable="false" enabled="true" scrollable="false" bounds="[42,500][1042,566]" displayed="true"/>
<android.widget.EditText index="1" package="example.parcels.courier" class="android.widget.EditText" text="Mara Lindqvist" resource-id="q19" clickable="true" enabled="true" scrollable="false" bounds="[42,580][1042,700]" displayed="true" hint="Who did you speak to?"/>
<android.widget.Button index="2" package="example.parcels.courier" class="android.widget.Button" text="Send report" resource-id="send" clickable="true" enabled="true" scrollable="false" bounds="[42,4226][1042,2402]" displayed="false"/>
</android.webkit.WebView>
</android.widget.FrameLayout></hierarchy>`

// deviceWith is a device whose screen is formPage, with a scroll service and an adb that says
// of the keyboard what windows gives (dumpsys window's lines of the input method).
func deviceWith(t *testing.T, sv *scrolls, windows string) *running {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	adb := filepath.Join(t.TempDir(), "adb")
	if err := os.WriteFile(adb, []byte("#!/bin/sh\ncase \"$*\" in\n*'dumpsys window'*) printf '%s\\n' '"+windows+"' ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	server := appiumtest.Start(t, appiumtest.App{Screens: map[string]string{"form": formPage}, Start: "form"})
	session, err := (&appium.Client{URL: server.URL, HTTP: &http.Client{}}).NewSession(t.Context(), map[string]any{"platformName": "Android"})
	if err != nil {
		t.Fatal(err)
	}
	return &running{app: &app{name: "reports"}, sdk: &sdk{adb: adb}, dev: &device{serial: "emulator-5554", scrolls: sv}, session: session}
}

// node is the control of the form that shows that text, with its place in the tree.
func node(t *testing.T, d *running, text string) *mobilecore.Node {
	t.Helper()
	s, err := d.Screen(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// A field by its label, anything else by its text.
	for _, n := range s.Nodes {
		if n.Role == mobilecore.RoleField && n.Hint == text {
			return n
		}
	}
	for _, n := range s.Nodes {
		if n.Text == text {
			return n
		}
	}
	t.Fatalf("the form has no %q", text)
	return nil
}

// A control the screen lists out of view is brought onto it by its view, which is asked to show
// it: nothing is swiped. A view still busy with the scroll before takes the action and does not
// move, so it is asked again, for the control where it is by then.
func TestAControlOutOfViewIsShownByItsView(t *testing.T) {
	sv, in := standInScrolls(t, func(n int, req map[string]any) map[string]any {
		if n == 1 {
			return map[string]any{"found": true, "offered": true, "performed": true, "moved": false, "bounds": []int{42, 4000, 1042, 2402}}
		}
		return map[string]any{"found": true, "offered": true, "performed": true, "moved": true, "bounds": []int{42, 2194, 1042, 2320}}
	})
	d := deviceWith(t, sv, "")
	if !d.show(t.Context(), node(t, d, "Send report")) {
		t.Fatal("the button at the page's end is not shown")
	}
	asks := in.asked()
	if len(asks) != 2 {
		t.Fatalf("the service was asked %d times, want twice: %v", len(asks), asks)
	}
	first := map[string]any{"op": "show", "class": "android.widget.Button", "text": "Send report", "desc": "", "id": "send", "bounds": []any{42.0, 4226.0, 1042.0, 2402.0}}
	if !reflect.DeepEqual(asks[0], first) {
		t.Errorf("the first request names the button as the screen gave it:\n got %v\nwant %v", asks[0], first)
	}
	if got := fmt.Sprint(asks[1]["bounds"]); got != "[42 4000 1042 2402]" {
		t.Errorf("the second request looks for the button where the first answer left it: %s", got)
	}
	// A field is found without its text, which is what it holds.
	sv2, in2 := standInScrolls(t, func(int, map[string]any) map[string]any {
		return map[string]any{"found": true, "offered": true, "performed": true, "moved": true, "bounds": []int{42, 580, 1042, 700}}
	})
	d = deviceWith(t, sv2, "")
	if !d.show(t.Context(), node(t, d, "Who did you speak to?")) {
		t.Fatal("the field is not shown")
	}
	if _, has := in2.asked()[0]["text"]; has {
		t.Errorf("a field is asked for by its text: %v", in2.asked()[0])
	}
	// One the service does not find, or one whose view will not show it, is not shown.
	for _, answer := range []map[string]any{{"found": false}, {"found": true, "offered": false}} {
		sv3, _ := standInScrolls(t, func(int, map[string]any) map[string]any { return answer })
		d = deviceWith(t, sv3, "")
		if d.show(t.Context(), node(t, d, "Send report")) {
			t.Errorf("the button is taken for shown after the service answered %v", answer)
		}
	}
}

// A control its view shows under the keyboard is scrolled above it: the view is asked for a page
// more. Where the view is at its end, the control is not shown, and the step closes the keyboard.
// So it is where a page went back to the field being typed in, a moment after it showed the
// control.
func TestAControlUnderTheKeyboardIsScrolledAboveIt(t *testing.T) {
	keyboard := "InsetsSource id=3 type=ime frame=[0,1633][1080,2400] visibleFrame=[0,1633][1080,2400] visible=true flags= sideHint=BOTTOM"
	for _, more := range []bool{true, false} {
		var scrolled bool
		sv, in := standInScrolls(t, func(_ int, req map[string]any) map[string]any {
			switch req["op"] {
			case "scroll":
				scrolled = more
				return map[string]any{"found": true, "offered": more, "scrolls": true, "performed": more, "moved": more}
			case "where":
				if scrolled {
					return map[string]any{"found": true, "bounds": []int{42, 1170, 1042, 1296}}
				}
			}
			// Under the keyboard.
			return map[string]any{"found": true, "offered": true, "performed": true, "moved": true, "bounds": []int{42, 1900, 1042, 2026}}
		})
		d := deviceWith(t, sv, keyboard)
		if got := d.show(t.Context(), node(t, d, "Send report")); got != more {
			t.Errorf("a view that can scroll on (%v): the button is shown: %v", more, got)
		}
		var ops []string
		for _, a := range in.asked() {
			ops = append(ops, fmt.Sprint(a["op"], a["direction"], a["class"]))
		}
		want := "show<nil>android.widget.Button where<nil>android.widget.Button scrolldownandroid.webkit.WebView"
		if more {
			want += " where<nil>android.widget.Button"
		}
		if got := strings.Join(ops, " "); got != want {
			t.Errorf("what the service was asked: %s, want %s", got, want)
		}
	}
	// A page that went back to its field: the control was on the screen, and is not a moment on.
	sv, in := standInScrolls(t, func(_ int, req map[string]any) map[string]any {
		if req["op"] == "where" {
			return map[string]any{"found": true, "bounds": []int{42, 0, 1042, -790}}
		}
		return map[string]any{"found": true, "offered": true, "performed": true, "moved": true, "bounds": []int{42, 600, 1042, 726}}
	})
	d := deviceWith(t, sv, keyboard)
	if d.show(t.Context(), node(t, d, "Send report")) || len(in.asked()) != 2 {
		t.Errorf("a control its page did not keep on the screen is taken for shown: asked %v", in.asked())
	}
	// A keyboard that does not show covers nothing.
	sv, in = standInScrolls(t, func(int, map[string]any) map[string]any {
		return map[string]any{"found": true, "offered": true, "performed": true, "moved": true, "bounds": []int{42, 1900, 1042, 2026}}
	})
	d = deviceWith(t, sv, "InsetsSource id=3 type=ime frame=[0,0][0,0] visibleFrame=[0,2274][1080,2400] visible=false flags= sideHint=NONE")
	if !d.show(t.Context(), node(t, d, "Send report")) || len(in.asked()) != 1 {
		t.Errorf("with no keyboard up, the button is shown where its view put it: asked %v", in.asked())
	}
}

// A view is asked to scroll a page. It says at once when it is at its end that way; one that
// offers no scroll at all is not asked (it is swiped); one that took the action and did not move
// is asked once more.
func TestAViewIsAskedToScrollAPage(t *testing.T) {
	for _, c := range []struct {
		name         string
		answers      []map[string]any
		moved, asked bool
		asks         int
	}{
		{"it scrolls", []map[string]any{{"found": true, "scrolls": true, "offered": true, "performed": true, "moved": true}}, true, true, 1},
		{"at its end", []map[string]any{{"found": true, "scrolls": true, "offered": false, "performed": false, "moved": false}}, false, true, 1},
		{"offers no scroll", []map[string]any{{"found": true, "scrolls": false}}, false, false, 1},
		{"not found", []map[string]any{{"found": false}}, false, false, 1},
		{"busy, then scrolls", []map[string]any{
			{"found": true, "scrolls": true, "offered": true, "performed": true, "moved": false},
			{"found": true, "scrolls": true, "offered": true, "performed": true, "moved": true},
		}, true, true, 2},
		{"takes it and never moves", []map[string]any{{"found": true, "scrolls": true, "offered": true, "performed": true, "moved": false}}, false, true, 2},
	} {
		sv, in := standInScrolls(t, func(n int, _ map[string]any) map[string]any { return c.answers[min(n, len(c.answers))-1] })
		d := deviceWith(t, sv, "")
		moved, asked := d.page(t.Context(), node(t, d, "Report a delivery problem"), "down")
		if moved != c.moved || asked != c.asked || len(in.asked()) != c.asks {
			t.Errorf("%s: moved %v, asked %v, in %d requests; want %v, %v, in %d", c.name, moved, asked, len(in.asked()), c.moved, c.asked, c.asks)
		}
		if got := in.asked()[0]; got["op"] != "scroll" || got["direction"] != "down" || got["class"] != "android.webkit.WebView" {
			t.Errorf("%s: the request: %v", c.name, got)
		}
	}
	// A device without the service asks nothing.
	d := deviceWith(t, nil, "")
	if moved, asked := d.page(t.Context(), node(t, d, "Report a delivery problem"), "down"); moved || asked {
		t.Errorf("a device without the scroll service: moved %v, asked %v", moved, asked)
	}
}

// The keyboard's place is the window manager's frame of the input method while it shows, as
// Android 15 and Android 13 write it.
func TestTheKeyboardsFrame(t *testing.T) {
	for windows, want := range map[string]string{
		"        InsetsSource id=3 type=ime frame=[0,1633][1080,2400] visibleFrame=[0,1633][1080,2400] visible=true flags= sideHint=BOTTOM boundingRects=null": "{0 1633 1080 767} true",
		"        InsetsSource type=ITYPE_IME frame=[0,1517][1080,2400] visibleFrame=[0,1517][1080,2400] visible=true insetsRoundedCornerFrame=false":           "{0 1517 1080 883} true",
		"        InsetsSource id=3 type=ime frame=[0,0][0,0] visibleFrame=[0,2274][1080,2400] visible=false flags= sideHint=NONE boundingRects=null":           "{0 0 0 0} false",
		"": "{0 0 0 0} false",
	} {
		r, up := keyboardFrame(windows)
		if got := fmt.Sprint(r, up); got != want {
			t.Errorf("the keyboard of %q: %s, want %s", strings.TrimSpace(windows), got, want)
		}
	}
	if !covers(mobilecore.Rect{Y: 1633, Width: 1080, Height: 767}, []float64{42, 1900, 1042, 2026}) || covers(mobilecore.Rect{Y: 1633, Width: 1080, Height: 767}, []float64{42, 1170, 1042, 1296}) {
		t.Error("a keyboard covers what has its middle under it, and nothing else")
	}
	screen := mobilecore.Rect{Width: 1080, Height: 2400}
	for place, want := range map[string]bool{
		"42 2194 1042 2320": true,  // on the screen
		"42 4226 1042 2402": false, // a web page's, below: its bottom cut to the screen's
		"42 0 1042 -790":    false, // a web page's, above
		"42 5000 1042 5126": false, // a scroll view's, below
	} {
		var b []float64
		for _, f := range strings.Fields(place) {
			var v float64
			_, _ = fmt.Sscan(f, &v)
			b = append(b, v)
		}
		if onScreen(b, screen) != want {
			t.Errorf("a control at %s is on the screen: %v", place, !want)
		}
	}
}

// The scroll service is switched on beside the accessibility services a device already has on,
// and taken out again, with those left as they were.
func TestTheScrollServiceIsSwitchedOnBesideOthers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	dir := t.TempDir()
	setting, adb := filepath.Join(dir, "setting"), filepath.Join(dir, "adb")
	script := "#!/bin/sh\ncase \"$*\" in\n" +
		"*'settings get secure enabled_accessibility_services'*) cat " + setting + " 2>/dev/null || echo null ;;\n" +
		"*'settings put secure enabled_accessibility_services '*) echo \"$8\" > " + setting + " ;;\n" +
		"*'settings delete secure enabled_accessibility_services'*) rm -f " + setting + " ;;\n" +
		"esac\n"
	if err := os.WriteFile(adb, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	p, d := &pool{sdk: &sdk{adb: adb}}, &device{serial: "emulator-5554"}
	read := func() string {
		b, _ := os.ReadFile(setting)
		return strings.TrimSpace(string(b))
	}
	for _, c := range []struct{ before, on, after string }{
		{"", scrollService, ""},
		{"com.example.reader/.Reader", "com.example.reader/.Reader:" + scrollService, "com.example.reader/.Reader"},
		{scrollService, scrollService, ""},
	} {
		_ = os.Remove(setting)
		if c.before != "" {
			if err := os.WriteFile(setting, []byte(c.before+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := p.switchScrollsOn(context.Background(), d); err != nil {
			t.Fatal(err)
		}
		if got := read(); got != c.on {
			t.Errorf("with %q on, the setting became %q, want %q", c.before, got, c.on)
		}
		p.stopScrolls(context.Background(), d)
		if got := read(); got != c.after {
			t.Errorf("with %q on, the setting was left %q, want %q", c.before, got, c.after)
		}
	}
}

// A device is ready once it has unlocked its user: Android says it has booted before that, and
// starts the home screen again as it unlocks, in front of an app a scenario had started.
func TestADeviceIsReadyOnceItsUserIsUnlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	for state, want := range map[string]bool{
		"RUNNING_UNLOCKED": true, "RUNNING_LOCKED": false, "RUNNING_UNLOCKING": false, "BOOTING": false, "": true, "Unknown command": true,
	} {
		adb := filepath.Join(t.TempDir(), "adb")
		if err := os.WriteFile(adb, []byte("#!/bin/sh\ncase \"$*\" in\n*'am get-started-user-state 0'*) echo '"+state+"' ;;\nesac\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := (&sdk{adb: adb}).userUnlocked(t.Context(), "emulator-5554"); got != want {
			t.Errorf("a device whose user is %q is taken for unlocked: %v", state, got)
		}
	}
}
