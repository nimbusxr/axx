// The app of the web view tests: one web view, with the page beside it in the bundle.
import SwiftUI
import WebKit

struct Page: UIViewRepresentable {
    func makeUIView(context: Context) -> WKWebView {
        let view = WKWebView()
        let page = Bundle.main.url(forResource: "report", withExtension: "html")!
        view.loadFileURL(page, allowingReadAccessTo: page.deletingLastPathComponent())
        return view
    }
    func updateUIView(_ view: WKWebView, context: Context) {}
}

@main
struct ReportsApp: App {
    var body: some Scene {
        WindowGroup { Page() }
    }
}
