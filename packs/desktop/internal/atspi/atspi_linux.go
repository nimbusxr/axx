//go:build linux

// Package atspi reads and acts on Linux's accessibility tree (AT-SPI), as
// screen readers do: over D-Bus, in pure Go.
package atspi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	ifaceAccessible = "org.a11y.atspi.Accessible"
	ifaceAction     = "org.a11y.atspi.Action"
	ifaceComponent  = "org.a11y.atspi.Component"
	ifaceText       = "org.a11y.atspi.Text"
	ifaceEditable   = "org.a11y.atspi.EditableText"
	registry        = "org.a11y.atspi.Registry"
	rootPath        = dbus.ObjectPath("/org/a11y/atspi/accessible/root")
	coordsScreen    = 0
	coordsWindow    = 1
)

// Client is a connection to the accessibility bus.
type Client struct {
	bus  *dbus.Conn
	addr string
	// inWindow are the apps (by their bus names) that know places in their
	// windows only, not on the screen (GTK 4); onScreen those known to know
	// them on the screen, never asked in their windows' (Qt 5's are wrong
	// there under a window manager).
	inWindow, onScreen sync.Map
	// noPlaces are the apps that hang when asked for a place (Flutter's
	// stops answering for seconds): they are not asked again.
	noPlaces sync.Map
	// placer says where a window's places start on the screen: on Wayland
	// an app knows places in its windows only, counted from its frame
	// (GTK 4), its surface with the shadow (GTK 3) or its content (Qt), and
	// a menu's from its own window. origins keeps what it said, and tops
	// each element's window, by bus name and path.
	placer  Placer
	origins sync.Map
	tops    sync.Map
}

// Placer says where a window's places start on the screen: given the
// window (an application's child), its process and its size as the app
// has it.
type Placer func(window *Element, pid int, width, height int32) (x, y int32, ok bool)

// SetPlacer has the client place the windows' places on the screen with p.
func (c *Client) SetPlacer(p Placer) { c.placer = p }

// window is the element's window: the application's child it is in (an
// application's own, its first).
func (e *Element) window() *Element {
	key := e.name + string(e.path)
	if v, ok := e.c.tops.Load(key); ok {
		return v.(*Element)
	}
	at := e
	if e.Role() == "application" {
		if kids, _ := e.Children(); len(kids) > 0 {
			at = kids[0]
		}
	} else {
		for range 64 {
			up := at.Parent()
			if up == nil {
				break
			}
			if r := up.Role(); r == "application" || r == "desktop frame" {
				break
			}
			at = up
		}
	}
	e.c.tops.Store(key, at)
	return at
}

// origin is where the element's window's places start on the screen.
func (e *Element) origin() (x, y int32) {
	if e.c.placer == nil {
		return 0, 0
	}
	w := e.window()
	key := w.name + string(w.path)
	if v, ok := e.c.origins.Load(key); ok {
		o := v.([2]int32)
		return o[0], o[1]
	}
	r := w.extents()
	x, y, ok := e.c.placer(w, e.c.pidOf(w.name), r.Width, r.Height)
	if ok {
		e.c.origins.Store(key, [2]int32{x, y})
	}
	return x, y
}

// New connects to the accessibility bus, whose address the session bus's
// org.a11y.Bus gives.
func New() (*Client, error) {
	session, err := dbus.SessionBus()
	if err != nil {
		return nil, fmt.Errorf("no session bus (DBUS_SESSION_BUS_ADDRESS): %w", err)
	}
	return newOn(session)
}

// NewOn connects to the accessibility bus of the session bus at an address.
func NewOn(sessionAddr string) (*Client, error) {
	session, err := dbus.Connect(sessionAddr)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the session bus %s: %w", sessionAddr, err)
	}
	defer session.Close()
	return newOn(session)
}

func newOn(session *dbus.Conn) (*Client, error) {
	var addr string
	if err := session.Object("org.a11y.Bus", "/org/a11y/bus").Call("org.a11y.Bus.GetAddress", 0).Store(&addr); err != nil {
		return nil, fmt.Errorf("no accessibility bus (at-spi2-core): %w", err)
	}
	bus, err := dbus.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the accessibility bus: %w", err)
	}
	if err := bus.Auth(nil); err != nil {
		bus.Close()
		return nil, err
	}
	if err := bus.Hello(); err != nil {
		bus.Close()
		return nil, err
	}
	return &Client{bus: bus, addr: addr}, nil
}

