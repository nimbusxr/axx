//go:build linux

package atspi

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

// Tray is a desktop's tray, as a desktop with one keeps it (KDE, GNOME with
// AppIndicator support): the StatusNotifierWatcher that apps register their
// status items with, on the session bus. A tray app shows its icon there,
// and its menu is read and chosen from through the item's dbusmenu.
type Tray struct {
	conn  *dbus.Conn
	props *prop.Properties
	mu    sync.Mutex
	items []TrayItem
}

// TrayItem is a status item an app registered: its bus name and object, and
// the app's process.
type TrayItem struct {
	Service string
	Path    dbus.ObjectPath
	PID     int
	tray    *Tray
}

// TrayEntry is an entry of a status item's menu.
type TrayEntry struct {
	ID      int32
	Label   string
	Enabled bool
	Entries []TrayEntry
}

const (
	watcherName  = "org.kde.StatusNotifierWatcher"
	watcherPath  = dbus.ObjectPath("/StatusNotifierWatcher")
	itemIface    = "org.kde.StatusNotifierItem"
	dbusmenuName = "com.canonical.dbusmenu"
)

// ServeTray keeps a tray on the session bus at addr.
func ServeTray(addr string) (*Tray, error) {
	conn, err := dbus.Connect(addr)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the session bus %s: %w", addr, err)
	}
	t := &Tray{conn: conn}
	if err := conn.Export(watcher{t}, watcherPath, watcherName); err != nil {
		conn.Close()
		return nil, err
	}
	t.props, err = prop.Export(conn, watcherPath, prop.Map{watcherName: {
		"RegisteredStatusNotifierItems":  {Value: []string{}, Emit: prop.EmitTrue},
		"IsStatusNotifierHostRegistered": {Value: true, Emit: prop.EmitConst},
		"ProtocolVersion":                {Value: int32(0), Emit: prop.EmitConst},
	}})
	if err != nil {
		conn.Close()
		return nil, err
	}
	node := &introspect.Node{Name: string(watcherPath), Interfaces: []introspect.Interface{
		introspect.IntrospectData, prop.IntrospectData,
		{
			Name: watcherName, Methods: introspect.Methods(watcher{t}), Properties: t.props.Introspection(watcherName),
			Signals: []introspect.Signal{
				{Name: "StatusNotifierItemRegistered", Args: []introspect.Arg{{Name: "service", Type: "s"}}},
				{Name: "StatusNotifierItemUnregistered", Args: []introspect.Arg{{Name: "service", Type: "s"}}},
				{Name: "StatusNotifierHostRegistered"},
			},
		},
	}}
	if err := conn.Export(introspect.NewIntrospectable(node), watcherPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		conn.Close()
		return nil, err
	}
	reply, err := conn.RequestName(watcherName, dbus.NameFlagDoNotQueue)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("cannot keep the tray on the session bus: %w", err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		return nil, fmt.Errorf("cannot keep the tray on the session bus: another one has it (%v)", reply)
	}
	return t, nil
}

// Close takes the tray off the bus.
func (t *Tray) Close() {
	if t != nil {
		t.conn.Close()
	}
}

// watcher is the StatusNotifierWatcher's methods.
type watcher struct{ t *Tray }

// RegisterStatusNotifierItem adds an app's status item: a bus name, whose
// item is at /StatusNotifierItem, or an object of the caller's (Ayatana's
// AppIndicator).
func (w watcher) RegisterStatusNotifierItem(sender dbus.Sender, service string) *dbus.Error {
	item := TrayItem{Service: string(sender), Path: "/StatusNotifierItem", tray: w.t}
	if strings.HasPrefix(service, "/") {
		item.Path = dbus.ObjectPath(service)
	} else if service != "" {
		item.Service = service
	}
	var pid uint32
	if err := w.t.conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, item.Service).Store(&pid); err == nil {
		item.PID = int(pid)
	}
	w.t.mu.Lock()
	w.t.items = append(w.t.items, item)
	names := make([]string, 0, len(w.t.items))
	for _, i := range w.t.items {
		names = append(names, i.Service+string(i.Path))
	}
	w.t.mu.Unlock()
	w.t.props.SetMust(watcherName, "RegisteredStatusNotifierItems", names)
	_ = w.t.conn.Emit(watcherPath, watcherName+".StatusNotifierItemRegistered", item.Service+string(item.Path))
	return nil
}

