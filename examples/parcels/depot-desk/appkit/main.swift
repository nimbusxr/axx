// The depot desk in AppKit (../README.md).
import AppKit

let towns = ["Leipzig", "Halle", "Markkleeberg", "Taucha", "Schkeuditz", "Delitzsch", "Borna",
             "Grimma", "Wurzen", "Eilenburg"]
let expected = (0..<40).map { ("PX-DSK-\(4101 + $0)", towns[$0 % towns.count]) }

struct Arrival: Codable {
    var reference: String
    var level: String
    var fragile: Bool
}

func parcels(_ n: Int) -> String { n == 1 ? "1 parcel" : "\(n) parcels" }

/// The day's arrivals, in arrivals.json in the desk's data folder.
enum Store {
    static var url: URL {
        let folder = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("Depot desk")
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        return folder.appendingPathComponent("arrivals.json")
    }
    static func load() -> [Arrival] {
        guard let data = try? Data(contentsOf: url) else { return [] }
        return (try? JSONDecoder().decode([Arrival].self, from: data)) ?? []
    }
    static func save(_ arrivals: [Arrival]) {
        try? JSONEncoder().encode(arrivals).write(to: url)
    }
}

/// Where the courier signs: a click puts a dot, a drag draws a stroke.
final class SignaturePad: NSView {
    var strokes: [[NSPoint]] = []
    var changed: () -> Void = {}
    override var isFlipped: Bool { true }
    override func isAccessibilityElement() -> Bool { true }
    override func accessibilityRole() -> NSAccessibility.Role? { .image }
    override func accessibilityLabel() -> String? { "Courier signature" }
    override func draw(_ dirtyRect: NSRect) {
        NSColor.white.setFill()
        bounds.fill()
        NSColor.black.setStroke()
        for stroke in strokes {
            let path = NSBezierPath()
            path.lineWidth = 3
            path.lineCapStyle = .round
            path.move(to: stroke[0])
            path.line(to: stroke[0])
            stroke.dropFirst().forEach { path.line(to: $0) }
            path.stroke()
        }
    }
    override func mouseDown(with event: NSEvent) {
        strokes.append([convert(event.locationInWindow, from: nil)])
        needsDisplay = true
        changed()
    }
    override func mouseDragged(with event: NSEvent) {
        strokes[strokes.count - 1].append(convert(event.locationInWindow, from: nil))
        needsDisplay = true
    }
    func clear() {
        strokes = []
        needsDisplay = true
    }
}

/// A view whose content starts at its top, as a page's does.
final class FlippedView: NSView {
    override var isFlipped: Bool { true }
}

