// SPDX-License-Identifier: Apache-2.0

import UIKit
import UIKit.UIGestureRecognizerSubclass

/// Tells the app's log of each touch its window is given, in a debug build: where it began, in
/// which view, and how it ended. It only watches: it takes no touch, and no other gesture waits
/// for it. A tap that did nothing is found here: the touch never came, it began in a view over
/// the one meant, it was cancelled, or it ended and the view did nothing with it.
final class TouchLog: UIGestureRecognizer, UIGestureRecognizerDelegate {
    /// Watches every window the app has now: once it shows.
    @MainActor static func watch() {
        #if DEBUG
        for case let scene as UIWindowScene in UIApplication.shared.connectedScenes {
            for window in scene.windows where !(window.gestureRecognizers ?? []).contains(where: { $0 is TouchLog }) {
                window.addGestureRecognizer(TouchLog())
            }
        }
        #endif
    }

    private var began: [UITouch: TimeInterval] = [:]

    private init() {
        super.init(target: nil, action: nil)
        cancelsTouchesInView = false
        delaysTouchesBegan = false
        delaysTouchesEnded = false
        delegate = self
    }

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent) {
        for touch in touches {
            began[touch] = touch.timestamp
            let view = touch.view.map { String(String(describing: type(of: $0)).prefix(80)) } ?? "no view"
            log.notice("touch began at \(Self.place(touch), privacy: .public) in \(view, privacy: .public)")
        }
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent) {
        finish(touches, "ended")
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent) {
        finish(touches, "was cancelled")
    }

    private func finish(_ touches: Set<UITouch>, _ how: String) {
        for touch in touches {
            let ms = Int(((touch.timestamp - (began.removeValue(forKey: touch) ?? touch.timestamp)) * 1000).rounded())
            log.notice("touch \(how, privacy: .public) at \(Self.place(touch), privacy: .public) after \(ms)ms")
        }
        if began.isEmpty { state = .failed }
    }

    override func reset() {
        began = [:]
    }

    private static func place(_ touch: UITouch) -> String {
        let p = touch.location(in: nil)
        return "\(Int(p.x.rounded())), \(Int(p.y.rounded()))"
    }

    override func canPrevent(_ preventedGestureRecognizer: UIGestureRecognizer) -> Bool { false }

    override func canBePrevented(by preventingGestureRecognizer: UIGestureRecognizer) -> Bool { false }

    func gestureRecognizer(_ gestureRecognizer: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool { true }
}
