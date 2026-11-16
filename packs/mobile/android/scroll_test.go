package mobileandroid

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
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
	return deviceShowing(t, sv, windows, formPage)
}

// deviceShowing is deviceWith, for a screen of the test's.
func deviceShowing(t *testing.T, sv *scrolls, windows, page string) *running {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in adb is a shell script")
	}
	adb := filepath.Join(t.TempDir(), "adb")
	if err := os.WriteFile(adb, []byte("#!/bin/sh\ncase \"$*\" in\n*'dumpsys window'*) printf '%s\\n' '"+windows+"' ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	server := appiumtest.Start(t, appiumtest.App{Screens: map[string]string{"form": page}, Start: "form"})
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

// checkPage is a web page of questions, each answered Yes or No, all below the screen: the web
// view gives every one of them the same place there, cut to the screen's edge. The first
// question's answers have an ID each; the others' have none. Two fields at the page's end have
// neither, and hold different texts.
const checkPage = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy index="0" class="hierarchy" rotation="0" width="1080" height="2400">
<android.widget.FrameLayout index="0" package="example.parcels.courier" class="android.widget.FrameLayout" text="" clickable="false" enabled="true" scrollable="false" bounds="[0,0][1080,2400]" displayed="true">
<android.webkit.WebView index="0" package="example.parcels.courier" class="android.webkit.WebView" text="Check the van" clickable="false" enabled="true" scrollable="true" bounds="[0,0][1080,2400]" displayed="true">
<android.view.View index="0" package="example.parcels.courier" class="android.view.View" text="Do the doors lock?" clickable="false" enabled="true" scrollable="false" bounds="[42,2400][1039,2400]" displayed="false">
<android.widget.RadioButton index="0" package="example.parcels.courier" class="android.widget.RadioButton" text="Yes" resource-id="q10-0" checkable="true" clickable="true" enabled="true" scrollable="false" bounds="[70,2400][139,2400]" displayed="false"/>
<android.widget.RadioButton index="1" package="example.parcels.courier" class="android.widget.RadioButton" text="No" resource-id="q10-1" checkable="true" clickable="true" enabled="true" scrollable="false" bounds="[70,2400][139,2400]" displayed="false"/>
</android.view.View>
<android.view.View index="1" package="example.parcels.courier" class="android.view.View" text="Is the cargo area clean?" clickable="false" enabled="true" scrollable="false" bounds="[42,2400][1039,2400]" displayed="false">
<android.widget.RadioButton index="0" package="example.parcels.courier" class="android.widget.RadioButton" text="Yes" checkable="true" clickable="true" enabled="true" scrollable="false" bounds="[70,2400][139,2400]" displayed="false"/>
<android.widget.RadioButton index="1" package="example.parcels.courier" class="android.widget.RadioButton" text="No" checkable="true" clickable="true" enabled="true" scrollable="false" bounds="[70,2400][139,2400]" displayed="false"/>
</android.view.View>
<android.view.View index="2" package="example.parcels.courier" class="android.view.View" text="Is the load secured?" clickable="false" enabled="true" scrollable="false" bounds="[42,2400][1039,2400]" displayed="false">
<android.widget.RadioButton index="0" package="example.parcels.courier" class="android.widget.RadioButton" text="Yes" checkable="true" clickable="true" enabled="true" scrollable="false" bounds="[70,2400][139,2400]" displayed="false"/>
<android.widget.RadioButton index="1" package="example.parcels.courier" class="android.widget.RadioButton" text="No" checkable="true" clickable="true" enabled="true" scrollable="false" bounds="[70,2400][139,2400]" displayed="false"/>
</android.view.View>
<android.widget.EditText index="3" package="example.parcels.courier" class="android.widget.EditText" text="L-PX 4071" clickable="true" enabled="true" scrollable="false" bounds="[42,2400][1039,2400]" displayed="false" hint="Which van?"/>
<android.widget.EditText index="4" package="example.parcels.courier" class="android.widget.EditText" text="Hanna Wolf" clickable="true" enabled="true" scrollable="false" bounds="[42,2400][1039,2400]" displayed="false" hint="Who checked it?"/>
</android.webkit.WebView>
</android.widget.FrameLayout></hierarchy>`

// The scroll service finds a control by what the screen's source gave of it, and where it is.
// Several alike out of view are at one place on a web page: the request says which of them the
// control is, in the screen's order, so the page shows that one, and not the first of them. One
// that is the only one of its kind (it has an ID) is asked for as before. Fields are alike
// without their texts, which are what they hold.
func TestTheScrollServiceIsToldWhichOfSeveralAlikeAControlIs(t *testing.T) {
	sv, in := standInScrolls(t, func(int, map[string]any) map[string]any {
		return map[string]any{"found": true, "offered": true, "performed": true, "moved": true, "bounds": []int{70, 1168, 139, 1233}}
	})
	d := deviceShowing(t, sv, "", checkPage)
	s, err := d.Screen(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	answer := func(question, text string) *mobilecore.Node {
		for _, n := range s.Nodes {
			if n.Text == text && n.Parent != nil && n.Parent.Text == question {
				return n
			}
		}
		t.Fatalf("the page has no %q under %q", text, question)
		return nil
	}
	field := func(hint string) *mobilecore.Node {
		for _, n := range s.Nodes {
			if n.Hint == hint {
				return n
			}
		}
		t.Fatalf("the page has no field %q", hint)
		return nil
	}
	for _, c := range []struct {
		name    string
		node    *mobilecore.Node
		nth, of any
		text    any
	}{
		{"the first question's No, which has an ID", answer("Do the doors lock?", "No"), nil, nil, "No"},
		{"the second question's No", answer("Is the cargo area clean?", "No"), 0.0, 2.0, "No"},
		{"the third question's No", answer("Is the load secured?", "No"), 1.0, 2.0, "No"},
		{"the third question's Yes", answer("Is the load secured?", "Yes"), 1.0, 2.0, "Yes"},
		{"the first field", field("Which van?"), 0.0, 2.0, nil},
		{"the second field", field("Who checked it?"), 1.0, 2.0, nil},
	} {
		before := len(in.asked())
		if !d.show(t.Context(), c.node) {
			t.Errorf("%s is not shown", c.name)
			continue
		}
		req := in.asked()[before]
		if req["nth"] != c.nth || req["of"] != c.of || req["text"] != c.text {
			t.Errorf("%s: the request says nth %v of %v, text %v; want %v of %v, text %v: %v", c.name, req["nth"], req["of"], req["text"], c.nth, c.of, c.text, req)
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

// filePicker is Android's file picker on Downloads: its grid of files scrolls inside a view that
// scrolls too (it folds the header away), under a row of chips that scrolls sideways.
const filePicker = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy index="0" class="hierarchy" rotation="0" width="1080" height="2400">
<android.widget.FrameLayout index="0" package="com.google.android.documentsui" class="android.widget.FrameLayout" text="" clickable="false" enabled="true" scrollable="false" bounds="[0,0][1080,2400]" displayed="true">
<android.view.ViewGroup index="0" package="com.google.android.documentsui" class="android.view.ViewGroup" text="" resource-id="com.google.android.documentsui:id/coordinator_layout" clickable="false" enabled="true" scrollable="true" bounds="[0,130][1080,2400]" displayed="true">
<android.widget.HorizontalScrollView index="0" package="com.google.android.documentsui" class="android.widget.HorizontalScrollView" text="" resource-id="com.google.android.documentsui:id/search_chip_group" clickable="false" enabled="true" scrollable="true" bounds="[0,270][1080,400]" displayed="true"/>
<androidx.recyclerview.widget.RecyclerView index="1" package="com.google.android.documentsui" class="androidx.recyclerview.widget.RecyclerView" text="" resource-id="com.google.android.documentsui:id/dir_list" clickable="false" enabled="true" scrollable="true" bounds="[0,500][1080,2400]" displayed="true">
<android.widget.TextView index="0" package="com.google.android.documentsui" class="android.widget.TextView" text="parcel-at-door.jpg" clickable="false" enabled="true" scrollable="false" bounds="[540,940][1040,1000]" displayed="true"/>
</androidx.recyclerview.widget.RecyclerView>
</android.view.ViewGroup>
</android.widget.FrameLayout></hierarchy>`

// A screen with one scrolling view inside another is paged through both: the outer one is asked
// first, and when it has no more to move, the one inside it is. A finger would move whichever
// can still move; a view that is asked moves only itself, so a search that asked the outer view
// alone stopped once the picker's header had folded away, a page short of the file. A row that
// scrolls sideways is not asked to scroll down.
func TestAPageIsAskedOfEachScrollingViewInTurn(t *testing.T) {
	left := map[string]int{"com.google.android.documentsui:id/coordinator_layout": 1, "com.google.android.documentsui:id/dir_list": 2}
	sv, in := standInScrolls(t, func(_ int, req map[string]any) map[string]any {
		id, _ := req["id"].(string)
		if left[id] == 0 {
			return map[string]any{"found": true, "scrolls": true, "offered": false, "performed": false, "moved": false}
		}
		left[id]--
		return map[string]any{"found": true, "scrolls": true, "offered": true, "performed": true, "moved": true}
	})
	d := deviceShowing(t, sv, "", filePicker)
	var moves []string
	for range 4 {
		moved, err := d.Scroll(t.Context(), "down")
		if err != nil {
			t.Fatal(err)
		}
		moves = append(moves, fmt.Sprint(moved))
	}
	if got := strings.Join(moves, " "); got != "true true true false" {
		t.Errorf("four pages down the picker moved: %s, want the header's page, the grid's two, and then none", got)
	}
	var asks []string
	for _, a := range in.asked() {
		id, _ := a["id"].(string)
		asks = append(asks, id[strings.LastIndex(id, "/")+1:])
	}
	want := "coordinator_layout coordinator_layout dir_list coordinator_layout dir_list coordinator_layout dir_list"
	if got := strings.Join(asks, " "); got != want {
		t.Errorf("the views asked, in order:\n got %s\nwant %s", got, want)
	}
	// A device without the scroll service swipes the first of them, as it did.
	d = deviceShowing(t, nil, "", filePicker)
	if _, err := d.Scroll(t.Context(), "down"); err != nil {
		t.Fatal(err)
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

// fetchedScroll stands in for a release's files: it gives the service it has, and counts.
type fetchedScroll struct {
	apk   []byte
	err   error
	asked int
}

func (f *fetchedScroll) FetchAndroidScroll(_ context.Context, tag, dir string) (string, error) {
	f.asked++
	if f.err != nil {
		return "", f.err
	}
	path := filepath.Join(dir, "axx_"+strings.TrimPrefix(tag, "v")+"_android-scroll.apk")
	return path, os.WriteFile(path, f.apk, 0o644)
}

// A release's axx takes the scroll service its release published, once: it is kept in axx's
// cache. One that cannot be had is said, with why.
func TestAReleasesScrollServiceIsDownloadedOnce(t *testing.T) {
	cache := t.TempDir()
	f := &fetchedScroll{apk: []byte("the service of 0.7.0")}
	for range 2 {
		apk, err := releasedScroll(t.Context(), f, "0.7.0", cache)
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(apk); string(b) != "the service of 0.7.0" || filepath.Base(apk) != "axx_0.7.0_android-scroll.apk" {
			t.Fatalf("the service is %s, holding %q", apk, b)
		}
	}
	if f.asked != 1 {
		t.Errorf("the release was asked %d times for a service axx keeps, want once", f.asked)
	}
	// Another release's is another file.
	if _, err := releasedScroll(t.Context(), &fetchedScroll{apk: []byte("the service of 0.7.1")}, "0.7.1", cache); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(cache, "scroll-0.7.0", "axx_0.7.0_android-scroll.apk")); string(b) != "the service of 0.7.0" {
		t.Errorf("the service of 0.7.0 is %q after 0.7.1's came", b)
	}
	if _, err := releasedScroll(t.Context(), &fetchedScroll{err: fmt.Errorf("no route to github.com")}, "0.7.2", cache); err == nil || !strings.Contains(err.Error(), "no route to github.com") {
		t.Errorf("a service that cannot be downloaded: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(cache, "download-*")); len(left) != 0 {
		t.Errorf("a download left %v in the cache", left)
	}
}

// The scroll service builds with the SDK's tools and a JDK, where a machine has them: an app
// of axx's package and of the version it was built as, signed with the key it was given, that
// Android's own tools take for an installable app.
func TestTheScrollServiceBuilds(t *testing.T) {
	s, err := findSDK()
	if err != nil {
		t.Skipf("no Android SDK: %v", err)
	}
	sdk := filepath.Dir(filepath.Dir(s.adb))
	tools, err := scrollTools(t.Context(), sdk)
	if err != nil {
		t.Skipf("the scroll service cannot be built here: %v", err)
	}
	dir := t.TempDir()
	keystore, apk := filepath.Join(dir, "test.keystore"), filepath.Join(dir, "out", "axx-scroll.apk")
	if err := MakeScrollKeystore(t.Context(), keystore, "release", "a password, with spaces", "axx scroll service, a test's"); err != nil {
		t.Fatal(err)
	}
	if err := BuildScrollService(t.Context(), ScrollBuild{SDK: sdk, Out: apk, Version: "0.7.0", Keystore: keystore, Alias: "release", Password: "a password, with spaces"}); err != nil {
		t.Fatal(err)
	}
	run := func(tool string, args ...string) string {
		cmd := exec.CommandContext(t.Context(), tool, args...)
		cmd.Env = append(os.Environ(), "JAVA_HOME="+tools.javaHome)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", filepath.Base(tool), args, err, out)
		}
		return string(out)
	}
	badging := run(filepath.Join(tools.tools, exe("aapt2", ".exe")), "dump", "badging", apk)
	for _, want := range []string{"package: name='" + scrollPackage + "'", "versionName='0.7.0'"} {
		if !strings.Contains(badging, want) {
			t.Errorf("the service's badging lacks %s:\n%s", want, badging)
		}
	}
	signed := run(filepath.Join(tools.tools, exe("apksigner", ".bat")), "verify", "--print-certs", apk)
	if !strings.Contains(signed, `CN="axx scroll service, a test's"`) && !strings.Contains(signed, `CN=axx scroll service\, a test's`) {
		t.Errorf("the service is not signed with the key it was given:\n%s", signed)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "out", "prepare-*")); len(left) != 0 {
		t.Errorf("the build left %v beside the service", left)
	}
	// A password that is not the keystore's fails the build, and leaves no service.
	wrong := filepath.Join(dir, "wrong", "axx-scroll.apk")
	if err := BuildScrollService(t.Context(), ScrollBuild{SDK: sdk, Out: wrong, Version: "0.7.0", Keystore: keystore, Alias: "release", Password: "another"}); err == nil {
		t.Error("a service was signed with a password that is not the keystore's")
	}
	if _, err := os.Stat(wrong); err == nil {
		t.Error("a build that failed left a service")
	}
}
