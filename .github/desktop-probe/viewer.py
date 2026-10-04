# A window with a WebKitGTK 4.1 web view, as Tauri and Wails make on Linux, showing the page its
# argument names.
import sys

import gi

gi.require_version("Gtk", "3.0")
gi.require_version("WebKit2", "4.1")
from gi.repository import Gtk, WebKit2  # noqa: E402

window = Gtk.Window(title="Probe")
window.set_default_size(900, 300)
view = WebKit2.WebView()
view.load_uri("file://" + sys.argv[1])
window.add(view)
window.connect("destroy", Gtk.main_quit)
window.show_all()
Gtk.main()
