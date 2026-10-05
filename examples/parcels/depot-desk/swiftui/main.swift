// The depot desk in SwiftUI (../README.md).
import SwiftUI

let towns = ["Leipzig", "Halle", "Markkleeberg", "Taucha", "Schkeuditz", "Delitzsch", "Borna",
             "Grimma", "Wurzen", "Eilenburg"]

struct Expected: Identifiable {
    let reference: String
    let town: String
    var id: String { reference }
}

let expected = (0..<40).map { Expected(reference: "PX-DSK-\(4101 + $0)", town: towns[$0 % towns.count]) }

struct Arrival: Codable, Identifiable, Hashable {
    var reference: String
    var level: String
    var fragile: Bool
    var id: String { reference }
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

final class Desk: ObservableObject {
    @Published var arrivals = Store.load()
    @Published var status = ""

    init() {
        status = arrivals.isEmpty ? "No parcels registered yet" : "\(parcels(arrivals.count)) registered today"
    }

    func closeDay() {
        let n = arrivals.count
        arrivals = []
        Store.save(arrivals)
        status = "Day closed: \(parcels(n)) handed over"
    }
}

@main
struct DepotDeskApp: App {
    @StateObject var desk = Desk()

    var body: some Scene {
        Window("Depot desk", id: "desk") {
            DeskView().environmentObject(desk)
        }
        .defaultSize(width: 640, height: 580)
        .commands {
            CommandMenu("Depot") {
                Button("Close day") { desk.closeDay() }.disabled(desk.arrivals.isEmpty)
            }
        }
    }
}

struct DeskView: View {
    var body: some View {
        VStack(alignment: .leading) {
            Image(nsImage: NSImage(contentsOf: Bundle.main.url(forResource: "depot", withExtension: "png")!)!)
                .accessibilityLabel("Leipzig depot")
            TabView {
                ArrivalsView().tabItem { Text("Arrivals") }
                HandoverView().tabItem { Text("Handover") }
            }
        }
        .padding()
        // The window's size: without it, SwiftUI makes the window as tall as the scrolling
        // column, which then has nothing to scroll.
        .frame(minWidth: 480, idealWidth: 640, minHeight: 300, idealHeight: 580)
    }
}

struct ArrivalsView: View {
    @EnvironmentObject var desk: Desk
    @AppStorage("serviceLevel") var lastLevel = "Standard"
    @State var reference = ""
    @State var fragile = false
    @State var level = UserDefaults.standard.string(forKey: "serviceLevel") ?? "Standard"
    @State var printLabel = false
    @State var arrivalSelection: String?
    @State var expectedSelection: String?

    var body: some View {
        // One column, taller than the window: it scrolls, as a page does.
        ScrollView {
            VStack(alignment: .leading) {
                form
                lists
            }
            .padding()
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    var form: some View {
        VStack(alignment: .leading) {
            TextField("Reference", text: $reference)
                .onSubmit(register)
                .onExitCommand { reference = "" }
            Toggle("Fragile", isOn: $fragile).toggleStyle(.checkbox)
            Picker("Service level", selection: $level) {
                Text("Standard").tag("Standard")
                Text("Express").tag("Express")
            }
            .pickerStyle(.radioGroup)
            .horizontalRadioGroupLayout()
            Toggle("Print label", isOn: $printLabel).toggleStyle(.switch)
            Button("Register", action: register).disabled(reference.trimmingCharacters(in: .whitespaces).isEmpty)
            Text(desk.status)
        }
    }

    var lists: some View {
        VStack(alignment: .leading) {
            Text("Arrivals")
            List(desk.arrivals, selection: $arrivalSelection) { a in
                Text(a.reference)
            }
            .accessibilityLabel("Arrivals")
            .frame(height: 160)
            .onChange(of: arrivalSelection) { _, id in
                guard let a = desk.arrivals.first(where: { $0.reference == id }) else { return }
                desk.status = "\(a.reference): \(a.level)" + (a.fragile ? ", fragile" : "")
                arrivalSelection = nil
            }
            Text("Expected today")
            Table(expected, selection: $expectedSelection) {
                TableColumn("Reference", value: \.reference)
                TableColumn("Town", value: \.town)
            }
            .accessibilityLabel("Expected today")
            .frame(height: 8 * 24 + 28)
            .onChange(of: expectedSelection) { _, id in
                guard let id else { return }
                reference = id
                expectedSelection = nil
            }
        }
    }

    func register() {
        let ref = reference.trimmingCharacters(in: .whitespaces)
        guard !ref.isEmpty else { return }
        if desk.arrivals.contains(where: { $0.reference == ref }) {
            desk.status = "\(ref) is already registered"
            return
        }
        desk.arrivals.append(Arrival(reference: ref, level: level, fragile: fragile))
        Store.save(desk.arrivals)
        lastLevel = level
        desk.status = "Registered \(ref): \(level)" + (fragile ? ", fragile" : "") + (printLabel ? ", label printed" : "")
        reference = ""
        fragile = false
    }
}

/// Where the courier signs: a click puts a dot, a drag draws a stroke.
struct SignaturePad: View {
    @Binding var strokes: [[CGPoint]]
    @State var drawing = false

    var body: some View {
        Canvas { context, _ in
            for stroke in strokes {
                var path = Path()
                path.move(to: stroke[0])
                path.addLine(to: stroke[0])
                stroke.dropFirst().forEach { path.addLine(to: $0) }
                context.stroke(path, with: .color(.black), style: StrokeStyle(lineWidth: 3, lineCap: .round))
            }
        }
        .background(Color.white)
        .frame(width: 400, height: 160)
        .gesture(DragGesture(minimumDistance: 0)
            .onChanged { drag in
                if drawing {
                    strokes[strokes.count - 1].append(drag.location)
                } else {
                    strokes.append([drag.location])
                    drawing = true
                }
            }
            .onEnded { _ in drawing = false })
        .accessibilityElement()
        .accessibilityLabel("Courier signature")
        .accessibilityAddTraits(.isImage)
    }
}

struct HandoverView: View {
    @State var strokes: [[CGPoint]] = []
    @State var showRules = false

    var body: some View {
        VStack(alignment: .leading) {
            SignaturePad(strokes: $strokes)
            Text(strokes.isEmpty ? "Not signed" : "Signed")
            Button("Clear signature") { strokes = [] }
            Link("Handover rules", destination: URL(string: "depotdesk:rules")!)
                .environment(\.openURL, OpenURLAction { _ in
                    showRules = true
                    return .handled
                })
            if showRules {
                Text("Parcels are handed over to the courier at 18:00.")
            }
        }
        .padding()
    }
}