// Address is the accessibility bus's address.
func (c *Client) Address() string { return c.addr }

// Announce tells the session an assistive technology is on, as a screen
// reader does as it starts: toolkits that build their accessibility only for
// one (WebKitGTK's web pages among them) then do.
func Announce() error {
	session, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("no session bus (DBUS_SESSION_BUS_ADDRESS): %w", err)
	}
	return announce(session)
}

// AnnounceOn is Announce on the session bus at an address.
func AnnounceOn(sessionAddr string) error {
	session, err := dbus.Connect(sessionAddr)
	if err != nil {
		return fmt.Errorf("cannot reach the session bus %s: %w", sessionAddr, err)
	}
	defer session.Close()
	return announce(session)
}

func announce(session *dbus.Conn) error {
	status := session.Object("org.a11y.Bus", "/org/a11y/bus")
	for _, p := range []string{"IsEnabled", "ScreenReaderEnabled"} {
		if err := status.SetProperty("org.a11y.Status."+p, dbus.MakeVariant(true)); err != nil {
			return fmt.Errorf("setting org.a11y.Status.%s: %w", p, err)
		}
	}
	return nil
}

// Close closes the connection.
func (c *Client) Close() { _ = c.bus.Close() }

// Element is an element of the accessibility tree: an application, a
// window, a button, a web page's text.
type Element struct {
	c    *Client
	name string // the D-Bus name of the app it belongs to
	path dbus.ObjectPath
}

// Desktop is the root of the tree, whose children are the applications.
func (c *Client) Desktop() *Element { return &Element{c: c, name: registry, path: rootPath} }

// ApplicationOf is the application of the process pid: the registry's
// child of that process.
func (c *Client) ApplicationOf(pid int) (*Element, error) {
	apps, err := c.Desktop().Children()
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		if c.pidOf(app.name) == pid {
			return app, nil
		}
	}
	return nil, ErrNoApplication
}

func (c *Client) pidOf(name string) int {
	var p uint32
	if err := c.bus.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, name).Store(&p); err != nil {
		return 0
	}
	return int(p)
}

// ErrNoApplication is a process with no application on the bus (yet).
var ErrNoApplication = errors.New("the process has no application on the accessibility bus")

func (e *Element) obj() dbus.BusObject { return e.c.bus.Object(e.name, e.path) }

// callTimeout is how long an app may take to answer one call: an app that
// hangs must not hang the scenario.
const callTimeout = 2 * time.Second

// call calls a method of the element's, giving up after callTimeout.
func (e *Element) call(method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return e.obj().CallWithContext(ctx, method, 0, args...)
}

func (e *Element) property(iface, name string) (dbus.Variant, error) {
	var v dbus.Variant
	err := e.call("org.freedesktop.DBus.Properties.Get", iface, name).Store(&v)
	return v, err
}

// Name is the element's name.
func (e *Element) Name() string {
	v, err := e.property(ifaceAccessible, "Name")
	if err != nil {
		return ""
	}
	s, _ := v.Value().(string)
	return s
}

// Toolkit is the toolkit an application element's app is made with, and its
// version, as the app tells them: "GTK" and "4.14.5", "Qt" and "6.4.2".
func (e *Element) Toolkit() (name, version string) {
	for _, p := range []struct {
		prop string
		out  *string
	}{{"ToolkitName", &name}, {"Version", &version}} {
		if v, err := e.property("org.a11y.atspi.Application", p.prop); err == nil {
			*p.out, _ = v.Value().(string)
		}
	}
	return name, version
}

// Description is the element's description (a web element's title).
func (e *Element) Description() string {
	v, err := e.property(ifaceAccessible, "Description")
	if err != nil {
		return ""
	}
	s, _ := v.Value().(string)
	return s
}

// Role is the element's role, by name: "push button", "frame", "label". It
// reads the role's number, which every toolkit gives (WebKitGTK answers
// GetRoleName with nothing).
func (e *Element) Role() string {
	var r uint32
	if err := e.call(ifaceAccessible + ".GetRole").Store(&r); err != nil {
		return ""
	}
	if int(r) < len(roles) {
		return roles[r]
	}
	return fmt.Sprintf("role %d", r)
}

