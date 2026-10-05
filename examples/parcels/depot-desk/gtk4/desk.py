"""The depot desk in GTK 4 (../README.md)."""

import json
import os
import sys

import gi

gi.require_version("Gtk", "4.0")
from gi.repository import Gio, GLib, GObject, Gtk  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))
TOWNS = ["Leipzig", "Halle", "Markkleeberg", "Taucha", "Schkeuditz", "Delitzsch", "Borna",
         "Grimma", "Wurzen", "Eilenburg"]
EXPECTED = [("PX-DSK-%d" % (4101 + i), TOWNS[i % len(TOWNS)]) for i in range(40)]
DATA = os.path.join(GLib.get_user_data_dir(), "depot-desk")
CONFIG = os.path.join(GLib.get_user_config_dir(), "depot-desk")


def parcels(n):
    return "1 parcel" if n == 1 else "%d parcels" % n


def load():
    try:
        with open(os.path.join(DATA, "arrivals.json")) as f:
            return json.load(f)
    except FileNotFoundError:
        return []


def save(arrivals):
    os.makedirs(DATA, exist_ok=True)
    with open(os.path.join(DATA, "arrivals.json"), "w") as f:
        json.dump(arrivals, f)


def named(widget, name):
    widget.update_property([Gtk.AccessibleProperty.LABEL], [name])
    return widget


def column(*widgets, spacing=6):
    box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=spacing)
    for w in widgets:
        box.append(w)
    return box


class Expected(GObject.Object):
    def __init__(self, reference, town):
        super().__init__()
        self.reference = reference
        self.town = town


