"""The depot desk in GTK 3 (../README.md)."""

import json
import os

import gi

gi.require_version("Gtk", "3.0")
gi.require_version("Gdk", "3.0")
from gi.repository import Gdk, GLib, Gtk  # noqa: E402

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
    widget.get_accessible().set_name(name)
    return widget


class Desk(Gtk.Window):
    def __init__(self):
        super().__init__(title="Depot desk")
        self.set_default_size(640, 580)
        self.arrivals = load()
        self.settings = GLib.KeyFile()
        try:
            self.settings.load_from_file(os.path.join(CONFIG, "settings.ini"), GLib.KeyFileFlags.NONE)
        except GLib.Error:
            pass

        self.close_day = Gtk.MenuItem(label="Close day")
        self.close_day.connect("activate", self.on_close_day)
        depot_menu = Gtk.Menu()
        depot_menu.append(self.close_day)
        depot = Gtk.MenuItem(label="Depot")
        depot.set_submenu(depot_menu)
        bar = Gtk.MenuBar()
        bar.append(depot)

        logo = named(Gtk.Image.new_from_file(os.path.join(HERE, "..", "depot.png")), "Leipzig depot")
        logo.set_halign(Gtk.Align.START)
        tabs = Gtk.Notebook()
        tabs.append_page(self.arrivals_tab(), Gtk.Label(label="Arrivals"))
        tabs.append_page(self.handover_tab(), Gtk.Label(label="Handover"))

        root = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=6)
        root.pack_start(bar, False, False, 0)
        content = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=6, margin=12)
        content.pack_start(logo, False, False, 0)
        content.pack_start(tabs, True, True, 0)
        root.pack_start(content, True, True, 0)
        self.add(root)
        self.connect("destroy", Gtk.main_quit)
        self.refresh()
        self.status.set_text(("%s registered today" % parcels(len(self.arrivals)))
                             if self.arrivals else "No parcels registered yet")

    def arrivals_tab(self):
        box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=6, margin=12)
        label = Gtk.Label(label="Reference", xalign=0)
        self.reference = named(Gtk.Entry(), "Reference")
        label.set_mnemonic_widget(self.reference)
        self.reference.connect("changed", lambda _: self.refresh())
        self.reference.connect("activate", lambda _: self.register())
        self.reference.connect("key-press-event", self.on_key)
        self.fragile = Gtk.CheckButton(label="Fragile")
        self.standard = Gtk.RadioButton(label="Standard")
        self.express = Gtk.RadioButton(label="Express", group=self.standard)
        (self.express if self.level_kept() == "Express" else self.standard).set_active(True)
        levels = Gtk.Box(spacing=12)
        levels.pack_start(self.standard, False, False, 0)
        levels.pack_start(self.express, False, False, 0)
        self.print_label = named(Gtk.Switch(), "Print label")
        switch_row = Gtk.Box(spacing=6)
        switch_row.pack_start(self.print_label, False, False, 0)
        switch_row.pack_start(Gtk.Label(label="Print label"), False, False, 0)
        self.register_button = Gtk.Button(label="Register")
        self.register_button.set_halign(Gtk.Align.START)
        self.register_button.connect("clicked", lambda _: self.register())
        self.status = Gtk.Label(xalign=0)

        self.list = named(Gtk.ListBox(), "Arrivals")
        self.list.connect("row-activated", self.on_arrival)
        list_scroll = Gtk.ScrolledWindow()
        list_scroll.set_size_request(-1, 160)
        list_scroll.add(self.list)

        store = Gtk.ListStore(str, str)
        for ref, town in EXPECTED:
            store.append([ref, town])
        self.expected = named(Gtk.TreeView(model=store), "Expected today")
        for i, title in enumerate(["Reference", "Town"]):
            self.expected.append_column(Gtk.TreeViewColumn(title, Gtk.CellRendererText(), text=i))
        self.expected.get_selection().connect("changed", self.on_expected)
        table_scroll = Gtk.ScrolledWindow()
        table_scroll.set_size_request(-1, 8 * 26 + 28)
        table_scroll.add(self.expected)

        # One column, taller than the window: it scrolls, as a page does.
        for w in [label, self.reference, self.fragile, levels, switch_row, self.register_button, self.status,
                  Gtk.Label(label="Arrivals", xalign=0), list_scroll, Gtk.Label(label="Expected today", xalign=0),
                  table_scroll]:
            box.pack_start(w, False, False, 0)
        page = Gtk.ScrolledWindow(hscrollbar_policy=Gtk.PolicyType.NEVER)
        page.add(box)
        return page

    def handover_tab(self):
        box = Gtk.Box(orientation=Gtk.Orientation.VERTICAL, spacing=6, margin=12)
        # Where the courier signs: a click puts a dot, a drag draws a stroke.
        self.strokes = []
        self.pad = named(Gtk.DrawingArea(), "Courier signature")
        self.pad.set_size_request(400, 160)
        self.pad.set_halign(Gtk.Align.START)
        self.pad.add_events(Gdk.EventMask.BUTTON_PRESS_MASK | Gdk.EventMask.BUTTON1_MOTION_MASK)
        self.pad.connect("draw", self.on_draw)
        self.pad.connect("button-press-event", self.on_press)
        self.pad.connect("motion-notify-event", self.on_motion)
        self.signed = Gtk.Label(label="Not signed", xalign=0)
        clear = Gtk.Button(label="Clear signature")
        clear.set_halign(Gtk.Align.START)
        clear.connect("clicked", self.on_clear)
        rules = Gtk.LinkButton(uri="depotdesk:rules", label="Handover rules")
        rules.set_halign(Gtk.Align.START)
        rules.connect("activate-link", self.on_rules)
        self.rules_text = Gtk.Label(label="Parcels are handed over to the courier at 18:00.", xalign=0)
        self.rules_text.set_no_show_all(True)
        for w in [self.pad, self.signed, clear, rules, self.rules_text]:
            box.pack_start(w, False, False, 0)
        return box

    def level_kept(self):
        try:
            return self.settings.get_string("desk", "serviceLevel")
        except GLib.Error:
            return "Standard"

    def on_key(self, entry, event):
        if event.keyval == Gdk.KEY_Escape:
            entry.set_text("")
            return True
        return False

    def refresh(self):
        self.register_button.set_sensitive(bool(self.reference.get_text().strip()))
        self.close_day.set_sensitive(bool(self.arrivals))
        for row in self.list.get_children():
            self.list.remove(row)
        for a in self.arrivals:
            self.list.add(Gtk.Label(label=a["reference"], xalign=0))
        self.list.show_all()

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

    def on_expected(self, selection):
        model, it = selection.get_selected()
        if it is not None:
            self.reference.set_text(model[it][0])
            GLib.idle_add(selection.unselect_all)

    def on_close_day(self, _):
        n = len(self.arrivals)
        self.arrivals = []
        save(self.arrivals)
        self.status.set_text("Day closed: %s handed over" % parcels(n))
        self.refresh()

    def on_draw(self, _, cr):
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

    def on_press(self, _, event):
        self.strokes.append([(event.x, event.y)])
        self.signed.set_text("Signed")
        self.pad.queue_draw()

    def on_motion(self, _, event):
        if self.strokes:
            self.strokes[-1].append((event.x, event.y))
            self.pad.queue_draw()

    def on_clear(self, _):
        self.strokes = []
        self.signed.set_text("Not signed")
        self.pad.queue_draw()

    def on_rules(self, _):
        self.rules_text.show()
        return True


Desk().show_all()
Gtk.main()
