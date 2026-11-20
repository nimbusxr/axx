package mobilecore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/packs/mobile/internal/appium"
)

// sessionOnly is a device of which a test uses the session alone.
type sessionOnly struct {
	Device
	session *appium.Session
}

func (d sessionOnly) Session() *appium.Session { return d.session }

// The node an element is, on a screen with several nodes at the element's place. A web page gives
// every control below the screen one place, cut to the screen's edge: three questions' "No"
// are all there, alike but for their order. The driver finds them in the screen's order, and
// gives an element the same ID each time, so the element is the node at its own place among
// what its locator finds. Taken for the last of them, the page was asked to show another
// question's answer, and scrolled past the one the step was to tap.
func TestTheNodeAnElementIsAmongSeveralAtItsPlace(t *testing.T) {
	below := Rect{X: 70, Y: 2400, Width: 69, Height: 0}
	radio := func(parent *Node) *Node {
		n := &Node{
			Class: "android.widget.RadioButton", Text: "No", Bounds: below, Parent: parent,
			Using: "xpath", Value: `//android.widget.RadioButton[@bounds="[70,2400][139,2400]" and @text="No"]`,
		}
		parent.Children = append(parent.Children, n)
		return n
	}
	page := &Node{Class: "android.webkit.WebView", Bounds: Rect{Width: 1080, Height: 2400}, Using: "xpath", Value: "//android.webkit.WebView"}
	horn, belt, charge := radio(page), radio(page), radio(page)
	// A row and the text that fills it, one inside the other, at one place.
	row := &Node{Class: "android.view.View", Bounds: Rect{X: 42, Y: 300, Width: 997, Height: 120}, Parent: page, Using: "xpath", Value: "//row"}
	text := &Node{Class: "android.widget.TextView", Text: "Do the brakes respond?", Bounds: row.Bounds, Parent: row, Using: "xpath", Value: "//text"}
	row.Children = []*Node{text}
	s := &Screen{Size: Rect{Width: 1080, Height: 2400}, Nodes: []*Node{page, row, text, horn, belt, charge}}

	// What the driver finds by the radio buttons' locator, which a case sets.
	var found []string
	finds := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		finds++
		refs := []map[string]string{}
		for _, id := range found {
			refs = append(refs, map[string]string{"element-6066-11e4-a52e-4f735466cecf": id})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"value": refs})
	}))
	t.Cleanup(server.Close)
	session := (&appium.Client{URL: server.URL, HTTP: server.Client()}).Attach("1", nil)
	d := sessionOnly{session: session}
	el := func(id string) *appium.Element { return &appium.Element{ID: id} }

	for _, c := range []struct {
		name  string
		found []string
		id    string
		want  *Node
	}{
		{"the first of three", []string{"horn", "belt", "charge"}, "horn", horn},
		{"the second", []string{"horn", "belt", "charge"}, "belt", belt},
		{"the last", []string{"horn", "belt", "charge"}, "charge", charge},
		// A screen that changed since it was read has another count, and says nothing; so does
		// one the element is not on: the last at the place is taken, as before.
		{"a screen that changed", []string{"horn", "belt"}, "horn", charge},
		{"an element not among them", []string{"horn", "belt", "charge"}, "another", charge},
	} {
		found = c.found
		if got := nodeOf(t.Context(), d, s, el(c.id), below); got != c.want {
			t.Errorf("%s: the element is taken for the node %d of the screen", c.name, indexOf(s, got))
		}
	}
	// One node inside another at one place is no question for the driver: the innermost.
	finds = 0
	if got := nodeOf(t.Context(), d, s, el("row"), row.Bounds); got != text || finds != 0 {
		t.Errorf("a row and its text: node %d, after %d finds; want the text, and none", indexOf(s, got), finds)
	}
	// A place with one node, and one with none.
	if got := nodeOf(t.Context(), d, s, el("page"), page.Bounds); got != page || finds != 0 {
		t.Errorf("the page: node %d, after %d finds", indexOf(s, got), finds)
	}
	if got := nodeOf(t.Context(), d, s, el("gone"), Rect{X: 1, Y: 2, Width: 3, Height: 4}); got != nil {
		t.Errorf("a place with no node gives node %d", indexOf(s, got))
	}
}

