// A window with a WKWebView, as Tauri and Wails make on macOS, showing the page its argument names.
import AppKit
import WebKit

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let window = NSWindow(contentRect: NSRect(x: 100, y: 100, width: 900, height: 300),
                      styleMask: [.titled, .closable], backing: .buffered, defer: false)
window.title = "Probe"
let web = WKWebView(frame: window.contentView!.bounds)
web.autoresizingMask = [.width, .height]
window.contentView!.addSubview(web)
let page = URL(fileURLWithPath: CommandLine.arguments[1])
web.loadFileURL(page, allowingReadAccessTo: page.deletingLastPathComponent())
window.makeKeyAndOrderFront(nil)
app.activate(ignoringOtherApps: true)
app.run()
