//go:build linux

package atspi

import (
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

// A status item's menu is its dbusmenu layout's entries as a person sees
// them: labels without their access key's underscore, no separators, nothing
// hidden.
func TestTrayMenuEntries(t *testing.T) {
	entry := func(id int32, props map[string]dbus.Variant, kids ...dbus.Variant) dbus.Variant {
		if kids == nil {
			kids = []dbus.Variant{}
		}
		return dbus.MakeVariant(menuLayout{ID: id, Props: props, Children: kids})
	}
	label := func(s string) map[string]dbus.Variant { return map[string]dbus.Variant{"label": dbus.MakeVariant(s)} }
	layout := menuLayout{ID: 0, Props: map[string]dbus.Variant{}, Children: []dbus.Variant{
		entry(1, label("_Pause tracking")),
		entry(2, map[string]dbus.Variant{"type": dbus.MakeVariant("separator")}),
		entry(3, map[string]dbus.Variant{"label": dbus.MakeVariant("Hidden"), "visible": dbus.MakeVariant(false)}),
		entry(4, map[string]dbus.Variant{"label": dbus.MakeVariant("Sync__now"), "enabled": dbus.MakeVariant(false)}),
		entry(5, label("Depots"), entry(6, label("Leipzig"))),
	}}
	want := []TrayEntry{
		{ID: 1, Label: "Pause tracking", Enabled: true},
		{ID: 4, Label: "Sync_now", Enabled: false},
		{ID: 5, Label: "Depots", Enabled: true, Entries: []TrayEntry{{ID: 6, Label: "Leipzig", Enabled: true}}},
	}
	if got := layout.entries(); !reflect.DeepEqual(got, want) {
		t.Errorf("entries:\n%+v\nwant:\n%+v", got, want)
	}
}
