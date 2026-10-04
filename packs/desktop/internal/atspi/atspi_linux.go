//go:build linux

// Package atspi reads and acts on Linux's accessibility tree (AT-SPI), as
// screen readers do: over D-Bus, in pure Go.
package atspi

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	ifaceAccessible = "org.a11y.atspi.Accessible"
	ifaceAction     = "org.a11y.atspi.Action"
	ifaceComponent  = "org.a11y.atspi.Component"
	registry        = "org.a11y.atspi.Registry"
	rootPath        = dbus.ObjectPath("/org/a11y/atspi/accessible/root")
	coordsScreen    = 0
)

// Client is a connection to the accessibility bus.
type Client struct{ bus *dbus.Conn }

// New connects to the accessibility bus, whose address the session bus's
// org.a11y.Bus gives.
func New() (*Client, error) {
	session, err := dbus.SessionBus()
	if err != nil {
		return nil, fmt.Errorf("no session bus (DBUS_SESSION_BUS_ADDRESS): %w", err)
	}
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
	return &Client{bus: bus}, nil
}

// Announce tells the session an assistive technology is on, as a screen
// reader does as it starts: toolkits that build their accessibility only for
// one (WebKitGTK's web pages among them) then do.
func Announce() error {
	session, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("no session bus (DBUS_SESSION_BUS_ADDRESS): %w", err)
	}
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

// ApplicationOf is the application of the process pid.
func (c *Client) ApplicationOf(pid int) (*Element, error) {
	apps, err := c.Desktop().Children()
	if err != nil {
		return nil, err
	}
	for _, app := range apps {
		var p uint32
		if err := c.bus.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, app.name).Store(&p); err == nil && int(p) == pid {
			return app, nil
		}
	}
	return nil, ErrNoApplication
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
	if n := e.childCount(); n > len(refs) {
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

func (e *Element) childCount() int {
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

// Rect is the element's place on the screen, in pixels.
type Rect struct{ X, Y, Width, Height int32 }

// Extents is the element's place on the screen.
func (e *Element) Extents() Rect {
	var r Rect
	_ = e.call(ifaceComponent+".GetExtents", uint32(coordsScreen)).Store(&r)
	return r
}