// Which of several alike a node is crosses to the axx host with the node, whose device has its
// scroll service told.
func TestWhichOfSeveralAlikeANodeIsCrossesToTheHost(t *testing.T) {
	page := &Node{Class: "android.webkit.WebView"}
	first := &Node{Class: "android.widget.RadioButton", Text: "No", Parent: page, Nth: 0, Of: 2}
	second := &Node{Class: "android.widget.RadioButton", Text: "No", Parent: page, Nth: 1, Of: 2}
	page.Children = []*Node{first, second}
	w, at := wireScreenOf(second)
	sent, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	var got wireScreen
	if err := json.Unmarshal(sent, &got); err != nil {
		t.Fatal(err)
	}
	if n := got.screen().Nodes[at]; n.Nth != 1 || n.Of != 2 || n.Parent == nil || n.Parent.Class != "android.webkit.WebView" {
		t.Errorf("the node on the host: %d of %d, in %v", n.Nth, n.Of, n.Parent)
	}
	if n := got.screen().Nodes[0]; n.Nth != 0 || n.Of != 0 {
		t.Errorf("the page, the only one of its kind: %d of %d", n.Nth, n.Of)
	}
}

func indexOf(s *Screen, n *Node) int {
	for i, m := range s.Nodes {
		if m == n {
			return i
		}
	}
	return -1
}

// lostThenFound is a device whose driver has lost the first controls it is asked of, as a web
// page that draws its controls again after a scroll loses the driver its own of them: it can say
// whether a control is out of view only of one found since.
type lostThenFound struct {
	lost  int
	asked []string
}

func (d *lostThenFound) OutOfView(_ context.Context, el *appium.Element) (bool, error) {
	d.asked = append(d.asked, el.ID)
	if d.lost > 0 {
		d.lost--
		return false, &appium.Error{Code: "stale element reference", Message: "The element '" + el.ID + "' does not exist in DOM anymore"}
	}
	return el.ID == "found-again", nil
}

// A control a selector found, which its driver has lost by the time it is asked whether the
// control is out of view, is found again and asked of again. It was taken for in view, and
// tapped: the driver taps a control that is out of view at the screen's edge, and says it
// tapped (a web page's radio button, right after the tap before it had scrolled the page, on
// CI's emulator: the answer was never given). One the driver loses for as long as the step
// waits fails the step with why, and is not tapped either.
func TestAControlItsDriverLostIsAskedOfAgainWhetherItShows(t *testing.T) {
	session := (&appium.Client{URL: "http://127.0.0.1:1"}).Attach("session-1", nil)
	d := &lostThenFound{lost: 2}
	el := session.Element("first-found")
	finds := 0
	again := func() (*appium.Element, error) {
		finds++
		if finds == 1 {
			// The page is still drawing itself: nothing matches for a moment.
			return nil, &appium.Error{Code: "no such element", Message: "An element could not be located on the page"}
		}
		return session.Element("found-again"), nil
	}
	out, err := outOfView(t.Context(), d, &el, again, 5*time.Second)
	if err != nil || !out {
		t.Fatalf("out of view: %v, %v; the control found again is out of view", out, err)
	}
	if el.ID != "found-again" || strings.Join(d.asked, " ") != "first-found first-found found-again" {
		t.Errorf("the control is %s, and the driver was asked of %v", el.ID, d.asked)
	}

	never := &lostThenFound{lost: 1 << 30}
	el = session.Element("first-found")
	out, err = outOfView(t.Context(), never, &el, again, 500*time.Millisecond)
	if err == nil || out || !appium.IsStale(err) {
		t.Errorf("a control its driver never has: out of view %v, %v; want the driver's error, and not in view", out, err)
	}
}