class Desk(Gtk.ApplicationWindow):
    def __init__(self, app):
        super().__init__(application=app, title="Depot desk")
        self.set_default_size(640, 580)
        self.set_show_menubar(True)
        self.arrivals = load()
        self.settings = GLib.KeyFile()
        try:
            self.settings.load_from_file(os.path.join(CONFIG, "settings.ini"), GLib.KeyFileFlags.NONE)
        except GLib.Error:
            pass
        self.close_day = app.lookup_action("close-day")
        self.close_day.connect("activate", self.on_close_day)

        logo = named(Gtk.Image.new_from_file(os.path.join(HERE, "..", "depot.png")), "Leipzig depot")
        logo.set_pixel_size(64)
        logo.set_halign(Gtk.Align.START)
        tabs = Gtk.Notebook(vexpand=True)
        tabs.append_page(self.arrivals_tab(), Gtk.Label(label="Arrivals"))
        tabs.append_page(self.handover_tab(), Gtk.Label(label="Handover"))
        root = column(logo, tabs)
        root.set_margin_start(12)
        root.set_margin_end(12)
        root.set_margin_top(12)
        root.set_margin_bottom(12)
        self.set_child(root)
        self.refresh()
        self.status.set_text(("%s registered today" % parcels(len(self.arrivals)))
                             if self.arrivals else "No parcels registered yet")

    def arrivals_tab(self):
        label = Gtk.Label(label="Reference", xalign=0)
        self.reference = named(Gtk.Entry(), "Reference")
        self.reference.connect("changed", lambda _: self.refresh())
        self.reference.connect("activate", lambda _: self.register())
        keys = Gtk.EventControllerKey()
        keys.connect("key-pressed", self.on_key)
        self.reference.add_controller(keys)
        self.fragile = Gtk.CheckButton(label="Fragile")
        self.standard = Gtk.CheckButton(label="Standard")
        self.express = Gtk.CheckButton(label="Express", group=self.standard)
        (self.express if self.level_kept() == "Express" else self.standard).set_active(True)
        levels = Gtk.Box(spacing=12)
        levels.append(self.standard)
        levels.append(self.express)
        self.print_label = named(Gtk.Switch(), "Print label")
        switch_row = Gtk.Box(spacing=6)
        switch_row.append(self.print_label)
        switch_row.append(Gtk.Label(label="Print label"))
        self.register_button = Gtk.Button(label="Register")
        self.register_button.set_halign(Gtk.Align.START)
        self.register_button.connect("clicked", lambda _: self.register())
        self.status = Gtk.Label(xalign=0)

        self.list = named(Gtk.ListBox(), "Arrivals")
        self.list.connect("row-activated", self.on_arrival)
        list_scroll = Gtk.ScrolledWindow(child=self.list)
        list_scroll.set_size_request(-1, 160)

        store = Gio.ListStore(item_type=Expected)
        for ref, town in EXPECTED:
            store.append(Expected(ref, town))
        self.selection = Gtk.SingleSelection(model=store, autoselect=False, can_unselect=True)
        self.selection.set_selected(Gtk.INVALID_LIST_POSITION)
        self.selection.connect("selection-changed", self.on_expected)
        view = named(Gtk.ColumnView(model=self.selection), "Expected today")
        for title, attr in [("Reference", "reference"), ("Town", "town")]:
            factory = Gtk.SignalListItemFactory()
            factory.connect("setup", lambda _f, item: item.set_child(Gtk.Label(xalign=0)))
            factory.connect("bind", lambda _f, item, a=attr: item.get_child().set_text(getattr(item.get_item(), a)))
            view.append_column(Gtk.ColumnViewColumn(title=title, factory=factory))
        table_scroll = Gtk.ScrolledWindow(child=view)
        table_scroll.set_size_request(-1, 8 * 30 + 30)

        # One column, taller than the window: it scrolls, as a page does.
        box = column(label, self.reference, self.fragile, levels, switch_row, self.register_button, self.status,
                     Gtk.Label(label="Arrivals", xalign=0), list_scroll,
                     Gtk.Label(label="Expected today", xalign=0), table_scroll)
        for side in ("start", "end", "top", "bottom"):
            getattr(box, "set_margin_" + side)(12)
        return Gtk.ScrolledWindow(child=box, hscrollbar_policy=Gtk.PolicyType.NEVER, vexpand=True)

    def handover_tab(self):
        # Where the courier signs: a click puts a dot, a drag draws a stroke.
        self.strokes = []
        # An image, to accessibility: GTK 4 leaves a drawing area with no role
        # out of its tree.
        self.pad = named(Gtk.DrawingArea(accessible_role=Gtk.AccessibleRole.IMG), "Courier signature")
        self.pad.set_size_request(400, 160)
        self.pad.set_halign(Gtk.Align.START)
        self.pad.set_draw_func(self.on_draw)
        drag = Gtk.GestureDrag()
        drag.connect("drag-begin", self.on_drag_begin)
        drag.connect("drag-update", self.on_drag_update)
        self.pad.add_controller(drag)
        self.signed = Gtk.Label(label="Not signed", xalign=0)
        clear = Gtk.Button(label="Clear signature")
        clear.set_halign(Gtk.Align.START)
        clear.connect("clicked", self.on_clear)
        rules = Gtk.LinkButton(uri="depotdesk:rules", label="Handover rules")
        rules.set_halign(Gtk.Align.START)
        rules.connect("activate-link", self.on_rules)
        self.rules_text = Gtk.Label(label="Parcels are handed over to the courier at 18:00.", xalign=0)
        self.rules_text.set_visible(False)
        return column(self.pad, self.signed, clear, rules, self.rules_text)

    def level_kept(self):
        try:
            return self.settings.get_string("desk", "serviceLevel")
        except GLib.Error:
            return "Standard"

    def on_key(self, _, keyval, keycode, state):
        if keyval == 0xff1b:  # Escape
            self.reference.set_text("")
            return True
        return False

    def refresh(self):
        self.register_button.set_sensitive(bool(self.reference.get_text().strip()))
        self.close_day.set_enabled(bool(self.arrivals))
        self.list.remove_all()
        for a in self.arrivals:
            self.list.append(Gtk.Label(label=a["reference"], xalign=0))

    def register(self):
        ref = self.reference.get_text().strip()
        if not ref:
            return
        if any(a["reference"] == ref for a in self.arrivals):
            self.status.set_text("%s is already registered" % ref)
            return
        level = "Express" if self.express.get_active() else "Standard"
        fragile = self.fragile.get_active()
        self.arrivals.append({"reference": ref, "level": level, "fragile": fragile})
        save(self.arrivals)
        self.settings.set_string("desk", "serviceLevel", level)
        os.makedirs(CONFIG, exist_ok=True)
        self.settings.save_to_file(os.path.join(CONFIG, "settings.ini"))
        self.status.set_text("Registered %s: %s%s%s" % (ref, level, ", fragile" if fragile else "",
                                                       ", label printed" if self.print_label.get_active() else ""))
        self.reference.set_text("")
        self.fragile.set_active(False)
        self.refresh()

    def on_arrival(self, _, row):
        a = self.arrivals[row.get_index()]
        self.status.set_text("%s: %s%s" % (a["reference"], a["level"], ", fragile" if a["fragile"] else ""))

    def on_expected(self, selection, *_):
        i = selection.get_selected()
        if i != Gtk.INVALID_LIST_POSITION:
            self.reference.set_text(EXPECTED[i][0])
            GLib.idle_add(lambda: selection.set_selected(Gtk.INVALID_LIST_POSITION))

    def on_close_day(self, *_):
        n = len(self.arrivals)
        self.arrivals = []
        save(self.arrivals)
        self.status.set_text("Day closed: %s handed over" % parcels(n))
        self.refresh()

    def on_draw(self, _, cr, width, height):
        cr.set_source_rgb(1, 1, 1)
        cr.paint()
        cr.set_source_rgb(0, 0, 0)
        cr.set_line_width(3)
        cr.set_line_cap(1)  # round
        for stroke in self.strokes:
            cr.move_to(*stroke[0])
            for p in stroke:
                cr.line_to(*p)
            cr.stroke()

    def on_drag_begin(self, _, x, y):
        self.start = (x, y)
        self.strokes.append([(x, y)])
        self.signed.set_text("Signed")
        self.pad.queue_draw()

    def on_drag_update(self, _, dx, dy):
        self.strokes[-1].append((self.start[0] + dx, self.start[1] + dy))
        self.pad.queue_draw()

    def on_clear(self, _):
        self.strokes = []
        self.signed.set_text("Not signed")
        self.pad.queue_draw()

    def on_rules(self, _):
        self.rules_text.set_visible(True)
        return True


def on_startup(app):
    app.add_action(Gio.SimpleAction.new("close-day", None))
    depot = Gio.Menu()
    depot.append("Close day", "app.close-day")
    bar = Gio.Menu()
    bar.append_submenu("Depot", depot)
    app.set_menubar(bar)


app = Gtk.Application(application_id="example.parcels.depotdesk.gtk4")
app.connect("startup", on_startup)
app.connect("activate", lambda a: Desk(a).present())
sys.exit(app.run([]))