// roles are AT-SPI's role names, by number (AtspiRole).
var roles = []string{
	"invalid", "accelerator label", "alert", "animation", "arrow", "calendar", "canvas", "check box",
	"check menu item", "color chooser", "column header", "combo box", "date editor", "desktop icon",
	"desktop frame", "dial", "dialog", "directory pane", "drawing area", "file chooser", "filler",
	"focus traversable", "font chooser", "frame", "glass pane", "html container", "icon", "image",
	"internal frame", "label", "layered pane", "list", "list item", "menu", "menu bar", "menu item",
	"option pane", "page tab", "page tab list", "panel", "password text", "popup menu", "progress bar",
	"push button", "radio button", "radio menu item", "root pane", "row header", "scroll bar",
	"scroll pane", "separator", "slider", "spin button", "split pane", "status bar", "table",
	"table cell", "table column header", "table row header", "tearoff menu item", "terminal", "text",
	"toggle button", "tool bar", "tool tip", "tree", "tree table", "unknown", "viewport", "window",
	"extended", "header", "footer", "paragraph", "ruler", "application", "autocomplete", "editbar",
	"embedded", "entry", "chart", "caption", "document frame", "heading", "page", "section",
	"redundant object", "form", "link", "input method window", "table row", "tree item",
	"document spreadsheet", "document presentation", "document text", "document web",
	"document email", "comment", "list box", "grouping", "image map", "notification", "info bar",
	"level bar", "title bar", "block quote", "audio", "video", "definition", "article", "landmark",
	"log", "marquee", "math", "rating", "timer", "static", "math fraction", "math root", "subscript",
	"superscript", "description list", "description term", "description value", "footnote",
	"content deletion", "content insertion", "mark", "suggestion", "push button menu",
}

// Attributes are the element's attributes, like a web element's id.
func (e *Element) Attributes() map[string]string {
	var a map[string]string
	_ = e.call(ifaceAccessible + ".GetAttributes").Store(&a)
	return a
}

// Children are the element's children. A child in another process, like a
// web page under WebKitGTK's web view, comes only by its index: GetChildren
// leaves it out, so children are read by index when it gives fewer than the
// element counts.
func (e *Element) Children() ([]*Element, error) {
	var refs []struct {
		Name string
		Path dbus.ObjectPath
	}
	if err := e.call(ifaceAccessible + ".GetChildren").Store(&refs); err != nil {
		return nil, fmt.Errorf("children: %w", err)
	}
	if n := e.ChildCount(); n > len(refs) {
		refs = refs[:0]
		for i := range n {
			var r struct {
				Name string
				Path dbus.ObjectPath
			}
			if err := e.call(ifaceAccessible+".GetChildAtIndex", int32(i)).Store(&r); err != nil {
				return nil, fmt.Errorf("child %d: %w", i, err)
			}
			refs = append(refs, r)
		}
	}
	out := make([]*Element, 0, len(refs))
	for _, r := range refs {
		out = append(out, &Element{c: e.c, name: r.Name, path: r.Path})
	}
	return out, nil
}

// ChildCount is how many children the element says it has.
func (e *Element) ChildCount() int {
	v, err := e.property(ifaceAccessible, "ChildCount")
	if err != nil {
		return 0
	}
	n, _ := v.Value().(int32)
	return int(n)
}

// Interfaces are the AT-SPI interfaces the element implements.
func (e *Element) Interfaces() []string {
	var ifaces []string
	_ = e.call(ifaceAccessible + ".GetInterfaces").Store(&ifaces)
	return ifaces
}

// Actions are the names of the actions the element takes, like "click". It
// reads them one by one (WebKitGTK does not answer GetActions).
func (e *Element) Actions() []string {
	if !slices.Contains(e.Interfaces(), ifaceAction) {
		return nil
	}
	v, err := e.property(ifaceAction, "NActions")
	if err != nil {
		return nil
	}
	n, _ := v.Value().(int32)
	out := make([]string, 0, n)
	for i := range n {
		var name string
		if err := e.call(ifaceAction+".GetName", i).Store(&name); err != nil {
			return out
		}
		out = append(out, name)
	}
	return out
}

