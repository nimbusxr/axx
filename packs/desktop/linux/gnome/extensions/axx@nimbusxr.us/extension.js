import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import Shell from 'gi://Shell';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

const iface = `<node><interface name="us.nimbusxr.axx.Desktop">
  <method name="Screenshot"><arg type="s" direction="in" name="path"/></method>
  <method name="Frames"><arg type="i" direction="in" name="pid"/><arg type="a(iiiiiiiii)" direction="out" name="frames"/></method>
  <method name="Display"><arg type="s" direction="out" name="display"/><arg type="s" direction="out" name="authority"/></method>
</interface></node>`;

export default class AxxExtension extends Extension {
    enable() {
        Main.panel.hide();
        Main.layoutManager.untrackChrome(Main.layoutManager.panelBox);
        this._mapped = global.window_manager.connect('map', (_wm, actor) => this._place(actor));
        this._dbus = Gio.DBusExportedObject.wrapJSObject(iface, this);
        this._dbus.export(Gio.DBus.session, '/us/nimbusxr/axx');
        this._owner = Gio.bus_own_name(Gio.BusType.SESSION, 'us.nimbusxr.axx.Desktop', Gio.BusNameOwnerFlags.NONE, null, null, null);
    }

    disable() {
        global.window_manager.disconnect(this._mapped);
        this._dbus.unexport();
        Gio.bus_unown_name(this._owner);
    }

    // Each window with its frame's corner at the screen's, as it shows (its
    // shadow outside it): the app's own coordinates, from its frame's corner,
    // are the screen's, as on axx's X11 desktops.
    _place(actor) {
        actor.meta_window.move_frame(true, 0, 0);
    }

    async ScreenshotAsync([path], invocation) {
        try {
            const stream = Gio.File.new_for_path(path).replace(null, false, Gio.FileCreateFlags.NONE, null);
            await new Shell.Screenshot().screenshot(false, stream);
            stream.close(null);
            invocation.return_value(null);
        } catch (e) {
            invocation.return_error_literal(Gio.IOErrorEnum, Gio.IOErrorEnum.FAILED, String(e));
        }
    }

    // The process's windows: each one's frame, its surface (buffer), and
    // whether it is an X11 app's, on Xwayland (Meta.WindowClientType: 1).
    Frames(pid) {
        return global.get_window_actors().map(a => a.meta_window).filter(w => w.get_pid() === pid).map(w => {
            const f = w.get_frame_rect(), b = w.get_buffer_rect();
            return [f.x, f.y, f.width, f.height, b.x, b.y, b.width, b.height, w.get_client_type()];
        });
    }

    // Xwayland's display, where X11 apps (Java's) show, and its X authority
    // file.
    Display() {
        return [GLib.getenv('DISPLAY') ?? '', GLib.getenv('XAUTHORITY') ?? ''];
    }
}