// RegisterStatusNotifierHost takes a host: the tray is one already.
func (w watcher) RegisterStatusNotifierHost(string) *dbus.Error { return nil }

// ItemOf is the status item one of the processes registered.
func (t *Tray) ItemOf(pids ...int) (TrayItem, bool) {
	if t == nil {
		return TrayItem{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, i := range t.items {
		for _, pid := range pids {
			if i.PID == pid {
				return i, true
			}
		}
	}
	return TrayItem{}, false
}

// get stores the item's property in to, and says whether it has it.
func (i TrayItem) get(name string, to any) bool {
	v, err := i.tray.conn.Object(i.Service, i.Path).GetProperty(itemIface + "." + name)
	return err == nil && v.Value() != nil && v.Store(to) == nil
}

// Name is what the item is called: its tooltip's title, as a person reads it
// by the icon, else its title, else its id.
func (i TrayItem) Name() string {
	var tip struct {
		Icon   string
		Pixmap []struct {
			W, H int32
			Data []byte
		}
		Title, Text string
	}
	if i.get("ToolTip", &tip) && tip.Title != "" {
		return tip.Title
	}
	for _, p := range []string{"Title", "Id"} {
		var s string
		if i.get(p, &s) && s != "" {
			return s
		}
	}
	return ""
}

// menu is the item's dbusmenu object.
func (i TrayItem) menu() (dbus.BusObject, error) {
	var path dbus.ObjectPath
	if !i.get("Menu", &path) || path == "" || path == "/" {
		return nil, fmt.Errorf("the tray icon %q has no menu", i.Name())
	}
	return i.tray.conn.Object(i.Service, path), nil
}

// Menu is the item's menu, as it is about to show.
func (i TrayItem) Menu() ([]TrayEntry, error) {
	m, err := i.menu()
	if err != nil {
		return nil, err
	}
	_ = m.Call(dbusmenuName+".AboutToShow", 0, int32(0)).Err
	var revision uint32
	var layout menuLayout
	if err := m.Call(dbusmenuName+".GetLayout", 0, int32(0), int32(-1), []string{}).Store(&revision, &layout); err != nil {
		return nil, fmt.Errorf("cannot read the tray icon %q's menu: %w", i.Name(), err)
	}
	return layout.entries(), nil
}

// Choose chooses an entry of the item's menu, as a click on it does.
func (i TrayItem) Choose(id int32) error {
	m, err := i.menu()
	if err != nil {
		return err
	}
	return m.Call(dbusmenuName+".Event", 0, id, "clicked", dbus.MakeVariant(int32(0)), uint32(time.Now().Unix())).Err
}

// menuLayout is dbusmenu's (ia{sv}av): an entry, its properties and those
// under it.
type menuLayout struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

func (l menuLayout) entries() []TrayEntry {
	var out []TrayEntry
	for _, c := range l.Children {
		var k menuLayout
		if dbus.Store([]any{c.Value()}, &k) != nil {
			continue
		}
		var kind, label string
		visible, enabled := true, true
		for name, to := range map[string]any{"type": &kind, "label": &label, "visible": &visible, "enabled": &enabled} {
			if v, ok := k.Props[name]; ok && v.Value() != nil {
				_ = v.Store(to)
			}
		}
		if kind == "separator" || !visible {
			continue
		}
		// An underscore marks the key that chooses the entry.
		label = strings.ReplaceAll(strings.ReplaceAll(label, "__", "\x00"), "_", "")
		out = append(out, TrayEntry{ID: k.ID, Label: strings.ReplaceAll(label, "\x00", "_"), Enabled: enabled, Entries: k.entries()})
	}
	return out
}