// Do has the element take its action named name, like "click".
func (e *Element) Do(name string) error {
	for i, a := range e.Actions() {
		if a != name {
			continue
		}
		var ok bool
		if err := e.call(ifaceAction+".DoAction", int32(i)).Store(&ok); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if !ok {
			return fmt.Errorf("%s: the app did not do it", name)
		}
		return nil
	}
	return fmt.Errorf("the element takes no %q action (it takes %v)", name, e.Actions())
}

// Text is the element's text, like a field's or a document's.
func (e *Element) Text() string {
	if !slices.Contains(e.Interfaces(), ifaceText) {
		return ""
	}
	// Up to the character count: Chromium does not take -1 for the end.
	v, err := e.property(ifaceText, "CharacterCount")
	if err != nil {
		return ""
	}
	n, _ := v.Value().(int32)
	var s string
	_ = e.call(ifaceText+".GetText", int32(0), n).Store(&s)
	if s == "" {
		// Java's bridge can give a count of none for a field that has text:
		// up to the end, then.
		_ = e.call(ifaceText+".GetText", int32(0), int32(-1)).Store(&s)
	}
	return s
}

// SetText replaces the element's text, as typing it into a field does.
func (e *Element) SetText(v string) error {
	var ok bool
	if err := e.call(ifaceEditable+".SetTextContents", v).Store(&ok); err != nil {
		return fmt.Errorf("setting the text: %w", err)
	}
	if !ok {
		return errors.New("the app did not take the text")
	}
	return nil
}

// Rect is the element's place on the screen, in pixels.
type Rect struct{ X, Y, Width, Height int32 }

// Extents is the element's place on the screen. GTK 4 knows no place on the
// screen (it gives the origin), only in its window: under axx's X11
// desktops, with no window manager, a window sits at the screen's origin;
// on Wayland, where no app knows its place, the window's origin is added
// (SetPlacer). What GTK 3 has not drawn has the least integer for a place,
// which stays as it is.
// TODO(desktop-linux): add the window's place, for a desktop that has a
// window manager (--watch).
func (e *Element) Extents() Rect {
	r := e.extents()
	if ox, oy := e.origin(); (ox != 0 || oy != 0) && r.Width > 0 && drawn(r.X, r.Y) {
		r.X, r.Y = r.X+ox, r.Y+oy
	}
	return r
}

// drawn is whether a point is a place, not the least integer GTK 3 gives
// what it has not drawn.
func drawn(x, y int32) bool { return x > math.MinInt32/2 && y > math.MinInt32/2 }

func (e *Element) extents() Rect {
	var r Rect
	if _, ok := e.c.noPlaces.Load(e.name); ok {
		return r
	}
	if err := e.call(ifaceComponent+".GetExtents", uint32(coordsScreen)).Store(&r); errors.Is(err, context.DeadlineExceeded) {
		e.c.noPlaces.Store(e.name, true)
		return Rect{}
	}
	if _, ok := e.c.onScreen.Load(e.name); ok {
		return r
	}
	if _, ok := e.c.inWindow.Load(e.name); ok || r.X == 0 && r.Y == 0 {
		var w Rect
		if e.call(ifaceComponent+".GetExtents", uint32(coordsWindow)).Store(&w) == nil && (w.X != 0 || w.Y != 0) {
			e.c.inWindow.Store(e.name, true)
			return w
		}
	}
	return r
}

// Caret is where the element's text cursor is on the screen, a line high
// and no width, when it shows one: the element is a text, with no part of
// it selected. It is at the left edge of the character after it, or the
// right edge of the one before it, at the text's end.
func (e *Element) Caret() (Rect, bool) {
	if !slices.Contains(e.Interfaces(), ifaceText) {
		return Rect{}, false
	}
	v, err := e.property(ifaceText, "CaretOffset")
	at, ok := v.Value().(int32)
	if err != nil || !ok || at < 0 {
		return Rect{}, false
	}
	var selections int32
	if e.call(ifaceText+".GetNSelections").Store(&selections) == nil && selections > 0 {
		return Rect{}, false
	}
	coords := uint32(coordsScreen)
	if _, ok := e.c.inWindow.Load(e.name); ok {
		coords = coordsWindow
	}
	extents := func(i int32) (Rect, bool) {
		var r Rect
		err := e.call(ifaceText+".GetCharacterExtents", i, coords).Store(&r.X, &r.Y, &r.Width, &r.Height)
		return r, err == nil && r.Height > 0
	}
	ox, oy := e.origin()
	if r, ok := extents(at); ok && drawn(r.X, r.Y) {
		return Rect{X: r.X + ox, Y: r.Y + oy, Height: r.Height}, true
	}
	if r, ok := extents(at - 1); ok && at > 0 && drawn(r.X, r.Y) {
		return Rect{X: r.X + r.Width + ox, Y: r.Y + oy, Height: r.Height}, true
	}
	return Rect{}, false
}