final class Desk: NSObject, NSApplicationDelegate, NSTextFieldDelegate, NSTableViewDataSource,
    NSTableViewDelegate, NSTextViewDelegate, NSMenuItemValidation {
    var window: NSWindow!
    var arrivals = Store.load()
    let reference = NSTextField()
    let fragile = NSButton(checkboxWithTitle: "Fragile", target: nil, action: nil)
    let standard = NSButton(radioButtonWithTitle: "Standard", target: nil, action: #selector(level(_:)))
    let express = NSButton(radioButtonWithTitle: "Express", target: nil, action: #selector(level(_:)))
    let printLabel = NSSwitch()
    let register = NSButton(title: "Register", target: nil, action: #selector(registerParcel))
    let status = NSTextField(labelWithString: "")
    let arrivalsTable = NSTableView()
    let expectedTable = NSTableView()
    let pad = SignaturePad()
    let signed = NSTextField(labelWithString: "Not signed")
    let rulesText = NSTextField(labelWithString: "Parcels are handed over to the courier at 18:00.")

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.mainMenu = menu()
        window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 640, height: 580),
                          styleMask: [.titled, .closable, .miniaturizable, .resizable], backing: .buffered, defer: false)
        window.title = "Depot desk"

        let logo = NSImageView(image: NSImage(contentsOf: Bundle.main.url(forResource: "depot", withExtension: "png")!)!)
        logo.setAccessibilityLabel("Leipzig depot")
        let tabs = NSTabView()
        let arrivalsTab = NSTabViewItem(identifier: "arrivals")
        arrivalsTab.label = "Arrivals"
        arrivalsTab.view = arrivalsView()
        let handoverTab = NSTabViewItem(identifier: "handover")
        handoverTab.label = "Handover"
        handoverTab.view = handoverView()
        tabs.addTabViewItem(arrivalsTab)
        tabs.addTabViewItem(handoverTab)

        let root = NSStackView(views: [logo, tabs])
        root.orientation = .vertical
        root.alignment = .leading
        root.edgeInsets = NSEdgeInsets(top: 12, left: 12, bottom: 12, right: 12)
        tabs.widthAnchor.constraint(equalTo: root.widthAnchor, constant: -24).isActive = true
        window.contentView = root
        window.center()
        window.makeKeyAndOrderFront(nil)
        refresh()
        status.stringValue = arrivals.isEmpty ? "No parcels registered yet" : "\(parcels(arrivals.count)) registered today"
        NSApp.activate(ignoringOtherApps: true)
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func menu() -> NSMenu {
        let main = NSMenu()
        let appItem = NSMenuItem()
        appItem.submenu = NSMenu()
        appItem.submenu!.addItem(withTitle: "Quit Depot desk", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        main.addItem(appItem)
        let depotItem = NSMenuItem()
        let depot = NSMenu(title: "Depot")
        depot.addItem(withTitle: "Close day", action: #selector(closeDay), keyEquivalent: "").target = self
        depotItem.submenu = depot
        main.addItem(depotItem)
        return main
    }

    func arrivalsView() -> NSView {
        let label = NSTextField(labelWithString: "Reference")
        reference.setAccessibilityLabel("Reference")
        reference.delegate = self
        reference.target = self
        reference.action = #selector(registerParcel)
        standard.target = self
        express.target = self
        (UserDefaults.standard.string(forKey: "serviceLevel") == "Express" ? express : standard).state = .on
        printLabel.setAccessibilityLabel("Print label")
        let printRow = NSStackView(views: [printLabel, NSTextField(labelWithString: "Print label")])
        register.target = self

        arrivalsTable.addTableColumn(NSTableColumn(identifier: .init("reference")))
        arrivalsTable.headerView = nil
        arrivalsTable.setAccessibilityLabel("Arrivals")
        arrivalsTable.dataSource = self
        arrivalsTable.delegate = self
        let arrivalsScroll = scroll(arrivalsTable, height: 160)

        for (id, title) in [("reference", "Reference"), ("town", "Town")] {
            let column = NSTableColumn(identifier: .init(id))
            column.title = title
            column.width = 200
            expectedTable.addTableColumn(column)
        }
        expectedTable.setAccessibilityLabel("Expected today")
        expectedTable.dataSource = self
        expectedTable.delegate = self
        let expectedScroll = scroll(expectedTable, height: 8 * (expectedTable.rowHeight + expectedTable.intercellSpacing.height) + 28)

        // One column, taller than the window: it scrolls, in a page of its own.
        let column = NSStackView(views: [label, reference, fragile, NSStackView(views: [standard, express]), printRow,
                                         register, status, NSTextField(labelWithString: "Arrivals"), arrivalsScroll,
                                         NSTextField(labelWithString: "Expected today"), expectedScroll])
        column.orientation = .vertical
        column.alignment = .leading
        column.edgeInsets = NSEdgeInsets(top: 12, left: 12, bottom: 12, right: 12)
        column.translatesAutoresizingMaskIntoConstraints = false
        for v in [reference, arrivalsScroll, expectedScroll] {
            v.widthAnchor.constraint(equalTo: column.widthAnchor, constant: -24).isActive = true
        }
        let document = FlippedView()
        document.translatesAutoresizingMaskIntoConstraints = false
        document.addSubview(column)
        let page = NSScrollView()
        page.hasVerticalScroller = true
        page.drawsBackground = false
        page.documentView = document
        NSLayoutConstraint.activate([
            column.leadingAnchor.constraint(equalTo: document.leadingAnchor),
            column.trailingAnchor.constraint(equalTo: document.trailingAnchor),
            column.topAnchor.constraint(equalTo: document.topAnchor),
            column.bottomAnchor.constraint(equalTo: document.bottomAnchor),
            document.widthAnchor.constraint(equalTo: page.contentView.widthAnchor),
        ])
        return page
    }

    func scroll(_ table: NSTableView, height: CGFloat) -> NSScrollView {
        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.heightAnchor.constraint(equalToConstant: height).isActive = true
        return scroll
    }

    func handoverView() -> NSView {
        pad.changed = { [unowned self] in signed.stringValue = "Signed" }
        pad.widthAnchor.constraint(equalToConstant: 400).isActive = true
        pad.heightAnchor.constraint(equalToConstant: 160).isActive = true
        let clear = NSButton(title: "Clear signature", target: self, action: #selector(clearSignature))

        // A link, in text as in a document: AX has it as a link.
        let rules = NSTextView()
        rules.isEditable = false
        rules.isSelectable = true
        rules.drawsBackground = false
        rules.delegate = self
        rules.textStorage?.setAttributedString(NSAttributedString(string: "Handover rules",
            attributes: [.link: URL(string: "depotdesk:rules")!, .font: NSFont.systemFont(ofSize: NSFont.systemFontSize)]))
        rules.widthAnchor.constraint(equalToConstant: 110).isActive = true
        rules.heightAnchor.constraint(equalToConstant: 20).isActive = true
        rulesText.isHidden = true

        let view = NSStackView(views: [pad, signed, clear, rules, rulesText])
        view.orientation = .vertical
        view.alignment = .leading
        view.edgeInsets = NSEdgeInsets(top: 12, left: 12, bottom: 12, right: 12)
        return view
    }

    func textView(_ textView: NSTextView, clickedOnLink link: Any, at charIndex: Int) -> Bool {
        rulesText.isHidden = false
        return true
    }

    func refresh() {
        register.isEnabled = !reference.stringValue.trimmingCharacters(in: .whitespaces).isEmpty
        arrivalsTable.reloadData()
    }

    func validateMenuItem(_ menuItem: NSMenuItem) -> Bool {
        menuItem.action == #selector(closeDay) ? !arrivals.isEmpty : true
    }

    func controlTextDidChange(_ obj: Notification) { refresh() }

    func control(_ control: NSControl, textView: NSTextView, doCommandBy selector: Selector) -> Bool {
        if selector == #selector(NSResponder.cancelOperation(_:)) {
            reference.stringValue = ""
            refresh()
            return true
        }
        return false
    }

    @objc func level(_ sender: NSButton) {
        standard.state = sender === standard ? .on : .off
        express.state = sender === express ? .on : .off
    }

    @objc func registerParcel() {
        let ref = reference.stringValue.trimmingCharacters(in: .whitespaces)
        guard !ref.isEmpty else { return }
        if arrivals.contains(where: { $0.reference == ref }) {
            status.stringValue = "\(ref) is already registered"
            return
        }
        let level = express.state == .on ? "Express" : "Standard"
        arrivals.append(Arrival(reference: ref, level: level, fragile: fragile.state == .on))
        Store.save(arrivals)
        UserDefaults.standard.set(level, forKey: "serviceLevel")
        status.stringValue = "Registered \(ref): \(level)" + (fragile.state == .on ? ", fragile" : "")
            + (printLabel.state == .on ? ", label printed" : "")
        reference.stringValue = ""
        fragile.state = .off
        refresh()
    }

    @objc func closeDay() {
        let n = arrivals.count
        arrivals = []
        Store.save(arrivals)
        status.stringValue = "Day closed: \(parcels(n)) handed over"
        refresh()
    }

    @objc func clearSignature() {
        pad.clear()
        signed.stringValue = "Not signed"
    }

    func numberOfRows(in tableView: NSTableView) -> Int {
        tableView === arrivalsTable ? arrivals.count : expected.count
    }

    func tableView(_ tableView: NSTableView, viewFor column: NSTableColumn?, row: Int) -> NSView? {
        let text: String
        if tableView === arrivalsTable {
            text = arrivals[row].reference
        } else {
            text = column?.identifier.rawValue == "town" ? expected[row].1 : expected[row].0
        }
        return NSTextField(labelWithString: text)
    }

    func tableViewSelectionDidChange(_ notification: Notification) {
        guard let table = notification.object as? NSTableView, table.selectedRow >= 0 else { return }
        if table === arrivalsTable {
            let a = arrivals[table.selectedRow]
            status.stringValue = "\(a.reference): \(a.level)" + (a.fragile ? ", fragile" : "")
        } else {
            reference.stringValue = expected[table.selectedRow].0
            refresh()
        }
        table.deselectAll(nil)
    }
}

let app = NSApplication.shared
let desk = Desk()
app.delegate = desk
app.setActivationPolicy(.regular)
app.run()