// States of an element, by their AT-SPI numbers (AtspiStateType).
const (
	StateChecked   = 4
	StateEditable  = 7
	StateEnabled   = 8
	StateFocused   = 12
	StateSelected  = 23
	StateSensitive = 24
	StateShowing   = 25
	StateVisible   = 30
)

// Is is whether the element is in the state (StateChecked, StateShowing...).
func (e *Element) Is(state uint) bool {
	var set []uint32
	if err := e.call(ifaceAccessible + ".GetState").Store(&set); err != nil || int(state/32) >= len(set) {
		return false
	}
	return set[state/32]&(1<<(state%32)) != 0
}

// Parent is the element's parent, or nil at the top.
func (e *Element) Parent() *Element {
	v, err := e.property(ifaceAccessible, "Parent")
	if err != nil {
		return nil
	}
	ref, ok := v.Value().([]any)
	if !ok || len(ref) != 2 {
		return nil
	}
	name, _ := ref[0].(string)
	path, _ := ref[1].(dbus.ObjectPath)
	if name == "" || path == "/org/a11y/atspi/null" {
		return nil
	}
	return &Element{c: e.c, name: name, path: path}
}

// ElementAt is the element at a point on the screen under e: what a click
// there would reach. An app that knows places in its window only (GTK 4,
// see Extents) is asked in its window's coordinates.
func (e *Element) ElementAt(x, y int) *Element {
	if _, ok := e.c.noPlaces.Load(e.name); ok {
		return nil
	}
	coords := uint32(coordsScreen)
	if _, ok := e.c.inWindow.Load(e.name); ok {
		coords = coordsWindow
	}
	var ref struct {
		Name string
		Path dbus.ObjectPath
	}
	ox, oy := e.origin()
	if err := e.call(ifaceComponent+".GetAccessibleAtPoint", int32(x)-ox, int32(y)-oy, coords).Store(&ref); err != nil {
		return nil
	}
	if ref.Name == "" || ref.Path == "/org/a11y/atspi/null" {
		return nil
	}
	return &Element{c: e.c, name: ref.Name, path: ref.Path}
}

// Same is whether two elements are one.
func (e *Element) Same(o *Element) bool { return o != nil && e.name == o.name && e.path == o.path }

// ScrollTo asks the app to scroll the element into view, wholly: to the top
// left of what shows (scrolled anywhere, Chromium leaves it half under the
// edge).
func (e *Element) ScrollTo() bool {
	const topLeft = 0 // ATSPI_SCROLL_TOP_LEFT
	var ok bool
	_ = e.call(ifaceComponent+".ScrollTo", uint32(topLeft)).Store(&ok)
	return ok
}

// WithoutPlaces marks the app of the element as one never to ask for places:
// Flutter's hangs when asked, and has none to give.
func (e *Element) WithoutPlaces() { e.c.noPlaces.Store(e.name, true) }

// WithScreenPlaces marks the app of the element as one that knows its
// places on the screen, whatever place it gives (an element at the screen's
// origin, or one hidden): it is never asked for them in its windows'.
func (e *Element) WithScreenPlaces() { e.c.onScreen.Store(e.name, true) }

// IsFlutter is whether the process runs Flutter's Linux embedder.
func IsFlutter(pid int) bool {
	maps, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
	return err == nil && strings.Contains(string(maps), "libflutter_linux_gtk.so")
}

// HasPlaces is whether the element's app gives places on the screen.
func (e *Element) HasPlaces() bool {
	_, none := e.c.noPlaces.Load(e.name)
	return !none
}
